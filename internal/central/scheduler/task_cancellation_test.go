package scheduler

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"testing"
)

func TestSchedulerTaskCancellationPreventsLaunchAndRetry(t *testing.T) {
	t.Run("initial", func(t *testing.T) {
		s, store, e := newHandoffTest(t)
		store.row.cancelRequested = true
		key, digest := store.row.launch.Meta.IdempotencyKey, store.row.digest
		out, err := s.LaunchOnce(context.Background(), store.row.project, store.row.id)
		if !runnerHasCode(err, f.InvalidState) || out.Summary().Status != Cancelled || e.launches != 0 || e.lookups != 0 || store.row.attempts != 0 || store.row.outcome != NotSent || store.row.launch.Meta.IdempotencyKey != key || store.row.digest != digest {
			t.Fatal("cancelled dispatch attempted launch or lost original identity", err)
		}
		if _, err = scanDispatch(dispatchTestRow{values: recordValues(t, store.row)}); err != nil {
			t.Fatal("cancelled row cannot be read", err)
		}
	})
	t.Run("retry", func(t *testing.T) {
		s, store, _, e := newRetryHandoffControl(t)
		store.row.cancelRequested = true
		deadline := *store.row.nextRetry
		out, err := s.RetryDue(context.Background(), store.row.project, store.row.id)
		if !runnerHasCode(err, f.InvalidState) || out.Summary().Status != Cancelled || e.launches != 0 || store.row.attempts != 1 || store.row.outcome != KnownNotCreated || *store.row.nextRetry != deadline {
			t.Fatal("cancellation retried or erased original diagnostic", err)
		}
		if _, err = scanDispatch(dispatchTestRow{values: recordValues(t, store.row)}); err != nil {
			t.Fatal("cancelled retry history cannot be read", err)
		}
	})
}

func TestSchedulerTaskCancellationUnknownUsesOriginalLookup(t *testing.T) {
	for _, found := range []bool{false, true} {
		t.Run(map[bool]string{false: "unobserved", true: "created"}[found], func(t *testing.T) {
			s, store, _, execution, work := newVisitTest(t)
			store.row.outcome, store.row.busyAttempt, store.row.cancelRequested = Unknown, 0, true
			id, key, attempt := store.row.id, store.row.launch.Meta.IdempotencyKey, store.row.attempts
			if found {
				execution.created = visitCreated(t, store.row)
			}
			out, err := s.VisitNext(context.Background(), store.row.project, nil)
			if out.Action != PendingVisitLookup || execution.launches != 0 || execution.lookups != 1 || work.apply != 0 || store.row.id != id || store.row.launch.Meta.IdempotencyKey != key || store.row.attempts != attempt {
				t.Fatal("cancelled Unknown left original Lookup lane", err)
			}
			if found {
				if err != nil || store.row.status != Launched || store.row.execution == nil || *store.row.execution != execution.created.ID {
					t.Fatal("original creation was discarded", err)
				}
			} else if err == nil || store.row.status != Pending || store.row.outcome != Unknown {
				t.Fatal("absence was promoted into known-not-created", err)
			}
		})
	}
}
func TestSchedulerTaskCancellationSkipsCompensation(t *testing.T) {
	s, store, _, execution, work := newVisitTest(t)
	store.row.cancelRequested = true
	out, err := s.VisitNext(context.Background(), store.row.project, nil)
	if err != nil || out.Action != PendingVisitCancel || store.row.status != Cancelled || work.apply != 0 || work.discover != 0 || execution.launches != 0 || execution.lookups != 0 || store.row.busyAttempt == 0 {
		t.Fatal("cancellation restored or dispatched terminal Task", err)
	}
	store.row.cancelRequested = false
	if _, err = scanDispatch(dispatchTestRow{values: recordValues(t, store.row)}); err == nil {
		t.Fatal("cancelled status accepted without request")
	}
}
