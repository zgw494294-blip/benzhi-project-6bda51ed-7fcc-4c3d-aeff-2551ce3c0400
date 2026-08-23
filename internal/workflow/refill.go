package workflow

import (
	"museum-label-governance/internal/label"
	"museum-label-governance/internal/ledger"
	"sort"
	"strings"
	"time"
)

func (s *Service) UpdateDossier(did string, expected int, patch DossierPatch, actor string) (label.Dossier, error) {
	var out label.Dossier
	err := s.Store.Update(func(st *ledger.State) error {
		dossier, ok := st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if dossier.Version != expected {
			return ledger.ErrConflict
		}
		if dossier.Status != label.StatusDraft {
			return ErrState
		}
		before := dossier
		if patch.ExhibitionName != nil {
			dossier.ExhibitionName = strings.TrimSpace(*patch.ExhibitionName)
		}
		if patch.ObjectRef != nil {
			dossier.ObjectRef = strings.TrimSpace(*patch.ObjectRef)
		}
		if patch.Title != nil {
			dossier.Title = strings.TrimSpace(*patch.Title)
		}
		if patch.Owner != nil {
			dossier.Owner = strings.TrimSpace(*patch.Owner)
		}
		revision := st.Revisions[did][dossier.CurrentRevision-1]
		if ValidateCreate(dossier.ExhibitionName, dossier.ObjectRef, dossier.Title, revision.Content, dossier.Owner) != nil {
			return ErrValidation
		}
		changes := map[string]map[string]string{}
		for field, values := range map[string][2]string{
			"exhibitionName": {before.ExhibitionName, dossier.ExhibitionName},
			"objectRef":      {before.ObjectRef, dossier.ObjectRef},
			"title":          {before.Title, dossier.Title},
		} {
			if values[0] != values[1] {
				changes[field] = map[string]string{"old": values[0], "new": values[1]}
			}
		}
		ownerChanged := before.Owner != dossier.Owner
		if len(changes) == 0 && !ownerChanged {
			out = before
			return nil
		}
		now := time.Now()
		dossier.Version++
		dossier.UpdatedAt = now
		st.Dossiers[did] = dossier
		if len(changes) > 0 {
			ledger.AddAudit(st, did, "dossier.corrected", actor, Encode(map[string]any{"changes": changes}))
		}
		if ownerChanged {
			ledger.AddAudit(st, did, "dossier.owner_transferred", actor, Encode(map[string]any{"fromOwner": before.Owner, "toOwner": dossier.Owner, "operator": actor, "transferredAt": now}))
		}
		out = dossier
		return nil
	})
	return out, err
}

func findEvidenceIndex(items []label.Evidence, evidenceID string) int {
	for i := range items {
		if items[i].ID == evidenceID {
			return i
		}
	}
	return -1
}

