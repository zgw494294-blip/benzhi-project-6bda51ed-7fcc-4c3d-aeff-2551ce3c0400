package credentialevidence_test

import (
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"testing"
)

func TestCredentialVerificationRejectsTamperedEvidenceExcerpt(t *testing.T) {
	store, err := ledger.New(t.TempDir() + "/ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	svc := workflow.New(store)
	dossier, err := svc.CreateDossier("展览", "OBJ-1", "标题", "事实", "负责人")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := svc.AddEvidence(dossier.ID, dossier.Version, "图录", "馆藏图录", "第1页", "事实证据", "馆藏记录", "编写人")
	if err != nil {
		t.Fatal(err)
	}
	dossier.Version++
	claim, dossier, err := svc.CreateClaimWithEvidence(dossier.ID, dossier.Version, "事实", "其他", []string{evidence.ID}, "编写人")
	if err != nil {
		t.Fatal(err)
	}
	precheck, err := svc.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "evidence-precheck")
	if err != nil {
		t.Fatal(err)
	}
	review, err := svc.ReviewClaimsBatch(dossier.ID, precheck.Dossier.Version, []workflow.ReviewInput{{ClaimID: claim.ID, Decision: "pass", Reason: "来源充分"}}, "研究员", "evidence-review")
	if err != nil {
		t.Fatal(err)
	}
	copyResult, err := svc.CopyReviewDetailed(dossier.ID, review.Dossier.Version, workflow.CopyReviewInput{Decision: "pass", Actor: "文字编辑"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := svc.Freeze(dossier.ID, copyResult.Dossier.Version, "负责人")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := svc.Issue(dossier.ID, copyResult.Dossier.Version+1, snapshot.ID, "发布负责人")
	if err != nil {
		t.Fatal(err)
	}
	err = store.Update(func(st *ledger.State) error {
		changed := st.Snapshots[snapshot.ID]
		changed.EvidenceManifest[0].Excerpt = "已被替换的证据摘录"
		st.Snapshots[snapshot.ID] = changed
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.VerifyCredential(credential.CredentialNo, "审计员")
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid {
		t.Fatalf("证据摘录与其checksum不一致却仍被判定为有效: %#v", report)
	}
}
