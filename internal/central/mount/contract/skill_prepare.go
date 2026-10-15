package contract

import (
	"context"
	"fmt"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"io"
	"log/slog"
)

// SkillPrepareIntent describes the original committed builtin, never a grant.
// RequestID is the existing Attempt's backend request, not a fresh dispatch ID.
type SkillPrepareIntent struct {
	Actor               id.Actor
	ProjectID           id.ProjectID
	AgentID             id.AgentID
	ExecutionID         id.ExecutionID
	OperationID         tc.OperationID
	RequestID           p.ID
	MountID             id.MountID
	SkillID, RevisionID p.ID
	Revision            f.Revision
	PackageSHA256       f.Digest
}

func (v SkillPrepareIntent) Validate() error {
	if v.Actor.Validate() != nil || v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil || v.ExecutionID.Validate() != nil || v.OperationID.Validate() != nil || !v.RequestID.Valid() || v.MountID.Validate() != nil || !v.SkillID.Valid() || !v.RevisionID.Valid() || v.Revision.Validate() != nil || v.PackageSHA256.Validate() != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	a := v.Actor.Details()
	if a.Kind != id.AgentRun || a.ProjectID != v.ProjectID.String() || a.AgentID != v.AgentID.String() || a.ExecutionID != v.ExecutionID.String() {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (SkillPrepareIntent) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_prepare_intent")
}
func (SkillPrepareIntent) MarshalJSON() ([]byte, error) { return []byte(`"skill_prepare_intent"`), nil }
func (SkillPrepareIntent) LogValue() slog.Value         { return slog.StringValue("skill_prepare_intent") }

// A nil payload resolves current metadata. A nonnil payload claims the ticket
// and must equal the private owner's exact original Transfer grant and manifest.
// The callback borrows current Execution configuration synchronously once in the
// same Store/Tx; no network operation, late lock or detached callback is allowed.
type SkillPrepareAuthority interface {
	SkillPrepareLocks(context.Context, SkillPrepareIntent) ([]f.LockRequest, error)
	WithSkillPrepareInTx(context.Context, f.Tx, SkillPrepareIntent, *p.SkillPreparePayload, func(context.Context) error) error
}
