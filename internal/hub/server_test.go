package hub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/service"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func TestShutdownFlushesPendingCommit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
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
	if _, err := service.New(root).Save(store.GlobalProject, store.Fact{Name: "shutdown", Body: "flush me"}); err != nil {
		t.Fatal(err)
	}
	s, err := Start(Options{Root: root, Addr: "127.0.0.1:0", AutoCommit: true, Debounce: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	s.committer.Schedule("mem(global): shutdown-test")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Wait(); err != nil {
		t.Fatal(err)
	}
	status, err := gitx.Status(root)
	if err != nil {
		t.Fatal(err)
	}
	if status != "" {
		t.Fatalf("expected commit flush to leave a clean managed tree, status=%q", status)
	}
	commits, err := gitx.LogEntries(root, 1)
	if err != nil || len(commits) != 1 || commits[0].Subject != "mem(global): shutdown-test" {
		t.Fatalf("pending commit was not flushed: %+v err=%v", commits, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, store.GlobalProject))
	if err != nil || len(entries) == 0 {
		t.Fatalf("memory file missing after shutdown: %v", err)
	}
}

func TestStartRejectsNonLoopbackListenAddress(t *testing.T) {
	if _, err := Start(Options{Root: t.TempDir(), Addr: "0.0.0.0:3777"}); err == nil {
		t.Fatal("Start accepted a non-loopback listen address")
	}
}
