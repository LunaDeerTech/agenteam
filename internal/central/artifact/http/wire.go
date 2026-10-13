package artifacthttp

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// 200 metadata rows, including 4KiB descriptions escaped at six JSON bytes per
// input byte, fit below 8MiB. A 64KiB text window plus metadata fits in 512KiB.
const maxListRepresentationBytes = 8 << 20
const maxReadRepresentationBytes = 512 << 10

func invalidInput() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }

// These encoders run after the Service successfully committed its Read Audit.
// A local projection failure must not erase that known transaction outcome.
func badProjection() error { return f.NewFault(f.DependencyUnavailable, f.Committed) }
func projectionContext(ctx context.Context) error {
	if ctx == nil {
		return badProjection()
	}
	if err := ctx.Err(); err != nil {
		return f.NewFault(f.DependencyUnavailable, f.Committed).WithCause(err)
	}
	return nil
}

type referenceDTO struct {
	ProjectID  id.ProjectID  `json:"project_id"`
	ArtifactID ac.ArtifactID `json:"artifact_id"`
	FileID     ac.FileID     `json:"file_id"`
}
type objectDTO struct {
	MediaType string     `json:"media_type"`
	ByteSize  f.Progress `json:"byte_size"`
	SHA256    f.Digest   `json:"sha256"`
}
type metadataDTO struct {
	Reference        referenceDTO `json:"artifact_ref"`
	Kind             ac.Kind      `json:"kind"`
	Name             string       `json:"name"`
	Description      string       `json:"description,omitempty"`
	Object           objectDTO    `json:"object"`
	ExecutionID      string       `json:"source_execution_id,omitempty"`
	OperationID      string       `json:"source_operation_id,omitempty"`
	CreatedByUserID  string       `json:"created_by_user_id,omitempty"`
	CreatedByAgentID string       `json:"created_by_agent_id,omitempty"`
	CreatedAt        f.Instant    `json:"created_at"`
	Version          f.Version    `json:"version"`
}
type listDTO struct {
	Items      []metadataDTO `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}
type readDTO struct {
	Artifact    metadataDTO   `json:"artifact"`
	Projection  ac.Projection `json:"projection"`
	Text        string        `json:"text,omitempty"`
	Offset      f.Progress    `json:"offset"`
	NextOffset  f.Progress    `json:"next_offset"`
	Truncated   bool          `json:"truncated"`
	InvalidUTF8 bool          `json:"invalid_utf8,omitempty"`
}

func projectMetadata(project id.ProjectID, metadata ac.Metadata) (metadataDTO, error) {
	d := metadata.Details()
	if project.Validate() != nil || metadata.Validate() != nil {
		return metadataDTO{}, badProjection()
	}
	if _, err := ac.NewMetadata(d); err != nil {
		return metadataDTO{}, badProjection()
	}
	scope := d.Object.Scope.Details()
	if d.Reference.ProjectID != project || scope.Kind != id.ProjectScope || scope.ProjectID != project.String() || scope.AgentID != "" {
		return metadataDTO{}, badProjection()
	}
	for _, value := range []string{d.ExecutionID, d.OperationID, d.CreatedByUserID, d.CreatedByAgentID} {
		if value == "" {
			continue
		}
		v, err := f.ParseID[struct{}](value)
		if err != nil || v.String() != value {
			return metadataDTO{}, badProjection()
		}
	}
	return metadataDTO{
		Reference: referenceDTO{d.Reference.ProjectID, d.Reference.ArtifactID, d.Reference.FileID}, Kind: d.Kind, Name: d.Name, Description: d.Description,
		Object: objectDTO{d.Object.MediaType, d.Object.ByteSize, d.Object.SHA256}, ExecutionID: d.ExecutionID, OperationID: d.OperationID,
		CreatedByUserID: d.CreatedByUserID, CreatedByAgentID: d.CreatedByAgentID, CreatedAt: d.CreatedAt, Version: d.Version,
	}, nil
}

func encodeRepresentation(ctx context.Context, value any, maximum int) ([]byte, error) {
	if err := projectionContext(ctx); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > maximum {
		return nil, badProjection()
	}
	if err := projectionContext(ctx); err != nil {
		return nil, err
	}
	return raw, nil
}

func encodeList(ctx context.Context, project id.ProjectID, filter ac.ListFilter, request f.PageRequest, items []ac.Metadata, cursor string) ([]byte, error) {
	if err := projectionContext(ctx); err != nil {
		return nil, err
	}
	if project.Validate() != nil || filter.Validate() != nil || request.Validate() != nil || len(items) > request.Limit || len(cursor) > maxCursorBytes || !utf8.ValidString(cursor) || strings.ContainsRune(cursor, 0) || cursor != "" && len(items) != request.Limit {
		return nil, badProjection()
	}
	out := listDTO{Items: make([]metadataDTO, 0, len(items)), NextCursor: cursor}
	seen := make(map[ac.ArtifactID]bool, len(items))
	var previous ac.MetadataDetails
	for index, item := range items {
		if err := projectionContext(ctx); err != nil {
			return nil, err
		}
		row, err := projectMetadata(project, item)
		if err != nil {
			return nil, err
		}
		d := item.Details()
		if seen[d.Reference.ArtifactID] || filter.ExecutionID != "" && d.ExecutionID != filter.ExecutionID || filter.Kind != "" && d.Kind != filter.Kind || filter.MediaType != "" && d.Object.MediaType != filter.MediaType || !strings.Contains(d.Name, filter.NameQuery) {
			return nil, badProjection()
		}
		if index > 0 && (d.CreatedAt.Time().After(previous.CreatedAt.Time()) || d.CreatedAt.Time().Equal(previous.CreatedAt.Time()) && d.Reference.ArtifactID.String() >= previous.Reference.ArtifactID.String()) {
			return nil, badProjection()
		}
		seen[d.Reference.ArtifactID] = true
		previous = d
		out.Items = append(out.Items, row)
	}
	return encodeRepresentation(ctx, out, maxListRepresentationBytes)
}

func encodeRead(ctx context.Context, reference ac.ArtifactRef, request ac.PreviewRequest, result ac.ReadResult) ([]byte, error) {
	if err := projectionContext(ctx); err != nil {
		return nil, err
	}
	if reference.Validate() != nil || request.Limit < 1 {
		return nil, badProjection()
	}
	if _, err := request.Normalize(); err != nil {
		return nil, badProjection()
	}
	d := result.Details()
	if _, err := ac.NewReadResult(d); err != nil {
		return nil, badProjection()
	}
	if d.Artifact.Details().Reference != reference || request.Offset > int64(d.Artifact.Details().Object.ByteSize) {
		return nil, badProjection()
	}
	metadata, err := projectMetadata(reference.ProjectID, d.Artifact)
	if err != nil {
		return nil, err
	}
	if d.Projection == ac.TextProjection && (d.Offset != f.Progress(request.Offset) || len(d.Text) > request.Limit) {
		return nil, badProjection()
	}
	return encodeRepresentation(ctx, readDTO{metadata, d.Projection, d.Text, d.Offset, d.NextOffset, d.Truncated, d.InvalidUTF8}, maxReadRepresentationBytes)
}
