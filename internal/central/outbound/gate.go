package outbound

import (
	"context"
	"sync"
)

// sendGate is process-wide for one PolicyService. Pending writers prevent new
// readers from barging, and both queues are cancellable before acquiring it.
// No goroutine waits on an uncancellable sync.RWMutex during shutdown.
type sendGate struct {
	mu                      sync.Mutex
	readers, writersWaiting int
	writer                  bool
	changed                 chan struct{}
}

func (g *sendGate) notifyLocked() {
	if g.changed != nil {
		close(g.changed)
	}
	g.changed = make(chan struct{})
}
func (g *sendGate) acquire(ctx context.Context, write bool) (func(), error) {
	g.mu.Lock()
	if g.changed == nil {
		g.changed = make(chan struct{})
	}
	if write {
		g.writersWaiting++
	}
	for {
		if err := ctx.Err(); err != nil {
			if write {
				g.writersWaiting--
				g.notifyLocked()
			}
			g.mu.Unlock()
			return nil, err
		}
		if !g.writer && (write && g.readers == 0 || !write && g.writersWaiting == 0) {
			if write {
				g.writersWaiting--
				g.writer = true
			} else {
				g.readers++
			}
			g.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					g.mu.Lock()
					if write {
						g.writer = false
					} else {
						g.readers--
					}
					g.notifyLocked()
					g.mu.Unlock()
				})
			}, nil
		}
		changed := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
		g.mu.Lock()
	}
}
