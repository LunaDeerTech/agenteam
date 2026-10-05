//go:build integration

package model_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Exact scalar wire DTO used only to build corrupt durable facts with a matching
// digest. It creates no Actor, registration or Consumer authority.
type resolutionStoredRequest struct {
	Format          int `json:"format_version"`
	Actor           id.ActorDetails
	Consumer        mc.Consumer
	Purpose         mc.Purpose
	Source          mc.ResolutionSource
	ModelRef        *mc.ModelID
	Selection       *mc.SelectionRef
	ReasoningEffort string
	Owner           sc.OwnerDetails
}

func TestModelCurrentResolutionSchema(t *testing.T) {
	t.Run("fresh-continuous-17", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrateModel(t, modelMigrator(t, db, modelMigrationFiles(t, "00017")), 17)
		conn := db.Connect(t)
		var n int
		if e := conn.QueryRow(testContext(t), `SELECT count(*) FROM pg_tables WHERE schemaname='agenteam_model' AND tablename IN ('resolution_preparations','snapshots','snapshot_bindings')`).Scan(&n); e != nil || n != 3 {
			t.Fatal("fresh schema", n, e)
		}
	})
	t.Run("populated-16-failed-17-transaction-then-forward", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		prefix := modelMigrationFiles(t, "00016")
		migrateModel(t, modelMigrator(t, db, prefix), 16)
		raw := openStore(t, db.Config(t, nil))
		base := assembleProjectConfiguration(t, db, raw, raw)
		base.admin = base.human(t, "admin")
		base.regular = base.human(t, "user")
		base.owner = base.regular
		base.project = base.createProject(t, base.owner)
		base.scope, _ = id.InProject(base.project.ID)
		credential := base.credential(t, sc.Model)
		p := base.provider(t, mc.OpenAIChat, &credential.CredentialRef)
		m := base.model(t, p, mc.ChatModel)
		oldRows := func() string {
			var value string
			if e := raw.QueryRow(testContext(t), `SELECT jsonb_build_array((SELECT to_jsonb(p) FROM agenteam_model.providers p WHERE id=$1),(SELECT to_jsonb(m) FROM agenteam_model.models m WHERE id=$2),(SELECT to_jsonb(s) FROM agenteam_secret.secrets s WHERE id=$3),(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$4))::text`, p.ID.String(), m.ID.String(), credential.CredentialRef.Details().ID.String(), base.project.ID.String()).Scan(&value); e != nil {
				t.Fatal(e)
			}
			return value
		}
		original := oldRows()
		oldConstraints := func() string {
			var value string
			if e := raw.QueryRow(testContext(t), `SELECT coalesce(jsonb_agg(jsonb_build_object('namespace',n.nspname,'table',c.relname,'catalog',to_jsonb(k),'definition',pg_get_constraintdef(k.oid)) ORDER BY n.nspname,k.oid),'[]'::jsonb)::text FROM pg_constraint k JOIN pg_namespace n ON n.oid=k.connamespace LEFT JOIN pg_class c ON c.oid=k.conrelid WHERE n.nspname LIKE 'agenteam_%' AND NOT(n.nspname='agenteam_model' AND coalesce(c.relname,'') IN ('resolution_preparations','snapshots','snapshot_bindings'))`).Scan(&value); e != nil {
				t.Fatal(e)
			}
			return value
		}
		constraintsBefore := oldConstraints()
		files := modelMigrationFiles(t, "00017")
		name := "00017_model_current_resolution.sql"
		files[name] = &fstest.MapFile{Data: append(append([]byte{}, files[name].Data...), []byte("\nSELECT public.model_current_resolution_migration_dependency();\n")...)}
		expectedChecksum := fmt.Sprintf("sha256:%x", sha256.Sum256(files[name].Data))
		runner := modelMigrator(t, db, files)
		result := runner.Migrate(testContext(t))
		var pg *pgconn.PgError
		if result.Migrated || !errors.As(result.Fault, &pg) || pg.Code != "42883" {
			t.Fatalf("expected missing fixture function SQLSTATE 42883, got %v", result.Fault)
		}
		var absent bool
		if e := raw.QueryRow(testContext(t), `SELECT to_regclass('agenteam_model.resolution_preparations') IS NULL AND to_regclass('agenteam_model.snapshots') IS NULL AND to_regclass('agenteam_model.snapshot_bindings') IS NULL`).Scan(&absent); e != nil || !absent {
			t.Fatal("failed migration left partial tables", e)
		}
		if original != oldRows() || oldConstraints() != constraintsBefore {
			t.Fatal("failed 17 migration changed an old canonical row or constraint")
		}
		var pending, applied, goose int
		var checksum string
		const journalState = `SELECT (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=17 AND state='pending'),(SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=17 AND state='applied'),(SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=17 AND is_applied),(SELECT checksum FROM agenteam_meta.migration_journal WHERE version=17)`
		if e := raw.QueryRow(testContext(t), journalState).Scan(&pending, &applied, &goose, &checksum); e != nil || pending != 1 || applied != 0 || goose != 0 || checksum != expectedChecksum {
			t.Fatalf("failed migration journal pending=%d applied=%d goose=%d checksum=%s want=%s: %v", pending, applied, goose, checksum, expectedChecksum, e)
		}
		// Repair only the test dependency. The migrator retains exactly the same
		// source bytes and checksum across the failed transaction and retry.
		if _, e := raw.Exec(testContext(t), `CREATE FUNCTION public.model_current_resolution_migration_dependency() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); e != nil {
			t.Fatal(e)
		}
		migrateModel(t, runner, 17)
		if e := raw.QueryRow(testContext(t), journalState).Scan(&pending, &applied, &goose, &checksum); e != nil || pending != 0 || applied != 1 || goose != 1 || checksum != expectedChecksum {
			t.Fatalf("retry journal pending=%d applied=%d goose=%d checksum=%s want=%s: %v", pending, applied, goose, checksum, expectedChecksum, e)
		}
		var tables int
		if e := raw.QueryRow(testContext(t), `SELECT count(*) FROM pg_tables WHERE schemaname='agenteam_model' AND tablename IN ('resolution_preparations','snapshots','snapshot_bindings')`).Scan(&tables); e != nil || tables != 3 {
			t.Fatal("same-source retry schema", tables, e)
		}
		if original != oldRows() || oldConstraints() != constraintsBefore {
			t.Fatal("successful 17 migration changed an old canonical row or constraint")
		}
		if strings.Contains(string(files[name].Data), "+goose Down") {
			t.Fatal("forbidden Down")
		}
		bindCurrentResolution(t, base)
	})
	t.Run("sql-checks-and-strict-loaders", func(t *testing.T) {
		v := newCurrentResolutionFixture(t)
		_, m, _ := v.config(t, false, true)
		r := v.request(t, m, false)
		p := v.discover(t, r)
		for _, statement := range []string{`UPDATE agenteam_model.resolution_preparations SET plan_version=0 WHERE snapshot_id=$1`, `UPDATE agenteam_model.resolution_preparations SET phase='committed' WHERE snapshot_id=$1`, `UPDATE agenteam_model.resolution_preparations SET request_data=request_data||'{"format_version":0}'::jsonb WHERE snapshot_id=$1`, `UPDATE agenteam_model.resolution_preparations SET draft_plan='[]'::jsonb WHERE snapshot_id=$1`, `UPDATE agenteam_model.resolution_preparations SET semantic_digest='invalid' WHERE snapshot_id=$1`} {
			_, e := v.raw.Exec(testContext(t), statement, p.Details().SnapshotID.String())
			if e == nil {
				t.Fatal("SQL malformed fact accepted")
			}
		}
		out := v.resolve(t, r)
		_, e := v.raw.Exec(testContext(t), `UPDATE agenteam_model.snapshot_bindings SET lease_id=NULL WHERE snapshot_id=$1`, out.Snapshot.ID.String())
		if e == nil {
			t.Fatal("partial lease fact accepted")
		}
		_, e = v.raw.Exec(testContext(t), `UPDATE agenteam_model.snapshots SET snapshot_data=snapshot_data||'{"unknown_field":true}'::jsonb WHERE id=$1`, out.Snapshot.ID.String())
		if e != nil {
			t.Fatal(e)
		}
		bad, e := v.resolving.ResolveModel(testContext(t), r)
		resolutionNoResult(t, bad, e)
		requireCode(t, e, f.DependencyUnavailable)
	})
	for _, mode := range []string{"empty-actor-matching-digest", "wrong-canonical-unit"} {
		t.Run(mode, func(t *testing.T) {
			v := newCurrentResolutionFixture(t)
			_, m, _ := v.config(t, false, true)
			first := v.request(t, m, false)
			out := v.resolve(t, first)
			var preparation, identity string
			var raw []byte
			if e := v.raw.QueryRow(testContext(t), `SELECT id::text,resolution_identity,request_data FROM agenteam_model.resolution_preparations WHERE snapshot_id=$1`, out.Snapshot.ID.String()).Scan(&preparation, &identity, &raw); e != nil {
				t.Fatal(e)
			}
			var data resolutionStoredRequest
			if e := json.Unmarshal(raw, &data); e != nil {
				t.Fatal(e)
			}
			result := v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				x, e := v.raw.InTx(tx)
				if e != nil {
					return e
				}
				if mode == "empty-actor-matching-digest" {
					data.Actor = id.ActorDetails{}
					wire := resolutionJSON(data)
					if _, e = x.Exec(ctx, `UPDATE agenteam_model.resolution_preparations SET request_data=$2,semantic_digest=$3 WHERE id=$1`, preparation, wire, resolutionDigest(data)); e != nil {
						return e
					}
					_, e = x.Exec(ctx, `UPDATE agenteam_model.snapshot_bindings SET request_data=$2 WHERE preparation_id=$1`, preparation, wire)
					return e
				}
				wrong, _ := f.NewCommandIdentity("model.resolve", []string{first.Consumer.ProjectID.String(), newID[struct{}](t).String()}, "current.execution", "wrong-unit")
				if _, e = x.Exec(ctx, `UPDATE agenteam_model.resolution_preparations SET resolution_identity=$2 WHERE id=$1`, preparation, wrong.Canonical()); e != nil {
					return e
				}
				_, e = x.Exec(ctx, `UPDATE agenteam_model.snapshot_bindings SET resolution_identity=$2 WHERE preparation_id=$1`, preparation, wrong.Canonical())
				return e
			})
			if result.State() != f.Committed {
				t.Fatal("corruption fixture not installed", result.Fault())
			}
			next := first.Clone()
			next.Purpose = mc.AgentCompaction
			next.Consumer.Purpose = mc.AgentCompaction
			v.bind(t, next)
			p, e := v.resolving.DiscoverResolve(testContext(t), next)
			resolutionZeroPlan(t, p, e)
			requireCode(t, e, f.DependencyUnavailable)
			if v.count(t, "snapshot_bindings") != 1 {
				t.Fatal("bad canonical fact produced second binding")
			}
			t.Log("strict persistent-fact rejection; no claim of an exploitable authority bypass")
		})
	}
}
