//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	"github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// This is the first real 00032..00035 schema/metadata composition, not F1.
// All migrations run through the production Migrator. Existing business data
// comes from real services; no successful SQL insertion manufactures an Agent,
// owner witness, registration, reference head or assignment. Audit candidates
// below are explicit rollback-only CHECK probes, never authorization evidence.
func TestAgentConfigurationSchema(t *testing.T) {
	t.Run("fresh-prefix-and-repeat", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		prefix := migrationPrefix(t, "00035")
		migrate(t, db, prefix)
		raw := openStore(t, db.Config(t, nil))
		assertAgentSchema35(t, raw)
		if got := agentSchemaCounts(t, raw); got != ([15]int64{}) {
			t.Fatal("fresh schema manufactured Agent/configuration facts")
		}
		migrate(t, db, prefix)
		assertAgentSchema35(t, raw)
		if got := agentSchemaCounts(t, raw); got != ([15]int64{}) {
			t.Fatal("repeat migration manufactured configuration facts")
		}
	})
	t.Run("upgrade-preserves-facts-and-audit-checks", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00031"))
		raw := openStore(t, db.Config(t, nil))
		base := assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
		v := assembleSecretOwnerFixture(t, base)
		ordinary := v.createVariable(t, "SCHEMA_OLD_ORDINARY", "task-owned-value")
		secretInput := secretCreateInput(t, "SCHEMA_OLD_SECRET", []byte("task-owned-schema-value"))
		secretMeta := meta(t, "schema-old-secret", nil)
		created, err := v.owner.CreateSecretVariable(ctxFor(t), v.ownerBrowser.actor, secretMeta, v.project.ID, secretInput)
		if err != nil {
			t.Fatal("formal pre-upgrade Secret creation failed")
		}
		ordinaryBefore, secretBefore := ordinaryMigrationSnapshot(t, base), v.secretSnapshot(t)
		migrate(t, db, migrationPrefix(t, "00035"))
		migrate(t, db, migrationPrefix(t, "00035"))
		assertAgentSchema35(t, raw)
		if ordinaryBefore != ordinaryMigrationSnapshot(t, base) || secretBefore != v.secretSnapshot(t) {
			t.Fatal("upgrade or repeat changed original business/Audit/Outbox facts")
		}
		got, err := v.service.GetVariable(ctxFor(t), v.ownerBrowser.actor, v.project.ID, ordinary.Fields().ID)
		if err != nil || got.Fields().Value != ordinary.Fields().Value {
			t.Fatal("original ordinary variable is no longer readable")
		}
		replayed, err := v.owner.CreateSecretVariable(ctxFor(t), v.ownerBrowser.actor, secretMeta, v.project.ID, secretInput)
		if err != nil || replayed.Fields().AuditID != created.Fields().AuditID || replayed.Fields().Variable.Fields().ID != created.Fields().Variable.Fields().ID {
			t.Fatal("upgrade changed original Secret command replay")
		}
		// Both old producers must still append through their real post-upgrade
		// services, not merely leave historical rows grandfathered in place.
		v.createVariable(t, "SCHEMA_NEW_ORDINARY", "task-owned-after")
		if _, err = v.owner.CreateSecretVariable(ctxFor(t), v.ownerBrowser.actor, meta(t, "schema-new-secret", nil), v.project.ID, secretCreateInput(t, "SCHEMA_NEW_SECRET", []byte("task-owned-after"))); err != nil {
			t.Fatal("new Audit checks rejected the existing Secret producer")
		}
		before := v.secretSnapshot(t)
		checkAgentAuditSchema(t, v)
		if before != v.secretSnapshot(t) || agentSchemaCounts(t, raw) != ([15]int64{}) {
			t.Fatal("rollback-only Audit probes left persistent facts")
		}
	})
	t.Run("schema-invariants-and-unbound-dependencies", func(t *testing.T) {
		v := newAgentMetadataFixture(t)
		assertAgentSchema35(t, v.raw)
		assembly, err := agentconfiguration.New(v.tracked, agentconfiguration.Options{Accounts: v.accounts})
		if err != nil {
			t.Fatal("fixed real-authority composition failed")
		}
		providers := assembly.Providers()
		// The real consumers now use the fixed combined authority graph on
		// this same Store; no alternative metadata service or owner is faked.
		v.selection, v.directory = providers.ModelSelections, providers.SecretDirectory
		selected := v.createModel(t, false)
		in := secretCreateInput(t, "SCHEMA_METADATA_SECRET", []byte("task-owned-metadata"))
		created, err := v.owner.CreateSecretVariable(ctxFor(t), v.ownerBrowser.actor, meta(t, "schema-metadata", nil), v.project.ID, in)
		if err != nil {
			t.Fatal("formal metadata Secret creation failed")
		}
		p := v.discover(t, v.ownerBrowser.actor, selected.ID, mc.AgentModelConfiguration, []vc.VariableID{created.Fields().Variable.Fields().ID})
		before := v.counts(t)
		called := false
		result := v.inTx(t, p, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
			m, s, err := v.require(ctx, tx, p)
			if err != nil {
				return err
			}
			entries := s.Entries()
			if m.ModelID != selected.ID || m.ProviderID != selected.ProviderID || m.ModelVersion != selected.Version || len(entries) != 1 || entries[0].ID != in.Fields().ID || entries[0].Status != vc.SecretDirectoryValid {
				return errors.New("schema metadata identity mismatch")
			}
			called = true
			return nil
		})
		if !called || result.State() != f.Committed || result.Fault() != nil || before != v.counts(t) {
			t.Fatal("real metadata ports failed or created configuration side effects")
		}
		configuration, err := registry.NewConfiguration(providers.Registry, providers.Agents, "")
		var fault *f.Fault
		if configuration != nil || !errors.As(err, &fault) || fault.Code != f.DependencyUnbound {
			t.Fatal("missing real builtin backend was not explicitly unbound")
		}
		service, err := assembly.NewService(v.keys, nil)
		fault = nil
		if service != nil || !errors.As(err, &fault) || fault.Code != f.DependencyUnbound {
			t.Fatal("fixed composition bypassed its unbound registry")
		}
		if before != v.counts(t) || agentSchemaCounts(t, v.raw) != ([15]int64{}) {
			t.Fatal("unbound configuration manufactured references or Agent facts")
		}
		checkAgentSchemaRejections(t, v)
		if before != v.counts(t) || agentSchemaCounts(t, v.raw) != ([15]int64{}) {
			t.Fatal("rejected schema candidates left persistent facts")
		}
	})
}

