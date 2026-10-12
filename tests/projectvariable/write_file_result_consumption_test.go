//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Reuse the accepted real Owner/Agent/Preparation/Model/Write/WSS fixture.
// This stage has no Runner or Model producer of its own. Its Store hooks only
// observe the original transaction body and, in one case, lose its COMMIT ack.
type writeResultConsumptionFixture struct {
	operation *operationWriteFileFixture
	terminal  operationWriteFileSQL
	expected  p.WriteFileResult
	before    toolResultConsumptionState
}

func newWriteResultConsumptionFixture(t *testing.T) *writeResultConsumptionFixture {
	t.Helper()
	v := newOperationWriteFileFixture(t)
	v.expectedRequests = 1
	executor, authority, store := v.executor(t, false, false)
	done := startOperationWriteFile(t, executor, v.facts.capture.created.ID, store)
	pending := v.sql(t)
	if pending.phase != "running" || pending.version != 2 || pending.retired || pending.results != 0 {
		t.Fatal("original write Attempt did not reach its real committed-return barrier")
	}
	v.requireWire(t, 0, pending)
	store.resume()
	completed := waitOperationWriteFile(t, done)
	readFileAdapterOK(t, completed.err, "original durable write return")
	terminal := v.sql(t)
	requireOperationWriteFileIdentity(t, pending, terminal)
	v.requireTerminal(t, terminal, true)
	v.requireWire(t, 1, terminal)
	v.readBytes(t, v.data)
	phase, outcome, attempted := completed.result.Status()
	result, present := completed.result.Result()
	if phase != "succeeded" || outcome != "success" || !attempted || !present || result.Path != v.path || result.State != "committed" || result.BytesWritten != p.MaxWriteFileBytes || !result.Durable || result.CompletionCode != nil || completed.result.OperationID().String() != terminal.operation || completed.result.AttemptID().String() != terminal.attempt || string(completed.result.RequestID()) != terminal.request {
		t.Fatal("actual 512KiB write receipt or original identities differ")
	}
	// The stopped dispatch authority is not passed to the historical reader.
	// Keep the actual ToolCallDriver alive until every consumer/reader joins.
	authority.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := authority.Drain(ctx); err != nil || !authority.Joined() {
		t.Fatal("original Write dispatch owner did not actually join", err)
	}
	x := &writeResultConsumptionFixture{operation: v, terminal: terminal, expected: result}
	var err error
	x.before, err = x.read(ctxFor(t), v.facts.capture.v.base.raw)
	readFileAdapterOK(t, err, "independent unconsumed write checkpoint")
	x.requireState(t, x.before, false)
	t.Cleanup(func() {
		v.facts.driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := v.facts.driver.Drain(ctx); err != nil || !v.facts.driver.Joined() || !v.facts.model.Joined() {
			t.Error("original write ToolCall/Model did not actually join")
		}
		x.requireOriginalWork(t)
	})
	return x
}

func (v *writeResultConsumptionFixture) consumer(t *testing.T) *execution.ToolResultConsumer {
	t.Helper()
	facts := v.operation.facts
	reader, err := toolruntime.NewWriteFileResultReader(facts.capture.v.base.tracked, facts.driver)
	readFileAdapterOK(t, err, "real historical Write receipt reader")
	consumer, err := execution.NewWriteFileResultConsumer(facts.driver, reader)
	readFileAdapterOK(t, err, "explicit Write result consumer")
	t.Cleanup(func() { stopToolResultConsumer(t, consumer) })
	return consumer
}

