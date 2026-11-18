// Package lockfile provides a per-repository advisory write lock.
//
// The lock is re-entrant within the same goroutine so higher-level operations
// can hold it across several store/index/git steps, while concurrent goroutines
// in the same process and separate MarkToast processes strictly serialise
// against each other.
package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type heldLock struct {
	cond    *sync.Cond
	held    bool
	owner   uint64
	depth   int
	waiters int
	file    *os.File
}

var registry = struct {
	sync.Mutex
	locks map[string]*heldLock
}{locks: map[string]*heldLock{}}

// Acquire locks <root>/.marktoast/lock until the returned function is called.
// It is safe to nest Acquire calls on the same goroutine for the same root,
// while other goroutines and OS processes block until the outermost release.
func Acquire(root string) (func(), error) {
	path, err := canonicalLockPath(root)
	if err != nil {
		return nil, err
	}

	gid := curGoroutineID()
	registry.Lock()
	entry := registry.locks[path]
	if entry == nil {
		entry = &heldLock{}
		entry.cond = sync.NewCond(&registry.Mutex)
		registry.locks[path] = entry
	}
	if entry.held && gid != 0 && entry.owner == gid {
		entry.depth++
		registry.Unlock()
		return releaseFunc(path), nil
	}
	entry.waiters++
	for entry.held {
		entry.cond.Wait()
	}
	entry.waiters--
	entry.held = true
	entry.owner = gid
	entry.depth = 1
	registry.Unlock()

	f, err := openLockFile(path)
	if err != nil {
		abortAcquire(path)
		return nil, err
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		abortAcquire(path)
		return nil, fmt.Errorf("lock repository: %w", err)
	}

	registry.Lock()
	entry.file = f
	registry.Unlock()
	return releaseFunc(path), nil
}

func canonicalLockPath(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("lock root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	metaDir := filepath.Join(abs, ".marktoast")
	if info, lerr := os.Lstat(metaDir); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("unsafe symlinked lock directory: %s", metaDir)
	}
	if err := os.MkdirAll(metaDir, 0o700); err != nil {
		return "", fmt.Errorf("create lock directory: %w", err)
	}
	realRoot, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	realMeta, err := filepath.EvalSymlinks(metaDir)
	if err != nil {
		return "", fmt.Errorf("resolve lock directory: %w", err)
	}
	rel, err := filepath.Rel(realRoot, realMeta)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("lock directory escapes repository")
	}
	return filepath.Join(realMeta, "lock"), nil
}

func openLockFile(path string) (*os.File, error) {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("unsafe lock file: %s", path)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat lock file: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("stat open lock file: %w", err)
		}
		return nil, fmt.Errorf("unsafe lock file target: %s", path)
	}
	return f, nil
}

func abortAcquire(path string) {
	registry.Lock()
	defer registry.Unlock()
	entry := registry.locks[path]
	if entry == nil {
		return
	}
	entry.held = false
	entry.owner = 0
	entry.depth = 0
	entry.file = nil
	if entry.waiters > 0 {
		entry.cond.Signal()
	} else {
		delete(registry.locks, path)
	}
}

func releaseFunc(path string) func() {
	var once sync.Once
	return func() {
		once.Do(func() { release(path) })
	}
}

func release(path string) {
	registry.Lock()
	defer registry.Unlock()
	entry := registry.locks[path]
	if entry == nil {
		return
	}
	entry.depth--
	if entry.depth > 0 {
		return
	}
	f := entry.file
	entry.file = nil
	entry.held = false
	entry.owner = 0
	if f != nil {
		_ = unlockFile(f)
		_ = f.Close()
	}
	if entry.waiters > 0 {
		entry.cond.Signal()
	} else {
		delete(registry.locks, path)
	}
}

// curGoroutineID extracts the numeric goroutine ID from runtime.Stack's
// header ("goroutine <id> [...]"). It allocates nothing on the heap.
func curGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	const prefix = "goroutine "
	s := buf[:n]
	if len(s) <= len(prefix) || string(s[:len(prefix)]) != prefix {
		return 0
	}
	s = s[len(prefix):]
	var id uint64
	var digits int
	for _, b := range s {
		if b < '0' || b > '9' {
			break
		}
		id = id*10 + uint64(b-'0')
		digits++
	}
	if digits == 0 {
		return 0
	}
	return id
}
