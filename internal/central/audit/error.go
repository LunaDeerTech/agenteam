package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Error implicitly projects only a fixed code/reason. The retained raw cause
// is accessible exclusively through explicit errors.Unwrap/Is/As.
type Error struct {
	code   foundation.Code
	reason string
	cause  func() error
}

func failure(code foundation.Code, reason string, cause error) *Error {
	state := foundation.NotStarted
	if code == foundation.CommitUnknown {
		state = foundation.Unknown
	}
	fault := foundation.NewFault(code, state)
	if cause != nil {
		fault = fault.WithCause(cause)
	}
	return &Error{code: code, reason: reason, cause: func() error { return fault }}
}
func (e *Error) Error() string {
	if e == nil {
		return string(foundation.InternalError)
	}
	return string(e.code.Safe())
}
func (e *Error) Code() foundation.Code {
	if e == nil {
		return foundation.InternalError
	}
	return e.code.Safe()
}
func (e *Error) Reason() string {
	if e == nil {
		return "audit_failed"
	}
	return e.reason
}
func (e *Error) Unwrap() error {
	if e == nil || e.cause == nil {
		return nil
	}
	return e.cause()
}
func (e Error) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, e.Error()) }
func (e Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Code   foundation.Code `json:"code"`
		Reason string          `json:"reason"`
	}{e.Code(), e.Reason()})
}
func (e Error) LogValue() slog.Value {
	return slog.GroupValue(slog.String("code", e.Error()), slog.String("reason", e.Reason()))
}
func portError(err error) error {
	if err == nil {
		return nil
	}
	var f *foundation.Fault
	if errors.As(err, &f) {
		switch f.Code {
		case foundation.Unauthenticated, foundation.SessionRevoked, foundation.Forbidden, foundation.NotFound, foundation.InvalidState, foundation.ProjectNotActive, foundation.DependencyUnbound:
			return failure(f.Code, "authorization_rejected", err)
		}
	}
	return failure(foundation.DependencyUnavailable, "authorization_unavailable", err)
}
func unavailable(err error) error {
	return failure(foundation.DependencyUnavailable, "audit_unavailable", err)
}
func invalid(reason string) error { return failure(foundation.InvalidArgument, reason, nil) }
