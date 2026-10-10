// Package scheduler owns durable dispatch intent and its caller-transaction
// proofs. It never reads or writes another domain's canonical SQL tables.
package scheduler

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
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func fault(code f.Code) *f.Fault { return f.NewFault(code, f.NotStarted) }
func invalid() error             { return fault(f.InvalidArgument) }

type storageFailure struct{ cause error }

func (storageFailure) Error() string              { return "scheduler_storage" }
func (storageFailure) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "scheduler_storage") }
func (storageFailure) LogValue() slog.Value       { return slog.StringValue("scheduler_storage") }
func (e storageFailure) Unwrap() error            { return e.cause }
func unavailable(err error) error {
	if err == nil {
		return fault(f.DependencyUnavailable)
	}
	return fault(f.DependencyUnavailable).WithCause(storageFailure{err})
}
func portError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var known *f.Fault
	if errors.As(err, &known) {
		return err
	}
	return unavailable(err)
}

type commitFailure struct{ result f.CommitResult }

func (commitFailure) Error() string { return "scheduler_commit_outcome" }
func (commitFailure) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "scheduler_commit_outcome")
}
func (commitFailure) LogValue() slog.Value { return slog.StringValue("scheduler_commit_outcome") }

// UnknownAttempt preserves the original physical attempt. The owning caller
// looks up its original Dispatch ID; it cannot manufacture another claim/key.
func UnknownAttempt(err error) (f.CommitResult, bool) {
	var original commitFailure
	if errors.As(err, &original) {
		return original.result, true
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
		err.RetryHint = "lookup"
		if result.AttemptID().Validate() == nil {
			err.CauseID = result.AttemptID().String()
		}
		return err.WithCause(commitFailure{result})
	}
}
