package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// DirectoryEntry is the explicit current Human Owner directory projection.
// It is neither AgentConfig, a runtime/busy observation, nor an authorization.
// DisplayName remains nullable; a UI falls back to Name without changing identity.
type DirectoryEntry struct {
	ID          i.AgentID   `json:"id"`
	ProjectID   i.ProjectID `json:"project_id"`
	Name        string      `json:"name"`
	DisplayName *string     `json:"display_name"`
	TagColor    *string     `json:"tag_color"`
	Description string      `json:"description"`
	Version     f.Version   `json:"version"`
	CreatedAt   f.Instant   `json:"created_at"`
	UpdatedAt   f.Instant   `json:"updated_at"`
}

const MaxDirectoryEntryBytes = 64 << 10

func (v DirectoryEntry) Validate() error {
	if v.ID.Validate() != nil || v.ProjectID.Validate() != nil || v.Version.Validate() != nil || v.CreatedAt.Validate() != nil || v.UpdatedAt.Validate() != nil || v.UpdatedAt.Time().Before(v.CreatedAt.Time()) || !validName(v.Name) || v.DisplayName != nil && !validDisplayName(*v.DisplayName) || v.TagColor != nil && !validTagColor(*v.TagColor) || !validBody(v.Description, MaxAgentDescriptionBytes) {
		return invalid("", "INVALID_AGENT_DIRECTORY_ENTRY")
	}
	return nil
}
func (v DirectoryEntry) Clone() DirectoryEntry {
	v.DisplayName = clonePtr(v.DisplayName)
	v.TagColor = clonePtr(v.TagColor)
	return v
}
func (v DirectoryEntry) MarshalJSON() ([]byte, error) {
	type wire DirectoryEntry
	return marshalChecked(wire(v), v.Validate(), MaxDirectoryEntryBytes)
}
func (v *DirectoryEntry) UnmarshalJSON(raw []byte) error {
	if v == nil {
		return invalid("", "INVALID_ENCODING")
	}
	type wire DirectoryEntry
	w, err := decodeObject[wire](raw, MaxDirectoryEntryBytes, []string{"id", "project_id", "name", "display_name", "tag_color", "description", "version", "created_at", "updated_at"}, []string{"display_name", "tag_color"})
	if err != nil {
		return err
	}
	next := DirectoryEntry(w)
	if err = next.Validate(); err != nil {
		return err
	}
	*v = next
	return nil
}
func (DirectoryEntry) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "agent_directory_entry") }
func (DirectoryEntry) LogValue() slog.Value       { return slog.StringValue("agent_directory_entry") }

// DirectoryQueries reauthorizes the current Human Session/Project Owner in each
// original transaction before reading Agent existence. Only initialized active
// Agents are listed. Get rejects missing, foreign, uninitialized or deleting
// identities; neither method invokes Model, tools, Secret or execution services.
// Lists use created_at DESC,id DESC, default limit 50 and maximum 200. Cursors
// bind the original Owner, Project, resource type and fixed order, not a grant.
// Pages are not a cross-request snapshot; changing limit is permitted.
// Errors/cancellation/unknown commit publish no partial entry or page.
type DirectoryQueries interface {
	GetAgent(context.Context, i.Actor, i.ProjectID, i.AgentID) (DirectoryEntry, error)
	ListAgents(context.Context, i.Actor, i.ProjectID, f.PageRequest) (f.Page[DirectoryEntry], error)
}
