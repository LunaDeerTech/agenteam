package meeting

import (
	"context"
	"testing"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestSummarySnapshotWitnessIsBoundToOriginalCallAndGeneration(t *testing.T) {
	a, owner, _, original := summaryOwnerFixture(t)
	owner.input.InputDigest = original.Input.Digest
	model := &SummaryModel{deps: SummaryModelDependencies{Authority: a}}
	call := &summaryModelCall{owner: owner, resolved: &mc.ResolvedModel{Snapshot: mc.ConfigSnapshot{ID: original.SnapshotID}}}
	request := mc.ServingSnapshotRetentionRequest{Actor: original.Actor, Owner: mc.ServingSnapshotOwner{Kind: mc.MeetingConsumer, ProjectID: owner.input.ProjectID, GenerationID: owner.input.GenerationID.String()}, SnapshotID: original.SnapshotID, OwnerVersion: 1, Action: mc.RetainServingSnapshot}
	w := &summarySnapshotWitness{model: model, input: owner.input, request: request, call: call, active: true}
	ctx := context.WithValue(context.Background(), summarySnapshotKey{}, w)
	if _, err := a.snapshotOwner(ctx, request); err != nil {
		t.Fatal("original generation pin witness rejected", err)
	}
	if _, err := a.snapshotOwner(context.Background(), request); err == nil {
		t.Fatal("bare pin DTO granted snapshot retention")
	}
	for _, field := range []string{"snapshot", "generation", "version", "release", "actor"} {
		t.Run(field, func(t *testing.T) {
			forged := request.Clone()
			switch field {
			case "snapshot":
				forged.SnapshotID = summaryTestID[mc.Snapshot](t)
			case "generation":
				forged.Owner.GenerationID = summaryTestID[struct{}](t).String()
			case "version":
				forged.OwnerVersion++
			case "release":
				forged.Action = mc.ReleaseServingSnapshot
			case "actor":
				forged.Actor = summaryTechnicalActor(t, original, original.Attempt.InvocationID)
			}
			if _, err := a.snapshotOwner(ctx, forged); err == nil {
				t.Fatal("foreign snapshot transition accepted")
			}
		})
	}
	call.done = true
	if _, err := a.snapshotOwner(ctx, request); err == nil {
		t.Fatal("returned call created another pin")
	}
	call.done, w.active = false, false
	if _, err := a.snapshotOwner(ctx, request); err == nil {
		t.Fatal("retired transition witness was reused")
	}
}
