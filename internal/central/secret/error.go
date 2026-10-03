// Package secret implements envelope encryption, stable credential leases and
// recoverable master-key maintenance. It installs no HTTP endpoints.
package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type Code string

const (
	InvalidConfiguration    Code = "SECRET_CONFIGURATION_INVALID"
	InvalidInput            Code = "SECRET_INPUT_INVALID"
	Unavailable             Code = "SECRET_UNAVAILABLE"
	PreparationRequired     Code = "SECRET_PREPARATION_REQUIRED"
	EpochChanged            Code = "SECRET_WRITE_EPOCH_CHANGED"
	DecryptFailed           Code = "SECRET_DECRYPT_FAILED"
	KeyUnavailable          Code = "SECRET_KEY_UNAVAILABLE"
	NonceExhausted          Code = "SECRET_NONCE_EXHAUSTED"
	NonceReservationUnknown Code = "SECRET_NONCE_RESERVATION_UNKNOWN"
	CommitUnknown           Code = "SECRET_COMMIT_UNKNOWN"
	AuthorizationRejected   Code = "SECRET_AUTHORIZATION_REJECTED"
	AuthorizationUnbound    Code = "SECRET_AUTHORIZATION_UNBOUND"
	Busy                    Code = "SECRET_RESOURCE_BUSY"
	Conflict                Code = "SECRET_VERSION_CONFLICT"
	NotFound                Code = "SECRET_NOT_FOUND"
	KeyReused               Code = "SECRET_KEY_REUSED"
	RotationFailed          Code = "SECRET_ROTATION_FAILED"
)

type Error struct {
	code  Code
	cause func() error
}

func failure(code Code, public foundation.Code, cause error) *Error {
	state := foundation.NotStarted
	if public == foundation.CommitUnknown {
		state = foundation.Unknown
	}
	f := foundation.NewFault(public, state)
	if cause != nil {
		f = f.WithCause(cause)
	}
	return &Error{code: code, cause: func() error { return f }}
}
func (e *Error) Error() string {
	if e == nil {
		return string(Unavailable)
	}
	return string(e.code)
}
func (e *Error) Code() Code {
	if e == nil {
		return Unavailable
	}
	return e.code
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
		Code Code `json:"code"`
	}{e.Code()})
}
func (e Error) LogValue() slog.Value { return slog.StringValue(e.Error()) }
func invalid() error                 { return failure(InvalidInput, foundation.InvalidArgument, nil) }
func unavailable(err error) error    { return failure(Unavailable, foundation.DependencyUnavailable, err) }
func authorization(err error) error {
	var f *foundation.Fault
	if errors.As(err, &f) {
		switch f.Code {
		case foundation.Forbidden, foundation.NotFound, foundation.Unauthenticated, foundation.SessionRevoked, foundation.ProjectNotActive, foundation.InvalidState:
			return failure(AuthorizationRejected, f.Code, err)
		case foundation.DependencyUnbound:
			return failure(AuthorizationUnbound, f.Code, err)
		}
	}
	return unavailable(err)
}
