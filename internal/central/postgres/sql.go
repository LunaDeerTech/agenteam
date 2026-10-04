package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SQLExecutor is for trusted SQL repositories, not externally supplied SQL.
// It intentionally omits Begin, Commit, Rollback, CopyFrom and raw connections.
type SQLExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (*Rows, error)
	QueryRow(context.Context, string, ...any) Row
}
type Row interface{ Scan(...any) error }
type txExecutor struct{ transaction *transaction }

func (e *txExecutor) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t := e.transaction
	if err := t.enter(); err != nil {
		return pgconn.CommandTag{}, err
	}
	defer t.leave()
	if transactionControl(sql) {
		return pgconn.CommandTag{}, t.poisonWith(failure(TransactionControlForbidden, nil))
	}
	op, cancel := t.context(ctx)
	defer cancel()
	tag, err := t.raw.Exec(op, sql, args...)
	if err != nil {
		return tag, t.poisonWith(failure(SQLFailed, op.sqlError(err)))
	}
	return tag, nil
}
func (e *txExecutor) Query(ctx context.Context, sql string, args ...any) (*Rows, error) {
	t := e.transaction
	if err := t.enter(); err != nil {
		return nil, err
	}
	if transactionControl(sql) {
		t.leave()
		return nil, t.poisonWith(failure(TransactionControlForbidden, nil))
	}
	op, cancel := t.context(ctx)
	raw, err := t.raw.Query(op, sql, args...)
	if err != nil {
		err = op.sqlError(err)
		cancel()
		t.leave()
		return nil, t.poisonWith(failure(SQLFailed, err))
	}
	rows := &Rows{raw: raw, transaction: t, ctx: op, cancellation: op, cancel: cancel}
	rows.release = func() { t.mu.Lock(); t.rows = nil; t.mu.Unlock(); t.leave() }
	t.mu.Lock()
	t.rows = rows
	t.mu.Unlock()
	return rows, nil
}
func (e *txExecutor) QueryRow(ctx context.Context, sql string, args ...any) Row {
	rows, err := e.Query(ctx, sql, args...)
	return oneRow{rows, err}
}

// Ordinary reads use the same admission/ownership accounting as transactions.
// Query owns its checkout until Close or EOF; callers must always close Rows.
func (s *Store) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if transactionControl(sql) {
		return pgconn.CommandTag{}, failure(TransactionControlForbidden, nil)
	}
	op, err := s.borrow(ctx)
	if err != nil {
		return pgconn.CommandTag{}, err
	}
	defer op.release()
	sqlCtx := op.sqlContext(op.ctx, ctx)
	defer sqlCtx.finish()
	tag, err := op.conn.Exec(sqlCtx, sql, args...)
	if err != nil {
		return tag, failure(SQLFailed, sqlCtx.sqlError(err))
	}
	return tag, nil
}
func (s *Store) Query(ctx context.Context, sql string, args ...any) (*Rows, error) {
	if transactionControl(sql) {
		return nil, failure(TransactionControlForbidden, nil)
	}
	op, err := s.borrow(ctx)
	if err != nil {
		return nil, err
	}
	sqlCtx := op.sqlContext(op.ctx, ctx)
	raw, err := op.conn.Query(sqlCtx, sql, args...)
	if err != nil {
		err = sqlCtx.sqlError(err)
		sqlCtx.finish()
		op.release()
		return nil, failure(SQLFailed, err)
	}
	return &Rows{raw: raw, ctx: sqlCtx, cancellation: sqlCtx,
		cancel:  func() { sqlCtx.cancelInternal(op.cancel) },
		release: func() { sqlCtx.finish(); op.release() }}, nil
}
func (s *Store) QueryRow(ctx context.Context, sql string, args ...any) Row {
	rows, err := s.Query(ctx, sql, args...)
	return oneRow{rows, err}
}

type oneRow struct {
	rows *Rows
	err  error
}

func (r oneRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	defer r.rows.Close()
	if !r.rows.Next() {
		if err := r.rows.Err(); err != nil {
			return err
		}
		return pgx.ErrNoRows
	}
	return r.rows.Scan(dest...)
}

// Rows does not expose pgx's Conn or raw values. Its methods reject concurrent
// access instead of racing the underlying connection. A live Rows reserves the
// transaction's single SQL slot until Close/EOF.
type Rows struct {
	raw          pgx.Rows
	transaction  *transaction
	ctx          context.Context
	cancellation *poolSQLContext
	cancel       context.CancelFunc
	release      func()
	mu           sync.Mutex
	closed       atomic.Bool
	err          atomic.Pointer[Error]
}

