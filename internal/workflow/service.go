package workflow

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"museum-label-governance/internal/label"
	"museum-label-governance/internal/ledger"
	"sort"
	"strings"
	"time"
)

var ErrValidation = errors.New("validation error")
var ErrState = errors.New("invalid state")
var ErrIdempotency = errors.New("idempotency conflict")
var ErrIntegrity = errors.New("snapshot integrity failure")
var ErrImmutable = errors.New("immutable resource")

type cachedOperation struct {
	result json.RawMessage
	hits   uint64
}

type Service struct {
	Store          *ledger.Store
	operationCache map[string]*cachedOperation
}

func New(s *ledger.Store) *Service {
	return &Service{Store: s, operationCache: map[string]*cachedOperation{}}
}

func (s *Service) cachedOperation(key string, result any) bool {
	if key == "" {
		return false
	}
	entry, ok := s.operationCache[key]
	if !ok {
		return false
	}
	entry.hits++
	return json.Unmarshal(entry.result, result) == nil
}

func (s *Service) rememberOperation(key string, result any) {
	if key == "" {
		return
	}
	raw, err := json.Marshal(result)
	if err == nil {
		s.operationCache[key] = &cachedOperation{result: raw}
	}
}
func id(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func (s *Service) CreateDossier(exhibition, object, title, content, owner string) (label.Dossier, error) {
	exhibition, object, title, owner = strings.TrimSpace(exhibition), strings.TrimSpace(object), strings.TrimSpace(title), strings.TrimSpace(owner)
	content = label.NormalizeContent(content)
	if err := ValidateCreate(exhibition, object, title, content, owner); err != nil {
		return label.Dossier{}, ErrValidation
	}
	now := time.Now()
	d := label.Dossier{ID: id("dos_"), ExhibitionName: exhibition, ObjectRef: object, Title: title, Owner: owner, Status: label.StatusDraft, CurrentRevision: 1, Version: 1, CreatedAt: now, UpdatedAt: now}
	r := label.Revision{DossierID: d.ID, Number: 1, Content: content, Status: label.StatusDraft, CreatedBy: owner, CreatedAt: now}
	e := s.Store.Update(func(st *ledger.State) error {
		st.Dossiers[d.ID] = d
		st.Revisions[d.ID] = []label.Revision{r}
		ledger.AddAudit(st, d.ID, "dossier.created", owner, "创建案卷")
		return nil
	})
	return d, e
}
func (s *Service) AddClaim(did string, expected int, statement, category, actor string) (label.Claim, error) {
	out, _, err := s.CreateClaimWithEvidence(did, expected, statement, category, nil, actor)
	return out, err
}
func (s *Service) AddEvidence(did string, expected int, source, citation, locator, excerpt, reliability, actor string) (label.Evidence, error) {
	var out label.Evidence
	e := s.Store.Update(func(st *ledger.State) error {
		d, ok := st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if d.Version != expected {
			return ledger.ErrConflict
		}
		if d.Status != label.StatusDraft {
			return ErrState
		}
		out = label.Evidence{ID: id("ev_"), DossierID: did, SourceType: strings.TrimSpace(source), Citation: strings.TrimSpace(citation), Locator: strings.TrimSpace(locator), Excerpt: label.NormalizeContent(excerpt), ReliabilityNote: strings.TrimSpace(reliability), CreatedAt: time.Now(), CreatedRevision: d.CurrentRevision, Status: label.EvidenceActive}
		if len(label.ValidateEvidence(out)) > 0 {
			return ErrValidation
		}
		out.Checksum = label.Digest(out.Excerpt, nil, nil)
		st.Evidence[did] = append(st.Evidence[did], out)
		d.Version++
		d.UpdatedAt = time.Now()
		st.Dossiers[did] = d
		ledger.AddAudit(st, did, "evidence.added", actor, out.ID)
		return nil
	})
	return out, e
}
func (s *Service) LinkEvidence(did, claimID string, expected int, ids []string, actor string) (label.Claim, error) {
	out, _, err := s.ReplaceEvidenceLinks(did, claimID, expected, ids, actor)
	return out, err
}
func (s *Service) RunPrecheck(did string, expected int, actor string) ([]label.Problem, label.Dossier, error) {
	result, err := s.RunPrecheckWithKey(did, expected, actor, "")
	return result.Problems, result.Dossier, err
}
func filterClaims(all []label.Claim, rev int) []label.Claim {
	var out []label.Claim
	for _, c := range all {
		if c.RevisionNo == rev {
			out = append(out, c)
		}
	}
	return out
}
func (s *Service) ReviewClaim(did, claimID string, expected int, decision, reason, actor string) (label.Dossier, error) {
	result, err := s.ReviewClaimsBatch(did, expected, []ReviewInput{{ClaimID: claimID, Decision: decision, Reason: reason, Actor: actor}}, actor, "")
	return result.Dossier, err
}
func (s *Service) CopyReview(did string, expected int, decision, reason, actor string) (label.Dossier, []label.Problem, error) {
	result, err := s.CopyReviewDetailed(did, expected, CopyReviewInput{Decision: decision, Reason: reason, Actor: actor})
	return result.Dossier, result.Problems, err
}
func (s *Service) Revise(did string, expected int, content, actor string) (label.Dossier, error) {
	dossier, _, err := s.ReviseDetailed(did, expected, content, nil, actor)
	return dossier, err
}
func (s *Service) Freeze(did string, expected int, actor string) (label.Snapshot, error) {
	out, _, err := s.FreezeDetailed(did, expected, actor)
	return out, err
}

func (s *Service) FreezeDetailed(did string, expected int, actor string) (label.Snapshot, bool, error) {
	var out label.Snapshot
	var replay bool
	e := s.Store.Update(func(st *ledger.State) error {
		d, ok := st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		if d.Status != label.StatusFrozen {
			return ErrState
		}
		for _, existing := range st.Snapshots {
			if existing.DossierID == did && existing.RevisionNo == d.CurrentRevision {
				out = cloneSnapshot(existing)
				replay = true
				return nil
			}
		}
		if d.Version != expected {
			return ledger.ErrConflict
		}
		r := st.Revisions[did][d.CurrentRevision-1]
		cs := label.SortClaims(filterClaims(st.Claims[did], d.CurrentRevision))
		if len(cs) == 0 {
			return ErrState
		}
		for _, c := range cs {
			if !label.ClaimPassed(c) || len(c.EvidenceIDs) == 0 {
				return ErrState
			}
		}
		ev, err := evidenceForClaims(*st, did, d.CurrentRevision, cs)
		if err != nil {
			return err
		}
		out = label.Snapshot{ID: id("snp_"), DossierID: did, RevisionNo: r.Number, Content: r.Content, ClaimSnapshot: cs, EvidenceManifest: ev, ContentDigest: label.Digest(r.Content, cs, ev), FrozenBy: actor, FrozenAt: time.Now()}
		st.Snapshots[out.ID] = cloneSnapshot(out)
		d.Version++
		d.UpdatedAt = time.Now()
		st.Dossiers[did] = d
		ledger.AddAudit(st, did, "snapshot.frozen", actor, out.ID)
		return nil
	})
	return cloneSnapshot(out), replay, e
}

func evidenceForClaims(st ledger.State, did string, revision int, claims []label.Claim) ([]label.Evidence, error) {
	available := make(map[string]label.Evidence, len(st.Evidence[did]))
	for _, evidence := range st.Evidence[did] {
		if evidence.DossierID == did && evidence.CreatedRevision <= revision && label.EvidenceIsUsable(evidence) && label.Digest(evidence.Excerpt, nil, nil) == evidence.Checksum {
			available[evidence.ID] = evidence
		}
	}
	seen := map[string]bool{}
	result := []label.Evidence{}
	for _, claim := range claims {
		for _, evidenceID := range claim.EvidenceIDs {
			evidence, ok := available[evidenceID]
			if !ok {
				return nil, ledger.ErrNotFound
			}
			if !seen[evidenceID] {
				result = append(result, evidence)
				seen[evidenceID] = true
			}
		}
	}
	return label.SortEvidence(result), nil
}

func cloneSnapshot(snapshot label.Snapshot) label.Snapshot {
	snapshot.ClaimSnapshot = append([]label.Claim(nil), snapshot.ClaimSnapshot...)
	for i := range snapshot.ClaimSnapshot {
		snapshot.ClaimSnapshot[i].EvidenceIDs = append([]string(nil), snapshot.ClaimSnapshot[i].EvidenceIDs...)
	}
	snapshot.EvidenceManifest = append([]label.Evidence(nil), snapshot.EvidenceManifest...)
	return snapshot
}
func (s *Service) Issue(did string, expected int, snapshotID, actor string) (label.Credential, error) {
	out, _, err := s.IssueWithKey(did, expected, snapshotID, actor, "")
	return out, err
}
func (s *Service) GetAudit(did string) (map[string]any, error) {
	var out = map[string]any{}
	e := s.Store.View(func(st ledger.State) error {
		d, ok := st.Dossiers[did]
		if !ok {
			return ledger.ErrNotFound
		}
		out["dossier"] = d
		out["revisions"] = st.Revisions[did]
		out["claims"] = st.Claims[did]
		out["evidence"] = st.Evidence[did]
		out["snapshots"] = snapshotsFor(st, did)
		out["credentials"] = credentialsFor(st, did)
		out["suggestions"] = st.Suggestions[did]
		out["prechecks"] = st.Prechecks[did]
		out["expertReviewDrafts"] = st.ExpertDrafts[did]
		out["audits"] = SortAudit(st.Audits[did])
		return nil
	})
	return out, e
}
func snapshotsFor(st ledger.State, did string) []label.Snapshot {
	var x []label.Snapshot
	for _, s := range st.Snapshots {
		if s.DossierID == did {
			x = append(x, s)
		}
	}
	sort.Slice(x, func(i, j int) bool { return x[i].RevisionNo < x[j].RevisionNo })
	return x
}
func credentialsFor(st ledger.State, did string) []label.Credential {
	var x []label.Credential
	for _, c := range st.Credentials {
		if c.DossierID == did {
			x = append(x, c)
		}
	}
	sort.Slice(x, func(i, j int) bool { return x[i].IssuedAt.Before(x[j].IssuedAt) })
	return x
}
func (s *Service) GetCredential(no string) (label.Credential, error) {
	var c label.Credential
	e := s.Store.View(func(st ledger.State) error {
		var ok bool
		c, ok = st.Credentials[no]
		if !ok {
			return ledger.ErrNotFound
		}
		return nil
	})
	return c, e
}
func Encode(v any) string { b, _ := json.Marshal(v); return string(b) }
