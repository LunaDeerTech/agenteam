//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	auditc "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	eventc "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

// One runtime-off App graph serves the real Model/Agent APIs and browser.
// Project initialization is fixture preparation, never a product claim.
func TestAgentModelPolicyWeb(t *testing.T) {
	t.Run("switch-model-auto-and-clear-approval", func(t *testing.T) {
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("Agent Model Policy original App handoff incomplete")
			}
		})
		a := newAgentCreateFixture(t)
		original := a.p2.base
		process, err := a.guard.CurrentProcess()
		agentAppRequire(t, err)
		registration := agentAppRegistration(t, a)
		var username string
		agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id::text=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
		if original.afterOwners != nil {
			t.Fatal("original owner handoff already occupied")
		}
		original.afterOwners = func() {
			entered = true
			if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
				t.Fatal("setup owners not joined")
			}
			if _, err := a.guard.CurrentProcess(); err == nil {
				t.Fatal("setup guard still held")
			}
			var stopped bool
			agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
			if !stopped {
				t.Fatal("setup process not retired")
			}
			w := newAgentModelPolicyWeb(t, a)
			defer w.dispose(t)
			withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
				defer w.close(t)
				w.start(t, address)
				if agentAppRegistration(t, a) != registration {
					t.Fatal("App changed builtin registration")
				}
				w.prepareModel(t, ctx)
				w.create(t, ctx)
				w.material(t, username)
				w.browser(t, ctx)
				w.verify(t, ctx)
				w.close(t)
			})
			completed = true
		}
	})
}

type agentModelPolicyWeb struct {
	*agentEditWeb
	second     mc.ModelID
	modelsRead int
}

