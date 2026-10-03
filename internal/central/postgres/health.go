package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/jackc/pgx/v5"
)

type DatabaseHealth struct {
	PostgreSQL       bool               `json:"postgresql"`
	PGVector         bool               `json:"pgvector"`
	Migrations       bool               `json:"migrations"`
	ReadWrite        bool               `json:"read_write"`
	CheckedAt        foundation.Instant `json:"checked_at"`
	ServerVersion    string             `json:"server_version"`
	ExtensionVersion string             `json:"extension_version"`
}

// Check performs a real write/read probe inside a transaction which is always
// rolled back. No health rows or command facts are committed.
func (s *Store) Check(ctx context.Context) (DatabaseHealth, error) {
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	op, err := s.borrow(bounded)
	if err != nil {
		return DatabaseHealth{}, err
	}
	defer op.release()
	if err := checkCompatibility(op.ctx, op.conn.Conn()); err != nil {
		return DatabaseHealth{}, err
	}
	tx, err := op.conn.Begin(op.ctx)
	if err != nil {
		return DatabaseHealth{}, failure(HealthFailed, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := tx.Rollback(cleanup); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			_ = op.conn.Conn().Close(cleanup)
		}
	}()
	var version, extension string
	var distance float64
	if err := tx.QueryRow(op.ctx, "SELECT current_setting('server_version'),(SELECT extversion FROM pg_extension WHERE extname='vector'),'[1,2,3]'::vector <-> '[1,2,3]'::vector").Scan(&version, &extension, &distance); err != nil || extension != "0.8.1" || distance != 0 {
		return DatabaseHealth{}, failure(HealthFailed, err)
	}
	source, err := EmbeddedSource()
	if err != nil {
		return DatabaseHealth{}, failure(HealthFailed, err)
	}
	var count, distinct, minimum, maximum int64
	var allApplied bool
	if err := tx.QueryRow(op.ctx, "SELECT count(*),count(DISTINCT version_id),coalesce(min(version_id),-1),coalesce(max(version_id),-1),coalesce(bool_and(is_applied),false) FROM agenteam_meta.goose_db_version").Scan(&count, &distinct, &minimum, &maximum, &allApplied); err != nil || count != source.Target()+1 || distinct != count || minimum != 0 || maximum != source.Target() || !allApplied {
		return DatabaseHealth{}, failure(HealthFailed, err)
	}
	var journalCount int64
	if err := tx.QueryRow(op.ctx, "SELECT count(*) FROM agenteam_meta.migration_journal").Scan(&journalCount); err != nil || journalCount != source.Target() {
		return DatabaseHealth{}, failure(HealthFailed, err)
	}
	for _, entry := range source.Manifest() {
		var match bool
		if err := tx.QueryRow(op.ctx, "SELECT EXISTS(SELECT 1 FROM agenteam_meta.migration_journal WHERE version=$1 AND filename=$2 AND checksum=$3 AND mode=$4 AND state='applied')", entry.Version, entry.Filename, string(entry.Checksum), string(entry.Mode)).Scan(&match); err != nil || !match {
			return DatabaseHealth{}, failure(HealthFailed, err)
		}
	}
	id, err := foundation.NewID[foundation.TransactionAttempt]()
	if err != nil {
		return DatabaseHealth{}, failure(HealthFailed, err)
	}
	value := time.Now().UnixNano()
	if _, err := tx.Exec(op.ctx, "INSERT INTO agenteam_meta.health_probe(probe_id,value) VALUES($1,$2)", id.String(), value); err != nil {
		return DatabaseHealth{}, failure(HealthFailed, err)
	}
	var read int64
	if err := tx.QueryRow(op.ctx, "SELECT value FROM agenteam_meta.health_probe WHERE probe_id=$1", id.String()).Scan(&read); err != nil || read != value {
		return DatabaseHealth{}, failure(HealthFailed, err)
	}
	if err := tx.Rollback(op.ctx); err != nil {
		return DatabaseHealth{}, failure(HealthFailed, err)
	}
	checked, err := foundation.NewInstant(time.Now())
	if err != nil {
		return DatabaseHealth{}, failure(HealthFailed, err)
	}
	return DatabaseHealth{true, true, true, true, checked, version, extension}, nil
}
