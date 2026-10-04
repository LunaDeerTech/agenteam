package artifact

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
func invalid() error              { return failure(foundation.InvalidArgument, nil) }
func unavailable(err error) error { return failure(foundation.DependencyUnavailable, err) }
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