func (s *Service) SupersedeEvidence(did, evidenceID, replacementID, reason string, expected int, actor string) (label.Dossier, label.Evidence, error) {
	var dossier label.Dossier
	var superseded label.Evidence
	reason, replacementID = strings.TrimSpace(reason), strings.TrimSpace(replacementID)
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
		if reason == "" || replacementID == "" || replacementID == evidenceID {
			return ErrValidation
		}
		oldIndex := findEvidenceIndex(st.Evidence[did], evidenceID)
		newIndex := findEvidenceIndex(st.Evidence[did], replacementID)
		if oldIndex < 0 || newIndex < 0 {
			return ledger.ErrNotFound
		}
		oldEvidence := st.Evidence[did][oldIndex]
		replacement := st.Evidence[did][newIndex]
		if !label.EvidenceIsActive(oldEvidence) || !label.EvidenceIsActive(replacement) || replacement.CreatedRevision > dossier.CurrentRevision {
			return ErrState
		}
		seen := map[string]bool{evidenceID: true}
		for current := replacement; current.ReplacementEvidenceID != ""; {
			if seen[current.ID] || seen[current.ReplacementEvidenceID] {
				return ErrState
			}
			seen[current.ID] = true
			next := findEvidenceIndex(st.Evidence[did], current.ReplacementEvidenceID)
			if next < 0 {
				return ErrState
			}
			current = st.Evidence[did][next]
		}
		now := time.Now()
		oldEvidence.Status = label.EvidenceSuperseded
		oldEvidence.ReplacementEvidenceID = replacementID
		oldEvidence.SupersedeReason = reason
		oldEvidence.SupersededBy = actor
		oldEvidence.SupersededAt = &now
		oldEvidence.EffectiveRevision = dossier.CurrentRevision
		st.Evidence[did][oldIndex] = oldEvidence
		migratedClaims := []string{}
		for i := range st.Claims[did] {
			claim := st.Claims[did][i]
			if claim.RevisionNo != dossier.CurrentRevision {
				continue
			}
			migrated := false
			rewritten := make([]string, 0, len(claim.EvidenceIDs))
			for _, currentID := range claim.EvidenceIDs {
				if currentID == evidenceID {
					currentID, migrated = replacementID, true
				}
				rewritten = append(rewritten, currentID)
			}
			if migrated {
				claim.EvidenceIDs = dedupeIDs(rewritten)
				st.Claims[did][i] = claim
				migratedClaims = append(migratedClaims, claim.ID)
			}
		}
		sort.Strings(migratedClaims)
		dossier.Version++
		dossier.UpdatedAt = now
		st.Dossiers[did] = dossier
		ledger.AddAudit(st, did, "evidence.superseded", actor, Encode(map[string]any{"evidenceId": evidenceID, "replacementEvidenceId": replacementID, "reason": reason, "revisionNo": dossier.CurrentRevision, "migratedClaimIds": migratedClaims}))
		superseded = oldEvidence
		return nil
	})
	return dossier, superseded, err
}

func (s *Service) EvidenceUsage(did, evidenceID string) (EvidenceUsage, error) {
	var result EvidenceUsage
	err := s.Store.View(func(st ledger.State) error {
		if _, ok := st.Dossiers[did]; !ok {
			return ledger.ErrNotFound
		}
		index := findEvidenceIndex(st.Evidence[did], evidenceID)
		if index < 0 {
			return ledger.ErrNotFound
		}
		evidence := st.Evidence[did][index]
		valid := label.EvidenceIsUsable(evidence) && label.Digest(evidence.Excerpt, nil, nil) == evidence.Checksum
		result = EvidenceUsage{Evidence: evidence, Usage: []EvidenceUsageRevision{}}
		for revisionNo := 1; revisionNo <= len(st.Revisions[did]); revisionNo++ {
			claimIDs := []string{}
			for _, claim := range st.Claims[did] {
				if claim.RevisionNo == revisionNo {
					for _, id := range claim.EvidenceIDs {
						if id == evidenceID {
							claimIDs = append(claimIDs, claim.ID)
							break
						}
					}
				}
			}
			if len(claimIDs) > 0 {
				sort.Strings(claimIDs)
				result.Usage = append(result.Usage, EvidenceUsageRevision{RevisionNo: revisionNo, ClaimIDs: claimIDs, Valid: valid})
			}
		}
		return nil
	})
	return result, err
}

func (s *Service) PrecheckHistory(did string, revisionNo, limit int, actor string) (PrecheckHistory, error) {
	result := PrecheckHistory{Items: []label.PrecheckSnapshot{}}
	if limit < 1 || limit > label.MaxPrecheckHistoryItems || revisionNo < 0 {
		return result, ErrValidation
	}
	err := s.Store.Update(func(st *ledger.State) error {
		if _, ok := st.Dossiers[did]; !ok {
			return ledger.ErrNotFound
		}
		for _, snapshot := range st.Prechecks[did] {
			if revisionNo == 0 || snapshot.RevisionNo == revisionNo {
				result.Items = append(result.Items, snapshot)
			}
		}
		if len(result.Items) > limit {
			result.Items = result.Items[len(result.Items)-limit:]
		}
		ledger.AddAudit(st, did, "precheck.history_queried", actor, Encode(map[string]any{"revisionNo": revisionNo, "limit": limit}))
		return nil
	})
	return result, err
}

