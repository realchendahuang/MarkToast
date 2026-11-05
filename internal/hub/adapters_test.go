package hub

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/adapter"
)

// TestHandleAdaptersContract pins the /api/adapters JSON shape the Web UI and
// scripts rely on: the new capability fields plus the legacy ones.
func TestHandleAdaptersContract(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	rec := httptest.NewRecorder()
	handleAdapters(rec, httptest.NewRequest(http.MethodGet, "/api/adapters", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	var payload struct {
		Adapters []map[string]any `json:"adapters"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Adapters) == 0 {
		t.Fatal("no adapters returned")
	}
	known, err := adapter.All("")
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]string{}
	for _, descriptor := range known {
		modes[descriptor.ID] = descriptor.EffectiveInstallMode()
	}
	seen := map[string]bool{}
	for _, row := range payload.Adapters {
		id, _ := row["id"].(string)
		seen[id] = true
		for _, field := range []string{"id", "title", "tier", "capabilities", "detect", "detected", "detected_path", "notes", "hooks", "mcp"} {
			if _, ok := row[field]; !ok {
				t.Errorf("adapter %s: legacy field %q missing", id, field)
			}
		}
		for _, field := range []string{"mode", "install_kind", "installable", "staged", "staged_ready", "installed"} {
			if _, ok := row[field]; !ok {
				t.Errorf("adapter %s: field %q missing", id, field)
			}
		}
		mode, _ := row["mode"].(string)
		if want := modes[id]; want != "" && mode != want {
			t.Errorf("adapter %s: mode=%q want %q", id, mode, want)
		}
		if mode != adapter.ModeStaged {
			if staged, _ := row["staged"].(bool); staged {
				t.Errorf("adapter %s: staged=true for mode %s", id, mode)
			}
		}
		installable, _ := row["installable"].(bool)
		if mode == adapter.ModeAuto {
			if !installable {
				t.Errorf("adapter %s: auto adapter must be installable in /api/adapters", id)
			}
		} else if installable {
			t.Errorf("adapter %s: %s adapter must not be installable", id, mode)
		}
		if mode != adapter.ModeAuto {
			if installed, _ := row["installed"].(bool); installed {
				t.Errorf("adapter %s: %s adapter must report installed=false", id, mode)
			}
		}
		hooks, ok := row["hooks"].(map[string]any)
		if !ok {
			t.Errorf("adapter %s: hooks is not an object", id)
			continue
		}
		for _, field := range []string{"events", "install_kind", "injection", "needs_trust", "schema", "scope", "target"} {
			if _, ok := hooks[field]; !ok {
				t.Errorf("adapter %s: hooks.%s missing", id, field)
			}
		}
		if _, ok := row["mcp"].(map[string]any); !ok {
			t.Errorf("adapter %s: mcp is not an object", id)
		}
	}
	for id := range modes {
		if !seen[id] {
			t.Errorf("adapter %s missing from /api/adapters", id)
		}
	}
}

func TestHandleAdapterActionValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := t.TempDir()

	rec := httptest.NewRecorder()
	body := bytes.NewReader([]byte(`{"id":"does-not-exist","action":"install"}`))
	req := httptest.NewRequest(http.MethodPost, "/api/adapters/action", body)
	handleAdapterAction(rec, req, Options{Root: root})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown adapter status=%d body=%s", rec.Code, rec.Body)
	}

	rec = httptest.NewRecorder()
	body = bytes.NewReader([]byte(`{"id":"claude-code","action":"explode"}`))
	req = httptest.NewRequest(http.MethodPost, "/api/adapters/action", body)
	handleAdapterAction(rec, req, Options{Root: root})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad action status=%d body=%s", rec.Code, rec.Body)
	}
}

// TestHandleAdapterActionStage exercises the generic stage action end to end
// with a temporary HOME so nothing outside the test is touched.
func TestHandleAdapterActionStage(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()

	adapters, err := adapter.All("")
	if err != nil {
		t.Fatal(err)
	}
	stagedID := ""
	for _, descriptor := range adapters {
		if descriptor.EffectiveInstallMode() == adapter.ModeStaged {
			stagedID = descriptor.ID
			break
		}
	}
	if stagedID == "" {
		t.Skip("no staged adapter in the built-in set")
	}

	rec := httptest.NewRecorder()
	payload := []byte(`{"id":"` + stagedID + `","action":"stage"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/adapters/action", bytes.NewReader(payload))
	handleAdapterAction(rec, req, Options{Root: root})
	if rec.Code != http.StatusOK {
		t.Fatalf("stage status=%d body=%s", rec.Code, rec.Body)
	}
	var report map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["staged"] != true || report["applied"] != true {
		t.Fatalf("unexpected stage report: %#v", report)
	}
	readme := filepath.Join(home, ".marktoast", "adapters", stagedID, "README.txt")
	if _, err := os.Stat(readme); err != nil {
		t.Fatalf("staged README missing: %v", err)
	}
}
