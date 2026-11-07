package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectKeyStable(t *testing.T) {
	a := ProjectKey("/home/demo/code/feedsieve")
	b := ProjectKey("/home/demo/code/feedsieve")
	if a != b {
		t.Fatalf("project key not stable: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "feedsieve-") || len(a) != len("feedsieve-")+16 {
		t.Fatalf("unexpected project key %q", a)
	}
	if ProjectKey("/home/demo/code/另一个项目")[:8] != "project-" {
		t.Fatalf("non-ascii slug should fall back to project-, got %q", ProjectKey("/home/demo/code/另一个项目"))
	}
}

func TestMemoryDirGlobal(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	got, err := MemoryDir(root, GlobalProject)
	if err != nil || got != filepath.Join(root, GlobalProject) {
		t.Fatalf("global memory dir = %q, err=%v", got, err)
	}
	got, err = MemoryDir(root, "demo-0123456789abcdef")
	if err != nil || got != filepath.Join(root, "projects", "demo-0123456789abcdef", "memory") {
		t.Fatalf("project memory dir = %q, err=%v", got, err)
	}
	for _, bad := range []string{"..", "../../etc", "a/b", `a\b`} {
		if _, err := MemoryDir(root, bad); err == nil {
			t.Fatalf("MemoryDir accepted unsafe project %q", bad)
		}
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
	dir, err := MemoryDir(root, "demo-0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(dir, IndexFile)
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

func TestWriteFactNameCollisionDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	first, err := WriteFact(root, GlobalProject, Fact{Name: "My Note", Body: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := WriteFact(root, GlobalProject, Fact{Name: "my-note", Body: "second"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("colliding names reused the same path: %s", first)
	}
	facts, err := ListFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 2 {
		t.Fatalf("expected two facts after collision, got %d: %+v", len(facts), facts)
	}
	bodies := map[string]string{}
	for _, f := range facts {
		bodies[f.Name] = strings.TrimSpace(f.Body)
	}
	if bodies["My Note"] != "first" || bodies["my-note"] != "second" {
		t.Fatalf("colliding writes lost data: %+v", bodies)
	}
}

func TestWriteIndexesLinksPointAtRealFiles(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteFact(root, GlobalProject, Fact{Name: "中文记忆", Description: "中文描述", Body: "正文"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	dir, err := MemoryDir(root, GlobalProject)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "](")
	end := strings.Index(text[start+2:], ")")
	if start < 0 || end < 0 {
		t.Fatalf("missing markdown link in index: %s", text)
	}
	target := text[start+2 : start+2+end]
	if target == "project.md" {
		t.Fatalf("index still uses sanitized placeholder link: %s", text)
	}
	if _, err := os.Stat(filepath.Join(dir, target)); err != nil {
		t.Fatalf("index link %q does not resolve: %v\n%s", target, err, text)
	}
}

func TestWriteFactRejectsProjectTraversal(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{"..", "../escape", "../../tmp", `..\\escape`, "a/b"} {
		if _, err := WriteFact(root, project, Fact{Name: "x", Body: "x"}); err == nil {
			t.Fatalf("WriteFact accepted unsafe project %q", project)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "escape")); !os.IsNotExist(err) {
		t.Fatalf("unsafe project created data outside expected layout: %v", err)
	}
}

func TestResolveMemoryPathRejectsDerivedAndSymlinks(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	path, err := WriteFact(root, GlobalProject, Fact{Name: "safe", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, project, err := ResolveMemoryPath(root, path); err != nil || project != GlobalProject {
		t.Fatalf("safe path rejected: project=%q err=%v", project, err)
	}
	token := filepath.Join(root, MetaDir, "token")
	if err := os.WriteFile(token, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := ResolveMemoryPath(root, token); err == nil {
		t.Fatal("derived token path was accepted as a memory")
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, GlobalProject, "linked.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, _, _, err := ResolveMemoryPath(root, link); err == nil {
		t.Fatal("symlinked memory outside root was accepted")
	}
}

func TestConcurrentWriteFactIsSerialized(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	const workers = 12
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			_, err := WriteFact(root, GlobalProject, Fact{
				Name: "shared", Body: fmt.Sprintf("body-%d", i),
			})
			errs <- err
		}(i)
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent write failed: %v", err)
		}
	}
	facts, err := ListFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 1 || !strings.HasPrefix(strings.TrimSpace(facts[0].Body), "body-") {
		t.Fatalf("concurrent writes corrupted facts: %+v", facts)
	}
}

func TestWriteFactRejectsOversizedBody(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	fact := Fact{Name: "too-big", Body: strings.Repeat("x", MaxFactBodyBytes+1)}
	if _, err := WriteFact(root, GlobalProject, fact); err == nil {
		t.Fatal("oversized body was accepted")
	}
}

func TestWriteIndexesClearsStaleLinksAfterDelete(t *testing.T) {
	root := t.TempDir()
	if err := Init(root); err != nil {
		t.Fatal(err)
	}
	path, err := WriteFact(root, GlobalProject, Fact{Name: "deleted", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := WriteIndexes(root); err != nil {
		t.Fatal(err)
	}
	dir, err := MemoryDir(root, GlobalProject)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, IndexFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "deleted") {
		t.Fatalf("stale MEMORY.md link remained after delete: %s", data)
	}
}
