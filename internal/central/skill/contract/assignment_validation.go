package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// AssignmentValidity is a current configuration fact, not an Execution or
// package-read capability. Missing, foreign or damaged identities are errors.
type AssignmentValidity string

const (
	AssignmentValid    AssignmentValidity = "valid"
	AssignmentRemoved  AssignmentValidity = "removed"
	AssignmentDisabled AssignmentValidity = "disabled"
	AssignmentDeleted  AssignmentValidity = "deleted"
)

func (v AssignmentValidity) Validate() error {
	switch v {
	case AssignmentValid, AssignmentRemoved, AssignmentDisabled, AssignmentDeleted:
		return nil
	default:
		return invalid()
	}
}

// RoundAssignmentValidation reads only Skill-owned canonical facts in the
// caller's same-Store live transaction. It never begins a transaction, acquires
// locks, creates protection or consumes pending ingress. The caller owns the
// Execution current-authority witness and all publication/commit decisions.
//
// Every method requires Project SH, ProjectSchedule EX and Agent EX. Validate
// additionally requires the exact Skill SH; RequirePending also requires that
// Skill SH and Execution EX. These are subsets of the caller's preacquired
// complete union; immutable Lookup does not call this current validator.
type RoundAssignmentValidation interface {
	ReadAssignmentSequenceInTx(context.Context, f.Tx, i.ProjectID, i.AgentID) (f.Version, error)
	RequirePendingAssignmentInTx(context.Context, f.Tx, i.ExecutionID, RuntimeAssignmentChange) error
	ValidateAssignmentInTx(context.Context, f.Tx, i.ProjectID, i.AgentID, AssignmentID, SkillID, RevisionID, f.Revision) (AssignmentValidity, error)
}
