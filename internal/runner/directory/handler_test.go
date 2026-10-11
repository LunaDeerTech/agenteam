package directory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/filesystem"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func request(t *testing.T, operation string) p.Request {
	t.Helper()
	a := p.DirectoryArguments{Path: "src", Limit: 2}
	if operation == p.FindFilesOperation {
		a.Pattern, a.Match = "*.go", "name"
	}
	raw, err := p.EncodeDirectoryPayload(operation, p.DirectoryPayload{WorkspaceName: "source", Arguments: a})
	if err != nil {
		t.Fatal(err)
	}
	return p.Request{RequestID: "01900000-0000-7000-8000-000000000001", OperationID: "01900000-0000-7000-8000-000000000002", ExecutionID: "01900000-0000-7000-8000-000000000003", ProjectID: "01900000-0000-7000-8000-000000000004", AgentID: "01900000-0000-7000-8000-000000000005", Mount: p.Mount{MountID: "01900000-0000-7000-8000-000000000006", WorkspaceID: "01900000-0000-7000-8000-000000000007"}, OperationName: operation, OperationRevision: p.DirectoryRevision, Payload: raw}
}

type controlledHandle struct {
	list  func(context.Context, filesystem.ListRequest) (filesystem.ListResult, error)
	find  func(context.Context, filesystem.FindRequest) (filesystem.FindResult, error)
	drain func(context.Context) error
}

func (h *controlledHandle) List(ctx context.Context, r filesystem.ListRequest) (filesystem.ListResult, error) {
	return h.list(ctx, r)
}
func (h *controlledHandle) Find(ctx context.Context, r filesystem.FindRequest) (filesystem.FindResult, error) {
	return h.find(ctx, r)
}
func (h *controlledHandle) Drain(ctx context.Context) error { return h.drain(ctx) }

func assertFailure(t *testing.T, r p.Request, response p.Response, err error, outcome p.Outcome, code p.Code) {
	t.Helper()
	if err != nil || response.RequestID != r.RequestID || response.OperationID != r.OperationID || response.Outcome != outcome || response.Code == nil || *response.Code != code || response.SafeMessage == nil || *response.SafeMessage != code.SafeMessage() || response.Payload != nil {
		t.Fatal("incorrect safe directory terminal")
	}
}

func TestDirectoryHandlerRejectsBeforeWorkspaceAdmission(t *testing.T) {
	calls := 0
	h := &Handler{ensure: func(context.Context, workspace.Identity) (directoryHandle, error) {
		calls++
		return nil, workspace.ErrUnavailable
	}}
	for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				edit func(*p.Request)
				code p.Code
			}{
				{"unknown-operation", func(r *p.Request) { r.OperationName = p.ReadFileOperation }, p.UnsupportedOperation},
				{"wrong-revision", func(r *p.Request) { r.OperationRevision = "2" }, p.UnsupportedOperation},
				{"request", func(r *p.Request) { r.RequestID = "" }, p.InvalidRequest},
				{"operation", func(r *p.Request) { r.OperationID = "" }, p.InvalidRequest},
				{"project", func(r *p.Request) { r.ProjectID = "../host" }, p.InvalidRequest},
				{"agent", func(r *p.Request) { r.AgentID = "" }, p.InvalidRequest},
				{"mount", func(r *p.Request) { r.Mount.MountID = "" }, p.InvalidRequest},
				{"workspace-id", func(r *p.Request) { r.Mount.WorkspaceID = "source" }, p.InvalidRequest},
				{"execution", func(r *p.Request) { r.ExecutionID = "" }, p.InvalidRequest},
				{"environment", func(r *p.Request) { r.Environment = map[string]string{} }, p.InvalidRequest},
				{"idempotency", func(r *p.Request) { v := "private-key"; r.IdempotencyKey = &v }, p.InvalidRequest},
				{"payload", func(r *p.Request) {
					r.Payload = []byte(`{"workspace_name":"source","arguments":{"path":"../host","limit":2}}`)
				}, p.InvalidRequest},
				{"deadline", func(r *p.Request) { v := p.Instant("bad"); r.Deadline = &v }, p.InvalidRequest},
			} {
				t.Run(tc.name, func(t *testing.T) {
					r := request(t, operation)
					tc.edit(&r)
					response, err := h.Execute(context.Background(), r)
					assertFailure(t, r, response, err, p.Failure, tc.code)
				})
			}
			r := request(t, operation)
			response, err := h.Execute(nil, r)
			assertFailure(t, r, response, err, p.Failure, p.InvalidRequest)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			response, err = h.Execute(ctx, r)
			assertFailure(t, r, response, err, p.Cancelled, p.CancelledCode)
			deadline, _ := p.NewInstant(time.Now().Add(-time.Second))
			r.Deadline = &deadline
			response, err = h.Execute(context.Background(), r)
			assertFailure(t, r, response, err, p.Cancelled, p.Timeout)
		})
	}
	if calls != 0 {
		t.Fatal("invalid request reached workspace Ensure")
	}
	if _, err := New(nil); !errors.Is(err, workspace.ErrInvalid) {
		t.Fatal("nil manager accepted")
	}
	if (*Handler)(nil).Accepts(request(t, p.ListDirectoryOperation)) || (&Handler{}).Accepts(request(t, p.FindFilesOperation)) {
		t.Fatal("unbound handler advertised support")
	}
}

