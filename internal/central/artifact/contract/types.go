// Package contract defines Artifact business identities and bounded inputs.
// It contains no bucket, storage key, signed URL, credential or SDK type.
package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

const MaxInlineBytes = 1 << 20
const DefaultPreviewBytes = 8 << 10
const MaxPreviewBytes = 64 << 10

type ArtifactEntity struct{}
type File struct{}
type CallAttempt struct{}
type ArtifactID = foundation.ID[ArtifactEntity]
type FileID = foundation.ID[File]
type CallAttemptID = foundation.ID[CallAttempt]

type Kind string

const (
	Generated  Kind = "generated"
	UserUpload Kind = "user_upload"
)

func (k Kind) Valid() bool { return k == Generated || k == UserUpload }

type ArtifactRef struct {
	ProjectID  identity.ProjectID `json:"project_id"`
	ArtifactID ArtifactID         `json:"artifact_id"`
	FileID     FileID             `json:"file_id"`
}

func (r ArtifactRef) Validate() error {
	if r.ProjectID.Validate() != nil || r.ArtifactID.Validate() != nil || r.FileID.Validate() != nil {
		return invalid()
	}
	return nil
}
func (r ArtifactRef) BusinessFile() (oc.BusinessFileRef, error) {
	if r.Validate() != nil {
		return oc.BusinessFileRef{}, invalid()
	}
	return oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ArtifactFile, ProjectID: r.ProjectID, ArtifactID: r.ArtifactID.String(), FileID: r.FileID.String()})
}

type Display struct{ Name, Description string }

func (d Display) Validate() error {
	if oc.ValidateFilename(d.Name) != nil || len(d.Description) > 4<<10 || !utf8.ValidString(d.Description) || strings.ContainsRune(d.Description, 0) {
		return invalid()
	}
	return nil
}
func ValidateInline(value string) error {
	if len(value) > MaxInlineBytes {
		return foundation.NewFault(foundation.PayloadTooLarge, foundation.NotStarted)
	}
	if !utf8.ValidString(value) {
		return invalid()
	}
	return nil
}

// Invocation comes from trusted Tool/HTTP composition, not a model's JSON
// arguments. Construction validates shape; the service still checks current
// Session/Agent/Execution/Project and operation binding through its authority.
type Invocation struct{ data func() InvocationDetails }
type InvocationDetails struct {
	Actor       identity.Actor
	ProjectID   identity.ProjectID
	ExecutionID string
	OperationID string
	ToolID      string
	ToolCallID  string
	AttemptID   CallAttemptID
}

func NewInvocation(d InvocationDetails) (Invocation, error) {
	if d.Actor.Validate() != nil || d.ProjectID.Validate() != nil || d.AttemptID.Validate() != nil {
		return Invocation{}, invalid()
	}
	a := d.Actor.Details()
	if a.Kind != identity.Human && a.Kind != identity.AgentRun || a.Kind == identity.AgentRun && (a.ProjectID != d.ProjectID.String() || a.ExecutionID != d.ExecutionID) {
		return Invocation{}, invalid()
	}
	for _, id := range []string{d.ExecutionID, d.OperationID, d.ToolID, d.ToolCallID} {
		if id != "" {
			if _, err := foundation.ParseID[struct{}](id); err != nil {
				return Invocation{}, invalid()
			}
		}
	}
	if d.OperationID != "" && d.ExecutionID == "" || d.ToolCallID != "" && d.ToolID == "" {
		return Invocation{}, invalid()
	}
	return Invocation{func() InvocationDetails { return d }}, nil
}
func (c Invocation) Validate() error {
	if c.data == nil {
		return invalid()
	}
	return nil
}
func (c Invocation) Details() InvocationDetails {
	if c.data == nil {
		return InvocationDetails{}
	}
	return c.data()
}
func (c Invocation) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "artifact_invocation") }
func (c Invocation) MarshalJSON() ([]byte, error) { return []byte(`"artifact_invocation"`), nil }
func (*Invocation) UnmarshalJSON([]byte) error    { return invalid() }
func (c Invocation) LogValue() slog.Value         { return slog.StringValue("artifact_invocation") }

// Metadata is an authorized response projection. Private closure storage also
// keeps display text out of recursive fmt/slog traversal of internal values.
type Metadata struct{ data func() MetadataDetails }
type MetadataDetails struct {
	Reference        ArtifactRef        `json:"artifact_ref"`
	Kind             Kind               `json:"kind"`
	Name             string             `json:"name"`
	Description      string             `json:"description,omitempty"`
	Object           oc.ObjectMeta      `json:"object"`
	ExecutionID      string             `json:"source_execution_id,omitempty"`
	OperationID      string             `json:"source_operation_id,omitempty"`
	CreatedByUserID  string             `json:"created_by_user_id,omitempty"`
	CreatedByAgentID string             `json:"created_by_agent_id,omitempty"`
	CreatedAt        foundation.Instant `json:"created_at"`
	Version          foundation.Version `json:"version"`
}

