// Package contract defines Runner management inputs and public durable facts.
// Validation supplies no current administrator or device authorization.
package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type Runner struct{}
type Command struct{}
type IdentityEvent struct{}
type RunnerID = f.ID[Runner]
type CommandID = f.ID[Command]
type IdentityEventID = f.ID[IdentityEvent]

type Status string

const (
	Offline      Status = "offline"
	Online       Status = "online"
	Incompatible Status = "incompatible"
)

// Snapshot deliberately has no raw public key or connection credentials.
// Status is derived by the current service, not trusted from a stored boolean.
type Snapshot struct {
	ID                   RunnerID       `json:"id"`
	Name                 string         `json:"name"`
	Description          string         `json:"description"`
	Tags                 []string       `json:"tags"`
	RootPath             string         `json:"root_path"`
	Version              f.Version      `json:"version"`
	CredentialGeneration f.Version      `json:"credential_generation"`
	Status               Status         `json:"status"`
	PublicKeyFingerprint *f.Digest      `json:"public_key_fingerprint"`
	EnrolledAt           *f.Instant     `json:"enrolled_at"`
	LastSeenAt           *f.Instant     `json:"last_seen_at"`
	LastHello            *HelloSnapshot `json:"last_hello"`
	CreatedAt            f.Instant      `json:"created_at"`
	UpdatedAt            f.Instant      `json:"updated_at"`
}

func (s Snapshot) Validate() error {
	if s.ID.Validate() != nil || !textValid(s.Name, 1, 128) || !textValid(s.Description, 0, 4096) || !tagsValid(s.Tags) || !rootValid(s.RootPath) || s.Version.Validate() != nil || s.CredentialGeneration.Validate() != nil || s.CreatedAt.Validate() != nil || s.UpdatedAt.Validate() != nil || s.UpdatedAt.Time().Before(s.CreatedAt.Time()) {
		return invalid()
	}
	if s.Status != Offline && s.Status != Online && s.Status != Incompatible {
		return invalid()
	}
	if (s.PublicKeyFingerprint == nil) != (s.EnrolledAt == nil) {
		return invalid()
	}
	if s.PublicKeyFingerprint != nil && (s.PublicKeyFingerprint.Validate() != nil || s.EnrolledAt.Validate() != nil || s.EnrolledAt.Time().Before(s.CreatedAt.Time())) {
		return invalid()
	}
	if s.LastSeenAt != nil && (s.LastSeenAt.Validate() != nil || s.LastSeenAt.Time().Before(s.CreatedAt.Time())) {
		return invalid()
	}
	if s.LastHello != nil && (s.LastHello.Validate() != nil || string(s.LastHello.RunnerID) != s.ID.String()) {
		return invalid()
	}
	if s.Status == Online && (s.PublicKeyFingerprint == nil || s.LastHello == nil || s.LastSeenAt == nil) {
		return invalid()
	}
	return nil
}
func (s Snapshot) Clone() Snapshot {
	s.Tags = append([]string{}, s.Tags...)
	s.PublicKeyFingerprint = clone(s.PublicKeyFingerprint)
	s.EnrolledAt = clone(s.EnrolledAt)
	s.LastSeenAt = clone(s.LastSeenAt)
	if s.LastHello != nil {
		h := *s.LastHello
		h.Capabilities = append([]string{}, h.Capabilities...)
		h.FeatureFlags = append([]string{}, h.FeatureFlags...)
		s.LastHello = &h
	}
	return s
}
func (s Snapshot) MarshalJSON() ([]byte, error) { type w Snapshot; return checked(w(s), s.Validate()) }
func (s *Snapshot) UnmarshalJSON(raw []byte) error {
	type w Snapshot
	v, e := decode[w](raw, MaxRecordBytes, []string{"id", "name", "description", "tags", "root_path", "version", "credential_generation", "status", "public_key_fingerprint", "enrolled_at", "last_seen_at", "last_hello", "created_at", "updated_at"}, nil, []string{"public_key_fingerprint", "enrolled_at", "last_seen_at", "last_hello"})
	if e != nil {
		return e
	}
	n := Snapshot(v)
	if e = n.Validate(); e == nil {
		*s = n
	}
	return e
}
func (Snapshot) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "runner_snapshot") }
func (Snapshot) LogValue() slog.Value       { return slog.StringValue("runner_snapshot") }

type Page struct {
	Items []Snapshot `json:"items"`
	Next  *RunnerID  `json:"next"`
}

func (v Page) Validate() error {
	if v.Items == nil || len(v.Items) > 200 {
		return invalid()
	}
	for n, s := range v.Items {
		if s.Validate() != nil || n > 0 && v.Items[n-1].ID.String() >= s.ID.String() {
			return invalid()
		}
	}
	if v.Next != nil && (v.Next.Validate() != nil || len(v.Items) == 0 || *v.Next != v.Items[len(v.Items)-1].ID) {
		return invalid()
	}
	return nil
}
func (v Page) MarshalJSON() ([]byte, error) {
	type w Page
	raw, e := checked(w(v), v.Validate())
	if e != nil {
		return nil, e
	}
	if len(raw) > MaxPageBytes {
		return nil, invalid()
	}
	return raw, nil
}

type ListRequest struct {
	Limit int
	After *RunnerID
}

func (q ListRequest) Validate() error {
	if q.Limit < 1 || q.Limit > 200 || q.After != nil && q.After.Validate() != nil {
		return invalid()
	}
	return nil
}

type Reader interface {
	List(context.Context, id.Actor, ListRequest) (Page, error)
	Get(context.Context, id.Actor, RunnerID) (Snapshot, error)
}

var _ json.Marshaler = Snapshot{}

// HelloSnapshot is the explicit public projection of an already checked hello;
// protocol payloads themselves deliberately redact generic JSON encoding.
type HelloSnapshot p.Hello

func (h HelloSnapshot) Validate() error {
	type w HelloSnapshot
	raw, e := json.Marshal(w(h))
	if e != nil {
		return invalid()
	}
	var protocol p.Hello
	if json.Unmarshal(raw, &protocol) != nil {
		return invalid()
	}
	return nil
}
func (h HelloSnapshot) MarshalJSON() ([]byte, error) {
	type w HelloSnapshot
	return checked(w(h), h.Validate())
}
func (h *HelloSnapshot) UnmarshalJSON(raw []byte) error {
	var v p.Hello
	if json.Unmarshal(raw, &v) != nil {
		return invalid()
	}
	*h = HelloSnapshot(v)
	return nil
}