func TestDirectoryHandlerSafeErrorsAndCloseFailure(t *testing.T) {
	for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
		t.Run(operation, func(t *testing.T) {
			for n, tc := range []struct {
				err  error
				code p.Code
			}{
				{filesystem.ErrInvalid, p.InvalidRequest}, {filesystem.ErrType, p.InvalidRequest}, {filesystem.ErrLimit, p.InvalidRequest},
				{filesystem.ErrNotFound, p.NotFound}, {filesystem.ErrOutside, p.PathOutsideWorkspace}, {filesystem.ErrConflict, p.Conflict},
				{filesystem.ErrUnavailable, p.CapabilityUnavailable}, {workspace.ErrPermission, p.CapabilityUnavailable}, {workspace.ErrClosed, p.CapabilityUnavailable},
				{errors.New("PRIVATE_IO_CANARY"), p.InternalError},
			} {
				t.Run(fmt.Sprint(n), func(t *testing.T) {
					closed := 0
					h := &Handler{ensure: func(context.Context, workspace.Identity) (directoryHandle, error) {
						return &controlledHandle{
							list: func(context.Context, filesystem.ListRequest) (filesystem.ListResult, error) {
								return filesystem.ListResult{Path: "PARTIAL_CANARY"}, fmt.Errorf("private: %w", tc.err)
							},
							find: func(context.Context, filesystem.FindRequest) (filesystem.FindResult, error) {
								return filesystem.FindResult{Path: "PARTIAL_CANARY"}, fmt.Errorf("private: %w", tc.err)
							},
							drain: func(context.Context) error { closed++; return nil },
						}, nil
					}}
					r := request(t, operation)
					response, err := h.Execute(context.Background(), r)
					assertFailure(t, r, response, err, p.Failure, tc.code)
					if closed != 1 {
						t.Fatal("failed operation did not close exactly once")
					}
				})
			}
			closed := 0
			h := &Handler{ensure: func(context.Context, workspace.Identity) (directoryHandle, error) {
				return &controlledHandle{
					list: func(context.Context, filesystem.ListRequest) (filesystem.ListResult, error) {
						return filesystem.ListResult{Path: "src", Entries: []filesystem.Entry{}}, nil
					},
					find: func(context.Context, filesystem.FindRequest) (filesystem.FindResult, error) {
						return filesystem.FindResult{Path: "src", Paths: []string{}}, nil
					},
					drain: func(context.Context) error { closed++; return filesystem.ErrUnavailable },
				}, nil
			}}
			r := request(t, operation)
			response, err := h.Execute(context.Background(), r)
			assertFailure(t, r, response, err, p.Failure, p.CapabilityUnavailable)
			if closed != 1 {
				t.Fatal("successful operation skipped actual close")
			}
		})
	}
}

func TestDirectoryHandlerRejectsMalformedLocalResults(t *testing.T) {
	for n, result := range []filesystem.ListResult{
		{Path: "other", Entries: []filesystem.Entry{}}, {Path: "src"},
		{Path: "src", Entries: []filesystem.Entry{{Name: "a", Kind: "file"}}, Truncated: true},
		{Path: "src", Entries: []filesystem.Entry{{Name: "b", Kind: "file"}, {Name: "a", Kind: "file"}}},
		{Path: "src", Entries: []filesystem.Entry{{Name: "a", Kind: "file"}, {Name: "b", Kind: "file"}, {Name: "c", Kind: "file"}}},
	} {
		t.Run("list/"+fmt.Sprint(n), func(t *testing.T) {
			closed := false
			h := &Handler{ensure: func(context.Context, workspace.Identity) (directoryHandle, error) {
				return &controlledHandle{list: func(context.Context, filesystem.ListRequest) (filesystem.ListResult, error) { return result, nil }, drain: func(context.Context) error { closed = true; return nil }}, nil
			}}
			r := request(t, p.ListDirectoryOperation)
			response, err := h.Execute(context.Background(), r)
			assertFailure(t, r, response, err, p.Failure, p.InternalError)
			if !closed {
				t.Fatal("malformed list bypassed close")
			}
		})
	}
	for n, result := range []filesystem.FindResult{
		{Path: "other", Paths: []string{}}, {Path: "src"},
		{Path: "src", Paths: []string{"src/a.go"}, Truncated: true},
		{Path: "src", Paths: []string{"src/b.go", "src/a.go"}},
		{Path: "src", Paths: []string{"src/a.go", "src/b.go", "src/c.go"}},
		{Path: "src", Paths: []string{"src/a.txt"}}, {Path: "src", Paths: []string{"elsewhere/a.go"}},
	} {
		t.Run("find/"+fmt.Sprint(n), func(t *testing.T) {
			closed := false
			h := &Handler{ensure: func(context.Context, workspace.Identity) (directoryHandle, error) {
				return &controlledHandle{find: func(context.Context, filesystem.FindRequest) (filesystem.FindResult, error) { return result, nil }, drain: func(context.Context) error { closed = true; return nil }}, nil
			}}
			r := request(t, p.FindFilesOperation)
			response, err := h.Execute(context.Background(), r)
			assertFailure(t, r, response, err, p.Failure, p.InternalError)
			if !closed {
				t.Fatal("malformed find bypassed close")
			}
		})
	}
}

