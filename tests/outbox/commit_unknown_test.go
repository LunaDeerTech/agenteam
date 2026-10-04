//go:build integration

package outbox_test

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This owned loopback proxy drops the client only after reading its actual
// COMMIT frame, retains the original live backend transaction, and later sends
// that same frame to PostgreSQL. No Store result is replaced.
type commitProxy struct {
	listener                          net.Listener
	upstream                          string
	reached, release, completed, quit chan struct{}
	armed                             atomic.Bool
	once                              sync.Once
	wg                                sync.WaitGroup
	mu                                sync.Mutex
	connections                       map[net.Conn]bool
	commit                            bool
}

func newCommitProxy(t *testing.T, upstream string, commit bool) *commitProxy {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &commitProxy{listener: listener, upstream: upstream, reached: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool), commit: commit}
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
func (p *commitProxy) serve(client net.Conn) {
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
				if !p.commit {
					_ = server.Close()
					close(p.completed)
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

func readPGFrame(reader io.Reader) ([]byte, error) {
	var header [5]byte
	if _, e := io.ReadFull(reader, header[:]); e != nil {
		return nil, e
	}
	size := binary.BigEndian.Uint32(header[1:])
	if size < 4 || size > 1<<20 {
		return nil, errors.New("invalid fixture frame")
	}
	frame := make([]byte, int(size)+1)
	copy(frame, header[:])
	_, e := io.ReadFull(reader, frame[5:])
	return frame, e
}
func proxyService(t *testing.T, f *fixture, commit bool) (*outbox.Service, *postgres.Store, *commitProxy) {
	t.Helper()
	p := newCommitProxy(t, net.JoinHostPort("127.0.0.1", f.db.Fixture.Port), commit)
	u, _ := url.Parse(f.db.Fixture.URL(f.db.Name))
	u.Host = p.listener.Addr().String()
	store := openStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
	auth := &authority{store: store, issuer: oc.NewPlanIssuer(), process: f.auth.process}
	svc, err := outbox.New(store, f.cat, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{"fixture": auth}, Projects: auth, Processes: auth})
	if err != nil {
		t.Fatal(err)
	}
	return svc, store, p
}
func reached(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("real commit proxy barrier timed out")
	}
}
func TestOutboxRegistrationUnknownWaitsForOriginalDatabaseWriter(t *testing.T) {
	for _, committed := range []bool{true, false} {
		name := "commit"
		if !committed {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			svc, store, proxy := proxyService(t, f, committed)
			h := f.handler("fixture.unknown")
			h.store = store
			proxy.armed.Store(true)
			ctx, cancel := context.WithTimeout(context.Background(), 650*time.Millisecond)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := svc.RegisterHandler(ctx, h.definition(1)); done <- err }()
			reached(t, proxy.reached)
			// The original registration has sent COMMIT to the proxy but not to PG.
			// A plain read sees nothing; the library's confirmation must really wait.
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.handlers`) != 0 {
				t.Fatal("uncommitted registry visible")
			}
			waitDB(t, f.db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE datname=current_database() AND cardinality(pg_blocking_pids(a.pid))>0 AND a.query LIKE '%pg_advisory_xact_lock%')`)
			err := <-done
			code(t, err, foundation.CommitUnknown)
			close(proxy.release)
			reached(t, proxy.completed)
			expected := int64(0)
			if committed {
				expected = 1
			}
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.handlers`) != expected {
				t.Fatal("original writer terminal differs from proxy evidence")
			}
			if committed {
				e := f.event(t, false, 1, "after-late-registration")
				_, result := f.append(t, f.svc, f.store, f.actor, e)
				state(t, result, foundation.Committed)
				delivery := f.delivery(t, e, string(h.name))
				f.claim(t, delivery)
				if _, err = svc.PrepareDelivery(ctxFor(t), delivery); err == nil {
					t.Fatal("unknown registration published local handler")
				} else {
					code(t, err, foundation.DependencyUnbound)
				}
			}
			reg, err := svc.RegisterHandler(ctxFor(t), h.definition(1))
			if err != nil || reg.Subscriptions[0].New == committed {
				t.Fatal("restart confirmation registration", err)
			}
		})
	}
}
func TestOutboxApplyUnknownSerializesMarkerBeforeAnyReexecution(t *testing.T) {
	for _, committed := range []bool{true, false} {
		name := "commit"
		if !committed {
			name = "rollback"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			svc, store, proxy := proxyService(t, f, committed)
			h := f.handler("fixture.unknown-marker")
			h.store = store
			if _, err := svc.RegisterHandler(ctxFor(t), h.definition(1)); err != nil {
				t.Fatal(err)
			}
			e := f.event(t, true, 1, "once")
			_, result := f.append(t, svc, store, f.actor, e)
			state(t, result, foundation.Committed)
			delivery := f.delivery(t, e, string(h.name))
			f.claim(t, delivery)
			plan, err := svc.PrepareDelivery(ctxFor(t), delivery)
			if err != nil {
				t.Fatal(err)
			}
			proxy.armed.Store(true)
			result, _ = svc.ApplyDelivery(ctxFor(t), plan)
			state(t, result, foundation.Unknown)
			// The D03 result intentionally permits nil Fault on Unknown.
			if result.Fault() != nil {
				t.Fatal("expected real transport unknown without a business Fault")
			}
			reached(t, proxy.reached)
			if f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != 0 || f.count(t, `SELECT count(*) FROM outbox_fixture.effects`) != 0 {
				t.Fatal("old writer should still be invisible")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
			confirmation := make(chan error, 1)
			go func() { _, _, err := svc.ConfirmProcessed(ctx, plan); confirmation <- err }()
			waitDB(t, f.db.Connect(t), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE datname=current_database() AND cardinality(pg_blocking_pids(a.pid))>0 AND a.query LIKE '%pg_advisory_xact_lock%')`)
			err = <-confirmation
			cancel()
			if err == nil {
				t.Fatal("marker absence crossed original writer")
			}
			if h.calls.Load() != 1 {
				t.Fatal("unknown retried callback")
			}
			close(proxy.release)
			reached(t, proxy.completed)
			receipt, found, err := svc.ConfirmProcessed(ctxFor(t), plan)
			if err != nil || found != committed {
				t.Fatal("settled marker confirmation", err)
			}
			expected := int64(0)
			if committed {
				expected = 1
				if receipt.EventID != e.Header().EventID || receipt.AttemptID != plan.Identity().AttemptID {
					t.Fatal("marker receipt identity")
				}
			}
			if f.count(t, `SELECT count(*) FROM outbox_fixture.effects`) != expected || f.count(t, `SELECT count(*) FROM agenteam_outbox.processed`) != expected || h.calls.Load() != 1 {
				t.Fatal("real unknown outcome mismatch")
			}
		})
	}
}
