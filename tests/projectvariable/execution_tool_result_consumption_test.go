//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// The original fixture supplies real Account/Owner configuration, Model-input
// capture, ToolCall production, Tool Operation/Attempt, WSS and Workspace I/O.
// Only its Model JSONCaller is controlled. This file adds no canonical seed,
// substitute authority, Runner response or second Model call.
type toolResultConsumptionFixture struct {
	operation *operationReadFileFixture
	terminal  operationReadFileSQL
	before    toolResultConsumptionState
}

func newToolResultConsumptionFixture(t *testing.T) *toolResultConsumptionFixture {
	t.Helper()
	v := newOperationReadFileFixture(t, true)
	executor, authority, store := v.executor(t, false)
	done := startOperationReadFile(t, executor, v.facts.capture.created.ID, store)
	pending := v.sql(t)
	if pending.phase != "running" || pending.version != 2 || pending.retired || pending.results != 0 {
		t.Fatal("original Attempt did not reach its real committed return barrier")
	}
	v.requireWire(t, 0, pending)
	store.resume()
	completed := waitOperationReadFile(t, done)
	readFileAdapterOK(t, completed.err, "original successful read-file return")
	terminal := v.sql(t)
	requireOperationReadFileIdentity(t, pending, terminal)
	v.requireTerminal(t, terminal, true)
	v.requireWire(t, 1, terminal)
	phase, outcome, attempted := completed.result.Status()
	result, present := completed.result.Result()
	if phase != "succeeded" || outcome != "success" || !attempted || !present || result != v.expected || result.BytesRead != p.MaxReadFileBytes || len(result.Data) != p.MaxReadFileBytes || completed.result.OperationID().String() != terminal.operation || completed.result.AttemptID().String() != terminal.attempt || string(completed.result.RequestID()) != terminal.request {
		t.Fatal("original completed call did not return the full durable 64KiB result")
	}
	// Consumption must work after the original Tool dispatch owner has actually
	// joined. The original Execution/Model/environment references remain live.
	authority.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := authority.Drain(ctx); err != nil || !authority.Joined() {
		t.Fatal("original Tool dispatch owner did not join", err)
	}
	x := &toolResultConsumptionFixture{operation: v, terminal: terminal}
	var err error
	x.before, err = x.read(ctxFor(t), v.facts.capture.v.base.raw)
	readFileAdapterOK(t, err, "original unconsumed checkpoint")
	x.requireState(t, x.before, false)
	t.Cleanup(func() {
		v.facts.driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := v.facts.driver.Drain(ctx); err != nil || !v.facts.driver.Joined() || !v.facts.model.Joined() {
			t.Error("original ToolCall driver and Model handle did not actually join")
		}
		// The old ToolFacts helper intentionally requires zero Operations. Use
		// this slice's original identities and counters at the downstream stage.
		x.requireOriginalWork(t)
	})
	return x
}

func (v *toolResultConsumptionFixture) consumer(t *testing.T) *execution.ToolResultConsumer {
	t.Helper()
	facts := v.operation.facts
	reader, err := toolruntime.NewReadFileResultReader(facts.capture.v.base.tracked, facts.driver)
	readFileAdapterOK(t, err, "real historical Tool result reader")
	consumer, err := execution.NewToolResultConsumer(facts.driver, reader)
	readFileAdapterOK(t, err, "explicit Execution result consumer")
	t.Cleanup(func() { stopToolResultConsumer(t, consumer) })
	return consumer
}

func stopToolResultConsumer(t *testing.T, consumer *execution.ToolResultConsumer) {
	t.Helper()
	consumer.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := consumer.Drain(ctx); err != nil || !consumer.Joined() {
		t.Error("original result consumer did not actually join", err)
	}
}

func (v *toolResultConsumptionFixture) requireOriginalWork(t *testing.T) {
	t.Helper()
	actual := v.operation.sql(t)
	if !reflect.DeepEqual(actual, v.terminal) {
		t.Fatal("consumption changed the original Operation, Attempt or result")
	}
	v.operation.requireTerminal(t, actual, true)
	v.operation.requireWire(t, 1, actual)
	_, _, begin, result, closeCalls, joined := v.operation.facts.model.observed()
	if begin != 1 || result != 1 || closeCalls != 1 || !joined {
		t.Fatal("consumption repeated Model work or lost its original handle join")
	}
}

func (v *toolResultConsumptionFixture) matches(cause f.TransactionCause) bool {
	d := cause.Details()
	return d.Kind == f.JobCause && d.JobType == "execution.tool-result.consume" && d.JobID == v.terminal.execution && d.JobAttemptID == v.terminal.attempt
}

