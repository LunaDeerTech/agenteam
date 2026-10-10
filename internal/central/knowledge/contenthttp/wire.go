package contenthttp

import (
	"context"
	"encoding/json"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

const maxRepresentationBytes = 7 << 20

func invalidInput() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func badProjection() error { return f.NewFault(f.DependencyUnavailable, f.NotStarted) }

type creatorDTO struct {
	Kind id.ActorKind `json:"kind"`
	User string `json:"user_id,omitempty"`
	Project string `json:"project_id,omitempty"`
	Agent string `json:"agent_id,omitempty"`
	Execution string `json:"execution_id,omitempty"`
}
type documentDTO struct {
	ID kc.DocumentID `json:"id"`
	ProjectID id.ProjectID `json:"project_id"`
	ParentDocumentID *kc.DocumentID `json:"parent_document_id"`
	Title string `json:"title"`
	ContentVersion f.Version `json:"content_version"`
	SourceKind kc.SourceKind `json:"source_kind"`
	MediaType string `json:"media_type"`
	Status kc.DocumentStatus `json:"status"`
	IndexingStatus kc.IndexingStatus `json:"indexing_status"`
	CreatedBy creatorDTO `json:"created_by"`
	CreatedAt f.Instant `json:"created_at"`
	UpdatedAt f.Instant `json:"updated_at"`
}
type textDTO struct {
	Text string `json:"text"`
	NextByteOffset f.Progress `json:"next_byte_offset"`
	Truncated bool `json:"truncated"`
}
type contentDTO struct {
	Document documentDTO `json:"document"`
	Text *textDTO `json:"text,omitempty"`
	Unavailable *kc.ReadableUnavailable `json:"unavailable,omitempty"`
}

func encodeContent(ctx context.Context, project id.ProjectID, document kc.DocumentID, request kc.ReadRequest, content kc.DocumentContent) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if request.Validate() != nil || content.Validate() != nil || content.Document.ID != document || content.Document.ProjectID != project {
		return nil, badProjection()
	}
	d := content.Document
	c := d.CreatedBy.Details()
	creator := creatorDTO{Kind: c.Kind}
	switch c.Kind {
	case id.Human:
		creator.User = c.UserID.String()
	case id.AgentRun:
		creator.Project, creator.Agent, creator.Execution = c.ProjectID.String(), c.AgentID.String(), c.ExecutionID.String()
	default:
		return nil, badProjection()
	}
	out := contentDTO{Document: documentDTO{d.ID, d.ProjectID, d.ParentDocumentID, d.Title, d.ContentVersion, d.SourceKind, d.MediaType, d.Status, d.IndexingStatus, creator, d.CreatedAt, d.UpdatedAt}}
	switch {
	case content.Text != nil:
		t := content.Text
		n := int64(len(t.Text))
		if n > int64(request.MaxBytes) || int64(request.ByteOffset) > math.MaxInt64-n || t.NextByteOffset != request.ByteOffset+f.Progress(n) {
			return nil, badProjection()
		}
		out.Text = &textDTO{t.Text, t.NextByteOffset, t.Truncated}
	case content.Unavailable != nil && *content.Unavailable == kc.ReadableUnbound:
		x := *content.Unavailable
		out.Unavailable = &x
	default:
		// File references and future readable providers are not this HTTP API.
		return nil, badProjection()
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > maxRepresentationBytes {
		return nil, badProjection()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return raw, nil
}
