package account

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// mailAdmission is shared by all delivery ports and invalidating mutations of
// one Authority. It precedes every database transaction that uses its guard.
type mailAdmission struct {
	mu      sync.Mutex
	readers int
	writer  bool
	queue   []*mailWaiter
}
type mailWaiter struct {
	exclusive, granted bool
	ready              chan struct{}
}
type mailGuard struct {
	gate      *mailAdmission
	exclusive bool
	once      sync.Once
	released  atomic.Bool
}

func (g *mailAdmission) acquire(ctx context.Context, exclusive bool) (*mailGuard, error) {
	if err := ctx.Err(); err != nil {
		return nil, unavailable(err)
	}
	w := &mailWaiter{exclusive: exclusive, ready: make(chan struct{})}
	g.mu.Lock()
	g.queue = append(g.queue, w)
	g.advance()
	g.mu.Unlock()
	select {
	case <-w.ready:
		guard := &mailGuard{gate: g, exclusive: exclusive}
		if err := ctx.Err(); err != nil {
			guard.release()
			return nil, unavailable(err)
		}
		return guard, nil
	case <-ctx.Done():
		g.mu.Lock()
		if w.granted {
			if exclusive {
				g.writer = false
			} else {
				g.readers--
			}
		} else {
			for i, p := range g.queue {
				if p == w {
					g.queue = append(g.queue[:i], g.queue[i+1:]...)
					break
				}
			}
		}
		g.advance()
		g.mu.Unlock()
		return nil, unavailable(ctx.Err())
	}
}
func (g *mailAdmission) advance() {
	if g.writer {
		return
	}
	for len(g.queue) > 0 {
		w := g.queue[0]
		if w.exclusive && g.readers != 0 {
			return
		}
		g.queue = g.queue[1:]
		w.granted = true
		if w.exclusive {
			g.writer = true
		} else {
			g.readers++
		}
		close(w.ready)
		if w.exclusive {
			return
		}
	}
}
func (g *mailGuard) release() {
	if g == nil {
		return
	}
	g.once.Do(func() {
		g.released.Store(true)
		g.gate.mu.Lock()
		if g.exclusive {
			g.gate.writer = false
		} else {
			g.gate.readers--
		}
		g.gate.advance()
		g.gate.mu.Unlock()
	})
}
func (a *Authority) mailExclusive(ctx context.Context) (*mailGuard, error) {
	return a.state().mail.acquire(ctx, true)
}
func (a *Authority) requireMailExclusive(g *mailGuard) error {
	if g == nil || g.gate != a.state().mail || !g.exclusive || g.released.Load() {
		return fault(foundation.Forbidden, nil)
	}
	return nil
}