// Source rows stay in memory and are compared byte for byte; no protected
// arguments or file data are written to logs or test attachments. Execution's
// version advances while the original pending Round and source facts remain.
type toolResultConsumptionState struct {
	status, round, call, operation, attempt, request               string
	inputDigest, fingerprint, terminalDigest, resultDigest         string
	bindingDigest, messageDigest, kind                             string
	version, checkpoint, sequence, resultBytes, through            int64
	rounds, calls, arguments, transcripts, consumptions, terminals int64
	retirements, modelLeases, environmentLeases                    int64
	pending, live, roundOpen, sameTx                               bool
	created                                                        time.Time
	message, binding, expectedBinding                              []byte
	immutable                                                      [5]string
}

func (v *toolResultConsumptionFixture) read(ctx context.Context, x postgres.SQLExecutor) (toolResultConsumptionState, error) {
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
 JOIN agenteam_tool.operations o ON o.execution_id=e.id AND o.operation_kind='runner.read-file'
 JOIN agenteam_tool.attempts u ON(u.operation_id,u.attempt_id)=(o.operation_id,o.active_attempt_id)
 JOIN agenteam_tool.read_file_results z ON(z.operation_id,z.attempt_id)=(o.operation_id,u.attempt_id)
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

func toolResultConsumptionDigest(raw []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}

func (v *toolResultConsumptionFixture) validateState(got toolResultConsumptionState, consumed bool) error {
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
	want, err := p.EncodeReadFileResult(v.operation.expected)
	if err != nil || result.CallID != got.call || result.IsError || len(result.Parts) != 1 || result.Parts[0].Text == nil || result.Parts[0].Text.Text != string(want) || !bytes.Equal(want, v.terminal.result) || len(want) <= 32768 {
		return errors.New("result transcript truncated or replaced the full original 64KiB read-file JSON")
	}
	return nil
}

func (v *toolResultConsumptionFixture) requireState(t *testing.T, got toolResultConsumptionState, consumed bool) {
	t.Helper()
	if err := v.validateState(got, consumed); err != nil {
		t.Fatal(err)
	}
}

func (v *toolResultConsumptionFixture) requireDurable(t *testing.T, consumed bool) toolResultConsumptionState {
	t.Helper()
	got, err := v.read(ctxFor(t), v.operation.facts.capture.v.base.raw)
	readFileAdapterOK(t, err, "independent original consumption observation")
	v.requireState(t, got, consumed)
	return got
}

func (v *toolResultConsumptionFixture) requireReceipt(t *testing.T, receipt ec.ToolResultConsumptionReceipt, actual toolResultConsumptionState) {
	t.Helper()
	if receipt.Validate() != nil || receipt.ExecutionID.String() != v.terminal.execution || receipt.RoundID.String() != actual.round || receipt.CallID != actual.call || receipt.OperationID.String() != actual.operation || receipt.AttemptID.String() != actual.attempt || int64(receipt.Version) != actual.checkpoint || receipt.TranscriptThrough != 3 || string(receipt.MessageDigest) != actual.messageDigest || string(receipt.ResultDigest) != actual.resultDigest || !receipt.CreatedAt.Time().Equal(actual.created) {
		t.Fatal("public consumption receipt differs from the original durable checkpoint")
	}
}

func TestExecutionToolResultConsumption(t *testing.T) {
	t.Run("full-result-once-and-current-binding", func(t *testing.T) {
		v := newToolResultConsumptionFixture(t)
		consumer := v.consumer(t)
		e := v.operation.facts.capture.created.ID
		first, err := consumer.Consume(ctxFor(t), e)
		readFileAdapterOK(t, err, "first real result consumption")
		committed := v.requireDurable(t, true)
		v.requireReceipt(t, first, committed)
		repeated, err := consumer.Consume(ctxFor(t), e)
		readFileAdapterOK(t, err, "duplicate consumption observation")
		v.requireReceipt(t, repeated, committed)
		if !reflect.DeepEqual(v.requireDurable(t, true), committed) {
			t.Fatal("duplicate consumption changed its original checkpoint")
		}
		stopToolResultConsumer(t, consumer)
		recovered := v.consumer(t)
		observed, found, err := recovered.Lookup(ctxFor(t), e)
		readFileAdapterOK(t, err, "new consumer durable recovery")
		if !found {
			t.Fatal("new consumer did not find the original consumption")
		}
		v.requireReceipt(t, observed, committed)
		// Revoke through the real ordinary Owner command. A saved receipt is
		// not current authorization to read or consume the historical payload.
		v.operation.facts.changeTools(t, "tool-result-consumption-revoke", []i.ToolID{})
		_, found, err = recovered.Lookup(ctxFor(t), e)
		requireCode(t, err, f.Forbidden)
		if found {
			t.Fatal("revoked current lookup exposed a consumed result")
		}
		_, err = recovered.Consume(ctxFor(t), e)
		requireCode(t, err, f.Forbidden)
		if !reflect.DeepEqual(v.requireDurable(t, true), committed) {
			t.Fatal("current refusal rewrote a historical consumption")
		}
		v.requireOriginalWork(t)
		t.Log("tool_result_consumption full_result=65536 consumption=1 transcript_sequence=3 repeated_and_new_consumer_observe=true current_revocation_rejected=true wire_requests=1 model_calls=1")
	})
	t.Run("rollback-and-commit-loss-observation", func(t *testing.T) {
		v := newToolResultConsumptionFixture(t)
		consumer := v.consumer(t)
		e := v.operation.facts.capture.created.ID
		store := v.operation.facts.capture.v.base.tracked
		marker := errors.New("owned-tool-result-consumption-rollback")
		var candidate toolResultConsumptionState
		var rollback f.CommitResult
		seen, returned, consumeTransactions := false, false, 0
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
			if err := v.validateState(candidate, true); err != nil {
				return err
			}
			seen = true
			return marker
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			if v.matches(cause) {
				consumeTransactions++
				if seen && !returned {
					rollback, returned = result, true
				}
			}
		}
		store.mu.Unlock()
		t.Cleanup(func() {
			store.setAfter(nil)
			store.mu.Lock()
			store.afterResult = nil
			store.mu.Unlock()
		})
		absent, err := consumer.Consume(ctxFor(t), e)
		store.setAfter(nil)
		if !seen || !returned || rollback.State() != f.NotCommitted || !errors.Is(rollback.Fault(), marker) || !errors.Is(err, marker) || absent != (ec.ToolResultConsumptionReceipt{}) || consumeTransactions != 1 {
			t.Fatal("complete candidate did not actually roll back with its original error")
		}
		if !reflect.DeepEqual(v.requireDurable(t, false), v.before) {
			t.Fatal("known rollback left partial canonical facts")
		}
		originalStore := store.fixtureStore
		loss := &captureCommitReceiptLoss{fixtureStore: originalStore, match: v.matches, attempt: id[f.TransactionAttempt](t)}
		store.fixtureStore = loss
		t.Cleanup(func() { store.fixtureStore = originalStore })
		absent, err = consumer.Consume(ctxFor(t), e)
		requireCode(t, err, f.CommitUnknown)
		original, retained := execution.UnknownAttempt(err)
		fired, physical := loss.observed()
		if absent != (ec.ToolResultConsumptionReceipt{}) || !fired || physical.State() != f.Committed || consumeTransactions != 2 || !retained || original.State() != f.Unknown || original.AttemptID() != loss.attempt || !v.matches(original.Cause()) {
			t.Fatal("lost acknowledgement did not follow the one real consumption COMMIT")
		}
		committed := v.requireDurable(t, true)
		if candidate.version != committed.version || candidate.operation != committed.operation || candidate.attempt != committed.attempt || !bytes.Equal(candidate.message, committed.message) {
			t.Fatal("known-rollback retry replaced the original result or checkpoint version")
		}
		// After Unknown there is no additional Consume, Execute or Model call.
		// ResolveUnknown and then Lookup only observe the exact original commit.
		resolved, err := consumer.ResolveUnknown(ctxFor(t), e)
		readFileAdapterOK(t, err, "original Unknown consumption observation")
		v.requireReceipt(t, resolved, committed)
		observed, found, err := consumer.Lookup(ctxFor(t), e)
		readFileAdapterOK(t, err, "independent current consumption lookup")
		if !found || consumeTransactions != 2 {
			t.Fatal("Unknown observation rewrote or failed to find the original consumption")
		}
		v.requireReceipt(t, observed, committed)
		if !reflect.DeepEqual(v.requireDurable(t, true), committed) {
			t.Fatal("Unknown observation changed the committed checkpoint")
		}
		v.requireOriginalWork(t)
		t.Log("tool_result_consumption rollback_zero_partial=true physical_commit_then_ack_loss=true consumption=1 observation_only=true wire_requests=1 model_calls=1")
	})
}
