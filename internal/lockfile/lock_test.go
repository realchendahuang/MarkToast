package lockfile

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAcquireIsReentrantAndConcurrentSafe(t *testing.T) {
	root := t.TempDir()
	unlockOuter, err := Acquire(root)
	if err != nil {
		t.Fatal(err)
	}
	defer unlockOuter()
	unlockInner, err := Acquire(root)
	if err != nil {
		t.Fatalf("reentrant acquire failed: %v", err)
	}
	unlockInner()

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock, err := Acquire(root)
			if err != nil {
				t.Errorf("concurrent acquire: %v", err)
				return
			}
			unlock()
		}()
	}
	wg.Wait()
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
