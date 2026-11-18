package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func authedReq(method, target, token, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.Host = "127.0.0.1:3777"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestMemoryCRUDDeleteAndPreviousPathMove(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	handler, root, token, _ := testHubHandler(t)

	// 1. Save with type="decision"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/memory", token, `{"project":"global","name":"arch-choice","description":"use SQLite FTS5","type":"decision","body":"Keep everything local."}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/memory status=%d body=%s", rec.Code, rec.Body)
	}
	var saved struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}

	// 2. Reject invalid type on POST /api/memory and GET /api/search
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/memory", token, `{"name":"bad","type":"bogus","body":"x"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid type, got %d: %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/search?q=local&type=bogus", token, ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid search type, got %d", rec.Code)
	}

	// 3. Reject traversal in previousPath without creating the new file
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/memory", token, `{"name":"should-not-exist","body":"x","previousPath":"../../outside.md"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on unsafe previousPath, got %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/memory?name=should-not-exist", token, ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for aborted save, got %d", rec.Code)
	}

	// 4. Rename/move across project with previousPath removes old file & refreshes index
	rec = httptest.NewRecorder()
	moveBody, _ := json.Marshal(map[string]any{
		"project":      "moved-proj-0123456789abcdef",
		"name":         "arch-choice-v2",
		"type":         "decision",
		"body":         "Updated local decision.",
		"previousPath": saved.Path,
	})
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/memory", token, string(moveBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("move POST status=%d body=%s", rec.Code, rec.Body)
	}
	var moved struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &moved)
	if _, err := os.Stat(saved.Path); !os.IsNotExist(err) {
		t.Fatalf("old path %s still exists after move", saved.Path)
	}
	factsAfterMove, _, err := index.ListPage(root, index.ListOptions{Limit: 0})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range factsAfterMove {
		if f.Path == saved.Path {
			t.Fatalf("stale index entry remained for old path %s", saved.Path)
		}
	}

	// 5. DELETE /api/memory via query parameter ?path=...
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodDelete, "/api/memory?path="+url.QueryEscape(moved.Path), token, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE via query status=%d body=%s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(moved.Path); !os.IsNotExist(err) {
		t.Fatalf("deleted file %s still exists", moved.Path)
	}
	// Deleting the same file a second time returns 404 Not Found
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodDelete, "/api/memory?path="+url.QueryEscape(moved.Path), token, ""))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("second DELETE status=%d, want 404", rec.Code)
	}

	// 6. DELETE /api/memory via JSON body {"path": "..."}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/memory", token, `{"project":"global","name":"delete-by-body","body":"bye"}`))
	var toDel struct{ Path string }
	_ = json.Unmarshal(rec.Body.Bytes(), &toDel)

	delPayload, _ := json.Marshal(map[string]string{"path": toDel.Path})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodDelete, "/api/memory", token, string(delPayload)))
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE via JSON body status=%d body=%s", rec.Code, rec.Body)
	}

	// 7. DELETE rejects missing path, traversal, and MEMORY.md index file with 400
	for _, bad := range []string{"", "../../etc/passwd", "global/MEMORY.md", ".marktoast/token"} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, authedReq(http.MethodDelete, "/api/memory?path="+url.QueryEscape(bad), token, ""))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("DELETE path=%q status=%d, want 400", bad, rec.Code)
		}
	}

	// 8. GET /api/memory distinguishes 400 (missing/unsafe params) from 404 (not found)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/memory", token, ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("GET /api/memory without params status=%d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/memory?id=deploy-gates", token, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/memory?id=deploy-gates status=%d body=%s", rec.Code, rec.Body)
	}
}

