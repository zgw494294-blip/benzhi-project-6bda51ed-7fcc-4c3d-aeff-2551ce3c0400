package workflow

import (
	"errors"
	"museum-label-governance/internal/label"
	"museum-label-governance/internal/ledger"
	"testing"
)

func addSupportedClaim(t *testing.T, svc *Service, dossier label.Dossier, statement string) (label.Dossier, label.Claim) {
	t.Helper()
	evidence, err := svc.AddEvidence(dossier.ID, dossier.Version, "图录", "馆藏图录", "第1页", statement, "馆藏记录", "编写人")
	if err != nil {
		t.Fatal(err)
	}
	dossier.Version++
	claim, updated, err := svc.CreateClaimWithEvidence(dossier.ID, dossier.Version, statement, "其他", []string{evidence.ID, evidence.ID}, "编写人")
	if err != nil {
		t.Fatal(err)
	}
	return updated, claim
}

func TestCompleteReviewFlow(t *testing.T) {
	st, err := ledger.New(t.TempDir() + "/ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	svc := New(st)
	d, err := svc.CreateDossier("展览", "OBJ-1", "标题", "正文", "负责人")
	if err != nil {
		t.Fatal(err)
	}
	e, err := svc.AddEvidence(d.ID, d.Version, "图录", "图录2024", "p.1", "正文证据", "馆藏", "研究员")
	if err != nil {
		t.Fatal(err)
	}
	d.Version++
	c, err := svc.AddClaim(d.ID, d.Version, "该器物为商代", "年代", "编写人")
	if err != nil {
		t.Fatal(err)
	}
	d.Version++
	if _, err = svc.LinkEvidence(d.ID, c.ID, d.Version, []string{e.ID}, "编写人"); err != nil {
		t.Fatal(err)
	}
	d.Version++
	problems, d2, err := svc.RunPrecheck(d.ID, d.Version, "研究员")
	if err != nil || len(problems) != 0 {
		t.Fatalf("precheck: %v %#v", err, problems)
	}
	if d2.Status != label.StatusPrechecked {
		t.Fatalf("status=%s", d2.Status)
	}
	updated, err := svc.ReviewClaim(d.ID, c.ID, d2.Version, "pass", "来源充分", "研究员")
	if err != nil {
		t.Fatal(err)
	}
	snapDossier, _, err := svc.CopyReview(d.ID, updated.Version, "pass", "", "编辑")
	if err != nil {
		t.Fatal(err)
	}
	snap, err := svc.Freeze(d.ID, snapDossier.Version, "负责人")
	if err != nil {
		t.Fatal(err)
	}
	repeated, replayedFreeze, err := svc.FreezeDetailed(d.ID, snapDossier.Version, "负责人")
	if err != nil || !replayedFreeze || repeated.ID != snap.ID {
		t.Fatalf("freeze replay: %#v %v %v", repeated, replayedFreeze, err)
	}
	cred, replay, err := svc.IssueWithKey(d.ID, snapDossier.Version+1, snap.ID, "发布负责人", "issue-1")
	if err != nil || cred.Signature == "" {
		t.Fatalf("issue: %v", err)
	}
	if replay {
		t.Fatal("首次签发不应是重放")
	}
	again, replay, err := svc.IssueWithKey(d.ID, snapDossier.Version+1, snap.ID, "发布负责人", "issue-1")
	if err != nil || !replay || again.CredentialNo != cred.CredentialNo || again.Signature != cred.Signature || !again.IssuedAt.Equal(cred.IssuedAt) {
		t.Fatalf("issue replay: %#v %v %v", again, replay, err)
	}
	issued, frozen := 0, 0
	for _, event := range st.Audits(d.ID) {
		if event.Action == "credential.issued" {
			issued++
		}
		if event.Action == "snapshot.frozen" {
			frozen++
		}
	}
	if issued != 1 || frozen != 1 {
		t.Fatalf("issued audits=%d frozen audits=%d", issued, frozen)
	}
}

func TestContentRevisionInvalidatesOnlyChangedLineClaim(t *testing.T) {
	st, _ := ledger.New(t.TempDir() + "/ledger.json")
	svc := New(st)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "标题", "事实一\n事实二", "负责人")
	dossier, first := addSupportedClaim(t, svc, dossier, "事实一")
	dossier, second := addSupportedClaim(t, svc, dossier, "事实二")
	precheck, err := svc.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "precheck-lines")
	if err != nil {
		t.Fatal(err)
	}
	review, err := svc.ReviewClaimsBatch(dossier.ID, precheck.Dossier.Version, []ReviewInput{{ClaimID: first.ID, Decision: "pass", Reason: "充分"}, {ClaimID: second.ID, Decision: "pass", Reason: "充分"}}, "研究员", "review-lines")
	if err != nil {
		t.Fatal(err)
	}
	returned, err := svc.CopyReviewDetailed(dossier.ID, review.Dossier.Version, CopyReviewInput{Decision: "return", Reason: "调整第一行", Actor: "文字编辑", Suggestions: []label.CopySuggestion{{Kind: "term", Start: 0, End: 1, Suggestion: "调整表述"}}})
	if err != nil {
		t.Fatal(err)
	}
	revised, diff, err := svc.ReviseDetailed(dossier.ID, returned.Dossier.Version, "事实一已修正\n事实二", nil, "编写人")
	if err != nil {
		t.Fatal(err)
	}
	claims := st.Claims(dossier.ID, revised.CurrentRevision)
	byID := map[string]label.Claim{}
	for _, claim := range claims {
		byID[claim.ID] = claim
	}
	if byID[first.ID].ReviewValid || !byID[second.ID].ReviewValid || len(diff.ModifiedClaims) != 1 || diff.ModifiedClaims[0] != first.ID {
		t.Fatalf("claims=%#v diff=%#v", claims, diff)
	}
	revisions := st.Revisions(dossier.ID)
	if len(revisions[1].Claims) != 2 {
		t.Fatalf("修订主张清单未复制: %#v", revisions[1].Claims)
	}
}

