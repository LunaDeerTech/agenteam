package model

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// Serving snapshots are immutable Model facts. The business consumer's
// current authority supplies the generation-to-snapshot association; this
// reader cannot establish that authority from the existence of a row.
func servingResolutionDraft(ctx context.Context, x postgres.SQLExecutor, r mc.ResolveRequest) (resolutionDraft, error) {
	d := resolutionDraft{Format: 1}
	if r.Source != mc.ServingSnapshotSource || r.Validate() != nil {
		return d, fault(f.InvalidArgument)
	}
	var raw []byte
	var digest f.Digest
	err := x.QueryRow(ctx, `SELECT snapshot_data,snapshot_digest,provider_version,model_version FROM agenteam_model.snapshots WHERE id=$1 AND project_id=$2`, r.ServingSnapshotID.String(), r.Consumer.ProjectID.String()).Scan(&raw, &digest, &d.ProviderVersion, &d.ModelVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, fault(f.NotFound)
	}
	if err != nil {
		return d, unavailable(err)
	}
	if err = resolutionDecode(raw, 128<<10, &d.Snapshot); err != nil {
		return d, err
	}
	canonical, err := encoded(d.Snapshot)
	if err != nil || hash(canonical) != digest || d.Snapshot.ID != *r.ServingSnapshotID || d.Snapshot.ProjectID != r.Consumer.ProjectID {
		return d, unavailable(err)
	}
	snapshot, err := d.Snapshot.snapshot()
	if err != nil {
		return d, err
	}
	compatible := snapshot.Identity.ModelType == r.Purpose.ModelType()
	if snapshot.Identity.ModelType == mc.EmbeddingModel {
		compatible = compatible && (snapshot.Identity.AdapterRevision == wire.OpenAIEmbeddingsFloatRevision || snapshot.Identity.AdapterRevision == wire.OpenAIEmbeddingsFloatUsageRevision || snapshot.Identity.AdapterRevision == wire.OpenAIEmbeddingsDimensionsRevision)
	} else {
		compatible = compatible && snapshot.SelectionVersion != nil && (snapshot.Identity.AdapterRevision == wire.OpenAIChatTextRevision || snapshot.Identity.AdapterRevision == wire.OpenAIChatStructuredRevision)
	}
	if !compatible || d.ProviderVersion.Validate() != nil || d.ModelVersion.Validate() != nil {
		return d, fault(f.CapabilityUnsupported)
	}
	return d, nil
}
