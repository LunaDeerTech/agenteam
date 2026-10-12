package runnerhttp

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func startWriteFileSession(t *testing.T) (*controlSession, *pureSessionSocket, context.CancelFunc, <-chan error) {
	t.Helper()
	socket := newPureSessionSocket()
	s, cancel, done := startPureCentralSession(t, &pureSessionAuthority{}, socket, pureControlGate, false)
	sessionInput(t, socket, p.Hello{RunnerID: s.runner, RunnerVersion: "unit", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{p.WriteFileOperation}, FeatureFlags: []string{}})
	if sessionOutput(t, socket).Type() != p.HelloAckType {
		t.Fatal("missing hello")
	}
	sessionInput(t, socket, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"})
	if sessionOutput(t, socket).Type() != p.HeartbeatAckType {
		t.Fatal("missing heartbeat")
	}
	return s, socket, cancel, done
}
func writeFileTestRequest(t *testing.T) p.Request {
	t.Helper()
	id := func() p.ID {
		v, e := p.NewID()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	raw, e := p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: "source", Arguments: p.WriteFileArguments{Path: "notes.txt", Data: []byte("abc"), Overwrite: true}})
	if e != nil {
		t.Fatal(e)
	}
	return p.Request{RequestID: id(), OperationID: id(), ExecutionID: id(), ProjectID: id(), AgentID: id(), Mount: p.Mount{MountID: id(), WorkspaceID: id()}, OperationName: p.WriteFileOperation, OperationRevision: p.WriteFileRevision, Payload: raw}
}

