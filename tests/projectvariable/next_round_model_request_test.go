//go:build integration && linux

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

// The inherited fixture creates one real Command ToolCall/Operation through
// the original Model JSON boundary and WSS. Request binding adds no Model or
// Runner work and does not manufacture the committed successor RoundInput.
type nextRoundRequestFixture struct {
	command    *commandResultConsumptionFixture
	input      ec.RoundInput
	projection agentloop.RoundProjection
	snapshot   ec.DirectTextSnapshot
	checkpoint toolResultConsumptionState
	round      commandResultRoundState
}

func newNextRoundRequestFixture(t *testing.T) *nextRoundRequestFixture {
	t.Helper()
	v := newCommandResultConsumptionFixture(t, false)
	v.revokeDispatch(t)
	consumer := v.consumer(t)
	c := v.operation.facts.capture
	receipt, err := consumer.Consume(ctxFor(t), c.created.ID)
	readFileAdapterOK(t, err, "real Command result consumption before next request")
	consumed := v.requireDurable(t, true)
	v.requireReceipt(t, receipt, consumed)
	v.determineAndProject(t, receipt, consumed)
	stopToolResultConsumer(t, consumer)
	round := v.roundState(t)
	input, err := ec.DecodeRoundInput(round.content)
	readFileAdapterOK(t, err, "actual committed successor input")
	var snapshotRaw []byte
	readFileAdapterOK(t, c.v.base.raw.QueryRow(ctxFor(t), `SELECT content FROM agenteam_execution.snapshots WHERE execution_id=$1 AND id=$2`, c.created.ID.String(), input.Fields().SnapshotID.String()).Scan(&snapshotRaw), "original captured Model snapshot")
	snapshot, err := ec.DecodeDirectTextSnapshot(snapshotRaw)
	clear(snapshotRaw)
	readFileAdapterOK(t, err, "original canonical snapshot")
	initial, err := skill.NewInitialRoundBindings(c.v.agent.providers.SkillAuthority)
	readFileAdapterOK(t, err, "actual historical Skill reader")
	assignments, err := skill.NewAssignmentValidation(c.v.agent.providers.SkillAuthority)
	readFileAdapterOK(t, err, "actual Skill assignment validator")
	determiner, err := execution.NewRoundInputDeterminer(c.v.base.tracked, c.authority, execution.RoundInputDependencies{Projects: c.projectAuthority, Agents: c.configuration, InitialSkills: initial, Assignments: assignments})
	readFileAdapterOK(t, err, "actual committed projection owner")
	t.Cleanup(func() {
		determiner.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if determiner.Drain(ctx) != nil || !determiner.Joined() {
			t.Error("original projection owner did not actually join")
		}
	})
	projector, err := agentloop.NewRoundProjector(determiner)
	readFileAdapterOK(t, err, "actual successor projection")
	projection, err := projector.Project(ctxFor(t), c.created.ID, input.Fields().RoundID)
	readFileAdapterOK(t, err, "read-only original Command projection")
	v.requireProjection(t, input, projection)
	checkpoint, err := v.read(ctxFor(t), c.v.base.raw)
	readFileAdapterOK(t, err, "post-Determine checkpoint baseline")
	x := &nextRoundRequestFixture{v, input, projection, snapshot, checkpoint, round}
	x.requireUnchanged(t)
	return x
}

func (v *nextRoundRequestFixture) binder(t *testing.T) *execution.RoundModelRequestBinder {
	t.Helper()
	c := v.command.operation.facts.capture
	binder, err := execution.NewRoundModelRequestBinder(c.v.base.tracked, c.authority, c.projectAuthority)
	readFileAdapterOK(t, err, "same original Store request binder")
	t.Cleanup(func() { stopNextRoundRequestBinder(t, binder) })
	return binder
}

func stopNextRoundRequestBinder(t *testing.T, binder *execution.RoundModelRequestBinder) {
	t.Helper()
	binder.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if binder.Drain(ctx) != nil || !binder.Joined() {
		t.Error("original request binder did not actually join")
	}
}

func (v *nextRoundRequestFixture) matches(cause f.TransactionCause) bool {
	d, q := cause.Details(), v.input.Fields()
	return d.Kind == f.JobCause && d.JobType == "execution.round-model-request.prepare" && d.JobID == q.ExecutionID.String() && d.JobAttemptID == q.RoundID.String()
}

