// Package contract defines the Agent-scoped Memory domain. Namespace values
// identify a boundary; only a current domain authority can grant access to it.
package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	MaxContentBytes = 32768
	MaxMemoryBytes  = 8192
	MaxCandidates   = 16
	MaxRelated      = 32
	MaxActions      = 48
	MaxTags         = 16
	MaxQueryBytes   = 4096
	MaxRecall       = 32
)

type Memory struct{}
type ID = f.ID[Memory]
type Namespace struct {
	ProjectID i.ProjectID `json:"project_id"`
	AgentID   i.AgentID   `json:"agent_id"`
}

func (n Namespace) Validate() error {
	if n.ProjectID.Validate() != nil || n.AgentID.Validate() != nil {
		return invalid()
	}
	return nil
}

type Type string

const (
	Fact        Type = "fact"
	Experience  Type = "experience"
	Procedure   Type = "procedure"
	Observation Type = "observation"
)

func (t Type) Valid() bool { return t == Fact || t == Experience || t == Procedure || t == Observation }

type Importance string

const (
	Low    Importance = "low"
	Normal Importance = "normal"
	High   Importance = "high"
)

func (v Importance) Valid() bool { return v == Low || v == Normal || v == High }

type Status string

const (
	Active     Status = "active"
	Superseded Status = "superseded"
	Deleted    Status = "deleted"
)

type Content struct {
	Type       Type       `json:"type"`
	Text       string     `json:"text"`
	Tags       []string   `json:"tags"`
	Importance Importance `json:"importance"`
}

func (v Content) Validate() error {
	if !v.Type.Valid() || !v.Importance.Valid() || !validText(v.Text, MaxMemoryBytes) || v.Tags == nil || len(v.Tags) > MaxTags {
		return invalid()
	}
	seen := map[string]bool{}
	for _, tag := range v.Tags {
		if !validText(tag, 64) || strings.TrimSpace(tag) != tag || seen[tag] {
			return invalid()
		}
		seen[tag] = true
	}
	return nil
}
func (v Content) Clone() Content           { v.Tags = append([]string{}, v.Tags...); return v }
func (Content) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "memory_content") }
func (Content) LogValue() slog.Value       { return slog.StringValue("memory_content") }

// Provenance is supplied by the server. Model source hints never override the
// original Execution and Tool Operation identities.
type Provenance struct {
	ExecutionID i.ExecutionID `json:"execution_id"`
	OperationID string        `json:"operation_id"`
	DerivedFrom []ID          `json:"derived_from_memory_ids"`
}

func (v Provenance) Validate() error {
	if v.ExecutionID.Validate() != nil || !uuid(v.OperationID) || v.DerivedFrom == nil || len(v.DerivedFrom) > MaxRelated {
		return invalid()
	}
	seen := map[ID]bool{}
	for _, id := range v.DerivedFrom {
		if id.Validate() != nil || seen[id] {
			return invalid()
		}
		seen[id] = true
	}
	return nil
}

type Revision struct {
	MemoryID ID        `json:"memory_id"`
	Revision f.Version `json:"revision"`
	Status   Status    `json:"status"`
	Content
	Provenance Provenance `json:"provenance"`
	CreatedAt  f.Instant  `json:"created_at"`
}

func (v Revision) Validate() error {
	if v.MemoryID.Validate() != nil || v.Revision.Validate() != nil || v.Status != Active && v.Status != Superseded && v.Status != Deleted || v.Content.Validate() != nil || v.Provenance.Validate() != nil || v.CreatedAt.Validate() != nil {
		return invalid()
	}
	return nil
}

type Record struct {
	Namespace Namespace `json:"namespace"`
	Current   Revision  `json:"current"`
	UpdatedAt f.Instant `json:"updated_at"`
}

func (v Record) Validate() error {
	if v.Namespace.Validate() != nil || v.Current.Validate() != nil || v.Current.Status == Superseded || v.UpdatedAt.Validate() != nil || v.UpdatedAt.Time().Before(v.Current.CreatedAt.Time()) {
		return invalid()
	}
	return nil
}

type Target struct {
	MemoryID ID        `json:"memory_id"`
	Revision f.Version `json:"revision"`
}

func (v Target) Validate() error {
	if v.MemoryID.Validate() != nil || v.Revision.Validate() != nil {
		return invalid()
	}
	return nil
}

func invalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func validText(s string, max int) bool {
	return len(s) > 0 && len(s) <= max && utf8.ValidString(s) && strings.TrimSpace(s) != "" && !strings.ContainsRune(s, '\x00')
}
func uuid(s string) bool { _, e := f.ParseID[struct{}](s); return e == nil }

// Decode rejects unknown/duplicate fields, nulls, invalid UTF-8, and trailing
// values before the standard decoder sees model- or browser-controlled JSON.
func Decode(raw []byte, out any, max int) error {
	if len(raw) == 0 || len(raw) > max || !utf8.Valid(raw) {
		return invalid()
	}
	if err := strictJSON(raw); err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return invalid()
	}
	return nil
}
