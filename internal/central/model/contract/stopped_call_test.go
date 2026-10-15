package contract

import (
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestMeetingServingSnapshotRetainsOriginalGeneration(t *testing.T) {
	actor, err := id.NewHuman(fresh[id.User](t), fresh[id.Session](t))
	must(t, err)
	for _, purpose := range []Purpose{MeetingSummaryInitial, MeetingSummaryUpdate} {
		s := meetingSelectionRequest(t, purpose)
		snapshot := fresh[Snapshot](t)
		owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, fresh[Call](t).String())
		must(t, err)
		r := ResolveRequest{Actor: actor, Consumer: s.Consumer, Purpose: purpose, Source: ServingSnapshotSource, ServingSnapshotID: &snapshot, ServingGenerationID: fresh[struct{}](t).String(), LeaseOwner: owner}
		must(t, r.Validate())
		before, err := ResolveBinding(r)
		must(t, err)
		r.ServingGenerationID = fresh[struct{}](t).String()
		after, err := ResolveBinding(r)
		must(t, err)
		if before == after {
			t.Fatal("Summary generation omitted")
		}
		r.Selection = &s.Selection
		reject(t, r.Validate())
		pin := ServingSnapshotRetentionRequest{Actor: actor, Owner: ServingSnapshotOwner{Kind: MeetingConsumer, ProjectID: s.Consumer.ProjectID, GenerationID: fresh[struct{}](t).String()}, SnapshotID: snapshot, OwnerVersion: 1, Action: RetainServingSnapshot}
		must(t, pin.Validate())
		agent := fresh[id.Agent](t)
		pin.Owner.AgentID = &agent
		reject(t, pin.Validate())
	}
}

func TestStoppedCallObservationCannotTreatAbsenceAsRetirement(t *testing.T) {
	must(t, (StoppedCallObservation{}).Validate())
	reject(t, (StoppedCallObservation{Retired: true}).Validate())
	a := AttemptIdentity{CallID: fresh[Call](t), InvocationID: fresh[Invocation](t), AttemptIndex: 1, ProcessID: fresh[oc.Process](t), Fence: 1}
	must(t, (StoppedCallObservation{Found: true, Attempt: &a, Retired: true, Status: "unknown"}).Validate())
	reject(t, (StoppedCallObservation{Found: true, Attempt: &a, Status: "succeeded"}).Validate())
	reject(t, (StoppedCallObservation{Found: true, Attempt: &a, Retired: true, Status: "running"}).Validate())
}
