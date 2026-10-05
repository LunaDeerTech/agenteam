package object

import (
	"context"
	"errors"
	"testing"
	"time"
)

func cleanupBudgetService() *Service {
	state := &serviceState{cleanupRequests: map[*cleanupRequest]bool{}, changed: make(chan struct{})}
	return &Service{func() *serviceState { return state }}
}
func requireCleanupCancelled(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("actual cleanup context did not receive cancellation")
	}
}
func TestAvatarCleanupBudgetIntersectsCallerAndForce(t *testing.T) {
	for _, shorter := range []string{"caller", "force"} {
		for _, cancelled := range []string{"caller", "force"} {
			t.Run(shorter+"/"+cancelled, func(t *testing.T) {
				now := time.Now()
				callerAt, forceAt := now.Add(5*time.Second), now.Add(5*time.Second)
				if shorter == "caller" {
					callerAt = now.Add(time.Second)
				} else {
					forceAt = now.Add(time.Second)
				}
				parent, parentCancel := context.WithDeadline(context.Background(), callerAt)
				defer parentCancel()
				force, forceCancel := context.WithDeadline(context.Background(), forceAt)
				defer forceCancel()
				s := cleanupBudgetService()
				s.state().forceContext = force
				bounded, finish := s.cleanupIOContext(context.WithValue(parent, runtimeRecoveryBudgetKey{}, true))
				defer finish()
				deadline, ok := bounded.Deadline()
				if !ok || deadline.After(callerAt) || deadline.After(forceAt) {
					t.Fatal("cleanup widened caller or force deadline")
				}
				if cancelled == "caller" {
					parentCancel()
				} else {
					forceCancel()
				}
				requireCleanupCancelled(t, bounded)
			})
		}
	}
	// An already expired runtime budget cannot begin a fresh compensation window.
	s := cleanupBudgetService()
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	bounded, finish := s.cleanupIOContext(context.WithValue(expired, runtimeRecoveryBudgetKey{}, true))
	defer finish()
	if !errors.Is(bounded.Err(), context.DeadlineExceeded) {
		t.Fatal("expired caller budget refreshed", bounded.Err())
	}
	// The old ordinary B01 cancellation compensation deliberately has a fresh
	// context; only the explicit runtime/recovery route changes this behavior.
	ordinary, stop := s.cleanupIOContext(expired)
	defer stop()
	if ordinary.Err() != nil {
		t.Fatal("ordinary compensation changed", ordinary.Err())
	}
}
func TestAvatarCleanupCheckpointKeepsRegistrationUntilActualCompletion(t *testing.T) {
	s := cleanupBudgetService()
	parent, parentCancel := context.WithTimeout(context.Background(), time.Second)
	defer parentCancel()
	force, forceCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer forceCancel()
	s.state().forceContext = force
	before := s.state().changed
	bounded, finish := s.cleanupCheckpointWithinBudget(parent)
	if len(s.state().cleanupRequests) != 1 {
		t.Fatal("checkpoint not registered")
	}
	deadline, ok := bounded.Deadline()
	pdeadline, _ := parent.Deadline()
	if !ok || deadline.After(pdeadline) {
		t.Fatal("checkpoint extended caller budget")
	}
	parentCancel()
	requireCleanupCancelled(t, bounded)
	if len(s.state().cleanupRequests) != 1 {
		t.Fatal("caller cancellation falsely joined checkpoint")
	}
	select {
	case <-before:
		t.Fatal("cancellation announced completion")
	default:
	}
	finish()
	finish()
	if len(s.state().cleanupRequests) != 0 {
		t.Fatal("joined checkpoint was not retired")
	}
	select {
	case <-before:
	default:
		t.Fatal("checkpoint completion did not notify drain")
	}
	// A Force operation cancels the original registered request. The derived
	// caller-bounded SQL context must receive that cancellation as well.
	bounded, finish = s.cleanupCheckpointWithinBudget(context.Background())
	for request := range s.state().cleanupRequests {
		request.cancel()
	}
	requireCleanupCancelled(t, bounded)
	if len(s.state().cleanupRequests) != 1 {
		t.Fatal("force cancellation falsely joined checkpoint")
	}
	finish()
	s.state().drained = true
	bounded, finish = s.cleanupCheckpointWithinBudget(context.Background())
	defer finish()
	if bounded.Err() == nil || len(s.state().cleanupRequests) != 0 {
		t.Fatal("drained fence admitted a new checkpoint")
	}
}