func (v *writeResultConsumptionFixture) revokeDispatch(t *testing.T) {
	t.Helper()
	facts, b := v.operation.facts, v.operation.facts.capture.v.base
	current, err := facts.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, facts.capture.v.agentID)
	readFileAdapterOK(t, err, "ordinary Owner current configuration before revocation")
	version := current.Fields().Core.Version
	tools, mounts := []i.ToolID{}, []ac.RunnerMountDeclaration{}
	change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{AllowedToolIDs: &tools, RunnerMounts: &mounts})
	readFileAdapterOK(t, err, "formal Tool and Mount revocation input")
	changed, err := facts.agents.UpdateAgent(ctxFor(t), b.ownerBrowser.actor, meta(t, "write-result-revoke-dispatch", &version), b.project.ID, facts.capture.v.agentID, change)
	readFileAdapterOK(t, err, "ordinary Owner committed Tool and Mount revocation")
	if changed.Validate() != nil || changed.Fields().Agent.Fields().Core.Version != version+1 || len(changed.Fields().Agent.Fields().AllowedToolIDs) != 0 || len(changed.Fields().Agent.Fields().AllowedMountIDs) != 0 || changed.Fields().RunnerMounts == nil || len(*changed.Fields().RunnerMounts) != 0 {
		t.Fatal("formal revocation left dispatch authorization")
	}
	actual, err := facts.agents.GetAgent(ctxFor(t), b.ownerBrowser.actor, b.project.ID, facts.capture.v.agentID)
	readFileAdapterOK(t, err, "independent current Owner read after revocation")
	if actual.Fields().Core.Version != version+1 || len(actual.Fields().AllowedToolIDs) != 0 || len(actual.Fields().AllowedMountIDs) != 0 {
		t.Fatal("current configuration did not retain the committed revocation")
	}
	v.requireOriginalWork(t)
}

func (v *writeResultConsumptionFixture) requireOriginalWork(t *testing.T) {
	t.Helper()
	actual := v.operation.sql(t)
	if !reflect.DeepEqual(actual, v.terminal) {
		t.Fatal("receipt consumption changed original Operation/Attempt/result facts")
	}
	v.operation.requireTerminal(t, actual, true)
	v.operation.requireWire(t, 1, actual)
	_, _, begin, result, closes, joined := v.operation.facts.model.observed()
	if begin != 1 || result != 1 || closes != 1 || !joined {
		t.Fatal("new consumption or projection repeated Model work")
	}
}

func (v *writeResultConsumptionFixture) matches(cause f.TransactionCause) bool {
	d := cause.Details()
	return d.Kind == f.JobCause && d.JobType == "execution.tool-result.consume" && d.JobID == v.terminal.execution && d.JobAttemptID == v.terminal.attempt
}

