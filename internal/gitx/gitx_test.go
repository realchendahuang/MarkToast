package gitx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureRepoAndCommit(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "MarkToast Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "MarkToast Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	root := t.TempDir()
	created, err := EnsureRepo(root)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected repository to be created")
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatalf(".git missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gitignore")); err != nil {
		t.Fatalf(".gitignore missing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	committed, err := CommitAll(root, "mem: test")
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("expected a commit")
	}
	out, err := Log(root, 5)
	if err != nil {
		t.Fatal(err)
	}
	if out == "" {
		t.Fatal("expected log output")
	}
	status, err := Status(root)
	if err != nil {
		t.Fatal(err)
	}
	if status != "" {
		t.Fatalf("expected clean status, got %q", status)
	}
}
