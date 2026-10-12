// Package command owns a single trusted-host child process and its local I/O.
// It supplies no execution authorization, shell, environment injection or WSS.
package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	MaxArguments     = 256
	MaxArgumentBytes = 64 << 10
	MaxStreamBytes   = 64 << 10
	MaxChunkBytes    = 8 << 10
)

var (
	ErrInvalid     = errors.New("command: invalid request")
	ErrUnsupported = errors.New("command: unsupported platform")
	ErrUnavailable = errors.New("command: unavailable")
	ErrStart       = errors.New("command: start failed")
	ErrIO          = errors.New("command: I/O failed")
	ErrClosed      = errors.New("command: closed")
	ErrBusy        = errors.New("command: already used")
)

// Request has no implicit timeout and never inherits the Runner environment.
// Program is an absolute canonical host path, not a PATH lookup or shell text.
// The trusted caller must not mutate Args while Run is taking its copy.
type Request struct {
	Program string
	Args    []string
}

func (r Request) Validate() error {
	if r.Program == "" || len(r.Program) > 4096 || !path.IsAbs(r.Program) || path.Clean(r.Program) != r.Program || r.Program == "/" || !validArgument(r.Program) || len(r.Args) > MaxArguments {
		return ErrInvalid
	}
	size := len(r.Program)
	for _, arg := range r.Args {
		if !validArgument(arg) || len(arg) > MaxArgumentBytes-size {
			return ErrInvalid
		}
		size += len(arg)
	}
	return nil
}

func validArgument(s string) bool { return utf8.ValidString(s) && !strings.ContainsRune(s, 0) }

type Stream string

const (
	Stdout Stream = "stdout"
	Stderr Stream = "stderr"
)

// Sink borrows one chunk until its synchronous return. Calls from stdout and
// stderr are serialized. Retaining bytes requires a copy; chunks are cleared
// after return. A nil sink discards output. A blocked sink keeps the owner live,
// even after cancellation; Drain cannot claim that callback has joined early.
type Sink func(context.Context, Stream, []byte) error

// Result preserves actual process facts even when Run also returns an error.
// ExitCode is -1 before Wait or after a signal. Byte counts are bytes delivered
// successfully to Sink (zero with a nil sink); each stream is bounded to 64KiB.
// OutputComplete means both pipe readers reached EOF without I/O/sink failure;
// truncation is reported separately. Group confirmation never proves that an
// escaped descendant (for example setsid) terminated: this is trusted-host,
// not a process-tree sandbox or Managed Process implementation.
type Result struct {
	Started                          bool
	Waited                           bool
	ExitCode                         int
	Signal                           int
	Cancelled                        bool
	TimedOut                         bool
	StdoutBytes                      int64
	StderrBytes                      int64
	StdoutTruncated                  bool
	StderrTruncated                  bool
	OutputComplete                   bool
	ProcessGroupTerminationConfirmed bool
}

func (Request) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "command_request") }
func (Request) MarshalJSON() ([]byte, error) { return []byte(`"command_request"`), nil }
func (Request) LogValue() slog.Value         { return slog.StringValue("command_request") }
func (Result) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "command_result") }
func (Result) MarshalJSON() ([]byte, error)  { return []byte(`"command_result"`), nil }
func (Result) LogValue() slog.Value          { return slog.StringValue("command_result") }
