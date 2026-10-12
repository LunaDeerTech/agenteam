package runcommand

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/runner/command"
	"github.com/LunaDeerTech/agenteam/internal/runner/workspace"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type controlledCommandHandle struct {
	run   func(context.Context, string, command.Request, command.Sink) (command.Result, error)
	drain func(context.Context) error
}

func (h *controlledCommandHandle) RunCommand(ctx context.Context, cwd string, r command.Request, sink command.Sink) (command.Result, error) {
	return h.run(ctx, cwd, r, sink)
}
func (h *controlledCommandHandle) Drain(ctx context.Context) error { return h.drain(ctx) }

func commandRequest(t *testing.T) p.Request {
	t.Helper()
	raw, err := p.EncodeRunCommandPayload(p.RunCommandPayload{WorkspaceName: "source", Arguments: p.RunCommandArguments{Mode: "argv", Program: "/bin/echo", Args: []string{"private-argument"}, CWD: "."}})
	if err != nil {
		t.Fatal(err)
	}
	return p.Request{RequestID: "01900000-0000-7000-8000-000000000001", OperationID: "01900000-0000-7000-8000-000000000002", ExecutionID: "01900000-0000-7000-8000-000000000003", ProjectID: "01900000-0000-7000-8000-000000000004", AgentID: "01900000-0000-7000-8000-000000000005", Mount: p.Mount{MountID: "01900000-0000-7000-8000-000000000006", WorkspaceID: "01900000-0000-7000-8000-000000000007"}, OperationName: p.RunCommandOperation, OperationRevision: p.RunCommandRevision, Payload: raw}
}

func report(t *testing.T, r p.Request, response p.Response, err error) p.RunCommandResult {
	t.Helper()
	if err != nil || response.Outcome != p.Success || response.RequestID != r.RequestID || response.OperationID != r.OperationID || response.Code != nil || response.SafeMessage != nil {
		t.Fatal("known invocation report was lost")
	}
	result, err := p.DecodeRunCommandResult(response.Payload)
	if err != nil {
		t.Fatal("invalid known report", err)
	}
	return result
}

