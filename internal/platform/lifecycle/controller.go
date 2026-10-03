// Package lifecycle provides process stop coordination without business models.
package lifecycle

import (
	"context"
	"os"
	"sync"
	"time"
)

// Controller separates a stop request from serving cancellation and maintains
// one total shutdown budget. Repeated Stop calls do not escalate or reset it.
type Controller struct {
	stop        context.Context
	cancelStop  context.CancelFunc
	force       context.Context
	cancelForce context.CancelFunc
	stopOnce    sync.Once
	forceOnce   sync.Once
	closeOnce   sync.Once
	closed      chan struct{}
	watcherDone chan struct{}
	timeout     time.Duration
	deadline    time.Time
}

func New(ctx context.Context, signals <-chan os.Signal, timeout time.Duration) *Controller {
	stop, cancelStop := context.WithCancel(context.WithoutCancel(ctx))
	force, cancelForce := context.WithCancel(context.Background())
	c := &Controller{stop: stop, cancelStop: cancelStop, force: force, cancelForce: cancelForce, closed: make(chan struct{}), watcherDone: make(chan struct{}), timeout: timeout}
	if ctx.Err() != nil {
		c.Stop()
	}
	go c.watch(ctx, signals)
	return c
}

func (c *Controller) watch(ctx context.Context, signals <-chan os.Signal) {
	defer close(c.watcherDone)
	parentDone := ctx.Done()
	signalled := false
	for {
		select {
		case <-c.closed:
			return
		case <-parentDone:
			c.Stop()
			parentDone = nil
		case _, ok := <-signals:
			if !ok {
				signals = nil
				continue
			}
			if signalled {
				c.Force()
			} else {
				signalled = true
				c.Stop()
			}
		}
	}
}

func (c *Controller) Stop() {
	c.stopOnce.Do(func() { c.deadline = time.Now().Add(c.timeout); c.cancelStop() })
}
func (c *Controller) Force()                       { c.Stop(); c.forceOnce.Do(c.cancelForce) }
func (c *Controller) Stopping() bool               { return c.stop.Err() != nil }
func (c *Controller) Forced() bool                 { return c.force.Err() != nil }
func (c *Controller) StopContext() context.Context { return c.stop }
func (c *Controller) ForceDone() <-chan struct{}   { return c.force.Done() }

// DrainContext is independent of StopContext and shares the first stop deadline.
func (c *Controller) DrainContext() (context.Context, context.CancelFunc) {
	c.Stop() // sync.Once also establishes visibility of the fixed deadline.
	return context.WithDeadline(context.Background(), c.deadline)
}

// Close joins the signal/cancellation watcher. No caller closes a signal channel.
func (c *Controller) Close() { c.closeOnce.Do(func() { close(c.closed) }); <-c.watcherDone }
