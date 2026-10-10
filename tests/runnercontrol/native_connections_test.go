//go:build integration

package runnercontrol_test

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This test-only tracker retires a physical connection only after its original
// Close returns. TLS may return ErrClosed from a concurrent second Close while
// the first is still running; HTTP Shutdown cannot join that call for us.
type runnerNativeConnections struct {
	mu      *sync.Mutex
	conns   map[*runnerNativeConn]struct{}
	changed chan struct{}
}

type runnerNativeConn struct {
	net.Conn
	owner    *runnerNativeConnections
	state    atomic.Int32
	inflight atomic.Int64
}

type runnerNativeConnectionState struct {
	state    http.ConnState
	inflight int64
}

func newRunnerNativeConnections() *runnerNativeConnections {
	return &runnerNativeConnections{mu: new(sync.Mutex), conns: make(map[*runnerNativeConn]struct{}), changed: make(chan struct{})}
}

func (v *runnerNativeConnections) track(conn net.Conn) *runnerNativeConn {
	owned := &runnerNativeConn{Conn: conn, owner: v}
	v.mu.Lock()
	v.conns[owned] = struct{}{}
	close(v.changed)
	v.changed = make(chan struct{})
	v.mu.Unlock()
	return owned
}

func (c *runnerNativeConn) Close() error {
	c.owner.mu.Lock()
	c.inflight.Add(1)
	c.owner.mu.Unlock()
	e := c.Conn.Close()
	c.owner.mu.Lock()
	if c.inflight.Add(-1) == 0 {
		delete(c.owner.conns, c)
		close(c.owner.changed)
		c.owner.changed = make(chan struct{})
	}
	c.owner.mu.Unlock()
	return e
}

func (v *runnerNativeConnections) observeState(conn net.Conn, state http.ConnState) {
	if secure, ok := conn.(*tls.Conn); ok {
		conn = secure.NetConn()
	}
	if owned, ok := conn.(*runnerNativeConn); ok && owned.owner == v {
		owned.state.Store(int32(state))
	}
}

