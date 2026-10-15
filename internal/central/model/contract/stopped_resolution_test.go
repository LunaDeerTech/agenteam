package contract

import (
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func TestStoppedResolutionBindsOriginalRequestAndProcess(t *testing.T) {
	s := meetingSelectionRequest(t, MeetingSummaryInitial)
	actor, err := id.NewHuman(fresh[id.User](t), fresh[id.Session](t))
	must(t, err)
	call := fresh[Call](t)
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, call.String())
	must(t, err)
	r := StoppedResolutionRequest{Resolve: ResolveRequest{Actor: actor, Consumer: s.Consumer, Purpose: s.Consumer.Purpose, Source: CurrentSelectionSource, Selection: &s.Selection, LeaseOwner: owner}, CallID: call, Input: InputIdentity{OperationID: s.Consumer.OperationID, Digest: f.Digest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"), SchemaVersion: 1}, PreparedProcessID: fresh[oc.Process](t)}
	r.Resolve.Selection.Version = nil
	before, err := StoppedResolutionBinding(r)
	must(t, err)
	for _, name := range []string{"process", "selection-version", "input", "call"} {
		t.Run(name, func(t *testing.T) {
			changed := r.Clone()
			switch name {
			case "process":
				changed.PreparedProcessID = fresh[oc.Process](t)
			case "selection-version":
				changed.Resolve.Selection.Version = ptrVersion(1)
			case "input":
				changed.Input.Digest = f.Digest("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
			case "call":
				changed.CallID = fresh[Call](t)
				changed.Resolve.LeaseOwner, err = sc.NewCredentialLeaseOwner(sc.ModelCallOwner, changed.CallID.String())
				must(t, err)
			}
			after, err := StoppedResolutionBinding(changed)
			must(t, err)
			if before == after {
				t.Fatal("original recovery fact omitted")
			}
		})
	}
	r.CallID = fresh[Call](t)
	reject(t, r.Validate())
}

func TestStoppedResolutionZeroIsNotSettledAbsence(t *testing.T) {
	reject(t, (StoppedResolutionObservation{}).Validate())
	reject(t, (StoppedResolutionObservation{Settled: true, CallFound: true}).Validate())
	must(t, (StoppedResolutionObservation{Settled: true}).Validate())
}
