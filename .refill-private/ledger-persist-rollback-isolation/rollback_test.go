package ledger_persist_rollback_isolation_test

import (
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"os"
	"path/filepath"
	"testing"
)

func TestFailedPersistenceDoesNotLeakTransactionState(t *testing.T) {
	root := t.TempDir()
	activeDir := filepath.Join(root, "active")
	ledgerPath := filepath.Join(activeDir, "ledger.json")
	store, err := ledger.New(ledgerPath)
	if err != nil {
		t.Fatalf("创建账本失败: %v", err)
	}
	svc := workflow.New(store)
	dossier, err := svc.CreateDossier("青铜器展", "OBJ-ROLLBACK", "器物说明", "器物铸造于西周时期。", "编写人")
	if err != nil {
		t.Fatalf("创建案卷失败: %v", err)
	}

	backupDir := filepath.Join(root, "persisted")
	if err := os.Rename(activeDir, backupDir); err != nil {
		t.Fatalf("移动持久化目录失败: %v", err)
	}
	if err := os.WriteFile(activeDir, []byte("阻止重新创建持久化目录"), 0600); err != nil {
		t.Fatalf("创建持久化阻断文件失败: %v", err)
	}

	_, err = svc.AddEvidence(dossier.ID, dossier.Version, "馆藏档案", "征集记录", "第3页", "器物于1956年入藏。", "馆方原始档案", "研究员")
	if err == nil {
		t.Fatal("持久化目录失效后添加证据应返回错误")
	}

	inMemory, err := store.GetDossier(dossier.ID)
	if err != nil {
		t.Fatalf("读取进程内案卷失败: %v", err)
	}
	if inMemory.Version != dossier.Version || len(store.Evidence(dossier.ID)) != 0 {
		t.Fatalf("失败事务污染进程内状态: version=%d evidence=%d", inMemory.Version, len(store.Evidence(dossier.ID)))
	}

	reopened, err := ledger.New(filepath.Join(backupDir, "ledger.json"))
	if err != nil {
		t.Fatalf("重新打开已持久化账本失败: %v", err)
	}
	onDisk, err := reopened.GetDossier(dossier.ID)
	if err != nil {
		t.Fatalf("读取磁盘案卷失败: %v", err)
	}
	if onDisk.Version != dossier.Version || len(reopened.Evidence(dossier.ID)) != 0 {
		t.Fatalf("失败事务不应写入磁盘: version=%d evidence=%d", onDisk.Version, len(reopened.Evidence(dossier.ID)))
	}
}
