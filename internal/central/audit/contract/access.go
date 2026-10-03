package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type Filter struct {
	From, To                                                                    *foundation.Instant
	ActorKind                                                                   identity.ActorKind
	ActorID                                                                     string
	Action                                                                      Action
	Outcome                                                                     Outcome
	ResourceKind                                                                ResourceKind
	ResourceID, ToolID, ExecutionID, OperationID, ApprovalID, RunnerID, AgentID string
}

func (f Filter) Validate() error {
	if f.From != nil && f.From.Validate() != nil || f.To != nil && f.To.Validate() != nil || f.From != nil && f.To != nil && !f.From.Time().Before(f.To.Time()) {
		return invalid("filter")
	}
	if f.ActorKind != "" && f.ActorKind != identity.Human && f.ActorKind != identity.AgentRun && f.ActorKind != identity.Service || f.Action != "" && !f.Action.Valid() || f.Outcome != "" && !f.Outcome.Valid() || f.ResourceKind != "" && !f.ResourceKind.Valid() {
		return invalid("filter")
	}
	for _, id := range []string{f.ActorID, f.ResourceID, f.ToolID, f.ExecutionID, f.OperationID, f.ApprovalID, f.RunnerID, f.AgentID} {
		if id != "" && !validID(id) {
			return invalid("filter")
		}
	}
	if f.ActorKind == identity.Service && f.ActorID != "" {
		return invalid("filter")
	}
	return nil
}

// ProjectAuthority is adapted by D08. It must verify persisted owner/gate/cause
// facts in the supplied Tx, never a caller boolean or cached AccessGrant. The
// caller already owns the Project lock for Append; no lower lock is added here.
type ProjectAuthority interface {
	AuthorizeProject(context.Context, foundation.Tx, identity.Actor, identity.ProjectID, identity.AccessIntent) (identity.AccessGrant, error)
	CheckAppendInTx(context.Context, foundation.Tx, Entry, AppendKey) error
	CheckServiceLookup(context.Context, identity.Actor, identity.Scope, AppendKey) error
	CheckCleanupInTx(context.Context, foundation.Tx, identity.Actor, LifecycleCause, identity.ProjectID) error
}
type LifecycleAction string

const (
	Archive LifecycleAction = "archive"
	Delete  LifecycleAction = "delete"
)

type LifecycleCause struct{ data func() LifecycleDetails }
type LifecycleDetails struct {
	OperationID    foundation.ID[LifecycleOperation]
	Action         LifecycleAction
	ProjectVersion foundation.Version
}

func NewLifecycleCause(operation foundation.ID[LifecycleOperation], action LifecycleAction, version foundation.Version) (LifecycleCause, error) {
	if operation.Validate() != nil || action != Archive && action != Delete || version.Validate() != nil {
		return LifecycleCause{}, invalid("lifecycle_cause")
	}
	d := LifecycleDetails{operation, action, version}
	return LifecycleCause{data: func() LifecycleDetails { return d }}, nil
}
func (c LifecycleCause) Validate() error {
	if c.data == nil {
		return invalid("lifecycle_cause")
	}
	return nil
}
func (c LifecycleCause) Details() LifecycleDetails {
	if c.data == nil {
		return LifecycleDetails{}
	}
	return c.data()
}
func (c LifecycleCause) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "audit_lifecycle_cause")
}
func (c LifecycleCause) MarshalJSON() ([]byte, error) { return []byte(`"audit_lifecycle_cause"`), nil }
func (c *LifecycleCause) UnmarshalJSON([]byte) error  { return invalid("lifecycle_cause") }
func (c LifecycleCause) LogValue() slog.Value         { return slog.StringValue("audit_lifecycle_cause") }

type CleanupCheckpoint struct {
	ProjectID   identity.ProjectID                `json:"project_id"`
	OperationID foundation.ID[LifecycleOperation] `json:"operation_id"`
	LastID      *ID                               `json:"last_id,omitempty"`
}
type CleanupState string

const (
	CleanupPending   CleanupState = "pending"
	CleanupCompleted CleanupState = "completed"
)

type CleanupReport struct {
	State      CleanupState      `json:"state"`
	Checkpoint CleanupCheckpoint `json:"checkpoint"`
}
type Cleaner interface {
	CleanupProject(context.Context, identity.Actor, LifecycleCause, identity.ProjectID, *CleanupCheckpoint) (CleanupReport, error)
}
