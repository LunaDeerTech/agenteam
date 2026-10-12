// Package runcommand adapts the single-use trusted-host command owner to one
// explicit Runner operation. A request, a workspace mapping and this Handler
// are not Central execution authority. No environment injection is supported.
package runcommand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strconv"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/command"
	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type commandHandle interface {
	RunCommand(context.Context, string, command.Request, command.Sink) (command.Result, error)
	Drain(context.Context) error
}

type Handler struct {
	ensure func(context.Context, workspace.Identity) (commandHandle, error)
}

func New(manager *workspace.Manager) (*Handler, error) {
	if manager == nil {
		return nil, workspace.ErrInvalid
	}
	if runtime.GOOS != "linux" {
		return nil, command.ErrUnsupported
	}
	return &Handler{ensure: func(ctx context.Context, identity workspace.Identity) (commandHandle, error) {
		handle, err := manager.Ensure(ctx, identity)
		if handle == nil {
			return nil, err
		}
		return handle, err
	}}, nil
}

func (*Handler) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "run_command_handler") }
func (*Handler) MarshalJSON() ([]byte, error) { return []byte(`"run_command_handler"`), nil }
func (*Handler) LogValue() slog.Value         { return slog.StringValue("run_command_handler") }

func (h *Handler) Accepts(r p.Request) bool {
	return h != nil && h.ensure != nil && r.OperationName == p.RunCommandOperation && r.OperationRevision == p.RunCommandRevision
}

