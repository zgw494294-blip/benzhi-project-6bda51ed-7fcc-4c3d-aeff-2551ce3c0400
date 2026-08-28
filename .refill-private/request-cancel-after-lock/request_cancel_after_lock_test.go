package requestcancelafterlock_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"museum-label-governance/internal/api"
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

type observedContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (c *observedContext) Err() error {
	c.once.Do(func() { close(c.checked) })
	return c.Context.Err()
}

func TestCanceledPatchDoesNotCommitAfterWaitingForLedgerLock(t *testing.T) {
	store, err := ledger.New(t.TempDir() + "/ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	service := workflow.New(store)
	dossier, err := service.CreateDossier("丝路展", "OBJ-17", "原展签", "原始正文", "编写人")
	if err != nil {
		t.Fatal(err)
	}

	lockHeld := make(chan struct{})
	releaseLock := make(chan struct{})
	lockDone := make(chan error, 1)
	go func() {
		lockDone <- store.Update(func(_ *ledger.State) error {
			close(lockHeld)
			<-releaseLock
			return nil
		})
	}()
	<-lockHeld

	body, err := json.Marshal(map[string]any{
		"expectedVersion": dossier.Version,
		"title":           "取消后不应保存的标题",
	})
	if err != nil {
		t.Fatal(err)
	}
	baseContext, cancel := context.WithCancel(context.Background())
	checked := make(chan struct{})
	requestContext := &observedContext{Context: baseContext, checked: checked}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/dossiers/"+dossier.ID, bytes.NewReader(body)).WithContext(requestContext)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handlerDone := make(chan struct{})
	go func() {
		api.New(service).Handler().ServeHTTP(response, request)
		close(handlerDone)
	}()

	<-checked
	cancel()
	close(releaseLock)
	if err := <-lockDone; err != nil {
		t.Fatal(err)
	}
	<-handlerDone

	stored, err := store.GetDossier(dossier.ID)
	if err != nil {
		t.Fatal(err)
	}
	if response.Code >= 200 && response.Code < 300 || stored.Title != dossier.Title || stored.Version != dossier.Version {
		t.Errorf("canceled request committed: status=%d title=%q version=%d", response.Code, stored.Title, stored.Version)
	}

	waitingStore, err := ledger.New(t.TempDir() + "/waiting-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	waitingLockHeld := make(chan struct{})
	waitingRelease := make(chan struct{})
	waitingLockDone := make(chan error, 1)
	go func() {
		waitingLockDone <- waitingStore.Update(func(_ *ledger.State) error {
			close(waitingLockHeld)
			<-waitingRelease
			return nil
		})
	}()
	<-waitingLockHeld
	waitingBase, waitingCancel := context.WithCancel(context.Background())
	waitingChecked := make(chan struct{})
	waitingContext := &observedContext{Context: waitingBase, checked: waitingChecked}
	callbackRan := false
	waitingUpdateDone := make(chan error, 1)
	go func() {
		waitingUpdateDone <- waitingStore.UpdateContext(waitingContext, func(_ *ledger.State) error {
			callbackRan = true
			return nil
		})
	}()
	<-waitingChecked
	waitingCancel()
	close(waitingRelease)
	if err := <-waitingLockDone; err != nil {
		t.Fatal(err)
	}
	waitingErr := <-waitingUpdateDone
	if !errors.Is(waitingErr, context.Canceled) || callbackRan {
		t.Errorf("canceled lock waiter ran callback: err=%v callbackRan=%t", waitingErr, callbackRan)
	}

	callbackStore, err := ledger.New(t.TempDir() + "/callback-ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	callbackService := workflow.New(callbackStore)
	callbackDossier, err := callbackService.CreateDossier("海贸展", "OBJ-29", "回调前标题", "原始正文", "编写人")
	if err != nil {
		t.Fatal(err)
	}
	callbackContext, callbackCancel := context.WithCancel(context.Background())
	callbackErr := callbackStore.UpdateContext(callbackContext, func(st *ledger.State) error {
		updated := st.Dossiers[callbackDossier.ID]
		updated.Title = "取消后不应落盘的回调标题"
		updated.Version++
		st.Dossiers[callbackDossier.ID] = updated
		callbackCancel()
		return nil
	})
	callbackStored, err := callbackStore.GetDossier(callbackDossier.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !errors.Is(callbackErr, context.Canceled) || callbackStored.Title != callbackDossier.Title || callbackStored.Version != callbackDossier.Version {
		t.Errorf("canceled callback persisted state: err=%v title=%q version=%d", callbackErr, callbackStored.Title, callbackStored.Version)
	}
}
