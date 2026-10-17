//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/memory"
	mm "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// One owned App/database exercises the new configuration boundary and the
// original Memory and Meeting consumers. Only provider DNS and replies are
// deterministic. The unchanged management/proxy/Unknown matrices are reused;
// this test does not replace consumer authorities or manufacture their rows.
func TestModelPickerReasoningIndependentReview(t *testing.T) {
	network, err := netfixture.Load()
	workAppCheck(t, "picker-owned-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	workAppCheck(t, "picker-owned-peer-address", err)
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env := guardEnvironment(t, v.root.db)
		for key, value := range memoryM3Settings() {
			env = append(env, "AGENTEAM_CENTRAL_"+key+"="+value)
		}
		env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		v.root.cfg = guardConfiguration(t, env)
		v.store.observeCommit = m1CommitObserver(t)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver, transport = appFixtureResolver{address}, actual
			return nil
		}
	})
	if transport == nil || v.runtime.memories == nil || v.runtime.meetings == nil {
		t.Fatal("picker review requires both original summary consumers")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	w := &m1Workflow{app: v, network: network}
	review := &pickerReasoningReview{app: v, secret: "owned-picker-" + guardID[struct{}](t).String()}
	defer func() {
		if t.Failed() {
			memoryM3Diagnose(t, v, "picker-reasoning-review")
		}
	}()
	var retainRaw string
	workAppCheck(t, "picker-retain-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:retain'`).Scan(&retainRaw))
	retain, err := f.ParseID[id.Tool](retainRaw)
	workAppCheck(t, "picker-retain-id", err)
	agentScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		memoryM3ToolReply(t, retain, map[string]any{"content": memoryM3Fact}, 1),
		memoryM3TextReply(t, "Retained the lesson.", 2),
		memoryM3TextReply(t, "Preserve the selected reasoning configuration.", 3),
		memoryM3TextReply(t, "Review the immutable summary snapshot.", 4),
	}})
	workAppCheck(t, "picker-agent-peer", err)
	content := map[string]any{"type": "procedure", "text": memoryM3Fact, "tags": []string{"migration"}, "importance": "normal"}
	extraction := string(workAppJSON(t, map[string]any{"candidates": []any{content}}))
	consolidation := string(workAppJSON(t, map[string]any{"actions": []any{map[string]any{"action": "ADD", "target_memory_id": "", "expected_revision": "", "content": content, "reason": "Reusable migration procedure"}}}))
	const title = "Frozen reasoning choice"
	summary := string(workAppJSON(t, map[string]string{"title": title, "goals": "Preserve selected reasoning", "decisions": "Use the captured value", "unresolved": "", "facts": "Two participants"}))
	summaryScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		memoryM3TextReply(t, extraction, 1), memoryM3TextReply(t, consolidation, 2),
		memoryM3TextReply(t, "invalid summary envelope", 3), memoryM3TextReply(t, summary, 4),
	}})
	workAppCheck(t, "picker-summary-peer", err)
	embeddingWire := netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, map[string]any{
		"object": "list", "model": "picker-embedding", "data": []any{map[string]any{"object": "embedding", "index": 0, "embedding": []float64{1, 0}}}, "usage": map[string]any{"prompt_tokens": 10, "total_tokens": 10},
	})}}
	embeddingScenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_embeddings_wire", Wire: &embeddingWire})
	workAppCheck(t, "picker-embedding-peer", err)
	var credential struct {
		ID string `json:"credential_id"`
	}
	review.decode(t, "picker-credential", review.call(t, http.MethodPost, "model-credentials", review.key(t), map[string]any{"value": review.secret}, http.StatusOK), &credential)
	if credential.ID == "" {
		t.Fatal("picker credential identity missing")
	}
	agentProvider := review.provider(t, agentScenario, credential.ID)
	summaryProvider := review.provider(t, summaryScenario, credential.ID)
	embeddingProvider := review.provider(t, embeddingScenario, credential.ID)
	agentInput := pickerReasoningInput("chat", "picker-agent", "")
	agentInput["capabilities"].(map[string]any)["tool_calls"] = true
	agentInput["capabilities"].(map[string]any)["structured_output_modes"] = []string{"text"}
	agent := review.model(t, agentProvider, agentInput)
	embedding := review.model(t, embeddingProvider, pickerReasoningInput("embedding", "picker-embedding", ""))
	longNotes := strings.Repeat("🧪", 2000) // Exactly 2,000 scalars / 8,000 UTF-8 bytes.
	toggleInput := pickerReasoningInput("chat", pickerReasoningLongModelID(), longNotes)
	toggleCaps := toggleInput["capabilities"].(map[string]any)
	toggleCaps["reasoning"], toggleCaps["reasoning_toggle_mode"] = true, "enable_thinking"
	toggle := review.model(t, summaryProvider, toggleInput)
	effortInput := pickerReasoningInput("chat", "picker-effort", "Remark is not the model identity")
	effortCaps := effortInput["capabilities"].(map[string]any)
	effortCaps["reasoning"] = true
	effortCaps["reasoning_efforts"] = []any{map[string]any{"name": "Displayed depth", "value": "vendor:deep"}}
	effort := review.model(t, summaryProvider, effortInput)
	thinkingInput := pickerReasoningInput("chat", "picker-thinking", "")
	thinkingCaps := thinkingInput["capabilities"].(map[string]any)
	thinkingCaps["reasoning"], thinkingCaps["reasoning_toggle_mode"] = true, "thinking_type"
	thinking := review.model(t, summaryProvider, thinkingInput)
	review.configurationChecks(t, embedding, toggle, effort, thinking, toggleInput, longNotes)
	t.Log("MODEL_PICKER_REVIEW stage=configuration_confirmed")

	// Memory resolves the platform choice itself. Its original request contains
	// neither a caller-supplied effort nor a fabricated reasoning flag.
	selectedOff := review.selectSummary(t, embedding, toggle, "off", "")
	agentID, err := f.ParseID[mc.Model](agent)
	workAppCheck(t, "picker-agent-model-id", err)
	agents := meetingM3CreateAgents(t, v, agentID)
	sprint := memoryM3CreateSprint(t, v)
	v.agent = agents[0]
	task := memoryM3CreateTask(t, v, sprint.ID, agents[0], "Explicitly retain the durable migration lesson.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	workAppCheck(t, "picker-start-retain", err)
	v.store.releaseDirectory()
	execution := memoryM3AwaitExecution(t, v, task.ID)
	page, err := v.runtime.memories.List(v.ctx, v.actor, mm.Namespace{ProjectID: v.a, AgentID: agents[0]}, memory.ListQuery{})
	workAppCheck(t, "picker-memory-canonical", err)
	if len(page.Memories) != 1 || page.Memories[0].Current.Text != memoryM3Fact || page.Memories[0].Current.Provenance.ExecutionID != execution {
		t.Fatal("real retain did not publish its original canonical memory")
	}
	var memorySnapshots [][]byte
	rows, err := v.admin.Query(v.ctx, `SELECT s.snapshot_data FROM agenteam_memory.model_calls c JOIN agenteam_model.snapshots s ON s.id::text=c.snapshot_id WHERE c.project_id::text=$1 AND c.purpose IN('memory_extraction','memory_consolidation') ORDER BY c.purpose`, v.a.String())
	workAppCheck(t, "picker-memory-snapshots", err)
	for rows.Next() {
		var raw []byte
		workAppCheck(t, "picker-memory-snapshot-row", rows.Scan(&raw))
		memorySnapshots = append(memorySnapshots, raw)
		pickerReasoningSnapshot(t, raw, toggle, pickerReasoningLongModelID(), selectedOff, "enable_thinking", "off", "")
	}
	workAppCheck(t, "picker-memory-snapshot-rows", rows.Err())
	rows.Close()
	if len(memorySnapshots) != 2 {
		t.Fatal("Memory did not capture both original summary calls", len(memorySnapshots))
	}
	t.Log("MODEL_PICKER_REVIEW stage=memory_canonical_and_snapshots_confirmed")

	// One genuine Meeting generation fails parsing, then retries after the
	// shared choice changes. The retry must retain its original value/snapshot.
	selectedEffort := review.selectSummary(t, embedding, effort, "effort", "vendor:deep")
	rejected := make(chan meetingM3RejectedTransaction, 1)
	started, err := v.runtime.meetings.Start(v.ctx, v.actor, mt.StartRequest{ProjectID: v.a, Agents: agents, Mode: "sequential", Content: []mt.InlineNode{{Type: "text", Text: "Discuss the saved reasoning choice."}}, Meta: executionSchedulerAppMeta(t, nil)})
	workAppCheck(t, "picker-meeting-start", err)
	generation, snapshot := meetingM3AwaitSummaryState(t, v, started, "failed", 1, rejected)
	meetingM3CheckSummaryPin(t, v, generation, snapshot, "retained", 1)
	var frozen []byte
	workAppCheck(t, "picker-meeting-snapshot", v.admin.QueryRow(v.ctx, `SELECT snapshot_data FROM agenteam_model.snapshots WHERE id::text=$1`, snapshot).Scan(&frozen))
	pickerReasoningSnapshot(t, frozen, effort, "picker-effort", selectedEffort, "", "effort", "vendor:deep")
	review.selectSummary(t, embedding, thinking, "on", "")
	changedEffort := pickerReasoningCopy(t, effortInput)
	changedEffort["capabilities"].(map[string]any)["reasoning_efforts"] = []any{map[string]any{"name": "New display depth", "value": "vendor:new"}}
	review.call(t, http.MethodPut, "models/"+effort, review.key(t), map[string]any{"expected_version": "1", "input": changedEffort}, http.StatusOK)
	detail, err := v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, started.MeetingID)
	workAppCheck(t, "picker-meeting-failed-detail", err)
	retry := mt.ControlRequest{ProjectID: v.a, MeetingID: started.MeetingID, TurnID: started.TurnID, ExpectedVersion: detail.Session.Version, Action: "retry-summary", Meta: executionSchedulerAppMeta(t, nil)}
	_, err = v.runtime.meetings.Control(v.ctx, v.actor, retry)
	workAppCheck(t, "picker-meeting-explicit-retry", err)
	retried, retained := meetingM3AwaitSummaryState(t, v, started, "published", 2, rejected)
	if retried != generation || retained != snapshot {
		t.Fatal("changed platform reasoning replaced the original Meeting snapshot")
	}
	meetingM3CheckSummaryPin(t, v, generation, snapshot, "released", 2)
	var after []byte
	workAppCheck(t, "picker-meeting-snapshot-replay", v.admin.QueryRow(v.ctx, `SELECT snapshot_data FROM agenteam_model.snapshots WHERE id::text=$1`, snapshot).Scan(&after))
	if !bytes.Equal(frozen, after) {
		t.Fatal("retained snapshot changed after the current choice changed")
	}
	detail, err = v.runtime.meetings.GetDetail(v.ctx, v.actor, v.a, started.MeetingID)
	workAppCheck(t, "picker-meeting-published-detail", err)
	if detail.Session.Title == nil || *detail.Session.Title != title || detail.Session.Summary == nil || detail.Session.Summary.Decisions != "Use the captured value" {
		t.Fatal("real Meeting retry did not publish its summary")
	}
	t.Log("MODEL_PICKER_REVIEW stage=meeting_retry_snapshot_confirmed")
	review.mutationChecks(t, embedding, thinking, toggle, effort, thinkingInput)
	review.permissionChecks(t, embedding, toggle)

	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "picker-original-runtime-drain", err)
	v.requestStop()
	usageAppWait(t, v.root.done, "picker-original-app-return")
	workAppCheck(t, "picker-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("picker App returned before its original owners joined")
	}
	memoryM3GuardReleased(t, v.workAppFixture)
	agentState := w.joinedPeer(t, agentScenario, 4, 4)
	summaryState := w.joinedPeer(t, summaryScenario, 4, 4)
	embeddingState := w.joinedPeer(t, embeddingScenario, 1, 1)
	for _, request := range agentState.Requests {
		pickerReasoningWire(t, request, review.secret, "picker-agent", "", "", false, longNotes)
	}
	for n, request := range summaryState.Requests {
		if n < 2 {
			pickerReasoningWire(t, request, review.secret, pickerReasoningLongModelID(), "enable_thinking", "off", true, longNotes)
		} else {
			pickerReasoningWire(t, request, review.secret, "picker-effort", "", "vendor:deep", false, longNotes)
		}
	}
	if len(embeddingState.Requests) != 1 {
		t.Fatal("unexpected embedding request count")
	}
	independentProxyNoSecret(t, []byte(v.root.logs.String()), review.secret, v.cookie, v.csrf)
	t.Log("MODEL_PICKER_REVIEW real_memory_and_meeting=true explicit_off_on_wire=true effort_value_on_wire=true retained_snapshot_retry=true notes_not_identity=true owned_app_joined=true external_model=false")
}

