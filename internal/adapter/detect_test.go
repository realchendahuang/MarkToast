package adapter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandPathAndDetect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	detected := filepath.Join(home, ".zcode")
	if err := os.MkdirAll(detected, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := ExpandPath("~/.zcode"); got != detected {
		t.Fatalf("ExpandPath = %q, want %q", got, detected)
	}
	found := Detect(Descriptor{Detect: []string{"~/.zcode", "~/.missing"}})
	if len(found) != 1 || found[0] != detected {
		t.Fatalf("Detect = %v", found)
	}
	if !strings.HasSuffix(found[0], ".zcode") {
		t.Fatalf("unexpected detection: %v", found)
	}
}

func TestExpandEnvDefaultAndSuggest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MT_CUSTOM_CFG", "")

	got := ExpandPath("${MT_CUSTOM_CFG:-~/.cline/settings.json}")
	want := filepath.Join(home, ".cline", "settings.json")
	if got != want {
		t.Fatalf("ExpandPath fallback = %q, want %q", got, want)
	}

	t.Setenv("MT_CUSTOM_CFG", filepath.Join(home, "override.json"))
	if got := ExpandPath("${MT_CUSTOM_CFG:-~/.cline/settings.json}"); got != filepath.Join(home, "override.json") {
		t.Fatalf("ExpandPath override = %q", got)
	}

	defaults, err := Defaults()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"agy":        "antigravity-cli",
		"omp":        "oh-my-pi",
		"claude":     "claude-code",
		"claud-code": "claude-code",
		"droid":      "factory-droid",
		"muse":       "meta-muse",
	}
	for input, expected := range cases {
		if actual := Suggest(input, defaults); actual != expected {
			t.Errorf("Suggest(%q) = %q, want %q", input, actual, expected)
		}
	}
}