func TestIssueRejectsChangedEvidenceAndKeepsFailureAudit(t *testing.T) {
	st, _ := ledger.New(t.TempDir() + "/ledger.json")
	svc := New(st)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "标题", "事实", "负责人")
	dossier, claim := addSupportedClaim(t, svc, dossier, "事实")
	precheck, _ := svc.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "precheck-integrity")
	review, _ := svc.ReviewClaimsBatch(dossier.ID, precheck.Dossier.Version, []ReviewInput{{ClaimID: claim.ID, Decision: "pass", Reason: "充分"}}, "研究员", "review-integrity")
	copyResult, _, err := svc.CopyReview(dossier.ID, review.Dossier.Version, "pass", "", "文字编辑")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.Freeze(dossier.ID, copyResult.Version, "负责人")
	if err != nil {
		t.Fatal(err)
	}
	err = st.Update(func(state *ledger.State) error {
		state.Evidence[dossier.ID][0].Excerpt = "被改写的证据摘录"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.IssueWithKey(dossier.ID, copyResult.Version+1, snapshot.ID, "发布负责人", "issue-integrity")
	if !errors.Is(err, ErrIntegrity) {
		t.Fatalf("expected integrity error, got %v", err)
	}
	current, _ := st.GetDossier(dossier.ID)
	if current.Status != label.StatusFrozen {
		t.Fatalf("status=%s", current.Status)
	}
	found := false
	for _, event := range st.Audits(dossier.ID) {
		if event.Action == "snapshot.integrity_failed" {
			found = true
		}
	}
	if !found {
		t.Fatal("缺少完整性失败审计")
	}
}

func TestPrecheckRejectsOrphanClaim(t *testing.T) {
	st, _ := ledger.New(t.TempDir() + "/ledger.json")
	svc := New(st)
	d, _ := svc.CreateDossier("展览", "OBJ-2", "标题", "正文", "负责人")
	_, _ = svc.AddClaim(d.ID, d.Version, "事实", "其他", "编写人")
	problems, _, err := svc.RunPrecheck(d.ID, d.Version+1, "研究员")
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) == 0 || problems[0].Code != "orphan_claim" {
		t.Fatalf("expected orphan claim, got %#v", problems)
	}
}

func TestDossierQueryUsesStableFilteredCursor(t *testing.T) {
	st, _ := ledger.New(t.TempDir() + "/ledger.json")
	svc := New(st)
	first, _ := svc.CreateDossier("青铜展", "OBJ-1", "甲", "正文", "张三")
	second, _ := svc.CreateDossier("青铜展", "OBJ-2", "乙", "正文", "张三")
	_, _ = svc.CreateDossier("陶瓷展", "OBJ-3", "丙", "正文", "李四")
	page1, err := svc.ListDossiers(DossierQuery{Status: label.StatusDraft, ExhibitionName: "青铜展", Owner: "张三", Limit: 1, Actor: "检索人"})
	if err != nil || len(page1.Items) != 1 || page1.NextCursor == "" {
		t.Fatalf("first page: %#v %v", page1, err)
	}
	page2, err := svc.ListDossiers(DossierQuery{Status: label.StatusDraft, ExhibitionName: "青铜展", Owner: "张三", Limit: 1, Cursor: page1.NextCursor, Actor: "检索人"})
	if err != nil || len(page2.Items) != 1 || page2.Items[0].ID == page1.Items[0].ID {
		t.Fatalf("second page: %#v %v", page2, err)
	}
	got := map[string]bool{page1.Items[0].ID: true, page2.Items[0].ID: true}
	if !got[first.ID] || !got[second.ID] {
		t.Fatalf("unexpected dossiers: %#v", got)
	}
}

