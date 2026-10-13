//go:build integration

package app

import (
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type skillAssignmentHTTPPage struct {
	ProjectID    i.ProjectID               `json:"project_id"`
	AgentID      i.AgentID                 `json:"agent_id"`
	AgentVersion f.Version                 `json:"agent_version"`
	Sequence     f.Version                 `json:"assignment_sequence"`
	Items        []sc.AgentSkillAssignment `json:"items"`
	NextCursor   string                    `json:"next_cursor"`
}

func skillAssignmentAppCall(t *testing.T, v *workAppFixture, method, path string, body any, key string, status int, out any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = workAppJSON(t, body)
	}
	r, err := v.request(v.ctx, method, path, raw, key)
	workAppCheck(t, "skill-assignment-http-transport", err)
	if r.status != status {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(r.body, &problem)
		t.Fatalf("skill assignment HTTP status=%d expected=%d code=%s", r.status, status, problem.Code.Safe())
	}
	if out != nil {
		workAppCheck(t, "skill-assignment-http-decode", json.Unmarshal(r.body, out))
	}
}

func skillAssignmentAppAgent(t *testing.T, v *workAppFixture) i.AgentID {
	t.Helper()
	base := "/api/v1/projects/" + v.a.String()
	var receipt mc.CommandReceipt
	skillAssignmentAppCall(t, v, http.MethodPost, base+"/model-providers", map[string]any{"input": map[string]any{"name": "assignment-provider", "protocol": mc.OpenAIChat, "base_url": "https://assignment.invalid/v1", "enabled": true, "credential_ref": nil, "options": map[string]any{}}}, guardID[struct{}](t).String(), 200, &receipt)
	provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "assignment-provider", err)
	skillAssignmentAppCall(t, v, http.MethodPost, base+"/models", map[string]any{"provider_id": provider, "input": map[string]any{"name": "assignment-model", "provider_model_id": "assignment-model", "type": mc.ChatModel, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": nil}}}, guardID[struct{}](t).String(), 200, &receipt)
	model, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "assignment-model", err)
	agent := guardID[i.Agent](t)
	enabled, disabled := true, false
	request, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: agent, Name: "assignment-agent", Description: "Explicit Skill configuration", Instructions: "No model calls", ModelRef: model, ApprovalPolicy: ac.ApprovalAuto, ApprovalModelRef: &model, AllowedToolIDs: []i.ToolID{}, AllowedMountIDs: []i.MountID{}, AllowedSecretVariableIDs: []i.ProjectVariableID{}, AddSkillsEnabled: &enabled, InstallSkillEnabled: &disabled})
	workAppCheck(t, "assignment-agent-intent", err)
	created, err := v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, request)
	workAppCheck(t, "assignment-original-agent-create", err)
	if created.Fields().Agent.Fields().Core.Version != 1 {
		t.Fatal("initial Agent version differs")
	}
	return agent
}

func skillAssignmentAppSelection(t *testing.T, v *workAppFixture, agent i.AgentID) string {
	t.Helper()
	var selection string
	workAppCheck(t, "assignment-unchanged-selection", v.admin.QueryRow(v.ctx, `SELECT jsonb_build_object(
 'agent',to_jsonb(a)-'version'-'updated_at',
 'models',(SELECT jsonb_agg(to_jsonb(r)-'owner_version' ORDER BY r.role) FROM agenteam_model.references r WHERE r.owner_kind='agent' AND r.owner_id=a.id),
 'initial',(SELECT to_jsonb(h) FROM agenteam_skill.agent_assignment_heads h WHERE h.project_id=a.project_id AND h.agent_id=a.id))::text
 FROM agenteam_agent.agents a WHERE a.id::text=$1`, agent.String()).Scan(&selection))
	return selection
}

