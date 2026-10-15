package knowledge

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

const indexCleanupNonSourcePredicate = ` AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.index_profiles p WHERE p.project_id=t.project_id AND p.source_operation_id=t.id)`

// Source operations are deleted only with their exact released profile in the
// same bounded transaction. Both complete preimages survive an Unknown result.
func captureIndexCleanupSources(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, batch *projectCleanupBatch) error {
	keys := make([]string, 0, len(batch.rows))
	profiles := make(map[string]string, len(batch.rows))
	for _, row := range batch.rows {
		var value struct {
			Source *string `json:"source_operation_id"`
		}
		if err := json.Unmarshal(row.Record, &value); err != nil {
			return unavailable(err)
		}
		if value.Source == nil {
			continue
		}
		parsed, err := f.ParseID[struct{}](*value.Source)
		if err != nil || parsed.String() != *value.Source || profiles[*value.Source] != "" {
			return fault(f.InvalidState)
		}
		profiles[*value.Source] = row.Key
		keys = append(keys, *value.Source)
	}
	if len(keys) == 0 {
		return nil
	}
	rows, err := readProjectCleanupRows(ctx, x, project, "model_operations", keys)
	if err != nil {
		return err
	}
	if len(rows) != len(keys) {
		return fault(f.InvalidState)
	}
	for _, row := range rows {
		var value struct {
			Profile    string  `json:"profile_id"`
			Generation string  `json:"generation_id"`
			Kind       string  `json:"kind"`
			Job        *string `json:"job_id"`
		}
		if json.Unmarshal(row.Record, &value) != nil || value.Profile != profiles[row.Key] || value.Generation != value.Profile || value.Kind != "profile" || value.Job != nil {
			return fault(f.InvalidState)
		}
	}
	batch.sources = &projectCleanupBatch{stage: "model_operations", rows: rows}
	return nil
}
