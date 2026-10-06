//go:build integration

package account_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	audithttp "github.com/LunaDeerTech/agenteam/internal/central/audit/http"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type auditHTTPReadTrace struct {
	locks                        []foundation.LockRequest
	sessions, authority, queries int
	state                        foundation.CommitState
}

type auditHTTPStore struct {
	*postgres.Store
	mu                  sync.Mutex
	reads               map[foundation.Tx]*auditHTTPReadTrace
	before, after, tail func(context.Context, foundation.Tx) error
	active              atomic.Int32
	last                foundation.CommitResult
}

func (s *auditHTTPStore) Acquire(ctx context.Context, tx foundation.Tx, key foundation.LockKey, mode foundation.LockMode) error {
	return s.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: mode}})
}

func (s *auditHTTPStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	if cause.Details().Owner != "audit.system-read" {
		return s.Store.WithinTx(ctx, cause, fn)
	}
	s.active.Add(1)
	defer s.active.Add(-1)
	trace := &auditHTTPReadTrace{}
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		s.mu.Lock()
		s.reads[tx] = trace
		s.mu.Unlock()
		err := fn(ctx, tx)
		s.mu.Lock()
		tail := s.tail
		s.mu.Unlock()
		if err == nil && tail != nil {
			return tail(ctx, tx)
		}
		return err
	})
	s.mu.Lock()
	trace.state = result.State()
	s.last = result
	s.mu.Unlock()
	return result
}
func (s *auditHTTPStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.mu.Lock()
	trace := s.reads[tx]
	before, after := s.before, s.after
	if trace != nil {
		trace.locks = append(trace.locks, locks...)
	}
	s.mu.Unlock()
	if trace == nil {
		return errors.New("Audit read transaction identity missing")
	}
	if before != nil {
		if err := before(ctx, tx); err != nil {
			return err
		}
	}
	if err := s.Store.AcquireAll(ctx, tx, locks); err != nil {
		return err
	}
	if after != nil {
		return after(ctx, tx)
	}
	return nil
}
func (s *auditHTTPStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	executor, err := s.Store.InTx(tx)
	if err != nil {
		return nil, err
	}
	return auditHTTPExecutor{executor, s, tx}, nil
}
func (s *auditHTTPStore) hooks(before, after, tail func(context.Context, foundation.Tx) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.before, s.after, s.tail = before, after, tail
}

type auditHTTPExecutor struct {
	postgres.SQLExecutor
	store *auditHTTPStore
	tx    foundation.Tx
}

func (e auditHTTPExecutor) capture(query string) {
	if !strings.Contains(query, "FROM agenteam_audit.audit_records") {
		return
	}
	e.store.mu.Lock()
	defer e.store.mu.Unlock()
	trace := e.store.reads[e.tx]
	if trace == nil || trace.sessions != 1 || trace.authority != 1 {
		panic("Audit SQL was not authorized in its same transaction")
	}
	trace.queries++
}
func (e auditHTTPExecutor) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	e.capture(query)
	return e.SQLExecutor.Query(ctx, query, args...)
}
func (e auditHTTPExecutor) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	e.capture(query)
	return e.SQLExecutor.QueryRow(ctx, query, args...)
}

type auditHTTPAuthority struct {
	*account.Authority
	store *auditHTTPStore
}

func (a auditHTTPAuthority) RequireCurrentSession(ctx context.Context, tx foundation.Tx, actor identity.Actor) error {
	if !tx.Valid() {
		return errors.New("Audit Session was checked outside read Tx")
	}
	a.store.mu.Lock()
	trace := a.store.reads[tx]
	if trace != nil {
		trace.sessions++
	}
	a.store.mu.Unlock()
	if trace == nil {
		return errors.New("Audit Session Tx not owned")
	}
	return a.Authority.RequireCurrentSession(ctx, tx, actor)
}
func (a auditHTTPAuthority) AuthorizeSystem(ctx context.Context, tx foundation.Tx, actor identity.Actor, intent identity.AccessIntent) (identity.AccessGrant, error) {
	a.store.mu.Lock()
	trace := a.store.reads[tx]
	valid := trace != nil && trace.sessions == 1 && intent == identity.Read
	if valid {
		trace.authority++
	}
	a.store.mu.Unlock()
	if !valid {
		return identity.AccessGrant{}, errors.New("Audit current admin check has wrong Tx/order")
	}
	return a.Authority.AuthorizeSystem(ctx, tx, actor, intent)
}

