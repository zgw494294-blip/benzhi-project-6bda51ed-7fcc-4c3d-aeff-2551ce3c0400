package idempotency_fast_cache_isolation_test

import (
	"museum-label-governance/internal/label"
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"path/filepath"
	"sync"
	"testing"
)

func newService(t *testing.T) (*workflow.Service, *ledger.Store) {
	t.Helper()
	store, err := ledger.New(filepath.Join(t.TempDir(), "ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	return workflow.New(store), store
}

func supportedDossier(t *testing.T, svc *workflow.Service, title string) label.Dossier {
	t.Helper()
	dossier, err := svc.CreateDossier("并发测试展", "OBJ-"+title, title, "事实正文", "编写人")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := svc.AddEvidence(dossier.ID, dossier.Version, "图录", "馆藏图录", "p.1", "事实正文", "来源可靠", "编写人")
	if err != nil {
		t.Fatal(err)
	}
	dossier.Version++
	_, dossier, err = svc.CreateClaimWithEvidence(dossier.ID, dossier.Version, "事实正文", "其他", []string{evidence.ID}, "编写人")
	if err != nil {
		t.Fatal(err)
	}
	return dossier
}

func TestIdempotencyFastCacheIsScopedAndSynchronized(t *testing.T) {
	svc, store := newService(t)
	first := supportedDossier(t, svc, "第一案卷")
	second := supportedDossier(t, svc, "第二案卷")

	firstResult, err := svc.RunPrecheckWithKey(first.ID, first.Version, "研究员", "shared-precheck-key")
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := svc.RunPrecheckWithKey(second.ID, second.Version, "研究员", "shared-precheck-key")
	if err != nil {
		t.Fatal(err)
	}
	if secondResult.Dossier.ID != second.ID {
		t.Errorf("幂等回放串案: got dossier %s, want %s", secondResult.Dossier.ID, second.ID)
	}
	storedSecond, err := store.GetDossier(second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedSecond.Status != label.StatusPrechecked {
		t.Errorf("第二案卷未执行预检: got status %s, first result was %s", storedSecond.Status, firstResult.Dossier.ID)
	}

	const workers = 24
	start := make(chan struct{})
	errors := make(chan error, workers)
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(workers)
	done.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			result, replayErr := svc.RunPrecheckWithKey(first.ID, first.Version, "研究员", "shared-precheck-key")
			if replayErr == nil && result.Dossier.ID != first.ID {
				replayErr = workflow.ErrIdempotency
			}
			errors <- replayErr
		}()
	}
	ready.Wait()
	close(start)
	done.Wait()
	close(errors)
	for replayErr := range errors {
		if replayErr != nil {
			t.Errorf("并发回放失败: %v", replayErr)
		}
	}
}
