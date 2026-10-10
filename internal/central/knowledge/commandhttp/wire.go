package commandhttp

import (
	"context"
	"encoding/json"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

const responseLimit = 5 << 20

func invalidInput() error  { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func badProjection() error { return f.NewFault(f.DependencyUnavailable, f.NotStarted) }
func encodeValue(ctx context.Context, v any) ([]byte, error) {
	if expired(ctx) {
		return nil, context.DeadlineExceeded
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > responseLimit {
		return nil, badProjection()
	}
	if expired(ctx) {
		return nil, context.DeadlineExceeded
	}
	return raw, nil
}

type creatorDTO struct {
	Kind      id.ActorKind `json:"kind"`
	User      string       `json:"user_id,omitempty"`
	Project   string       `json:"project_id,omitempty"`
	Agent     string       `json:"agent_id,omitempty"`
	Execution string       `json:"execution_id,omitempty"`
}
type documentDTO struct {
	ID       kc.DocumentID     `json:"id"`
	Project  id.ProjectID      `json:"project_id"`
	Parent   *kc.DocumentID    `json:"parent_document_id"`
	Title    string            `json:"title"`
	Version  f.Version         `json:"content_version"`
	Source   kc.SourceKind     `json:"source_kind"`
	Media    string            `json:"media_type"`
	Status   kc.DocumentStatus `json:"status"`
	Indexing kc.IndexingStatus `json:"indexing_status"`
	Creator  creatorDTO        `json:"created_by"`
	Created  f.Instant         `json:"created_at"`
	Updated  f.Instant         `json:"updated_at"`
}

func projectDocument(project id.ProjectID, doc kc.DocumentRef) (documentDTO, error) {
	if doc.Validate() != nil || doc.ProjectID != project || doc.Status != kc.Active {
		return documentDTO{}, badProjection()
	}
	c := doc.CreatedBy.Details()
	w := creatorDTO{Kind: c.Kind}
	switch c.Kind {
	case id.Human:
		w.User = c.UserID.String()
	case id.AgentRun:
		w.Project = c.ProjectID.String()
		w.Agent = c.AgentID.String()
		w.Execution = c.ExecutionID.String()
	default:
		return documentDTO{}, badProjection()
	}
	var parent *kc.DocumentID
	if doc.ParentDocumentID != nil {
		copy := *doc.ParentDocumentID
		parent = &copy
	}
	return documentDTO{doc.ID, doc.ProjectID, parent, doc.Title, doc.ContentVersion, doc.SourceKind, doc.MediaType, doc.Status, doc.IndexingStatus, w, doc.CreatedAt, doc.UpdatedAt}, nil
}
func versionAfter(version f.Version, expected *f.Version, changed bool) bool {
	if expected == nil {
		return false
	}
	if !changed {
		return version == *expected
	}
	return *expected < math.MaxInt64 && version == *expected+1
}
func sameParent(a, b *kc.DocumentID) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

type documentResult struct {
	Document documentDTO `json:"document"`
}
type moveResult struct {
	Document documentDTO `json:"document"`
	Changed  bool        `json:"changed"`
}

func renameValue(project id.ProjectID, in intent, out kc.DocumentRef) (documentResult, error) {
	doc, err := projectDocument(project, out)
	if err != nil || out.ID != in.target || out.Title != in.title || !(versionAfter(out.ContentVersion, in.meta.ExpectedVersion, false) || versionAfter(out.ContentVersion, in.meta.ExpectedVersion, true)) {
		return documentResult{}, badProjection()
	}
	return documentResult{doc}, nil
}
func moveValue(project id.ProjectID, in intent, out kc.MoveResult) (moveResult, error) {
	doc, err := projectDocument(project, out.Document)
	if err != nil || out.Validate() != nil || out.Document.ID != in.target || !sameParent(out.Document.ParentDocumentID, in.move.TargetParentID) || out.Changed == sameParent(in.move.ExpectedParentID, in.move.TargetParentID) {
		return moveResult{}, badProjection()
	}
	return moveResult{doc, out.Changed}, nil
}

type deleteResult struct {
	Root    kc.DocumentID   `json:"root_id"`
	IDs     []kc.DocumentID `json:"deleted_ids"`
	Pending bool            `json:"cleanup_pending"`
}

func deleteValue(root kc.DocumentID, out kc.DeleteResult) (deleteResult, error) {
	if out.Validate() != nil || out.Root != root {
		return deleteResult{}, badProjection()
	}
	return deleteResult{out.Root, append([]kc.DocumentID{}, out.DeletedIDs...), out.CleanupPending}, nil
}

type previewResult struct {
	Root         kc.DocumentID `json:"root_id"`
	Nodes        []documentDTO `json:"nodes"`
	Digest       f.Digest      `json:"scope_digest"`
	Confirmation string        `json:"confirmation_token"`
	Expires      f.Instant     `json:"expires_at"`
}

func previewValue(project id.ProjectID, root kc.DocumentID, out kc.DeletePreview) (previewResult, error) {
	if out.Validate() != nil || out.Root != root {
		return previewResult{}, badProjection()
	}
	result := previewResult{Root: root, Nodes: make([]documentDTO, len(out.Nodes)), Digest: out.ScopeDigest, Expires: out.ExpiresAt}
	for i, doc := range out.Nodes {
		var err error
		result.Nodes[i], err = projectDocument(project, doc)
		if err != nil {
			return previewResult{}, err
		}
	}
	// This is the sole explicit Human-response material projection. No token
	// or private object identity is carried by other DTOs or errors.
	result.Confirmation = out.Confirmation.ForHumanResponse()
	return result, nil
}

type documentReceipt struct {
	Command  kc.CommandName `json:"command"`
	Document documentDTO    `json:"document"`
	Changed  bool           `json:"changed"`
}
type deleteReceipt struct {
	Command kc.CommandName  `json:"command"`
	Root    kc.DocumentID   `json:"root_id"`
	Changed bool            `json:"changed"`
	IDs     []kc.DocumentID `json:"deleted_ids"`
	Pending bool            `json:"cleanup_pending"`
}
type lookupResult struct {
	State   kc.LookupState `json:"state"`
	Receipt any            `json:"receipt"`
}

func lookupValue(project id.ProjectID, in intent, out kc.CommandLookup) (lookupResult, error) {
	if out.Validate() != nil {
		return lookupResult{}, badProjection()
	}
	result := lookupResult{State: out.State}
	if out.State != kc.Committed {
		return result, nil
	}
	r := out.Receipt
	if r == nil || r.Command != in.name {
		return lookupResult{}, badProjection()
	}
	switch in.name {
	case kc.Update:
		if r.Document == nil || !versionAfter(r.Document.ContentVersion, in.meta.ExpectedVersion, r.Changed) {
			return lookupResult{}, badProjection()
		}
		v, err := renameValue(project, in, *r.Document)
		if err != nil {
			return lookupResult{}, err
		}
		result.Receipt = documentReceipt{r.Command, v.Document, r.Changed}
	case kc.Move:
		if r.Document == nil {
			return lookupResult{}, badProjection()
		}
		v, err := moveValue(project, in, kc.MoveResult{Document: *r.Document, Changed: r.Changed})
		if err != nil {
			return lookupResult{}, err
		}
		result.Receipt = documentReceipt{r.Command, v.Document, v.Changed}
	case kc.DeleteSubtree:
		if r.RootID == nil {
			return lookupResult{}, badProjection()
		}
		v, err := deleteValue(in.target, kc.DeleteResult{Root: *r.RootID, DeletedIDs: r.DeletedIDs, CleanupPending: r.CleanupPending})
		if err != nil {
			return lookupResult{}, err
		}
		result.Receipt = deleteReceipt{r.Command, v.Root, true, v.IDs, v.Pending}
	default:
		return lookupResult{}, badProjection()
	}
	return result, nil
}