func TestClaimEvidenceAssociationIsAtomicAndDeduplicated(t *testing.T) {
	st, _ := ledger.New(t.TempDir() + "/ledger.json")
	svc := New(st)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "标题", "正文", "负责人")
	evidence, _ := svc.AddEvidence(dossier.ID, dossier.Version, "图录", "馆藏图录", "第1页", "证据", "馆藏记录", "编写人")
	dossier.Version++
	claim, updated, err := svc.CreateClaimWithEvidence(dossier.ID, dossier.Version, "  事实   主张  ", " 年代 ", []string{evidence.ID, evidence.ID}, "编写人")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != dossier.Version+1 || len(claim.EvidenceIDs) != 1 || claim.Statement != "事实 主张" {
		t.Fatalf("claim=%#v dossier=%#v", claim, updated)
	}
	other, _ := svc.CreateDossier("展览", "OBJ-2", "标题", "正文", "负责人")
	foreign, _ := svc.AddEvidence(other.ID, other.Version, "图录", "另一图录", "第2页", "外部证据", "馆藏记录", "编写人")
	_, _, err = svc.ReplaceEvidenceLinks(dossier.ID, claim.ID, updated.Version, []string{foreign.ID}, "编写人")
	if !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	current, _ := st.GetDossier(dossier.ID)
	if current.Version != updated.Version || st.Claims(dossier.ID, 1)[0].EvidenceIDs[0] != evidence.ID {
		t.Fatal("失败关联改变了案卷")
	}
}

func TestPrecheckIdempotencyAndBatchRollback(t *testing.T) {
	st, _ := ledger.New(t.TempDir() + "/ledger.json")
	svc := New(st)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "标题", "正文", "负责人")
	dossier, first := addSupportedClaim(t, svc, dossier, "事实一")
	dossier, second := addSupportedClaim(t, svc, dossier, "事实二")
	result, err := svc.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "precheck-1")
	if err != nil || result.ProblemCount != 0 {
		t.Fatalf("precheck=%#v err=%v", result, err)
	}
	replayed, err := svc.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "precheck-1")
	if err != nil || replayed.Dossier.Version != result.Dossier.Version {
		t.Fatalf("replay=%#v err=%v", replayed, err)
	}
	_, err = svc.ReviewClaimsBatch(dossier.ID, result.Dossier.Version, []ReviewInput{{ClaimID: first.ID, Decision: "pass", Reason: "充分"}, {ClaimID: "clm_missing", Decision: "pass", Reason: "充分"}}, "研究员", "review-bad")
	if !errors.Is(err, ledger.ErrNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
	claims := st.Claims(dossier.ID, 1)
	if claims[0].ReviewDecision != "" || claims[1].ReviewDecision != "" {
		t.Fatal("非法批次留下了部分结论")
	}
	current, _ := st.GetDossier(dossier.ID)
	valid, err := svc.ReviewClaimsBatch(dossier.ID, current.Version, []ReviewInput{{ClaimID: first.ID, Decision: "pass", Reason: "充分"}, {ClaimID: second.ID, Decision: "pass", Reason: "充分"}}, "研究员", "review-ok")
	if err != nil || valid.Dossier.Status != label.StatusCopyReview {
		t.Fatalf("review=%#v err=%v", valid, err)
	}
}

func TestRevisionInheritanceAndDifference(t *testing.T) {
	st, _ := ledger.New(t.TempDir() + "/ledger.json")
	svc := New(st)
	dossier, _ := svc.CreateDossier("展览", "OBJ-1", "标题", "第一行\n第二行", "负责人")
	dossier, first := addSupportedClaim(t, svc, dossier, "事实一")
	dossier, second := addSupportedClaim(t, svc, dossier, "事实二")
	precheck, _ := svc.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "precheck")
	review, _ := svc.ReviewClaimsBatch(dossier.ID, precheck.Dossier.Version, []ReviewInput{{ClaimID: first.ID, Decision: "pass", Reason: "充分"}, {ClaimID: second.ID, Decision: "pass", Reason: "充分"}}, "研究员", "review")
	copyResult, err := svc.CopyReviewDetailed(dossier.ID, review.Dossier.Version, CopyReviewInput{Decision: "return", Reason: "调整术语", Actor: "文字编辑", Suggestions: []label.CopySuggestion{{Kind: "term", Start: 0, End: 1, Suggestion: "替换术语", AffectedClaimIDs: []string{first.ID}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.RunPrecheckWithKey(dossier.ID, copyResult.Dossier.Version, "研究员", "same-revision"); !errors.Is(err, ErrState) {
		t.Fatalf("退回后同修订预检应冲突，得到 %v", err)
	}
	revised, diff, err := svc.ReviseDetailed(dossier.ID, copyResult.Dossier.Version, "第一行已改\n第二行", nil, "编写人")
	if err != nil || revised.CurrentRevision != 2 {
		t.Fatalf("revise=%#v diff=%#v err=%v", revised, diff, err)
	}
	claims := st.Claims(dossier.ID, 2)
	byID := map[string]label.Claim{}
	for _, claim := range claims {
		byID[claim.ID] = claim
	}
	if byID[first.ID].ReviewValid || byID[first.ID].ReviewDecision != "" || !byID[second.ID].ReviewValid || byID[second.ID].ReviewDecision != "pass" {
		t.Fatalf("inheritance failed: %#v", claims)
	}
	if len(diff.ModifiedClaims) != 1 || diff.ModifiedClaims[0] != first.ID || len(diff.AddedLines) != 1 || len(diff.RemovedLines) != 1 {
		t.Fatalf("unexpected diff: %#v", diff)
	}
}