// The shared state DTO carries safe counters and in-memory canonical bytes.
// Its SQL here is independent of the old read-only fixture and original
// ToolCall checkpoint-version join, which is no longer true after consumption.
func (v *writeResultConsumptionFixture) read(ctx context.Context, x postgres.SQLExecutor) (toolResultConsumptionState, error) {
	var out toolResultConsumptionState
	err := x.QueryRow(ctx, `SELECT e.status,e.version,r.id,c.call_id,r.tool_pending,r.transcript_through,
 (e.cancel_requested_at IS NULL),(r.terminal_status IS NULL),
 (SELECT count(*) FROM agenteam_execution.rounds z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.tool_calls z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.tool_call_arguments z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.transcript_entries z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.tool_result_consumptions z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_outbox.events z WHERE z.aggregate_id=e.id::uuid AND z.event_type IN('execution.succeeded','execution.failed','execution.cancelled')),
 (SELECT count(*) FROM agenteam_mount.execution_use_retirements z WHERE z.execution_id=e.id),
 (SELECT count(*) FROM agenteam_model.snapshot_bindings z JOIN agenteam_secret.secret_leases l ON l.id=z.lease_id WHERE z.owner_kind='execution' AND z.owner_id::text=e.id AND NOT l.released),
 (SELECT count(*) FROM agenteam_projectvariable.execution_secret_references z WHERE z.execution_id=e.id),
 row_to_json(s)::text,row_to_json(r)::text,row_to_json(c)::text,row_to_json(a)::text,
 (SELECT json_agg(z ORDER BY z.sequence)::text FROM agenteam_execution.transcript_entries z WHERE z.execution_id=e.id AND z.sequence IN(1,2)),
 COALESCE(k.operation_id,''),COALESCE(k.attempt_id,''),COALESCE(k.request_id,''),
 COALESCE(k.input_digest,''),COALESCE(k.fingerprint,''),COALESCE(k.terminal_digest,''),COALESCE(k.result_digest,''),COALESCE(k.result_bytes,0),
 COALESCE(k.binding,''::bytea),COALESCE(k.binding_digest,''),COALESCE(k.message_digest,''),COALESCE(k.transcript_sequence,0),COALESCE(k.checkpoint_version,0),COALESCE(k.created_at,TIMESTAMPTZ 'epoch'),
 COALESCE(t.message,''::bytea),COALESCE(t.kind,''),COALESCE(e.xmin=k.xmin AND k.xmin=t.xmin AND k.created_at=t.created_at AND k.message_digest=t.digest
  AND k.round_id=r.id AND k.invocation_id=c.invocation_id AND k.call_id=c.call_id AND t.round_id=r.id,false),
 json_build_object(
 'project_id',e.project_id,'agent_id',e.agent_id,'execution_id',e.id,'round_id',r.id,'snapshot_id',s.id,'input_binding_id',r.input_binding_id,
 'logical_call_id',r.call_id,'invocation_id',c.invocation_id,'call_id',c.call_id,'model_visible_name',c.model_visible_name,'tool_id',c.tool_id,'spec_revision',c.spec_revision,
 'operation_name',c.operation_name,'operation_revision',c.operation_revision,'protocol_major',c.protocol_major,
 'payload_id',a.id,'payload_digest',a.original_digest,'payload_bytes',octet_length(a.original),'canonical_arguments',a.canonical_digest,
 'operation_id',o.operation_id,'attempt_id',u.attempt_id,'request_id',u.backend_request_id,'input_digest',o.input_digest,'fingerprint',o.fingerprint,
 'terminal_digest','sha256:'||encode(sha256(o.terminal_data),'hex'),'outcome','success','code','','attempted',true,'result_digest',z.body_sha256,'result_bytes',z.body_bytes)::text
 FROM agenteam_execution.executions e
 JOIN agenteam_execution.snapshots s ON(s.execution_id,s.id)=(e.id,e.snapshot_id)
 JOIN agenteam_execution.rounds r ON(r.execution_id,r.snapshot_id)=(e.id,s.id)
 JOIN agenteam_execution.tool_calls c ON(c.execution_id,c.round_id)=(e.id,r.id)
 JOIN agenteam_execution.tool_call_arguments a ON(a.execution_id,a.round_id,a.id)=(e.id,r.id,c.payload_id)
 JOIN agenteam_tool.operations o ON o.execution_id=e.id AND o.operation_kind='runner.write-file'
 JOIN agenteam_tool.attempts u ON(u.operation_id,u.attempt_id)=(o.operation_id,o.active_attempt_id)
 JOIN agenteam_tool.write_file_results z ON(z.operation_id,z.attempt_id)=(o.operation_id,u.attempt_id)
 LEFT JOIN agenteam_execution.tool_result_consumptions k ON k.execution_id=e.id
 LEFT JOIN agenteam_execution.transcript_entries t ON t.execution_id=e.id AND t.sequence=3
 WHERE e.id=$1`, v.terminal.execution).Scan(
		&out.status, &out.version, &out.round, &out.call, &out.pending, &out.through, &out.live, &out.roundOpen,
		&out.rounds, &out.calls, &out.arguments, &out.transcripts, &out.consumptions, &out.terminals, &out.retirements, &out.modelLeases, &out.environmentLeases,
		&out.immutable[0], &out.immutable[1], &out.immutable[2], &out.immutable[3], &out.immutable[4],
		&out.operation, &out.attempt, &out.request, &out.inputDigest, &out.fingerprint, &out.terminalDigest, &out.resultDigest, &out.resultBytes,
		&out.binding, &out.bindingDigest, &out.messageDigest, &out.sequence, &out.checkpoint, &out.created, &out.message, &out.kind, &out.sameTx, &out.expectedBinding)
	return out, err
}

