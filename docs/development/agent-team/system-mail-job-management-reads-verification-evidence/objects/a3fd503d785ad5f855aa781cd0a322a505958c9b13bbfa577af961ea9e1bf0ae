//go:build integration

package account_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	c "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mailManagementReadRecord struct {
	locks                           []foundation.LockRequest
	acquires, authority, statements int
	sql                             string
	state                           foundation.CommitState
}

// Delegates to the real Store. Only a formal enqueue commit is gated; no mail
// row, phase, pointer, fence or attempt is fabricated by the test.
type mailManagementPGStore struct {
	*postgres.Store
	mu               sync.Mutex
	reads            map[foundation.Tx]*mailManagementReadRecord
	blockEnqueue     atomic.Bool
	arrived, release chan struct{}
}

func (s *mailManagementPGStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	var record *mailManagementReadRecord
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if strings.HasPrefix(cause.Details().Owner, "account.http-mail-job-management") {
			record = &mailManagementReadRecord{}
			s.mu.Lock()
			s.reads[tx] = record
			s.mu.Unlock()
		}
		e := fn(ctx, tx)
		if e == nil && cause.Details().Owner == "account.delivery-enqueue" && s.blockEnqueue.CompareAndSwap(true, false) {
			close(s.arrived)
			select {
			case <-s.release:
			case <-ctx.Done():
				return foundation.NewFault(foundation.DependencyUnavailable, foundation.NotCommitted).WithCause(ctx.Err())
			}
		}
		return e
	})
	if record != nil {
		s.mu.Lock()
		record.state = result.State()
		s.mu.Unlock()
	}
	return result
}
func (s *mailManagementPGStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.mu.Lock()
	if r := s.reads[tx]; r != nil {
		r.acquires++
		r.locks = append([]foundation.LockRequest(nil), locks...)
	}
	s.mu.Unlock()
	return s.Store.AcquireAll(ctx, tx, locks)
}
func (s *mailManagementPGStore) RequireHeldLocks(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.mu.Lock()
	if r := s.reads[tx]; r != nil {
		r.authority++
	}
	s.mu.Unlock()
	return s.Store.RequireHeldLocks(ctx, tx, locks)
}
func (s *mailManagementPGStore) InTx(tx foundation.Tx) (postgres.SQLExecutor, error) {
	x, e := s.Store.InTx(tx)
	if e != nil {
		return nil, e
	}
	return mailManagementPGExecutor{x, s, tx}, nil
}

type mailManagementPGExecutor struct {
	postgres.SQLExecutor
	owner *mailManagementPGStore
	tx    foundation.Tx
}

