package contract

import (
	"context"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// EmbeddingProfileRequest observes the system embedding selector for an active
// original domain operation. It is not a Model call, snapshot or lease request.
type EmbeddingProfileRequest struct {
	Actor    id.Actor `json:"-"`
	Consumer Consumer `json:"consumer"`
}

func (r EmbeddingProfileRequest) Validate() error {
	if r.Actor.Validate() != nil || r.Consumer.Validate() != nil {
		return bad()
	}
	a := r.Actor.Details()
	if a.Kind != id.Service || a.ProjectID != r.Consumer.ProjectID.String() || a.CauseRef != r.Consumer.OperationID {
		return bad()
	}
	switch r.Consumer.Kind {
	case KnowledgeConsumer:
		if r.Consumer.Purpose != KnowledgeEmbedding || a.ServiceName != id.KnowledgeRuntime {
			return bad()
		}
	case MemoryConsumer:
		if r.Consumer.Purpose != MemoryEmbedding || a.ServiceName != id.MemoryRuntime {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
func (r EmbeddingProfileRequest) Clone() EmbeddingProfileRequest {
	r.Consumer = r.Consumer.Clone()
	return r
}
func EmbeddingProfileBinding(r EmbeddingProfileRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	return binding(struct {
		Format   int
		Actor    id.ActorDetails
		Consumer Consumer
	}{1, r.Actor.Details(), r.Consumer})
}

// Stable configuration revisions and the Model-owned dimensions identify the
// desired embedding space. Credentials and endpoints stay in Model.
type EmbeddingProfile struct {
	SelectionVersion f.Version  `json:"selection_version"`
	ModelID          ModelID    `json:"model_id"`
	ModelVersion     f.Version  `json:"model_version"`
	ProviderID       ProviderID `json:"provider_id"`
	ProviderVersion  f.Version  `json:"provider_version"`
	AdapterRevision  string     `json:"adapter_revision"`
	Dimensions       int        `json:"dimensions"`
	DimensionsMode   string     `json:"dimensions_mode"`
}

func (p EmbeddingProfile) Validate() error {
	if (EmbeddingCapabilities{Dimensions: EmbeddingDimensions(p.Dimensions), DimensionsMode: p.DimensionsMode}).Validate() != nil {
		return bad()
	}
	if p.SelectionVersion.Validate() != nil || p.ModelID.Validate() != nil || p.ModelVersion.Validate() != nil || p.ProviderID.Validate() != nil || p.ProviderVersion.Validate() != nil || !safeToken(p.AdapterRevision, 128) {
		return bad()
	}
	return nil
}
func (p EmbeddingProfile) Digest() (f.Digest, error) {
	if p.Validate() != nil {
		return "", bad()
	}
	return binding(struct {
		Format  int
		Profile EmbeddingProfile
	}{1, p})
}

// The domain must verify its private still-active operation, exact namespace,
// same Store and current ProcessGuard/permissions in this transaction. DTOs
// alone never confer access. RetryPolicy is unused and must remain nil.
type EmbeddingProfileAuthority interface {
	DiscoverEmbeddingProfile(context.Context, EmbeddingProfileRequest) (ConsumerDependencies, error)
	RequireEmbeddingProfileInTx(context.Context, f.Tx, EmbeddingProfileRequest, ConsumerDependencies) error
}
type EmbeddingProfileReader interface {
	ReadEmbeddingProfile(context.Context, EmbeddingProfileRequest) (EmbeddingProfile, error)
}

func (EmbeddingProfileRequest) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_embedding_profile_request"))
}
func (EmbeddingProfileRequest) LogValue() slog.Value {
	return slog.StringValue("model_embedding_profile_request")
}
