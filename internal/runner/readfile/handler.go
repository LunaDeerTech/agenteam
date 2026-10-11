// Package readfile adapts the single bounded read-file operation to Workspace.
// Only an authenticated Control owner may supply requests whose current Mount
// mapping and operation authorization were verified by Central.
package readfile

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

type readHandle interface {
	Read(context.Context, filesystem.ReadRequest) (filesystem.ReadResult, error)
	Drain(context.Context) error
}

// Handler borrows its Manager. Its caller owns Manager.Stop/Drain, and must
// construct that Manager with workspace.New against the trusted device root.
// Each execution closes its own Handle before returning a terminal.
type Handler struct {
	ensure func(context.Context, workspace.Identity) (readHandle, error)
}

func New(manager *workspace.Manager) (*Handler, error) {
	if manager == nil {
		return nil, workspace.ErrInvalid
	}
	return &Handler{ensure: func(ctx context.Context, identity workspace.Identity) (readHandle, error) {
		return manager.Ensure(ctx, identity)
	}}, nil
}

func (*Handler) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "read_file_handler") }
func (*Handler) MarshalJSON() ([]byte, error) { return []byte(`"read_file_handler"`), nil }
func (*Handler) LogValue() slog.Value         { return slog.StringValue("read_file_handler") }

func (h *Handler) Accepts(request p.Request) bool {
	return h != nil && h.ensure != nil && request.OperationName == p.ReadFileOperation && request.OperationRevision == p.ReadFileRevision
}

// Execute never streams. A determined failure/cancellation is a valid terminal,
// returned with nil error so Control does not convert it into Unknown. It waits
// synchronously for the original Ensure, Read and Handle close, including after
// cancellation; the connection/process owner remains responsible for that wait.
func (h *Handler) Execute(ctx context.Context, request p.Request) (p.Response, error) {
	if !h.Accepts(request) {
		return terminal(request, p.Failure, p.UnsupportedOperation), nil
	}
	if ctx == nil || !request.RequestID.Valid() || !request.OperationID.Valid() || !request.ExecutionID.Valid() ||
		!request.ProjectID.Valid() || !request.AgentID.Valid() || !request.Mount.MountID.Valid() || !request.Mount.WorkspaceID.Valid() ||
		request.Environment != nil || request.IdempotencyKey != nil {
		return terminal(request, p.Failure, p.InvalidRequest), nil
	}
	input, err := p.DecodeReadFilePayload(request.Payload)
	if err != nil {
		return terminal(request, p.Failure, p.InvalidRequest), nil
	}
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
		return failure(request, err), nil
	}
	if handle == nil {
		return terminal(request, p.Failure, p.InternalError), nil
	}
	a := input.Arguments
	result, readErr := handle.Read(ctx, filesystem.ReadRequest{Path: a.Path, Offset: a.Offset, Length: a.Length, Encoding: a.Encoding})
	// The handle belongs to this execution. A cancelled context must not skip its
	// actual close or detach cleanup into an untracked background goroutine.
	if err := handle.Drain(context.Background()); err != nil {
		return failure(request, err), nil
	}
	if readErr != nil {
		return failure(request, readErr), nil
	}
	if err := ctx.Err(); err != nil {
		return failure(request, err), nil
	}
	if result.Path != a.Path || result.Offset != a.Offset || result.Encoding != a.Encoding || result.BytesRead > a.Length || !result.EOF && result.BytesRead != a.Length {
		return terminal(request, p.Failure, p.InternalError), nil
	}
	raw, err := p.EncodeReadFileResult(p.ReadFileResult{Path: result.Path, Offset: result.Offset, BytesRead: result.BytesRead, EOF: result.EOF, Encoding: result.Encoding, Data: result.Data})
	if err != nil {
		return terminal(request, p.Failure, p.InternalError), nil
	}
	return p.Response{RequestID: request.RequestID, OperationID: request.OperationID, Outcome: p.Success, Payload: raw}, nil
}

func terminal(request p.Request, outcome p.Outcome, code p.Code) p.Response {
	message := code.SafeMessage()
	return p.Response{RequestID: request.RequestID, OperationID: request.OperationID, Outcome: outcome, Code: &code, SafeMessage: &message}
}

func failure(request p.Request, err error) p.Response {
	code := p.InternalError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return terminal(request, p.Cancelled, p.Timeout)
	case errors.Is(err, context.Canceled):
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
