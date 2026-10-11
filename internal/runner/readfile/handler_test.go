package readfile

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

func request(t *testing.T) p.Request {
	t.Helper()
	raw, err := p.EncodeReadFilePayload(p.ReadFilePayload{WorkspaceName: "source", Arguments: p.ReadFileArguments{Path: "file", Length: 3, Encoding: "utf8"}})
	if err != nil {
		t.Fatal(err)
	}
	return p.Request{RequestID: "01900000-0000-7000-8000-000000000001", OperationID: "01900000-0000-7000-8000-000000000002", ExecutionID: "01900000-0000-7000-8000-000000000003", ProjectID: "01900000-0000-7000-8000-000000000004", AgentID: "01900000-0000-7000-8000-000000000005", Mount: p.Mount{MountID: "01900000-0000-7000-8000-000000000006", WorkspaceID: "01900000-0000-7000-8000-000000000007"}, OperationName: p.ReadFileOperation, OperationRevision: p.ReadFileRevision, Payload: raw}
}

type controlledHandle struct {
	read  func(context.Context, filesystem.ReadRequest) (filesystem.ReadResult, error)
	drain func(context.Context) error
}

func (h *controlledHandle) Read(ctx context.Context, r filesystem.ReadRequest) (filesystem.ReadResult, error) {
	return h.read(ctx, r)
}
func (h *controlledHandle) Drain(ctx context.Context) error { return h.drain(ctx) }

func assertFailure(t *testing.T, r p.Request, response p.Response, err error, outcome p.Outcome, code p.Code) {
	t.Helper()
	if err != nil || response.RequestID != r.RequestID || response.OperationID != r.OperationID || response.Outcome != outcome || response.Code == nil || *response.Code != code || response.SafeMessage == nil || *response.SafeMessage != code.SafeMessage() || response.Payload != nil {
		t.Fatal("incorrect safe terminal", response, err)
	}
}

func TestHandlerRejectsBeforeWorkspaceAdmission(t *testing.T) {
	calls := 0
	h := &Handler{ensure: func(context.Context, workspace.Identity) (readHandle, error) {
		calls++
		return nil, workspace.ErrUnavailable
	}}
	for _, tc := range []struct {
		name string
		edit func(*p.Request)
		code p.Code
	}{
		{"unknown-operation", func(r *p.Request) { r.OperationName = "write-file" }, p.UnsupportedOperation},
		{"wrong-revision", func(r *p.Request) { r.OperationRevision = "2" }, p.UnsupportedOperation},
		{"project", func(r *p.Request) { r.ProjectID = "../host" }, p.InvalidRequest},
		{"agent", func(r *p.Request) { r.AgentID = "" }, p.InvalidRequest},
		{"mount", func(r *p.Request) { r.Mount.MountID = "" }, p.InvalidRequest},
		{"workspace-id", func(r *p.Request) { r.Mount.WorkspaceID = "source" }, p.InvalidRequest},
		{"execution", func(r *p.Request) { r.ExecutionID = "" }, p.InvalidRequest},
		{"environment", func(r *p.Request) { r.Environment = map[string]string{} }, p.InvalidRequest},
		{"idempotency", func(r *p.Request) { key := "key"; r.IdempotencyKey = &key }, p.InvalidRequest},
		{"path", func(r *p.Request) {
			r.Payload = []byte(`{"workspace_name":"source","arguments":{"path":"../secret","offset":"0","length":3,"encoding":"utf8"}}`)
		}, p.InvalidRequest},
		{"name", func(r *p.Request) {
			r.Payload = []byte(`{"workspace_name":"../source","arguments":{"path":"file","offset":"0","length":3,"encoding":"utf8"}}`)
		}, p.InvalidRequest},
		{"deadline", func(r *p.Request) { d := p.Instant("bad"); r.Deadline = &d }, p.InvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request(t)
			tc.edit(&r)
			response, err := h.Execute(context.Background(), r)
			assertFailure(t, r, response, err, p.Failure, tc.code)
		})
	}
	r := request(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err := h.Execute(ctx, r)
	assertFailure(t, r, response, err, p.Cancelled, p.CancelledCode)
	deadline, _ := p.NewInstant(time.Now().Add(-time.Second))
	r.Deadline = &deadline
	response, err = h.Execute(context.Background(), r)
	assertFailure(t, r, response, err, p.Cancelled, p.Timeout)
	if calls != 0 {
		t.Fatal("invalid request reached workspace Ensure")
	}
	if _, err := New(nil); !errors.Is(err, workspace.ErrInvalid) {
		t.Fatal("nil manager accepted")
	}
	if (*Handler)(nil).Accepts(request(t)) || (&Handler{}).Accepts(request(t)) {
		t.Fatal("unbound handler advertised support")
	}
}

