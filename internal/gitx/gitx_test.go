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
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inbox", "20261118-candidate.md"), []byte("pending"), 0o600); err != nil {
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
	if strings.Contains(status, "inbox") {
		t.Fatalf("inbox candidates must be ignored so working tree stays clean, status=%q", status)
	}
	if strings.Contains(status, "global/") {
		t.Fatalf("managed file should be committed, status=%q", status)
	}
}

func TestEmptyRepoLogDoesNotFail(t *testing.T) {
	root := t.TempDir()
	if _, err := EnsureRepo(root); err != nil {
		t.Fatal(err)
	}
	out, err := Log(root, 5)
	if err != nil || out != "" {
		t.Fatalf("empty repo Log = %q, err=%v (want empty, nil)", out, err)
	}
	entries, err := LogEntries(root, 5)
	if err != nil || len(entries) != 0 {
		t.Fatalf("empty repo LogEntries = %+v, err=%v", entries, err)
	}
	fileCommits, err := FileLog(root, "global/none.md", 5)
	if err != nil || len(fileCommits) != 0 {
		t.Fatalf("empty repo FileLog = %+v, err=%v", fileCommits, err)
	}
}

func TestCommitAllFallsBackIdentityInBareEnvironment(t *testing.T) {
	emptyHome := t.TempDir()
	t.Setenv("HOME", emptyHome)
	t.Setenv("XDG_CONFIG_HOME", emptyHome)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(emptyHome, "nonexistent-gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "")
	t.Setenv("GIT_AUTHOR_EMAIL", "")
	t.Setenv("GIT_COMMITTER_NAME", "")
	t.Setenv("GIT_COMMITTER_EMAIL", "")

	root := t.TempDir()
	if _, err := EnsureRepo(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "global"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "global", "中文 空格.md"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	committed, err := CommitAll(root, "mem: bare env")
	if err != nil || !committed {
		t.Fatalf("bare environment commit failed: committed=%v err=%v", committed, err)
	}
	entries, err := LogEntries(root, 1)
	if err != nil || len(entries) != 1 || entries[0].Subject != "mem: bare env" {
		t.Fatalf("unexpected commit entries: %+v err=%v", entries, err)
	}
}

func TestUninitialisedSubdirNeverTouchesOuterParentGitRepo(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "Outer Author")
	t.Setenv("GIT_AUTHOR_EMAIL", "outer@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Outer Author")
	t.Setenv("GIT_COMMITTER_EMAIL", "outer@example.com")

	outer := t.TempDir()
	if _, err := EnsureRepo(outer); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outer, "global"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outer, "global", "outer.md"), []byte("outer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitAll(outer, "outer commit"); err != nil {
		t.Fatal(err)
	}

	// Subdirectory inside outer has NO .git of its own.
	nestedRoot := filepath.Join(outer, "sub-memory-root")
	if err := os.MkdirAll(filepath.Join(nestedRoot, "global"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedRoot, "global", "nested.md"), []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := CommitAll(nestedRoot, "should fail"); err == nil {
		t.Fatal("CommitAll in uninitialised child mutated parent Git repository")
	}
	if _, err := Status(nestedRoot); err == nil {
		t.Fatal("Status in uninitialised child leaked parent Git status")
	}
	if _, err := Log(nestedRoot, 5); err == nil {
		t.Fatal("Log in uninitialised child leaked parent Git history")
	}
	if _, err := FileLog(nestedRoot, "global/nested.md", 5); err == nil {
		t.Fatal("FileLog in uninitialised child accessed parent Git repo")
	}
	outerCommits, err := LogEntries(outer, 10)
	if err != nil || len(outerCommits) != 1 {
		t.Fatalf("outer repository was mutated: %+v err=%v", outerCommits, err)
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