func TestRunCommandHandlerRejectsBeforeAdmission(t *testing.T) {
	admitted := 0
	handler := &Handler{ensure: func(context.Context, workspace.Identity) (commandHandle, error) {
		admitted++
		return nil, workspace.ErrUnavailable
	}}
	for _, tc := range []struct {
		name string
		edit func(*p.Request)
		code p.Code
	}{
		{"operation", func(r *p.Request) { r.OperationName = p.ReadFileOperation }, p.UnsupportedOperation},
		{"revision", func(r *p.Request) { r.OperationRevision = "2" }, p.UnsupportedOperation},
		{"request-id", func(r *p.Request) { r.RequestID = "" }, p.InvalidRequest},
		{"mount", func(r *p.Request) { r.Mount.WorkspaceID = "source" }, p.InvalidRequest},
		{"environment-empty", func(r *p.Request) { r.Environment = map[string]string{} }, p.InvalidRequest},
		{"environment-secret", func(r *p.Request) { r.Environment = map[string]string{"SECRET": "private"} }, p.InvalidRequest},
		{"idempotency", func(r *p.Request) { v := "key"; r.IdempotencyKey = &v }, p.InvalidRequest},
		{"shell-mode", func(r *p.Request) {
			r.Payload = []byte(`{"workspace_name":"source","arguments":{"mode":"shell","program":"/bin/sh","args":[],"cwd":"."}}`)
		}, p.InvalidRequest},
		{"outside-cwd", func(r *p.Request) {
			r.Payload = []byte(`{"workspace_name":"source","arguments":{"mode":"argv","program":"/bin/sh","args":[],"cwd":"../outside"}}`)
		}, p.InvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := commandRequest(t)
			tc.edit(&r)
			response, err := handler.Execute(context.Background(), r, func(string, string) error { return nil })
			if err != nil || response.Outcome != p.Failure || response.Code == nil || *response.Code != tc.code || response.Payload != nil {
				t.Fatal("invalid request reached the command profile")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response, err := handler.Execute(ctx, commandRequest(t), func(string, string) error { return nil })
	if err != nil || response.Outcome != p.Cancelled || response.Code == nil || *response.Code != p.CancelledCode || response.Payload != nil {
		t.Fatal("pre-start cancellation changed")
	}
	if admitted != 0 {
		t.Fatal("rejected request acquired a workspace")
	}
	if fmt.Sprint(handler) != "run_command_handler" {
		t.Fatal("default handler projection changed")
	}
}

func TestRunCommandHandlerPreservesKnownWaitFacts(t *testing.T) {
	for _, tc := range []struct {
		name             string
		native           command.Result
		runErr, closeErr error
		outcome          p.Outcome
		code             p.Code
	}{
		{"zero", command.Result{Started: true, Waited: true, ExitCode: 0, OutputComplete: true, ProcessGroupTerminationConfirmed: true}, nil, nil, p.Success, ""},
		{"nonzero", command.Result{Started: true, Waited: true, ExitCode: 23, OutputComplete: true}, nil, nil, p.Success, ""},
		{"signal", command.Result{Started: true, Waited: true, ExitCode: -1, Signal: 9, OutputComplete: true}, nil, nil, p.Success, ""},
		{"cancelled", command.Result{Started: true, Waited: true, ExitCode: -1, Signal: 15, Cancelled: true, OutputComplete: true}, context.Canceled, nil, p.Success, p.CancelledCode},
		{"timeout", command.Result{Started: true, Waited: true, ExitCode: -1, Signal: 9, Cancelled: true, TimedOut: true, OutputComplete: true}, context.DeadlineExceeded, nil, p.Success, p.Timeout},
		{"io-and-cancel", command.Result{Started: true, Waited: true, ExitCode: -1, Signal: 9, Cancelled: true}, errors.Join(command.ErrIO, context.Canceled), nil, p.Success, p.InternalError},
		{"close-error", command.Result{Started: true, Waited: true, ExitCode: 0, OutputComplete: true}, nil, workspace.ErrUnavailable, p.Success, p.InternalError},
		{"unknown-wait", command.Result{Started: true, ExitCode: -1}, command.ErrUnavailable, nil, p.Unknown, ""},
		{"impossible-status", command.Result{Started: true, Waited: true, ExitCode: 7, Signal: 9}, nil, nil, p.Unknown, ""},
		{"start-failure", command.Result{ExitCode: -1}, command.ErrStart, nil, p.Failure, p.InternalError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			drained := false
			h := &controlledCommandHandle{run: func(_ context.Context, cwd string, r command.Request, _ command.Sink) (command.Result, error) {
				if cwd != "." || r.Program != "/bin/echo" || len(r.Args) != 1 || r.Args[0] != "private-argument" {
					t.Fatal("argv or cwd changed")
				}
				return tc.native, tc.runErr
			}, drain: func(ctx context.Context) error {
				if ctx.Err() != nil {
					t.Fatal("cancelled cleanup context")
				}
				drained = true
				return tc.closeErr
			}}
			r := commandRequest(t)
			handler := &Handler{ensure: func(_ context.Context, id workspace.Identity) (commandHandle, error) {
				if id.ProjectID != r.ProjectID || id.AgentID != r.AgentID || id.Name != "source" {
					t.Fatal("mapping changed")
				}
				return h, nil
			}}
			response, err := handler.Execute(context.Background(), r, func(string, string) error { return nil })
			if !drained || err != nil || response.Outcome != tc.outcome {
				t.Fatal("status or actual Drain lost")
			}
			if tc.outcome == p.Success {
				v := report(t, r, response, err)
				if v.ExitCode != tc.native.ExitCode || v.Signal != tc.native.Signal || v.Cancelled != tc.native.Cancelled || v.TimedOut != tc.native.TimedOut || v.OutputComplete != tc.native.OutputComplete || v.ProcessGroupTerminationConfirmed != tc.native.ProcessGroupTerminationConfirmed {
					t.Fatal("known native facts changed")
				}
				if tc.code == "" && v.CompletionCode != nil || tc.code != "" && (v.CompletionCode == nil || *v.CompletionCode != tc.code) {
					t.Fatal("completion code changed")
				}
			} else if response.Payload != nil {
				t.Fatal("non-success carried status payload")
			}
		})
	}
}

func waitCommandSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("original callback did not reach barrier")
	}
}

