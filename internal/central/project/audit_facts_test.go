package project

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
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
		{"object-nil", ac.ObjectProducer, nil, foundation.DependencyUnbound},
		{"object-typed-nil", ac.ObjectProducer, typedNil, foundation.DependencyUnbound},
		{"project", ac.ProjectProducer, checker, foundation.InvalidArgument},
		{"artifact-nil", ac.ArtifactProducer, nil, foundation.DependencyUnbound},
		{"artifact-typed-nil", ac.ArtifactProducer, typedNil, foundation.DependencyUnbound},
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
	operation := testID[c.Operation](t).String()
	f.store.row = func(q string, args ...any) postgres.Row {
		if q == "SELECT clock_timestamp()" {
			return valuesRow(now)
		}
		if strings.Contains(q, "FROM agenteam_project.lifecycle_operations") {
			return valuesRow(f.project.String(), f.actor.Details().UserID, string(c.Delete), int64(1), string(c.OperationStopping), "", "")
		}
		var archived *time.Time
		if f.lifecycle == c.Archived {
			archived = &now
		}
		var current *string
		if f.lifecycle == c.Deleting {
			current = &operation
		}
		return valuesRow(f.project.String(), f.actor.Details().UserID, "Demo", "demo", "", string(f.lifecycle), int64(1), nil, now, now, archived, creation.String(), f.initialized, current)
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
	if action == ac.SecretDelete {
		metadata, _ = ac.SecretMutationMetadata(action, 1, nil)
	}
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

type secretAuditGateStore struct{ *authorityStore }

func (*secretAuditGateStore) Acquire(context.Context, foundation.Tx, foundation.LockKey, foundation.LockMode) error {
	return errors.New("audit observation must not acquire locks")
}

func TestSecretTechnicalMutationRequiresOriginalSecretProof(t *testing.T) {
	for _, action := range []ac.Action{ac.SecretCreate, ac.SecretUpdate, ac.SecretDelete} {
		t.Run(string(action), func(t *testing.T) {
			f := newAuditGateFixture(t, nil)
			scope, _ := identity.InProject(f.project)
			registration, _ := identity.RegisterService(identity.SecretService)
			actor, _ := registration.Actor(testID[struct{}](t).String(), scope)
			entry, _ := auditGateEntry(t, f, actor, action)
			key, err := ac.NewAppendKey(ac.SecretProducer, string(digest([]byte("original sealed OAuth command"))), 0)
			if err != nil {
				t.Fatal(err)
			}
			checker, err := secret.NewProjectAuditAuthority(&secretAuditGateStore{f.store})
			if err != nil {
				t.Fatal(err)
			}
			f.a.state().auditFacts[ac.SecretProducer] = checker
			// The real Secret checker must reject a valid technical actor and
			// public audit tuple without the original private write proof.
			hasCode(t, f.a.CheckAppendInTx(context.Background(), f.store.tx, entry, key), foundation.Forbidden)
			type marker struct{}
			ctx := context.WithValue(context.Background(), marker{}, "original write context")
			calls := 0
			refusal := fault(foundation.Forbidden)
			f.a.state().auditFacts[ac.SecretProducer] = auditFactFunc(func(got context.Context, tx foundation.Tx, gotEntry ac.Entry, gotKey ac.AppendKey) error {
				calls++
				if got != ctx || tx != f.store.tx || !gotEntry.Fields().Actor.Equal(entry.Fields().Actor) || !reflect.DeepEqual(gotEntry.Fields().Metadata.JSON(), entry.Fields().Metadata.JSON()) || gotEntry.Fields().Resource.Details() != entry.Fields().Resource.Details() || gotEntry.Fields().Action != action || gotKey.Details() != key.Details() {
					t.Fatal("technical write proof changed at Project boundary")
				}
				return refusal
			})
			hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, entry, key), foundation.Forbidden)
			if calls != 1 || len(f.store.locks) != 1 || f.store.locks[0].Mode != foundation.Shared {
				t.Fatal("missing current Project gate or exact fact dispatch")
			}
			for _, phase := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
				f.lifecycle = phase
				hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, entry, key), foundation.ProjectNotActive)
			}
			f.lifecycle, f.initialized = c.Active, false
			hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, entry, key), foundation.ProjectNotActive)
			f.initialized = true
			f.store.lockErr = errors.New("missing project lock")
			hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, entry, key), foundation.DependencyUnavailable)
			if calls != 1 {
				t.Fatal("closed Project or missing lock reached Secret facts")
			}
		})
	}
}

