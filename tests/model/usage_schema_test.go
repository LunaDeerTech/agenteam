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
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUsageSchemaAndHistory(t *testing.T) {
	t.Run("pg17-fresh-continuous-18", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrateModel(t, modelMigrator(t, db, modelMigrationFiles(t, "00018")), 18)
		conn := db.Connect(t)
		var version, tables int
		if e := conn.QueryRow(testContext(t), `SELECT current_setting('server_version_num')::int,(SELECT count(*) FROM pg_tables WHERE schemaname='agenteam_model' AND tablename IN ('invocations','invocation_observations','execution_usage_summaries'))`).Scan(&version, &tables); e != nil || version/10000 != 17 || version%10000 < 8 || tables != 3 {
			t.Fatal("supported fresh schema", e)
		}
		t.Logf("PostgreSQL %d; schema prefix 1..18", version)
	})
	t.Run("pg17-populated-17-failure-rollback-same-bytes-retry", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrateModel(t, modelMigrator(t, db, modelMigrationFiles(t, "00017")), 17)
		raw := openStore(t, db.Config(t, nil))
		base := assembleProjectConfiguration(t, db, raw, raw)
		base.admin = base.human(t, "admin")
		base.regular = base.human(t, "user")
		base.owner = base.regular
		base.project = base.createProject(t, base.owner)
		base.scope, _ = id.InProject(base.project.ID)
		provider := base.provider(t, mc.OpenAIChat, nil)
		base.model(t, provider, mc.ChatModel)
		oldRows := func() string {
			var data string
			if e := raw.QueryRow(testContext(t), `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agenteam_model.providers p),(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agenteam_model.models m),(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agenteam_project.projects p))::text`).Scan(&data); e != nil {
				t.Fatal(e)
			}
			return data
		}
		before := oldRows()
		files := modelMigrationFiles(t, "00018")
		name := "00018_model_invocation_usage.sql"
		files[name] = &fstest.MapFile{Data: append(append([]byte{}, files[name].Data...), []byte("\nSELECT public.usage_migration_owned_dependency();\n")...)}
		checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(files[name].Data))
		runner := modelMigrator(t, db, files)
		result := runner.Migrate(testContext(t))
		var pg *pgconn.PgError
		if result.Migrated || !errors.As(result.Fault, &pg) || pg.Code != "42883" {
			t.Fatal("expected owned missing migration dependency", result.Fault)
		}
		var absent bool
		var stored string
		if e := raw.QueryRow(testContext(t), `SELECT to_regclass('agenteam_model.invocations') IS NULL AND to_regclass('agenteam_model.invocation_observations') IS NULL AND to_regclass('agenteam_model.execution_usage_summaries') IS NULL,(SELECT checksum FROM agenteam_meta.migration_journal WHERE version=18 AND state='pending')`).Scan(&absent, &stored); e != nil || !absent || stored != checksum || oldRows() != before {
			t.Fatal("migration partial effects/checksum", e)
		}
		if _, e := raw.Exec(testContext(t), `CREATE FUNCTION public.usage_migration_owned_dependency() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); e != nil {
			t.Fatal(e)
		}
		migrateModel(t, runner, 18)
		if oldRows() != before {
			t.Fatal("populated migration changed old facts")
		}
		if strings.Contains(string(files[name].Data), "+goose Down") {
			t.Fatal("rollback migration forbidden")
		}
	})
	t.Run("pg16-remains-unsupported", func(t *testing.T) {
		descriptor, e := pgfixture.LoadUnsupported()
		if e != nil {
			t.Fatal(e)
		}
		conn, e := descriptor.Connect(testContext(t), "fixture_control")
		if e != nil {
			t.Fatal(e)
		}
		defer conn.Close(context.Background())
		var version int
		if e = conn.QueryRow(testContext(t), `SELECT current_setting('server_version_num')::int`).Scan(&version); e != nil || version/10000 != 16 {
			t.Fatal("PG16 counterexample", e)
		}
		config, e := descriptor.Config("fixture_control", nil)
		if e != nil {
			t.Fatal(e)
		}
		store, e := postgres.Open(testContext(t), config)
		if e == nil {
			_ = store.ForceClose(testContext(t))
			t.Fatal("PG16 admitted")
		}
		if postgres.CodeOf(e) != postgres.VersionUnsupported {
			t.Fatal("wrong version rejection", e)
		}
		m, e := postgres.NewMigrator(config)
		if e != nil {
			t.Fatal(e)
		}
		if result := m.Migrate(testContext(t)); result.Migrated || result.Fault.Code() != postgres.VersionUnsupported {
			t.Fatal("PG16 migrated")
		}
	})
	t.Run("constraints-and-live-fk-history", func(t *testing.T) {
		v := newUsageFixture(t, true)
		c := v.success(t, json.RawMessage(`{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}`), nil)
		for name, statement := range map[string]string{
			"U-R01-null-terminal-version": `UPDATE agenteam_model.invocations SET terminal_version=NULL WHERE id=$1`,
			"partial-terminal":            `UPDATE agenteam_model.invocations SET final_digest=NULL WHERE id=$1`,
			"negative-token":              `UPDATE agenteam_model.invocations SET input_tokens=-1 WHERE id=$1`,
			"known-unknown-source":        `UPDATE agenteam_model.invocations SET usage_source='unknown' WHERE id=$1`,
			"sent-without-time":           `UPDATE agenteam_model.invocations SET dispatched_at=NULL WHERE id=$1`,
			"wrong-consumer-projection":   `UPDATE agenteam_model.invocations SET consumer_kind='tool' WHERE id=$1`,
			"zero-sequence":               `UPDATE agenteam_model.invocations SET last_sequence=0 WHERE id=$1`,
			"unsafe-request-id":           `UPDATE agenteam_model.invocations SET provider_request_id=repeat('a',257) WHERE id=$1`,
		} {
			t.Run(name, func(t *testing.T) {
				_, e := v.raw.Exec(testContext(t), statement, c.event.Value.ID.String())
				var pg *pgconn.PgError
				if !errors.As(e, &pg) || pg.Code != "23514" {
					t.Fatal("SQL CHECK not enforced", e)
				}
			})
		}
		// Repetition-bound regression covers 255 and 256 safe characters in SQL.
		for _, n := range []int{255, 256} {
			result := v.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
				x, e := v.raw.InTx(tx)
				if e != nil {
					return e
				}
				if _, e = x.Exec(ctx, `UPDATE agenteam_model.invocations SET provider_request_id=repeat('a',$2) WHERE id=$1`, c.event.Value.ID.String(), n); e != nil {
					return e
				}
				return f.NewFault(f.DependencyUnavailable, f.NotStarted)
			})
			if result.State() != f.NotCommitted || usageErrorCode(result.Fault()) != f.DependencyUnavailable {
				t.Fatal("safe max-length SQL token rejected", result.Fault())
			}
		}
		model, e := v.service.GetModel(testContext(t), v.admin, c.call.Model.ModelID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = v.service.DeleteModel(testContext(t), mc.DeleteModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: model.ID, ExpectedVersion: model.Version}); e != nil {
			t.Fatal("formal model delete", e)
		}
		provider, e := v.service.GetProvider(testContext(t), v.admin, c.call.Model.ProviderID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = v.service.DeleteProvider(testContext(t), mc.DeleteProviderRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ID: provider.ID, ExpectedVersion: provider.Version}); e != nil {
			t.Fatal("formal provider delete", e)
		}
		page, e := v.ledger.List(testContext(t), v.owner, uc.Query{Filter: uc.Filter{ProjectID: v.project.ID, ProviderID: &c.call.Model.ProviderID, ModelID: &c.call.Model.ModelID}, Limit: 100})
		if e != nil || len(page.Items) != 1 || page.Items[0].LiveProviderID != nil || page.Items[0].LiveModelID != nil || page.Items[0].Identity != c.call.Model {
			t.Fatal("safe historical identity not retained", e)
		}
		r := uc.InvocationRequest{Actor: v.technical(t, c.event.Identity), Access: uc.ConfirmInvocation, Action: uc.FinalizeAction, Identity: c.event.Identity, Sequence: c.event.Sequence}
		lookup, e := v.ledger.LookupInvocation(testContext(t), r)
		if e != nil || !lookup.Observed || lookup.Receipt.Value.LiveProviderID != nil || lookup.Receipt.Value.LiveModelID != nil {
			t.Fatal("historical receipt live projection", e)
		}
	})
}