func TestSystemAuditHTTPTransactionAndBudget(t *testing.T) {
	started := time.Now()
	f := newB02Account(t).fixture
	password := f.bootstrap(t)
	_, cursor, _ := keys(t)
	store := &auditHTTPStore{Store: f.store, reads: map[foundation.Tx]*auditHTTPReadTrace{}}
	authority := auditHTTPAuthority{f.authority, store}
	service, err := audit.New(store, cursor, audit.Authorizations{Sessions: authority, System: authority})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := audithttp.NewSystemHTTPHandler(service, f.service, audithttp.SystemHTTPOptions{PublicOrigin: httpOrigin})
	if err != nil {
		t.Fatal(err)
	}
	handler = httpapi.Handler(nil, handler)
	var detailID string
	if err = f.store.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_audit.audit_records WHERE action='account.bootstrap' ORDER BY created_at DESC,id DESC LIMIT 1`).Scan(&detailID); err != nil {
		t.Fatal(err)
	}
	login := func(t *testing.T) (identity.Actor, string) {
		t.Helper()
		request, _ := loginRequest(t, f, password, "admin@mail.com")
		response, err := f.service.Login(ctxFor(t), request)
		if err != nil {
			t.Fatal(err)
		}
		cookie := useCookie(t, response)
		if err = response.Close(ctxFor(t)); err != nil {
			t.Fatal(err)
		}
		actor, err := f.service.Authenticate(ctxFor(t), cookie)
		if err != nil {
			t.Fatal(err)
		}
		var value string
		if err = cookie.Use(func(b []byte) error { value = string(b); return nil }); err != nil {
			t.Fatal(err)
		}
		return actor, value
	}
	request := func(t *testing.T, ctx context.Context, cookie, path string) *http.Request {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, "GET", httpOrigin+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", httpOrigin)
		r.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: cookie})
		return r
	}
	releaseGate := func() (<-chan struct{}, func()) {
		release := make(chan struct{})
		var once sync.Once
		return release, func() { once.Do(func() { close(release) }) }
	}
	waitGate := func(t *testing.T, ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatal("formal read seam not reached")
		}
	}
	for _, path := range []string{systemAuditHTTPPath, systemAuditHTTPPath + "/" + detailID} {
		t.Run("preauthenticated_then_revoked_"+path, func(t *testing.T) {
			actor, cookie := login(t)
			entered := make(chan struct{})
			release, finish := releaseGate()
			defer finish()
			var once sync.Once
			store.hooks(func(ctx context.Context, _ foundation.Tx) error {
				once.Do(func() { close(entered) })
				select {
				case <-release:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}, nil, nil)
			defer store.hooks(nil, nil, nil)
			done := outboundHTTPStart(t, handler, request(t, ctxFor(t), cookie, path))
			waitGate(t, entered)
			if err := f.service.Logout(ctxFor(t), account.LogoutRequest{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}); err != nil {
				t.Fatal("formal revoke", err)
			}
			finish()
			got := outboundHTTPJoin(t, done)
			if got.aborted {
				t.Fatal("live revoked request aborted")
			}
			got.response.problem(t, 401, "SESSION_REVOKED")
			if bytes.Contains(got.response.data, []byte(`"items"`)) || bytes.Contains(got.response.data, []byte(`"audit_id"`)) || store.active.Load() != 0 {
				t.Fatal("stale preauthentication published candidate or released early")
			}
		})
	}
	t.Run("held_shared_serializes_formal_logout", func(t *testing.T) {
		actor, cookie := login(t)
		entered := make(chan uint32, 1)
		release, finish := releaseGate()
		defer finish()
		store.hooks(nil, func(ctx context.Context, tx foundation.Tx) error {
			key, _ := foundation.UserLock(actor.Details().UserID)
			if err := f.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); err != nil {
				return err
			}
			executor, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			var pid uint32
			if err = executor.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			entered <- pid
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}, nil)
		defer store.hooks(nil, nil, nil)
		done := outboundHTTPStart(t, handler, request(t, ctxFor(t), cookie, systemAuditHTTPPath))
		var pid uint32
		select {
		case pid = <-entered:
		case <-time.After(time.Second):
			t.Fatal("User SH not observed")
		}
		revokeCtx, cancel := context.WithCancel(ctxFor(t))
		revoked := make(chan error, 1)
		joined := make(chan struct{})
		t.Cleanup(func() {
			cancel()
			finish()
			select {
			case <-joined:
			case <-time.After(2 * time.Second):
				t.Error("formal Logout did not actually join")
			}
		})
		go func() {
			defer close(joined)
			revoked <- f.service.Logout(revokeCtx, account.LogoutRequest{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())})
		}()
		outboundHTTPWaitBlocked(t, f.store, pid)
		select {
		case <-revoked:
			t.Fatal("Logout passed User SH")
		default:
		}
		finish()
		got := outboundHTTPJoin(t, done)
		if got.aborted {
			t.Fatal("serialized reader aborted")
		}
		got.response.want(t, 200)
		select {
		case err := <-revoked:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Logout failed after committed reader")
		}
		<-joined
		cancel()
		store.hooks(nil, nil, nil)
		next := outboundHTTPJoin(t, outboundHTTPStart(t, handler, request(t, ctxFor(t), cookie, systemAuditHTTPPath)))
		next.response.problem(t, 401, "SESSION_REVOKED")
	})
	t.Run("candidate_waits_for_real_transaction_terminal", func(t *testing.T) {
		_, cookie := login(t)
		entered := make(chan struct{})
		release, finish := releaseGate()
		defer finish()
		store.hooks(nil, nil, func(ctx context.Context, tx foundation.Tx) error {
			store.mu.Lock()
			trace := store.reads[tx]
			valid := trace != nil && trace.queries == 1
			store.mu.Unlock()
			if !valid {
				return errors.New("candidate gate before complete SQL")
			}
			close(entered)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		defer store.hooks(nil, nil, nil)
		done := outboundHTTPStart(t, handler, request(t, ctxFor(t), cookie, systemAuditHTTPPath))
		waitGate(t, entered)
		if store.active.Load() != 1 {
			t.Fatal("real transaction no longer owned")
		}
		select {
		case <-done:
			t.Fatal("candidate escaped before actual commit")
		default:
		}
		finish()
		got := outboundHTTPJoin(t, done)
		got.response.want(t, 200)
		store.mu.Lock()
		state := store.last.State()
		store.mu.Unlock()
		if got.aborted || state != foundation.Committed || store.active.Load() != 0 {
			t.Fatal("normal commit/actual owner terminal missing")
		}
	})
	if f.db.Config(t, nil).LockTimeout() != time.Second {
		t.Fatal("fixture DB lock budget changed")
	}
	for _, detail := range []bool{false, true} {
		for _, early := range []bool{false, true} {
			name := "list"
			path := systemAuditHTTPPath
			if detail {
				name = "detail"
				path += "/" + detailID
			}
			if early {
				name += "_earlier_parent"
			} else {
				name += "_earlier_database_timeout"
			}
			t.Run(name, func(t *testing.T) {
				actor, cookie := login(t)
				conn := f.db.Connect(t)
				tx, err := conn.Begin(ctxFor(t))
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					ctx, cancel := context.WithTimeout(context.Background(), time.Second)
					defer cancel()
					_ = tx.Rollback(ctx)
				}()
				entered := make(chan context.Context, 1)
				release, finish := releaseGate()
				defer finish()
				store.hooks(func(ctx context.Context, _ foundation.Tx) error {
					entered <- ctx
					select {
					case <-release:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				}, nil, nil)
				defer store.hooks(nil, nil, nil)
				budget := 5 * time.Second
				if early {
					budget = 250 * time.Millisecond
				}
				ctx, cancel := context.WithTimeout(context.Background(), budget)
				defer cancel()
				start := time.Now()
				done := outboundHTTPStart(t, handler, request(t, ctx, cookie, path))
				var actual context.Context
				select {
				case actual = <-entered:
				case <-time.After(time.Second):
					t.Fatal("HTTP preauthentication did not enter Audit Tx")
				}
				// Acquire the competing lock only after HTTP preauthentication; otherwise
				// the old Account precheck, rather than this read Tx, would be measured.
				key, _ := foundation.UserLock(actor.Details().UserID)
				if _, err = tx.Exec(ctxFor(t), `SELECT pg_advisory_xact_lock($1)`, key.AdvisoryKey()); err != nil {
					t.Fatal(err)
				}
				finish()
				outboundHTTPWaitBlocked(t, f.store, conn.PgConn().PID())
				got := outboundHTTPJoin(t, done)
				elapsed := time.Since(start)
				store.mu.Lock()
				result := store.last
				store.mu.Unlock()
				var databaseError *postgres.Error
				if store.active.Load() != 0 || result.State() != foundation.NotCommitted || result.Fault() == nil || !errors.As(result.Fault(), &databaseError) || databaseError.Code() != postgres.LockFailed {
					t.Fatal("read lock failure lost actual terminal/typed state")
				}
				if bytes.Contains(got.response.data, []byte(`"items"`)) || bytes.Contains(got.response.data, []byte(`"audit_id"`)) {
					t.Fatal("failed read candidate exposed")
				}
				actualDeadline, _ := actual.Deadline()
				parentDeadline, _ := ctx.Deadline()
				if actualDeadline.After(parentDeadline) || actualDeadline.After(time.Now().Add(3*time.Second)) {
					t.Fatal("read extended HTTP/parent budget")
				}
				if early {
					if ctx.Err() != context.DeadlineExceeded || !got.aborted || len(got.response.data) != 0 || elapsed > budget+500*time.Millisecond {
						t.Fatal("expired parent response or unjoined tail", elapsed)
					}
				} else {
					if ctx.Err() != nil || got.aborted || databaseError.SQLState() != "55P03" || elapsed > 3*time.Second {
						t.Fatal("DB timeout confused with local deadline", elapsed)
					}
					got.response.problem(t, 500, "INTERNAL_ERROR")
				}
				if err = tx.Rollback(ctxFor(t)); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s actual_join=%s typed=%s SQLSTATE=%q parent_expired=%t error_is_deadline=%t", name, elapsed, databaseError.Code(), databaseError.SQLState(), ctx.Err() == context.DeadlineExceeded, errors.Is(result.Fault(), context.DeadlineExceeded))
			})
		}
	}
	store.mu.Lock()
	for tx, trace := range store.reads {
		if !tx.Valid() || len(trace.locks) != 1 || trace.locks[0].Mode != foundation.Shared {
			t.Error("wrong read transaction/lock set")
		}
		if trace.state == foundation.Committed && (trace.sessions != 1 || trace.authority != 1 || trace.queries != 1) {
			t.Error("successful read did not use one same-Tx current authority and query")
		}
	}
	store.mu.Unlock()
	if time.Since(started) > 2*time.Minute {
		t.Fatal("scenario exceeded two-minute budget")
	}
}