func newAgentModelPolicyWeb(t *testing.T, a *agentCreateFixture) *agentModelPolicyWeb {
	t.Helper()
	const prefix = "AGENTEAM_AGENT_MODEL_POLICY_WEB"
	dist, runtime := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence, inputHash := os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("frozen Model Policy assets/runtime/input required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen Model Policy index missing")
	}
	directory, err := os.MkdirTemp(runtime, "amp-")
	agentAppRequire(t, err)
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.RemoveAll(directory)
		t.Fatal(err)
	}
	b := &agentConfigurationWeb{a: a, mode: "switch-model-auto-and-clear-approval", origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}, draft: agentWebDraft{Name: "model-policy-agent", Description: "Original model policy description.", Instructions: "Keep these original instructions unchanged."}}
	return &agentModelPolicyWeb{agentEditWeb: &agentEditWeb{agentConfigurationWeb: b, target: id[i.Agent](t)}}
}
func modelPolicyOptionalID(v *mc.ModelID) any {
	if v == nil {
		return nil
	}
	return v.String()
}
func (w *agentModelPolicyWeb) patch(index int) map[string]any {
	if index == 0 {
		return map[string]any{"model_ref": w.second.String(), "reasoning_effort": nil, "inject_agents_md": false, "approval_policy": "auto", "approval_model_ref": w.a.modelID.String()}
	}
	return map[string]any{"approval_policy": "default", "approval_model_ref": nil}
}
func (w *agentModelPolicyWeb) prepareModel(t *testing.T, ctx context.Context) {
	t.Helper()
	old := w.request(t, w.a.p2.base.ownerBrowser, http.MethodGet, variableHTTPPath(w.a.p2.project.ID, "/models/"+w.a.modelID.String()), nil, "")
	var existing struct {
		ID       mc.ModelID    `json:"id"`
		Provider mc.ProviderID `json:"provider_id"`
	}
	if old.status != 200 || json.Unmarshal(old.body, &existing) != nil || existing.ID != w.a.modelID || existing.Provider.Validate() != nil {
		t.Fatal("formal current original Model missing")
	}
	capabilities := map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": nil}
	body := jsonBytes(t, map[string]any{"provider_id": existing.Provider, "input": map[string]any{"name": "agent-policy-second-chat", "provider_model_id": "agent-policy-second-chat", "type": "chat", "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": capabilities}})
	reply := w.request(t, w.a.p2.base.ownerBrowser, http.MethodPost, variableHTTPPath(w.a.p2.project.ID, "/models"), body, "agent-policy-create-second-model")
	var receipt mc.CommandReceipt
	if reply.status != 200 || json.Unmarshal(reply.body, &receipt) != nil || receipt.Validate() != nil || receipt.Kind != "model.create" || receipt.Version != 1 || receipt.AffectedReferences != 0 {
		t.Fatal("real App Owner Model create failed", reply.status)
	}
	var err error
	w.second, err = f.ParseID[mc.Model](receipt.ResourceID)
	agentAppRequire(t, err)
	if w.second == w.a.modelID {
		t.Fatal("second Model reused original ID")
	}
	read := w.request(t, w.a.p2.base.ownerBrowser, http.MethodGet, variableHTTPPath(w.a.p2.project.ID, "/available-chat-models?limit=50"), nil, "")
	if read.status != 200 {
		t.Fatal("real available Models unavailable")
	}
	agentAppRequire(t, w.modelPage(read.body))
}
func (w *agentModelPolicyWeb) modelPage(raw []byte) error {
	var page struct {
		Items []struct {
			ID       mc.ModelID    `json:"id"`
			Provider mc.ProviderID `json:"provider_id"`
			Version  f.Version     `json:"version"`
			Scope    struct {
				Kind    string `json:"kind"`
				Project string `json:"project_id"`
			} `json:"scope"`
			Capabilities mc.Capabilities `json:"capabilities"`
		} `json:"items"`
		Next *string `json:"next_cursor"`
	}
	if json.Unmarshal(raw, &page) != nil || page.Items == nil || len(page.Items) != 2 || page.Next != nil {
		return errors.New("actual two-Model page incomplete")
	}
	seen := map[mc.ModelID]bool{}
	for _, m := range page.Items {
		if (m.ID != w.a.modelID && m.ID != w.second) || seen[m.ID] || m.Provider.Validate() != nil || m.Scope.Kind != "project" || m.Scope.Project != w.a.p2.project.ID.String() || m.Version != 1 || m.Capabilities.Validate() != nil || !slices.Equal(m.Capabilities.InputModalities, []string{"text"}) || !slices.Equal(m.Capabilities.OutputModalities, []string{"text"}) || m.Capabilities.Reasoning || len(m.Capabilities.ReasoningEfforts) != 0 || m.Capabilities.ToolCalls || m.Capabilities.ParallelToolCalls {
			return errors.New("available Model is not original enabled text-chat profile")
		}
		seen[m.ID] = true
	}
	return nil
}
func (w *agentModelPolicyWeb) material(t *testing.T, username string) {
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.a.p2.project.ID, "agent_id": w.target, "route": "/" + username + "/" + w.a.p2.project.NormalizedName + "/settings/agents", "version": "1", "old_model_id": w.a.modelID, "new_model_id": w.second, "initial": w.draft})
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "agent-model-policy-material.json"), raw, 0600))
	clear(raw)
}
func (w *agentModelPolicyWeb) start(t *testing.T, address string) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned Agent Edit TLS verification disabled")
	}
	target, err := url.Parse(w.backend.URL)
	agentAppRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport, proxy.ErrorLog = w.transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse, proxy.ErrorHandler = w.response, w.proxyError
	static := http.FileServer(http.Dir(w.dist))
	w.front = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.frontWG.Add(1)
		defer w.frontWG.Done()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if err := w.observe(r); err != nil {
				w.fail(err)
				http.Error(out, "owned edit observation failed", 500)
				return
			}
			proxy.ServeHTTP(out, r)
			return
		}
		if w.dist == "" || r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(out, r)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(w.dist, clean)); err == nil && info.Mode().IsRegular() {
			static.ServeHTTP(out, r)
			return
		}
		http.ServeFile(out, r, filepath.Join(w.dist, "index.html"))
	})}
	go func() { w.serve <- w.front.Serve(w.listener) }()
}

func (w *agentModelPolicyWeb) create(t *testing.T, ctx context.Context) {
	t.Helper()
	rawRequest := jsonBytes(t, map[string]any{"agent_id": w.target, "name": w.draft.Name, "description": w.draft.Description, "instructions": w.draft.Instructions,
		"model_ref": w.a.modelID, "inject_agents_md": true, "approval_policy": "default", "allowed_tool_ids": []string{}, "allowed_mount_ids": []string{}, "allowed_secret_variable_ids": []string{}})
	request, err := ac.DecodeAgentCreate(rawRequest)
	agentAppRequire(t, err)
	const key f.IdempotencyKey = "agent-model-policy-original-create"
	reply := w.request(t, w.a.p2.base.ownerBrowser, http.MethodPost, variableHTTPPath(w.a.p2.project.ID, "/agents"), jsonBytes(t, map[string]any{"request": json.RawMessage(rawRequest)}), key)
	if reply.status != 200 {
		t.Fatal("original App could not create edit target", reply.status)
	}
	receipt, err := ac.DecodeAgentMutation(reply.body)
	agentAppRequire(t, err)
	rid, err := f.ParseID[f.Request](reply.header.Get("X-Request-ID"))
	agentAppRequire(t, err)
	agentAppRequire(t, w.createFacts(ctx, agentWebIntent{kind: "create", key: key, request: request, rawRequest: rawRequest}, rid, receipt))
	w.initial, w.current = receipt.Fields().Agent.Clone(), receipt.Clone()
}

