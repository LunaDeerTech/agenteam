//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/LunaDeerTech/agenteam/db/migrations"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

func (v *variableHTTPFixture) snapshot(t *testing.T) string {
	t.Helper()
	var raw string
	err := v.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'variables',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_projectvariable.variables x),
 'generations',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY project_id),'[]'::jsonb) FROM agenteam_projectvariable.project_generations x),
 'history',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_projectvariable.history x),
 'completed',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_projectvariable.commands x WHERE state='completed'),
 'audit',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_audit.audit_records x WHERE producer='projectvariable'),
 'events',(SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events x WHERE producer='projectvariable'),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$1))::text`, v.ownerBrowser.actor.Details().SessionID).Scan(&raw)
	if err != nil {
		t.Fatal("snapshot SQL failed", err)
	}
	return raw
}
func (v *variableHTTPFixture) counts(t *testing.T, p vc.ProjectID) (generation, history, audit, events int64) {
	t.Helper()
	err := v.raw.QueryRow(ctxFor(t), `SELECT
 coalesce((SELECT query_generation FROM agenteam_projectvariable.project_generations WHERE project_id=$1),1),
 (SELECT count(*) FROM agenteam_projectvariable.history WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='projectvariable'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND producer='projectvariable')`, p.String()).Scan(&generation, &history, &audit, &events)
	if err != nil {
		t.Fatal("counts SQL", err)
	}
	return
}
func sameReceipt(t *testing.T, a, b vc.VariableMutation) {
	t.Helper()
	if string(jsonBytes(t, a)) != string(jsonBytes(t, b)) {
		t.Fatal("original receipt changed")
	}
}
func queryFor(t *testing.T, a identity.Actor, m f.CommandMeta, p vc.ProjectID, target vc.VariableID, n vc.CommandName, request any) vc.VariableCommandLookupRequest {
	t.Helper()
	d, e := vc.VariableCommandDigest(a, m, p, target, n, request)
	if e != nil {
		t.Fatal(e)
	}
	q, e := vc.NewVariableCommandLookupRequest(vc.VariableCommandLookupFields{ProjectID: p, Command: n, IdempotencyKey: m.IdempotencyKey, SemanticDigest: d})
	if e != nil {
		t.Fatal(e)
	}
	return q
}

func TestProjectVariablePersistence(t *testing.T) {
	v := newVariableHTTPFixture(t)
	a, p := v.ownerBrowser.actor, v.project.ID
	r := createInput(t, "Alpha", "private-value-canary")
	m := meta(t, "persistent-create", nil)
	created, e := v.service.CreateVariable(ctxFor(t), a, m, p, r)
	if e != nil {
		t.Fatal("create", e)
	}
	first := created.Fields().Variable.Fields()
	if first.Version != 1 || first.CreatedAt != first.UpdatedAt {
		t.Fatal("create version/time")
	}
	if g, h, au, ev := v.counts(t, p); g != 2 || h != 1 || au != 1 || ev != 1 {
		t.Fatalf("create facts %d %d %d %d", g, h, au, ev)
	}
	// The three business records share the frozen DB instant; Audit/Activity do not promise that precision.
	var coherent bool
	e = v.raw.QueryRow(ctxFor(t), `SELECT h.occurred_at=v.updated_at AND e.occurred_at=h.occurred_at AND h.version=v.version AND e.aggregate_version=v.version
 FROM agenteam_projectvariable.variables v JOIN agenteam_projectvariable.history h ON h.variable_id=v.id
 JOIN agenteam_outbox.events e ON e.id=h.event_id WHERE v.id=$1`, first.ID.String()).Scan(&coherent)
	if e != nil || !coherent {
		t.Fatal("frozen business time/version mismatch", e)
	}
	before := v.snapshot(t)
	replayed, e := v.service.CreateVariable(ctxFor(t), a, m, p, r)
	if e != nil {
		t.Fatal(e)
	}
	sameReceipt(t, created, replayed)
	if v.snapshot(t) != before {
		t.Fatal("create replay changed facts")
	}
	name, empty := "Beta", ""
	update := updateInput(t, vc.VariableUpdateFields{Name: &name, Value: &empty, Description: &empty})
	um := meta(t, "persistent-update", &first.Version)
	updated, e := v.service.UpdateVariable(ctxFor(t), a, um, p, first.ID, update)
	if e != nil {
		t.Fatal(e)
	}
	second := updated.Fields().Variable.Fields()
	if second.Version != 2 || second.Name != name || second.Value != "" || second.Description != "" || second.CreatedAt != first.CreatedAt {
		t.Fatal("patch/presence")
	}
	before = v.snapshot(t)
	nm := meta(t, "persistent-noop", &second.Version)
	noop, e := v.service.UpdateVariable(ctxFor(t), a, nm, p, first.ID, update)
	if e != nil || noop.Fields().Changed || noop.Fields().EventID != nil || noop.Fields().AuditID != nil {
		t.Fatal("no-op", e)
	}
	if g, h, au, ev := v.counts(t, p); g != 3 || h != 2 || au != 2 || ev != 2 {
		t.Fatal("no-op emitted business facts")
	}
	// Excluding only its completed command, every no-op persisted business value is unchanged.
	var receiptCount int
	e = v.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_projectvariable.commands WHERE idempotency_key=$1 AND state='completed' AND plan IS NULL AND event_id IS NULL`, string(nm.IdempotencyKey)).Scan(&receiptCount)
	if e != nil || receiptCount != 1 {
		t.Fatal("no-op command missing", e)
	}
	var beforeFacts, afterFacts map[string]json.RawMessage
	if json.Unmarshal([]byte(before), &beforeFacts) != nil || json.Unmarshal([]byte(v.snapshot(t)), &afterFacts) != nil {
		t.Fatal("snapshot parse")
	}
	delete(beforeFacts, "completed")
	delete(afterFacts, "completed")
	if string(jsonBytes(t, beforeFacts)) != string(jsonBytes(t, afterFacts)) {
		t.Fatal("no-op changed business facts or activity")
	}
	_, e = v.service.UpdateVariable(ctxFor(t), a, meta(t, "stale-noop", &first.Version), p, first.ID, update)
	requireCode(t, e, f.VersionConflict)
	dm := meta(t, "persistent-delete", &second.Version)
	deleted, e := v.service.DeleteVariable(ctxFor(t), a, dm, p, first.ID)
	if e != nil || deleted.Fields().Deleted.Version != 3 {
		t.Fatal("delete", e)
	}
	_, e = v.service.GetVariable(ctxFor(t), a, p, first.ID)
	requireCode(t, e, f.NotFound)
	_, e = v.service.DeleteVariable(ctxFor(t), a, meta(t, "new-delete-key", &second.Version), p, first.ID)
	requireCode(t, e, f.NotFound)
	before = v.snapshot(t)
	again, e := v.service.DeleteVariable(ctxFor(t), a, dm, p, first.ID)
	if e != nil {
		t.Fatal(e)
	}
	sameReceipt(t, deleted, again)
	if before != v.snapshot(t) {
		t.Fatal("delete replay changed facts")
	}
	var clean bool
	e = v.raw.QueryRow(ctxFor(t), `SELECT deleted_at=updated_at AND value='' AND description='' FROM agenteam_projectvariable.variables WHERE id=$1`, first.ID.String()).Scan(&clean)
	if e != nil || !clean {
		t.Fatal("tombstone", e)
	}
	for _, old := range []struct {
		m    f.CommandMeta
		n    vc.CommandName
		r    any
		want vc.VariableMutation
	}{{m, vc.CreateCommand, r, created}, {um, vc.UpdateCommand, update, updated}, {dm, vc.DeleteCommand, nil, deleted}} {
		got, e := v.service.LookupVariableCommand(ctxFor(t), a, queryFor(t, a, old.m, p, first.ID, old.n, old.r))
		if e != nil || got.Status() != vc.LookupCommitted {
			t.Fatal("historical lookup", e)
		}
		sameReceipt(t, *got.Receipt(), old.want)
	}
	replacement := v.createVariable(t, "Beta", "replacement")
	if replacement.Fields().ID == first.ID {
		t.Fatal("ID reused")
	}
	_, e = v.service.CreateVariable(ctxFor(t), a, meta(t, "resurrect", nil), p, r)
	requireCode(t, e, f.ResourceBusy)
	if g, h, au, ev := v.counts(t, p); g != 5 || h != 4 || au != 4 || ev != 4 {
		t.Fatalf("final facts %d %d %d %d", g, h, au, ev)
	}
	// Prove audit/event payloads omit all user material, even though history receipts preserve it.
	var safe string
	e = v.raw.QueryRow(ctxFor(t), `SELECT (SELECT coalesce(jsonb_agg(metadata),'[]'::jsonb) FROM agenteam_audit.audit_records WHERE producer='projectvariable')::text || (SELECT coalesce(jsonb_agg(convert_from(payload,'UTF8')::jsonb),'[]'::jsonb) FROM agenteam_outbox.events WHERE producer='projectvariable')::text`).Scan(&safe)
	if e != nil {
		t.Fatal(e)
	}
	for _, canary := range []string{"private-value-canary", "private description", "Alpha", "Beta"} {
		if strings.Contains(safe, canary) || strings.Contains(v.logs.text(), canary) {
			t.Fatal("user material in safety output")
		}
	}
}

