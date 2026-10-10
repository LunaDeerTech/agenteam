//go:build integration

package runnercontrol_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5"
)

// Independent management tests use the accepted real Account/Audit bootstrap,
// but their timing and durable oracles are independent of the service methods.
func independentAwait(t *testing.T, done <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("independent callback did not join: %s", stage)
	}
}

type independentCall[T any] struct {
	result <-chan T
	joined <-chan struct{}
}

func independentStart[T any](t *testing.T, fn func(context.Context) T) independentCall[T] {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	result, joined := make(chan T, 1), make(chan struct{})
	go func() {
		defer close(joined)
		result <- fn(ctx)
	}()
	t.Cleanup(func() { cancel(); independentAwait(t, joined, "original service call") })
	return independentCall[T]{result, joined}
}

func independentResult[T any](t *testing.T, call independentCall[T]) T {
	t.Helper()
	independentAwait(t, call.joined, "service result")
	return <-call.result
}

type independentMutation struct {
	value rc.Mutation
	err   error
}
type independentLookup struct {
	value rc.Lookup
	err   error
}

type independentFacts struct {
	runners, commands, tokens, liveTokens, events, audits int
}

func independentCount(t *testing.T, v *runnerServiceFixture, target rc.RunnerID) independentFacts {
	t.Helper()
	var out independentFacts
	e := v.store.QueryRow(migrationContext(t), `SELECT
 (SELECT count(*) FROM agenteam_runner.runners WHERE id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.commands WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1::uuid AND revoked_at IS NULL),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1::uuid),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND runner_id=$1::uuid)`, target.String()).Scan(&out.runners, &out.commands, &out.tokens, &out.liveTokens, &out.events, &out.audits)
	requireServiceOK(t, e, "independent persistent facts")
	return out
}

func independentCommand(t *testing.T, v *runnerServiceFixture, key f.IdempotencyKey, in rc.Intent) f.LockKey {
	t.Helper()
	id, e := in.Identity(v.actor, key)
	requireServiceOK(t, e, "independent command identity")
	lock, e := f.CommandLock(id)
	requireServiceOK(t, e, "independent command lock")
	return lock
}

type independentBarrier struct {
	conn *pgx.Conn
	key  int64
	pid  int32
	held bool
}

func independentHold(t *testing.T, v *runnerServiceFixture, key f.LockKey) *independentBarrier {
	t.Helper()
	b := &independentBarrier{conn: v.db.Connect(t), key: key.AdvisoryKey()}
	e := b.conn.QueryRow(migrationContext(t), `SELECT pg_backend_pid()`).Scan(&b.pid)
	requireServiceOK(t, e, "independent holder identity")
	_, e = b.conn.Exec(migrationContext(t), `SELECT pg_advisory_lock($1)`, b.key)
	requireServiceOK(t, e, "hold exact independent barrier")
	b.held = true
	t.Cleanup(func() { b.release(t) })
	return b
}

func (b *independentBarrier) release(t *testing.T) {
	t.Helper()
	if !b.held {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var released bool
	e := b.conn.QueryRow(ctx, `SELECT pg_advisory_unlock($1)`, b.key).Scan(&released)
	if e != nil || !released {
		t.Error("exact independent barrier did not release")
		return
	}
	b.held = false
}

// Actual pg_locks + blocking PID prove the call reached the designated wait;
// polling is bounded observation, not a delay used to assume a race occurred.
func independentWaiters(t *testing.T, conn *pgx.Conn, key int64, blocker int32, mode string, want int) []int32 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	n := uint64(key)
	tick := time.NewTicker(2 * time.Millisecond)
	defer tick.Stop()
	for {
		var pids []int32
		e := conn.QueryRow(ctx, `SELECT COALESCE(array_agg(l.pid ORDER BY l.pid),ARRAY[]::integer[])
 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid
 WHERE a.datname=current_database() AND a.xact_start IS NOT NULL
 AND l.locktype='advisory' AND l.classid::bigint=$1 AND l.objid::bigint=$2
 AND l.objsubid=1 AND NOT l.granted AND l.mode=$3
 AND $4::integer=ANY(pg_blocking_pids(l.pid))`, int64(n>>32), int64(uint32(n)), mode, blocker).Scan(&pids)
		requireServiceOK(t, e, "independent exact lock observation")
		if len(pids) > want {
			t.Fatal("ambiguous independent lock waiters")
		}
		if len(pids) == want {
			for _, pid := range pids {
				if pid <= 0 || pid == blocker {
					t.Fatal("invalid independent blocking relationship")
				}
			}
			return pids
		}
		select {
		case <-ctx.Done():
			t.Fatal("exact independent lock wait not observed")
		case <-tick.C:
		}
	}
}

// The AFTER trigger holds an already-authorized real business transaction. It
// neither supplies facts nor changes its result. Inputs here are generated IDs
// and closed command names, never token/body material.
func independentAuditBarrier(t *testing.T, v *runnerServiceFixture, target rc.RunnerID, logout bool) (*independentBarrier, *pgx.Conn) {
	t.Helper()
	lock, e := f.RecordLock(f.ReferenceRecordLock, "runner-independent-audit:"+serviceID[struct{}](t).String())
	requireServiceOK(t, e, "independent Audit barrier identity")
	b := independentHold(t, v, lock)
	admin := v.db.Connect(t)
	condition := fmt.Sprintf("NEW.producer='runner' AND NEW.runner_id='%s'::uuid", target.String())
	if logout {
		condition = fmt.Sprintf("NEW.producer='account' AND NEW.action='account.logout' AND NEW.user_id='%s'::uuid AND NEW.session_id='%s'::uuid", v.actor.Details().UserID, v.actor.Details().SessionID)
	}
	query := fmt.Sprintf(`CREATE SCHEMA independent_runner_gate;
 CREATE FUNCTION independent_runner_gate.hold_audit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF %s THEN PERFORM pg_advisory_xact_lock(%d::bigint); END IF; RETURN NEW; END $$;
 CREATE TRIGGER independent_runner_audit AFTER INSERT ON agenteam_audit.audit_records
 FOR EACH ROW EXECUTE FUNCTION independent_runner_gate.hold_audit()`, condition, b.key)
	_, e = admin.Exec(migrationContext(t), query)
	requireServiceOK(t, e, "install independent exact Audit boundary")
	t.Cleanup(func() {
		b.release(t)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, e := admin.Exec(ctx, `DROP SCHEMA independent_runner_gate CASCADE`); e != nil {
			t.Error("owned independent trigger did not retire")
		}
	})
	return b, admin
}