func TestHandlerMapsSafeErrorsAndClosesOnReadFailure(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code p.Code
	}{
		{filesystem.ErrInvalid, p.InvalidRequest}, {filesystem.ErrType, p.InvalidRequest}, {filesystem.ErrLimit, p.InvalidRequest},
		{filesystem.ErrNotFound, p.NotFound}, {filesystem.ErrOutside, p.PathOutsideWorkspace}, {filesystem.ErrConflict, p.Conflict},
		{filesystem.ErrUnsupported, p.CapabilityUnavailable}, {workspace.ErrPermission, p.CapabilityUnavailable},
		{workspace.ErrClosed, p.CapabilityUnavailable}, {errors.New("PRIVATE_IO_CANARY"), p.InternalError},
	} {
		t.Run(string(tc.code)+"/"+fmt.Sprint(tc.err), func(t *testing.T) {
			closed := 0
			h := &Handler{ensure: func(context.Context, workspace.Identity) (readHandle, error) {
				return &controlledHandle{read: func(context.Context, filesystem.ReadRequest) (filesystem.ReadResult, error) {
					return filesystem.ReadResult{Data: "PARTIAL_CONTENT_CANARY"}, fmt.Errorf("private: %w", tc.err)
				}, drain: func(context.Context) error { closed++; return nil }}, nil
			}}
			r := request(t)
			response, err := h.Execute(context.Background(), r)
			assertFailure(t, r, response, err, p.Failure, tc.code)
			if closed != 1 {
				t.Fatal("failed read did not close its handle exactly once")
			}
		})
	}
	closed := false
	h := &Handler{ensure: func(context.Context, workspace.Identity) (readHandle, error) {
		return &controlledHandle{read: func(context.Context, filesystem.ReadRequest) (filesystem.ReadResult, error) {
			return filesystem.ReadResult{Path: "file", BytesRead: 3, EOF: true, Encoding: "utf8", Data: "abc"}, nil
		}, drain: func(context.Context) error { closed = true; return filesystem.ErrUnavailable }}, nil
	}}
	r := request(t)
	response, err := h.Execute(context.Background(), r)
	assertFailure(t, r, response, err, p.Failure, p.CapabilityUnavailable)
	if !closed {
		t.Fatal("close was skipped")
	}
}

func await(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("original operation did not reach barrier")
	}
}

func TestHandlerCancellationWaitsForActualReadAndClose(t *testing.T) {
	readEntered, readRelease := make(chan struct{}), make(chan struct{})
	closeEntered, closeRelease := make(chan struct{}), make(chan struct{})
	var readOnce, closeOnce sync.Once
	releaseRead := func() { readOnce.Do(func() { close(readRelease) }) }
	releaseClose := func() { closeOnce.Do(func() { close(closeRelease) }) }
	defer releaseRead()
	defer releaseClose()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := request(t)
	h := &Handler{ensure: func(_ context.Context, identity workspace.Identity) (readHandle, error) {
		if identity.Name != "source" || identity.ProjectID != r.ProjectID || identity.AgentID != r.AgentID {
			t.Error("protocol IDs were confused with the logical workspace name")
		}
		return &controlledHandle{read: func(ctx context.Context, _ filesystem.ReadRequest) (filesystem.ReadResult, error) {
			close(readEntered)
			<-readRelease
			return filesystem.ReadResult{}, ctx.Err()
		}, drain: func(ctx context.Context) error {
			if ctx.Err() != nil {
				t.Error("cleanup inherited the cancelled operation context")
			}
			close(closeEntered)
			<-closeRelease
			return nil
		}}, nil
	}}
	done := make(chan p.Response, 1)
	go func() { response, _ := h.Execute(ctx, r); done <- response }()
	await(t, readEntered)
	cancel()
	select {
	case <-done:
		t.Fatal("cancel detached the original read")
	default:
	}
	releaseRead()
	await(t, closeEntered)
	select {
	case <-done:
		t.Fatal("terminal preceded the original close return")
	default:
	}
	releaseClose()
	select {
	case response := <-done:
		assertFailure(t, r, response, nil, p.Cancelled, p.CancelledCode)
	case <-time.After(time.Second):
		t.Fatal("execution did not actually return")
	}
}

func TestHandlerCancellationWaitsForOriginalEnsure(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	releaseEnsure := func() { once.Do(func() { close(release) }) }
	defer releaseEnsure()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &Handler{ensure: func(ctx context.Context, _ workspace.Identity) (readHandle, error) {
		close(entered)
		<-release
		return nil, ctx.Err()
	}}
	r := request(t)
	done := make(chan p.Response, 1)
	go func() { response, _ := h.Execute(ctx, r); done <- response }()
	await(t, entered)
	cancel()
	select {
	case <-done:
		t.Fatal("cancel detached the original Ensure")
	default:
	}
	releaseEnsure()
	select {
	case response := <-done:
		assertFailure(t, r, response, nil, p.Cancelled, p.CancelledCode)
	case <-time.After(time.Second):
		t.Fatal("Ensure owner did not actually return")
	}
}