func TestProjectVariablePaginationAndLimits(t *testing.T) {
	v := newVariableHTTPFixture(t)
	a, p := v.ownerBrowser.actor, v.project.ID
	var names []string
	for i := 102; i >= 0; i-- {
		name := fmt.Sprintf("N%03d", i)
		names = append(names, name)
		v.createVariable(t, name, "value-not-in-summary")
	}
	slices.Sort(names)
	page, e := v.service.ListVariables(ctxFor(t), a, p, f.PageRequest{Limit: 50})
	if e != nil || len(page.Items) != 50 || page.NextCursor == "" {
		t.Fatal("first page", e)
	}
	cursor := page.NextCursor
	seen := []string{}
	for _, x := range page.Items {
		seen = append(seen, x.Fields().Name)
	}
	// limit is deliberately not a cursor binding; a real same-user new Session may continue.
	newer := v.login(t, v.ownerBrowser.email)
	for cursor != "" {
		page, e = v.service.ListVariables(ctxFor(t), newer.actor, p, f.PageRequest{Limit: 17, Cursor: cursor})
		if e != nil {
			t.Fatal(e)
		}
		for _, x := range page.Items {
			seen = append(seen, x.Fields().Name)
		}
		cursor = page.NextCursor
	}
	if !slices.Equal(seen, names) {
		t.Fatal("C-ordered page coverage differs")
	}
	first, e := v.service.ListVariables(ctxFor(t), a, p, f.PageRequest{Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	_, e = v.service.ListVariables(ctxFor(t), a, p, f.PageRequest{Limit: 10, Cursor: first.NextCursor + "x"})
	requireCode(t, e, f.CursorInvalid)
	other, _, _ := v.createProject(t, a, "page-other")
	_, e = v.service.ListVariables(ctxFor(t), a, other.ID, f.PageRequest{Limit: 10, Cursor: first.NextCursor})
	requireCode(t, e, f.CursorInvalid)
	_, e = v.service.ListVariables(ctxFor(t), v.otherBrowser.actor, p, f.PageRequest{Limit: 10, Cursor: first.NextCursor})
	requireCode(t, e, f.NotFound)
	target, e := v.service.GetVariable(ctxFor(t), a, p, first.Items[0].Fields().ID)
	if e != nil {
		t.Fatal(e)
	}
	fields := target.Fields()
	unchanged := fields.Value
	no := updateInput(t, vc.VariableUpdateFields{Value: &unchanged})
	m := meta(t, "page-noop", &fields.Version)
	receipt, e := v.service.UpdateVariable(ctxFor(t), a, m, p, fields.ID, no)
	if e != nil || receipt.Fields().Changed {
		t.Fatal(e)
	}
	if _, e = v.service.ListVariables(ctxFor(t), a, p, f.PageRequest{Limit: 1, Cursor: first.NextCursor}); e != nil {
		t.Fatal("no-op invalidated page", e)
	}
	if _, e = v.service.UpdateVariable(ctxFor(t), a, m, p, fields.ID, no); e != nil {
		t.Fatal(e)
	}
	next := "different"
	if _, e = v.service.UpdateVariable(ctxFor(t), a, meta(t, "page-change", &fields.Version), p, fields.ID, updateInput(t, vc.VariableUpdateFields{Value: &next})); e != nil {
		t.Fatal(e)
	}
	_, e = v.service.ListVariables(ctxFor(t), a, p, f.PageRequest{Limit: 1, Cursor: first.NextCursor})
	requireCode(t, e, f.CursorStale)
	// Explicit capacity/version fixtures are boundary inputs, not producer/history evidence.
	t.Log("capacity and max-version rows below are test-owned SQL boundary facts; normal page/order rows above use real commands")
	_, e = v.raw.Exec(ctxFor(t), `INSERT INTO agenteam_projectvariable.variables(id,project_id,type,name,description,value,version,created_at,updated_at)
 SELECT ('01900000-0000-7000-8000-'||lpad(to_hex(n),12,'0'))::uuid,$1,'variable','Capacity_'||n,'','',1,clock_timestamp(),clock_timestamp() FROM generate_series(1,3993)n`, p.String())
	if e != nil {
		t.Fatal(e)
	}
	_, e = v.service.CreateVariable(ctxFor(t), a, meta(t, "capacity-reject", nil), p, createInput(t, "Overflow", ""))
	requireCode(t, e, f.ResourceBusy)
	var problem *f.Fault
	if !errors.As(e, &problem) || !slices.Contains(problem.FieldErrors, f.FieldError{Path: "/variable_id", Code: "PROJECT_VARIABLE_LIMIT"}) {
		t.Fatal("capacity reason missing")
	}
	version := f.Version(9223372036854775807)
	maxID := id[identity.ProjectVariable](t)
	_, e = v.raw.Exec(ctxFor(t), `INSERT INTO agenteam_projectvariable.variables(id,project_id,type,name,description,value,version,created_at,updated_at) VALUES($1,$2,'variable','Max','','',9223372036854775807,clock_timestamp(),clock_timestamp())`, maxID.String(), other.ID.String())
	if e != nil {
		t.Fatal(e)
	}
	empty := ""
	maxNo := updateInput(t, vc.VariableUpdateFields{Value: &empty})
	maxMeta := meta(t, "max-noop", &version)
	maxReceipt, e := v.service.UpdateVariable(ctxFor(t), a, maxMeta, other.ID, maxID, maxNo)
	if e != nil || maxReceipt.Fields().Changed {
		t.Fatal("max no-op", e)
	}
	_, e = v.service.UpdateVariable(ctxFor(t), a, meta(t, "max-change", &version), other.ID, maxID, updateInput(t, vc.VariableUpdateFields{Value: &next}))
	requireCode(t, e, f.ResourceBusy)
	_, e = v.service.DeleteVariable(ctxFor(t), a, meta(t, "max-delete", &version), other.ID, maxID)
	requireCode(t, e, f.ResourceBusy)
}

func TestProjectVariableMigration(t *testing.T) {
	t.Run("upgrade-repeat-and-old-rows", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00023"))
		raw := openStore(t, db.Config(t, nil))
		// Setup uses only main's Account/Project storage before 00024 is installed.
		v := assembleVariableHTTPFixture(t, db, raw, &hookStore{fixtureStore: raw})
		var before string
		if e := raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object('project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a))::text`, v.project.ID.String()).Scan(&before); e != nil {
			t.Fatal(e)
		}
		migrate(t, db)
		migrate(t, db)
		var after string
		if e := raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object('project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1),'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a))::text`, v.project.ID.String()).Scan(&after); e != nil {
			t.Fatal(e)
		}
		if before != after {
			t.Fatal("upgrade rewrote main Project/Audit facts")
		}
		created := v.createVariable(t, "Upgraded", "available")
		version := created.Fields().Version
		value := "updated"
		updated, e := v.service.UpdateVariable(ctxFor(t), v.ownerBrowser.actor, meta(t, "upgrade-update", &version), v.project.ID, created.Fields().ID, updateInput(t, vc.VariableUpdateFields{Value: &value}))
		if e != nil {
			t.Fatal(e)
		}
		version = updated.Fields().Variable.Fields().Version
		if _, e = v.service.DeleteVariable(ctxFor(t), v.ownerBrowser.actor, meta(t, "upgrade-delete", &version), v.project.ID, created.Fields().ID); e != nil {
			t.Fatal(e)
		}
		description := "old Project producer still valid"
		if _, e = v.projects.UpdateProject(ctxFor(t), v.ownerBrowser.actor, meta(t, "old-project-update", &v.project.Version), v.project.ID, pc.UpdateProjectRequest{Description: &description}); e != nil {
			t.Fatal("old Project audit rejected", e)
		}
		var auditID string
		if e = raw.QueryRow(ctxFor(t), `SELECT id::text FROM agenteam_audit.audit_records WHERE producer='projectvariable' AND action='project.variable.create'`).Scan(&auditID); e != nil {
			t.Fatal(e)
		}
		// Test-owned negative INSERTs clone an actual legal record and change one
		// closed property. They must fail at a CHECK, not a duplicate key.
		for _, bad := range []struct{ name, producer, action, resource, metadata string }{
			{"producer", "project", "project.variable.create", "project_variable", ""},
			{"resource", "projectvariable", "project.variable.create", "project", ""},
			{"side-action", "projectvariable", "project.variable.fake", "project_variable", ""},
			{"old-action", "projectvariable", "project.update", "project_variable", ""},
			{"unknown-metadata", "projectvariable", "project.variable.create", "project_variable", `{"unexpected":true}`},
		} {
			t.Run(bad.name, func(t *testing.T) {
				_, err := raw.Exec(ctxFor(t), `INSERT INTO agenteam_audit.audit_records(id,scope,project_id,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal)
          SELECT $1::uuid,scope,project_id,actor_kind,user_id,session_id,$2,outcome,$3,resource_id,
           CASE WHEN $4::text='' THEN metadata ELSE metadata||$4::jsonb END,semantic_digest,$5,$6,ordinal
          FROM agenteam_audit.audit_records WHERE id=$7::uuid`, id[struct{}](t).String(), bad.action, bad.resource, bad.metadata, bad.producer, "sha256:"+strings.Repeat("f", 64), auditID)
				var pg *pgconn.PgError
				if !errors.As(err, &pg) || pg.Code != "23514" {
					t.Fatal("invalid Audit tuple did not fail CHECK")
				}
			})
		}
		for _, sql := range []string{
			`UPDATE agenteam_projectvariable.variables SET value='resurrect',version=version+1 WHERE id=$1`,
			`DELETE FROM agenteam_projectvariable.variables WHERE id=$1`,
		} {
			if _, e = raw.Exec(ctxFor(t), sql, created.Fields().ID.String()); e == nil {
				t.Fatal("tombstone/history protection bypassed")
			}
		}

	})
	t.Run("failure-rolls-back-whole-migration", func(t *testing.T) {
		db := pgfixture.NewDatabase(t)
		migrate(t, db, migrationPrefix(t, "00023"))
		files := migrationFiles(t, "00024")
		raw, e := fs.ReadFile(migrations.SQL, "00024_project_variables.sql")
		if e != nil {
			t.Fatal(e)
		}
		files["00024_project_variables.sql"] = &fstest.MapFile{Data: append(raw, []byte("\nSELECT 1/0;\n")...)}
		source, e := postgres.NewSource(files, nil)
		if e != nil {
			t.Fatal(e)
		}
		m, e := postgres.NewMigrator(db.Config(t, nil), source)
		if e != nil {
			t.Fatal(e)
		}
		if r := m.Migrate(ctxFor(t)); r.Migrated {
			t.Fatal("injected migration accepted")
		}
		conn := db.Connect(t)
		var absent bool
		if e = conn.QueryRow(ctxFor(t), `SELECT to_regnamespace('agenteam_projectvariable') IS NULL`).Scan(&absent); e != nil || !absent {
			t.Fatal("partial schema survived", e)
		}
		migrate(t, db)
	})
}

// AFTER ROW failure with a nontransactional sequence proves the real SQL
// boundary was reached, while all business facts must roll back.
func TestProjectVariableAtomicity(t *testing.T) {
	v := newVariableHTTPFixture(t)
	a, p := v.ownerBrowser.actor, v.project.ID
	_, e := v.raw.Exec(ctxFor(t), `CREATE SEQUENCE variable_fixture.fault_hits;
 CREATE FUNCTION variable_fixture.fail_after_row() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF to_jsonb(NEW)->>TG_ARGV[0]=TG_ARGV[1] AND (TG_ARGV[2]='any' OR to_jsonb(NEW)->>'state'=TG_ARGV[2]) THEN
 PERFORM nextval('variable_fixture.fault_hits');RAISE EXCEPTION USING ERRCODE='P0001',MESSAGE='private-variable-fault-canary';END IF;RETURN NEW;END $$`)
	if e != nil {
		t.Fatal(e)
	}
	for _, command := range []vc.CommandName{vc.CreateCommand, vc.UpdateCommand, vc.DeleteCommand} {
		for _, point := range []struct{ name, table, operation, field string }{{"variable", "agenteam_projectvariable.variables", "INSERT OR UPDATE", "id"}, {"generation", "agenteam_projectvariable.project_generations", "INSERT OR UPDATE", "project_id"}, {"history", "agenteam_projectvariable.history", "INSERT", "variable_id"}, {"audit", "agenteam_audit.audit_records", "INSERT", "resource_id"}, {"outbox", "agenteam_outbox.events", "INSERT", "aggregate_id"}, {"command", "agenteam_projectvariable.commands", "UPDATE", "idempotency_key"}, {"activity", "agenteam_account.sessions", "UPDATE", "id"}} {
			t.Run(string(command)+"/"+point.name, func(t *testing.T) {
				target := createInput(t, "Atomic_"+id[struct{}](t).String()[24:], "old")
				version := f.Version(1)
				if command != vc.CreateCommand {
					out, e := v.service.CreateVariable(ctxFor(t), a, meta(t, id[struct{}](t).String(), nil), p, target)
					if e != nil {
						t.Fatal(e)
					}
					version = out.Fields().Variable.Fields().Version
				}
				// Account's existing 60s activity throttle is intentionally made due.
				if _, e := v.raw.Exec(ctxFor(t), `UPDATE agenteam_account.sessions SET last_activity_at=clock_timestamp()-interval '2 minutes' WHERE id=$1`, a.Details().SessionID); e != nil {
					t.Fatal(e)
				}
				key := id[struct{}](t).String()
				value := target.Fields().ID.String()
				state := "any"
				switch point.name {
				case "generation":
					value = p.String()
				case "command":
					value = key
					state = "completed"
				case "activity":
					value = a.Details().SessionID
				}
				if _, e := v.raw.Exec(ctxFor(t), `ALTER SEQUENCE variable_fixture.fault_hits RESTART WITH 1`); e != nil {
					t.Fatal(e)
				}
				sql := fmt.Sprintf("CREATE TRIGGER variable_test_failure AFTER %s ON %s FOR EACH ROW EXECUTE FUNCTION variable_fixture.fail_after_row('%s','%s','%s')", point.operation, point.table, point.field, value, state)
				if _, e := v.raw.Exec(ctxFor(t), sql); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() {
					if _, e := v.raw.Exec(ctxFor(t), "DROP TRIGGER IF EXISTS variable_test_failure ON "+point.table); e != nil {
						t.Error(e)
					}
				})
				before := v.snapshot(t)
				var err error
				switch command {
				case vc.CreateCommand:
					_, err = v.service.CreateVariable(ctxFor(t), a, meta(t, key, nil), p, target)
				case vc.UpdateCommand:
					newValue := "new"
					_, err = v.service.UpdateVariable(ctxFor(t), a, meta(t, key, &version), p, target.Fields().ID, updateInput(t, vc.VariableUpdateFields{Value: &newValue}))
				case vc.DeleteCommand:
					_, err = v.service.DeleteVariable(ctxFor(t), a, meta(t, key, &version), p, target.Fields().ID)
				}
				if err == nil {
					t.Fatal("injected SQL failure succeeded")
				}
				if strings.Contains(fmt.Sprintf("%+v", err), "private-variable-fault-canary") {
					t.Fatal("SQL detail escaped")
				}
				var hit bool
				if e := v.raw.QueryRow(ctxFor(t), `SELECT is_called FROM variable_fixture.fault_hits`).Scan(&hit); e != nil || !hit {
					t.Fatal("real SQL boundary not reached", e)
				}
				if before != v.snapshot(t) {
					t.Fatal("failed final Tx changed business facts")
				}
			})
		}
	}
	t.Run("correct-postimage-missing-history-producer", func(t *testing.T) {
		drop := &missingHistoryAppender{Appender: v.events, v: v}
		s := v.newService(t, drop, v.accounts)
		before := v.snapshot(t)
		_, e := s.CreateVariable(ctxFor(t), a, meta(t, "missing-history", nil), p, createInput(t, "MissingHistory", "canary"))
		if e == nil || !drop.seen {
			t.Fatal("producer did not reject missing history", e)
		}
		if before != v.snapshot(t) {
			t.Fatal("missing history failure escaped rollback")
		}
	})
}

type missingHistoryAppender struct {
	oc.Appender
	v    *variableHTTPFixture
	seen bool
}

func (a *missingHistoryAppender) AppendEventInTx(ctx context.Context, tx f.Tx, actor identity.Actor, ev event.Event, plan oc.AppendPlan) (oc.AppendReceipt, error) {
	if e := a.v.authority.ValidateAppendInTx(ctx, tx, actor, ev.Summary(), plan.Details().Producer, oc.NewFact); e != nil {
		return oc.AppendReceipt{}, e
	}
	payload, e := a.v.variableEvents.Decode(ev)
	if e != nil {
		return oc.AppendReceipt{}, e
	}
	x, e := a.v.tracked.InTx(tx)
	if e != nil {
		return oc.AppendReceipt{}, e
	}
	tag, e := x.Exec(ctx, `DELETE FROM agenteam_projectvariable.history WHERE operation_id=$1`, payload.OperationID.String())
	if e != nil {
		return oc.AppendReceipt{}, e
	}
	if tag.RowsAffected() != 1 {
		return oc.AppendReceipt{}, errors.New("expected one real history")
	}
	a.seen = true
	return a.Appender.AppendEventInTx(ctx, tx, actor, ev, plan)
}
