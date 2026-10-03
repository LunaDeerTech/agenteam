package foundation

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Code is a stable semantic code. HTTP mappings belong to the HTTP adapter.
type Code string

const (
	InvalidArgument         Code = "INVALID_ARGUMENT"
	CursorInvalid           Code = "CURSOR_INVALID"
	CursorStale             Code = "CURSOR_STALE"
	Unauthenticated         Code = "UNAUTHENTICATED"
	SessionRevoked          Code = "SESSION_REVOKED"
	Forbidden               Code = "FORBIDDEN"
	CSRFFailed              Code = "CSRF_FAILED"
	OriginDenied            Code = "ORIGIN_DENIED"
	NotFound                Code = "NOT_FOUND"
	MethodNotAllowed        Code = "METHOD_NOT_ALLOWED"
	ResourceDeleted         Code = "RESOURCE_DELETED"
	VersionConflict         Code = "VERSION_CONFLICT"
	IdempotencyKeyReused    Code = "IDEMPOTENCY_KEY_REUSED"
	InvalidState            Code = "INVALID_STATE"
	AgentBusy               Code = "AGENT_BUSY"
	ResourceBusy            Code = "RESOURCE_BUSY"
	ProjectNotActive        Code = "PROJECT_NOT_ACTIVE"
	ConfirmationStale       Code = "CONFIRMATION_STALE"
	SchemaUnsupported       Code = "SCHEMA_UNSUPPORTED"
	CapabilityUnsupported   Code = "CAPABILITY_UNSUPPORTED"
	RateLimited             Code = "RATE_LIMITED"
	DependencyUnbound       Code = "DEPENDENCY_UNBOUND"
	DependencyUnavailable   Code = "DEPENDENCY_UNAVAILABLE"
	CommitUnknown           Code = "COMMIT_UNKNOWN"
	InternalError           Code = "INTERNAL_ERROR"
	PayloadTooLarge         Code = "PAYLOAD_TOO_LARGE"
	UnsupportedMediaType    Code = "UNSUPPORTED_MEDIA_TYPE"
	ShuttingDown            Code = "SHUTTING_DOWN"
	ObjectPayloadMissing    Code = "OBJECT_PAYLOAD_MISSING"
	ObjectIntegrityMismatch Code = "OBJECT_INTEGRITY_MISMATCH"
	RangeNotSatisfiable     Code = "RANGE_NOT_SATISFIABLE"
)

func (c Code) Known() bool {
	switch c {
	case InvalidArgument, CursorInvalid, CursorStale, Unauthenticated, SessionRevoked,
		Forbidden, CSRFFailed, OriginDenied, NotFound, MethodNotAllowed, ResourceDeleted,
		VersionConflict, IdempotencyKeyReused, InvalidState, AgentBusy, ResourceBusy,
		ProjectNotActive, ConfirmationStale, SchemaUnsupported, CapabilityUnsupported,
		RateLimited, DependencyUnbound, DependencyUnavailable, CommitUnknown, InternalError,
		PayloadTooLarge, UnsupportedMediaType, ShuttingDown, ObjectPayloadMissing,
		ObjectIntegrityMismatch, RangeNotSatisfiable:
		return true
	}
	return false
}

func (c Code) Safe() Code {
	if c.Known() {
		return c
	}
	return InternalError
}

type CommitState string

const (
	NotStarted   CommitState = "not_started"
	NotCommitted CommitState = "not_committed"
	Committed    CommitState = "committed"
	Unknown      CommitState = "unknown"
)

func (s CommitState) Safe() CommitState {
	switch s {
	case NotStarted, NotCommitted, Committed, Unknown:
		return s
	}
	return Unknown
}

// FieldError contains a safe schema path, never an offending input value.
type FieldError struct {
	Path string `json:"path"`
	Code string `json:"code"`
}

func (f FieldError) Validate() error {
	if f.Path != "" && !strings.HasPrefix(f.Path, "/") {
		return errScalar
	}
	for i := 0; i < len(f.Path); i++ {
		if f.Path[i] == '~' {
			if i+1 == len(f.Path) || f.Path[i+1] != '0' && f.Path[i+1] != '1' {
				return errScalar
			}
			i++
		}
	}
	if len(f.Code) == 0 || f.Code[0] < 'A' || f.Code[0] > 'Z' {
		return errScalar
	}
	for i := range len(f.Code) {
		c := f.Code[i]
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return errScalar
		}
	}
	return nil
}

// Fault separates explicitly safe metadata from a private diagnostic cause.
// SafeMessage, FieldErrors and CauseID must be populated from trusted projections.
// Generic formatting and structured logging emit only recognized codes and states.
// Access to the original cause always requires an explicit errors.Unwrap/Is/As.
type Fault struct {
	Code        Code         `json:"code"`
	SafeMessage string       `json:"safe_message,omitempty"`
	FieldErrors []FieldError `json:"field_errors,omitempty"`
	RetryHint   string       `json:"retry_hint,omitempty"`
	CommitState CommitState  `json:"commit_state"`
	CauseID     string       `json:"cause_id,omitempty"`
	// A closure keeps fmt's recursive traversal of unexported enclosing fields
	// from inspecting the raw error. Merely making an error field private is
	// insufficient: such traversal does not call this type's Format method.
	cause func() error
}

func NewFault(code Code, state CommitState) *Fault {
	return &Fault{Code: code, CommitState: state.Safe()}
}

// WithCause returns a copy; sharing a fault does not make diagnostic attachment mutable.
func (f Fault) WithCause(cause error) *Fault {
	f.cause = func() error { return cause }
	return &f
}
func (f Fault) Unwrap() error {
	if f.cause == nil {
		return nil
	}
	return f.cause()
}
func (f *Fault) Error() string {
	if f == nil {
		return string(InternalError)
	}
	return string(f.Code.Safe())
}
func (f Fault) Format(w fmt.State, verb rune) { _, _ = io.WriteString(w, f.Error()) }
func (f Fault) LogValue() slog.Value {
	return slog.GroupValue(slog.String("code", f.Error()), slog.String("commit_state", string(f.CommitState.Safe())))
}

func (f Fault) MarshalJSON() ([]byte, error) {
	type safeFault Fault
	f.Code = f.Code.Safe()
	f.CommitState = f.CommitState.Safe()
	return json.Marshal(safeFault(f))
}
