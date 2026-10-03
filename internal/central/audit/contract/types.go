// Package contract exposes typed, append-oriented Audit ports. It contains no
// identity implementation, database driver or unstructured metadata input.
package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type Record struct{}
type LifecycleOperation struct{}
type ID = foundation.ID[Record]
type Action string

const (
	SecretCreate           Action = "secret.create"
	SecretUpdate           Action = "secret.update"
	SecretDelete           Action = "secret.delete"
	SecretResolve          Action = "secret.resolve"
	MasterRegister         Action = "secret.master.register"
	RotationStart          Action = "secret.master.rotation.start"
	RotationComplete       Action = "secret.master.rotation.complete"
	RotationFailed         Action = "secret.master.rotation.failed"
	PolicyUpdate           Action = "outbound.policy.update"
	AccessDeny             Action = "outbound.access.deny"
	ObjectUploadComplete   Action = "object.upload.complete"
	ObjectUploadFailed     Action = "object.upload.failed"
	ObjectDelete           Action = "object.delete"
	ObjectTransferIssue    Action = "object.transfer.issue"
	ObjectTransferComplete Action = "object.transfer.complete"
	ObjectTransferRevoke   Action = "object.transfer.revoke"
	ArtifactCreate         Action = "artifact.create"
	ArtifactList           Action = "artifact.list"
	ArtifactRead           Action = "artifact.read"
	ArtifactDownload       Action = "artifact.download"
)

func (a Action) Valid() bool {
	switch a {
	case SecretCreate, SecretUpdate, SecretDelete, SecretResolve, MasterRegister, RotationStart, RotationComplete, RotationFailed, PolicyUpdate, AccessDeny:
		return true
	case ObjectUploadComplete, ObjectUploadFailed, ObjectDelete, ObjectTransferIssue, ObjectTransferComplete, ObjectTransferRevoke, ArtifactCreate, ArtifactList, ArtifactRead, ArtifactDownload:
		return true
	}
	return false
}

type Outcome string

const (
	Success Outcome = "success"
	Denied  Outcome = "denied"
	Failed  Outcome = "failed"
	Unknown Outcome = "unknown"
)

func (o Outcome) Valid() bool { return o == Success || o == Denied || o == Failed || o == Unknown }

type ResourceKind string

const (
	SecretResource             ResourceKind = "secret"
	MasterResource             ResourceKind = "secret_master"
	RotationResource           ResourceKind = "secret_rotation"
	PolicyResource             ResourceKind = "outbound_policy"
	AgentResource              ResourceKind = "agent"
	ObjectResource             ResourceKind = "stored_object"
	ObjectTransferResource     ResourceKind = "object_transfer"
	ArtifactResource           ResourceKind = "artifact"
	ArtifactCollectionResource ResourceKind = "artifact_collection"
)

func (k ResourceKind) Valid() bool {
	switch k {
	case SecretResource, MasterResource, RotationResource, PolicyResource, AgentResource, ObjectResource, ObjectTransferResource, ArtifactResource, ArtifactCollectionResource:
		return true
	}
	return false
}

type Resource struct{ data func() ResourceDetails }
type ResourceDetails struct {
	Kind ResourceKind `json:"kind"`
	ID   string       `json:"id,omitempty"`
}

func NewResource(kind ResourceKind, id string) (Resource, error) {
	if !kind.Valid() || ((kind == MasterResource || kind == PolicyResource) && id != "") || ((kind != MasterResource && kind != PolicyResource) && !validID(id)) {
		return Resource{}, invalid("resource")
	}
	d := ResourceDetails{kind, id}
	return Resource{data: func() ResourceDetails { return d }}, nil
}
func (r Resource) Details() ResourceDetails {
	if r.data == nil {
		return ResourceDetails{}
	}
	return r.data()
}
func (r Resource) Validate() error {
	if r.data == nil {
		return invalid("resource")
	}
	return nil
}
func (r Resource) MarshalJSON() ([]byte, error) {
	if r.Validate() != nil {
		return nil, invalid("resource")
	}
	return json.Marshal(r.Details())
}
func (r Resource) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "audit_resource") }
func (r Resource) LogValue() slog.Value       { return slog.StringValue("audit_resource") }

type Associations struct {
	ToolID        string `json:"tool_id,omitempty"`
	ExecutionID   string `json:"execution_id,omitempty"`
	ToolCallID    string `json:"tool_call_id,omitempty"`
	OperationID   string `json:"operation_id,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	ApprovalID    string `json:"approval_id,omitempty"`
	RunnerID      string `json:"runner_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	HTTPTraceID   string `json:"http_trace_id,omitempty"`
}

func (a Associations) Validate() error {
	for _, id := range []string{a.ToolID, a.ExecutionID, a.ToolCallID, a.OperationID, a.RequestID, a.ApprovalID, a.RunnerID, a.CorrelationID, a.HTTPTraceID} {
		if id != "" && !validID(id) {
			return invalid("association")
		}
	}
	return nil
}