func (w *agentModelPolicyWeb) createFacts(ctx context.Context, intent agentWebIntent, requestID f.ID[f.Request], receipt ac.AgentMutation) error {
	a := w.a
	request := intent.request.Fields()
	mutation := receipt.Fields()
	config := mutation.Agent.Fields()
	core := config.Core
	if receipt.Validate() != nil || !mutation.Changed || len(mutation.EventIDs) != 1 || core.ID != request.AgentID || core.ProjectID != a.p2.project.ID || core.Version != 1 || core.Lifecycle != ac.AgentActive || core.Name != request.Name || core.Description != request.Description || core.Instructions != request.Instructions || !core.InjectAgentsMD || core.ModelRef != a.modelID || core.ApprovalPolicy != ac.ApprovalDefault || core.ApprovalModelRef != nil || core.ReasoningEffort != nil || core.DisplayName != nil || core.TagColor != nil || !core.CreatedAt.Time().Equal(core.UpdatedAt.Time()) || !slices.Equal(config.AllowedToolIDs, []i.ToolID{a.tool.ToolID}) || len(config.AllowedMountIDs) != 0 || len(config.AllowedSecretVariableIDs) != 0 {
		return errors.New("browser Create receipt lost fixed draft/default install binding")
	}
	meta := f.CommandMeta{RequestID: requestID, IdempotencyKey: intent.key}
	digest, err := ac.AgentCommandDigest(a.p2.base.ownerBrowser.actor, meta, a.p2.project.ID, core.ID, ac.CreateAgentCommand, intent.request)
	if err != nil {
		return err
	}
	command, err := ac.AgentCommandIdentity(a.p2.project.ID, ac.CreateAgentCommand, intent.key)
	if err != nil {
		return err
	}
	facts, err := agentCreateFacts(ctx, a.p2.base.raw, a.p2.project.ID, core.ID, intent.key)
	if err != nil {
		return err
	}
	if facts != ([16]int64{1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}) {
		return fmt.Errorf("browser Agent atomic fact counts %v", facts)
	}
	var stored []byte
	var commandID string
	var revision int64
	var exact bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT id::text,plan_revision,receipt,
 after_config=receipt->'agent' AND before_config IS NULL AND expected_version IS NULL AND add_skills_enabled AND install_skill_enabled AND committed_at IS NOT NULL
 FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND command_name='agent.create' AND idempotency_key=$3 AND semantic_digest=$4 AND actor_user_id::text=$5 AND state='completed'`, a.p2.project.ID.String(), core.ID.String(), string(intent.key), digest.String(), a.p2.base.ownerBrowser.actor.Details().UserID).Scan(&commandID, &revision, &stored, &exact)
	persisted, decodeErr := ac.DecodeAgentMutation(stored)
	if err != nil || decodeErr != nil || !exact || revision < 1 || commandID != mutation.EventIDs[0].String() || !bytes.Equal(jsonAgentWeb(persisted), jsonAgentWeb(receipt)) {
		return errors.New("browser Agent command differs from original receipt/semantic identity")
	}
	var canonical bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT name=$3 AND normalized_name=$4 AND description=$5 AND instructions=$6 AND inject_agents_md
 AND model_id::text=$7 AND approval_policy='default' AND approval_model_id IS NULL AND reasoning_effort IS NULL AND display_name IS NULL AND tag_color IS NULL
 AND lifecycle='active' AND version=1 AND created_at=$8 AND updated_at=$8
 FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2`, a.p2.project.ID.String(), core.ID.String(), core.Name, core.NormalizedName, core.Description, core.Instructions, a.modelID.String(), core.CreatedAt.Time()).Scan(&canonical)
	if err != nil || !canonical {
		return errors.New("browser Agent receipt differs from canonical configuration")
	}
	var refs bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*)=1 AND bool_and(project_id::text=$1 AND model_id::text=$3 AND owner_version=1 AND reasoning_effort='' AND role='agent_model') FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id::text=$2)
 AND EXISTS(SELECT 1 FROM agenteam_agent.tool_allowlist WHERE project_id::text=$1 AND agent_id::text=$2 AND tool_id::text=$4)
 AND EXISTS(SELECT 1 FROM agenteam_tool.agent_configurations h JOIN agenteam_tool.agent_references r ON r.agent_id=h.agent_id WHERE h.project_id::text=$1 AND h.agent_id::text=$2 AND h.config_version=1 AND r.tool_id::text=$4)
 AND EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads WHERE project_id::text=$1 AND agent_id::text=$2 AND owner_version=1)`, a.p2.project.ID.String(), core.ID.String(), a.modelID.String(), a.tool.ToolID.String()).Scan(&refs)
	if err != nil || !refs {
		return errors.New("browser Agent actual provider references differ")
	}
	var skillCommand, binding string
	var skillRevision int64
	var published bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT h.creation_command,h.plan_revision,h.request_binding,
 h.add_skills_enabled AND h.assignment_sequence=1 AND h.observed_revision=s.current_revision
 AND a.id=h.initial_assignment_id AND a.skill_id=h.initial_skill_id AND a.enabled AND a.assignment_sequence=1 AND a.removed_at IS NULL AND a.created_at=h.created_at
 AND s.protected AND s.serving AND s.name='Add Skills' AND s.normalized_name='add-skills'
 AND r.revision=h.observed_revision AND z.phase='published' AND z.bundle_id='builtin.add-skills.v1' AND z.object_id=r.object_id
 FROM agenteam_skill.agent_assignment_heads h
 JOIN agenteam_skill.agent_assignments a ON a.project_id=h.project_id AND a.agent_id=h.agent_id AND a.id=h.initial_assignment_id
 JOIN agenteam_skill.skills s ON s.project_id=h.project_id AND s.id=h.initial_skill_id
 JOIN agenteam_skill.revisions r ON r.project_id=s.project_id AND r.skill_id=s.id AND r.id=s.revision_id
 JOIN agenteam_skill.initializations z ON z.project_id=s.project_id AND z.skill_id=s.id AND z.revision_id=r.id AND z.creation_id=s.creation_id
 WHERE h.project_id::text=$1 AND h.agent_id::text=$2`, a.p2.project.ID.String(), core.ID.String()).Scan(&skillCommand, &skillRevision, &binding, &published)
	if err != nil || skillCommand != command.Canonical() || skillRevision != revision || f.Digest(binding).Validate() != nil || !published {
		return errors.New("browser default Skill did not bind original publication/command")
	}
	var auditRaw, headerRaw, payloadRaw []byte
	var auditUser, session, cause string
	err = a.p2.base.raw.QueryRow(ctx, `SELECT metadata,user_id::text,session_id::text,cause_ref FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND resource_id::text=$2 AND producer='agent' AND action='agent.create' AND scope='project' AND actor_kind='human' AND outcome='success' AND resource_kind='agent' AND ordinal=0`, a.p2.project.ID.String(), core.ID.String()).Scan(&auditRaw, &auditUser, &session, &cause)
	if err != nil || auditUser != a.p2.base.ownerBrowser.actor.Details().UserID || session != a.p2.base.ownerBrowser.actor.Details().SessionID {
		return errors.New("browser Agent audit differs from original Owner Session")
	}
	metadata, err := auditc.DecodeMetadata(auditc.AgentCreate, auditRaw)
	if err != nil {
		return err
	}
	auditFields, err := metadata.AgentFields()
	causeDigest, causeErr := cursor.Digest([]byte(command.Canonical()))
	if err != nil || causeErr != nil || cause != causeDigest.String() || auditFields.AgentID != core.ID.String() || auditFields.CommandID != commandID || auditFields.Version != 1 || !slices.Equal(auditFields.ChangedFields, ac.EditableFields()) {
		return errors.New("browser Agent audit differs from original command/version")
	}
	var eventColumns bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,id::text=$3 AND event_type='agent.config_changed' AND schema_version=1 AND scope='project' AND aggregate_type='agent.config' AND aggregate_version=1 AND aggregate_sequence IS NULL AND occurred_at=$4 FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent'`, a.p2.project.ID.String(), core.ID.String(), commandID, core.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &eventColumns)
	if err != nil || !eventColumns {
		return errors.New("browser Agent Outbox identity differs")
	}
	header, err := eventc.DecodeHeader(headerRaw)
	var payload ac.ConfigChangedPayload
	if err != nil || ac.ValidateAgentEventHeader(header) != nil || json.Unmarshal(payloadRaw, &payload) != nil || payload.Validate() != nil || header.EventID != mutation.EventIDs[0] || header.AggregateID.String() != core.ID.String() || header.Scope.ProjectID.String() != core.ProjectID.String() || *header.AggregateVersion != 1 || !header.OccurredAt.Time().Equal(core.UpdatedAt.Time()) || payload.CommandID.String() != commandID || payload.ActorUserID.String() != auditUser || payload.Operation != ac.ConfigCreated || !slices.Equal(payload.ChangedFields, auditFields.ChangedFields) {
		return errors.New("browser Agent event differs from original receipt")
	}
	return ctx.Err()
}

