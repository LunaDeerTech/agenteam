package contract

import (
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// These neutral payload fields do not authorize append or register a handler.
type ConfigurationChanged struct {
	AggregateID   string    `json:"aggregate_id"`
	Version       f.Version `json:"version"`
	Scope         string    `json:"scope"`
	ProjectID     string    `json:"project_id,omitempty"`
	ChangedFields []string  `json:"changed_fields"`
}

func (e ConfigurationChanged) Validate() error {
	if !uuid(e.AggregateID) || e.Version.Validate() != nil || e.Scope != "system" && e.Scope != "project" || e.Scope == "system" && e.ProjectID != "" || e.Scope == "project" && !uuid(e.ProjectID) || len(e.ChangedFields) == 0 || !unique(e.ChangedFields, "created", "deleted", "name", "enabled", "base_url", "credential_ref", "provider_options", "model_id", "parameters", "request_overwrite", "header_overwrite", "capabilities", "replacement", "selection") {
		return bad()
	}
	return nil
}
func (e ConfigurationChanged) Clone() ConfigurationChanged {
	e.ChangedFields = append([]string(nil), e.ChangedFields...)
	return e
}

type EmbeddingSelectionChanged struct {
	AggregateID string    `json:"aggregate_id"`
	Version     f.Version `json:"version"`
	OldModelID  *ModelID  `json:"old_model_id"`
	NewModelID  ModelID   `json:"new_model_id"`
}

func (e EmbeddingSelectionChanged) Validate() error {
	if !uuid(e.AggregateID) || e.Version.Validate() != nil || e.NewModelID.Validate() != nil || e.OldModelID != nil && (e.OldModelID.Validate() != nil || *e.OldModelID == e.NewModelID) {
		return bad()
	}
	return nil
}
func (e EmbeddingSelectionChanged) Clone() EmbeddingSelectionChanged {
	e.OldModelID = copyPtr(e.OldModelID)
	return e
}

func (ConfigurationChanged) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_configuration_changed"))
}
func (ConfigurationChanged) LogValue() slog.Value {
	return slog.StringValue("model_configuration_changed")
}

func (EmbeddingSelectionChanged) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_embedding_selection_changed"))
}
func (EmbeddingSelectionChanged) LogValue() slog.Value {
	return slog.StringValue("model_embedding_selection_changed")
}
