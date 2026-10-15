package project

import (
	"context"
	"errors"
	"reflect"
	"testing"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	object "github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func objectAuditEntry(t *testing.T, x *auditGateFixture, action audit.Action, ordinal int64) (audit.Entry, audit.AppendKey) {
	t.Helper()
	scope, _ := identity.InProject(x.project)
	cause := testID[struct{}](t).String()
	registration, _ := identity.RegisterService(identity.ObjectService)
	actor, _ := registration.Actor(cause, scope)
	resource, _ := audit.NewResource(audit.ObjectResource, testID[struct{}](t).String())
	fields := audit.ObjectMetadataFields{ObjectID: resource.Details().ID, InitiatorKind: identity.Human, InitiatorID: x.actor.Details().UserID, MediaType: "text/plain", ByteSize: 3, Phase: audit.PublishedPhase}
	outcome := audit.Success
	if action == audit.ObjectUploadFailed {
		fields.Phase, fields.Reason, outcome = audit.FailedPhase, audit.StorageUnavailable, audit.Unknown
	} else if action == audit.ObjectDelete {
		fields.Phase = audit.DeletedPhase
	}
	if action == audit.ObjectTransferIssue || action == audit.ObjectTransferComplete || action == audit.ObjectTransferRevoke {
		resource, _ = audit.NewResource(audit.ObjectTransferResource, cause)
		fields.TransferID = cause
		fields.Phase = audit.IssuedPhase
		if action == audit.ObjectTransferComplete {
			fields.Phase, fields.SentBytes = audit.SentPhase, fields.ByteSize
		} else if action == audit.ObjectTransferRevoke {
			fields.Phase = audit.RevokedPhase
		}
	}
	metadata, err := audit.ObjectMetadata(action, fields)
	if err != nil {
		t.Fatal(err)
	}
	entry, err := audit.NewEntry(audit.EntryFields{Scope: scope, Actor: actor, Action: action, Resource: resource, Metadata: metadata, Outcome: outcome})
	if err != nil {
		t.Fatal(err)
	}
	key, err := audit.NewAppendKey(audit.ObjectProducer, cause, ordinal)
	if err != nil {
		t.Fatal(err)
	}
	return entry, key
}

