package account

import (
	"errors"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func fault(code foundation.Code, cause error) error {
	state := foundation.NotStarted
	if code == foundation.CommitUnknown {
		state = foundation.Unknown
	}
	f := foundation.NewFault(code, state)
	if cause != nil {
		return f.WithCause(cause)
	}
	return f
}
func invalid() error              { return fault(foundation.InvalidArgument, nil) }
func unavailable(err error) error { return fault(foundation.DependencyUnavailable, err) }
func field(path, code string) error {
	f := foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	f.FieldErrors = []foundation.FieldError{{Path: path, Code: code}}
	return f
}
func resultError(r foundation.CommitResult) error {
	if r.State() == foundation.Committed {
		return nil
	}
	if r.State() == foundation.Unknown {
		return confirmationError(r, nil)
	}
	if f := r.Fault(); f != nil {
		return f
	}
	return unavailable(nil)
}

// A failed confirmation cannot decide the original writer's outcome. Call only
// when confirmation has not established success; confirmed business failures
// from an already committed operation retain their existing classification.
func confirmationError(original foundation.CommitResult, confirmation error) error {
	if original.State() != foundation.Unknown {
		return confirmation
	}
	var originalCause error
	if f := original.Fault(); f != nil {
		originalCause = f
	}
	return fault(foundation.CommitUnknown, errors.Join(originalCause, confirmation))
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Map, reflect.Interface, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}
func portError(err error) error {
	if err == nil {
		return nil
	}
	var f *foundation.Fault
	if errors.As(err, &f) && f != nil {
		return f
	}
	return unavailable(err)
}
