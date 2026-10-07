//go:build integration

package model_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelProjectOwnerUpdateHTTPUnknown(t *testing.T) {
	projectOwnerReadTop(t)
	v := newProjectUpdateFixture(t)
	for _, phase := range []string{"planned", "completed"} {
		t.Run(phase, func(t *testing.T) {
			// Only this synchronous Project Service uses the proxy Store; Account HTTP
			// and default-root/background transactions cannot steal the next COMMIT arm.
			proxy := newProjectModelPGTrace(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port))
			u, e := url.Parse(v.db.Fixture.URL(v.db.Name))
			if e != nil {
				t.Fatal(e)
			}
			u.Host = proxy.listener.Addr().String()
			raw := openStore(t, v.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			tracked := &projectUsageHTTPStore{Store: raw}
			service, _, _ := projectUpdateService(t, v.projectUsageHTTPFixture, tracked)
			old := v.handler
			v.install(t, service)
			defer func() { v.handler = old }()
			current, e := v.projectService.GetProject(testContext(t), v.owner, v.project.ID)
			if e != nil {
				t.Fatal(e)
			}
			key := "physical-" + phase
			identity, _ := pc.CommandIdentity(v.project.ID, pc.UpdateCommand, f.IdempotencyKey(key))
			name := "uncertain-" + phase
			body := projectUpdateBody(t, current.Version, &name, nil)
			var armed atomic.Bool
			var backend atomic.Int32
			original := make(chan f.CommitResult, 1)
			tracked.hooks(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
				if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != identity.Canonical() || armed.Load() {
					return nil
				}
				x, e := raw.InTx(tx)
				if e != nil {
					return e
				}
				var state string
				var final bool
				var pid int32
				e = x.QueryRow(ctx, `SELECT state,pg_backend_pid(),safe_result IS NOT NULL AND EXISTS(SELECT 1 FROM agenteam_outbox.events e WHERE e.id=c.event_id) AND EXISTS(SELECT 1 FROM agenteam_audit.audit_records a WHERE a.project_id=c.project_id AND a.action='project.update' AND a.cause_ref=c.audit_cause) FROM agenteam_project.commands c WHERE project_id=$1 AND command_name='update' AND key=$2`, v.project.ID.String(), key).Scan(&state, &pid, &final)
				if e != nil {
					return e
				}
				if state == phase {
					if phase == "completed" && !final {
						return f.NewFault(f.DependencyUnavailable, f.NotStarted)
					}
					backend.Store(pid)
					armed.Store(true)
					proxy.dropCommitACK.Store(true)
				}
				return nil
			}, func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if result.State() == f.Unknown && cause.Details().Primary.Canonical() == identity.Canonical() {
					original <- result
				}
				return result
			})
			response := v.write(t, v.ownerBrowser, "PATCH", projectOwnerReadPath(v.project.ID), key, body)
			if phase == "planned" {
				response.problem(t, 503, f.CommitUnknown)
			} else {
				response.want(t, 200)
			}
			waitSignal(t, proxy.ackDropped)
			var attempt f.CommitResult
			select {
			case attempt = <-original:
			default:
				t.Fatal("no actual Unknown")
			}
			if !armed.Load() || attempt.AttemptID().Validate() != nil {
				t.Fatal("missing phase/attempt")
			}
			trace := strings.Join(proxy.snapshot(), " ")
			if !strings.Contains(trace, "commit-idle-ack-dropped") || !strings.Contains(trace, fmt.Sprintf("backend-%d", backend.Load())) {
				t.Fatal("COMMIT not bound to target backend")
			}
			tracked.hooks(nil, nil)
			observed := v.write(t, v.ownerBrowser, "POST", projectUpdateLookupPath(v.project.ID), key, projectUpdateLookupBody).want(t, 200)
			want := "committed"
			if phase == "planned" {
				want = "in_progress"
			}
			if observed.object(t)["state"] != want {
				t.Fatal("planning conflated with success")
			}
			v.write(t, v.ownerBrowser, "PATCH", projectOwnerReadPath(v.project.ID), key, body).want(t, 200)
			after := projectUpdateSnapshot(t, v.projectUsageHTTPFixture)
			v.write(t, v.ownerBrowser, "PATCH", projectOwnerReadPath(v.project.ID), key, body).want(t, 200)
			if projectUpdateSnapshot(t, v.projectUsageHTTPFixture) != after {
				t.Fatal("uncertain replay duplicated facts")
			}
			t.Log("physical phase", phase, "backend", backend.Load(), "attempt", attempt.AttemptID().String(), "ACK dropped; actual writer terminal serialized")
		})
	}
	projectUpdateHeldConfirmation(t, v)
}

// This owned loopback proxy drops the client only after reading its actual
// COMMIT frame, retains the original live backend transaction, and later sends
// that same frame to PostgreSQL. No Store result is replaced.
type projectUpdateHeldProxy struct {
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
}

