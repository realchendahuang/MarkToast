package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveDesktopOptionsPriority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	envRoot := filepath.Join(home, "EnvMemory")
	flagRoot := filepath.Join(home, "FlagMemory")

	t.Setenv("MARKTOAST_ROOT", envRoot)
	t.Setenv("MARKTOAST_ADDR", "127.0.0.1:4888")

	cfg, addr, err := resolveDesktopOptions("", "")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Root != envRoot || addr != "127.0.0.1:4888" {
		t.Fatalf("env fallback failed: root=%q addr=%q", cfg.Root, addr)
	}

	cfg, addr, err = resolveDesktopOptions(flagRoot, "127.0.0.1:4999")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Root != flagRoot || addr != "127.0.0.1:4999" {
		t.Fatalf("flag override failed: root=%q addr=%q", cfg.Root, addr)
	}
}

func TestWaitForHubStrictHealthCheck(t *testing.T) {
	// Wrong payload on /health must return false.
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"starting"}`))
	}))
	defer badSrv.Close()
	if waitForHub(badSrv.URL, 150*time.Millisecond) {
		t.Fatal("waitForHub accepted non-ok health status")
	}

	// Healthy /health returns true immediately.
	goodSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","version":"test"}`))
	}))
	defer goodSrv.Close()
	if !waitForHub(goodSrv.URL, time.Second) {
		t.Fatal("waitForHub rejected healthy server")
	}
}