func (w *agentModelPolicyWeb) updateFacts(ctx context.Context, intent agentEditIntent, rid f.ID[f.Request], receipt ac.AgentMutation) error {
	a, before, mutation := w.a, intent.before.Fields(), receipt.Fields()
	if intent.request.Validate() != nil || receipt.Validate() != nil || intent.before.Validate() != nil {
		return errors.New("invalid original Update fact")
	}
	after := mutation.Agent.Fields()
	proposed, err := intent.request.Proposed(intent.before)
	if err != nil {
		return err
	}
	var oldFields, newFields map[string]json.RawMessage
	if json.Unmarshal(jsonAgentWeb(intent.before), &oldFields) != nil || json.Unmarshal(jsonAgentWeb(proposed), &newFields) != nil {
		return errors.New("original Update fields did not encode")
	}
	changedFields := []string{}
	for _, field := range ac.EditableFields() {
		if !bytes.Equal(oldFields[field], newFields[field]) {
			changedFields = append(changedFields, field)
		}
	}
	changed := len(changedFields) != 0
	wantVersion := before.Core.Version
	if changed {
		wantVersion++
	}
	if mutation.Changed != changed || after.Core.Version != wantVersion || after.Core.ID != w.target || after.Core.ProjectID != a.p2.project.ID ||
		len(mutation.EventIDs) != map[bool]int{false: 0, true: 1}[changed] || after.Core.CreatedAt != before.Core.CreatedAt ||
		(changed && !after.Core.UpdatedAt.Time().After(before.Core.UpdatedAt.Time())) || (!changed && after.Core.UpdatedAt != before.Core.UpdatedAt) {
		return errors.New("Update receipt changed original identity/version/noop semantics")
	}
	wantFields := proposed.Fields()
	wantFields.Core.Version, wantFields.Core.UpdatedAt = wantVersion, after.Core.UpdatedAt
	want, err := ac.NewAgentConfig(wantFields)
	if err != nil || !bytes.Equal(jsonAgentWeb(want), jsonAgentWeb(mutation.Agent)) {
		return errors.New("Update receipt differs from full presence-sensitive patch")
	}
	meta := f.CommandMeta{RequestID: rid, IdempotencyKey: intent.key, ExpectedVersion: &before.Core.Version}
	digest, err := ac.AgentCommandDigest(a.p2.base.ownerBrowser.actor, meta, a.p2.project.ID, w.target, ac.UpdateAgentCommand, intent.request)
	if err != nil {
		return err
	}
	identity, err := ac.AgentCommandIdentity(a.p2.project.ID, ac.UpdateAgentCommand, intent.key)
	if err != nil {
		return err
	}
	var commandID string
	var stored, storedInput, storedBefore, storedAfter, fieldsRaw []byte
	var exact bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT id::text,input,before_config,after_config,receipt,changed_fields,
 expected_version=$6 AND add_skills_enabled IS NULL AND install_skill_enabled IS NULL AND plan_revision>=1 AND committed_at IS NOT NULL
 FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND command_name='agent.update'
 AND idempotency_key=$3 AND semantic_digest=$4 AND actor_user_id::text=$5 AND state='completed'`, a.p2.project.ID.String(), w.target.String(), string(intent.key), digest.String(), a.p2.base.ownerBrowser.actor.Details().UserID, int64(before.Core.Version)).Scan(&commandID, &storedInput, &storedBefore, &storedAfter, &stored, &fieldsRaw, &exact)
	if err != nil {
		return err
	}
	storedRequest, inputErr := ac.DecodeAgentUpdate(storedInput)
	preimage, beforeErr := ac.DecodeAgentConfig(storedBefore)
	postimage, afterErr := ac.DecodeAgentConfig(storedAfter)
	mutationStored, receiptErr := ac.DecodeAgentMutation(stored)
	var fields []string
	if inputErr != nil || beforeErr != nil || afterErr != nil || receiptErr != nil || !exact || json.Unmarshal(fieldsRaw, &fields) != nil ||
		!bytes.Equal(jsonAgentWeb(storedRequest), jsonAgentWeb(intent.request)) || !bytes.Equal(jsonAgentWeb(preimage), jsonAgentWeb(intent.before)) ||
		!bytes.Equal(jsonAgentWeb(postimage), jsonAgentWeb(mutation.Agent)) || !bytes.Equal(jsonAgentWeb(mutationStored), jsonAgentWeb(receipt)) || !slices.Equal(fields, changedFields) {
		return errors.New("durable Update command lost original input/preimage/postimage/receipt")
	}
	var canonical bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT name=$3 AND normalized_name=$4 AND description=$5 AND instructions=$6
 AND display_name IS NOT DISTINCT FROM $7::text AND tag_color IS NOT DISTINCT FROM $8::text
 AND inject_agents_md=$9 AND model_id::text=$10 AND approval_policy=$11 AND approval_model_id::text IS NOT DISTINCT FROM $15::text AND reasoning_effort IS NULL
 AND lifecycle='active' AND version=$12 AND created_at=$13 AND updated_at=$14
 FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2`, a.p2.project.ID.String(), w.target.String(), after.Core.Name, after.Core.NormalizedName,
		after.Core.Description, after.Core.Instructions, after.Core.DisplayName, after.Core.TagColor, after.Core.InjectAgentsMD, after.Core.ModelRef.String(), string(after.Core.ApprovalPolicy), int64(wantVersion), after.Core.CreatedAt.Time(), after.Core.UpdatedAt.Time(), modelPolicyOptionalID(after.Core.ApprovalModelRef)).Scan(&canonical)
	if err != nil || !canonical {
		return errors.New("Update canonical row differs from its actual full receipt")
	}
	var refs bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*)=$7 AND bool_and(project_id::text=$1 AND owner_version=$5 AND reasoning_effort='' AND ((role='agent_model' AND model_id::text=$3) OR (role='approval_model' AND model_id::text=$6))) FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id::text=$2)
 AND (SELECT count(*)=1 FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id::text=$2 AND role='agent_model')
 AND (SELECT count(*)=1 AND bool_and(tool_id::text=$4) FROM agenteam_agent.tool_allowlist WHERE project_id::text=$1 AND agent_id::text=$2)
 AND EXISTS(SELECT 1 FROM agenteam_tool.agent_configurations h JOIN agenteam_tool.agent_references r ON r.agent_id=h.agent_id WHERE h.project_id::text=$1 AND h.agent_id::text=$2 AND h.config_version=$5 AND r.tool_id::text=$4)
 AND EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads WHERE project_id::text=$1 AND agent_id::text=$2 AND owner_version=$5)
 AND NOT EXISTS(SELECT 1 FROM agenteam_agent.mount_allowlist WHERE project_id::text=$1 AND agent_id::text=$2)
 AND NOT EXISTS(SELECT 1 FROM agenteam_agent.secret_allowlist WHERE project_id::text=$1 AND agent_id::text=$2)
 AND EXISTS(SELECT 1 FROM agenteam_skill.agent_assignment_heads h JOIN agenteam_skill.agent_assignments s ON s.project_id=h.project_id AND s.agent_id=h.agent_id AND s.id=h.initial_assignment_id
 WHERE h.project_id::text=$1 AND h.agent_id::text=$2 AND h.add_skills_enabled AND h.assignment_sequence=1 AND s.enabled AND s.removed_at IS NULL)`, a.p2.project.ID.String(), w.target.String(), after.Core.ModelRef.String(), a.tool.ToolID.String(), int64(wantVersion), modelPolicyOptionalID(after.Core.ApprovalModelRef), map[bool]int{false: 1, true: 2}[after.Core.ApprovalModelRef != nil]).Scan(&refs)
	if err != nil {
		return err
	}
	if !refs || !slices.Equal(after.AllowedToolIDs, []i.ToolID{a.tool.ToolID}) || len(after.AllowedMountIDs) != 0 || len(after.AllowedSecretVariableIDs) != 0 {
		return errors.New("Update changed omitted default Skill/Tool or reference versions")
	}
	var auditCount, eventCount int
	err = a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND resource_id::text=$2 AND action='agent.update' AND metadata->>'command_id'=$3),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent' AND id::text=$3)`, a.p2.project.ID.String(), w.target.String(), commandID).Scan(&auditCount, &eventCount)
	if err != nil {
		return err
	}
	wantCount := 0
	if changed {
		wantCount = 1
	}
	if auditCount != wantCount || eventCount != wantCount {
		return errors.New("Update/noop duplicated or omitted Audit/Outbox")
	}
	if !changed {
		return ctx.Err()
	}
	if mutation.EventIDs[0].String() != commandID {
		return errors.New("Update event lost original command identity")
	}
	var auditRaw, headerRaw, payloadRaw []byte
	var user, session, cause string
	err = a.p2.base.raw.QueryRow(ctx, `SELECT metadata,user_id::text,session_id::text,cause_ref FROM agenteam_audit.audit_records
 WHERE project_id::text=$1 AND resource_id::text=$2 AND producer='agent' AND action='agent.update' AND metadata->>'command_id'=$3
 AND scope='project' AND actor_kind='human' AND outcome='success' AND resource_kind='agent' AND ordinal=0`, a.p2.project.ID.String(), w.target.String(), commandID).Scan(&auditRaw, &user, &session, &cause)
	if err != nil {
		return err
	}
	metadata, err := auditc.DecodeMetadata(auditc.AgentUpdate, auditRaw)
	if err != nil {
		return err
	}
	af, err := metadata.AgentFields()
	cd, causeErr := cursor.Digest([]byte(identity.Canonical()))
	if err != nil || causeErr != nil || cause != cd.String() || user != a.p2.base.ownerBrowser.actor.Details().UserID || session != a.p2.base.ownerBrowser.actor.Details().SessionID || af.AgentID != w.target.String() || af.CommandID != commandID || af.Version != wantVersion || !slices.Equal(af.ChangedFields, changedFields) {
		return errors.New("Update audit lost original Owner/command/fields")
	}
	var columns bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='agent.config_changed' AND schema_version=1 AND scope='project' AND aggregate_type='agent.config' AND aggregate_version=$4 AND aggregate_sequence IS NULL AND occurred_at=$5
 FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent' AND id::text=$3`, a.p2.project.ID.String(), w.target.String(), commandID, int64(wantVersion), after.Core.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &columns)
	if err != nil {
		return err
	}
	header, err := eventc.DecodeHeader(headerRaw)
	var payload ac.ConfigChangedPayload
	if err != nil || !columns || ac.ValidateAgentEventHeader(header) != nil || json.Unmarshal(payloadRaw, &payload) != nil || payload.Validate() != nil || header.EventID != mutation.EventIDs[0] || header.AggregateID.String() != w.target.String() || header.Scope.ProjectID.String() != a.p2.project.ID.String() || *header.AggregateVersion != wantVersion || !header.OccurredAt.Time().Equal(after.Core.UpdatedAt.Time()) || payload.CommandID.String() != commandID || payload.ActorUserID.String() != user || payload.Operation != ac.ConfigUpdated || !slices.Equal(payload.ChangedFields, changedFields) {
		return errors.New("Update Outbox differs from original command/receipt")
	}
	return ctx.Err()
}

