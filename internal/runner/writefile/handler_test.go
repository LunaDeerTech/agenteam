package writefile

import (
	"bytes"
	"context"
	"encoding/base64"
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
	raw, err := p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: "source", Arguments: p.WriteFileArguments{Path: "file", Data: []byte("abc")}})
	if err != nil {
		t.Fatal(err)
	}
	return p.Request{RequestID: "01900000-0000-7000-8000-000000000001", OperationID: "01900000-0000-7000-8000-000000000002", ExecutionID: "01900000-0000-7000-8000-000000000003", ProjectID: "01900000-0000-7000-8000-000000000004", AgentID: "01900000-0000-7000-8000-000000000005", Mount: p.Mount{MountID: "01900000-0000-7000-8000-000000000006", WorkspaceID: "01900000-0000-7000-8000-000000000007"}, OperationName: p.WriteFileOperation, OperationRevision: p.WriteFileRevision, Payload: raw}
}

type controlledHandle struct {
	write func(context.Context, filesystem.WriteRequest) (filesystem.WriteResult, error)
	drain func(context.Context) error
}

func (h *controlledHandle) Write(ctx context.Context, request filesystem.WriteRequest) (filesystem.WriteResult, error) {
	return h.write(ctx, request)
}
func (h *controlledHandle) Drain(ctx context.Context) error { return h.drain(ctx) }

func assertFailure(t *testing.T, request p.Request, response p.Response, err error, outcome p.Outcome, code p.Code) {
	t.Helper()
	if err != nil || response.RequestID != request.RequestID || response.OperationID != request.OperationID || response.Outcome != outcome || response.Code == nil || *response.Code != code || response.SafeMessage == nil || *response.SafeMessage != code.SafeMessage() || response.Payload != nil {
		t.Fatal("incorrect safe failure terminal")
	}
}

func assertCommitted(t *testing.T, request p.Request, response p.Response, err error, count int, durable bool, code p.Code) {
	t.Helper()
	if err != nil || response.RequestID != request.RequestID || response.OperationID != request.OperationID || response.Outcome != p.Success || response.Code != nil || response.SafeMessage != nil {
		t.Fatal("publication fact was lost or promoted to a generic error")
	}
	result, err := p.DecodeWriteFileResult(response.Payload)
	if err != nil || result.Path != "file" || result.State != "committed" || result.BytesWritten != count || result.Durable != durable {
		t.Fatal("committed result does not match the original write")
	}
	if code == "" && result.CompletionCode != nil || code != "" && (result.CompletionCode == nil || *result.CompletionCode != code) {
		t.Fatal("completion warning changed")
	}
}

