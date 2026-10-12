package execution

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type roundModelRequestRecord struct {
	request c.NextRoundModelRequest
	at      f.Instant
}

// This is a private same-Store/full-union seam, not a public committed DTO
// consumer. All historical rows are loaded by the original Execution readers
// in this transaction; public Project/Lookup wrappers are never nested here.
func (b *RoundModelRequestBinder) requestInTx(ctx context.Context, tx f.Tx, scope *executionRecord, next c.RoundID, call mc.CallID, locks []f.LockRequest, write bool) (c.NextRoundModelRequest, bool, error) {
	var zero c.NextRoundModelRequest
	if b == nil || ctx == nil || scope == nil || scope.summary.ID.Validate() != nil || next.Validate() != nil || write && call.Validate() != nil {
		return zero, false, invalid()
	}
	x, err := b.store.InTx(tx)
	if err != nil {
		return zero, false, portError(err)
	}
	// Derive mandatory locks from the actual discovered scope rather than
	// accepting a caller's reduced list as evidence of the complete union.
	want := roundInputLocks(scope.summary.ProjectID, scope.summary.AgentID, scope.summary.ID)
	if err = b.store.RequireHeldLocks(ctx, tx, want); err != nil {
		return zero, false, portError(err)
	}
	if err = b.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return zero, false, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return zero, false, err
	}
	row, err := loadExecution(ctx, x, scope.summary.ID)
	if err != nil {
		return zero, false, err
	}
	if row == nil || row.summary.ProjectID != scope.summary.ProjectID || row.summary.AgentID != scope.summary.AgentID {
		return zero, false, fault(f.ConfirmationStale)
	}
	previous, err := loadRoundModelRequest(ctx, x, row.summary.ID, next)
	if err != nil {
		return zero, false, err
	}
	if previous == nil && !write {
		return zero, false, ctx.Err()
	}
	// An immutable previous request is read before current writing gates.
	// Revocation or cancellation cannot turn its observation into new work.
	if previous == nil {
		if row.summary.Status != c.Running || row.summary.CancelRequestedAt != nil {
			return zero, false, fault(f.ConfirmationStale)
		}
		project, err := b.projects.RequirePreparingProjectInTx(ctx, tx, row.summary.ProjectID)
		if err != nil {
			return zero, false, portError(err)
		}
		if project.ID != row.summary.ProjectID {
			return zero, false, fault(f.Forbidden)
		}
		if err = ctx.Err(); err != nil {
			return zero, false, err
		}
	} else {
		call = previous.request.Fields().CallID
	}
	input, found, err := loadRoundInput(ctx, x, row.summary.ID)
	if err != nil {
		return zero, false, err
	}
	if !found {
		return zero, false, fault(f.NotFound)
	}
	fields := input.Fields()
	if fields.RoundID != next || fields.Initial.Request.ProjectID != row.summary.ProjectID || fields.Initial.Request.AgentID != row.summary.AgentID || row.summary.SnapshotID == nil || *row.summary.SnapshotID != fields.SnapshotID {
		return zero, false, fault(f.ConfirmationStale)
	}
	source, err := loadCommittedRoundProjection(ctx, x, input)
	if err != nil {
		return zero, false, err
	}
	var candidate c.NextRoundModelRequest
	err = lendRoundProjection(ctx, source, func(source c.RoundProjectionSource) error {
		var err error
		candidate, err = agentloop.BuildNextRoundModelRequest(ctx, source, call)
		return portError(err)
	})
	if err != nil {
		return zero, false, err
	}
	if candidate.Validate() != nil {
		return zero, false, fault(f.InvalidState)
	}
	if previous != nil {
		a, z := previous.request.CanonicalBytes(), candidate.CanonicalBytes()
		defer clear(a)
		defer clear(z)
		if !bytes.Equal(a, z) || previous.at.Time().Before(fields.CreatedAt.Time()) {
			return zero, false, fault(f.ConfirmationStale)
		}
		return previous.request, true, ctx.Err()
	}
	at, err := f.NewInstant(time.Now())
	if err != nil {
		return zero, false, err
	}
	if at.Time().Before(fields.CreatedAt.Time()) {
		at = fields.CreatedAt
	}
	if err = insertRoundModelRequest(ctx, x, candidate, at); err != nil {
		return zero, false, err
	}
	return candidate, true, ctx.Err()
}

func loadRoundModelRequest(ctx context.Context, x postgres.SQLExecutor, e i.ExecutionID, next c.RoundID) (*roundModelRequestRecord, error) {
	var project, agent, snapshot, inputDigest, roundDigest, snapshotDigest, projectionDigest, call, requestDigest string
	var version, size int64
	var raw []byte
	var at time.Time
	err := x.QueryRow(ctx, `SELECT project_id::text,agent_id::text,snapshot_id::text,round_input_digest,snapshot_digest,projection_digest,call_id::text,schema_version,input_digest,request,request_digest,request_bytes,created_at FROM agenteam_execution.round_model_requests WHERE execution_id=$1 AND round_input_id=$2`, e.String(), next.String()).Scan(&project, &agent, &snapshot, &roundDigest, &snapshotDigest, &projectionDigest, &call, &version, &inputDigest, &raw, &requestDigest, &size, &at)
	defer clear(raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	v, err := c.DecodeNextRoundModelRequest(ctx, raw)
	if err != nil {
		return nil, portError(err)
	}
	q := v.Fields()
	created, err := f.NewInstant(at)
	if err != nil || q.ExecutionID != e || q.RoundInputID != next || q.ProjectID.String() != project || q.AgentID.String() != agent || q.SnapshotID.String() != snapshot || string(q.RoundInputDigest) != roundDigest || string(q.SnapshotDigest) != snapshotDigest || string(q.ProjectionDigest) != projectionDigest || q.CallID.String() != call || int64(q.SchemaVersion) != version || string(v.InputDigest()) != inputDigest || string(v.Digest()) != requestDigest || size != int64(len(raw)) {
		return nil, unavailable(nil)
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &roundModelRequestRecord{request: v, at: created}, nil
}

func insertRoundModelRequest(ctx context.Context, x postgres.SQLExecutor, candidate c.NextRoundModelRequest, at f.Instant) error {
	if candidate.Validate() != nil || at.Validate() != nil {
		return invalid()
	}
	q := candidate.Fields()
	raw := candidate.CanonicalBytes()
	defer clear(raw)
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.round_model_requests(execution_id,round_input_id,project_id,agent_id,snapshot_id,round_input_digest,snapshot_digest,projection_digest,call_id,schema_version,input_digest,request,request_digest,request_bytes,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, q.ExecutionID.String(), q.RoundInputID.String(), q.ProjectID.String(), q.AgentID.String(), q.SnapshotID.String(), string(q.RoundInputDigest), string(q.SnapshotDigest), string(q.ProjectionDigest), q.CallID.String(), int64(q.SchemaVersion), string(candidate.InputDigest()), raw, string(candidate.Digest()), int64(len(raw)), at.Time())
	if err != nil || tag.RowsAffected() != 1 {
		return unavailable(err)
	}
	return ctx.Err()
}
