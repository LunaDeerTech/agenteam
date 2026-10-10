package contract

import (
	"encoding/json"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	ProjectSecretVariableCreate Action = "project.secret_variable.create"
	ProjectSecretVariableUpdate Action = "project.secret_variable.update"
	ProjectSecretVariableDelete Action = "project.secret_variable.delete"
)

// ProjectSecretVariableAction is separate from the ordinary variable authority
// allowlist. Recognizing an Audit shape grants no permission to append it.
func ProjectSecretVariableAction(action Action) bool {
	return projectSecretVariableShape(action) != ""
}

func projectSecretVariableShape(action Action) Action {
	switch action {
	case ProjectSecretVariableCreate:
		return ProjectVariableCreate
	case ProjectSecretVariableUpdate:
		return ProjectVariableUpdate
	case ProjectSecretVariableDelete:
		return ProjectVariableDelete
	default:
		return ""
	}
}

// ProjectSecretVariableMetadataFields contains identities and field names only.
// It never contains the changed values, their lengths, or Credential identities.
type ProjectSecretVariableMetadataFields = ProjectVariableMetadataFields

func ProjectSecretVariableMetadata(action Action, fields ProjectSecretVariableMetadataFields) (Metadata, error) {
	m, err := ProjectVariableMetadata(projectSecretVariableShape(action), fields)
	if err != nil {
		return Metadata{}, err
	}
	return projectSecretMetadata(action, m), nil
}

func projectSecretMetadata(action Action, shape Metadata) Metadata {
	d := shape.data()
	d.action = action
	return Metadata{data: func() metadataData { return d }}
}

func (m Metadata) ProjectSecretVariableFields() (ProjectSecretVariableMetadataFields, error) {
	var fields ProjectSecretVariableMetadataFields
	if m.data == nil || !ProjectSecretVariableAction(m.data().action) || json.Unmarshal([]byte(m.data().raw), &fields) != nil {
		return fields, invalid("metadata")
	}
	return fields, nil
}

func decodeProjectSecretVariableMetadata(action Action, raw []byte) (Metadata, error) {
	m, err := decodeProjectVariableMetadata(projectSecretVariableShape(action), raw)
	if err != nil {
		return Metadata{}, err
	}
	return projectSecretMetadata(action, m), nil
}

func validateProjectSecretVariableEntry(v EntryFields) error {
	m, err := v.Metadata.ProjectSecretVariableFields()
	if err != nil {
		return err
	}
	r := v.Resource.Details()
	if v.Scope.Details().Kind != i.ProjectScope || v.Actor.Details().Kind != i.Human || v.Outcome != Success || r.Kind != ProjectVariableResource || r.ID != m.VariableID || v.Associations != (Associations{}) {
		return invalid("entry")
	}
	return nil
}
