package project

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type auditFactFunc func(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) error

func (f auditFactFunc) CheckProjectAuditInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	return f(ctx, tx, entry, key)
}

func TestAuditFactsConstructionClosedAndCopied(t *testing.T) {
	store := &authorityStore{}
	sessions := sessionFunc(func(context.Context, foundation.Tx, identity.Actor) error {
		t.Fatal("construction called Session")
		return nil
	})
	checker := auditFactFunc(func(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) error {
		t.Fatal("construction called checker")
		return nil
	})
	var typedNil auditFactFunc
	for _, tc := range []struct {
		name     string
		key      ac.Producer
		provider ac.ProjectFactAuthority
		code     foundation.Code
	}{
		{"nil", ac.SecretProducer, nil, foundation.DependencyUnbound},
		{"typed-nil", ac.SecretProducer, typedNil, foundation.DependencyUnbound},
		{"object-reserved", ac.ObjectProducer, checker, foundation.DependencyUnbound},
		{"project", ac.ProjectProducer, checker, foundation.InvalidArgument},
		{"artifact", ac.ArtifactProducer, checker, foundation.InvalidArgument},
		{"account", ac.AccountProducer, checker, foundation.InvalidArgument},
		{"master", ac.MasterProducer, checker, foundation.InvalidArgument},
		{"outbox", ac.OutboxProducer, checker, foundation.InvalidArgument},
		{"unknown", "other", checker, foundation.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAuthority(store, AuthorityDependencies{Sessions: sessions, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{tc.key: tc.provider}})
			hasCode(t, err, tc.code)
		})
	}
	for _, facts := range []map[ac.Producer]ac.ProjectFactAuthority{nil, {}} {
		if _, err := NewAuthority(store, AuthorityDependencies{Sessions: sessions, AuditFacts: facts}); err != nil {
			t.Fatal("legacy empty map", err)
		}
	}
	selected := 0
	checker = func(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) error { selected++; return nil }
	facts := map[ac.Producer]ac.ProjectFactAuthority{ac.SecretProducer: checker}
	a, err := NewAuthority(store, AuthorityDependencies{Sessions: sessions, AuditFacts: facts})
	if err != nil {
		t.Fatal(err)
	}
	delete(facts, ac.SecretProducer)
	facts[ac.ObjectProducer] = checker
	if len(a.state().auditFacts) != 1 || a.state().auditFacts[ac.ObjectProducer] != nil {
		t.Fatal("caller map aliases registry")
	}
	if err = a.state().auditFacts[ac.SecretProducer].CheckProjectAuditInTx(context.Background(), foundation.Tx{}, ac.Entry{}, ac.AppendKey{}); err != nil || selected != 1 {
		t.Fatal("selection changed")
	}
}

type auditGateFixture struct {
	a           *Authority
	store       *authorityStore
	actor       identity.Actor
	project     c.ProjectID
	lifecycle   c.Lifecycle
	initialized bool
	sessionErr  error
	order       []string
}