type pickerReasoningReview struct {
	app    *executionSchedulerApp
	secret string
}

func (*pickerReasoningReview) key(t *testing.T) string { return guardID[struct{}](t).String() }

func (r *pickerReasoningReview) call(t *testing.T, method, path, key string, input any, want int) []byte {
	t.Helper()
	var body []byte
	if input != nil {
		body = workAppJSON(t, input)
	}
	defer clear(body)
	response, err := r.app.request(r.app.ctx, method, "/api/v1/system/"+path, body, key)
	workAppCheck(t, "picker-http-"+path, err)
	independentProxyNoSecret(t, response.body, r.secret)
	if response.status != want {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		t.Fatalf("picker method=%s route=%s status=%d want=%d code=%s", method, path, response.status, want, problem.Code.Safe())
	}
	return response.body
}

func (*pickerReasoningReview) decode(t *testing.T, stage string, raw []byte, out any) {
	t.Helper()
	workAppCheck(t, stage, json.Unmarshal(raw, out))
}

func (r *pickerReasoningReview) provider(t *testing.T, scenario, credential string) string {
	t.Helper()
	var receipt mc.CommandReceipt
	r.decode(t, "picker-provider-create", r.call(t, http.MethodPost, "model-providers", r.key(t), map[string]any{"input": map[string]any{
		"name": "Picker review provider", "protocol": "openai-compatible", "base_url": "https://fixture.test:8443/case/" + scenario,
		"enabled": true, "credential_ref": credential, "options": map[string]any{},
	}}, http.StatusOK), &receipt)
	return receipt.ResourceID
}