func (v *writeResultConsumptionFixture) validateState(got toolResultConsumptionState, consumed bool) error {
	beforeVersion := int64(v.operation.facts.capture.created.Version) + 3
	if got.status != "running" || !got.live || !got.roundOpen || !got.pending || got.through != 2 || got.rounds != 1 || got.calls != 1 || got.arguments != 1 || got.terminals != 0 || got.retirements != 0 || got.modelLeases != 1 || got.environmentLeases != 1 {
		return errors.New("result checkpoint changed the pending Round, current execution or live references")
	}
	if v.before.round != "" && (got.round != v.before.round || got.call != v.before.call || got.immutable != v.before.immutable || !bytes.Equal(got.expectedBinding, v.before.expectedBinding)) {
		return errors.New("result consumption changed original Snapshot, Round, ToolCall, protected arguments or earlier transcript")
	}
	if !consumed {
		if got.version != beforeVersion || got.transcripts != 2 || got.consumptions != 0 || got.operation != "" || got.attempt != "" || got.sequence != 0 || got.checkpoint != 0 || len(got.binding) != 0 || len(got.message) != 0 || got.sameTx {
			return errors.New("unconsumed or rolled-back checkpoint contains partial facts")
		}
		return nil
	}
	if got.version != beforeVersion+1 || got.checkpoint != got.version || got.sequence != 3 || got.transcripts != 3 || got.consumptions != 1 || got.kind != "tool_result" || !got.sameTx || got.operation != v.terminal.operation || got.attempt != v.terminal.attempt || got.request != v.terminal.request || got.inputDigest != v.terminal.inputDigest || got.fingerprint != v.terminal.fingerprint || got.terminalDigest != toolResultConsumptionDigest(v.terminal.terminal) || got.resultDigest != v.terminal.resultDigest || got.resultBytes != v.terminal.resultBytes || got.bindingDigest != toolResultConsumptionDigest(got.binding) || got.messageDigest != toolResultConsumptionDigest(got.message) {
		return errors.New("consumption did not bind one original result, transcript and Execution version in the same transaction")
	}
	var binding, expected map[string]any
	if json.Unmarshal(got.binding, &binding) != nil || json.Unmarshal(got.expectedBinding, &expected) != nil || !reflect.DeepEqual(binding, expected) {
		return errors.New("consumption binding differs from original canonical Call, Operation, Attempt or payload facts")
	}
	canonical, err := cursor.CanonicalJSON(got.message)
	if err != nil || !bytes.Equal(canonical, got.message) {
		return errors.New("result transcript is not canonical JSON")
	}
	var message mc.Message
	decoder := json.NewDecoder(bytes.NewReader(got.message))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&message) != nil || message.Validate() != nil || message.Role != "tool" || len(message.Parts) != 1 || message.Parts[0].ToolResult == nil {
		return errors.New("result transcript is not the exact model-visible tool message")
	}
	result := message.Parts[0].ToolResult
	want, err := p.EncodeWriteFileResult(v.expected)
	if err != nil || result.CallID != got.call || result.IsError || len(result.Parts) != 1 || result.Parts[0].Text == nil || result.Parts[0].Text.Text != string(want) || !bytes.Equal(want, v.terminal.result) {
		return errors.New("result transcript replaced the original complete committed write receipt")
	}
	// Only the new metadata receipt excludes file data. The original seq2
	// assistant call, protected args and prefix remain byte-for-byte intact.
	var fields map[string]json.RawMessage
	if json.Unmarshal(want, &fields) != nil || len(fields) != 4 {
		return errors.New("the actual durable receipt gained or lost metadata fields")
	}
	for _, key := range []string{"path", "state", "bytes_written", "durable"} {
		if _, present := fields[key]; !present {
			return errors.New("the actual metadata receipt omitted a required field")
		}
	}
	return nil
}

func (v *writeResultConsumptionFixture) requireState(t *testing.T, got toolResultConsumptionState, consumed bool) {
	t.Helper()
	if err := v.validateState(got, consumed); err != nil {
		t.Fatal(err)
	}
}

func (v *writeResultConsumptionFixture) requireDurable(t *testing.T, consumed bool) toolResultConsumptionState {
	t.Helper()
	got, err := v.read(ctxFor(t), v.operation.facts.capture.v.base.raw)
	readFileAdapterOK(t, err, "independent original consumption observation")
	v.requireState(t, got, consumed)
	return got
}

