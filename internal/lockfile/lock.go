// Package lockfile provides a per-repository advisory write lock.
//
// The lock is re-entrant within one process so higher-level operations can
// hold it across several store/index/git steps, while separate MarkToast
// processes still serialise against each other through the OS file lock.
package lockfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type heldLock struct {
	file *os.File
	refs int
}

var registry = struct {
	sync.Mutex
	locks map[string]*heldLock
}{locks: map[string]*heldLock{}}

// Acquire locks <root>/.marktoast/lock until the returned function is called.
// It is safe to nest Acquire calls for the same root in one process.
func Acquire(root string) (func(), error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(abs, ".marktoast", "lock")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}

	registry.Lock()
	if existing := registry.locks[path]; existing != nil {
		existing.refs++
		registry.Unlock()
		return releaseFunc(path), nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		registry.Unlock()
		return nil, fmt.Errorf("open lock file: %w", err)
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		registry.Unlock()
		return nil, fmt.Errorf("lock repository: %w", err)
	}
	registry.locks[path] = &heldLock{file: f, refs: 1}
	registry.Unlock()
	return releaseFunc(path), nil
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
	entry.refs--
	if entry.refs > 0 {
		return
	}
	delete(registry.locks, path)
	_ = unlockFile(entry.file)
	_ = entry.file.Close()
}
