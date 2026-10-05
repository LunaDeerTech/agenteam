// Package contract defines Knowledge's pure types and rules, never authorization.
package contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"unicode"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type Document struct{}
type DocumentID = f.ID[Document]
type SourceKind string

const (
	Text SourceKind = "text"
	File SourceKind = "file"
)

type DocumentStatus string

const (
	Active  DocumentStatus = "active"
	Deleted DocumentStatus = "deleted"
)

type IndexingStatus string

const (
	IndexPending    IndexingStatus = "pending"
	IndexProcessing IndexingStatus = "processing"
	IndexReady      IndexingStatus = "ready"
	IndexFailed     IndexingStatus = "failed"
)

type ReadableUnavailable string

const (
	ReadableProcessing ReadableUnavailable = "processing"
	ReadableFailed     ReadableUnavailable = "failed"
	ReadableUnbound    ReadableUnavailable = "dependency_unbound"
)

const (
	Markdown  = "text/markdown"
	PlainText = "text/plain"
	PDF       = "application/pdf"
	DOCX      = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
)
const (
	MaxTitleRunes    = 512
	DefaultReadBytes = 64 << 10
	MaxReadBytes     = 1 << 20
)

func invalid() error       { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func fault(c f.Code) error { return f.NewFault(c, f.NotStarted) }
func oneOf[T ~string](v T, allowed ...T) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return invalid()
}
func enumDecode[T ~string](raw []byte, valid func(T) error) (T, error) {
	var s string
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || b[0] != '"' || !utf8.Valid(b) || json.Unmarshal(b, &s) != nil || valid(T(s)) != nil {
		return "", invalid()
	}
	return T(s), nil
}
func enumJSON[T ~string](v T, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(v))
}
func checked(v any, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
func decode[T any](raw []byte, required, optional, nullable []string) (T, error) {
	var z T
	if _, e := cursor.CanonicalJSON(raw); e != nil {
		return z, invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return z, invalid()
	}
	allowed := map[string]bool{}
	nulls := map[string]bool{}
	for _, k := range nullable {
		nulls[k] = true
	}
	for _, k := range required {
		if _, ok := fields[k]; !ok {
			return z, invalid()
		}
		allowed[k] = true
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k, v := range fields {
		if !allowed[k] || bytes.Equal(bytes.TrimSpace(v), []byte("null")) && !nulls[k] {
			return z, invalid()
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var v T
	if d.Decode(&v) != nil {
		return z, invalid()
	}
	return v, nil
}
func digest(domain string, value any) (f.Digest, error) {
	b, e := json.Marshal(value)
	if e != nil {
		return "", invalid()
	}
	b, e = cursor.CanonicalJSON(b)
	if e != nil {
		return "", invalid()
	}
	return hash(domain, b), nil
}
func hash(domain string, b []byte) f.Digest {
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write([]byte{0})
	h.Write(b)
	return f.Digest("sha256:" + hex.EncodeToString(h.Sum(nil)))
}
func rawSHA(b []byte) f.Digest {
	s := sha256.Sum256(b)
	return f.Digest("sha256:" + hex.EncodeToString(s[:]))
}
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
func samePtr[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
func validParent(p *DocumentID) bool { return p == nil || p.Validate() == nil }
func ValidateTitle(s string) error {
	if !utf8.ValidString(s) || len(s) == 0 || utf8.RuneCountInString(s) > MaxTitleRunes {
		return invalid()
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return invalid()
		}
	}
	return nil
}
func mediaKind(media string) (SourceKind, error) {
	switch media {
	case Markdown, PlainText:
		return Text, nil
	case PDF, DOCX:
		return File, nil
	}
	return "", fault(f.UnsupportedMediaType)
}

type CreatorDetails struct {
	Kind        id.ActorKind
	UserID      id.UserID
	ProjectID   id.ProjectID
	AgentID     id.AgentID
	ExecutionID id.ExecutionID
}
type CreatorRef struct{ data func() CreatorDetails }

func NewCreatorRef(d CreatorDetails) (CreatorRef, error) {
	switch d.Kind {
	case id.Human:
		if d.UserID.Validate() != nil || d.ProjectID != (id.ProjectID{}) || d.AgentID != (id.AgentID{}) || d.ExecutionID != (id.ExecutionID{}) {
			return CreatorRef{}, invalid()
		}
	case id.AgentRun:
		if d.UserID != (id.UserID{}) || d.ProjectID.Validate() != nil || d.AgentID.Validate() != nil || d.ExecutionID.Validate() != nil {
			return CreatorRef{}, invalid()
		}
	default:
		return CreatorRef{}, invalid()
	}
	return CreatorRef{func() CreatorDetails { return d }}, nil
}
func (c CreatorRef) Validate() error {
	if c.data == nil {
		return invalid()
	}
	return nil
}
func (c CreatorRef) Details() CreatorDetails {
	if c.data == nil {
		return CreatorDetails{}
	}
	return c.data()
}
func (CreatorRef) Format(w fmt.State, _ rune)   { io.WriteString(w, "knowledge_creator") }
func (CreatorRef) MarshalJSON() ([]byte, error) { return []byte(`"knowledge_creator"`), nil }
func (*CreatorRef) UnmarshalJSON([]byte) error  { return invalid() }
func (CreatorRef) LogValue() slog.Value         { return slog.StringValue("knowledge_creator") }

type creatorWire struct {
	Kind      id.ActorKind `json:"kind"`
	User      string       `json:"user_id,omitempty"`
	Project   string       `json:"project_id,omitempty"`
	Agent     string       `json:"agent_id,omitempty"`
	Execution string       `json:"execution_id,omitempty"`
}

func idText[T any](v f.ID[T]) string {
	if v.Validate() != nil {
		return ""
	}
	return v.String()
}
func (c CreatorRef) wire() creatorWire {
	d := c.Details()
	return creatorWire{d.Kind, idText(d.UserID), idText(d.ProjectID), idText(d.AgentID), idText(d.ExecutionID)}
}
func (w *creatorWire) UnmarshalJSON(b []byte) error {
	type wire creatorWire
	v, e := decode[wire](b, []string{"kind"}, []string{"user_id", "project_id", "agent_id", "execution_id"}, nil)
	if e != nil {
		return e
	}
	fields := []string{"kind", "user_id"}
	if v.Kind == id.AgentRun {
		fields = []string{"kind", "project_id", "agent_id", "execution_id"}
	}
	v, e = decode[wire](b, fields, nil, nil)
	if e != nil {
		return e
	}
	c, e := creatorWire(v).creator()
	if e != nil {
		return e
	}
	*w = c.wire()
	return nil
}
func (w creatorWire) creator() (CreatorRef, error) {
	var d CreatorDetails
	d.Kind = w.Kind
	var e error
	if w.User != "" {
		d.UserID, e = f.ParseID[id.User](w.User)
		if e != nil {
			return CreatorRef{}, invalid()
		}
	}
	if w.Project != "" {
		d.ProjectID, e = f.ParseID[id.Project](w.Project)
		if e != nil {
			return CreatorRef{}, invalid()
		}
	}
	if w.Agent != "" {
		d.AgentID, e = f.ParseID[id.Agent](w.Agent)
		if e != nil {
			return CreatorRef{}, invalid()
		}
	}
	if w.Execution != "" {
		d.ExecutionID, e = f.ParseID[id.Execution](w.Execution)
		if e != nil {
			return CreatorRef{}, invalid()
		}
	}
	return NewCreatorRef(d)
}

type DocumentRef struct {
	ID               DocumentID     `json:"id"`
	ProjectID        id.ProjectID   `json:"project_id"`
	ParentDocumentID *DocumentID    `json:"parent_document_id"`
	Title            string         `json:"title"`
	ContentVersion   f.Version      `json:"content_version"`
	SourceKind       SourceKind     `json:"source_kind"`
	MediaType        string         `json:"media_type"`
	ObjectID         oc.ObjectID    `json:"object_id"`
	Status           DocumentStatus `json:"status"`
	IndexingStatus   IndexingStatus `json:"indexing_status"`
	CreatedBy        CreatorRef     `json:"-"`
	CreatedAt        f.Instant      `json:"created_at"`
	UpdatedAt        f.Instant      `json:"updated_at"`
}

func (d DocumentRef) Validate() error {
	kind, e := mediaKind(d.MediaType)
	if d.ID.Validate() != nil || d.ProjectID.Validate() != nil || !validParent(d.ParentDocumentID) || d.ParentDocumentID != nil && *d.ParentDocumentID == d.ID || ValidateTitle(d.Title) != nil || d.ContentVersion.Validate() != nil || e != nil || kind != d.SourceKind || d.ObjectID.Validate() != nil || d.Status.Validate() != nil || d.IndexingStatus.Validate() != nil || d.CreatedBy.Validate() != nil || d.CreatedAt.Validate() != nil || d.UpdatedAt.Validate() != nil || d.UpdatedAt.Time().Before(d.CreatedAt.Time()) {
		return invalid()
	}
	if c := d.CreatedBy.Details(); c.Kind == id.AgentRun && c.ProjectID != d.ProjectID {
		return invalid()
	}
	return nil
}
func cloneDocument(d DocumentRef) DocumentRef {
	d.ParentDocumentID = clonePtr(d.ParentDocumentID)
	return d
}

type documentAlias DocumentRef
type documentWire struct {
	documentAlias
	Creator creatorWire `json:"created_by"`
}

func (d DocumentRef) MarshalJSON() ([]byte, error) {
	return checked(documentWire{documentAlias(d), d.CreatedBy.wire()}, d.Validate())
}
func (d *DocumentRef) UnmarshalJSON(b []byte) error {
	v, e := decode[documentWire](b, []string{"id", "project_id", "parent_document_id", "title", "content_version", "source_kind", "media_type", "object_id", "status", "indexing_status", "created_by", "created_at", "updated_at"}, nil, []string{"parent_document_id"})
	if e != nil {
		return e
	}
	n := DocumentRef(v.documentAlias)
	n.CreatedBy, e = v.Creator.creator()
	if e != nil {
		return e
	}
	if e = n.Validate(); e == nil {
		*d = n
	}
	return e
}

type DocumentTombstone struct {
	ID             DocumentID   `json:"id"`
	ProjectID      id.ProjectID `json:"project_id"`
	ContentVersion f.Version    `json:"content_version"`
	DeletedAt      f.Instant    `json:"deleted_at"`
}

func (d DocumentTombstone) Validate() error {
	if d.ID.Validate() != nil || d.ProjectID.Validate() != nil || d.ContentVersion.Validate() != nil || d.DeletedAt.Validate() != nil {
		return invalid()
	}
	return nil
}

type DocumentHead struct {
	Active  *DocumentRef       `json:"active,omitempty"`
	Deleted *DocumentTombstone `json:"deleted,omitempty"`
}

func (d DocumentHead) Validate() error {
	if d.Active != nil && d.Deleted == nil && d.Active.Status == Active {
		return d.Active.Validate()
	}
	if d.Active == nil && d.Deleted != nil {
		return d.Deleted.Validate()
	}
	return invalid()
}

type ReadRequest struct {
	ByteOffset f.Progress `json:"byte_offset"`
	MaxBytes   int        `json:"max_bytes"`
}

func (r ReadRequest) Validate() error {
	if r.ByteOffset.Validate() != nil || r.MaxBytes < 1 || r.MaxBytes > MaxReadBytes {
		return invalid()
	}
	return nil
}
func DefaultReadRequest() ReadRequest { return ReadRequest{MaxBytes: DefaultReadBytes} }
func (r ReadRequest) MarshalJSON() ([]byte, error) {
	type w ReadRequest
	return checked(w(r), r.Validate())
}
func (r *ReadRequest) UnmarshalJSON(b []byte) error {
	type w struct {
		ByteOffset f.Progress `json:"byte_offset"`
		MaxBytes   *int       `json:"max_bytes"`
	}
	v, e := decode[w](b, nil, []string{"byte_offset", "max_bytes"}, nil)
	if e != nil {
		return e
	}
	n := DefaultReadRequest()
	n.ByteOffset = v.ByteOffset
	if v.MaxBytes != nil {
		n.MaxBytes = *v.MaxBytes
	}
	if e = n.Validate(); e == nil {
		*r = n
	}
	return e
}

type TextContent struct {
	Text           string     `json:"text"`
	NextByteOffset f.Progress `json:"next_byte_offset"`
	Truncated      bool       `json:"truncated"`
}

func (t TextContent) Validate() error {
	if !utf8.ValidString(t.Text) || len(t.Text) > MaxReadBytes || t.NextByteOffset.Validate() != nil || int64(t.NextByteOffset) < int64(len(t.Text)) {
		return invalid()
	}
	return nil
}

type DocumentContent struct {
	Document    DocumentRef          `json:"document"`
	Text        *TextContent         `json:"text,omitempty"`
	File        *oc.BusinessFileRef  `json:"-"`
	Unavailable *ReadableUnavailable `json:"unavailable,omitempty"`
}

func (d DocumentContent) Validate() error {
	if d.Document.Validate() != nil || d.Document.Status != Active {
		return invalid()
	}
	if d.Text != nil && d.File == nil && d.Unavailable == nil && d.Document.SourceKind == Text {
		return d.Text.Validate()
	}
	if d.Unavailable != nil && d.Text == nil && d.File == nil && d.Document.SourceKind == File {
		return d.Unavailable.Validate()
	}
	if d.File != nil && d.Text == nil && d.Unavailable == nil && d.File.Validate() == nil {
		v := d.File.Details()
		if v.Kind == oc.KnowledgeFile && v.ProjectID == d.Document.ProjectID && v.DocumentID == d.Document.ID.String() && v.Revision == d.Document.ContentVersion {
			return nil
		}
	}
	return invalid()
}

type knowledgeFileWire struct {
	Project  id.ProjectID `json:"project_id"`
	Document DocumentID   `json:"document_id"`
	Version  f.Version    `json:"content_version"`
}

func (d DocumentContent) MarshalJSON() ([]byte, error) {
	type w DocumentContent
	var file *knowledgeFileWire
	if d.File != nil {
		file = &knowledgeFileWire{d.Document.ProjectID, d.Document.ID, d.Document.ContentVersion}
	}
	return checked(struct {
		w
		File *knowledgeFileWire `json:"file,omitempty"`
	}{w(d), file}, d.Validate())
}
func (d *DocumentContent) UnmarshalJSON(b []byte) error {
	type w DocumentContent
	v, e := decode[struct {
		w
		File *knowledgeFileWire `json:"file,omitempty"`
	}](b, []string{"document"}, []string{"text", "file", "unavailable"}, nil)
	if e != nil {
		return e
	}
	n := DocumentContent(v.w)
	if v.File != nil {
		ref, e := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.KnowledgeFile, ProjectID: v.File.Project, DocumentID: v.File.Document.String(), Revision: v.File.Version})
		if e != nil {
			return invalid()
		}
		n.File = &ref
	}
	if e = n.Validate(); e == nil {
		*d = n
	}
	return e
}
func (w *knowledgeFileWire) UnmarshalJSON(b []byte) error {
	type v knowledgeFileWire
	x, e := decode[v](b, []string{"project_id", "document_id", "content_version"}, nil, nil)
	if e == nil {
		*w = knowledgeFileWire(x)
	}
	return e
}

type ListFilter struct {
	TitleQuery     string          `json:"title_query,omitempty"`
	SourceKind     *SourceKind     `json:"source_kind,omitempty"`
	MediaType      *string         `json:"media_type,omitempty"`
	IndexingStatus *IndexingStatus `json:"indexing_status,omitempty"`
}

func (q ListFilter) Validate() error {
	if !utf8.ValidString(q.TitleQuery) {
		return invalid()
	}
	for _, r := range q.TitleQuery {
		if unicode.IsControl(r) {
			return invalid()
		}
	}
	if q.SourceKind != nil && q.SourceKind.Validate() != nil || q.IndexingStatus != nil && q.IndexingStatus.Validate() != nil {
		return invalid()
	}
	if q.MediaType != nil {
		kind, e := mediaKind(*q.MediaType)
		if e != nil || q.SourceKind != nil && *q.SourceKind != kind {
			return invalid()
		}
	}
	return nil
}

type TitleHit struct {
	Document  DocumentRef   `json:"document"`
	Ancestors []DocumentRef `json:"ancestors"`
}

func (h TitleHit) Validate() error {
	if h.Document.Validate() != nil || h.Document.Status != Active || h.Ancestors == nil {
		return invalid()
	}
	var parent *DocumentID
	seen := map[DocumentID]bool{h.Document.ID: true}
	for _, n := range h.Ancestors {
		if n.Validate() != nil || n.ProjectID != h.Document.ProjectID || n.Status != Active || !samePtr(n.ParentDocumentID, parent) || seen[n.ID] {
			return invalid()
		}
		seen[n.ID] = true
		x := n.ID
		parent = &x
	}
	if !samePtr(h.Document.ParentDocumentID, parent) {
		return invalid()
	}
	return nil
}

type Documents interface {
	CreateDocument(context.Context, id.Actor, f.CommandMeta, CreateRequest, SourceInput) (DocumentRef, error)
	UpdateDocument(context.Context, id.Actor, f.CommandMeta, id.ProjectID, DocumentID, UpdateRequest, *SourceInput) (DocumentRef, error)
	GetDocument(context.Context, id.Actor, id.ProjectID, DocumentID) (DocumentHead, error)
	ReadDocument(context.Context, id.Actor, id.ProjectID, DocumentID, ReadRequest) (DocumentContent, error)
	ListDocuments(context.Context, id.Actor, id.ProjectID, ListFilter, f.PageRequest) (f.Page[DocumentRef], error)
	ListChildren(context.Context, id.Actor, id.ProjectID, *DocumentID, ListFilter, f.PageRequest) (f.Page[DocumentRef], error)
	ReadAncestors(context.Context, id.Actor, id.ProjectID, DocumentID) ([]DocumentRef, error)
	SearchTitles(context.Context, id.Actor, id.ProjectID, string, f.PageRequest) (f.Page[TitleHit], error)
	MoveDocument(context.Context, id.Actor, f.CommandMeta, id.ProjectID, DocumentID, MoveRequest) (MoveResult, error)
	PrepareDeleteSubtree(context.Context, id.Actor, id.ProjectID, DocumentID) (DeletePreview, error)
	DeleteSubtree(context.Context, id.Actor, f.CommandMeta, id.ProjectID, DocumentID, ConfirmationToken) (DeleteResult, error)
	LookupCommand(context.Context, id.Actor, LookupRequest) (CommandLookup, error)
}

func (v SourceKind) Validate() error              { return oneOf(v, Text, File) }
func (v SourceKind) MarshalJSON() ([]byte, error) { return enumJSON(v, v.Validate()) }
func (v *SourceKind) UnmarshalJSON(b []byte) error {
	n, e := enumDecode(b, SourceKind.Validate)
	if e == nil {
		*v = n
	}
	return e
}

func (v DocumentStatus) Validate() error              { return oneOf(v, Active, Deleted) }
func (v DocumentStatus) MarshalJSON() ([]byte, error) { return enumJSON(v, v.Validate()) }
func (v *DocumentStatus) UnmarshalJSON(b []byte) error {
	n, e := enumDecode(b, DocumentStatus.Validate)
	if e == nil {
		*v = n
	}
	return e
}

func (v IndexingStatus) Validate() error {
	return oneOf(v, IndexPending, IndexProcessing, IndexReady, IndexFailed)
}
func (v IndexingStatus) MarshalJSON() ([]byte, error) { return enumJSON(v, v.Validate()) }
func (v *IndexingStatus) UnmarshalJSON(b []byte) error {
	n, e := enumDecode(b, IndexingStatus.Validate)
	if e == nil {
		*v = n
	}
	return e
}

func (v ReadableUnavailable) Validate() error {
	return oneOf(v, ReadableProcessing, ReadableFailed, ReadableUnbound)
}
func (v ReadableUnavailable) MarshalJSON() ([]byte, error) { return enumJSON(v, v.Validate()) }
func (v *ReadableUnavailable) UnmarshalJSON(b []byte) error {
	n, e := enumDecode(b, ReadableUnavailable.Validate)
	if e == nil {
		*v = n
	}
	return e
}

func (v DocumentTombstone) MarshalJSON() ([]byte, error) {
	type wire DocumentTombstone
	return checked(wire(v), v.Validate())
}
func (v *DocumentTombstone) UnmarshalJSON(b []byte) error {
	type wire DocumentTombstone
	w, e := decode[wire](b, []string{"id", "project_id", "content_version", "deleted_at"}, nil, nil)
	if e != nil {
		return e
	}
	n := DocumentTombstone(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v DocumentHead) MarshalJSON() ([]byte, error) {
	type wire DocumentHead
	return checked(wire(v), v.Validate())
}
func (v *DocumentHead) UnmarshalJSON(b []byte) error {
	type wire DocumentHead
	w, e := decode[wire](b, nil, []string{"active", "deleted"}, nil)
	if e != nil {
		return e
	}
	n := DocumentHead(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v TextContent) MarshalJSON() ([]byte, error) {
	type wire TextContent
	return checked(wire(v), v.Validate())
}
func (v *TextContent) UnmarshalJSON(b []byte) error {
	type wire TextContent
	w, e := decode[wire](b, []string{"text", "next_byte_offset", "truncated"}, nil, nil)
	if e != nil {
		return e
	}
	n := TextContent(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v ListFilter) MarshalJSON() ([]byte, error) {
	type wire ListFilter
	return checked(wire(v), v.Validate())
}
func (v *ListFilter) UnmarshalJSON(b []byte) error {
	type wire ListFilter
	w, e := decode[wire](b, nil, []string{"title_query", "source_kind", "media_type", "indexing_status"}, nil)
	if e != nil {
		return e
	}
	n := ListFilter(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (v TitleHit) MarshalJSON() ([]byte, error) {
	type wire TitleHit
	return checked(wire(v), v.Validate())
}
func (v *TitleHit) UnmarshalJSON(b []byte) error {
	type wire TitleHit
	w, e := decode[wire](b, []string{"document", "ancestors"}, nil, nil)
	if e != nil {
		return e
	}
	n := TitleHit(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}
