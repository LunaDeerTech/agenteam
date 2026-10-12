package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// All fields are immutable scalars. The record is private and is never accepted
// from a caller. Its historical ProcessID does not convey the current owner.
type roundModelClaimRecord struct{ fields c.RoundModelClaimFields }

func (r roundModelClaimRecord) Fields() c.RoundModelClaimFields { return r.fields }
func (r roundModelClaimRecord) Validate() error                 { return r.fields.Validate() }
func (roundModelClaimRecord) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_round_model_claim")
}
func (roundModelClaimRecord) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_round_model_claim"`), nil
}
func (roundModelClaimRecord) LogValue() slog.Value {
	return slog.StringValue("execution_round_model_claim")
}

func (r roundModelClaimRecord) matchesRequest(request c.NextRoundModelRequest, at f.Instant) bool {
	if r.Validate() != nil || request.Validate() != nil || at.Validate() != nil {
		return false
	}
	a, b := r.fields, request.Fields()
	return a.ProjectID == b.ProjectID && a.AgentID == b.AgentID && a.ExecutionID == b.ExecutionID && a.RoundInputID == b.RoundInputID && a.RoundInputDigest == b.RoundInputDigest && a.SnapshotID == b.SnapshotID && a.SnapshotDigest == b.SnapshotDigest && a.ProjectionDigest == b.ProjectionDigest && a.CallID == b.CallID && a.InputDigest == request.InputDigest() && a.RequestDigest == request.Digest() && !a.CreatedAt.Time().Before(at.Time())
}

func sameRoundModelClaimObservation(original *roundModelCallUnknown, observed roundModelClaimRecord) bool {
	return original != nil && original.candidate.Validate() == nil && observed.Validate() == nil && original.key.execution == observed.fields.ExecutionID && original.key.next == observed.fields.RoundInputID && original.candidate.fields == observed.fields
}

func (o *RoundModelCallOwner) claimInTx(ctx context.Context, tx f.Tx, work *roundModelCallWork, scope *executionRecord, candidate *roundModelClaimRecord, locks []f.LockRequest) (*roundModelClaimRecord, bool, error) {
	if scope == nil || work == nil || scope.summary.ID != work.key.execution || !work.observe && (candidate == nil || candidate.Validate() != nil) {
		return nil, false, invalid()
	}
	if err := o.requireWork(ctx, work); err != nil {
		return nil, false, err
	}
	x, err := o.store.InTx(tx)
	if err != nil {
		return nil, false, portError(err)
	}
	want := roundInputLocks(scope.summary.ProjectID, scope.summary.AgentID, scope.summary.ID)
	if err = o.store.RequireHeldLocks(ctx, tx, want); err != nil {
		return nil, false, portError(err)
	}
	if err = o.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, false, portError(err)
	}
	if err = o.guard.RequireCurrentProcessInTx(ctx, tx, o.process); err != nil {
		return nil, false, portError(err)
	}
	row, err := loadExecution(ctx, x, work.key.execution)
	if err != nil {
		return nil, false, err
	}
	if row == nil || row.summary.ProjectID != scope.summary.ProjectID || row.summary.AgentID != scope.summary.AgentID {
		return nil, false, fault(f.ConfirmationStale)
	}
	previous, err := loadRoundModelClaim(ctx, x, work.key.execution, work.key.next)
	if err != nil {
		return nil, false, err
	}
	if previous == nil && work.observe {
		return nil, false, ctx.Err()
	}
	if !work.observe {
		if previous != nil {
			// Even the same ProcessID cannot reconstruct another local owner.
			if work.owned == nil {
				return nil, false, fault(f.ResourceBusy)
			}
			if previous.fields != work.owned.fields || previous.fields != candidate.fields {
				return nil, false, fault(f.ConfirmationStale)
			}
		} else {
			if work.owned != nil {
				return nil, false, fault(f.ConfirmationStale)
			}
			if candidate.fields.ProcessID != o.process || candidate.fields.ExecutionID != work.key.execution || candidate.fields.RoundInputID != work.key.next {
				return nil, false, fault(f.Forbidden)
			}
			if row.summary.Status != c.Running || row.summary.CancelRequestedAt != nil {
				return nil, false, fault(f.ConfirmationStale)
			}
			project, err := o.projects.RequirePreparingProjectInTx(ctx, tx, row.summary.ProjectID)
			if err != nil {
				return nil, false, portError(err)
			}
			if project.ID != row.summary.ProjectID {
				return nil, false, fault(f.Forbidden)
			}
		}
	}
	// This is the existing private same-Tx reader, not the public wrapper. It
	// revalidates 68, Snapshot and complete projected bytes against original 72.
	request, found, err := o.history.requestInTx(ctx, tx, row, work.key.next, mc.CallID{}, locks, false)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, fault(f.NotFound)
	}
	var originalAt time.Time
	if err = x.QueryRow(ctx, `SELECT created_at FROM agenteam_execution.round_model_requests WHERE execution_id=$1 AND round_input_id=$2`, work.key.execution.String(), work.key.next.String()).Scan(&originalAt); err != nil {
		return nil, false, unavailable(err)
	}
	requestAt, err := f.NewInstant(originalAt)
	if err != nil {
		return nil, false, unavailable(nil)
	}
	actual := previous
	if actual == nil {
		actual = candidate
	}
	if !actual.matchesRequest(request, requestAt) {
		return nil, false, fault(f.ConfirmationStale)
	}
	if err = o.requireWork(ctx, work); err != nil {
		return nil, false, err
	}
	if previous == nil {
		// Recheck the original held guard after the potentially long history
		// loan and before this write. This still does not pin its lifetime.
		if err = o.guard.RequireCurrentProcessInTx(ctx, tx, o.process); err != nil {
			return nil, false, portError(err)
		}
		if err = insertRoundModelClaim(ctx, x, *actual); err != nil {
			return nil, false, err
		}
	}
	copy := *actual
	return &copy, true, ctx.Err()
}

