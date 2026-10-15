package skill

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// Object's release plan remains technical-only even when Close inherits an
// active or already-expired Skill read context. This is only the domain's
// immutable mapping check; Object still owns the exact private lease proof.
func TestRuntimeSkillReadContextPreservesOriginalObjectMaintenance(t *testing.T) {
	for _, active := range []bool{true, false} {
		name := "expired"
		if active {
			name = "active"
		}
		t.Run(name, func(t *testing.T) {
			service, base, projects, actor, initial := readFixture(t)
			store := &maintenanceStore{initializationReadStore: base, process: stateID[oc.Process](50)}
			authority := service.state().authority
			authority.state().store = store
			projects.gate = fault(f.Forbidden)
			witness := &runtimeReadWitness{authority: authority}
			witness.active.Store(active)
			ctx := context.WithValue(context.Background(), runtimeReadKey{}, witness)
			request, err := oc.NewMaintenanceAccess(oc.AccessRequestDetails{Operation: oc.ReleaseReaderAccess, InstanceID: store.process, ObjectID: stateID[oc.StoredObject](7), LeaseID: stateID[oc.Lease](179)})
			if err != nil {
				t.Fatal(err)
			}
			plan, err := authority.Discover(ctx, request)
			if err != nil {
				t.Fatal("ambient read witness blocked original release mapping", err)
			}
			base.live, base.tx = true, f.NewTx()
			base.held = map[string]f.LockMode{}
			if err = base.AcquireAll(ctx, base.tx, plan.Locks()); err != nil {
				t.Fatal(err)
			}
			if err = authority.ValidateInTx(ctx, base.tx, request, plan); err != nil {
				t.Fatal("original same-Tx release mapping rejected", err)
			}
			if projects.initializes != 0 || projects.converges != 0 {
				t.Fatal("technical release used a domain mutation grant")
			}
			store.missingObject = true
			if err = authority.ValidateInTx(ctx, base.tx, request, plan); err == nil {
				t.Fatal("changed original mapping accepted")
			}
			store.missingObject = false
			clear(base.held)
			if err = authority.ValidateInTx(ctx, base.tx, request, plan); err == nil {
				t.Fatal("release accepted without original plan locks")
			}
			// The maintenance branch is not a fallback for a new body read.
			owner, err := loadInitialization(ctx, store, initial.ProjectID)
			if err != nil || owner == nil {
				t.Fatal("fixture initialization missing", err)
			}
			resource, err := owner.owner()
			if err != nil {
				t.Fatal(err)
			}
			read, err := oc.NewObjectReadAccess(oc.AccessRequestDetails{Operation: oc.ReadAccess, Actor: actor, Owner: resource, Intent: "read", ObjectID: owner.object})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = authority.Discover(ctx, read); err == nil {
				t.Fatal("ambient maintenance context granted a new body read")
			}
		})
	}
}