func TestMCPAgentSecretResolveRequiresCurrentProjectAndOriginalProof(t *testing.T) {
	f := newAuditGateFixture(t, nil)
	actor, _ := identity.NewAgentRun(f.project, testID[identity.Agent](t), testID[identity.Execution](t))
	entry, key := auditGateEntry(t, f, actor, ac.SecretResolve)
	fields := entry.Fields()
	fields.Metadata, _ = ac.SecretResolveMetadata(testID[struct{}](t).String(), ac.MCP, "")
	fields.Associations.OperationID = testID[struct{}](t).String()
	fields.Associations.ToolID = testID[identity.Tool](t).String()
	fields.Associations.RequestID = testID[struct{}](t).String()
	var err error
	entry, err = ac.NewEntry(fields)
	if err != nil {
		t.Fatal(err)
	}
	checker, err := secret.NewProjectAuditAuthority(&secretAuditGateStore{f.store})
	if err != nil {
		t.Fatal(err)
	}
	f.a.state().auditFacts[ac.SecretProducer] = checker
	// Actual Secret authority rejects public Actor/Operation/Tool tuples in
	// the absence of its same-transaction resolution and Usage witness.
	hasCode(t, f.a.CheckAppendInTx(context.Background(), f.store.tx, entry, key), foundation.Forbidden)
	type marker struct{}
	ctx := context.WithValue(context.Background(), marker{}, "original MCP resolution")
	calls := 0
	f.a.state().auditFacts[ac.SecretProducer] = auditFactFunc(func(got context.Context, tx foundation.Tx, e ac.Entry, k ac.AppendKey) error {
		calls++
		if got != ctx || tx != f.store.tx || !e.Fields().Actor.Equal(actor) || e.Fields().Associations != entry.Fields().Associations || k.Details() != key.Details() {
			t.Fatal("lost exact original MCP resolution")
		}
		return fault(foundation.Forbidden)
	})
	hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, entry, key), foundation.Forbidden)
	if calls != 1 || len(f.store.locks) != 1 || f.store.locks[0].Mode != foundation.Shared {
		t.Fatal("missing Project gate or original proof dispatch")
	}
	for _, phase := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		f.lifecycle = phase
		hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, entry, key), foundation.ProjectNotActive)
	}
	f.lifecycle, f.initialized = c.Active, false
	hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, entry, key), foundation.ProjectNotActive)
	f.initialized = true
	f.store.lockErr = errors.New("missing Project lock")
	hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, entry, key), foundation.DependencyUnavailable)
	f.store.lockErr = nil
	for _, mutate := range []func(*ac.EntryFields){
		func(v *ac.EntryFields) {
			v.Metadata, _ = ac.SecretResolveMetadata(testID[struct{}](t).String(), ac.Model, "")
		},
		func(v *ac.EntryFields) { v.Associations.OperationID = "" },
		func(v *ac.EntryFields) { v.Associations.ToolID = "" },
		func(v *ac.EntryFields) { v.Associations.RequestID = "" },
	} {
		v := entry.Fields()
		mutate(&v)
		bad, err := ac.NewEntry(v)
		if err != nil {
			t.Fatal(err)
		}
		hasCode(t, f.a.CheckAppendInTx(ctx, f.store.tx, bad, key), foundation.Forbidden)
	}
	if calls != 1 {
		t.Fatal("closed Project or foreign invocation shape reached Secret provider")
	}
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

func TestSecretVariableAuditReadShapeDoesNotAuthorizeProjectFacts(t *testing.T) {
	f := newAuditGateFixture(t, nil)
	// The write-fact phase now has an exact Secret route. Use the real D10
	// checker: public read-compatible metadata still cannot mint its private
	// same-transaction mutation witness. No allow/deny stand-in proves this.
	checker, err := pv.NewAuthority(f.store)
	if err != nil {
		t.Fatal(err)
	}
	a, err := NewAuthority(f.store, AuthorityDependencies{
		Sessions: f.a.state().sessions,
		AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{
			ac.ProjectVariableProducer: checker,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := identity.InProject(f.project)
	variable := testID[identity.ProjectVariable](t)
	resource, _ := ac.NewResource(ac.ProjectVariableResource, variable.String())
	key, _ := ac.NewAppendKey(ac.ProjectVariableProducer, string(digest([]byte("canonical Secret command"))), 0)
	for _, action := range []ac.Action{ac.ProjectSecretVariableCreate, ac.ProjectSecretVariableUpdate, ac.ProjectSecretVariableDelete} {
		version, fields := foundation.Version(2), []string{"value"}
		if action == ac.ProjectSecretVariableCreate {
			version, fields = 1, []string{"created"}
		} else if action == ac.ProjectSecretVariableDelete {
			fields = []string{"deleted"}
		}
		metadata, err := ac.ProjectSecretVariableMetadata(action, ac.ProjectSecretVariableMetadataFields{VariableID: variable.String(), Version: version, ChangedFields: fields})
		if err != nil {
			t.Fatal(err)
		}
		entry, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: f.actor, Action: action, Outcome: ac.Success, Resource: resource, Metadata: metadata})
		if err != nil {
			t.Fatal(err)
		}
		hasCode(t, a.CheckAppendInTx(context.Background(), f.store.tx, entry, key), foundation.Forbidden)
		if len(f.order) == 0 {
			t.Fatal("Secret fact route skipped current gate")
		}
		f.sessionErr = fault(foundation.SessionRevoked)
		hasCode(t, a.CheckAppendInTx(context.Background(), f.store.tx, entry, key), foundation.SessionRevoked)
		f.sessionErr = nil
		f.lifecycle = c.Archived
		hasCode(t, a.CheckAppendInTx(context.Background(), f.store.tx, entry, key), foundation.ProjectNotActive)
		f.lifecycle = c.Active
	}
}
