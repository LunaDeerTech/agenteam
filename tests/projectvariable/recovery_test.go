//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	auditc "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Adapted owned real-COMMIT frame proxy: backend PID binds the exact writer.
type commitProxy struct {
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

func newCommitProxy(t *testing.T, upstream string, commit bool) *commitProxy {
	t.Helper()
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	p := &commitProxy{listener: listener, upstream: upstream, reached: make(chan struct{}), release: make(chan struct{}), completed: make(chan struct{}), quit: make(chan struct{}), connections: make(map[net.Conn]bool), commit: commit}
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
			select {
			case <-p.quit:
				p.mu.Unlock()
				_ = client.Close()
				return
			default:
				p.connections[client] = true
			}
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
			joined := make(chan struct{})
			go func() { p.wg.Wait(); close(joined) }()
			select {
			case <-joined:
			case <-time.After(5 * time.Second):
				t.Error("owned COMMIT proxy did not actually join")
			}

		})
	})
	return p
}
func (p *commitProxy) serve(client net.Conn) {
	defer p.wg.Done()
	defer func() { _ = client.Close(); p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	server, e := net.DialTimeout("tcp", p.upstream, 3*time.Second)
	if e != nil {
		return
	}
	defer func() { _ = server.Close(); p.mu.Lock(); delete(p.connections, server); p.mu.Unlock() }()
	p.mu.Lock()
	select {
	case <-p.quit:
		p.mu.Unlock()
		return
	default:
		p.connections[server] = true
	}
	p.mu.Unlock()
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
			frame, e := readPGFrame(client)
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
			frame, e := readPGFrame(server)
			if e != nil {
				return
			}
			if frame[0] == 'K' && len(frame) >= 9 {
				backend.Store(int32(binary.BigEndian.Uint32(frame[5:9])))
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

// Rebind every in-Tx authority, Audit and Outbox to the same physical proxy
// Store. Public HTTP authentication still uses the real Account Service; its
// separate preauthentication is not substituted for this final authorization.
func proxyVariableService(t *testing.T, v *variableHTTPFixture, forwarded bool) (*pv.Service, *hookStore, *commitProxy) {
	t.Helper()
	proxy := newCommitProxy(t, net.JoinHostPort("127.0.0.1", v.db.Fixture.Port), forwarded)
	u, e := url.Parse(v.db.Fixture.URL(v.db.Name))
	if e != nil {
		t.Fatal(e)
	}
	u.Host = proxy.listener.Addr().String()
	raw := openStore(t, v.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable", "LOCK_TIMEOUT": "5s"}))
	store := &hookStore{fixtureStore: raw}
	ak, ck := keys(t)
	accounts, e := account.NewAuthority(store, ak)
	if e != nil {
		t.Fatal(e)
	}
	if e = accounts.Initialize(ctxFor(t)); e != nil {
		t.Fatal(e)
	}
	va, e := pv.NewAuthority(store)
	if e != nil {
		t.Fatal(e)
	}
	pa, e := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts, AuditFacts: map[auditc.Producer]auditc.ProjectFactAuthority{auditc.ProjectVariableProducer: va}})
	if e != nil {
		t.Fatal(e)
	}
	aud, e := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: pa})
	if e != nil {
		t.Fatal(e)
	}
	catalog := event.NewCatalog()
	factory, e := vc.RegisterVariableEvents(catalog)
	if e != nil {
		t.Fatal(e)
	}
	box, e := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{vc.VariableProducer: va}, Sessions: accounts, System: accounts, Projects: pa, Audit: aud, Cursors: ck, Processes: fixtureProcess{id[oc.Process](t)}})
	if e != nil {
		t.Fatal(e)
	}
	service, e := pv.New(store, pv.Dependencies{Authority: va, Projects: pa, Events: box, VariableEvents: factory, Audit: aud, Activity: accounts, Cursors: ck})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		service.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := service.Drain(ctx); e != nil {
			t.Error("proxy service did not join", e)
		}
	})
	return service, store, proxy
}
func armVariableProxy(t *testing.T, store *hookStore, proxy *commitProxy, i variableIntent, phase string) (<-chan lockAttempt, <-chan f.CommitResult) {
	t.Helper()
	key, _ := f.CommandLock(i.identity)
	observed := make(chan lockAttempt, 1)
	original := make(chan f.CommitResult, 1)
	var armed, once atomic.Bool
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != i.identity.Canonical() || armed.Load() {
			return nil
		}
		x, e := store.InTx(tx)
		if e != nil {
			return e
		}
		var state string
		if e = x.QueryRow(ctx, `SELECT coalesce((SELECT state FROM agenteam_projectvariable.commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3),'')`, i.project.String(), string(i.command), string(i.meta.IdempotencyKey)).Scan(&state); e != nil {
			return e
		}
		if state == phase && armed.CompareAndSwap(false, true) {
			var pid int32
			if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				return e
			}
			proxy.targetPID.Store(pid)
		}
		return nil
	})
	store.mu.Lock()
	store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
		if cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == i.identity.Canonical() && result.State() == f.Unknown {
			select {
			case original <- result:
			default:
			}
		}
	}
	store.beforeLocks = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
		select {
		case <-proxy.reached:
		default:
			return nil
		}
		for _, request := range locks {
			if f.CompareLockKeys(request.Key, key) != 0 || !once.CompareAndSwap(false, true) {
				continue
			}
			x, e := store.InTx(tx)
			if e != nil {
				return e
			}
			var pid int32
			if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				return e
			}
			observed <- lockAttempt{pid, request}
			break
		}
		return nil
	}
	store.mu.Unlock()
	return observed, original
}

