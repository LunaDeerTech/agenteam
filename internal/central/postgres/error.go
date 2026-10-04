// Package postgres owns Central's PostgreSQL adapter. Raw SQL, driver errors
// and credentials are never implicit diagnostic projections.
package postgres

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5/pgconn"
)

type Code string

const (
	InvalidConfiguration        Code = "DATABASE_CONFIGURATION_INVALID"
	EnvironmentRejected         Code = "DATABASE_ENVIRONMENT_REJECTED"
	ConnectionFailed            Code = "DATABASE_CONNECTION_FAILED"
	VersionUnsupported          Code = "DATABASE_VERSION_UNSUPPORTED"
	ExtensionUnsupported        Code = "DATABASE_EXTENSION_UNSUPPORTED"
	AdmissionStopped            Code = "DATABASE_STOPPING"
	DrainTimeout                Code = "DATABASE_DRAIN_TIMEOUT"
	InvalidTransaction          Code = "INVALID_TRANSACTION"
	TransactionExpired          Code = "TRANSACTION_EXPIRED"
	TransactionConcurrentUse    Code = "TRANSACTION_CONCURRENT_USE"
	TransactionRowsOpen         Code = "TRANSACTION_ROWS_OPEN"
	TransactionNested           Code = "TRANSACTION_NESTED"
	TransactionControlForbidden Code = "TRANSACTION_CONTROL_FORBIDDEN"
	SQLFailed                   Code = "DATABASE_SQL_FAILED"
	TransactionBeginFailed      Code = "TRANSACTION_BEGIN_FAILED"
	TransactionCommitFailed     Code = "TRANSACTION_COMMIT_FAILED"
	LockOrderViolation          Code = "LOCK_ORDER_VIOLATION"
	LockUpgradeForbidden        Code = "LOCK_UPGRADE_FORBIDDEN"
	LockNotHeld                 Code = "LOCK_NOT_HELD"
	InvalidLock                 Code = "INVALID_LOCK"
	LockFailed                  Code = "DATABASE_LOCK_FAILED"
	MigrationInvalid            Code = "MIGRATION_INVALID"
	MigrationFailed             Code = "MIGRATION_FAILED"
	MigrationHistoryDiverged    Code = "MIGRATION_HISTORY_DIVERGED"
	MigrationRepairRequired     Code = "MIGRATION_REPAIR_REQUIRED"
	MigrationRepairUnsupported  Code = "MIGRATION_REPAIR_UNSUPPORTED"
	MigrationRepairFailed       Code = "MIGRATION_REPAIR_FAILED"
	MigrationGuardFailed        Code = "MIGRATION_GUARD_FAILED"
	HealthFailed                Code = "DATABASE_HEALTH_FAILED"
)

// Error retains diagnostic identity only through explicit Unwrap. A closure
// also prevents fmt from traversing a raw cause through private outer fields.
type Error struct {
	code     Code
	sqlState string
	field    string
	cause    func() error
}

func failure(code Code, err error) *Error {
	e := &Error{code: code}
	if err != nil {
		e.cause = func() error { return err }
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && validSQLState(pg.Code) {
		e.sqlState = pg.Code
	}
	return e
}
func invalidConfig(field string) *Error { return &Error{code: InvalidConfiguration, field: field} }
func (e *Error) Error() string {
	if e == nil {
		return string(SQLFailed)
	}
	return string(e.code)
}
func (e *Error) Code() Code {
	if e == nil {
		return SQLFailed
	}
	return e.code
}
func (e *Error) SQLState() string {
	if e == nil {
		return ""
	}
	return e.sqlState
}
func (e *Error) Field() string {
	if e == nil {
		return ""
	}
	return e.field
}
func (e *Error) Unwrap() error {
	if e == nil || e.cause == nil {
		return nil
	}
	return e.cause()
}
func (e Error) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, e.Error()) }
func (e Error) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Code     Code   `json:"code"`
		SQLState string `json:"sqlstate,omitempty"`
		Field    string `json:"field,omitempty"`
	}{e.code, e.sqlState, e.field})
}
func (e Error) LogValue() slog.Value {
	return slog.GroupValue(slog.String("code", e.Error()), slog.String("sqlstate", e.sqlState))
}
func CodeOf(err error) Code {
	var e *Error
	if errors.As(err, &e) {
		return e.Code()
	}
	return SQLFailed
}
func validSQLState(state string) bool {
	if len(state) != 5 {
		return false
	}
	for i := range state {
		if !(state[i] >= '0' && state[i] <= '9' || state[i] >= 'A' && state[i] <= 'Z') {
			return false
		}
	}
	return true
}
