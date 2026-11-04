package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func seededRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	for _, f := range []store.Fact{
		{Name: "wechat-pay-hook", Description: "微信支付回调幂等处理", Type: "project", Body: "启用微信支付服务后检查回调。"},
		{Name: "deploy-gates", Description: "deploy gates", Type: "feedback", Body: "run smoke tests before production."},
	} {
		if _, err := store.WriteFact(root, "demo-0123456789abcdef", f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := index.Rebuild(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestHandleStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	handleStatus(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil), Options{Root: seededRoot(t)})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Facts    int                    `json:"facts"`
		Projects []struct{ Key string } `json:"projects"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Facts != 2 || len(body.Projects) != 1 {
		t.Fatalf("unexpected status: %+v", body)
	}
}

func TestHandleSearch(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=%E5%BE%AE%E4%BF%A1", nil)
	handleSearch(rec, req, Options{Root: seededRoot(t)})
	if rec.Code != http.StatusOK {
		t.Fatalf("search = %d: %s", rec.Code, rec.Body)
	}
	var body struct {
		Count   int                     `json:"count"`
		Results []struct{ Name string } `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Count != 1 || body.Results[0].Name != "wechat-pay-hook" {
		t.Fatalf("unexpected results: %+v", body)
	}
}

func TestHandleMemorySave(t *testing.T) {
	root := seededRoot(t)
	payload := []byte(`{"project":"demo-0123456789abcdef","name":"hub-save","description":"保存测试","type":"reference","body":"通过 API 保存。"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/memory", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	handleMemory(rec, req, Options{Root: root})
	if rec.Code != http.StatusOK {
		t.Fatalf("save = %d: %s", rec.Code, rec.Body)
	}
	var body struct{ Path string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	fact, err := store.ParseFile(body.Path)
	if err != nil {
		t.Fatal(err)
	}
	if fact.Name != "hub-save" || fact.Body == "" {
		t.Fatalf("saved fact wrong: %+v", fact)
	}
}

func TestMCPAuthRequired(t *testing.T) {
	root := seededRoot(t)
	handler := MCPHTTPHandler(NewMCPServer(root), "secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{}`))))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}
