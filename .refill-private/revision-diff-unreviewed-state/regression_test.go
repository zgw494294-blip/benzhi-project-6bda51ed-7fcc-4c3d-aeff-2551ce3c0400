package revisiondiff_test

import (
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"testing"
)

func TestRevisionDiffDoesNotInventModificationForUnreviewedClaim(t *testing.T) {
	store, err := ledger.New(t.TempDir() + "/ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	svc := workflow.New(store)
	dossier, err := svc.CreateDossier("展览", "OBJ-1", "标题", "未改动正文", "负责人")
	if err != nil {
		t.Fatal(err)
	}
	claim, dossier, err := svc.CreateClaimWithEvidence(dossier.ID, dossier.Version, "未核校主张", "其他", nil, "编写人")
	if err != nil {
		t.Fatal(err)
	}
	revised, immediate, err := svc.ReviseDetailed(dossier.ID, dossier.Version, "未改动正文", nil, "编写人")
	if err != nil {
		t.Fatal(err)
	}
	if len(immediate.ModifiedClaims) != 0 {
		t.Fatalf("派生修订已意外标记修改: %#v", immediate)
	}
	reloaded, err := svc.RevisionDiff(dossier.ID, 1, revised.CurrentRevision)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.ModifiedClaims) != 0 {
		t.Fatalf("未核校状态被误当成修订导致的失效，claim=%s diff=%#v", claim.ID, reloaded)
	}
}