func TestAliasesRejectNilWipeAndSettingsHeaders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	handler, _, token, _ := testHubHandler(t)

	// Save an alias
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPut, "/api/aliases", token, `{"aliases":{"demo-0123456789abcdef":"Demo App"}}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT /api/aliases status=%d body=%s", rec.Code, rec.Body)
	}

	// Omitted or null "aliases" MUST be rejected with 400 without wiping stored aliases
	for _, badBody := range []string{`{}`, `{"aliases":null}`} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, authedReq(http.MethodPut, "/api/aliases", token, badBody))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("PUT /api/aliases %s status=%d, want 400", badBody, rec.Code)
		}
	}

	// Confirm "Demo App" is intact
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/aliases", token, ""))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Demo App") {
		t.Fatalf("stored alias was lost: status=%d body=%s", rec.Code, rec.Body)
	}

	// Explicit empty object {"aliases":{}} intentionally clears aliases
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPut, "/api/aliases", token, `{"aliases":{}}`))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Demo App") {
		t.Fatalf("explicit empty aliases failed: status=%d body=%s", rec.Code, rec.Body)
	}

	// /api/settings sets Cache-Control: no-store
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/settings", token, ""))
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("/api/settings status=%d Cache-Control=%q body=%s", rec.Code, rec.Header().Get("Cache-Control"), rec.Body)
	}
}

func TestHistoryDiffRestoreGitLogInboxBackupReindexAndImport(t *testing.T) {
	setTestGitEnv(t)
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.EnsureRepo(root); err != nil {
		t.Fatal(err)
	}
	const mcpToken = "0123456789abcdef0123456789abcdef0123456789abcdef"
	const session = "abcdef0123456789abcdef0123456789abcdef0123456789"
	handler := buildHandler(Options{Root: root, AutoCommit: true, commitConfigured: true}, mcpToken, session)

	// Save v1 and v2 of a memory to create Git history
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/memory", mcpToken, `{"project":"global","name":"versioned-note","body":"version one"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("save v1: %d %s", rec.Code, rec.Body)
	}
	var saved struct{ Path string }
	_ = json.Unmarshal(rec.Body.Bytes(), &saved)

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/memory", mcpToken, `{"project":"global","name":"versioned-note","body":"version two"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("save v2: %d %s", rec.Code, rec.Body)
	}

	// /api/git/log clamps huge n without failing
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/git/log?n=999999999", mcpToken, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/git/log status=%d body=%s", rec.Code, rec.Body)
	}

	// /api/memory/history returns both commits
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/memory/history?path="+url.QueryEscape(saved.Path), mcpToken, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", rec.Code, rec.Body)
	}
	var hist struct {
		Commits []struct {
			Hash    string `json:"hash"`
			Subject string `json:"subject"`
		} `json:"commits"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &hist)
	if len(hist.Commits) < 2 {
		t.Fatalf("expected >=2 history commits, got %+v", hist.Commits)
	}
	olderHash := hist.Commits[len(hist.Commits)-1].Hash

	// /api/memory/diff rejects option-injection hashes starting with "-"
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/memory/diff?path="+url.QueryEscape(saved.Path)+"&hash=--output=/tmp/x", mcpToken, ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("diff flag injection status=%d, want 400", rec.Code)
	}

	// Valid diff and restore back to v1
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/memory/diff?path="+url.QueryEscape(saved.Path)+"&hash="+olderHash, mcpToken, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("diff status=%d body=%s", rec.Code, rec.Body)
	}

	restorePayload, _ := json.Marshal(map[string]string{"path": saved.Path, "hash": olderHash})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/memory/restore", mcpToken, string(restorePayload)))
	if rec.Code != http.StatusOK {
		t.Fatalf("restore status=%d body=%s", rec.Code, rec.Body)
	}
	fact, err := store.ParseFile(saved.Path)
	if err != nil || !strings.Contains(fact.Body, "version one") {
		t.Fatalf("restored body mismatch: %+v err=%v", fact, err)
	}

	// Inbox workflow: candidate -> list -> promote & dismiss
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/inbox/candidate", mcpToken, `{"name":"cand-1","body":"promote me","agent":"test"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("inbox candidate status=%d body=%s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/inbox/candidate", mcpToken, `{"name":"cand-2","body":"dismiss me","agent":"test"}`))
	if rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodGet, "/api/inbox", mcpToken, ""))
	var inboxList struct {
		Candidates []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"candidates"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &inboxList)
	if len(inboxList.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %+v", inboxList.Candidates)
	}

	promoteBody, _ := json.Marshal(map[string]string{"path": inboxList.Candidates[0].Path, "project": "global"})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/inbox/promote", mcpToken, string(promoteBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("promote status=%d body=%s", rec.Code, rec.Body)
	}

	dismissBody, _ := json.Marshal(map[string]string{"path": inboxList.Candidates[1].Path})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, authedReq(http.MethodPost, "/api/inbox/dismiss", mcpToken, string(dismissBody)))
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss status=%d body=%s", rec.Code, rec.Body)
	}

	zcodeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(zcodeDir, "projects", "demo-proj", "memory"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zcodeDir, "projects", "demo-proj", "memory", "imported.md"), []byte("---\nname: imported\n---\n\nbody\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// /api/projects, /api/reindex, /api/backup, /api/import (dryRun + validation)
	for _, tc := range []struct {
		method string
		path   string
		body   string
		want   int
	}{
		{http.MethodGet, "/api/projects", "", http.StatusOK},
		{http.MethodPost, "/api/reindex", "", http.StatusOK},
		{http.MethodPost, "/api/backup", "", http.StatusOK},
		{http.MethodPost, "/api/import", `{"from":"unknown"}`, http.StatusBadRequest},
		{http.MethodPost, "/api/import", `{"from":"zcode","source":"` + filepath.ToSlash(zcodeDir) + `","dryRun":true}`, http.StatusOK},
		{http.MethodGet, "/api/does-not-exist", "", http.StatusNotFound},
		{http.MethodGet, "/health", "", http.StatusOK},
		{http.MethodHead, "/health", "", http.StatusOK},
	} {
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, authedReq(tc.method, tc.path, mcpToken, tc.body))
		if rec.Code != tc.want {
			t.Fatalf("%s %s status=%d, want %d (body=%s)", tc.method, tc.path, rec.Code, tc.want, rec.Body)
		}
	}
}

