package contract

import (
	"context"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// ServingSnapshotOwner identifies one immutable embedding space or Summary generation. Its identity
// and snapshot are facts to authorize, never an authorization grant.
type ServingSnapshotOwner struct {
	Kind         ConsumerKind `json:"kind"`
	ProjectID    id.ProjectID `json:"project_id"`
	AgentID      *id.AgentID  `json:"agent_id,omitempty"`
	GenerationID string       `json:"generation_id"`
}

func (o ServingSnapshotOwner) Validate() error {
	if o.ProjectID.Validate() != nil || !uuid(o.GenerationID) {
		return bad()
	}
	switch o.Kind {
	case KnowledgeConsumer, MeetingConsumer:
		if o.AgentID != nil {
			return bad()
		}
	case MemoryConsumer:
		if o.AgentID == nil || o.AgentID.Validate() != nil {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}

type ServingSnapshotRetentionAction string

const (
	RetainServingSnapshot  ServingSnapshotRetentionAction = "retain"
	ReleaseServingSnapshot ServingSnapshotRetentionAction = "release"
)

type ServingSnapshotRetentionRequest struct {
	Actor        id.Actor                       `json:"-"`
	Owner        ServingSnapshotOwner           `json:"owner"`
	SnapshotID   SnapshotID                     `json:"snapshot_id"`
	OwnerVersion f.Version                      `json:"owner_version"`
	Action       ServingSnapshotRetentionAction `json:"action"`
}

func (r ServingSnapshotRetentionRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Owner.Validate() != nil || r.SnapshotID.Validate() != nil || r.OwnerVersion.Validate() != nil || r.Action != RetainServingSnapshot && r.Action != ReleaseServingSnapshot {
		return bad()
	}
	a := r.Actor.Details()
	if a.ProjectID != "" && a.ProjectID != r.Owner.ProjectID.String() || a.Kind == id.AgentRun && (r.Owner.AgentID == nil || a.AgentID != r.Owner.AgentID.String()) {
		return bad()
	}
	return nil
}
func (r ServingSnapshotRetentionRequest) Clone() ServingSnapshotRetentionRequest {
	r.Owner.AgentID = copyPtr(r.Owner.AgentID)
	return r
}
func ServingSnapshotRetentionBinding(r ServingSnapshotRetentionRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	return binding(struct {
		Actor   id.ActorDetails
		Request ServingSnapshotRetentionRequest
	}{r.Actor.Details(), r})
}

// Discovery may use a private pending generation intent. Validation must check
// current permissions, original owner/process and the exact durable generation
// in this transaction: building/active for retain, retired/failed for release.
// RetryPolicy is unused and must be nil. Observation uses the same original
// request/plan and may validate a terminal owner after its callback has ended;
// that does not authorize starting a new model call or generation.
type ServingSnapshotAuthority interface {
	DiscoverServingSnapshot(context.Context, ServingSnapshotRetentionRequest) (ConsumerDependencies, error)
	ValidateServingSnapshotInTx(context.Context, f.Tx, ServingSnapshotRetentionRequest, ConsumerDependencies) error
	// Observation revalidates the original private owner and current authority
	// against either side of its transition. A rolled-back release may still
	// have an active generation; that is observable, never permission to release.
	ValidateServingSnapshotObservationInTx(context.Context, f.Tx, ServingSnapshotRetentionRequest, ConsumerDependencies) error
}

type ServingSnapshotRetentionPlan interface {
	RequiredLocks() []f.LockRequest
}

// The caller acquires the union of these locks before changing business state.
// Apply joins that same transaction. A released generation cannot be rebound
// or retained again. After CommitUnknown, observe the original request/plan;
// observation performs no write, acquisition, provider request or retry.
type ServingSnapshotRetention interface {
	DiscoverServingSnapshotRetention(context.Context, ServingSnapshotRetentionRequest) (ServingSnapshotRetentionPlan, error)
	ApplyServingSnapshotRetentionInTx(context.Context, f.Tx, ServingSnapshotRetentionRequest, ServingSnapshotRetentionPlan) error
	ServingSnapshotRetentionAppliedInTx(context.Context, f.Tx, ServingSnapshotRetentionRequest, ServingSnapshotRetentionPlan) (bool, error)
}

func (ServingSnapshotRetentionRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_serving_snapshot_retention"))
}
func (ServingSnapshotRetentionRequest) LogValue() slog.Value {
	return slog.StringValue("model_serving_snapshot_retention")
}
