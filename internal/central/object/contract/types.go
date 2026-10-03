// Package contract defines object identities, safe metadata and trusted domain
// ports. It contains no storage SDK, credential, bucket, key or filesystem path.
package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const MaxObjectSize int64 = 1 << 30
const StreamBufferSize = 64 << 10

type StoredObject struct{}
type Upload struct{}
type Attempt struct{}
type Receipt struct{}
type Payload struct{}
type Lease struct{}
type Process struct{}
type CleanupOperation struct{}
type ObjectID = foundation.ID[StoredObject]
type UploadID = foundation.ID[Upload]
type AttemptID = foundation.ID[Attempt]
type ReceiptID = foundation.ID[Receipt]
type PayloadID = foundation.ID[Payload]
type LeaseID = foundation.ID[Lease]
type ProcessID = foundation.ID[Process]
type CleanupID = foundation.ID[CleanupOperation]

type OwnerKind string

const (
	Avatar           OwnerKind = "avatar"
	Artifact         OwnerKind = "artifact"
	Knowledge        OwnerKind = "knowledge"
	SkillRevision    OwnerKind = "skill_revision"
	MCPContent       OwnerKind = "mcp_content"
	ExecutionPayload OwnerKind = "execution_payload"
	MeetingFile      OwnerKind = "meeting_file"
)

func (k OwnerKind) Valid() bool {
	switch k {
	case Avatar, Artifact, Knowledge, SkillRevision, MCPContent, ExecutionPayload, MeetingFile:
		return true
	}
	return false
}

type ObjectOwner struct{ data func() OwnerDetails }
type OwnerDetails struct {
	Kind      OwnerKind `json:"kind"`
	ID        string    `json:"owner_id"`
	ProjectID string    `json:"project_id,omitempty"`
}

func NewObjectOwner(kind OwnerKind, ownerID, projectID string) (ObjectOwner, error) {
	if !kind.Valid() || !validID(ownerID) || kind == Avatar && projectID != "" || kind != Avatar && !validID(projectID) {
		return ObjectOwner{}, bad()
	}
	d := OwnerDetails{kind, ownerID, projectID}
	return ObjectOwner{data: func() OwnerDetails { return d }}, nil
}
func (o ObjectOwner) Validate() error {
	if o.data == nil {
		return bad()
	}
	return nil
}
func (o ObjectOwner) Details() OwnerDetails {
	if o.data == nil {
		return OwnerDetails{}
	}
	return o.data()
}
func (o ObjectOwner) Equal(other ObjectOwner) bool {
	return o.data != nil && other.data != nil && o.Details() == other.Details()
}
func (o ObjectOwner) Scope() identity.Scope {
	if o.data == nil {
		return identity.Scope{}
	}
	if o.Details().Kind == Avatar {
		return identity.SystemScope()
	}
	id, _ := foundation.ParseID[identity.Project](o.Details().ProjectID)
	scope, _ := identity.InProject(id)
	return scope
}

// Partition separates Avatar users even though both have System scope.
func (o ObjectOwner) Partition() string {
	if o.data == nil {
		return ""
	}
	d := o.Details()
	if d.Kind == Avatar {
		return d.ID
	}
	return d.ProjectID
}
func (o ObjectOwner) MarshalJSON() ([]byte, error) {
	if o.Validate() != nil {
		return nil, bad()
	}
	return json.Marshal(o.Details())
}
func (*ObjectOwner) UnmarshalJSON([]byte) error  { return bad() }
func (o ObjectOwner) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "object_owner") }
func (o ObjectOwner) LogValue() slog.Value       { return slog.StringValue("object_owner") }

type State string

const (
	Pending   State = "pending"
	Available State = "available"
	Failed    State = "failed"
	Deleted   State = "deleted"
)

func (s State) Valid() bool { return s == Pending || s == Available || s == Failed || s == Deleted }

type ObjectMeta struct {
	ID        ObjectID            `json:"id"`
	Scope     identity.Scope      `json:"scope"`
	MediaType string              `json:"media_type"`
	ByteSize  foundation.Progress `json:"byte_size"`
	SHA256    foundation.Digest   `json:"sha256"`
	State     State               `json:"state"`
	Version   foundation.Version  `json:"version"`
	CreatedAt foundation.Instant  `json:"created_at"`
}

