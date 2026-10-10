// Package mount owns logical Mount definitions and Agent configuration
// references. It does not create physical directories or infer Runner liveness.
package mount

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type Store interface {
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
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func fault(code f.Code) *f.Fault { return f.NewFault(code, f.NotStarted) }

// SQL diagnostics can contain logical names. Only cancellation remains public.
type storageFailure struct{ original error }

func (storageFailure) Error() string              { return "mount_storage" }
func (storageFailure) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "mount_storage") }
func (storageFailure) LogValue() slog.Value       { return slog.StringValue("mount_storage") }
func (e storageFailure) Is(target error) bool {
	return (target == context.Canceled || target == context.DeadlineExceeded) && errors.Is(e.original, target)
}
func portError(err error) error {
	var known *f.Fault
	if err == nil || errors.As(err, &known) {
		return err
	}
	return fault(f.DependencyUnavailable).WithCause(storageFailure{err})
}

type commitFailure struct{ result f.CommitResult }

func (commitFailure) Error() string              { return "mount_commit_outcome" }
func (commitFailure) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "mount_commit_outcome") }
func (commitFailure) LogValue() slog.Value       { return slog.StringValue("mount_commit_outcome") }

// UnknownAttempt exposes original physical provenance, not permission to retry.
func UnknownAttempt(err error) (f.CommitResult, bool) {
	var failure commitFailure
	if errors.As(err, &failure) {
		return failure.result, true
	}
	return f.CommitResult{}, false
}
func commitError(result f.CommitResult) error {
	switch result.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		if err := result.Fault(); err != nil {
			return err
		}
		return f.NewFault(f.InternalError, f.NotCommitted)
	default:
		err := f.NewFault(f.CommitUnknown, f.Unknown)
		if result.AttemptID().Validate() == nil {
			err.CauseID = result.AttemptID().String()
		}
		return err.WithCause(commitFailure{result})
	}
}