func (s *Service) DiffPrechecks(did string, fromVersion, toVersion int, actor string) (PrecheckDiff, error) {
	var result PrecheckDiff
	if fromVersion < 1 || toVersion < 1 || fromVersion >= toVersion {
		return result, ErrValidation
	}
	err := s.Store.Update(func(st *ledger.State) error {
		if _, ok := st.Dossiers[did]; !ok {
			return ledger.ErrNotFound
		}
		fromIndex, toIndex := -1, -1
		for i, snapshot := range st.Prechecks[did] {
			if snapshot.Version == fromVersion {
				fromIndex = i
			}
			if snapshot.Version == toVersion {
				toIndex = i
			}
		}
		if fromIndex < 0 || toIndex < 0 {
			return ledger.ErrNotFound
		}
		if fromIndex >= toIndex || toIndex-fromIndex > label.MaxPrecheckHistoryItems {
			return ErrValidation
		}
		from, to := st.Prechecks[did][fromIndex], st.Prechecks[did][toIndex]
		resolved, introduced, remaining := label.DiffProblems(from.Problems, to.Problems)
		result = PrecheckDiff{DossierID: did, From: from, To: to, Resolved: resolved, Introduced: introduced, Remaining: remaining}
		ledger.AddAudit(st, did, "precheck.diff_queried", actor, Encode(map[string]any{"fromVersion": fromVersion, "toVersion": toVersion}))
		return nil
	})
	return result, err
}

func (s *Service) SaveExpertReviewDraft(did, claimID string, expected int, decision, reason, reviewer string) (ExpertReviewProgress, error) {
	var result ExpertReviewProgress
	reason = strings.TrimSpace(reason)
	err := s.Store.Update(func(st *ledger.State) error {
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
		if !ValidDecision(decision) || reason == "" || strings.TrimSpace(reviewer) == "" {
			return ErrValidation
		}
		known := false
		for _, claim := range st.Claims[did] {
			if claim.RevisionNo == dossier.CurrentRevision && claim.ID == claimID {
				known = true
				break
			}
		}
		if !known {
			return ledger.ErrNotFound
		}
		now, draftIndex := time.Now(), -1
		for i, draft := range st.ExpertDrafts[did] {
			if draft.RevisionNo == dossier.CurrentRevision && draft.ClaimID == claimID {
				draftIndex = i
				if draft.Reviewer != reviewer {
					return ErrState
				}
				break
			}
		}
		draft := label.ExpertReviewDraft{DossierID: did, RevisionNo: dossier.CurrentRevision, ClaimID: claimID, Decision: decision, Reason: reason, Reviewer: reviewer, UpdatedAt: now}
		if draftIndex >= 0 {
			st.ExpertDrafts[did][draftIndex] = draft
		} else {
			st.ExpertDrafts[did] = append(st.ExpertDrafts[did], draft)
		}
		dossier.Status = label.StatusExpertReview
		dossier.Version++
		dossier.UpdatedAt = now
		st.Dossiers[did] = dossier
		ledger.AddAudit(st, did, "expert.draft_saved", reviewer, Encode(map[string]any{"claimId": claimID, "revisionNo": dossier.CurrentRevision, "decision": decision}))
		result = expertProgress(*st, dossier)
		return nil
	})
	return result, err
}

func expertProgress(st ledger.State, dossier label.Dossier) ExpertReviewProgress {
	claims := label.SortClaims(filterClaims(st.Claims[dossier.ID], dossier.CurrentRevision))
	drafts := []label.ExpertReviewDraft{}
	byClaim := map[string]label.ExpertReviewDraft{}
	for _, draft := range st.ExpertDrafts[dossier.ID] {
		if draft.RevisionNo == dossier.CurrentRevision {
			byClaim[draft.ClaimID] = draft
			drafts = append(drafts, draft)
		}
	}
	sort.Slice(drafts, func(i, j int) bool { return drafts[i].ClaimID < drafts[j].ClaimID })
	result := ExpertReviewProgress{Dossier: dossier, RevisionNo: dossier.CurrentRevision, TotalCount: len(claims), CompletedCount: len(drafts), MissingClaimIDs: []string{}, Drafts: drafts}
	for _, claim := range claims {
		draft, ok := byClaim[claim.ID]
		if !ok {
			result.MissingClaimIDs = append(result.MissingClaimIDs, claim.ID)
			continue
		}
		switch draft.Decision {
		case "pass":
			result.DecisionCounts.Pass++
		case "doubt":
			result.DecisionCounts.Doubt++
		case "reject":
			result.DecisionCounts.Reject++
		}
	}
	return result
}

