package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
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
	PermissionDenied  Reason = "permission_denied"
	ScopeMismatch     Reason = "scope_mismatch"
	LeaseInvalid      Reason = "lease_invalid"
	DecryptFailed     Reason = "decrypt_failed"
	KeyUnavailable    Reason = "key_unavailable"
	CiphertextInvalid Reason = "ciphertext_invalid"
	RotationError     Reason = "rotation_failed"
	InvalidTarget     Reason = "invalid_target"
	AddressForbidden  Reason = "address_forbidden"
	PrivateNotAllowed Reason = "private_not_allowed"
	PortDenied        Reason = "port_denied"
	HTTPDenied        Reason = "http_denied"
	TLSFailed         Reason = "tls_failed"
	DNSFailed         Reason = "dns_failed"
	RedirectDenied    Reason = "redirect_denied"
	PolicyUnavailable Reason = "policy_unavailable"
	BindingInvalid    Reason = "binding_invalid"
	ConsumerDenied    Reason = "consumer_denied"
	CredentialDenied  Reason = "credential_denied"
	OriginDenied      Reason = "origin_denied"
	ResponseLimit     Reason = "response_limit"
	Timeout           Reason = "timeout"
	Cancelled         Reason = "cancelled"
	InternalError     Reason = "internal_error"
)

func (r Reason) Valid() bool {
	switch r {
	case PermissionDenied, ScopeMismatch, LeaseInvalid, DecryptFailed, KeyUnavailable, CiphertextInvalid, RotationError, InvalidTarget, AddressForbidden, PrivateNotAllowed, PortDenied, HTTPDenied, TLSFailed, DNSFailed, RedirectDenied, PolicyUnavailable, BindingInvalid, ConsumerDenied, CredentialDenied, OriginDenied, ResponseLimit, Timeout, Cancelled, InternalError:
		return true
	}
	return false
}

type metadataWire struct {
	Version       string         `json:"version,omitempty"`
	ChangedFields []ChangedField `json:"changed_fields,omitempty"`
	LeaseID       string         `json:"lease_id,omitempty"`
	Consumer      Consumer       `json:"consumer,omitempty"`
	Reason        Reason         `json:"reason,omitempty"`
	RotationID    string         `json:"rotation_id,omitempty"`
	Count         string         `json:"count,omitempty"`
	RuleCount     string         `json:"rule_count,omitempty"`
}
type metadataData struct {
	action   Action
	raw      string
	rotation string
}
type Metadata struct{ data func() metadataData }

func metadata(action Action, w metadataWire) (Metadata, error) {
	b, err := json.Marshal(w)
	if err != nil || len(b) > 4096 {
		return Metadata{}, invalid("metadata")
	}
	d := metadataData{action, string(b), w.RotationID}
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
	allowed := map[string]bool{}
	var names []string
	switch action {
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