func (r *pickerReasoningReview) model(t *testing.T, provider string, input map[string]any) string {
	t.Helper()
	var receipt mc.CommandReceipt
	r.decode(t, "picker-model-create", r.call(t, http.MethodPost, "models", r.key(t), map[string]any{"provider_id": provider, "input": input}, http.StatusOK), &receipt)
	var view struct {
		ProviderID string         `json:"provider_id"`
		Version    string         `json:"version"`
		Input      map[string]any `json:"input"`
	}
	r.decode(t, "picker-model-read", r.call(t, http.MethodGet, "models/"+receipt.ResourceID, "", nil, http.StatusOK), &view)
	if view.ProviderID != provider || view.Version != "1" || !bytes.Equal(workAppJSON(t, view.Input), workAppJSON(t, input)) {
		t.Fatal("model input did not persist and read back exactly")
	}
	if _, legacy := view.Input["name"]; legacy {
		t.Fatal("legacy required model name remains in the DTO")
	}
	return receipt.ResourceID
}

func pickerReasoningInput(kind, providerID, notes string) map[string]any {
	output, structured := []string{"text"}, []string{"text", "json_schema"}
	if kind == "embedding" {
		output, structured = []string{"vector"}, []string{}
	}
	return map[string]any{
		"notes": notes, "provider_model_id": providerID, "type": kind, "enabled": true,
		"parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{},
		"capabilities": map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false,
			"input_modalities": []string{"text"}, "output_modalities": output, "reasoning_efforts": []any{}, "reasoning_toggle_mode": nil,
			"structured_output_modes": structured, "context_length": nil, "max_output": nil},
	}
}

