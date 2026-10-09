//go:build integration

package knowledge_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	knowledgehttp "github.com/LunaDeerTech/agenteam/internal/central/knowledge/http"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Observe or hold original physical transactions only. Every callback, lock,
// SQL row, COMMIT outcome and actual Tx retirement remains the real Store's.
type knowledgeOwnerHTTPReadStore struct {
	knowledge.Store
	before, after func(context.Context, f.Tx) error
	attempting    func(context.Context, f.Tx) error
	result        chan f.CommitResult
	once          atomic.Bool
}

func (s *knowledgeOwnerHTTPReadStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	matched := cause.Kind() == f.RecoveryCause && cause.Details().Owner == "knowledge.read" && s.once.CompareAndSwap(false, true)
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if matched && s.before != nil {
			if e := s.before(ctx, tx); e != nil {
				return e
			}
		}
		if e := fn(ctx, tx); e != nil {
			return e
		}
		if matched && s.after != nil {
			return s.after(ctx, tx)
		}
		return nil
	})
	if matched && s.result != nil {
		s.result <- result
	}
	return result
}
func (s *knowledgeOwnerHTTPReadStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if s.attempting != nil {
		if e := s.attempting(ctx, tx); e != nil {
			return e
		}
	}
	return s.Store.AcquireAll(ctx, tx, locks)
}
func knowledgeOwnerHTTPInstall(t *testing.T, v *knowledgeOwnerHTTPFixture, s *knowledge.Service) {
	t.Helper()
	h, e := knowledgehttp.NewHTTPHandler(s, v.boundary)
	if e != nil {
		t.Fatal(e)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), h)
}
func knowledgeOwnerHTTPTxPID(ctx context.Context, store knowledge.Store, tx f.Tx) (int32, error) {
	q, e := store.InTx(tx)
	if e != nil {
		return 0, e
	}
	var pid int32
	e = q.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid)
	return pid, e
}
func knowledgeOwnerHTTPGate() (<-chan struct{}, func()) {
	wait := make(chan struct{})
	var once sync.Once
	return wait, func() { once.Do(func() { close(wait) }) }
}
func knowledgeOwnerHTTPWait(ctx context.Context, wait <-chan struct{}) error {
	select {
	case <-wait:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func knowledgeOwnerHTTPRun(t *testing.T, v *knowledgeOwnerHTTPFixture, r *http.Request) (<-chan knowledgeOwnerHTTPResponse, <-chan struct{}) {
	t.Helper()
	reply, done := make(chan knowledgeOwnerHTTPResponse, 1), make(chan struct{})
	ctx, cancel := context.WithCancel(r.Context())
	go func() { defer close(done); reply <- v.serve(r.WithContext(ctx)) }()
	t.Cleanup(func() { cancel(); runtimeAwait(t, done) })
	return reply, done
}
func knowledgeOwnerHTTPReply(t *testing.T, reply <-chan knowledgeOwnerHTTPResponse, done <-chan struct{}) knowledgeOwnerHTTPResponse {
	t.Helper()
	select {
	case r := <-reply:
		runtimeAwait(t, done)
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("actual HTTP caller did not return")
	}
	return knowledgeOwnerHTTPResponse{}
}
func knowledgeOwnerHTTPPID(t *testing.T, c <-chan int32) int32 {
	t.Helper()
	select {
	case pid := <-c:
		if pid <= 0 {
			t.Fatal("invalid actual PID")
		}
		return pid
	case <-time.After(3 * time.Second):
		t.Fatal("actual transaction boundary not reached")
	}
	return 0
}
func knowledgeOwnerHTTPLockWait(t *testing.T, raw *postgres.Store, key f.LockKey, holder, waiter int32, holderMode, waiterMode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(knowledgeContext(t), time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	n := uint64(key.AdvisoryKey())
	for {
		var found bool
		e := raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid JOIN pg_stat_activity a ON a.pid=w.pid WHERE a.datname=current_database() AND w.locktype='advisory' AND w.classid::bigint=$1 AND w.objid::bigint=$2 AND w.objsubid=1 AND w.pid=$3 AND w.mode=$5 AND NOT w.granted AND h.pid=$4 AND h.mode=$6 AND h.granted AND $4::int=ANY(pg_blocking_pids(w.pid)))`, int64(n>>32), int64(n&0xffffffff), waiter, holder, waiterMode, holderMode).Scan(&found)
		if e != nil {
			t.Fatal(e)
		}
		if found {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("exact owned lock wait not observed")
		}
	}
}

// This mutates only the explicit upstream test Project under the real User EX
// then Project EX union; it is not a production transfer-owner API or receipt.
func knowledgeOwnerHTTPOwnerWriter(t *testing.T, v *knowledgeOwnerHTTPFixture, hold <-chan struct{}, attempted, held chan<- int32) (<-chan f.CommitResult, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(knowledgeContext(t))
	reply, done := make(chan f.CommitResult, 1), make(chan struct{})
	cause, e := f.NewRecoveryCause("knowledge.http.fixture", knowledgeID(t), "")
	if e != nil {
		t.Fatal(e)
	}
	user, e := f.UserLock(v.ownerBrowser.actor.Details().UserID)
	if e != nil {
		t.Fatal(e)
	}
	project, e := f.ProjectLock(v.project.String())
	if e != nil {
		t.Fatal(e)
	}
	go func() {
		defer close(done)
		reply <- v.raw.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			pid, e := knowledgeOwnerHTTPTxPID(ctx, v.raw, tx)
			if e != nil {
				return e
			}
			if attempted != nil {
				attempted <- pid
			}
			if e = v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Exclusive}, {Key: project, Mode: f.Exclusive}}); e != nil {
				return e
			}
			q, e := v.raw.InTx(tx)
			if e != nil {
				return e
			}
			tag, e := q.Exec(ctx, `UPDATE agenteam_project.projects SET owner_user_id=$2,version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND owner_user_id=$3`, v.project.String(), v.otherBrowser.actor.Details().UserID, v.ownerBrowser.actor.Details().UserID)
			if e != nil {
				return e
			}
			if tag.RowsAffected() != 1 {
				return errors.New("upstream exact owner fixture drift")
			}
			if held != nil {
				held <- pid
			}
			if hold != nil {
				return knowledgeOwnerHTTPWait(ctx, hold)
			}
			return nil
		})
	}()
	t.Cleanup(func() { cancel(); runtimeAwait(t, done) })
	return reply, done
}
func knowledgeOwnerHTTPCommit(t *testing.T, reply <-chan f.CommitResult, done <-chan struct{}) {
	t.Helper()
	select {
	case r := <-reply:
		runtimeAwait(t, done)
		if r.State() != f.Committed {
			t.Fatal("upstream fixture did not commit", r.Fault())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual fixture writer did not return")
	}
}

func TestKnowledgeOwnerReadHTTPTransactions(t *testing.T) {
	knowledgeOwnerHTTPTop(t)
	for _, readFirst := range []bool{true, false} {
		name := "writer_first"
		if readFirst {
			name = "reader_first"
		}
		t.Run(name, func(t *testing.T) {
			v := newKnowledgeOwnerHTTPFixture(t)
			v.create(t, nil, "one")
			v.create(t, nil, "two")
			base := knowledgeOwnerHTTPPath(v.project, "")
			first := v.request(t, v.ownerBrowser, "GET", base+"?limit=1", "", "").want(t, 200)
			cursor := first["next_cursor"].(string)
			user, e := f.UserLock(v.ownerBrowser.actor.Details().UserID)
			if e != nil {
				t.Fatal(e)
			}
			gate, release := knowledgeOwnerHTTPGate()
			defer release()
			seen := make(chan int32, 1)
			readAttempt := make(chan int32, 1)
			hook := &knowledgeOwnerHTTPReadStore{Store: v.raw, result: make(chan f.CommitResult, 1)}
			var once atomic.Bool
			hook.attempting = func(ctx context.Context, tx f.Tx) error {
				if !once.CompareAndSwap(false, true) {
					return nil
				}
				pid, e := knowledgeOwnerHTTPTxPID(ctx, v.raw, tx)
				if e == nil {
					readAttempt <- pid
				}
				return e
			}
			pause := func(ctx context.Context, tx f.Tx) error {
				pid, e := knowledgeOwnerHTTPTxPID(ctx, v.raw, tx)
				if e != nil {
					return e
				}
				seen <- pid
				return knowledgeOwnerHTTPWait(ctx, gate)
			}
			if readFirst {
				hook.after = pause
			} else {
				hook.before = pause
			}
			service := concurrentService(t, v.ownerTreeFixture, hook)
			knowledgeOwnerHTTPInstall(t, v, service)
			replies, readDone := knowledgeOwnerHTTPRun(t, v, knowledgeOwnerHTTPRequest(knowledgeContext(t), v.ownerBrowser, "GET", base+"?cursor="+url.QueryEscape(cursor), "", ""))
			readPID := knowledgeOwnerHTTPPID(t, seen)
			writerAttempt, writerHeld := make(chan int32, 1), make(chan int32, 1)
			if readFirst {
				writes, writeDone := knowledgeOwnerHTTPOwnerWriter(t, v, nil, writerAttempt, nil)
				writePID := knowledgeOwnerHTTPPID(t, writerAttempt)
				knowledgeOwnerHTTPLockWait(t, v.raw, user, readPID, writePID, "ShareLock", "ExclusiveLock")
				release()
				knowledgeOwnerHTTPReply(t, replies, readDone).want(t, 200)
				knowledgeOwnerHTTPCommit(t, writes, writeDone)
			} else {
				writerGate, writerRelease := knowledgeOwnerHTTPGate()
				defer writerRelease()
				writes, writeDone := knowledgeOwnerHTTPOwnerWriter(t, v, writerGate, nil, writerHeld)
				writePID := knowledgeOwnerHTTPPID(t, writerHeld)
				release()
				if attempt := knowledgeOwnerHTTPPID(t, readAttempt); attempt != readPID {
					t.Fatal("read changed physical transaction")
				}
				knowledgeOwnerHTTPLockWait(t, v.raw, user, writePID, readPID, "ExclusiveLock", "ShareLock")
				writerRelease()
				knowledgeOwnerHTTPCommit(t, writes, writeDone)
				knowledgeOwnerHTTPReply(t, replies, readDone).want(t, 404)
			}
			// A cursor cannot preserve permission after either committed ordering.
			v.request(t, v.ownerBrowser, "GET", base+"?cursor="+url.QueryEscape(cursor), "", "").want(t, 404)
			v.request(t, v.otherBrowser, "GET", base+"?cursor="+url.QueryEscape(cursor), "", "").want(t, 400)
			v.request(t, v.otherBrowser, "GET", base, "", "").want(t, 200)
		})
	}
	t.Run("actual_query_cancel_and_original_tx_join", func(t *testing.T) {
		v := newKnowledgeOwnerHTTPFixture(t)
		v.create(t, nil, "cancel target")
		before := v.facts(t)
		gate, release := knowledgeOwnerHTTPGate()
		defer release()
		held := make(chan int32, 1)
		writerDone := make(chan struct{})
		writerResult := make(chan f.CommitResult, 1)
		ctx, stop := context.WithCancel(knowledgeContext(t))
		defer stop()
		cause, e := f.NewRecoveryCause("knowledge.http.fixture", knowledgeID(t), "")
		if e != nil {
			t.Fatal(e)
		}
		go func() {
			defer close(writerDone)
			writerResult <- v.raw.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				q, e := v.raw.InTx(tx)
				if e != nil {
					return e
				}
				if _, e = q.Exec(ctx, `LOCK TABLE agenteam_knowledge.documents IN ACCESS EXCLUSIVE MODE`); e != nil {
					return e
				}
				pid, e := knowledgeOwnerHTTPTxPID(ctx, v.raw, tx)
				if e != nil {
					return e
				}
				held <- pid
				return knowledgeOwnerHTTPWait(ctx, gate)
			})
		}()
		t.Cleanup(func() { stop(); release(); runtimeAwait(t, writerDone) })
		writerPID := knowledgeOwnerHTTPPID(t, held)
		attempted := make(chan int32, 1)
		hook := &knowledgeOwnerHTTPReadStore{Store: v.raw, result: make(chan f.CommitResult, 1)}
		var token f.Tx
		hook.before = func(ctx context.Context, tx f.Tx) error {
			token = tx
			pid, e := knowledgeOwnerHTTPTxPID(ctx, v.raw, tx)
			if e == nil {
				attempted <- pid
			}
			return e
		}
		service := concurrentService(t, v.ownerTreeFixture, hook)
		knowledgeOwnerHTTPInstall(t, v, service)
		requestCtx, cancel := context.WithCancel(knowledgeContext(t))
		defer cancel()
		replies, readDone := knowledgeOwnerHTTPRun(t, v, knowledgeOwnerHTTPRequest(requestCtx, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(v.project, ""), "", ""))
		readPID := knowledgeOwnerHTTPPID(t, attempted)
		deadline, done := context.WithTimeout(knowledgeContext(t), time.Second)
		defer done()
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			var blocked bool
			e := v.raw.QueryRow(deadline, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=current_database() AND wait_event_type='Lock' AND wait_event='relation' AND $2::int=ANY(pg_blocking_pids(pid)))`, readPID, writerPID).Scan(&blocked)
			if e != nil {
				t.Fatal(e)
			}
			if blocked {
				break
			}
			select {
			case <-tick.C:
			case <-deadline.Done():
				t.Fatal("original document SELECT did not reach actual relation wait")
			}
		}
		cancel()
		response := knowledgeOwnerHTTPReply(t, replies, readDone)
		if !response.aborted || len(response.body) != 0 {
			t.Fatal("canceled original SQL published a response")
		}
		select {
		case original := <-hook.result:
			if original.State() != f.NotCommitted {
				t.Fatal("canceled query did not return original rollback")
			}
		default:
			t.Fatal("HTTP returned before original transaction result")
		}
		if _, e = v.raw.InTx(token); e == nil {
			t.Fatal("original ended transaction remains live")
		}
		release()
		knowledgeOwnerHTTPCommit(t, writerResult, writerDone)
		v.request(t, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(v.project, ""), "", "").want(t, 200)
		if got := v.facts(t); got != before {
			t.Fatal("cancellation/read modified durable facts")
		}
	})
}

