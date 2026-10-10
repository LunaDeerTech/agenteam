package skillhttp

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// The one bounded P2 Metadata, including worst-case JSON escaping, fits here.
// This is not a pagination limit for a future external Skill catalogue.
const maxRepresentationBytes = 64 << 10

func invalidInput() error  { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func badProjection() error { return f.NewFault(f.DependencyUnavailable, f.NotStarted) }

type metadataDTO struct {
	ID              sc.SkillID   `json:"id"`
	ProjectID       id.ProjectID `json:"project_id"`
	Name            string       `json:"name"`
	NormalizedName  string       `json:"normalized_name"`
	Description     string       `json:"description"`
	Protected       bool         `json:"protected"`
	CurrentRevision f.Revision   `json:"current_revision"`
	Version         f.Version    `json:"version"`
}

func projectMetadata(project id.ProjectID, item sc.Metadata) (metadataDTO, error) {
	if project.Validate() != nil || item.Validate() != nil || item.ProjectID != project {
		return metadataDTO{}, badProjection()
	}
	return metadataDTO{item.ID, item.ProjectID, item.Name, item.NormalizedName, item.Description, item.Protected, item.CurrentRevision, item.Version}, nil
}
func encode(ctx context.Context, value any) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > maxRepresentationBytes {
		return nil, badProjection()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
func encodeDirectory(ctx context.Context, project id.ProjectID, items []sc.Metadata) ([]byte, error) {
	// A missing/unpublished initializer is an error from the real service. Do
	// not replace it with an empty catalogue or silently truncate new entries.
	if len(items) != 1 {
		return nil, badProjection()
	}
	item, err := projectMetadata(project, items[0])
	if err != nil {
		return nil, err
	}
	return encode(ctx, struct {
		Items []metadataDTO `json:"items"`
	}{[]metadataDTO{item}})
}
func encodeDetail(ctx context.Context, project id.ProjectID, target sc.SkillID, item sc.Metadata) ([]byte, error) {
	if target.Validate() != nil || item.ID != target {
		return nil, badProjection()
	}
	dto, err := projectMetadata(project, item)
	if err != nil {
		return nil, err
	}
	return encode(ctx, dto)
}