func TestWriteFilePreparedCallRejectsUnboundAndCopiedReuse(t *testing.T) {
	hasCode := func(err error, code f.Code) bool {
		var fault *f.Fault
		return errors.As(err, &fault) && fault.Code == code && fault.CommitState == f.NotStarted
	}
	var absent *WriteFileDispatcher
	if call, err := absent.Prepare(mount.WriteFileTicket{}); call != nil || !hasCode(err, f.DependencyUnbound) {
		t.Fatal("unbound dispatcher prepared a call", err)
	}
	d := &WriteFileDispatcher{device: &DeviceHandler{}, mounts: &mount.WriteFileResolver{}}
	if call, err := d.Prepare(mount.WriteFileTicket{}); call != nil || !hasCode(err, f.InvalidArgument) {
		t.Fatal("empty ticket prepared a call", err)
	}
	for _, call := range []*WriteFileCall{nil, {}} {
		if call.RequestID() != "" {
			t.Fatal("empty call invented a request identity")
		}
		if result, err := call.Dispatch(context.Background()); result.Attempted || result.Terminal != nil || !hasCode(err, f.InvalidArgument) {
			t.Fatal("empty call was admitted", err)
		}
	}
	// Even a pre-admission failure consumes the opaque call. A caller copying
	// the public value cannot obtain a second attempt from the same identity.
	requestID := writeFileTestRequest(t).RequestID
	call := &WriteFileCall{state: &writeFileCallState{requestID: requestID}}
	copied := *call
	outcomes := make(chan error, 2)
	for _, value := range []*WriteFileCall{call, &copied} {
		go func() { _, err := value.Dispatch(context.Background()); outcomes <- err }()
	}
	unbound, reused := 0, 0
	for range 2 {
		select {
		case err := <-outcomes:
			if hasCode(err, f.DependencyUnbound) {
				unbound++
			} else if hasCode(err, f.InvalidState) {
				reused++
			} else {
				t.Fatal("unexpected consumption outcome", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("copied call did not return")
		}
	}
	if unbound != 1 || reused != 1 || call.RequestID() != requestID || copied.RequestID() != requestID {
		t.Fatal("copied call replaced identity or admission")
	}
}

func TestWriteFileDispatchRequiresOriginalIdentityAndCurrentCapability(t *testing.T) {
	for _, mode := range []string{"missing-rid", "read-operation", "wrong-revision", "invalid-payload", "read-only-hello", "status-cleared"} {
		t.Run(mode, func(t *testing.T) {
			var s *controlSession
			var socket *pureSessionSocket
			if mode == "read-only-hello" {
				s, socket, _, _ = startReadFileSession(t)
			} else {
				s, socket, _, _ = startWriteFileSession(t)
			}
			input := writeFileTestRequest(t)
			want := errControlProtocol
			switch mode {
			case "missing-rid":
				input.RequestID = ""
			case "read-operation":
				input.OperationName = p.ReadFileOperation
			case "wrong-revision":
				input.OperationRevision = "2"
			case "invalid-payload":
				input.Payload = []byte(`{"workspace_name":"source","arguments":{}}`)
			case "read-only-hello":
				want = errControlClosed
			case "status-cleared":
				if err := s.receive(context.Background(), pureControlMessage(t, p.RunnerStatus{Capabilities: []string{p.ReadFileOperation}})); err != nil {
					t.Fatal(err)
				}
				want = errControlClosed
			}
			result, err := s.dispatchWriteFile(context.Background(), input)
			if err != want || result.attempted || result.terminal != nil {
				t.Fatal("unadmitted write produced an attempt", mode, err)
			}
			select {
			case <-socket.out:
				t.Fatal("rejected write reached actual wire")
			default:
			}
		})
	}
}

func TestWriteFileDispatchRejectsPreparedIdentityReuse(t *testing.T) {
	s, socket, _, _ := startWriteFileSession(t)
	input := writeFileTestRequest(t)
	type answer struct {
		result controlResult
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		result, err := s.dispatchWriteFile(context.Background(), input)
		done <- answer{result, err}
	}()
	wire := sessionOutput(t, socket).Payload().(p.Request)
	for _, phase := range []string{"pending", "completed"} {
		repeated := make(chan answer, 1)
		go func() {
			result, err := s.dispatchWriteFile(context.Background(), input)
			repeated <- answer{result, err}
		}()
		select {
		case out := <-repeated:
			if out.err != errControlProtocol || out.result.attempted || out.result.terminal != nil {
				t.Fatal("prepared identity reused", phase, out.err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("duplicate identity was not refused before enqueue", phase)
		}
		select {
		case <-socket.out:
			t.Fatal("duplicate identity reached the wire", phase)
		default:
		}
		if phase == "pending" {
			code := p.CancelledCode
			message := code.SafeMessage()
			sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Cancelled, Code: &code, SafeMessage: &message})
			select {
			case out := <-done:
				if out.err != nil || !out.result.attempted || out.result.terminal == nil || out.result.terminal.RequestID != input.RequestID {
					t.Fatal("duplicate displaced the original call", out.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("original call was not joined")
			}
		}
	}
}

func TestWriteFileDispatchWaitsForOriginalTerminalAfterCancel(t *testing.T) {
	s, socket, _, _ := startWriteFileSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type answer struct {
		r controlResult
		e error
	}
	done := make(chan answer, 1)
	input := writeFileTestRequest(t)
	go func() { r, e := s.dispatchWriteFile(ctx, input); done <- answer{r, e} }()
	wire := sessionOutput(t, socket).Payload().(p.Request)
	if wire.RequestID != input.RequestID || wire.OperationID != input.OperationID {
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
	code := p.CancelledCode
	raw, err := p.EncodeWriteFileResult(p.WriteFileResult{Path: "notes.txt", State: "committed", BytesWritten: 3, Durable: false, CompletionCode: &code})
	if err != nil {
		t.Fatal(err)
	}
	sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Success, Payload: raw})
	select {
	case out := <-done:
		if out.e != nil || !out.r.attempted || out.r.terminal == nil || out.r.terminal.Outcome != p.Success {
			t.Fatal("post-cancellation known publication lost", out.e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("original terminal not joined")
	}
}

func TestWriteFileDispatchPreservesPublicationAndUnknown(t *testing.T) {
	for _, mode := range []string{"success", "maximum-data", "durability-warning", "postcommit-warning", "wrong-path", "wrong-length", "disconnect", "explicit-unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, socket, cancel, owner := startWriteFileSession(t)
			input := writeFileTestRequest(t)
			expected := []byte("abc")
			if mode == "maximum-data" {
				expected = bytes.Repeat([]byte{0xff}, p.MaxWriteFileBytes)
				var err error
				input.Payload, err = p.EncodeWriteFilePayload(p.WriteFilePayload{WorkspaceName: "source", Arguments: p.WriteFileArguments{Path: "notes.txt", Data: expected, Overwrite: true}})
				if err != nil {
					t.Fatal(err)
				}
			}
			type answer struct {
				r controlResult
				e error
			}
			done := make(chan answer, 1)
			go func() { r, e := s.dispatchWriteFile(context.Background(), input); done <- answer{r, e} }()
			message := sessionOutput(t, socket)
			wire := message.Payload().(p.Request)
			actual, err := p.DecodeWriteFilePayload(wire.Payload)
			if err != nil || wire.RequestID != input.RequestID || wire.OperationID != input.OperationID || !bytes.Equal(actual.Arguments.Data, expected) || !actual.Arguments.Overwrite {
				t.Fatal("original payload or prepared identity changed", err)
			}
			clear(actual.Arguments.Data)
			encoded, err := p.Encode(message)
			if err != nil || len(encoded) > p.MaxMessageBytes {
				t.Fatal("actual whole frame escaped protocol bound", err)
			}
			clear(encoded)
			result := p.WriteFileResult{Path: "notes.txt", State: "committed", BytesWritten: len(expected), Durable: true}
			switch mode {
			case "disconnect":
				cancel()
				<-owner
			case "explicit-unknown":
				msg := p.UnknownMessage
				sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Unknown, SafeMessage: &msg})
			default:
				if mode == "wrong-path" {
					result.Path = "other.txt"
				}
				if mode == "wrong-length" {
					result.BytesWritten--
				}
				if mode == "durability-warning" || mode == "postcommit-warning" {
					code := p.InternalError
					result.CompletionCode = &code
					result.Durable = mode == "postcommit-warning"
				}
				raw, e := p.EncodeWriteFileResult(result)
				if e != nil {
					t.Fatal(e)
				}
				sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Success, Payload: raw})
			}
			select {
			case out := <-done:
				if !out.r.attempted {
					t.Fatal("actual send was lost")
				}
				switch mode {
				case "wrong-path", "wrong-length", "disconnect":
					if out.e == nil || out.r.terminal != nil {
						t.Fatal("unverified write outcome was published")
					}
					if mode == "disconnect" {
						var fault *f.Fault
						if !errors.As(out.e, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown {
							t.Fatal("disconnect was downgraded to not dispatched", out.e)
						}
					}
				case "explicit-unknown":
					if out.e != nil || out.r.terminal == nil || out.r.terminal.Outcome != p.Unknown || len(out.r.terminal.Payload) != 0 {
						t.Fatal("unknown was lost or invented a publication", out.e)
					}
				default:
					if out.e != nil || out.r.terminal == nil || out.r.terminal.Outcome != p.Success {
						t.Fatal("known publication rejected", out.e)
					}
					got, e := p.DecodeWriteFileResult(out.r.terminal.Payload)
					if e != nil || got.Path != result.Path || got.State != result.State || got.BytesWritten != result.BytesWritten || got.Durable != result.Durable || (got.CompletionCode == nil) != (result.CompletionCode == nil) {
						t.Fatal("publication facts changed", e)
					}
					if got.CompletionCode != nil && *got.CompletionCode != *result.CompletionCode {
						t.Fatal("completion warning changed")
					}
				}
			case <-time.After(2 * time.Second):
				t.Fatal("original call did not return")
			}
		})
	}
}