func TestHandlerRejectsBeforeWorkspaceAdmission(t *testing.T) {
	calls := 0
	handler := &Handler{ensure: func(context.Context, workspace.Identity) (writeHandle, error) {
		calls++
		return nil, workspace.ErrUnavailable
	}}
	for _, tc := range []struct {
		name string
		edit func(*p.Request)
		code p.Code
	}{
		{"operation", func(r *p.Request) { r.OperationName = p.ReadFileOperation }, p.UnsupportedOperation},
		{"revision", func(r *p.Request) { r.OperationRevision = "2" }, p.UnsupportedOperation},
		{"request", func(r *p.Request) { r.RequestID = "" }, p.InvalidRequest},
		{"operation-id", func(r *p.Request) { r.OperationID = "" }, p.InvalidRequest},
		{"project", func(r *p.Request) { r.ProjectID = "../host" }, p.InvalidRequest},
		{"agent", func(r *p.Request) { r.AgentID = "" }, p.InvalidRequest},
		{"execution", func(r *p.Request) { r.ExecutionID = "" }, p.InvalidRequest},
		{"mount", func(r *p.Request) { r.Mount.MountID = "" }, p.InvalidRequest},
		{"workspace", func(r *p.Request) { r.Mount.WorkspaceID = "source" }, p.InvalidRequest},
		{"environment", func(r *p.Request) { r.Environment = map[string]string{} }, p.InvalidRequest},
		{"key", func(r *p.Request) { key := "key"; r.IdempotencyKey = &key }, p.InvalidRequest},
		{"path", func(r *p.Request) {
			r.Payload = []byte(`{"workspace_name":"source","arguments":{"path":"../secret","data_base64":"","overwrite":false}}`)
		}, p.InvalidRequest},
		{"name", func(r *p.Request) {
			r.Payload = []byte(`{"workspace_name":"../source","arguments":{"path":"file","data_base64":"","overwrite":false}}`)
		}, p.InvalidRequest},
		{"base64", func(r *p.Request) {
			r.Payload = []byte(`{"workspace_name":"source","arguments":{"path":"file","data_base64":"YQ","overwrite":false}}`)
		}, p.InvalidRequest},
		{"size", func(r *p.Request) {
			r.Payload = []byte(`{"workspace_name":"source","arguments":{"path":"file","data_base64":"` + base64.StdEncoding.EncodeToString(make([]byte, p.MaxWriteFileBytes+1)) + `","overwrite":false}}`)
		}, p.InvalidRequest},
		{"deadline", func(r *p.Request) { deadline := p.Instant("invalid"); r.Deadline = &deadline }, p.InvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request(t)
			tc.edit(&r)
			response, err := handler.Execute(context.Background(), r)
			assertFailure(t, r, response, err, p.Failure, tc.code)
		})
	}
	r := request(t)
	response, err := handler.Execute(nil, r)
	assertFailure(t, r, response, err, p.Failure, p.InvalidRequest)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err = handler.Execute(ctx, r)
	assertFailure(t, r, response, err, p.Cancelled, p.CancelledCode)
	deadline, _ := p.NewInstant(time.Now().Add(-time.Second))
	r.Deadline = &deadline
	response, err = handler.Execute(context.Background(), r)
	assertFailure(t, r, response, err, p.Cancelled, p.Timeout)
	if calls != 0 {
		t.Fatal("rejected request reached Workspace Ensure")
	}
	if _, err := New(nil); !errors.Is(err, workspace.ErrInvalid) || (*Handler)(nil).Accepts(r) || (&Handler{}).Accepts(r) {
		t.Fatal("unbound handler accepted")
	}
}

func TestHandlerKeepsPublicationAndCompletionFacts(t *testing.T) {
	committed := filesystem.WriteResult{State: filesystem.WriteCommitted, BytesWritten: 3, Durable: true}
	for _, tc := range []struct {
		name     string
		result   filesystem.WriteResult
		writeErr error
		closeErr error
		outcome  p.Outcome
		code     p.Code
	}{
		{"committed", committed, nil, nil, p.Success, ""},
		{"parent-fsync", filesystem.WriteResult{State: filesystem.WriteCommitted, BytesWritten: 3}, filesystem.ErrUnavailable, nil, p.Success, p.InternalError},
		{"durability-incomplete", filesystem.WriteResult{State: filesystem.WriteCommitted, BytesWritten: 3}, nil, nil, p.Success, p.InternalError},
		{"close-after-durable", committed, nil, filesystem.ErrUnavailable, p.Success, p.InternalError},
		{"cancel-after-commit", committed, fmt.Errorf("wrapped: %w", context.Canceled), nil, p.Success, p.CancelledCode},
		{"deadline-after-commit", committed, context.DeadlineExceeded, nil, p.Success, p.Timeout},
		{"io-and-cancel", committed, errors.Join(filesystem.ErrUnavailable, context.Canceled), nil, p.Success, p.InternalError},
		{"conflict", filesystem.WriteResult{State: filesystem.WriteNotCommitted}, filesystem.ErrConflict, nil, p.Failure, p.Conflict},
		{"cancel-before-commit", filesystem.WriteResult{State: filesystem.WriteNotCommitted}, context.Canceled, nil, p.Cancelled, p.CancelledCode},
		{"close-without-commit", filesystem.WriteResult{State: filesystem.WriteNotCommitted}, filesystem.ErrConflict, errors.New("PRIVATE_CLOSE_ERROR"), p.Failure, p.InternalError},
		{"unknown-cancel", filesystem.WriteResult{State: filesystem.WriteUnknown}, context.Canceled, nil, p.Unknown, ""},
		{"unknown-close", filesystem.WriteResult{State: filesystem.WriteUnknown}, nil, filesystem.ErrUnavailable, p.Unknown, ""},
		{"short-commit", filesystem.WriteResult{State: filesystem.WriteCommitted, BytesWritten: 2, Durable: true}, nil, nil, p.Unknown, ""},
		{"contradictory-not-committed", filesystem.WriteResult{State: filesystem.WriteNotCommitted, BytesWritten: 3}, nil, nil, p.Unknown, ""},
		{"missing-state", filesystem.WriteResult{}, errors.New("PRIVATE_WRITE_ERROR"), nil, p.Unknown, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request(t)
			writes, closes := 0, 0
			handler := &Handler{ensure: func(_ context.Context, identity workspace.Identity) (writeHandle, error) {
				if identity.Name != "source" || identity.ProjectID != r.ProjectID || identity.AgentID != r.AgentID {
					t.Fatal("workspace identity was derived from untrusted paths or wrong IDs")
				}
				return &controlledHandle{write: func(_ context.Context, in filesystem.WriteRequest) (filesystem.WriteResult, error) {
					writes++
					if in.Path != "file" || !bytes.Equal(in.Data, []byte("abc")) || in.Overwrite {
						t.Fatal("complete decoded arguments changed")
					}
					return tc.result, tc.writeErr
				}, drain: func(ctx context.Context) error {
					closes++
					if ctx.Err() != nil {
						t.Error("close inherited cancellation")
					}
					return tc.closeErr
				}}, nil
			}}
			response, err := handler.Execute(context.Background(), r)
			if writes != 1 || closes != 1 {
				t.Fatal("write or actual close count changed")
			}
			switch tc.outcome {
			case p.Success:
				assertCommitted(t, r, response, err, 3, tc.result.Durable, tc.code)
			case p.Unknown:
				if err != nil || response.RequestID != r.RequestID || response.OperationID != r.OperationID || response.Outcome != p.Unknown || response.Code != nil || response.SafeMessage == nil || *response.SafeMessage != p.UnknownMessage || response.Payload != nil {
					t.Fatal("unknown publication was misreported")
				}
			default:
				assertFailure(t, r, response, err, tc.outcome, tc.code)
			}
		})
	}
}

