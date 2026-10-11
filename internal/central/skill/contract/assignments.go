package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// AssignmentChange identifies one committed configuration mutation. It is
// separate from the grant identity: remove followed by assign creates a new
// AssignmentID, while replay keeps the original change and receipt.
type AssignmentChange struct{}
type AssignmentChangeID = f.ID[AssignmentChange]

// AgentSkillAssignment is a current configuration fact, never an Execution
// binding or authority to read a package. Its original sequence is immutable.
type AgentSkillAssignment struct {
	ID        AssignmentID `json:"id"`
	ProjectID i.ProjectID  `json:"project_id"`
	AgentID   i.AgentID    `json:"agent_id"`
	SkillID   SkillID      `json:"skill_id"`
	Enabled   bool         `json:"enabled"`
	Sequence  f.Version    `json:"assignment_sequence"`
	CreatedAt f.Instant    `json:"created_at"`
}

func (v AgentSkillAssignment) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil ||
		v.SkillID.Validate() != nil || v.Sequence.Validate() != nil || v.CreatedAt.Validate() != nil {
		return invalid()
	}
	return nil
}

// RuntimeAssignmentChange is emitted only for a new enabled grant. The Skill
// writer captures the actual immutable revision under the same Skill gate.
// The DTO cannot authorize ingress: its complete contents must match a real
// Skill-owned change in the caller's original transaction.
type RuntimeAssignmentChange struct {
	ID                 AssignmentChangeID `json:"change_id"`
	ProjectID          i.ProjectID        `json:"project_id"`
	AgentID            i.AgentID          `json:"agent_id"`
	AssignmentSequence f.Version          `json:"assignment_sequence"`
	AgentVersion       f.Version          `json:"agent_config_version"`
	Binding            SkillBinding       `json:"binding"`
	RecordedAt         f.Instant          `json:"recorded_at"`
}

func (v RuntimeAssignmentChange) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil ||
		v.AssignmentSequence < 2 || v.AgentVersion < 2 || v.Binding.Validate() != nil ||
		v.Binding.AssignmentSequence != v.AssignmentSequence || v.RecordedAt.Validate() != nil {
		return invalid()
	}
	return nil
}

// AssignmentIngressReceipt describes the original routing decision. It never
// means the change was applied to a RoundInput or that waiting was resumed.
// A zero receipt is invalid; no-active must be an explicit observed result.
type AssignmentIngressReceipt struct {
	ChangeID    AssignmentChangeID `json:"change_id"`
	Queued      bool               `json:"queued"`
	ExecutionID *i.ExecutionID     `json:"execution_id"`
}

func (v AssignmentIngressReceipt) Validate() error {
	if v.ChangeID.Validate() != nil || v.Queued != (v.ExecutionID != nil) ||
		v.ExecutionID != nil && v.ExecutionID.Validate() != nil {
		return invalid()
	}
	return nil
}

func (v AssignmentIngressReceipt) Clone() AssignmentIngressReceipt {
	if v.ExecutionID != nil {
		x := *v.ExecutionID
		v.ExecutionID = &x
	}
	return v
}

// AssignmentRuntimeSink belongs to Skill; Execution implements it at explicit
// composition without a Skill -> Execution package dependency. RecordInTx must
// use the original same-Store transaction and complete preheld Project SH,
// ProjectSchedule EX, Agent EX and Skill SH union. It acquires no late locks,
// opens no transaction and performs no external I/O. Active means exactly the
// unique created/preparing/running/waiting slot, including cancel-requested.
// Both queued and no-active decisions are immutable per change identity.
type AssignmentRuntimeSink interface {
	RecordInTx(context.Context, f.Tx, RuntimeAssignmentChange) (AssignmentIngressReceipt, error)
}

// AssignmentChangeAuthority is implemented only by the Skill owner. Require
// checks the actual immutable change and full captured metadata, not merely
// this DTO. Protect writes/checks the exact revision/package reference in the
// same transaction. The permanent revision -> Object reference stays owned by
// Skill; no Object I/O or new Execution authorization is created here.
type AssignmentChangeAuthority interface {
	RequireAssignmentChangeInTx(context.Context, f.Tx, RuntimeAssignmentChange) error
	ProtectAssignmentChangeInTx(context.Context, f.Tx, RuntimeAssignmentChange, i.ExecutionID) error
}

type AssignmentMutation struct {
	ProjectID          i.ProjectID               `json:"project_id"`
	AgentID            i.AgentID                 `json:"agent_id"`
	AgentVersion       f.Version                 `json:"agent_version"`
	AssignmentSequence f.Version                 `json:"assignment_sequence"`
	ChangeID           AssignmentChangeID        `json:"change_id"`
	Changed            bool                      `json:"changed"`
	Assignment         *AgentSkillAssignment     `json:"assignment"`
	Runtime            *AssignmentIngressReceipt `json:"runtime"`
}

func (v AssignmentMutation) Validate() error {
	if v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil || v.AgentVersion.Validate() != nil ||
		v.AssignmentSequence.Validate() != nil || v.ChangeID.Validate() != nil {
		return invalid()
	}
	if v.Assignment != nil && (v.Assignment.Validate() != nil || v.Assignment.ProjectID != v.ProjectID ||
		v.Assignment.AgentID != v.AgentID || v.Assignment.Sequence > v.AssignmentSequence) {
		return invalid()
	}
	if v.Runtime != nil && (v.Runtime.Validate() != nil || v.Runtime.ChangeID != v.ChangeID ||
		!v.Changed || v.Assignment == nil || !v.Assignment.Enabled) {
		return invalid()
	}
	return nil
}

func (v AssignmentMutation) Clone() AssignmentMutation {
	if v.Assignment != nil {
		a := *v.Assignment
		v.Assignment = &a
	}
	if v.Runtime != nil {
		r := v.Runtime.Clone()
		v.Runtime = &r
	}
	return v
}
