package postgres

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/pgproto3"
)

func cancellationTestState() *storeState {
	return &storeState{operations: make(map[*operation]struct{}), changed: make(chan struct{}), closed: make(chan struct{})}
}
func cancellationAwait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(2 * time.Second):
		t.Fatal("local cancellation barrier did not join")
		var zero T
		return zero
	}
}

// Construct supplies only a driver protocol fixture, never production SQL or
// server-stopped evidence. It lets these unit tests observe real pipe EOF and
// the exact local owner barrier without depending on Docker.
func cancellationTestTarget(t *testing.T, s *storeState, mode string, wrap func(net.Conn) net.Conn) (*poolCancelTarget, *atomic.Int32, <-chan struct{}) {
	t.Helper()
	data, peer := net.Pipe()
	if wrap != nil {
		data = wrap(data)
	}
	t.Cleanup(func() { _ = data.Close(); _ = peer.Close() })
	count := new(atomic.Int32)
	reached := make(chan struct{}, 8)
	var workers sync.WaitGroup
	config := &pgconn.Config{BuildFrontend: pgproto3.NewFrontend, BuildContextWatcherHandler: s.poolWatcher}
	config.DialFunc = func(ctx context.Context, _, _ string) (net.Conn, error) {
		count.Add(1)
		if mode == "dial_error" {
			return nil, net.ErrClosed
		}
		client, server := net.Pipe()
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer server.Close()
			if mode == "tls_error" {
				var packet [8]byte
				if _, err := io.ReadFull(server, packet[:]); err == nil && binary.BigEndian.Uint32(packet[4:]) == 80877103 {
					_, _ = server.Write([]byte{'N'})
				}
				return
			}
			var packet [16]byte
			if _, err := io.ReadFull(server, packet[:]); err != nil {
				return
			}
			clear(packet[:])
			reached <- struct{}{}
			if mode == "read_timeout" {
				_, _ = io.Copy(io.Discard, server) // must end at actual control Close
			}
		}()
		return client, nil
	}
	var tlsConfig *tls.Config
	if mode == "tls_error" {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "fixture.invalid"}
	}
	pg, err := pgconn.Construct(&pgconn.HijackedConn{Conn: data, TLSConfig: tlsConfig, Config: config, PID: 1, SecretKey: []byte{1, 2, 3, 4}, CustomData: map[string]any{}, TxStatus: 'I'})
	if err != nil {
		t.Fatal(err)
	}
	target := pg.CustomData()[poolCancelTargetKey].(*poolCancelTarget)
	t.Cleanup(func() { workers.Wait() })
	return target, count, reached
}

type cancellationCloseBarrier struct {
	net.Conn
	entered, allow chan struct{}
	once           sync.Once
}

func (c *cancellationCloseBarrier) Close() error {
	c.once.Do(func() { close(c.entered); <-c.allow })
	return c.Conn.Close()
}