func NewMetadata(d MetadataDetails) (Metadata, error) {
	if d.Reference.Validate() != nil || !d.Kind.Valid() || (Display{d.Name, d.Description}).Validate() != nil || d.Object.Validate() != nil || d.Object.State != oc.Available || d.Object.Scope.Details().ProjectID != d.Reference.ProjectID.String() || d.CreatedAt.Validate() != nil || d.Version.Validate() != nil {
		return Metadata{}, invalid()
	}
	for _, id := range []string{d.ExecutionID, d.OperationID, d.CreatedByUserID, d.CreatedByAgentID} {
		if id != "" {
			if _, e := foundation.ParseID[struct{}](id); e != nil {
				return Metadata{}, invalid()
			}
		}
	}
	if (d.CreatedByUserID == "") == (d.CreatedByAgentID == "") || d.CreatedByAgentID != "" && d.ExecutionID == "" || d.OperationID != "" && d.ExecutionID == "" {
		return Metadata{}, invalid()
	}
	return Metadata{func() MetadataDetails { return d }}, nil
}
func (m Metadata) Validate() error {
	if m.data == nil {
		return invalid()
	}
	return nil
}
func (m Metadata) Details() MetadataDetails {
	if m.data == nil {
		return MetadataDetails{}
	}
	return m.data()
}
func (m Metadata) MarshalJSON() ([]byte, error) {
	if m.data == nil {
		return nil, invalid()
	}
	return json.Marshal(m.data())
}
func (*Metadata) UnmarshalJSON([]byte) error  { return invalid() }
func (m Metadata) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "artifact_metadata") }
func (m Metadata) LogValue() slog.Value       { return slog.StringValue("artifact_metadata") }

type ListFilter struct {
	ExecutionID string
	Kind        Kind
	MediaType   string
	NameQuery   string
}

func (f ListFilter) Validate() error {
	if f.ExecutionID != "" {
		if _, e := foundation.ParseID[identity.Execution](f.ExecutionID); e != nil {
			return invalid()
		}
	}
	if f.Kind != "" && !f.Kind.Valid() || len(f.NameQuery) > 256 || !utf8.ValidString(f.NameQuery) || strings.ContainsRune(f.NameQuery, 0) {
		return invalid()
	}
	if f.MediaType != "" {
		m, e := oc.NormalizeMediaType(f.MediaType)
		if e != nil || m != f.MediaType {
			return invalid()
		}
	}
	return nil
}

type PreviewRequest struct {
	Offset int64
	Limit  int
}

func (p PreviewRequest) Normalize() (PreviewRequest, error) {
	if p.Limit == 0 {
		p.Limit = DefaultPreviewBytes
	}
	if p.Offset < 0 || p.Offset > oc.MaxObjectSize || p.Limit < 1 || p.Limit > MaxPreviewBytes {
		return PreviewRequest{}, invalid()
	}
	return p, nil
}

type Projection string

const (
	TextProjection  Projection = "text"
	ImageProjection Projection = "image_ref"
	FileProjection  Projection = "file_ref"
)

type ReadResult struct{ data func() ReadDetails }
type ReadDetails struct {
	Artifact    Metadata            `json:"artifact"`
	Projection  Projection          `json:"projection"`
	Text        string              `json:"text,omitempty"`
	Offset      foundation.Progress `json:"offset"`
	NextOffset  foundation.Progress `json:"next_offset"`
	Truncated   bool                `json:"truncated"`
	InvalidUTF8 bool                `json:"invalid_utf8,omitempty"`
}

func NewReadResult(d ReadDetails) (ReadResult, error) {
	if d.Artifact.Validate() != nil || d.Offset.Validate() != nil || d.NextOffset.Validate() != nil || d.NextOffset < d.Offset || d.NextOffset > d.Artifact.Details().Object.ByteSize {
		return ReadResult{}, invalid()
	}
	switch d.Projection {
	case TextProjection:
		if !utf8.ValidString(d.Text) || len(d.Text) > MaxPreviewBytes || int64(len(d.Text)) != int64(d.NextOffset-d.Offset) || d.InvalidUTF8 || d.Truncated != (d.NextOffset < d.Artifact.Details().Object.ByteSize) {
			return ReadResult{}, invalid()
		}
	case ImageProjection, FileProjection:
		if d.Text != "" || d.Offset != 0 || d.NextOffset != 0 || d.Truncated || d.Projection == ImageProjection && d.InvalidUTF8 {
			return ReadResult{}, invalid()
		}
	default:
		return ReadResult{}, invalid()
	}
	return ReadResult{func() ReadDetails { return d }}, nil
}
func (r ReadResult) Details() ReadDetails {
	if r.data == nil {
		return ReadDetails{}
	}
	return r.data()
}
func (r ReadResult) MarshalJSON() ([]byte, error) {
	if r.data == nil {
		return nil, invalid()
	}
	return json.Marshal(r.data())
}
func (*ReadResult) UnmarshalJSON([]byte) error  { return invalid() }
func (r ReadResult) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "artifact_read") }
func (r ReadResult) LogValue() slog.Value       { return slog.StringValue("artifact_read") }
func invalid() error                            { return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted) }
