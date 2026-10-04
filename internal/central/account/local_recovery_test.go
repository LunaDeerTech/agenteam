package account

import (
	"context"
	"fmt"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This narrow state test checks local bookkeeping only. The integration tests
// separately hold real COMMITs to prove database serialization and zero output.
type localRecoveryStore struct {
	Store
	required map[string]bool
	unknown  bool
	held     bool
	reads    int
}

func (s *localRecoveryStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.held = false
	if e := fn(ctx, foundation.NewTx()); e != nil {
		return foundation.NotCommittedResult(foundation.NewFault(foundation.InternalError, foundation.NotCommitted))
	}
	if s.unknown {
		id, _ := foundation.NewID[foundation.TransactionAttempt]()
		return foundation.UnknownResult(id, cause)
	}
	return foundation.CommittedResult()
}
func (s *localRecoveryStore) AcquireAll(_ context.Context, _ foundation.Tx, locks []foundation.LockRequest) error {
	if len(locks) != 2 || locks[0].Mode != foundation.Exclusive || locks[1].Mode != foundation.Exclusive {
		panic("missing writer union")
	}
	s.held = true
	return nil
}
func (s *localRecoveryStore) InTx(foundation.Tx) (postgres.SQLExecutor, error) { return s, nil }

type localRow bool

func (r localRow) Scan(out ...any) error { *out[0].(*bool) = bool(r); return nil }
func (s *localRecoveryStore) QueryRow(_ context.Context, _ string, args ...any) postgres.Row {
	if !s.held {
		panic("observation before writer lock")
	}
	s.reads++
	return localRow(s.required[args[0].(string)])
}
func TestLocalRecoveryNeedsActualJoinAndConfirmedObservation(t *testing.T) {
	store := &localRecoveryStore{required: map[string]bool{}}
	st := &serviceState{store: store, logins: map[string]*loginOperation{}, responses: map[string]*responseState{}}
	s := &Service{func() *serviceState { return st }}
	key, _ := foundation.NewCommandIdentity("account.login", []string{"browser"}, "login", "local")
	op := &operation{done: make(chan struct{})}
	st.logins["id"] = &loginOperation{op: op, identity: key}
	var status RecoveryStatus
	if e := s.recoverLocal(context.Background(), &status); e != nil || store.reads != 0 || len(st.logins) != 1 {
		t.Fatal("live local owner touched", e)
	}
	close(op.done)
	store.unknown = true
	if e := s.recoverLocal(context.Background(), &status); !hasFaultCode(e, foundation.CommitUnknown) || len(st.logins) != 1 {
		t.Fatal("unknown read retired proof", e)
	}
	store.unknown = false
	store.required["id"] = true
	if e := s.recoverLocal(context.Background(), &status); e != nil || len(st.logins) != 1 {
		t.Fatal("durable checkpoint lost", e)
	}
	store.required["id"] = false
	if e := s.recoverLocal(context.Background(), &status); e != nil || len(st.logins) != 0 {
		t.Fatal("terminal absence retained", e)
	}
}
func TestLocalRecoveryProtectedPrefixCannotStarveAndCancelStartsNoNextItem(t *testing.T) {
	store := &localRecoveryStore{required: map[string]bool{}}
	st := &serviceState{store: store, logins: map[string]*loginOperation{}, responses: map[string]*responseState{}}
	s := &Service{func() *serviceState { return st }}
	key, _ := foundation.NewCommandIdentity("account.login", []string{"browser"}, "login", "local")
	op := &operation{done: make(chan struct{})}
	close(op.done)
	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("%03d", i)
		st.logins[id] = &loginOperation{op: op, identity: key}
		store.required[id] = i < 100
	}
	var status RecoveryStatus
	if e := s.recoverLocal(context.Background(), &status); e != nil || store.reads != 100 || len(st.logins) != 101 {
		t.Fatal(status, e)
	}
	if e := s.recoverLocal(context.Background(), &status); e != nil || len(st.logins) != 100 {
		t.Fatal("protected prefix starved independent item", status, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := store.reads
	if e := s.recoverLocal(ctx, &status); e == nil || store.reads != before {
		t.Fatal("cancel started new database observation", e)
	}
}
