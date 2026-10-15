package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// This controlled owner checks the App context and shutdown handoff. It does
// not manufacture a ProcessGuard, a durable MCP receipt or recovery authority.
type mcpStartupTestOwner struct {
	mu      sync.Mutex
	stopped bool
	calls   []*mcpStartupTestCall
	release chan struct{}
	once    sync.Once
}

type mcpStartupTestCall struct {
	ctx       context.Context
	cancel    context.CancelFunc
	cancelled chan struct{}
	returned  chan struct{}
}

func newMCPStartupTestOwner(t *testing.T) *mcpStartupTestOwner {
	t.Helper()
	o := &mcpStartupTestOwner{release: make(chan struct{})}
	t.Cleanup(func() {
		o.StopAdmission()
		o.once.Do(func() { close(o.release) })
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := o.Drain(ctx); err != nil {
			t.Error("original controlled calls did not return", err)
		}
	})
	return o
}

func (o *mcpStartupTestOwner) start(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.stopped {
		return errors.New("controlled owner stopped")
	}
	child, cancel := context.WithCancel(ctx)
	c := &mcpStartupTestCall{child, cancel, make(chan struct{}), make(chan struct{})}
	o.calls = append(o.calls, c)
	go func() {
		<-child.Done()
		close(c.cancelled)
		<-o.release
		close(c.returned)
	}()
	return nil
}

func (o *mcpStartupTestOwner) StopAdmission() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stopped = true
	for _, c := range o.calls {
		c.cancel()
	}
}

func (o *mcpStartupTestOwner) Drain(ctx context.Context) error {
	o.StopAdmission()
	o.mu.Lock()
	calls := append([]*mcpStartupTestCall(nil), o.calls...)
	o.mu.Unlock()
	for _, c := range calls {
		select {
		case <-c.returned:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (o *mcpStartupTestOwner) Force(ctx context.Context) error { return o.Drain(ctx) }

func (o *mcpStartupTestOwner) Joined() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.stopped {
		return false
	}
	for _, c := range o.calls {
		select {
		case <-c.returned:
		default:
			return false
		}
	}
	return true
}

func requireMCPStartupOwnedJoin(t *testing.T, owner *mcpStartupTestOwner) {
	t.Helper()
	// Use the original App shutdown traversal, including partial construction.
	assembly := &accountAssembly{sink: owner}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- assembly.Drain(ctx) }()
	for _, call := range owner.calls {
		select {
		case <-call.cancelled:
		case <-ctx.Done():
			t.Fatal("App stop did not cancel the original admitted call")
		}
	}
	if assembly.Joined() {
		t.Fatal("App joined before original worker return")
	}
	select {
	case err := <-done:
		t.Fatal("App drain skipped the original return barrier", err)
	default:
	}
	owner.once.Do(func() { close(owner.release) })
	select {
	case err := <-done:
		if err != nil || !assembly.Joined() {
			t.Fatal("original return did not release App join", err)
		}
	case <-ctx.Done():
		t.Fatal("App did not join returned workers")
	}
}

func TestMCPRecoverySurvivesStartupCancellationAndUsesOwnedJoin(t *testing.T) {
	owner := newMCPStartupTestOwner(t)
	type key struct{}
	startup, cancelStartup := context.WithTimeout(context.WithValue(context.Background(), key{}, "original-startup"), time.Second)
	defer cancelStartup()
	if err := startMCPRecovery(startup, owner.start, owner.start); err != nil {
		t.Fatal(err)
	}
	cancelStartup() // initialize returned; the root cancels its startup budget.
	if len(owner.calls) != 2 {
		t.Fatal("content and reconciliation did not each admit one original call")
	}
	for _, call := range owner.calls {
		if call.ctx.Err() != nil || call.ctx.Value(key{}) != "original-startup" {
			t.Fatal("startup completion cancelled recovery or lost its context values")
		}
		if _, hasDeadline := call.ctx.Deadline(); hasDeadline {
			t.Fatal("initialization deadline still owns the recovery call")
		}
	}
	requireMCPStartupOwnedJoin(t, owner)
}

func TestMCPRecoveryPartialStartupFailureKeepsOriginalOwner(t *testing.T) {
	for _, mode := range []string{"reconciliation-failed", "startup-cancelled-after-content"} {
		t.Run(mode, func(t *testing.T) {
			owner := newMCPStartupTestOwner(t)
			startup, cancelStartup := context.WithCancel(context.Background())
			defer cancelStartup()
			want := errors.New("reconciliation unavailable")
			reconciled := false
			content := func(ctx context.Context) error {
				if err := owner.start(ctx); err != nil {
					return err
				}
				if mode == "startup-cancelled-after-content" {
					cancelStartup()
				}
				return nil
			}
			err := startMCPRecovery(startup, content, func(context.Context) error { reconciled = true; return want })
			if mode == "reconciliation-failed" {
				if !errors.Is(err, want) || !reconciled {
					t.Fatal("startup failure was lost", err)
				}
			} else if !errors.Is(err, context.Canceled) || reconciled {
				t.Fatal("cancelled startup admitted another worker", err)
			}
			cancelStartup()
			if len(owner.calls) != 1 || owner.calls[0].ctx.Err() != nil {
				t.Fatal("partial startup lost its original admitted call")
			}
			requireMCPStartupOwnedJoin(t, owner)
		})
	}
}

func TestMCPRecoveryRejectsCancelledStartupBeforeAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	start := func(context.Context) error { called = true; return nil }
	if err := startMCPRecovery(ctx, start, start); !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled initialization started recovery", err)
	}
}
