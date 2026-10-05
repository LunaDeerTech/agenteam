package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ChangedField string

const (
	ValueChanged   ChangedField = "value"
	PurposeChanged ChangedField = "purpose"
)

type Consumer string

const (
	Model          Consumer = "model"
	MCP            Consumer = "mcp"
	Runner         Consumer = "runner"
	SMTP           Consumer = "smtp"
	ObjectStorage  Consumer = "object_storage"
	SystemConsumer Consumer = "system"
)

func (c Consumer) Valid() bool {
	switch c {
	case Model, MCP, Runner, SMTP, ObjectStorage, SystemConsumer:
		return true
	}
	return false
}

type Reason string

const (
	PermissionDenied   Reason = "permission_denied"
	ScopeMismatch      Reason = "scope_mismatch"
	LeaseInvalid       Reason = "lease_invalid"
	DecryptFailed      Reason = "decrypt_failed"
	KeyUnavailable     Reason = "key_unavailable"
	CiphertextInvalid  Reason = "ciphertext_invalid"
	RotationError      Reason = "rotation_failed"
	InvalidTarget      Reason = "invalid_target"
	AddressForbidden   Reason = "address_forbidden"
	PrivateNotAllowed  Reason = "private_not_allowed"
	PortDenied         Reason = "port_denied"
	HTTPDenied         Reason = "http_denied"
	TLSFailed          Reason = "tls_failed"
	DNSFailed          Reason = "dns_failed"
	RedirectDenied     Reason = "redirect_denied"
	PolicyUnavailable  Reason = "policy_unavailable"
	BindingInvalid     Reason = "binding_invalid"
	ConsumerDenied     Reason = "consumer_denied"
	CredentialDenied   Reason = "credential_denied"
	OriginDenied       Reason = "origin_denied"
	ResponseLimit      Reason = "response_limit"
	Timeout            Reason = "timeout"
	Cancelled          Reason = "cancelled"
	InternalError      Reason = "internal_error"
	StorageUnavailable Reason = "storage_unavailable"
	PayloadMissing     Reason = "payload_missing"
	IntegrityMismatch  Reason = "integrity_mismatch"
)

func (r Reason) Valid() bool {
	switch r {
	case PermissionDenied, ScopeMismatch, LeaseInvalid, DecryptFailed, KeyUnavailable, CiphertextInvalid, RotationError, InvalidTarget, AddressForbidden, PrivateNotAllowed, PortDenied, HTTPDenied, TLSFailed, DNSFailed, RedirectDenied, PolicyUnavailable, BindingInvalid, ConsumerDenied, CredentialDenied, OriginDenied, ResponseLimit, Timeout, Cancelled, InternalError, StorageUnavailable, PayloadMissing, IntegrityMismatch:
		return true
	}
	return false
}

type metadataWire struct {
	Version              string             `json:"version,omitempty"`
	ChangedFields        []ChangedField     `json:"changed_fields,omitempty"`
	LeaseID              string             `json:"lease_id,omitempty"`
	Consumer             Consumer           `json:"consumer,omitempty"`
	Reason               Reason             `json:"reason,omitempty"`
	RotationID           string             `json:"rotation_id,omitempty"`
	Count                string             `json:"count,omitempty"`
	RuleCount            string             `json:"rule_count,omitempty"`
	ObjectID             string             `json:"object_id,omitempty"`
	TransferID           string             `json:"transfer_id,omitempty"`
	ArtifactID           string             `json:"artifact_id,omitempty"`
	InitiatorKind        identity.ActorKind `json:"initiator_kind,omitempty"`
	InitiatorID          string             `json:"initiator_id,omitempty"`
	InitiatorExecutionID string             `json:"initiator_execution_id,omitempty"`
	MediaType            string             `json:"media_type,omitempty"`
	ByteSize             string             `json:"byte_size,omitempty"`
	SentBytes            string             `json:"sent_bytes,omitempty"`
	Phase                ContentPhase       `json:"phase,omitempty"`
	SourceKind           SourceKind         `json:"source_kind,omitempty"`
	SourceID             string             `json:"source_id,omitempty"`
	SourceRevision       string             `json:"source_revision,omitempty"`
	DeliveryID           string             `json:"delivery_id,omitempty"`
	EventID              string             `json:"event_id,omitempty"`
	HandlerID            string             `json:"handler_id,omitempty"`
	FromState            string             `json:"from_state,omitempty"`
	RedriveCycle         string             `json:"redrive_cycle,omitempty"`
	ReasonCode           RequeueReason      `json:"reason_code,omitempty"`
}
type metadataData struct {
	action                     Action
	raw                        string
	rotation                   string
	object, transfer, artifact string
	delivery                   string
	phase                      ContentPhase
}
type Metadata struct{ data func() metadataData }