type EntryFields struct {
	Scope        identity.Scope
	Actor        identity.Actor
	Action       Action
	Outcome      Outcome
	Resource     Resource
	Metadata     Metadata
	Associations Associations
}
type Entry struct{ data func() EntryFields }

func NewEntry(f EntryFields) (Entry, error) {
	if f.Scope.Validate() != nil || f.Scope.Details().Kind == identity.AgentMemory || f.Actor.Validate() != nil || !f.Action.Valid() || !f.Outcome.Valid() || f.Resource.Validate() != nil || f.Metadata.Validate(f.Action) != nil || f.Associations.Validate() != nil {
		return Entry{}, invalid("entry")
	}
	a, s, r := f.Actor.Details(), f.Scope.Details(), f.Resource.Details()
	if a.Kind == identity.AgentRun && (s.Kind != identity.ProjectScope || a.ProjectID != s.ProjectID) {
		return Entry{}, invalid("actor")
	}
	if a.Kind == identity.Service && a.ProjectID != s.ProjectID {
		return Entry{}, invalid("actor")
	}
	if a.Kind == identity.AgentRun {
		if f.Associations.ExecutionID != "" && f.Associations.ExecutionID != a.ExecutionID {
			return Entry{}, invalid("association")
		}
		f.Associations.ExecutionID = a.ExecutionID
	}
	if s.Kind == identity.System && (f.Associations.ExecutionID != "" || f.Associations.ToolCallID != "" || f.Associations.OperationID != "" || f.Associations.ApprovalID != "" || r.Kind == AgentResource) {
		return Entry{}, invalid("scope")
	}
	switch f.Action {
	case SecretCreate, SecretUpdate, SecretDelete, SecretResolve:
		if r.Kind != SecretResource {
			return Entry{}, invalid("resource")
		}
	case MasterRegister:
		if s.Kind != identity.System || r.Kind != MasterResource || f.Outcome != Success {
			return Entry{}, invalid("entry")
		}
	case RotationStart, RotationComplete, RotationFailed:
		if s.Kind != identity.System || r.Kind != RotationResource || r.ID != f.Metadata.rotationID() {
			return Entry{}, invalid("entry")
		}
		if f.Action == RotationFailed && f.Outcome != Failed || f.Action != RotationFailed && f.Outcome != Success {
			return Entry{}, invalid("outcome")
		}
	case PolicyUpdate:
		if s.Kind != identity.System || r.Kind != PolicyResource {
			return Entry{}, invalid("entry")
		}
	case AccessDeny:
		if f.Outcome != Denied || r.Kind != PolicyResource && r.Kind != AgentResource && r.Kind != SecretResource {
			return Entry{}, invalid("entry")
		}
	case ObjectUploadComplete, ObjectUploadFailed, ObjectDelete:
		if r.Kind != ObjectResource || r.ID != f.Metadata.objectID() || a.Kind != identity.Service || a.ServiceName != identity.ObjectService && a.ServiceName != identity.ObjectMaintenance {
			return Entry{}, invalid("entry")
		}
		if f.Action == ObjectUploadFailed && f.Outcome != Failed && f.Outcome != Unknown || f.Action != ObjectUploadFailed && f.Outcome != Success {
			return Entry{}, invalid("outcome")
		}
	case ObjectTransferIssue, ObjectTransferComplete, ObjectTransferRevoke:
		if s.Kind != identity.ProjectScope || r.Kind != ObjectTransferResource || r.ID != f.Metadata.transferID() || a.Kind != identity.Service || a.ServiceName != identity.ObjectService && a.ServiceName != identity.ObjectMaintenance {
			return Entry{}, invalid("entry")
		}
		if f.Outcome != Success {
			return Entry{}, invalid("outcome")
		}
	case ArtifactCreate, ArtifactRead, ArtifactDownload, ArtifactList:
		if s.Kind != identity.ProjectScope || a.Kind != identity.Human && a.Kind != identity.AgentRun {
			return Entry{}, invalid("entry")
		}
		if f.Action == ArtifactList {
			if r.Kind != ArtifactCollectionResource || r.ID != s.ProjectID {
				return Entry{}, invalid("resource")
			}
		} else if r.Kind != ArtifactResource || r.ID != f.Metadata.artifactID() {
			return Entry{}, invalid("resource")
		}
		if f.Metadata.contentPhase() == FailedPhase {
			if f.Action != ArtifactDownload || f.Outcome != Failed {
				return Entry{}, invalid("outcome")
			}
		} else if f.Outcome != Success {
			return Entry{}, invalid("outcome")
		}
	}
	return Entry{data: func() EntryFields { return f }}, nil
}
func (e Entry) Validate() error {
	if e.data == nil {
		return invalid("entry")
	}
	return nil
}
func (e Entry) Fields() EntryFields {
	if e.data == nil {
		return EntryFields{}
	}
	return e.data()
}
func (e Entry) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "audit_entry") }
func (e Entry) MarshalJSON() ([]byte, error) { return []byte(`"audit_entry"`), nil }
func (e *Entry) UnmarshalJSON([]byte) error  { return invalid("entry") }
func (e Entry) LogValue() slog.Value         { return slog.StringValue("audit_entry") }

