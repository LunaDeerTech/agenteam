package approval

import (
	"context"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"reflect"
)

type Store interface {
	postgres.SQLExecutor
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func fault(c f.Code) error { return f.NewFault(c, f.NotStarted) }
func portError(err error) error {
	var v *f.Fault
	if err == nil || errors.As(err, &v) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fault(f.DependencyUnavailable)
}

type commitFailure struct{ result f.CommitResult }

func (commitFailure) Error() string { return "approval_commit_outcome" }
func UnknownAttempt(err error) (f.CommitResult, bool) {
	var c commitFailure
	if errors.As(err, &c) {
		return c.result, true
	}
	return f.CommitResult{}, false
}
func commitError(r f.CommitResult) error {
	switch r.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		if r.Fault() != nil {
			return r.Fault()
		}
		return f.NewFault(f.InternalError, f.NotCommitted)
	default:
		v := f.NewFault(f.CommitUnknown, f.Unknown)
		v.RetryHint = "lookup"
		if r.AttemptID().Validate() == nil {
			v.CauseID = r.AttemptID().String()
		}
		return v.WithCause(commitFailure{r})
	}
}
