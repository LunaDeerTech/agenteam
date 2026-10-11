package project

import (
	"context"
	"errors"
	"reflect"
	"testing"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestRuntimeInitializationAuditConstructionAndDelegation(t *testing.T) {
	type witness struct{}
	ctx := context.WithValue(context.Background(), witness{}, "original ordinary witness")
	refusal := fault(f.Forbidden)
	ordinaryCalls, specialCalls := 0, 0
	var entry audit.Entry
	var key audit.AppendKey
	var x *auditGateFixture
	x = newAuditGateFixture(t, auditFactFunc(func(got context.Context, tx f.Tx, e audit.Entry, k audit.AppendKey) error {
		ordinaryCalls++
		if got != ctx || tx != x.store.tx || !e.Fields().Actor.Equal(entry.Fields().Actor) || !sameMetadata(e.Fields().Metadata, entry.Fields().Metadata) || k.Details() != key.Details() {
			t.Fatal("ordinary source lost original context/Tx/entry/key")
		}
		return refusal
	}))
	entry, key = auditGateEntry(t, x, x.actor, audit.SecretUpdate)
	special := auditFactFunc(func(context.Context, f.Tx, audit.Entry, audit.AppendKey) error {
		specialCalls++
		return nil
	})
	for _, original := range []*Authority{nil, {}} {
		got, err := NewRuntimeInitializationAuditAuthority(original, special, special)
		hasCode(t, err, f.DependencyUnbound)
		if got != nil {
			t.Fatal("unbound original produced a composition")
		}
	}
	for _, missing := range []audit.ProjectFactAuthority{nil, auditFactFunc(nil)} {
		for _, providers := range [][2]audit.ProjectFactAuthority{{missing, special}, {special, missing}} {
			got, err := NewRuntimeInitializationAuditAuthority(x.a, providers[0], providers[1])
			hasCode(t, err, f.DependencyUnbound)
			if got != nil {
				t.Fatal("missing fact source produced a composition")
			}
		}
	}
	gate, err := NewRuntimeInitializationAuditAuthority(x.a, special, special)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.order) != 0 || ordinaryCalls != 0 || specialCalls != 0 || len(x.a.state().auditFacts) != 1 {
		t.Fatal("construction accessed dependencies or changed original facts")
	}
	if err = gate.CheckAppendInTx(ctx, x.store.tx, entry, key); err != refusal || ordinaryCalls != 1 {
		t.Fatal("ordinary append did not retain the original refusal", err)
	}
	if len(x.order) < 3 || !reflect.DeepEqual(x.order[:3], []string{"locks", "session", "executor"}) {
		t.Fatal("ordinary append bypassed the current Owner", x.order)
	}
	x.sessionErr = fault(f.SessionRevoked)
	if err = gate.CheckAppendInTx(ctx, x.store.tx, entry, key); err != x.sessionErr || ordinaryCalls != 1 {
		t.Fatal("composition bypassed Session revocation", err)
	}
	if _, err = gate.AuthorizeProject(ctx, x.store.tx, x.actor, x.project, i.Read); err != x.sessionErr {
		t.Fatal("Owner authorization no longer uses the original Project", err)
	}
	x.sessionErr = nil
	scope, err := i.InProject(x.project)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := gate.AuthorizeProject(ctx, x.store.tx, x.actor, x.project, i.Read)
	if err != nil || !grant.Matches(x.actor, scope, i.Read) {
		t.Fatal("ordinary Owner read changed", err)
	}
	hasCode(t, gate.CheckServiceLookup(ctx, x.actor, scope, key), f.DependencyUnbound)
	cause, err := audit.NewLifecycleCause(testID[audit.LifecycleOperation](t), audit.Archive, 1)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, gate.CheckCleanupInTx(ctx, x.store.tx, x.actor, cause, x.project), f.DependencyUnbound)
	if specialCalls != 0 || len(x.a.state().auditFacts) != 1 || x.a.state().auditFacts[audit.AccessProducer] != nil || x.a.state().auditFacts[audit.ObjectProducer] != nil {
		t.Fatal("ordinary methods borrowed specialized facts or late-bound original")
	}
}