func (s *Service) ExpertReviewProgress(did string) (ExpertReviewProgress, error) {
	var result ExpertReviewProgress
	err := s.Store.View(func(st ledger.State) error {
		dossier, ok := st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		result = expertProgress(st, dossier)
		return nil
	})
	return result, err
}

func (s *Service) FinalizeExpertReview(did string, expected int, reviewer string) (ExpertReviewResult, error) {
	var result ExpertReviewResult
	err := s.Store.Update(func(st *ledger.State) error {
		dossier, ok := st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if dossier.Version != expected {
			return ledger.ErrConflict
		}
		if dossier.Status != label.StatusExpertReview {
			return ErrState
		}
		progress := expertProgress(*st, dossier)
		if progress.TotalCount == 0 || progress.CompletedCount != progress.TotalCount || len(progress.MissingClaimIDs) > 0 {
			return ErrState
		}
		drafts := map[string]label.ExpertReviewDraft{}
		for _, draft := range progress.Drafts {
			drafts[draft.ClaimID] = draft
		}
		now, allPass := time.Now(), true
		for i := range st.Claims[did] {
			claim := st.Claims[did][i]
			if claim.RevisionNo != dossier.CurrentRevision {
				continue
			}
			draft, ok := drafts[claim.ID]
			if !ok {
				return ErrState
			}
			claim.ReviewDecision, claim.ReviewReason, claim.Reviewer = draft.Decision, draft.Reason, draft.Reviewer
			claim.ReviewedAt, claim.ReviewValid = &now, true
			st.Claims[did][i] = claim
			if draft.Decision != "pass" {
				allPass = false
			}
		}
		if allPass {
			dossier.Status, dossier.RequiresRevision = label.StatusCopyReview, false
			ledger.AddAudit(st, did, "expert.passed", reviewer, Encode(progress.Drafts))
		} else {
			dossier.Status, dossier.RequiresRevision = label.StatusDraft, true
			ledger.AddAudit(st, did, "expert.returned", reviewer, Encode(progress.Drafts))
		}
		dossier.Version++
		dossier.UpdatedAt = now
		st.Dossiers[did] = dossier
		result = ExpertReviewResult{Dossier: dossier, Claims: filterClaims(st.Claims[did], dossier.CurrentRevision)}
		return nil
	})
	return result, err
}

func suggestionPending(suggestion label.CopySuggestion) bool {
	return suggestion.Status == label.SuggestionPending || (suggestion.Status == "" && !suggestion.Resolved)
}

func (s *Service) DisposeCopySuggestion(did, suggestionID string, expected int, disposition, note, actor string) (label.Dossier, label.CopySuggestion, error) {
	var dossier label.Dossier
	var result label.CopySuggestion
	disposition, note = strings.TrimSpace(disposition), strings.TrimSpace(note)
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
		index := -1
		for i, suggestion := range st.Suggestions[did] {
			if suggestion.ID == suggestionID {
				index = i
				result = suggestion
				break
			}
		}
		if index < 0 {
			return ledger.ErrNotFound
		}
		if !suggestionPending(result) {
			return ErrState
		}
		if disposition != label.SuggestionApplied && disposition != label.SuggestionDismissed {
			return ErrValidation
		}
		if dossier.CurrentRevision <= result.RevisionNo {
			return ErrState
		}
		if disposition == label.SuggestionDismissed && note == "" {
			return ErrValidation
		}
		if disposition == label.SuggestionApplied {
			for _, claimID := range result.AffectedClaimIDs {
				found, invalid := false, false
				for _, claim := range st.Claims[did] {
					if claim.RevisionNo == dossier.CurrentRevision && claim.ID == claimID {
						found, invalid = true, !claim.ReviewValid
						break
					}
				}
				if !found {
					return ledger.ErrNotFound
				}
				if !invalid {
					return ErrState
				}
			}
		}
		now := time.Now()
		result.Status, result.Resolved = disposition, true
		result.DispositionNote, result.HandledBy, result.HandledAt, result.HandledRevision = note, actor, &now, dossier.CurrentRevision
		st.Suggestions[did][index] = result
		dossier.Version++
		dossier.UpdatedAt = now
		st.Dossiers[did] = dossier
		ledger.AddAudit(st, did, "copy_suggestion.disposed", actor, Encode(map[string]any{"suggestionId": suggestionID, "disposition": disposition, "revisionNo": dossier.CurrentRevision, "note": note}))
		return nil
	})
	return dossier, result, err
}