func assertAgentSchema35(t *testing.T, raw *postgres.Store) {
	t.Helper()
	var journal, goose, constraints int
	var tables, deferred bool
	err := raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_meta.migration_journal WHERE version>0 AND state='applied'),
 (SELECT count(*) FROM agenteam_meta.goose_db_version WHERE version_id>0 AND is_applied),
 (SELECT count(*) FROM pg_constraint WHERE conrelid='agenteam_audit.audit_records'::regclass
  AND conname IN ('audit_records_action_check','audit_records_producer_check','audit_records_agent_contract') AND contype='c' AND convalidated),
 to_regclass('agenteam_agent.agents') IS NOT NULL AND to_regclass('agenteam_agent.commands') IS NOT NULL
 AND to_regclass('agenteam_tool.identities') IS NOT NULL AND to_regclass('agenteam_tool.agent_configurations') IS NOT NULL
 AND to_regclass('agenteam_skill.agent_assignment_heads') IS NOT NULL AND to_regclass('agenteam_skill.agent_assignments') IS NOT NULL
 AND to_regclass('agenteam_mount.mounts') IS NOT NULL AND to_regclass('agenteam_mount.agent_mount_heads') IS NOT NULL,
 (SELECT count(*)=2 AND bool_and(condeferrable AND condeferred) FROM pg_constraint WHERE
  (conrelid='agenteam_tool.identities'::regclass AND conname='tool_latest_spec') OR
  (conrelid='agenteam_skill.agent_assignment_heads'::regclass AND conname='skill_initial_assignment_identity'))`).Scan(&journal, &goose, &constraints, &tables, &deferred)
	if err != nil || journal != 35 || goose != 35 || constraints != 3 || !tables || !deferred {
		t.Fatal("continuous migrated prefix/repeat/schema contract not established")
	}
}

func agentSchemaCounts(t *testing.T, raw *postgres.Store) [15]int64 {
	t.Helper()
	var n [15]int64
	err := raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_agent.agents),(SELECT count(*) FROM agenteam_agent.commands),
 (SELECT count(*) FROM agenteam_agent.tool_allowlist),(SELECT count(*) FROM agenteam_agent.mount_allowlist),(SELECT count(*) FROM agenteam_agent.secret_allowlist),
 (SELECT count(*) FROM agenteam_tool.identities),(SELECT count(*) FROM agenteam_tool.spec_revisions),(SELECT count(*) FROM agenteam_tool.registrations),
 (SELECT count(*) FROM agenteam_tool.agent_configurations),(SELECT count(*) FROM agenteam_tool.agent_references),
 (SELECT count(*) FROM agenteam_skill.agent_assignment_heads),(SELECT count(*) FROM agenteam_skill.agent_assignments),
 (SELECT count(*) FROM agenteam_mount.mounts),(SELECT count(*) FROM agenteam_mount.agent_mount_heads),(SELECT count(*) FROM agenteam_mount.agent_mount_refs)
 `).Scan(&n[0], &n[1], &n[2], &n[3], &n[4], &n[5], &n[6], &n[7], &n[8], &n[9], &n[10], &n[11], &n[12], &n[13], &n[14])
	if err != nil {
		t.Fatal("configuration schema count query failed")
	}
	return n
}