func TestWriteFileDispatchWaitsForOriginalFrameAfterTerminal(t *testing.T) {
	s, socket, _, _ := startWriteFileSession(t)
	entered, release, writeReturned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	socket.writeFn = func(_ context.Context, raw []byte) error {
		message, err := p.Decode(raw)
		if err != nil {
			return err
		}
		if message.Type() == p.RequestType {
			close(entered)
			<-release
			close(writeReturned)
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	type answer struct {
		result controlResult
		err    error
	}
	done, returned := make(chan answer, 1), make(chan struct{})
	input := writeFileTestRequest(t)
	go func() {
		defer close(returned)
		r, err := s.dispatchWriteFile(ctx, input)
		done <- answer{r, err}
	}()
	t.Cleanup(func() {
		cancel()
		releaseOnce.Do(func() { close(release) })
		select {
		case <-returned:
		case <-time.After(2 * time.Second):
			t.Error("original dispatch did not return")
		}
	})
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("original frame did not enter write")
	}
	s.mu.Lock()
	call := s.pending[input.RequestID]
	s.mu.Unlock()
	if call == nil {
		t.Fatal("prepared identity missing from original pending call")
	}
	code := p.CancelledCode
	message := code.SafeMessage()
	sessionInput(t, socket, p.Response{RequestID: input.RequestID, OperationID: input.OperationID, Outcome: p.Cancelled, Code: &code, SafeMessage: &message})
	select {
	case <-call.done:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal not consumed while original write is held")
	}
	select {
	case <-done:
		t.Fatal("terminal receipt was mistaken for actual frame return")
	case <-time.After(15 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case out := <-done:
		if out.err != nil || !out.result.attempted || out.result.terminal == nil || out.result.terminal.RequestID != input.RequestID {
			t.Fatal("original joined terminal lost", out.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("released original frame did not join")
	}
	select {
	case <-writeReturned:
	default:
		t.Fatal("dispatch returned before native write")
	}
	if wire := sessionOutput(t, socket).Payload().(p.Request); wire.RequestID != input.RequestID {
		t.Fatal("actual frame used a different prepared identity")
	}
}
