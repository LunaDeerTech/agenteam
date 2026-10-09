package contract

import (
	"bytes"
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"io"
	"slices"
)

const (
	ProjectVariableCreate   Action       = "project.variable.create"
	ProjectVariableUpdate   Action       = "project.variable.update"
	ProjectVariableDelete   Action       = "project.variable.delete"
	ProjectVariableProducer Producer     = "projectvariable"
	ProjectVariableResource ResourceKind = "project_variable"
)

func ProjectVariableAction(action Action) bool {
	return action == ProjectVariableCreate || action == ProjectVariableUpdate || action == ProjectVariableDelete
}

type ProjectVariableMetadataFields struct {
	VariableID    string    `json:"variable_id"`
	Version       f.Version `json:"version"`
	ChangedFields []string  `json:"changed_fields"`
}

func ProjectVariableMetadata(action Action, v ProjectVariableMetadataFields) (Metadata, error) {
	if !ProjectVariableAction(action) || !validID(v.VariableID) || v.Version.Validate() != nil {
		return Metadata{}, invalid("metadata")
	}
	switch action {
	case ProjectVariableCreate:
		if v.Version != 1 || !slices.Equal(v.ChangedFields, []string{"created"}) {
			return Metadata{}, invalid("metadata")
		}
	case ProjectVariableDelete:
		if v.Version < 2 || !slices.Equal(v.ChangedFields, []string{"deleted"}) {
			return Metadata{}, invalid("metadata")
		}
	case ProjectVariableUpdate:
		if v.Version < 2 || len(v.ChangedFields) < 1 || len(v.ChangedFields) > 3 {
			return Metadata{}, invalid("metadata")
		}
		for n, s := range v.ChangedFields {
			if s != "name" && s != "description" && s != "value" || n > 0 && v.ChangedFields[n-1] >= s {
				return Metadata{}, invalid("metadata")
			}
		}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return Metadata{}, invalid("metadata")
	}
	d := metadataData{action: action, raw: string(raw)}
	return Metadata{data: func() metadataData { return d }}, nil
}
func (m Metadata) ProjectVariableFields() (ProjectVariableMetadataFields, error) {
	var v ProjectVariableMetadataFields
	if m.data == nil || !ProjectVariableAction(m.data().action) || json.Unmarshal([]byte(m.data().raw), &v) != nil {
		return v, invalid("metadata")
	}
	return v, nil
}
func decodeProjectVariableMetadata(action Action, raw []byte) (Metadata, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		if err != nil || !ok || seen[name] || name != "variable_id" && name != "version" && name != "changed_fields" {
			return Metadata{}, invalid("metadata")
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	if len(seen) != 3 {
		return Metadata{}, invalid("metadata")
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return Metadata{}, invalid("metadata")
	}
	if _, err = d.Token(); err != io.EOF {
		return Metadata{}, invalid("metadata")
	}
	var v ProjectVariableMetadataFields
	if json.Unmarshal(raw, &v) != nil {
		return Metadata{}, invalid("metadata")
	}
	return ProjectVariableMetadata(action, v)
}
func validateProjectVariableEntry(v EntryFields) error {
	m, err := v.Metadata.ProjectVariableFields()
	if err != nil {
		return err
	}
	r := v.Resource.Details()
	if v.Scope.Details().Kind != i.ProjectScope || v.Actor.Details().Kind != i.Human || v.Outcome != Success || r.Kind != ProjectVariableResource || r.ID != m.VariableID || v.Associations != (Associations{}) {
		return invalid("entry")
	}
	return nil
}
