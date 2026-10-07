//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This private proxy inherits the accepted targeted held-writer ownership,
// adds full frame writes and safe C+Z phase evidence, and changes rollback into
// an actual ROLLBACK response. It never records authentication or frame payloads.
func configurationWriteFrame(w io.Writer, p []byte) error {
	for len(p) > 0 {
		n, e := w.Write(p)
		if n < 0 || n > len(p) {
			return io.ErrShortWrite
		}
		p = p[n:]
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
func configurationWriteQuery(query string) []byte {
	p := make([]byte, 5+len(query)+1)
	p[0] = 'Q'
	binary.BigEndian.PutUint32(p[1:5], uint32(len(p)-1))
	copy(p[5:], query)
	return p
}

type configurationWriteProxy struct {
	listener                          net.Listener
	upstream                          string
	reached, release, completed, quit chan struct{}
	targetPID                         atomic.Int32
	backendPID                        atomic.Int32
	once                              sync.Once
	wg                                sync.WaitGroup
	mu                                sync.Mutex
	connections                       map[net.Conn]bool
	commit                            bool
	events                            []string
}

func newConfigurationWriteProxy(t *testing.T, upstream string, commit bool) *configurationWriteProxy {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &configurationWriteProxy{listener: listener, upstream: upstream, reached: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool), commit: commit}
	p.targetPID.Store(0)
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
func (p *configurationWriteProxy) record(event string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}
func (p *configurationWriteProxy) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}
func (p *configurationWriteProxy) serve(client net.Conn) {
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
	if e = configurationWriteFrame(server, startup); e != nil {
		return
	}
	var held atomic.Bool
	var backend atomic.Int32
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := readModelPGFrame(client)
			if e != nil {
				return
			}
			if frame[0] == 'Q' && strings.EqualFold(strings.Trim(string(frame[5:]), "\x00; \t\r\n"), "COMMIT") && backend.Load() != 0 && p.targetPID.CompareAndSwap(backend.Load(), 0) {
				held.Store(true)
				p.backendPID.Store(backend.Load())
				p.record("target-final-commit-intercepted")
				_ = client.Close()
				close(p.reached)
				select {
				case <-p.release:
				case <-p.quit:
					return
				}
				if !p.commit {
					frame = configurationWriteQuery("ROLLBACK")
				}
				if p.commit {
					p.record("send-COMMIT")
				} else {
					p.record("send-ROLLBACK")
				}
				if e = configurationWriteFrame(server, frame); e != nil {
					return
				}
				select {
				case <-p.completed:
				case <-p.quit:
				}
				return
			}
			if e = configurationWriteFrame(server, frame); e != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, e := readModelPGFrame(server)
			if e != nil {
				return
			}
			if frame[0] == 'K' && len(frame) >= 9 {
				backend.Store(int32(binary.BigEndian.Uint32(frame[5:9])))
			}
			tag := "COMMIT"
			if !p.commit {
				tag = "ROLLBACK"
			}
			if held.Load() && frame[0] == 'C' && string(frame[5:]) == tag+"\x00" {
				p.record("tag-" + tag)
				ready, e := readModelPGFrame(server)
				if e == nil && ready[0] == 'Z' && len(ready) == 6 && ready[5] == 'I' {
					p.record("ready-I-before-drop")
					p.record("terminal-idle-ack-dropped")
					close(p.completed)
				}
				return
			}
			if e = configurationWriteFrame(client, frame); e != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}

// Observe original Store returns and confirmation context; never substitute a
// CommitResult or manufacture its cause/attempt. All methods use this same raw
// Store and its real Project/User/Command EX proof.
type configurationWriteCommitStore struct {
	*postgres.Store
	target                               string
	proxy                                *configurationWriteProxy
	mu                                   sync.Mutex
	original                             f.CommitResult
	originalContext, confirmationContext context.Context
	finalFacts                           bool
	unknownReached                       chan struct{}
	confirmationRelease                  chan struct{}
	once                                 sync.Once
}

func (s *configurationWriteCommitStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	targeted := cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == s.target
	s.mu.Lock()
	if targeted && s.originalContext != nil && s.original.State() == f.Unknown {
		s.confirmationContext = ctx
	}
	s.mu.Unlock()
	result := s.Store.WithinTx(ctx, cause, func(c context.Context, tx f.Tx) error {
		if e := fn(c, tx); e != nil {
			return e
		}
		if !targeted {
			return nil
		}
		s.mu.Lock()
		armed := s.finalFacts
		s.mu.Unlock()
		if armed {
			return nil
		}
		x, e := s.Store.InTx(tx)
		if e != nil {
			return e
		}
		var pid int32
		var final bool
		e = x.QueryRow(c, `SELECT pg_backend_pid(),EXISTS(SELECT 1 FROM agenteam_model.commands c JOIN agenteam_model.providers p ON p.id=c.resource_id AND p.project_id=c.project_id WHERE c.command_identity=$1 AND c.scope='project' AND c.phase='committed' AND c.safe_receipt IS NOT NULL AND EXISTS(SELECT 1 FROM agenteam_audit.audit_records a WHERE a.producer='model' AND a.action='provider.create' AND a.project_id=c.project_id AND a.resource_id=c.resource_id) AND EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=c.event_id AND e.producer='model' AND e.project_id=c.project_id))`, s.target).Scan(&pid, &final)
		if e != nil || !final {
			return e
		}
		command, _ := f.CommandLock(cause.Details().Primary)
		if e = s.Store.RequireHeldLocks(c, tx, []f.LockRequest{{Key: command, Mode: f.Exclusive}}); e != nil {
			return e
		}
		n := uint64(command.AdvisoryKey())
		var owners int
		var unique int32
		e = x.QueryRow(c, `SELECT count(*),COALESCE(min(pid),0) FROM pg_locks WHERE locktype='advisory' AND database=(SELECT oid FROM pg_database WHERE datname=current_database()) AND classid::bigint=$1 AND objid::bigint=$2 AND objsubid=1 AND granted AND mode='ExclusiveLock'`, int64(uint32(n>>32)), int64(uint32(n))).Scan(&owners, &unique)
		if e != nil {
			return e
		}
		if owners != 1 || unique != pid {
			return f.NewFault(f.InvalidState, f.NotStarted)
		}
		s.mu.Lock()
		s.finalFacts = true
		s.mu.Unlock()
		s.proxy.targetPID.Store(pid)
		return nil
	})
	if targeted && result.State() == f.Unknown {
		s.once.Do(func() {
			s.mu.Lock()
			s.original = result
			s.originalContext = ctx
			s.mu.Unlock()
			close(s.unknownReached)
			select {
			case <-s.confirmationRelease:
			case <-ctx.Done():
			}
		})
	}
	return result
}
func TestModelProjectConfigurationWriteHTTPUnknownAndLookup(t *testing.T) {
	configurationWriteTop(t)
	v := newConfigurationWriteFixture(t)
	for _, mode := range []string{"committed", "rollback", "pending", "cancel_confirmation"} {
		t.Run(mode, func(t *testing.T) {
			proxy := newConfigurationWriteProxy(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port), mode != "rollback")
			var writerOnce, confirmOnce sync.Once
			releaseWriter := func() { writerOnce.Do(func() { close(proxy.release) }) }
			confirmRelease := make(chan struct{})
			releaseConfirm := func() { confirmOnce.Do(func() { close(confirmRelease) }) }
			t.Cleanup(releaseWriter)
			t.Cleanup(releaseConfirm)
			uri, e := url.Parse(v.db.Fixture.URL(v.db.Name))
			if e != nil {
				t.Fatal(e)
			}
			uri.Host = proxy.listener.Addr().String()
			raw := openStore(t, v.db.Config(t, map[string]string{"URL": uri.String(), "TLS_MODE": "disable"}))
			key := "configuration-unknown-" + mode
			identity := configurationCommandIdentity(t, v.ownerBrowser, v.project.ID, "provider.create", key)
			store := &configurationWriteCommitStore{Store: raw, target: identity.Canonical(), proxy: proxy, unknownReached: make(chan struct{}), confirmationRelease: confirmRelease}
			fixed := assembleProjectConfiguration(t, v.db, raw, store)
			old := v.handler
			v.installConfiguration(t, fixed.service)
			defer func() { v.handler = old }()
			ctx, cancel := context.WithCancel(testContext(t))
			body := projectUpdateJSON(t, map[string]any{"input": configurationProviderBody(nil)})
			request := projectUpdateRequest(ctx, v.ownerBrowser, "POST", v.base()+"model-providers", key, body)
			writer := &projectUsageRecorder{ResponseRecorder: httptest.NewRecorder()}
			done := make(chan bool, 1)
			joined := make(chan struct{})
			t.Cleanup(func() { cancel(); releaseWriter(); releaseConfirm(); <-joined })
			before := v.modelCounts(t)
			go func() {
				defer close(joined)
				done <- projectUpdateAbort(func() { v.handler.ServeHTTP(writer, request) })
			}()
			waitSignal(t, proxy.reached)
			waitSignal(t, store.unknownReached)
			if mode == "committed" || mode == "rollback" {
				releaseWriter()
				waitSignal(t, proxy.completed)
			}
			if mode == "cancel_confirmation" {
				cancel()
			}
			releaseConfirm()
			aborted := <-done
			store.mu.Lock()
			original, same, final := store.original, store.originalContext == store.confirmationContext, store.finalFacts
			store.mu.Unlock()
			if original.State() != f.Unknown || original.AttemptID().Validate() != nil || original.Cause().Details().Primary.Canonical() != identity.Canonical() || !same || !final {
				t.Fatal("original physical Unknown/context/target facts lost")
			}
			response := systemHTTPResponse{writer.Code, writer.Header().Clone(), bytes.Clone(writer.Body.Bytes())}
			if mode == "cancel_confirmation" {
				if !aborted || len(response.body) != 0 {
					t.Fatal("cancelled confirmation published")
				}
			} else if mode == "committed" {
				if aborted {
					t.Fatal("confirmed response aborted")
				}
				configurationWriteReceipt(t, response, "provider.create")
				configurationWriteSchema(t, "ConfigurationReceipt", response.body)
				configurationWriteExport(t, "confirmed-write", "POST", v.base()+"model-providers", response)
			} else {
				if aborted {
					t.Fatal("live Unknown response aborted")
				}
				response.problem(t, 503, f.CommitUnknown)
				configurationWriteSchema(t, "Problem", response.body)
				configurationWriteExport(t, "unknown-"+mode, "POST", v.base()+"model-providers", response)
			}
			if mode == "pending" || mode == "cancel_confirmation" {
				releaseWriter()
				waitSignal(t, proxy.completed)
			}
			command, _ := f.CommandLock(identity)
			result := raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				return raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: command, Mode: f.Exclusive}})
			})
			if result.State() != f.Committed {
				t.Fatal("original writer not actually retired", result.Fault())
			}
			events := proxy.snapshot()
			want := "send-COMMIT,tag-COMMIT,ready-I-before-drop,terminal-idle-ack-dropped"
			if mode == "rollback" {
				want = "send-ROLLBACK,tag-ROLLBACK,ready-I-before-drop,terminal-idle-ack-dropped"
			}
			if strings.Join(events, ",") != "target-final-commit-intercepted,"+want {
				t.Fatal("same backend terminal evidence missing")
			}
			t.Logf("safe_proxy_stages=%s targeted_backend=true original_unknown=true same_context_confirmation=true", strings.Join(events, ","))
			lookup := v.command(t, v.ownerBrowser, "POST", v.base()+"model-commands/lookup", key, projectUpdateJSON(t, map[string]any{"command": "provider.create"})).want(t, 200)
			if mode == "rollback" {
				configurationLookupReceipt(t, lookup, nil)
				if v.modelCounts(t) != before {
					t.Fatal("rollback kept side effects")
				}
			} else {
				var observed model.CommandLookup
				if e = jsonUnmarshalConfigurationLookup(lookup.body, &observed); e != nil || !observed.Found || observed.Receipt == nil {
					t.Fatal("explicit terminal lookup missing")
				}
				after := v.modelCounts(t)
				for i := 0; i < 5; i++ {
					if i == 1 {
						continue
					}
					if after[i] != before[i]+1 {
						t.Fatal("exactly one configuration fact")
					}
				}
				replay := v.command(t, v.ownerBrowser, "POST", v.base()+"model-providers", key, body).want(t, 200)
				configurationWriteReceipt(t, replay, "provider.create")
				v.command(t, v.ownerBrowser, "POST", v.base()+"model-providers", key, projectUpdateJSON(t, map[string]any{"input": configurationProviderBody("01900000-0000-7000-8000-000000000019")})).problem(t, 409, f.IdempotencyKeyReused)
			}
		})
	}
	configurationWritePublicLookup(t, v)
}
func jsonUnmarshalConfigurationLookup(raw []byte, out *model.CommandLookup) error {
	return json.Unmarshal(raw, out)
}
func configurationWritePublicLookup(t *testing.T, v *configurationWriteFixture) {
	t.Helper()
	key := "lookup-exclusive"
	identity := configurationCommandIdentity(t, v.ownerBrowser, v.project.ID, "provider.create", key)
	command, _ := f.CommandLock(identity)
	user, _ := f.UserLock(v.ownerBrowser.actor.Details().UserID)
	project, _ := f.ProjectLock(v.project.ID.String())
	var calls atomic.Int32
	v.tracked.hooks(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
		if c.Kind() == f.RecoveryCause && c.Details().Owner == "model.query" {
			calls.Add(1)
			return v.raw.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: command, Mode: f.Exclusive}, {Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}})
		}
		return nil
	}, nil)
	body := projectUpdateJSON(t, map[string]any{"command": "provider.create"})
	configurationLookupReceipt(t, v.command(t, v.ownerBrowser, "POST", v.base()+"model-commands/lookup", key, body), nil)
	v.tracked.hooks(nil, nil)
	if calls.Load() != 1 {
		t.Fatal("actual EX lookup lock proof absent")
	}
	release := managementHold(t, &systemHTTPFixture{fixture: v.fixture}, command, f.Shared)
	defer release()
	response := v.command(t, v.ownerBrowser, "POST", v.base()+"model-commands/lookup", key, body)
	response.problem(t, 500, f.InternalError)
	if bytes.Contains(response.body, []byte(`"found"`)) {
		t.Fatal("pending EX writer became false observation")
	}
	release()
	renewed := v.login(t, v.ownerBrowser.email)
	if e := v.core.Logout(testContext(t), account.LogoutRequest{Actor: renewed.actor, Key: "lookup-current-revoke"}); e != nil {
		t.Fatal(e)
	}
	v.command(t, renewed, "POST", v.base()+"model-commands/lookup", key, body).problem(t, 401, f.SessionRevoked)
	// Controlled terminal rejection follows a real read, separately from physical
	// writer ACK-loss above. It proves no typed candidate escapes failed terminal.
	var actual atomic.Bool
	v.tracked.hooks(nil, func(c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if c.Kind() == f.RecoveryCause && c.Details().Owner == "model.query" {
			actual.Store(r.State() == f.Committed)
			return f.UnknownResult(newID[f.TransactionAttempt](t), c)
		}
		return r
	})
	defer v.tracked.hooks(nil, nil)
	r := v.command(t, v.ownerBrowser, "POST", v.base()+"model-commands/lookup", key, body)
	r.problem(t, 503, f.CommitUnknown)
	if !actual.Load() || bytes.Contains(r.body, []byte(`"found"`)) {
		t.Fatal("Unknown read published observation")
	}
	configurationWriteSchema(t, "Problem", r.body)
	configurationWriteExport(t, "lookup-read-unknown", "POST", v.base()+"model-commands/lookup", r)
}
