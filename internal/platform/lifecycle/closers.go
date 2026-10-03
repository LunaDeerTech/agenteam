package lifecycle

import (
	"io"
	"sync"
)

// Closers owns acquired process resources. Register only closers whose Close
// releases resources promptly; stopping cooperative work is a separate concern.
// Late registration closes immediately, so acquisition racing with stop cannot
// orphan an already acquired listener or connection.
type Closers struct {
	mu        sync.Mutex
	closed    bool
	resources []io.Closer
	done      chan struct{}
}

func (c *Closers) Add(resource io.Closer) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = resource.Close()
		return
	}
	c.resources = append(c.resources, resource)
	c.mu.Unlock()
}

func (c *Closers) Close() {
	c.mu.Lock()
	if c.closed {
		done := c.done
		c.mu.Unlock()
		<-done
		return
	}
	c.closed = true
	c.done = make(chan struct{})
	resources := c.resources
	c.resources = nil
	c.mu.Unlock()
	defer close(c.done)
	for i := len(resources) - 1; i >= 0; i-- {
		_ = resources[i].Close()
	}
}
