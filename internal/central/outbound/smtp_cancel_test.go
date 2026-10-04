package outbound

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
)

// The socket-closing callback and propagation into BeginSend's context are
// independent. Keep that propagation pending while the actual TCP Read ends.
// The pre-existing sent flag is an input here; real first-write/policy behavior
// remains covered by the owned network integration tests.
func TestSMTPReadCancellationUsesOwningOperation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sent   bool
		reason ac.Reason
	}{
		{"operation_cancel", false, ac.Cancelled},
		{"operation_cancel_sent", true, ac.Cancelled},
		{"operation_deadline", true, ac.Timeout},
		{"send_cancel", true, ac.Cancelled},
		{"read_deadline", false, ac.Timeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := smtpCancellationTCP(t)
			observed := &smtpCancellationConn{Conn: raw, reading: make(chan struct{}), closed: make(chan struct{})}
			owner := &clientState{connections: map[*pooledConn]bool{}, active: map[*operation]bool{}, changed: make(chan struct{})}
			pc := &pooledConn{raw: observed, conn: observed, owner: owner}
			owner.connections[pc] = true
			opCtx, cancelOperation := context.WithCancel(context.Background())
			if tc.name == "operation_deadline" {
				cancelOperation()
				opCtx, cancelOperation = context.WithTimeout(context.Background(), 500*time.Millisecond)
			}
			sendCtx, cancelSend := context.WithCancel(context.Background())
			op := &operation{ctx: opCtx, cancel: cancelOperation, owner: owner}
			owner.active[op] = true
			a := &attempt{ctx: sendCtx, decision: Decision{Consumer: ac.SMTP}}
			a.sent.Store(tc.sent)
			state := &smtpState{op: op, pc: pc, a: a, started: true, active: true}
			conn := &Conn{data: func() *smtpState { return state }}
			stopSocket := stopOnCancel(opCtx, pc)
			stopSendSocket := stopOnCancel(sendCtx, pc)
			propagationStarted := make(chan struct{})
			allowPropagation := make(chan struct{})
			propagationDone := make(chan struct{})
			var allowOnce sync.Once
			stopParent := context.AfterFunc(opCtx, func() {
				close(propagationStarted)
				<-allowPropagation
				cancelSend()
				close(propagationDone)
			})
			readDone := make(chan error, 1)
			readJoined := make(chan struct{})
			go func() {
				defer close(readJoined)
				_, err := conn.Read(make([]byte, 1))
				readDone <- err
			}()
			t.Cleanup(func() {
				cancelOperation()
				allowOnce.Do(func() { close(allowPropagation) })
				if !stopParent() {
					awaitSMTPCancellation(t, propagationDone, "send propagation joined")
				}
				cancelSend()
				stopSocket()
				stopSendSocket()
				pc.close()
				op.finish()
				awaitSMTPCancellation(t, readJoined, "Read joined")
				owner.mu.Lock()
				connections, operations := len(owner.connections), len(owner.active)
				owner.mu.Unlock()
				if connections != 0 || operations != 0 {
					t.Errorf("unjoined resources: connections=%d operations=%d", connections, operations)
				}
			})
			awaitSMTPCancellation(t, observed.reading, "TCP Read entered")
			switch tc.name {
			case "send_cancel":
				cancelSend()
				awaitSMTPCancellation(t, observed.closed, "send cancellation closed socket")
				if opCtx.Err() != nil {
					t.Fatal("send cancellation changed owning operation")
				}
			case "read_deadline":
				if err := raw.SetReadDeadline(time.Now()); err != nil {
					t.Fatal(err)
				}
			default:
				if tc.name != "operation_deadline" {
					cancelOperation()
				}
				awaitSMTPCancellation(t, propagationStarted, "operation ended before propagation")
				awaitSMTPCancellation(t, observed.closed, "operation cancellation closed socket")
				if opCtx.Err() == nil || sendCtx.Err() != nil {
					t.Fatalf("missing cancellation interval: operation=%v send=%v", opCtx.Err(), sendCtx.Err())
				}
			}
			select {
			case err := <-readDone:
				if err == nil {
					t.Fatal("interrupted TCP Read succeeded")
				}
				d := SafeNetworkError(err).Decision()
				if d.Reason != tc.reason || d.Sent != tc.sent {
					t.Fatalf("Read reason/sent=%s/%t, want %s/%t", d.Reason, d.Sent, tc.reason, tc.sent)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Read did not finish")
			}
		})
	}
}

func smtpCancellationTCP(t *testing.T) net.Conn {
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := listener.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	raw, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	peer, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	if err := raw.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return raw
}

type smtpCancellationConn struct {
	net.Conn
	reading, closed     chan struct{}
	readOnce, closeOnce sync.Once
}

func (c *smtpCancellationConn) Read(p []byte) (int, error) {
	c.readOnce.Do(func() { close(c.reading) })
	return c.Conn.Read(p)
}

func (c *smtpCancellationConn) Close() error {
	err := c.Conn.Close()
	c.closeOnce.Do(func() { close(c.closed) })
	return err
}

func awaitSMTPCancellation(t *testing.T, done <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal(what + " timed out")
	}
}
