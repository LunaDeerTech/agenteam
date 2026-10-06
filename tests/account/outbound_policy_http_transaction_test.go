//go:build integration

package account_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	outboundhttp "github.com/LunaDeerTech/agenteam/internal/central/outbound/http"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This observer delegates every actual transaction/lock to PostgreSQL. Its
// barriers only choose when the real PolicyService crosses an existing seam.
type outboundHTTPStore struct {
	*postgres.Store
	mu            sync.Mutex
	before, after func(context.Context, foundation.Tx, []foundation.LockRequest) error
	last          foundation.CommitResult
	active        atomic.Int64
}

func (s *outboundHTTPStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.active.Add(1)
	defer s.active.Add(-1)
	result := s.Store.WithinTx(ctx, cause, fn)
	s.mu.Lock()
	s.last = result
	s.mu.Unlock()
	return result
}
func (s *outboundHTTPStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.mu.Lock()
	before, after := s.before, s.after
	s.mu.Unlock()
	if before != nil {
		if err := before(ctx, tx, locks); err != nil {
			return err
		}
	}
	if err := s.Store.AcquireAll(ctx, tx, locks); err != nil {
		return err
	}
	if after != nil {
		return after(ctx, tx, locks)
	}
	return nil
}
func (s *outboundHTTPStore) hooks(before, after func(context.Context, foundation.Tx, []foundation.LockRequest) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.before, s.after = before, after
}

// A deadline-capable controlled writer is used only for this real-DB group.
// Native socket deadlines and response-loss are proved by their separate tests.
type outboundHTTPRecorder struct {
	*httptest.ResponseRecorder
	mu                          sync.Mutex
	readDeadline, writeDeadline time.Time
}

func (w *outboundHTTPRecorder) SetReadDeadline(v time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.readDeadline = v
	return nil
}
func (w *outboundHTTPRecorder) SetWriteDeadline(v time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeDeadline = v
	return nil
}

type outboundHTTPCall struct {
	response   httpResult
	aborted    bool
	unexpected bool
}

func outboundHTTPStart(t *testing.T, h http.Handler, r *http.Request) <-chan outboundHTTPCall {
	t.Helper()
	ctx, cancel := context.WithCancel(r.Context())
	r = r.WithContext(ctx)
	done := make(chan outboundHTTPCall, 1)
	finished := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Error("failed-case HTTP/transaction callback did not actually join")
		}
	})
	go func() {
		defer close(finished)
		w := &outboundHTTPRecorder{ResponseRecorder: httptest.NewRecorder()}
		defer func() {
			p := recover()
			w.mu.Lock()
			cleared := w.readDeadline.IsZero() && w.writeDeadline.IsZero()
			w.mu.Unlock()
			done <- outboundHTTPCall{response: httpResult{w.Code, w.Header().Clone(), bytes.Clone(w.Body.Bytes())}, aborted: p == http.ErrAbortHandler, unexpected: p != nil && p != http.ErrAbortHandler || !cleared}
		}()
		h.ServeHTTP(w, r)
	}()
	return done
}
func outboundHTTPJoin(t *testing.T, done <-chan outboundHTTPCall) outboundHTTPCall {
	t.Helper()
	select {
	case got := <-done:
		if got.unexpected {
			t.Fatal("unexpected handler panic or unjoined deadline cleanup")
		}
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("actual HTTP/transaction tail did not return")
	}
	return outboundHTTPCall{}
}

