package model

import (
	"context"
	"errors"
	"math"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	"github.com/jackc/pgx/v5"
)

// Only bounded association facts are durable. Messages, answers, materials,
// provider error bodies, Actor capabilities and local witnesses are excluded.
type runtimeBinding struct {
	Format         int `json:"format_version"`
	CallID         mc.CallID
	Consumer       mc.Consumer
	Input          mc.InputIdentity
	Initiator      id.ActorDetails
	SnapshotID     mc.SnapshotID
	SnapshotDigest f.Digest
	PreparationID  string
	LeaseID        sc.LeaseID
	Owner          sc.OwnerDetails
	Policy         mc.RetryPolicy
	Mode           wire.ResponseMode
	AgentTiming    *mc.AgentRetryTimingFields `json:"agent_retry_timing,omitempty"`
}

type runtimeRecord struct {
	binding     runtimeBinding
	digest      f.Digest
	value       uc.Invocation
	sequence    f.Sequence
	phase       string
	retired     bool
	version     f.Version
	cancelledAt *f.Instant
}

type runtimeAttemptDTO struct {
	Format   int `json:"format_version"`
	Sequence f.Sequence
	Value    uc.Invocation
}

func runtimeStableActor(actor id.Actor) id.ActorDetails {
	d := actor.Details()
	if d.Kind == id.Human {
		d.SessionID = ""
	}
	return d
}

func (b runtimeBinding) validate() error {
	if b.CallID.Validate() != nil || b.Consumer.Validate() != nil || b.Input.ValidateFor(b.Consumer) != nil || b.Input.SchemaVersion != 1 || b.SnapshotID.Validate() != nil || b.SnapshotDigest.Validate() != nil || b.LeaseID.Validate() != nil || b.Policy.ValidateFor(b.Consumer) != nil || b.Mode != wire.JSONResponse && b.Mode != wire.SSEResponse {
		return unavailable(nil)
	}
	if b.Format == 1 {
		if b.AgentTiming != nil || b.Owner.Kind != sc.ModelCallOwner || b.Owner.ID != b.CallID.String() || b.Policy.Class != mc.BoundedRetry || b.Policy.MaxAttempts == nil || *b.Policy.MaxAttempts != 1 || len(b.Policy.Categories) != 0 {
			return unavailable(nil)
		}
	} else if b.Format == 2 {
		if b.AgentTiming == nil || b.Policy.Class != mc.AgentRetry || b.Consumer.Kind != mc.AgentConsumer || b.Consumer.Purpose != mc.AgentGeneration || b.Consumer.ExecutionID == nil || b.Owner.Kind != sc.ExecutionOwner || b.Owner.ID != b.Consumer.ExecutionID.String() || b.Initiator.Kind != id.AgentRun {
			return unavailable(nil)
		}
		if _, err := mc.NewAgentRetryTiming(*b.AgentTiming); err != nil {
			return unavailable(err)
		}
	} else {
		return unavailable(nil)
	}
	if _, err := f.ParseID[struct{}](b.PreparationID); err != nil {
		return unavailable(err)
	}
	// This validates the stored association, never reconstructs an Actor.
	a := b.Initiator
	switch a.Kind {
	case id.AgentRun:
		if b.Format != 2 || b.Consumer.AgentID == nil || b.Consumer.ExecutionID == nil || a.ProjectID != b.Consumer.ProjectID.String() || a.AgentID != b.Consumer.AgentID.String() || a.ExecutionID != b.Consumer.ExecutionID.String() || a.UserID != "" || a.SessionID != "" || a.ServiceName != "" || a.CauseRef != "" {
			return unavailable(nil)
		}
	case id.Human:
		if _, err := f.ParseID[id.User](a.UserID); err != nil || a.SessionID != "" || a.ProjectID != "" || a.AgentID != "" || a.ExecutionID != "" || a.ServiceName != "" || a.CauseRef != "" {
			return unavailable(nil)
		}
	case id.Service:
		if a.UserID != "" || a.SessionID != "" || a.AgentID != "" || a.ExecutionID != "" || a.ProjectID != b.Consumer.ProjectID.String() || !id.ValidCauseRef(a.CauseRef) {
			return unavailable(nil)
		}
		switch a.ServiceName {
		case id.SecretService, id.SecretMaintenance, id.OutboundService, id.ObjectService, id.ObjectMaintenance, id.ProjectLifecycle, id.ProjectInitialization, id.OutboxDelivery, id.AccountBootstrap, id.AccountAuth, id.AccountMaintenance, id.AccountMail, id.ModelRuntime:
		default:
			return unavailable(nil)
		}
	default:
		return unavailable(nil)
	}
	return nil
}

