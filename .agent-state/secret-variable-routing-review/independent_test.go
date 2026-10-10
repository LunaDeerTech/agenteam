package project

import (
	"context"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
)

// Current Project/Session and SQL are controlled ports. The delegated D10 fact
// provider is the actual production Authority, never an allow/deny replacement.
func TestIndependentSecretRoutePreservesContextAndDenialOrder(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	real, err := pv.NewAuthority(x.store)
	if err != nil {
		t.Fatal(err)
	}
	scope, _ := i.InProject(x.project)
	variable := testID[i.ProjectVariable](t)
	resource, _ := ac.NewResource(ac.ProjectVariableResource, variable.String())
	metadata, err := ac.ProjectSecretVariableMetadata(ac.ProjectSecretVariableCreate,
		ac.ProjectSecretVariableMetadataFields{VariableID: variable.String(), Version: 1, ChangedFields: []string{"created"}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: x.actor, Action: ac.ProjectSecretVariableCreate,
		Outcome: ac.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ac.NewAppendKey(ac.ProjectVariableProducer, string(digest([]byte("review original command"))), 0)
	if err != nil {
		t.Fatal(err)
	}
	// Public metadata in a caller-owned context is not a private witness.
	type callerKey struct{}
	ctx := context.WithValue(context.Background(), callerKey{}, entry)
	calls := 0
	forward := auditFactFunc(func(got context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) error {
		calls++
		if got != ctx || tx != x.store.tx || !e.Fields().Actor.Equal(entry.Fields().Actor) ||
			string(e.Fields().Metadata.JSON()) != string(entry.Fields().Metadata.JSON()) || k.Details() != key.Details() {
			t.Fatal("original context/transaction/entry/key lost")
		}
		return real.CheckProjectAuditInTx(got, tx, e, k)
	})
	a, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions,
		AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.ProjectVariableProducer: forward}})
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.Forbidden)
	if calls != 1 {
		t.Fatal("real provider not reached exactly once")
	}
	x.sessionErr = fault(f.SessionRevoked)
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.SessionRevoked)
	x.sessionErr, x.lifecycle = nil, c.Archived
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.ProjectNotActive)
	if calls != 1 {
		t.Fatal("current denial reached fact provider")
	}
}

func TestIndependentSecretDependenciesRejectSessionIssuerAndWeakLocksBeforeIO(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	d := variableProjectRequest(t, x).Details()
	d.Event.Header.EventType = "project.secret_variable_changed"
	r, err := oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := x.a.Discover(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if err = x.a.ValidateInTx(context.Background(), x.store.tx, r, deps); err != nil {
		t.Fatal(err)
	}
	x.order = nil
	user, _ := f.ParseID[i.User](x.actor.Details().UserID)
	d.Actor, _ = i.NewHuman(user, testID[i.Session](t))
	otherSession, err := oc.NewProjectRequest(d)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, otherSession, deps), f.Forbidden)
	other, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions})
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, other.ValidateInTx(context.Background(), x.store.tx, r, deps), f.Forbidden)
	weakened, err := oc.NewDependencies(x.a.state().projectIssuer, deps.Binding(), deps.Locks()[:1], deps.Opaque())
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, x.a.ValidateInTx(context.Background(), x.store.tx, r, weakened), f.Forbidden)
	if len(x.order) != 0 {
		t.Fatal("rebound dependencies reached Store/Session")
	}
}
