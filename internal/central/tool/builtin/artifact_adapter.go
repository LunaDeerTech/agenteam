package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ArtifactService interface {
	CreateFromContent(context.Context, ac.Invocation, f.CommandMeta, ac.Display, string, string) (ac.Metadata, error)
	LookupCreation(context.Context, ac.Invocation, f.CommandMeta, ac.Display, string, string) (ac.CreationLookupResult, error)
	ListArtifacts(context.Context, ac.Invocation, ac.ListFilter, f.PageRequest) (artifact.Page, error)
	ReadArtifact(context.Context, ac.Invocation, ac.ArtifactRef, ac.PreviewRequest) (ac.ReadResult, error)
}

type ArtifactAdapter struct{ service ArtifactService }

func NewArtifactAdapter(service ArtifactService) (*ArtifactAdapter, error) {
	if skillInstallNil(service) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &ArtifactAdapter{service: service}, nil
}

func (a *ArtifactAdapter) RequireBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if a == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	service, ok := a.service.(*artifact.Service)
	if !ok || service == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return service.RequireBuiltinBindingInTx(ctx, tx, store)
}

func ArtifactCreateKey(operation string) (f.IdempotencyKey, error) {
	if _, err := f.ParseID[struct{}](operation); err != nil {
		return "", artifactInvalid()
	}
	key := f.IdempotencyKey("tool.artifact.create:" + operation)
	if key.Validate() != nil {
		return "", artifactInvalid()
	}
	return key, nil
}