// A valid provider ID can exceed the former 128-character display-name limit.
func pickerReasoningLongModelID() string { return "picker-" + strings.Repeat("m", 249) }

type pickerReasoningSelection struct {
	ID         string         `json:"id"`
	Version    string         `json:"version"`
	Configured map[string]any `json:"configured"`
}

func (r *pickerReasoningReview) selection(t *testing.T) pickerReasoningSelection {
	t.Helper()
	var selected pickerReasoningSelection
	r.decode(t, "picker-selection-read", r.call(t, http.MethodGet, "model-selection", "", nil, http.StatusOK), &selected)
	if selected.ID == "" || selected.Version == "" {
		t.Fatal("selection identity/version missing")
	}
	return selected
}

func pickerReasoningChoice(model, mode, value string) map[string]any {
	reasoning := map[string]any{"mode": mode}
	if mode == "effort" {
		reasoning["value"] = value
	}
	return map[string]any{"model_id": model, "reasoning": reasoning}
}

func pickerReasoningSelectionInput(selected pickerReasoningSelection, embedding, summary, mode, value string) map[string]any {
	return map[string]any{"id": selected.ID, "expected_version": selected.Version,
		"embedding": pickerReasoningChoice(embedding, "default", ""), "summary": pickerReasoningChoice(summary, mode, value), "reranker": nil, "image": nil}
}

