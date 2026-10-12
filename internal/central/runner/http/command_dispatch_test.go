package runnerhttp

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func startCommandSession(t *testing.T) (*controlSession, *pureSessionSocket, context.CancelFunc, <-chan error) {
	t.Helper()
	socket := newPureSessionSocket()
	s, cancel, done := startPureCentralSession(t, &pureSessionAuthority{}, socket, pureControlGate, false)
	sessionInput(t, socket, p.Hello{RunnerID: s.runner, RunnerVersion: "unit", ProtocolVersion: p.CurrentVersion(), OS: "linux", Arch: "amd64", Headless: true, Capabilities: []string{p.RunCommandOperation}, FeatureFlags: []string{}})
	if sessionOutput(t, socket).Type() != p.HelloAckType {
		t.Fatal("missing hello")
	}
	sessionInput(t, socket, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"})
	if sessionOutput(t, socket).Type() != p.HeartbeatAckType {
		t.Fatal("missing heartbeat")
	}
	return s, socket, cancel, done
}
func commandTestRequest(t *testing.T) p.Request {
	t.Helper()
	id := func() p.ID {
		v, e := p.NewID()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	raw, e := p.EncodeRunCommandPayload(p.RunCommandPayload{WorkspaceName: "source", Arguments: p.RunCommandArguments{Mode: "argv", Program: "/bin/echo", Args: []string{"private argument"}, CWD: "."}})
	if e != nil {
		t.Fatal(e)
	}
	return p.Request{RequestID: id(), OperationID: id(), ExecutionID: id(), ProjectID: id(), AgentID: id(), Mount: p.Mount{MountID: id(), WorkspaceID: id()}, OperationName: p.RunCommandOperation, OperationRevision: p.RunCommandRevision, Payload: raw}
}

func TestCommandPreparedCallRejectsUnboundAndCopiedReuse(t *testing.T) {
	hasCode := func(err error, code f.Code) bool {
		var fault *f.Fault
		return errors.As(err, &fault) && fault.Code == code && fault.CommitState == f.NotStarted
	}
	var absent *CommandDispatcher
	if call, err := absent.Prepare(mount.CommandTicket{}); call != nil || !hasCode(err, f.DependencyUnbound) {
		t.Fatal("unbound dispatcher prepared a call", err)
	}
	d := &CommandDispatcher{device: &DeviceHandler{}, mounts: &mount.CommandResolver{}}
	if call, err := d.Prepare(mount.CommandTicket{}); call != nil || !hasCode(err, f.InvalidArgument) {
		t.Fatal("empty ticket prepared a call", err)
	}
	for _, call := range []*CommandCall{nil, {}} {
		if call.RequestID() != "" {
			t.Fatal("empty call invented a request identity")
		}
		if result, err := call.Dispatch(context.Background()); result.Attempted || result.Terminal != nil || !hasCode(err, f.InvalidArgument) {
			t.Fatal("empty call was admitted", err)
		}
	}
	// Even a pre-admission failure consumes the opaque call. A caller copying
	// the public value cannot obtain a second attempt from the same identity.
	requestID := commandTestRequest(t).RequestID
	call := &CommandCall{state: &commandCallState{requestID: requestID}}
	copied := *call
	outcomes := make(chan error, 2)
	for _, value := range []*CommandCall{call, &copied} {
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

func TestCommandDispatchRequiresOriginalIdentityAndCurrentCapability(t *testing.T) {
	for _, mode := range []string{"missing-rid", "read-operation", "wrong-revision", "invalid-payload", "read-only-hello", "status-cleared", "environment", "idempotency"} {
		t.Run(mode, func(t *testing.T) {
			var s *controlSession
			var socket *pureSessionSocket
			if mode == "read-only-hello" {
				s, socket, _, _ = startReadFileSession(t)
			} else {
				s, socket, _, _ = startCommandSession(t)
			}
			input := commandTestRequest(t)
			want := errControlProtocol
			switch mode {
			case "missing-rid":
				input.RequestID = ""
			case "read-operation":
				input.OperationName = p.ReadFileOperation
			case "wrong-revision":
				input.OperationRevision = "2"
			case "environment":
				input.Environment = map[string]string{}
			case "idempotency":
				key := "caller_key"
				input.IdempotencyKey = &key
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
			result, err := s.dispatchCommand(context.Background(), input)
			if err != want || result.Attempted || result.Terminal != nil {
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

func TestCommandDispatchRejectsPreparedIdentityReuse(t *testing.T) {
	s, socket, _, _ := startCommandSession(t)
	input := commandTestRequest(t)
	type answer struct {
		result CommandDispatchResult
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		result, err := s.dispatchCommand(context.Background(), input)
		done <- answer{result, err}
	}()
	wire := sessionOutput(t, socket).Payload().(p.Request)
	for _, phase := range []string{"pending", "completed"} {
		repeated := make(chan answer, 1)
		go func() {
			result, err := s.dispatchCommand(context.Background(), input)
			repeated <- answer{result, err}
		}()
		select {
		case out := <-repeated:
			if out.err != errControlProtocol || out.result.Attempted || out.result.Terminal != nil {
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
				if out.err != nil || !out.result.Attempted || out.result.Terminal == nil || out.result.Terminal.RequestID != input.RequestID {
					t.Fatal("duplicate displaced the original call", out.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("original call was not joined")
			}
		}
	}
}

type commandAnswer struct {
	r CommandDispatchResult
	e error
}

func startCommandDispatch(t *testing.T, s *controlSession, ctx context.Context, in p.Request) <-chan commandAnswer {
	done := make(chan commandAnswer, 1)
	returned := make(chan struct{})
	go func() { defer close(returned); r, e := s.dispatchCommand(ctx, in); done <- commandAnswer{r, e} }()
	t.Cleanup(func() {
		s.wire.stop()
		select {
		case <-returned:
		case <-time.After(3 * time.Second):
			t.Error("original command call did not retire")
		}
	})
	return done
}
func commandStream(in p.Request, stream string, seq int, data string) p.Stream {
	return p.Stream{RequestID: in.RequestID, OperationID: in.OperationID, Stream: stream, Sequence: p.Decimal(strconv.Itoa(seq)), Data: data, Timestamp: "2026-10-09T00:00:00Z"}
}
func commandReport(t *testing.T, in p.Request, out, errout string, change func(*p.RunCommandResult)) p.Response {
	t.Helper()
	report := p.RunCommandResult{State: "completed", ExitCode: 0, DurationMS: "1", StdoutBytes: p.Decimal(strconv.Itoa(len(out))), StderrBytes: p.Decimal(strconv.Itoa(len(errout))), OutputComplete: true, ProcessGroupTerminationConfirmed: true}
	if change != nil {
		change(&report)
	}
	raw, err := p.EncodeRunCommandResult(report)
	if err != nil {
		t.Fatal(err)
	}
	return p.Response{RequestID: in.RequestID, OperationID: in.OperationID, Outcome: p.Success, Payload: raw}
}
func awaitCommand(t *testing.T, done <-chan commandAnswer) commandAnswer {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-time.After(2 * time.Second):
		t.Fatal("original command did not actually return")
		return commandAnswer{}
	}
}

func TestCommandDispatchPreservesCompleteReportsAndBoundedStreams(t *testing.T) {
	for _, mode := range []string{"success", "nonzero-exit", "cancelled", "timed-out", "io-failure", "truncated", "explicit-unknown", "pre-start-failure"} {
		t.Run(mode, func(t *testing.T) {
			s, socket, _, _ := startCommandSession(t)
			input := commandTestRequest(t)
			done := startCommandDispatch(t, s, context.Background(), input)
			message := sessionOutput(t, socket)
			wire := message.Payload().(p.Request)
			if wire.RequestID != input.RequestID || wire.OperationID != input.OperationID || wire.Environment != nil {
				t.Fatal("prepared command identity or empty environment changed")
			}
			encoded, err := p.Encode(message)
			if err != nil || len(encoded) > p.MaxMessageBytes {
				t.Fatal("actual whole frame exceeded limit", err)
			}
			var stdout, stderr string
			if mode != "pre-start-failure" {
				// Four valid chunks per stream exercise independent sequence and
				// exact 64 KiB retention rather than one monolithic fake report.
				chunk := strings.Repeat("é", 8192)
				for n := 1; n <= 4; n++ {
					sessionInput(t, socket, commandStream(wire, "stdout", n, chunk))
					sessionInput(t, socket, commandStream(wire, "stderr", n, chunk))
					stdout += chunk
					stderr += chunk
				}
			}
			var response p.Response
			switch mode {
			case "explicit-unknown":
				msg := p.UnknownMessage
				response = p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Unknown, SafeMessage: &msg}
			case "pre-start-failure":
				code := p.InternalError
				msg := code.SafeMessage()
				response = p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Failure, Code: &code, SafeMessage: &msg}
			default:
				response = commandReport(t, wire, stdout, stderr, func(r *p.RunCommandResult) {
					switch mode {
					case "nonzero-exit":
						r.ExitCode = 7
					case "cancelled":
						code := p.CancelledCode
						r.Cancelled = true
						r.CompletionCode = &code
						r.Signal = 15
						r.ExitCode = -1
					case "timed-out":
						code := p.Timeout
						r.Cancelled = true
						r.TimedOut = true
						r.CompletionCode = &code
						r.Signal = 9
						r.ExitCode = -1
					case "io-failure":
						code := p.InternalError
						r.CompletionCode = &code
						r.OutputComplete = false
					case "truncated":
						r.StdoutTruncated = true
						r.StderrTruncated = true
					}
				})
			}
			sessionInput(t, socket, response)
			out := awaitCommand(t, done)
			if out.e != nil || !out.r.Attempted || out.r.Terminal == nil || out.r.Terminal.Outcome != response.Outcome {
				t.Fatal("original invocation report lost", out.e)
			}
			// Compare stable report encoding below: the real Message codec may reorder nested keys.
			if response.Outcome == p.Success {
				actual, e := p.DecodeRunCommandResult(out.r.Terminal.Payload)
				if e != nil {
					t.Fatal(e)
				}
				stable, e := p.EncodeRunCommandResult(actual)
				if e != nil || !bytes.Equal(stable, response.Payload) || string(out.r.Stdout) != stdout || string(out.r.Stderr) != stderr {
					t.Fatal("complete report or stream content changed", e)
				}
			} else if len(out.r.Stdout) != 0 || len(out.r.Stderr) != 0 {
				t.Fatal("unverified prefix escaped without a complete report")
			}
		})
	}
}

func TestCommandDispatchRejectsUntrustedStreamOrReport(t *testing.T) {
	for _, mode := range []string{"sequence-gap", "repeated-sequence", "wrong-rid", "wrong-operation", "unexpected-stream", "stream-overflow", "wrong-count", "bad-report", "failure-after-stream", "invalid-utf8", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			s, socket, cancel, owner := startCommandSession(t)
			input := commandTestRequest(t)
			done := startCommandDispatch(t, s, context.Background(), input)
			wire := sessionOutput(t, socket).Payload().(p.Request)
			if mode == "disconnect" {
				cancel()
				<-owner
			} else {
				stream := commandStream(wire, "stdout", 1, "abc")
				switch mode {
				case "sequence-gap":
					stream.Sequence = "2"
				case "wrong-rid":
					stream.RequestID = commandTestRequest(t).RequestID
				case "wrong-operation":
					stream.OperationID = commandTestRequest(t).OperationID
				case "unexpected-stream":
					stream.Stream = "progress"
				}
				if mode == "invalid-utf8" {
					m, e := controlMessage(stream, stream.RequestID, stream.OperationID)
					if e != nil {
						t.Fatal(e)
					}
					raw, e := p.Encode(m)
					if e != nil {
						t.Fatal(e)
					}
					bad := bytes.Replace(raw, []byte(`"data":"abc"`), []byte{'"', 'd', 'a', 't', 'a', '"', ':', '"', 0xff, '"'}, 1)
					if bytes.Equal(raw, bad) {
						t.Fatal("invalid UTF8 fixture did not alter actual frame")
					}
					socket.in <- bad
				} else {
					sessionInput(t, socket, stream)
				}
				switch mode {
				case "repeated-sequence":
					sessionInput(t, socket, stream)
				case "stream-overflow":
					for n := 2; n <= 5; n++ {
						sessionInput(t, socket, commandStream(wire, "stdout", n, strings.Repeat("x", 16<<10)))
					}
				case "wrong-count":
					sessionInput(t, socket, commandReport(t, wire, "different", "", nil))
				case "bad-report":
					sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Success, Payload: []byte(`{"state":"completed"}`)})
				case "failure-after-stream":
					code := p.InternalError
					msg := code.SafeMessage()
					sessionInput(t, socket, p.Response{RequestID: wire.RequestID, OperationID: wire.OperationID, Outcome: p.Failure, Code: &code, SafeMessage: &msg})
				}
			}
			out := awaitCommand(t, done)
			var fault *f.Fault
			if !out.r.Attempted || out.r.Terminal != nil || len(out.r.Stdout) != 0 || len(out.r.Stderr) != 0 || !errors.As(out.e, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown {
				t.Fatal("untrusted command was classified as a known outcome", out.e)
			}
			select {
			case <-s.retired:
			default:
				t.Fatal("failed stream escaped before actual connection owner retirement")
			}
		})
	}
}

func TestCommandDispatchCancellationRetainsOriginalStreamAndFrame(t *testing.T) {
	s, socket, _, _ := startCommandSession(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	socket.writeFn = func(_ context.Context, raw []byte) error {
		m, e := p.Decode(raw)
		if e != nil {
			return e
		}
		if m.Type() == p.CancelType {
			close(entered)
			<-release
		}
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	input := commandTestRequest(t)
	done := startCommandDispatch(t, s, ctx, input)
	t.Cleanup(func() { cancel(); once.Do(func() { close(release) }) })
	wire := sessionOutput(t, socket).Payload().(p.Request)
	cancel()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("original cancel frame never entered write")
	}
	// Cancellation does not invalidate the original authenticated streams.
	sessionInput(t, socket, commandStream(wire, "stdout", 1, "late output"))
	sessionInput(t, socket, commandReport(t, wire, "late output", "", func(r *p.RunCommandResult) { code := p.CancelledCode; r.Cancelled = true; r.CompletionCode = &code }))
	s.mu.Lock()
	call := s.pending[input.RequestID]
	s.mu.Unlock()
	if call != nil {
		select {
		case <-call.done:
		case <-time.After(2 * time.Second):
			t.Fatal("terminal did not reach original call")
		}
	}
	select {
	case <-done:
		t.Fatal("terminal escaped while original Cancel write was held")
	case <-time.After(15 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	out := awaitCommand(t, done)
	if out.e != nil || out.r.Terminal == nil || string(out.r.Stdout) != "late output" {
		t.Fatal("cancel discarded original report/stream", out.e)
	}
	if sessionOutput(t, socket).Type() != p.CancelType {
		t.Fatal("original cancel frame missing")
	}
}

func TestCommandDispatchWaitsForOriginalWriteAndConnection(t *testing.T) {
	for _, mode := range []string{"write-return", "write-fails-after-report"} {
		t.Run(mode, func(t *testing.T) {
			s, socket, _, _ := startCommandSession(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			socket.writeFn = func(_ context.Context, raw []byte) error {
				m, e := p.Decode(raw)
				if e != nil {
					return e
				}
				if m.Type() == p.RequestType {
					close(entered)
					<-release
					if mode == "write-fails-after-report" {
						return errControlTransport
					}
				}
				return nil
			}
			t.Cleanup(func() { once.Do(func() { close(release) }) })
			input := commandTestRequest(t)
			done := startCommandDispatch(t, s, context.Background(), input)
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("original request never entered native write")
			}
			s.mu.Lock()
			call := s.pending[input.RequestID]
			s.mu.Unlock()
			if call == nil {
				t.Fatal("missing original call")
			}
			sessionInput(t, socket, commandReport(t, input, "", "", nil))
			select {
			case <-call.done:
			case <-time.After(2 * time.Second):
				t.Fatal("terminal never arrived")
			}
			select {
			case <-done:
				t.Fatal("terminal mistaken for original write return")
			case <-time.After(15 * time.Millisecond):
			}
			once.Do(func() { close(release) })
			out := awaitCommand(t, done)
			if mode == "write-return" {
				if out.e != nil || !out.r.Attempted || out.r.Terminal == nil {
					t.Fatal("joined report lost", out.e)
				}
			} else {
				var fault *f.Fault
				if !out.r.Attempted || out.r.Terminal != nil || !errors.As(out.e, &fault) || fault.Code != f.CommitUnknown {
					t.Fatal("failed actual native write became known report", out.e)
				}
				select {
				case <-s.retired:
				default:
					t.Fatal("failed native write owner not joined")
				}
			}
		})
	}
}
