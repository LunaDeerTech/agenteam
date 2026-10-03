//go:build integration

package security_test

import (
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"strings"
	"testing"
)

func TestAuditPaginationBindingsFiltersAndRevocation(t *testing.T) {
	f := newAuditFixture(t)
	agentID, execution := newID[identity.Agent](t), newID[identity.Execution](t)
	agent, _ := identity.NewAgentRun(f.project, agentID, execution)
	tool, operation, approval, runner := newID[struct{}](t).String(), newID[struct{}](t).String(), newID[struct{}](t).String(), newID[struct{}](t).String()
	var target []ac.ID
	for i := range 9 {
		fields := f.entry(t, f.actor, f.scope).Fields()
		fields.Action = ac.AccessDeny
		fields.Outcome = ac.Denied
		fields.Metadata, _ = ac.DenialMetadata(ac.Model, ac.AddressForbidden, 1)
		if i%2 == 0 {
			fields.Actor = agent
		} else {
			fields.Resource, _ = ac.NewResource(ac.AgentResource, agentID.String())
		}
		fields.Associations = ac.Associations{ToolID: tool, OperationID: operation, ApprovalID: approval, RunnerID: runner}
		entry, err := ac.NewEntry(fields)
		if err != nil {
			t.Fatal(err)
		}
		r, result := appendAudit(t, f, entry, appendKey(t, ac.AccessProducer))
		if result.State() != foundation.Committed {
			t.Fatal(result.Fault())
		}
		target = append(target, r.AuditID)
	}
	// The test fixture fixes timestamps to exercise the UUID tie breaker.
	stamp, _ := foundation.ParseInstant("2026-10-03T12:00:00.000000Z")
	end, _ := foundation.ParseInstant("2026-10-03T13:00:00.000000Z")
	if _, err := f.store.Exec(auditContext(t), `UPDATE agenteam_audit.audit_records SET created_at=$1`, stamp.Time()); err != nil {
		t.Fatal(err)
	}
	filter := ac.Filter{From: &stamp, To: &end, Action: ac.AccessDeny, Outcome: ac.Denied, ToolID: tool, OperationID: operation, ApprovalID: approval, RunnerID: runner, AgentID: agentID.String()}
	page, err := f.service.List(auditContext(t), f.actor, f.scope, filter, foundation.PageRequest{Limit: 3})
	if err != nil || len(page.Items) != 3 || page.NextCursor == "" {
		t.Fatalf("first page: %v", err)
	}
	seen := map[ac.ID]bool{}
	last := "ffffffff-ffff-7fff-bfff-ffffffffffff"
	saved := page.NextCursor
	for {
		for _, record := range page.Items {
			if seen[record.AuditID] || record.AuditID.String() >= last {
				t.Fatal("duplicate or non-descending tuple")
			}
			seen[record.AuditID] = true
			last = record.AuditID.String()
		}
		if page.NextCursor == "" {
			break
		}
		page, err = f.service.List(auditContext(t), f.actor, f.scope, filter, foundation.PageRequest{Limit: 4, Cursor: page.NextCursor})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 9 {
		t.Fatal("agent actor OR resource, AND filters lost rows")
	}
	changed := filter
	changed.RunnerID = newID[struct{}](t).String()
	_, err = f.service.List(auditContext(t), f.actor, f.scope, changed, foundation.PageRequest{Limit: 3, Cursor: saved})
	requireCode(t, err, foundation.CursorInvalid)
	_, err = f.service.List(auditContext(t), f.actor, identity.SystemScope(), filter, foundation.PageRequest{Limit: 3, Cursor: saved})
	requireCode(t, err, foundation.CursorInvalid)
	changed = filter
	changed.From = nil
	changed.To = &stamp
	page, err = f.service.List(auditContext(t), f.actor, f.scope, changed, foundation.DefaultPageRequest())
	if err != nil || len(page.Items) != 0 {
		t.Fatal("time upper bound was not exclusive")
	}
	changed = filter
	changed.ExecutionID = execution.String()
	page, err = f.service.List(auditContext(t), f.actor, f.scope, changed, foundation.DefaultPageRequest())
	if err != nil || len(page.Items) != 5 {
		t.Fatal("execution filter lost actor attempts")
	}
	changed = filter
	changed.ActorKind = identity.Human
	changed.ActorID = f.actor.Details().UserID
	page, err = f.service.List(auditContext(t), f.actor, f.scope, changed, foundation.DefaultPageRequest())
	if err != nil || len(page.Items) != 4 {
		t.Fatal("human actor filter incorrect")
	}
	changed = filter
	changed.ResourceKind = ac.AgentResource
	changed.ResourceID = agentID.String()
	page, err = f.service.List(auditContext(t), f.actor, f.scope, changed, foundation.DefaultPageRequest())
	if err != nil || len(page.Items) != 4 {
		t.Fatal("resource filter incorrect")
	}
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.projects SET state='archived' WHERE id=$1`, f.project.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.Get(auditContext(t), f.actor, f.scope, target[0]); err != nil {
		t.Fatal("archive hid audit")
	}
	if _, err = f.store.Exec(auditContext(t), `UPDATE audit_fixture.sessions SET active=false`); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.List(auditContext(t), f.actor, f.scope, filter, foundation.PageRequest{Limit: 3, Cursor: saved})
	requireCode(t, err, foundation.SessionRevoked)
	_, err = f.service.List(auditContext(t), agent, f.scope, filter, foundation.DefaultPageRequest())
	requireCode(t, err, foundation.Forbidden)
}

func TestAuditCommonFilterIndexesOnPopulatedFixture(t *testing.T) {
	f := newAuditFixture(t)
	base := f.entry(t, f.actor, f.scope)
	r, result := appendAudit(t, f, base, appendKey(t, ac.SecretProducer))
	if result.State() != foundation.Committed {
		t.Fatal(result.Fault())
	}
	// Populate only the isolated fixture, with distinct append identities. IDs
	// remain UUIDv7. Spread each indexed dimension so selective plans matter.
	_, err := f.store.Exec(auditContext(t), `INSERT INTO agenteam_audit.audit_records(id,scope,project_id,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal,tool_id,execution_id,operation_id,approval_id,runner_id)
 SELECT ('01900000-0000-7000-8000-'||lpad(to_hex(g),12,'0'))::uuid,scope,project_id,actor_kind,('01900001-0000-7000-8000-'||lpad(to_hex(g),12,'0'))::uuid,session_id,CASE WHEN g=5000 THEN 'secret.update' ELSE action END,outcome,resource_kind,('01900002-0000-7000-8000-'||lpad(to_hex(g),12,'0'))::uuid,metadata,semantic_digest,producer,cause_ref,g,('01900003-0000-7000-8000-'||lpad(to_hex(g),12,'0'))::uuid,('01900003-0000-7000-8000-'||lpad(to_hex(g),12,'0'))::uuid,('01900003-0000-7000-8000-'||lpad(to_hex(g),12,'0'))::uuid,('01900003-0000-7000-8000-'||lpad(to_hex(g),12,'0'))::uuid,('01900003-0000-7000-8000-'||lpad(to_hex(g),12,'0'))::uuid
 FROM agenteam_audit.audit_records CROSS JOIN generate_series(1,6000) AS g WHERE id=$1`, r.AuditID.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Exec(auditContext(t), `ANALYZE agenteam_audit.audit_records`); err != nil {
		t.Fatal(err)
	}
	// Mirror the populated dimensions into System scope without any Project
	// association; both scope-specific partial-index families are exercised.
	_, err = f.store.Exec(auditContext(t), `INSERT INTO agenteam_audit.audit_records(id,scope,actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal,tool_id,runner_id)
 SELECT ('01900004-0000-7000-8000-'||lpad(to_hex(row_number() OVER()),12,'0'))::uuid,'system',actor_kind,user_id,session_id,action,outcome,resource_kind,resource_id,metadata,semantic_digest,producer,cause_ref,ordinal,tool_id,runner_id FROM agenteam_audit.audit_records WHERE scope='project'`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Exec(auditContext(t), `ANALYZE agenteam_audit.audit_records`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ where, index, value string }{{"action=$1", "audit_system_action", "secret.update"}, {"actor_kind='human' AND actor_id=$1", "audit_system_actor", "01900001-0000-7000-8000-000000001388"}, {"resource_kind='secret' AND resource_id=$1", "audit_system_resource", "01900002-0000-7000-8000-000000001388"}, {"tool_id=$1", "audit_system_tool", "01900003-0000-7000-8000-000000001388"}, {"runner_id=$1", "audit_system_runner", "01900003-0000-7000-8000-000000001388"}} {
		rows, err := f.store.Query(auditContext(t), `EXPLAIN SELECT id FROM agenteam_audit.audit_records WHERE scope='system' AND `+tc.where+` ORDER BY created_at DESC,id DESC LIMIT 51`, tc.value)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err = rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(&plan, line)
		}
		rows.Close()
		if rows.Err() != nil || !strings.Contains(plan.String(), tc.index) {
			t.Fatalf("selective System filter missing %s: %s", tc.index, plan.String())
		}
	}
	for _, tc := range []struct {
		where, index string
		value        any
	}{{"action=$2", "audit_project_action", "secret.update"}, {"actor_kind='human' AND actor_id=$2", "audit_project_actor", "01900001-0000-7000-8000-000000001388"}, {"resource_kind='secret' AND resource_id=$2", "audit_project_resource", "01900002-0000-7000-8000-000000001388"}, {"tool_id=$2", "audit_project_tool", "01900003-0000-7000-8000-000000001388"}, {"execution_id=$2", "audit_project_execution", "01900003-0000-7000-8000-000000001388"}, {"operation_id=$2", "audit_project_operation", "01900003-0000-7000-8000-000000001388"}, {"approval_id=$2", "audit_project_approval", "01900003-0000-7000-8000-000000001388"}, {"runner_id=$2", "audit_project_runner", "01900003-0000-7000-8000-000000001388"}} {
		rows, err := f.store.Query(auditContext(t), `EXPLAIN SELECT id FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1 AND `+tc.where+` ORDER BY created_at DESC,id DESC LIMIT 51`, f.project.String(), tc.value)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintln(&plan, line)
		}
		rows.Close()
		if rows.Err() != nil || !strings.Contains(plan.String(), tc.index) {
			t.Fatalf("selective filter plan missing %s: %s", tc.index, plan.String())
		}
	}
	// Keep an actual typed read in this populated fixture too.
	digest, err := audit.SemanticDigest(base)
	if err != nil || digest.Validate() != nil {
		t.Fatal("fixture semantic digest invalid")
	}
}