func (r *pickerReasoningReview) selectSummary(t *testing.T, embedding, summary, mode, value string) string {
	t.Helper()
	before := r.selection(t)
	input := pickerReasoningSelectionInput(before, embedding, summary, mode, value)
	key := r.key(t)
	raw := r.call(t, http.MethodPut, "model-selection", key, input, http.StatusOK)
	var receipt mc.CommandReceipt
	r.decode(t, "picker-selection-receipt", raw, &receipt)
	after := r.selection(t)
	want := map[string]any{"embedding": input["embedding"], "summary": input["summary"], "reranker": nil, "image": nil}
	if after.ID != before.ID || after.Version == before.Version || after.Version != receipt.Version.String() || !reflect.DeepEqual(after.Configured, want) {
		t.Fatal("reasoning choice did not participate in the durable platform selection")
	}
	if !bytes.Equal(raw, r.call(t, http.MethodPut, "model-selection", key, input, http.StatusOK)) || !reflect.DeepEqual(after, r.selection(t)) {
		t.Fatal("same-key selection replay mutated or lost the original result")
	}
	return after.Version
}

func pickerReasoningCopy(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	var out map[string]any
	workAppCheck(t, "picker-copy-input", json.Unmarshal(workAppJSON(t, value), &out))
	return out
}

