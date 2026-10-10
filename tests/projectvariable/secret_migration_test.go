//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// This test requires the real installed 00030. It never executes the draft
// directly or bypasses the production Migrator's continuous prefix protocol.
func TestSecretVariableOwnerMigration(t *testing.T) {
	t.Run("empty-repeat-and-exact-new-schema", func(t *testing.T) {
		db := newDatabase(t)
		migrate(t, db)
		raw := openStore(t, db.Config(t, nil))
		var journal, goose int
		var ready, deferred bool
		err := raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version=30 AND state='applied'),
 (SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id=30 AND is_applied),
 to_regclass('agenteam_projectvariable.secret_commands') IS NOT NULL
 AND to_regclass('agenteam_projectvariable.secret_history') IS NOT NULL
 AND to_regclass('agenteam_projectvariable.secret_project_generations') IS NOT NULL
 AND to_regclass('agenteam_projectvariable.secret_references') IS NOT NULL,
 (SELECT condeferrable AND condeferred FROM pg_constraint WHERE conrelid='agenteam_projectvariable.secret_history'::regclass AND conname='secret_history_command')`).Scan(&journal, &goose, &ready, &deferred)
		if err != nil || journal != 1 || goose != 1 || !ready || !deferred {
			t.Fatal("actual migration30 not complete/idempotent/deferred", err, journal, goose, ready, deferred)
		}
	})
	t.Run("ordinary-stored-facts-survive-upgrade", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00029"))
		raw := openStore(t, db.Config(t, nil))
		v := assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
		in := createInput(t, "LEGACY_ORDINARY", "ordinary-plaintext-contract")
		m := meta(t, "ordinary-before30", nil)
		created, err := v.service.CreateVariable(ctxFor(t), v.ownerBrowser.actor, m, v.project.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		before := ordinaryMigrationSnapshot(t, v)
		migrate(t, db)
		migrate(t, db)
		if before != ordinaryMigrationSnapshot(t, v) {
			t.Fatal("migration changed ordinary stored facts")
		}
		replayed, err := v.service.CreateVariable(ctxFor(t), v.ownerBrowser.actor, m, v.project.ID, in)
		if err != nil {
			t.Fatal(err)
		}
		sameReceipt(t, created, replayed)
		secret := assembleSecretOwnerFixture(t, v)
		_, err = secret.owner.CreateSecretVariable(ctxFor(t), v.ownerBrowser.actor, meta(t, "secret-after30", nil), v.project.ID, secretCreateInput(t, "FIRST_SECRET", []byte("migration-secret-material")))
		if err != nil {
			t.Fatal("upgraded schema does not support actual producer", err)
		}
		got, err := v.service.GetVariable(ctxFor(t), v.ownerBrowser.actor, v.project.ID, in.Fields().ID)
		if err != nil || got.Fields().Value != in.Fields().Value {
			t.Fatal("new Secret changed old ordinary value", err)
		}
	})
	t.Run("actual-closed-checks-and-deferred-history", func(t *testing.T) {
		v := newSecretOwnerFixture(t)
		a, p := v.ownerBrowser.actor, v.project.ID
		in := secretCreateInput(t, "CONSTRAINT_SECRET", []byte("protected-check-value"))
		r, err := v.owner.CreateSecretVariable(ctxFor(t), a, meta(t, "constraints-create", nil), p, in)
		if err != nil {
			t.Fatal(err)
		}
		ordinary := v.createVariable(t, "CONSTRAINT_ORDINARY", "ordinary")
		for _, test := range []struct {
			name, sql, constraint string
			arg                   any
		}{
			{"secret-plaintext", `UPDATE agenteam_projectvariable.variables SET value='forbidden',version=version+1,updated_at=clock_timestamp() WHERE id=$1`, "variables_payload_shape", in.Fields().ID.String()},
			{"secret-null-internal-version", `UPDATE agenteam_projectvariable.variables SET credential_version=NULL,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, "variables_payload_shape", in.Fields().ID.String()},
			{"ordinary-null-value", `UPDATE agenteam_projectvariable.variables SET value=NULL,version=version+1,updated_at=clock_timestamp() WHERE id=$1`, "variables_payload_shape", ordinary.Fields().ID.String()},
			{"completed-without-audit", `UPDATE agenteam_projectvariable.secret_commands SET audit_id=NULL WHERE target_id=$1`, "secret_commands_result_shape", in.Fields().ID.String()},
			{"audit-extra-material-field", `UPDATE agenteam_audit.audit_records SET metadata=metadata||'{"value":"forbidden"}'::jsonb WHERE id=$1`, "audit_records_projectsecretvariable_contract", r.Fields().AuditID.String()},
		} {
			t.Run(test.name, func(t *testing.T) {
				before := v.secretSnapshot(t)
				_, err := v.raw.Exec(ctxFor(t), test.sql, test.arg)
				var pg *pgconn.PgError
				if !errors.As(err, &pg) || pg.Code != "23514" || pg.ConstraintName != test.constraint {
					t.Fatal("target real CHECK not reached", err)
				}
				if before != v.secretSnapshot(t) {
					t.Fatal("rejected constraint changed stored facts")
				}
			})
		}
		baseline := v.secretSnapshot(t)
		updated := false
		result := v.tracked.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
			x, err := v.tracked.InTx(tx)
			if err != nil {
				return err
			}
			tag, err := x.Exec(ctx, `UPDATE agenteam_projectvariable.secret_history SET operation_id=$2 WHERE project_id=$1 AND variable_id=$3`, p.String(), id[vc.Operation](t).String(), in.Fields().ID.String())
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return errors.New("history row absent")
			}
			updated = true
			_, err = x.Exec(ctx, `SET CONSTRAINTS agenteam_projectvariable.secret_history_command IMMEDIATE`)
			return err
		})
		var pg *pgconn.PgError
		if !updated || result.State() != f.NotCommitted || !errors.As(result.Fault(), &pg) || pg.Code != "23503" || pg.ConstraintName != "secret_history_command" {
			t.Fatal("real deferred FK did not reject incomplete history", result.State(), result.Fault())
		}
		if baseline != v.secretSnapshot(t) {
			t.Fatal("deferred constraint failure escaped rollback")
		}
	})
}

func ordinaryMigrationSnapshot(t *testing.T, v *variableHTTPFixture) string {
	t.Helper()
	var raw string
	err := v.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'variables',(SELECT jsonb_agg(to_jsonb(x)-ARRAY['credential_id','credential_version'] ORDER BY id) FROM agenteam_projectvariable.variables x),
 'commands',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_projectvariable.commands x),
 'history',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_projectvariable.history x),
 'generation',(SELECT jsonb_agg(to_jsonb(x) ORDER BY project_id) FROM agenteam_projectvariable.project_generations x),
 'audit',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_audit.audit_records x),
 'events',(SELECT jsonb_agg(to_jsonb(x) ORDER BY id) FROM agenteam_outbox.events x))::text`).Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
