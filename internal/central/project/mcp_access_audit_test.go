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

type mcpAccessProofControl struct {
	auditFactFunc
	check auditFactFunc
}

func (p *mcpAccessProofControl) RequireMCPAccessAuditInTx(ctx context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) error {
	return p.check(ctx, tx, e, k)
}

func mcpAccessEntry(t *testing.T, x *auditGateFixture) (ac.Entry, ac.AppendKey) {
	t.Helper()
	e, k := modelAccessEntry(t, x, ac.AddressForbidden)
	v := e.Fields()
	v.Metadata, _ = ac.DenialMetadata(ac.MCP, ac.AddressForbidden, 1)
	v.Associations = ac.Associations{RequestID: k.Details().CauseRef}
	e, err := ac.NewEntry(v)
	if err != nil {
		t.Fatal(err)
	}
	return e, k
}

func TestMCPStartupAccessAuditUsesOriginalProjectAndPrivateProof(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	e, k := mcpAccessEntry(t, x)
	type originalKey struct{}
	ctx := context.WithValue(context.Background(), originalKey{}, "original owner")
	cause := errors.New("private proof refusal")
	want := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
	calls := 0
	x.a.state().auditFacts[ac.MCPProducer] = &mcpAccessProofControl{check: func(got context.Context, tx f.Tx, actual ac.Entry, actualKey ac.AppendKey) error {
		calls++
		if got != ctx || tx != x.store.tx || !actual.Fields().Actor.Equal(e.Fields().Actor) || actualKey.Details() != k.Details() || !sameMetadata(actual.Fields().Metadata, e.Fields().Metadata) {
			t.Fatal("changed original denial")
		}
		if !reflect.DeepEqual(x.order, []string{"locks", "executor", "query"}) || len(x.store.locks) != 1 || x.store.locks[0].Mode != f.Shared {
			t.Fatal("added locks or skipped current Project", x.order)
		}
		return want
	}}
	wrapped, err := NewRuntimeAccessAuditAuthority(x.a, auditFactFunc(func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error {
		t.Fatal("MCP denial borrowed Model authority")
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err = wrapped.CheckAppendInTx(ctx, x.store.tx, e, k); err != want || !errors.Is(err, cause) || calls != 1 {
		t.Fatal("private original proof lost", err)
	}
	for _, lifecycle := range []pc.Lifecycle{pc.Archiving, pc.Archived, pc.Deleting} {
		x.lifecycle = lifecycle
		hasCode(t, wrapped.CheckAppendInTx(ctx, x.store.tx, e, k), f.ProjectNotActive)
	}
	x.lifecycle = pc.Active
	x.initialized = false
	hasCode(t, wrapped.CheckAppendInTx(ctx, x.store.tx, e, k), f.ProjectNotActive)
	if calls != 1 {
		t.Fatal("closed Project reached source proof")
	}
}

func TestMCPStartupAccessAuditRejectsForeignScopeAndMissingProof(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	e, k := mcpAccessEntry(t, x)
	calls := 0
	x.a.state().auditFacts[ac.MCPProducer] = &mcpAccessProofControl{check: func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error { calls++; return nil }}
	for _, change := range []func(*ac.EntryFields){
		func(v *ac.EntryFields) { v.Actor = x.actor },
		func(v *ac.EntryFields) { v.Associations.RequestID = testID[struct{}](t).String() },
		func(v *ac.EntryFields) { v.Associations.OperationID = testID[struct{}](t).String() },
		func(v *ac.EntryFields) { scope, _ := id.InProject(testID[id.Project](t)); v.Scope = scope },
	} {
		v := e.Fields()
		change(&v)
		other, err := ac.NewEntry(v)
		if err != nil {
			continue
		}
		hasCode(t, x.a.CheckAppendInTx(context.Background(), x.store.tx, other, k), f.Forbidden)
	}
	if calls != 0 {
		t.Fatal("invalid tuple reached private proof")
	}
	x.a.state().auditFacts[ac.MCPProducer] = auditFactFunc(func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error {
		t.Fatal("generic Audit port granted network facts")
		return nil
	})
	hasCode(t, x.a.CheckAppendInTx(context.Background(), x.store.tx, e, k), f.DependencyUnbound)
}