func (r *pickerReasoningReview) configurationChecks(t *testing.T, embedding, toggle, effort, thinking string, input map[string]any, notes string) {
	t.Helper()
	var page struct {
		Items []struct {
			ID       string         `json:"id"`
			Input    map[string]any `json:"input"`
			Provider map[string]any `json:"provider"`
		} `json:"items"`
	}
	raw := r.call(t, http.MethodGet, "models?limit=100", "", nil, http.StatusOK)
	r.decode(t, "picker-global-list", raw, &page)
	if len(page.Items) != 5 {
		t.Fatal("global picker list omitted configured models")
	}
	found := false
	for _, item := range page.Items {
		if _, legacy := item.Input["name"]; legacy {
			t.Fatal("picker still relies on model display name")
		}
		if item.ID == toggle {
			found = true
			if item.Input["notes"] != notes || item.Input["provider_model_id"] != pickerReasoningLongModelID() {
				t.Fatal("picker projection changed notes or provider ID")
			}
		}
		for _, field := range []string{"credential_ref", "credential_id", "base_url", "options"} {
			if _, present := item.Provider[field]; present {
				t.Fatal("picker provider projection leaked configuration", field)
			}
		}
	}
	if !found {
		t.Fatal("long-notes model missing from picker")
	}
	before := r.call(t, http.MethodGet, "models/"+toggle, "", nil, http.StatusOK)
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"over-rune-limit", func(v map[string]any) { v["notes"] = strings.Repeat("a", 2001) }},
		{"nul", func(v map[string]any) { v["notes"] = "remark\x00suffix" }},
		{"missing-notes", func(v map[string]any) { delete(v, "notes") }},
		{"null-notes", func(v map[string]any) { v["notes"] = nil }},
		{"legacy-name", func(v map[string]any) { v["name"] = "legacy" }},
	} {
		bad := pickerReasoningCopy(t, input)
		tc.mutate(bad)
		r.call(t, http.MethodPut, "models/"+toggle, r.key(t), map[string]any{"expected_version": "1", "input": bad}, http.StatusBadRequest)
		if !bytes.Equal(before, r.call(t, http.MethodGet, "models/"+toggle, "", nil, http.StatusOK)) {
			t.Fatal("rejected notes changed model", tc.name)
		}
	}
	// Saving and clearing optional remarks must not change the primary ID.
	changed := pickerReasoningCopy(t, input)
	changed["notes"] = ""
	r.call(t, http.MethodPut, "models/"+toggle, r.key(t), map[string]any{"expected_version": "1", "input": changed}, http.StatusOK)
	var cleared struct {
		ID      string         `json:"id"`
		Version string         `json:"version"`
		Input   map[string]any `json:"input"`
	}
	r.decode(t, "picker-empty-notes-read", r.call(t, http.MethodGet, "models/"+toggle, "", nil, http.StatusOK), &cleared)
	if cleared.ID != toggle || cleared.Version != "2" || cleared.Input["notes"] != "" || cleared.Input["provider_model_id"] != pickerReasoningLongModelID() {
		t.Fatal("clearing remarks changed model identity")
	}
	r.call(t, http.MethodPut, "models/"+toggle, r.key(t), map[string]any{"expected_version": "2", "input": input}, http.StatusOK)
	r.selectSummary(t, embedding, toggle, "default", "")
	selected := r.selection(t)
	valid := pickerReasoningSelectionInput(selected, embedding, toggle, "on", "")
	key := r.key(t)
	confirmed := r.call(t, http.MethodPut, "model-selection", key, valid, http.StatusOK)
	changedChoice := pickerReasoningCopy(t, valid)
	changedChoice["summary"] = pickerReasoningChoice(toggle, "off", "")
	r.call(t, http.MethodPut, "model-selection", key, changedChoice, http.StatusConflict)
	if !bytes.Equal(confirmed, r.call(t, http.MethodPut, "model-selection", key, valid, http.StatusOK)) {
		t.Fatal("choice semantic conflict damaged original receipt")
	}
	selected = r.selection(t)
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"legacy-id", func(v map[string]any) { v["summary"] = toggle }},
		{"missing-reasoning", func(v map[string]any) { v["summary"] = map[string]any{"model_id": toggle} }},
		{"null-reasoning", func(v map[string]any) { v["summary"] = map[string]any{"model_id": toggle, "reasoning": nil} }},
		{"unknown-choice-field", func(v map[string]any) {
			v["summary"] = map[string]any{"model_id": toggle, "reasoning": map[string]any{"mode": "off", "parameter": "x"}}
		}},
		{"default-has-value", func(v map[string]any) {
			v["summary"] = map[string]any{"model_id": toggle, "reasoning": map[string]any{"mode": "default", "value": ""}}
		}},
		{"effort-missing-value", func(v map[string]any) {
			v["summary"] = map[string]any{"model_id": effort, "reasoning": map[string]any{"mode": "effort"}}
		}},
		{"effort-display-name", func(v map[string]any) { v["summary"] = pickerReasoningChoice(effort, "effort", "Displayed depth") }},
		{"effort-undeclared", func(v map[string]any) { v["summary"] = pickerReasoningChoice(effort, "effort", "other") }},
		{"levels-have-no-toggle", func(v map[string]any) { v["summary"] = pickerReasoningChoice(effort, "off", "") }},
		{"embedding-has-no-reasoning", func(v map[string]any) { v["embedding"] = pickerReasoningChoice(embedding, "on", "") }},
	} {
		bad := pickerReasoningSelectionInput(selected, embedding, toggle, "on", "")
		tc.mutate(bad)
		want := http.StatusBadRequest
		if tc.name == "effort-undeclared" || tc.name == "levels-have-no-toggle" {
			want = http.StatusUnprocessableEntity
		}
		r.call(t, http.MethodPut, "model-selection", r.key(t), bad, want)
		if !reflect.DeepEqual(selected, r.selection(t)) {
			t.Fatal("invalid choice changed selection", tc.name)
		}
	}
	// Changing model explicitly to default must drop the previous on/off choice.
	r.selectSummary(t, embedding, effort, "default", "")
	r.selectSummary(t, embedding, thinking, "on", "")
}