func (r *runtimeRecord) validate() error {
	if r == nil || r.binding.validate() != nil || r.value.Validate() != nil || r.sequence.Validate() != nil || r.version.Validate() != nil {
		return unavailable(nil)
	}
	raw, err := encoded(r.binding)
	if err != nil || hash(raw) != r.digest || r.value.CallID != r.binding.CallID || !r.value.Consumer.Equal(r.binding.Consumer) || r.value.SnapshotID != r.binding.SnapshotID || (r.binding.Format == 1 && r.value.AttemptIndex != 1) || r.value.Fence != 1 {
		return unavailable(err)
	}
	if r.value.Final == nil {
		if r.cancelledAt != nil || r.retired || r.phase != "accepted" && r.phase != "running" || r.phase == "accepted" && r.value.Dispatch != uc.Reserved {
			return unavailable(nil)
		}
	} else if r.phase == "retry_wait" {
		if r.binding.Format != 2 || r.retired || r.value.Final.Status != uc.Failed || r.value.Final.Error == nil || r.cancelledAt != nil {
			return unavailable(nil)
		}
	} else if r.cancelledAt != nil {
		if r.binding.Format != 2 || r.phase != string(uc.Cancelled) || !r.retired || r.cancelledAt.Validate() != nil || r.cancelledAt.Time().Before(r.value.Final.FinishedAt.Time()) {
			return unavailable(nil)
		}
	} else if r.phase != string(r.value.Final.Status) {
		return unavailable(nil)
	}
	return nil
}

func runtimeCallCause(project id.ProjectID, call mc.CallID) (f.TransactionCause, error) {
	c, err := f.NewCommandIdentity("model.usage", []string{project.String()}, "call-ledger", f.IdempotencyKey(call.String()))
	if err != nil {
		return f.TransactionCause{}, portError(err)
	}
	return f.NewCommandsCause(c)
}

// The same call gate is shared with the existing Usage writer's command cause.
func runtimeCallLocks(project id.ProjectID, call mc.CallID, invocation mc.InvocationID) ([]f.LockRequest, error) {
	cause, err := runtimeCallCause(project, call)
	if err != nil {
		return nil, err
	}
	return resolutionUnion([]f.LockRequest{commandLock(cause.Details().Primary), projectLock(project.String()), recordLock(f.CommandRecordLock, "model-invocation:"+invocation.String())})
}

func loadRuntimeSnapshot(ctx context.Context, x postgres.SQLExecutor, request mc.ModelRequest) (*resolutionPreparation, error) {
	identity, err := resolutionUnitIdentity(request.Consumer, request.Consumer.Purpose, request.Model.LeaseOwner.Details())
	if err != nil {
		return nil, err
	}
	p, err := loadResolutionPreparation(ctx, x, identity.Canonical())
	if err != nil {
		return nil, err
	}
	if p == nil || p.Phase != "committed" {
		return nil, fault(f.InvalidState)
	}
	if err = loadCommittedResolution(ctx, x, p); err != nil {
		return nil, err
	}
	snapshot, err := p.Draft.Snapshot.snapshot()
	if err != nil {
		return nil, err
	}
	if request.Model.CredentialLease == nil || !p.Request.Consumer.Equal(request.Consumer) || p.Request.Owner != request.Model.LeaseOwner.Details() || p.Draft.LeaseID != request.Model.CredentialLease.LeaseID.String() || !resolutionEqual(snapshot, request.Model.Snapshot) {
		return nil, fault(f.Forbidden)
	}
	return p, nil
}