func await(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("original directory owner did not reach barrier")
	}
}

func TestDirectoryHandlerCancellationWaitsForOriginalOperationAndClose(t *testing.T) {
	for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
		t.Run(operation, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			closeEntered, closeRelease := make(chan struct{}), make(chan struct{})
			var once, closeOnce sync.Once
			releaseOperation := func() { once.Do(func() { close(release) }) }
			releaseClose := func() { closeOnce.Do(func() { close(closeRelease) }) }
			ctx, cancel := context.WithCancel(context.Background())
			r := request(t, operation)
			args, err := p.DecodeDirectoryPayload(operation, r.Payload)
			if err != nil {
				t.Fatal(err)
			}
			h := &Handler{ensure: func(_ context.Context, identity workspace.Identity) (directoryHandle, error) {
				if identity.Name != "source" || identity.ProjectID != r.ProjectID || identity.AgentID != r.AgentID {
					t.Error("workspace identity confused protocol IDs with its name")
				}
				return &controlledHandle{
					list: func(ctx context.Context, input filesystem.ListRequest) (filesystem.ListResult, error) {
						if input.Path != args.Arguments.Path || input.Limit != args.Arguments.Limit {
							t.Error("list arguments changed")
						}
						close(entered)
						<-release
						return filesystem.ListResult{}, ctx.Err()
					},
					find: func(ctx context.Context, input filesystem.FindRequest) (filesystem.FindResult, error) {
						a := args.Arguments
						if input.Path != a.Path || input.Limit != a.Limit || input.Pattern != a.Pattern || input.Match != a.Match {
							t.Error("find arguments changed")
						}
						close(entered)
						<-release
						return filesystem.FindResult{}, ctx.Err()
					},
					drain: func(ctx context.Context) error {
						if ctx.Err() != nil {
							t.Error("close inherited cancelled operation context")
						}
						close(closeEntered)
						<-closeRelease
						return nil
					},
				}, nil
			}}
			var response p.Response
			var executeErr error
			done := make(chan struct{})
			go func() { defer close(done); response, executeErr = h.Execute(ctx, r) }()
			t.Cleanup(func() { cancel(); releaseOperation(); releaseClose(); await(t, done) })
			await(t, entered)
			cancel()
			select {
			case <-done:
				t.Fatal("cancellation detached the original directory operation")
			default:
			}
			releaseOperation()
			await(t, closeEntered)
			select {
			case <-done:
				t.Fatal("terminal preceded actual close return")
			default:
			}
			releaseClose()
			await(t, done)
			assertFailure(t, r, response, executeErr, p.Cancelled, p.CancelledCode)
		})
	}
}

func TestDirectoryHandlerCancellationWaitsForOriginalEnsure(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseEnsure := func() { once.Do(func() { close(release) }) }
	ctx, cancel := context.WithCancel(context.Background())
	h := &Handler{ensure: func(ctx context.Context, _ workspace.Identity) (directoryHandle, error) {
		close(entered)
		<-release
		return nil, ctx.Err()
	}}
	r := request(t, p.FindFilesOperation)
	var response p.Response
	var executeErr error
	done := make(chan struct{})
	go func() { defer close(done); response, executeErr = h.Execute(ctx, r) }()
	t.Cleanup(func() { cancel(); releaseEnsure(); await(t, done) })
	await(t, entered)
	cancel()
	select {
	case <-done:
		t.Fatal("cancellation detached original Ensure")
	default:
	}
	releaseEnsure()
	await(t, done)
	assertFailure(t, r, response, executeErr, p.Cancelled, p.CancelledCode)
}