func (m ObjectMeta) Validate() error {
	media, err := NormalizeMediaType(m.MediaType)
	if m.ID.Validate() != nil || m.Scope.Validate() != nil || m.Scope.Details().Kind == identity.AgentMemory || err != nil || media != m.MediaType || int64(m.ByteSize) > MaxObjectSize || m.ByteSize.Validate() != nil || m.SHA256.Validate() != nil || !m.State.Valid() || m.Version.Validate() != nil || m.CreatedAt.Validate() != nil {
		return bad()
	}
	return nil
}
func NormalizeMediaType(value string) (string, error) {
	if value == "" || len(value) > 256 {
		return "", bad()
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "", bad()
		}
	}
	kind, params, err := mime.ParseMediaType(value)
	if err != nil {
		return "", bad()
	}
	value = mime.FormatMediaType(kind, params)
	if value == "" || len(value) > 256 {
		return "", bad()
	}
	return value, nil
}

type ByteRange struct{ Offset, Length int64 }
type ResolvedRange struct {
	Offset foundation.Progress `json:"offset"`
	Length foundation.Progress `json:"length"`
	Total  foundation.Progress `json:"total"`
}

func (r ByteRange) Resolve(size int64) (ResolvedRange, error) {
	if r.Offset < 0 || r.Length <= 0 || r.Offset > math.MaxInt64-r.Length {
		return ResolvedRange{}, bad()
	}
	if size <= 0 || r.Offset >= size {
		return ResolvedRange{}, foundation.NewFault(foundation.RangeNotSatisfiable, foundation.NotStarted)
	}
	n := min(r.Length, size-r.Offset)
	return ResolvedRange{foundation.Progress(r.Offset), foundation.Progress(n), foundation.Progress(size)}, nil
}

type ReferenceKind string

const (
	ReservedReference  ReferenceKind = "reserved"
	CanonicalReference ReferenceKind = "canonical"
)

type ObjectReference struct {
	ObjectID ObjectID      `json:"object_id"`
	Owner    ObjectOwner   `json:"owner"`
	Kind     ReferenceKind `json:"kind"`
}
type LeaseOwnerKind string

const (
	ReaderOwner    LeaseOwnerKind = "reader"
	SourceOwner    LeaseOwnerKind = "source"
	WriterOwner    LeaseOwnerKind = "writer"
	ExecutionOwner LeaseOwnerKind = "execution"
	HistoryOwner   LeaseOwnerKind = "history"
	TransferOwner  LeaseOwnerKind = "transfer"
)

func (k LeaseOwnerKind) Valid() bool {
	switch k {
	case ReaderOwner, SourceOwner, WriterOwner, ExecutionOwner, HistoryOwner, TransferOwner:
		return true
	}
	return false
}

type LeaseOwner struct{ data func() LeaseOwnerDetails }
type LeaseOwnerDetails struct {
	Kind LeaseOwnerKind `json:"kind"`
	ID   string         `json:"id"`
}

func NewLeaseOwner(kind LeaseOwnerKind, id string) (LeaseOwner, error) {
	if !kind.Valid() || !validID(id) {
		return LeaseOwner{}, bad()
	}
	d := LeaseOwnerDetails{kind, id}
	return LeaseOwner{func() LeaseOwnerDetails { return d }}, nil
}
func (o LeaseOwner) Validate() error {
	if o.data == nil {
		return bad()
	}
	return nil
}
func (o LeaseOwner) Details() LeaseOwnerDetails {
	if o.data == nil {
		return LeaseOwnerDetails{}
	}
	return o.data()
}
func (o LeaseOwner) Equal(other LeaseOwner) bool {
	return o.data != nil && other.data != nil && o.Details() == other.Details()
}
func (o LeaseOwner) MarshalJSON() ([]byte, error) {
	if o.Validate() != nil {
		return nil, bad()
	}
	return json.Marshal(o.Details())
}
func (*LeaseOwner) UnmarshalJSON([]byte) error  { return bad() }
func (o LeaseOwner) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "object_lease_owner") }
func (o LeaseOwner) LogValue() slog.Value       { return slog.StringValue("object_lease_owner") }

type ObjectLease struct {
	ID       LeaseID    `json:"lease_id"`
	ObjectID ObjectID   `json:"object_id"`
	Owner    LeaseOwner `json:"owner"`
}

func (l ObjectLease) Validate() error {
	if l.ID.Validate() != nil || l.ObjectID.Validate() != nil || l.Owner.Validate() != nil {
		return bad()
	}
	return nil
}

type ReferenceInspection struct {
	References   []ObjectReference `json:"references"`
	ActiveLeases []ObjectLease     `json:"active_leases"`
}

// Receipt is an upload lookup identity, never a bearer authorization. Services
// recheck its DB row, stable actor, exact owner/cause and disposition every use.
// The explicit Details projection is only for trusted composition adapters.
type UploadReceipt struct{ data func() ReceiptDetails }
type ReceiptDetails struct {
	ID            ReceiptID
	UploadID      UploadID
	ObjectID      ObjectID
	Owner         ObjectOwner
	CreationCause string
}