func (r *Rows) setError(err *Error) {
	r.err.CompareAndSwap(nil, err)
	if r.transaction != nil {
		r.transaction.poisonWith(err)
	}
}
func (r *Rows) enter() bool {
	if !r.mu.TryLock() {
		r.setError(failure(TransactionConcurrentUse, nil))
		r.cancel()
		return false
	}
	if r.closed.Load() {
		r.mu.Unlock()
		return false
	}
	if r.transaction != nil {
		r.transaction.mu.Lock()
		active := r.transaction.active
		r.transaction.mu.Unlock()
		if !active {
			r.setError(failure(TransactionExpired, nil))
			r.closeLocked()
			r.mu.Unlock()
			return false
		}
	}
	return true
}
func (r *Rows) leave() {
	if r.ctx.Err() != nil {
		r.closeLocked()
	}
	r.mu.Unlock()
}
func (r *Rows) Next() bool {
	if !r.enter() {
		return false
	}
	defer r.leave()
	if !r.raw.Next() {
		r.closeLocked()
		return false
	}
	return true
}
func (r *Rows) Scan(dest ...any) error {
	if !r.enter() {
		if err := r.Err(); err != nil {
			return err
		}
		return failure(TransactionExpired, nil)
	}
	defer r.leave()
	if err := r.raw.Scan(dest...); err != nil {
		safe := failure(SQLFailed, r.cancellation.sqlError(err))
		r.setError(safe)
		return safe
	}
	return nil
}
func (r *Rows) Err() error {
	if err := r.err.Load(); err != nil {
		return err
	}
	return nil
}
func (r *Rows) Close() {
	if !r.mu.TryLock() {
		r.setError(failure(TransactionConcurrentUse, nil))
		r.cancel()
		return
	}
	defer r.mu.Unlock()
	r.closeLocked()
}
func (r *Rows) closeLocked() {
	if r.closed.Swap(true) {
		return
	}
	r.raw.Close()
	if err := r.raw.Err(); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		r.setError(failure(SQLFailed, r.cancellation.sqlError(err)))
	}
	r.cancel()
	r.release()
}

// A small lexer catches transaction control at statement boundaries, including
// comments and quoted strings. This is a misuse check for trusted repository
// code, not a SQL sandbox or an authorization parser.
func transactionControl(sql string) bool {
	first := true
	for i := 0; i < len(sql); {
		c := sql[i]
		if c == ';' {
			first = true
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if i+1 < len(sql) && sql[i:i+2] == "--" {
			if end := strings.IndexByte(sql[i:], '\n'); end >= 0 {
				i += end + 1
				continue
			}
			return false
		}
		if i+1 < len(sql) && sql[i:i+2] == "/*" {
			i += 2
			depth := 1
			for i < len(sql) && depth > 0 {
				if i+1 < len(sql) && sql[i:i+2] == "/*" {
					depth++
					i += 2
				} else if i+1 < len(sql) && sql[i:i+2] == "*/" {
					depth--
					i += 2
				} else {
					i++
				}
			}
			continue
		}
		if c == '\'' || c == '"' {
			i++
			for i < len(sql) {
				if sql[i] == c {
					i++
					if i < len(sql) && sql[i] == c {
						i++
						continue
					}
					break
				}
				if sql[i] == '\\' && i+1 < len(sql) {
					i += 2
				} else {
					i++
				}
			}
			first = false
			continue
		}
		if c == '$' {
			j := i + 1
			for j < len(sql) && (sql[j] == '_' || sql[j] >= 'a' && sql[j] <= 'z' || sql[j] >= 'A' && sql[j] <= 'Z' || sql[j] >= '0' && sql[j] <= '9') {
				j++
			}
			if j < len(sql) && sql[j] == '$' {
				tag := sql[i : j+1]
				if end := strings.Index(sql[j+1:], tag); end >= 0 {
					i = j + 1 + end + len(tag)
					first = false
					continue
				}
			}
		}
		start := i
		for i < len(sql) && (sql[i] >= 'a' && sql[i] <= 'z' || sql[i] >= 'A' && sql[i] <= 'Z' || sql[i] == '_') {
			i++
		}
		if i == start {
			i++
			first = false
			continue
		}
		if first {
			switch strings.ToUpper(sql[start:i]) {
			case "BEGIN", "START", "COMMIT", "END", "ROLLBACK", "ABORT", "SAVEPOINT", "RELEASE", "PREPARE":
				return true
			}
			first = false
		}
	}
	return false
}
