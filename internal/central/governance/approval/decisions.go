package approval

import (
	"context"
	"encoding/json"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
	"time"
)

func commandLocks(a i.Actor, p i.ProjectID, id f.CommandIdentity) []f.LockRequest {
	k, _ := f.CommandLock(id)
	return append(humanLocks(a, p), f.LockRequest{Key: k, Mode: f.Exclusive})
}
func loadCommand(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, key f.IdempotencyKey, digest f.Digest) (*c.Receipt, error) {
	var saved string
	var raw []byte
	err := x.QueryRow(ctx, `SELECT semantic_digest,receipt FROM agenteam_governance.approval_commands WHERE project_id=$1 AND idempotency_key=$2`, p.String(), key.String()).Scan(&saved, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, portError(err)
	}
	if saved != string(digest) {
		return nil, fault(f.IdempotencyKeyReused)
	}
	var r c.Receipt
	if json.Unmarshal(raw, &r) != nil || r.CommandID.Validate() != nil || r.Approval.Validate() != nil {
		return nil, fault(f.InvalidState)
	}
	return &r, nil
}
func validDecision(a i.Actor, p i.ProjectID, id c.RequestID, meta f.CommandMeta, in c.Decision) error {
	if a.Validate() != nil || a.Details().Kind != i.Human || p.Validate() != nil || id.Validate() != nil || meta.Validate() != nil || in.Validate() != nil || meta.ExpectedVersion == nil || *meta.ExpectedVersion != in.ExpectedVersion {
		return fault(f.InvalidArgument)
	}
	return nil
}
func (s *Service) LookupDecision(ctx context.Context, a i.Actor, p i.ProjectID, id c.RequestID, meta f.CommandMeta, in c.Decision) (c.Lookup, error) {
	if s == nil || ctx == nil {
		return c.Lookup{}, fault(f.InvalidArgument)
	}
	if err := validDecision(a, p, id, meta, in); err != nil {
		return c.Lookup{}, err
	}
	identity, _ := decisionIdentity(p, meta.IdempotencyKey)
	digest, err := decisionDigest(a, p, id, in)
	if err != nil {
		return c.Lookup{}, portError(err)
	}
	cause, _ := f.NewCommandsCause(identity)
	out := c.Lookup{Status: "not_observed"}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, commandLocks(a, p, identity)); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, a, p, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		r, err := loadCommand(ctx, x, p, meta.IdempotencyKey, digest)
		if err != nil {
			return err
		}
		if r != nil {
			out.Status, out.Receipt = "committed", r
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return c.Lookup{}, err
	}
	return out, nil
}
func (s *Service) Decide(ctx context.Context, a i.Actor, p i.ProjectID, id c.RequestID, meta f.CommandMeta, in c.Decision) (c.Receipt, error) {
	if s == nil || ctx == nil {
		return c.Receipt{}, fault(f.InvalidArgument)
	}
	if err := validDecision(a, p, id, meta, in); err != nil {
		return c.Receipt{}, err
	}
	// Replays are authorized historical reads before checking the old version or
	// terminal Execution; they never resume or dispatch any operation.
	prior, err := s.LookupDecision(ctx, a, p, id, meta, in)
	if err != nil {
		return c.Receipt{}, err
	}
	if prior.Receipt != nil {
		return *prior.Receipt, nil
	}
	source, err := loadByID(ctx, s.store, id)
	if err != nil {
		return c.Receipt{}, err
	}
	if source == nil || source.value.ProjectID != p {
		return c.Receipt{}, fault(f.NotFound)
	}
	// A resolved request is immutable. A different command must report the
	// decision conflict even when the original Execution has already resumed
	// or finished and can no longer provide its pending ToolCall plan.
	if source.value.Status != c.Pending || source.value.Version != in.ExpectedVersion {
		return c.Receipt{}, fault(f.VersionConflict)
	}
	locks, err := s.operationLocks(ctx, source.operation)
	if err != nil {
		return c.Receipt{}, err
	}
	identity, _ := decisionIdentity(p, meta.IdempotencyKey)
	digest, err := decisionDigest(a, p, id, in)
	if err != nil {
		return c.Receipt{}, portError(err)
	}
	locks, err = oc.NormalizeLocks(append(locks, commandLocks(a, p, identity)...))
	if err != nil {
		return c.Receipt{}, err
	}
	at, _ := f.NewInstant(time.Now())
	if at.Time().Before(source.value.CreatedAt.Time()) {
		at = source.value.CreatedAt
	}
	user, _ := f.ParseID[i.User](a.Details().UserID)
	candidate := *source
	candidate.value.Status = c.Approved
	if in.Decision == "reject" {
		candidate.value.Status = c.Rejected
	}
	candidate.value.Version = 2
	candidate.value.ResolvedAt, candidate.value.ResolvedBy = &at, &user
	plan, err := s.prepareEvent(ctx, a, &candidate, locks)
	if err != nil {
		return c.Receipt{}, err
	}
	commandID, err := f.NewID[c.CommandIdentity]()
	if err != nil {
		return c.Receipt{}, portError(err)
	}
	cause, _ := f.NewCommandsCause(identity)
	var out c.Receipt
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, plan.locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, a, p, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		replay, err := loadCommand(ctx, x, p, meta.IdempotencyKey, digest)
		if err != nil {
			return err
		}
		if replay != nil {
			out = *replay
			return nil
		}
		if err = s.owner(ctx, tx, a, p, i.Mutate); err != nil {
			return err
		}
		current, err := loadByID(ctx, x, id)
		if err != nil {
			return err
		}
		if current == nil || current.value.ProjectID != p {
			return fault(f.NotFound)
		}
		if current.value.Status != c.Pending || current.value.Version != in.ExpectedVersion {
			return fault(f.VersionConflict)
		}
		if !sameBinding(current.operation.Binding, source.operation.Binding) || current.operation.OperationID != source.operation.OperationID || current.operation.Fingerprint != source.operation.Fingerprint {
			return fault(f.ConfirmationStale)
		}
		if err = s.deps.Executions.RequireApprovalExecutionInTx(ctx, tx, current.operation.Binding, id.String()); err != nil {
			return portError(err)
		}
		if err = s.deps.Operations.RequireUnattemptedApprovalOperationInTx(ctx, tx, current.operation); err != nil {
			return portError(err)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_governance.approval_requests SET status=$2,version=2,resolved_at=$3,resolved_by=$4,resolved_event_id=$5 WHERE id=$1 AND status='pending' AND version=$6`, id.String(), candidate.value.Status, at.Time(), user.String(), candidate.resolvedEvent, int64(in.ExpectedVersion))
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		if candidate.value.Status == c.Approved {
			_, err = x.Exec(ctx, `INSERT INTO agenteam_governance.one_time_approvals(request_id,project_id,agent_id,operation_id,tool_id,fingerprint,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id.String(), p.String(), current.value.AgentID.String(), current.value.OperationID.String(), current.value.ToolID.String(), string(current.value.Fingerprint), at.Time())
			if err != nil {
				return portError(err)
			}
		} else {
			if err = s.deps.Operations.RejectApprovalOperationInTx(ctx, tx, current.operation, id.String()); err != nil {
				return portError(err)
			}
		}
		if err = s.appendEvent(ctx, tx, plan); err != nil {
			return err
		}
		out = c.Receipt{CommandID: commandID, Approval: candidate.value}
		raw, err := json.Marshal(out)
		if err != nil {
			return portError(err)
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_governance.approval_commands(id,project_id,user_id,idempotency_key,semantic_digest,request_id,decision,expected_version,receipt,committed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, commandID.String(), p.String(), user.String(), meta.IdempotencyKey.String(), string(digest), id.String(), in.Decision, int64(in.ExpectedVersion), raw, at.Time())
		if err != nil {
			return portError(err)
		}
		return ctx.Err()
	})
	if err = commitError(result); err != nil {
		return c.Receipt{}, err
	}
	return out, nil
}
