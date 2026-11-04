package hub

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func TestCommitterBatchesAndFlushes(t *testing.T) {
	t.Setenv("GIT_AUTHOR_NAME", "MarkToast Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "MarkToast Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.EnsureRepo(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newCommitter(root, time.Hour)
	c.Schedule("mem: batched")
	if status, err := gitx.Status(root); err != nil || status == "" {
		t.Fatalf("expected uncommitted changes before flush: %q %v", status, err)
	}
	c.Flush()
	status, err := gitx.Status(root)
	if err != nil || status != "" {
		t.Fatalf("expected clean tree after flush: %q %v", status, err)
	}
	commits, err := gitx.LogEntries(root, 1)
	if err != nil || len(commits) != 1 || commits[0].Subject != "mem: batched" {
		t.Fatalf("unexpected commits: %+v %v", commits, err)
	}
}
