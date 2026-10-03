package outbound

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Error retains arbitrary causes only in a closure: even formatting an outer
// unexported field cannot reflect through it to a URL, key or driver message.
type Error struct {
	code   foundation.Code
	reason ac.Reason
	cause  func() error
}

func failure(code foundation.Code, reason ac.Reason, cause error) *Error {
	state := foundation.NotStarted
	if code == foundation.CommitUnknown {
		state = foundation.Unknown
	}
	f := foundation.NewFault(code, state)
	if cause != nil {
		f = f.WithCause(cause)
	}
	return &Error{code, reason, func() error { return f }}
}
func invalid() error { return failure(foundation.InvalidArgument, ac.InvalidTarget, nil) }
func unavailable(cause error) error {
	return failure(foundation.DependencyUnavailable, ac.PolicyUnavailable, cause)
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
func (e *Error) Reason() ac.Reason {
	if e == nil || !e.reason.Valid() {
		return ac.InternalError
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
		Reason ac.Reason       `json:"reason"`
	}{e.Code(), e.Reason()})
}
func (e Error) LogValue() slog.Value {
	return slog.GroupValue(slog.String("code", e.Error()), slog.String("reason", string(e.Reason())))
}