func loadRuntimeRecord(ctx context.Context, x postgres.SQLExecutor, call mc.CallID) (*runtimeRecord, error) {
	r := &runtimeRecord{}
	var binding, attempt []byte
	var project, snapshot, invocation, process, lease, preparation string
	var fence, ordinal int64
	var dispatch uc.Dispatch
	var started time.Time
	var finished *time.Time
	err := x.QueryRow(ctx, `SELECT c.project_id::text,c.snapshot_id::text,c.preparation_id::text,c.lease_id::text,c.invocation_id::text,c.process_id::text,c.fence,c.request_binding,c.request_data,c.phase,c.retired,c.version,c.accepted_at,c.finished_at,a.ordinal,a.dispatch,a.fact_data FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id AND a.call_id=c.id AND a.project_id=c.project_id AND a.process_id=c.process_id AND a.fence=c.fence WHERE c.id=$1`, call.String()).Scan(&project, &snapshot, &preparation, &lease, &invocation, &process, &fence, &r.digest, &binding, &r.phase, &r.retired, &r.version, &started, &finished, &ordinal, &dispatch, &attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	var a runtimeAttemptDTO
	if err = resolutionDecode(binding, 32<<10, &r.binding); err != nil {
		return nil, err
	}
	if err = resolutionDecode(attempt, 32<<10, &a); err != nil {
		return nil, err
	}
	r.value, r.sequence = a.Value, a.Sequence
	if r.phase == string(uc.Cancelled) && r.value.Final != nil && r.value.Final.Status != uc.Cancelled && finished != nil {
		at, e := f.NewInstant(*finished)
		if e != nil {
			return nil, unavailable(e)
		}
		r.cancelledAt = &at
	}

	if a.Format != 1 || r.validate() != nil || r.binding.CallID != call || r.binding.Consumer.ProjectID.String() != project || r.binding.SnapshotID.String() != snapshot || r.binding.PreparationID != preparation || r.binding.LeaseID.String() != lease || r.value.ID.String() != invocation || r.value.ProcessID.String() != process || int64(r.value.Fence) != fence || int64(r.value.AttemptIndex) != ordinal || r.value.Dispatch != dispatch || (r.value.AttemptIndex == 1 && !r.value.StartedAt.Time().Equal(started) || r.value.StartedAt.Time().Before(started)) || (r.value.Final == nil || r.phase == "retry_wait") != (finished == nil) || finished != nil && r.cancelledAt == nil && !r.value.Final.FinishedAt.Time().Equal(*finished) {
		return nil, unavailable(nil)
	}
	return r, nil
}

func insertRuntimeRecord(ctx context.Context, x postgres.SQLExecutor, r *runtimeRecord) error {
	if err := r.validate(); err != nil {
		return err
	}
	if r.phase != "accepted" || r.sequence != 1 || r.version != 1 || r.value.Dispatch != uc.Reserved || r.value.Final != nil || r.retired {
		return fault(f.InvalidState)
	}
	binding, err := encoded(r.binding)
	if err != nil {
		return err
	}
	attempt, err := encoded(runtimeAttemptDTO{1, r.sequence, r.value})
	if err != nil {
		return err
	}
	if len(binding) > 32<<10 || len(attempt) > 32<<10 {
		return fault(f.PayloadTooLarge)
	}
	v, b := r.value, r.binding
	_, err = x.Exec(ctx, `INSERT INTO agenteam_model.calls(id,project_id,snapshot_id,preparation_id,lease_id,invocation_id,process_id,fence,request_binding,request_data,phase,accepted_at,retired,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'accepted',$11,false,1)`, b.CallID.String(), b.Consumer.ProjectID.String(), b.SnapshotID.String(), b.PreparationID, b.LeaseID.String(), v.ID.String(), v.ProcessID.String(), int64(v.Fence), r.digest.String(), binding, v.StartedAt.Time())
	if err != nil {
		return unavailable(err)
	}
	_, err = x.Exec(ctx, `INSERT INTO agenteam_model.runtime_attempts(id,call_id,project_id,ordinal,process_id,fence,dispatch,sequence,fact_data) VALUES($1,$2,$3,1,$4,1,'reserved',1,$5)`, v.ID.String(), v.CallID.String(), v.Consumer.ProjectID.String(), v.ProcessID.String(), attempt)
	if err != nil {
		return unavailable(err)
	}
	return nil
}

func updateRuntimeRecord(ctx context.Context, x postgres.SQLExecutor, r *runtimeRecord, previous f.Version) error {
	if err := r.validate(); err != nil {
		return err
	}
	if previous <= 0 || previous == math.MaxInt64 || r.version != previous+1 {
		return fault(f.InvalidState)
	}
	attempt, err := encoded(runtimeAttemptDTO{1, r.sequence, r.value})
	if err != nil {
		return err
	}
	if len(attempt) > 32<<10 {
		return fault(f.PayloadTooLarge)
	}
	var finished any
	if r.value.Final != nil && r.phase != "retry_wait" {
		finished = r.value.Final.FinishedAt.Time()
	}
	if r.cancelledAt != nil {
		finished = r.cancelledAt.Time()
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_model.calls SET phase=$1,finished_at=$2,retired=$3,version=$4 WHERE id=$5 AND invocation_id=$6 AND process_id=$7 AND fence=$8 AND request_binding=$9 AND version=$10`, r.phase, finished, r.retired, int64(r.version), r.binding.CallID.String(), r.value.ID.String(), r.value.ProcessID.String(), int64(r.value.Fence), r.digest.String(), int64(previous))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	tag, err = x.Exec(ctx, `UPDATE agenteam_model.runtime_attempts SET dispatch=$1,sequence=$2,fact_data=$3 WHERE id=$4 AND call_id=$5 AND process_id=$6 AND fence=$7`, string(r.value.Dispatch), int64(r.sequence), attempt, r.value.ID.String(), r.binding.CallID.String(), r.value.ProcessID.String(), int64(r.value.Fence))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}
