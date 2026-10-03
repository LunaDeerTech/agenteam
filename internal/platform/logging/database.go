package logging

import "log/slog"

// DatabasePhase describes technical process state without importing the
// Central adapter. Arbitrary SQL, errors and configuration are never accepted.
type DatabasePhase string

const (
	DatabaseConnecting  DatabasePhase = "connecting"
	DatabaseMigrating   DatabasePhase = "migrating"
	DatabaseMigrated    DatabasePhase = "migrated"
	DatabaseChecking    DatabasePhase = "checking"
	DatabaseHealthy     DatabasePhase = "healthy"
	DatabaseUnavailable DatabasePhase = "unavailable"
	DatabaseRepairing   DatabasePhase = "repairing"
	DatabaseRepaired    DatabasePhase = "repaired_to_pending"
)

func (l *Logger) Database(phase DatabasePhase, code, sqlState string, version int64) {
	switch phase {
	case DatabaseConnecting, DatabaseMigrating, DatabaseMigrated, DatabaseChecking, DatabaseHealthy, DatabaseUnavailable, DatabaseRepairing, DatabaseRepaired:
	default:
		phase = DatabaseUnavailable
	}
	if code != "" {
		switch code {
		case "DATABASE_CONFIGURATION_INVALID", "DATABASE_ENVIRONMENT_REJECTED", "DATABASE_CONNECTION_FAILED", "DATABASE_VERSION_UNSUPPORTED", "DATABASE_EXTENSION_UNSUPPORTED", "DATABASE_STOPPING", "DATABASE_DRAIN_TIMEOUT", "DATABASE_SQL_FAILED", "DATABASE_HEALTH_FAILED", "MIGRATION_INVALID", "MIGRATION_FAILED", "MIGRATION_HISTORY_DIVERGED", "MIGRATION_REPAIR_REQUIRED", "MIGRATION_REPAIR_UNSUPPORTED", "MIGRATION_REPAIR_FAILED", "MIGRATION_GUARD_FAILED":
		default:
			code = "DATABASE_FAILED"
		}
	}
	valid := len(sqlState) == 5
	for _, c := range sqlState {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			valid = false
		}
	}
	if !valid {
		sqlState = ""
	}
	if version < 0 {
		version = 0
	}
	attrs := []slog.Attr{slog.String("event", "database"), slog.String("database_phase", string(phase))}
	if code != "" {
		attrs = append(attrs, slog.String("code", code))
	}
	if sqlState != "" {
		attrs = append(attrs, slog.String("sqlstate", sqlState))
	}
	if version > 0 {
		attrs = append(attrs, slog.Int64("migration_version", version))
	}
	level := slog.LevelInfo
	if phase == DatabaseUnavailable || code != "" {
		level = slog.LevelError
	}
	l.logger.LogAttrs(nil, level, "database", attrs...)
}
