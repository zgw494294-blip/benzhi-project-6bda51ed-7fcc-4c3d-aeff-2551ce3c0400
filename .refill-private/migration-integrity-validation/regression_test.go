package migrationintegrity_test

import (
	"museum-label-governance/internal/label"
	"museum-label-governance/internal/ledger"
	"testing"
)

func TestMigrationRejectsOrphanSnapshot(t *testing.T) {
	path := t.TempDir() + "/ledger.json"
	store, err := ledger.New(path)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Update(func(st *ledger.State) error {
		st.Snapshots["snp_orphan"] = label.Snapshot{
			ID:            "snp_orphan",
			DossierID:     "dos_missing",
			RevisionNo:    1,
			Content:       "正文",
			ContentDigest: label.Digest("正文", nil, nil),
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := ledger.New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.Migrate(); err == nil {
		t.Fatal("启动迁移接受了引用不存在案卷的冻结快照")
	}
}
