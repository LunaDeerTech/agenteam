package mcp

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// These tests exercise only the lifecycle barrier. They do not manufacture a
// bound ProcessGuard, a durable request or permission to recover a request.
func TestMCPProcessOwnerWaitsForOriginalReturnBeforeObservation(t *testing.T) {
	o := &processOwner{changed: make(chan struct{}), calls: make(map[*processCall]struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	call := &processCall{owner: o, cancel: cancel}
	o.calls[call] = struct{}{}
	var observed atomic.Bool
	done := make(chan error, 1)
	go func() { done <- o.join(context.Background()) }()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("stop did not cancel original call")
	}
	select {
	case <-done:
		t.Fatal("drain returned before original call")
	default:
	}
	call.returnedWith(func(context.Context) (bool, error) { observed.Store(true); return true, nil })
	select {
	case err := <-done:
		if err != nil || !observed.Load() || !o.joined() {
			t.Fatal("original observation did not join", err)
		}
	case <-time.After(time.Second):
		t.Fatal("drain did not join")
	}
}

func TestMCPProcessOwnerRetainsUnconfirmedOriginal(t *testing.T) {
	o := &processOwner{changed: make(chan struct{}), calls: make(map[*processCall]struct{})}
	_, cancel := context.WithCancel(context.Background())
	call := &processCall{owner: o, cancel: cancel}
	o.calls[call] = struct{}{}
	var confirmed atomic.Bool
	call.returnedWith(func(context.Context) (bool, error) { return confirmed.Load(), nil })
	if err := o.join(context.Background()); err == nil || o.joined() {
		t.Fatal("unconfirmed original was retired")
	}
	confirmed.Store(true)
	if err := o.join(context.Background()); err != nil || !o.joined() {
		t.Fatal("same original observer failed", err)
	}
}

func TestMCPProcessOwnerCancelledDrainDoesNotInventReturn(t *testing.T) {
	o := &processOwner{changed: make(chan struct{}), calls: make(map[*processCall]struct{})}
	_, cancel := context.WithCancel(context.Background())
	call := &processCall{owner: o, cancel: cancel}
	o.calls[call] = struct{}{}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if err := o.join(ctx); !errors.Is(err, context.Canceled) || o.joined() {
		t.Fatal("expired drain discarded original call", err)
	}
	call.returnedWith(nil)
	if !o.joined() {
		t.Fatal("actual original return did not join")
	}
	if got, err := newProcessOwner(nil, ProcessOptions{}); got != nil || err == nil {
		t.Fatal("unbound process accepted")
	}
}
