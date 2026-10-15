package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type InvocationIntent string

const (
	ListSkills   InvocationIntent = "skill.list"
	ReadSkill    InvocationIntent = "skill.read"
	AssignSkill  InvocationIntent = "skill.assign"
	PrepareSkill InvocationIntent = "skill.prepare"
)

// Invocation identifies the original admitted Tool call. It is not authority:
// the Runtime must retain its private live witness and original input loan.
type Invocation struct {
	Actor        i.Actor
	ProjectID    i.ProjectID
	AgentID      i.AgentID
	ExecutionID  i.ExecutionID
	OperationID  string
	AttemptID    string
	ToolCallID   string
	ToolID       i.ToolID
	SpecRevision f.Version
	Intent       InvocationIntent
}

func (v Invocation) Validate() error {
	if v.Actor.Validate() != nil || v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil || v.ExecutionID.Validate() != nil || v.ToolID.Validate() != nil || v.SpecRevision.Validate() != nil {
		return invalid()
	}
	a := v.Actor.Details()
	if a.Kind != i.AgentRun || a.ProjectID != v.ProjectID.String() || a.AgentID != v.AgentID.String() || a.ExecutionID != v.ExecutionID.String() {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	for _, value := range []string{v.OperationID, v.AttemptID, v.ToolCallID} {
		if _, err := f.ParseID[struct{}](value); err != nil {
			return invalid()
		}
	}
	switch v.Intent {
	case ListSkills, ReadSkill, AssignSkill, PrepareSkill:
		return nil
	default:
		return invalid()
	}
}

type InvocationPlan interface{ RequiredLocks() []f.LockRequest }

// The callback borrows the exact protected arguments and immutable bindings
// effective for this Model round. It runs once in the same Store's live Tx
// under the complete precollected locks and current Execution/Tool authority.
// Skill independently checks current assignment identity and publication.
// No package I/O, late locks or asynchronous callback is allowed here.
type InvocationAuthority interface {
	DiscoverSkillInvocation(context.Context, Invocation) (InvocationPlan, error)
	WithSkillInvocationInTx(context.Context, f.Tx, Invocation, InvocationPlan, func(context.Context, []byte, []SkillBinding) error) error
	RequireSkillRuntimeBindingInTx(context.Context, f.Tx, any) error
}

func (Invocation) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_invocation") }
func (Invocation) LogValue() slog.Value         { return slog.StringValue("skill_invocation") }
func (Invocation) MarshalJSON() ([]byte, error) { return []byte(`"skill_invocation"`), nil }
