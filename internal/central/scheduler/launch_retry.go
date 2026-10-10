package scheduler

import (
	"context"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// LaunchRetryState describes the most recent proven temporary rejection. It
// remains diagnostic when a newer attempt is unknown or has another outcome;
// callers cannot use this projection to issue an attempt or settle a failure.
type LaunchRetryState struct {
	Attempt     int64
	Reason      ec.LaunchTemporaryReason
	Code        f.Code
	OccurredAt  f.Instant
	NextRetryAt *f.Instant
}

// RetryState is present iff a typed temporary rejection was persisted. The
// timestamp value and optional deadline are copies. Presence alone does not
// imply a bound policy, a current-attempt rejection, or permission to retry.
func (d Dispatch) RetryState() (LaunchRetryState, bool) {
	if d.data == nil {
		return LaunchRetryState{}, false
	}
	r := d.data()
	if r.temporaryAttempt == 0 || r.temporaryOccurredAt == nil {
		return LaunchRetryState{}, false
	}
	return LaunchRetryState{Attempt: r.temporaryAttempt, Reason: r.temporaryReason, Code: r.temporaryCode, OccurredAt: *r.temporaryOccurredAt, NextRetryAt: cloneInstant(r.nextRetry)}, true
}

// NewLaunchHandoffWithRetry binds the actual Project owner for the new send
// gate and subsequent association. It creates the one handoff instance; there
// is no setter, replacement call set, or policy override of stored receipts.
func NewLaunchHandoffWithRetry(a *PendingAuthority, deps LaunchHandoffDependencies, projects ProjectSchedulerGate) (*LaunchHandoff, error) {
	if nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	s, err := NewLaunchHandoff(a, deps)
	if err != nil {
		return nil, err
	}
	s.retryProjects = projects
	return s, nil
}

// RetryDue performs at most one same-key send. No policy, a not-yet-due
// rejection, or a paused/current-Sprint mismatch returns the stored receipt
// with InvalidState and no write. Unknown ownership permits only Lookup.
func (s *LaunchHandoff) RetryDue(ctx context.Context, p i.ProjectID, id DispatchID) (Dispatch, error) {
	if s == nil || nilPort(s.retryProjects) {
		return Dispatch{}, fault(f.DependencyUnbound)
	}
	return s.launch(ctx, p, id, true)
}

// retryDeadline preserves nanosecond policy identity but rounds the resulting
// wall-clock deadline upward to PostgreSQL microseconds. UnixNano is avoided
// because otherwise valid Foundation instants can lie outside its int64 range.
func retryDeadline(observed f.Instant, delay time.Duration) (f.Instant, error) {
	if observed.Validate() != nil || observed.Time().IsZero() || delay <= 0 {
		return f.Instant{}, invalid()
	}
	at := observed.Time().Add(delay)
	if remainder := at.Nanosecond() % 1000; remainder != 0 {
		at = at.Add(time.Duration(1000-remainder) * time.Nanosecond)
	}
	if !at.After(observed.Time()) {
		return f.Instant{}, fault(f.InvalidState)
	}
	out, err := f.NewInstant(at)
	if err != nil {
		return f.Instant{}, fault(f.InvalidState)
	}
	return out, nil
}
