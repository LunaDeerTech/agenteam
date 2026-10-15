package execution

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestExecutionRecoveryLateCancellationRevisitsOnlyCanonicalCancellation(t *testing.T) {
	for _, mode := range []string{"created", "preparing", "no-durable-request", "blocked-then-safe", "signal-during-original-call"} {
		t.Run(mode, func(t *testing.T) {
			s, job, ctx, store, events, processes := executionRecoveryFixture(t)
			// This scheduling fixture supplies an exact original Dispatch
			// association; it does not claim to exercise Scheduler's writer.
			row, err := loadExecution(ctx, store, job.dispatch.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			row.launch.Lineage.DispatchID = newTestID[struct{}](t).String()
			raw, err := json.Marshal(row.launch)
			if err != nil {
				t.Fatal(err)
			}
			digest, err := row.launch.Digest()
			if err != nil {
				t.Fatal(err)
			}
			store.rows[row.summary.ID.String()][10], store.rows[row.summary.ID.String()][13] = raw, string(digest)
			job.dispatch.DispatchID, job.dispatch.Digest = row.launch.Lineage.DispatchID, digest
			e := &AssociatedExecutor{state: s}
			s.started, s.root = true, context.Background()
			s.changed = make(chan struct{})
			s.options = AssociatedExecutorOptions{MaxOwned: 1, RecoveryInterval: time.Hour}
			s.ops = associatedExecutorOps{
				owners: s.ownedCalls,
				inspect: func(context.Context, i.ProjectID, associatedIdentity) (associatedFacts, error) {
					t.Error("cancellation-only visit escaped into ordinary traversal")
					return associatedFacts{}, fault(f.Forbidden)
				},
				prepare: func(context.Context, i.ExecutionID) error {
					t.Error("cancellation replayed preparation")
					return fault(f.Forbidden)
				},
				start: func(context.Context, i.ExecutionID) (c.DirectTextReceipt, error) {
					t.Error("cancellation replayed DirectText Start")
					return c.DirectTextReceipt{}, fault(f.Forbidden)
				},
			}
			job.ctx, job.cancel = context.WithCancel(ctx)
			defer job.cancel()
			job.active, job.err = false, fault(f.DependencyUnavailable)
			if mode == "preparing" {
				store.rows[job.dispatch.ExecutionID.String()][3] = "preparing"
				claim := preparationClaim{execution: job.dispatch.ExecutionID, project: job.project, agent: job.dispatch.AgentID, attempt: newTestID[preparationAttempt](t), process: processes.process, fence: 1, phase: "terminal"}
				store.claims[claim.execution.String()], store.attempts[claim.attempt.String()] = claim, claim
			}
			if mode == "no-durable-request" {
				store.rows[job.dispatch.ExecutionID.String()][5] = (*time.Time)(nil)
			}
			if mode == "blocked-then-safe" {
				store.history = true
			}
			if mode == "signal-during-original-call" {
				job.active = true
			}
			s.notifyCancellation(job.dispatch.ExecutionID)
			if mode == "signal-during-original-call" {
				job.active = false // Original callback's physical return.
			}
			before := store.writes
			_, _ = e.Advance(context.Background(), job.project, job.dispatch.AssociatedDispatch)
			associatedTestIdle(t, e)
			if mode == "blocked-then-safe" {
				if !job.cancelPending || job.retained || store.writes != before {
					t.Fatal("failed canonical proof was forgotten or became fake retained I/O")
				}
				store.history = false
				s.mu.Lock()
				job.next = time.Time{}
				s.mu.Unlock()
				_, _ = e.Advance(context.Background(), job.project, job.dispatch.AssociatedDispatch)
				associatedTestIdle(t, e)
			}
			if mode == "no-durable-request" {
				if store.writes != before || events.appends != 0 || job.cancelPending || job.active || job.retained {
					t.Fatal("notification itself became cancellation authority")
				}
				return
			}
			if store.rows[job.dispatch.ExecutionID.String()][3] != "cancelled" || events.appends != 1 || s.jobs[job.dispatch.ExecutionID] != nil {
				t.Fatal("late cancellation stayed trapped in cached known failure")
			}
		})
	}
}
