package execution

import (
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"testing"
)

func TestMeetingAssociationRejectsChangedOriginalLineage(t *testing.T) {
	project, task := associatedTestIdentity(t)
	trigger := c.Trigger{Kind: "meeting", MeetingID: newTestID[struct{}](t).String(), TurnID: newTestID[struct{}](t).String(), ContributionID: newTestID[struct{}](t).String(), ParticipantID: newTestID[struct{}](t).String()}
	m := c.AssociatedMeeting{ExecutionID: task.ExecutionID, AgentID: task.AgentID, Key: task.Key, Digest: task.Digest, Trigger: trigger, Generation: 1, Attempt: 1}
	d := associatedIdentity{AssociatedDispatch: c.AssociatedDispatch{ExecutionID: m.ExecutionID, AgentID: m.AgentID, Key: m.Key, Digest: m.Digest}, meeting: m}
	generation, attempt := f.Version(1), f.Version(1)
	launch := c.LaunchRequest{Trigger: trigger, Purpose: "meeting/response", Lineage: c.Lineage{ContributionGeneration: &generation, ContributionAttempt: &attempt}}
	if validateAssociated(project, d) != nil || !d.matchesLaunch(launch) || d.sourceID() != trigger.ContributionID {
		t.Fatal("valid committed Meeting tuple rejected")
	}
	for _, mutate := range []func(*c.LaunchRequest){
		func(r *c.LaunchRequest) { r.Trigger.ParticipantID = newTestID[struct{}](t).String() },
		func(r *c.LaunchRequest) { r.Trigger.TurnID = newTestID[struct{}](t).String() },
		func(r *c.LaunchRequest) { r.Trigger.ContributionID = newTestID[struct{}](t).String() },
		func(r *c.LaunchRequest) { r.Trigger.MeetingID = newTestID[struct{}](t).String() },
		func(r *c.LaunchRequest) { *r.Lineage.ContributionGeneration = 2 },
		func(r *c.LaunchRequest) { *r.Lineage.ContributionAttempt = 2 },
		func(r *c.LaunchRequest) { r.Lineage.DispatchID = task.DispatchID },
		func(r *c.LaunchRequest) { r.Purpose = "task/work" },
	} {
		bad := launch.Clone()
		mutate(&bad)
		if d.matchesLaunch(bad) {
			t.Fatal("changed Meeting launch reused original driver identity")
		}
	}
	mixed := d
	mixed.DispatchID = task.DispatchID
	if validateAssociated(project, mixed) == nil {
		t.Fatal("accepted mixed Task and Meeting association")
	}
}