func (x mailManagementPGExecutor) capture(sql string) {
	if !strings.Contains(sql, "LEFT JOIN agenteam_account.mail_jobs by_id") {
		return
	}
	x.owner.mu.Lock()
	defer x.owner.mu.Unlock()
	r := x.owner.reads[x.tx]
	if r == nil || r.authority != 1 || r.acquires != 1 {
		panic("management statement outside current authorized transaction")
	}
	r.statements++
	r.sql = sql
}
func (x mailManagementPGExecutor) Query(ctx context.Context, sql string, args ...any) (*postgres.Rows, error) {
	x.capture(sql)
	return x.SQLExecutor.Query(ctx, sql, args...)
}
func (x mailManagementPGExecutor) QueryRow(ctx context.Context, sql string, args ...any) postgres.Row {
	x.capture(sql)
	return x.SQLExecutor.QueryRow(ctx, sql, args...)
}
func TestAccountMailJobManagementReadTransactionAndBudget(t *testing.T) {
	db, raw, _ := database(t)
	store := &mailManagementPGStore{Store: raw, reads: map[foundation.Tx]*mailManagementReadRecord{}, arrived: make(chan struct{}), release: make(chan struct{})}
	accountKeys, ring, _ := keys(t)
	authority, e := account.NewAuthority(store, accountKeys)
	if e != nil {
		t.Fatal(e)
	}
	f := assembleB02(t, db, raw, store, authority, nil)
	admin := b02Admin(t, f)
	facade, e := account.NewSystemHTTPFacade(f.service, ring)
	if e != nil {
		t.Fatal(e)
	}
	empty, e := facade.ListMailJobManagement(ctxFor(t), admin, account.HTTPListRequest{})
	if e != nil || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatal("empty list", e)
	}
	var jobs []c.JobID
	for _, email := range []string{"query-management-one@example.test", "query-management-two@example.test", "query-management-three@example.test"} {
		q, e := c.NewInvitationCreate(c.InvitationCreateFields{Actor: admin, Key: foundation.IdempotencyKey(id[struct{}](t).String()), Email: email})
		if e != nil {
			t.Fatal(e)
		}
		v, e := f.service.CreateInvitation(ctxFor(t), q)
		if e != nil {
			t.Fatal(e)
		}
		jobs = append(jobs, v.JobID)
	}
	page, e := facade.ListMailJobManagement(ctxFor(t), admin, account.HTTPListRequest{Limit: 1})
	if e != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatal("first page", e)
	}
	first := page.Items[0]
	second, e := facade.ListMailJobManagement(ctxFor(t), admin, account.HTTPListRequest{Limit: 100, Cursor: page.NextCursor})
	if e != nil || len(second.Items) != 2 || second.NextCursor != "" {
		t.Fatal("continuation", e)
	}
	all := append(append([]account.HTTPMailJobManagement{}, page.Items...), second.Items...)
	for i, item := range all {
		if item.Status.JobID != jobs[len(jobs)-1-i] || item.Kind != c.InvitationDelivery || item.Status.Phase != "enqueue_pending" || item.AttemptChannel != nil || item.AttemptResult != nil {
			t.Fatal("formal intent order/shape")
		}
		detail, e := facade.GetMailJobManagement(ctxFor(t), admin, item.Status.JobID)
		if e != nil || detail.HTTPMailJob != item.HTTPMailJob || detail.Kind != item.Kind || detail.AttemptChannel != nil || detail.AttemptResult != nil {
			t.Fatal("detail/list snapshot", e)
		}
		var at time.Time
		if e = raw.QueryRow(ctxFor(t), `SELECT created_at FROM agenteam_account.delivery_intents WHERE job_id=$1`, item.Status.JobID.String()).Scan(&at); e != nil || !at.Equal(item.CreatedAt.Time()) {
			t.Fatal("read changed original intent time", e)
		}
	}
	// The producer has executed its INSERT but cannot yet commit. The read must
	// see a complete old snapshot; after actual producer join it sees pending.
	store.blockEnqueue.Store(true)
	producerCtx, cancelProducer := context.WithCancel(ctxFor(t))
	producerJoined := make(chan struct{})
	var producerError error
	released := false
	go func() { _, producerError = f.service.ReconcileDeliveryIntents(producerCtx); close(producerJoined) }()
	t.Cleanup(func() {
		cancelProducer()
		if !released {
			close(store.release)
		}
		select {
		case <-producerJoined:
		case <-time.After(time.Second):
			t.Error("owned enqueue did not actually join")
		}
	})
	select {
	case <-store.arrived:
	case <-time.After(time.Second):
		t.Fatal("formal enqueue never reached precommit gate")
	}
	before, e := facade.ListMailJobManagement(ctxFor(t), admin, account.HTTPListRequest{Limit: 100})
	if e != nil || len(before.Items) != 3 {
		t.Fatal("read while formal enqueue pending", e)
	}
	for _, item := range before.Items {
		if item.Status.Phase != "enqueue_pending" || item.Status.Attempts != 0 || item.Status.Version != 1 || item.AttemptChannel != nil || item.AttemptResult != nil {
			t.Fatal("uncommitted job contaminated statement snapshot")
		}
	}
	close(store.release)
	released = true
	select {
	case <-producerJoined:
	case <-time.After(3 * time.Second):
		t.Fatal("enqueue failed to join")
	}
	if producerError != nil {
		t.Fatal(producerError)
	}
	after, e := facade.ListMailJobManagement(ctxFor(t), admin, account.HTTPListRequest{Limit: 100})
	if e != nil || len(after.Items) != 3 {
		t.Fatal("committed enqueue missing", e)
	}
	for _, item := range after.Items {
		if item.Status.Phase != "pending" || item.AttemptChannel != nil || item.AttemptResult != nil {
			t.Fatal("committed pending facts")
		}
	}
	// Capture the actual SQL handed to the formal transaction; no copied query.
	var listSQL string
	store.mu.Lock()
	for _, r := range store.reads {
		if r.acquires != 1 || r.authority != 1 || r.statements != 1 || len(r.locks) != 2 || r.state != foundation.Committed {
			t.Error("read did not authorize and query exactly once in one committed transaction")
		}
		for _, lock := range r.locks {
			if lock.Mode != foundation.Shared {
				t.Error("read acquired nonshared lock")
			}
		}
		if strings.Contains(r.sql, "WITH page AS MATERIALIZED") {
			listSQL = r.sql
		}
	}
	store.mu.Unlock()
	if listSQL == "" {
		t.Fatal("actual list statement not captured")
	}
	conn := db.Connect(t)
	var count int
	if e = conn.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_account.delivery_intents`).Scan(&count); e != nil || count != 3 {
		t.Fatal("formal plan cardinality", e)
	}
	indexes, e := conn.Query(ctxFor(t), `SELECT tablename,indexdef FROM pg_indexes WHERE schemaname='agenteam_account' AND tablename IN ('delivery_intents','mail_jobs','mail_attempts') ORDER BY tablename,indexname`)
	if e != nil {
		t.Fatal(e)
	}
	for indexes.Next() {
		var table, definition string
		if e = indexes.Scan(&table, &definition); e != nil {
			indexes.Close()
			t.Fatal(e)
		}
		t.Log("existing index", table, definition)
	}
	indexes.Close()
	if indexes.Err() != nil {
		t.Fatal(indexes.Err())
	}
	for _, plan := range []struct {
		name    string
		at, job any
	}{{"normal", nil, nil}, {"next-page", first.CreatedAt.Time(), first.Status.JobID.String()}, {"empty", time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), jobs[0].String()}} {
		ctx, cancel := context.WithTimeout(ctxFor(t), 3*time.Second)
		var result []byte
		e = conn.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+listSQL, plan.at, plan.job, 26).Scan(&result)
		cancel()
		if e != nil || !json.Valid(result) {
			t.Fatal("actual management EXPLAIN", plan.name, e)
		}
		t.Logf("EXPLAIN %s, formal intents=%d, limit=25+sentinel: %s", plan.name, count, result)
	}
	t.Log("No new index: three formal intents demonstrate query shape only; historical sorting remains bounded by 3s, not an unlimited-scale SLA")
	// Native advisory lock wait, with unchanged DB lock_timeout=1s.
	lock, _ := foundation.SystemConfigLock("account-mail")
	hold := func() pgx.Tx {
		t.Helper()
		tx, e := conn.Begin(ctxFor(t))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctxFor(t), `SELECT pg_advisory_xact_lock($1)`, lock.AdvisoryKey()); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = tx.Rollback(ctx)
		})
		return tx
	}
	waitBlocked := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for {
			var blocked bool
			if e := raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND wait_event='advisory' AND $1=ANY(pg_blocking_pids(pid)))`, int64(conn.PgConn().PID())).Scan(&blocked); e != nil {
				t.Fatal(e)
			}
			if blocked {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("reader never reached actual owned lock wait")
			case <-ticker.C:
			}
		}
	}
	type observed struct {
		list account.HTTPMailJobManagementList
		item account.HTTPMailJobManagement
		err  error
	}
	start := func(parent context.Context, tx pgx.Tx, detail bool) func() observed {
		ctx, cancel := context.WithCancel(parent)
		joined := make(chan struct{})
		var result observed
		go func() {
			if detail {
				result.item, result.err = facade.GetMailJobManagement(ctx, admin, jobs[0])
			} else {
				result.list, result.err = facade.ListMailJobManagement(ctx, admin, account.HTTPListRequest{})
			}
			close(joined)
		}()
		t.Cleanup(func() {
			cancel()
			cleanup, stop := context.WithTimeout(context.Background(), time.Second)
			_ = tx.Rollback(cleanup)
			stop()
			select {
			case <-joined:
			case <-time.After(time.Second):
				t.Error("original management call did not join")
			}
		})
		return func() observed {
			t.Helper()
			select {
			case <-joined:
				return result
			case <-time.After(3500 * time.Millisecond):
				t.Fatal("management read exceeded original bounded join")
				return observed{}
			}
		}
	}
	if db.Config(t, nil).LockTimeout() != time.Second {
		t.Fatal("fixture lock timeout changed")
	}
	for _, detail := range []bool{false, true} {
		for _, budget := range []time.Duration{75 * time.Millisecond, 5 * time.Second} {
			tx := hold()
			ctx, cancel := context.WithTimeout(context.Background(), budget)
			t.Cleanup(cancel)
			started := time.Now()
			join := start(ctx, tx, detail)
			waitBlocked()
			result := join()
			elapsed := time.Since(started)
			var fault *foundation.Fault
			var databaseError *postgres.Error
			if !errors.As(result.err, &fault) || fault.Code != foundation.InternalError || fault.CommitState != foundation.NotCommitted || !errors.As(result.err, &databaseError) || databaseError.Code() != postgres.LockFailed || result.list.Items != nil || result.list.NextCursor != "" || !reflect.DeepEqual(result.item, account.HTTPMailJobManagement{}) {
				t.Fatal("lock failure lost typed error or zero projection", result.err)
			}
			if budget < time.Second {
				if ctx.Err() != context.DeadlineExceeded || elapsed > budget+500*time.Millisecond {
					t.Fatal("earlier parent extended", elapsed)
				}
			} else {
				var pg *pgconn.PgError
				if !errors.As(result.err, &pg) || pg.Code != "55P03" || databaseError.SQLState() != "55P03" || ctx.Err() != nil || elapsed > 3500*time.Millisecond {
					t.Fatal("earlier DB1s timeout lost native cause/live parent", result.err)
				}
			}
			t.Logf("detail=%t callerBudget=%s joined=%s dbCode=%s SQLSTATE=%q callerDeadline=%t errorIsDeadline=%t", detail, budget, elapsed, databaseError.Code(), databaseError.SQLState(), ctx.Err() == context.DeadlineExceeded, errors.Is(result.err, context.DeadlineExceeded))
			cancel()
			if e = tx.Rollback(ctxFor(t)); e != nil {
				t.Fatal(e)
			}
		}
	}
	// Prior external authorization is not a receipt. Change current role while
	// the read really waits, then require the in-Tx check to reject both APIs.
	for _, detail := range []bool{false, true} {
		if _, e = authority.AuthorizeSystem(ctxFor(t), foundation.Tx{}, admin, identity.Read); e != nil {
			t.Fatal(e)
		}
		tx := hold()
		join := start(ctxFor(t), tx, detail)
		waitBlocked()
		if _, e = raw.Exec(ctxFor(t), `UPDATE agenteam_account.users SET role='user' WHERE id=$1`, admin.Details().UserID); e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			_, _ = raw.Exec(context.Background(), `UPDATE agenteam_account.users SET role='admin' WHERE id=$1`, admin.Details().UserID)
		})
		if e = tx.Rollback(ctxFor(t)); e != nil {
			t.Fatal(e)
		}
		result := join()
		if !hasCode(result.err, foundation.Forbidden) || result.list.Items != nil || result.list.NextCursor != "" || !reflect.DeepEqual(result.item, account.HTTPMailJobManagement{}) {
			t.Fatal("stale authorization bypassed in-Tx current role", result.err)
		}
		if _, e = raw.Exec(ctxFor(t), `UPDATE agenteam_account.users SET role='admin' WHERE id=$1`, admin.Details().UserID); e != nil {
			t.Fatal(e)
		}
	}
}
