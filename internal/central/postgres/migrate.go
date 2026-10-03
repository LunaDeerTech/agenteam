package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

const migrationTable = "agenteam_meta.goose_db_version"
const journalTable = "agenteam_meta.migration_journal"
const guardNamespace int32 = 0x4147544d

type MigrationState struct {
	Migrated bool
	Version  int64
	Fault    *Error
}
type RepairResult struct {
	RepairedToPending bool
	Version           int64
	Fault             *Error
}
type Migrator struct {
	config Config
	source Source
}

func NewMigrator(cfg Config, sources ...Source) (*Migrator, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if len(sources) > 1 {
		return nil, failure(MigrationInvalid, nil)
	}
	source, err := EmbeddedSource()
	if len(sources) == 1 {
		source = sources[0]
	}
	if err != nil || source.Target() == 0 {
		return nil, failure(MigrationInvalid, err)
	}
	return &Migrator{config: cfg, source: source}, nil
}
func (m *Migrator) database() (*sql.DB, func(), error) {
	parsed, err := m.config.driverConfig()
	if err != nil {
		return nil, nil, err
	}
	sockets := &socketSet{}
	parsed.DialFunc = sockets.dial
	db := stdlib.OpenDB(*parsed)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, func() { sockets.close(); _ = db.Close() }, nil
}

type silentGoose struct{}

func (silentGoose) Printf(string, ...any) {}
func (silentGoose) Fatalf(string, ...any) {} // Provider never exits the process.

func (m *Migrator) Migrate(ctx context.Context) MigrationState {
	ctx, cancel := context.WithTimeout(ctx, m.config.StartupTimeout())
	defer cancel()
	db, closeDB, err := m.database()
	if err != nil {
		return MigrationState{Fault: failure(MigrationFailed, err)}
	}
	defer closeDB()
	for _, entry := range m.source.Manifest() {
		locker := &migrationLocker{source: m.source, target: entry.Version}
		provider, err := goose.NewProvider(goose.DialectPostgres, db, m.source.data().files, goose.WithTableName(migrationTable), goose.WithDisableGlobalRegistry(true), goose.WithSessionLocker(locker), goose.WithLogger(silentGoose{}))
		if err != nil {
			return MigrationState{Fault: failure(MigrationInvalid, err)}
		}
		_, err = provider.ApplyVersion(ctx, entry.Version, true)
		if locker.fault != nil {
			return MigrationState{Fault: locker.fault}
		}
		if err != nil && !errors.Is(err, goose.ErrAlreadyApplied) {
			return MigrationState{Fault: failure(MigrationFailed, err)}
		}
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return MigrationState{Fault: failure(MigrationFailed, err)}
	}
	defer conn.Close()
	locker := &migrationLocker{source: m.source}
	if err := locker.SessionLock(ctx, conn); err != nil {
		return MigrationState{Fault: asMigrationError(err)}
	}
	err = verifyInstalledVector(ctx, conn)
	if unlockErr := locker.SessionUnlock(ctx, conn); unlockErr != nil {
		err = unlockErr
	}
	if err != nil {
		return MigrationState{Fault: asMigrationError(err)}
	}
	return MigrationState{Migrated: true, Version: m.source.Target()}
}
func asMigrationError(err error) *Error {
	var safe *Error
	if errors.As(err, &safe) {
		return safe
	}
	return failure(MigrationFailed, err)
}

type migrationLocker struct {
	source Source
	target int64
	repair int64
	held   bool
	fault  *Error
}

func (l *migrationLocker) SessionLock(ctx context.Context, conn *sql.Conn) (retErr error) {
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1::integer,$2::integer)", guardNamespace, 1); err != nil {
		discardSQL(conn)
		l.fault = failure(MigrationGuardFailed, err)
		return l.fault
	}
	l.held = true
	defer func() {
		if retErr != nil {
			l.fault = asMigrationError(retErr)
			_ = l.unlock(conn)
		}
	}()
	if err := checkSQLCompatibility(ctx, conn); err != nil {
		return err
	}
	if err := bootstrapMetadata(ctx, conn); err != nil {
		return err
	}
	prefix, err := reconcile(ctx, conn, l.source, l.repair)
	if err != nil {
		return err
	}
	if l.target == 0 {
		if l.repair == 0 && prefix != l.source.Target() {
			return failure(MigrationHistoryDiverged, nil)
		}
		return nil
	}
	if l.target <= prefix {
		return nil
	}
	if l.target != prefix+1 {
		return failure(MigrationHistoryDiverged, nil)
	}
	entry, _ := l.source.entry(l.target)
	attempt, err := foundation.NewID[foundation.TransactionAttempt]()
	if err != nil {
		return failure(MigrationFailed, err)
	}
	_, err = conn.ExecContext(ctx, `INSERT INTO agenteam_meta.migration_journal(version,filename,checksum,mode,state,attempt_id,started_at)
 VALUES($1,$2,$3,$4,'running',$5,now()) ON CONFLICT(version) DO UPDATE SET state='running',attempt_id=excluded.attempt_id,started_at=excluded.started_at,finished_at=NULL,safe_code=NULL`, entry.Version, entry.Filename, string(entry.Checksum), string(entry.Mode), attempt.String())
	if err != nil {
		return failure(MigrationFailed, err)
	}
	return nil
}
func (l *migrationLocker) SessionUnlock(_ context.Context, conn *sql.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := reconcile(ctx, conn, l.source, l.repair)
	if err != nil {
		l.fault = asMigrationError(err)
	}
	if unlockErr := l.unlock(conn); unlockErr != nil {
		l.fault = failure(MigrationGuardFailed, unlockErr)
	}
	if l.fault != nil {
		return l.fault
	}
	return nil
}
func (l *migrationLocker) unlock(conn *sql.Conn) error {
	if !l.held {
		return nil
	}
	l.held = false
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var unlocked bool
	err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1::integer,$2::integer)", guardNamespace, 1).Scan(&unlocked)
	if err != nil || !unlocked {
		discardSQL(conn)
		return failure(MigrationGuardFailed, err)
	}
	return nil
}
func discardSQL(conn *sql.Conn) { _ = conn.Raw(func(any) error { return driver.ErrBadConn }) }