func skillAssignmentAppAtomic(t *testing.T, v *workAppFixture, r sc.AssignmentMutation) {
	t.Helper()
	var valid bool
	workAppCheck(t, "assignment-original-same-transaction", v.admin.QueryRow(v.ctx, `SELECT
 c.state='completed' AND convert_from(c.receipt,'UTF8')::jsonb=$2::jsonb AND a.version=$3 AND ch.agent_version=a.version
 AND c.xmin=ch.xmin AND c.xmin=h.xmin AND c.xmin=a.xmin AND c.xmin=e.xmin AND c.xmin=au.xmin
 AND e.producer='skill' AND e.aggregate_type='skill.assignment' AND e.aggregate_id=a.id AND e.aggregate_version=a.version
 AND au.producer='skill' AND au.user_id::text=$4 AND au.resource_id=a.id AND au.action=c.command_name
 AND EXISTS(SELECT 1 FROM agenteam_tool.agent_configurations z WHERE z.agent_id::text=a.id::text AND z.config_version=a.version AND z.xmin=c.xmin)
 AND EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads z WHERE z.agent_id=a.id AND z.owner_version=a.version AND z.xmin=c.xmin)
 AND (SELECT count(*)=2 AND bool_and(z.owner_version=a.version AND z.xmin=c.xmin) FROM agenteam_model.references z WHERE z.owner_kind='agent' AND z.owner_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_references z WHERE z.owner_kind='agent' AND z.owner_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_tool.agent_references z WHERE z.agent_id::text=a.id::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_refs z WHERE z.agent_id=a.id)
 FROM agenteam_skill.assignment_commands c
 JOIN agenteam_skill.assignment_changes ch ON ch.id=c.id
 JOIN agenteam_skill.assignment_current h ON h.change_id=ch.id
 JOIN agenteam_agent.agents a ON a.id=c.agent_id
 JOIN agenteam_outbox.events e ON e.id=c.id
 JOIN agenteam_audit.audit_records au ON au.producer='skill' AND au.metadata->>'change_id'=c.id::text
	 WHERE c.id::text=$1`, r.ChangeID.String(), string(workAppJSON(t, r)), int64(r.AgentVersion), v.actor.Details().UserID).Scan(&valid))
	if !valid {
		t.Fatal("HTTP command was not committed with original Agent/references/Audit/Outbox")
	}
	if r.Runtime != nil {
		workAppCheck(t, "assignment-actual-no-active-ingress", v.admin.QueryRow(v.ctx, `SELECT i.outcome='no_active_execution' AND i.execution_id IS NULL AND i.xmin=c.xmin
 AND convert_from(i.change_data,'UTF8')::jsonb=convert_from(ch.runtime_data,'UTF8')::jsonb
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.assignment_pending_refs p WHERE p.change_id=c.id)
 FROM agenteam_skill.assignment_commands c JOIN agenteam_skill.assignment_changes ch ON ch.id=c.id
 JOIN agenteam_execution.skill_assignment_ingress i ON i.change_id::text=c.id::text WHERE c.id::text=$1`, r.ChangeID.String()).Scan(&valid))
		if !valid || r.Runtime.Queued || r.Runtime.ExecutionID != nil {
			t.Fatal("disabled Runtime synthesized or changed ingress fact")
		}
	}
}

