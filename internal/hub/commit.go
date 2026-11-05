package hub

import (
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/realchendahuang/MarkToast/internal/gitx"
)

// committer batches Git commits during a quiet period. The Markdown files are
// written immediately; only the commit is delayed.
type committer struct {
	mu      sync.Mutex
	root    string
	delay   time.Duration
	timer   *time.Timer
	message string
	lastErr error
}

func newCommitter(root string, delay time.Duration) *committer {
	if delay <= 0 {
		delay = 5 * time.Second
	}
	return &committer{root: root, delay: delay}
}

func (c *committer) Schedule(message string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.message = message
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(c.delay, func() {
		if err := c.flush(); err != nil {
			log.Printf("marktoast: debounced commit failed: %v", err)
		}
	})
}

func (c *committer) flush() error {
	c.mu.Lock()
	message := c.message
	c.timer = nil
	c.message = ""
	c.mu.Unlock()
	if message == "" {
		return c.lastErr
	}
	_, err := gitx.CommitAll(c.root, message)
	c.mu.Lock()
	c.lastErr = err
	c.mu.Unlock()
	return err
}

// Flush commits any pending mutation immediately and returns its error.
func (c *committer) Flush() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	return c.flush()
}

// commit writes a commit now, or schedules it when batching is enabled.
func commit(o Options, message string) error {
	if o.Committer != nil {
		o.Committer.Schedule(message)
		return nil
	}
	if !gitx.Available() {
		return nil
	}
	if _, err := os.Stat(filepath.Join(o.Root, ".git")); err != nil {
		return nil
	}
	_, err := gitx.CommitAll(o.Root, message)
	if err != nil {
		log.Printf("marktoast: commit failed: %v", err)
	}
	return err
}
