package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/store"
)

func testHubHandler(t *testing.T) (http.Handler, string, string, string) {
	t.Helper()
	root := seededRoot(t)
	const mcpToken = "0123456789abcdef0123456789abcdef0123456789abcdef"
	const session = "abcdef0123456789abcdef0123456789abcdef0123456789"
	return buildHandler(Options{Root: root}, mcpToken, session), root, mcpToken, session
}

func loopbackRequest(method, target string, body *bytes.Reader) *http.Request {
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, body)
	}
	req.Host = "127.0.0.1:3777"
	return req
}

func TestAPIAuthAndHostGuard(t *testing.T) {
	handler, _, mcpToken, session := testHubHandler(t)

	t.Run("unauthenticated API rejected", func(t *testing.T) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/status", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
		}
	})

	t.Run("non-loopback Host rejected", func(t *testing.T) {
		req := loopbackRequest(http.MethodGet, "/api/status", nil)
		req.Host = "attacker.example:3777"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMisdirectedRequest {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
		}
	})

	t.Run("session cookie works", func(t *testing.T) {
		req := loopbackRequest(http.MethodGet, "/api/status", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
		}
	})

	t.Run("bearer token works for scripts", func(t *testing.T) {
		req := loopbackRequest(http.MethodGet, "/api/status", nil)
		req.Header.Set("Authorization", "Bearer "+mcpToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
		}
	})
}

func TestCookieMutationRequiresSameOrigin(t *testing.T) {
	handler, root, _, session := testHubHandler(t)
	payload := `{"project":"global","name":"csrf-check","body":"body"}`
	req := loopbackRequest(http.MethodPost, "/api/memory", bytes.NewReader([]byte(payload)))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	req.Header.Set("Origin", "https://attacker.example")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin mutation status=%d body=%s", rec.Code, rec.Body)
	}
	facts, err := store.ListFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range facts {
		if f.Name == "csrf-check" {
			t.Fatal("cross-origin mutation was applied")
		}
	}

	req = loopbackRequest(http.MethodPost, "/api/memory", bytes.NewReader([]byte(payload)))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	req.Header.Set("Origin", "http://127.0.0.1:3777")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-origin save status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestRootResponseSetsSessionCookie(t *testing.T) {
	handler, _, _, _ := testHubHandler(t)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, loopbackRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("root status=%d body=%s", rec.Code, rec.Body)
	}
	found := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.HttpOnly && cookie.SameSite == http.SameSiteStrictMode && cookie.Value != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("session cookie was not set on UI response")
	}
}

func TestMCPEmptyTokenFailsClosed(t *testing.T) {
	root := seededRoot(t)
	handler := MCPHTTPHandler(NewMCPServer(root), "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader([]byte(`{}`)))
	req.Host = "127.0.0.1:3777"
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("empty-token MCP status=%d, want 503", rec.Code)
	}
}

func TestEnsureTokenRejectsTruncatedFile(t *testing.T) {
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, store.MetaDir, "token")
	if err := os.WriteFile(path, []byte("short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureToken(root); err == nil {
		t.Fatal("EnsureToken accepted a truncated token")
	}
}

func TestSearchPaginationResponseShape(t *testing.T) {
	handler, _, mcpToken, _ := testHubHandler(t)
	req := loopbackRequest(http.MethodGet, "/api/search?q=deploy&limit=1&offset=0", nil)
	req.Header.Set("Authorization", "Bearer "+mcpToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	var body struct {
		Results []map[string]any `json:"results"`
		Count   int              `json:"count"`
		Total   int              `json:"total"`
		Offset  int              `json:"offset"`
		Limit   int              `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total < 1 || body.Count > 1 || body.Limit != 1 || body.Offset != 0 {
		t.Fatalf("unexpected pagination response: %+v", body)
	}
}

func TestHTTPRejectsProjectTraversalAndDerivedReads(t *testing.T) {
	handler, root, mcpToken, _ := testHubHandler(t)
	payload := `{"project":"../../escape","name":"pwn","body":"bad"}`
	req := loopbackRequest(http.MethodPost, "/api/memory", bytes.NewReader([]byte(payload)))
	req.Header.Set("Authorization", "Bearer "+mcpToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("traversal save status=%d body=%s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
		t.Fatalf("traversal created an escaped file: %v", err)
	}

	tokenPath := filepath.Join(root, store.MetaDir, "token")
	if err := os.WriteFile(tokenPath, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	req = loopbackRequest(http.MethodGet, "/api/memory?path="+tokenPath, nil)
	req.Header.Set("Authorization", "Bearer "+mcpToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("derived token file was readable through memory API: %s", rec.Body)
	}
}

func TestHTTPRejectsOversizedJSONBody(t *testing.T) {
	handler, _, mcpToken, _ := testHubHandler(t)
	oversized := `{"project":"global","name":"too-big","body":"` + strings.Repeat("x", store.MaxFactBodyBytes+2<<20) + `"}`
	req := loopbackRequest(http.MethodPost, "/api/memory", bytes.NewReader([]byte(oversized)))
	req.Header.Set("Authorization", "Bearer "+mcpToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversized body status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestMemoriesPaginationAndSort(t *testing.T) {
	handler, _, mcpToken, _ := testHubHandler(t)
	req := loopbackRequest(http.MethodGet, "/api/memories?limit=1&offset=0&sort=title", nil)
	req.Header.Set("Authorization", "Bearer "+mcpToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	var body struct {
		Memories []struct {
			Name string `json:"name"`
		} `json:"memories"`
		Total  int `json:"total"`
		Count  int `json:"count"`
		Offset int `json:"offset"`
		Limit  int `json:"limit"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Total != 2 || body.Count != 1 || body.Offset != 0 || body.Limit != 1 {
		t.Fatalf("unexpected page metadata: %+v", body)
	}
	if len(body.Memories) != 1 || body.Memories[0].Name != "deploy-gates" {
		t.Fatalf("unexpected title-sorted page: %+v", body.Memories)
	}
}

func TestEnsureTokenRejectsSymlinkedMetaDir(t *testing.T) {
	root := t.TempDir()
	if err := os.RemoveAll(filepath.Join(root, store.MetaDir)); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, store.MetaDir)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := EnsureToken(root); err == nil {
		t.Fatal("EnsureToken followed .marktoast symlink outside the repository")
	}
	if _, err := os.Stat(filepath.Join(outside, "token")); !os.IsNotExist(err) {
		t.Fatalf("token was written outside repository: %v", err)
	}
}
