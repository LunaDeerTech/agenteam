//go:build integration

package model_test

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"testing/fstest"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestModelMeetingSummarySchema(t *testing.T) {
	t.Run("fresh20-nullable-no-default-and-constraints", func(t *testing.T) {
		v := newMeetingSummaryFixture(t, false)
		var count, max, rows int64
		if e := v.raw.QueryRow(testContext(t), `SELECT count(*),max(version),(SELECT count(*) FROM agenteam_model.meeting_summary_selection) FROM agenteam_meta.migration_journal WHERE state='applied'`).Scan(&count, &max, &rows); e != nil || count != 20 || max != 20 || rows != 0 {
			t.Fatal("fresh continuous20 invented a default", e)
		}
		if _, e := v.raw.Exec(testContext(t), `DELETE FROM agenteam_model.platform_selection`); e != nil {
			t.Fatal(e)
		}
		requireCode(t, v.service.InitializeMeetingSummarySelection(testContext(t)), f.DependencyUnbound)
		if e := v.service.Initialize(testContext(t)); e != nil {
			t.Fatal(e)
		}
		if e := v.service.InitializeMeetingSummarySelection(testContext(t)); e != nil {
			t.Fatal(e)
		}
		s := v.summary(t)
		if s.Model != nil || s.Version != 1 {
			t.Fatal("initial selector")
		}
		m := v.chat(t, false)
		for _, tc := range []struct {
			name, sql, code string
			args            []any
		}{
			{"bad-id", `UPDATE agenteam_model.meeting_summary_selection SET id='018f0000-0000-4000-8000-000000000001'`, "23514", nil},
			{"zero-version", `UPDATE agenteam_model.meeting_summary_selection SET version=0`, "23514", nil},
			{"null-version", `UPDATE agenteam_model.meeting_summary_selection SET version=NULL`, "23502", nil},
			{"null-singleton", `UPDATE agenteam_model.meeting_summary_selection SET singleton=NULL`, "23502", nil},
			{"false-singleton", `UPDATE agenteam_model.meeting_summary_selection SET singleton=false`, "23514", nil},
			{"second-singleton", `INSERT INTO agenteam_model.meeting_summary_selection(id,version,updated_at) VALUES($1,1,clock_timestamp())`, "23505", []any{newID[struct{}](t).String()}},
			{"unknown-model", `UPDATE agenteam_model.meeting_summary_selection SET model_id=$1`, "23503", []any{newID[mc.Model](t).String()}},
			{"summary-effort", `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,model_id,owner_version,reasoning_effort) VALUES('platform_selector',$1,'meeting_summary',$2,1,'high')`, "23514", []any{s.ID, m.String()}},
			{"summary-project", `INSERT INTO agenteam_model.references(owner_kind,owner_id,role,project_id,model_id,owner_version) VALUES('platform_selector',$1,'meeting_summary',$1,$2,1)`, "23514", []any{s.ID, m.String()}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, e := v.raw.Exec(testContext(t), tc.sql, tc.args...)
				var pg *pgconn.PgError
				if !errors.As(e, &pg) || pg.Code != tc.code {
					t.Fatal("constraint not enforced", e)
				}
			})
		}
		v.selectModel(t, m)
		if _, e := v.raw.Exec(testContext(t), `DELETE FROM agenteam_model.models WHERE id=$1`, m.String()); e == nil {
			t.Fatal("selected model FK not protected")
		}
	})
	t.Run("populated19-failed20-atomic-same-checksum-retry", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrateModel(t, modelMigrator(t, db, modelMigrationFiles(t, "00019")), 19)
		raw := openStore(t, db.Config(t, nil))
		base := assemble(t, db, raw, raw)
		base.admin = base.human(t, "admin")
		provider := base.provider(t, mc.OpenAIChat, nil)
		base.model(t, provider, mc.ChatModel)
		snapshot := func() string {
			var out string
			if e := raw.QueryRow(testContext(t), `SELECT jsonb_build_array((SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM agenteam_model.providers p),(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agenteam_model.models m),(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_model.commands c),(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a),(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e),(SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='agenteam_model.references'::regclass AND conname='references_check'))::text`).Scan(&out); e != nil {
				t.Fatal(e)
			}
			return out
		}
		before := snapshot()
		files := modelMigrationFiles(t, "00020")
		name := "00020_model_meeting_summary_selection.sql"
		files[name] = &fstest.MapFile{Data: append(append([]byte{}, files[name].Data...), []byte("\nSELECT public.summary_migration_owned_dependency();\n")...)}
		checksum := fmt.Sprintf("sha256:%x", sha256.Sum256(files[name].Data))
		runner := modelMigrator(t, db, files)
		result := runner.Migrate(testContext(t))
		var pg *pgconn.PgError
		if result.Migrated || !errors.As(result.Fault, &pg) || pg.Code != "42883" {
			t.Fatal("expected transactional DDL failure", result.Fault)
		}
		var absent bool
		var stored string
		if e := raw.QueryRow(testContext(t), `SELECT to_regclass('agenteam_model.meeting_summary_selection') IS NULL,(SELECT checksum FROM agenteam_meta.migration_journal WHERE version=20 AND state='pending')`).Scan(&absent, &stored); e != nil || !absent || stored != checksum || snapshot() != before {
			t.Fatal("partial DDL/history changed", e)
		}
		if _, e := raw.Exec(testContext(t), `CREATE FUNCTION public.summary_migration_owned_dependency() RETURNS void LANGUAGE SQL AS 'SELECT NULL::void'`); e != nil {
			t.Fatal(e)
		}
		migrateModel(t, runner, 20)
		var summaryRows int64
		if e := raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_model.meeting_summary_selection`).Scan(&summaryRows); e != nil || summaryRows != 0 {
			t.Fatal("populated upgrade invented a choice", e)
		}
	})
}
