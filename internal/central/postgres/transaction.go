package postgres

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Isolation string

const (
	ReadCommitted Isolation = "read_committed"
	Serializable  Isolation = "serializable"
)

type TxOptions struct{ Isolation Isolation }
type txContextKey struct{}

// This registry only rejects/poisons live handle misuse across Stores. It is
// not a business lock, authorization mechanism or durable outcome registry.
var liveTokens sync.Map

type transaction struct {
	owner   *Store
	op      *operation
	raw     pgx.Tx
	token   foundation.Tx
	mu      sync.Mutex
	active  bool
	poison  error
	gate    chan struct{}
	rows    *Rows
	held    map[string]foundation.LockMode
	highest foundation.LockKey
}

func (t *transaction) poisonWith(err error) error {
	t.mu.Lock()
	if t.poison == nil {
		t.poison = err
	}
	t.mu.Unlock()
	return err
}
func rejected(err error) foundation.CommitResult {
	var fault *foundation.Fault
	if errors.As(err, &fault) {
		return foundation.NotCommittedResult(fault)
	}
	return foundation.NotCommittedResult(foundation.NewFault(foundation.InternalError, foundation.NotCommitted).WithCause(err))
}
func (s *Store) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	return s.WithinTxOptions(ctx, cause, TxOptions{Isolation: ReadCommitted}, fn)
}
func (s *Store) WithinTxOptions(ctx context.Context, cause foundation.TransactionCause, options TxOptions, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	if parent, ok := ctx.Value(txContextKey{}).(*transaction); ok {
		return rejected(parent.poisonWith(failure(TransactionNested, nil)))
	}
	if cause.Validate() != nil || fn == nil {
		return rejected(failure(InvalidTransaction, nil))
	}
	isolation := pgx.ReadCommitted
	if options.Isolation == Serializable {
		isolation = pgx.Serializable
	} else if options.Isolation != ReadCommitted && options.Isolation != "" {
		return rejected(failure(InvalidTransaction, nil))
	}
	attempt, err := foundation.NewID[foundation.TransactionAttempt]()
	if err != nil {
		return rejected(failure(TransactionBeginFailed, err))
	}
	op, err := s.borrow(ctx)
	if err != nil {
		return rejected(err)
	}
	defer op.release()
	raw, err := op.conn.BeginTx(op.ctx, pgx.TxOptions{IsoLevel: isolation})
	if err != nil {
		return rejected(failure(TransactionBeginFailed, err))
	}
	t := &transaction{owner: s, op: op, raw: raw, token: foundation.NewTx(), active: true, gate: make(chan struct{}, 1), held: make(map[string]foundation.LockMode)}
	t.gate <- struct{}{}
	state := s.state()
	state.mu.Lock()
	state.txs[t.token] = t
	state.mu.Unlock()
	liveTokens.Store(t.token, t)
	defer func() { state.mu.Lock(); delete(state.txs, t.token); state.mu.Unlock(); liveTokens.Delete(t.token) }()
	defer func() {
		if panicValue := recover(); panicValue != nil {
			t.finishCallback()
			t.rollback()
			panic(panicValue)
		}
	}()
	if _, err := raw.Exec(op.ctx, "SELECT set_config('lock_timeout',$1,true)", s.state().config.LockTimeout().String()); err != nil {
		t.finishCallback()
		t.rollback()
		return rejected(failure(TransactionBeginFailed, err))
	}
	callbackErr := fn(context.WithValue(op.ctx, txContextKey{}, t), t.token)
	t.finishCallback()
	t.mu.Lock()
	poisoned := t.poison
	t.mu.Unlock()
	if callbackErr != nil || poisoned != nil || op.ctx.Err() != nil {
		t.rollback()
		if poisoned != nil {
			return rejected(poisoned)
		}
		if callbackErr != nil {
			return rejected(callbackErr)
		}
		return rejected(failure(SQLFailed, op.ctx.Err()))
	}
	err = raw.Commit(op.ctx)
	if err == nil {
		return foundation.CommittedResult()
	}
	if provenRollback(err, op.conn.Conn()) {
		t.discard()
		return rejected(failure(TransactionCommitFailed, err))
	}
	t.discard()
	return foundation.UnknownResult(attempt, cause)
}
func provenRollback(err error, conn *pgx.Conn) bool {
	// A transport error after Commit has been invoked is never proof that the
	// server did not commit. In pgx 5.11.0 even EOF after a sent COMMIT can become
	// connLockError (SafeToRetry=true) while decoding the response.
	if errors.Is(err, pgx.ErrTxCommitRollback) {
		return true
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Severity != "ERROR" || conn.IsClosed() || conn.PgConn().TxStatus() != 'I' {
		return false
	}
	switch pg.Code {
	case "40001", "40P01", "23503", "23505", "23P01":
		return true
	}
	return false
}
func (t *transaction) discard() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = t.op.conn.Conn().Close(ctx)
}
func (t *transaction) rollback() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := t.raw.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.discard()
	}
}
func (t *transaction) finishCallback() {
	t.mu.Lock()
	t.active = false
	rows := t.rows
	t.mu.Unlock()
	select {
	case <-t.gate:
		return
	default:
	}
	t.poisonWith(failure(TransactionRowsOpen, nil))
	t.op.cancel()
	if rows != nil {
		rows.Close()
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-t.gate:
	case <-timer.C:
		t.discard()
	}
}
func (s *Store) find(tx foundation.Tx) (*transaction, error) {
	state := s.state()
	state.mu.Lock()
	t := state.txs[tx]
	state.mu.Unlock()
	if t == nil {
		err := failure(InvalidTransaction, nil)
		if owner, ok := liveTokens.Load(tx); ok {
			owner.(*transaction).poisonWith(err)
		}
		return nil, err
	}
	t.mu.Lock()
	active := t.active
	t.mu.Unlock()
	if !active {
		return nil, t.poisonWith(failure(TransactionExpired, nil))
	}
	return t, nil
}
func (s *Store) InTx(tx foundation.Tx) (SQLExecutor, error) {
	t, err := s.find(tx)
	if err != nil {
		return nil, err
	}
	return &txExecutor{transaction: t}, nil
}
func (t *transaction) enter() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.active {
		return failure(TransactionExpired, nil)
	}
	if t.poison != nil {
		return t.poison
	}
	select {
	case <-t.gate:
		return nil
	default:
		err := failure(TransactionConcurrentUse, nil)
		t.poison = err
		return err
	}
}
func (t *transaction) leave() { t.gate <- struct{}{} }
func (t *transaction) context(ctx context.Context) (context.Context, func()) {
	op, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(t.op.ctx, cancel)
	return op, func() { stop(); cancel() }
}

