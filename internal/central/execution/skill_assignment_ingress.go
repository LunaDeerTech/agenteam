package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/jackc/pgx/v5"
)

// SkillAssignmentIngress uses only the caller's original transaction. Agent EX
// serializes the slot decision against Launch and all current state writers;
// no Execution lock is discovered or acquired after that union is held.
type SkillAssignmentIngress struct {
	slots   *DispatchObservation
	changes sc.AssignmentChangeAuthority
}

func NewSkillAssignmentIngress(store Store, changes sc.AssignmentChangeAuthority) (*SkillAssignmentIngress, error) {
	if nilPort(changes) {
		return nil, fault(f.DependencyUnbound)
	}
	slots, err := NewDispatchObservation(store)
	if err != nil {
		return nil, err
	}
	return &SkillAssignmentIngress{slots: slots, changes: changes}, nil
}

func skillAssignmentIngressLocks(change sc.RuntimeAssignmentChange) []f.LockRequest {
	schedule, _ := f.ProjectScheduleLock(change.ProjectID.String())
	skill, _ := f.AggregateLock(f.SkillAggregate, change.Binding.SkillID.String())
	return []f.LockRequest{projectLock(change.ProjectID), {Key: schedule, Mode: f.Exclusive},
		agentLock(change.AgentID, f.Exclusive), {Key: skill, Mode: f.Shared}}
}

const maxSkillAssignmentIngressBytes = 65536

const skillAssignmentIngressSelect = `SELECT project_id::text,agent_id::text,execution_id::text,outcome,change_data,change_digest,created_at FROM agenteam_execution.skill_assignment_ingress WHERE change_id=$1`
const skillAssignmentIngressInsert = `INSERT INTO agenteam_execution.skill_assignment_ingress(change_id,project_id,agent_id,execution_id,outcome,change_data,change_digest,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`

// RecordInTx must follow the actual Agent version/ref-owner advancement and
// Skill immutable change write. The authority checks that original change;
// this adapter does not infer ownership or permission from its public DTO.
// All errors return no receipt and must abort the caller's whole transaction.
func (s *SkillAssignmentIngress) RecordInTx(ctx context.Context, tx f.Tx, change sc.RuntimeAssignmentChange) (sc.AssignmentIngressReceipt, error) {
	if ctx == nil || change.Validate() != nil {
		return sc.AssignmentIngressReceipt{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return sc.AssignmentIngressReceipt{}, err
	}
	if s == nil || s.slots == nil || nilPort(s.changes) {
		return sc.AssignmentIngressReceipt{}, fault(f.DependencyUnbound)
	}
	x, err := s.slots.executor(ctx, tx, skillAssignmentIngressLocks(change))
	if err != nil {
		return sc.AssignmentIngressReceipt{}, err
	}
	if err = s.changes.RequireAssignmentChangeInTx(ctx, tx, change); err != nil {
		return sc.AssignmentIngressReceipt{}, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return sc.AssignmentIngressReceipt{}, err
	}
	raw, err := directTextJSON(change)
	if err != nil || len(raw) > maxSkillAssignmentIngressBytes {
		return sc.AssignmentIngressReceipt{}, invalid()
	}
	digest := c.TriggerInputDigest(raw)
	old, found, err := readSkillAssignmentIngress(ctx, x, change, raw, digest)
	if err != nil {
		return sc.AssignmentIngressReceipt{}, err
	}
	if found {
		// Check the original protection, even after the original Execution is
		// terminal. Never rediscover a slot or redirect this historical change.
		if old.Queued {
			err = s.changes.ProtectAssignmentChangeInTx(ctx, tx, change, *old.ExecutionID)
		}
		if err != nil {
			return sc.AssignmentIngressReceipt{}, portError(err)
		}
		if err = ctx.Err(); err != nil {
			return sc.AssignmentIngressReceipt{}, err
		}
		return old.Clone(), nil
	}
	slot, err := s.slots.AgentSlotInTx(ctx, tx, change.ProjectID, change.AgentID)
	if err != nil {
		return sc.AssignmentIngressReceipt{}, err
	}
	receipt := sc.AssignmentIngressReceipt{ChangeID: change.ID, Queued: slot.Occupied, ExecutionID: slot.ExecutionID}
	if receipt.Validate() != nil {
		return sc.AssignmentIngressReceipt{}, unavailable(nil)
	}
	outcome := "no_active_execution"
	var execution *string
	if receipt.Queued {
		if err = s.changes.ProtectAssignmentChangeInTx(ctx, tx, change, *receipt.ExecutionID); err != nil {
			return sc.AssignmentIngressReceipt{}, portError(err)
		}
		value := receipt.ExecutionID.String()
		execution, outcome = &value, "queued"
	}
	if err = ctx.Err(); err != nil {
		return sc.AssignmentIngressReceipt{}, err
	}
	tag, err := x.Exec(ctx, skillAssignmentIngressInsert, change.ID.String(), change.ProjectID.String(), change.AgentID.String(), execution, outcome, raw, string(digest), change.RecordedAt.Time())
	// Observe cancellation only after the actual SQL/port call has returned.
	if ctx.Err() != nil {
		return sc.AssignmentIngressReceipt{}, ctx.Err()
	}
	if err != nil || tag.RowsAffected() != 1 {
		return sc.AssignmentIngressReceipt{}, unavailable(err)
	}
	return receipt.Clone(), nil
}

func readSkillAssignmentIngress(ctx context.Context, x postgres.SQLExecutor, change sc.RuntimeAssignmentChange, raw []byte, digest f.Digest) (sc.AssignmentIngressReceipt, bool, error) {
	var project, agent, outcome, savedDigest string
	var execution *string
	var saved []byte
	var at time.Time
	err := x.QueryRow(ctx, skillAssignmentIngressSelect, change.ID.String()).Scan(&project, &agent, &execution, &outcome, &saved, &savedDigest, &at)
	if ctx.Err() != nil {
		return sc.AssignmentIngressReceipt{}, false, ctx.Err()
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sc.AssignmentIngressReceipt{}, false, nil
	}
	if err != nil {
		return sc.AssignmentIngressReceipt{}, false, unavailable(err)
	}
	if project != change.ProjectID.String() || agent != change.AgentID.String() ||
		!at.Equal(change.RecordedAt.Time()) || savedDigest != string(digest) || !bytes.Equal(raw, saved) {
		return sc.AssignmentIngressReceipt{}, false, fault(f.ConfirmationStale)
	}
	receipt := sc.AssignmentIngressReceipt{ChangeID: change.ID, Queued: outcome == "queued"}
	if outcome != "queued" && outcome != "no_active_execution" {
		return sc.AssignmentIngressReceipt{}, false, unavailable(nil)
	}
	if execution != nil {
		id, parseErr := f.ParseID[i.Execution](*execution)
		if parseErr != nil {
			return sc.AssignmentIngressReceipt{}, false, unavailable(nil)
		}
		receipt.ExecutionID = &id
	}
	if receipt.Validate() != nil {
		return sc.AssignmentIngressReceipt{}, false, unavailable(nil)
	}
	return receipt, true, nil
}

func (*SkillAssignmentIngress) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_skill_assignment_ingress")
}
func (*SkillAssignmentIngress) LogValue() slog.Value {
	return slog.StringValue("execution_skill_assignment_ingress")
}
func (*SkillAssignmentIngress) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_skill_assignment_ingress"`), nil
}

var _ c.SkillAssignmentIngress = (*SkillAssignmentIngress)(nil)
