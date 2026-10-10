package object

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func TestMetadataPurgeTransactionBudgetCannotBeReused(t *testing.T) {
	tx := foundation.NewTx()
	store := &auditTestStore{tx: tx}
	state := &serviceState{store: store, accessTransactions: map[foundation.Tx]bool{tx: true}}
	s := &Service{data: func() *serviceState { return state }}
	var accepted atomic.Int32
	var joined sync.WaitGroup
	for range 8 {
		joined.Go(func() {
			if s.consumeMetadataTransaction(tx) == nil {
				accepted.Add(1)
			}
		})
	}
	joined.Wait()
	if accepted.Load() != 1 || s.consumeMetadataTransaction(tx) == nil {
		t.Fatal("one transaction consumed multiple deletion budgets")
	}
	foreign := foundation.NewTx()
	state.accessTransactions[foreign] = true
	if s.consumeMetadataTransaction(foreign) == nil {
		t.Fatal("foreign transaction passed through public identity")
	}
	store.tx = foundation.Tx{}
	if s.consumeMetadataTransaction(tx) == nil {
		t.Fatal("ended transaction reused its original acquisition")
	}
	store.tx = foundation.NewTx()
	if s.consumeMetadataTransaction(store.tx) == nil {
		t.Fatal("live transaction without original union was accepted")
	}
	state.accessTransactions[store.tx] = true
	if err := s.consumeMetadataTransaction(store.tx); err != nil {
		t.Fatal("fresh acquired transaction could not make progress", err)
	}
	if store.queries != 0 {
		t.Fatal("transaction budget check issued native SQL")
	}
}

func TestMetadataPurgeRequiresNativeTerminalAndActualLocalReturn(t *testing.T) {
	object := auditID[oc.StoredObject](t)
	other := auditID[oc.StoredObject](t)
	live := foundation.NewTx()
	store := &auditTestStore{tx: live}
	state := &serviceState{store: store, projectWork: map[string]*projectWorkHandle{}}
	s := &Service{data: func() *serviceState { return state }}
	for _, pending := range []bool{true, false} {
		store.row = func(_ string, args ...any) postgres.Row {
			if len(args) != 1 || args[0] != object.String() {
				t.Fatal("terminal predicate crossed Object")
			}
			return auditValues(pending)
		}
		for _, fixture := range []struct {
			name   string
			object oc.ObjectID
			ended  bool
			origin foundation.Tx
			ok     bool
		}{
			{"live work", object, false, foundation.Tx{}, false},
			{"ended with live original Tx", object, true, live, false},
			{"actual ended and original Tx ended", object, true, foundation.NewTx(), true},
			{"unrelated live work", other, false, live, true},
		} {
			t.Run(fixture.name, func(t *testing.T) {
				state.projectWork["controlled-original"] = &projectWorkHandle{work: projectWork{object: fixture.object}, ended: fixture.ended, origin: fixture.origin}
				err := s.metadataPhysicalCompleted(context.Background(), store, object)
				if (err == nil) != (!pending && fixture.ok) {
					t.Fatal("terminal or actual-return condition bypassed", pending, err)
				}
			})
		}
	}
}

func TestMetadataPurgeRejectsUnissuedOperationBeforeReadingFacts(t *testing.T) {
	project := auditID[struct{}](t).String()
	owner, _ := oc.NewObjectOwner(oc.SkillRevision, auditID[struct{}](t).String(), project)
	cause, _ := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: auditID[oc.CleanupOperation](t), Owner: owner, Reason: oc.ProjectDeleted})
	tx := foundation.NewTx()
	store := &auditTestStore{tx: tx}
	state := &serviceState{store: store, accessIssuer: oc.NewAccessIssuer(), accessTransactions: map[foundation.Tx]bool{tx: true}}
	s := &Service{data: func() *serviceState { return state }}
	result, err := s.PurgeDeletedObjectMetadataInTx(context.Background(), tx, cause, auditID[oc.StoredObject](t), oc.AccessLockPlan{}, oc.LockedAccess{})
	if err == nil || result.State == oc.CleanupCompleted || store.queries != 0 {
		t.Fatal("unissued plan reached native facts or completed")
	}
	for _, name := range []string{"object_references", "audit_records", "objects WHERE true;--", "", "agenteam_object.objects"} {
		if _, err := metadataDeleteSQL(name); err == nil {
			t.Fatal("non-private table name accepted")
		}
	}
}
