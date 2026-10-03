package lifecycle

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
)

type FailureCode string

const (
	InitializationFailed FailureCode = "INITIALIZATION_FAILED"
	ListenFailed         FailureCode = "LISTEN_FAILED"
	ServeFailed          FailureCode = "SERVE_FAILED"
	ShutdownFailed       FailureCode = "SHUTDOWN_FAILED"
	ShutdownTimeout      FailureCode = "SHUTDOWN_TIMEOUT"
	ForcedShutdown       FailureCode = "FORCED_SHUTDOWN"
)

func (c FailureCode) Safe() FailureCode {
	switch c {
	case InitializationFailed, ListenFailed, ServeFailed, ShutdownFailed, ShutdownTimeout, ForcedShutdown:
		return c
	}
	return InitializationFailed
}

// Failure retains an explicit diagnostic cause without exposing it through
// formatting (including traversal of an unexported enclosing field) or logging.
type Failure struct {
	code  FailureCode
	cause func() error
}

func NewFailure(code FailureCode, cause error) *Failure {
	return &Failure{code: code.Safe(), cause: func() error { return cause }}
}
func (f Failure) Code() FailureCode { return f.code.Safe() }
func (f *Failure) Error() string {
	if f == nil {
		return string(InitializationFailed)
	}
	return string(f.Code())
}
func (f Failure) Unwrap() error {
	if f.cause == nil {
		return nil
	}
	return f.cause()
}
func (f Failure) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, string(f.Code())) }
func (f Failure) LogValue() slog.Value       { return slog.StringValue(string(f.Code())) }
func CodeOf(err error) FailureCode {
	var f *Failure
	if errors.As(err, &f) && f != nil {
		return f.Code()
	}
	return InitializationFailed
}