func NewUploadReceipt(d ReceiptDetails) (UploadReceipt, error) {
	if d.ID.Validate() != nil || d.UploadID.Validate() != nil || d.ObjectID.Validate() != nil || d.Owner.Validate() != nil || !identity.ValidCauseRef(d.CreationCause) {
		return UploadReceipt{}, bad()
	}
	return UploadReceipt{func() ReceiptDetails { return d }}, nil
}
func (r UploadReceipt) Validate() error {
	if r.data == nil {
		return bad()
	}
	return nil
}
func (r UploadReceipt) Details() ReceiptDetails {
	if r.data == nil {
		return ReceiptDetails{}
	}
	return r.data()
}
func (r UploadReceipt) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "upload_receipt") }
func (r UploadReceipt) MarshalJSON() ([]byte, error) { return []byte(`"upload_receipt"`), nil }
func (*UploadReceipt) UnmarshalJSON([]byte) error    { return bad() }
func (r UploadReceipt) LogValue() slog.Value         { return slog.StringValue("upload_receipt") }

type PreparedPayload struct{ data func() PreparedDetails }
type PreparedDetails struct {
	ID        PayloadID
	MediaType string
	Length    int64
	SHA256    foundation.Digest
}

// NewPreparedPayload is used by the owned spool adapter. It does not make a
// caller-supplied ID readable: every service operation verifies its live owned
// preparation registry, original actor/owner and immutable measured metadata.
func NewPreparedPayload(d PreparedDetails) (PreparedPayload, error) {
	m, e := NormalizeMediaType(d.MediaType)
	if d.ID.Validate() != nil || e != nil || m != d.MediaType || d.Length < 0 || d.Length > MaxObjectSize || d.SHA256.Validate() != nil {
		return PreparedPayload{}, bad()
	}
	return PreparedPayload{func() PreparedDetails { return d }}, nil
}
func (p PreparedPayload) Validate() error {
	if p.data == nil {
		return bad()
	}
	return nil
}
func (p PreparedPayload) Details() PreparedDetails {
	if p.data == nil {
		return PreparedDetails{}
	}
	return p.data()
}
func (p PreparedPayload) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "prepared_payload") }
func (p PreparedPayload) MarshalJSON() ([]byte, error) { return []byte(`"prepared_payload"`), nil }
func (*PreparedPayload) UnmarshalJSON([]byte) error    { return bad() }
func (p PreparedPayload) LogValue() slog.Value         { return slog.StringValue("prepared_payload") }

// Attempt handles carry no client-supplied verified flag or storage locator.
// Publishing requires the durable verified row under the object lock.
type UploadAttempt struct{ data func() AttemptDetails }
type AttemptDetails struct {
	ID       AttemptID
	UploadID UploadID
	ObjectID ObjectID
}

func NewUploadAttempt(d AttemptDetails) (UploadAttempt, error) {
	if d.ID.Validate() != nil || d.UploadID.Validate() != nil || d.ObjectID.Validate() != nil {
		return UploadAttempt{}, bad()
	}
	return UploadAttempt{func() AttemptDetails { return d }}, nil
}
func (a UploadAttempt) Validate() error {
	if a.data == nil {
		return bad()
	}
	return nil
}
func (a UploadAttempt) Details() AttemptDetails {
	if a.data == nil {
		return AttemptDetails{}
	}
	return a.data()
}
func (a UploadAttempt) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "upload_attempt") }
func (a UploadAttempt) MarshalJSON() ([]byte, error) { return []byte(`"upload_attempt"`), nil }
func (*UploadAttempt) UnmarshalJSON([]byte) error    { return bad() }
func (a UploadAttempt) LogValue() slog.Value         { return slog.StringValue("upload_attempt") }

type LookupState string

const (
	NotObserved     LookupState = "not_observed"
	UploadPending   LookupState = "pending"
	UploadUnknown   LookupState = "unknown"
	UploadFailed    LookupState = "failed"
	UploadCommitted LookupState = "committed"
	UploadRevoked   LookupState = "revoked"
)

type LookupResult struct {
	State    LookupState   `json:"state"`
	Meta     *ObjectMeta   `json:"object,omitempty"`
	ObjectID *ObjectID     `json:"object_id,omitempty"`
	Receipt  UploadReceipt `json:"-"`
	Cleanup  CleanupState  `json:"cleanup_state,omitempty"`
}
type PutResult struct {
	Meta    ObjectMeta    `json:"object"`
	Receipt UploadReceipt `json:"-"`
}

