//go:build integration

package skill_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/minio/minio-go/v7"
)

type cleanupHistoryIdentity struct{ object, upload, attempt, key string }

func (x *skillCleanupFixture) historyIdentity(t *testing.T) cleanupHistoryIdentity {
	t.Helper()
	var value cleanupHistoryIdentity
	err := x.pg.store.QueryRow(testContext(t), `SELECT i.object_id::text,i.upload_id::text,i.current_attempt_id::text,a.candidate_key FROM agenteam_skill.initializations i JOIN agenteam_object.upload_attempts a ON a.id=i.current_attempt_id WHERE i.project_id=$1`, x.seed.request.ProjectID.String()).Scan(&value.object, &value.upload, &value.attempt, &value.key)
	if err != nil {
		t.Fatal("original retained candidate identity", err)
	}
	return value
}

func (x *skillCleanupFixture) completeHistoryPhysical(t *testing.T, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) *pc.CleanupCheckpoint {
	t.Helper()
	var checkpoint *pc.CleanupCheckpoint
	for round := 0; round < 8; round++ {
		report, err := x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
		if err != nil || !report.Matches(pc.SkillsParticipant, cause, scope) || report.Details().State != pc.CleanupPending || report.Details().Checkpoint == nil {
			t.Fatal("physical history cleanup without metadata deletion", err)
		}
		checkpoint = report.Details().Checkpoint
		var phase string
		if err = x.pg.store.QueryRow(testContext(t), `SELECT phase FROM agenteam_skill.cleanup WHERE project_id=$1`, scope.ProjectID.String()).Scan(&phase); err != nil {
			t.Fatal(err)
		}
		if phase == "completed" {
			return checkpoint
		}
	}
	t.Fatal("actual candidate cleanup failed to make finite progress")
	return nil
}

func (x *skillCleanupFixture) finishHistoryCleanup(t *testing.T, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, object string, checkpoint *pc.CleanupCheckpoint) {
	t.Helper()
	for round := 0; round < 20; round++ {
		before := x.counts(t, object)
		report, err := x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
		if err != nil || !report.Matches(pc.SkillsParticipant, cause, scope) {
			t.Fatal("actual history metadata cleanup", err)
		}
		after := x.counts(t, object)
		if report.Details().State == pc.CleanupCompleted {
			if after.local != 0 || after.native != 0 || after.references != 0 || after.activeLeases != 0 || after.deletedAudit != 1 {
				t.Fatal("two-domain final emptiness/Audit", after)
			}
			replayed, err := x.newService(t).Cleanup(testContext(t), actor, cause, scope, checkpoint)
			if err != nil || replayed.Details().State != pc.CleanupCompleted || x.counts(t, object) != after {
				t.Fatal("reconstructed empty cleanup repeated original effects", err)
			}
			return
		}
		if report.Details().State != pc.CleanupPending || report.Details().Checkpoint == nil || after.deletedAudit != 1 || after.local > before.local || after.native > before.native || before.local-after.local > 32 || before.native-after.native > 32 || after.local == before.local && after.native == before.native {
			t.Fatal("bounded historical metadata progress", before, after)
		}
		checkpoint = report.Details().Checkpoint
	}
	t.Fatal("history cleanup did not finish in bounded calls")
}

