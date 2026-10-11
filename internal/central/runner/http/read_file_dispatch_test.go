package runnerhttp

import (
	"context"
	"testing"
	"time"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func startReadFileSession(t *testing.T) (*controlSession, *pureSessionSocket, context.CancelFunc, <-chan error) {
	t.Helper()
	socket := newPureSessionSocket()
	s, cancel, done := startPureCentralSession(t, &pureSessionAuthority{}, socket, pureControlGate, false)
	sessionInput(t, socket, p.Hello{RunnerID: s.runner, RunnerVersion: "unit", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{p.ReadFileOperation}, FeatureFlags: []string{}})
	if sessionOutput(t, socket).Type() != p.HelloAckType {
		t.Fatal("missing hello")
	}
	sessionInput(t, socket, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"})
	if sessionOutput(t, socket).Type() != p.HeartbeatAckType {
		t.Fatal("missing heartbeat")
	}
	return s, socket, cancel, done
}
func readFileTestRequest(t *testing.T) p.Request {
	t.Helper()
	id := func() p.ID {
		v, e := p.NewID()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	raw, e := p.EncodeReadFilePayload(p.ReadFilePayload{WorkspaceName: "source", Arguments: p.ReadFileArguments{Path: "notes.txt", Length: 8, Encoding: "utf8"}})
	if e != nil {
		t.Fatal(e)
	}
	return p.Request{OperationID: id(), ExecutionID: id(), ProjectID: id(), AgentID: id(), Mount: p.Mount{MountID: id(), WorkspaceID: id()}, OperationName: p.ReadFileOperation, OperationRevision: p.ReadFileRevision, Payload: raw}
}

func TestReadFileDispatchWaitsForOriginalTerminalAfterCancel(t *testing.T) {
	s, socket, _, _ := startReadFileSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type answer struct {
		r controlResult
		e error
	}
	done := make(chan answer, 1)
	input := readFileTestRequest(t)
	go func() { r, e := s.dispatchReadFile(ctx, input); done <- answer{r, e} }()
	wire := sessionOutput(t, socket).Payload().(p.Request)
	if wire.RequestID == "" || wire.OperationID != input.OperationID {
		t.Fatal("attempt identity")
	}
	cancel()
	message := sessionOutput(t, socket)
	if message.Type() != p.CancelType || message.Payload().(p.Cancel).RequestID != wire.RequestID {
		t.Fatal("original cancellation missing")
	}
	select {
	case <-done:
		t.Fatal("Cancel enqueue was mistaken for join")
	default:
	}
	code, messageText := p.CancelledCode, p.CancelledCode.SafeMessage()
	sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Cancelled, Code: &code, SafeMessage: &messageText})
	select {
	case out := <-done:
		if out.e != nil || !out.r.attempted || out.r.terminal == nil || out.r.terminal.Outcome != p.Cancelled {
			t.Fatal("actual terminal lost", out.e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("original terminal not joined")
	}
}

func TestReadFileDispatchChecksResultAndDisconnectUnknown(t *testing.T) {
	for _, mode := range []string{"success", "wrong-path", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			s, socket, cancel, owner := startReadFileSession(t)
			input := readFileTestRequest(t)
			type answer struct {
				r controlResult
				e error
			}
			done := make(chan answer, 1)
			go func() { r, e := s.dispatchReadFile(context.Background(), input); done <- answer{r, e} }()
			wire := sessionOutput(t, socket).Payload().(p.Request)
			if mode == "disconnect" {
				cancel()
				<-owner
			} else {
				path := "notes.txt"
				if mode == "wrong-path" {
					path = "other.txt"
				}
				raw, e := p.EncodeReadFileResult(p.ReadFileResult{Path: path, Offset: 0, BytesRead: 3, EOF: true, Encoding: "utf8", Data: "abc"})
				if e != nil {
					t.Fatal(e)
				}
				sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Success, Payload: raw})
			}
			select {
			case out := <-done:
				if !out.r.attempted {
					t.Fatal("actual attempt lost")
				}
				if mode == "success" {
					if out.e != nil || out.r.terminal == nil {
						t.Fatal("success rejected", out.e)
					}
				} else if out.e == nil || out.r.terminal != nil {
					t.Fatal("unverified result published")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("call did not actually return")
			}
		})
	}
}
