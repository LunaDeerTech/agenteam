package runnerhttp

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type heldControlSocket struct {
	input   chan []byte
	closed  chan struct{}
	entered chan context.Context
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	writes  [][]byte
}

func newHeldControlSocket() *heldControlSocket {
	return &heldControlSocket{input: make(chan []byte, 2), closed: make(chan struct{}), entered: make(chan context.Context, 2), release: make(chan struct{})}
}
func (s *heldControlSocket) read() ([]byte, error) {
	select {
	case raw := <-s.input:
		return raw, nil
	case <-s.closed:
		return nil, errControlTransport
	}
}
func (s *heldControlSocket) write(ctx context.Context, raw []byte) error {
	s.entered <- ctx
	<-s.release
	s.mu.Lock()
	s.writes = append(s.writes, slices.Clone(raw))
	s.mu.Unlock()
	return ctx.Err()
}
func (s *heldControlSocket) close() error {
	s.once.Do(func() { close(s.closed) })
	return nil
}

func pureControlGate(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return fn(ctx)
}

func pureControlMessage(t *testing.T, payload p.Payload) p.Message {
	t.Helper()
	var request, operation p.ID
	if r, ok := payload.(p.Request); ok {
		request, operation = r.RequestID, r.OperationID
	}
	m, e := controlMessage(payload, request, operation)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func pureControlRequest(t *testing.T, size int) p.Message {
	t.Helper()
	id := func() p.ID { v, _ := p.NewID(); return v }
	return pureControlMessage(t, p.Request{RequestID: id(), OperationID: id(), ExecutionID: id(), ProjectID: id(), AgentID: id(), Mount: p.Mount{MountID: id(), WorkspaceID: id()}, OperationName: "unit.operation", OperationRevision: "1", Payload: []byte(`{"data":"` + strings.Repeat("x", size) + `"}`)})
}

func TestRunnerCentralWirePureQueueReserveAndOrdering(t *testing.T) {
	ctx := context.Background()
	request := pureControlRequest(t, 0)
	control := pureControlMessage(t, p.HeartbeatAck{Sequence: "1"})
	q := newControlQueue()
	for i := range p.MaxQueuedMessages - p.ReservedControlMessages {
		if _, e := q.enqueue(request, nil); e != nil {
			t.Fatalf("data admission %d: %v", i, e)
		}
	}
	if _, e := q.enqueue(request, nil); e != errControlCapacity {
		t.Fatal("request consumed control reservation", e)
	}
	first, e := q.acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	original := slices.Clone(first.raw)
	for range p.ReservedControlMessages {
		if _, e := q.enqueue(control, nil); e != nil {
			t.Fatal("reserved control refused", e)
		}
	}
	if _, e := q.enqueue(control, nil); e != errControlCapacity {
		t.Fatal("leased writer escaped total count", e)
	}
	q.stop()
	if !slices.Equal(first.raw, original) || q.bytes != len(original) || len(q.frames) != 1 {
		t.Fatal("stop released active buffer before actual write")
	}
	q.release(first, errControlClosed)
	if q.bytes != 0 || q.data != 0 || q.dataSize != 0 || len(q.frames) != 0 || first.raw != nil {
		t.Fatal("actual release did not retire exact capacity")
	}

	q = newControlQueue()
	large := pureControlRequest(t, 940000)
	for range 4 {
		if _, e := q.enqueue(large, nil); e != nil {
			t.Fatal("bounded large request refused", e)
		}
	}
	if _, e := q.enqueue(large, nil); e != errControlCapacity {
		t.Fatal("encoded byte cap bypassed", e)
	}
	q.stop()
	q = newControlQueue()
	a, _ := q.enqueue(request, nil)
	b, _ := q.enqueue(control, nil)
	got, _ := q.acquire(ctx)
	if got != a {
		t.Fatal("control overtook admitted request")
	}
	q.release(got, nil)
	got, _ = q.acquire(ctx)
	if got != b {
		t.Fatal("FIFO order changed")
	}
	q.release(got, nil)
}

func TestRunnerCentralWirePureActualWriteAndGateJoin(t *testing.T) {
	s := newHeldControlSocket()
	var inGate atomic.Bool
	gateReturned := make(chan struct{})
	w := newControlWire(s, func(ctx context.Context, fn func(context.Context) error) error {
		inGate.Store(true)
		defer func() { inGate.Store(false); close(gateReturned) }()
		return pureControlGate(ctx, fn)
	})
	m := pureControlMessage(t, p.HeartbeatAck{Sequence: "1"})
	frame, e := w.send(m, nil)
	if e != nil {
		t.Fatal(e)
	}
	original := slices.Clone(frame.raw)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- w.run(ctx, func(context.Context, p.Message) error { return nil }) }()
	var gated context.Context
	select {
	case gated = <-s.entered:
	case <-result:
		t.Fatal("wire returned without original write")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case <-s.release:
		default:
			close(s.release)
		}
		<-w.done
	})
	d, _ := ctx.Deadline()
	gd, ok := gated.Deadline()
	if !ok || !d.Equal(gd) || !inGate.Load() {
		t.Fatal("write did not use exact earlier transaction deadline")
	}
	cancel()
	<-s.closed
	select {
	case <-w.done:
		t.Fatal("cancel was mistaken for actual join")
	case <-gateReturned:
		t.Fatal("connection transaction retired before native write")
	case <-time.After(15 * time.Millisecond):
	}
	close(s.release)
	<-result
	<-gateReturned
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.writes) != 1 || !slices.Equal(s.writes[0], original) || w.queue.bytes != 0 {
		t.Fatal("active bytes cleared early or retained after return")
	}
}