func TestMethodNotAllowedHostValidationAndCSRFDefenseInDepth(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	handler, _, token, session := testHubHandler(t)

	// Method checks across read-only and write-only endpoints
	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/health"},
		{http.MethodPost, "/api/status"},
		{http.MethodDelete, "/api/search"},
		{http.MethodPost, "/api/memories"},
		{http.MethodPut, "/api/memory"},
		{http.MethodPost, "/api/projects"},
		{http.MethodPost, "/api/adapters"},
		{http.MethodGet, "/api/adapters/action"},
		{http.MethodPost, "/api/git/log"},
		{http.MethodPost, "/api/settings"},
		{http.MethodGet, "/api/reindex"},
		{http.MethodDelete, "/api/aliases"},
		{http.MethodGet, "/api/import"},
		{http.MethodGet, "/api/backup"},
		{http.MethodPost, "/api/inbox"},
		{http.MethodGet, "/api/inbox/promote"},
		{http.MethodGet, "/api/inbox/dismiss"},
		{http.MethodGet, "/api/inbox/candidate"},
		{http.MethodPost, "/api/memory/history"},
		{http.MethodPost, "/api/memory/diff"},
		{http.MethodGet, "/api/memory/restore"},
		{http.MethodPost, "/"},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, authedReq(tc.method, tc.path, token, ""))
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") == "" {
			t.Fatalf("%s %s status=%d Allow=%q, want 405 with Allow header", tc.method, tc.path, rec.Code, rec.Header().Get("Allow"))
		}
	}

	// Host header edge cases
	for host, wantOK := range map[string]bool{
		"127.0.0.1:3777":   true,
		"localhost:3777":   true,
		"localhost":        true,
		"[::1]:3777":       true,
		"[::1]":            true,
		"[[127.0.0.1]]":    false,
		"127.0.0.1:99999":  false,
		"127.0.0.1:0":      false,
		"127.0.0.1:notnum": false,
		"0.0.0.0:3777":     false,
		"evil.local:3777":  false,
	} {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		gotOK := rec.Code == http.StatusOK
		if gotOK != wantOK {
			t.Fatalf("Host=%q status=%d, wantOK=%v", host, rec.Code, wantOK)
		}
	}

	// CSRF edge cases: Sec-Fetch-Site=same-site (another port on localhost), non-http Origin, mismatched Origin+Sec-Fetch-Site, valid Referer fallback
	csrfCases := []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{
			name:    "same-site different port rejected",
			headers: map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "http://127.0.0.1:8080"},
			want:    http.StatusForbidden,
		},
		{
			name:    "spoofed Sec-Fetch-Site with mismatched Origin rejected",
			headers: map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://127.0.0.1:8080"},
			want:    http.StatusForbidden,
		},
		{
			name:    "non-http scheme Origin rejected",
			headers: map[string]string{"Origin": "ws://127.0.0.1:3777"},
			want:    http.StatusForbidden,
		},
		{
			name:    "missing Origin/Sec-Fetch-Site/Referer rejected",
			headers: map[string]string{},
			want:    http.StatusForbidden,
		},
		{
			name:    "same-origin Referer fallback accepted",
			headers: map[string]string{"Referer": "http://127.0.0.1:3777/"},
			want:    http.StatusOK,
		},
	}
	for _, tc := range csrfCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/reindex", bytes.NewReader(nil))
			req.Host = "127.0.0.1:3777"
			req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d, want %d (body=%s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

func TestAdaptersEndpointIncludesStagedDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/adapters", nil)
	handleAdapters(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	var payload struct {
		Adapters []map[string]any `json:"adapters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, row := range payload.Adapters {
		if _, ok := row["staged_dir"]; !ok {
			t.Fatalf("adapter %v missing staged_dir field", row["id"])
		}
	}
}