func (v *writeResultConsumptionFixture) requireReceipt(t *testing.T, receipt ec.ToolResultConsumptionReceipt, actual toolResultConsumptionState) {
	t.Helper()
	if receipt.Validate() != nil || receipt.ExecutionID.String() != v.terminal.execution || receipt.RoundID.String() != actual.round || receipt.CallID != actual.call || receipt.OperationID.String() != actual.operation || receipt.AttemptID.String() != actual.attempt || int64(receipt.Version) != actual.checkpoint || receipt.TranscriptThrough != 3 || string(receipt.MessageDigest) != actual.messageDigest || string(receipt.ResultDigest) != actual.resultDigest || !receipt.CreatedAt.Time().Equal(actual.created) {
		t.Fatal("public consumption receipt differs from the original durable checkpoint")
	}
}

func TestWriteFileResultConsumption(t *testing.T) {
	t.Run("revoked-dispatch-grants-consume-and-project", func(t *testing.T) {
		v := newWriteResultConsumptionFixture(t)
		v.revokeDispatch(t)
		if !reflect.DeepEqual(v.requireDurable(t, false), v.before) {
			t.Fatal("formal current revocation changed immutable unconsumed facts")
		}
		consumer := v.consumer(t)
		e := v.operation.facts.capture.created.ID
		receipt, err := consumer.Consume(ctxFor(t), e)
		readFileAdapterOK(t, err, "first historical write consumption after current revocation")
		committed := v.requireDurable(t, true)
		v.requireReceipt(t, receipt, committed)
		observed, found, err := consumer.Lookup(ctxFor(t), e)
		readFileAdapterOK(t, err, "historical receipt Lookup after current revocation")
		if !found || observed != receipt || !reflect.DeepEqual(v.requireDurable(t, true), committed) {
			t.Fatal("receipt Lookup changed the one consumed write checkpoint")
		}
		v.determineAndProject(t, receipt, committed)
		v.requireOriginalWork(t)
		t.Log("write_result_consumption original_write_bytes=524288 original_wire_requests=1 original_model_calls=1 formal_tool_mount_revoked=true first_consume_sequence=3 original_receipt_metadata_preserved=true determined_projection_read_only=true")
	})
	t.Run("commit-loss-lookup-no-resend", func(t *testing.T) {
		v := newWriteResultConsumptionFixture(t)
		consumer := v.consumer(t)
		e := v.operation.facts.capture.created.ID
		store := v.operation.facts.capture.v.base.tracked
		var candidate toolResultConsumptionState
		seen, consumeTransactions := false, 0
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if !v.matches(cause) {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			candidate, err = v.read(ctx, x)
			if err != nil {
				return err
			}
			if err = v.validateState(candidate, true); err != nil {
				return err
			}
			seen = true
			return nil
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, _ f.CommitResult) {
			if v.matches(cause) {
				consumeTransactions++
			}
		}
		store.mu.Unlock()
		originalStore := store.fixtureStore
		loss := &captureCommitReceiptLoss{fixtureStore: originalStore, match: v.matches, attempt: id[f.TransactionAttempt](t)}
		store.fixtureStore = loss
		t.Cleanup(func() {
			roundInputClearHooks(store)
			store.fixtureStore = originalStore
		})
		absent, err := consumer.Consume(ctxFor(t), e)
		requireCode(t, err, f.CommitUnknown)
		original, retained := execution.UnknownAttempt(err)
		fired, physical := loss.observed()
		if !seen || consumeTransactions != 1 || absent != (ec.ToolResultConsumptionReceipt{}) || !fired || physical.State() != f.Committed || !retained || original.State() != f.Unknown || original.AttemptID() != loss.attempt || !v.matches(original.Cause()) {
			t.Fatal("receipt loss did not follow the original fully written consumption COMMIT")
		}
		committed := v.requireDurable(t, true)
		if !reflect.DeepEqual(candidate, committed) {
			t.Fatal("physical COMMIT differs from the complete original candidate")
		}
		v.revokeDispatch(t)
		// No second Consume or Execute after Unknown. The first Lookup observes
		// the original candidate; a new consumer then reads the durable receipt.
		observed, found, err := consumer.Lookup(ctxFor(t), e)
		readFileAdapterOK(t, err, "original unknown Write consumption Lookup after revocation")
		if !found {
			t.Fatal("original Lookup did not observe the committed candidate")
		}
		v.requireReceipt(t, observed, committed)
		stopToolResultConsumer(t, consumer)
		recovered := v.consumer(t)
		again, found, err := recovered.Lookup(ctxFor(t), e)
		readFileAdapterOK(t, err, "new consumer historical receipt recovery")
		if !found || again != observed || consumeTransactions != 1 || !reflect.DeepEqual(v.requireDurable(t, true), committed) {
			t.Fatal("historical recovery repeated or changed the original consumption")
		}
		v.requireReceipt(t, again, committed)
		v.requireOriginalWork(t)
		t.Log("write_result_consumption physical_consume_commit_then_ack_loss=true original_and_new_consumer_lookup=true consumption_count=1 transcript_sequence=3 original_operation_attempt_rid_unchanged=true wire_requests=1 model_calls=1")
	})
}