func withObjectFacts(t *testing.T, x *auditGateFixture, provider audit.ProjectFactAuthority) *Authority {
	t.Helper()
	a, err := NewAuthority(x.store, AuthorityDependencies{Sessions: x.a.state().sessions, AuditFacts: map[audit.Producer]audit.ProjectFactAuthority{audit.ObjectProducer: provider}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestObjectAuditExactDispatchAndOriginalFault(t *testing.T) {
	type privateMarker struct{}
	ctx := context.WithValue(context.Background(), privateMarker{}, "opaque provider marker")
	x := newAuditGateFixture(t, nil)
	entry, key := objectAuditEntry(t, x, audit.ObjectUploadComplete, 0)
	hasCode(t, x.a.CheckAppendInTx(ctx, x.store.tx, entry, key), f.DependencyUnbound)
	cause := errors.New("private provider error")
	unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
	calls := 0
	a := withObjectFacts(t, x, auditFactFunc(func(actual context.Context, tx f.Tx, got audit.Entry, gotKey audit.AppendKey) error {
		calls++
		gf, ef := got.Fields(), entry.Fields()
		if actual != ctx || tx != x.store.tx || !gf.Actor.Equal(ef.Actor) || !gf.Scope.Equal(ef.Scope) || gf.Resource.Details() != ef.Resource.Details() || gf.Action != ef.Action || gf.Outcome != ef.Outcome || gf.Associations != ef.Associations || !sameMetadata(gf.Metadata, ef.Metadata) || gotKey.Details() != key.Details() {
			t.Fatal("Object witness context/Tx/entry/key changed")
		}
		if !reflect.DeepEqual(x.order, []string{"locks", "executor", "query"}) || len(x.store.locks) != 1 || x.store.locks[0].Mode != f.Shared || x.store.locks[0].Key.Canonical() != projectLock(x.project, f.Shared).Key.Canonical() {
			t.Fatal("provider ran without current Project/held SH", x.order)
		}
		return unknown
	}))
	x.order = nil
	if got := a.CheckAppendInTx(ctx, x.store.tx, entry, key); got != unknown || !errors.Is(got, cause) || calls != 1 {
		t.Fatal("exact provider Unknown not preserved", got, calls)
	}
}

func TestObjectAuditGateDistinguishesNewFactFromExactConvergence(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	calls := 0
	a := withObjectFacts(t, x, auditFactFunc(func(context.Context, f.Tx, audit.Entry, audit.AppendKey) error { calls++; return nil }))
	for _, action := range []audit.Action{audit.ObjectUploadComplete, audit.ObjectUploadFailed, audit.ObjectDelete, audit.ObjectTransferIssue, audit.ObjectTransferComplete, audit.ObjectTransferRevoke} {
		ordinal := int64(0)
		if action == audit.ObjectDelete || action == audit.ObjectTransferComplete {
			ordinal = 1
		} else if action == audit.ObjectTransferRevoke {
			ordinal = 2
		}
		entry, key := objectAuditEntry(t, x, action, ordinal)
		for _, state := range []c.Lifecycle{c.Active, c.Archiving, c.Archived, c.Deleting} {
			x.lifecycle, x.initialized = state, true
			before := calls
			err := a.CheckAppendInTx(context.Background(), x.store.tx, entry, key)
			if (action == audit.ObjectUploadComplete || action == audit.ObjectTransferIssue) && state != c.Active {
				hasCode(t, err, f.ProjectNotActive)
				if calls != before {
					t.Fatal("new fact reached checker after gate denial")
				}
			} else if err != nil || calls != before+1 {
				t.Fatal("valid gate did not require exact fact checker", action, state, err)
			}
			x.initialized = false
			before = calls
			hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.ProjectNotActive)
			if calls != before {
				t.Fatal("initialization used ordinary Object dispatcher")
			}
		}
	}
	// This dispatcher did not add a general-purpose Owner convergence grant.
	hasCode(t, c.CheckOwnerGate(c.Active, c.Initialized, identity.Converge), f.Forbidden)
}

func TestObjectAuditRejectsWrongIdentityTransactionAndStage(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	calls := 0
	a := withObjectFacts(t, x, auditFactFunc(func(context.Context, f.Tx, audit.Entry, audit.AppendKey) error { calls++; return nil }))
	entry, key := objectAuditEntry(t, x, audit.ObjectUploadComplete, 0)
	for _, tx := range []f.Tx{f.NewTx()} {
		hasCode(t, a.CheckAppendInTx(context.Background(), tx, entry, key), f.DependencyUnavailable)
	}
	ended := x.store.tx
	x.store.tx = f.NewTx()
	hasCode(t, a.CheckAppendInTx(context.Background(), ended, entry, key), f.DependencyUnavailable)
	x.store.lockErr = errors.New("missing or weak Project lock")
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.DependencyUnavailable)
	x.store.lockErr = nil
	originalRow := x.store.row
	x.store.row = func(string, ...any) postgres.Row { return objectAuditErrorRow{pgx.ErrNoRows} }
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, key), f.NotFound)
	x.store.row = originalRow
	for _, ordinal := range []int64{1, 2} {
		wrong, _ := audit.NewAppendKey(audit.ObjectProducer, key.Details().CauseRef, ordinal)
		hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, wrong), f.Forbidden)
	}
	wrong, _ := audit.NewAppendKey(audit.ObjectProducer, testID[struct{}](t).String(), 0)
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, wrong), f.Forbidden)
	wrong, _ = audit.NewAppendKey(audit.SecretProducer, key.Details().CauseRef, 0)
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, entry, wrong), f.Forbidden)
	for _, service := range []identity.ServiceName{identity.ObjectMaintenance} {
		registration, _ := identity.RegisterService(service)
		fields := entry.Fields()
		fields.Actor, _ = registration.Actor(key.Details().CauseRef, fields.Scope)
		other, err := audit.NewEntry(fields)
		if err != nil {
			t.Fatal(err)
		}
		hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, other, key), f.Forbidden)
	}
	failed, failedKey := objectAuditEntry(t, x, audit.ObjectUploadFailed, 0)
	fields := failed.Fields()
	fields.Outcome = audit.Failed
	failed, err := audit.NewEntry(fields)
	if err != nil {
		t.Fatal(err)
	}
	hasCode(t, a.CheckAppendInTx(context.Background(), x.store.tx, failed, failedKey), f.Forbidden)
	if calls != 0 {
		t.Fatal("invalid request reached provider", calls)
	}
}

