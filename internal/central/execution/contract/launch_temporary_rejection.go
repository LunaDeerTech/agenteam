package contract

import (
	"context"
	"errors"

	"github.com/LunaDeerTech/agenteam/internal/central/execution/internal/launchtemporary"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type LaunchTemporaryReason string

const LaunchTemporaryLockTimeout LaunchTemporaryReason = "launch_lock_timeout_v1"

// MatchLaunchTemporaryRejection recognizes only Execution's private marker for
// the exact original request. A match classifies a known rolled-back Launch; it
// does not grant retry permission or identify a Scheduler dispatch attempt.
// Callers must retain their original synchronous call and persistent intent.
func MatchLaunchTemporaryRejection(err error, original LaunchRequest) (LaunchTemporaryReason, bool) {
	if err == nil || original.Validate() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "", false
	}
	var fault *f.Fault
	if !errors.As(err, &fault) || fault == nil || fault.CommitState != f.NotCommitted || fault.Code == f.CommitUnknown {
		return "", false
	}
	digest, e := original.Digest()
	if e != nil || !launchtemporary.Match(err, digest, original.Meta.RequestID, original.Meta.IdempotencyKey) {
		return "", false
	}
	return LaunchTemporaryLockTimeout, true
}
