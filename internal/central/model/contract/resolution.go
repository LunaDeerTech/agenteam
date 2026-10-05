package contract

import (
	"context"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type ResolutionSource string

const (
	CurrentSelectionSource ResolutionSource = "current_selection"
	ServingSnapshotSource  ResolutionSource = "serving_snapshot"
)

type ResolveRequest struct {
	Actor               id.Actor                `json:"-"`
	Consumer            Consumer                `json:"consumer"`
	Purpose             Purpose                 `json:"purpose"`
	Source              ResolutionSource        `json:"source"`
	ModelRef            *ModelID                `json:"model_ref,omitempty"`
	Selection           *SelectionRef           `json:"selection,omitempty"`
	ReasoningEffort     string                  `json:"reasoning_effort,omitempty"`
	ServingSnapshotID   *SnapshotID             `json:"serving_snapshot_id,omitempty"`
	ServingGenerationID string                  `json:"serving_generation_id,omitempty"`
	LeaseOwner          sc.CredentialLeaseOwner `json:"lease_owner"`
}

func (r ResolveRequest) Validate() error {
	if r.Consumer.Validate() != nil || !actorMatches(r.Actor, r.Consumer) || r.Purpose != r.Consumer.Purpose || !ownerMatches(r.LeaseOwner, r.Consumer, nil) || r.ReasoningEffort != "" && !safeToken(r.ReasoningEffort, 32) {
		return bad()
	}
	switch r.Source {
	case CurrentSelectionSource:
		if r.Selection == nil || r.ServingSnapshotID != nil || r.ServingGenerationID != "" || (SelectionRequest{r.Consumer, r.ModelRef, *r.Selection}).Validate() != nil {
			return bad()
		}
		if r.Purpose.ModelType() != ChatModel && r.ReasoningEffort != "" {
			return bad()
		}
	case ServingSnapshotSource:
		if r.ServingSnapshotID == nil || r.ServingSnapshotID.Validate() != nil || !uuid(r.ServingGenerationID) || r.ModelRef != nil || r.Selection != nil || r.ReasoningEffort != "" || r.Purpose != KnowledgeEmbedding && r.Purpose != MemoryEmbedding || r.LeaseOwner.Details().Kind != sc.ModelCallOwner {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
func (r ResolveRequest) Clone() ResolveRequest {
	r.Consumer = r.Consumer.Clone()
	r.ModelRef = copyPtr(r.ModelRef)
	r.Selection = copyPtr(r.Selection)
	if r.Selection != nil {
		x := r.Selection.Clone()
		r.Selection = &x
	}
	r.ServingSnapshotID = copyPtr(r.ServingSnapshotID)
	return r
}
func ResolveBinding(r ResolveRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	return binding(struct {
		Actor   id.ActorDetails
		Request ResolveRequest
	}{r.Actor.Details(), r})
}

type ResolutionPlanDetails struct {
	Binding, Mapping                                f.Digest
	Locks                                           []f.LockRequest
	SnapshotID                                      SnapshotID
	ProviderID                                      ProviderID
	ModelID                                         ModelID
	ProviderVersion, ModelVersion, SelectionVersion *f.Version
	CredentialRef                                   *sc.CredentialRef
	LeaseID                                         *sc.LeaseID
	Consumer                                        ConsumerDependencies
	Secret                                          *sc.UsageDependencies
}

func (d ResolutionPlanDetails) validate() error {
	if d.SnapshotID.Validate() != nil || d.ProviderID.Validate() != nil || d.ModelID.Validate() != nil || d.Consumer.Validate() != nil {
		return bad()
	}
	for _, v := range []*f.Version{d.ProviderVersion, d.ModelVersion, d.SelectionVersion} {
		if v != nil && v.Validate() != nil {
			return bad()
		}
	}
	if (d.CredentialRef == nil) != (d.LeaseID == nil) || (d.LeaseID == nil) != (d.Secret == nil) {
		return bad()
	}
	if d.CredentialRef != nil && (d.CredentialRef.Validate() != nil || d.LeaseID.Validate() != nil || d.Secret.Validate() != nil) {
		return bad()
	}
	return nil
}
func (d ResolutionPlanDetails) clone() ResolutionPlanDetails {
	d.Locks = append([]f.LockRequest(nil), d.Locks...)
	d.ProviderVersion = copyPtr(d.ProviderVersion)
	d.ModelVersion = copyPtr(d.ModelVersion)
	d.SelectionVersion = copyPtr(d.SelectionVersion)
	d.CredentialRef = copyPtr(d.CredentialRef)
	d.LeaseID = copyPtr(d.LeaseID)
	d.Secret = copyPtr(d.Secret)
	return d
}

type Resolver interface {
	SelectModel(context.Context, id.Actor, SelectionRequest) (SelectionResult, error)
	DiscoverResolve(context.Context, ResolveRequest) (ResolutionPlan, error)
	ResolveModelInTx(context.Context, f.Tx, ResolveRequest, ResolutionPlan) (ResolvedModel, error)
	ResolveModel(context.Context, ResolveRequest) (ResolvedModel, error)
}

func (ResolveRequest) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_resolve_request")) }
func (ResolveRequest) LogValue() slog.Value       { return slog.StringValue("model_resolve_request") }

func (ResolutionPlanDetails) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_resolution_plan_details"))
}
func (ResolutionPlanDetails) LogValue() slog.Value {
	return slog.StringValue("model_resolution_plan_details")
}
