package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileHistoryDiffAndRestore(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "MarkToast Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "MarkToast Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	root := t.TempDir()
	if _, err := EnsureRepo(root); err != nil {
		t.Fatal(err)
	}
	rel := "projects/demo/memory/fact.md"
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitAll(root, "add fact"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitAll(root, "edit fact"); err != nil {
		t.Fatal(err)
	}

	commits, err := FileLog(root, rel, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
	oldHash := commits[1].Hash

	content, err := ShowFile(root, oldHash, rel)
	if err != nil {
		t.Fatal(err)
	}
	if content != "one\n" {
		t.Fatalf("unexpected old content: %q", content)
	}

	patch, err := DiffFile(root, commits[0].Hash, rel)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(patch, "+two") || !strings.Contains(patch, "-one") {
		t.Fatalf("patch does not contain the edit: %s", patch)
	}

	if _, err := ShowFile(root, "zzzzzz", rel); err == nil {
		t.Fatal("expected invalid hash to fail")
	}
	if _, err := FileLog(root, "../etc/passwd", 10); err == nil {
		t.Fatal("expected path escape to fail")
	}

	committed, err := RestoreFile(root, oldHash, rel)
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("expected restore to commit")
	}
	restored, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != "one\n" {
		t.Fatalf("restore wrote %q", restored)
	}
}
