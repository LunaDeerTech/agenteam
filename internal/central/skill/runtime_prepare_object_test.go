package skill

import (
	"context"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type prepareCleanupStore struct {
	*skillAuthorityStore
	witness *preparationWitness
	rows    int
}

func (s *prepareCleanupStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.skillAuthorityStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *prepareCleanupStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if strings.HasPrefix(query, "SELECT s.version,COALESCE") {
		s.rows++
		w := s.witness
		if len(args) != 5 || args[0] != w.invocation.Invocation.ProjectID.String() || args[1] != w.binding.SkillID.String() || args[2] != w.binding.RevisionID.String() || args[3] != w.metadata.Object.ID.String() || args[4] != int64(w.metadata.Revision) {
			return skillRowValues{err: fault(f.Forbidden)}
		}
		return skillRowValues{values: []any{int64(3), stateID[Installation](50).String()}}
	}
	return s.skillAuthorityStore.QueryRow(ctx, query, args...)
}

type prepareCleanupProof struct {
	sc.PreparationInvocationAuthority
	store      *prepareCleanupStore
	invocation sc.PreparationInvocation
	accepted   bool
	calls      int
}

func (a *prepareCleanupProof) RequireSkillPreparationRetirementInTx(_ context.Context, tx f.Tx, inv sc.PreparationInvocation, _ sc.InvocationPlan) (f.Version, error) {
	a.calls++
	if !a.accepted || tx != a.store.tx || inv.RequestID != a.invocation.RequestID || inv.Invocation.OperationID != a.invocation.Invocation.OperationID || inv.Invocation.AttemptID != a.invocation.Invocation.AttemptID {
		return 0, fault(f.Forbidden)
	}
	return 2, nil
}
func TestSkillPrepareCleanupOwnerDoesNotAuthorizeBodyOrMutation(t *testing.T) {
	authority, base, _, _, _ := authorityFixture(t)
	project, agent, execution := stateID[id.Project](120), stateID[id.Agent](121), stateID[id.Execution](122)
	actor, err := id.NewAgentRun(project, agent, execution)
	if err != nil {
		t.Fatal(err)
	}
	revision := stateID[sc.Revision](123)
	owner, err := oc.NewObjectOwner(oc.SkillRevision, revision.String(), project.String())
	if err != nil {
		t.Fatal(err)
	}
	inv := sc.PreparationInvocation{Invocation: sc.Invocation{Actor: actor, ProjectID: project, AgentID: agent, ExecutionID: execution, OperationID: stateID[struct{}](124).String(), AttemptID: stateID[struct{}](125).String(), ToolCallID: stateID[struct{}](126).String(), ToolID: stateID[id.Tool](127), SpecRevision: 1, Intent: sc.PrepareSkill}, RequestID: p.ID(stateID[struct{}](128).String())}
	if err = inv.Validate(); err != nil {
		t.Fatal(err)
	}
	store := &prepareCleanupStore{skillAuthorityStore: base}
	proof := &prepareCleanupProof{store: store, invocation: inv, accepted: true}
	authority.state().store = store
	a := &PreparationAuthority{core: authority, runtime: proof}
	w := &preparationWitness{authority: a, invocation: inv, joining: true, returned: true, materialReleased: true, operationVersion: 2, binding: sc.SkillBinding{SkillID: stateID[pc.Skill](129), RevisionID: revision}, metadata: sc.RevisionMetadata{Revision: 1, Object: oc.ObjectMeta{ID: stateID[oc.StoredObject](130)}}, locks: []f.LockRequest{skillLock(stateID[pc.Skill](129), f.Shared)}}
	w.active.Store(true)
	store.witness = w
	ctx := context.WithValue(context.Background(), preparationKey{}, w)
	tx := armAuthorityTx(t, base, w.locks)
	for _, intent := range []id.AccessIntent{id.Read, id.Converge} {
		grant, err := authority.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
		if err != nil || !grant.Matches(actor, owner, intent) || grant.Details().ReadObjectID != (oc.ObjectID{}) {
			t.Fatal("exact original cleanup bookkeeping rejected", err)
		}
	}
	if proof.calls != 2 || store.rows != 2 {
		t.Fatal("cleanup omitted original retirement or immutable owner query")
	}
	for _, intent := range []id.AccessIntent{id.Mutate, id.Lifecycle} {
		if _, err = authority.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent); err == nil {
			t.Fatal("cleanup admitted new owner mutation")
		}
	}
	for _, operation := range []oc.AccessOperation{oc.StatAccess, oc.ReadAccess} {
		r, e := oc.NewObjectReadAccess(oc.AccessRequestDetails{Operation: operation, Actor: actor, Owner: owner, ObjectID: w.metadata.Object.ID, Intent: id.Read})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = authority.Discover(ctx, r); e == nil {
			t.Fatal("cleanup admitted package body discovery")
		}
	}
	if _, err = authority.AuthorizeObjectReadInTx(ctx, tx, actor, owner, w.metadata.Object.ID); err == nil {
		t.Fatal("cleanup direct body authority accepted")
	}
	if _, err = authority.AuthorizeOwnerInTx(context.Background(), tx, actor, owner, id.Converge); err == nil {
		t.Fatal("DTO replaced private cleanup witness")
	}
	proof.accepted = false
	if _, err = authority.AuthorizeOwnerInTx(ctx, tx, actor, owner, id.Converge); err == nil {
		t.Fatal("unproven retirement accepted")
	}
	proof.accepted = true
	clear(base.held)
	if _, err = authority.AuthorizeOwnerInTx(ctx, tx, actor, owner, id.Converge); err == nil {
		t.Fatal("cleanup omitted complete locks")
	}
	_ = base.AcquireAll(ctx, tx, w.locks)
	w.materialReleased = false
	if _, err = authority.AuthorizeOwnerInTx(ctx, tx, actor, owner, id.Converge); err == nil {
		t.Fatal("retained material accepted as retirement")
	}
	w.materialReleased = true
	w.returned = false
	if _, err = authority.AuthorizeOwnerInTx(ctx, tx, actor, owner, id.Converge); err == nil {
		t.Fatal("running call accepted as returned")
	}
	w.returned = true
	w.active.Store(false)
	if _, err = authority.AuthorizeOwnerInTx(ctx, tx, actor, owner, id.Converge); err == nil {
		t.Fatal("expired private owner accepted")
	}
}
func TestSkillPrepareUnreturnedCallCannotClaimConvergence(t *testing.T) {
	// A bare completion DTO never substitutes for the actual dispatcher handle.
	for _, w := range []*preparationWitness{nil, {}, {returned: true, materialReleased: true, terminal: &p.SkillPrepareResult{}}} {
		if preparationOwnerIntent(w, id.Converge) {
			t.Fatal("unproven terminal acquired Converge")
		}
	}
}
