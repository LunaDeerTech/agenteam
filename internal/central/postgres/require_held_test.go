package postgres

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func heldFixture() (*Store, *transaction) {
	state := &storeState{txs: make(map[foundation.Tx]*transaction)}
	s := &Store{state: func() *storeState { return state }}
	t := &transaction{owner: s, token: foundation.NewTx(), active: true, gate: make(chan struct{}, 1), held: map[string]foundation.LockMode{}}
	t.gate <- struct{}{}
	state.txs[t.token] = t
	return s, t
}

func TestRequireHeldNeverAcquiresOrChangesOrder(t *testing.T) {
	key, _ := foundation.SystemConfigLock("outbox-registration")
	high, _ := foundation.RecordLock(foundation.OutboxRecordLock, "event:fixture")
	for _, mode := range []foundation.LockMode{foundation.Shared, foundation.Exclusive} {
		s, tx := heldFixture()
		tx.held[key.Canonical()] = mode
		tx.highest = high
		if err := s.RequireHeldLocks(context.Background(), tx.token, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); err != nil {
			t.Fatal(err)
		}
		if tx.highest.Canonical() != high.Canonical() || len(tx.held) != 1 || tx.held[key.Canonical()] != mode {
			t.Fatal("validation changed lock state")
		}
		// raw is nil: any SQL acquisition would panic, including validation of
		// an already-held lower key after a higher key has been acquired.
	}
	for _, test := range []struct {
		name string
		mode foundation.LockMode
		held foundation.LockMode
		want Code
	}{
		{"missing", foundation.Shared, "", LockNotHeld},
		{"weak", foundation.Exclusive, foundation.Shared, LockNotHeld},
		{"invalid_mode", foundation.LockMode("upgrade"), foundation.Exclusive, InvalidLock},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, tx := heldFixture()
			if test.held != "" {
				tx.held[key.Canonical()] = test.held
			}
			err := s.RequireHeldLocks(context.Background(), tx.token, []foundation.LockRequest{{Key: key, Mode: test.mode}})
			if CodeOf(err) != test.want || CodeOf(tx.poison) != test.want {
				t.Fatal("requirement did not poison", err)
			}
			if tx.highest.Validate() == nil {
				t.Fatal("highest changed")
			}
		})
	}
}

func TestRequireHeldUsesLiveOwnerAndSingleSlot(t *testing.T) {
	s, tx := heldFixture()
	liveTokens.Store(tx.token, tx)
	defer liveTokens.Delete(tx.token)
	other, _ := heldFixture()
	if err := other.RequireHeldLocks(context.Background(), tx.token, nil); CodeOf(err) != InvalidTransaction || tx.poison == nil {
		t.Fatal("foreign store did not reject/poison", err)
	}
	s, tx = heldFixture()
	<-tx.gate
	if err := s.RequireHeldLocks(context.Background(), tx.token, nil); CodeOf(err) != TransactionConcurrentUse || tx.poison == nil {
		t.Fatal("concurrent use accepted", err)
	}
	s, tx = heldFixture()
	tx.active = false
	if err := s.RequireHeldLocks(context.Background(), tx.token, nil); CodeOf(err) != TransactionExpired {
		t.Fatal("expired handle accepted", err)
	}
	s, tx = heldFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.RequireHeldLocks(ctx, tx.token, nil); CodeOf(err) != LockNotHeld || tx.poison == nil {
		t.Fatal("cancel ignored", err)
	}
}
