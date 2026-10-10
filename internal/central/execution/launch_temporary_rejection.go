package execution

import (
	"context"
	"errors"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution/internal/launchtemporary"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// launchCommitError runs after the original final WithinTx has returned. The
// PostgreSQL adapter's lock failure poisons that transaction; its synchronous
// rollback/connection cleanup must finish before NotCommitted is returned.
// Callback errors alone are insufficient: poison may replace them, and an
// unknown physical outcome must retain the original attempt unchanged.
func launchCommitError(ctx context.Context, request c.LaunchRequest, acquireErr error, result f.CommitResult) error {
	err := commitError(result)
	if err == nil || result.State() != f.NotCommitted || acquireErr == nil || ctx == nil || ctx.Err() != nil {
		return err
	}
	original := result.Fault()
	if original == nil || original.Code == f.CommitUnknown ||
		errors.Is(acquireErr, context.Canceled) || errors.Is(acquireErr, context.DeadlineExceeded) ||
		errors.Is(original, context.Canceled) || errors.Is(original, context.DeadlineExceeded) {
		return err
	}
	var acquired, rejected *postgres.Error
	if !errors.As(acquireErr, &acquired) || acquired == nil || acquired.Code() != postgres.LockFailed || acquired.SQLState() != "55P03" ||
		!errors.As(original, &rejected) || rejected != acquired {
		return err
	}
	digest, digestErr := request.Digest()
	if digestErr != nil {
		return err
	}
	marker := launchtemporary.MintLockTimeout(digest, request.Meta.RequestID, request.Meta.IdempotencyKey, original)
	if marker == nil {
		return err
	}
	// Preserve the original safe code/state and the exact diagnostic chain. No
	// RetryHint is added: policy and dispatch-attempt eligibility belong to the
	// caller, independently of this one known-not-created classification.
	return original.WithCause(marker)
}