func (s *Store) Acquire(ctx context.Context, tx foundation.Tx, key foundation.LockKey, mode foundation.LockMode) error {
	return s.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: key, Mode: mode}})
}

// RequireHeldLocks verifies the adapter's actual transaction locks. It never
// acquires a missing lock or upgrades a shared one. A failed requirement poisons
// the transaction even when a caller ignores the returned error.
func (s *Store) RequireHeldLocks(ctx context.Context, tx foundation.Tx, requests []foundation.LockRequest) error {
	t, err := s.find(tx)
	if err != nil {
		return err
	}
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	if err := ctx.Err(); err != nil {
		return t.poisonWith(failure(LockNotHeld, err))
	}
	for _, request := range requests {
		if request.Key.Validate() != nil || !request.Mode.Valid() {
			return t.poisonWith(failure(InvalidLock, nil))
		}
		held, ok := t.held[request.Key.Canonical()]
		if !ok || held == foundation.Shared && request.Mode == foundation.Exclusive {
			return t.poisonWith(failure(LockNotHeld, nil))
		}
	}
	return nil
}

func (s *Store) AcquireAll(ctx context.Context, tx foundation.Tx, requests []foundation.LockRequest) error {
	t, err := s.find(tx)
	if err != nil {
		return err
	}
	if err := t.enter(); err != nil {
		return err
	}
	defer t.leave()
	merged := make(map[string]foundation.LockRequest)
	for _, request := range requests {
		if request.Key.Validate() != nil || !request.Mode.Valid() {
			return t.poisonWith(failure(InvalidLock, nil))
		}
		if merged[request.Key.Canonical()].Mode != foundation.Exclusive {
			merged[request.Key.Canonical()] = request
		}
	}
	ordered := make([]foundation.LockKey, 0, len(merged))
	for _, request := range merged {
		ordered = append(ordered, request.Key)
	}
	sort.Slice(ordered, func(i, j int) bool { return foundation.CompareLockKeys(ordered[i], ordered[j]) < 0 })
	// Validate the complete batch before its first SQL lock acquisition.
	for _, key := range ordered {
		if held, ok := t.held[key.Canonical()]; ok {
			if held == foundation.Shared && merged[key.Canonical()].Mode == foundation.Exclusive {
				return t.poisonWith(failure(LockUpgradeForbidden, nil))
			}
			continue
		}
		if t.highest.Validate() == nil && foundation.CompareLockKeys(key, t.highest) < 0 {
			return t.poisonWith(failure(LockOrderViolation, nil))
		}
	}
	op, cancel := t.context(ctx)
	defer cancel()
	for _, key := range ordered {
		if _, ok := t.held[key.Canonical()]; ok {
			continue
		}
		query := "SELECT pg_advisory_xact_lock($1)"
		if merged[key.Canonical()].Mode == foundation.Shared {
			query = "SELECT pg_advisory_xact_lock_shared($1)"
		}
		if _, err := t.raw.Exec(op, query, key.AdvisoryKey()); err != nil {
			return t.poisonWith(failure(LockFailed, err))
		}
		t.held[key.Canonical()] = merged[key.Canonical()].Mode
		t.highest = key
	}
	return nil
}
