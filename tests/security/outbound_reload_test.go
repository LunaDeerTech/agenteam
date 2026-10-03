//go:build integration

package security_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
)

// This owned loopback proxy drops the client only after reading its actual
// COMMIT frame, retains the original live backend transaction, and later sends
// that same frame to PostgreSQL. No Store result is replaced.
type outboundLateCommitProxy struct {
	listener                          net.Listener
	upstream                          string
	reached, release, completed, quit chan struct{}
	armed                             atomic.Bool
	once                              sync.Once
	wg                                sync.WaitGroup
	mu                                sync.Mutex
	connections                       map[net.Conn]bool
}

func newOutboundLateCommitProxy(t *testing.T, upstream string) *outboundLateCommitProxy {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &outboundLateCommitProxy{listener: listener, upstream: upstream, reached: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool)}
	p.armed.Store(false)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, e := listener.Accept()
			if e != nil {
				return
			}
			p.mu.Lock()
			p.connections[client] = true
			p.mu.Unlock()
			p.wg.Add(1)
			go p.serve(client)
		}
	}()
	t.Cleanup(func() {
		p.once.Do(func() {
			close(p.quit)
			_ = p.listener.Close()
			p.mu.Lock()
			for c := range p.connections {
				_ = c.Close()
			}
			p.mu.Unlock()
			p.wg.Wait()
		})
	})
	return p
}
func (p *outboundLateCommitProxy) serve(client net.Conn) {
	defer p.wg.Done()
	defer func() { _ = client.Close(); p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	server, e := net.Dial("tcp", p.upstream)
	if e != nil {
		return
	}
	p.mu.Lock()
	p.connections[server] = true
	p.mu.Unlock()
	defer func() { _ = server.Close(); p.mu.Lock(); delete(p.connections, server); p.mu.Unlock() }()
	var header [4]byte
	if _, e = io.ReadFull(client, header[:]); e != nil {
		return
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 1<<20 {
		return
	}
	startup := make([]byte, int(size))
	copy(startup, header[:])
	if _, e = io.ReadFull(client, startup[4:]); e != nil {
		return
	}
	if _, e = server.Write(startup); e != nil {
		return
	}
	var held atomic.Bool
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := readPGFrame(client)
			if e != nil {
				return
			}
			if frame[0] == 'Q' && strings.EqualFold(strings.Trim(string(frame[5:]), "\x00; \t\r\n"), "COMMIT") && p.armed.CompareAndSwap(true, false) {
				held.Store(true)
				_ = client.Close()
				close(p.reached)
				select {
				case <-p.release:
				case <-p.quit:
					return
				}
				if _, e = server.Write(frame); e != nil {
					return
				}
				select {
				case <-p.completed:
				case <-p.quit:
				}
				return
			}
			if _, e = server.Write(frame); e != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := readPGFrame(server)
			if e != nil {
				return
			}
			if held.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" {
				ready, e := readPGFrame(server)
				if e == nil && ready[0] == 'Z' && len(ready) == 6 && ready[5] == 'I' {
					close(p.completed)
				}
				return
			}
			if _, e = client.Write(frame); e != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}

func TestOutboundReloadWaitsForOriginalUnknownWriter(t *testing.T) {
	f := newAuditFixture(t)
	initial := outboundPolicy(t, f, nil)
	if out, e := initial.UpdatePolicy(auditContext(t), f.actor, policyCommand(t, f.actor, 1), policyRules(t, "10.44.0.0/16")); e != nil || out.Version != 2 {
		t.Fatal("initial policy", e)
	}
	proxy := newOutboundLateCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port))
	u, _ := url.Parse(f.db.Fixture.URL(f.db.Name))
	u.Host = proxy.listener.Addr().String()
	store := openAuditStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	auth := &auditAuthority{store}
	appender, e := audit.New(store, auditKeys(t), audit.Authorizations{Sessions: auth, System: auth})
	if e != nil {
		t.Fatal(e)
	}
	service, e := outbound.NewPolicyService(store, appender, outbound.Authorizations{Sessions: auth, System: auth})
	if e != nil {
		t.Fatal(e)
	}
	if e = service.Reload(auditContext(t)); e != nil {
		t.Fatal(e)
	}
	proxy.armed.Store(true) // Reload now has its own read transaction; arm only the target write.
	command := policyCommand(t, f.actor, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := service.UpdatePolicy(ctx, f.actor, command, policyRules(t, "")); done <- e }()
	select {
	case <-proxy.reached:
	case <-time.After(3 * time.Second):
		t.Fatal("actual COMMIT not intercepted")
	}
	select {
	case e := <-done:
		t.Fatalf("unknown verification failed to wait for old writer: %v", e)
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case e = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("unknown verification unbounded")
	}
	requireCode(t, e, foundation.CommitUnknown)
	var fault *foundation.Fault
	if !errors.As(e, &fault) || fault.CommitState != foundation.Unknown || service.Status().Available {
		t.Fatal("unknown policy did not fail closed")
	}
	var carrier interface {
		TransactionCause() foundation.TransactionCause
	}
	if !errors.As(e, &carrier) || carrier.TransactionCause().Kind() != foundation.CommandsCause || carrier.TransactionCause().Details().Primary.Canonical() != command.Identity.Canonical() {
		t.Fatal("original command cause lost")
	}
	for _, verb := range []string{"%v", "%+v", "%#v"} {
		if strings.Contains(fmt.Sprintf(verb, e), command.Identity.Canonical()) {
			t.Fatal("implicit original cause disclosure")
		}
	}
	var before int64
	if e = f.store.QueryRow(auditContext(t), `SELECT version FROM agenteam_outbound.outbound_policy`).Scan(&before); e != nil || before != 2 {
		t.Fatal("held old transaction not observable", e)
	}
	reloadCtx, reloadCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	reloadErr := service.Reload(reloadCtx)
	reloadCancel()
	prematurelyAvailable := service.Status().Available
	close(proxy.release)
	select {
	case <-proxy.completed:
	case <-time.After(3 * time.Second):
		t.Fatal("delayed real COMMIT did not complete")
	}
	var after int64
	if e = f.store.QueryRow(auditContext(t), `SELECT version FROM agenteam_outbound.outbound_policy`).Scan(&after); e != nil || after != 3 {
		t.Fatal("late commit was not durable", e)
	}
	if reloadErr == nil || prematurelyAvailable {
		t.Errorf("Reload published while original unknown writer still held policy lock: reload_error=%v before=%d after=%d mirror=%v", reloadErr, before, after, service.Status())
	}
	if e = service.Reload(auditContext(t)); e != nil || !service.Status().Available || *service.Status().Version != 3 {
		t.Fatal("settled writer reload failed", e)
	}
}
