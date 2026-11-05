package gitx

import (
	"os"
	"path/filepath"
	"strings"
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
	managedDir := filepath.Join(root, "global")
	if err := os.MkdirAll(managedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".marktoast"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".marktoast", "token"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(managedDir, "a.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "user-note.md"), []byte("not managed"), 0o600); err != nil {
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
	if !strings.Contains(status, "user-note.md") {
		t.Fatalf("unmanaged user file should remain uncommitted, status=%q", status)
	}
	if strings.Contains(status, ".marktoast") {
		t.Fatalf("derived token must be ignored, status=%q", status)
	}
	if strings.Contains(status, "global/") {
		t.Fatalf("managed file should be committed, status=%q", status)
	}
}

func TestCommitAllDoesNotCommitPrestagedUserFiles(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "MarkToast Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "MarkToast Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	root := t.TempDir()
	if _, err := EnsureRepo(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "global"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "global", "fact.md"), []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitAll(root, "managed one"); err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(root, "user-note.md")
	if err := os.WriteFile(user, []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(root, "add", "user-note.md"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "global", "fact.md"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitAll(root, "managed two"); err != nil {
		t.Fatal(err)
	}
	files, err := Run(root, "show", "--name-only", "--format=", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(files, "user-note.md") {
		t.Fatalf("managed commit included pre-staged user file: %s", files)
	}
	status, err := Status(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "A  user-note.md") {
		t.Fatalf("pre-staged user file should remain staged, status=%q", status)
	}
}
