package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectKeyStable(t *testing.T) {
	a := ProjectKey("/Users/kim/code/feedsieve")
	b := ProjectKey("/Users/kim/code/feedsieve")
	if a != b {
		t.Fatalf("project key not stable: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "feedsieve-") || len(a) != len("feedsieve-")+16 {
		t.Fatalf("unexpected project key %q", a)
	}
	if ProjectKey("/Users/kim/code/另一个项目")[:8] != "project-" {
		t.Fatalf("non-ascii slug should fall back to project-, got %q", ProjectKey("/Users/kim/code/另一个项目"))
	}
}

func TestMemoryDirGlobal(t *testing.T) {
	root := t.TempDir()
	if got := MemoryDir(root, GlobalProject); got != filepath.Join(root, GlobalProject) {
		t.Fatalf("global memory dir = %q", got)
	}
	if got := MemoryDir(root, "demo-0123456789abcdef"); got != filepath.Join(root, "projects", "demo-0123456789abcdef", "memory") {
		t.Fatalf("project memory dir = %q", got)
	}
}

func TestWriteGlobalFact(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	path, err := WriteFact(root, GlobalProject, Fact{Name: "global-fact", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, GlobalProject, "global-fact.md")
	if path != want {
		t.Fatalf("global path = %q, want %q", path, want)
	}
	facts, err := ListFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || facts[0].Project != GlobalProject {
		t.Fatalf("unexpected facts: %+v", facts)
	}
}

func TestWriteAndParseFact(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	path, err := WriteFact(root, "demo-0123456789abcdef", Fact{
		Name:        "release-rollback",
		Description: "2026-09 发版回滚",
		Type:        "project",
		Body:        "发版后首页闪屏，决定回滚。",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "type: project") {
		t.Fatalf("frontmatter missing type: %s", data)
	}
	f, err := ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Name != "release-rollback" || f.Type != "project" || !strings.Contains(f.Body, "闪屏") {
		t.Fatalf("parsed fact wrong: %+v", f)
	}
}

func TestWriteFactRedactsSecrets(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	path, err := WriteFact(root, "demo-0123456789abcdef", Fact{
		Name:        "leaked-token",
		Description: "token sk-proj-abcdefghijklmnopqrstuvwxyz123456",
		Body:        "never commit ghp_0123456789abcdefghijABCDEFGHIJ",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "sk-proj-abcdefghijklmnopqrstuvwxyz123456") || strings.Contains(string(data), "ghp_0123456789abcdefghijABCDEFGHIJ") {
		t.Fatalf("secret was not redacted: %s", data)
	}
	if !strings.Contains(string(data), "[REDACTED:") {
		t.Fatalf("redaction marker missing: %s", data)
	}
}

func TestWriteIndexes(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b-fact", "a-fact"} {
		if _, err := WriteFact(root, "demo-0123456789abcdef", Fact{
			Name: name, Description: "desc of " + name, Type: "reference", Body: "body",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(MemoryDir(root, "demo-0123456789abcdef"), IndexFile)
	data, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.HasPrefix(text, "# Memory Index") {
		t.Fatalf("index header missing: %q", text)
	}
	if strings.Index(text, "a-fact") > strings.Index(text, "b-fact") {
		t.Fatalf("index should be sorted: %q", text)
	}
}

func TestSanitizeSlug(t *testing.T) {
	cases := map[string]string{
		"Feedsieve":        "feedsieve",
		"My Project!":      "my-project",
		"../../etc/passwd": "etc-passwd",
		"":                 "project",
		"中文":               "project",
	}
	for in, want := range cases {
		if got := SanitizeSlug(in); got != want {
			t.Errorf("SanitizeSlug(%q) = %q, want %q", in, got, want)
		}
	}
}