func TestKnowledgeOwnerReadHTTPCommitUnknown(t *testing.T) {
	knowledgeOwnerHTTPTop(t)
	for _, commit := range []bool{false, true} {
		name := "not_forwarded"
		if commit {
			name = "committed_ack_lost"
		}
		t.Run(name, func(t *testing.T) {
			x, observer, proxy := unknownPublicationFixture(t)
			v := assembleKnowledgeOwnerHTTPFixture(t, x)
			v.create(t, nil, "unknown read target")
			before := v.facts(t)
			hook := &knowledgeOwnerHTTPReadStore{Store: x.raw, result: make(chan f.CommitResult, 1)}
			hook.after = func(ctx context.Context, tx f.Tx) error {
				pid, e := knowledgeOwnerHTTPTxPID(ctx, x.raw, tx)
				if e != nil {
					return e
				}
				return proxy.Arm(pid)
			}
			service := concurrentService(t, x, hook)
			knowledgeOwnerHTTPInstall(t, v, service)
			t.Cleanup(proxy.Release)
			response := v.request(t, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(v.project, ""), "", "")
			body := response.want(t, 503)
			if body["code"] != string(f.CommitUnknown) || body["commit_state"] != string(f.Unknown) || body["items"] != nil || body["cause_id"] != nil {
				t.Fatal("read Unknown published candidates or changed safe outcome")
			}
			runtimeAwait(t, proxy.Reached())
			var original f.CommitResult
			select {
			case original = <-hook.result:
			default:
				t.Fatal("original Store outcome missing")
			}
			if original.State() != f.Unknown || original.AttemptID().Validate() != nil || original.Cause().Kind() != f.RecoveryCause || original.Cause().Details().Owner != "knowledge.read" || proxy.WriterPID() <= 0 {
				t.Fatal("missing original physical read Unknown")
			}
			if commit {
				proxy.Release()
				runtimeAwait(t, proxy.Committed())
				runtimeAwait(t, proxy.HeldJoined())
			} else {
				unknownRollbackWriter(t, observer, proxy)
			}
			// This is a fresh authorized read, never confirmation of the lost read.
			v.request(t, v.ownerBrowser, "GET", knowledgeOwnerHTTPPath(v.project, ""), "", "").want(t, 200)
			if got := v.facts(t); got != before {
				t.Fatal("Unknown read changed durable facts")
			}
		})
	}
}
