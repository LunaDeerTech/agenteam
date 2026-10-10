package agent

import (
	"context"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type auditStoreBoundary struct {
	Store
	want  f.Tx
	calls int
	err   error
}

func (s *auditStoreBoundary) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.calls++
	if tx != s.want {
		return nil, fault(f.DependencyUnavailable)
	}
	return nil, s.err
}

func TestAgentProjectAuditConstructorRequiresPrivateSameStoreWitness(t *testing.T) {
	var typedNil *auditStoreBoundary
	for _, store := range []Store{nil, typedNil} {
		if _, err := NewProjectAuditAuthority(store); err == nil {
			t.Fatal("unbound Store accepted")
		}
	}
	original := f.NewFault(f.DependencyUnavailable, f.NotCommitted)
	store := &auditStoreBoundary{want: f.NewTx(), err: original}
	a, err := NewProjectAuditAuthority(store)
	if err != nil || store.calls != 0 {
		t.Fatal("constructor performed I/O", err)
	}
	ctx := context.Background()
	entry, key := ac.Entry{}, ac.AppendKey{}
	requireCode(t, a.CheckProjectAuditInTx(ctx, store.want, entry, key), f.Forbidden)
	// Even an internal test's synthetic value cannot bypass Store/Tx identity.
	// This is a negative wiring control, not a real canonical-writer witness.
	other := &auditStoreBoundary{want: store.want, err: original}
	foreign := context.WithValue(ctx, mutationWitnessKey{}, mutationWitness{authority: &authorityState{store: other}, tx: store.want})
	requireCode(t, a.CheckProjectAuditInTx(foreign, store.want, entry, key), f.Forbidden)
	witness := context.WithValue(ctx, mutationWitnessKey{}, mutationWitness{authority: &authorityState{store: store}, tx: store.want})
	requireCode(t, a.CheckProjectAuditInTx(witness, f.NewTx(), entry, key), f.Forbidden)
	if store.calls != 0 || other.calls != 0 {
		t.Fatal("invalid witness reached Store")
	}
	if err = a.CheckProjectAuditInTx(witness, store.want, entry, key); err != original || store.calls != 1 {
		t.Fatal("original live Store refusal was replaced", err)
	}
	// Even a Store that accepts the handle must still reach the original full
	// Authority checker. It rejects invalid public Entry/Key without SQL.
	store.err = nil
	requireCode(t, a.CheckProjectAuditInTx(witness, store.want, entry, key), f.InvalidArgument)
	if store.calls != 2 {
		t.Fatal("same-Store boundary not checked")
	}
	canceled, cancel := context.WithCancel(witness)
	cancel()
	if err = a.CheckProjectAuditInTx(canceled, store.want, entry, key); !errors.Is(err, context.Canceled) || store.calls != 2 {
		t.Fatal("cancellation started another check", err)
	}
}
