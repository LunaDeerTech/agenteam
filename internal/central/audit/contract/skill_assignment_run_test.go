package contract

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"testing"
)

func TestAgentSkillAssignmentAuditRequiresExactAssociations(t *testing.T) {
	const id = "01900000-0000-7000-8000-000000000041"
	const target = "01900000-0000-7000-8000-000000000042"
	p, _ := f.ParseID[i.Project](id)
	a, _ := f.ParseID[i.Agent](id)
	e, _ := f.ParseID[i.Execution](id)
	actor, _ := i.NewAgentRun(p, a, e)
	scope, _ := i.InProject(p)
	resource, _ := NewResource(AgentResource, target)
	meta, _ := SkillAssignmentMetadata(SkillAssign, SkillAssignmentMetadataFields{AgentID: target, AssignmentID: id, SkillID: id, ChangeID: id, Version: 2, Sequence: 2, Enabled: true})
	fields := EntryFields{Actor: actor, Scope: scope, Resource: resource, Action: SkillAssign, Outcome: Success, Metadata: meta, Associations: Associations{ExecutionID: id, ToolID: id, ToolCallID: id, OperationID: id}}
	if _, err := NewEntry(fields); err != nil {
		t.Fatal("same-project other target rejected", err)
	}
	for _, mutate := range []func(*EntryFields){
		func(v *EntryFields) { v.Associations.ToolCallID = "" },
		func(v *EntryFields) { v.Associations.ExecutionID = target },
		func(v *EntryFields) { v.Associations.RequestID = id },
		func(v *EntryFields) { v.Scope = i.SystemScope() },
		func(v *EntryFields) { v.Associations = Associations{} },
	} {
		bad := fields
		mutate(&bad)
		if _, err := NewEntry(bad); err == nil {
			t.Fatal("invalid Agent assignment audit tuple accepted")
		}
	}
}