// A reader keeps its storage body behind a closure, including in recursively
// formatted enclosing private fields. Close must cancel/join actual I/O before
// its lease can be released; a consumed reader is not an authorization token.
type ObjectReader struct{ data func() readerDetails }
type readerDetails struct {
	meta     ObjectMeta
	resolved *ResolvedRange
	body     io.ReadCloser
}

func NewObjectReader(meta ObjectMeta, resolved *ResolvedRange, body io.ReadCloser) (*ObjectReader, error) {
	if meta.Validate() != nil || meta.State != Available || nilReader(body) {
		return nil, bad()
	}
	var copied *ResolvedRange
	if resolved != nil {
		if resolved.Offset.Validate() != nil || resolved.Length <= 0 || resolved.Total != meta.ByteSize || resolved.Offset >= resolved.Total || resolved.Length > resolved.Total-resolved.Offset {
			return nil, bad()
		}
		v := *resolved
		copied = &v
	}
	d := readerDetails{meta, copied, body}
	return &ObjectReader{func() readerDetails { return d }}, nil
}
func nilReader(body io.ReadCloser) bool {
	if body == nil {
		return true
	}
	v := reflect.ValueOf(body)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}
func (r *ObjectReader) Meta() ObjectMeta {
	if r == nil || r.data == nil {
		return ObjectMeta{}
	}
	return r.data().meta
}
func (r *ObjectReader) Range() *ResolvedRange {
	if r == nil || r.data == nil || r.data().resolved == nil {
		return nil
	}
	v := *r.data().resolved
	return &v
}
func (r *ObjectReader) Read(p []byte) (int, error) {
	if r == nil || r.data == nil {
		return 0, bad()
	}
	return r.data().body.Read(p)
}
func (r *ObjectReader) Close() error {
	if r == nil || r.data == nil {
		return nil
	}
	return r.data().body.Close()
}
func (r ObjectReader) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "object_reader") }
func (r ObjectReader) MarshalJSON() ([]byte, error) { return []byte(`"object_reader"`), nil }
func (*ObjectReader) UnmarshalJSON([]byte) error    { return bad() }
func (r ObjectReader) LogValue() slog.Value         { return slog.StringValue("object_reader") }

type CleanupReason string

const (
	CancelledUpload  CleanupReason = "cancelled_upload"
	AbandonedAttempt CleanupReason = "abandoned_attempt"
	OwnerDeleted     CleanupReason = "owner_deleted"
	ProjectDeleted   CleanupReason = "project_deleted"
	ReplacedObject   CleanupReason = "replaced_object"
)

func (r CleanupReason) Valid() bool {
	switch r {
	case CancelledUpload, AbandonedAttempt, OwnerDeleted, ProjectDeleted, ReplacedObject:
		return true
	}
	return false
}

type ObjectCleanupCause struct{ data func() CleanupDetails }
type CleanupDetails struct {
	OperationID CleanupID
	Owner       ObjectOwner
	Reason      CleanupReason
}

func NewObjectCleanupCause(d CleanupDetails) (ObjectCleanupCause, error) {
	if d.OperationID.Validate() != nil || d.Owner.Validate() != nil || !d.Reason.Valid() {
		return ObjectCleanupCause{}, bad()
	}
	return ObjectCleanupCause{func() CleanupDetails { return d }}, nil
}
func (c ObjectCleanupCause) Validate() error {
	if c.data == nil {
		return bad()
	}
	return nil
}
func (c ObjectCleanupCause) Details() CleanupDetails {
	if c.data == nil {
		return CleanupDetails{}
	}
	return c.data()
}
func (c ObjectCleanupCause) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_cleanup_cause")
}
func (c ObjectCleanupCause) MarshalJSON() ([]byte, error) {
	return []byte(`"object_cleanup_cause"`), nil
}
func (*ObjectCleanupCause) UnmarshalJSON([]byte) error { return bad() }
func (c ObjectCleanupCause) LogValue() slog.Value      { return slog.StringValue("object_cleanup_cause") }

type CleanupState string

const (
	CleanupPending   CleanupState = "pending"
	CleanupCompleted CleanupState = "completed"
	CleanupFailed    CleanupState = "failed"
)

type CleanupResult struct {
	State       CleanupState        `json:"state"`
	OperationID CleanupID           `json:"operation_id"`
	Remaining   ReferenceInspection `json:"remaining"`
}

func validID(id string) bool { _, e := foundation.ParseID[struct{}](id); return e == nil }
func bad() error             { return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted) }
