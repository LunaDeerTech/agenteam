//go:build integration

package skill_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	skillhttp "github.com/LunaDeerTech/agenteam/internal/central/skill/http"
)

// Observe or hold original physical transactions only. Every callback, lock,
// SQL row, COMMIT outcome and actual Tx retirement remains the real Store's.
type skillOwnerHTTPReadStore struct {
	skill.Store
	before, after func(context.Context, f.Tx) error
	attempting    func(context.Context, f.Tx) error
	result        chan f.CommitResult
	once          atomic.Bool
}

func (s *skillOwnerHTTPReadStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	matched := cause.Kind() == f.JobCause && cause.Details().JobType == "skill-read" && s.once.CompareAndSwap(false, true)
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
func (s *skillOwnerHTTPReadStore) AcquireAll(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if s.attempting != nil {
		if e := s.attempting(ctx, tx); e != nil {
			return e
		}
	}
	return s.Store.AcquireAll(ctx, tx, locks)
}
func skillOwnerHTTPInstall(t *testing.T, v *skillOwnerHTTPFixture, s *skill.Service) {
	t.Helper()
	h, e := skillhttp.NewHTTPHandler(s, v.boundary)
	if e != nil {
		t.Fatal(e)
	}
	v.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(v.logs, nil)), h)
}
func skillOwnerHTTPTxPID(ctx context.Context, store skill.Store, tx f.Tx) (int32, error) {
	q, e := store.InTx(tx)
	if e != nil {
		return 0, e
	}
	var pid int32
	e = q.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid)
	return pid, e
}
func skillOwnerHTTPGate() (<-chan struct{}, func()) {
	wait := make(chan struct{})
	var once sync.Once
	return wait, func() { once.Do(func() { close(wait) }) }
}
func skillOwnerHTTPWait(ctx context.Context, wait <-chan struct{}) error {
	select {
	case <-wait:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func skillOwnerHTTPRun(t *testing.T, v *skillOwnerHTTPFixture, r *http.Request) (<-chan skillOwnerHTTPResponse, <-chan struct{}) {
	t.Helper()
	reply, done := make(chan skillOwnerHTTPResponse, 1), make(chan struct{})
	ctx, cancel := context.WithCancel(r.Context())
	go func() { defer close(done); reply <- v.serve(r.WithContext(ctx)) }()
	t.Cleanup(func() { cancel(); skillOwnerHTTPAwait(t, done) })
	return reply, done
}
func skillOwnerHTTPReply(t *testing.T, reply <-chan skillOwnerHTTPResponse, done <-chan struct{}) skillOwnerHTTPResponse {
	t.Helper()
	select {
	case r := <-reply:
		skillOwnerHTTPAwait(t, done)
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("actual HTTP caller did not return")
	}
	return skillOwnerHTTPResponse{}
}
func skillOwnerHTTPPID(t *testing.T, c <-chan int32) int32 {
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
func skillOwnerHTTPLockWait(t *testing.T, raw *postgres.Store, key f.LockKey, holder, waiter int32, holderMode, waiterMode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(testContext(t), time.Second)
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
func skillOwnerHTTPOwnerWriter(t *testing.T, v *skillOwnerHTTPFixture, hold <-chan struct{}, attempted, held chan<- int32) (<-chan f.CommitResult, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(testContext(t))
	reply, done := make(chan f.CommitResult, 1), make(chan struct{})
	cause, e := f.NewRecoveryCause("skills.http.fixture", testID[struct{}](t).String(), "")
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
		reply <- v.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			pid, e := skillOwnerHTTPTxPID(ctx, v.store, tx)
			if e != nil {
				return e
			}
			if attempted != nil {
				attempted <- pid
			}
			if e = v.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Exclusive}, {Key: project, Mode: f.Exclusive}}); e != nil {
				return e
			}
			q, e := v.store.InTx(tx)
			if e != nil {
				return e
			}
			tag, e := q.Exec(ctx, `UPDATE agenteam_project.projects SET owner_user_id=$2,version=version+1,updated_at=statement_timestamp() WHERE id=$1 AND owner_user_id=$3`, v.project.String(), v.otherBrowser.actor.Details().UserID, v.ownerBrowser.actor.Details().UserID)
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
				return skillOwnerHTTPWait(ctx, hold)
			}
			return nil
		})
	}()
	t.Cleanup(func() { cancel(); skillOwnerHTTPAwait(t, done) })
	return reply, done
}
func skillOwnerHTTPCommit(t *testing.T, reply <-chan f.CommitResult, done <-chan struct{}) {
	t.Helper()
	select {
	case r := <-reply:
		skillOwnerHTTPAwait(t, done)
		if r.State() != f.Committed {
			t.Fatal("upstream fixture did not commit", r.Fault())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("actual fixture writer did not return")
	}
}

func TestSkillOwnerReadHTTPTransactions(t *testing.T) {
	skillOwnerHTTPTop(t)
	for _, readFirst := range []bool{true, false} {
		name := "writer_first"
		if readFirst {
			name = "reader_first"
		}
		t.Run(name, func(t *testing.T) {
			v := newSkillOwnerHTTPFixture(t)
			base := skillOwnerHTTPPath(v.project, "")
			user, e := f.UserLock(v.ownerBrowser.actor.Details().UserID)
			if e != nil {
				t.Fatal(e)
			}
			gate, release := skillOwnerHTTPGate()
			defer release()
			seen := make(chan int32, 1)
			readAttempt := make(chan int32, 1)
			hook := &skillOwnerHTTPReadStore{Store: v.store, result: make(chan f.CommitResult, 1)}
			var once atomic.Bool
			hook.attempting = func(ctx context.Context, tx f.Tx) error {
				if !once.CompareAndSwap(false, true) {
					return nil
				}
				pid, e := skillOwnerHTTPTxPID(ctx, v.store, tx)
				if e == nil {
					readAttempt <- pid
				}
				return e
			}
			pause := func(ctx context.Context, tx f.Tx) error {
				pid, e := skillOwnerHTTPTxPID(ctx, v.store, tx)
				if e != nil {
					return e
				}
				seen <- pid
				return skillOwnerHTTPWait(ctx, gate)
			}
			if readFirst {
				hook.after = pause
			} else {
				hook.before = pause
			}
			service := skillOwnerHTTPService(t, v, hook)
			skillOwnerHTTPInstall(t, v, service)
			replies, readDone := skillOwnerHTTPRun(t, v, skillOwnerHTTPRequest(testContext(t), v.ownerBrowser, "GET", base, "", ""))
			readPID := skillOwnerHTTPPID(t, seen)
			writerAttempt, writerHeld := make(chan int32, 1), make(chan int32, 1)
			if readFirst {
				writes, writeDone := skillOwnerHTTPOwnerWriter(t, v, nil, writerAttempt, nil)
				writePID := skillOwnerHTTPPID(t, writerAttempt)
				skillOwnerHTTPLockWait(t, v.store, user, readPID, writePID, "ShareLock", "ExclusiveLock")
				release()
				skillOwnerHTTPReply(t, replies, readDone).want(t, 200)
				skillOwnerHTTPCommit(t, writes, writeDone)
			} else {
				writerGate, writerRelease := skillOwnerHTTPGate()
				defer writerRelease()
				writes, writeDone := skillOwnerHTTPOwnerWriter(t, v, writerGate, nil, writerHeld)
				writePID := skillOwnerHTTPPID(t, writerHeld)
				release()
				if attempt := skillOwnerHTTPPID(t, readAttempt); attempt != readPID {
					t.Fatal("read changed physical transaction")
				}
				skillOwnerHTTPLockWait(t, v.store, user, writePID, readPID, "ExclusiveLock", "ShareLock")
				writerRelease()
				skillOwnerHTTPCommit(t, writes, writeDone)
				skillOwnerHTTPReply(t, replies, readDone).want(t, 404)
			}
			// A completed earlier read never preserves permission after the writer.
			before := v.facts(t)
			v.request(t, v.ownerBrowser, "GET", base, "", "").want(t, 404)
			v.request(t, v.otherBrowser, "GET", base, "", "").want(t, 200)
			if v.facts(t) != before {
				t.Fatal("current authority reads changed business facts")
			}
		})
	}
	t.Run("actual_query_cancel_and_original_tx_join", func(t *testing.T) {
		v := newSkillOwnerHTTPFixture(t)
		before := v.facts(t)
		gate, release := skillOwnerHTTPGate()
		defer release()
		held := make(chan int32, 1)
		writerDone := make(chan struct{})
		writerResult := make(chan f.CommitResult, 1)
		ctx, stop := context.WithCancel(testContext(t))
		defer stop()
		cause, e := f.NewRecoveryCause("skills.http.fixture", testID[struct{}](t).String(), "")
		if e != nil {
			t.Fatal(e)
		}
		go func() {
			defer close(writerDone)
			writerResult <- v.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
				q, e := v.store.InTx(tx)
				if e != nil {
					return e
				}
				if _, e = q.Exec(ctx, `LOCK TABLE agenteam_skill.skills IN ACCESS EXCLUSIVE MODE`); e != nil {
					return e
				}
				pid, e := skillOwnerHTTPTxPID(ctx, v.store, tx)
				if e != nil {
					return e
				}
				held <- pid
				return skillOwnerHTTPWait(ctx, gate)
			})
		}()
		t.Cleanup(func() { stop(); release(); skillOwnerHTTPAwait(t, writerDone) })
		writerPID := skillOwnerHTTPPID(t, held)
		attempted := make(chan int32, 1)
		hook := &skillOwnerHTTPReadStore{Store: v.store, result: make(chan f.CommitResult, 1)}
		var token f.Tx
		hook.before = func(ctx context.Context, tx f.Tx) error {
			token = tx
			pid, e := skillOwnerHTTPTxPID(ctx, v.store, tx)
			if e == nil {
				attempted <- pid
			}
			return e
		}
		service := skillOwnerHTTPService(t, v, hook)
		skillOwnerHTTPInstall(t, v, service)
		requestCtx, cancel := context.WithCancel(testContext(t))
		defer cancel()
		replies, readDone := skillOwnerHTTPRun(t, v, skillOwnerHTTPRequest(requestCtx, v.ownerBrowser, "GET", skillOwnerHTTPPath(v.project, ""), "", ""))
		readPID := skillOwnerHTTPPID(t, attempted)
		deadline, done := context.WithTimeout(testContext(t), time.Second)
		defer done()
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			var blocked bool
			e := v.store.QueryRow(deadline, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a JOIN pg_locks w ON w.pid=a.pid JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.relation=w.relation WHERE a.pid=$1 AND a.datname=current_database() AND a.wait_event_type='Lock' AND a.wait_event='relation' AND w.locktype='relation' AND w.relation='agenteam_skill.skills'::regclass AND w.mode='AccessShareLock' AND NOT w.granted AND h.pid=$2 AND h.mode='AccessExclusiveLock' AND h.granted AND $2::int=ANY(pg_blocking_pids(a.pid)))`, readPID, writerPID).Scan(&blocked)
			if e != nil {
				t.Fatal(e)
			}
			if blocked {
				break
			}
			select {
			case <-tick.C:
			case <-deadline.Done():
				t.Fatal("original Skill SELECT did not reach actual relation wait")
			}
		}
		cancel()
		response := skillOwnerHTTPReply(t, replies, readDone)
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
		if _, e = v.store.InTx(token); e == nil {
			t.Fatal("original ended transaction remains live")
		}
		release()
		skillOwnerHTTPCommit(t, writerResult, writerDone)
		v.request(t, v.ownerBrowser, "GET", skillOwnerHTTPPath(v.project, ""), "", "").want(t, 200)
		if got := v.facts(t); got != before {
			t.Fatal("cancellation/read modified durable facts")
		}
	})
}

func TestSkillOwnerReadHTTPCommitUnknown(t *testing.T) {
	skillOwnerHTTPTop(t)
	for _, commit := range []bool{false, true} {
		name := "not_forwarded"
		if commit {
			name = "committed_ack_lost"
		}
		t.Run(name, func(t *testing.T) {
			direct := newSkillPG(t)
			p, proxy := withCommitProxy(t, direct)
			v := assembleSkillOwnerHTTPFixture(t, p)
			before := v.facts(t)
			hook := &skillOwnerHTTPReadStore{Store: p.store, result: make(chan f.CommitResult, 1)}
			hook.after = func(ctx context.Context, tx f.Tx) error {
				pid, e := skillOwnerHTTPTxPID(ctx, p.store, tx)
				if e != nil {
					return e
				}
				return proxy.Arm(pid)
			}
			service := skillOwnerHTTPService(t, v, hook)
			skillOwnerHTTPInstall(t, v, service)
			t.Cleanup(proxy.Release)
			response := v.request(t, v.ownerBrowser, "GET", skillOwnerHTTPPath(v.project, ""), "", "")
			body := response.want(t, 503)
			if body["code"] != string(f.CommitUnknown) || body["commit_state"] != string(f.Unknown) || body["items"] != nil || body["cause_id"] != nil {
				t.Fatal("read Unknown published candidates or changed safe outcome")
			}
			skillOwnerHTTPAwait(t, proxy.Reached())
			var original f.CommitResult
			select {
			case original = <-hook.result:
			default:
				t.Fatal("original Store outcome missing")
			}
			if original.State() != f.Unknown || original.AttemptID().Validate() != nil || original.Cause().Kind() != f.JobCause || original.Cause().Details().JobType != "skill-read" || original.Cause().Details().JobID != v.project.String() || proxy.WriterPID() <= 0 {
				t.Fatal("missing original physical read Unknown")
			}
			if commit {
				proxy.Release()
				skillOwnerHTTPAwait(t, proxy.Committed())
				skillOwnerHTTPAwait(t, proxy.HeldJoined())
			} else {
				skillOwnerHTTPRollbackWriter(t, direct.store, proxy)
			}
			// This is a fresh authorized read, never confirmation of the lost read.
			v.request(t, v.ownerBrowser, "GET", skillOwnerHTTPPath(v.project, ""), "", "").want(t, 200)
			if got := v.facts(t); got != before {
				t.Fatal("Unknown read changed durable facts")
			}
		})
	}
}

// These helpers preserve the original Service/Store/Tx; only the read callback
// is observed. Object is never consumed and the original owner is drained.
func skillOwnerHTTPService(t *testing.T, v *skillOwnerHTTPFixture, store skill.Store) *skill.Service {
	t.Helper()
	authority, err := skill.NewAuthority(store, v.projects)
	if err != nil {
		t.Fatal(err)
	}
	return v.skillPG.newService(t, authority, v.seed.objects)
}
func skillOwnerHTTPAwait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("actual owned caller did not join")
	}
}

func skillOwnerHTTPRollbackWriter(t *testing.T, observer *postgres.Store, proxy *commitproxy.Proxy) {
	t.Helper()
	pid := proxy.WriterPID()
	if pid <= 0 {
		t.Fatal("COMMIT was not bound to an exact writer")
	}
	// The original complete frame is still held. Terminate only the verified
	// backend in this owned database, observe absence, then release and join.
	var stopped bool
	err := observer.QueryRow(testContext(t), `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid=$1 AND datname=current_database() AND application_name='agenteam'`, pid).Scan(&stopped)
	if err != nil || !stopped {
		t.Fatal("owned held writer termination not confirmed", err)
	}
	ctx, cancel := context.WithTimeout(testContext(t), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var present bool
		if err = observer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND datname=current_database())`, pid).Scan(&present); err != nil {
			t.Fatal("owned writer retirement observation failed", err)
		}
		if !present {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("owned held writer did not actually exit")
		case <-ticker.C:
		}
	}
	proxy.Release()
	skillOwnerHTTPAwait(t, proxy.HeldJoined())
	select {
	case <-proxy.Committed():
		t.Fatal("not-forwarded COMMIT unexpectedly completed")
	default:
	}
}