type writeResultRoundState struct {
	version, inputs, controls, dispositions, ingress int64
	id, digest, binding                              string
	through, checkpoint                              int64
	content                                          []byte
	sameTx                                           bool
}

func (v *writeResultConsumptionFixture) roundState(t *testing.T) writeResultRoundState {
	t.Helper()
	var out writeResultRoundState
	b := v.operation.facts.capture.v.base
	err := b.raw.QueryRow(ctxFor(t), `SELECT e.version,
 (SELECT count(*) FROM agenteam_execution.round_inputs x WHERE x.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.control_inputs x WHERE x.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.skill_assignment_dispositions x WHERE x.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.skill_assignment_ingress x WHERE x.execution_id=e.id),
 COALESCE(q.id,''),COALESCE(q.digest,''),COALESCE(q.skill_input_binding_id,''),
 COALESCE(q.transcript_through,0),COALESCE(q.checkpoint_version,0),COALESCE(q.content,''::bytea),
 COALESCE(q.xmin=e.xmin AND q.project_id=e.project_id AND q.agent_id=e.agent_id
  AND q.snapshot_id=e.snapshot_id AND q.predecessor_round_id=k.round_id
  AND q.checkpoint_version=e.version AND q.checkpoint_version=k.checkpoint_version+1,false)
 FROM agenteam_execution.executions e
 LEFT JOIN agenteam_execution.tool_result_consumptions k ON k.execution_id=e.id
 LEFT JOIN agenteam_execution.round_inputs q ON q.execution_id=e.id
 WHERE e.id=$1`, v.terminal.execution).Scan(&out.version, &out.inputs, &out.controls, &out.dispositions, &out.ingress, &out.id, &out.digest, &out.binding, &out.through, &out.checkpoint, &out.content, &out.sameTx)
	readFileAdapterOK(t, err, "independent Write successor input facts")
	return out
}

