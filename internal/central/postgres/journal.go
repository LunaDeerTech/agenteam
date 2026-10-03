package postgres

import (
	"context"
	"database/sql"
	"strings"

	"github.com/pressly/goose/v3/database"
)

const journalDDL = `CREATE TABLE agenteam_meta.migration_journal (
 version bigint PRIMARY KEY CHECK(version > 0),
 filename text NOT NULL,
 checksum text NOT NULL CHECK(checksum ~ '^sha256:[0-9a-f]{64}$'),
 mode text NOT NULL CHECK(mode IN ('tx','non_tx')),
 state text NOT NULL CHECK(state IN ('pending','running','applied','needs_repair','repairing')),
 attempt_id uuid NOT NULL,
 started_at timestamptz NOT NULL,
 finished_at timestamptz,
 safe_code text
)`

type sqlQueries interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func bootstrapMetadata(ctx context.Context, conn *sql.Conn) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return failure(MigrationFailed, err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS agenteam_meta"); err != nil {
		return failure(MigrationFailed, err)
	}
	var gooseExists, journalExists bool
	if err := tx.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL,to_regclass($2) IS NOT NULL", migrationTable, journalTable).Scan(&gooseExists, &journalExists); err != nil {
		return failure(MigrationFailed, err)
	}
	if gooseExists != journalExists {
		return failure(MigrationHistoryDiverged, nil)
	}
	if !gooseExists {
		store, err := database.NewStore(database.DialectPostgres, migrationTable)
		if err != nil {
			return failure(MigrationFailed, err)
		}
		if err := store.CreateVersionTable(ctx, tx); err != nil {
			return failure(MigrationFailed, err)
		}
		if err := store.Insert(ctx, tx, database.InsertRequest{Version: 0}); err != nil {
			return failure(MigrationFailed, err)
		}
		if _, err := tx.ExecContext(ctx, journalDDL); err != nil {
			return failure(MigrationFailed, err)
		}
	}
	if err := verifyMetadataFormat(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return failure(MigrationFailed, err)
	}
	return nil
}
func verifyMetadataFormat(ctx context.Context, q sqlQueries) error {
	for _, table := range []struct {
		name, signature, defaults string
		checks                    int
	}{
		{migrationTable, "id:integer:true:d|version_id:bigint:true:|is_applied:boolean:true:|tstamp:timestamp without time zone:true:", "tstamp:now()", 0},
		{journalTable, "version:bigint:true:|filename:text:true:|checksum:text:true:|mode:text:true:|state:text:true:|attempt_id:uuid:true:|started_at:timestamp with time zone:true:|finished_at:timestamp with time zone:false:|safe_code:text:false:", "", 4},
	} {
		var signature string
		var kind string
		var primary, checks, constraints int
		var valid bool
		err := q.QueryRowContext(ctx, `SELECT c.relkind::text,string_agg(a.attname || ':' || format_type(a.atttypid,a.atttypmod) || ':' || a.attnotnull::text || ':' || a.attidentity::text,'|' ORDER BY a.attnum)
 FROM pg_class c JOIN pg_attribute a ON a.attrelid=c.oid WHERE c.oid=to_regclass($1) AND a.attnum>0 AND NOT a.attisdropped GROUP BY c.relkind`, table.name).Scan(&kind, &signature)
		if err != nil || kind != "r" || signature != table.signature {
			return failure(MigrationHistoryDiverged, err)
		}
		if err := q.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE contype='p' AND conkey=ARRAY[1]::smallint[]),count(*) FILTER(WHERE contype='c'),count(*),bool_and(convalidated AND NOT condeferrable) FROM pg_constraint WHERE conrelid=to_regclass($1)`, table.name).Scan(&primary, &checks, &constraints, &valid); err != nil || primary != 1 || checks != table.checks || constraints != 1+checks || !valid {
			return failure(MigrationHistoryDiverged, err)
		}
		var defaults string
		if err := q.QueryRowContext(ctx, `SELECT coalesce(string_agg(a.attname || ':' || pg_get_expr(d.adbin,d.adrelid),'|' ORDER BY a.attnum),'') FROM pg_attribute a JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum WHERE a.attrelid=to_regclass($1)`, table.name).Scan(&defaults); err != nil || defaults != table.defaults {
			return failure(MigrationHistoryDiverged, err)
		}
	}
	rows, err := q.QueryContext(ctx, "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid=to_regclass($1) AND contype='c'", journalTable)
	if err != nil {
		return failure(MigrationHistoryDiverged, err)
	}
	defer rows.Close()
	expected := map[string]bool{
		"CHECK ((version > 0))":                                    true,
		"CHECK ((checksum ~ '^sha256:[0-9a-f]{64}$'::text))":       true,
		"CHECK ((mode = ANY (ARRAY['tx'::text, 'non_tx'::text])))": true,
		"CHECK ((state = ANY (ARRAY['pending'::text, 'running'::text, 'applied'::text, 'needs_repair'::text, 'repairing'::text])))": true,
	}
	for rows.Next() {
		var definition string
		if err := rows.Scan(&definition); err != nil || !expected[definition] {
			return failure(MigrationHistoryDiverged, err)
		}
		delete(expected, definition)
	}
	if err := rows.Err(); err != nil || len(expected) != 0 {
		return failure(MigrationHistoryDiverged, err)
	}
	return nil
}

type journalRecord struct {
	version                         int64
	filename, checksum, mode, state string
}

// reconcile is only called while owning the same session guard as execution.
func reconcile(ctx context.Context, q sqlQueries, source Source, repair int64) (int64, error) {
	rows, err := q.QueryContext(ctx, "SELECT version_id,is_applied FROM agenteam_meta.goose_db_version ORDER BY version_id,id")
	if err != nil {
		return 0, failure(MigrationHistoryDiverged, err)
	}
	var prefix int64 = -1
	for rows.Next() {
		var version int64
		var applied bool
		if err := rows.Scan(&version, &applied); err != nil || !applied || version != prefix+1 || version > source.Target() {
			rows.Close()
			return 0, failure(MigrationHistoryDiverged, err)
		}
		prefix = version
	}
	err = rows.Err()
	rows.Close()
	if err != nil || prefix < 0 {
		return 0, failure(MigrationHistoryDiverged, err)
	}
	rows, err = q.QueryContext(ctx, "SELECT version,filename,checksum,mode,state FROM agenteam_meta.migration_journal ORDER BY version")
	if err != nil {
		return 0, failure(MigrationHistoryDiverged, err)
	}
	records := make([]journalRecord, 0)
	for rows.Next() {
		var record journalRecord
		if err := rows.Scan(&record.version, &record.filename, &record.checksum, &record.mode, &record.state); err != nil {
			rows.Close()
			return 0, failure(MigrationHistoryDiverged, err)
		}
		records = append(records, record)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, failure(MigrationHistoryDiverged, err)
	}
	if len(records) < int(prefix) || len(records) > int(prefix+1) {
		return 0, failure(MigrationHistoryDiverged, nil)
	}
	for i, record := range records {
		entry, ok := source.entry(record.version)
		if !ok || record.version != int64(i+1) || record.filename != entry.Filename || record.checksum != string(entry.Checksum) || record.mode != string(entry.Mode) {
			return 0, failure(MigrationHistoryDiverged, nil)
		}
		if record.version <= prefix {
			if record.state != "applied" && record.state != "running" && record.state != "repairing" {
				return 0, failure(MigrationHistoryDiverged, nil)
			}
			if record.state != "applied" {
				if _, err := q.ExecContext(ctx, "UPDATE agenteam_meta.migration_journal SET state='applied',finished_at=now(),safe_code=NULL WHERE version=$1", record.version); err != nil {
					return 0, failure(MigrationFailed, err)
				}
			}
			continue
		}
		if record.state == "applied" {
			return 0, failure(MigrationHistoryDiverged, nil)
		}
		if entry.Mode == Transactional {
			if record.state != "pending" && record.state != "running" {
				return 0, failure(MigrationHistoryDiverged, nil)
			}
			if record.state == "running" {
				if _, err := q.ExecContext(ctx, "UPDATE agenteam_meta.migration_journal SET state='pending',safe_code=$2 WHERE version=$1", record.version, string(MigrationFailed)); err != nil {
					return 0, failure(MigrationFailed, err)
				}
			}
		} else if record.state != "pending" {
			if record.state != "running" && record.state != "needs_repair" && record.state != "repairing" {
				return 0, failure(MigrationHistoryDiverged, nil)
			}
			if repair == record.version {
				continue
			}
			if record.state != "repairing" {
				if _, err := q.ExecContext(ctx, "UPDATE agenteam_meta.migration_journal SET state='needs_repair',safe_code=$2 WHERE version=$1", record.version, string(MigrationRepairRequired)); err != nil {
					return 0, failure(MigrationFailed, err)
				}
			}
			return prefix, failure(MigrationRepairRequired, nil)
		}
	}
	if repair != 0 && prefix != repair-1 {
		return 0, failure(MigrationRepairUnsupported, nil)
	}
	return prefix, nil
}
func checkSQLCompatibility(ctx context.Context, q sqlQueries) error {
	var version int
	var available bool
	var installed string
	if err := q.QueryRowContext(ctx, "SELECT current_setting('server_version_num')::int,EXISTS(SELECT 1 FROM pg_available_extension_versions WHERE name='vector' AND version='0.8.1'),coalesce((SELECT extversion FROM pg_extension WHERE extname='vector'),'')").Scan(&version, &available, &installed); err != nil {
		return failure(ConnectionFailed, err)
	}
	if version/10000 != 17 || version%10000 < 8 {
		return failure(VersionUnsupported, nil)
	}
	if !available || installed != "" && installed != "0.8.1" {
		return failure(ExtensionUnsupported, nil)
	}
	return nil
}
func verifyInstalledVector(ctx context.Context, q sqlQueries) error {
	var version string
	if err := q.QueryRowContext(ctx, "SELECT extversion FROM pg_extension WHERE extname='vector'").Scan(&version); err != nil || strings.TrimSpace(version) != "0.8.1" {
		return failure(ExtensionUnsupported, err)
	}
	return nil
}
