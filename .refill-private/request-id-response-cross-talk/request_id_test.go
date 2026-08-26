package requestidcrosstalk_test

import (
	"bytes"
	"museum-label-governance/internal/api"
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"net/http/httptest"
	"sync"
	"testing"
)

type gatedBody struct {
	reader  *bytes.Reader
	entered chan struct{}
	release <-chan struct{}
	once    sync.Once
}

func (b *gatedBody) Read(p []byte) (int, error) {
	b.once.Do(func() {
		close(b.entered)
		<-b.release
	})
	return b.reader.Read(p)
}

func (b *gatedBody) Close() error { return nil }

func TestConcurrentResponsesKeepRequestIDsIsolated(t *testing.T) {
	store, err := ledger.New(t.TempDir() + "/ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	handler := api.New(workflow.New(store)).Handler()

	entered := make(chan struct{})
	release := make(chan struct{})
	body := &gatedBody{
		reader:  bytes.NewReader([]byte(`{"exhibitionName":"展览","objectRef":"OBJ-1","title":"标题","content":"正文","owner":"负责人"}`)),
		entered: entered,
		release: release,
	}
	first := httptest.NewRequest("POST", "/api/v1/dossiers", body)
	first.Header.Set("Content-Type", "application/json")
	first.Header.Set("X-Request-ID", "request-first")
	firstResponse := httptest.NewRecorder()
	firstDone := make(chan struct{})
	go func() {
		handler.ServeHTTP(firstResponse, first)
		close(firstDone)
	}()

	<-entered
	second := httptest.NewRequest("GET", "/healthz", nil)
	second.Header.Set("X-Request-ID", "request-second")
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, second)
	close(release)
	<-firstDone

	if got := firstResponse.Header().Get("X-Request-ID"); got != "request-first" {
		t.Fatalf("first response X-Request-ID = %q, want %q", got, "request-first")
	}
}
