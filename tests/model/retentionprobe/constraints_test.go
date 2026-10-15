//go:build integration

// Package retentionprobe checks the exact retention DDL in an owned PostgreSQL
// fixture. The minimal prerequisite table/domain isolate CHECK naming and
// semantics; this is not a full migration or business authorization test.
package retentionprobe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func setup(t *testing.T) (*pgx.Conn, context.Context) {
	t.Helper()
	db := pgfixture.NewDatabase(t)
	conn := db.Connect(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	_, err := conn.Exec(ctx, `CREATE SCHEMA agenteam_model;
CREATE DOMAIN agenteam_model.safe_id AS uuid;
CREATE TABLE agenteam_model.snapshots(id uuid NOT NULL,project_id uuid NOT NULL,UNIQUE(id,project_id));`)
	if err != nil {
		t.Fatal(err)
	}
	return conn, ctx
}

func apply(t *testing.T, conn *pgx.Conn, ctx context.Context, file string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", file))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.Exec(ctx, string(raw)); err != nil {
		t.Fatal("exact migration SQL", file, err)
	}
}

func constraints(t *testing.T, conn *pgx.Conn, ctx context.Context, phase string) map[string]string {
	t.Helper()
	rows, err := conn.Query(ctx, `SELECT conname,pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='agenteam_model.serving_snapshot_retentions'::regclass AND contype='c' ORDER BY conname`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, definition string
		if err := rows.Scan(&name, &definition); err != nil {
			t.Fatal(err)
		}
		out[name] = definition
		t.Logf("RETENTION_CHECK phase=%s name=%s definition=%s", phase, name, definition)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestServingRetentionConstraintCatalog(t *testing.T) {
	conn, ctx := setup(t)
	apply(t, conn, ctx, "00115_model_serving_retention.sql")
	before := constraints(t, conn, ctx, "115")
	apply(t, conn, ctx, "00127_model_summary_snapshot_retention.sql")
	after := constraints(t, conn, ctx, "127")
	if len(before) == 0 || len(after) == 0 {
		t.Fatal("missing actual retention CHECK catalog")
	}
}

func TestServingRetentionForwardRepair(t *testing.T) {
	conn, ctx := setup(t)
	apply(t, conn, ctx, "00115_model_serving_retention.sql")
	apply(t, conn, ctx, "00127_model_summary_snapshot_retention.sql")
	const project = "018f0000-0000-7000-8000-000000000001"
	const snapshot = "018f0000-0000-7000-8000-000000000002"
	const agent = "018f0000-0000-7000-8000-000000000003"
	if _, err := conn.Exec(ctx, `INSERT INTO agenteam_model.snapshots(id,project_id) VALUES($1,$2)`, snapshot, project); err != nil {
		t.Fatal(err)
	}
	insert := func(kind string, agentID any, state string, releasedVersion, releasedAt any) (string, error) {
		var id string
		err := conn.QueryRow(ctx, `INSERT INTO agenteam_model.serving_snapshot_retentions(reference_id,project_id,consumer_kind,agent_id,generation_id,owner_version,snapshot_id,snapshot_digest,state,released_version,released_at)
VALUES(gen_random_uuid(),$1,$2,$3,gen_random_uuid(),2,$4,'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',$5,$6,$7) RETURNING reference_id::text`, project, kind, agentID, snapshot, state, releasedVersion, releasedAt).Scan(&id)
		return id, err
	}
	at := time.Now().Add(time.Hour)
	bad, err := insert("knowledge", nil, "released", int64(1), at)
	if err != nil {
		t.Fatal("127 regression was not reproduced", err)
	}
	var original, after string
	row := `SELECT (to_jsonb(r)||jsonb_build_object('xmin',r.xmin::text))::text FROM agenteam_model.serving_snapshot_retentions r WHERE reference_id=$1`
	if err = conn.QueryRow(ctx, row, bad).Scan(&original); err != nil {
		t.Fatal(err)
	}
	repair, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "migrations", "00133_model_retention_constraint_repair.sql"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(ctx, string(repair))
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" || pg.ConstraintName != "model_serving_retention_released_version" {
		t.Fatal("invalid existing release version did not abort repair")
	}
	if err = conn.QueryRow(ctx, row, bad).Scan(&after); err != nil || original != after {
		t.Fatal("failed migration rewrote invalid stored evidence", err)
	}
	checks := constraints(t, conn, ctx, "failed-repair")
	if checks["serving_snapshot_retentions_check1"] == "" || checks["model_serving_retention_released_version"] != "" {
		t.Fatal("failed ALTER partially changed constraints")
	}
	// Delete only this deliberately invalid probe row; production repair never
	// edits it. Existing valid Knowledge/Memory rows must survive byte-for-byte.
	if _, err = conn.Exec(ctx, `DELETE FROM agenteam_model.serving_snapshot_retentions WHERE reference_id=$1`, bad); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		kind  string
		agent any
	}{{"knowledge", nil}, {"memory", agent}} {
		if _, err = insert(v.kind, v.agent, "retained", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	const all = `SELECT jsonb_agg(to_jsonb(r)||jsonb_build_object('xmin',r.xmin::text) ORDER BY reference_id)::text FROM agenteam_model.serving_snapshot_retentions r`
	if err = conn.QueryRow(ctx, all).Scan(&original); err != nil {
		t.Fatal(err)
	}
	apply(t, conn, ctx, "00133_model_retention_constraint_repair.sql")
	if err = conn.QueryRow(ctx, all).Scan(&after); err != nil || original != after {
		t.Fatal("repair changed valid stored references", err)
	}
	checks = constraints(t, conn, ctx, "133")
	if checks["serving_snapshot_retentions_check1"] != "" || checks["model_serving_retention_released_version"] == "" || checks["model_serving_retention_owner"] == "" || checks["serving_snapshot_retentions_check2"] == "" || checks["serving_snapshot_retentions_check3"] == "" {
		t.Fatal("repair changed unrelated constraints or omitted bound")
	}
	for _, v := range []struct {
		name, kind, state  string
		agent, version, at any
		valid              bool
	}{
		{"meeting-retained", "meeting", "retained", nil, nil, nil, true},
		{"meeting-released-same-version", "meeting", "released", nil, int64(2), at, true},
		{"memory-released-new-version", "memory", "released", agent, int64(3), at, true},
		{"meeting-agent", "meeting", "retained", agent, nil, nil, false},
		{"knowledge-agent", "knowledge", "retained", agent, nil, nil, false},
		{"memory-no-agent", "memory", "retained", nil, nil, nil, false},
		{"unknown-kind", "other", "retained", nil, nil, nil, false},
		{"released-old-version", "meeting", "released", nil, int64(1), at, false},
		{"released-no-version", "meeting", "released", nil, nil, at, false},
		{"released-no-time", "meeting", "released", nil, int64(2), nil, false},
	} {
		t.Run(v.name, func(t *testing.T) {
			_, err := insert(v.kind, v.agent, v.state, v.version, v.at)
			if v.valid {
				if err != nil {
					t.Fatal("valid owner/lifecycle rejected", err)
				}
				return
			}
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != "23514" {
				t.Fatal("invalid owner/lifecycle accepted")
			}
		})
	}
}
