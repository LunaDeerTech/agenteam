package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// Assignment is the identity of one grant, not a Skill revision or Agent.
// Removing and later assigning the same Skill requires a new identity.
type Assignment struct{}
type AssignmentID = f.ID[Assignment]

// InitialAssignment is the first grant stored atomically with a new Agent.
// It retains the stable Skill identity; execution revision binding is separate.
type InitialAssignment struct {
	ID        AssignmentID
	ProjectID id.ProjectID
	AgentID   id.AgentID
	SkillID   SkillID
	Sequence  f.Version
	CreatedAt f.Instant
}

func (a InitialAssignment) Validate() error {
	if a.ID.Validate() != nil || a.ProjectID.Validate() != nil || a.AgentID.Validate() != nil ||
		a.SkillID.Validate() != nil || a.Sequence != 1 || a.CreatedAt.Validate() != nil {
		return invalid()
	}
	return nil
}

// AgentInitializationReceipt is a safe initial-configuration result. It is
// tentative until the caller's final Tx commits and is never a runtime grant.
// The Agent consumer uses its own initialization port, not this receipt type.
type AgentInitializationReceipt struct {
	ProjectID          id.ProjectID
	AgentID            id.AgentID
	AssignmentSequence f.Version
	AddSkillsEnabled   bool
	Assignment         *InitialAssignment
	ObservedRevision   f.Revision
}

func (r AgentInitializationReceipt) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.AssignmentSequence != 1 ||
		r.ObservedRevision.Validate() != nil || r.AddSkillsEnabled != (r.Assignment != nil) {
		return invalid()
	}
	if r.Assignment != nil && (r.Assignment.Validate() != nil || r.Assignment.ProjectID != r.ProjectID ||
		r.Assignment.AgentID != r.AgentID || r.Assignment.Sequence != r.AssignmentSequence) {
		return invalid()
	}
	return nil
}

func (r AgentInitializationReceipt) Clone() AgentInitializationReceipt {
	if r.Assignment != nil {
		a := *r.Assignment
		r.Assignment = &a
	}
	return r
}