func newAuditGateFixture(t *testing.T, checker ac.ProjectFactAuthority) *auditGateFixture {
	t.Helper()
	f := &auditGateFixture{actor: testActor(t), project: testID[identity.Project](t), lifecycle: c.Active, initialized: true}
	f.store = &authorityStore{tx: foundation.NewTx(), order: &f.order}
	now, creation := time.Now().UTC(), testID[c.Creation](t)
	f.store.row = func(q string, args ...any) postgres.Row {
		if q == "SELECT clock_timestamp()" {
			return valuesRow(now)
		}
		var archived *time.Time
		if f.lifecycle == c.Archived {
			archived = &now
		}
		return valuesRow(f.project.String(), f.actor.Details().UserID, "Demo", "demo", "", string(f.lifecycle), int64(1), nil, now, now, archived, creation.String(), f.initialized, nil)
	}
	sessions := sessionFunc(func(_ context.Context, tx foundation.Tx, actor identity.Actor) error {
		f.order = append(f.order, "session")
		if tx != f.store.tx || !actor.Equal(f.actor) {
			t.Fatal("wrong Session identity or Tx")
		}
		return f.sessionErr
	})
	facts := map[ac.Producer]ac.ProjectFactAuthority{}
	if checker != nil {
		facts[ac.SecretProducer] = checker
	}
	var err error
	f.a, err = NewAuthority(f.store, AuthorityDependencies{Sessions: sessions, AuditFacts: facts})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func auditGateEntry(t *testing.T, f *auditGateFixture, actor identity.Actor, action ac.Action) (ac.Entry, ac.AppendKey) {
	t.Helper()
	scope, _ := identity.InProject(f.project)
	resource, _ := ac.NewResource(ac.SecretResource, testID[struct{}](t).String())
	metadata, _ := ac.SecretMutationMetadata(action, 1, []ac.ChangedField{ac.ValueChanged})
	cause := testID[struct{}](t).String()
	if action == ac.SecretResolve {
		metadata, _ = ac.SecretResolveMetadata(testID[struct{}](t).String(), ac.Model, "")
		if actor.Details().Kind == identity.Service {
			cause = actor.Details().CauseRef
		}
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ac.NewAppendKey(ac.SecretProducer, cause, 0)
	if err != nil {
		t.Fatal(err)
	}
	return entry, key
}

func TestAuditFactsChecksCurrentOwnerBeforeExactDispatch(t *testing.T) {
	type marker struct{}
	ctx := context.WithValue(context.Background(), marker{}, "private-witness")
	var f *auditGateFixture
	var expected ac.Entry
	var key ac.AppendKey
	calls := 0
	providerError := fault(foundation.Forbidden)
	f = newAuditGateFixture(t, auditFactFunc(func(got context.Context, tx foundation.Tx, entry ac.Entry, actual ac.AppendKey) error {
		calls++
		if got != ctx || tx != f.store.tx || !entry.Fields().Actor.Equal(expected.Fields().Actor) || !reflect.DeepEqual(entry.Fields().Metadata.JSON(), expected.Fields().Metadata.JSON()) || actual.Details() != key.Details() {
			t.Fatal("dispatch lost exact witness/entry/key/Tx")
		}
		if !reflect.DeepEqual(f.order[:3], []string{"locks", "session", "executor"}) {
			t.Fatal("fact provider called before current authority", f.order)
		}
		return providerError
	}))
	expected, key = auditGateEntry(t, f, f.actor, ac.SecretCreate)
	hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, expected, key), foundation.Forbidden)
	if calls != 1 {
		t.Fatal("provider not called")
	}
	for _, state := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		f.lifecycle, f.order = state, nil
		hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, expected, key), foundation.ProjectNotActive)
	}
	f.lifecycle, f.initialized = c.Active, false
	hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, expected, key), foundation.ProjectNotActive)
	f.initialized, f.sessionErr = true, fault(foundation.SessionRevoked)
	hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, expected, key), foundation.SessionRevoked)
	f.sessionErr, f.store.lockErr = nil, errors.New("missing user lock")
	hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, expected, key), foundation.DependencyUnavailable)
	if calls != 1 {
		t.Fatal("denial reached provider")
	}
}

