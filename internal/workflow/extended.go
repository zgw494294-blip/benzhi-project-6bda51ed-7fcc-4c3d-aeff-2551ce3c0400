package workflow

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"museum-label-governance/internal/label"
	"museum-label-governance/internal/ledger"
	"sort"
	"strings"
	"time"
)

type dossierCursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
	Filter    string    `json:"filter"`
}

func fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func queryFingerprint(q DossierQuery) string {
	return fingerprint(struct {
		Status         label.Status
		ExhibitionName string
		Owner          string
	}{q.Status, q.ExhibitionName, q.Owner})
}

func (s *Service) ListDossiers(q DossierQuery) (DossierPage, error) {
	if q.Limit < 1 || q.Limit > label.MaxListItems || (q.Status != "" && !label.ValidStatus(q.Status)) {
		return DossierPage{}, ErrValidation
	}
	filter := ledger.DossierFilter{Status: q.Status, ExhibitionName: strings.TrimSpace(q.ExhibitionName), Owner: strings.TrimSpace(q.Owner), Limit: q.Limit}
	if q.Cursor != "" {
		b, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil {
			return DossierPage{}, ErrValidation
		}
		var cursor dossierCursor
		if json.Unmarshal(b, &cursor) != nil || cursor.ID == "" || cursor.CreatedAt.IsZero() || cursor.Filter != queryFingerprint(q) {
			return DossierPage{}, ErrValidation
		}
		filter.AfterCreatedAt, filter.AfterID = cursor.CreatedAt, cursor.ID
	}
	items, more := s.Store.QueryDossiers(filter)
	page := DossierPage{Items: items}
	if more && len(items) > 0 {
		last := items[len(items)-1]
		b, _ := json.Marshal(dossierCursor{CreatedAt: last.CreatedAt, ID: last.ID, Filter: queryFingerprint(q)})
		page.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	detail := Encode(map[string]any{"status": q.Status, "exhibitionName": q.ExhibitionName, "owner": q.Owner, "limit": q.Limit, "cursor": q.Cursor})
	if err := s.Store.Update(func(st *ledger.State) error {
		event := label.AuditEvent{ID: id("qry_"), Action: "dossier.queried", Actor: q.Actor, Detail: detail, At: time.Now()}
		st.QueryAudits = append(st.QueryAudits, event)
		for _, dossier := range items {
			copy := event
			copy.DossierID = dossier.ID
			st.Audits[dossier.ID] = append(st.Audits[dossier.ID], copy)
		}
		return nil
	}); err != nil {
		return DossierPage{}, err
	}
	return page, nil
}

func dedupeIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, value := range ids {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func validateEvidenceIDs(st *ledger.State, did string, revision int, ids []string) error {
	available := map[string]bool{}
	for _, evidence := range st.Evidence[did] {
		if evidence.CreatedRevision <= revision && label.EvidenceIsUsable(evidence) {
			available[evidence.ID] = true
		}
	}
	for _, evidenceID := range ids {
		if !available[evidenceID] {
			return ledger.ErrNotFound
		}
	}
	return nil
}

func (s *Service) CreateClaimWithEvidence(did string, expected int, statement, category string, evidenceIDs []string, actor string) (label.Claim, label.Dossier, error) {
	var out label.Claim
	var dossier label.Dossier
	statement = label.NormalizeStatement(statement)
	category = label.NormalizeCategory(category)
	evidenceIDs = dedupeIDs(evidenceIDs)
	err := s.Store.Update(func(st *ledger.State) error {
		var ok bool
		dossier, ok = st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if dossier.Version != expected {
			return ledger.ErrConflict
		}
		if dossier.Status != label.StatusDraft {
			return ErrState
		}
		candidate := label.Claim{ID: id("clm_"), DossierID: did, RevisionNo: dossier.CurrentRevision, Statement: statement, Category: category, EvidenceIDs: evidenceIDs}
		if len(label.ValidateClaim(candidate)) > 0 {
			return ErrValidation
		}
		if err := validateEvidenceIDs(st, did, dossier.CurrentRevision, evidenceIDs); err != nil {
			return err
		}
		out = candidate
		st.Claims[did] = append(st.Claims[did], out)
		revision := &st.Revisions[did][dossier.CurrentRevision-1]
		revision.Claims = append(revision.Claims, out.ID)
		dossier.Version++
		dossier.UpdatedAt = time.Now()
		st.Dossiers[did] = dossier
		ledger.AddAudit(st, did, "claim.added", actor, Encode(map[string]any{"claimId": out.ID, "evidenceIds": out.EvidenceIDs}))
		return nil
	})
	return out, dossier, err
}

func (s *Service) ReplaceEvidenceLinks(did, claimID string, expected int, evidenceIDs []string, actor string) (label.Claim, label.Dossier, error) {
	var out label.Claim
	var dossier label.Dossier
	evidenceIDs = dedupeIDs(evidenceIDs)
	err := s.Store.Update(func(st *ledger.State) error {
		var ok bool
		dossier, ok = st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if dossier.Version != expected {
			return ledger.ErrConflict
		}
		if dossier.Status != label.StatusDraft {
			return ErrState
		}
		if err := validateEvidenceIDs(st, did, dossier.CurrentRevision, evidenceIDs); err != nil {
			return err
		}
		index := -1
		for i, claim := range st.Claims[did] {
			if claim.ID == claimID && claim.RevisionNo == dossier.CurrentRevision {
				index = i
				out = claim
				break
			}
		}
		if index < 0 {
			return ledger.ErrNotFound
		}
		if equalStrings(out.EvidenceIDs, evidenceIDs) {
			return nil
		}
		out.EvidenceIDs = evidenceIDs
		st.Claims[did][index] = out
		dossier.Version++
		dossier.UpdatedAt = time.Now()
		st.Dossiers[did] = dossier
		ledger.AddAudit(st, did, "claim.evidence_replaced", actor, Encode(map[string]any{"claimId": claimID, "evidenceIds": evidenceIDs}))
		return nil
	})
	return out, dossier, err
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *Service) RunPrecheckWithKey(did string, expected int, actor, key string) (PrecheckResult, error) {
	var result PrecheckResult
	if s.cachedOperation(key, &result) {
		return result, nil
	}
	fp := fingerprint(struct {
		Expected int
		Actor    string
	}{expected, actor})
	err := s.Store.Update(func(st *ledger.State) error {
		if record, ok := ledger.GetOperation(*st, "precheck", did, key); ok {
			if record.Fingerprint != fp {
				return ErrIdempotency
			}
			return json.Unmarshal(record.Result, &result)
		}
		dossier, ok := st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if dossier.Version != expected {
			return ledger.ErrConflict
		}
		if dossier.Status != label.StatusDraft && dossier.Status != label.StatusPrechecked {
			return ErrState
		}
		if dossier.RequiresRevision {
			return ErrState
		}
		revision := st.Revisions[did][dossier.CurrentRevision-1]
		evidence := map[string]label.Evidence{}
		for _, item := range st.Evidence[did] {
			evidence[item.ID] = item
		}
		problems := label.Precheck(dossier, revision, filterClaims(st.Claims[did], dossier.CurrentRevision), evidence)
		if problems == nil {
			problems = []label.Problem{}
		}
		if len(problems) == 0 {
			dossier.Status = label.StatusPrechecked
			ledger.AddAudit(st, did, "precheck.passed", actor, Encode(map[string]any{"problemCount": 0, "problems": problems}))
		} else {
			dossier.Status = label.StatusDraft
			ledger.AddAudit(st, did, "precheck.failed", actor, Encode(map[string]any{"problemCount": len(problems), "problems": problems}))
		}
		dossier.Version++
		dossier.UpdatedAt = time.Now()
		st.Dossiers[did] = dossier
		result = PrecheckResult{Dossier: dossier, Problems: problems, ProblemCount: len(problems)}
		st.Prechecks[did] = append(st.Prechecks[did], label.PrecheckSnapshot{DossierID: did, Version: dossier.Version, RevisionNo: dossier.CurrentRevision, Problems: problems, Status: dossier.Status, Count: len(problems), CreatedAt: time.Now()})
		return ledger.PutOperation(st, "precheck", did, key, fp, result)
	})
	if err == nil {
		s.rememberOperation(key, result)
	}
	return result, err
}

func (s *Service) ReviewClaimsBatch(did string, expected int, reviews []ReviewInput, actor, key string) (ExpertReviewResult, error) {
	var result ExpertReviewResult
	fp := fingerprint(struct {
		Expected int
		Actor    string
		Reviews  []ReviewInput
	}{expected, actor, reviews})
	err := s.Store.Update(func(st *ledger.State) error {
		if record, ok := ledger.GetOperation(*st, "expert-review", did, key); ok {
			if record.Fingerprint != fp {
				return ErrIdempotency
			}
			return json.Unmarshal(record.Result, &result)
		}
		dossier, ok := st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if dossier.Version != expected {
			return ledger.ErrConflict
		}
		if dossier.Status != label.StatusPrechecked && dossier.Status != label.StatusExpertReview {
			return ErrState
		}
		claims := filterClaims(st.Claims[did], dossier.CurrentRevision)
		if len(reviews) == 0 || len(reviews) > label.MaxReviewBatchItems || len(reviews) != len(claims) {
			return ErrValidation
		}
		claimIndexes := map[string]int{}
		for i, claim := range st.Claims[did] {
			if claim.RevisionNo == dossier.CurrentRevision {
				claimIndexes[claim.ID] = i
			}
		}
		seen := map[string]bool{}
		for _, review := range reviews {
			if _, ok := claimIndexes[review.ClaimID]; !ok {
				return ledger.ErrNotFound
			}
			if seen[review.ClaimID] || !ValidDecision(review.Decision) || strings.TrimSpace(review.Reason) == "" {
				return ErrValidation
			}
			seen[review.ClaimID] = true
		}
		now := time.Now()
		allPass := true
		for _, review := range reviews {
			index := claimIndexes[review.ClaimID]
			claim := st.Claims[did][index]
			claim.ReviewDecision = review.Decision
			claim.ReviewReason = strings.TrimSpace(review.Reason)
			claim.Reviewer = actor
			claim.ReviewedAt = &now
			claim.ReviewValid = true
			st.Claims[did][index] = claim
			if review.Decision != "pass" {
				allPass = false
			}
		}
		if allPass {
			dossier.Status = label.StatusCopyReview
			dossier.RequiresRevision = false
			ledger.AddAudit(st, did, "expert.passed", actor, Encode(reviews))
		} else {
			dossier.Status = label.StatusDraft
			dossier.RequiresRevision = true
			ledger.AddAudit(st, did, "expert.returned", actor, Encode(reviews))
		}
		dossier.Version++
		dossier.UpdatedAt = now
		st.Dossiers[did] = dossier
		result = ExpertReviewResult{Dossier: dossier, Claims: filterClaims(st.Claims[did], dossier.CurrentRevision)}
		return ledger.PutOperation(st, "expert-review", did, key, fp, result)
	})
	return result, err
}

func (s *Service) CopyReviewDetailed(did string, expected int, input CopyReviewInput) (CopyReviewResult, error) {
	var result CopyReviewResult
	err := s.Store.Update(func(st *ledger.State) error {
		dossier, ok := st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if dossier.Version != expected {
			return ledger.ErrConflict
		}
		if dossier.Status != label.StatusCopyReview {
			return ErrState
		}
		if input.Decision != "pass" && input.Decision != "return" && input.Decision != "reject" {
			return ErrValidation
		}
		revision := st.Revisions[did][dossier.CurrentRevision-1]
		validClaims := map[string]bool{}
		for _, claim := range filterClaims(st.Claims[did], dossier.CurrentRevision) {
			validClaims[claim.ID] = true
		}
		problems := label.ValidateCopySuggestions(revision.Content, input.Suggestions, validClaims)
		if len(problems) > 0 {
			return ErrValidation
		}
		ruleProblems := label.CopyProblems(revision.Content)
		if input.Decision == "pass" {
			for _, suggestion := range st.Suggestions[did] {
				if suggestionPending(suggestion) {
					return ErrState
				}
			}
			for _, suggestion := range input.Suggestions {
				if !suggestion.Resolved {
					return ErrState
				}
			}
			if len(ruleProblems) > 0 {
				return ErrState
			}
			dossier.Status = label.StatusFrozen
			dossier.RequiresRevision = false
			ledger.AddAudit(st, did, "copy.passed", input.Actor, "文字复核通过")
		} else {
			if strings.TrimSpace(input.Reason) == "" || len(input.Suggestions) == 0 {
				return ErrValidation
			}
			dossier.Status = label.StatusDraft
			dossier.RequiresRevision = true
			ledger.AddAudit(st, did, "copy.returned", input.Actor, Encode(map[string]any{"reason": input.Reason, "suggestions": input.Suggestions}))
		}
		now := time.Now()
		for i := range input.Suggestions {
			input.Suggestions[i].ID = id("sug_")
			input.Suggestions[i].DossierID = did
			input.Suggestions[i].RevisionNo = dossier.CurrentRevision
			input.Suggestions[i].AffectedClaimIDs = dedupeIDs(input.Suggestions[i].AffectedClaimIDs)
			input.Suggestions[i].CreatedBy = input.Actor
			input.Suggestions[i].CreatedAt = now
			if input.Suggestions[i].Resolved {
				input.Suggestions[i].Status = label.SuggestionApplied
			} else {
				input.Suggestions[i].Status = label.SuggestionPending
			}
		}
		st.Suggestions[did] = append(st.Suggestions[did], input.Suggestions...)
		dossier.Version++
		dossier.UpdatedAt = now
		st.Dossiers[did] = dossier
		if ruleProblems == nil {
			ruleProblems = []label.Problem{}
		}
		result = CopyReviewResult{Dossier: dossier, Problems: ruleProblems, Suggestions: input.Suggestions}
		return nil
	})
	return result, err
}

func (s *Service) ReviseDetailed(did string, expected int, content string, affected []string, actor string) (label.Dossier, label.RevisionDiff, error) {
	var dossier label.Dossier
	var diff label.RevisionDiff
	content = label.NormalizeContent(content)
	affected = dedupeIDs(affected)
	err := s.Store.Update(func(st *ledger.State) error {
		var ok bool
		dossier, ok = st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if dossier.Version != expected {
			return ledger.ErrConflict
		}
		if dossier.Status != label.StatusDraft {
			return ErrState
		}
		if content == "" || len([]rune(content)) > label.MaxContentRunes {
			return ErrValidation
		}
		oldRevision := dossier.CurrentRevision
		oldClaims := filterClaims(st.Claims[did], oldRevision)
		known := map[string]bool{}
		for _, claim := range oldClaims {
			known[claim.ID] = true
		}
		for _, suggestion := range st.Suggestions[did] {
			if suggestion.RevisionNo == oldRevision && suggestionPending(suggestion) {
				affected = dedupeIDs(append(affected, suggestion.AffectedClaimIDs...))
			}
		}
		for _, claimID := range affected {
			if !known[claimID] {
				return ledger.ErrNotFound
			}
		}
		baseRevision := st.Revisions[did][oldRevision-1]
		newRevision, revisionErr := NewRevision(dossier, content, actor)
		if revisionErr != nil {
			return revisionErr
		}
		if FactTextChanged(baseRevision.Content, newRevision.Content) {
			affected = dedupeIDs(append(affected, ClaimsAffectedByContent(baseRevision.Content, newRevision.Content, oldClaims)...))
		}
		affectedSet := map[string]bool{}
		for _, claimID := range affected {
			affectedSet[claimID] = true
		}
		now := newRevision.CreatedAt
		dossier.CurrentRevision++
		newRevision.Claims = make([]string, 0, len(oldClaims))
		for _, claim := range oldClaims {
			newRevision.Claims = append(newRevision.Claims, claim.ID)
		}
		st.Revisions[did] = append(st.Revisions[did], newRevision)
		for _, old := range oldClaims {
			inherited := old
			inherited.RevisionNo = dossier.CurrentRevision
			inherited.InheritedFrom = oldRevision
			if affectedSet[old.ID] {
				inherited.ReviewDecision, inherited.ReviewReason, inherited.Reviewer = "", "", ""
				inherited.ReviewedAt = nil
				inherited.ReviewValid = false
			}
			st.Claims[did] = append(st.Claims[did], inherited)
		}
		dossier.Version++
		dossier.RequiresRevision = false
		dossier.UpdatedAt = now
		st.Dossiers[did] = dossier
		diff = buildDiff(did, baseRevision, newRevision, oldClaims, filterClaims(st.Claims[did], dossier.CurrentRevision), affectedSet)
		ledger.AddAudit(st, did, "revision.created", actor, Encode(diff))
		return nil
	})
	return dossier, diff, err
}

func buildDiff(did string, from, to label.Revision, oldClaims, newClaims []label.Claim, affected map[string]bool) label.RevisionDiff {
	diff := label.RevisionDiff{DossierID: did, FromRevision: from.Number, ToRevision: to.Number, AddedLines: []string{}, RemovedLines: []string{}, AddedClaims: []string{}, RemovedClaims: []string{}, ModifiedClaims: []string{}}
	diff.RemovedLines, diff.AddedLines, _, _ = lineChanges(from.Content, to.Content)
	oldIDs, newIDs := map[string]bool{}, map[string]bool{}
	for _, claim := range oldClaims {
		oldIDs[claim.ID] = true
	}
	for _, claim := range newClaims {
		newIDs[claim.ID] = true
	}
	for claimID := range oldIDs {
		if !newIDs[claimID] {
			diff.RemovedClaims = append(diff.RemovedClaims, claimID)
		}
	}
	for claimID := range newIDs {
		if !oldIDs[claimID] {
			diff.AddedClaims = append(diff.AddedClaims, claimID)
		}
	}
	for claimID := range affected {
		diff.ModifiedClaims = append(diff.ModifiedClaims, claimID)
	}
	sort.Strings(diff.AddedClaims)
	sort.Strings(diff.RemovedClaims)
	sort.Strings(diff.ModifiedClaims)
	return diff
}

func (s *Service) RevisionDiff(did string, fromNo, toNo int) (label.RevisionDiff, error) {
	var out label.RevisionDiff
	err := s.Store.View(func(st ledger.State) error {
		if _, ok := st.Dossiers[did]; !ok {
			return ledger.ErrNotFound
		}
		if fromNo < 1 || toNo < 1 || fromNo > len(st.Revisions[did]) || toNo > len(st.Revisions[did]) {
			return ledger.ErrNotFound
		}
		from, to := st.Revisions[did][fromNo-1], st.Revisions[did][toNo-1]
		oldClaims, newClaims := filterClaims(st.Claims[did], fromNo), filterClaims(st.Claims[did], toNo)
		affected := map[string]bool{}
		for _, claim := range newClaims {
			if claim.InheritedFrom == fromNo && !claim.ReviewValid {
				affected[claim.ID] = true
			}
		}
		out = buildDiff(did, from, to, oldClaims, newClaims, affected)
		return nil
	})
	return out, err
}

func (s *Service) IssueWithKey(did string, expected int, snapshotID, actor, key string) (label.Credential, bool, error) {
	var out label.Credential
	var replay bool
	fp := fingerprint(struct {
		Expected          int
		SnapshotID, Actor string
	}{expected, snapshotID, actor})
	var integrityErr error
	err := s.Store.Update(func(st *ledger.State) error {
		if record, ok := ledger.GetOperation(*st, "issue", did, key); ok {
			if record.Fingerprint != fp {
				return ErrIdempotency
			}
			replay = true
			return json.Unmarshal(record.Result, &out)
		}
		dossier, ok := st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if dossier.Version != expected {
			return ledger.ErrConflict
		}
		if dossier.Status != label.StatusFrozen {
			return ErrState
		}
		snapshot, ok := st.Snapshots[snapshotID]
		if !ok || snapshot.DossierID != did {
			return ledger.ErrNotFound
		}
		currentRevision := st.Revisions[did][dossier.CurrentRevision-1]
		currentClaims := label.SortClaims(filterClaims(st.Claims[did], dossier.CurrentRevision))
		currentEvidence, evidenceErr := evidenceForClaims(*st, did, dossier.CurrentRevision, currentClaims)
		if evidenceErr != nil {
			ledger.AddAudit(st, did, "snapshot.integrity_failed", actor, Encode(map[string]any{"snapshotId": snapshotID, "reason": "当前证据清单不完整"}))
			integrityErr = ErrIntegrity
			return nil
		}
		currentDigest := label.Digest(currentRevision.Content, currentClaims, currentEvidence)
		if snapshot.RevisionNo != dossier.CurrentRevision || snapshot.Content != currentRevision.Content || !sameClaimSnapshot(currentClaims, snapshot.ClaimSnapshot) || !sameEvidenceManifest(currentEvidence, snapshot.EvidenceManifest) || label.Digest(snapshot.Content, snapshot.ClaimSnapshot, snapshot.EvidenceManifest) != snapshot.ContentDigest || currentDigest != snapshot.ContentDigest {
			ledger.AddAudit(st, did, "snapshot.integrity_failed", actor, Encode(map[string]any{"snapshotId": snapshotID, "reason": "摘要或修订不匹配"}))
			integrityErr = ErrIntegrity
			return nil
		}
		for _, evidence := range snapshot.EvidenceManifest {
			if evidence.DossierID != did || label.Digest(evidence.Excerpt, nil, nil) != evidence.Checksum {
				ledger.AddAudit(st, did, "snapshot.integrity_failed", actor, Encode(map[string]any{"snapshotId": snapshotID, "evidenceId": evidence.ID}))
				integrityErr = ErrIntegrity
				return nil
			}
		}
		out = label.Credential{CredentialNo: id("PUB-"), DossierID: did, SnapshotID: snapshotID, RevisionNo: snapshot.RevisionNo, ContentDigest: snapshot.ContentDigest, IssuedBy: actor, IssuedAt: time.Now(), SchemaVersion: "1"}
		out.Signature = label.Signature(out.CredentialNo, out.ContentDigest, actor)
		st.Credentials[out.CredentialNo] = out
		dossier.Status = label.StatusPublished
		dossier.Version++
		dossier.UpdatedAt = time.Now()
		st.Dossiers[did] = dossier
		ledger.AddAudit(st, did, "credential.issued", actor, out.CredentialNo)
		return ledger.PutOperation(st, "issue", did, key, fp, out)
	})
	if err == nil && integrityErr != nil {
		err = integrityErr
	}
	return out, replay, err
}

func (s *Service) GetCredentialView(no string) (CredentialView, error) {
	var out CredentialView
	err := s.Store.View(func(st ledger.State) error {
		credential, ok := st.Credentials[no]
		if !ok {
			return ledger.ErrNotFound
		}
		dossier, ok := st.Dossiers[credential.DossierID]
		if !ok {
			return ledger.ErrNotFound
		}
		snapshot, ok := st.Snapshots[credential.SnapshotID]
		if !ok {
			return ledger.ErrNotFound
		}
		out = CredentialView{Credential: credential, Dossier: dossier, Snapshot: cloneSnapshot(snapshot), Audits: SortAudit(st.Audits[credential.DossierID])}
		if len(out.Audits) > label.MaxListItems {
			out.Audits = out.Audits[len(out.Audits)-label.MaxListItems:]
		}
		if out.Audits == nil {
			out.Audits = []label.AuditEvent{}
		}
		return nil
	})
	return out, err
}

func sameEvidenceManifest(current, frozen []label.Evidence) bool {
	current = label.SortEvidence(current)
	frozen = label.SortEvidence(frozen)
	if len(current) != len(frozen) {
		return false
	}
	for i := range current {
		if current[i].ID != frozen[i].ID || current[i].DossierID != frozen[i].DossierID || current[i].SourceType != frozen[i].SourceType || current[i].Citation != frozen[i].Citation || current[i].Locator != frozen[i].Locator || current[i].Excerpt != frozen[i].Excerpt || current[i].ReliabilityNote != frozen[i].ReliabilityNote || current[i].Checksum != frozen[i].Checksum || current[i].CreatedRevision != frozen[i].CreatedRevision || current[i].Status != frozen[i].Status || current[i].ReplacementEvidenceID != frozen[i].ReplacementEvidenceID || current[i].SupersedeReason != frozen[i].SupersedeReason || current[i].SupersededBy != frozen[i].SupersededBy || current[i].EffectiveRevision != frozen[i].EffectiveRevision || !sameOptionalTime(current[i].SupersededAt, frozen[i].SupersededAt) || !current[i].CreatedAt.Equal(frozen[i].CreatedAt) {
			return false
		}
	}
	return true
}

func sameClaimSnapshot(current, frozen []label.Claim) bool {
	current = label.SortClaims(current)
	frozen = label.SortClaims(frozen)
	if len(current) != len(frozen) {
		return false
	}
	for i := range current {
		a, b := current[i], frozen[i]
		if a.ID != b.ID || a.DossierID != b.DossierID || a.RevisionNo != b.RevisionNo || a.Statement != b.Statement || a.Category != b.Category || !equalStrings(a.EvidenceIDs, b.EvidenceIDs) || a.ReviewDecision != b.ReviewDecision || a.ReviewReason != b.ReviewReason || a.Reviewer != b.Reviewer || a.ReviewValid != b.ReviewValid || a.InheritedFrom != b.InheritedFrom || !sameOptionalTime(a.ReviewedAt, b.ReviewedAt) {
			return false
		}
	}
	return true
}

func sameOptionalTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func missingClaimsMessage(claims []label.Claim, reviews []ReviewInput) string {
	seen := map[string]bool{}
	for _, review := range reviews {
		seen[review.ClaimID] = true
	}
	var missing []string
	for _, claim := range claims {
		if !seen[claim.ID] {
			missing = append(missing, claim.ID)
		}
	}
	return fmt.Sprintf("缺少主张结论: %s", strings.Join(missing, ","))
}
