package importer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/realchendahuang/MarkToast/internal/store"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestZCodeImport(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "MarkToast Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "MarkToast Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	source := t.TempDir()
	writeFile(t, filepath.Join(source, "projects", "demo-0123456789abcdef", "memory", "fact.md"),
		"---\nname: fact\ndescription: demo\n---\n\nbody\n")
	writeFile(t, filepath.Join(source, "projects", "demo-0123456789abcdef", "memory", "MEMORY.md"), "# index\n")
	root := t.TempDir()

	result, err := ZCode(root, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Facts != 1 || result.Projects != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	dest := filepath.Join(root, "projects", "demo-0123456789abcdef", "memory", "fact.md")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("imported fact missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "demo-0123456789abcdef", "memory", "MEMORY.md")); err != nil {
		t.Fatal("index should be regenerated")
	}

	again, err := ZCode(root, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Facts != 0 || again.Skipped != 1 {
		t.Fatalf("expected second run to skip, got %+v", again)
	}
}

func TestHermesImport(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "MarkToast Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "MarkToast Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	source := t.TempDir()
	writeFile(t, filepath.Join(source, "memories", "MEMORY.md"),
		"用户工作台 = ~/vault\n§\n浏览器优先 ego\n")
	writeFile(t, filepath.Join(source, "memories", "USER.md"),
		"用户要快，不要长扫描\n§\n中文名字的项目也要能导入\n")
	writeFile(t, filepath.Join(source, "profiles", "coder", "memories", "MEMORY.md"),
		"写代码先读现有配置\n")
	root := t.TempDir()

	result, err := Hermes(root, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Facts != 5 {
		t.Fatalf("expected 5 facts, got %+v", result)
	}
	facts, err := store.ListFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 5 {
		t.Fatalf("expected 5 facts on disk, got %d", len(facts))
	}
	names := map[string]bool{}
	for _, fact := range facts {
		names[fact.Name] = true
	}
	if len(names) != 5 {
		t.Fatalf("fact names collided: %v", names)
	}

	again, err := Hermes(root, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Facts != 0 || again.Skipped != 5 {
		t.Fatalf("expected second run to skip, got %+v", again)
	}
}
