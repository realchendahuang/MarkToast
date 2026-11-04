package hub

import (
	"log"
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
	c.timer = time.AfterFunc(c.delay, c.flush)
}

func (c *committer) flush() {
	c.mu.Lock()
	message := c.message
	c.timer = nil
	c.message = ""
	c.mu.Unlock()
	if message == "" {
		return
	}
	if _, err := gitx.CommitAll(c.root, message); err != nil {
		log.Printf("marktoast: debounced commit failed: %v", err)
	}
}

// Flush commits any pending mutation immediately.
func (c *committer) Flush() {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
	c.flush()
}

// commit writes a commit now, or schedules it when batching is enabled.
func commit(o Options, message string) {
	if o.Committer != nil {
		o.Committer.Schedule(message)
		return
	}
	if !gitx.Available() {
		return
	}
	if _, err := gitx.CommitAll(o.Root, message); err != nil {
		log.Printf("marktoast: commit failed: %v", err)
	}
}
