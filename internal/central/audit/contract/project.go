package contract

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const (
	ProjectCreateAccepted    Action       = "project.create.accepted"
	ProjectCreateCompleted   Action       = "project.create.completed"
	ProjectUpdate            Action       = "project.update"
	ProjectArchiveAccepted   Action       = "project.archive.accepted"
	ProjectArchiveCompleted  Action       = "project.archive.completed"
	ProjectRestore           Action       = "project.restore"
	ProjectDeleteAccepted    Action       = "project.delete.accepted"
	ProjectLifecycleRetry    Action       = "project.lifecycle.retry"
	ProjectProducer          Producer     = "project"
	ProjectResource          ResourceKind = "project"
	ProjectOperationResource ResourceKind = "project_operation"
	ProjectCreationResource  ResourceKind = "project_creation"
)

func ProjectAction(a Action) bool {
	switch a {
	case ProjectCreateAccepted, ProjectCreateCompleted, ProjectUpdate, ProjectArchiveAccepted, ProjectArchiveCompleted, ProjectRestore, ProjectDeleteAccepted, ProjectLifecycleRetry:
		return true
	}
	return false
}

type ProjectChangedField string

const (
	ProjectNameChanged        ProjectChangedField = "name"
	ProjectDescriptionChanged ProjectChangedField = "description"
)

// Only stable identifiers, versions and closed transitions are durable Audit
// metadata. User supplied names, descriptions, paths and arbitrary maps have no
// field here. Audit depends on identities, never the Project implementation.
type ProjectMetadataFields struct {
	ProjectID        string                `json:"project_id"`
	InitiatorID      string                `json:"initiator_id"`
	ProjectVersion   foundation.Version    `json:"project_version"`
	CreationID       string                `json:"creation_id,omitempty"`
	CreationVersion  *foundation.Version   `json:"creation_version,omitempty"`
	OperationID      string                `json:"operation_id,omitempty"`
	OperationVersion *foundation.Version   `json:"operation_version,omitempty"`
	ChangedFields    []ProjectChangedField `json:"changed_fields,omitempty"`
	From             string                `json:"from,omitempty"`
	To               string                `json:"to,omitempty"`
	Action           string                `json:"action,omitempty"`
}

func ProjectMetadata(action Action, f ProjectMetadataFields) (Metadata, error) {
	bad := func() (Metadata, error) { return Metadata{}, invalid("metadata") }
	if !ProjectAction(action) || !validID(f.ProjectID) || !validID(f.InitiatorID) || f.ProjectVersion.Validate() != nil {
		return bad()
	}
	if f.CreationID != "" && !validID(f.CreationID) || f.OperationID != "" && !validID(f.OperationID) || f.CreationVersion != nil && f.CreationVersion.Validate() != nil || f.OperationVersion != nil && f.OperationVersion.Validate() != nil {
		return bad()
	}
	creation := action == ProjectCreateAccepted || action == ProjectCreateCompleted
	operation := action == ProjectArchiveAccepted || action == ProjectArchiveCompleted || action == ProjectDeleteAccepted || action == ProjectLifecycleRetry
	if creation != (f.CreationID != "") || creation != (f.CreationVersion != nil) || operation != (f.OperationID != "") || operation != (f.OperationVersion != nil) {
		return bad()
	}
	if creation && f.ProjectVersion != 1 || action != ProjectUpdate && len(f.ChangedFields) != 0 {
		return bad()
	}
	if action == ProjectUpdate {
		if f.ProjectVersion < 2 || len(f.ChangedFields) < 1 || len(f.ChangedFields) > 2 {
			return bad()
		}
		f.ChangedFields = append([]ProjectChangedField(nil), f.ChangedFields...)
		sort.Slice(f.ChangedFields, func(i, j int) bool { return f.ChangedFields[i] < f.ChangedFields[j] })
		for i, v := range f.ChangedFields {
			if v != ProjectNameChanged && v != ProjectDescriptionChanged || i > 0 && v == f.ChangedFields[i-1] {
				return bad()
			}
		}
	}
	switch action {
	case ProjectCreateAccepted, ProjectCreateCompleted, ProjectUpdate:
		if f.From != "" || f.To != "" || f.Action != "" {
			return bad()
		}
	case ProjectArchiveAccepted:
		if f.From != "active" || f.To != "archiving" || f.Action != "archive" {
			return bad()
		}
	case ProjectArchiveCompleted:
		if f.From != "archiving" || f.To != "archived" || f.Action != "archive" {
			return bad()
		}
	case ProjectRestore:
		if f.From != "archived" || f.To != "active" || f.Action != "restore" {
			return bad()
		}
	case ProjectDeleteAccepted:
		if f.From != "active" && f.From != "archived" || f.To != "deleting" || f.Action != "delete" {
			return bad()
		}
	case ProjectLifecycleRetry:
		if f.From != "" || f.To != "" || f.Action != "archive" && f.Action != "delete" {
			return bad()
		}
	}
	raw, err := json.Marshal(f)
	if err != nil || len(raw) > 4096 {
		return bad()
	}
	d := metadataData{action: action, raw: string(raw)}
	return Metadata{data: func() metadataData { return d }}, nil
}

