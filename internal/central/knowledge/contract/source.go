package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type InputKind string

const (
	InputText         InputKind = "text"
	InputUpload       InputKind = "upload"
	InputBusinessFile InputKind = "business_file"
)

type UploadInputDetails struct {
	Length         f.Progress
	ExpectedSHA256 f.Digest
}
type SourceInputDetails struct {
	Kind         InputKind
	MediaType    string
	Text         *string
	Upload       *UploadInputDetails
	BusinessFile *oc.BusinessFileRef
}

func (d SourceInputDetails) Validate() error {
	switch d.Kind {
	case InputText:
		k, e := mediaKind(d.MediaType)
		if e != nil || k != Text || d.Text == nil || !utf8.ValidString(*d.Text) || int64(len(*d.Text)) > oc.MaxObjectSize || d.Upload != nil || d.BusinessFile != nil {
			return invalid()
		}
	case InputUpload:
		if _, e := mediaKind(d.MediaType); e != nil {
			return e
		}
		if d.Text != nil || d.BusinessFile != nil || d.Upload == nil || d.Upload.Length.Validate() != nil || int64(d.Upload.Length) > oc.MaxObjectSize || d.Upload.ExpectedSHA256.Validate() != nil {
			return invalid()
		}
	case InputBusinessFile:
		if d.MediaType != "" || d.Text != nil || d.Upload != nil || d.BusinessFile == nil || d.BusinessFile.Validate() != nil {
			return invalid()
		}
	default:
		return invalid()
	}
	return nil
}
func copySource(d SourceInputDetails) SourceInputDetails {
	d.Text = clonePtr(d.Text)
	d.Upload = clonePtr(d.Upload)
	d.BusinessFile = clonePtr(d.BusinessFile)
	return d
}

// streamState owns exactly one underlying Close. It does not infer that an
// error means the provider joined or released a lease. Close never waits on
// readMu, so a provider can interrupt its own blocked Read.
type streamState struct {
	body          io.ReadCloser
	mu            sync.Mutex
	readMu        sync.Mutex
	once          sync.Once
	closed, taken bool
	closeErr      error
}

func (s *streamState) read(p []byte) (int, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return 0, invalid()
	}
	return s.body.Read(p)
}
func (s *streamState) close() error {
	s.once.Do(func() { s.mu.Lock(); s.closed = true; s.mu.Unlock(); s.closeErr = s.body.Close() })
	return s.closeErr
}

type managedBody struct{ state func() *streamState }

func (b managedBody) Read(p []byte) (int, error) { return b.state().read(p) }
func (b managedBody) Close() error               { return b.state().close() }
func (managedBody) Format(w fmt.State, _ rune)   { io.WriteString(w, "knowledge_upload_body") }
func (managedBody) MarshalJSON() ([]byte, error) { return []byte(`"knowledge_upload_body"`), nil }
func (*managedBody) UnmarshalJSON([]byte) error  { return invalid() }
func (managedBody) LogValue() slog.Value         { return slog.StringValue("knowledge_upload_body") }
func nilBody(v io.ReadCloser) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Func, reflect.Interface, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}

type sourceData struct {
	details SourceInputDetails
	stream  *streamState
}
type SourceInput struct{ data func() sourceData }

func NewTextSource(media, text string) (SourceInput, error) {
	return newSource(SourceInputDetails{Kind: InputText, MediaType: media, Text: &text}, nil)
}
func NewUploadSource(media string, length f.Progress, sha f.Digest, body io.ReadCloser) (SourceInput, error) {
	if nilBody(body) {
		return SourceInput{}, invalid()
	}
	return newSource(SourceInputDetails{Kind: InputUpload, MediaType: media, Upload: &UploadInputDetails{length, sha}}, body)
}
func NewBusinessSource(ref oc.BusinessFileRef) (SourceInput, error) {
	return newSource(SourceInputDetails{Kind: InputBusinessFile, BusinessFile: &ref}, nil)
}
func newSource(d SourceInputDetails, body io.ReadCloser) (SourceInput, error) {
	if e := d.Validate(); e != nil {
		return SourceInput{}, e
	}
	d = copySource(d)
	var s *streamState
	if body != nil {
		s = &streamState{body: body}
	}
	return SourceInput{func() sourceData { return sourceData{copySource(d), s} }}, nil
}
func (s SourceInput) Validate() error {
	if s.data == nil {
		return invalid()
	}
	return s.data().details.Validate()
}
func (s SourceInput) Details() (SourceInputDetails, error) {
	if s.Validate() != nil {
		return SourceInputDetails{}, invalid()
	}
	return s.data().details, nil
}
func (s SourceInput) TakeUploadBody() (io.ReadCloser, error) {
	if s.Validate() != nil {
		return nil, invalid()
	}
	v := s.data()
	if v.details.Kind != InputUpload {
		return nil, invalid()
	}
	v.stream.mu.Lock()
	defer v.stream.mu.Unlock()
	if v.stream.taken || v.stream.closed {
		return nil, invalid()
	}
	v.stream.taken = true
	return managedBody{func() *streamState { return v.stream }}, nil
}
func (s SourceInput) Close() error {
	if s.Validate() != nil {
		return invalid()
	}
	if v := s.data(); v.stream != nil {
		return v.stream.close()
	}
	return nil
}

