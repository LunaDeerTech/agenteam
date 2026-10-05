package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	ProviderCreate         Action       = "provider.create"
	ProviderUpdate         Action       = "provider.update"
	ProviderDelete         Action       = "provider.delete"
	ModelCreate            Action       = "model.create"
	ModelUpdate            Action       = "model.update"
	ModelDelete            Action       = "model.delete"
	ModelSelectionUpdate   Action       = "model.selection.update"
	ModelProducer          Producer     = "model"
	ModelProviderResource  ResourceKind = "model_provider"
	ModelConfigResource    ResourceKind = "model_config"
	ModelSelectionResource ResourceKind = "model_selection"
)

func ModelAction(a Action) bool {
	switch a {
	case ProviderCreate, ProviderUpdate, ProviderDelete, ModelCreate, ModelUpdate, ModelDelete, ModelSelectionUpdate:
		return true
	}
	return false
}

// ModelMetadataFields contains configuration identities, never configuration
// bodies or credential identities. The action determines its exact shape.
type ModelMetadataFields struct {
	ProviderID    string      `json:"provider_id,omitempty"`
	ModelID       string      `json:"model_id,omitempty"`
	SelectionID   string      `json:"selection_id,omitempty"`
	Version       f.Version   `json:"version"`
	ChangedFields []string    `json:"changed_fields"`
	ReplacementID string      `json:"replacement_id,omitempty"`
	AffectedCount *f.Progress `json:"affected_count,omitempty"`
	SelectorKind  string      `json:"selector_kind,omitempty"`
}