func metadata(action Action, w metadataWire) (Metadata, error) {
	b, err := json.Marshal(w)
	if err != nil || len(b) > 4096 {
		return Metadata{}, invalid("metadata")
	}
	d := metadataData{action: action, raw: string(b), rotation: w.RotationID, object: w.ObjectID, transfer: w.TransferID, artifact: w.ArtifactID, phase: w.Phase, delivery: w.DeliveryID}
	return Metadata{data: func() metadataData { return d }}, nil
}
func SecretMutationMetadata(action Action, version foundation.Version, changed []ChangedField) (Metadata, error) {
	if action != SecretCreate && action != SecretUpdate && action != SecretDelete || version.Validate() != nil {
		return Metadata{}, invalid("metadata")
	}
	fields := append([]ChangedField{}, changed...)
	for _, f := range fields {
		if f != ValueChanged && f != PurposeChanged {
			return Metadata{}, invalid("metadata")
		}
	}
	slices.Sort(fields)
	fields = slices.Compact(fields)
	if action == SecretDelete && len(fields) != 0 || action != SecretDelete && len(fields) == 0 {
		return Metadata{}, invalid("metadata")
	}
	return metadata(action, metadataWire{Version: version.String(), ChangedFields: fields})
}
func SecretResolveMetadata(leaseID string, consumer Consumer, reason Reason) (Metadata, error) {
	if !validID(leaseID) || !consumer.Valid() || reason != "" && !reason.Valid() {
		return Metadata{}, invalid("metadata")
	}
	return metadata(SecretResolve, metadataWire{LeaseID: leaseID, Consumer: consumer, Reason: reason})
}
func MasterMetadata(action Action, version foundation.Version, rotationID string, count foundation.Progress, reason Reason) (Metadata, error) {
	if version.Validate() != nil || count.Validate() != nil {
		return Metadata{}, invalid("metadata")
	}
	if action == MasterRegister {
		if rotationID != "" || count != 0 || reason != "" {
			return Metadata{}, invalid("metadata")
		}
		return metadata(action, metadataWire{Version: version.String()})
	}
	if action != RotationStart && action != RotationComplete && action != RotationFailed || !validID(rotationID) || action == RotationFailed && !reason.Valid() || action != RotationFailed && reason != "" {
		return Metadata{}, invalid("metadata")
	}
	return metadata(action, metadataWire{Version: version.String(), RotationID: rotationID, Count: count.String(), Reason: reason})
}
func PolicyMetadata(version foundation.Version, ruleCount int) (Metadata, error) {
	if version.Validate() != nil || ruleCount < 0 || ruleCount > 256 {
		return Metadata{}, invalid("metadata")
	}
	return metadata(PolicyUpdate, metadataWire{Version: version.String(), RuleCount: foundation.Progress(ruleCount).String()})
}
func DenialMetadata(consumer Consumer, reason Reason, policyVersion foundation.Version) (Metadata, error) {
	if !consumer.Valid() || !reason.Valid() || policyVersion.Validate() != nil {
		return Metadata{}, invalid("metadata")
	}
	return metadata(AccessDeny, metadataWire{Consumer: consumer, Reason: reason, Version: policyVersion.String()})
}

