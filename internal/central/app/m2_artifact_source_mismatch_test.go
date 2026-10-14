//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

// A real AgentRun in A supplies an existing B ArtifactRef through its actual
// captured ToolCall. The original Adapter rejects that project mismatch before
// the domain read. Failure Operation/Attempt/result records remain legitimate;
// the Artifact/Object business state and successful read audit must not change.
func TestM2ArtifactSourceMismatch(t *testing.T) {
	w, _ := newM1WorkflowWithOptions(t, nil, true)
	v := w.app
	w.seed(t)
	w.startRunner(t)
	const privateContent = "Project B content must never enter Project A's model history.\n"
	source := artifactWebCreate(t, v.workAppFixture, v.assembly.artifacts.pair.Service(), v.actor, v.b, "source-only.txt", "text/plain", privateContent)
	ref := source.Details().Reference
	var raw string
	workAppCheck(t, "m2-source-read-registration", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:read-artifact'`).Scan(&raw))
	tool, err := f.ParseID[id.Tool](raw)
	workAppCheck(t, "m2-source-read-tool", err)
	w.tools["builtin:read-artifact"] = tool
	const final = "The cross-project Artifact read was refused. No source content was read."
	w.responseSequence = func(t *testing.T) []netfixture.WireScenario {
		response := map[string]any{"id": "owned-m2-source-rejected", "object": "chat.completion", "created": 1, "model": "m1-statistics", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": final}, "finish_reason": "stop"}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}}
		return []netfixture.WireScenario{m1FailureResponse(t, w, "builtin:read-artifact", map[string]any{"artifact_ref": ref}), {Status: http.StatusOK, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{workAppJSON(t, response)}}}
	}
	w.createModelAndTask(t, "https://fixture.test:8443/m2-source-not-yet-scheduled")
	// Reuse the existing complete Artifact/Object snapshot on this same App.
	view := &humanArtifactApp{workAppFixture: v.workAppFixture}
	beforeA, beforeB := view.snapshot(t, v.a), view.snapshot(t, v.b)
	v.store.releaseDirectory()
	ctx, cancel := context.WithTimeout(v.ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		var count, terminal, succeeded int
		workAppCheck(t, "m2-source-original-execution", v.admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE status IN('succeeded','failed','cancelled')),count(*) FILTER(WHERE status='succeeded'),COALESCE(min(id),'') FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), w.task.ID.String()).Scan(&count, &terminal, &succeeded, &raw))
		if count > 1 || terminal > succeeded {
			w.diagnoseResult(t, count, raw, "source-mismatch")
			t.Fatal("source rejection did not preserve its original Tool result workflow")
		}
		if count == 1 && succeeded == 1 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("source mismatch did not settle within its original budget")
		case <-tick.C:
		}
	}
	w.execution, err = f.ParseID[id.Execution](raw)
	workAppCheck(t, "m2-source-execution-id", err)
	var arguments, body, message, next []byte
	var paired bool
	workAppCheck(t, "m2-source-original-denial", v.admin.QueryRow(ctx, `SELECT a.original,b.body_data,t.message,r.content,
 o.project_id=$2 AND o.agent_id=$3 AND o.operation_kind='artifact.read' AND o.phase='failed' AND o.outcome='error'
 AND p.phase='failed' AND p.retired AND q.result_digest=b.body_sha256 AND q.message_digest=t.digest
 FROM agenteam_execution.tool_calls c
 JOIN agenteam_execution.tool_call_arguments a ON (a.execution_id,a.round_id,a.id)=(c.execution_id,c.round_id,c.payload_id)
 JOIN agenteam_tool.operations o ON o.execution_id=c.execution_id AND o.invocation_id=c.invocation_id AND o.call_id=c.call_id AND o.logical_call_id=a.logical_call_id AND o.tool_id=c.tool_id
 JOIN agenteam_tool.attempts p ON p.attempt_id=o.active_attempt_id AND p.operation_id=o.operation_id
 JOIN agenteam_tool.builtin_results b ON (b.execution_id,b.operation_id,b.attempt_id,b.request_id)=(o.execution_id,o.operation_id,p.attempt_id,p.backend_request_id)
 JOIN agenteam_execution.loop_tool_results q ON (q.execution_id,q.operation_id,q.attempt_id,q.request_id)=(b.execution_id,b.operation_id,b.attempt_id,b.request_id)
 JOIN agenteam_execution.transcript_entries t ON (t.execution_id,t.round_id,t.sequence)=(q.execution_id,q.round_id,q.transcript_sequence)
 JOIN agenteam_execution.rounds r ON r.execution_id=c.execution_id AND r.predecessor_round_id=c.round_id
 WHERE c.execution_id=$1`, w.execution.String(), v.a.String(), v.agent.String()).Scan(&arguments, &body, &message, &next, &paired))
	var selected struct {
		Reference json.RawMessage `json:"artifact_ref"`
	}
	workAppCheck(t, "m2-source-original-ref", json.Unmarshal(arguments, &selected))
	if !paired || !m1LiveJSONEqual(selected.Reference, workAppJSON(t, ref)) || !bytes.Equal(body, []byte(`{"error":{"code":"FORBIDDEN"}}`)) {
		t.Fatal("source mismatch lost its original Ref or closed Forbidden result")
	}
	var result mc.Message
	workAppCheck(t, "m2-source-denial-transcript", json.Unmarshal(message, &result))
	if result.Validate() != nil || len(result.Parts) != 1 || result.Parts[0].ToolResult == nil || !result.Parts[0].ToolResult.IsError || len(result.Parts[0].ToolResult.Parts) != 1 || result.Parts[0].ToolResult.Parts[0].Text == nil || result.Parts[0].ToolResult.Parts[0].Text.Text != string(body) || bytes.Contains(next, []byte(privateContent)) || bytes.Contains(next, workAppJSON(t, privateContent)) {
		t.Fatal("source content leaked through the denied Tool response")
	}
	round, err := ec.DecodeLoopRound(next)
	workAppCheck(t, "m2-source-denial-consumption", err)
	messages := round.Fields().Round.Messages
	if len(messages) == 0 || !reflect.DeepEqual(messages[len(messages)-1], result) {
		t.Fatal("successor did not consume the exact persisted denial")
	}
	var operations, attempts, artifacts, rounds int
	workAppCheck(t, "m2-source-bounded-facts", v.admin.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_tool.attempts WHERE execution_id=$1),
 (SELECT count(*) FROM agenteam_artifact.artifacts WHERE project_id=$2::uuid),
 (SELECT count(*) FROM agenteam_execution.rounds WHERE execution_id=$1)`, w.execution.String(), v.a.String()).Scan(&operations, &attempts, &artifacts, &rounds))
	if operations != 1 || attempts != 1 || artifacts != 0 || rounds != 2 || view.snapshot(t, v.a) != beforeA || view.snapshot(t, v.b) != beforeB {
		t.Fatal("source refusal changed Artifact/Object state, leases or successful audit")
	}
	// The legitimate Owner can still read the original B bytes after proving
	// the denied AgentRun created no read lease or successful domain audit.
	read := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, http.MethodGet, artifactWebContentPath(ref), http.StatusOK)
	var actual struct {
		Text string `json:"text"`
	}
	workAppCheck(t, "m2-source-original-owner-read", json.Unmarshal(read.body, &actual))
	clear(read.body)
	if actual.Text != privateContent {
		t.Fatal("source mismatch changed the original source bytes")
	}
	m2StopApproval(t, w, 2)
	t.Log("M2_ARTIFACT_SOURCE_MISMATCH actual_agent_run=true actual_foreign_artifact_ref=true original_adapter_project_gate=true forbidden_only=true artifact_object_business_writes=0 successful_domain_reads=0 original_owners_joined=true revocation_not_covered=true")
}