func TestProjectVariableHTTPUnknown(t *testing.T) {
	v := newVariableHTTPFixture(t)
	type scenario struct {
		name             string
		command          vc.CommandName
		phase            string
		forwarded, early bool
	}
	var cases []scenario
	for _, command := range []vc.CommandName{vc.CreateCommand, vc.UpdateCommand, vc.DeleteCommand} {
		for _, forwarded := range []bool{false, true} {
			cases = append(cases, scenario{fmt.Sprintf("%s/final-forwarded=%t", command, forwarded), command, "completed", forwarded, false})
		}
	}
	cases = append(cases, scenario{"planned-unforwarded", vc.CreateCommand, "planned", false, false}, scenario{"built-in-confirmation-success", vc.CreateCommand, "completed", true, true}, scenario{"built-in-not-observed-remains-unknown", vc.CreateCommand, "planned", false, true}, scenario{"built-in-planned-remains-unknown", vc.CreateCommand, "planned", true, true})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i := v.intent(t, tc.command)
			service, store, proxy := proxyVariableService(t, v, tc.forwarded)
			v.bindService(t, service)
			defer v.bindService(t, v.service)
			attempts, original := armVariableProxy(t, store, proxy, i, tc.phase)
			var once sync.Once
			release := func() { once.Do(func() { close(proxy.release) }) }
			defer release()
			before := v.snapshot(t)
			g, h, au, ev := v.counts(t, i.project)
			writer := httpAsync(t, v, variableHTTPRequest(ctxFor(t), v.ownerBrowser, i.method, i.path, i.body, i.meta.IdempotencyKey))
			select {
			case <-proxy.reached:
			case early := <-writer:
				t.Fatalf("HTTP returned before real COMMIT status=%d", early.status)
			case <-time.After(5 * time.Second):
				t.Fatal("real COMMIT frame not reached")
			}
			var attempt lockAttempt
			select {
			case attempt = <-attempts:
			case early := <-writer:
				t.Fatalf("HTTP returned before confirmation lock %d", early.status)
			case <-time.After(3 * time.Second):
				t.Fatal("confirmation did not request exact lock")
			}
			if proxy.backendPID.Load() <= 0 || attempt.pid == proxy.backendPID.Load() {
				t.Fatal("confirmation did not use independent physical Tx")
			}
			v.waitLock(t, attempt, false, proxy.backendPID.Load())
			if before != v.snapshot(t) {
				t.Fatal("held COMMIT exposed persistent business facts")
			}
			if tc.early {
				release()
				await(t, proxy.completed)
			}
			response := httpReply(t, writer)
			if tc.early && tc.forwarded && tc.phase == "completed" {
				mutationHTTP(t, response)
			} else {
				problem := requireProblem(t, response, f.CommitUnknown)
				if problem.CommitState != f.Unknown || problem.RetryHint != "lookup" {
					t.Fatal("Unknown HTTP state/hint changed")
				}
			}
			select {
			case result := <-original:
				if result.AttemptID().Validate() != nil || result.Cause().Details().Primary.Canonical() != i.identity.Canonical() {
					t.Fatal("original physical attempt/cause lost")
				}
			default:
				t.Fatal("no actual Store Unknown observed")
			}
			release()
			await(t, proxy.completed)
			v.bindService(t, v.service)
			fresh := v.login(t, v.ownerBrowser.email)
			looked := lookupHTTP(t, v.lookupHTTP(t, fresh, i))
			want := vc.LookupInProgress
			if tc.phase == "planned" && !tc.forwarded {
				want = vc.LookupNotObserved
			}
			if tc.phase == "completed" && tc.forwarded {
				want = vc.LookupCommitted
			}
			if looked.Status() != want {
				t.Fatalf("serialized late status=%s want=%s", looked.Status(), want)
			}
			recovered := mutationHTTP(t, v.send(t, fresh, i))
			if looked.Receipt() != nil {
				sameReceipt(t, *looked.Receipt(), recovered)
			}
			if gotG, gotH, gotA, gotE := v.counts(t, i.project); gotG != g+1 || gotH != h+1 || gotA != au+1 || gotE != ev+1 {
				t.Fatal("Unknown recovery did not produce exactly one fact")
			}
			stable := v.snapshot(t)
			sameReceipt(t, recovered, mutationHTTP(t, v.send(t, fresh, i)))
			sameReceipt(t, recovered, *lookupHTTP(t, v.lookupHTTP(t, fresh, i)).Receipt())
			if stable != v.snapshot(t) {
				t.Fatal("Unknown replay duplicated facts")
			}
			var commands int
			if e := v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_projectvariable.commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, i.project.String(), string(i.command), string(i.meta.IdempotencyKey)).Scan(&commands); e != nil || commands != 1 {
				t.Fatal("original operation identity replaced", e)
			}
		})
	}
}