// Content metadata deliberately excludes names, descriptions, paths, storage
// locators, URLs, signatures and bodies. Its decimal counters are JSON strings.
type ContentPhase string

const (
	PublishedPhase ContentPhase = "published"
	DeletedPhase   ContentPhase = "deleted"
	IssuedPhase    ContentPhase = "issued"
	StartedPhase   ContentPhase = "started"
	SentPhase      ContentPhase = "sent"
	FailedPhase    ContentPhase = "failed"
	RevokedPhase   ContentPhase = "revoked"
	ReadPhase      ContentPhase = "read"
	ListedPhase    ContentPhase = "listed"
)

type SourceKind string

const (
	InlineSource    SourceKind = "inline"
	UploadSource    SourceKind = "uploaded_object"
	ArtifactSource  SourceKind = "artifact_file"
	KnowledgeSource SourceKind = "knowledge_file"
	ExecutionSource SourceKind = "execution_file"
)

type ObjectMetadataFields struct {
	ObjectID, TransferID              string
	InitiatorKind                     identity.ActorKind
	InitiatorID, InitiatorExecutionID string
	MediaType                         string
	ByteSize, SentBytes               foundation.Progress
	Phase                             ContentPhase
	Reason                            Reason
}
type ArtifactMetadataFields struct {
	ArtifactID, ObjectID       string
	SourceKind                 SourceKind
	SourceID                   string
	SourceRevision             foundation.Version // optional; zero means no supplied revision
	MediaType                  string
	ByteSize, SentBytes, Count foundation.Progress
	Phase                      ContentPhase
	Reason                     Reason
}

func canonicalMediaType(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	kind, parameters, err := mime.ParseMediaType(value)
	if err != nil || mime.FormatMediaType(kind, parameters) != value {
		return false
	}
	// MIME parameters are not generally safe metadata: name, filename and
	// arbitrary extensions can contain user text or credentials. Keep only a
	// closed charset vocabulary; callers may project other MIME to its base.
	for name, parameter := range parameters {
		if name != "charset" || parameter != "utf-8" && parameter != "us-ascii" {
			return false
		}
	}
	return true
}