func TestSkillLifecycleCleanupHistoricalAttempts(t *testing.T) {
	t.Run("native_retry_preserves_abandoned_cause", func(t *testing.T) {
		proxy := newSkillAttemptProxy(t)
		x := newSkillCleanupFixture(t, nil, proxy.configure)
		v := x.seed
		proxy.reject.Store(true)
		first, err := x.service.InitializeProjectSkills(testContext(t), v.actor, v.request)
		if err == nil || first.State == pc.InitializationCompleted || proxy.puts.Load() != 1 || proxy.rejected.Load() == 0 {
			t.Fatal("actual PUT and failed original candidate verification", err)
		}
		old := x.historyIdentity(t)
		// A pre-RoundTrip PUT counter is not proof of stored bytes. Independently
		// consume the original persisted key through the actual MinIO client,
		// outside the failing verification proxy, and join this reader as well.
		pkg, err := x.bundle.Package()
		if err != nil {
			t.Fatal(err)
		}
		want, err := pkg.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		stored, err := x.s3.GetObject(testContext(t), x.bucket, old.key, minio.GetObjectOptions{})
		if err != nil {
			t.Fatal("independent original candidate read", err)
		}
		actual, readErr := io.ReadAll(stored)
		closeErr := stored.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(actual, want) {
			t.Fatal("original PUT did not store the complete accepted package", readErr, closeErr)
		}
		var originalClosed bool
		err = x.pg.store.QueryRow(testContext(t), `SELECT a.io_closed AND a.phase='unknown' AND NOT a.cleanup_gate AND u.state='unknown' AND i.phase='reserved' AND NOT EXISTS(SELECT 1 FROM agenteam_object.object_leases l WHERE l.object_id=a.object_id AND l.state='active') AND NOT EXISTS(SELECT 1 FROM agenteam_skill.work w WHERE w.project_id=i.project_id AND w.phase<>'joined') AND (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=i.project_id AND resource_id=a.object_id AND action='object.upload.failed')=1 FROM agenteam_skill.initializations i JOIN agenteam_object.uploads u ON u.id=i.upload_id JOIN agenteam_object.upload_attempts a ON a.id=i.current_attempt_id WHERE i.project_id=$1`, v.request.ProjectID.String()).Scan(&originalClosed)
		if err != nil || !originalClosed || x.counts(t, old.object).attempts != 1 || x.facts.calls.Load() != 1 {
			t.Fatal("failed native writer, original Audit and Skill work were not retired", err)
		}
		// The same real initialization command retries once. Reserve itself gates
		// the old closed attempt; no RecoverAttemptAccess or cleanup grant is added.
		proxy.reject.Store(false)
		published, err := x.service.InitializeProjectSkills(testContext(t), v.actor, v.request)
		if err != nil || published.State != pc.InitializationCompleted || !published.Matches(v.request) || published.AddSkillsID == nil {
			t.Fatal("same-command native retry publication", err)
		}
		current := x.historyIdentity(t)
		var oldCleanup string
		var exact bool
		err = x.pg.store.QueryRow(testContext(t), `SELECT c.id::text,c.operation_id=u.id AND c.reason='abandoned_attempt' AND c.phase='gated' AND a.cleanup_gate AND a.io_closed AND a.phase='abandoned' AND u.current_attempt_id=$3 AND u.state='committed' AND u.disposition='attached' FROM agenteam_object.cleanup_operations c JOIN agenteam_object.upload_attempts a ON a.id=c.attempt_id JOIN agenteam_object.uploads u ON u.id=a.upload_id WHERE c.attempt_id=$1 AND c.object_id=$2`, old.attempt, old.object, current.attempt).Scan(&oldCleanup, &exact)
		before := x.counts(t, old.object)
		if err != nil || !exact || current.object != old.object || current.upload != old.upload || current.attempt == old.attempt || before.attempts != 2 || before.work != 2 || before.references != 1 || before.activeLeases != 0 || before.deletedAudit != 0 || x.facts.calls.Load() != 2 {
			t.Fatal("real old AbandonedAttempt/current publication provenance", err, before)
		}
		seedReaderReadyProject(t, x.pg, v, *published.AddSkillsID)
		actor, cause, scope := seedCleanupStop(t, x.pg, v, x.manifest, pc.Delete)
		x.enterCleaning(t, actor, cause, scope)
		checkpoint := x.completeHistoryPhysical(t, actor, cause, scope)
		var mode, currentMode string
		err = x.pg.store.QueryRow(testContext(t), `SELECT old.mode,now.mode,old.id=$4 AND old.operation_id=$3 AND old.reason='abandoned_attempt' AND old.phase='completed' AND now.operation_id=local.id AND now.reason='project_deleted' AND now.phase='completed' AND old.id<>now.id AND o.state='deleted' AND u.disposition='revoked' FROM agenteam_object.cleanup_operations old JOIN agenteam_object.cleanup_operations now ON now.object_id=old.object_id AND now.attempt_id=$2 JOIN agenteam_skill.cleanup local ON local.object_id=old.object_id JOIN agenteam_object.objects o ON o.id=old.object_id JOIN agenteam_object.uploads u ON u.id=local.upload_id WHERE old.attempt_id=$1`, old.attempt, current.attempt, old.upload, oldCleanup).Scan(&mode, &currentMode, &exact)
		if err != nil || !exact || mode != "zero_marker" || x.counts(t, old.object).deletedAudit != 1 || x.facts.calls.Load() != 3 {
			t.Fatal("original two causes/IDs changed or native Audit duplicated", err)
		}
		for _, candidate := range []struct{ key, mode string }{{old.key, mode}, {current.key, currentMode}} {
			stat, err := x.s3.StatObject(testContext(t), x.bucket, candidate.key, minio.StatObjectOptions{})
			if candidate.mode == "zero_marker" {
				if err != nil || stat.Size != 0 {
					t.Fatal("permanent empty marker/payload distinction", err)
				}
			} else if candidate.mode != "delete" || minio.ToErrorResponse(err).Code != "NoSuchKey" {
				t.Fatal("actual candidate payload deletion", err)
			}
		}
		x.finishHistoryCleanup(t, actor, cause, scope, old.object, checkpoint)
		if x.facts.calls.Load() != 3 {
			t.Fatal("mixed-cause replay produced another native Audit")
		}
	})

	t.Run("seeded_retained_mapping_history_batches_and_fk_rollback", func(t *testing.T) {
		x := newSkillCleanupFixture(t, nil)
		v := x.seed
		published, err := x.service.InitializeProjectSkills(testContext(t), v.actor, v.request)
		if err != nil || published.State != pc.InitializationCompleted || published.AddSkillsID == nil {
			t.Fatal("actual current publication", err)
		}
		current := x.historyIdentity(t)
		seedReaderReadyProject(t, x.pg, v, *published.AddSkillsID)
		actor, cause, scope := seedCleanupStop(t, x.pg, v, x.manifest, pc.Delete)
		x.enterCleaning(t, actor, cause, scope)
		checkpoint := x.completeHistoryPhysical(t, actor, cause, scope)
		native := x.counts(t, current.object)
		// Explicit historical compatibility fixture, AFTER actual physical
		// completion: only local retained noncurrent mappings are inserted. They
		// are not 65 API-produced attempts or native cleanup/join/witness facts.
		// D05 currently caps uncleaned attempts per command at two; Skills does
		// not bind initialization RecoverAttemptAccess to evade that boundary.
		var historical []string
		for i := 0; i < 65; i++ {
			historical = append(historical, testID[oc.Attempt](t).String())
		}
		result := x.pg.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
			projectKey, _ := f.ProjectLock(scope.ProjectID.String())
			objectKey, _ := f.AggregateLock(f.ObjectAggregate, current.object)
			if err := x.pg.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: projectKey, Mode: f.Exclusive}, {Key: objectKey, Mode: f.Exclusive}}); err != nil {
				return err
			}
			q, err := x.pg.store.InTx(tx)
			if err != nil {
				return err
			}
			for _, attempt := range historical {
				tag, err := q.Exec(ctx, `INSERT INTO agenteam_skill.object_attempts(attempt_id,project_id,creation_id,skill_id,revision_id,object_id,upload_id,process_id,created_at) SELECT $2,project_id,creation_id,skill_id,revision_id,object_id,upload_id,process_id,created_at FROM agenteam_skill.object_attempts WHERE attempt_id=$1`, current.attempt, attempt)
				if err != nil {
					return err
				}
				if tag.RowsAffected() != 1 {
					return errors.New("historical mapping fixture lost original parent")
				}
			}
			// Exercise deferred FK validation, not only statement execution.
			_, err = q.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
			return err
		})
		requireCommitted(t, result)
		seeded := x.counts(t, current.object)
		if seeded.attempts != 66 || seeded.native != native.native || seeded.local != native.local+65 || seeded.deletedAudit != 1 {
			t.Fatal("historical fixture escaped its local mapping scope", seeded)
		}
		// The real initialization work must retire before mapping compression.
		report, err := x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
		if err != nil || report.Details().State != pc.CleanupPending || x.counts(t, current.object).work != 0 || x.counts(t, current.object).attempts != 66 {
			t.Fatal("work-before-attempt history order", err)
		}
		checkpoint = report.Details().Checkpoint
		before := x.counts(t, current.object)
		injected := errors.New("test rollback after actual 32 retained mapping deletes")
		var reached atomic.Int32
		x.store.setAfter(func(ctx context.Context, tx f.Tx) error {
			q, err := x.pg.store.InTx(tx)
			if err != nil {
				return err
			}
			counts, err := readCleanupCounts(ctx, q, scope.ProjectID.String(), current.object)
			if err != nil || counts.attempts != 34 {
				return err
			}
			if _, err := q.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`); err != nil {
				return err
			}
			reached.Add(1)
			return injected
		})
		report, err = x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
		x.store.setAfter(nil)
		if !errors.Is(err, injected) || report.Details().State == pc.CleanupCompleted || reached.Load() != 1 || x.counts(t, current.object) != before {
			t.Fatal("real mapping batch rollback lost historical rows", err)
		}
		for _, remaining := range []int{34, 2, 1} {
			report, err = x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
			after := x.counts(t, current.object)
			if err != nil || report.Details().State != pc.CleanupPending || after.attempts != remaining || after.native != before.native || after.deletedAudit != 1 || after.local != remaining+4 {
				t.Fatal("actual 32/32/1 mapping batch or retained anchors", err, after)
			}
			var retained bool
			if err = x.pg.store.QueryRow(testContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_skill.initializations i JOIN agenteam_skill.object_attempts a ON a.attempt_id=i.current_attempt_id WHERE i.project_id=$1 AND a.attempt_id=$2)`, scope.ProjectID.String(), current.attempt).Scan(&retained); err != nil || !retained {
				t.Fatal("bounded mapping batch removed current anchor", err)
			}
			checkpoint = report.Details().Checkpoint
		}
		x.finishHistoryCleanup(t, actor, cause, scope, current.object, checkpoint)
	})
}
