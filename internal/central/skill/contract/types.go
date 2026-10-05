// Package contract defines pure Skill values and read ports. Constructing them
// does not authenticate an actor or publish a Skill, object, or Project.
package contract

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

type SkillID = pc.SkillID
type Revision struct{}
type RevisionID = f.ID[Revision]

const MaxNameBytes = 128
const MaxDescriptionBytes = 8192

func invalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }

// NormalizeName derives only the library name key; it never rewrites content.
// This does not establish the protected identity of the builtin Skill.
func NormalizeName(name string) (string, error) {
	if !displayText(name, MaxNameBytes, false) || strings.TrimSpace(name) != name || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return "", invalid()
	}
	key := cases.Fold().String(norm.NFC.String(strings.Join(strings.Fields(name), "-")))
	if len(key) == 0 || len(key) > MaxNameBytes*3 {
		return "", invalid()
	}
	return key, nil
}
func displayText(s string, max int, multiline bool) bool {
	if len(s) == 0 || len(s) > max || !utf8.ValidString(s) || strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\t')) {
			return false
		}
	}
	return true
}

type Metadata struct {
	ID              SkillID      `json:"id"`
	ProjectID       id.ProjectID `json:"project_id"`
	Name            string       `json:"name"`
	NormalizedName  string       `json:"normalized_name"`
	Description     string       `json:"description"`
	Protected       bool         `json:"protected"`
	CurrentRevision f.Revision   `json:"current_revision"`
	Version         f.Version    `json:"version"`
}

func (m Metadata) Validate() error {
	key, e := NormalizeName(m.Name)
	if e != nil || key != m.NormalizedName || !displayText(m.Description, MaxDescriptionBytes, true) || m.ID.Validate() != nil || m.ProjectID.Validate() != nil || m.CurrentRevision.Validate() != nil || m.Version.Validate() != nil {
		return invalid()
	}
	return nil
}
func NewMetadata(v Metadata) (Metadata, error) {
	if e := v.Validate(); e != nil {
		return Metadata{}, e
	}
	return v, nil
}
func (m Metadata) MarshalJSON() ([]byte, error) {
	if e := m.Validate(); e != nil {
		return nil, e
	}
	type wire Metadata
	return json.Marshal(wire(m))
}
func (m *Metadata) UnmarshalJSON(raw []byte) error {
	if strictObject(raw, 6*(MaxDescriptionBytes+4*MaxNameBytes)+2048, []string{"id", "project_id", "name", "normalized_name", "description", "protected", "current_revision", "version"}) != nil {
		return invalid()
	}
	type wire Metadata
	var v wire
	if json.Unmarshal(raw, &v) != nil || Metadata(v).Validate() != nil {
		return invalid()
	}
	*m = Metadata(v)
	return nil
}

type RevisionMetadata struct {
	ID            RevisionID    `json:"id"`
	SkillID       SkillID       `json:"skill_id"`
	ProjectID     id.ProjectID  `json:"project_id"`
	Revision      f.Revision    `json:"revision"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	EntryPath     string        `json:"entry_path"`
	Object        oc.ObjectMeta `json:"object"`
	Manifest      Manifest      `json:"manifest"`
	PackageSHA256 f.Digest      `json:"package_sha256"`
	PublishedAt   f.Instant     `json:"published_at"`
}

func (m RevisionMetadata) Validate() error {
	_, err := NormalizeName(m.Name)
	if err != nil || !displayText(m.Description, MaxDescriptionBytes, true) || m.ID.Validate() != nil || m.SkillID.Validate() != nil || m.ProjectID.Validate() != nil || m.Revision.Validate() != nil || m.EntryPath != EntryPath || m.Manifest.Validate() != nil || m.Object.Validate() != nil || m.Object.State != oc.Available || m.Object.Scope.Details().Kind != id.ProjectScope || m.Object.Scope.Details().ProjectID != m.ProjectID.String() || m.Object.MediaType != PackageMediaType || m.Object.ByteSize <= 0 || int64(m.Object.ByteSize) > MaxArchiveBytes || m.PackageSHA256.Validate() != nil || m.PackageSHA256 != m.Object.SHA256 || m.PublishedAt.Validate() != nil || m.PublishedAt.Time().Before(m.Object.CreatedAt.Time()) {
		return invalid()
	}
	return nil
}
func NewRevisionMetadata(v RevisionMetadata) (RevisionMetadata, error) {
	if e := v.Validate(); e != nil {
		return RevisionMetadata{}, e
	}
	return v, nil
}
func (m RevisionMetadata) MarshalJSON() ([]byte, error) {
	if e := m.Validate(); e != nil {
		return nil, e
	}
	type wire RevisionMetadata
	return json.Marshal(wire(m))
}

// Object Scope is a trusted typed value. Restore rows through the constructors;
// public JSON cannot manufacture its authority or overwrite an existing value.
func (*RevisionMetadata) UnmarshalJSON([]byte) error { return invalid() }

// strictObject checks exact field spelling, duplicates, explicit presence and
// null before ordinary typed decoding. Nested File values perform the same check.
func strictObject(raw []byte, max int, names []string) error {
	if len(raw) > max || !utf8.Valid(raw) {
		return invalid()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, e := d.Token()
	if e != nil || first != json.Delim('{') {
		return invalid()
	}
	allowed := make(map[string]bool, len(names))
	for _, n := range names {
		allowed[n] = true
	}
	seen := make(map[string]bool, len(names))
	for d.More() {
		token, e := d.Token()
		name, ok := token.(string)
		if e != nil || !ok || !allowed[name] || seen[name] {
			return invalid()
		}
		seen[name] = true
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return invalid()
		}
	}
	end, e := d.Token()
	if e != nil || end != json.Delim('}') || len(seen) != len(names) || d.Decode(new(any)) != io.EOF {
		return invalid()
	}
	return nil
}
