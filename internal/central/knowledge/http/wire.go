package knowledgehttp

import (
	"context"
	"encoding/json"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

const maxRepresentationBytes = 5 << 20

func invalidInput() error  { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func badProjection() error { return f.NewFault(f.DependencyUnavailable, f.NotStarted) }

type creatorDTO struct {
	Kind      id.ActorKind `json:"kind"`
	User      string       `json:"user_id,omitempty"`
	Project   string       `json:"project_id,omitempty"`
	Agent     string       `json:"agent_id,omitempty"`
	Execution string       `json:"execution_id,omitempty"`
}
type documentDTO struct {
	ID               kc.DocumentID     `json:"id"`
	ProjectID        id.ProjectID      `json:"project_id"`
	ParentDocumentID *kc.DocumentID    `json:"parent_document_id"`
	Title            string            `json:"title"`
	ContentVersion   f.Version         `json:"content_version"`
	SourceKind       kc.SourceKind     `json:"source_kind"`
	MediaType        string            `json:"media_type"`
	Status           kc.DocumentStatus `json:"status"`
	IndexingStatus   kc.IndexingStatus `json:"indexing_status"`
	CreatedBy        creatorDTO        `json:"created_by"`
	CreatedAt        f.Instant         `json:"created_at"`
	UpdatedAt        f.Instant         `json:"updated_at"`
}

func projectDocument(project id.ProjectID, d kc.DocumentRef) (documentDTO, error) {
	if d.Validate() != nil || d.ProjectID != project || d.Status != kc.Active {
		return documentDTO{}, badProjection()
	}
	c := d.CreatedBy.Details()
	creator := creatorDTO{Kind: c.Kind}
	switch c.Kind {
	case id.Human:
		creator.User = c.UserID.String()
	case id.AgentRun:
		creator.Project, creator.Agent, creator.Execution = c.ProjectID.String(), c.AgentID.String(), c.ExecutionID.String()
	default:
		return documentDTO{}, badProjection()
	}
	return documentDTO{d.ID, d.ProjectID, d.ParentDocumentID, d.Title, d.ContentVersion, d.SourceKind, d.MediaType, d.Status, d.IndexingStatus, creator, d.CreatedAt, d.UpdatedAt}, nil
}

// Compose into a private bounded buffer. No response bytes are published until
// all rows/paths have passed validation. Individual JSON values are bounded
// DTOs; a huge ancestor chain never enters a single unbounded json.Marshal.
type representation struct {
	ctx context.Context
	raw []byte
	err error
}

func (b *representation) append(raw []byte) {
	if b.err != nil {
		return
	}
	if err := b.ctx.Err(); err != nil {
		b.err = err
		return
	}
	if len(raw) > maxRepresentationBytes-len(b.raw) {
		b.err = badProjection()
		return
	}
	b.raw = append(b.raw, raw...)
}
func (b *representation) text(s string) { b.append([]byte(s)) }
func (b *representation) value(v any) {
	if b.err != nil {
		return
	}
	raw, err := json.Marshal(v)
	if err != nil {
		b.err = badProjection()
		return
	}
	b.append(raw)
}
func (b *representation) result() ([]byte, error) {
	if b.err != nil {
		return nil, b.err
	}
	if err := b.ctx.Err(); err != nil {
		return nil, err
	}
	return b.raw, nil
}
func sameParent(a, b *kc.DocumentID) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func ordered(a, b kc.DocumentRef) bool {
	return a.Title < b.Title || a.Title == b.Title && a.ID.String() < b.ID.String()
}
func matches(d kc.DocumentRef, q kc.ListFilter) bool {
	return strings.Contains(d.Title, q.TitleQuery) && (q.SourceKind == nil || d.SourceKind == *q.SourceKind) && (q.MediaType == nil || d.MediaType == *q.MediaType) && (q.IndexingStatus == nil || d.IndexingStatus == *q.IndexingStatus)
}
func validPage(q f.PageRequest, n int, next string) bool {
	return q.Validate() == nil && n <= q.Limit && len(next) <= maxCursorBytes && safeQueryText(next) && (next == "" || n == q.Limit)
}
func (b *representation) endPage(next string) {
	b.text("]")
	if next != "" {
		b.text(`,"next_cursor":`)
		b.value(next)
	}
	b.text("}")
}

func encodePage(ctx context.Context, project id.ProjectID, kind resource, q query, page f.Page[kc.DocumentRef]) ([]byte, error) {
	if !validPage(q.page, len(page.Items), page.NextCursor) {
		return nil, badProjection()
	}
	b := representation{ctx: ctx}
	b.text(`{"items":[`)
	seen := map[kc.DocumentID]bool{}
	for i, d := range page.Items {
		v, err := projectDocument(project, d)
		if err != nil || seen[d.ID] || !matches(d, q.filter) || i > 0 && !ordered(page.Items[i-1], d) || kind == children && !sameParent(q.parent, d.ParentDocumentID) {
			return nil, badProjection()
		}
		seen[d.ID] = true
		if i > 0 {
			b.text(",")
		}
		b.value(v)
		if b.err != nil {
			return nil, b.err
		}
	}
	b.endPage(page.NextCursor)
	return b.result()
}
func encodeHead(ctx context.Context, project id.ProjectID, target kc.DocumentID, head kc.DocumentHead) ([]byte, error) {
	if head.Validate() != nil {
		return nil, badProjection()
	}
	b := representation{ctx: ctx}
	if head.Active != nil {
		v, err := projectDocument(project, *head.Active)
		if err != nil || v.ID != target {
			return nil, badProjection()
		}
		b.text(`{"active":`)
		b.value(v)
	} else {
		d := head.Deleted
		if d.ProjectID != project || d.ID != target {
			return nil, badProjection()
		}
		// Tombstone is already the formal four-field safe value, never a
		// metadata record retaining its old title, parent, object or creator.
		b.text(`{"deleted":`)
		b.value(*d)
	}
	b.text("}")
	return b.result()
}
func (b *representation) ancestorArray(project id.ProjectID, target kc.DocumentID, items []kc.DocumentRef) error {
	b.text("[")
	seen := map[kc.DocumentID]bool{target: true}
	var parent *kc.DocumentID
	for i, d := range items {
		v, err := projectDocument(project, d)
		if err != nil || seen[d.ID] || !sameParent(parent, d.ParentDocumentID) {
			return badProjection()
		}
		seen[d.ID] = true
		parent = &d.ID
		if i > 0 {
			b.text(",")
		}
		b.value(v)
		if b.err != nil {
			return b.err
		}
	}
	b.text("]")
	return b.err
}
func encodeAncestors(ctx context.Context, project id.ProjectID, target kc.DocumentID, items []kc.DocumentRef) ([]byte, error) {
	b := representation{ctx: ctx}
	b.text(`{"items":`)
	if err := b.ancestorArray(project, target, items); err != nil {
		return nil, err
	}
	b.text("}")
	return b.result()
}
func encodeHits(ctx context.Context, project id.ProjectID, q query, page f.Page[kc.TitleHit]) ([]byte, error) {
	if !validPage(q.page, len(page.Items), page.NextCursor) {
		return nil, badProjection()
	}
	b := representation{ctx: ctx}
	b.text(`{"items":[`)
	seen := map[kc.DocumentID]bool{}
	for i, hit := range page.Items {
		d := hit.Document
		// Check the endpoint without first walking an unbounded chain. The
		// per-node validation below shares the original context/byte budget.
		var last *kc.DocumentID
		if len(hit.Ancestors) != 0 {
			last = &hit.Ancestors[len(hit.Ancestors)-1].ID
		}
		if hit.Ancestors == nil || !sameParent(last, d.ParentDocumentID) || seen[d.ID] || !matches(d, q.filter) || i > 0 && !ordered(page.Items[i-1].Document, d) {
			return nil, badProjection()
		}
		v, err := projectDocument(project, d)
		if err != nil {
			return nil, err
		}
		seen[d.ID] = true
		if i > 0 {
			b.text(",")
		}
		b.text(`{"document":`)
		b.value(v)
		b.text(`,"ancestors":`)
		if err := b.ancestorArray(project, d.ID, hit.Ancestors); err != nil {
			return nil, err
		}
		b.text("}")
		if b.err != nil {
			return nil, b.err
		}
	}
	b.endPage(page.NextCursor)
	return b.result()
}
