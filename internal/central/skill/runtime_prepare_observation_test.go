package skill

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func TestSkillPrepareObservationRequiresOriginalPrivateCleanup(t *testing.T) {
	core, base, _, _, _ := authorityFixture(t)
	_, w, _ := prepareCleanupCommandFixture(t)
	w.invocation.Invocation.ToolCallID = stateID[struct{}](170).String()
	w.invocation.Invocation.ToolID = stateID[id.Tool](171)
	w.invocation.Invocation.SpecRevision = 1
	w.invocation.Invocation.Intent = sc.PrepareSkill
	w.metadata.Revision = 1
	w.operationVersion, w.runnerGeneration = 2, 2
	w.active.Store(true)
	w.joining, w.returned, w.materialReleased = true, true, true
	w.issueError = f.NewFault(f.CommitUnknown, f.Unknown)
	w.locks = []f.LockRequest{skillLock(w.binding.SkillID, f.Shared)}
	store := &prepareCleanupStore{skillAuthorityStore: base, witness: w}
	core.state().store = store
	proof := &prepareCleanupProof{store: store, invocation: w.invocation, accepted: true}
	a := &PreparationAuthority{core: core, runtime: proof}
	w.authority = a
	ctx := context.WithValue(context.Background(), preparationKey{}, w)
	r, err := oc.NewTransferObservationRequest(oc.TransferObservationDetails{Original: w.issueRequest})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.DiscoverTransferObservation(context.Background(), r); err == nil {
		t.Fatal("bare DTO authorized original observation")
	}
	plan, err := a.DiscoverTransferObservation(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	tx := armAuthorityTx(t, base, w.locks)
	facts, err := a.ValidateTransferObservationInTx(ctx, tx, r, plan)
	if err != nil || facts.OperationVersion != 2 || facts.RunnerGeneration != 2 || facts.ExecutionID != w.invocation.Invocation.ExecutionID || proof.calls != 1 || store.rows != 1 {
		t.Fatal("observation omitted original proof or immutable owner", err)
	}
	changed := w.issueRequest.Details()
	changed.IssueCommand.IdempotencyKey = "foreign.original"
	foreign, _ := oc.NewTransferAccessRequest(changed)
	other, _ := oc.NewTransferObservationRequest(oc.TransferObservationDetails{Original: foreign})
	if _, err = a.DiscoverTransferObservation(ctx, other); err == nil {
		t.Fatal("observation rebound original command")
	}
	w.joining = false
	if _, err = a.DiscoverTransferObservation(ctx, r); err == nil {
		t.Fatal("observation borrowed active Issue admission")
	}
	w.joining = true
	w.returned = false
	if _, err = a.DiscoverTransferObservation(ctx, r); err == nil {
		t.Fatal("unreturned original provider call observed")
	}
	w.returned = true
	proof.accepted = false
	if _, err = a.ValidateTransferObservationInTx(ctx, tx, r, plan); err == nil {
		t.Fatal("missing actual retirement proof accepted")
	}
	proof.accepted = true
	clear(base.held)
	if _, err = a.ValidateTransferObservationInTx(ctx, tx, r, plan); err == nil {
		t.Fatal("original complete locks omitted")
	}
}
