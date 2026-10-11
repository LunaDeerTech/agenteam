package runnerhttp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
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
	return p.Request{RequestID: id(), OperationID: id(), ExecutionID: id(), ProjectID: id(), AgentID: id(), Mount: p.Mount{MountID: id(), WorkspaceID: id()}, OperationName: p.ReadFileOperation, OperationRevision: p.ReadFileRevision, Payload: raw}
}

func TestReadFilePreparedCallRejectsUnboundAndCopiedReuse(t *testing.T) {
	hasCode := func(err error, code f.Code) bool {
		var fault *f.Fault
		return errors.As(err, &fault) && fault.Code == code && fault.CommitState == f.NotStarted
	}
	var absent *ReadFileDispatcher
	if call, err := absent.Prepare(mount.ReadFileTicket{}); call != nil || !hasCode(err, f.DependencyUnbound) {
		t.Fatal("unbound dispatcher prepared a call", err)
	}
	d := &ReadFileDispatcher{device: &DeviceHandler{}, mounts: &mount.ReadFileResolver{}}
	if call, err := d.Prepare(mount.ReadFileTicket{}); call != nil || !hasCode(err, f.InvalidArgument) {
		t.Fatal("empty ticket prepared a call", err)
	}
	for _, call := range []*ReadFileCall{nil, {}} {
		if call.RequestID() != "" {
			t.Fatal("empty call invented a request identity")
		}
		if result, err := call.Dispatch(context.Background()); result.Attempted || result.Terminal != nil || !hasCode(err, f.InvalidArgument) {
			t.Fatal("empty call was admitted", err)
		}
	}
	// Even a pre-admission failure consumes the opaque call. A caller copying
	// the public value cannot obtain a second attempt from the same identity.
	requestID := readFileTestRequest(t).RequestID
	call := &ReadFileCall{state: &readFileCallState{requestID: requestID}}
	copied := *call
	outcomes := make(chan error, 2)
	for _, value := range []*ReadFileCall{call, &copied} {
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

func TestReadFileDispatchRejectsMissingPreparedIdentity(t *testing.T) {
	s, socket, _, _ := startReadFileSession(t)
	input := readFileTestRequest(t)
	input.RequestID = ""
	result, err := s.dispatchReadFile(context.Background(), input)
	if err != errControlProtocol || result.attempted || result.terminal != nil {
		t.Fatal("dispatch generated a replacement request identity", err)
	}
	select {
	case <-socket.out:
		t.Fatal("missing prepared identity reached the wire")
	default:
	}
}

func TestReadFileDispatchRejectsPreparedIdentityReuse(t *testing.T) {
	s, socket, _, _ := startReadFileSession(t)
	input := readFileTestRequest(t)
	type answer struct {
		result controlResult
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		result, err := s.dispatchReadFile(context.Background(), input)
		done <- answer{result, err}
	}()
	wire := sessionOutput(t, socket).Payload().(p.Request)
	for _, phase := range []string{"pending", "completed"} {
		repeated := make(chan answer, 1)
		go func() {
			result, err := s.dispatchReadFile(context.Background(), input)
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
			if wire.RequestID != input.RequestID {
				t.Fatal("prepared identity was replaced at dispatch")
			}
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

func TestReadFileDispatchWaitsForOriginalFrameAfterTerminal(t *testing.T) {
	s, socket, _, _ := startReadFileSession(t)
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
	input := readFileTestRequest(t)
	go func() {
		defer close(returned)
		r, err := s.dispatchReadFile(ctx, input)
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
