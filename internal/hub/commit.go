package hub

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/realchendahuang/MarkToast/internal/gitx"
)

var repoCommitLocks sync.Map // cleanRoot (string) -> *sync.Mutex

func repoCommitMutex(root string) *sync.Mutex {
	key := filepath.Clean(root)
	if abs, err := filepath.Abs(key); err == nil {
		key = abs
	}
	if v, ok := repoCommitLocks.Load(key); ok {
		return v.(*sync.Mutex)
	}
	m := &sync.Mutex{}
	actual, _ := repoCommitLocks.LoadOrStore(key, m)
	return actual.(*sync.Mutex)
}

func commitRepoNow(root, message string) error {
	if root == "" || message == "" {
		return nil
	}
	if !gitx.Available() {
		return nil
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		return nil
	}
	mu := repoCommitMutex(root)
	mu.Lock()
	defer mu.Unlock()

	_, err := gitx.CommitAll(root, message)
	if err != nil {
		log.Printf("marktoast: commit failed: %v", err)
	}
	return err
}

// committer batches Git commits during a quiet period. The Markdown files are
// written immediately; only the commit is delayed.
//
// runMu serialises git invocations and ensures Flush waits for any in-flight
// timer callback before returning during server shutdown.
type committer struct {
	mu      sync.Mutex
	runMu   sync.Mutex
	root    string
	delay   time.Duration
	timer   *time.Timer
	message string
	lastErr error
	closed  bool
}

func newCommitter(root string, delay time.Duration) *committer {
	if delay <= 0 {
		delay = 5 * time.Second
	}
	return &committer{root: root, delay: delay}
}

func (c *committer) Schedule(message string) {
	if c == nil || message == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.message = message
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(c.delay, func() {
		_ = c.drain(false)
	})
}

func (c *committer) drain(closing bool) error {
	if c == nil {
		return nil
	}
	if closing {
		c.mu.Lock()
		c.closed = true
		if c.timer != nil {
			c.timer.Stop()
			c.timer = nil
		}
		c.mu.Unlock()
	}

	c.runMu.Lock()
	defer c.runMu.Unlock()

	for {
		c.mu.Lock()
		if !closing {
			c.timer = nil
		}
		msg := c.message
		c.message = ""
		if msg == "" {
			err := c.lastErr
			c.mu.Unlock()
			return err
		}
		c.mu.Unlock()

		err := commitRepoNow(c.root, msg)

		c.mu.Lock()
		c.lastErr = err
		done := !closing || c.message == ""
		c.mu.Unlock()
		if done {
			return err
		}
	}
}

// Flush stops any pending debounce timer, waits for any in-flight commit to
// finish, and commits any remaining queued mutation immediately.
func (c *committer) Flush() error {
	return c.drain(true)
}

// commit writes a commit now, schedules it when batching is enabled, or skips
// it when automatic commits have been explicitly disabled.
func commit(o Options, message string) error {
	if o.Committer != nil {
		o.Committer.Schedule(message)
		return nil
	}
	if o.commitConfigured && !o.AutoCommit {
		return nil
	}
	return commitRepoNow(o.Root, message)
}

func commitWithWarning(o Options, message string) string {
	if err := commit(o, message); err != nil {
		return err.Error()
	}
	return ""
}
