package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAliasesRoundTrip(t *testing.T) {
	root := t.TempDir()
	aliases, err := LoadAliases(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 0 {
		t.Fatalf("expected empty map, got %v", aliases)
	}
	err = SaveAliases(root, map[string]string{
		"demo-0123456789abcdef": "  Demo 项目  ",
		"marktoast-qa":          "QA",
		"":                      "ignored",
		"empty":                 "   ",
	})
	if err != nil {
		t.Fatal(err)
	}
	aliases, err = LoadAliases(root)
	if err != nil {
		t.Fatal(err)
	}
	if aliases["demo-0123456789abcdef"] != "Demo 项目" {
		t.Fatalf("unexpected alias: %q", aliases["demo-0123456789abcdef"])
	}
	if aliases["marktoast-qa"] != "QA" {
		t.Fatalf("unexpected alias: %q", aliases["marktoast-qa"])
	}
	if _, ok := aliases[""]; ok {
		t.Fatal("empty key should be dropped")
	}
	if _, ok := aliases["empty"]; ok {
		t.Fatal("empty alias should be dropped")
	}
	if _, err := os.Stat(filepath.Join(root, MetaDir, "aliases.json")); err != nil {
		t.Fatalf("aliases file missing: %v", err)
	}
}

func TestAliasesBadJSON(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, MetaDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(AliasesPath(root), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAliases(root); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}