func (r *pickerReasoningReview) mutationChecks(t *testing.T, embedding, thinking, toggle, effort string, thinkingInput map[string]any) {
	t.Helper()
	beforeSelection := r.selection(t)
	beforeModel := r.call(t, http.MethodGet, "models/"+thinking, "", nil, http.StatusOK)
	bad := pickerReasoningCopy(t, thinkingInput)
	bad["capabilities"].(map[string]any)["reasoning_toggle_mode"] = nil
	r.call(t, http.MethodPut, "models/"+thinking, r.key(t), map[string]any{"expected_version": "1", "input": bad}, http.StatusUnprocessableEntity)
	r.call(t, http.MethodDelete, "models/"+thinking, r.key(t), map[string]any{"expected_version": "1", "replacement": effort}, http.StatusUnprocessableEntity)
	if !bytes.Equal(beforeModel, r.call(t, http.MethodGet, "models/"+thinking, "", nil, http.StatusOK)) || !reflect.DeepEqual(beforeSelection, r.selection(t)) {
		t.Fatal("incompatible mutation lost the selected model or reasoning choice")
	}
	var receipt mc.CommandReceipt
	r.decode(t, "picker-compatible-replacement", r.call(t, http.MethodDelete, "models/"+thinking, r.key(t), map[string]any{"expected_version": "1", "replacement": toggle}, http.StatusOK), &receipt)
	after := r.selection(t)
	if receipt.AffectedReferences != 1 || after.Version == beforeSelection.Version || !reflect.DeepEqual(after.Configured["summary"], pickerReasoningChoice(toggle, "on", "")) || !reflect.DeepEqual(after.Configured["embedding"], pickerReasoningChoice(embedding, "default", "")) {
		t.Fatal("compatible replacement failed to carry the selected choice and reference version")
	}
	r.call(t, http.MethodGet, "models/"+thinking, "", nil, http.StatusNotFound)
}

func (r *pickerReasoningReview) permissionChecks(t *testing.T, embedding, summary string) {
	t.Helper()
	selected := r.selection(t)
	input := pickerReasoningSelectionInput(selected, embedding, summary, "default", "")
	key := r.key(t)
	r.call(t, http.MethodPut, "model-selection", key, input, http.StatusOK)
	confirmed := r.selection(t)
	_, err := r.app.admin.Exec(r.app.ctx, `UPDATE agenteam_account.users SET role='user' WHERE id=$1`, r.app.actor.Details().UserID)
	workAppCheck(t, "picker-revoke-owned-administrator", err)
	r.call(t, http.MethodPut, "model-selection", key, input, http.StatusForbidden)
	r.call(t, http.MethodPut, "model-selection", r.key(t), pickerReasoningSelectionInput(confirmed, embedding, summary, "off", ""), http.StatusForbidden)
	r.call(t, http.MethodPost, "model-commands/lookup", key, map[string]any{"command": "model.selection.update"}, http.StatusForbidden)
	r.call(t, http.MethodGet, "models?limit=100", "", nil, http.StatusForbidden)
	var version string
	workAppCheck(t, "picker-revoked-selection-unchanged", r.app.admin.QueryRow(r.app.ctx, `SELECT version::text FROM agenteam_model.platform_selection WHERE id=$1`, confirmed.ID).Scan(&version))
	if version != confirmed.Version {
		t.Fatal("revoked caller mutated the platform selection")
	}
}

