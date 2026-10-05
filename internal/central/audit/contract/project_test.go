package contract

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func projectFields(action Action) ProjectMetadataFields {
	v := foundation.Version(1)
	f := ProjectMetadataFields{ProjectID: "01900000-0000-7000-8000-000000000001", InitiatorID: "01900000-0000-7000-8000-000000000002", ProjectVersion: 2}
	switch action {
	case ProjectCreateAccepted, ProjectCreateCompleted:
		f.ProjectVersion = 1
		f.CreationID = "01900000-0000-7000-8000-000000000003"
		f.CreationVersion = &v
	case ProjectUpdate:
		f.ChangedFields = []ProjectChangedField{ProjectNameChanged}
	case ProjectArchiveAccepted:
		f.From = "active"
		f.To = "archiving"
		f.Action = "archive"
	case ProjectArchiveCompleted:
		f.From = "archiving"
		f.To = "archived"
		f.Action = "archive"
	case ProjectDeleteAccepted:
		f.From = "active"
		f.To = "deleting"
		f.Action = "delete"
	case ProjectRestore:
		f.From = "archived"
		f.To = "active"
		f.Action = "restore"
	case ProjectLifecycleRetry:
		f.Action = "delete"
	}
	if action == ProjectArchiveAccepted || action == ProjectArchiveCompleted || action == ProjectDeleteAccepted || action == ProjectLifecycleRetry {
		f.OperationID = "01900000-0000-7000-8000-000000000004"
		f.OperationVersion = &v
	}
	return f
}
func projectEntry(t *testing.T, action Action) EntryFields {
	t.Helper()
	f := projectFields(action)
	m, e := ProjectMetadata(action, f)
	if e != nil {
		t.Fatal(e)
	}
	project, _ := foundation.ParseID[identity.Project](f.ProjectID)
	scope, _ := identity.InProject(project)
	user, _ := foundation.ParseID[identity.User](f.InitiatorID)
	session, _ := foundation.ParseID[identity.Session]("01900000-0000-7000-8000-000000000005")
	actor, _ := identity.NewHuman(user, session)
	kind, id := ProjectResource, f.ProjectID
	if f.CreationID != "" {
		kind, id = ProjectCreationResource, f.CreationID
	}
	if f.OperationID != "" {
		kind, id = ProjectOperationResource, f.OperationID
	}
	if action == ProjectCreateCompleted || action == ProjectArchiveCompleted {
		name := identity.ProjectInitialization
		cause := f.CreationID
		if action == ProjectArchiveCompleted {
			name = identity.ProjectLifecycle
			cause = f.OperationID
		}
		reg, _ := identity.RegisterService(name)
		actor, _ = reg.Actor(cause, scope)
	}
	resource, _ := NewResource(kind, id)
	return EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: Success, Resource: resource, Metadata: m}
}
func TestProjectAuditClosedActionsAndStrictDurableDecode(t *testing.T) {
	actions := []Action{ProjectCreateAccepted, ProjectCreateCompleted, ProjectUpdate, ProjectArchiveAccepted, ProjectArchiveCompleted, ProjectRestore, ProjectDeleteAccepted, ProjectLifecycleRetry}
	for _, action := range actions {
		t.Run(string(action), func(t *testing.T) {
			if !action.Valid() || ProducerFor(action) != ProjectProducer {
				t.Fatal("not registered")
			}
			entry := projectEntry(t, action)
			if _, e := NewEntry(entry); e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(entry.Metadata)
			if _, e := DecodeMetadata(action, raw); e != nil {
				t.Fatal("cannot read canonical metadata", e)
			}
			bad := []string{strings.TrimSuffix(string(raw), "}") + `,"name":"private"}`, strings.Replace(string(raw), `"project_id":`, `"project_id":null,"project_id":`, 1), string(raw) + ` {}`, strings.Replace(string(raw), `"project_version":"`, `"project_version":`, 1)}
			for _, input := range bad {
				if _, e := DecodeMetadata(action, []byte(input)); e == nil {
					t.Fatal("unsafe metadata accepted")
				}
			}
			entry.Outcome = Unknown
			if _, e := NewEntry(entry); e == nil {
				t.Fatal("non-success outcome widened")
			}
			entry = projectEntry(t, action)
			entry.Scope = identity.SystemScope()
			if _, e := NewEntry(entry); e == nil {
				t.Fatal("system scope widened")
			}
		})
	}
	for _, a := range []Action{"project.delete.completed", "project.unknown", "project.create"} {
		if a.Valid() || ProjectAction(a) {
			t.Fatal("unknown action registered")
		}
	}
}
func TestProjectMetadataCannotCarryForeignFieldsOrAlias(t *testing.T) {
	f := projectFields(ProjectUpdate)
	f.ChangedFields = []ProjectChangedField{ProjectNameChanged, ProjectDescriptionChanged}
	m, e := ProjectMetadata(ProjectUpdate, f)
	if e != nil {
		t.Fatal(e)
	}
	f.ChangedFields[0] = "password"
	got, e := m.ProjectFields()
	if e != nil || got.ChangedFields[0] != ProjectDescriptionChanged {
		t.Fatal("caller slice mutated metadata")
	}
	got.ChangedFields[0] = "password"
	again, _ := m.ProjectFields()
	if again.ChangedFields[0] != ProjectDescriptionChanged {
		t.Fatal("projection aliases immutable metadata")
	}
	f = projectFields(ProjectCreateAccepted)
	f.OperationID = f.CreationID
	if _, e = ProjectMetadata(ProjectCreateAccepted, f); e == nil {
		t.Fatal("foreign operation field")
	}
	entry := projectEntry(t, ProjectCreateCompleted)
	reg, _ := identity.RegisterService(identity.ProjectLifecycle)
	entry.Actor, _ = reg.Actor(projectFields(ProjectCreateAccepted).CreationID, entry.Scope)
	if _, e = NewEntry(entry); e == nil {
		t.Fatal("wrong role authorized initialization")
	}
}
