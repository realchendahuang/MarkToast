package lockfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquireIsReentrantAndConcurrentSafe(t *testing.T) {
	if curGoroutineID() == 0 {
		t.Fatal("expected non-zero goroutine ID")
	}
	root := t.TempDir()
	unlockOuter, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	unlockInner, err := Acquire(root)
	if err != nil {
		unlockOuter()
		t.Fatalf("reentrant acquire failed: %v", err)
	}
	// Release is idempotent via sync.Once.
	unlockInner()
	unlockInner()
	unlockOuter()

	// Verify true mutual exclusion across concurrent goroutines in one process.
	var active atomic.Int32
	var counter int
	var wg sync.WaitGroup
	const workers = 16
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := Acquire(root)
			if err != nil {
				t.Errorf("concurrent acquire: %v", err)
				return
			}
			defer unlock()
			if cur := active.Add(1); cur != 1 {
				t.Errorf("lock violated mutual exclusion: %d goroutines inside critical section", cur)
			}
			// Nested re-entrant acquire inside each worker must also succeed.
			nestedUnlock, err := Acquire(root)
			if err != nil {
				t.Errorf("nested concurrent acquire: %v", err)
				active.Add(-1)
				return
			}
			v := counter
			time.Sleep(2 * time.Millisecond)
			counter = v + 1
			nestedUnlock()
			active.Add(-1)
		}()
	}
	wg.Wait()
	if counter != workers {
		t.Fatalf("counter = %d, want %d (concurrent critical section raced)", counter, workers)
	}
}

func TestAcquireRejectsSymlinkedMetaDirAndLockFile(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	metaLink := filepath.Join(root, ".marktoast")
	if err := os.Symlink(outside, metaLink); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := Acquire(root); err == nil {
		t.Fatal("expected symlinked .marktoast directory to be rejected")
	}
	if err := os.Remove(metaLink); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(metaLink, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "outside-lock")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(metaLink, "lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(root); err == nil {
		t.Fatal("expected symlinked lock file to be rejected")
	}
}

func TestAcquireNormalizesSymlinkedRootAliases(t *testing.T) {
	realRoot := t.TempDir()
	aliasDir := t.TempDir()
	aliasRoot := filepath.Join(aliasDir, "alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	unlock, err := Acquire(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	var secondAcquired atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		u2, err := Acquire(aliasRoot)
		if err != nil {
			t.Errorf("alias acquire: %v", err)
			return
		}
		secondAcquired.Store(true)
		u2()
	}()
	time.Sleep(30 * time.Millisecond)
	if secondAcquired.Load() {
		unlock()
		<-done
		t.Fatal("symlinked alias bypassed in-process lock held on real root")
	}
	unlock()
	<-done
	if !secondAcquired.Load() {
		t.Fatal("alias goroutine did not acquire lock after release")
	}
}

func TestLockIsCrossProcess(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "child-acquired")
	unlock, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess")
	cmd.Env = append(os.Environ(), "MARKTOAST_LOCK_HELPER=1", "MARKTOAST_LOCK_ROOT="+root, "MARKTOAST_LOCK_MARKER="+marker)
	if err := cmd.Start(); err != nil {
		unlock()
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		unlock()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("child acquired the lock while parent held it (marker err=%v)", err)
	}
	unlock()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("lock helper failed: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("child never acquired the released lock: %v", err)
	}
}

func TestLockHelperProcess(t *testing.T) {
	if os.Getenv("MARKTOAST_LOCK_HELPER") != "1" {
		return
	}
	unlock, err := Acquire(os.Getenv("MARKTOAST_LOCK_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := os.WriteFile(os.Getenv("MARKTOAST_LOCK_MARKER"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
}