func loadRoundModelClaim(ctx context.Context, x postgres.SQLExecutor, e i.ExecutionID, next c.RoundID) (*roundModelClaimRecord, error) {
	var claim, project, agent, execution, round, snapshot, call, process, roundDigest, snapshotDigest, projectionDigest, inputDigest, requestDigest string
	var version int64
	var at time.Time
	err := x.QueryRow(ctx, `SELECT id::text,project_id::text,agent_id::text,execution_id::text,round_input_id::text,snapshot_id::text,call_id::text,process_id::text,schema_version,round_input_digest,snapshot_digest,projection_digest,input_digest,request_digest,created_at FROM agenteam_execution.round_model_claims WHERE execution_id=$1 AND round_input_id=$2`, e.String(), next.String()).Scan(&claim, &project, &agent, &execution, &round, &snapshot, &call, &process, &version, &roundDigest, &snapshotDigest, &projectionDigest, &inputDigest, &requestDigest, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	id, err := f.ParseID[c.RoundModelClaim](claim)
	if err != nil {
		return nil, unavailable(nil)
	}
	p, err := f.ParseID[i.Project](project)
	if err != nil {
		return nil, unavailable(nil)
	}
	a, err := f.ParseID[i.Agent](agent)
	if err != nil {
		return nil, unavailable(nil)
	}
	s, err := f.ParseID[c.Snapshot](snapshot)
	if err != nil {
		return nil, unavailable(nil)
	}
	logical, err := f.ParseID[mc.Call](call)
	if err != nil {
		return nil, unavailable(nil)
	}
	proc, err := f.ParseID[oc.Process](process)
	if err != nil {
		return nil, unavailable(nil)
	}
	created, err := f.NewInstant(at)
	if err != nil {
		return nil, unavailable(nil)
	}
	r := roundModelClaimRecord{c.RoundModelClaimFields{SchemaVersion: f.Version(version), ClaimID: id, ProjectID: p, AgentID: a, ExecutionID: e, RoundInputID: next, RoundInputDigest: f.Digest(roundDigest), SnapshotID: s, SnapshotDigest: f.Digest(snapshotDigest), ProjectionDigest: f.Digest(projectionDigest), CallID: logical, ProcessID: proc, InputDigest: f.Digest(inputDigest), RequestDigest: f.Digest(requestDigest), CreatedAt: created}}
	if execution != e.String() || round != next.String() || r.Validate() != nil {
		return nil, unavailable(nil)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &r, nil
}

func insertRoundModelClaim(ctx context.Context, x postgres.SQLExecutor, r roundModelClaimRecord) error {
	if r.Validate() != nil {
		return invalid()
	}
	v := r.fields
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.round_model_claims(id,project_id,agent_id,execution_id,round_input_id,snapshot_id,call_id,process_id,schema_version,round_input_digest,snapshot_digest,projection_digest,input_digest,request_digest,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, v.ClaimID.String(), v.ProjectID.String(), v.AgentID.String(), v.ExecutionID.String(), v.RoundInputID.String(), v.SnapshotID.String(), v.CallID.String(), v.ProcessID.String(), int64(v.SchemaVersion), string(v.RoundInputDigest), string(v.SnapshotDigest), string(v.ProjectionDigest), string(v.InputDigest), string(v.RequestDigest), v.CreatedAt.Time())
	if err != nil || tag.RowsAffected() != 1 {
		return unavailable(err)
	}
	return ctx.Err()
}
