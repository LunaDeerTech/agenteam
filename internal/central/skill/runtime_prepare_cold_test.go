package skill

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestSkillPrepareColdObservationNeedsExactPrivateLookup(t *testing.T) {
	core, _, _, _, _ := authorityFixture(t)
	_, original, _ := prepareCleanupCommandFixture(t)
	a := &PreparationAuthority{core: core}
	d := original.issueRequest.Details()
	version := f.Version(2)
	d.IssueCommand.ExpectedVersion = &version
	d.IssueCommand.IdempotencyKey = f.IdempotencyKey("skill-prepare-" + d.Spec.Details().OperationID.String())
	request, err := oc.NewSkillTransferRetirementRequest(oc.SkillTransferRetirementDetails{Actor: d.Actor, OldProcess: stateID[oc.Process](201), Operation: d.Spec.Details().OperationID, Command: d.IssueCommand, Owner: d.Spec.Details().Owner, Object: d.ObjectID, Manifest: d.Manifest})
	if err != nil {
		t.Fatal(err)
	}
	w := &preparationColdWitness{authority: a, invocation: original.invocation, request: request, locks: []f.LockRequest{skillLock(original.binding.SkillID, f.Shared)}}
	w.active.Store(true)
	ctx := context.WithValue(context.Background(), preparationColdKey{}, w)
	if _, err = a.DiscoverSkillTransferRetirement(context.Background(), request); err == nil {
		t.Fatal("bare DTO borrowed stopped authority")
	}
	if _, err = a.DiscoverSkillTransferRetirement(ctx, request); err != nil {
		t.Fatal(err)
	}
	changed := request.Details()
	changed.OldProcess = stateID[oc.Process](202)
	foreign, err := oc.NewSkillTransferRetirementRequest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.DiscoverSkillTransferRetirement(ctx, foreign); err == nil {
		t.Fatal("borrowed lookup rebound original process")
	}
	plan, err := a.DiscoverSkillTransferRetirement(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if a.ValidateSkillTransferRetirementInTx(ctx, f.Tx{}, request, plan) == nil {
		t.Fatal("private pointer alone replaced original runtime/transaction proof")
	}
	w.active.Store(false)
	if _, err = a.DiscoverSkillTransferRetirement(ctx, request); err == nil {
		t.Fatal("returned lookup retained its authority")
	}
}