var agentSchemaProbeRollback = errors.New("agent schema probe rollback")

func checkAgentAuditSchema(t *testing.T, v *secretOwnerFixture) {
	t.Helper()
	fields := []string{"allowed_mount_ids", "allowed_secret_variable_ids", "allowed_tool_ids", "approval_model_ref", "approval_policy", "description", "display_name", "inject_agents_md", "instructions", "model_ref", "name", "reasoning_effort", "tag_color"}
	for _, probe := range []struct {
		label, action, version string
		changed                []string
		extra                  bool
		want                   string
	}{
		{"create-branch", "agent.create", "1", fields, false, ""},
		{"update-branch", "agent.update", "2", []string{"display_name"}, false, ""},
		{"create-wrong-version", "agent.create", "2", fields, false, "audit_records_agent_contract"},
		{"update-empty-fields", "agent.update", "2", []string{}, false, "audit_records_agent_contract"},
		{"extra-private-field", "agent.create", "1", fields, true, "audit_records_agent_contract"},
		{"unknown-action", "agent.delete", "1", fields, false, "audit_records_action_check"},
	} {
		resource := id[i.Agent](t).String()
		metadata := map[string]any{"agent_id": resource, "version": probe.version, "command_id": id[struct{}](t).String(), "changed_fields": probe.changed}
		if probe.extra {
			metadata["value"] = "schema-private-canary"
		}
		encoded, err := json.Marshal(metadata)
		if err != nil {
			t.Fatal("probe metadata encoding failed")
		}
		inserted := false
		result := v.tracked.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
			x, err := v.tracked.InTx(tx)
			if err != nil {
				return err
			}
			a := v.ownerBrowser.actor.Details()
			tag, err := x.Exec(ctx, `INSERT INTO agenteam_audit.audit_records
 (id,scope,project_id,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal)
 VALUES($1,'project',$2,'human',$3,$4,$5,'success','agent',$6,$7::jsonb,$8,'agent',$8,0)`,
				id[struct{}](t).String(), v.project.ID.String(), a.UserID, a.SessionID, probe.action, resource, string(encoded), "sha256:"+strings.Repeat("0", 64))
			if err != nil {
				return err
			}
			inserted = tag.RowsAffected() == 1
			return agentSchemaProbeRollback
		})
		if result.State() != f.NotCommitted {
			t.Fatalf("Audit CHECK probe %s did not physically roll back", probe.label)
		}
		if probe.want == "" {
			if !inserted || !errors.Is(result.Fault(), agentSchemaProbeRollback) {
				t.Fatalf("Audit CHECK probe %s failed before explicit rollback", probe.label)
			}
			continue
		}
		assertSchemaPGError(t, probe.label, result.Fault(), "23514", probe.want)
		if inserted {
			t.Fatalf("Audit CHECK probe %s accepted an invalid tuple", probe.label)
		}
	}
}