func TestSystemOutboundPolicyHTTPTransactionAndBudget(t *testing.T) {
	started := time.Now()
	f := newB02Account(t).fixture
	password := f.bootstrap(t)
	_, cursor, _ := keys(t)
	aud, err := audit.New(f.store, cursor, audit.Authorizations{Sessions: f.authority, System: f.authority})
	if err != nil {
		t.Fatal(err)
	}
	store := &outboundHTTPStore{Store: f.store}
	policy, err := outbound.NewPolicyService(store, aud, outbound.Authorizations{Sessions: f.authority, System: f.authority})
	if err != nil {
		t.Fatal(err)
	}
	if err = policy.Reload(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	handler, err := outboundhttp.NewSystemHTTPHandler(policy, f.service, outboundhttp.SystemHTTPOptions{PublicOrigin: httpOrigin})
	if err != nil {
		t.Fatal(err)
	}
	handler = httpapi.Handler(nil, handler)
	login := func() (identity.Actor, string, string) {
		t.Helper()
		request, _ := loginRequest(t, f, password, "admin@mail.com")
		response, err := f.service.Login(ctxFor(t), request)
		if err != nil {
			t.Fatal(err)
		}
		material := useCookie(t, response)
		if err = response.Close(ctxFor(t)); err != nil {
			t.Fatal(err)
		}
		actor, err := f.service.Authenticate(ctxFor(t), material)
		if err != nil {
			t.Fatal(err)
		}
		session, err := f.service.GetSession(ctxFor(t), material)
		if err != nil {
			t.Fatal(err)
		}
		defer session.CSRF.Destroy()
		var cookie, csrf string
		if err = material.Use(func(b []byte) error { cookie = string(b); return nil }); err != nil {
			t.Fatal(err)
		}
		if err = session.CSRF.Use(func(b []byte) error { csrf = string(b); return nil }); err != nil {
			t.Fatal(err)
		}
		return actor, cookie, csrf
	}
	request := func(ctx context.Context, method, cookie, csrf string) *http.Request {
		t.Helper()
		var body []byte
		if method == "PUT" {
			body = []byte(`{"expected_version":"1","rules":[]}`)
		}
		r, err := http.NewRequestWithContext(ctx, method, httpOrigin+outboundPolicyHTTPPath, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Origin", httpOrigin)
		r.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: cookie})
		if method == "PUT" {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
			r.Header.Set("Idempotency-Key", id[struct{}](t).String())
		}
		return r
	}
	for _, method := range []string{"GET", "PUT"} {
		t.Run("preauthenticated_then_revoked_"+method, func(t *testing.T) {
			actor, cookie, csrf := login()
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			finish := func() { once.Do(func() { close(release) }) }
			defer finish()
			var armed atomic.Bool
			armed.Store(true)
			store.hooks(func(ctx context.Context, _ foundation.Tx, _ []foundation.LockRequest) error {
				if armed.CompareAndSwap(true, false) {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				return nil
			}, nil)
			defer store.hooks(nil, nil)
			done := outboundHTTPStart(t, handler, request(ctxFor(t), method, cookie, csrf))
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("preauthentication did not reach real policy transaction")
			}
			if err := f.service.Logout(ctxFor(t), account.LogoutRequest{Actor: actor, Key: foundation.IdempotencyKey(id[struct{}](t).String())}); err != nil {
				t.Fatal("formal revocation failed", err)
			}
			finish()
			got := outboundHTTPJoin(t, done)
			if got.aborted {
				t.Fatal("live revoked request was aborted")
			}
			got.response.problem(t, 401, "SESSION_REVOKED")
			if bytes.Contains(got.response.data, []byte(`"rules"`)) {
				t.Fatal("revocation published a policy candidate")
			}
		})
	}
	t.Run("held_user_shared_serializes_formal_revoke", func(t *testing.T) {
		actor, cookie, csrf := login()
		entered := make(chan uint32, 1)
		release := make(chan struct{})
		var once sync.Once
		finish := func() { once.Do(func() { close(release) }) }
		defer finish()
		var armed atomic.Bool
		armed.Store(true)
		store.hooks(nil, func(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
			if !armed.CompareAndSwap(true, false) {
				return nil
			}
			user, _ := foundation.UserLock(actor.Details().UserID)
			policyKey, _ := foundation.SystemConfigLock("outbound-policy")
			if err := f.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: user, Mode: foundation.Shared}, {Key: policyKey, Mode: foundation.Shared}}); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			var pid uint32
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			entered <- pid
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		defer store.hooks(nil, nil)
		done := outboundHTTPStart(t, handler, request(ctxFor(t), "GET", cookie, csrf))
		var pid uint32
		select {
		case pid = <-entered:
		case <-time.After(time.Second):
			t.Fatal("real shared authority locks not observed")
		}
		revoked := make(chan error, 1)
		key := foundation.IdempotencyKey(id[struct{}](t).String())
		revokeCtx, revokeCancel := context.WithCancel(ctxFor(t))
		revokeFinished := make(chan struct{})
		t.Cleanup(func() {
			revokeCancel()
			select {
			case <-revokeFinished:
			case <-time.After(2 * time.Second):
				t.Error("formal revocation callback did not actually join")
			}
		})
		go func() {
			defer close(revokeFinished)
			revoked <- f.service.Logout(revokeCtx, account.LogoutRequest{Actor: actor, Key: key})
		}()
		outboundHTTPWaitBlocked(t, f.store, pid)
		select {
		case <-revoked:
			t.Fatal("revocation passed the admitted User SH transaction")
		default:
		}
		finish()
		got := outboundHTTPJoin(t, done)
		got.response.want(t, 200)
		select {
		case err := <-revoked:
			if err != nil {
				t.Fatal("formal revoke did not complete after reader", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("formal revoke tail did not join")
		}
		if _, err := policy.GetPolicy(ctxFor(t), actor); !hasCode(err, foundation.SessionRevoked) {
			t.Fatal("completed revoke did not affect following read")
		}
	})

	_, cookie, csrf := login()
	if f.db.Config(t, nil).LockTimeout() != time.Second {
		t.Fatal("fixed PG one-second lock timeout changed")
	}
	for _, method := range []string{"GET", "PUT"} {
		for _, earlyParent := range []bool{true, false} {
			name := method + "_database_timeout"
			if earlyParent {
				name = method + "_earlier_parent"
			}
			t.Run(name, func(t *testing.T) {
				conn := f.db.Connect(t)
				tx, err := conn.Begin(ctxFor(t))
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback(context.Background())
				lock, _ := foundation.SystemConfigLock("outbound-policy")
				if _, err = tx.Exec(ctxFor(t), `SELECT pg_advisory_xact_lock($1)`, lock.AdvisoryKey()); err != nil {
					t.Fatal(err)
				}
				budget := 5 * time.Second
				if earlyParent {
					budget = 250 * time.Millisecond
				}
				ctx, cancel := context.WithTimeout(context.Background(), budget)
				defer cancel()
				captured := make(chan context.Context, 1)
				store.hooks(func(ctx context.Context, _ foundation.Tx, _ []foundation.LockRequest) error {
					captured <- ctx
					return nil
				}, nil)
				defer store.hooks(nil, nil)
				start := time.Now()
				done := outboundHTTPStart(t, handler, request(ctx, method, cookie, csrf))
				var serviceContext context.Context
				select {
				case serviceContext = <-captured:
				case <-time.After(time.Second):
					t.Fatal("HTTP request did not enter policy")
				}
				deadline, _ := serviceContext.Deadline()
				parent, _ := ctx.Deadline()
				if deadline.After(parent) || method == "GET" && time.Until(deadline) > 3*time.Second {
					t.Fatal("HTTP renewed its inherited budget")
				}
				outboundHTTPWaitBlocked(t, f.store, conn.PgConn().PID())
				got := outboundHTTPJoin(t, done)
				elapsed := time.Since(start)
				store.mu.Lock()
				result := store.last
				store.mu.Unlock()
				var databaseError *postgres.Error
				if store.active.Load() != 0 || result.State() != foundation.NotCommitted || result.Fault() == nil || !errors.As(result.Fault(), &databaseError) || databaseError.Code() != postgres.LockFailed {
					t.Fatal("lock failure lost its typed result or actual transaction tail")
				}
				if bytes.Contains(got.response.data, []byte(`"rules"`)) || bytes.Contains(got.response.data, []byte(`"audit_id"`)) {
					t.Fatal("failed read/write published a candidate")
				}
				if earlyParent {
					if ctx.Err() != context.DeadlineExceeded || !got.aborted || len(got.response.data) != 0 || elapsed > budget+500*time.Millisecond {
						t.Fatal("earlier parent was extended or returned an expired response")
					}
				} else {
					if ctx.Err() != nil || got.aborted || databaseError.SQLState() != "55P03" || elapsed > 2500*time.Millisecond {
						t.Fatal("DB timeout was confused with HTTP budget expiry")
					}
					got.response.problem(t, 500, "INTERNAL_ERROR")
				}
				if err = tx.Rollback(ctxFor(t)); err != nil {
					t.Fatal(err)
				}
				t.Logf("%s: actual join=%s typed=%s sqlstate=%q; no candidate", name, elapsed, databaseError.Code(), databaseError.SQLState())
			})
		}
	}
	var version, receipts, audits int
	if err = f.store.QueryRow(ctxFor(t), `SELECT version,(SELECT count(*) FROM agenteam_outbound.outbound_policy_receipts),(SELECT count(*) FROM agenteam_audit.audit_records WHERE action='outbound.policy.update') FROM agenteam_outbound.outbound_policy WHERE singleton`).Scan(&version, &receipts, &audits); err != nil || version != 1 || receipts != 0 || audits != 0 {
		t.Fatal("rejected/cancelled commands changed policy facts", version, receipts, audits, err)
	}
	if time.Since(started) > 2*time.Minute {
		t.Fatal("scenario exceeded its two-minute budget")
	}
}

func outboundHTTPWaitBlocked(t *testing.T, store *postgres.Store, pid uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	for {
		var blocked bool
		if err := store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND wait_event='advisory' AND $1=ANY(pg_blocking_pids(pid)))`, int64(pid)).Scan(&blocked); err != nil {
			t.Fatal("owned lock observation failed", err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("exact owned advisory lock did not block the real operation")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
