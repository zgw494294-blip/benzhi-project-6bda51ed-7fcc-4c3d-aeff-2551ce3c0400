package auditcache_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"museum-label-governance/internal/api"
	"museum-label-governance/internal/label"
	"museum-label-governance/internal/ledger"
	"museum-label-governance/internal/workflow"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuditReadAfterMutationDoesNotReuseStaleCache(t *testing.T) {
	store, err := ledger.New(t.TempDir() + "/ledger.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.New(workflow.New(store)).Handler())
	t.Cleanup(server.Close)

	created := requestJSON(t, server.URL, http.MethodPost, "/api/v1/dossiers", map[string]any{
		"exhibitionName": "青铜文明",
		"objectRef":      "OBJ-CACHE-1",
		"title":          "初始标题",
		"content":        "器物事实正文",
		"owner":          "编写人",
	}, http.StatusCreated)
	var dossier label.Dossier
	decodeField(t, created, "dossier", &dossier)

	first := requestJSON(t, server.URL, http.MethodGet, "/api/v1/dossiers/"+dossier.ID, nil, http.StatusOK)
	var before label.Dossier
	decodeField(t, first, "dossier", &before)

	requestJSON(t, server.URL, http.MethodPatch, "/api/v1/dossiers/"+dossier.ID, map[string]any{
		"expectedVersion": before.Version,
		"title":           "修订后的标题",
	}, http.StatusOK)

	second := requestJSON(t, server.URL, http.MethodGet, "/api/v1/dossiers/"+dossier.ID, nil, http.StatusOK)
	var after label.Dossier
	decodeField(t, second, "dossier", &after)
	if after.Title != "修订后的标题" || after.Version != before.Version+1 {
		t.Fatalf("读取写入后的案卷仍命中旧审计缓存: before=%+v after=%+v", before, after)
	}
}

func requestJSON(t *testing.T, baseURL, method, path string, body any, wantStatus int) map[string]json.RawMessage {
	t.Helper()
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, baseURL+path, payload)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%s", method, path, resp.StatusCode, wantStatus, data)
	}
	result := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode response: %v: %s", err, data)
	}
	return result
}

func decodeField(t *testing.T, value map[string]json.RawMessage, field string, target any) {
	t.Helper()
	raw, ok := value[field]
	if !ok {
		t.Fatal(fmt.Sprintf("响应缺少字段 %s", field))
	}
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode %s: %v", field, err)
	}
}
