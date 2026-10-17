package model

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func TestEmbeddingDimensionsServingDraftPreservesFrozenSpace(t *testing.T) {
	for _, mode := range []string{mc.EmbeddingFixedDimensions, mc.EmbeddingRequestDimensions} {
		t.Run(mode, func(t *testing.T) {
			r := platformEmbeddingTestRequest(t, mc.KnowledgeEmbedding)
			p, m := platformEmbeddingTestProfile()
			m.Input.Capabilities.Embedding = &mc.EmbeddingCapabilities{Dimensions: 3, DimensionsMode: mode}
			selectionVersion := f.Version(2)
			snapshot := resolutionSnapshotDTO{Format: 1, ID: mustID[mc.Snapshot](t), ProjectID: r.Consumer.ProjectID, Identity: mc.ModelIdentity{
				ProviderID: mustID[mc.Provider](t), ModelID: mustID[mc.Model](t), ProviderName: p.Input.Name, ModelName: m.Input.ProviderModelID, ProviderModelID: m.Input.ProviderModelID,
				Protocol: mc.OpenAIEmbeddings, Profile: mc.OpenAIEmbeddingsV1, ModelType: mc.EmbeddingModel, AdapterRevision: wire.OpenAIEmbeddingsDimensionsRevision,
			}, Endpoint: p.Input.BaseURL, Parameters: m.Input.Parameters, RequestOverwrite: m.Input.RequestOverwrite, HeaderOverwrite: map[string]string{}, Capabilities: m.Input.Capabilities.Clone(), SelectionVersion: &selectionVersion}
			r.Source, r.Selection, r.ServingSnapshotID, r.ServingGenerationID = mc.ServingSnapshotSource, nil, &snapshot.ID, mustID[struct{}](t).String()
			reads := 0
			store := &projectScopeStore{row: func(q string, args ...any) postgres.Row {
				reads++
				if q != `SELECT snapshot_data,snapshot_digest,provider_version,model_version FROM agenteam_model.snapshots WHERE id=$1 AND project_id=$2` || len(args) != 2 || args[0] != snapshot.ID.String() || args[1] != snapshot.ProjectID.String() {
					t.Fatal("serving resolution read something other than the exact frozen snapshot")
				}
				raw, err := encoded(snapshot)
				if err != nil {
					t.Fatal(err)
				}
				return scanFunc(func(out ...any) error {
					*out[0].(*[]byte), *out[1].(*f.Digest) = raw, hash(raw)
					*out[2].(*f.Version), *out[3].(*f.Version) = 3, 4
					return nil
				})
			}}
			got, err := servingResolutionDraft(context.Background(), store, r)
			if err != nil || got.ProviderVersion != 3 || got.ModelVersion != 4 || got.Snapshot.Capabilities.Embedding == nil || *got.Snapshot.Capabilities.Embedding != *snapshot.Capabilities.Embedding || got.Snapshot.Identity.AdapterRevision != wire.OpenAIEmbeddingsDimensionsRevision || reads != 1 {
				t.Fatal("valid v3 frozen vector space rejected or changed", err)
			}
			snapshot.Identity.AdapterRevision = "unverified-embedding-revision"
			_, err = servingResolutionDraft(context.Background(), store, r)
			requireCode(t, err, f.CapabilityUnsupported)
			if reads != 2 {
				t.Fatal("unsupported revision retried or read live selection")
			}
		})
	}
}