func newProjectUpdateHeldProxy(t *testing.T, upstream string, commit bool) *projectUpdateHeldProxy {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &projectUpdateHeldProxy{listener: listener, upstream: upstream, reached: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool), commit: commit}
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
func (p *projectUpdateHeldProxy) serve(client net.Conn) {
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
			frame, e := readModelPGFrame(server)
			if e != nil {
				return
			}
			if frame[0] == 'K' && len(frame) >= 9 {
				backend.Store(int32(binary.BigEndian.Uint32(frame[5:9])))
			}
			if held.Load() && frame[0] == 'C' && string(frame[5:]) == "COMMIT\x00" {
				ready, e := readModelPGFrame(server)
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

// Observes the actual confirmation context at the Store boundary without
// changing a result, callback, cancellation or transaction.
type projectUpdateConfirmStore struct {
	*projectUsageHTTPStore
	target    string
	uncertain atomic.Bool
	confirm   chan context.Context
}

func (s *projectUpdateConfirmStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	if cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == s.target && s.uncertain.Load() {
		select {
		case s.confirm <- ctx:
		default:
		}
	}
	result := s.projectUsageHTTPStore.WithinTx(ctx, cause, fn)
	if result.State() == f.Unknown && cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == s.target {
		s.uncertain.Store(true)
	}
	return result
}
func projectUpdateHeldConfirmation(t *testing.T, v *projectUpdateFixture) {
	for _, mode := range []string{"Stop", "revoke"} {
		t.Run("original-confirmation-"+mode, func(t *testing.T) {
			proxy := newProjectUpdateHeldProxy(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port), true)
			var once sync.Once
			release := func() { once.Do(func() { close(proxy.release) }) }
			defer release()
			u, e := url.Parse(v.db.Fixture.URL(v.db.Name))
			if e != nil {
				t.Fatal(e)
			}
			u.Host = proxy.listener.Addr().String()
			raw := openStore(t, v.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
			tracked := &projectUpdateConfirmStore{projectUsageHTTPStore: &projectUsageHTTPStore{Store: raw}, confirm: make(chan context.Context, 1)}
			service, _, _ := projectUpdateService(t, v.projectUsageHTTPFixture, tracked)
			old := v.handler
			v.install(t, service)
			defer func() { v.handler = old }()
			browser := v.login(t, v.ownerBrowser.email)
			current, e := v.projectService.GetProject(testContext(t), v.owner, v.project.ID)
			if e != nil {
				t.Fatal(e)
			}
			key := "held-" + mode
			identity, _ := pc.CommandIdentity(v.project.ID, pc.UpdateCommand, f.IdempotencyKey(key))
			tracked.target = identity.Canonical()
			description := "held-" + mode
			body := projectUpdateBody(t, current.Version, nil, &description)
			var armed atomic.Bool
			tracked.hooks(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
				if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != tracked.target || armed.Load() {
					return nil
				}
				x, e := raw.InTx(tx)
				if e != nil {
					return e
				}
				var state string
				var pid int32
				e = x.QueryRow(ctx, `SELECT state,pg_backend_pid() FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND key=$2`, v.project.ID.String(), key).Scan(&state, &pid)
				if e != nil {
					return e
				}
				if state == "completed" {
					armed.Store(true)
					proxy.targetPID.Store(pid)
				}
				return nil
			}, nil)
			ctx, cancel := context.WithCancel(testContext(t))
			defer cancel()
			w := &projectUsageRecorder{ResponseRecorder: httptest.NewRecorder()}
			done := make(chan bool, 1)
			joined := false
			var logout chan error
			logoutJoined := false
			defer func() {
				cancel()
				release()
				if !joined {
					<-done
				}
				if logout != nil && !logoutJoined {
					<-logout
				}
			}()
			go func() {
				done <- projectUpdateAbort(func() {
					v.handler.ServeHTTP(w, projectUpdateRequest(ctx, browser, "PATCH", projectOwnerReadPath(v.project.ID), key, body))
				})
			}()
			waitSignal(t, proxy.reached)
			cancel()
			var confirmation context.Context
			select {
			case confirmation = <-tracked.confirm:
			case <-time.After(time.Second):
				t.Fatal("original confirmation not entered")
			}
			deadline, ok := confirmation.Deadline()
			if !ok || confirmation.Err() != nil || time.Until(deadline) > 3*time.Second || time.Until(deadline) < 2*time.Second {
				t.Fatal("WithoutCancel original 3s deadline not retained")
			}
			commandLock, _ := f.CommandLock(identity)
			managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, commandLock)
			if mode == "Stop" {
				service.Stop()
			} else {
				logout = make(chan error, 1)
				go func() {
					logout <- v.core.Logout(testContext(t), account.LogoutRequest{Actor: browser.actor, Key: f.IdempotencyKey(newID[struct{}](t).String())})
				}()
				user, _ := f.UserLock(browser.actor.Details().UserID)
				managementWaitBlocked(t, &systemHTTPFixture{fixture: v.fixture}, user)
			}
			drainCtx, drainCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			err := service.Drain(drainCtx)
			drainCancel()
			if err == nil || confirmation.Err() != nil {
				t.Fatal("original call abandoned/Stop cancelled detached confirmation", err)
			}
			select {
			case <-done:
				joined = true
				t.Fatal("HTTP returned before actual confirmation")
			default:
			}
			release()
			waitSignal(t, proxy.completed)
			select {
			case aborted := <-done:
				joined = true
				if !aborted || w.Body.Len() != 0 {
					t.Fatal("late response after parent cancellation")
				}
			case <-time.After(4 * time.Second):
				t.Fatal("HTTP actual terminal")
			}
			if logout != nil {
				err := <-logout
				logoutJoined = true
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := service.Drain(testContext(t)); err != nil {
				t.Fatal("actual Drain after terminal", err)
			}
			if mode == "revoke" {
				v.write(t, browser, "POST", projectUpdateLookupPath(v.project.ID), key, projectUpdateLookupBody).problem(t, 401, f.SessionRevoked)
			}
			t.Log("real original writer held at final COMMIT; original <=3s confirmation remains live after parent/Stop; actual HTTP+Drain joined; no late publication", mode)
		})
	}
}
func projectUpdateAbort(fn func()) (aborted bool) {
	defer func() {
		if value := recover(); value != nil {
			if value != http.ErrAbortHandler {
				panic(value)
			}
			aborted = true
		}
	}()
	fn()
	return
}