// The default App, true Owner login, Project/Skill initialization, Agent service
// and HTTP adapters are used unchanged. Runtime stays disabled; no external
// endpoint is contacted and no execution/model/tool operation is created.
func TestSkillAssignmentHTTPDefaultApp(t *testing.T) {
	v := newWorkAppFixture(t)
	agent := skillAssignmentAppAgent(t, v)
	base := "/api/v1/projects/" + v.a.String()
	path := base + "/agents/" + agent.String() + "/skills"
	var initial skillAssignmentHTTPPage
	skillAssignmentAppCall(t, v, http.MethodGet, path+"?limit=1", nil, "", 200, &initial)
	if initial.AgentVersion != 1 || initial.Sequence != 1 || len(initial.Items) != 1 || !initial.Items[0].Enabled || initial.NextCursor != "" {
		t.Fatal("default Add Skills grant missing")
	}
	builtin := initial.Items[0]
	selection := skillAssignmentAppSelection(t, v, agent)
	ordinary := guardID[pc.Skill](t)
	install := map[string]any{"request": map[string]any{"skill_id": ordinary, "mode": "create", "source": map[string]any{"kind": "text_files", "files": []map[string]string{{"path": "SKILL.md", "utf8_text": "---\nname: assignment-http\ndescription: Explicit HTTP assignment fixture\n---\nUse existing permissions only.\n"}}}}}
	skillAssignmentAppCall(t, v, http.MethodPost, base+"/skills", install, guardID[struct{}](t).String(), 200, nil)
	assign := func(version f.Version, skill sc.SkillID, enabled bool) map[string]any {
		return map[string]any{"expected_version": version, "request": map[string]any{"skill_id": skill, "enabled": enabled}}
	}
	remove := func(version f.Version, id sc.AssignmentID) map[string]any {
		return map[string]any{"expected_version": version, "request": map[string]any{"assignment_id": id}}
	}
	originalBody, originalKey := assign(1, ordinary, true), guardID[struct{}](t).String()
	var added sc.AssignmentMutation
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/assign", originalBody, originalKey, 200, &added)
	if added.Validate() != nil || !added.Changed || added.AgentVersion != 2 || added.AssignmentSequence != 2 || added.Assignment == nil || added.Assignment.SkillID != ordinary || added.Runtime == nil {
		t.Fatal("explicit ordinary assignment failed")
	}
	skillAssignmentAppAtomic(t, v, added)
	var first, last skillAssignmentHTTPPage
	skillAssignmentAppCall(t, v, http.MethodGet, path+"?limit=1", nil, "", 200, &first)
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatal("two assignments were silently truncated")
	}
	skillAssignmentAppCall(t, v, http.MethodGet, path+"?limit=1&cursor="+url.QueryEscape(first.NextCursor), nil, "", 200, &last)
	if len(last.Items) != 1 || last.NextCursor != "" || last.Items[0].SkillID == first.Items[0].SkillID {
		t.Fatal("assignment continuation repeated or omitted item")
	}
	var disabled sc.AssignmentMutation
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/assign", assign(2, builtin.SkillID, false), guardID[struct{}](t).String(), 200, &disabled)
	if !disabled.Changed || disabled.Assignment == nil || disabled.Assignment.Enabled || disabled.Assignment.ID == builtin.ID || disabled.AgentVersion != 3 || disabled.Runtime != nil {
		t.Fatal("disabled Add Skills grant identity or routing changed")
	}
	skillAssignmentAppAtomic(t, v, disabled)
	var problem struct {
		Code f.Code `json:"code"`
	}
	skillAssignmentAppCall(t, v, http.MethodGet, path+"?limit=1&cursor="+url.QueryEscape(first.NextCursor), nil, "", 409, &problem)
	if problem.Code != f.CursorStale {
		t.Fatal("old assignment page was not invalidated")
	}
	var replay sc.AssignmentMutation
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/assign", originalBody, originalKey, 200, &replay)
	if !reflect.DeepEqual(replay, added) {
		t.Fatal("historical replay followed current configuration")
	}
	lookup := map[string]any{"command": "skill.assign", "expected_version": f.Version(1), "request": originalBody["request"]}
	var observed struct {
		State   string                 `json:"state"`
		Receipt *sc.AssignmentMutation `json:"receipt"`
	}
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/commands/lookup", lookup, originalKey, 200, &observed)
	if observed.State != "committed" || observed.Receipt == nil || !reflect.DeepEqual(*observed.Receipt, added) {
		t.Fatal("original lookup did not retain immutable ingress")
	}
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/assign", assign(1, ordinary, false), originalKey, 409, &problem)
	if problem.Code != f.IdempotencyKeyReused {
		t.Fatal("changed full intent reused original key")
	}
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/remove", remove(1, added.Assignment.ID), guardID[struct{}](t).String(), 409, &problem)
	if problem.Code != f.VersionConflict {
		t.Fatal("stale Agent version accepted")
	}
	foreignPath := "/api/v1/projects/" + v.b.String() + "/agents/" + agent.String() + "/skills"
	skillAssignmentAppCall(t, v, http.MethodGet, foreignPath, nil, "", 404, nil)
	skillAssignmentAppCall(t, v, http.MethodPost, foreignPath+"/commands/lookup", lookup, originalKey, 404, nil)
	var removed sc.AssignmentMutation
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/remove", remove(3, added.Assignment.ID), guardID[struct{}](t).String(), 200, &removed)
	if !removed.Changed || removed.AgentVersion != 4 || removed.Assignment != nil || removed.Runtime != nil {
		t.Fatal("removal altered scope or runtime")
	}
	skillAssignmentAppAtomic(t, v, removed)
	var oldIdentity sc.AssignmentMutation
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/remove", remove(4, builtin.ID), guardID[struct{}](t).String(), 200, &oldIdentity)
	if oldIdentity.Changed || oldIdentity.AgentVersion != 4 || oldIdentity.AssignmentSequence != 4 {
		t.Fatal("retired identity removed replacement disabled grant")
	}
	var enabled sc.AssignmentMutation
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/assign", assign(4, builtin.SkillID, true), guardID[struct{}](t).String(), 200, &enabled)
	if !enabled.Changed || enabled.Assignment == nil || !enabled.Assignment.Enabled || enabled.Assignment.ID == disabled.Assignment.ID || enabled.Runtime == nil {
		t.Fatal("re-enable resurrected old grant")
	}
	skillAssignmentAppAtomic(t, v, enabled)
	skillAssignmentAppCall(t, v, http.MethodPost, path+"/remove", remove(5, enabled.Assignment.ID), guardID[struct{}](t).String(), 200, &removed)
	skillAssignmentAppAtomic(t, v, removed)
	var empty skillAssignmentHTTPPage
	skillAssignmentAppCall(t, v, http.MethodGet, path, nil, "", 200, &empty)
	if empty.AgentVersion != 6 || empty.Sequence != 6 || empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatal("final current set not empty")
	}
	if skillAssignmentAppSelection(t, v, agent) != selection {
		t.Fatal("Skill commands altered original Agent selection or initial grant receipt")
	}
	var exact bool
	workAppCheck(t, "assignment-no-extra-side-effects", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*)=6 FROM agenteam_skill.assignment_commands WHERE agent_id::text=$1 AND state='completed')
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.assignment_commands WHERE agent_id::text=$1 AND state='planned')
 AND (SELECT count(*)=5 FROM agenteam_skill.assignment_changes WHERE agent_id::text=$1)
 AND (SELECT count(*)=2 AND bool_and(outcome='no_active_execution' AND execution_id IS NULL) FROM agenteam_execution.skill_assignment_ingress WHERE agent_id::text=$1)
 AND (SELECT count(*)=5 FROM agenteam_outbox.events WHERE aggregate_id::text=$1 AND producer='skill')
 AND (SELECT count(*)=5 FROM agenteam_audit.audit_records WHERE resource_id::text=$1 AND producer='skill')
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.assignment_pending_refs)
 AND (SELECT count(*)=2 FROM agenteam_skill.skills WHERE project_id::text=$2 AND serving=true)`, agent.String(), v.a.String()).Scan(&exact))
	if !exact {
		t.Fatal("replay/lookup/disabled configuration duplicated or deleted durable facts")
	}
	v.joined(t)
	t.Log("SKILL_ASSIGNMENT_DEFAULT_APP real_http=true default_graph=true no_active_observed=true assign_disable_remove=true historical_lookup=true exact_tx_refs_audit_outbox=true initialized_grant_preserved=true current_cursor=true execution_model_tool_zero=true")
}
