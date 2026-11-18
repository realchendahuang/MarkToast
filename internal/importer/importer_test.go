package importer

import (
	"os"
	"path/filepath"
	"strings"
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

func TestZCodeImportDryRunDoesNotTouchDiskAndImportRedactsSecrets(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "MarkToast Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "MarkToast Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	source := t.TempDir()
	secret := "sk-proj-abcdefghijklmnopqrstuvwxyz123456"
	writeFile(t, filepath.Join(source, "projects", "demo-0123456789abcdef", "memory", "fact.md"),
		"---\nname: fact\ndescription: demo\n---\n\nbody with "+secret+"\n")
	writeFile(t, filepath.Join(source, "projects", "demo-0123456789abcdef", "memory", "MEMORY.md"), "# index\n")

	root := filepath.Join(t.TempDir(), "fresh-root")
	dry, err := ZCode(root, source, true)
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.Facts != 1 || dry.Projects != 1 {
		t.Fatalf("unexpected dryRun result: %+v", dry)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("ZCode dryRun created directories on disk: %v", err)
	}

	result, err := ZCode(root, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Facts != 1 || result.Projects != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	dest := filepath.Join(root, "projects", "demo-0123456789abcdef", "memory", "fact.md")
	raw, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("imported fact missing: %v", err)
	}
	if strings.Contains(string(raw), secret) || !strings.Contains(string(raw), "[REDACTED:openai]") {
		t.Fatalf("imported ZCode file bypassed secret redaction: %s", raw)
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

func TestHermesImportCountsActualProjectsAndScopesExistingNamesPerProject(t *testing.T) {
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
		"写代码先读现有配置\n§\n用户要快，不要长扫描\n")
	root := t.TempDir()

	// Pre-populate an unrelated project with the exact derived name of a global
	// Hermes block (different body): global import must NOT be skipped by it.
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := store.WriteFact(root, "other-proj", store.Fact{
		Name: "记忆 · 浏览器优先 ego",
		Body: "different project body",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := Hermes(root, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Facts != 5 {
		t.Fatalf("expected 5 unique facts, got %+v", result)
	}
	if result.Projects != 2 {
		t.Fatalf("expected 2 target projects (global + hermes-coder), got %+v", result)
	}
	if result.Skipped != 1 {
		t.Fatalf("expected 1 duplicate to be skipped, got %+v", result)
	}
	facts, err := store.ListFacts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != 6 {
		t.Fatalf("expected 6 total facts on disk (1 pre-existing + 5 imported), got %d", len(facts))
	}

	again, err := Hermes(root, source, false)
	if err != nil {
		t.Fatal(err)
	}
	if again.Facts != 0 || again.Projects != 0 || again.Skipped != 6 {
		t.Fatalf("expected second run to skip all 6 with 0 projects modified, got %+v", again)
	}
}
