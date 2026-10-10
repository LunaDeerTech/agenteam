//go:build integration

package skill_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

// This observes an independent real backend waiting on the original writer's
// exact Project advisory key. Cancellation is followed by the original actual
// transaction return; a timeout alone is never taken as evidence of contention.
func cleanupHeldWriter(t *testing.T, direct *skillPG, project string, writer int32) {
	t.Helper()
	key, err := f.ProjectLock(project)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	pid := make(chan int32, 1)
	returned := make(chan f.CommitResult, 1)
	done := make(chan struct{})
	cause := testCause(t)
	t.Cleanup(func() { cancel(); waitSignal(t, done, "independent lock waiter not joined") })
	go func() {
		defer close(done)
		returned <- direct.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			q, err := direct.store.InTx(tx)
			if err != nil {
				return err
			}
			var backend int32
			if err = q.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backend); err != nil {
				return err
			}
			pid <- backend
			return direct.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}})
		})
	}()
	var waiter int32
	select {
	case waiter = <-pid:
	case <-done:
		t.Fatal("lock waiter ended before its actual backend was recorded")
	case <-ctx.Done():
		t.Fatal("lock waiter backend was not reached")
	}
	bits := uint64(key.AdvisoryKey())
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var held bool
		err = direct.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid WHERE w.locktype='advisory' AND w.pid=$1 AND h.pid=$2 AND NOT w.granted AND h.granted AND w.mode='ExclusiveLock' AND h.mode='ExclusiveLock' AND w.classid::bigint=$3 AND w.objid::bigint=$4 AND w.objsubid=1 AND h.pid=ANY(pg_blocking_pids(w.pid)))`, waiter, writer, int64(uint32(bits>>32)), int64(uint32(bits))).Scan(&held)
		if err != nil {
			t.Fatal("original exact lock evidence", err)
		}
		if held {
			break
		}
		select {
		case <-done:
			t.Fatal("waiter escaped original held Project lock")
		case <-ctx.Done():
			t.Fatal("original writer was not the exact lock blocker")
		case <-ticker.C:
		}
	}
	cancel()
	waitSignal(t, done, "cancelled original lock waiter not joined")
	if result := <-returned; result.State() != f.NotCommitted {
		t.Fatal("cancelled waiter committed or became an unowned Unknown", result.State())
	}
}

func TestSkillLifecycleCleanupCommitRecovery(t *testing.T) {
	for _, boundary := range []string{"gate", "last_two_domain_anchors"} {
		t.Run(boundary, func(t *testing.T) {
			direct := newSkillPG(t)
			p, proxy := withCommitProxy(t, direct)
			x := newSkillCleanupFixture(t, p)
			// Runs before the Object/Skill/Store drain cleanups, including when
			// an assertion fails while the selected original COMMIT is held.
			t.Cleanup(func() {
				proxy.Release()
				select {
				case <-proxy.Reached():
					waitSignal(t, proxy.HeldJoined(), "failed-case original proxy writer not joined")
				default:
				}
			})
			v := x.seed
			initialized, err := x.service.InitializeProjectSkills(testContext(t), v.actor, v.request)
			if err != nil || initialized.State != pc.InitializationCompleted || initialized.AddSkillsID == nil {
				t.Fatal("real publication before COMMIT stimulus", err)
			}
			seedReaderReadyProject(t, p, v, *initialized.AddSkillsID)
			actor, cause, scope := seedCleanupStop(t, p, v, x.manifest, pc.Delete)
			x.enterCleaning(t, actor, cause, scope)
			var object string
			if err = p.store.QueryRow(testContext(t), `SELECT object_id::text FROM agenteam_skill.initializations WHERE project_id=$1`, scope.ProjectID.String()).Scan(&object); err != nil {
				t.Fatal(err)
			}
			var checkpoint *pc.CleanupCheckpoint
			if boundary == "last_two_domain_anchors" {
				for round := 0; round < 16; round++ {
					c := x.counts(t, object)
					if c.local == 5 && c.work == 0 && c.native == 4 && c.deletedAudit == 1 {
						break
					}
					report, err := x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
					if err != nil || report.Details().State != pc.CleanupPending {
						t.Fatal("real pre-final progress", err)
					}
					checkpoint = report.Details().Checkpoint
				}
				if c := x.counts(t, object); c.local != 5 || c.work != 0 || c.native != 4 || c.deletedAudit != 1 {
					t.Fatal("final COMMIT stimulus did not reach actual anchors", c)
				}
			}
			before := x.counts(t, object)
			var writer, selected atomic.Int32
			x.store.setAfter(func(ctx context.Context, tx f.Tx) error {
				q, err := p.store.InTx(tx)
				if err != nil {
					return err
				}
				c, err := readCleanupCounts(ctx, q, scope.ProjectID.String(), object)
				if err != nil {
					return err
				}
				reached := c.local == 0 && c.native == 0 && c.deletedAudit == 1
				if boundary == "gate" {
					if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.cleanup c JOIN agenteam_skill.skills s ON s.id=c.skill_id JOIN agenteam_object.objects o ON o.id=c.object_id JOIN agenteam_object.uploads u ON u.id=c.upload_id WHERE c.project_id=$1 AND c.phase='gated' AND NOT s.serving AND o.cleaning AND o.state='available' AND u.disposition='revoked')`, scope.ProjectID.String()).Scan(&reached); err != nil {
						return err
					}
					reached = reached && c.references == 0 && c.deletedAudit == 0
				}
				if !reached {
					return nil
				}
				if !selected.CompareAndSwap(0, 1) {
					return f.NewFault(f.InvalidState, f.NotStarted)
				}
				var backend int32
				if err = q.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&backend); err != nil {
					return err
				}
				writer.Store(backend)
				return proxy.Arm(backend)
			})
			first, firstErr := x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
			x.store.setAfter(nil)
			waitSignal(t, proxy.Reached(), "selected original complete COMMIT frame not held")
			unknown, ok := skill.UnknownAttempt(firstErr)
			if !ok || unknown.State() != f.Unknown || unknown.AttemptID().Validate() != nil || first.Details().State != "" || selected.Load() != 1 || writer.Load() <= 0 || proxy.WriterPID() != writer.Load() {
				t.Fatal("original native Unknown/caller result lost", firstErr)
			}
			select {
			case <-proxy.Committed():
				t.Fatal("held original COMMIT was forwarded early")
			default:
			}
			visible, err := readCleanupCounts(testContext(t), direct.store, scope.ProjectID.String(), object)
			if err != nil || visible != before {
				t.Fatal("Unknown was treated as committed or physical work escaped its gate", err, visible, before)
			}
			cleanupHeldWriter(t, direct, scope.ProjectID.String(), writer.Load())
			proxy.Release()
			waitSignal(t, proxy.Committed(), "original upstream COMMIT not confirmed")
			waitSignal(t, proxy.HeldJoined(), "original proxy writer tail not joined")
			var originalCleanup string
			if boundary == "gate" {
				var phase string
				if err = p.store.QueryRow(testContext(t), `SELECT id::text,phase FROM agenteam_skill.cleanup WHERE project_id=$1`, scope.ProjectID.String()).Scan(&originalCleanup, &phase); err != nil || phase != "gated" || x.counts(t, object).deletedAudit != 0 {
					t.Fatal("late gate COMMIT itself repeated physical work", err)
				}
			}
			fresh := x.newService(t)
			completed := false
			for round := 0; round < 16; round++ {
				report, err := fresh.Cleanup(testContext(t), actor, cause, scope, checkpoint)
				if err != nil || !report.Matches(pc.SkillsParticipant, cause, scope) {
					t.Fatal("fresh current-authorized recovery", err)
				}
				if report.Details().State == pc.CleanupCompleted {
					completed = true
					break
				}
				if boundary == "last_two_domain_anchors" {
					t.Fatal("late final COMMIT was not recovered by all-six-empty proof")
				}
				var actual string
				if err = p.store.QueryRow(testContext(t), `SELECT id::text FROM agenteam_skill.cleanup WHERE project_id=$1`, scope.ProjectID.String()).Scan(&actual); err != nil || actual != originalCleanup {
					t.Fatal("gate recovery replaced original cleanup operation", err)
				}
				checkpoint = report.Details().Checkpoint
			}
			c := x.counts(t, object)
			if !completed || c.local != 0 || c.native != 0 || c.deletedAudit != 1 || x.facts.calls.Load() != 2 {
				t.Fatal("real Unknown recovery final facts", c)
			}
			preserved, ok := skill.UnknownAttempt(firstErr)
			if !ok || preserved.AttemptID() != unknown.AttemptID() || preserved.State() != f.Unknown {
				t.Fatal("late proof rewrote the original Unknown result")
			}
		})
	}
}