func ModelMetadata(action Action, value ModelMetadataFields) (Metadata, error) {
	if !ModelAction(action) || value.Version.Validate() != nil || len(value.ChangedFields) == 0 || len(value.ChangedFields) > 15 {
		return Metadata{}, invalid("metadata")
	}
	v := value
	v.ChangedFields = append([]string(nil), value.ChangedFields...)
	slices.Sort(v.ChangedFields)
	for i, field := range v.ChangedFields {
		if i > 0 && field == v.ChangedFields[i-1] {
			return Metadata{}, invalid("changed_fields")
		}
		switch field {
		case "created", "deleted", "name", "enabled", "base_url", "credential_ref", "provider_options", "model_id", "parameters", "request_overwrite", "header_overwrite", "capabilities", "replacement", "selection":
		default:
			return Metadata{}, invalid("changed_fields")
		}
	}
	if v.ReplacementID != "" && !validID(v.ReplacementID) || v.AffectedCount != nil && *v.AffectedCount < 0 {
		return Metadata{}, invalid("metadata")
	}
	switch action {
	case ProviderCreate, ProviderUpdate, ProviderDelete:
		if !validID(v.ProviderID) || v.ModelID != "" || v.SelectionID != "" || v.ReplacementID != "" || v.AffectedCount != nil || v.SelectorKind != "" {
			return Metadata{}, invalid("metadata")
		}
	case ModelCreate, ModelUpdate, ModelDelete:
		if !validID(v.ProviderID) || !validID(v.ModelID) || v.SelectionID != "" || v.SelectorKind != "" {
			return Metadata{}, invalid("metadata")
		}
		if action != ModelDelete && (v.ReplacementID != "" || v.AffectedCount != nil) || action == ModelDelete && v.AffectedCount == nil {
			return Metadata{}, invalid("metadata")
		}
	case ModelSelectionUpdate:
		if !validID(v.SelectionID) || v.ProviderID != "" || v.ModelID != "" || v.ReplacementID != "" || v.AffectedCount != nil || v.SelectorKind != "platform" {
			return Metadata{}, invalid("metadata")
		}
	}
	if action == ProviderCreate || action == ModelCreate {
		if !slices.Equal(v.ChangedFields, []string{"created"}) {
			return Metadata{}, invalid("changed_fields")
		}
	} else if action == ProviderDelete || action == ModelDelete {
		expected := []string{"deleted"}
		if v.ReplacementID != "" {
			expected = append(expected, "replacement")
		}
		if !slices.Equal(v.ChangedFields, expected) {
			return Metadata{}, invalid("changed_fields")
		}
	} else if action == ModelSelectionUpdate {
		if !slices.Equal(v.ChangedFields, []string{"selection"}) {
			return Metadata{}, invalid("changed_fields")
		}
	} else {
		for _, field := range v.ChangedFields {
			if field == "created" || field == "deleted" || field == "replacement" || field == "selection" {
				return Metadata{}, invalid("changed_fields")
			}
			provider := field == "name" || field == "enabled" || field == "base_url" || field == "credential_ref" || field == "provider_options"
			model := field == "name" || field == "enabled" || field == "model_id" || field == "parameters" || field == "request_overwrite" || field == "header_overwrite" || field == "capabilities"
			if action == ProviderUpdate && !provider || action == ModelUpdate && !model {
				return Metadata{}, invalid("changed_fields")
			}
		}
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 4096 {
		return Metadata{}, invalid("metadata")
	}
	d := metadataData{action: action, raw: string(raw)}
	return Metadata{data: func() metadataData { return d }}, nil
}

func (m Metadata) ModelFields() (ModelMetadataFields, error) {
	var v ModelMetadataFields
	if m.data == nil || !ModelAction(m.data().action) || json.Unmarshal([]byte(m.data().raw), &v) != nil {
		return v, invalid("metadata")
	}
	return v, nil
}

func decodeModelMetadata(action Action, raw []byte) (Metadata, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := map[string]bool{}
	for d.More() {
		t, err = d.Token()
		name, ok := t.(string)
		if err != nil || !ok || seen[name] {
			return Metadata{}, invalid("metadata")
		}
		switch name {
		case "provider_id", "model_id", "selection_id", "version", "changed_fields", "replacement_id", "affected_count", "selector_kind":
		default:
			return Metadata{}, invalid("metadata")
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	if _, err = d.Token(); err != nil {
		return Metadata{}, invalid("metadata")
	}
	if _, err = d.Token(); err != io.EOF {
		return Metadata{}, invalid("metadata")
	}
	var v ModelMetadataFields
	if json.Unmarshal(raw, &v) != nil || !seen["version"] || !seen["changed_fields"] {
		return Metadata{}, invalid("metadata")
	}
	m, err := ModelMetadata(action, v)
	if err != nil {
		return Metadata{}, err
	}
	// Reject forbidden optional fields even when they decode to a zero value.
	var canonical map[string]json.RawMessage
	_ = json.Unmarshal(m.JSON(), &canonical)
	for name := range seen {
		if _, ok := canonical[name]; !ok {
			return Metadata{}, invalid("metadata")
		}
	}
	return m, nil
}

func validateModelEntry(v EntryFields) error {
	m, err := v.Metadata.ModelFields()
	if err != nil {
		return err
	}
	if v.Actor.Details().Kind != id.Human || v.Outcome != Success || v.Scope.Details().Kind != id.System && v.Scope.Details().Kind != id.ProjectScope {
		return invalid("entry")
	}
	kind, resource := ModelProviderResource, m.ProviderID
	if m.ModelID != "" {
		kind, resource = ModelConfigResource, m.ModelID
	}
	if m.SelectionID != "" {
		kind, resource = ModelSelectionResource, m.SelectionID
		if v.Scope.Details().Kind != id.System {
			return invalid("scope")
		}
	}
	if v.Resource.Details().Kind != kind || v.Resource.Details().ID != resource {
		return invalid("resource")
	}
	a := v.Associations
	if a.ToolID != "" || a.ExecutionID != "" || a.ToolCallID != "" || a.OperationID != "" || a.RequestID != "" || a.ApprovalID != "" || a.RunnerID != "" {
		return invalid("association")
	}
	return nil
}

type ModelAuthority interface {
	CheckAppendInTx(context.Context, f.Tx, Entry, AppendKey) error
}