func TestRunnerCentralWirePureCallbackJoinAndRefusedGeneration(t *testing.T) {
	t.Run("callback", func(t *testing.T) {
		s := newHeldControlSocket()
		w := newControlWire(s, pureControlGate)
		message := pureControlMessage(t, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"})
		raw, _ := p.Encode(message)
		s.input <- raw
		entered, release := make(chan struct{}), make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() {
			result <- w.run(ctx, func(context.Context, p.Message) error { close(entered); <-release; return nil })
		}()
		select {
		case <-entered:
		case <-result:
			t.Fatal("read rejected valid callback")
		}
		t.Cleanup(func() {
			cancel()
			select {
			case <-release:
			default:
				close(release)
			}
			<-w.done
		})
		cancel()
		<-s.closed
		select {
		case <-w.done:
			t.Fatal("socket close replaced callback join")
		case <-time.After(15 * time.Millisecond):
		}
		close(release)
		<-result
	})
	t.Run("stale generation", func(t *testing.T) {
		s := newHeldControlSocket()
		denied := errors.New("owned gate refused")
		w := newControlWire(s, func(context.Context, func(context.Context) error) error { return denied })
		attempts := 0
		_, e := w.send(pureControlRequest(t, 0), func() bool { attempts++; return true })
		if e != nil {
			t.Fatal(e)
		}
		if e := w.run(context.Background(), func(context.Context, p.Message) error { return nil }); !errors.Is(e, denied) || attempts != 0 {
			t.Fatal("stale generation attempted native write", e)
		}
		select {
		case <-s.entered:
			t.Fatal("gate bypass")
		default:
		}
	})
}

func TestRunnerCentralWirePureDirectionAndDecode(t *testing.T) {
	wrong, _ := p.Encode(pureControlMessage(t, p.HeartbeatAck{Sequence: "1"}))
	for _, raw := range [][]byte{wrong, []byte(`{"private":"canary"}`)} {
		s := newHeldControlSocket()
		close(s.release)
		w := newControlWire(s, pureControlGate)
		s.input <- raw
		calls := 0
		if e := w.run(context.Background(), func(context.Context, p.Message) error { calls++; return nil }); e == nil || calls != 0 {
			t.Fatal("invalid input reached publication")
		}
		s.mu.Lock()
		if len(s.writes) != 1 || strings.Contains(string(s.writes[0]), "canary") {
			t.Error("safe fatal response missing or leaked input")
		} else if message, e := p.Decode(s.writes[0]); e != nil || message.Type() != p.ProtocolErrorType {
			t.Error("invalid fatal projection", e)
		}
		s.mu.Unlock()
	}
}

func TestRunnerCentralWirePureCompatibilityClassification(t *testing.T) {
	raw, _ := p.Encode(pureControlMessage(t, p.Heartbeat{Sequence: "1", RunnerTime: "2026-10-09T00:00:00Z"}))
	major := []byte(strings.Replace(string(raw), `"major":1`, `"major":2`, 1))
	for _, test := range []struct {
		raw  []byte
		want int
	}{{major, 1}, {[]byte(`{"broken":true}`), 0}} {
		s := newHeldControlSocket()
		close(s.release)
		w := newControlWire(s, pureControlGate)
		marked := 0
		w.incompatible = func(context.Context) error { marked++; return nil }
		s.input <- test.raw
		if e := w.run(context.Background(), func(context.Context, p.Message) error { t.Error("invalid major reached application"); return nil }); e == nil || marked != test.want {
			t.Fatal("wrong classification or missing committed compatibility hook", e, marked)
		}
	}
}