func pickerReasoningSnapshot(t *testing.T, raw []byte, modelID, providerModelID, version, mapping, mode, value string) {
	t.Helper()
	var valueDTO struct {
		Identity struct {
			ModelID         string `json:"model_id"`
			ProviderModelID string `json:"provider_model_id"`
			ModelName       string `json:"model_name"`
		} `json:"Identity"`
		Capabilities struct {
			Mapping *string                        `json:"reasoning_toggle_mode"`
			Efforts []struct{ Name, Value string } `json:"reasoning_efforts"`
		} `json:"Capabilities"`
		SelectionVersion string `json:"SelectionVersion"`
		Enabled          *bool  `json:"reasoning_enabled"`
		Effort           string `json:"reasoning_effort"`
	}
	workAppCheck(t, "picker-frozen-snapshot-decode", json.Unmarshal(raw, &valueDTO))
	if valueDTO.Identity.ModelID != modelID || valueDTO.Identity.ProviderModelID != providerModelID || valueDTO.Identity.ModelName != providerModelID || valueDTO.SelectionVersion != version {
		t.Fatal("snapshot lost the model ID, trusted selection version, or used remarks as identity")
	}
	if mode == "off" {
		if valueDTO.Enabled == nil || *valueDTO.Enabled || valueDTO.Effort != "" || valueDTO.Capabilities.Mapping == nil || *valueDTO.Capabilities.Mapping != mapping {
			t.Fatal("snapshot lost explicit false or the fixed mapping")
		}
	} else if mode == "effort" {
		if valueDTO.Enabled != nil || valueDTO.Effort != value || len(valueDTO.Capabilities.Efforts) != 1 || valueDTO.Capabilities.Efforts[0].Value != value {
			t.Fatal("snapshot lost the declared reasoning value")
		}
	} else {
		t.Fatal("unexpected snapshot review mode")
	}
	if bytes.Contains(raw, []byte(`"notes"`)) {
		t.Fatal("notes entered the immutable runtime identity")
	}
}

func pickerReasoningWire(t *testing.T, request netfixture.Request, credential, model, mapping, choice string, structured bool, notes string) {
	t.Helper()
	if request.Method != http.MethodPost || request.Headers.Get("Authorization") != "Bearer "+credential || request.Headers.Get("Proxy-Authorization") != "" || request.Query != "" || !strings.HasSuffix(request.Path, "/chat/completions") {
		t.Fatal("summary lost the fixed target/credential separation")
	}
	var body map[string]json.RawMessage
	workAppCheck(t, "picker-captured-wire-decode", json.Unmarshal([]byte(request.Body), &body))
	if string(body["model"]) != fmt.Sprintf("%q", model) {
		t.Fatal("wire model was replaced by display metadata")
	}
	_, hasEffort := body["reasoning_effort"]
	_, hasToggle := body["enable_thinking"]
	_, hasThinking := body["thinking"]
	if mapping == "enable_thinking" {
		if choice != "off" || !hasToggle || string(body["enable_thinking"]) != "false" || hasEffort || hasThinking {
			t.Fatal("explicit off omitted or sent the wrong fixed reasoning field")
		}
	} else if choice != "" {
		if !hasEffort || string(body["reasoning_effort"]) != `"vendor:deep"` || hasToggle || hasThinking {
			t.Fatal("selected effort sent a display name or changed during retry")
		}
	} else if hasEffort || hasToggle || hasThinking {
		t.Fatal("supplier default acquired an explicit reasoning field")
	}
	if choice != "" {
		var format struct {
			Type string `json:"type"`
		}
		workAppCheck(t, "picker-response-format", json.Unmarshal(body["response_format"], &format))
		want := "text"
		if structured {
			want = "json_schema"
		}
		if format.Type != want {
			t.Fatal("reasoning changed the consumer response format")
		}
	}
	for _, forbidden := range []string{notes, "Displayed depth", "Remark is not the model identity", `"notes"`, `"reasoning_toggle_mode"`, `"reasoning_enabled"`, `"reasoning_efforts"`} {
		if strings.Contains(request.Body, forbidden) {
			t.Fatal("display/configuration metadata leaked into provider request")
		}
	}
}