func await(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("original call did not reach its barrier")
	}
}

func TestHandlerCancellationWaitsForActualWriteAndClose(t *testing.T) {
	for _, phase := range []string{"ensure", "write", "close"} {
		t.Run(phase, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var borrowed []byte
			writes, closes := 0, 0
			hold := func(at string) {
				if at == phase {
					close(entered)
					<-release
				}
			}
			handler := &Handler{ensure: func(ctx context.Context, _ workspace.Identity) (writeHandle, error) {
				hold("ensure")
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return &controlledHandle{write: func(_ context.Context, request filesystem.WriteRequest) (filesystem.WriteResult, error) {
					writes++
					borrowed = request.Data
					hold("write")
					return filesystem.WriteResult{State: filesystem.WriteCommitted, BytesWritten: len(request.Data), Durable: true}, nil
				}, drain: func(ctx context.Context) error {
					closes++
					if ctx.Err() != nil {
						t.Error("close inherited operation cancellation")
					}
					if !bytes.Equal(borrowed, []byte("abc")) {
						t.Error("owned data was cleared before original close")
					}
					hold("close")
					return nil
				}}, nil
			}}
			r := request(t)
			type execution struct {
				response p.Response
				err      error
			}
			done := make(chan execution, 1)
			go func() { response, err := handler.Execute(ctx, r); done <- execution{response, err} }()
			await(t, entered)
			cancel()
			select {
			case <-done:
				t.Fatal("cancellation detached original call")
			default:
			}
			unblock()
			var out execution
			select {
			case out = <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("execution did not actually return")
			}
			if phase == "ensure" {
				assertFailure(t, r, out.response, out.err, p.Cancelled, p.CancelledCode)
				if writes != 0 || closes != 0 {
					t.Fatal("cancelled Ensure admitted a write")
				}
			} else {
				assertCommitted(t, r, out.response, out.err, 3, true, p.CancelledCode)
				if writes != 1 || closes != 1 || !bytes.Equal(borrowed, make([]byte, 3)) {
					t.Fatal("original close or owned data retirement missing")
				}
			}
		})
	}
}
