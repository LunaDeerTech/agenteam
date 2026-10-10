package scheduler

import (
	"context"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// This is only called inside recordRejected after the original synchronous
// Launch's request-bound temporary proof and current attempt CAS were checked.
// No caller-supplied timestamp, code, or retryable flag reaches this writer.
func recordTemporary(r *dispatchRecord) error {
	r.temporaryAttempt, r.temporaryReason, r.temporaryCode = r.attempts, ec.LaunchTemporaryLockTimeout, f.InternalError
	r.temporaryOccurredAt = cloneInstant(&r.updatedAt)
	r.nextRetry = nil
	if r.retryPolicy == (LaunchRetryPolicy{}) {
		return nil
	}
	if r.attempts > r.retryPolicy.MaxAttempts() {
		return unavailable(nil)
	}
	delay, remaining, err := r.retryPolicy.NextDelay(r.attempts)
	if err != nil {
		return err
	}
	if remaining {
		at, err := retryDeadline(*r.temporaryOccurredAt, delay)
		if err != nil {
			return err
		}
		r.nextRetry = &at
	} else {
		r.finalAttempt, r.failureReason, r.failureCode = r.attempts, wc.TaskLaunchFailureRetryExhausted, r.temporaryCode
		r.failureOccurredAt = cloneInstant(r.temporaryOccurredAt)
	}
	return nil
}

func currentTemporary(r *dispatchRecord) bool {
	return r != nil && r.temporaryAttempt > 0 && r.temporaryAttempt == r.attempts &&
		r.temporaryReason == ec.LaunchTemporaryLockTimeout && r.temporaryCode == f.InternalError &&
		r.temporaryOccurredAt != nil && r.temporaryOccurredAt.Validate() == nil &&
		!r.temporaryOccurredAt.Time().Before(r.createdAt.Time()) && !r.temporaryOccurredAt.Time().After(r.updatedAt.Time()) &&
		r.outcome == KnownNotCreated && r.execution == nil && r.busyAttempt == 0
}

func retryableTemporary(r *dispatchRecord) bool {
	if !currentTemporary(r) || r.status != Pending || r.finalAttempt != 0 || r.retryPolicy.Validate() != nil || r.nextRetry == nil || r.attempts >= r.retryPolicy.MaxAttempts() {
		return false
	}
	delay, remaining, err := r.retryPolicy.NextDelay(r.attempts)
	if err != nil || !remaining {
		return false
	}
	at, err := retryDeadline(*r.temporaryOccurredAt, delay)
	return err == nil && at.Time().Equal(r.nextRetry.Time())
}

func exhaustedTemporary(r *dispatchRecord) bool {
	return currentTemporary(r) && r.retryPolicy.Validate() == nil && r.attempts == r.retryPolicy.MaxAttempts() &&
		r.nextRetry == nil && r.failureReason == wc.TaskLaunchFailureRetryExhausted && r.failureCode == r.temporaryCode &&
		r.failureOccurredAt != nil && r.failureOccurredAt.Time().Equal(r.temporaryOccurredAt.Time())
}

func validateRetryRecord(r *dispatchRecord) error {
	if r.retryPolicy != (LaunchRetryPolicy{}) && (r.retryPolicy.Validate() != nil || r.attempts > r.retryPolicy.MaxAttempts()) {
		return unavailable(nil)
	}
	if r.temporaryAttempt == 0 {
		if r.temporaryReason != "" || r.temporaryCode != "" || r.temporaryOccurredAt != nil {
			return unavailable(nil)
		}
		return nil // Legacy next_retry_at never supplies typed authority.
	}
	if r.temporaryAttempt < 0 || r.temporaryAttempt > r.attempts || r.attempts-r.temporaryAttempt > 1 ||
		r.temporaryReason != ec.LaunchTemporaryLockTimeout || r.temporaryCode != f.InternalError || r.temporaryOccurredAt == nil ||
		r.temporaryOccurredAt.Validate() != nil || r.temporaryOccurredAt.Time().Before(r.createdAt.Time()) || r.temporaryOccurredAt.Time().After(r.updatedAt.Time()) {
		return unavailable(nil)
	}
	if r.temporaryAttempt < r.attempts {
		if r.nextRetry != nil || r.failureReason == wc.TaskLaunchFailureRetryExhausted {
			return unavailable(nil)
		}
		return nil // Previous attempt's last_error is diagnostic only.
	}
	if !currentTemporary(r) {
		return unavailable(nil)
	}
	if r.retryPolicy == (LaunchRetryPolicy{}) {
		if r.status != Pending || r.nextRetry != nil || r.finalAttempt != 0 {
			return unavailable(nil)
		}
		return nil
	}
	if r.attempts < r.retryPolicy.MaxAttempts() {
		if !retryableTemporary(r) {
			return unavailable(nil)
		}
	} else if !exhaustedTemporary(r) || r.finalAttempt != r.attempts || (r.status != Pending && r.status != Failed) {
		return unavailable(nil)
	}
	return nil
}

// The explicit dependency is mandatory for RetryDue; a visitor context cannot
// substitute for it. Both callers already hold ProjectSH and ScheduleEX in the
// original transaction. Historical association rechecks pause, not Sprint.
func (s *LaunchHandoff) retryProjectInTx(ctx context.Context, tx f.Tx, r *dispatchRecord, sending bool) (bool, error) {
	if nilPort(s.retryProjects) {
		return false, fault(f.DependencyUnbound)
	}
	if _, err := s.authority.inTx(ctx, tx, r.project); err != nil {
		return false, err
	}
	current, err := s.retryProjects.RequireSchedulerProjectInTx(ctx, tx, r.project)
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if err != nil {
		return false, portError(err)
	}
	if current.Project.ID != r.project || current.Config.Validate() != nil {
		return false, unavailable(nil)
	}
	return current.Config.Enabled && (!sending || current.Project.CurrentSprintID != nil && current.Project.CurrentSprintID.String() == r.sprint), nil
}
