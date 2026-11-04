package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestZCodeInstallAndUninstall(t *testing.T) {
	home := t.TempDir()
	configPath := ZCodeConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("{\n  \"theme\": \"dark\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ZCodeInstalled(home) {
		t.Fatal("expected not installed")
	}

	report, err := InstallZCode(home, "/tmp/memory", "token-123", "/usr/local/bin/marktoast", false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Changed || report.Installed {
		t.Fatalf("dry run changed something: %+v", report)
	}

	report, err = InstallZCode(home, "/tmp/memory", "token-123", "/usr/local/bin/marktoast", true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Changed || !report.Installed {
		t.Fatalf("install did not apply: %+v", report)
	}
	if !ZCodeInstalled(home) {
		t.Fatal("expected ZCodeInstalled after install")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var conf map[string]any
	if err := json.Unmarshal(data, &conf); err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "marktoast") || !strings.Contains(text, "token-123") {
		t.Fatalf("config missing MarkToast entries: %s", text)
	}
	if conf["theme"] != "dark" {
		t.Fatalf("unrelated config key lost: %v", conf["theme"])
	}
	backups, _ := filepath.Glob(configPath + ".marktoast-backup-*")
	if len(backups) != 1 {
		t.Fatalf("expected one backup, got %v", backups)
	}

	report, err = UninstallZCode(home, true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Changed || report.Installed {
		t.Fatalf("uninstall report: %+v", report)
	}
	if ZCodeInstalled(home) {
		t.Fatal("expected uninstalled")
	}
	if data, err := os.ReadFile(configPath); err != nil || strings.Contains(string(data), "marktoast") {
		t.Fatalf("MarkToast entries remained: %s", data)
	}
}
