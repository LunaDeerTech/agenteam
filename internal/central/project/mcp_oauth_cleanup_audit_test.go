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
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type mcpCleanupAuditProbe struct {
	auditFactFunc
	cleanup func(context.Context, f.Tx, ac.Entry, ac.AppendKey) (bool, error)
}

func (p *mcpCleanupAuditProbe) CheckMCPOAuthCleanupAuditInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) (bool, error) {
	return p.cleanup(ctx, tx, e, k)
}

func mcpCleanupAuditEntry(t *testing.T, fixture *auditGateFixture) (ac.Entry, ac.AppendKey) {
	t.Helper()
	scope, _ := id.InProject(fixture.project)
	registration, _ := id.RegisterService(id.SecretService)
	actor, _ := registration.Actor(testID[struct{}](t).String(), scope)
	entry, _ := auditGateEntry(t, fixture, actor, ac.SecretDelete)
	key, err := ac.NewAppendKey(ac.SecretProducer, string(digest([]byte("original OAuth flow cleanup command"))), 0)
	if err != nil {
		t.Fatal(err)
	}
	return entry, key
}

func TestMCPOAuthCleanupAuditRequiresOriginalSecretProofAndCurrentProject(t *testing.T) {
	fixture := newAuditGateFixture(t, nil)
	entry, key := mcpCleanupAuditEntry(t, fixture)
	actual, err := secret.NewProjectAuditAuthority(&secretAuditGateStore{fixture.store})
	if err != nil {
		t.Fatal(err)
	}
	fixture.a.state().auditFacts[ac.SecretProducer] = actual
	// A valid Service actor/entry/key cannot manufacture the actual Secret
	// engine's private cleanup proof, even after the Project is archived.
	for _, state := range []pc.Lifecycle{pc.Archiving, pc.Archived, pc.Deleting} {
		fixture.lifecycle = state
		hasCode(t, fixture.a.CheckAppendInTx(context.Background(), fixture.store.tx, entry, key), f.ProjectNotActive)
	}
	fixture.lifecycle = pc.Active
	hasCode(t, fixture.a.CheckAppendInTx(context.Background(), fixture.store.tx, entry, key), f.Forbidden)

	type originalContext struct{}
	ctx := context.WithValue(context.Background(), originalContext{}, "original private cleanup")
	calls, fallback := 0, 0
	proof := &mcpCleanupAuditProbe{auditFactFunc: func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error {
		fallback++
		return nil
	}, cleanup: func(got context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) (bool, error) {
		calls++
		if got != ctx || tx != fixture.store.tx || !e.Fields().Actor.Equal(entry.Fields().Actor) ||
			e.Fields().Resource.Details() != entry.Fields().Resource.Details() || !reflect.DeepEqual(e.Fields().Metadata.JSON(), entry.Fields().Metadata.JSON()) || k.Details() != key.Details() {
			t.Fatal("cleanup dispatch changed original context/Tx/entry/key")
		}
		if len(fixture.order) < 2 || fixture.order[0] != "locks" || fixture.order[1] != "executor" {
			t.Fatal("cleanup proof preceded the current Project read", fixture.order)
		}
		return true, nil
	}}
	fixture.a.state().auditFacts[ac.SecretProducer] = proof
	for _, state := range []pc.Lifecycle{pc.Active, pc.Archiving, pc.Archived, pc.Deleting} {
		fixture.lifecycle, fixture.order = state, nil
		if err := fixture.a.CheckAppendInTx(ctx, fixture.store.tx, entry, key); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 4 || fallback != 0 {
		t.Fatal("cleanup did not use exactly its mandatory private checker")
	}
	fixture.initialized = false
	hasCode(t, fixture.a.CheckAppendInTx(ctx, fixture.store.tx, entry, key), f.ProjectNotActive)
	fixture.initialized = true
	fixture.store.lockErr = errors.New("original Project lock missing")
	hasCode(t, fixture.a.CheckAppendInTx(ctx, fixture.store.tx, entry, key), f.DependencyUnavailable)
	fixture.store.lockErr = nil
	fields := entry.Fields()
	otherScope, _ := id.InProject(testID[id.Project](t))
	fields.Scope = otherScope
	wrong, err := ac.NewEntry(fields)
	if err == nil {
		hasCode(t, fixture.a.CheckAppendInTx(ctx, fixture.store.tx, wrong, key), f.Forbidden)
	}
	if calls != 4 || fallback != 0 {
		t.Fatal("invalid current Project or foreign tuple reached cleanup proof")
	}
}

func TestMCPOAuthCleanupAuditRefusalNeverFallsBackOrAdmitsOtherWrites(t *testing.T) {
	fixture := newAuditGateFixture(t, nil)
	entry, key := mcpCleanupAuditEntry(t, fixture)
	refusal := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(errors.New("original cleanup observation"))
	calls, fallback := 0, 0
	fixture.a.state().auditFacts[ac.SecretProducer] = &mcpCleanupAuditProbe{
		auditFactFunc: func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error { fallback++; return nil },
		cleanup:       func(context.Context, f.Tx, ac.Entry, ac.AppendKey) (bool, error) { calls++; return true, refusal },
	}
	if got := fixture.a.CheckAppendInTx(context.Background(), fixture.store.tx, entry, key); got != refusal {
		t.Fatalf("original refusal identity changed: %T", got)
	}
	fixture.lifecycle = pc.Archived
	for _, action := range []ac.Action{ac.SecretCreate, ac.SecretUpdate} {
		other, _ := auditGateEntry(t, fixture, entry.Fields().Actor, action)
		hasCode(t, fixture.a.CheckAppendInTx(context.Background(), fixture.store.tx, other, key), f.ProjectNotActive)
	}
	human, humanKey := auditGateEntry(t, fixture, fixture.actor, ac.SecretDelete)
	hasCode(t, fixture.a.CheckAppendInTx(context.Background(), fixture.store.tx, human, humanKey), f.ProjectNotActive)
	if calls != 1 || fallback != 0 {
		t.Fatal("cleanup refusal fell back or authorized a fresh/Human write")
	}
}
