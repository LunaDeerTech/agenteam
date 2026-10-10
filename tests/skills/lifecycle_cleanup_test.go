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
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/minio/minio-go/v7"
)

type cleanupCounts struct {
	local, work, attempts, native, references, activeLeases, deletedAudit int
}

// Counts include both domains independently; they never manufacture the
// expected private witness, canonical reference, or a cleanup completion.
func readCleanupCounts(ctx context.Context, q postgres.SQLExecutor, project, object string) (cleanupCounts, error) {
	var c cleanupCounts
	err := q.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_skill.initializations WHERE project_id=$1)
 +(SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1)
 +(SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1)
 +(SELECT count(*) FROM agenteam_skill.cleanup WHERE project_id=$1)
 +(SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1)
 +(SELECT count(*) FROM agenteam_skill.object_attempts WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_skill.object_attempts WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_object.objects WHERE id=$2)
 +(SELECT count(*) FROM agenteam_object.uploads WHERE object_id=$2)
 +(SELECT count(*) FROM agenteam_object.upload_attempts WHERE object_id=$2)
 +(SELECT count(*) FROM agenteam_object.cleanup_operations WHERE object_id=$2)
 +(SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$2)
 +(SELECT count(*) FROM agenteam_object.project_work WHERE object_id=$2)
 +(SELECT count(*) FROM agenteam_object.object_transfers WHERE object_id=$2),
 (SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$2),
 (SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$2 AND state='active'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND resource_id=$2 AND action='object.delete')`, project, object).Scan(&c.local, &c.work, &c.attempts, &c.native, &c.references, &c.activeLeases, &c.deletedAudit)
	return c, err
}

func (x *skillCleanupFixture) counts(t *testing.T, object string) cleanupCounts {
	t.Helper()
	c, err := readCleanupCounts(testContext(t), x.pg.store, x.seed.request.ProjectID.String(), object)
	if err != nil {
		t.Fatal("actual two-domain counts", err)
	}
	return c
}

func TestSkillLifecycleCleanupPersistence(t *testing.T) {
	x := newSkillCleanupFixture(t, nil)
	v := x.seed
	initialized, err := x.service.InitializeProjectSkills(testContext(t), v.actor, v.request)
	if err != nil || initialized.State != pc.InitializationCompleted || !initialized.Matches(v.request) || initialized.AddSkillsID == nil {
		t.Fatal("actual publication before cleanup", err)
	}
	skillID := *initialized.AddSkillsID
	initializeReaderAccountKeys(t, x.pg)
	owner := seedReaderHuman(t, x.pg, v.owner, "cleanup-owner", "user")
	seedReaderReadyProject(t, x.pg, v, skillID)
	content, err := x.bundle.Package()
	if err != nil {
		t.Fatal(err)
	}
	want, err := content.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	// Every history row comes from a real public call and actual source EOF /
	// Close. Neither cleanup history nor lease retirement is inserted by SQL.
	for i := 0; i < 65; i++ {
		body, err := x.service.OpenPackage(testContext(t), owner, v.request.ProjectID, skillID, 1)
		if err != nil {
			t.Fatal("actual history OpenPackage", i, err)
		}
		got, readErr := io.ReadAll(body)
		closeErr := body.Close()
		if readErr != nil || closeErr != nil || !body.Joined() || !bytes.Equal(got, want) {
			t.Fatal("actual history reader tail", i, readErr, closeErr)
		}
	}
	var object, upload, candidate string
	err = x.pg.store.QueryRow(testContext(t), `SELECT i.object_id::text,i.upload_id::text,o.candidate_key FROM agenteam_skill.initializations i JOIN agenteam_object.objects o ON o.id=i.object_id WHERE i.project_id=$1 AND i.phase='published'`, v.request.ProjectID.String()).Scan(&object, &upload, &candidate)
	if err != nil {
		t.Fatal("original published identity", err)
	}
	original := x.counts(t, object)
	if original.work != 66 || original.attempts != 1 || original.references != 1 || original.activeLeases != 0 || original.deletedAudit != 0 || original.native <= 65 || x.facts.calls.Load() != 1 {
		t.Fatal("real publication/history prerequisites", original)
	}
	actor, cause, scope := seedCleanupStop(t, x.pg, v, x.manifest, pc.Delete)
	var checkpoint *pc.CleanupCheckpoint
	if !t.Run("current_gate_before_irreversible_release", func(t *testing.T) {
		if _, err := x.service.Cleanup(testContext(t), actor, cause, scope, nil); err == nil {
			t.Fatal("Stopping was treated as Cleaning")
		}
		if got := x.counts(t, object); got != original {
			t.Fatal("denied current phase mutated either domain", got)
		}
		x.enterCleaning(t, actor, cause, scope)
		before := x.counts(t, object)
		wrong := cause
		wrong.ProjectVersion++
		if _, err := x.service.Cleanup(testContext(t), actor, wrong, scope, nil); err == nil {
			t.Fatal("current lifecycle version mismatch authorized cleanup")
		}
		if got := x.counts(t, object); got != before {
			t.Fatal("wrong version mutated either domain", got)
		}
		injected := errors.New("test-only rollback after real serving close and Release")
		var selected atomic.Int32
		x.store.setAfter(func(ctx context.Context, tx f.Tx) error {
			q, err := x.pg.store.InTx(tx)
			if err != nil {
				return err
			}
			var reached bool
			err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.cleanup c JOIN agenteam_skill.skills s ON s.id=c.skill_id JOIN agenteam_object.objects o ON o.id=c.object_id JOIN agenteam_object.uploads u ON u.id=c.upload_id WHERE c.project_id=$1 AND c.phase='gated' AND NOT s.serving AND o.cleaning AND o.state='available' AND u.disposition='revoked') AND NOT EXISTS(SELECT 1 FROM agenteam_object.object_references WHERE object_id=$2)`, scope.ProjectID.String(), object).Scan(&reached)
			if err != nil || !reached {
				return err
			}
			selected.Add(1)
			return injected
		})
		report, err := x.service.Cleanup(testContext(t), actor, cause, scope, nil)
		x.store.setAfter(nil)
		if !errors.Is(err, injected) || report.Details().State == pc.CleanupCompleted || selected.Load() != 1 || x.counts(t, object) != before || x.facts.calls.Load() != 1 {
			t.Fatal("initial gate and canonical Release did not roll back together", err, selected.Load())
		}
		var restored bool
		err = x.pg.store.QueryRow(testContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_skill.skills s JOIN agenteam_skill.revisions r ON r.id=s.revision_id JOIN agenteam_object.objects o ON o.id=r.object_id JOIN agenteam_object.uploads u ON u.object_id=o.id WHERE s.project_id=$1 AND s.serving AND NOT o.cleaning AND o.state='available' AND u.disposition='attached') AND NOT EXISTS(SELECT 1 FROM agenteam_skill.cleanup WHERE project_id=$1)`, scope.ProjectID.String()).Scan(&restored)
		if err != nil || !restored {
			t.Fatal("rollback retained a closed admission or detached upload", err)
		}
	}) {
		return
	}
	if !t.Run("actual_physical_audit_and_bounded_history", func(t *testing.T) {
		phase := ""
		for round := 0; round < 8; round++ {
			report, err := x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
			if err != nil || !report.Matches(pc.SkillsParticipant, cause, scope) || report.Details().State != pc.CleanupPending {
				t.Fatal("physical completion retains parent until metadata", err)
			}
			checkpoint = report.Details().Checkpoint
			if checkpoint == nil {
				t.Fatal("committed cleanup lost its provider checkpoint")
			}
			if err = x.pg.store.QueryRow(testContext(t), `SELECT phase FROM agenteam_skill.cleanup WHERE project_id=$1`, scope.ProjectID.String()).Scan(&phase); err != nil {
				t.Fatal(err)
			}
			if phase == "completed" {
				break
			}
		}
		if phase != "completed" {
			t.Fatal("actual physical cleanup did not finish within finite progress calls")
		}
		var state, disposition, mode string
		err = x.pg.store.QueryRow(testContext(t), `SELECT o.state,u.disposition,c.mode FROM agenteam_object.objects o JOIN agenteam_object.uploads u ON u.object_id=o.id JOIN agenteam_object.cleanup_operations c ON c.attempt_id=u.current_attempt_id WHERE o.id=$1 AND u.id=$2 AND c.reason='project_deleted' AND c.phase='completed'`, object, upload).Scan(&state, &disposition, &mode)
		c := x.counts(t, object)
		if err != nil || state != "deleted" || disposition != "revoked" || c.references != 0 || c.activeLeases != 0 || c.deletedAudit != 1 || c.work != 66 || x.facts.calls.Load() != 2 {
			t.Fatal("real physical/native Audit/current parent relation", err, c)
		}
		stat, statErr := x.s3.StatObject(testContext(t), x.bucket, candidate, minio.StatObjectOptions{})
		switch mode {
		case "zero_marker":
			if statErr != nil || stat.Size != 0 {
				t.Fatal("permanent empty marker was removed or retained payload", statErr)
			}
		case "delete":
			if minio.ToErrorResponse(statErr).Code != "NoSuchKey" {
				t.Fatal("actual payload was not deleted", statErr)
			}
		default:
			t.Fatal("unknown native physical mode")
		}
		for _, remaining := range []int{34, 2, 0} {
			before := x.counts(t, object)
			report, err := x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
			if err != nil || report.Details().State != pc.CleanupPending {
				t.Fatal("bounded local history", err)
			}
			checkpoint = report.Details().Checkpoint
			after := x.counts(t, object)
			if after.work != remaining || before.work-after.work > 32 || after.attempts != 1 || after.native != before.native || after.local != remaining+5 || after.deletedAudit != 1 {
				t.Fatal("local batch lost parent or crossed 32 rows", before, after)
			}
		}
	}) {
		return
	}
	t.Run("last_object_and_skill_anchors_share_original_transaction", func(t *testing.T) {
		for round := 0; x.counts(t, object).native > 4 && round < 16; round++ {
			before := x.counts(t, object)
			report, err := x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
			if err != nil || report.Details().State != pc.CleanupPending {
				t.Fatal("actual D05 metadata batch", err)
			}
			checkpoint = report.Details().Checkpoint
			after := x.counts(t, object)
			if n := before.native - after.native; n < 1 || n > oc.ObjectMetadataPurgeBatchLimit || after.native < 4 || after.local != 5 || after.deletedAudit != 1 {
				t.Fatal("native bounded progress or parent lifetime", before, after)
			}
		}
		before := x.counts(t, object)
		if before.native != 4 || before.local != 5 {
			t.Fatal("expected real final anchors", before)
		}
		injected := errors.New("test-only rollback after real two-domain final deletion")
		var selected atomic.Int32
		x.store.setAfter(func(ctx context.Context, tx f.Tx) error {
			q, err := x.pg.store.InTx(tx)
			if err != nil {
				return err
			}
			c, err := readCleanupCounts(ctx, q, scope.ProjectID.String(), object)
			if err != nil || c.local != 0 || c.native != 0 {
				return err
			}
			projectKey, _ := f.ProjectLock(scope.ProjectID.String())
			objectKey, _ := f.AggregateLock(f.ObjectAggregate, object)
			if err = x.pg.store.RequireHeldLocks(ctx, tx, []f.LockRequest{{Key: projectKey, Mode: f.Exclusive}, {Key: objectKey, Mode: f.Exclusive}}); err != nil {
				return err
			}
			selected.Add(1)
			return injected
		})
		report, err := x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
		x.store.setAfter(nil)
		if err == nil || !errors.Is(err, injected) || report.Details().State == pc.CleanupCompleted || selected.Load() != 1 || x.counts(t, object) != before {
			t.Fatal("real final transaction did not roll back both domains", err, selected.Load())
		}
		report, err = x.service.Cleanup(testContext(t), actor, cause, scope, checkpoint)
		if err != nil || report.Details().State != pc.CleanupCompleted {
			t.Fatal("actual final same-Tx commit", err)
		}
		closed := x.counts(t, object)
		if closed.local != 0 || closed.native != 0 || closed.references != 0 || closed.activeLeases != 0 || closed.deletedAudit != 1 {
			t.Fatal("final completion lost durable empty proof/Audit", closed)
		}
		fresh := x.newService(t)
		replayed, err := fresh.Cleanup(testContext(t), actor, cause, scope, checkpoint)
		if err != nil || replayed.Details().State != pc.CleanupCompleted || x.counts(t, object) != closed || x.facts.calls.Load() != 2 {
			t.Fatal("all-empty recovery repeated missing-parent Object cleanup", err)
		}
	})
}