func checkAgentSchemaRejections(t *testing.T, v *agentMetadataFixture) {
	t.Helper()
	projectID, agentID, targetID := v.project.ID.String(), id[i.Agent](t).String(), id[struct{}](t).String()
	for _, probe := range []struct {
		label, sql, code, constraint string
		args                         []any
	}{
		{"canonical-name", `INSERT INTO agenteam_agent.agents
 (id,project_id,name,normalized_name,description,instructions,inject_agents_md,model_id,approval_policy,lifecycle,version,created_at,updated_at)
 VALUES($2,$1,'a','a','','',true,$3,'default','active',1,transaction_timestamp(),transaction_timestamp())`, "23514", "agents_name_check", []any{projectID, agentID, targetID}},
		{"canonical-tool-orphan", `INSERT INTO agenteam_agent.tool_allowlist(project_id,agent_id,tool_id) VALUES($1,$2,$3)`, "23503", "tool_allowlist_project_id_agent_id_fkey", []any{projectID, agentID, targetID}},
		{"tool-reference-orphan", `INSERT INTO agenteam_tool.agent_references(agent_id,tool_id) VALUES($1,$2)`, "23503", "agent_references_agent_id_fkey", []any{agentID, targetID}},
		{"tool-head-zero-version", `INSERT INTO agenteam_tool.agent_configurations(project_id,agent_id,config_version) VALUES($1,$2,0)`, "23514", "agent_configurations_config_version_check", []any{projectID, agentID}},
		{"skill-head-zero-sequence", `INSERT INTO agenteam_skill.agent_assignment_heads
 (project_id,agent_id,creation_command,plan_revision,request_binding,add_skills_enabled,assignment_sequence,initial_skill_id,observed_revision,created_at)
 VALUES($1,$2,'schema-probe',1,'sha256:'||repeat('0',64),false,0,$3,1,transaction_timestamp())`, "23514", "agent_assignment_heads_assignment_sequence_check", []any{projectID, agentID, targetID}},
		{"mount-head-zero-version", `INSERT INTO agenteam_mount.agent_mount_heads(project_id,agent_id,owner_version) VALUES($1,$2,0)`, "23514", "agent_mount_heads_owner_version_check", []any{projectID, agentID}},
		{"mount-reference-orphan", `INSERT INTO agenteam_mount.agent_mount_refs(project_id,agent_id,mount_id) VALUES($1,$2,$3)`, "23503", "agent_mount_refs_project_id_agent_id_fkey", []any{projectID, agentID, targetID}},
	} {
		// Each candidate is expected to fail in the real schema. A surprising
		// success is still rolled back; it must never seed a later positive.
		result := v.tracked.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
			x, err := v.tracked.InTx(tx)
			if err != nil {
				return err
			}
			_, err = x.Exec(ctx, probe.sql, probe.args...)
			if err != nil {
				return err
			}
			return agentSchemaProbeRollback
		})
		if result.State() != f.NotCommitted {
			t.Fatalf("schema probe %s did not physically roll back", probe.label)
		}
		assertSchemaPGError(t, probe.label, result.Fault(), probe.code, probe.constraint)
	}
	inserted := false
	result := v.tracked.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx f.Tx) error {
		x, err := v.tracked.InTx(tx)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_skill.agent_assignments
 (id,project_id,agent_id,skill_id,enabled,assignment_sequence,created_at)
 VALUES($1,$2,$3,$4,true,1,transaction_timestamp())`, id[struct{}](t).String(), projectID, agentID, targetID)
		if err != nil {
			return err
		}
		inserted = tag.RowsAffected() == 1
		_, err = x.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
		if err != nil {
			return err
		}
		return agentSchemaProbeRollback
	})
	if !inserted || result.State() != f.NotCommitted {
		t.Fatal("deferred assignment FK probe did not reach actual constraint evaluation")
	}
	var pg *pgconn.PgError
	if !errors.As(result.Fault(), &pg) || pg.Code != "23503" || pg.TableName != "agent_assignments" || pg.ConstraintName == "" {
		t.Fatal("deferred assignment FK did not reject missing same-domain facts")
	}
}

func assertSchemaPGError(t *testing.T, label string, err error, code, constraint string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != code || pg.ConstraintName != constraint {
		// Never print SQL, parameters, raw dependency errors or metadata.
		got := "non-pg"
		if pg != nil {
			got = fmt.Sprintf("%s/%s", pg.Code, pg.ConstraintName)
		}
		t.Fatalf("schema probe %s: want %s/%s, got %s", label, code, constraint, got)
	}
}