// Execute's success is the command profile's complete known invocation report,
// not a zero exit code or a claim that cancellation undid external side effects.
// Every path retains the original Handle through its actual Drain return.
func (h *Handler) Execute(ctx context.Context, r p.Request, emit func(string, string) error) (p.Response, error) {
	if !h.Accepts(r) {
		return terminal(r, p.Failure, p.UnsupportedOperation), nil
	}
	if ctx == nil || emit == nil || !r.RequestID.Valid() || !r.OperationID.Valid() || !r.ExecutionID.Valid() ||
		!r.ProjectID.Valid() || !r.AgentID.Valid() || !r.Mount.MountID.Valid() || !r.Mount.WorkspaceID.Valid() ||
		r.Environment != nil || r.IdempotencyKey != nil {
		return terminal(r, p.Failure, p.InvalidRequest), nil
	}
	input, err := p.DecodeRunCommandPayload(r.Payload)
	if err != nil {
		return terminal(r, p.Failure, p.InvalidRequest), nil
	}
	if r.Deadline != nil {
		deadline, err := r.Deadline.Time()
		if err != nil {
			return terminal(r, p.Failure, p.InvalidRequest), nil
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return failure(r, err), nil
	}
	handle, err := h.ensure(ctx, workspace.Identity{ProjectID: r.ProjectID, AgentID: r.AgentID, Name: input.WorkspaceName})
	if err != nil {
		if handle != nil {
			err = errors.Join(err, handle.Drain(context.Background()))
		}
		return failure(r, err), nil
	}
	if handle == nil {
		return terminal(r, p.Failure, p.InternalError), nil
	}
	output := &commandOutput{emit: emit}
	defer output.clear()
	started := time.Now()
	result, runErr := handle.RunCommand(ctx, input.Arguments.CWD,
		command.Request{Program: input.Arguments.Program, Args: input.Arguments.Args}, output.sink)
	closeErr := handle.Drain(context.Background())
	elapsed := time.Since(started).Milliseconds()
	output.finish(ctx, result)
	if output.transportFailed {
		// A stream failure keeps Control's original failed-wire semantics, even
		// if the process subsequently produced a known status.
		return unknown(r), nil
	}
	if !result.Started {
		if result.Waited || result.Signal != 0 {
			return unknown(r), nil
		}
		return failure(r, errors.Join(runErr, closeErr)), nil
	}
	if !result.Waited || !validStatus(result) ||
		result.StdoutBytes < output.streams[0].queued || result.StderrBytes < output.streams[1].queued ||
		result.StdoutBytes > command.MaxStreamBytes || result.StderrBytes > command.MaxStreamBytes {
		return unknown(r), nil
	}
	timedOut := result.TimedOut || errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(runErr, context.DeadlineExceeded)
	cancelled := result.Cancelled || timedOut || errors.Is(ctx.Err(), context.Canceled) || errors.Is(runErr, context.Canceled)
	complete := result.OutputComplete && !output.dropped && !output.invalid
	var code *p.Code
	switch {
	case closeErr != nil || output.invalid || runErr != nil && !onlyCancellation(runErr) || !complete && !cancelled:
		v := p.InternalError
		code = &v
	case timedOut:
		v := p.Timeout
		code = &v
	case cancelled:
		v := p.CancelledCode
		code = &v
	}
	encoded, err := p.EncodeRunCommandResult(p.RunCommandResult{
		State: "completed", ExitCode: result.ExitCode, Signal: result.Signal,
		Cancelled: cancelled, TimedOut: timedOut, DurationMS: p.Decimal(strconv.FormatInt(elapsed, 10)),
		StdoutBytes:     p.Decimal(strconv.FormatInt(output.streams[0].queued, 10)),
		StderrBytes:     p.Decimal(strconv.FormatInt(output.streams[1].queued, 10)),
		StdoutTruncated: result.StdoutTruncated, StderrTruncated: result.StderrTruncated,
		OutputComplete: complete, ProcessGroupTerminationConfirmed: result.ProcessGroupTerminationConfirmed,
		CompletionCode: code,
	})
	if err != nil {
		return unknown(r), nil
	}
	return p.Response{RequestID: r.RequestID, OperationID: r.OperationID, Outcome: p.Success, Payload: encoded}, nil
}

func validStatus(r command.Result) bool {
	return r.Signal == 0 && r.ExitCode >= 0 && r.ExitCode <= 255 || r.Signal > 0 && r.ExitCode == -1
}

func onlyCancellation(err error) bool {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !onlyCancellation(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyCancellation(wrapped.Unwrap())
	}
	return false
}

func terminal(r p.Request, outcome p.Outcome, code p.Code) p.Response {
	safe := code.SafeMessage()
	return p.Response{RequestID: r.RequestID, OperationID: r.OperationID, Outcome: outcome, Code: &code, SafeMessage: &safe}
}

func unknown(r p.Request) p.Response {
	safe := p.UnknownMessage
	return p.Response{RequestID: r.RequestID, OperationID: r.OperationID, Outcome: p.Unknown, SafeMessage: &safe}
}

func failure(r p.Request, err error) p.Response {
	code := p.InternalError
	switch {
	case onlyCancellation(err) && errors.Is(err, context.DeadlineExceeded):
		return terminal(r, p.Cancelled, p.Timeout)
	case onlyCancellation(err):
		return terminal(r, p.Cancelled, p.CancelledCode)
	case errors.Is(err, command.ErrInvalid), errors.Is(err, filesystem.ErrInvalid), errors.Is(err, filesystem.ErrType), errors.Is(err, workspace.ErrInvalid), errors.Is(err, workspace.ErrType):
		code = p.InvalidRequest
	case errors.Is(err, filesystem.ErrNotFound):
		code = p.NotFound
	case errors.Is(err, filesystem.ErrOutside), errors.Is(err, workspace.ErrOutside):
		code = p.PathOutsideWorkspace
	case errors.Is(err, command.ErrUnsupported), errors.Is(err, command.ErrUnavailable), errors.Is(err, command.ErrClosed),
		errors.Is(err, filesystem.ErrUnsupported), errors.Is(err, filesystem.ErrUnavailable), errors.Is(err, filesystem.ErrClosed),
		errors.Is(err, workspace.ErrUnsupported), errors.Is(err, workspace.ErrUnavailable), errors.Is(err, workspace.ErrClosed), errors.Is(err, workspace.ErrPermission):
		code = p.CapabilityUnavailable
	}
	return terminal(r, p.Failure, code)
}