func (w *agentModelPolicyWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPatch || r.URL.Path != w.path() || r.URL.RawQuery != "" {
		return errors.New("undeclared Model Policy browser write")
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, (512<<10)+1))
	closed := r.Body.Close()
	if e != nil || closed != nil || len(raw) > 512<<10 {
		return errors.New("original Model Policy request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	var envelope struct {
		Expected f.Version       `json:"expected_version"`
		Request  json.RawMessage `json:"request"`
	}
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 || agentWebDecode(raw, &envelope) != nil {
		return errors.New("original Update envelope invalid")
	}
	patch, e := ac.DecodeAgentUpdate(envelope.Request)
	if e != nil {
		return e
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	index := len(w.writes)
	before := w.current.Fields().Agent.Clone()
	var actual map[string]any
	if index >= 2 || index != len(w.mutations) || envelope.Expected != before.Fields().Core.Version || json.Unmarshal(envelope.Request, &actual) != nil || !reflect.DeepEqual(actual, w.patch(index)) {
		return errors.New("Model Policy touched fields/version changed")
	}
	for _, v := range w.writes {
		if v.key == key {
			return errors.New("duplicate Model Policy key")
		}
	}
	in := agentEditIntent{kind: "update", key: key, before: before, request: patch, rawRequest: bytes.Clone(envelope.Request), bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	w.keys = append(w.keys, string(key))
	w.writes = append(w.writes, in)
	*r = *r.WithContext(context.WithValue(r.Context(), agentEditIntentKey{}, in))
	return nil
}
func (w *agentModelPolicyWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	in, write := r.Request.Context().Value(agentEditIntentKey{}).(agentEditIntent)
	if write {
		receipt, err := ac.DecodeAgentMutation(raw)
		if err != nil || r.StatusCode != 200 {
			return errors.New("original Model Policy Update failed")
		}
		rid, err := f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
		if err != nil {
			return err
		}
		if err = w.updateFacts(r.Request.Context(), in, rid, receipt); err != nil {
			return err
		}
		w.mu.Lock()
		w.current = receipt.Clone()
		w.mutations = append(w.mutations, receipt.Clone())
		w.changed++
		w.mu.Unlock()
	} else if r.Request.Method == http.MethodGet && r.StatusCode == 200 {
		if r.Request.URL.Path == variableHTTPPath(w.a.p2.project.ID, "/available-chat-models") {
			if err = w.modelPage(raw); err != nil {
				return err
			}
			w.mu.Lock()
			w.modelsRead++
			w.mu.Unlock()
		} else if err = w.observeRead(r.Request.URL.Path, raw); err != nil {
			return err
		}
	}
	if r.StatusCode == 200 && (write || r.Request.Method == http.MethodGet) {
		proof := map[string]any{"input_hash": w.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
		if write {
			proof["kind"], proof["request_body_sha256"], proof["idempotency_key_sha256"], proof["committed_verified"] = "update", fmt.Sprintf("%x", in.bodyDigest), fmt.Sprintf("%x", in.keyDigest), true
		}
		encoded, e := json.Marshal(proof)
		if e != nil {
			return e
		}
		if e = os.MkdirAll(w.evidence, 0700); e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(w.evidence, "agent-model-policy-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600); e != nil {
			return e
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *agentModelPolicyWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	failure := w.failure
	mutations := append([]ac.AgentMutation(nil), w.mutations...)
	writes := append([]agentEditIntent(nil), w.writes...)
	pages, configs, models := w.pages, w.configurations, w.modelsRead
	w.mu.Unlock()
	if failure != nil || len(writes) != 2 || len(mutations) != 2 || w.changed != 2 || w.lookups != 0 || w.cut != 0 || pages < 2 || configs < 2 || models < 2 {
		t.Fatal("original Model Policy browser facts incomplete", failure)
	}
	for _, m := range mutations {
		if !m.Fields().Changed || len(m.Fields().EventIDs) != 1 {
			t.Fatal("changed Model Policy event missing")
		}
	}
	final := w.current.Fields().Agent.Fields().Core
	if final.Version != 3 || final.ModelRef != w.second || final.ApprovalPolicy != ac.ApprovalDefault || final.ApprovalModelRef != nil || final.InjectAgentsMD || final.ReasoningEffort != nil {
		t.Fatal("final Model Policy state mismatch")
	}
	counts, err := w.counts(ctx)
	agentAppRequire(t, err)
	if counts != ([5]int64{3, 3, 2, 3, 3}) {
		t.Fatal("Model Policy duplicate or missing command facts", counts)
	}
	// Final exact original-key Lookups remain read-only and preserve historical
	// v2/v3 receipts; they are not substitutes for each response's commit proof.
	for n, in := range writes {
		reply := w.request(t, w.a.p2.base.ownerBrowser, http.MethodPost, w.lookupPath(), agentEditLookupBody(t, w.target, in), in.key)
		result, err := ac.DecodeAgentCommandLookup(reply.body)
		if reply.status != 200 || err != nil || result.Status() != ac.AgentLookupCommitted || result.Receipt() == nil || !bytes.Equal(jsonAgentWeb(*result.Receipt()), jsonAgentWeb(mutations[n])) {
			t.Fatal("original Model Policy Lookup lost historical receipt")
		}
	}
	after, err := w.counts(ctx)
	agentAppRequire(t, err)
	if after != counts {
		t.Fatal("original Model Policy Lookup mutated history")
	}
	var executions int
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1`, w.a.p2.project.ID.String()).Scan(&executions))
	if executions != 0 {
		t.Fatal("runtime-off policy edit launched Execution")
	}
}
func (w *agentModelPolicyWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "agent-model-policy.config.js"))
	cmd.Dir, cmd.SysProcAttr, cmd.WaitDelay = root, &syscall.SysProcAttr{Setpgid: true}, time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	const prefix = "AGENTEAM_AGENT_MODEL_POLICY_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Agent Model Policy Playwright did not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
	}()
	err = <-done
	joined = true
	t.Logf("Agent Model Policy Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	failure := w.failure
	w.mu.Unlock()
	tokens = append(tokens, w.a.p2.base.ownerBrowser.cookie, w.a.p2.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if failure != nil {
		t.Error("owned Agent Model Policy observation failed", failure)
	}
	if err != nil {
		t.Fatal("original Agent Model Policy browser call failed and was waited")
	}
}
