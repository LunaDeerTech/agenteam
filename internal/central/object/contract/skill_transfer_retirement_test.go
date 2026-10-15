package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestSkillPrepareRetirementRequestIsImmutableAndNotAbsence(t *testing.T) {
	base := observationFixture(t)
	project, _ := f.ParseID[id.Project](base.Spec.Details().Owner.Details().ProjectID)
	agent, _ := f.NewID[id.Agent]()
	execution, _ := f.NewID[id.Execution]()
	actor, _ := id.NewAgentRun(project, agent, execution)
	revision, _ := f.NewID[struct{}]()
	owner, _ := NewObjectOwner(SkillRevision, revision.String(), project.String())
	process, _ := f.NewID[Process]()
	version := f.Version(2)
	d := SkillTransferRetirementDetails{Actor: actor, OldProcess: process, Operation: base.Spec.Details().OperationID, Command: base.IssueCommand, Owner: owner, Object: base.ObjectID, Manifest: base.Manifest}
	d.Command.ExpectedVersion = &version
	d.Command.IdempotencyKey = f.IdempotencyKey("skill-prepare-" + d.Operation.String())
	r, err := NewSkillTransferRetirementRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	version = 3
	if *r.Details().Command.ExpectedVersion != 2 {
		t.Fatal("caller changed captured command")
	}
	copy := r.Details()
	*copy.Command.ExpectedVersion = 4
	if *r.Details().Command.ExpectedVersion != 2 {
		t.Fatal("returned details changed command")
	}
	if (SkillTransferRetirementObservation{}).ClosedFor(r) {
		t.Fatal("zero observation claimed closed")
	}
	for _, mutate := range []func(*SkillTransferRetirementDetails){
		func(d *SkillTransferRetirementDetails) { d.Command.ExpectedVersion = nil },
		func(d *SkillTransferRetirementDetails) { d.Command.IdempotencyKey = "other" },
		func(d *SkillTransferRetirementDetails) { d.OldProcess = ProcessID{} },
		func(d *SkillTransferRetirementDetails) { d.Actor = base.Actor },
	} {
		changed := r.Details()
		mutate(&changed)
		if _, err = NewSkillTransferRetirementRequest(changed); err == nil {
			t.Fatal("unbound retirement request accepted")
		}
	}
}
