package object

import (
	"errors"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func failure(code foundation.Code, cause error) error {
	state := foundation.NotStarted
	if code == foundation.CommitUnknown {
		state = foundation.Unknown
	}
	return foundation.NewFault(code, state).WithCause(cause)
}
func invalid() error                { return failure(foundation.InvalidArgument, nil) }
func unavailable(cause error) error { return failure(foundation.DependencyUnavailable, cause) }
func hasCode(err error, code foundation.Code) bool {
	var f *foundation.Fault
	return errors.As(err, &f) && f.Code == code
}
func portError(err error) error {
	var f *foundation.Fault
	if errors.As(err, &f) && f.Code.Known() {
		return foundation.NewFault(f.Code, f.CommitState).WithCause(err)
	}
	return unavailable(err)
}
func commitError(r foundation.CommitResult) error {
	if r.State() == foundation.Committed {
		return nil
	}
	if r.State() == foundation.Unknown {
		return failure(foundation.CommitUnknown, nil)
	}
	if f := r.Fault(); f != nil {
		return portError(f)
	}
	return unavailable(nil)
}

// commandCommitError separates the completed command's historical result from
// this invocation's read/check transaction. The DB correctly reports its own
// rollback as not_committed; it must not erase an authorized, locked observation
// that the original successful command was subsequently revoked. Unknown always
// remains unknown. Other errors keep the actual transaction outcome.
func commandCommitError(r foundation.CommitResult, domain error) error {
	if txFault := r.Fault(); r.State() == foundation.NotCommitted && txFault != nil && txFault.Code == foundation.ResourceDeleted {
		var f *foundation.Fault
		if errors.As(domain, &f) && f.Code == foundation.ResourceDeleted && f.CommitState == foundation.Committed {
			return deleted(true)
		}
	}
	return commitError(r)
}
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}
