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
