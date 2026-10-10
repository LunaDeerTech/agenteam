package project

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func agentProjectAuditEntry(t *testing.T, x *auditGateFixture) (ac.Entry, ac.AppendKey) {
	t.Helper()
	scope, _ := id.InProject(x.project)
	agent := testID[id.Agent](t)
	resource, _ := ac.NewResource(ac.AgentResource, agent.String())
	metadata, err := ac.AgentMetadata(ac.AgentUpdate, ac.AgentMetadataFields{AgentID: agent.String(), Version: 2, CommandID: testID[struct{}](t).String(), ChangedFields: []string{"name"}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: x.actor, Action: ac.AgentUpdate, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ac.NewAppendKey(ac.AgentProducer, digest([]byte("original Agent command")).String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	return entry, key
}

func TestAgentAuditDelegatesOriginalWitnessAfterCurrentOwner(t *testing.T) {
	type privateMarker struct{}
	ctx := context.WithValue(context.Background(), privateMarker{}, "original witness")
	x := newAuditGateFixture(t, nil)
	entry, key := agentProjectAuditEntry(t, x)
	cause := errors.New("private domain refusal")
	want := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
	providerErr := error(nil)
	calls := 0
	facts := map[ac.Producer]ac.ProjectFactAuthority{ac.AgentProducer: auditFactFunc(func(got context.Context, tx f.Tx, actual ac.Entry, actualKey ac.AppendKey) error {
		calls++
		a, b := actual.Fields(), entry.Fields()
		if got != ctx || tx != x.store.tx || !a.Actor.Equal(b.Actor) || a.Scope.Details() != b.Scope.Details() || a.Action != b.Action || a.Outcome != b.Outcome || a.Resource.Details() != b.Resource.Details() || a.Associations != b.Associations || !sameMetadata(a.Metadata, b.Metadata) || actualKey.Details() != key.Details() {
			t.Fatal("original sameTx witness/entry changed")
		}
		if len(x.order) < 3 || !reflect.DeepEqual(x.order[:3], []string{"locks", "session", "executor"}) || len(slices.DeleteFunc(slices.Clone(x.order), func(v string) bool { return v != "session" })) != 2 {
			t.Fatal("Read and Mutate current gates missing", x.order)
		}
		return providerErr
	})}
	a, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: facts})
	if err != nil {
		t.Fatal(err)
	}
	delete(facts, ac.AgentProducer)
	if err = a.CheckAppendInTx(ctx, x.store.tx, entry, key); err != nil || calls != 1 {
		t.Fatal("immutable registration/valid delegate", err)
	}
	providerErr, x.order = want, nil
	if err = a.CheckAppendInTx(ctx, x.store.tx, entry, key); err != want || !errors.Is(err, cause) || calls != 2 {
		t.Fatal("domain Unknown changed", err)
	}
	for _, lifecycle := range []pc.Lifecycle{pc.Archiving, pc.Archived, pc.Deleting} {
		x.lifecycle = lifecycle
		hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.ProjectNotActive)
	}
	x.lifecycle, x.initialized = pc.Active, false
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.ProjectNotActive)
	x.initialized, x.sessionErr = true, fault(f.SessionRevoked)
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.SessionRevoked)
	x.sessionErr = nil
	hasCode(t, a.CheckAppendInTx(ctx, f.NewTx(), entry, key), f.DependencyUnavailable)
	x.store.lockErr = errors.New("missing original held locks")
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.DependencyUnavailable)
	x.store.lockErr = nil
	wrong, _ := ac.NewAppendKey(ac.SecretProducer, key.Details().CauseRef, 0)
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, wrong), f.Forbidden)
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err = a.CheckAppendInTx(canceled, x.store.tx, entry, key); !errors.Is(err, context.Canceled) {
		t.Fatal("cancel lost", err)
	}
	if calls != 2 {
		t.Fatal("denied request reached Agent provider")
	}
}

func TestAgentAuditOptionalRegistrationNeverSubstitutesOtherProducer(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	entry, key := agentProjectAuditEntry(t, x)
	hasCode(t, x.a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.DependencyUnbound)
	for _, provider := range []ac.ProjectFactAuthority{nil, auditFactFunc(nil)} {
		_, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.AgentProducer: provider}})
		hasCode(t, err, f.DependencyUnbound)
	}
	a, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.AgentProducer: auditFactFunc(func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error {
		t.Fatal("other producer dispatched to Agent")
		return nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	secret, secretKey := auditGateEntry(t, x, x.actor, ac.SecretCreate)
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, secret, secretKey), f.DependencyUnbound)
}
