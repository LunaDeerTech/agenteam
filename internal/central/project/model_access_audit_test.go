package project

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func modelAccessEntry(t *testing.T, fixture *auditGateFixture, reason ac.Reason) (ac.Entry, ac.AppendKey) {
	t.Helper()
	scope, _ := id.InProject(fixture.project)
	cause := testID[struct{}](t).String()
	registration, _ := id.RegisterService(id.OutboundService)
	actor, _ := registration.Actor(cause, scope)
	resource, _ := ac.NewResource(ac.PolicyResource, "")
	metadata, _ := ac.DenialMetadata(ac.Model, reason, 1)
	entry, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: ac.AccessDeny, Outcome: ac.Denied, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ac.NewAppendKey(ac.AccessProducer, cause, 0)
	if err != nil {
		t.Fatal(err)
	}
	return entry, key
}

func withModelAccessFacts(t *testing.T, fixture *auditGateFixture, provider ac.ProjectFactAuthority) *Authority {
	t.Helper()
	a, err := NewAuthority(fixture.store, AuthorityDependencies{Sessions: fixture.a.state().sessions, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.AccessProducer: provider}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestModelAccessAuditExactTxAndOriginalDenyFault(t *testing.T) {
	type marker struct{}
	ctx := context.WithValue(context.Background(), marker{}, "private owner")
	x := newAuditGateFixture(t, nil)
	cause := errors.New("private runtime cause")
	want := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
	for _, reason := range []ac.Reason{ac.AddressForbidden, ac.RedirectDenied, ac.ResponseLimit} {
		entry, key := modelAccessEntry(t, x, reason)
		hasCode(t, x.a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.DependencyUnbound)
		calls := 0
		a := withModelAccessFacts(t, x, auditFactFunc(func(got context.Context, tx f.Tx, actual ac.Entry, actualKey ac.AppendKey) error {
			calls++
			if got != ctx || tx != x.store.tx || !actual.Fields().Actor.Equal(entry.Fields().Actor) || !sameMetadata(actual.Fields().Metadata, entry.Fields().Metadata) || actualKey.Details() != key.Details() {
				t.Fatal("original callback context/Tx/entry/key changed")
			}
			if !reflect.DeepEqual(x.order, []string{"locks", "executor", "query"}) || len(x.store.locks) != 1 || x.store.locks[0].Mode != f.Shared {
				t.Fatal("callback skipped current Project SH or added a lock")
			}
			return want
		}))
		x.order = nil
		if err := a.CheckAppendInTx(ctx, x.store.tx, entry, key); err != want || !errors.Is(err, cause) || calls != 1 {
			t.Fatal("runtime failure changed or callback was skipped")
		}
	}
}

func TestModelAccessAuditCurrentGateAndExactActor(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	entry, key := modelAccessEntry(t, x, ac.AddressForbidden)
	calls := 0
	a := withModelAccessFacts(t, x, auditFactFunc(func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error { calls++; return nil }))
	for _, gate := range []struct {
		initialized bool
		lifecycle   pc.Lifecycle
	}{{false, pc.Active}, {true, pc.Archived}, {true, pc.Deleting}} {
		x.initialized, x.lifecycle = gate.initialized, gate.lifecycle
		hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.ProjectNotActive)
	}
	x.initialized, x.lifecycle = true, pc.Active
	x.store.lockErr = errors.New("weak project lock")
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.DependencyUnavailable)
	x.store.lockErr = nil
	hasCode(t, a.CheckAppendInTx(context.Background(), f.NewTx(), entry, key), f.DependencyUnavailable)
	for _, mutate := range []func(*ac.EntryFields){
		func(e *ac.EntryFields) { e.Actor = x.actor },
		func(e *ac.EntryFields) { e.Metadata, _ = ac.DenialMetadata(ac.MCP, ac.AddressForbidden, 1) },
		func(e *ac.EntryFields) {
			registration, _ := id.RegisterService(id.SecretService)
			e.Actor, _ = registration.Actor(key.Details().CauseRef, e.Scope)
		},
		func(e *ac.EntryFields) {
			registration, _ := id.RegisterService(id.OutboundService)
			e.Actor, _ = registration.Actor(testID[struct{}](t).String(), e.Scope)
		},
	} {
		fields := entry.Fields()
		mutate(&fields)
		other, err := ac.NewEntry(fields)
		if err != nil {
			t.Fatal(err)
		}
		hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, other, key), f.Forbidden)
	}
	if calls != 0 {
		t.Fatal("invalid gate reached Runtime facts")
	}
}

func TestModelAccessAuditRegistrationDoesNotGrantOtherProducer(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	for _, provider := range []ac.ProjectFactAuthority{nil, auditFactFunc(nil)} {
		_, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.AccessProducer: provider}})
		hasCode(t, err, f.DependencyUnbound)
	}
	a := withModelAccessFacts(t, x, auditFactFunc(func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error {
		t.Fatal("other producer reached Runtime")
		return nil
	}))
	entry, key := auditGateEntry(t, x, x.actor, ac.SecretCreate)
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.DependencyUnbound)
}
