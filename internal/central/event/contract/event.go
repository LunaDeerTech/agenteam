// Package contract contains neutral, typed domain-event values. It depends on
// foundation only; identity and authorization belong to outbox/contract.
package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

const MaxPayloadBytes = 64 << 10

type EventIdentity struct{}
type Project struct{}
type Aggregate struct{}
type EventID = foundation.ID[EventIdentity]
type ProjectID = foundation.ID[Project]
type AggregateID = foundation.ID[Aggregate]
type StableName string

func (n StableName) Validate() error {
	if len(n) == 0 || len(n) > 128 || strings.TrimSpace(string(n)) != string(n) {
		return invalid()
	}
	for i, c := range []byte(n) {
		if !(c >= 'a' && c <= 'z' || i > 0 && (c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.')) {
			return invalid()
		}
	}
	return nil
}

type ScopeKind string

const (
	SystemScope  ScopeKind = "system"
	ProjectScope ScopeKind = "project"
)

type Scope struct {
	Kind      ScopeKind `json:"kind"`
	ProjectID ProjectID `json:"project_id,omitempty"`
}

func (s Scope) Validate() error {
	if s.Kind == SystemScope && s.ProjectID == (ProjectID{}) || s.Kind == ProjectScope && s.ProjectID.Validate() == nil {
		return nil
	}
	return invalid()
}

func (s Scope) MarshalJSON() ([]byte, error) {
	if s.Validate() != nil {
		return nil, invalid()
	}
	project := ""
	if s.Kind == ProjectScope {
		project = s.ProjectID.String()
	}
	return json.Marshal(struct {
		Kind    ScopeKind `json:"kind"`
		Project string    `json:"project_id,omitempty"`
	}{s.Kind, project})
}
func (s *Scope) UnmarshalJSON(raw []byte) error {
	if exactFields(raw, []string{"kind"}, []string{"project_id"}) != nil {
		return invalid()
	}
	var wire struct {
		Kind    ScopeKind `json:"kind"`
		Project string    `json:"project_id,omitempty"`
	}
	if decodeStrict(raw, &wire) != nil {
		return invalid()
	}
	value := Scope{Kind: wire.Kind}
	if wire.Project != "" {
		var err error
		value.ProjectID, err = foundation.ParseID[Project](wire.Project)
		if err != nil {
			return invalid()
		}
	}
	if value.Validate() != nil {
		return invalid()
	}
	*s = value
	return nil
}

type Header struct {
	EventID           EventID              `json:"event_id"`
	EventType         StableName           `json:"event_type"`
	SchemaVersion     uint32               `json:"schema_version"`
	OccurredAt        foundation.Instant   `json:"occurred_at"`
	Scope             Scope                `json:"scope"`
	AggregateType     StableName           `json:"aggregate_type"`
	AggregateID       AggregateID          `json:"aggregate_id"`
	AggregateVersion  *foundation.Version  `json:"aggregate_version,omitempty"`
	AggregateSequence *foundation.Sequence `json:"aggregate_sequence,omitempty"`
}

func (h Header) Validate() error {
	if h.EventID.Validate() != nil || h.EventType.Validate() != nil || h.SchemaVersion == 0 || h.OccurredAt.Validate() != nil || h.Scope.Validate() != nil || h.AggregateType.Validate() != nil || h.AggregateID.Validate() != nil || h.AggregateVersion == nil && h.AggregateSequence == nil {
		return invalid()
	}
	if h.AggregateVersion != nil && h.AggregateVersion.Validate() != nil || h.AggregateSequence != nil && h.AggregateSequence.Validate() != nil {
		return invalid()
	}
	return nil
}

func cloneHeader(h Header) Header {
	if h.AggregateVersion != nil {
		v := *h.AggregateVersion
		h.AggregateVersion = &v
	}
	if h.AggregateSequence != nil {
		v := *h.AggregateSequence
		h.AggregateSequence = &v
	}
	return h
}

// DecodeHeader is the strict persisted/wire boundary, including duplicate keys
// and foundation's canonical positive decimal string scalars.
func DecodeHeader(raw []byte) (Header, error) {
	if exactFields(raw, []string{"event_id", "event_type", "schema_version", "occurred_at", "scope", "aggregate_type", "aggregate_id"}, []string{"aggregate_version", "aggregate_sequence"}) != nil {
		return Header{}, invalid()
	}
	var h Header
	if err := decodeStrict(raw, &h); err != nil || h.Validate() != nil {
		return Header{}, invalid()
	}
	return h, nil
}

type Summary struct {
	Producer      StableName
	Header        Header
	PayloadDigest foundation.Digest
}

type eventData struct {
	issuer   *catalogState
	header   Header
	producer StableName
	payload  []byte
	digest   foundation.Digest
}

// Explicit Header/Summary/PayloadBytes access is for trusted repositories and
// domain adapters. Implicit formatting never exposes even nested payloads.
type Event struct{ data func() eventData }

func (e Event) Validate() error {
	if e.data == nil {
		return invalid()
	}
	return nil
}
func (e Event) Header() Header {
	if e.data == nil {
		return Header{}
	}
	return cloneHeader(e.data().header)
}
func (e Event) Summary() Summary {
	if e.data == nil {
		return Summary{}
	}
	d := e.data()
	return Summary{d.producer, cloneHeader(d.header), d.digest}
}
func (e Event) PayloadBytes() []byte {
	if e.data == nil {
		return nil
	}
	return append([]byte(nil), e.data().payload...)
}
func (e Event) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "domain_event") }
func (e Event) MarshalJSON() ([]byte, error) { return []byte(`"domain_event"`), nil }
func (*Event) UnmarshalJSON([]byte) error    { return invalid() }
func (e Event) LogValue() slog.Value         { return slog.StringValue("domain_event") }

func invalid() error { return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted) }
func unsupported() error {
	return foundation.NewFault(foundation.SchemaUnsupported, foundation.NotStarted)
}

// HeaderJSON is an explicit canonical projection; the general Event marshaler
// deliberately cannot be used as the semantic digest input.
func (e Event) HeaderJSON() ([]byte, error) {
	if e.Validate() != nil {
		return nil, invalid()
	}
	return json.Marshal(e.Header())
}
