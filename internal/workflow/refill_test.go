package workflow

import (
	"errors"
	"museum-label-governance/internal/label"
	"museum-label-governance/internal/ledger"
	"testing"
	"time"
)

func newRefillService(t *testing.T) (*Service, *ledger.Store) {
	t.Helper()
	store, err := ledger.New(t.TempDir() + "/ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	return New(store), store
}

func TestDraftDossierCorrectionAndTransfer(t *testing.T) {
	svc, store := newRefillService(t)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "旧标题", "正文", "甲")
	title, owner := "  新标题  ", "  乙 "
	updated, err := svc.UpdateDossier(dossier.ID, dossier.Version, DossierPatch{Title: &title, Owner: &owner}, "交接人")
	if err != nil || updated.Title != "新标题" || updated.Owner != "乙" || updated.Version != dossier.Version+1 {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	noChange := " 新标题 "
	unchanged, err := svc.UpdateDossier(dossier.ID, updated.Version, DossierPatch{Title: &noChange}, "交接人")
	if err != nil || unchanged.Version != updated.Version {
		t.Fatalf("no-op=%#v err=%v", unchanged, err)
	}
	if _, err = svc.UpdateDossier(dossier.ID, dossier.Version, DossierPatch{Title: &title}, "交接人"); !errors.Is(err, ledger.ErrConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	actions := AuditActions(store.Audits(dossier.ID))
	if !containsString(actions, "dossier.corrected") || !containsString(actions, "dossier.owner_transferred") {
		t.Fatalf("audits=%#v", actions)
	}
}

func TestEvidenceSupersedeMigratesCurrentRevisionOnly(t *testing.T) {
	svc, store := newRefillService(t)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "标题", "事实一\n事实二", "负责人")
	oldEvidence, _ := svc.AddEvidence(dossier.ID, dossier.Version, "图录", "旧图录", "p.1", "旧证据", "可靠", "编写人")
	dossier.Version++
	replacement, _ := svc.AddEvidence(dossier.ID, dossier.Version, "图录", "新图录", "p.2", "新证据", "可靠", "编写人")
	dossier.Version++
	first, dossier, _ := svc.CreateClaimWithEvidence(dossier.ID, dossier.Version, "事实一", "其他", []string{oldEvidence.ID}, "编写人")
	second, dossier, _ := svc.CreateClaimWithEvidence(dossier.ID, dossier.Version, "事实二", "其他", []string{oldEvidence.ID}, "编写人")
	dossier, _, _ = svc.ReviseDetailed(dossier.ID, dossier.Version, "事实一\n事实二", nil, "编写人")
	updated, old, err := svc.SupersedeEvidence(dossier.ID, oldEvidence.ID, replacement.ID, "旧版页码错误", dossier.Version, "编写人")
	if err != nil || old.Status != label.EvidenceSuperseded || updated.Version != dossier.Version+1 {
		t.Fatalf("dossier=%#v evidence=%#v err=%v", updated, old, err)
	}
	oldClaims := store.Claims(dossier.ID, 1)
	newClaims := store.Claims(dossier.ID, 2)
	if oldClaims[0].EvidenceIDs[0] != oldEvidence.ID || oldClaims[1].EvidenceIDs[0] != oldEvidence.ID {
		t.Fatalf("historical claims changed: %#v", oldClaims)
	}
	for _, claim := range newClaims {
		if len(claim.EvidenceIDs) != 1 || claim.EvidenceIDs[0] != replacement.ID {
			t.Fatalf("claim not migrated: %#v", claim)
		}
	}
	usage, err := svc.EvidenceUsage(dossier.ID, oldEvidence.ID)
	if err != nil || len(usage.Usage) != 1 || usage.Usage[0].RevisionNo != 1 || usage.Usage[0].Valid {
		t.Fatalf("usage=%#v err=%v", usage, err)
	}
	if _, _, err = svc.SupersedeEvidence(dossier.ID, oldEvidence.ID, replacement.ID, "再次作废", updated.Version, "编写人"); !errors.Is(err, ErrState) {
		t.Fatalf("expected repeated supersede conflict, got %v", err)
	}
	current := store.Claims(dossier.ID, 2)
	if current[0].ID != first.ID || current[1].ID != second.ID {
		t.Fatalf("unexpected claim identities: %#v", current)
	}
}

func TestPrecheckDifferenceIsStableAndReadOnly(t *testing.T) {
	svc, store := newRefillService(t)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "标题", "正文", "负责人")
	now := time.Now()
	err := store.Update(func(st *ledger.State) error {
		st.Prechecks[dossier.ID] = []label.PrecheckSnapshot{
			{DossierID: dossier.ID, Version: 2, RevisionNo: 1, CreatedAt: now, Problems: []label.Problem{{Code: "orphan_claim", Target: "clm_2", Message: "缺证"}, {Code: "orphan_claim", Target: "clm_1", Message: "缺证"}, {Code: "conflicting_years", Target: "claims", Message: "年代冲突"}}},
			{DossierID: dossier.ID, Version: 5, RevisionNo: 2, CreatedAt: now.Add(time.Second), Problems: []label.Problem{{Code: "orphan_claim", Target: "clm_2", Message: "仍缺证"}, {Code: "invalid_evidence", Target: "ev_1", Message: "证据失效"}}},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	diff, err := svc.DiffPrechecks(dossier.ID, 2, 5, "查询人")
	if err != nil || len(diff.Resolved) != 2 || len(diff.Introduced) != 1 || len(diff.Remaining) != 1 || diff.Remaining[0].Message != "仍缺证" {
		t.Fatalf("diff=%#v err=%v", diff, err)
	}
	current, _ := store.GetDossier(dossier.ID)
	if current.Version != dossier.Version || current.Status != dossier.Status {
		t.Fatalf("diff query changed dossier: %#v", current)
	}
	if _, err = svc.DiffPrechecks(dossier.ID, 5, 2, "查询人"); !errors.Is(err, ErrValidation) {
		t.Fatalf("expected reverse range validation, got %v", err)
	}
}

func TestExpertReviewDraftProgressAndFinalize(t *testing.T) {
	svc, store := newRefillService(t)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "标题", "一\n二\n三", "负责人")
	claims := make([]label.Claim, 0, 3)
	for _, statement := range []string{"一", "二", "三"} {
		var claim label.Claim
		dossier, claim = addSupportedClaim(t, svc, dossier, statement)
		claims = append(claims, claim)
	}
	precheck, err := svc.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "draft-precheck")
	if err != nil {
		t.Fatal(err)
	}
	progress, err := svc.SaveExpertReviewDraft(dossier.ID, claims[0].ID, precheck.Dossier.Version, "pass", "来源充分", "研究员")
	if err != nil || progress.Dossier.Status != label.StatusExpertReview {
		t.Fatalf("progress=%#v err=%v", progress, err)
	}
	progress, _ = svc.SaveExpertReviewDraft(dossier.ID, claims[1].ID, progress.Dossier.Version, "pass", "来源充分", "研究员")
	if progress.CompletedCount != 2 || len(progress.MissingClaimIDs) != 1 {
		t.Fatalf("progress=%#v", progress)
	}
	beforeVersion := progress.Dossier.Version
	if _, err = svc.FinalizeExpertReview(dossier.ID, beforeVersion, "研究员"); !errors.Is(err, ErrState) {
		t.Fatalf("expected incomplete conflict, got %v", err)
	}
	current, _ := store.GetDossier(dossier.ID)
	if current.Version != beforeVersion || current.Status != label.StatusExpertReview {
		t.Fatalf("failed finalize changed dossier: %#v", current)
	}
	if _, err = svc.SaveExpertReviewDraft(dossier.ID, claims[0].ID, beforeVersion, "reject", "不同意见", "另一研究员"); !errors.Is(err, ErrState) {
		t.Fatalf("expected reviewer conflict, got %v", err)
	}
	progress, _ = svc.SaveExpertReviewDraft(dossier.ID, claims[2].ID, beforeVersion, "pass", "来源充分", "研究员")
	finalized, err := svc.FinalizeExpertReview(dossier.ID, progress.Dossier.Version, "研究员")
	if err != nil || finalized.Dossier.Status != label.StatusCopyReview {
		t.Fatalf("finalized=%#v err=%v", finalized, err)
	}
}

func TestCopySuggestionDispositionLifecycle(t *testing.T) {
	svc, _ := newRefillService(t)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "标题", "事实", "负责人")
	dossier, claim := addSupportedClaim(t, svc, dossier, "事实")
	precheck, _ := svc.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "suggestion-precheck")
	review, _ := svc.ReviewClaimsBatch(dossier.ID, precheck.Dossier.Version, []ReviewInput{{ClaimID: claim.ID, Decision: "pass", Reason: "充分"}}, "研究员", "suggestion-review")
	returned, err := svc.CopyReviewDetailed(dossier.ID, review.Dossier.Version, CopyReviewInput{Decision: "return", Reason: "调整文字", Actor: "文字编辑", Suggestions: []label.CopySuggestion{{Kind: "term", Start: 0, End: 1, Suggestion: "替换术语", AffectedClaimIDs: []string{claim.ID}}, {Kind: "sensitive", Start: 1, End: 2, Suggestion: "核对语气", AffectedClaimIDs: []string{claim.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	revised, _, err := svc.ReviseDetailed(dossier.ID, returned.Dossier.Version, "事实已调整", nil, "编写人")
	if err != nil {
		t.Fatal(err)
	}
	first, second := returned.Suggestions[0], returned.Suggestions[1]
	revised, applied, err := svc.DisposeCopySuggestion(dossier.ID, first.ID, revised.Version, label.SuggestionApplied, "已按建议修改", "编写人")
	if err != nil || applied.HandledRevision != 2 || applied.HandledBy != "编写人" {
		t.Fatalf("applied=%#v err=%v", applied, err)
	}
	revised, dismissed, err := svc.DisposeCopySuggestion(dossier.ID, second.ID, revised.Version, label.SuggestionDismissed, "与馆方术语表不符", "文字编辑")
	if err != nil || dismissed.Status != label.SuggestionDismissed {
		t.Fatalf("dismissed=%#v err=%v", dismissed, err)
	}
	list, _ := svc.CopySuggestions(dossier.ID, "")
	if list.PendingCount != 0 || len(list.Items) != 2 {
		t.Fatalf("list=%#v", list)
	}
	if _, _, err = svc.DisposeCopySuggestion(dossier.ID, first.ID, revised.Version, label.SuggestionDismissed, "改写", "文字编辑"); !errors.Is(err, ErrState) {
		t.Fatalf("expected immutable disposition, got %v", err)
	}
}

func TestCredentialVerificationReportsDamageWithoutRepair(t *testing.T) {
	svc, store := newRefillService(t)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "标题", "事实", "负责人")
	dossier, claim := addSupportedClaim(t, svc, dossier, "事实")
	precheck, _ := svc.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "verify-precheck")
	review, _ := svc.ReviewClaimsBatch(dossier.ID, precheck.Dossier.Version, []ReviewInput{{ClaimID: claim.ID, Decision: "pass", Reason: "充分"}}, "研究员", "verify-review")
	copyResult, _, _ := svc.CopyReview(dossier.ID, review.Dossier.Version, "pass", "", "文字编辑")
	snapshot, _ := svc.Freeze(dossier.ID, copyResult.Version, "负责人")
	credential, _ := svc.Issue(dossier.ID, copyResult.Version+1, snapshot.ID, "发布负责人")
	report, err := svc.VerifyCredential(credential.CredentialNo, "审计员")
	if err != nil || !report.Valid || !report.SignatureValid || !report.DigestValid {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	err = store.Update(func(st *ledger.State) error {
		changed := st.Snapshots[snapshot.ID]
		changed.Content = "被破坏的正文"
		st.Snapshots[snapshot.ID] = changed
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err = svc.VerifyCredential(credential.CredentialNo, "审计员")
	if err != nil || report.Valid || report.DigestValid || !containsString(report.ProblemCodes, "snapshot_digest_mismatch") {
		t.Fatalf("damaged report=%#v err=%v", report, err)
	}
	stored, _ := store.Snapshot(snapshot.ID)
	if stored.Content != "被破坏的正文" {
		t.Fatal("verification unexpectedly repaired snapshot")
	}
}

func containsString(items []string, expected string) bool {
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}