func (m *Migrator) Repair(ctx context.Context, version int64, expectedChecksum foundation.Digest) (result RepairResult) {
	entry, ok := m.source.entry(version)
	if !ok || entry.Mode != NonTransactional {
		return RepairResult{Fault: failure(MigrationRepairUnsupported, nil)}
	}
	if expectedChecksum.Validate() != nil || expectedChecksum != entry.Checksum {
		return RepairResult{Fault: failure(MigrationHistoryDiverged, nil)}
	}
	ctx, cancel := context.WithTimeout(ctx, m.config.StartupTimeout())
	defer cancel()
	db, closeDB, err := m.database()
	if err != nil {
		return RepairResult{Fault: failure(MigrationRepairFailed, err)}
	}
	defer closeDB()
	conn, err := db.Conn(ctx)
	if err != nil {
		return RepairResult{Fault: failure(MigrationRepairFailed, err)}
	}
	defer conn.Close()
	locker := &migrationLocker{source: m.source, repair: version}
	if err := locker.SessionLock(ctx, conn); err != nil {
		return RepairResult{Fault: asMigrationError(err)}
	}
	defer func() {
		if err := locker.SessionUnlock(ctx, conn); err != nil {
			result = RepairResult{Fault: asMigrationError(err)}
		}
	}()
	var state, checksum string
	if err := conn.QueryRowContext(ctx, "SELECT state,checksum FROM agenteam_meta.migration_journal WHERE version=$1", version).Scan(&state, &checksum); err != nil || checksum != string(entry.Checksum) {
		return RepairResult{Fault: failure(MigrationRepairUnsupported, err)}
	}
	if state != "running" && state != "needs_repair" && state != "repairing" {
		return RepairResult{Fault: failure(MigrationRepairUnsupported, nil)}
	}
	attempt, err := foundation.NewID[foundation.TransactionAttempt]()
	if err != nil {
		return RepairResult{Fault: failure(MigrationRepairFailed, err)}
	}
	if _, err := conn.ExecContext(ctx, "UPDATE agenteam_meta.migration_journal SET state='repairing',attempt_id=$2,started_at=now(),finished_at=NULL,safe_code=NULL WHERE version=$1", version, attempt.String()); err != nil {
		return RepairResult{Fault: failure(MigrationRepairFailed, err)}
	}
	plan := m.source.data().plans[version]
	for _, step := range plan.RestoreSteps {
		if _, err := conn.ExecContext(ctx, step); err != nil {
			return RepairResult{Fault: failure(MigrationRepairFailed, err)}
		}
	}
	verified, err := verifyRestored(ctx, conn, plan.VerifyRestored)
	if err != nil || !verified {
		return RepairResult{Fault: failure(MigrationRepairFailed, err)}
	}
	if _, err := conn.ExecContext(ctx, "UPDATE agenteam_meta.migration_journal SET state='pending',finished_at=now(),safe_code=NULL WHERE version=$1 AND state='repairing' AND attempt_id=$2", version, attempt.String()); err != nil {
		return RepairResult{Fault: failure(MigrationRepairFailed, err)}
	}
	return RepairResult{RepairedToPending: true, Version: version}
}
func verifyRestored(ctx context.Context, conn *sql.Conn, query string) (bool, error) {
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	columns, err := rows.ColumnTypes()
	if err != nil || len(columns) != 1 || columns[0].DatabaseTypeName() != "BOOL" {
		return false, failure(MigrationRepairFailed, err)
	}
	if !rows.Next() {
		return false, failure(MigrationRepairFailed, rows.Err())
	}
	var restored bool
	if err := rows.Scan(&restored); err != nil {
		return false, err
	}
	if rows.Next() {
		return false, failure(MigrationRepairFailed, nil)
	}
	return restored, rows.Err()
}