// The caller must first join listener Serve, so no later Accept can add work.
// The exact existing Shutdown context bounds this wait; no timer is restarted.
func (v *runnerNativeConnections) wait(ctx context.Context) error {
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		v.mu.Lock()
		empty, changed := len(v.conns) == 0, v.changed
		v.mu.Unlock()
		if empty {
			return ctx.Err()
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (v *runnerNativeConnections) remainder() (connections []*runnerNativeConn, states []runnerNativeConnectionState) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for conn := range v.conns {
		connections = append(connections, conn)
		states = append(states, runnerNativeConnectionState{http.ConnState(conn.state.Load()), conn.inflight.Load()})
	}
	return connections, states
}

// No listener/socket/PG is used below. The real TLS implementation and tracker
// are exercised with one held original net.Conn.Close, never a replacement join.
type runnerHeldNativeClose struct {
	net.Conn
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *runnerHeldNativeClose) Close() error {
	c.once.Do(func() { close(c.entered) })
	<-c.release
	return nil
}

type runnerObservedCloseContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *runnerObservedCloseContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func TestRunnerControlNativeFixtureClose(t *testing.T) {
	for _, name := range []string{"original close actually returns", "original context cancelled", "original deadline expires"} {
		t.Run(name, func(t *testing.T) {
			owner := newRunnerNativeConnections()
			held := &runnerHeldNativeClose{entered: make(chan struct{}), release: make(chan struct{})}
			var release sync.Once
			firstDone := make(chan error, 1)
			owned := owner.track(held)
			secure := tls.Client(owned, &tls.Config{ServerName: "memory.invalid"})
			owner.observeState(secure, http.StateIdle)
			go func() { firstDone <- secure.Close() }()
			t.Cleanup(func() {
				release.Do(func() { close(held.release) })
				select {
				case e := <-firstDone:
					if e != nil {
						t.Error("original physical Close failed")
					}
				case <-time.After(time.Second):
					t.Error("original physical Close did not join")
				}
			})
			select {
			case <-held.entered:
			case <-time.After(time.Second):
				t.Fatal("original physical Close did not start")
			}
			if !errors.Is(secure.Close(), net.ErrClosed) {
				t.Fatal("second TLS Close did not return the original closed result")
			}
			left, states := owner.remainder()
			if len(left) != 1 || len(states) != 1 || states[0].state != http.StateIdle || states[0].inflight != 1 {
				t.Fatal("second TLS Close forged physical retirement or lost safe state")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if name != "original close actually returns" {
				want := context.Canceled
				if name == "original context cancelled" {
					cancel()
				} else {
					var stop context.CancelFunc
					ctx, stop = context.WithTimeout(ctx, 20*time.Millisecond)
					defer stop()
					want = context.DeadlineExceeded
				}
				if !errors.Is(owner.wait(ctx), want) {
					t.Fatal("original cancelled/expired context was replaced or accepted")
				}
				if left, _ := owner.remainder(); len(left) != 1 {
					t.Fatal("failed wait erased original physical owner")
				}
				return
			}
			waiting := make(chan error, 1)
			observed := &runnerObservedCloseContext{Context: ctx, waiting: make(chan struct{})}
			go func() { waiting <- owner.wait(observed) }()
			select {
			case <-waiting:
				t.Fatal("held original Close was reported joined")
			case <-observed.waiting:
			case <-ctx.Done():
				t.Fatal("physical Close wait did not begin")
			}
			release.Do(func() { close(held.release) })
			select {
			case e := <-waiting:
				if e != nil {
					t.Fatal("actual physical Close did not retire tracker")
				}
			case <-ctx.Done():
				t.Fatal("actual physical Close did not wake original wait")
			}
			if left, _ := owner.remainder(); len(left) != 0 {
				t.Fatal("actual physical Close left an owned connection")
			}
		})
	}
}

// Even an empty snapshot cannot report success after waiting for the tracker
// mutex used up the original deadline.
type runnerCheckedCloseContext struct {
	context.Context
	checked chan struct{}
	once    sync.Once
}

func (c *runnerCheckedCloseContext) Err() error {
	e := c.Context.Err()
	c.once.Do(func() { close(c.checked) })
	return e
}

func TestRunnerControlNativeFixtureCloseLateSnapshot(t *testing.T) {
	owner := newRunnerNativeConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	observed := &runnerCheckedCloseContext{Context: ctx, checked: make(chan struct{})}
	owner.mu.Lock()
	done := make(chan error, 1)
	go func() { done <- owner.wait(observed) }()
	<-observed.checked
	<-ctx.Done()
	owner.mu.Unlock()
	select {
	case e := <-done:
		if !errors.Is(e, context.DeadlineExceeded) {
			t.Fatal("late empty snapshot escaped original deadline")
		}
	case <-time.After(time.Second):
		t.Fatal("late snapshot wait did not actually return")
	}
}

// A repeated physical Close can also return early (the native FD close path
// does so). Every entered original Close must return before the map retires it.
type runnerConcurrentNativeClose struct {
	runnerHeldNativeClose
	calls atomic.Int64
}

func (c *runnerConcurrentNativeClose) Close() error {
	if c.calls.Add(1) > 1 {
		return net.ErrClosed
	}
	return c.runnerHeldNativeClose.Close()
}

func TestRunnerControlNativeFixtureConcurrentPhysicalClose(t *testing.T) {
	owner := newRunnerNativeConnections()
	held := &runnerConcurrentNativeClose{runnerHeldNativeClose: runnerHeldNativeClose{entered: make(chan struct{}), release: make(chan struct{})}}
	owned := owner.track(held)
	first := make(chan error, 1)
	var release sync.Once
	go func() { first <- owned.Close() }()
	t.Cleanup(func() {
		release.Do(func() { close(held.release) })
		select {
		case <-first:
		case <-time.After(time.Second):
			t.Error("first physical Close did not actually return")
		}
	})
	select {
	case <-held.entered:
	case <-time.After(time.Second):
		t.Fatal("first physical Close did not enter")
	}
	if !errors.Is(owned.Close(), net.ErrClosed) {
		t.Fatal("second physical Close result changed")
	}
	left, states := owner.remainder()
	if len(left) != 1 || len(states) != 1 || states[0].inflight != 1 {
		t.Fatal("second physical Close retired an earlier still-running call")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(owner.wait(ctx), context.Canceled) {
		t.Fatal("held physical Close forged joined")
	}
	release.Do(func() { close(held.release) })
	tail, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if owner.wait(tail) != nil {
		t.Fatal("original physical Close return did not retire owner")
	}
}