func TestPoolCancellationOwnerRetainedThroughActualJoin(t *testing.T) {
	for _, mode := range []string{"release_fallback", "watcher"} {
		t.Run(mode, func(t *testing.T) {
			s := cancellationTestState()
			barrier := &cancellationCloseBarrier{entered: make(chan struct{}), allow: make(chan struct{})}
			var allow sync.Once
			target, count, _ := cancellationTestTarget(t, s, "ack", func(conn net.Conn) net.Conn { barrier.Conn = conn; return barrier })
			t.Cleanup(func() { allow.Do(func() { close(barrier.allow) }) })
			caller, cancelCaller := context.WithCancel(context.Background())
			defer cancelCaller()
			ctx, cancel := context.WithCancel(caller)
			op := &operation{owner: s, caller: caller, ctx: ctx, cancel: cancel, target: target}
			s.mu.Lock()
			s.operations[op] = struct{}{}
			target.retainLocked()
			s.mu.Unlock()
			watcher := ctxwatch.NewContextWatcher(&poolCancelWatcher{target: target})
			if mode == "watcher" {
				watcher.Watch(ctx)
			}
			cancelCaller()
			done := make(chan struct{})
			if mode == "release_fallback" {
				go func() { op.release(); close(done) }()
			}
			cancellationAwait(t, barrier.entered)
			if mode == "watcher" {
				go func() { watcher.Unwatch(); op.release(); close(done) }()
			}
			s.mu.Lock()
			_, registered := s.operations[op]
			work := target.work
			inflight := len(s.works)
			s.mu.Unlock()
			if !registered || inflight != 1 {
				t.Fatal("owner/work disappeared before actual data Close joined")
			}
			select {
			case <-done:
				t.Fatal("owner returned before actual Close")
			case <-work.done:
				t.Fatal("local work declared joined before actual Close")
			default:
			}
			allow.Do(func() { close(barrier.allow) })
			cancellationAwait(t, done)
			select {
			case <-work.done:
			default:
				t.Fatal("owner returned before work.done")
			}
			if !target.pg.IsClosed() || count.Load() != 1 {
				t.Fatal("owner failed to discard or duplicated cancellation")
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if len(s.operations)+len(s.targets)+len(s.works) != 0 {
				t.Fatal("joined local ticket retained capacity")
			}
		})
	}
}

func TestPoolCancellationInternalCleanupDoesNotDiscard(t *testing.T) {
	s := cancellationTestState()
	target, count, _ := cancellationTestTarget(t, s, "ack", nil)
	ctx, cancel := context.WithCancel(context.Background())
	op := &operation{owner: s, caller: context.Background(), ctx: ctx, cancel: cancel, target: target}
	s.mu.Lock()
	s.operations[op] = struct{}{}
	target.retainLocked()
	s.mu.Unlock()
	cancel() // the order used by normal Rows.Close, not caller cancellation
	op.release()
	if count.Load() != 0 || target.pg.IsClosed() || target.work != nil {
		t.Fatal("internal cleanup cancellation discarded a healthy connection")
	}
	if len(s.operations)+len(s.targets)+len(s.works) != 0 {
		t.Fatal("normal checkout retained an owner")
	}
	_ = target.data.Close()
	target.discard()
}

func TestPoolCancellationBeforeCheckoutKeepsTemporaryOwner(t *testing.T) {
	s := cancellationTestState()
	target, counter, _ := cancellationTestTarget(t, s, "ack", nil)
	watcher := ctxwatch.NewContextWatcher(&poolCancelWatcher{target: target})
	ctx, cancel := context.WithCancel(context.Background())
	watcher.Watch(ctx)
	cancel()
	// A pipe peer's EOF establishes actual data close before examining the
	// temporary owner. No checkout operation was ever registered here.
	var b [1]byte
	_, _ = target.data.Read(b[:])
	s.mu.Lock()
	work := target.work
	s.mu.Unlock()
	if work == nil {
		t.Fatal("pre-checkout watcher never acquired cancellation ownership")
	}
	cancellationAwait(t, work.done)
	s.mu.Lock()
	owners, operations := len(s.targets), len(s.operations)
	s.mu.Unlock()
	if owners != 1 || operations != 0 {
		t.Fatal("temporary watcher ownership missing before Unwatch")
	}
	store := &Store{state: func() *storeState { return s }}
	short, stop := context.WithCancel(context.Background())
	stop()
	if err := store.Drain(short); CodeOf(err) != DrainTimeout {
		t.Fatal("Drain ignored an owned pre-checkout cancellation")
	}
	watcher.Unwatch()
	if !target.pg.IsClosed() || counter.Load() != 1 || len(s.targets)+len(s.works) != 0 {
		t.Fatal("pre-checkout owner did not discard and retire after Unwatch")
	}
}

func TestPoolCancellationOneWorkAndShortenedSharedPhase(t *testing.T) {
	s := cancellationTestState()
	var targets []*poolCancelTarget
	var counters []*atomic.Int32
	for range 4 {
		target, counter, reached := cancellationTestTarget(t, s, "read_timeout", nil)
		s.mu.Lock()
		target.retainLocked()
		work := target.workLocked()
		s.mu.Unlock()
		work.start() // ordinary cancellation already in flight before Force
		cancellationAwait(t, reached)
		targets, counters = append(targets, target), append(counters, counter)
	}
	phase, work, _ := s.beginForce(context.Background())
	originalTotal, _ := phase.total.Deadline()
	originalRequest, _ := phase.request.Deadline()
	for range 16 {
		s.beginForce(context.Background())
	}
	if total, _ := phase.total.Deadline(); total != originalTotal {
		t.Fatal("repeated Force renewed its total budget")
	}
	if request, _ := phase.request.Deadline(); request != originalRequest {
		t.Fatal("repeated Force renewed its request phase")
	}
	short, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	s.beginForce(short)
	limit, _ := short.Deadline()
	for _, w := range work {
		if deadline, _ := w.budget.Deadline(); deadline.After(limit) {
			t.Fatal("existing cancellation escaped shorter shared deadline")
		}
	}
	var joined sync.WaitGroup
	for range 16 {
		for _, w := range work {
			joined.Add(1)
			go func() { defer joined.Done(); w.runOrJoin() }()
		}
	}
	done := make(chan struct{})
	go func() { joined.Wait(); close(done) }()
	cancellationAwait(t, done)
	for i, target := range targets {
		if counters[i].Load() != 1 || target.work.outcome != poolCancelBudgetExhausted || CodeOf(target.work.err) != DrainTimeout {
			t.Fatal("duplicate request or nil ACK read mistaken for timely success")
		}
		target.discard()
		s.mu.Lock()
		target.releaseLocked()
		s.mu.Unlock()
	}
	if len(s.targets)+len(s.works) != 0 {
		t.Fatal("completed cancellation work leaked owners")
	}
	phase.total.stop(context.Canceled)
	phase.request.stop(context.Canceled)
}

func TestPoolCancellationFailureAndExpiredPhase(t *testing.T) {
	for _, mode := range []string{"dial_error", "tls_error", "expired", "sealed"} {
		t.Run(mode, func(t *testing.T) {
			s := cancellationTestState()
			target, counter, _ := cancellationTestTarget(t, s, mode, nil)
			s.mu.Lock()
			if mode == "expired" || mode == "sealed" {
				s.force = &poolForcePhase{request: newPoolCancelBudget(time.Now().Add(-time.Second)), total: newPoolCancelBudget(time.Now().Add(-time.Second)), sealed: mode == "sealed"}
			}
			w := target.workLocked()
			s.mu.Unlock()
			w.runOrJoin()
			want := ConnectionFailed
			if mode == "expired" || mode == "sealed" {
				want = DrainTimeout
				if counter.Load() != 0 {
					t.Fatal("expired phase opened a new control socket")
				}
			}
			if CodeOf(w.err) != want {
				t.Fatalf("outcome=%v wanted=%s", w.err, want)
			}
			target.discard()
			if len(s.targets)+len(s.works) != 0 {
				t.Fatal("known local failure kept a completed ticket")
			}
		})
	}
}

func TestPoolCancellationSealRejectsDialBeforeNetwork(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var sockets socketSet
	sockets.close()
	if _, err := sockets.dial(context.Background(), "tcp", l.Addr().String()); !errors.Is(err, net.ErrClosed) {
		t.Fatal("sealed registry attempted a new network connection")
	}
	if err := l.(*net.TCPListener).SetDeadline(time.Now().Add(10 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if conn, err := l.Accept(); err == nil {
		conn.Close()
		t.Fatal("server accepted a connection after seal")
	} else if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatal("unexpected listener failure")
	}
}

func TestPoolCancellationExpiredParentPreservesOriginalDeadline(t *testing.T) {
	s := cancellationTestState()
	deadline := time.Now().Add(-time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	phase, _, _ := s.beginForce(ctx)
	for _, budget := range []*poolCancelBudget{phase.total, phase.request} {
		if got, _ := budget.Deadline(); !got.Equal(deadline) || !budget.expired() {
			t.Fatal("already-expired parent's deadline was replaced with a later time")
		}
	}
	s.beginForce(context.Background())
	for _, budget := range []*poolCancelBudget{phase.total, phase.request} {
		if got, _ := budget.Deadline(); !got.Equal(deadline) {
			t.Fatal("repeat Force changed the expired parent's absolute deadline")
		}
	}
}
