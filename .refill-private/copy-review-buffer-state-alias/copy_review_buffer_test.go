package copyreviewbufferalias

import (
	"bytes"
	"encoding/json"
	"io"
	"museum-label-governance/internal/api"
	"museum-label-governance/internal/label"
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func prepareCopyReview(t *testing.T, service *workflow.Service, objectRef string) (label.Dossier, label.Claim) {
	t.Helper()
	dossier, err := service.CreateDossier("常设展", objectRef, "展签", "器物铸造工艺成熟", "编写人")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := service.AddEvidence(dossier.ID, dossier.Version, "图录", "馆藏图录", "第12页", "铸造记录", "馆藏档案", "编写人")
	if err != nil {
		t.Fatal(err)
	}
	dossier, err = service.Store.GetDossier(dossier.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, dossier, err := service.CreateClaimWithEvidence(dossier.ID, dossier.Version, "器物采用铸造工艺", "工艺", []string{evidence.ID}, "编写人")
	if err != nil {
		t.Fatal(err)
	}
	precheck, err := service.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "")
	if err != nil {
		t.Fatal(err)
	}
	review, err := service.ReviewClaimsBatch(dossier.ID, precheck.Dossier.Version, []workflow.ReviewInput{{ClaimID: claim.ID, Decision: "pass", Reason: "来源充分"}}, "研究员", "")
	if err != nil {
		t.Fatal(err)
	}
	return review.Dossier, claim
}

func copyReviewBody(version int, claimID string) []byte {
	body, _ := json.Marshal(map[string]any{
		"expectedVersion": version,
		"decision":        "return",
		"reason":          "调整术语",
		"suggestions": []map[string]any{{
			"kind":             "term",
			"start":            0,
			"end":              1,
			"suggestion":       "统一工艺术语",
			"affectedClaimIds": []string{claimID},
		}},
	})
	return body
}

func postCopyReview(handler http.Handler, dossier label.Dossier, claimID string, body io.Reader) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/dossiers/"+dossier.ID+"/copy-review", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Operator", "文字编辑")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type gatedReader struct {
	reader *bytes.Reader
	ready  chan<- struct{}
	start  <-chan struct{}
	once   sync.Once
}

func (r *gatedReader) Read(p []byte) (int, error) {
	r.once.Do(func() {
		r.ready <- struct{}{}
		<-r.start
	})
	return r.reader.Read(p)
}

func TestCopyReviewRequestBufferDoesNotCrossContaminateDossiers(t *testing.T) {
	store, err := ledger.New(t.TempDir() + "/ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	service := workflow.New(store)
	handler := api.New(service).Handler()

	firstDossier, firstClaim := prepareCopyReview(t, service, "OBJ-FIRST")
	secondDossier, secondClaim := prepareCopyReview(t, service, "OBJ-SECOND")
	firstResponse := postCopyReview(handler, firstDossier, firstClaim.ID, bytes.NewReader(copyReviewBody(firstDossier.Version, firstClaim.ID)))
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first copy review status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	secondResponse := postCopyReview(handler, secondDossier, secondClaim.ID, bytes.NewReader(copyReviewBody(secondDossier.Version, secondClaim.ID)))
	if secondResponse.Code != http.StatusOK {
		t.Fatalf("second copy review status=%d body=%s", secondResponse.Code, secondResponse.Body.String())
	}
	firstSuggestions, err := service.CopySuggestions(firstDossier.ID, "")
	if err != nil || len(firstSuggestions.Items) != 1 || len(firstSuggestions.Items[0].AffectedClaimIDs) != 1 {
		t.Fatalf("first suggestions=%#v err=%v", firstSuggestions, err)
	}
	contaminated := firstSuggestions.Items[0].AffectedClaimIDs[0] != firstClaim.ID

	thirdDossier, thirdClaim := prepareCopyReview(t, service, "OBJ-THIRD")
	fourthDossier, fourthClaim := prepareCopyReview(t, service, "OBJ-FOURTH")
	ready := make(chan struct{})
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, item := range []struct {
		dossier label.Dossier
		claim   label.Claim
	}{{thirdDossier, thirdClaim}, {fourthDossier, fourthClaim}} {
		wg.Add(1)
		go func(item struct {
			dossier label.Dossier
			claim   label.Claim
		}) {
			defer wg.Done()
			body := copyReviewBody(item.dossier.Version, item.claim.ID)
			reader := &gatedReader{reader: bytes.NewReader(body), ready: ready, start: start}
			postCopyReview(handler, item.dossier, item.claim.ID, reader)
		}(item)
	}
	<-ready
	<-ready
	close(start)
	wg.Wait()

	if contaminated {
		t.Fatalf("first dossier suggestion was rewritten to claim %q after second request", firstSuggestions.Items[0].AffectedClaimIDs[0])
	}
}