func TestRuntimeInitializationAuditKeepsInitializationFacts(t *testing.T) {
	// Reuse the existing controlled Project/creation fixture and its original
	// fact callback; this proves routing, not a real Skill/Object grant.
	for _, action := range []audit.Action{audit.ObjectUploadComplete, audit.ObjectUploadFailed, audit.ObjectDelete} {
		x := newInitializationAuditFixture(t, action, nil)
		x.base.state(pc.CreationInitializing)
		initialization := x.gate.(*initializationAuditAuthority).facts
		runtimeCalls := 0
		gate, err := NewRuntimeInitializationAuditAuthority(x.base.authority, initialization, auditFactFunc(func(context.Context, f.Tx, audit.Entry, audit.AppendKey) error {
			runtimeCalls++
			return nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		x.gate = gate
		x.check(t, "", "held", "executor", "creation", "project", "provider")
		if runtimeCalls != 0 {
			t.Fatal("initialization borrowed Runtime facts")
		}
	}
	x := newInitializationAuditFixture(t, audit.ObjectUploadFailed, nil)
	original := errors.New("original initialization uncertainty")
	x.providerError = f.NewFault(f.CommitUnknown, f.Unknown).WithCause(original)
	gate, err := NewRuntimeInitializationAuditAuthority(x.base.authority, x.gate.(*initializationAuditAuthority).facts, auditFactFunc(func(context.Context, f.Tx, audit.Entry, audit.AppendKey) error {
		t.Fatal("initialization rejection fell through to Runtime")
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	x.gate = gate
	if err = x.check(t, f.CommitUnknown, "held", "executor", "creation", "project", "provider"); !errors.Is(err, original) {
		t.Fatal("initialization uncertainty lost its original cause")
	}
	x.calls, x.base.store.order = 0, nil
	x.base.store.projectRow[11] = testID[pc.Creation](t).String()
	x.check(t, f.DependencyUnavailable, "held", "executor", "creation", "project")
	x.base.store.order = nil
	badKey, err := audit.NewAppendKey(audit.AccessProducer, x.key.Details().CauseRef, 0)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, gate.CheckAppendInTx(x.base.store.ctx, x.base.store.tx, x.entry, badKey), f.Forbidden)
	if x.calls != 0 || len(x.base.store.order) != 0 {
		t.Fatal("wrong producer reached initialization facts")
	}
}

func TestRuntimeInitializationAuditKeepsRuntimeFacts(t *testing.T) {
	// Controlled Runtime fact responses test the composition only. Native
	// acceptance still requires the real accepted Invocation/private handoff.
	type witness struct{}
	ctx := context.WithValue(context.Background(), witness{}, "original Runtime handoff")
	x := newAuditGateFixture(t, nil)
	entry, key := modelAccessEntry(t, x, audit.AddressForbidden)
	initializationCalls, runtimeCalls := 0, 0
	var outcome error
	gate, err := NewRuntimeInitializationAuditAuthority(x.a, auditFactFunc(func(context.Context, f.Tx, audit.Entry, audit.AppendKey) error {
		initializationCalls++
		return nil
	}), auditFactFunc(func(got context.Context, tx f.Tx, e audit.Entry, k audit.AppendKey) error {
		runtimeCalls++
		if got != ctx {
			return fault(f.Forbidden)
		}
		if tx != x.store.tx || !e.Fields().Actor.Equal(entry.Fields().Actor) || !sameMetadata(e.Fields().Metadata, entry.Fields().Metadata) || k.Details() != key.Details() {
			t.Fatal("Runtime lost original context/Tx/entry/key")
		}
		if !reflect.DeepEqual(x.order, []string{"locks", "executor", "query"}) || len(x.store.locks) != 1 || x.store.locks[0].Mode != f.Shared {
			t.Fatal("Runtime callback preceded the original Project SH/current gate", x.order)
		}
		return outcome
	}))
	if err != nil {
		t.Fatal(err)
	}
	original := errors.New("original Runtime uncertainty")
	unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(original)
	for _, result := range []error{nil, unknown} {
		outcome, x.order = result, nil
		if err = gate.CheckAppendInTx(ctx, x.store.tx, entry, key); err != result {
			t.Fatal("Runtime outcome changed", err)
		}
		if result != nil && !errors.Is(err, original) {
			t.Fatal("Runtime uncertainty lost its original cause")
		}
	}
	x.order = nil
	hasCode(t, gate.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.Forbidden)
	if runtimeCalls != 3 || initializationCalls != 0 {
		t.Fatal("Runtime route skipped its private source or used initialization")
	}
	hasCode(t, x.a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.DependencyUnbound)
	x.initialized = false
	hasCode(t, gate.CheckAppendInTx(ctx, x.store.tx, entry, key), f.ProjectNotActive)
	x.initialized = true
	hasCode(t, gate.CheckAppendInTx(ctx, f.NewTx(), entry, key), f.DependencyUnavailable)
	hasCode(t, gate.CheckAppendInTx(ctx, f.Tx{}, entry, key), f.InvalidArgument)
	fields := entry.Fields()
	fields.Metadata, err = audit.DenialMetadata(audit.MCP, audit.AddressForbidden, 1)
	if err != nil {
		t.Fatal(err)
	}
	other, err := audit.NewEntry(fields)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, gate.CheckAppendInTx(ctx, x.store.tx, other, key), f.Forbidden)
	if runtimeCalls != 3 || initializationCalls != 0 || x.a.state().auditFacts[audit.AccessProducer] != nil {
		t.Fatal("invalid Runtime request borrowed another grant or mutated original")
	}
}
