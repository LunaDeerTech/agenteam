package contract_test

import (
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestMeetingRetirementLocksExcludeRegistrationAndCapture(t *testing.T) {
	project := inputID[i.Project](t, 1)
	m := ec.AssociatedMeeting{ExecutionID: inputID[i.Execution](t, 2), AgentID: inputID[i.Agent](t, 3), Key: "meeting-launch", Digest: ec.TriggerInputDigest([]byte("original")), Generation: 1, Attempt: 1,
		Trigger: ec.Trigger{Kind: "meeting", MeetingID: inputID[struct{}](t, 4).String(), TurnID: inputID[struct{}](t, 5).String(), ContributionID: inputID[struct{}](t, 6).String(), ParticipantID: inputID[struct{}](t, 7).String()}}
	locks, err := ec.MeetingExecutionRetirementLocks(project, m)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := f.ProjectLock(project.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, m.ExecutionID.String())
	command, _ := (ec.LaunchLookupKey{ProjectID: project, AgentID: m.AgentID, IdempotencyKey: m.Key}).Command()
	c, _ := f.CommandLock(command)
	for _, want := range []f.LockKey{p, e, c} {
		found := false
		for _, lock := range locks {
			if lock.Key.Canonical() == want.Canonical() && lock.Mode == f.Exclusive {
				found = true
			}
		}
		if !found {
			t.Fatal("original registration/capture barrier missing", want.Canonical())
		}
	}
	for _, change := range []func(*ec.AssociatedMeeting){
		func(v *ec.AssociatedMeeting) { v.ExecutionID = i.ExecutionID{} },
		func(v *ec.AssociatedMeeting) { v.Trigger.Kind = "task" },
		func(v *ec.AssociatedMeeting) { v.Generation = 0 },
		func(v *ec.AssociatedMeeting) { v.Digest = "" },
	} {
		bad := m
		change(&bad)
		if _, err := ec.MeetingExecutionRetirementLocks(project, bad); err == nil {
			t.Fatal("partial launch identity accepted")
		}
	}
}