func TestProjectVariableUnknownStopJoin(t *testing.T) {
	v := newVariableHTTPFixture(t)
	i := v.intent(t, vc.CreateCommand)
	service, store, proxy := proxyVariableService(t, v, true)
	attempts, original := armVariableProxy(t, store, proxy, i, "completed")
	request := i.request.(vc.VariableCreate)
	result := asyncVariable(t, func(ctx context.Context) (vc.VariableMutation, error) {
		return service.CreateVariable(ctx, v.ownerBrowser.actor, i.meta, i.project, request)
	})
	var once sync.Once
	release := func() { once.Do(func() { close(proxy.release) }) }
	defer release()
	awaitStage(t, proxy.reached, result)
	attempt := waitAttempt(t, attempts, result)
	v.waitLock(t, attempt, false, proxy.backendPID.Load())
	service.Stop()
	got := reply(t, result)
	requireCode(t, got.err, f.CommitUnknown)
	var fault *f.Fault
	if !errors.As(got.err, &fault) || fault.CommitState != f.Unknown || fault.RetryHint != "lookup" {
		t.Fatal("Stop rewrote unknown")
	}
	select {
	case original := <-original:
		if fault.CauseID != original.AttemptID().String() {
			t.Fatal("Stop lost original physical AttemptID")
		}
	default:
		t.Fatal("missing actual Unknown")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if e := service.Drain(ctx); e != nil {
		t.Fatal("confirmation did not actually join", e)
	}
	_, e := service.CreateVariable(ctxFor(t), v.ownerBrowser.actor, i.meta, i.project, request)
	requireCode(t, e, f.ShuttingDown)
	release()
	await(t, proxy.completed)
	gotLookup := lookupHTTP(t, v.lookupHTTP(t, v.ownerBrowser, i))
	if gotLookup.Status() != vc.LookupCommitted {
		t.Fatal("late committed fact missing")
	}
}

// Only the actual returned PostgreSQL Rows is held before delivery to the
// reader. No synthetic Rows, buffered result, or CommitResult is substituted.
type heldRowStore struct {
	*hookStore
	entered chan struct{}
	pid     atomic.Int32
	once    atomic.Bool
}
type heldRowExecutor struct {
	postgres.SQLExecutor
	owner *heldRowStore
}

func (s *heldRowStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	x, e := s.hookStore.InTx(tx)
	if e != nil {
		return nil, e
	}
	return heldRowExecutor{x, s}, nil
}
func (x heldRowExecutor) Query(ctx context.Context, sql string, args ...any) (*postgres.Rows, error) {
	held := strings.Contains(sql, "FROM agenteam_projectvariable.variables") && strings.Contains(sql, "ORDER BY name") && x.owner.once.CompareAndSwap(false, true)
	if held {
		var pid int32
		if e := x.SQLExecutor.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
			return nil, e
		}
		x.owner.pid.Store(pid)
	}
	rows, e := x.SQLExecutor.Query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	if held {
		close(x.owner.entered)
		<-ctx.Done()
	}
	return rows, nil
}
func TestProjectVariableReadCancellationJoin(t *testing.T) {
	v := newVariableHTTPFixture(t)
	v.createVariable(t, "A", "a")
	v.createVariable(t, "B", "b")
	store := &heldRowStore{hookStore: v.tracked, entered: make(chan struct{})}
	authority, e := pv.NewAuthority(store)
	if e != nil {
		t.Fatal(e)
	}
	reader, e := pv.New(store, pv.Dependencies{Authority: authority, Projects: v.projectAuthority, Events: v.events, VariableEvents: v.variableEvents, Audit: v.audit, Activity: v.accounts, Cursors: v.keys})
	if e != nil {
		t.Fatal(e)
	}
	// This separate service is used only for read/cancellation; no Outbox issuer
	// fact or mutation is supplied by the Rows observation wrapper.
	t.Cleanup(func() {
		reader.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := reader.Drain(ctx); e != nil {
			t.Error("held reader did not join", e)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type pageReply struct {
		page f.Page[vc.VariableSummary]
		err  error
	}
	out := make(chan pageReply, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		page, e := reader.ListVariables(ctx, v.ownerBrowser.actor, v.project.ID, f.PageRequest{Limit: 1})
		out <- pageReply{page, e}
	}()
	t.Cleanup(func() { cancel(); await(t, done) })
	select {
	case <-store.entered:
	case got := <-out:
		t.Fatal("real rows were not held", got.err)
	case <-time.After(5 * time.Second):
		t.Fatal("list did not reach actual Rows")
	}
	user, _ := f.UserLock(v.ownerBrowser.actor.Details().UserID)
	attempts := observeLock(v.tracked, user)
	input := createInput(t, "C", "writer")
	m := meta(t, "after-reader", nil)
	writer := asyncVariable(t, func(ctx context.Context) (vc.VariableMutation, error) {
		return v.service.CreateVariable(ctx, v.ownerBrowser.actor, m, v.project.ID, input)
	})
	attempt := waitAttempt(t, attempts, writer)
	v.waitLock(t, attempt, false, store.pid.Load())
	cancel()
	await(t, done)
	got := <-out
	if got.err == nil || !errors.Is(got.err, context.Canceled) || len(got.page.Items) != 0 || got.page.NextCursor != "" {
		t.Fatal("cancelled actual Rows returned a partial page", got.err)
	}
	if got := reply(t, writer); got.err != nil {
		t.Fatal("reader lock did not release to writer", got.err)
	}
}