func (m Metadata) contentPhase() ContentPhase {
	if m.data == nil {
		return ""
	}
	return m.data().phase
}
func ObjectMetadata(action Action, f ObjectMetadataFields) (Metadata, error) {
	if !validID(f.ObjectID) || !validID(f.InitiatorID) || !canonicalMediaType(f.MediaType) || f.ByteSize.Validate() != nil || f.SentBytes.Validate() != nil || f.SentBytes > f.ByteSize || f.Reason != "" && !f.Reason.Valid() {
		return Metadata{}, invalid("metadata")
	}
	if f.InitiatorKind != identity.Human && f.InitiatorKind != identity.AgentRun && f.InitiatorKind != identity.Service || f.InitiatorKind == identity.AgentRun && !validID(f.InitiatorExecutionID) || f.InitiatorKind != identity.AgentRun && f.InitiatorExecutionID != "" {
		return Metadata{}, invalid("metadata")
	}
	transfer := action == ObjectTransferIssue || action == ObjectTransferComplete || action == ObjectTransferRevoke
	if transfer && !validID(f.TransferID) || !transfer && f.TransferID != "" {
		return Metadata{}, invalid("metadata")
	}
	valid := false
	switch action {
	case ObjectUploadComplete:
		valid = f.Phase == PublishedPhase && f.Reason == ""
	case ObjectUploadFailed:
		valid = f.Phase == FailedPhase && f.Reason != ""
	case ObjectDelete:
		valid = f.Phase == DeletedPhase && f.Reason == ""
	case ObjectTransferIssue:
		valid = f.Phase == IssuedPhase && f.Reason == ""
	case ObjectTransferComplete:
		valid = f.Phase == SentPhase && f.SentBytes == f.ByteSize && f.Reason == ""
	case ObjectTransferRevoke:
		valid = f.Phase == RevokedPhase
	}
	if !valid {
		return Metadata{}, invalid("metadata")
	}
	return metadata(action, metadataWire{ObjectID: f.ObjectID, TransferID: f.TransferID, InitiatorKind: f.InitiatorKind, InitiatorID: f.InitiatorID, InitiatorExecutionID: f.InitiatorExecutionID, MediaType: f.MediaType, ByteSize: f.ByteSize.String(), SentBytes: f.SentBytes.String(), Phase: f.Phase, Reason: f.Reason})
}
func ArtifactMetadata(action Action, f ArtifactMetadataFields) (Metadata, error) {
	if f.Count.Validate() != nil || f.ByteSize.Validate() != nil || f.SentBytes.Validate() != nil || f.SentBytes > f.ByteSize || f.Reason != "" && !f.Reason.Valid() {
		return Metadata{}, invalid("metadata")
	}
	if action == ArtifactList {
		if f.Phase != ListedPhase || f.ArtifactID != "" || f.ObjectID != "" || f.SourceKind != "" || f.SourceID != "" || f.SourceRevision != 0 || f.MediaType != "" || f.ByteSize != 0 || f.SentBytes != 0 || f.Reason != "" {
			return Metadata{}, invalid("metadata")
		}
		return metadata(action, metadataWire{Phase: f.Phase, Count: f.Count.String()})
	}
	if !validID(f.ArtifactID) || !validID(f.ObjectID) || !canonicalMediaType(f.MediaType) || f.Count != 0 {
		return Metadata{}, invalid("metadata")
	}
	var revision string
	if f.SourceRevision != 0 {
		if f.SourceRevision.Validate() != nil {
			return Metadata{}, invalid("metadata")
		}
		revision = f.SourceRevision.String()
	}
	switch f.SourceKind {
	case "", InlineSource:
		if f.SourceID != "" || revision != "" {
			return Metadata{}, invalid("metadata")
		}
	case UploadSource, ArtifactSource, KnowledgeSource, ExecutionSource:
		if !validID(f.SourceID) {
			return Metadata{}, invalid("metadata")
		}
	default:
		return Metadata{}, invalid("metadata")
	}
	valid := false
	switch action {
	case ArtifactCreate:
		valid = f.Phase == PublishedPhase && f.SourceKind != "" && f.Reason == "" && f.SentBytes == 0
	case ArtifactRead:
		valid = f.Phase == ReadPhase && f.Reason == ""
	case ArtifactDownload:
		valid = (f.Phase == IssuedPhase || f.Phase == StartedPhase || f.Phase == SentPhase || f.Phase == FailedPhase) && (f.Phase == FailedPhase) == (f.Reason != "")
	}
	if !valid {
		return Metadata{}, invalid("metadata")
	}
	return metadata(action, metadataWire{ArtifactID: f.ArtifactID, ObjectID: f.ObjectID, SourceKind: f.SourceKind, SourceID: f.SourceID, SourceRevision: revision, MediaType: f.MediaType, ByteSize: f.ByteSize.String(), SentBytes: f.SentBytes.String(), Phase: f.Phase, Reason: f.Reason})
}
func (m Metadata) objectID() string {
	if m.data == nil {
		return ""
	}
	return m.data().object
}
func (m Metadata) transferID() string {
	if m.data == nil {
		return ""
	}
	return m.data().transfer
}
func (m Metadata) artifactID() string {
	if m.data == nil {
		return ""
	}
	return m.data().artifact
}
func (m Metadata) Validate(action Action) error {
	if m.data == nil || m.data().action != action {
		return invalid("metadata")
	}
	return nil
}
func (m Metadata) JSON() []byte {
	if m.data == nil {
		return []byte(`{}`)
	}
	return []byte(m.data().raw)
}
func (m Metadata) rotationID() string {
	if m.data == nil {
		return ""
	}
	return m.data().rotation
}
func (m Metadata) deliveryID() string {
	if m.data == nil {
		return ""
	}
	return m.data().delivery
}
func (m Metadata) MarshalJSON() ([]byte, error) { return m.JSON(), nil }
func (m *Metadata) UnmarshalJSON([]byte) error  { return invalid("metadata") }
func (m Metadata) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "audit_metadata") }
func (m Metadata) LogValue() slog.Value         { return slog.StringValue("audit_metadata") }

