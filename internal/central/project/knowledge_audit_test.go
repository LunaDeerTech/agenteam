package project

import (
	"context"
	"errors"
	"reflect"
	"testing"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func knowledgeAuditEntry(t *testing.T, x *auditGateFixture) (audit.Entry, audit.AppendKey) {
	t.Helper()
	scope, _ := identity.InProject(x.project)
	root := testID[struct{}](t).String()
	resource, _ := audit.NewResource(audit.KnowledgeDocumentResource, root)
	metadata, err := audit.KnowledgeMetadata(audit.KnowledgeDeleteSubtree, audit.KnowledgeMetadataFields{ProjectID: x.project.String(), RootID: root, InitiatorID: x.actor.Details().UserID, ScopeDigest: digest([]byte("exact deleted scope")), DeletedCount: 3})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := audit.NewEntry(audit.EntryFields{Scope: scope, Actor: x.actor, Action: audit.KnowledgeDeleteSubtree, Outcome: audit.Success, Resource: resource, Metadata: metadata})
	if err != nil {
		t.Fatal(err)
	}
	key, err := audit.NewAppendKey(audit.KnowledgeProducer, string(digest([]byte("original command"))), 0)
	if err != nil {
		t.Fatal(err)
	}
	return entry, key
}

func TestKnowledgeAuditProviderSelectionAndDefaultUnbound(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	var typedNil auditFactFunc
	for _, provider := range []audit.ProjectFactAuthority{nil, typedNil} {
		_, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: map[audit.Producer]audit.ProjectFactAuthority{audit.KnowledgeProducer: provider}})
		hasCode(t, err, f.DependencyUnbound)
	}
	entry, key := knowledgeAuditEntry(t, x)
	hasCode(t, x.a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.DependencyUnbound)
	called := 0
	provider := auditFactFunc(func(context.Context, f.Tx, audit.Entry, audit.AppendKey) error { called++; return nil })
	selected := map[audit.Producer]audit.ProjectFactAuthority{audit.KnowledgeProducer: provider}
	a, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: selected})
	if err != nil {
		t.Fatal(err)
	}
	delete(selected, audit.KnowledgeProducer)
	selected[audit.ObjectProducer] = provider
	if err = a.CheckAppendInTx(context.Background(), x.store.tx, entry, key); err != nil || called != 1 {
		t.Fatal("provider selection aliased caller map", err, called)
	}
	if _, err = NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: selected}); err != nil {
		t.Fatal("separate Object provider selection rejected", err)
	}
}

func TestKnowledgeAuditCurrentOwnerThenOriginalWitnessAndFault(t *testing.T) {
	type witness struct{}
	ctx := context.WithValue(context.Background(), witness{}, "original private witness")
	x := newAuditGateFixture(t, nil)
	entry, key := knowledgeAuditEntry(t, x)
	cause := errors.New("unexposed provider cause")
	unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
	calls := 0
	checker := auditFactFunc(func(got context.Context, tx f.Tx, actual audit.Entry, gotKey audit.AppendKey) error {
		calls++
		if got != ctx || tx != x.store.tx || !actual.Fields().Actor.Equal(entry.Fields().Actor) || !reflect.DeepEqual(actual.Fields().Metadata.JSON(), entry.Fields().Metadata.JSON()) || actual.Fields().Scope.Details() != entry.Fields().Scope.Details() || actual.Fields().Resource.Details() != entry.Fields().Resource.Details() || gotKey.Details() != key.Details() {
			t.Fatal("original context/Tx/entry/key changed")
		}
		if len(x.order) < 3 || !reflect.DeepEqual(x.order[:3], []string{"locks", "session", "executor"}) {
			t.Fatal("fact checker ran before current Owner", x.order)
		}
		return unknown
	})
	a, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: map[audit.Producer]audit.ProjectFactAuthority{audit.KnowledgeProducer: checker}})
	if err != nil {
		t.Fatal(err)
	}
	if got := a.CheckAppendInTx(ctx, x.store.tx, entry, key); got != unknown || !errors.Is(got, cause) || calls != 1 {
		t.Fatal("provider Unknown lost identity/cause", got, calls)
	}
	for _, lifecycle := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		x.lifecycle = lifecycle
		hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.ProjectNotActive)
	}
	x.lifecycle, x.initialized = c.Active, false
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.ProjectNotActive)
	x.initialized, x.sessionErr = true, fault(f.SessionRevoked)
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.SessionRevoked)
	x.sessionErr = nil
	hasCode(t, a.CheckAppendInTx(ctx, f.NewTx(), entry, key), f.DependencyUnavailable)
	x.store.lockErr = errors.New("weak or missing lock")
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.DependencyUnavailable)
	x.store.lockErr = nil
	wrong, err := audit.NewAppendKey(audit.SecretProducer, key.Details().CauseRef, 0)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, wrong), f.Forbidden)
	if calls != 1 {
		t.Fatal("denied gate reached Knowledge fact checker", calls)
	}
}
