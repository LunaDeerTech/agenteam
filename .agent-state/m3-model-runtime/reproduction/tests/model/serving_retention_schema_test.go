//go:build integration

package model_test

import (
	"errors"
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestModelM3ServingRetentionPopulatedUpgrade(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	migrateModel(t, modelMigrator(t, db, modelMigrationFiles(t, "00114")), 114)
	raw := openStore(t, db.Config(t, nil))
	base := assembleProjectConfiguration(t, db, raw, raw)
	base.admin = base.human(t, "admin")
	base.owner = base.human(t, "user")
	base.regular = base.owner
	base.project = base.createProject(t, base.owner)
	base.scope, _ = id.InProject(base.project.ID)
	_, err := raw.Exec(testContext(t), `CREATE SCHEMA model_resolution_fixture; CREATE TABLE model_resolution_fixture.consumers(unit text PRIMARY KEY,request_data jsonb NOT NULL,version bigint NOT NULL,enabled boolean NOT NULL); CREATE TABLE model_resolution_fixture.inputs(unit text PRIMARY KEY,snapshot_id uuid NOT NULL)`)
	requireTextRuntime(t, err)
	v := bindCurrentResolution(t, base)
	_, configured, _ := v.config(t, false, false)
	snapshot := v.resolve(t, v.request(t, configured, true)).Snapshot.ID
	facts := func() string {
		var out string
		err := raw.QueryRow(testContext(t), `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_model.snapshots s),(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agenteam_model.providers p),(SELECT jsonb_agg(to_jsonb(b) ORDER BY preparation_id) FROM agenteam_model.snapshot_bindings b))::text`).Scan(&out)
		requireTextRuntime(t, err)
		return out
	}
	before := facts()
	migrator := modelMigrator(t, db, modelMigrationFiles(t, "00115"))
	migrateModel(t, migrator, 115)
	migrateModel(t, migrator, 115)
	if before != facts() {
		t.Fatal("forward retention migration changed immutable history")
	}
	conn := db.Connect(t)
	var count int
	requireTextRuntime(t, conn.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_model.serving_snapshot_retentions`).Scan(&count))
	if count != 0 {
		t.Fatal("migration invented authorized generation references")
	}
	const insert = `INSERT INTO agenteam_model.serving_snapshot_retentions(reference_id,project_id,consumer_kind,agent_id,generation_id,owner_version,snapshot_id,snapshot_digest,credential_id,credential_scope,credential_project_id,state,released_at,released_version) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8,$9,$10,$11,$12,$13)`
	var digest string
	requireTextRuntime(t, conn.QueryRow(testContext(t), `SELECT snapshot_digest FROM agenteam_model.snapshots WHERE id=$1`, snapshot.String()).Scan(&digest))
	for _, name := range []string{"memory missing agent", "knowledge unexpected agent", "wrong credential project", "released missing time", "released missing version", "wrong snapshot project"} {
		t.Run(name, func(t *testing.T) {
			project, kind, state := base.project.ID.String(), "knowledge", "retained"
			var agent, credential, scope, credentialProject, releasedAt, releasedVersion any
			want := "23514"
			switch name {
			case "memory missing agent":
				kind = "memory"
			case "knowledge unexpected agent":
				agent = newID[id.Agent](t).String()
			case "wrong credential project":
				credential = newID[struct{}](t).String()
				scope = "project"
				credentialProject = newID[id.Project](t).String()
			case "released missing time":
				state = "released"
				releasedVersion = int64(2)
			case "released missing version":
				state = "released"
			case "wrong snapshot project":
				project = newID[id.Project](t).String()
				want = "23503"
			}
			_, err := conn.Exec(testContext(t), insert, newID[struct{}](t).String(), project, kind, agent, newID[struct{}](t).String(), snapshot.String(), digest, credential, scope, credentialProject, state, releasedAt, releasedVersion)
			var pg *pgconn.PgError
			if !errors.As(err, &pg) || pg.Code != want {
				t.Fatalf("constraint SQLSTATE mismatch expected=%s", want)
			}
		})
	}
}