func TestAuditFactsServiceResolveAndMissingProvider(t *testing.T) {
	calls := 0
	f := newAuditGateFixture(t, auditFactFunc(func(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) error { calls++; return nil }))
	scope, _ := identity.InProject(f.project)
	reg, _ := identity.RegisterService(identity.SecretService)
	actor, _ := reg.Actor(testID[struct{}](t).String(), scope)
	entry, key := auditGateEntry(t, f, actor, ac.SecretResolve)
	if err := f.a.CheckAppendInTx(context.Background(), f.store.tx, entry, key); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(f.store.locks) != 1 || f.store.locks[0].Mode != foundation.Shared {
		t.Fatal("wrong Service gate")
	}
	for _, event := range []ac.Action{ac.SecretCreate, ac.SecretUpdate} {
		e, k := auditGateEntry(t, f, actor, event)
		hasCode(t, f.a.CheckAppendInTx(context.Background(), f.store.tx, e, k), foundation.Forbidden)
	}
	wrong, _ := ac.NewAppendKey(ac.ObjectProducer, key.Details().CauseRef, 0)
	hasCode(t, f.a.CheckAppendInTx(context.Background(), f.store.tx, entry, wrong), foundation.Forbidden)
	wrong, _ = ac.NewAppendKey(ac.SecretProducer, testID[struct{}](t).String(), 0)
	hasCode(t, f.a.CheckAppendInTx(context.Background(), f.store.tx, entry, wrong), foundation.Forbidden)
	wrong, _ = ac.NewAppendKey(ac.SecretProducer, key.Details().CauseRef, 1)
	hasCode(t, f.a.CheckAppendInTx(context.Background(), f.store.tx, entry, wrong), foundation.Forbidden)
	f.initialized = false
	hasCode(t, f.a.CheckAppendInTx(context.Background(), f.store.tx, entry, key), foundation.ProjectNotActive)
	if calls != 1 {
		t.Fatal("forged Service reached provider")
	}
	f = newAuditGateFixture(t, nil)
	entry, key = auditGateEntry(t, f, f.actor, ac.SecretCreate)
	hasCode(t, f.a.CheckAppendInTx(context.Background(), f.store.tx, entry, key), foundation.DependencyUnbound)
	var zero *Authority
	hasCode(t, zero.CheckAppendInTx(context.Background(), f.store.tx, entry, key), foundation.DependencyUnbound)
}

func TestProjectVariableAuditFactsCurrentGateAndExactDispatch(t *testing.T) {
	f := newAuditGateFixture(t, nil)
	scope, _ := identity.InProject(f.project)
	variable := testID[identity.ProjectVariable](t)
	resource, _ := ac.NewResource(ac.ProjectVariableResource, variable.String())
	metadata, _ := ac.ProjectVariableMetadata(ac.ProjectVariableCreate, ac.ProjectVariableMetadataFields{VariableID: variable.String(), Version: 1, ChangedFields: []string{"created"}})
	entry, e := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: f.actor, Action: ac.ProjectVariableCreate, Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if e != nil {
		t.Fatal(e)
	}
	key, e := ac.NewAppendKey(ac.ProjectVariableProducer, string(digest([]byte("canonical command"))), 0)
	if e != nil {
		t.Fatal(e)
	}
	witness := errors.New("independent domain fact refusal")
	calls := 0
	f.a.state().auditFacts[ac.ProjectVariableProducer] = auditFactFunc(func(_ context.Context, tx foundation.Tx, got ac.Entry, k ac.AppendKey) error {
		calls++
		if tx != f.store.tx || !got.Fields().Actor.Equal(entry.Fields().Actor) || k.Details() != key.Details() {
			t.Fatal("different caller fact")
		}
		return witness
	})
	if e = f.a.checkDomainAuditInTx(context.Background(), f.store.tx, entry, key); !errors.Is(e, witness) {
		t.Fatal("domain refusal hidden", e)
	}
	if calls != 1 {
		t.Fatal("not dispatched")
	}
	for _, phase := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		f.lifecycle = phase
		hasCode(t, f.a.checkDomainAuditInTx(context.Background(), f.store.tx, entry, key), foundation.ProjectNotActive)
	}
	if calls != 1 {
		t.Fatal("fact checker bypassed lifecycle")
	}
	f.lifecycle = c.Active
	f.sessionErr = fault(foundation.SessionRevoked)
	hasCode(t, f.a.checkDomainAuditInTx(context.Background(), f.store.tx, entry, key), foundation.SessionRevoked)
	if calls != 1 {
		t.Fatal("fact checker bypassed Session")
	}
	f.sessionErr = nil
	delete(f.a.state().auditFacts, ac.ProjectVariableProducer)
	hasCode(t, f.a.checkDomainAuditInTx(context.Background(), f.store.tx, entry, key), foundation.DependencyUnbound)
}