// DecodeMetadata is a repository boundary, not an untyped event input. Its
// closed action schema revalidates even database JSON before a safe projection.
func DecodeMetadata(action Action, raw []byte) (Metadata, error) {
	if len(raw) > 4096 {
		return Metadata{}, invalid("metadata")
	}
	if AccountAction(action) {
		return decodeAccountMetadata(action, raw)
	}
	if ProjectAction(action) {
		return decodeProjectMetadata(action, raw)
	}
	allowed := map[string]bool{}
	var names []string
	switch action {
	case OutboxDeliveryRequeue:
		names = []string{"delivery_id", "event_id", "handler_id", "from_state", "redrive_cycle", "reason_code"}
	case SecretCreate, SecretUpdate:
		names = []string{"version", "changed_fields"}
	case SecretDelete:
		names = []string{"version"}
	case SecretResolve:
		names = []string{"lease_id", "consumer", "reason"}
	case MasterRegister:
		names = []string{"version"}
	case RotationStart, RotationComplete:
		names = []string{"version", "rotation_id", "count"}
	case RotationFailed:
		names = []string{"version", "rotation_id", "count", "reason"}
	case PolicyUpdate:
		names = []string{"version", "rule_count"}
	case AccessDeny:
		names = []string{"consumer", "reason", "version"}
	case ObjectUploadComplete, ObjectUploadFailed, ObjectDelete, ObjectTransferIssue, ObjectTransferComplete, ObjectTransferRevoke:
		names = []string{"object_id", "transfer_id", "initiator_kind", "initiator_id", "initiator_execution_id", "media_type", "byte_size", "sent_bytes", "phase", "reason"}
	case ArtifactCreate, ArtifactRead, ArtifactDownload:
		names = []string{"artifact_id", "object_id", "source_kind", "source_id", "source_revision", "media_type", "byte_size", "sent_bytes", "phase", "reason"}
	case ArtifactList:
		names = []string{"phase", "count"}
	default:
		return Metadata{}, invalid("metadata")
	}
	for _, name := range names {
		allowed[name] = true
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := map[string]bool{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || !allowed[name] || seen[name] {
			return Metadata{}, invalid("metadata")
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	if last, err := d.Token(); err != nil || last != json.Delim('}') {
		return Metadata{}, invalid("metadata")
	}
	if d.Decode(new(any)) != io.EOF {
		return Metadata{}, invalid("metadata")
	}
	var w metadataWire
	if json.Unmarshal(raw, &w) != nil {
		return Metadata{}, invalid("metadata")
	}
	if action == OutboxDeliveryRequeue {
		cycle, err := foundation.ParseVersion(w.RedriveCycle)
		if err != nil {
			return Metadata{}, invalid("metadata")
		}
		return OutboxRequeueMetadata(OutboxRequeueFields{DeliveryID: w.DeliveryID, EventID: w.EventID, HandlerID: w.HandlerID, FromState: w.FromState, RedriveCycle: cycle, Reason: w.ReasonCode})
	}
	if ProducerFor(action) == ObjectProducer {
		size, se := foundation.ParseProgress(w.ByteSize)
		sent, te := foundation.ParseProgress(w.SentBytes)
		if se != nil || te != nil {
			return Metadata{}, invalid("metadata")
		}
		return ObjectMetadata(action, ObjectMetadataFields{ObjectID: w.ObjectID, TransferID: w.TransferID, InitiatorKind: w.InitiatorKind, InitiatorID: w.InitiatorID, InitiatorExecutionID: w.InitiatorExecutionID, MediaType: w.MediaType, ByteSize: size, SentBytes: sent, Phase: w.Phase, Reason: w.Reason})
	}
	if ProducerFor(action) == ArtifactProducer {
		var size, sent, count foundation.Progress
		var revision foundation.Version
		var e error
		if action == ArtifactList {
			count, e = foundation.ParseProgress(w.Count)
		} else {
			size, e = foundation.ParseProgress(w.ByteSize)
			if e == nil {
				sent, e = foundation.ParseProgress(w.SentBytes)
			}
		}
		if e == nil && w.SourceRevision != "" {
			revision, e = foundation.ParseVersion(w.SourceRevision)
		}
		if e != nil {
			return Metadata{}, invalid("metadata")
		}
		return ArtifactMetadata(action, ArtifactMetadataFields{ArtifactID: w.ArtifactID, ObjectID: w.ObjectID, SourceKind: w.SourceKind, SourceID: w.SourceID, SourceRevision: revision, MediaType: w.MediaType, ByteSize: size, SentBytes: sent, Count: count, Phase: w.Phase, Reason: w.Reason})
	}
	version, versionErr := foundation.ParseVersion(w.Version)
	if action != SecretResolve && versionErr != nil {
		return Metadata{}, invalid("metadata")
	}
	switch action {
	case SecretCreate, SecretUpdate, SecretDelete:
		return SecretMutationMetadata(action, version, w.ChangedFields)
	case SecretResolve:
		return SecretResolveMetadata(w.LeaseID, w.Consumer, w.Reason)
	case MasterRegister:
		return MasterMetadata(action, version, "", 0, "")
	case RotationStart, RotationComplete, RotationFailed:
		count, e := foundation.ParseProgress(w.Count)
		if e != nil {
			return Metadata{}, invalid("metadata")
		}
		return MasterMetadata(action, version, w.RotationID, count, w.Reason)
	case PolicyUpdate:
		count, e := foundation.ParseProgress(w.RuleCount)
		if e != nil || count > 256 {
			return Metadata{}, invalid("metadata")
		}
		return PolicyMetadata(version, int(count))
	case AccessDeny:
		return DenialMetadata(w.Consumer, w.Reason, version)
	}
	return Metadata{}, invalid("metadata")
}

type RequeueReason string

const (
	OperatorRetry      RequeueReason = "operator_retry"
	SchemaAvailable    RequeueReason = "schema_available"
	DependencyRestored RequeueReason = "dependency_restored"
)

func (r RequeueReason) Valid() bool {
	return r == OperatorRetry || r == SchemaAvailable || r == DependencyRestored
}

type OutboxRequeueFields struct {
	DeliveryID, EventID, HandlerID, FromState string
	RedriveCycle                              foundation.Version
	Reason                                    RequeueReason
}

func OutboxRequeueMetadata(f OutboxRequeueFields) (Metadata, error) {
	if !validID(f.DeliveryID) || !validID(f.EventID) || !auditHandlerName(f.HandlerID) || (f.FromState != "failed" && f.FromState != "dead_letter") || f.RedriveCycle.Validate() != nil || !f.Reason.Valid() {
		return Metadata{}, invalid("metadata")
	}
	return metadata(OutboxDeliveryRequeue, metadataWire{DeliveryID: f.DeliveryID, EventID: f.EventID, HandlerID: f.HandlerID, FromState: f.FromState, RedriveCycle: f.RedriveCycle.String(), ReasonCode: f.Reason})
}

func auditHandlerName(name string) bool {
	if len(name) == 0 || len(name) > 128 {
		return false
	}
	for i, c := range []byte(name) {
		if !(c >= 'a' && c <= 'z' || i > 0 && (c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.')) {
			return false
		}
	}
	return true
}