type Producer string

const (
	SecretProducer   Producer = "secret"
	MasterProducer   Producer = "secret.master"
	PolicyProducer   Producer = "outbound.policy"
	AccessProducer   Producer = "outbound.access"
	ObjectProducer   Producer = "object"
	ArtifactProducer Producer = "artifact"
)

func (p Producer) Valid() bool {
	return p == SecretProducer || p == MasterProducer || p == PolicyProducer || p == AccessProducer || p == ObjectProducer || p == ArtifactProducer
}
func ProducerFor(action Action) Producer {
	switch action {
	case SecretCreate, SecretUpdate, SecretDelete, SecretResolve:
		return SecretProducer
	case MasterRegister, RotationStart, RotationComplete, RotationFailed:
		return MasterProducer
	case PolicyUpdate:
		return PolicyProducer
	case AccessDeny:
		return AccessProducer
	case ObjectUploadComplete, ObjectUploadFailed, ObjectDelete, ObjectTransferIssue, ObjectTransferComplete, ObjectTransferRevoke:
		return ObjectProducer
	case ArtifactCreate, ArtifactList, ArtifactRead, ArtifactDownload:
		return ArtifactProducer
	}
	return ""
}

type AppendKey struct{ data func() AppendKeyDetails }
type AppendKeyDetails struct {
	Producer Producer
	CauseRef string
	Ordinal  int64
}

func NewAppendKey(producer Producer, causeRef string, ordinal int64) (AppendKey, error) {
	if !producer.Valid() || !identity.ValidCauseRef(causeRef) || ordinal < 0 {
		return AppendKey{}, invalid("append_key")
	}
	d := AppendKeyDetails{producer, causeRef, ordinal}
	return AppendKey{data: func() AppendKeyDetails { return d }}, nil
}
func (k AppendKey) Validate() error {
	if k.data == nil {
		return invalid("append_key")
	}
	return nil
}
func (k AppendKey) Details() AppendKeyDetails {
	if k.data == nil {
		return AppendKeyDetails{}
	}
	return k.data()
}
func (k AppendKey) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "audit_append_key") }
func (k AppendKey) MarshalJSON() ([]byte, error) { return []byte(`"audit_append_key"`), nil }
func (k *AppendKey) UnmarshalJSON([]byte) error  { return invalid("append_key") }
func (k AppendKey) LogValue() slog.Value         { return slog.StringValue("audit_append_key") }

type AppendReceipt struct {
	AuditID   ID                 `json:"audit_id"`
	CreatedAt foundation.Instant `json:"created_at"`
}
type LookupState string

const (
	Committed   LookupState = "committed"
	NotObserved LookupState = "not_observed"
)

type AppendLookup struct {
	State   LookupState    `json:"state"`
	Receipt *AppendReceipt `json:"receipt,omitempty"`
}
type ActorSummary struct {
	Kind        identity.ActorKind   `json:"kind"`
	ID          string               `json:"id,omitempty"`
	ProjectID   string               `json:"project_id,omitempty"`
	ExecutionID string               `json:"execution_id,omitempty"`
	Service     identity.ServiceName `json:"service,omitempty"`
	CauseRef    string               `json:"cause_ref,omitempty"`
}
type SafeRecord struct {
	AppendReceipt
	Scope        identity.Scope `json:"scope"`
	Actor        ActorSummary   `json:"actor"`
	Action       Action         `json:"action"`
	Outcome      Outcome        `json:"outcome"`
	Resource     Resource       `json:"resource"`
	Metadata     Metadata       `json:"metadata"`
	Associations Associations   `json:"associations"`
	Summary      string         `json:"summary"`
}

// Append does not commit. Receipt existence in memory says nothing about the
// outcome of the caller's outer transaction.
type Appender interface {
	AppendInTx(context.Context, foundation.Tx, Entry, AppendKey) (AppendReceipt, error)
}
type Reader interface {
	LookupAppend(context.Context, identity.Actor, identity.Scope, AppendKey, foundation.Digest) (AppendLookup, error)
	List(context.Context, identity.Actor, identity.Scope, Filter, foundation.PageRequest) (foundation.Page[SafeRecord], error)
	Get(context.Context, identity.Actor, identity.Scope, ID) (SafeRecord, error)
}

func validID(s string) bool { _, e := foundation.ParseID[struct{}](s); return e == nil }
func invalid(field string) error {
	f := foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	f.FieldErrors = []foundation.FieldError{{Path: "/" + field, Code: "INVALID"}}
	return f
}
