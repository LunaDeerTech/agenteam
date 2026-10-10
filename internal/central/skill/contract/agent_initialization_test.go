package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestAgentInitializationReceiptOwnsInitialSet(t *testing.T) {
	revision := sampleRevision(t)
	agent, _ := f.ParseID[id.Agent]("01900000-0000-7000-8000-000000000010")
	assignment, _ := f.ParseID[Assignment]("01900000-0000-7000-8000-000000000011")
	base := AgentInitializationReceipt{ProjectID: revision.ProjectID, AgentID: agent, AssignmentSequence: 1, ObservedRevision: revision.Revision}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	base.AddSkillsEnabled = true
	base.Assignment = &InitialAssignment{ID: assignment, ProjectID: base.ProjectID, AgentID: agent, SkillID: revision.SkillID, Sequence: 1, CreatedAt: revision.PublishedAt}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	clone := base.Clone()
	clone.Assignment.Sequence = 2
	if base.Assignment.Sequence != 1 || clone.Validate() == nil {
		t.Fatal("copy mutated initial receipt")
	}
	for _, change := range []func(*AgentInitializationReceipt){
		func(r *AgentInitializationReceipt) { r.AssignmentSequence = 0 },
		func(r *AgentInitializationReceipt) { r.ObservedRevision = 0 },
		func(r *AgentInitializationReceipt) { r.AddSkillsEnabled = false },
		func(r *AgentInitializationReceipt) { r.Assignment = nil },
		func(r *AgentInitializationReceipt) { r.Assignment.AgentID = id.AgentID{} },
		func(r *AgentInitializationReceipt) { r.Assignment.SkillID = SkillID{} },
	} {
		bad := base.Clone()
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("inconsistent initial set accepted")
		}
	}
}
