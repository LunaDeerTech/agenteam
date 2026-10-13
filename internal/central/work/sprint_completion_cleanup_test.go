package work

import (
	"bytes"
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"testing"
)

// The existing owned cleanup controls prove the new table's complete preimage
// and Unknown lane. These are controlled physical outcomes, not PostgreSQL.
func TestSprintCompleteCleanupOriginalPreimage(t *testing.T) {
	for _, applied := range []bool{false, true} {
		name := "not-applied"
		if applied {
			name = "applied"
		}
		t.Run(name, func(t *testing.T) {
			v := newWorkCleanup(t)
			const stage = "sprint_complete_commands"
			v.store.seed(t, stage, v.base.p, 1)
			v.store.seed(t, "structure_commands", v.base.p, 1)
			before := v.store.rows(stage, v.base.p)[0]
			var original f.CommitResult
			masked := false
			v.base.store.after = func(_ context.Context, cause f.TransactionCause, _ int, result f.CommitResult) f.CommitResult {
				if cause.Details().Owner == "work-project-cleanup" && result.State() == f.Committed && !masked && v.store.writes() == 1 {
					masked = true
					if !applied {
						v.store.put(stage, v.base.p, before)
					}
					original = f.UnknownResult(pureID[f.TransactionAttempt](t, 930), cause)
					return original
				}
				return result
			}
			report, err := v.cleanup(v.ctx, nil)
			requireWorkCleanupUnknown(t, report, err, original)
			v.base.store.after = nil
			writes := v.store.writes()
			changed := before
			changed.Record = bytes.Replace(before.Record, []byte(`"id":`), []byte(`"plan":{"changed":true},"id":`), 1)
			if bytes.Equal(changed.Record, before.Record) {
				t.Fatal("preimage stimulus did not change full bytes")
			}
			v.store.put(stage, v.base.p, changed)
			report, err = v.cleanup(v.ctx, nil)
			requireWorkCleanupUnknown(t, report, err, original)
			if applied {
				v.store.dataMu.Lock()
				delete(v.store.tables[stage][v.base.p], before.Key)
				v.store.dataMu.Unlock()
			} else {
				v.store.put(stage, v.base.p, before)
			}
			report, err = v.cleanup(v.ctx, nil)
			// Read-only confirmation resets the diagnostic cursor to the first
			// stage; only the following explicit call may inspect and delete the
			// next actual batch (project_cleanup.go: projectCleanupPending).
			cp := requireWorkCleanup(t, v, report, err, pc.CleanupPending, "task_events")
			if v.store.writes() != writes {
				t.Fatal("confirmation replayed DELETE")
			}
			report, err = v.cleanup(v.ctx, cp)
			state, next := pc.CleanupPending, "structure_commands"
			if applied {
				state, next = pc.CleanupCompleted, "completed"
			}
			requireWorkCleanup(t, v, report, err, state, next)
			if v.store.writes() != writes+1 {
				t.Fatal("explicit next batch did not preserve original cursor")
			}
		})
	}
}
