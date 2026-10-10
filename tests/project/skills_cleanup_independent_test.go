//go:build integration

package project_test

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The production authority, Store and PostgreSQL reads are real. Initialization,
// Cleaning and predecessor progress retain the author's explicit test-owned
// stimuli; this does not claim that Skills or Object executed participant work.
func TestProjectSkillsCleanupIndependentRevalidation(t *testing.T) {
	v := newSkillsCleanupFixture(t)
	t.Run("progress_revoked_and_restored_in_original_tx", func(t *testing.T) {
		op, actor, _ := v.accept(t, c.Delete)
		skillsCleanupPhase(t, v, op)
		before := independentSkillsCleanupSnapshot(t, v.raw, op)
		activity := v.snapshot(t, op.ProjectID, v.owner)
		rollback := errors.New("independent rollback after successful revalidation")
		checks := 0
		result := v.raw.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
			if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{r3Lock(op.ProjectID, f.Exclusive)}); err != nil {
				return err
			}
			e, err := v.raw.InTx(tx)
			if err != nil {
				return err
			}
			check := func(want f.Code) {
				t.Helper()
				state := independentSkillsCleanupSnapshot(t, e, op)
				err := v.facts.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(op), c.SkillsParticipant, c.CleanupPhase)
				if want == "" {
					if err != nil {
						t.Fatal("valid original transaction rejected", err)
					}
				} else {
					requireCode(t, err, want)
				}
				if independentSkillsCleanupSnapshot(t, e, op) != state {
					t.Fatal("authority changed original transaction facts")
				}
				checks++
			}
			change := func(name c.ParticipantName, state string) error {
				tag, err := e.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET cleanup_state=$3,version=version+1 WHERE operation_id=$1 AND participant_name=$2`, op.ID.String(), string(name), state)
				if err == nil && tag.RowsAffected() != 1 {
					return errors.New("exact participant stimulus did not update one row")
				}
				return err
			}
			check("")
			if err := change(c.SkillsParticipant, "completed"); err != nil {
				return err
			}
			check(f.InvalidState)
			if err := change(c.SkillsParticipant, "required"); err != nil {
				return err
			}
			if err := change(skillsCleanupPrerequisite, "pending"); err != nil {
				return err
			}
			check(f.InvalidState)
			if err := change(skillsCleanupPrerequisite, "completed"); err != nil {
				return err
			}
			check("")
			return rollback
		})
		if checks != 4 || result.State() != f.NotCommitted {
			t.Fatal("original callback did not finish its revalidation/rollback")
		}
		if independentSkillsCleanupSnapshot(t, v.raw, op) != before || v.snapshot(t, op.ProjectID, v.owner) != activity {
			t.Fatal("rolled back stimulus escaped the original transaction")
		}
		if err := skillsCleanupCheck(t, v, v.facts, op, actor, r3Cause(op)); err != nil {
			t.Fatal("rolled back denial was cached", err)
		}
	})

	t.Run("owner_reread_after_prior_grant", func(t *testing.T) {
		op, actor, _ := v.accept(t, c.Delete)
		skillsCleanupPhase(t, v, op)
		other := v.human(t, "independent-owner", "user")
		originalID, err := f.ParseID[i.User](v.owner.Details().UserID)
		if err != nil {
			t.Fatal(err)
		}
		otherID, err := f.ParseID[i.User](other.Details().UserID)
		if err != nil {
			t.Fatal(err)
		}
		originalLock, err := f.UserLock(originalID.String())
		if err != nil {
			t.Fatal(err)
		}
		otherLock, err := f.UserLock(otherID.String())
		if err != nil {
			t.Fatal(err)
		}
		before := independentSkillsCleanupSnapshot(t, v.raw, op)
		activity := v.snapshot(t, op.ProjectID, v.owner)
		checks := 0
		result := v.raw.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
			if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{{Key: originalLock, Mode: f.Exclusive}, {Key: otherLock, Mode: f.Exclusive}, r3Lock(op.ProjectID, f.Exclusive)}); err != nil {
				return err
			}
			e, err := v.raw.InTx(tx)
			if err != nil {
				return err
			}
			check := func(want f.Code) {
				t.Helper()
				state := independentSkillsCleanupSnapshot(t, e, op)
				err := v.facts.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(op), c.SkillsParticipant, c.CleanupPhase)
				if want == "" {
					if err != nil {
						t.Fatal("current owner rejected", err)
					}
				} else {
					requireCode(t, err, want)
				}
				if independentSkillsCleanupSnapshot(t, e, op) != state {
					t.Fatal("authority wrote current owner facts")
				}
				checks++
			}
			change := func(owner string) error {
				tag, err := e.Exec(ctx, `UPDATE agenteam_project.projects SET owner_user_id=$2 WHERE id=$1`, op.ProjectID.String(), owner)
				if err == nil && tag.RowsAffected() != 1 {
					return errors.New("exact owner stimulus did not update one row")
				}
				return err
			}
			check("")
			// Only current Project owner changes; the original accepted operation
			// owner/cause are retained. This is not a public ownership transfer.
			if err := change(otherID.String()); err != nil {
				return err
			}
			check(f.DependencyUnavailable)
			if err := change(originalID.String()); err != nil {
				return err
			}
			check("")
			return nil
		})
		if checks != 3 || result.State() != f.Committed {
			t.Fatal("owner revalidation did not finish in the original transaction", result.Fault())
		}
		if independentSkillsCleanupSnapshot(t, v.raw, op) != before || v.snapshot(t, op.ProjectID, v.owner) != activity {
			t.Fatal("restored owner transaction changed facts/activity")
		}
	})

	t.Run("cancelled_original_call_cannot_reuse_prior_grant", func(t *testing.T) {
		op, actor, _ := v.accept(t, c.Delete)
		skillsCleanupPhase(t, v, op)
		before := independentSkillsCleanupSnapshot(t, v.raw, op)
		activity := v.snapshot(t, op.ProjectID, v.owner)
		var ended f.Tx
		var denied error
		first := false
		result := v.raw.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
			ended = tx
			if err := v.raw.AcquireAll(ctx, tx, []f.LockRequest{r3Lock(op.ProjectID, f.Shared)}); err != nil {
				return err
			}
			if err := v.facts.ValidateLifecycleInTx(ctx, tx, actor, r3Cause(op), c.SkillsParticipant, c.CleanupPhase); err != nil {
				return err
			}
			first = true
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			denied = v.facts.ValidateLifecycleInTx(cancelled, tx, actor, r3Cause(op), c.SkillsParticipant, c.CleanupPhase)
			return denied
		})
		if !first || denied == nil || result.State() != f.NotCommitted {
			t.Fatal("cancelled second call reused an earlier grant")
		}
		requireCode(t, v.facts.ValidateLifecycleInTx(ctxFor(t), ended, actor, r3Cause(op), c.SkillsParticipant, c.CleanupPhase), f.DependencyUnavailable)
		if independentSkillsCleanupSnapshot(t, v.raw, op) != before || v.snapshot(t, op.ProjectID, v.owner) != activity {
			t.Fatal("cancelled original transaction changed facts/activity")
		}
		if err := skillsCleanupCheck(t, v, v.facts, op, actor, r3Cause(op)); err != nil {
			t.Fatal("healthy successor rejected after actual original transaction return", err)
		}
	})
}

func independentSkillsCleanupSnapshot(t *testing.T, e postgres.SQLExecutor, op c.LifecycleOperation) string {
	t.Helper()
	var snapshot string
	err := e.QueryRow(ctxFor(t), `SELECT jsonb_build_array(
 (SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),
 (SELECT to_jsonb(o) FROM agenteam_project.lifecycle_operations o WHERE id=$2),
 (SELECT jsonb_agg(to_jsonb(p) ORDER BY participant_name) FROM agenteam_project.lifecycle_participants p WHERE operation_id=$2),
 (SELECT jsonb_agg(to_jsonb(r) ORDER BY operation_id) FROM agenteam_project.deletion_receipts r WHERE deleted_project_id=$1))::text`, op.ProjectID.String(), op.ID.String()).Scan(&snapshot)
	if err != nil {
		t.Fatal("independent current-facts snapshot", err)
	}
	return snapshot
}