func TestRunCommandHandlerWaitsForOriginalReturnAndDrain(t *testing.T) {
	entered, cancelled, releaseRun, draining, releaseDrain := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	returned := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	t.Cleanup(func() {
		cancel()
		select {
		case <-releaseRun:
		default:
			close(releaseRun)
		}
		select {
		case <-releaseDrain:
		default:
			close(releaseDrain)
		}
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Error("original Handler goroutine did not return after cleanup")
		}
	})
	h := &controlledCommandHandle{run: func(ctx context.Context, _ string, _ command.Request, sink command.Sink) (command.Result, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-releaseRun
		if sink(ctx, command.Stdout, []byte("tail")) != nil {
			return command.Result{}, command.ErrIO
		}
		return command.Result{Started: true, Waited: true, ExitCode: -1, Signal: 15, Cancelled: true, StdoutBytes: 4, OutputComplete: true}, context.Canceled
	}, drain: func(ctx context.Context) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		close(draining)
		<-releaseDrain
		return nil
	}}
	handler := &Handler{ensure: func(context.Context, workspace.Identity) (commandHandle, error) { return h, nil }}
	type answer struct {
		response p.Response
		err      error
	}
	done := make(chan answer, 1)
	r := commandRequest(t)
	go func() {
		defer close(returned)
		response, err := handler.Execute(ctx, r, func(string, string) error { return errors.New("cancelled tail reached emitter") })
		done <- answer{response, err}
	}()
	waitCommandSignal(t, entered)
	cancel()
	waitCommandSignal(t, cancelled)
	select {
	case <-done:
		t.Fatal("cancellation detached the original run")
	default:
	}
	close(releaseRun)
	waitCommandSignal(t, draining)
	select {
	case <-done:
		t.Fatal("native Wait bypassed original Handle Drain")
	default:
	}
	close(releaseDrain)
	select {
	case a := <-done:
		v := report(t, r, a.response, a.err)
		if !v.Cancelled || v.OutputComplete || v.StdoutBytes != "0" || v.CompletionCode == nil || *v.CompletionCode != p.CancelledCode {
			t.Fatal("discarded tail or cancellation was misreported")
		}
	case <-time.After(time.Second):
		t.Fatal("actual released owner did not return")
	}
}

func TestRunCommandHandlerRetainsEnsureAndEmitterFailures(t *testing.T) {
	t.Run("partial-ensure", func(t *testing.T) {
		drained := false
		h := &controlledCommandHandle{drain: func(context.Context) error { drained = true; return nil }}
		handler := &Handler{ensure: func(context.Context, workspace.Identity) (commandHandle, error) { return h, workspace.ErrUnavailable }}
		r := commandRequest(t)
		response, err := handler.Execute(context.Background(), r, func(string, string) error { return nil })
		if !drained || err != nil || response.Outcome != p.Failure || response.Code == nil || *response.Code != p.CapabilityUnavailable {
			t.Fatal("partial Ensure did not join")
		}
	})
	for _, damaged := range []bool{false, true} {
		name := "transport"
		if damaged {
			name = "invalid-utf8"
		}
		t.Run(name, func(t *testing.T) {
			drained, failed := false, false
			h := &controlledCommandHandle{run: func(ctx context.Context, _ string, _ command.Request, sink command.Sink) (command.Result, error) {
				raw := []byte("text")
				if damaged {
					raw = []byte{0xff}
				}
				failed = sink(ctx, command.Stdout, raw) != nil
				return command.Result{Started: true, Waited: true, ExitCode: -1, Signal: 15}, command.ErrIO
			}, drain: func(context.Context) error { drained = true; return nil }}
			handler := &Handler{ensure: func(context.Context, workspace.Identity) (commandHandle, error) { return h, nil }}
			r := commandRequest(t)
			response, err := handler.Execute(context.Background(), r, func(string, string) error { return errors.New("transport failure") })
			if !drained || !failed || err != nil {
				t.Fatal("output error lost original join")
			}
			if damaged {
				v := report(t, r, response, err)
				if v.OutputComplete || v.Cancelled || v.CompletionCode == nil || *v.CompletionCode != p.InternalError {
					t.Fatal("invalid bytes claimed cancellation or complete output")
				}
			} else if response.Outcome != p.Unknown || response.Payload != nil {
				t.Fatal("transport failure became a successful terminal")
			}
		})
	}
}
