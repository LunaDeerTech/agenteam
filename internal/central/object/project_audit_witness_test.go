package object

import (
	"context"
	"reflect"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestObjectProjectAuditExactEntryKeyAndLiveStoreTransaction(t *testing.T) {
	for _, name := range []string{"no-witness", "wrong-store", "wrong-tx", "expired-tx", "wrong-producer", "wrong-cause", "wrong-ordinal", "wrong-resource", "wrong-metadata", "wrong-actor", "missing-lock"} {
		t.Run(name, func(t *testing.T) {
			v := newAuditPublishFixture(t)
			ctx := v.context()
			tx := v.store.tx
			entry, key := v.w.entry, v.w.key
			f := entry.Fields()
			switch name {
			case "no-witness":
				ctx = context.Background()
			case "wrong-store":
				v.checker, _ = NewProjectAuditAuthority(&auditTestStore{tx: tx})
			case "wrong-tx":
				tx = foundation.NewTx()
			case "expired-tx":
				v.store.tx = foundation.NewTx()
			case "wrong-producer":
				key, _ = ac.NewAppendKey(ac.ArtifactProducer, key.Details().CauseRef, 0)
			case "wrong-cause":
				key, _ = ac.NewAppendKey(ac.ObjectProducer, auditID[struct{}](t).String(), 0)
			case "wrong-ordinal":
				key, _ = ac.NewAppendKey(ac.ObjectProducer, key.Details().CauseRef, 1)
			case "wrong-resource":
				f.Resource, _ = ac.NewResource(ac.ObjectResource, auditID[struct{}](t).String())
				entry, _ = ac.NewEntry(f)
			case "wrong-metadata":
				f.Metadata, _ = ac.ObjectMetadata(ac.ObjectUploadComplete, ac.ObjectMetadataFields{ObjectID: v.u.object.String(), InitiatorKind: identity.Human, InitiatorID: v.u.initiatorID, MediaType: "application/json", ByteSize: 4, Phase: ac.PublishedPhase})
				entry, _ = ac.NewEntry(f)
			case "wrong-actor":
				reg, _ := identity.RegisterService(identity.ObjectMaintenance)
				f.Actor, _ = reg.Actor(key.Details().CauseRef, f.Scope)
				entry, _ = ac.NewEntry(f)
			case "missing-lock":
				v.w.stage.locks = nil
				ctx = v.context()
			}
			if err := v.check(ctx, tx, entry, key); err == nil {
				t.Fatal("forged call accepted")
			}
			if name != "missing-lock" && v.store.queries != 0 {
				t.Fatal("unbound envelope read canonical rows")
			}
		})
	}
	for i := 0; i < 9; i++ {
		t.Run(reflect.TypeOf(ac.Associations{}).Field(i).Name, func(t *testing.T) {
			v := newAuditPublishFixture(t)
			f := v.w.entry.Fields()
			reflect.ValueOf(&f.Associations).Elem().Field(i).SetString(auditID[struct{}](t).String())
			entry, err := ac.NewEntry(f)
			if err != nil {
				t.Fatal(err)
			}
			auditCode(t, v.check(v.context(), v.store.tx, entry, v.w.key), foundation.Forbidden)
			if v.store.queries != 0 {
				t.Fatal("altered association reached SQL")
			}
		})
	}
}
func TestObjectProjectAuditStageRequiresRealPrivateAcquire(t *testing.T) {
	v := newAuditPublishFixture(t)
	r := &serviceState{store: v.store, accessIssuer: oc.NewAccessIssuer(), accessTransactions: map[foundation.Tx]bool{}}
	s := &Service{data: func() *serviceState { return r }}
	attempt, _ := oc.NewUploadAttempt(oc.AttemptDetails{ID: v.a.id, UploadID: v.u.id, ObjectID: v.u.object})
	request := ownerRequest(v.w.stage.actor, v.u.owner, oc.PublishAccess, oc.AccessRequestDetails{Attempt: attempt})
	deps, err := oc.NewAccessDependencies(v.o.meta.SHA256, v.w.stage.locks)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := oc.NewAccessLockPlan(r.accessIssuer, oc.AccessPlanDetails{Request: request, DependencyRequest: request, Dependencies: deps, DomainBinding: v.o.meta.SHA256, Objects: []oc.ObjectID{v.u.object}, Locks: v.w.stage.locks})
	if err != nil {
		t.Fatal(err)
	}
	locked, err := oc.NewLockedAccess(r.accessIssuer, v.store.tx, []oc.AccessLockPlan{plan}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.projectAuditAccessContext(context.Background(), v.store.tx, request, plan, locked); err == nil {
		t.Fatal("token without actual acquisition minted stage")
	}
	r.accessTransactions[v.store.tx] = true
	foreign, _ := oc.NewLockedAccess(oc.NewAccessIssuer(), v.store.tx, []oc.AccessLockPlan{plan}, nil)
	if _, err = s.projectAuditAccessContext(context.Background(), v.store.tx, request, plan, foreign); err == nil {
		t.Fatal("foreign issuer minted stage")
	}
	ctx, err := s.projectAuditAccessContext(context.Background(), v.store.tx, request, plan, locked)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.projectAuditWitnessContext(ctx, foundation.NewTx(), v.w); err == nil {
		t.Fatal("stage moved to another Tx")
	}
	if _, err = s.projectAuditWitnessContext(ctx, v.store.tx, v.w); err != nil {
		t.Fatal(err)
	}
}
