package runnerhttp

import (
	"context"
	"testing"
	"time"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func startDirectorySession(t *testing.T) (*controlSession, *pureSessionSocket) {
	t.Helper()
	socket := newPureSessionSocket()
	s, _, _ := startPureCentralSession(t, &pureSessionAuthority{}, socket, pureControlGate, false)
	sessionInput(t, socket, p.Hello{RunnerID: s.runner, RunnerVersion: "unit", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{p.ListDirectoryOperation, p.FindFilesOperation}, FeatureFlags: []string{}})
	if sessionOutput(t, socket).Type() != p.HelloAckType {
		t.Fatal("missing hello")
	}
	sessionInput(t, socket, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-11T00:00:00Z"})
	if sessionOutput(t, socket).Type() != p.HeartbeatAckType {
		t.Fatal("missing heartbeat")
	}
	return s, socket
}
func directoryTestRequest(t *testing.T, operation string) p.Request {
	t.Helper()
	in := readFileTestRequest(t)
	args := p.DirectoryArguments{Path: "src", Limit: 2}
	if operation == p.FindFilesOperation {
		args.Pattern = "*.go"
		args.Match = "name"
	}
	raw, err := p.EncodeDirectoryPayload(operation, p.DirectoryPayload{WorkspaceName: "source", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	in.OperationName = operation
	in.OperationRevision = p.DirectoryRevision
	in.Payload = raw
	return in
}
func TestDirectoryDispatchWaitsForTerminalAfterCancel(t *testing.T) {
	for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
		t.Run(operation, func(t *testing.T) {
			s, socket := startDirectorySession(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			type answer struct {
				result controlResult
				err    error
			}
			done := make(chan answer, 1)
			in := directoryTestRequest(t, operation)
			go func() { r, e := s.dispatchDirectory(ctx, in); done <- answer{r, e} }()
			wire := sessionOutput(t, socket).Payload().(p.Request)
			if wire.OperationName != operation || wire.OperationID != in.OperationID || wire.RequestID == "" {
				t.Fatal("wrong original operation")
			}
			cancel()
			message := sessionOutput(t, socket)
			if message.Type() != p.CancelType || message.Payload().(p.Cancel).RequestID != wire.RequestID {
				t.Fatal("wrong cancellation")
			}
			select {
			case <-done:
				t.Fatal("Cancel enqueue mistaken for operation join")
			default:
			}
			code := p.CancelledCode
			messageText := code.SafeMessage()
			sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Cancelled, Code: &code, SafeMessage: &messageText})
			select {
			case out := <-done:
				if out.err != nil || !out.result.attempted || out.result.terminal == nil || out.result.terminal.Outcome != p.Cancelled {
					t.Fatal("original terminal not consumed", out.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("terminal did not join")
			}
		})
	}
}
func TestDirectoryDispatchChecksResultAgainstOriginalRequest(t *testing.T) {
	for _, test := range []struct {
		name, operation, payload string
		valid                    bool
	}{
		{"list", p.ListDirectoryOperation, `{"path":"src","entries":[{"name":"a.go","kind":"file"}],"truncated":false}`, true},
		{"find", p.FindFilesOperation, `{"path":"src","paths":["src/a.go"],"truncated":false}`, true},
		{"list-path", p.ListDirectoryOperation, `{"path":"other","entries":[],"truncated":false}`, false},
		{"list-limit", p.ListDirectoryOperation, `{"path":"src","entries":[{"name":"a","kind":"file"},{"name":"b","kind":"file"},{"name":"c","kind":"file"}],"truncated":false}`, false},
		{"list-short-truncated", p.ListDirectoryOperation, `{"path":"src","entries":[],"truncated":true}`, false},
		{"find-outside-start", p.FindFilesOperation, `{"path":"src","paths":["other/a.go"],"truncated":false}`, false},
		{"find-pattern", p.FindFilesOperation, `{"path":"src","paths":["src/a.txt"],"truncated":false}`, false},
		{"find-short-truncated", p.FindFilesOperation, `{"path":"src","paths":["src/a.go"],"truncated":true}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, socket := startDirectorySession(t)
			type answer struct {
				result controlResult
				err    error
			}
			done := make(chan answer, 1)
			input := directoryTestRequest(t, test.operation)
			go func() {
				r, e := s.dispatchDirectory(context.Background(), input)
				done <- answer{r, e}
			}()
			wire := sessionOutput(t, socket).Payload().(p.Request)
			sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Success, Payload: []byte(test.payload)})
			select {
			case out := <-done:
				if !out.result.attempted {
					t.Fatal("actual attempt lost")
				}
				if test.valid {
					if out.err != nil || out.result.terminal == nil {
						t.Fatal("valid result rejected", out.err)
					}
				} else if out.err == nil || out.result.terminal != nil {
					t.Fatal("unbound result escaped")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("result did not join")
			}
		})
	}
}
func TestDirectoryCurrentCapabilityAndRevisionRejectBeforeAttempt(t *testing.T) {
	s, socket := startDirectorySession(t)
	sessionInput(t, socket, p.RunnerStatus{Headless: true, Capabilities: []string{p.FindFilesOperation}})
	sessionInput(t, socket, p.Heartbeat{Sequence: "2", RunnerTime: "2026-10-11T00:00:00Z"})
	if sessionOutput(t, socket).Type() != p.HeartbeatAckType {
		t.Fatal("status ordering barrier missing")
	}
	s.mu.Lock()
	valid := !s.listDirectory && s.findFiles && !s.readFile
	s.mu.Unlock()
	if !valid {
		t.Fatal("current authenticated capabilities not retained")
	}
	out, err := s.dispatchDirectory(context.Background(), directoryTestRequest(t, p.ListDirectoryOperation))
	if err == nil || out.attempted || out.terminal != nil {
		t.Fatal("removed capability attempted")
	}
	in := directoryTestRequest(t, p.FindFilesOperation)
	in.OperationRevision = "2"
	out, err = s.dispatchDirectory(context.Background(), in)
	if err == nil || out.attempted || out.terminal != nil {
		t.Fatal("wrong revision attempted")
	}
}