func (s *Service) CopySuggestions(did, status string) (CopySuggestionList, error) {
	result := CopySuggestionList{Items: []label.CopySuggestion{}}
	if status != "" && status != label.SuggestionPending && status != label.SuggestionApplied && status != label.SuggestionDismissed {
		return result, ErrValidation
	}
	err := s.Store.View(func(st ledger.State) error {
		if _, ok := st.Dossiers[did]; !ok {
			return ledger.ErrNotFound
		}
		for _, suggestion := range st.Suggestions[did] {
			actual := suggestion.Status
			if actual == "" {
				if suggestion.Resolved {
					actual = label.SuggestionApplied
				} else {
					actual = label.SuggestionPending
				}
				suggestion.Status = actual
			}
			if actual == label.SuggestionPending {
				result.PendingCount++
			}
			if status == "" || actual == status {
				result.Items = append(result.Items, suggestion)
			}
		}
		sort.Slice(result.Items, func(i, j int) bool {
			if result.Items[i].CreatedAt.Equal(result.Items[j].CreatedAt) {
				return result.Items[i].ID < result.Items[j].ID
			}
			return result.Items[i].CreatedAt.Before(result.Items[j].CreatedAt)
		})
		return nil
	})
	return result, err
}

func (s *Service) VerifyCredential(no, actor string) (CredentialVerification, error) {
	var result CredentialVerification
	err := s.Store.Update(func(st *ledger.State) error {
		credential, ok := st.Credentials[no]
		if !ok {
			return ledger.ErrNotFound
		}
		result = CredentialVerification{CredentialNo: credential.CredentialNo, DossierID: credential.DossierID, SnapshotID: credential.SnapshotID, RevisionNo: credential.RevisionNo, ContentDigest: credential.ContentDigest, IssuedBy: credential.IssuedBy, IssuedAt: credential.IssuedAt, ProblemCodes: []string{}, Checks: []VerificationCheck{}}
		addCheck := func(name string, valid bool, code string) {
			check := VerificationCheck{Name: name, Valid: valid}
			if !valid {
				check.ProblemCode = code
				result.ProblemCodes = append(result.ProblemCodes, code)
			}
			result.Checks = append(result.Checks, check)
		}
		result.SignatureValid = credential.Signature == label.Signature(credential.CredentialNo, credential.ContentDigest, credential.IssuedBy)
		addCheck("signature", result.SignatureValid, "signature_mismatch")
		snapshot, snapshotOK := st.Snapshots[credential.SnapshotID]
		addCheck("snapshot_exists", snapshotOK, "snapshot_missing")
		result.DigestValid = snapshotOK && label.Digest(snapshot.Content, snapshot.ClaimSnapshot, snapshot.EvidenceManifest) == snapshot.ContentDigest
		addCheck("snapshot_digest", result.DigestValid, "snapshot_digest_mismatch")
		_, dossierOK := st.Dossiers[credential.DossierID]
		addCheck("dossier_exists", dossierOK, "dossier_missing")
		addCheck("snapshot_id", snapshotOK && snapshot.ID == credential.SnapshotID, "snapshot_id_mismatch")
		addCheck("dossier_link", snapshotOK && dossierOK && snapshot.DossierID == credential.DossierID, "dossier_link_mismatch")
		addCheck("revision_link", snapshotOK && credential.RevisionNo == snapshot.RevisionNo, "revision_link_mismatch")
		addCheck("content_digest_link", snapshotOK && credential.ContentDigest == snapshot.ContentDigest, "content_digest_link_mismatch")
		result.Valid = true
		for _, check := range result.Checks {
			if !check.Valid {
				result.Valid = false
				break
			}
		}
		ledger.AddAudit(st, credential.DossierID, "credential.verified", actor, Encode(map[string]any{"credentialNo": no, "valid": result.Valid, "problemCodes": result.ProblemCodes}))
		return nil
	})
	return result, err
}
