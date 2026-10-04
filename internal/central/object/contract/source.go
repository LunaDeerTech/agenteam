package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type SourceKind string

const (
	UploadedObject SourceKind = "uploaded_object"
	ArtifactFile   SourceKind = "artifact_file"
	KnowledgeFile  SourceKind = "knowledge_file"
	ExecutionFile  SourceKind = "execution_file"
)

type BusinessFileRef struct{ data func() BusinessFileDetails }
type BusinessFileDetails struct {
	Kind                                                   SourceKind
	ProjectID                                              identity.ProjectID
	ArtifactID, FileID, DocumentID, ExecutionID, PayloadID string
	Revision                                               foundation.Version
	Receipt                                                UploadReceipt
}

func NewBusinessFileRef(d BusinessFileDetails) (BusinessFileRef, error) {
	if d.Kind == UploadedObject {
		if d.Receipt.Validate() != nil || d.ProjectID.Validate() == nil || d.ArtifactID != "" || d.FileID != "" || d.DocumentID != "" || d.ExecutionID != "" || d.PayloadID != "" || d.Revision != 0 {
			return BusinessFileRef{}, bad()
		}
	} else {
		if d.ProjectID.Validate() != nil || d.Receipt.Validate() == nil {
			return BusinessFileRef{}, bad()
		}
		switch d.Kind {
		case ArtifactFile:
			if !validID(d.ArtifactID) || !validID(d.FileID) || d.DocumentID != "" || d.ExecutionID != "" || d.PayloadID != "" || d.Revision != 0 {
				return BusinessFileRef{}, bad()
			}
		case KnowledgeFile:
			if !validID(d.DocumentID) || d.Revision.Validate() != nil || d.ArtifactID != "" || d.FileID != "" || d.ExecutionID != "" || d.PayloadID != "" {
				return BusinessFileRef{}, bad()
			}
		case ExecutionFile:
			if !validID(d.ExecutionID) || !validID(d.PayloadID) || d.ArtifactID != "" || d.FileID != "" || d.DocumentID != "" || d.Revision != 0 {
				return BusinessFileRef{}, bad()
			}
		default:
			return BusinessFileRef{}, bad()
		}
	}
	return BusinessFileRef{func() BusinessFileDetails { return d }}, nil
}
func (r BusinessFileRef) Validate() error {
	if r.data == nil {
		return bad()
	}
	return nil
}
func (r BusinessFileRef) Details() BusinessFileDetails {
	if r.data == nil {
		return BusinessFileDetails{}
	}
	return r.data()
}
func (r BusinessFileRef) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "business_file_ref") }
func (r BusinessFileRef) MarshalJSON() ([]byte, error) { return []byte(`"business_file_ref"`), nil }
func (*BusinessFileRef) UnmarshalJSON([]byte) error    { return bad() }
func (r BusinessFileRef) LogValue() slog.Value         { return slog.StringValue("business_file_ref") }

type ResolvedSource struct{ data func() ResolvedSourceDetails }
type ResolvedSourceDetails struct {
	Reference BusinessFileRef
	Owner     ObjectOwner
	Meta      ObjectMeta
	Revision  foundation.Version
}

func NewResolvedSource(d ResolvedSourceDetails) (ResolvedSource, error) {
	if d.Reference.Validate() != nil || d.Owner.Validate() != nil || d.Meta.Validate() != nil || d.Meta.State != Available || !d.Owner.Scope().Equal(d.Meta.Scope) || d.Revision.Validate() != nil {
		return ResolvedSource{}, bad()
	}
	r := d.Reference.Details()
	if r.Kind == UploadedObject {
		p := r.Receipt.Details()
		if p.ObjectID != d.Meta.ID || !p.Owner.Equal(d.Owner) {
			return ResolvedSource{}, bad()
		}
	} else if r.ProjectID.String() != d.Owner.Details().ProjectID || r.Kind == KnowledgeFile && r.Revision != d.Revision {
		return ResolvedSource{}, bad()
	}
	o := d.Owner.Details()
	if r.Kind == ArtifactFile && (o.Kind != Artifact || o.ID != r.ArtifactID) || r.Kind == KnowledgeFile && (o.Kind != Knowledge || o.ID != r.DocumentID) || r.Kind == ExecutionFile && (o.Kind != ExecutionPayload || o.ID != r.PayloadID) {
		return ResolvedSource{}, bad()
	}
	return ResolvedSource{func() ResolvedSourceDetails { return d }}, nil
}
func (s ResolvedSource) Validate() error {
	if s.data == nil {
		return bad()
	}
	return nil
}
func (s ResolvedSource) Details() ResolvedSourceDetails {
	if s.data == nil {
		return ResolvedSourceDetails{}
	}
	return s.data()
}
func (s ResolvedSource) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "resolved_object_source")
}
func (s ResolvedSource) MarshalJSON() ([]byte, error) { return []byte(`"resolved_object_source"`), nil }
func (*ResolvedSource) UnmarshalJSON([]byte) error    { return bad() }
func (s ResolvedSource) LogValue() slog.Value         { return slog.StringValue("resolved_object_source") }

// A real provider resolves its own business entity/version and checks current
// access. B02 consumes this port; unbound variants must fail explicitly. It is
// not a raw-object-ID lookup and cannot silently substitute the latest revision.
// ValidateInTx consumes the same complete outer access plan/token as object
// mutations; it rechecks the exact source and cannot discover or add locks.
type SourceResolver interface {
	Resolve(context.Context, identity.Actor, BusinessFileRef) (ResolvedSource, error)
	ValidateInTx(context.Context, foundation.Tx, identity.Actor, ResolvedSource, AccessLockPlan, LockedAccess) error
}
