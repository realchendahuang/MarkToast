package harness

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStageDsh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	report, err := Stage("dsh", "/tmp/memory", "/usr/local/bin/marktoast", "token", []string{"SessionStart", "Stop"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Dir != filepath.Join(home, ".marktoast", "adapters", "dsh") {
		t.Fatalf("unexpected dir: %s", report.Dir)
	}
	if len(report.Files) != 2 {
		t.Fatalf("unexpected files: %v", report.Files)
	}
	data, err := os.ReadFile(filepath.Join(report.Dir, "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "--event") || !strings.Contains(string(data), "/usr/local/bin/marktoast") {
		t.Fatalf("hooks.json looks wrong: %s", data)
	}
	if _, err := os.Stat(filepath.Join(report.Dir, "README.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage("unknown", "/tmp/memory", "bin", "token", nil); err == nil {
		t.Fatal("expected unknown adapter to fail")
	}
}
