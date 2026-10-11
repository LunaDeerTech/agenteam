// Package directory binds only bounded list-directory and find-files to a
// trusted Workspace Manager. Current Mount and operation authorization remain
// Central's responsibility before authenticated Control delivers the request.
package directory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type directoryHandle interface {
	List(context.Context, filesystem.ListRequest) (filesystem.ListResult, error)
	Find(context.Context, filesystem.FindRequest) (filesystem.FindResult, error)
	Drain(context.Context) error
}

// Handler borrows a Manager constructed by workspace.New for the trusted device
// root. The composition owner retains Manager.Stop/Drain; every execution owns
// and actually closes its individual Handle before returning its terminal.
type Handler struct {
	ensure func(context.Context, workspace.Identity) (directoryHandle, error)
}

func New(manager *workspace.Manager) (*Handler, error) {
	if manager == nil {
		return nil, workspace.ErrInvalid
	}
	return &Handler{ensure: func(ctx context.Context, identity workspace.Identity) (directoryHandle, error) {
		return manager.Ensure(ctx, identity)
	}}, nil
}

func (*Handler) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "directory_handler") }
func (*Handler) MarshalJSON() ([]byte, error) { return []byte(`"directory_handler"`), nil }
func (*Handler) LogValue() slog.Value         { return slog.StringValue("directory_handler") }

func (h *Handler) Accepts(request p.Request) bool {
	return h != nil && h.ensure != nil && request.OperationRevision == p.DirectoryRevision &&
		(request.OperationName == p.ListDirectoryOperation || request.OperationName == p.FindFilesOperation)
}

// Execute is synchronous through the original List/Find and actual Handle
// Drain. Determined failures/cancellation return a safe terminal and nil error;
// Control alone owns connection loss and Unknown. No stream is emitted.
func (h *Handler) Execute(ctx context.Context, request p.Request) (p.Response, error) {
	if !h.Accepts(request) {
		return terminal(request, p.Failure, p.UnsupportedOperation), nil
	}
	if ctx == nil || !request.RequestID.Valid() || !request.OperationID.Valid() || !request.ExecutionID.Valid() ||
		!request.ProjectID.Valid() || !request.AgentID.Valid() || !request.Mount.MountID.Valid() || !request.Mount.WorkspaceID.Valid() ||
		request.Environment != nil || request.IdempotencyKey != nil {
		return terminal(request, p.Failure, p.InvalidRequest), nil
	}
	input, err := p.DecodeDirectoryPayload(request.OperationName, request.Payload)
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
	var listed filesystem.ListResult
	var found filesystem.FindResult
	if request.OperationName == p.ListDirectoryOperation {
		listed, err = handle.List(ctx, filesystem.ListRequest{Path: a.Path, Limit: a.Limit})
	} else {
		found, err = handle.Find(ctx, filesystem.FindRequest{Path: a.Path, Limit: a.Limit, Pattern: a.Pattern, Match: a.Match})
	}
	// A cancelled operation may not abandon its Handle or detach the original
	// close in a timeout goroutine. Manager retains any actual close failure.
	if closeErr := handle.Drain(context.Background()); closeErr != nil {
		return failure(request, closeErr), nil
	}
	if err != nil {
		return failure(request, err), nil
	}
	if err := ctx.Err(); err != nil {
		return failure(request, err), nil
	}
	var raw []byte
	if request.OperationName == p.ListDirectoryOperation {
		raw, err = encodeList(a, listed)
	} else {
		raw, err = encodeFind(a, found)
	}
	if err != nil {
		return terminal(request, p.Failure, p.InternalError), nil
	}
	return p.Response{RequestID: request.RequestID, OperationID: request.OperationID, Outcome: p.Success, Payload: raw}, nil
}

func encodeList(args p.DirectoryArguments, result filesystem.ListResult) ([]byte, error) {
	if result.Path != args.Path || result.Entries == nil || len(result.Entries) > args.Limit || result.Truncated && len(result.Entries) != args.Limit {
		return nil, p.ErrInvalid
	}
	entries := make([]p.DirectoryEntry, len(result.Entries))
	for n, entry := range result.Entries {
		entries[n] = p.DirectoryEntry{Name: entry.Name, Kind: entry.Kind}
	}
	return p.EncodeListDirectoryResult(p.ListDirectoryResult{Path: result.Path, Entries: entries, Truncated: result.Truncated})
}

func encodeFind(args p.DirectoryArguments, result filesystem.FindResult) ([]byte, error) {
	if result.Path != args.Path || result.Paths == nil || len(result.Paths) > args.Limit || result.Truncated && len(result.Paths) != args.Limit {
		return nil, p.ErrInvalid
	}
	for _, name := range result.Paths {
		if args.Match == "name" {
			name = path.Base(name)
		}
		matched, err := path.Match(args.Pattern, name)
		if err != nil || !matched {
			return nil, p.ErrInvalid
		}
	}
	return p.EncodeFindFilesResult(p.FindFilesResult{Path: result.Path, Paths: result.Paths, Truncated: result.Truncated})
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