func TestObjectAuditRealCheckerCannotBeReplacedByPublicMetadata(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	checker, err := object.NewProjectAuditAuthority(x.store)
	if err != nil {
		t.Fatal(err)
	}
	a := withObjectFacts(t, x, checker)
	for _, action := range []audit.Action{audit.ObjectUploadComplete, audit.ObjectUploadFailed, audit.ObjectDelete, audit.ObjectTransferIssue, audit.ObjectTransferComplete, audit.ObjectTransferRevoke} {
		ordinal := int64(0)
		if action == audit.ObjectDelete || action == audit.ObjectTransferComplete {
			ordinal = 1
		} else if action == audit.ObjectTransferRevoke {
			ordinal = 2
		}
		entry, key := objectAuditEntry(t, x, action, ordinal)
		for _, state := range []c.Lifecycle{c.Active, c.Archived, c.Deleting} {
			x.lifecycle = state
			got := a.CheckAppendInTx(context.Background(), x.store.tx, entry, key)
			code := f.Forbidden
			if (action == audit.ObjectUploadComplete || action == audit.ObjectTransferIssue) && state != c.Active {
				code = f.ProjectNotActive
			}
			hasCode(t, got, code)
		}
	}
}

func TestObjectAuditTransferDispatchRequiresExactStageAndPrivateProof(t *testing.T) {
	x := newAuditGateFixture(t, nil)
	cause := errors.New("original transfer checker")
	unknown := f.NewFault(f.CommitUnknown, f.Unknown).WithCause(cause)
	type marker struct{}
	ctx := context.WithValue(context.Background(), marker{}, new(int))
	for _, stage := range []struct {
		action  audit.Action
		ordinal int64
	}{
		{audit.ObjectTransferIssue, 0}, {audit.ObjectTransferComplete, 1}, {audit.ObjectTransferRevoke, 2},
	} {
		entry, key := objectAuditEntry(t, x, stage.action, stage.ordinal)
		calls := 0
		a := withObjectFacts(t, x, auditFactFunc(func(got context.Context, tx f.Tx, e audit.Entry, k audit.AppendKey) error {
			calls++
			if got != ctx || tx != x.store.tx || k.Details() != key.Details() || !e.Fields().Actor.Equal(entry.Fields().Actor) || !sameMetadata(e.Fields().Metadata, entry.Fields().Metadata) {
				t.Fatal("original private transfer proof changed")
			}
			return unknown
		}))
		if got := a.CheckAppendInTx(ctx, x.store.tx, entry, key); got != unknown || !errors.Is(got, cause) || calls != 1 {
			t.Fatal("transfer checker error lost", got, calls)
		}
		for ordinal := int64(0); ordinal < 3; ordinal++ {
			if ordinal == stage.ordinal {
				continue
			}
			wrong, _ := audit.NewAppendKey(audit.ObjectProducer, key.Details().CauseRef, ordinal)
			hasCode(t, a.CheckAppendInTx(ctx, x.store.tx, entry, wrong), f.Forbidden)
		}
		fields := entry.Fields()
		fields.Resource, _ = audit.NewResource(audit.ObjectResource, fields.Resource.Details().ID)
		if _, err := audit.NewEntry(fields); err == nil {
			t.Fatal("transfer entry accepted the object resource kind")
		}
		if calls != 1 {
			t.Fatal("invalid transfer metadata reached checker")
		}
	}
	// Recovery publication failures retain their existing separate ordinal.
	x.lifecycle = c.Deleting
	entry, key := objectAuditEntry(t, x, audit.ObjectUploadFailed, 1)
	calls := 0
	a := withObjectFacts(t, x, auditFactFunc(func(context.Context, f.Tx, audit.Entry, audit.AppendKey) error { calls++; return nil }))
	if err := a.CheckAppendInTx(ctx, x.store.tx, entry, key); err != nil || calls != 1 {
		t.Fatal("recovery ordinal changed", err)
	}
}

type objectAuditErrorRow struct{ err error }

func (r objectAuditErrorRow) Scan(...any) error { return r.err }
