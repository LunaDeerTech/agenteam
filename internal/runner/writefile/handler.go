// Package writefile adapts bounded atomic whole-file writes to Workspace.
// A request is not an authorization grant. Only an authenticated Control owner
// may supply a current, authorized workspace mapping to this explicit adapter.
package writefile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type writeHandle interface {
	Write(context.Context, filesystem.WriteRequest) (filesystem.WriteResult, error)
	Drain(context.Context) error
}

// Handler borrows a Manager created from the trusted device root. Its caller
// owns that Manager until Control joins. Every execution drains its own Handle.
type Handler struct {
	ensure func(context.Context, workspace.Identity) (writeHandle, error)
}

func New(manager *workspace.Manager) (*Handler, error) {
	if manager == nil {
		return nil, workspace.ErrInvalid
	}
	return &Handler{ensure: func(ctx context.Context, identity workspace.Identity) (writeHandle, error) {
		handle, err := manager.Ensure(ctx, identity)
		if err != nil {
			return nil, err
		}
		return handle, nil
	}}, nil
}

func (*Handler) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "write_file_handler") }
func (*Handler) MarshalJSON() ([]byte, error) { return []byte(`"write_file_handler"`), nil }
func (*Handler) LogValue() slog.Value         { return slog.StringValue("write_file_handler") }

func (h *Handler) Accepts(request p.Request) bool {
	return h != nil && h.ensure != nil && request.OperationName == p.WriteFileOperation && request.OperationRevision == p.WriteFileRevision
}

// Execute preserves publication facts even when Write also returns an error.
// It waits for the original Ensure, Write and Drain before returning, including
// after cancellation. Valid terminals return nil error because Control converts
// an executor error into generic Unknown. No readback or automatic retry occurs.
func (h *Handler) Execute(ctx context.Context, request p.Request) (p.Response, error) {
	if !h.Accepts(request) {
		return terminal(request, p.Failure, p.UnsupportedOperation), nil
	}
	if ctx == nil || !request.RequestID.Valid() || !request.OperationID.Valid() || !request.ExecutionID.Valid() ||
		!request.ProjectID.Valid() || !request.AgentID.Valid() || !request.Mount.MountID.Valid() || !request.Mount.WorkspaceID.Valid() ||
		request.Environment != nil || request.IdempotencyKey != nil {
		return terminal(request, p.Failure, p.InvalidRequest), nil
	}
	input, err := p.DecodeWriteFilePayload(request.Payload)
	if err != nil {
		return terminal(request, p.Failure, p.InvalidRequest), nil
	}
	defer clear(input.Arguments.Data)
	if request.Deadline != nil {
		deadline, err := request.Deadline.Time()
		if err != nil {
			return terminal(request, p.Failure, p.InvalidRequest), nil
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		return failure(request, err), nil
	}
	handle, err := h.ensure(ctx, workspace.Identity{ProjectID: request.ProjectID, AgentID: request.AgentID, Name: input.WorkspaceName})
	if err != nil {
		if handle != nil {
			if closeErr := handle.Drain(context.Background()); closeErr != nil {
				return terminal(request, p.Failure, p.InternalError), nil
			}
		}
		return failure(request, err), nil
	}
	if handle == nil {
		return terminal(request, p.Failure, p.InternalError), nil
	}
	a := input.Arguments
	result, writeErr := handle.Write(ctx, filesystem.WriteRequest{Path: a.Path, Data: a.Data, Overwrite: a.Overwrite})
	// Cancellation must not bypass the original close or replace it with an
	// untracked goroutine. Keep the decoded bytes alive through this actual tail.
	closeErr := handle.Drain(context.Background())
	switch result.State {
	case filesystem.WriteCommitted:
		if result.BytesWritten != len(a.Data) {
			return unknown(request), nil
		}
		code := completionCode(writeErr, closeErr, ctx.Err())
		if !result.Durable && code == nil {
			value := p.InternalError
			code = &value
		}
		raw, err := p.EncodeWriteFileResult(p.WriteFileResult{Path: a.Path, State: "committed", BytesWritten: result.BytesWritten, Durable: result.Durable, CompletionCode: code})
		if err != nil {
			return unknown(request), nil
		}
		return p.Response{RequestID: request.RequestID, OperationID: request.OperationID, Outcome: p.Success, Payload: raw}, nil
	case filesystem.WriteNotCommitted:
		if result.BytesWritten != 0 || result.Durable {
			return unknown(request), nil
		}
		if closeErr != nil {
			return terminal(request, p.Failure, p.InternalError), nil
		}
		if writeErr != nil {
			return failure(request, writeErr), nil
		}
		if err := ctx.Err(); err != nil {
			return failure(request, err), nil
		}
		return terminal(request, p.Failure, p.InternalError), nil
	default:
		// Unknown or an invalid result cannot be downgraded to zero publication,
		// even if cancellation or close failed after the attempted rename.
		return unknown(request), nil
	}
}

func completionCode(writeErr, closeErr, ctxErr error) *p.Code {
	var code p.Code
	switch {
	case closeErr != nil || writeErr != nil && !onlyCancellation(writeErr):
		code = p.InternalError
	case errors.Is(writeErr, context.DeadlineExceeded) || errors.Is(ctxErr, context.DeadlineExceeded):
		code = p.Timeout
	case errors.Is(writeErr, context.Canceled) || errors.Is(ctxErr, context.Canceled):
		code = p.CancelledCode
	default:
		return nil
	}
	return &code
}

// errors.Is alone would hide an I/O failure joined with cancellation.
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

func unknown(request p.Request) p.Response {
	message := p.UnknownMessage
	return p.Response{RequestID: request.RequestID, OperationID: request.OperationID, Outcome: p.Unknown, SafeMessage: &message}
}

func terminal(request p.Request, outcome p.Outcome, code p.Code) p.Response {
	message := code.SafeMessage()
	return p.Response{RequestID: request.RequestID, OperationID: request.OperationID, Outcome: outcome, Code: &code, SafeMessage: &message}
}

func failure(request p.Request, err error) p.Response {
	code := p.InternalError
	switch {
	case onlyCancellation(err) && errors.Is(err, context.DeadlineExceeded):
		return terminal(request, p.Cancelled, p.Timeout)
	case onlyCancellation(err):
		return terminal(request, p.Cancelled, p.CancelledCode)
	case errors.Is(err, filesystem.ErrInvalid), errors.Is(err, filesystem.ErrType), errors.Is(err, filesystem.ErrLimit), errors.Is(err, workspace.ErrInvalid), errors.Is(err, workspace.ErrType):
		code = p.InvalidRequest
	case errors.Is(err, filesystem.ErrNotFound):
		code = p.NotFound
	case errors.Is(err, filesystem.ErrOutside), errors.Is(err, workspace.ErrOutside):
		code = p.PathOutsideWorkspace
	case errors.Is(err, filesystem.ErrConflict), errors.Is(err, workspace.ErrConflict):
		code = p.Conflict
	case errors.Is(err, filesystem.ErrUnsupported), errors.Is(err, filesystem.ErrUnavailable), errors.Is(err, filesystem.ErrClosed),
		errors.Is(err, workspace.ErrUnsupported), errors.Is(err, workspace.ErrUnavailable), errors.Is(err, workspace.ErrClosed), errors.Is(err, workspace.ErrPermission):
		code = p.CapabilityUnavailable
	}
	return terminal(request, p.Failure, code)
}
