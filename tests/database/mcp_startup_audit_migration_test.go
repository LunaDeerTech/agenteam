//go:build integration

package database_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestMCPStartupAuditMigrationPreservesHumanAndClosesRuntimeFields(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	cfg := db.Config(t, nil)
	files := fstest.MapFS{}
	names, err := fs.Glob(migrations.SQL, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if name >= "00143" {
			continue
		}
		raw, err := fs.ReadFile(migrations.SQL, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: raw}
	}
	old, err := postgres.NewSource(files, nil)
	if err != nil || old.Target() != 142 {
		t.Fatal("expected exact schema 142 predecessor", err)
	}
	migrate(t, cfg, old)
	conn := db.Connect(t)
	const project = "01900000-0000-7000-8000-000000000031"
	const config = "01900000-0000-7000-8000-000000000032"
	const connection = "01900000-0000-7000-8000-000000000033"
	const other = "01900000-0000-7000-8000-000000000034"
	const user = "01900000-0000-7000-8000-000000000035"
	const session = "01900000-0000-7000-8000-000000000036"
	const digest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	type row struct {
		id, cause, scope, actor, action, outcome, resource, producer string
		project, actorProject, user, session, service, serviceCause  any
		resourceID, requestID                                        any
		ordinal                                                      int64
		metadata                                                     map[string]any
	}
	fresh := func() row {
		id, err := f.NewID[struct{}]()
		if err != nil {
			t.Fatal(err)
		}
		return row{
			id: id.String(), cause: id.String(), scope: "project", project: project,
			actor: "service", actorProject: project, service: "mcp-runtime", serviceCause: id.String(),
			action: "mcp.discovery.refresh", outcome: "success", resource: "mcp_connection", resourceID: connection, producer: "mcp",
			metadata: map[string]any{"config_id": config, "connection_id": connection, "version": "2", "tool_count": 1, "resource_count": 1, "invalid_count": 0},
		}
	}
	insert := func(t *testing.T, r row) error {
		t.Helper()
		metadata, err := json.Marshal(r.metadata)
		if err != nil {
			t.Fatal(err)
		}
		_, err = conn.Exec(testContext(t), `INSERT INTO agenteam_audit.audit_records
		 (id,scope,project_id,actor_kind,user_id,session_id,actor_project_id,service_name,service_cause,
		 action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal,request_id)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14::jsonb,$15,$16,$17,$18,$19)`,
			r.id, r.scope, r.project, r.actor, r.user, r.session, r.actorProject, r.service, r.serviceCause,
			r.action, r.outcome, r.resource, r.resourceID, string(metadata), digest, r.producer, r.cause, r.ordinal, r.requestID)
		return err
	}
	rejected := func(t *testing.T, r row) {
		t.Helper()
		err := insert(t, r)
		var constraint *pgconn.PgError
		if !errors.As(err, &constraint) || constraint.Code != "23514" {
			t.Fatalf("expected CHECK rejection, got error type %T", err)
		}
	}
	historical := fresh()
	historical.actor, historical.user, historical.session = "human", user, session
	historical.actorProject, historical.service, historical.serviceCause = nil, nil, nil
	if err = insert(t, historical); err != nil {
		t.Fatalf("schema 142 rejected the existing Human contract: %T", err)
	}
	var before string
	if err = conn.QueryRow(testContext(t), `SELECT row_to_json(r)::text FROM agenteam_audit.audit_records r WHERE id=$1`, historical.id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// This is the old DDL mismatch, not an attempt to manufacture domain authority.
	rejected(t, fresh())
	m := migrate(t, cfg)
	if result := m.Migrate(testContext(t)); !result.Migrated || result.Version != fixtureVersion(t)-1 {
		t.Fatal("repeat migration failed")
	}
	store := openStore(t, cfg)
	health, err := store.Check(testContext(t))
	if err != nil || !health.PostgreSQL || !health.PGVector || !health.Migrations || !health.ReadWrite {
		t.Fatalf("upgraded health failed: %T", err)
	}
	var after string
	if err = conn.QueryRow(testContext(t), `SELECT row_to_json(r)::text FROM agenteam_audit.audit_records r WHERE id=$1`, historical.id).Scan(&after); err != nil || before != after {
		t.Fatal("migration changed the historical Human row", err)
	}
	for _, outcome := range []string{"success", "failed"} {
		r := fresh()
		r.outcome = outcome
		if outcome == "failed" {
			r.metadata["error_code"] = "credential_denied"
		}
		if err = insert(t, r); err != nil {
			t.Fatalf("valid runtime %s rejected: %T", outcome, err)
		}
	}
	t.Run("maximum-version", func(t *testing.T) {
		r := fresh()
		r.metadata["version"] = "9223372036854775807"
		if err := insert(t, r); err != nil {
			t.Fatalf("maximum typed version rejected: %T", err)
		}
	})
	for _, tc := range []struct {
		name   string
		change func(*row)
	}{
		{"foreign-project", func(r *row) { r.actorProject = other }},
		{"missing-project", func(r *row) { r.actorProject = nil }},
		{"system", func(r *row) { r.scope, r.project, r.actorProject = "system", nil, nil }},
		{"other-service", func(r *row) { r.service = "secret" }},
		{"human-field", func(r *row) { r.user = user }},
		{"cause-mismatch", func(r *row) { r.serviceCause = other }},
		{"digest-cause", func(r *row) { r.cause, r.serviceCause = digest, digest }},
		{"other-action", func(r *row) { r.action = "mcp.connection.connect" }},
		{"other-producer", func(r *row) { r.producer = "secret" }},
		{"other-resource", func(r *row) { r.resource = "mcp_config" }},
		{"target-mismatch", func(r *row) { r.resourceID = other }},
		{"request-association", func(r *row) { r.requestID = other }},
		{"ordinal", func(r *row) { r.ordinal = 1 }},
		{"extra-metadata", func(r *row) { r.metadata["body"] = "not-allowed" }},
		{"missing-count", func(r *row) { delete(r.metadata, "tool_count") }},
		{"string-count", func(r *row) { r.metadata["tool_count"] = "1" }},
		{"version-overflow", func(r *row) { r.metadata["version"] = "9223372036854775808" }},
		{"missing-failure-code", func(r *row) { r.outcome = "failed" }},
		{"unknown-failure-code", func(r *row) { r.outcome = "failed"; r.metadata["error_code"] = "unrecognized" }},
		{"success-with-failure", func(r *row) { r.metadata["error_code"] = "credential_denied" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fresh()
			tc.change(&r)
			rejected(t, r)
		})
	}
	// The predecessor Human branch still accepts its old shape and rejects
	// metadata/target substitutions after the technical branch is added.
	human := fresh()
	human.actor, human.user, human.session = "human", user, session
	human.actorProject, human.service, human.serviceCause = nil, nil, nil
	if err = insert(t, human); err != nil {
		t.Fatalf("existing Human shape changed: %T", err)
	}
	human.id, human.cause = other, other
	human.metadata["connection_id"] = other
	rejected(t, human)
}