func (v *nextRoundRequestFixture) requireUnchanged(t *testing.T) {
	t.Helper()
	c := v.command.operation.facts.capture
	actual, err := v.command.read(ctxFor(t), c.v.base.raw)
	readFileAdapterOK(t, err, "independent original checkpoint after binding")
	if !reflect.DeepEqual(actual, v.checkpoint) || !reflect.DeepEqual(v.command.roundState(t), v.round) {
		t.Fatal("request binding changed the original Execution, Snapshot, pending Round, ToolCall, seq1-3 or committed input")
	}
	v.command.requireOriginalWork(t)
}

// This independent SQL observes every persisted request column, not a caller
// DTO. An absent row still returns the original Execution's total row count.
type nextRoundRequestRow struct {
	count                                               int64
	execution, round, project, agent, snapshot          string
	roundDigest, snapshotDigest, projectionDigest, call string
	schema                                              int64
	inputDigest                                         string
	request                                             []byte
	requestDigest                                       string
	requestBytes                                        int64
	created                                             time.Time
	xmin                                                string
}

func (v *nextRoundRequestFixture) read(ctx context.Context, x postgres.SQLExecutor) (nextRoundRequestRow, error) {
	var got nextRoundRequestRow
	q := v.input.Fields()
	err := x.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_execution.round_model_requests z WHERE z.execution_id=e.id),
 COALESCE(r.execution_id,''),COALESCE(r.round_input_id,''),COALESCE(r.project_id,''),COALESCE(r.agent_id,''),COALESCE(r.snapshot_id,''),
 COALESCE(r.round_input_digest,''),COALESCE(r.snapshot_digest,''),COALESCE(r.projection_digest,''),COALESCE(r.call_id,''),COALESCE(r.schema_version,0),
 COALESCE(r.input_digest,''),COALESCE(r.request,''::bytea),COALESCE(r.request_digest,''),COALESCE(r.request_bytes,0),
 COALESCE(r.created_at,TIMESTAMPTZ 'epoch'),COALESCE(r.xmin::text,'')
 FROM agenteam_execution.executions e
 LEFT JOIN agenteam_execution.round_model_requests r ON r.execution_id=e.id AND r.round_input_id=$2
 WHERE e.id=$1`, q.ExecutionID.String(), q.RoundID.String()).Scan(&got.count,
		&got.execution, &got.round, &got.project, &got.agent, &got.snapshot,
		&got.roundDigest, &got.snapshotDigest, &got.projectionDigest, &got.call, &got.schema,
		&got.inputDigest, &got.request, &got.requestDigest, &got.requestBytes, &got.created, &got.xmin)
	return got, err
}

func (v *nextRoundRequestFixture) durable(t *testing.T) nextRoundRequestRow {
	t.Helper()
	got, err := v.read(ctxFor(t), v.command.operation.facts.capture.v.base.raw)
	readFileAdapterOK(t, err, "independent original request row")
	return got
}

func (v *nextRoundRequestFixture) requireAbsent(t *testing.T) nextRoundRequestRow {
	t.Helper()
	got := v.durable(t)
	if got.count != 0 || got.execution != "" || got.call != "" || got.schema != 0 || len(got.request) != 0 || got.requestBytes != 0 || got.requestDigest != "" || got.xmin != "" {
		t.Fatal("not-committed request left a partial durable fact")
	}
	v.requireUnchanged(t)
	return got
}

func (v *nextRoundRequestFixture) requireRequest(t *testing.T, value ec.NextRoundModelRequest, row nextRoundRequestRow) {
	t.Helper()
	q, fields := v.input.Fields(), value.Fields()
	captured := v.snapshot.Fields().Context.Input().Fields()
	request := value.Request()
	originalRequest, _, _, _, _, _ := v.command.operation.facts.model.observed()
	if value.Validate() != nil || request.Validate() != nil || fields.SchemaVersion != 1 || fields.ProjectID != captured.Request.Launch.ProjectID || fields.AgentID != captured.Request.Launch.AgentID || fields.ExecutionID != q.ExecutionID || fields.RoundInputID != q.RoundID || fields.RoundInputDigest != v.input.Digest() || fields.SnapshotID != q.SnapshotID || fields.SnapshotDigest != v.snapshot.Digest() || fields.ProjectionDigest != v.projection.Digest() || fields.CallID.Validate() != nil || fields.CallID == originalRequest.CallID {
		t.Fatal("request identity does not bind the actual committed input, snapshot and full projection")
	}
	actor := request.Actor.Details()
	if actor.Kind != "agent_run" || actor.ProjectID != fields.ProjectID.String() || actor.AgentID != fields.AgentID.String() || actor.ExecutionID != fields.ExecutionID.String() || request.CallID != fields.CallID || request.Input.ExecutionID == nil || *request.Input.ExecutionID != q.ExecutionID || request.Input.RoundID != q.RoundID.String() || request.Input.SchemaVersion != 3 || request.Input.Digest != fields.InputDigest || request.ToolChoice != (mc.ToolChoice{Kind: "auto"}) || request.ResponseFormat.Kind != "text" || request.ResponseFormat.Name != "" || len(request.ResponseFormat.Schema) != 0 || request.RetryClass != mc.AgentRetry {
		t.Fatal("next request changed the explicit Agent identity or auto/text/schema3 profile")
	}
	if !request.Consumer.Equal(captured.Model.Consumer) || !bytes.Equal(nextProjectionJSON(t, request.Model), nextProjectionJSON(t, captured.Model)) || !bytes.Equal(nextProjectionJSON(t, request.Messages), nextProjectionJSON(t, v.projection.Messages())) || !bytes.Equal(nextProjectionJSON(t, request.Tools), nextProjectionJSON(t, v.projection.Tools())) {
		t.Fatal("request changed the captured Model or any complete projected message/tool bytes")
	}
	// The original assistant argv and Tool error with both complete streams,
	// including JSON-escaped NUL, remain in their original message positions.
	if len(request.Messages) != 5 || request.Messages[3].Parts[0].ToolResult == nil || !request.Messages[3].Parts[0].ToolResult.IsError || request.Messages[3].Parts[0].ToolResult.Parts[0].Text.Text != string(v.command.bundle) {
		t.Fatal("next request changed the original known Command error result")
	}
	raw := value.CanonicalBytes()
	defer clear(raw)
	canonical, err := cursor.CanonicalJSON(raw)
	readFileAdapterOK(t, err, "canonical next request bytes")
	defer clear(canonical)
	var envelope map[string]json.RawMessage
	readFileAdapterOK(t, json.Unmarshal(raw, &envelope), "explicit next request envelope")
	semantic, err := cursor.CanonicalJSON(envelope["semantic"])
	readFileAdapterOK(t, err, "complete semantic request bytes")
	defer clear(semantic)
	if !bytes.Equal(raw, canonical) || string(value.Digest()) != toolResultConsumptionDigest(raw) || value.InputDigest() != fields.InputDigest || string(fields.InputDigest) != toolResultConsumptionDigest(semantic) {
		t.Fatal("request or semantic digest omits original canonical bytes")
	}
	if row.count != 1 || row.execution != fields.ExecutionID.String() || row.round != fields.RoundInputID.String() || row.project != fields.ProjectID.String() || row.agent != fields.AgentID.String() || row.snapshot != fields.SnapshotID.String() || row.roundDigest != string(fields.RoundInputDigest) || row.snapshotDigest != string(fields.SnapshotDigest) || row.projectionDigest != string(fields.ProjectionDigest) || row.call != fields.CallID.String() || row.schema != 1 || row.inputDigest != string(fields.InputDigest) || !bytes.Equal(row.request, raw) || row.requestDigest != string(value.Digest()) || row.requestBytes != int64(len(raw)) || row.created.Before(q.CreatedAt.Time()) || row.xmin == "" {
		t.Fatal("independent request row differs from the entire returned committed value")
	}
}

func requireSameNextRoundRequest(t *testing.T, a, b ec.NextRoundModelRequest) {
	t.Helper()
	if a.Validate() != nil || b.Validate() != nil || a.Fields() != b.Fields() || a.Digest() != b.Digest() || a.InputDigest() != b.InputDigest() || !bytes.Equal(a.CanonicalBytes(), b.CanonicalBytes()) {
		t.Fatal("historical observation changed original CallID, full request or digests")
	}
}

func TestNextRoundModelRequest(t *testing.T) {
	t.Run("committed-command-input-request-and-history", func(t *testing.T) {
		v := newNextRoundRequestFixture(t)
		v.requireAbsent(t)
		binder := v.binder(t)
		q := v.input.Fields()
		value, err := binder.Prepare(ctxFor(t), q.ExecutionID, q.RoundID)
		readFileAdapterOK(t, err, "original next request committed return")
		committed := v.durable(t)
		v.requireRequest(t, value, committed)
		v.requireUnchanged(t)
		// Replaying the same committed identity returns the stored request,
		// including the original logical CallID, without adding a row.
		again, err := binder.Prepare(ctxFor(t), q.ExecutionID, q.RoundID)
		readFileAdapterOK(t, err, "same input request replay")
		requireSameNextRoundRequest(t, value, again)
		if !reflect.DeepEqual(v.durable(t), committed) {
			t.Fatal("request replay rewrote the immutable original row")
		}
		// Setup formally revoked both dispatch grants before consumption.
		// Historical request observation cannot reacquire either authority.
		for _, owner := range []*execution.RoundModelRequestBinder{binder, v.binder(t)} {
			observed, found, err := owner.Lookup(ctxFor(t), q.ExecutionID, q.RoundID)
			readFileAdapterOK(t, err, "historical next request after formal dispatch revocation")
			if !found {
				t.Fatal("historical request disappeared after current revocation")
			}
			requireSameNextRoundRequest(t, value, observed)
			v.requireRequest(t, observed, v.durable(t))
		}
		// Mutate only exported copies; later reads must retain the full original
		// request and the protected durable canonical history.
		raw := value.CanonicalBytes()
		raw[0] ^= 1
		clear(raw)
		copyRequest := value.Request()
		copyRequest.Messages[0].Parts[0].Text.Text = "modified caller copy"
		copyRequest.Tools[0].Description = "modified caller copy"
		observed, found, err := binder.Lookup(ctxFor(t), q.ExecutionID, q.RoundID)
		readFileAdapterOK(t, err, "original request after caller copy mutation")
		if !found || !reflect.DeepEqual(v.durable(t), committed) {
			t.Fatal("caller copy mutation changed original request persistence")
		}
		requireSameNextRoundRequest(t, value, observed)
		v.requireRequest(t, observed, committed)
		v.requireUnchanged(t)
		t.Log("next_round_model_request committed_input=true complete_command_history=true original_call_id_replayed=true independent_history_lookup=true binding_model_calls=0 binding_wire_requests=0 setup_model_calls=1 setup_wire_requests=1")
	})
	t.Run("request-rollback-and-commit-loss-lookup", func(t *testing.T) {
		v := newNextRoundRequestFixture(t)
		before := v.requireAbsent(t)
		binder := v.binder(t)
		q := v.input.Fields()
		store := v.command.operation.facts.capture.v.base.tracked
		var candidate nextRoundRequestRow
		var physical f.CommitResult
		finalCalls := 0
		rollback := errors.New("owned next request rollback")
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
			if candidate.count != 1 || len(candidate.request) == 0 || candidate.call == "" {
				return errors.New("request callback lacked its complete insert")
			}
			return rollback
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			if v.matches(cause) {
				finalCalls++
				physical = result
			}
		}
		store.mu.Unlock()
		t.Cleanup(func() {
			roundInputClearHooks(store)
			store.mu.Lock()
			store.transformResult = nil
			store.mu.Unlock()
		})
		absent, err := binder.Prepare(ctxFor(t), q.ExecutionID, q.RoundID)
		if err == nil || absent.Validate() == nil || finalCalls != 1 || physical.State() != f.NotCommitted || candidate.count != 1 || !reflect.DeepEqual(v.requireAbsent(t), before) {
			t.Fatal("real callback rollback did not leave zero request facts")
		}
		observed, found, err := binder.Lookup(ctxFor(t), q.ExecutionID, q.RoundID)
		readFileAdapterOK(t, err, "independent absent request after known rollback")
		if found || observed.Validate() == nil {
			t.Fatal("rolled-back request was published")
		}
		// After known NotCommitted, one new Prepare may commit. Only its
		// acknowledgement is hidden after the original Store physically returns.
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if !v.matches(cause) {
				return nil
			}
			x, err := store.InTx(tx)
			if err != nil {
				return err
			}
			candidate, err = v.read(ctx, x)
			return err
		})
		entered, release, returned := make(chan f.CommitResult, 1), make(chan struct{}), make(chan struct{})
		var once sync.Once
		resume := func() { once.Do(func() { close(release) }) }
		attempt := id[f.TransactionAttempt](t)
		store.mu.Lock()
		store.transformResult = func(cause f.TransactionCause, result f.CommitResult) f.CommitResult {
			if v.matches(cause) && result.State() == f.Committed {
				entered <- result
				<-release
				return f.UnknownResult(attempt, cause)
			}
			return result
		}
		store.mu.Unlock()
		type completion struct {
			value ec.NextRoundModelRequest
			err   error
		}
		done := make(chan completion, 1)
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		t.Cleanup(func() {
			resume()
			cancel()
			select {
			case <-returned:
			case <-time.After(5 * time.Second):
				t.Error("original held Prepare did not actually return")
			}
		})
		go func() {
			defer close(returned)
			value, err := binder.Prepare(ctx, q.ExecutionID, q.RoundID)
			done <- completion{value, err}
		}()
		select {
		case actual := <-entered:
			// CommittedResult itself has no cause. The original hook's exact
			// cause predicate above binds this physical return to this Prepare.
			if actual.State() != f.Committed || finalCalls != 2 || physical.State() != f.Committed {
				t.Fatal("receipt barrier did not follow actual original request COMMIT")
			}
		case <-returned:
			t.Fatal("Prepare returned before its original COMMIT return barrier")
		case <-ctx.Done():
			t.Fatal("original request COMMIT barrier was not reached")
		}
		committed := v.durable(t)
		if !reflect.DeepEqual(committed, candidate) || committed.count != 1 {
			t.Fatal("original physical COMMIT lost its complete candidate")
		}
		select {
		case <-done:
			t.Fatal("Prepare published before original Store return")
		default:
		}
		resume()
		var result completion
		select {
		case result = <-done:
		case <-ctx.Done():
			t.Fatal("original Prepare did not return after barrier release")
		}
		<-returned
		requireCode(t, result.err, f.CommitUnknown)
		unknown, retained := execution.UnknownAttempt(result.err)
		if result.value.Validate() == nil || !retained || unknown.State() != f.Unknown || unknown.AttemptID() != attempt || !v.matches(unknown.Cause()) {
			t.Fatal("Unknown did not retain the original request transaction identity")
		}
		// No second Prepare after Unknown. Even after Stop, only the exact
		// original Lookup resolves this retained unknown; a new owner then reads.
		binder.Stop()
		if binder.Joined() {
			t.Fatal("unobserved request Unknown was silently joined")
		}
		observed, found, err = binder.Lookup(ctxFor(t), q.ExecutionID, q.RoundID)
		readFileAdapterOK(t, err, "original request Unknown Lookup")
		if !found {
			t.Fatal("original Lookup did not observe the actual committed request")
		}
		v.requireRequest(t, observed, committed)
		stopNextRoundRequestBinder(t, binder)
		recovered := v.binder(t)
		again, found, err := recovered.Lookup(ctxFor(t), q.ExecutionID, q.RoundID)
		readFileAdapterOK(t, err, "new binder historical request Lookup")
		if !found || finalCalls != 2 || !reflect.DeepEqual(v.durable(t), committed) {
			t.Fatal("Lookup rewrote or repeated the one committed request")
		}
		requireSameNextRoundRequest(t, observed, again)
		v.requireRequest(t, again, committed)
		v.requireUnchanged(t)
		t.Log("next_round_model_request actual_rollback_zero_rows=true physical_commit_before_ack_loss=true original_and_new_lookup_only=true request_rows=1 stable_call_and_complete_bytes=true binding_model_calls=0 binding_wire_requests=0 setup_model_calls=1 setup_wire_requests=1")
	})
}
