package api

import (
	"bytes"
	"encoding/json"
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testServer(t *testing.T) http.Handler {
	t.Helper()
	store, err := ledger.New(t.TempDir() + "/ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	return New(workflow.New(store)).Handler()
}

func TestDossierListValidationAndEmptyArray(t *testing.T) {
	handler := testServer(t)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/dossiers?status=draft&limit=2", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || page.Items == nil || len(page.Items) != 0 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	for _, target := range []string{"/api/v1/dossiers?status=unknown", "/api/v1/dossiers?limit=0", "/api/v1/dossiers?extra=1"} {
		request = httptest.NewRequest(http.MethodGet, target, nil)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("target=%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
}

func TestClaimCreationRejectsForeignEvidenceWithoutPartialWrite(t *testing.T) {
	store, _ := ledger.New(t.TempDir() + "/ledger.json")
	service := workflow.New(store)
	handler := New(service).Handler()
	first, _ := service.CreateDossier("展览", "OBJ-1", "甲", "正文", "负责人")
	second, _ := service.CreateDossier("展览", "OBJ-2", "乙", "正文", "负责人")
	evidence, _ := service.AddEvidence(second.ID, second.Version, "图录", "馆藏图录", "第1页", "证据", "馆藏记录", "编写人")
	body, _ := json.Marshal(map[string]any{"expectedVersion": first.Version, "statement": "事实", "category": "年代", "evidenceIds": []string{evidence.ID}})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/dossiers/"+first.ID+"/claims", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(store.Claims(first.ID, first.CurrentRevision)) != 0 {
		t.Fatal("跨案卷证据请求留下了主张")
	}
}

func TestRefillRoutesReachWorkflow(t *testing.T) {
	store, _ := ledger.New(t.TempDir() + "/ledger.json")
	service := workflow.New(store)
	handler := New(service).Handler()
	dossier, _ := service.CreateDossier("展览", "OBJ-1", "旧标题", "正文", "甲")

	requestJSON := func(method, path string, body any, operator string) *httptest.ResponseRecorder {
		encoded, _ := json.Marshal(body)
		request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
		request.Header.Set("Content-Type", "application/json")
		if operator != "" {
			request.Header.Set("X-Operator", operator)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}

	response := requestJSON(http.MethodPatch, "/api/v1/dossiers/"+dossier.ID, map[string]any{"expectedVersion": dossier.Version, "title": "新标题", "owner": "乙"}, "交接人")
	if response.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", response.Code, response.Body.String())
	}
	dossier, _ = store.GetDossier(dossier.ID)
	oldEvidence, _ := service.AddEvidence(dossier.ID, dossier.Version, "图录", "旧图录", "p.1", "旧证据", "可靠", "编写人")
	dossier, _ = store.GetDossier(dossier.ID)
	newEvidence, _ := service.AddEvidence(dossier.ID, dossier.Version, "图录", "新图录", "p.2", "新证据", "可靠", "编写人")
	dossier, _ = store.GetDossier(dossier.ID)
	response = requestJSON(http.MethodPost, "/api/v1/dossiers/"+dossier.ID+"/evidence/"+oldEvidence.ID+"/supersede", map[string]any{"expectedVersion": dossier.Version, "replacementEvidenceId": newEvidence.ID, "reason": "勘误"}, "编写人")
	if response.Code != http.StatusOK {
		t.Fatalf("supersede status=%d body=%s", response.Code, response.Body.String())
	}
	dossier, _ = store.GetDossier(dossier.ID)
	claim, dossier, _ := service.CreateClaimWithEvidence(dossier.ID, dossier.Version, "事实", "其他", []string{newEvidence.ID}, "编写人")
	precheck, err := service.RunPrecheckWithKey(dossier.ID, dossier.Version, "研究员", "http-refill")
	if err != nil {
		t.Fatal(err)
	}
	response = requestJSON(http.MethodPut, "/api/v1/dossiers/"+dossier.ID+"/expert-review/drafts/"+claim.ID, map[string]any{"expectedVersion": precheck.Dossier.Version, "decision": "pass", "reason": "来源充分"}, "研究员")
	if response.Code != http.StatusOK {
		t.Fatalf("draft status=%d body=%s", response.Code, response.Body.String())
	}
	dossier, _ = store.GetDossier(dossier.ID)
	response = requestJSON(http.MethodPost, "/api/v1/dossiers/"+dossier.ID+"/expert-review/finalize", map[string]any{"expectedVersion": dossier.Version}, "研究员")
	if response.Code != http.StatusOK {
		t.Fatalf("finalize status=%d body=%s", response.Code, response.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/dossiers/"+dossier.ID+"/evidence/"+oldEvidence.ID+"/usage", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("usage status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/credentials/PUB-missing/verify", nil)
	request.Header.Set("X-Operator", "审计员")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("verify status=%d body=%s", response.Code, response.Body.String())
	}
}
