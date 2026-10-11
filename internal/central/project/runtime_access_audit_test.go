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

func TestRuntimeAccessAuditAuthorityConstructionAndDelegation(t *testing.T) {
	type witness struct{}
	ctx := context.WithValue(context.Background(), witness{}, "original Secret witness")
	secretRefusal := fault(f.Forbidden)
	secretCalls, runtimeCalls := 0, 0
	var entry ac.Entry
	var key ac.AppendKey
	var x *auditGateFixture
	x = newAuditGateFixture(t, auditFactFunc(func(got context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) error {
		secretCalls++
		if got != ctx || tx != x.store.tx || !e.Fields().Actor.Equal(entry.Fields().Actor) || !sameMetadata(e.Fields().Metadata, entry.Fields().Metadata) || k.Details() != key.Details() {
			t.Fatal("ordinary Secret dispatch lost original arguments")
		}
		return secretRefusal
	}))
	entry, key = auditGateEntry(t, x, x.actor, ac.SecretUpdate)
	runtime := auditFactFunc(func(context.Context, f.Tx, ac.Entry, ac.AppendKey) error {
		runtimeCalls++
		return nil
	})
	for _, original := range []*Authority{nil, {}} {
		got, err := NewRuntimeAccessAuditAuthority(original, runtime)
		hasCode(t, err, f.DependencyUnbound)
		if got != nil {
			t.Fatal("unbound original produced an adapter")
		}
	}
	for _, facts := range []ac.ProjectFactAuthority{nil, auditFactFunc(nil)} {
		got, err := NewRuntimeAccessAuditAuthority(x.a, facts)
		hasCode(t, err, f.DependencyUnbound)
		if got != nil {
			t.Fatal("unbound Runtime produced an adapter")
		}
	}
	a, err := NewRuntimeAccessAuditAuthority(x.a, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.a.state().auditFacts) != 1 || x.a.state().auditFacts[ac.AccessProducer] != nil || len(x.order) != 0 {
		t.Fatal("construction mutated the original map or accessed a dependency")
	}
	if err = a.CheckAppendInTx(ctx, x.store.tx, entry, key); err != secretRefusal || secretCalls != 1 {
		t.Fatal("ordinary producer no longer delegates to original facts", err)
	}
	if !reflect.DeepEqual(x.order[:3], []string{"locks", "session", "executor"}) {
		t.Fatal("ordinary producer lost its current Owner gate", x.order)
	}
	x.sessionErr = fault(f.SessionRevoked)
	if err = a.CheckAppendInTx(ctx, x.store.tx, entry, key); err != x.sessionErr || secretCalls != 1 {
		t.Fatal("ordinary append bypassed current Session")
	}
	if _, err = a.AuthorizeProject(ctx, x.store.tx, x.actor, x.project, id.Read); err != x.sessionErr {
		t.Fatal("AuthorizeProject lost original Session failure", err)
	}
	x.sessionErr = nil
	scope, err := id.InProject(x.project)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := a.AuthorizeProject(ctx, x.store.tx, x.actor, x.project, id.Read)
	if err != nil || !grant.Matches(x.actor, scope, id.Read) {
		t.Fatal("original Owner read changed", err)
	}
	hasCode(t, a.CheckServiceLookup(ctx, x.actor, scope, key), f.DependencyUnbound)
	cause, err := ac.NewLifecycleCause(testID[ac.LifecycleOperation](t), ac.Archive, 1)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, a.CheckCleanupInTx(ctx, x.store.tx, x.actor, cause, x.project), f.DependencyUnbound)
	if runtimeCalls != 0 || len(x.a.state().auditFacts) != 1 || x.a.state().auditFacts[ac.AccessProducer] != nil {
		t.Fatal("delegation borrowed Runtime facts or changed the original map")
	}
}

func TestRuntimeAccessAuditAuthorityOriginalFacts(t *testing.T) {
	// This is a controlled fact-port test, not a Runtime Invocation grant.
	// The real Runtime's live handoff and accepted Invocation are exercised by
	// the native Execution/D04 fixture.
	type witness struct{}
	ctx := context.WithValue(context.Background(), witness{}, "controlled live handoff")
	x := newAuditGateFixture(t, nil)
	entry, key := modelAccessEntry(t, x, ac.AddressForbidden)
	originalCause := errors.New("original Runtime uncertainty")
	unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(originalCause)
	var result error
	calls := 0
	a, err := NewRuntimeAccessAuditAuthority(x.a, auditFactFunc(func(got context.Context, tx f.Tx, e ac.Entry, k ac.AppendKey) error {
		calls++
		if got != ctx {
			return fault(f.Forbidden)
		}
		if tx != x.store.tx || !e.Fields().Actor.Equal(entry.Fields().Actor) || !sameMetadata(e.Fields().Metadata, entry.Fields().Metadata) || k.Details() != key.Details() {
			t.Fatal("Model dispatch changed original Tx/entry/key")
		}
		if !reflect.DeepEqual(x.order, []string{"locks", "executor", "query"}) || len(x.store.locks) != 1 || x.store.locks[0].Mode != f.Shared || f.CompareLockKeys(x.store.locks[0].Key, projectLock(x.project, f.Shared).Key) != 0 {
			t.Fatal("Model facts skipped the original current Project SH gate", x.order)
		}
		return result
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, outcome := range []error{nil, unknown} {
		result, x.order = outcome, nil
		if err = a.CheckAppendInTx(ctx, x.store.tx, entry, key); err != outcome {
			t.Fatal("original Runtime result changed", err)
		}
		if outcome == unknown && !errors.Is(err, originalCause) {
			t.Fatal("CommitUnknown lost its original cause")
		}
	}
	if calls != 2 {
		t.Fatal("Runtime callback was skipped or repeated")
	}
	x.order = nil
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.Forbidden)
	if calls != 3 {
		t.Fatal("public Entry/Key bypassed the fact callback")
	}
	// Installing the wrapper must not late-bind the original Authority.
	hasCode(t, x.a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.DependencyUnbound)
	for _, gate := range []struct {
		initialized bool
		lifecycle   pc.Lifecycle
	}{{false, pc.Active}, {true, pc.Archived}, {true, pc.Deleting}} {
		x.initialized, x.lifecycle = gate.initialized, gate.lifecycle
		hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.ProjectNotActive)
	}
	x.initialized, x.lifecycle = true, pc.Active
	x.store.lockErr = errors.New("missing original Project SH")
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.DependencyUnavailable)
	x.store.lockErr = nil
	hasCode(t, a.CheckAppendInTx(ctx, f.NewTx(), entry, key), f.DependencyUnavailable)
	hasCode(t, a.CheckAppendInTx(ctx, f.Tx{}, entry, key), f.InvalidArgument)
	otherKey, err := ac.NewAppendKey(ac.SecretProducer, key.Details().CauseRef, 0)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, otherKey), f.Forbidden)
	fields := entry.Fields()
	fields.Metadata, err = ac.DenialMetadata(ac.MCP, ac.AddressForbidden, 1)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ac.NewEntry(fields)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, other, key), f.Forbidden)
	if calls != 3 {
		t.Fatal("invalid gate or another consumer reached Runtime facts")
	}
}