func (v *writeResultConsumptionFixture) determineAndProject(t *testing.T, receipt ec.ToolResultConsumptionReceipt, consumed toolResultConsumptionState) {
	t.Helper()
	facts, c := v.operation.facts, v.operation.facts.capture
	before := v.roundState(t)
	if before.version != consumed.version || before.inputs != 0 || before.controls != 0 || before.dispositions != 0 || before.ingress != 0 || before.id != "" || len(before.content) != 0 {
		t.Fatal("Write-only successor fixture already contains extra round/control facts")
	}
	initial, err := skill.NewInitialRoundBindings(c.v.agent.providers.SkillAuthority)
	readFileAdapterOK(t, err, "actual historical initial Skill capture reader")
	assignments, err := skill.NewAssignmentValidation(c.v.agent.providers.SkillAuthority)
	readFileAdapterOK(t, err, "actual current Skill assignment validator")
	determiner, err := execution.NewRoundInputDeterminer(c.v.base.tracked, c.authority, execution.RoundInputDependencies{Projects: c.projectAuthority, Agents: c.configuration, InitialSkills: initial, Assignments: assignments})
	readFileAdapterOK(t, err, "actual successor input determiner")
	t.Cleanup(func() {
		determiner.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := determiner.Drain(ctx); err != nil || !determiner.Joined() {
			t.Error("original Write successor input calls did not actually join")
		}
	})
	next := id[ec.Round](t)
	input, err := determiner.Determine(ctxFor(t), c.created.ID, next)
	readFileAdapterOK(t, err, "real determined Write successor input after revocation")
	q := input.Fields()
	initialFields := facts.input.Fields().Skills
	if input.Validate() != nil || q.Source != receipt || q.ExecutionID != c.created.ID || q.RoundID != next || q.PredecessorRoundID.String() != consumed.round || q.SnapshotID.String() != v.operation.checkpoint.Snapshot || q.CheckpointVersion != receipt.Version+1 || q.TranscriptThrough != 3 || q.Control != nil || len(q.AppliedChangeIDs) != 0 || len(q.Dispositions) != 0 || q.CompactionRef != nil || !reflect.DeepEqual(q.Initial, initialFields) || !reflect.DeepEqual(q.Bindings, initialFields.Bindings) || q.AssignmentCutoff != initialFields.AssignmentSequence {
		t.Fatal("successor input changed original receipt/capture or invented pending work")
	}
	committed := v.roundState(t)
	if committed.inputs != 1 || committed.controls != 0 || committed.dispositions != 0 || committed.ingress != 0 || !committed.sameTx || committed.version != consumed.version+1 || committed.checkpoint != committed.version || committed.through != 3 || committed.id != next.String() || committed.binding != q.SkillInputBindingID.String() || committed.digest != string(input.Digest()) || !bytes.Equal(committed.content, input.CanonicalBytes()) {
		t.Fatal("actual next input was not committed with its one Execution version change")
	}
	after, err := v.read(ctxFor(t), c.v.base.raw)
	readFileAdapterOK(t, err, "post-Determine original consumption facts")
	if after.version != consumed.version+1 || after.sameTx {
		t.Fatal("Determine did not use its separate original checkpoint transaction")
	}
	stable := after
	stable.version, stable.sameTx = consumed.version, consumed.sameTx
	if !reflect.DeepEqual(stable, consumed) {
		t.Fatal("Determine changed the original consumption, pending Model round or seq1-3")
	}
	projector, err := agentloop.NewRoundProjector(determiner)
	readFileAdapterOK(t, err, "real committed Write projection reader")
	// Reuse only the existing actual transaction read-only observation helper;
	// it needs facts, not the old read-file producer or its fixture assertions.
	observation := &roundInputFixture{facts: facts}
	project := func() agentloop.RoundProjection {
		var got agentloop.RoundProjection
		nextProjectionReadOnly(t, observation, next, func() error {
			var err error
			got, err = projector.Project(ctxFor(t), c.created.ID, next)
			return err
		})
		v.requireProjection(t, input, got)
		current, err := v.read(ctxFor(t), c.v.base.raw)
		readFileAdapterOK(t, err, "independent facts after historical Write projection")
		if !reflect.DeepEqual(current, after) || !reflect.DeepEqual(v.roundState(t), committed) {
			t.Fatal("read-only projection changed durable facts")
		}
		v.requireOriginalWork(t)
		return got
	}
	first, again := project(), project()
	if !bytes.Equal(first.CanonicalBytes(), again.CanonicalBytes()) || first.Digest() != again.Digest() {
		t.Fatal("same committed Write input changed its historical projection")
	}
}