type canonicalData struct {
	document DocumentRef
	resolved *oc.ResolvedRange
	stream   *streamState
}
type CanonicalRead struct{ data func() canonicalData }

func NewCanonicalRead(d DocumentRef, r *oc.ObjectReader) (CanonicalRead, error) {
	if d.Validate() != nil || d.Status != Active || r == nil {
		return CanonicalRead{}, invalid()
	}
	m := r.Meta()
	scope := m.Scope.Details()
	if m.Validate() != nil || m.State != oc.Available || m.ID != d.ObjectID || scope.Kind != id.ProjectScope || scope.ProjectID != d.ProjectID.String() || m.MediaType != d.MediaType {
		return CanonicalRead{}, invalid()
	}
	resolved := r.Range()
	if resolved != nil && (resolved.Offset.Validate() != nil || resolved.Length <= 0 || resolved.Total != m.ByteSize || resolved.Offset >= resolved.Total || resolved.Length > resolved.Total-resolved.Offset) {
		return CanonicalRead{}, invalid()
	}
	d = cloneDocument(d)
	s := &streamState{body: r}
	return CanonicalRead{func() canonicalData { return canonicalData{cloneDocument(d), clonePtr(resolved), s} }}, nil
}
func (r CanonicalRead) Validate() error {
	if r.data == nil {
		return invalid()
	}
	return nil
}
func (r CanonicalRead) Document() DocumentRef {
	if r.data == nil {
		return DocumentRef{}
	}
	return r.data().document
}
func (r CanonicalRead) Range() *oc.ResolvedRange {
	if r.data == nil {
		return nil
	}
	return r.data().resolved
}
func (r CanonicalRead) Read(p []byte) (int, error) {
	if r.data == nil {
		return 0, invalid()
	}
	return r.data().stream.read(p)
}
func (r CanonicalRead) Close() error {
	if r.data == nil {
		return invalid()
	}
	return r.data().stream.close()
}

type CanonicalReads interface {
	OpenCanonical(context.Context, id.Actor, id.ProjectID, DocumentID, *oc.ByteRange) (CanonicalRead, error)
}
type CurrentDocumentFact struct {
	ProjectID      id.ProjectID   `json:"project_id"`
	DocumentID     DocumentID     `json:"document_id"`
	ContentVersion f.Version      `json:"content_version"`
	Status         DocumentStatus `json:"status"`
	ObjectID       *oc.ObjectID   `json:"object_id,omitempty"`
}

func (d CurrentDocumentFact) Validate() error {
	if d.ProjectID.Validate() != nil || d.DocumentID.Validate() != nil || d.ContentVersion.Validate() != nil || d.Status.Validate() != nil {
		return invalid()
	}
	if d.Status == Active {
		if d.ObjectID == nil || d.ObjectID.Validate() != nil {
			return invalid()
		}
	} else if d.ObjectID != nil {
		return invalid()
	}
	return nil
}

type CanonicalFacts interface {
	ReadCurrentInTx(context.Context, f.Tx, id.Actor, id.ProjectID, DocumentID) (CurrentDocumentFact, error)
}

func (v InputKind) Validate() error              { return oneOf(v, InputText, InputUpload, InputBusinessFile) }
func (v InputKind) MarshalJSON() ([]byte, error) { return enumJSON(v, v.Validate()) }
func (v *InputKind) UnmarshalJSON(b []byte) error {
	n, e := enumDecode(b, InputKind.Validate)
	if e == nil {
		*v = n
	}
	return e
}

func (v CurrentDocumentFact) MarshalJSON() ([]byte, error) {
	type wire CurrentDocumentFact
	return checked(wire(v), v.Validate())
}
func (v *CurrentDocumentFact) UnmarshalJSON(b []byte) error {
	type wire CurrentDocumentFact
	w, e := decode[wire](b, []string{"project_id", "document_id", "content_version", "status"}, []string{"object_id"}, nil)
	if e != nil {
		return e
	}
	n := CurrentDocumentFact(w)
	if e = n.Validate(); e == nil {
		*v = n
	}
	return e
}

func (SourceInput) Format(w fmt.State, _ rune)   { io.WriteString(w, "knowledge_source") }
func (SourceInput) MarshalJSON() ([]byte, error) { return []byte(`"knowledge_source"`), nil }
func (*SourceInput) UnmarshalJSON([]byte) error  { return invalid() }
func (SourceInput) LogValue() slog.Value         { return slog.StringValue("knowledge_source") }

func (CanonicalRead) Format(w fmt.State, _ rune)   { io.WriteString(w, "knowledge_canonical_read") }
func (CanonicalRead) MarshalJSON() ([]byte, error) { return []byte(`"knowledge_canonical_read"`), nil }
func (*CanonicalRead) UnmarshalJSON([]byte) error  { return invalid() }
func (CanonicalRead) LogValue() slog.Value         { return slog.StringValue("knowledge_canonical_read") }
