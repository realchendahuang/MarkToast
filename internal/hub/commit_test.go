package hub

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/realchendahuang/MarkToast/internal/gitx"
	"github.com/realchendahuang/MarkToast/internal/store"
)

func setTestGitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_AUTHOR_NAME", "MarkToast Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "MarkToast Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
}

func TestCommitterBatchesAndFlushes(t *testing.T) {
	setTestGitEnv(t)
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.EnsureRepo(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "global", "a.md"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newCommitter(root, time.Hour)
	c.Schedule("mem: batched")
	if status, err := gitx.Status(root); err != nil || status == "" {
		t.Fatalf("expected uncommitted changes before flush: %q %v", status, err)
	}
	if err := c.Flush(); err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	status, err := gitx.Status(root)
	if err != nil || status != "" {
		t.Fatalf("expected clean tree after flush: %q %v", status, err)
	}
	commits, err := gitx.LogEntries(root, 1)
	if err != nil || len(commits) != 1 || commits[0].Subject != "mem: batched" {
		t.Fatalf("unexpected commits: %+v %v", commits, err)
	}
}

func TestAutoCommitFalseDoesNotCommitOnAPISaveOrMCPSave(t *testing.T) {
	setTestGitEnv(t)
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.EnsureRepo(root); err != nil {
		t.Fatal(err)
	}

	s, err := Start(Options{Root: root, Addr: "127.0.0.1:0", AutoCommit: false})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
		_ = s.Wait()
	}()

	token, err := EnsureToken(root)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	base := "http://" + s.Addr()

	// 1. Save via REST API
	apiReq, err := http.NewRequest(http.MethodPost, base+"/api/memory", strings.NewReader(`{"name":"no-auto-api","type":"decision","body":"api body"}`))
	if err != nil {
		t.Fatal(err)
	}
	apiReq.Header.Set("Authorization", "Bearer "+token)
	apiReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(apiReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("API save status=%d", resp.StatusCode)
	}

	// 2. Save via Hub /mcp endpoint
	mcpPayload := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"memory_save","arguments":{"name":"no-auto-mcp","type":"user","body":"mcp body"}}}`
	mcpReq, err := http.NewRequest(http.MethodPost, base+"/mcp", bytes.NewReader([]byte(mcpPayload)))
	if err != nil {
		t.Fatal(err)
	}
	mcpReq.Header.Set("Authorization", "Bearer "+token)
	mcpReq.Header.Set("Content-Type", "application/json")
	resp, err = client.Do(mcpReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("MCP save status=%d", resp.StatusCode)
	}

	// Files must exist on disk, remain uncommitted in git status, and have zero commits.
	status, err := gitx.Status(root)
	if err != nil {
		t.Fatal(err)
	}
	if status == "" {
		t.Fatal("expected uncommitted files when AutoCommit=false, but working tree was committed")
	}
	commits, _ := gitx.LogEntries(root, 5)
	if len(commits) != 0 {
		t.Fatalf("expected 0 commits when AutoCommit=false, got %+v", commits)
	}
}

func TestCommitterConcurrentScheduleTimerAndFlushNoRace(t *testing.T) {
	setTestGitEnv(t)
	root := t.TempDir()
	if err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.EnsureRepo(root); err != nil {
		t.Fatal(err)
	}

	c := newCommitter(root, 2*time.Millisecond)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			file := filepath.Join(root, "global", fmt.Sprintf("item-%d.md", i))
			_ = os.WriteFile(file, []byte(fmt.Sprintf("body %d\n", i)), 0o600)
			c.Schedule(fmt.Sprintf("mem: item %d", i))
			time.Sleep(time.Duration(i%4) * time.Millisecond)
		}(i)
	}
	wg.Wait()
	if err := c.Flush(); err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	// Idempotent second Flush and late Schedule after Flush must be safe.
	c.Schedule("mem: ignored after close")
	if err := c.Flush(); err != nil {
		t.Fatalf("second Flush error: %v", err)
	}

	status, err := gitx.Status(root)
	if err != nil || status != "" {
		t.Fatalf("expected clean git status after concurrent flush, got status=%q err=%v", status, err)
	}
}
