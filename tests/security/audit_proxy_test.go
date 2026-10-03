//go:build integration

package security_test

import (
	"context"
	"encoding/binary"
	"errors"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// commitProxy forwards the real PostgreSQL protocol on a test-only plaintext
// loopback connection. It withholds actual commit response frames; no driver
// Commit result is replaced or faked.
type commitProxy struct {
	listener    net.Listener
	upstream    string
	after       bool
	reached     chan struct{}
	release     chan struct{}
	quit        chan struct{}
	once        sync.Once
	wg          sync.WaitGroup
	mu          sync.Mutex
	connections map[net.Conn]bool
	armed       atomic.Bool
}

func newCommitProxy(t *testing.T, upstream string, after bool) *commitProxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &commitProxy{listener: listener, upstream: upstream, after: after, reached: make(chan struct{}, 1), release: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool)}
	p.armed.Store(true)
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.connections[client] = true
			p.mu.Unlock()
			p.wg.Add(1)
			go p.serve(client)
		}
	}()
	t.Cleanup(p.Close)
	return p
}
func (p *commitProxy) Close() {
	p.once.Do(func() {
		close(p.quit)
		_ = p.listener.Close()
		p.mu.Lock()
		for conn := range p.connections {
			_ = conn.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
}
func (p *commitProxy) hold() {
	p.reached <- struct{}{}
	select {
	case <-p.release:
	case <-p.quit:
	}
}
func (p *commitProxy) serve(client net.Conn) {
	defer p.wg.Done()
	defer func() { _ = client.Close(); p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	server, err := net.Dial("tcp", p.upstream)
	if err != nil {
		return
	}
	defer server.Close()
	var header [4]byte
	if _, err := io.ReadFull(client, header[:]); err != nil {
		return
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 1<<20 {
		return
	}
	startup := make([]byte, int(size))
	copy(startup, header[:])
	if _, err := io.ReadFull(client, startup[4:]); err != nil {
		return
	}
	if _, err := server.Write(startup); err != nil {
		return
	}
	var committing atomic.Bool
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, err := readPGFrame(client)
			if err != nil {
				return
			}
			if frame[0] == 'Q' && strings.EqualFold(strings.Trim(string(frame[5:]), "\x00; \t\r\n"), "COMMIT") && p.armed.CompareAndSwap(true, false) {
				committing.Store(true)
				if !p.after {
					p.hold()
					return
				}
			}
			if _, err := server.Write(frame); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, err := readPGFrame(server)
			if err != nil {
				return
			}
			if p.after && committing.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" {
				ready, err := readPGFrame(server)
				if err != nil || ready[0] != 'Z' || len(ready) != 6 || ready[5] != 'I' {
					return
				}
				p.hold()
				return
			}
			if _, err := client.Write(frame); err != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}
func readPGFrame(reader io.Reader) ([]byte, error) {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size < 4 || size > 1<<20 {
		return nil, errors.New("fixture protocol frame rejected")
	}
	frame := make([]byte, int(size)+1)
	copy(frame, header[:])
	_, err := io.ReadFull(reader, frame[5:])
	return frame, err
}

func TestAuditRealCommitResponseLossLookup(t *testing.T) {
	for _, after := range []bool{true, false} {
		name := "before_server_commit"
		if after {
			name = "after_server_commit"
		}
		t.Run(name, func(t *testing.T) {
			f := newAuditFixture(t)
			proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port), after)
			u, _ := url.Parse(f.db.Fixture.URL(f.db.Name))
			u.Host = proxy.listener.Addr().String()
			store := openAuditStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			// Service System event needs no Project/session adapter; the registered
			// producer/cause is the formal restricted receipt authority.
			key := appendKey(t, ac.SecretProducer)
			registration, _ := identity.RegisterService(identity.SecretService)
			actor, _ := registration.Actor(key.Details().CauseRef, identity.SystemScope())
			entry := f.entry(t, actor, identity.SystemScope())
			service, err := audit.New(store, auditKeys(t), audit.Authorizations{})
			if err != nil {
				t.Fatal(err)
			}
			results := make(chan foundation.CommitResult, 1)
			cause := txCause(t)
			go func() {
				results <- store.WithinTx(auditContext(t), cause, func(ctx context.Context, tx foundation.Tx) error {
					_, err := service.AppendInTx(ctx, tx, entry, key)
					return err
				})
			}()
			select {
			case <-proxy.reached:
			case <-time.After(5 * time.Second):
				t.Fatal("COMMIT protocol barrier not reached")
			}
			digest, _ := audit.SemanticDigest(entry)
			lookup, err := f.service.LookupAppend(auditContext(t), actor, identity.SystemScope(), key, digest)
			if err != nil || after && (lookup.State != ac.Committed) || !after && (lookup.State != ac.NotObserved) {
				t.Fatal("formal lookup disagrees with held real wire response")
			}
			close(proxy.release)
			select {
			case result := <-results:
				if result.State() != foundation.Unknown {
					t.Fatal("real wire failure did not remain unknown")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("COMMIT failure did not return")
			}
			verified, err := f.service.LookupAppend(auditContext(t), actor, identity.SystemScope(), key, digest)
			if err != nil || verified.State != lookup.State {
				t.Fatal("lookup changed after disconnect")
			}
			if after && *verified.Receipt != *lookup.Receipt {
				t.Fatal("receipt identity changed")
			}
		})
	}
}

func TestAuditCleanupUnknownRechecksRemainingUnderGate(t *testing.T) {
	f := newAuditFixture(t)
	if _, r := appendAudit(t, f, f.entry(t, f.actor, f.scope), appendKey(t, ac.SecretProducer)); r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	operation := newID[ac.LifecycleOperation](t)
	cause, _ := ac.NewLifecycleCause(operation, ac.Delete, 2)
	registration, _ := identity.RegisterService(identity.ProjectLifecycle)
	actor, _ := registration.Actor(operation.String(), f.scope)
	if _, err := f.store.Exec(auditContext(t), `UPDATE audit_fixture.projects SET state='deleting',stopped=true,operation_id=$1,version=2`, operation.String()); err != nil {
		t.Fatal(err)
	}
	proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port), true)
	u, _ := url.Parse(f.db.Fixture.URL(f.db.Name))
	u.Host = proxy.listener.Addr().String()
	store := openAuditStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	auth := &auditAuthority{store}
	service, _ := audit.New(store, auditKeys(t), audit.Authorizations{Projects: auth})
	type outcome struct {
		report ac.CleanupReport
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		report, err := service.CleanupProject(auditContext(t), actor, cause, f.project, nil)
		done <- outcome{report, err}
	}()
	select {
	case <-proxy.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup commit not intercepted")
	}
	var count int
	if err := f.store.QueryRow(auditContext(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1`, f.project.String()).Scan(&count); err != nil || count != 0 {
		t.Fatal("delete did not actually commit before response loss")
	}
	close(proxy.release)
	select {
	case result := <-done:
		if result.err != nil || result.report.State != ac.CleanupCompleted || result.report.Checkpoint.LastID != nil {
			t.Fatalf("unknown cleanup did not use remaining state: %v", result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup recovery hung")
	}
}
