package meeting

import (
	"context"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type summaryTransition struct {
	mu      sync.Mutex
	witness *summarySnapshotWitness
	plan    mc.ServingSnapshotRetentionPlan
	target  string
	unknown bool
}

type summaryTerminalOwner interface {
	summaryTerminal(context.Context, SummaryInput, string, func(context.Context, f.Tx, postgres.SQLExecutor) error) error
}

func (r *Runtime) summaryTransaction(ctx context.Context, input SummaryInput, target string, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	if owner, ok := r.deps.Summaries.(summaryTerminalOwner); ok {
		err := owner.summaryTerminal(ctx, input, target, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			if err := r.requireRuntimeProjectInTx(ctx, tx, input.ProjectID, input.MeetingID); err != nil {
				return err
			}
			return fn(ctx, tx, x)
		})
		if err == nil {
			// Success also includes formal observation of the original
			// transition after an uncertain commit; it never replays a write.
			invalidateMeeting(r.deps.Realtime, input.ProjectID, input.MeetingID)
		}
		return err
	}
	// Explicit domain doubles do not allocate a Model snapshot or Secret pin.
	return r.transaction(ctx, input.ProjectID, input.MeetingID, "summary-"+target, fn)
}

func (m *SummaryModel) summaryTerminal(ctx context.Context, input SummaryInput, target string, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	if target != "published" && target != "failed" && target != "cancelled" {
		return fault(f.InvalidArgument)
	}
	m.mu.Lock()
	pending := m.transitions[input.GenerationID]
	m.mu.Unlock()
	if pending != nil {
		pending.mu.Lock()
		defer pending.mu.Unlock()
		// A later cancellation cannot replace an uncertain release. Observe
		// its original request/plan first, even when the requested target has
		// changed. Only a subsequent runtime visit may perform that new step.
		if err := m.observeSummaryTransition(ctx, pending); err != nil {
			return err
		}
		if pending.target != target {
			return fault(f.ResourceBusy)
		}
		return nil
	}
	var snapshot *string
	var released *time.Time
	var attempt int
	err := m.tx(ctx, input, "terminal-plan", nil, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := m.deps.Authority.guard.RequireCurrentProcessInTx(ctx, tx, m.deps.Authority.process); err != nil {
			return portError(err)
		}
		if err := x.QueryRow(ctx, `SELECT snapshot_id,released_at,attempt FROM agenteam_meeting.summary_generations WHERE id=$1 AND meeting_id=$2 AND turn_id=$3`, input.GenerationID.String(), input.MeetingID.String(), input.TurnID.String()).Scan(&snapshot, &released, &attempt); err != nil {
			return portError(err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if snapshot == nil || released != nil || target == "failed" && attempt < 3 {
		return m.tx(ctx, input, "terminal-without-release", nil, fn)
	}
	if nilPort(m.deps.Retention) {
		return fault(f.DependencyUnbound)
	}
	snapshotID, err := f.ParseID[mc.Snapshot](*snapshot)
	if err != nil {
		return fault(f.InternalError)
	}
	scope, err := i.InProject(input.ProjectID)
	if err != nil {
		return err
	}
	actor, err := m.deps.Actor.Actor(input.GenerationID.String(), scope)
	if err != nil {
		return err
	}
	request := mc.ServingSnapshotRetentionRequest{Actor: actor, Owner: mc.ServingSnapshotOwner{Kind: mc.MeetingConsumer, ProjectID: input.ProjectID, GenerationID: input.GenerationID.String()}, SnapshotID: snapshotID, OwnerVersion: 2, Action: mc.ReleaseServingSnapshot}
	w := &summarySnapshotWitness{model: m, input: input, request: request, active: true}
	plan, err := m.deps.Retention.DiscoverServingSnapshotRetention(context.WithValue(ctx, summarySnapshotKey{}, w), request)
	if err != nil {
		w.active = false
		return err
	}
	transition := &summaryTransition{witness: w, plan: plan, target: target}
	transition.mu.Lock()
	defer transition.mu.Unlock()
	m.mu.Lock()
	if m.transitions[input.GenerationID] != nil {
		m.mu.Unlock()
		w.active = false
		return fault(f.ResourceBusy)
	}
	m.transitions[input.GenerationID] = transition
	m.mu.Unlock()
	ctx = context.WithValue(ctx, summarySnapshotKey{}, w)
	err = m.tx(ctx, input, "terminal-release", plan.RequiredLocks(), func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := fn(ctx, tx, x); err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.summary_generations SET released_at=$3,retention_version=2 WHERE id=$1 AND snapshot_id=$2 AND released_at IS NULL AND status=$4 AND (status IN('published','cancelled') OR (status='failed' AND attempt=3))`, input.GenerationID.String(), snapshotID.String(), now(), target)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ConfirmationStale)
		}
		return m.deps.Retention.ApplyServingSnapshotRetentionInTx(ctx, tx, request, plan)
	})
	transition.unknown = isUnknown(err)
	if !transition.unknown {
		m.forgetSummaryTransition(transition)
	}
	return err
}

func (m *SummaryModel) observeSummaryTransition(ctx context.Context, transition *summaryTransition) error {
	if !transition.unknown {
		return fault(f.ResourceBusy)
	}
	w := transition.witness
	ctx = context.WithValue(ctx, summarySnapshotKey{}, w)
	confirmed := false
	err := m.tx(ctx, w.input, "confirm-terminal-release", transition.plan.RequiredLocks(), func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		applied, err := m.deps.Retention.ServingSnapshotRetentionAppliedInTx(ctx, tx, w.request, transition.plan)
		if err != nil {
			return err
		}
		var state string
		var released *time.Time
		if err = x.QueryRow(ctx, `SELECT status,released_at FROM agenteam_meeting.summary_generations WHERE id=$1`, w.input.GenerationID.String()).Scan(&state, &released); err != nil {
			return portError(err)
		}
		confirmed = applied && state == transition.target && released != nil
		return nil
	})
	if err != nil {
		return err
	}
	if !confirmed {
		return unknownSummary()
	}
	m.forgetSummaryTransition(transition)
	return nil
}
func (m *SummaryModel) forgetSummaryTransition(transition *summaryTransition) {
	m.mu.Lock()
	if m.transitions[transition.witness.input.GenerationID] == transition {
		delete(m.transitions, transition.witness.input.GenerationID)
	}
	m.mu.Unlock()
	transition.witness.active = false
}

var _ summaryTerminalOwner = (*SummaryModel)(nil)
