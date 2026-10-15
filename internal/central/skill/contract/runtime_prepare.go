package contract

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// PreparationInvocation adds the exact committed Builtin backend request. It
// never creates a second Operation or Attempt, and is not an authorization.
type PreparationInvocation struct {
	Invocation Invocation
	RequestID  p.ID
}

func (v PreparationInvocation) Validate() error {
	if v.Invocation.Validate() != nil || v.Invocation.Intent != PrepareSkill || !v.RequestID.Valid() {
		return invalid()
	}
	return nil
}

type PrepareToolArguments struct {
	SkillID SkillID    `json:"skill_id"`
	MountID id.MountID `json:"mount_id"`
}

func DecodePrepareToolArguments(raw []byte) (PrepareToolArguments, error) {
	var out PrepareToolArguments
	if strictObject(raw, 8192, []string{"skill_id", "mount_id"}) != nil || json.Unmarshal(raw, &out) != nil || out.SkillID.Validate() != nil || out.MountID.Validate() != nil {
		return out, invalid()
	}
	return out, nil
}

// Current checks still borrow the original protected input/current round. The
// cleanup method accepts only the retained original backend's cleanup witness,
// same Store, current ProcessGuard and exact still-running durable Attempt.
// It grants no current Skill access, new transfer, Resolve or Runner dispatch.
type PreparationInvocationAuthority interface {
	InvocationAuthority
	WithSkillPreparationInTx(context.Context, f.Tx, PreparationInvocation, InvocationPlan, func(context.Context, []byte, []SkillBinding, f.Version) error) error
	RequireSkillPreparationRetirementInTx(context.Context, f.Tx, PreparationInvocation, InvocationPlan) (f.Version, error)
}
