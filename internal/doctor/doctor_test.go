package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/adapter"
	"github.com/realchendahuang/MarkToast/internal/harness"
	"github.com/realchendahuang/MarkToast/internal/index"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func TestRun(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(root, store.GlobalProject, store.Fact{Name: "a", Body: "body"}); err != nil {
		t.Fatal(err)
	}
	checks := Run(root)
	seen := map[string]Check{}
	for _, check := range checks {
		seen[check.Name] = check
	}
	if seen["repository"].Status != "ok" || seen["facts"].Status != "ok" || seen["lock"].Status != "ok" {
		t.Fatalf("unexpected checks: %+v", checks)
	}
	if seen["index"].Status != "warn" || !strings.Contains(seen["index"].Hint, "marktoast reindex") {
		t.Fatalf("expected index drift warning before reindex: %+v", seen["index"])
	}
	if _, err := index.Rebuild(root); err != nil {
		t.Fatal(err)
	}
	checks = Run(root)
	for _, check := range checks {
		seen[check.Name] = check
	}
	if seen["index"].Status != "ok" {
		t.Fatalf("expected index ok after Rebuild, got %+v", seen["index"])
	}
}

func TestTokenValidationAndAdapterDrift(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}

	// Corrupt non-hex token must fail so users know why hub.Start refuses it.
	tokenPath := filepath.Join(root, store.MetaDir, "token")
	if err := os.WriteFile(tokenPath, []byte("not-a-valid-hex-token!!"), 0o600); err != nil {
		t.Fatal(err)
	}
	byName := func(checks []Check) map[string]Check {
		m := map[string]Check{}
		for _, c := range checks {
			m[c.Name] = c
		}
		return m
	}
	got := byName(Run(root))
	if got["mcp token"].Status != "fail" {
		t.Fatalf("expected corrupt token to fail, got %+v", got["mcp token"])
	}

	// Valid 64-char hex token passes.
	validHex := strings.Repeat("a1b2c3d4", 8)
	if err := os.WriteFile(tokenPath, []byte(validHex+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got = byName(Run(root))
	if got["mcp token"].Status != "ok" {
		t.Fatalf("expected valid hex token to pass, got %+v", got["mcp token"])
	}

	// Simulate adapter drift: install claude-code with old token, then compare against validHex.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	defaults, err := adapter.Defaults()
	if err != nil {
		t.Fatal(err)
	}
	var claude adapter.Descriptor
	for _, d := range defaults {
		if d.ID == "claude-code" {
			claude = d
			break
		}
	}
	if _, err := harness.Install(claude, harness.Options{
		Root: root, Home: home, Binary: "/usr/local/bin/marktoast", Token: strings.Repeat("00", 32), Apply: true,
	}); err != nil {
		t.Fatal(err)
	}
	got = byName(Run(root))
	if got["adapters"].Status != "warn" || !strings.Contains(got["adapters"].Detail, "drift") {
		t.Fatalf("expected adapter token drift warning, got %+v", got["adapters"])
	}
}