func (m Metadata) ProjectFields() (ProjectMetadataFields, error) {
	if m.data == nil || !ProjectAction(m.data().action) {
		return ProjectMetadataFields{}, invalid("metadata")
	}
	var f ProjectMetadataFields
	if json.Unmarshal([]byte(m.data().raw), &f) != nil {
		return ProjectMetadataFields{}, invalid("metadata")
	}
	return f, nil
}

func decodeProjectMetadata(action Action, raw []byte) (Metadata, error) {
	if len(raw) > 4096 || !utf8.Valid(raw) {
		return Metadata{}, invalid("metadata")
	}
	allowed := map[string]bool{"project_id": true, "initiator_id": true, "project_version": true, "creation_id": true, "creation_version": true, "operation_id": true, "operation_version": true, "changed_fields": true, "from": true, "to": true, "action": true}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return Metadata{}, invalid("metadata")
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		name, ok := token.(string)
		if err != nil || !ok || !allowed[name] || seen[name] {
			return Metadata{}, invalid("metadata")
		}
		seen[name] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return Metadata{}, invalid("metadata")
		}
	}
	token, err = d.Token()
	if err != nil || token != json.Delim('}') {
		return Metadata{}, invalid("metadata")
	}
	if _, err = d.Token(); err != io.EOF {
		return Metadata{}, invalid("metadata")
	}
	var f ProjectMetadataFields
	if json.Unmarshal(raw, &f) != nil {
		return Metadata{}, invalid("metadata")
	}
	// Reject explicit empty optional fields as well as foreign action fields.
	creation := action == ProjectCreateAccepted || action == ProjectCreateCompleted
	operation := action == ProjectArchiveAccepted || action == ProjectArchiveCompleted || action == ProjectDeleteAccepted || action == ProjectLifecycleRetry
	for _, name := range []string{"creation_id", "creation_version"} {
		if seen[name] != creation {
			return Metadata{}, invalid("metadata")
		}
	}
	for _, name := range []string{"operation_id", "operation_version"} {
		if seen[name] != operation {
			return Metadata{}, invalid("metadata")
		}
	}
	transition := action == ProjectArchiveAccepted || action == ProjectArchiveCompleted || action == ProjectRestore || action == ProjectDeleteAccepted
	if seen["changed_fields"] != (action == ProjectUpdate) || seen["from"] != transition || seen["to"] != transition || seen["action"] != (transition || action == ProjectLifecycleRetry) {
		return Metadata{}, invalid("metadata")
	}
	return ProjectMetadata(action, f)
}

func validateProjectEntry(f EntryFields) error {
	m, err := f.Metadata.ProjectFields()
	if err != nil {
		return err
	}
	a, s, r := f.Actor.Details(), f.Scope.Details(), f.Resource.Details()
	if s.Kind != identity.ProjectScope || s.ProjectID != m.ProjectID || f.Outcome != Success {
		return invalid("entry")
	}
	resourceKind, resourceID := ProjectResource, m.ProjectID
	switch f.Action {
	case ProjectCreateAccepted, ProjectCreateCompleted:
		resourceKind, resourceID = ProjectCreationResource, m.CreationID
	case ProjectArchiveAccepted, ProjectArchiveCompleted, ProjectDeleteAccepted, ProjectLifecycleRetry:
		resourceKind, resourceID = ProjectOperationResource, m.OperationID
	}
	if r.Kind != resourceKind || r.ID != resourceID {
		return invalid("entry")
	}
	switch f.Action {
	case ProjectCreateCompleted:
		if a.Kind != identity.Service || a.ServiceName != identity.ProjectInitialization || a.CauseRef != m.CreationID {
			return invalid("actor")
		}
	case ProjectArchiveCompleted:
		if a.Kind != identity.Service || a.ServiceName != identity.ProjectLifecycle || a.CauseRef != m.OperationID {
			return invalid("actor")
		}
	default:
		if a.Kind != identity.Human || a.UserID != m.InitiatorID {
			return invalid("actor")
		}
	}
	if f.Associations.OperationID != "" && f.Associations.OperationID != m.OperationID {
		return invalid("association")
	}
	if f.Associations.ToolID != "" || f.Associations.ToolCallID != "" || f.Associations.ExecutionID != "" || f.Associations.ApprovalID != "" {
		return invalid("association")
	}
	return nil
}
