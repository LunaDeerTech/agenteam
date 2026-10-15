package meeting

import (
	"context"
	"testing"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestSummaryStoppedObservationRequiresOriginalPrivateOwner(t *testing.T) {
	a, owner, _, original := summaryOwnerFixture(t)
	request := mc.StoppedCallObservationRequest{Actor: original.Actor, Consumer: original.Consumer, CallID: *original.CallID, Input: *original.Input, SnapshotID: original.SnapshotID}
	w := &summaryStoppedOwner{owner: owner, request: request}
	ctx := context.WithValue(context.Background(), summaryStoppedKey{}, w)
	if _, err := a.stoppedOwner(ctx, request); err != nil {
		t.Fatal("original cleanup witness rejected", err)
	}
	if _, err := a.stoppedOwner(context.Background(), request); err == nil {
		t.Fatal("bare original DTO authorized stopped observation")
	}
	for _, field := range []string{"call", "snapshot", "input", "generation", "actor"} {
		t.Run(field, func(t *testing.T) {
			forged := request.Clone()
			switch field {
			case "call":
				forged.CallID = summaryTestID[mc.Call](t)
			case "snapshot":
				forged.SnapshotID = summaryTestID[mc.Snapshot](t)
			case "input":
				forged.Input.SchemaVersion++
			case "generation":
				forged.Consumer.OperationID = summaryTestID[struct{}](t).String()
				forged.Input.OperationID = forged.Consumer.OperationID
			case "actor":
				forged.Actor = summaryTechnicalActor(t, original, original.Attempt.InvocationID)
			}
			if _, err := a.stoppedOwner(ctx, forged); err == nil {
				t.Fatal("foreign stopped-call facts accepted")
			}
		})
	}
	owner.active = false
	if _, err := a.stoppedOwner(ctx, request); err == nil {
		t.Fatal("retired owner authorized another observation")
	}
}

func TestSummaryExistingCallRowsNeverAuthorizeBegin(t *testing.T) {
	for _, status := range []string{"prepared", "resolving", "resolved", "running", "unknown", "succeeded", "failed", "cancelled"} {
		if (summaryCallRecord{status: status}).mayBegin() {
			t.Fatalf("durable %s row authorized a new provider request", status)
		}
	}
	if !(summaryCallRecord{fresh: true, status: "prepared"}).mayBegin() {
		t.Fatal("the original fresh preparation lost its dispatch authority")
	}
	if (summaryCallRecord{fresh: true, status: "prepared", unknown: true}).mayBegin() {
		t.Fatal("an unknown preparation authorized dispatch")
	}
}