func (a *ArtifactAdapter) validate(ctx context.Context, v ac.Invocation) error {
	if ctx == nil {
		return artifactInvalid()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || skillInstallNil(a.service) {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if v.Validate() != nil {
		return artifactInvalid()
	}
	d := v.Details()
	if d.Actor.Details().Kind != i.AgentRun || d.ExecutionID == "" || d.OperationID == "" || d.ToolID == "" || d.ToolCallID == "" {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}

func artifactCommand(v ac.Invocation, command f.CommandMeta) error {
	key, err := ArtifactCreateKey(v.Details().OperationID)
	if err != nil || command.Validate() != nil || command.IdempotencyKey != key || command.ExpectedVersion != nil {
		return artifactInvalid()
	}
	return nil
}

// Execute performs one synchronous domain call. Runtime supplies the original
// persisted invocation and a live authority witness; the domain checks that
// witness again inside every authorization transaction, including Object I/O.
func (a *ArtifactAdapter) Execute(ctx context.Context, v ac.Invocation, command f.CommandMeta, name string, raw json.RawMessage) (json.RawMessage, error) {
	if err := a.validate(ctx, v); err != nil {
		return nil, err
	}
	args, err := parseArtifactArguments(ctx, name, raw)
	if err != nil {
		return nil, err
	}
	switch name {
	case ArtifactCreateName:
		if err = artifactCommand(v, command); err != nil {
			return nil, err
		}
		meta, err := a.service.CreateFromContent(ctx, v, command, args.display, args.media, args.content)
		if err != nil {
			return nil, err
		}
		if err = artifactCreatedBy(v, meta); err != nil {
			return nil, err
		}
		return json.Marshal(projectArtifactMetadata(meta))
	case ArtifactReadName:
		if args.ref.ProjectID != v.Details().ProjectID {
			return nil, f.NewFault(f.Forbidden, f.NotStarted)
		}
		result, err := a.service.ReadArtifact(ctx, v, args.ref, args.preview)
		if err != nil {
			return nil, err
		}
		if _, validationErr := ac.NewReadResult(result.Details()); validationErr != nil || result.Details().Artifact.Details().Reference != args.ref {
			return nil, f.NewFault(f.DependencyUnavailable, f.Unknown)
		}
		d := result.Details()
		return json.Marshal(struct {
			Artifact    artifactMetadata `json:"artifact"`
			Projection  ac.Projection    `json:"projection"`
			Text        string           `json:"text,omitempty"`
			Offset      f.Progress       `json:"offset"`
			NextOffset  f.Progress       `json:"next_offset"`
			Truncated   bool             `json:"truncated"`
			InvalidUTF8 bool             `json:"invalid_utf8,omitempty"`
		}{projectArtifactMetadata(d.Artifact), d.Projection, d.Text, d.Offset, d.NextOffset, d.Truncated, d.InvalidUTF8})
	case ArtifactListName:
		page, err := a.service.ListArtifacts(ctx, v, args.filter, args.page)
		if err != nil {
			return nil, err
		}
		items := make([]artifactMetadata, 0, len(page.Items()))
		for _, meta := range page.Items() {
			if meta.Validate() != nil || meta.Details().Reference.ProjectID != v.Details().ProjectID {
				return nil, f.NewFault(f.DependencyUnavailable, f.Unknown)
			}
			items = append(items, projectArtifactMetadata(meta))
		}
		return json.Marshal(struct {
			Items      []artifactMetadata `json:"items"`
			NextCursor string             `json:"next_cursor,omitempty"`
		}{items, page.NextCursor()})
	}
	return nil, artifactInvalid()
}

// LookupCreate never dispatches an upload. A pending/unknown original command
// must be reconciled by its owner; it cannot authorize another side effect.
func (a *ArtifactAdapter) LookupCreate(ctx context.Context, v ac.Invocation, command f.CommandMeta, raw json.RawMessage) (json.RawMessage, bool, error) {
	if err := a.validate(ctx, v); err != nil {
		return nil, false, err
	}
	if err := artifactCommand(v, command); err != nil {
		return nil, false, err
	}
	args, err := parseArtifactArguments(ctx, ArtifactCreateName, raw)
	if err != nil {
		return nil, false, err
	}
	result, err := a.service.LookupCreation(ctx, v, command, args.display, args.media, args.content)
	if err != nil {
		return nil, false, err
	}
	if result.Validate() != nil {
		return nil, false, f.NewFault(f.DependencyUnavailable, f.Unknown)
	}
	switch result.State {
	case ac.CreationNotObserved:
		return nil, false, nil
	case ac.CreationCommitted:
		if err = artifactCreatedBy(v, *result.Artifact); err != nil {
			return nil, false, err
		}
		raw, err := json.Marshal(projectArtifactMetadata(*result.Artifact))
		return raw, true, err
	case ac.CreationCancelled, ac.CreationCancelRequested:
		return nil, false, f.NewFault(f.ResourceDeleted, f.Committed)
	default:
		return nil, false, f.NewFault(f.ResourceBusy, f.Unknown)
	}
}

func artifactCreatedBy(v ac.Invocation, meta ac.Metadata) error {
	if meta.Validate() != nil {
		return f.NewFault(f.DependencyUnavailable, f.Unknown)
	}
	d, m := v.Details(), meta.Details()
	if m.Reference.ProjectID != d.ProjectID || m.Kind != ac.Generated || m.ExecutionID != d.ExecutionID || m.OperationID != d.OperationID || m.CreatedByAgentID != d.Actor.Details().AgentID {
		return f.NewFault(f.DependencyUnavailable, f.Unknown)
	}
	return nil
}

type artifactMetadata struct {
	Reference   ac.ArtifactRef `json:"artifact_ref"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	MediaType   string         `json:"media_type"`
	Size        f.Progress     `json:"size"`
	ExecutionID string         `json:"source_execution_id,omitempty"`
	OperationID string         `json:"source_operation_id,omitempty"`
	AgentID     string         `json:"created_by_agent_id,omitempty"`
}

func projectArtifactMetadata(meta ac.Metadata) artifactMetadata {
	d := meta.Details()
	return artifactMetadata{d.Reference, d.Name, d.Description, d.Object.MediaType, d.Object.ByteSize, d.ExecutionID, d.OperationID, d.CreatedByAgentID}
}
func (ArtifactAdapter) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "artifact_adapter") }
func (ArtifactAdapter) LogValue() slog.Value         { return slog.StringValue("artifact_adapter") }
func (ArtifactAdapter) MarshalJSON() ([]byte, error) { return []byte(`"artifact_adapter"`), nil }

var _ ArtifactService = (*artifact.Service)(nil)