func (v *writeResultConsumptionFixture) requireProjection(t *testing.T, input ec.RoundInput, got agentloop.RoundProjection) {
	t.Helper()
	q := input.Fields()
	if got.Validate() != nil || got.ExecutionID() != q.ExecutionID || got.RoundID() != q.RoundID || got.SourceDigest() != input.Digest() || string(got.Digest()) != toolResultConsumptionDigest(got.CanonicalBytes()) {
		t.Fatal("projection lost the committed Write input identity")
	}
	var roundRaw []byte
	b := v.operation.facts.capture.v.base
	readFileAdapterOK(t, b.raw.QueryRow(ctxFor(t), `SELECT content FROM agenteam_execution.rounds WHERE execution_id=$1 AND id=$2`, q.ExecutionID.String(), q.PredecessorRoundID.String()).Scan(&roundRaw), "original persisted Write Model round")
	old, err := ec.DecodeToolCallRound(roundRaw)
	readFileAdapterOK(t, err, "original canonical Write Model round")
	messages := got.Messages()
	if len(messages) != 5 || len(got.Tools()) != 1 || q.Control != nil {
		t.Fatal("Write projection changed its no-control message shape")
	}
	if !bytes.Equal(nextProjectionJSON(t, messages[0]), nextProjectionJSON(t, old.Fields().Round.Messages[0])) || !bytes.Equal(nextProjectionJSON(t, got.Tools()[0]), nextProjectionJSON(t, old.Fields().Tool)) {
		t.Fatal("Write projection changed the original system or captured Tool schema")
	}
	for n, role := range []string{"system", "user", "assistant", "tool", "user"} {
		if messages[n].Role != role || messages[n].Validate() != nil {
			t.Fatal("Write projection changed complete message order")
		}
	}
	var prefix []json.RawMessage
	for n := 1; n <= 3; n++ {
		var raw []byte
		readFileAdapterOK(t, b.raw.QueryRow(ctxFor(t), `SELECT message FROM agenteam_execution.transcript_entries WHERE execution_id=$1 AND sequence=$2 AND round_id=$3`, q.ExecutionID.String(), n, q.PredecessorRoundID.String()).Scan(&raw), "independent original canonical transcript")
		if !bytes.Equal(nextProjectionJSON(t, messages[n]), raw) {
			t.Fatal("projection replaced an original complete canonical message")
		}
		prefix = append(prefix, json.RawMessage(raw))
	}
	prefixBytes, err := json.Marshal(prefix)
	readFileAdapterOK(t, err, "complete canonical transcript prefix")
	if q.TranscriptPrefixDigest != ec.TriggerInputDigest(prefixBytes) {
		t.Fatal("Write projection prefix no longer binds the complete original args")
	}
	// The accepted historical policy retains the original seq2 assistant args,
	// including data. Only the newly appended ToolResult is metadata-only.
	var originalCalls int
	for _, part := range messages[2].Parts {
		if part.ToolCall != nil {
			originalCalls++
			if part.ToolCall.ID != q.Source.CallID || !bytes.Equal(part.ToolCall.Arguments, v.operation.expectedCanonical) {
				t.Fatal("projection changed the original protected Write call arguments")
			}
		}
	}
	if originalCalls != 1 || len(messages[3].Parts) != 1 || messages[3].Parts[0].ToolResult == nil {
		t.Fatal("Write projection lacks its exact original call/result pair")
	}
	result := messages[3].Parts[0].ToolResult
	if result.CallID != q.Source.CallID || result.IsError || len(result.Parts) != 1 || result.Parts[0].Text == nil {
		t.Fatal("known committed Write receipt was recast as a Tool error")
	}
	raw := []byte(result.Parts[0].Text.Text)
	report, err := p.DecodeWriteFileResult(raw)
	readFileAdapterOK(t, err, "projected original Write receipt")
	if !reflect.DeepEqual(report, v.expected) || !bytes.Equal(raw, v.terminal.result) {
		t.Fatal("projection changed path/state/bytes/durable or optional completion code")
	}
	catalog := make([]any, 0, len(q.Bindings))
	for _, binding := range q.Bindings {
		catalog = append(catalog, nextProjectionSkill(binding))
	}
	effective := nextProjectionJSON(t, map[string]any{"assignment_cutoff": q.AssignmentCutoff, "skills": catalog})
	wantCatalog := mc.Message{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "Effective Skill catalog for this round (metadata only; not a current access grant):\n" + string(effective)}}}}
	if !bytes.Equal(nextProjectionJSON(t, messages[4]), nextProjectionJSON(t, wantCatalog)) {
		t.Fatal("projection changed the exact final effective Skill directory")
	}
	want := nextProjectionJSON(t, map[string]any{"projection_version": f.Version(1), "execution_id": q.ExecutionID, "round_id": q.RoundID, "round_input_digest": input.Digest(), "messages": messages, "tools": got.Tools()})
	if !bytes.Equal(got.CanonicalBytes(), want) {
		t.Fatal("Write projection envelope differs from its checked complete content")
	}
}
