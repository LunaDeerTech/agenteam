package execution

import (
	"bytes"
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/jackc/pgx/v5"
)

type toolArgumentPayload struct{}
type toolCallCandidate struct {
	payload             f.ID[toolArgumentPayload]
	response            mc.ModelResponse
	call                mc.ToolCallPart
	original, canonical []byte
	at                  f.Instant
	version             f.Version
}

func newToolCallCandidate(run *directTextCall, response mc.ModelResponse) (*toolCallCandidate, error) {
	if run == nil || !run.allJoined || run.invocation.session == nil || !run.invocation.session.Joined() || response.Validate() != nil || response.CallID != run.round.Fields().CallID || response.FinishReason != "tool_calls" {
		return nil, fault(f.InvalidState)
	}
	fields := run.snapshot.Fields().Context.Input().Fields()
	if len(fields.Tools) == 0 || run.owner.multi == nil && len(fields.Tools) != 1 {
		return nil, fault(f.CapabilityUnsupported)
	}
	var part *mc.ToolCallPart
	for _, p := range response.Message.Parts {
		if p.ToolCall != nil {
			if part != nil {
				return nil, fault(f.CapabilityUnsupported)
			}
			v := *p.ToolCall
			part = &v
		} else if p.Text == nil {
			return nil, fault(f.CapabilityUnsupported)
		}
	}
	if part == nil {
		return nil, fault(f.InvalidState)
	}
	selected, err := capturedToolByName(run.input.input, part.Name)
	if err != nil {
		return nil, err
	}
	if selected.BindingSnapshot.RunnerBinding != (tc.RunnerBinding{}) {
		mountID, err := toolCallMountID(run.ctx, selected.BindingSnapshot.RunnerBinding, part.Arguments)
		if err != nil {
			return nil, portError(err)
		}
		found := false
		for _, mount := range fields.Mounts.Mounts {
			if mount.ID == mountID {
				found = true
			}
		}
		if !found || !slices.Contains(fields.Agent.Fields().AllowedMountIDs, mountID) {
			return nil, fault(f.Forbidden)
		}
	} else if run.owner.multi == nil || !tc.WorkArtifactBuiltinBinding(selected.BindingSnapshot.Binding) {
		return nil, fault(f.CapabilityUnsupported)
	}
	canonical, err := cursor.CanonicalJSON(part.Arguments)
	if err != nil {
		return nil, invalid()
	}
	payload, err := f.NewID[toolArgumentPayload]()
	if err != nil {
		return nil, unavailable(err)
	}
	at, err := f.NewInstant(time.Now())
	if err != nil {
		return nil, err
	}
	return &toolCallCandidate{payload: payload, response: response.Clone(), call: *part, original: bytes.Clone(part.Arguments), canonical: canonical, at: at}, nil
}

func (s *directTextState) saveToolCallFacts(ctx context.Context, run *directTextCall, response mc.ModelResponse) (c.DirectTextReceipt, error) {
	if run.ctx.Err() != nil {
		return s.finish(run)
	}
	if run.toolFacts == nil {
		candidate, err := newToolCallCandidate(run, response)
		if err != nil {
			run.invocation.callError = err
			return s.finish(run)
		}
		run.toolFacts = candidate
	}
	s.mu.Lock()
	run.phase = "tool-writing"
	s.mu.Unlock()
	cause, _ := f.NewJobCause("execution-tool-call-checkpoint", run.request.ExecutionID.String(), run.toolFacts.payload.String())
	canonicalCancelled := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, run.locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if run.ctx.Err() != nil {
			return run.ctx.Err()
		}
		if !run.allJoined || !run.invocation.session.Joined() || s.processes.CurrentProcess() != s.process {
			return fault(f.Forbidden)
		}
		row, err := loadExecution(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil || row.summary.Status != c.Running || row.summary.Version == f.Version(math.MaxInt64) || !sameDirectText(stored, run.snapshot, run.round) || stored.terminal != nil || stored.toolPending != nil {
			return fault(f.ConfirmationStale)
		}
		if row.summary.CancelRequestedAt != nil {
			canonicalCancelled = true
			return context.Canceled
		}
		if err = s.requireCurrentToolFacts(ctx, tx, run); err != nil {
			return err
		}
		if err = s.verifyDirectTextMountUse(ctx, tx, run, row, stored, run.input); err != nil {
			return err
		}
		run.toolFacts.version = row.summary.Version + 1
		return insertToolCallFacts(ctx, x, run, row.summary.Version)
	})
	if err := commitError(result); err != nil {
		// A canonical cancel is distinct from an uncertain checkpoint. Only a
		// known rollback with no candidate writes may enter the old terminal
		// owner; the actual JSON handle has already joined at this boundary.
		if canonicalCancelled && result.State() == f.NotCommitted {
			s.mu.Lock()
			run.phase = "closing"
			run.invocation.callError = context.Canceled
			s.mu.Unlock()
			return s.finish(run)
		}
		s.mu.Lock()
		run.uncertainty = "tool-writing"
		if directTextUnknown(err) {
			run.uncertainty = "tool-checkpoint"
		}
		s.mu.Unlock()
		s.retain(run, err)
		return s.receipt(run), err
	}
	s.mu.Lock()
	run.phase = "tool-pending"
	run.toolPending = true
	run.summary.Version = run.toolFacts.version
	run.unresolved = nil
	run.uncertainty = ""
	s.mu.Unlock()
	return s.receipt(run), nil
}

func insertToolCallFacts(ctx context.Context, x postgres.SQLExecutor, run *directTextCall, before f.Version) error {
	v := run.toolFacts
	r := run.round.Fields()
	q := run.request
	t, err := capturedToolByName(run.input.input, v.call.Name)
	if err != nil {
		return err
	}
	binding := toolCallStorageBinding(t)
	response, err := directTextJSON(v.response)
	if err != nil {
		return err
	}
	tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.tool_call_arguments(id,execution_id,round_id,input_binding_id,logical_call_id,snapshot_id,project_id,agent_id,original,original_digest,canonical,canonical_digest,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, v.payload.String(), q.ExecutionID.String(), r.ID.String(), r.InputBindingID.String(), r.CallID.String(), r.SnapshotID.String(), q.Launch.ProjectID.String(), q.Launch.AgentID.String(), v.original, string(c.TriggerInputDigest(v.original)), v.canonical, string(c.TriggerInputDigest(v.canonical)), v.at.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	tag, err = x.Exec(ctx, `INSERT INTO agenteam_execution.tool_calls(execution_id,round_id,invocation_id,call_id,model_visible_name,tool_id,spec_revision,operation_name,operation_revision,protocol_major,payload_id,checkpoint_version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, q.ExecutionID.String(), r.ID.String(), v.response.InvocationID.String(), v.call.ID, v.call.Name, t.ToolID.String(), int64(t.SpecRevision), binding.OperationName, binding.OperationRevision, int16(binding.ProtocolMajor), v.payload.String(), int64(v.version), v.at.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return unavailable(nil)
	}
	if err = insertDirectTextEntry(ctx, x, q.ExecutionID, r.ID, int64(roundAssistantSequence(run.round)), "assistant", v.response.Message, v.at); err != nil {
		return err
	}
	tag, err = x.Exec(ctx, `UPDATE agenteam_execution.rounds SET tool_pending=true,checkpoint_at=$3,transcript_through=$6,response=$4,response_digest=$5 WHERE execution_id=$1 AND id=$2 AND schema_version IN (2,4) AND terminal_status IS NULL AND NOT tool_pending`, q.ExecutionID.String(), r.ID.String(), v.at.Time(), response, string(c.TriggerInputDigest(response)), int64(roundAssistantSequence(run.round)))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	tag, err = x.Exec(ctx, `UPDATE agenteam_execution.executions SET version=version+1,updated_at=GREATEST($3,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status='running' AND cancel_requested_at IS NULL`, q.ExecutionID.String(), int64(before), v.at.Time())
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}

// Only Execution's internal writer/authority reads the protected bytes. No
// public HTTP or implicit formatting path exposes this record.
func loadToolCallFacts(ctx context.Context, x postgres.SQLExecutor, stored *directTextRecord) (*toolCallCandidate, error) {
	r := stored.round.Fields()
	input := stored.snapshot.Fields().Context.Input().Fields()
	var payload, invocation, call, name, tool, round, binding, logical, snapshot, project, agent, od, cd, operation, revision string
	var spec, major, version int64
	var at time.Time
	var original, canonical []byte
	err := x.QueryRow(ctx, `SELECT a.id::text,t.invocation_id::text,t.call_id,t.model_visible_name,t.tool_id::text,t.spec_revision,t.operation_name,t.operation_revision,t.protocol_major,t.checkpoint_version,t.created_at,a.round_id::text,a.input_binding_id::text,a.logical_call_id::text,a.snapshot_id::text,a.project_id::text,a.agent_id::text,a.original,a.original_digest,a.canonical,a.canonical_digest FROM agenteam_execution.tool_calls t JOIN agenteam_execution.tool_call_arguments a ON(a.execution_id,a.round_id,a.id)=(t.execution_id,t.round_id,t.payload_id) WHERE t.execution_id=$1 AND t.round_id=$2`, r.ExecutionID.String(), r.ID.String()).Scan(&payload, &invocation, &call, &name, &tool, &spec, &operation, &revision, &major, &version, &at, &round, &binding, &logical, &snapshot, &project, &agent, &original, &od, &canonical, &cd)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, unavailable(nil)
	}
	if err != nil {
		return nil, unavailable(err)
	}
	if stored.toolPending == nil || len(input.Tools) == 0 {
		return nil, unavailable(nil)
	}
	v := stored.toolPending
	id, err := f.ParseID[toolArgumentPayload](payload)
	if err != nil {
		return nil, unavailable(nil)
	}
	expected, err := capturedToolByName(stored.snapshot.Fields().Context.Input(), v.call.Name)
	if err != nil {
		return nil, err
	}
	rb := toolCallStorageBinding(expected)
	if round != r.ID.String() || binding != r.InputBindingID.String() || logical != r.CallID.String() || snapshot != r.SnapshotID.String() || project != input.Request.Launch.ProjectID.String() || agent != input.Request.Launch.AgentID.String() || tool != expected.ToolID.String() || spec != int64(expected.SpecRevision) || name != expected.ModelVisibleName || operation != rb.OperationName || revision != rb.OperationRevision || major != int64(rb.ProtocolMajor) || invocation != v.response.InvocationID.String() || call != v.call.ID || name != v.call.Name || version <= int64(stored.startedVersion) || !at.Equal(v.at.Time()) || f.Digest(od) != c.TriggerInputDigest(original) || f.Digest(cd) != c.TriggerInputDigest(canonical) {
		return nil, unavailable(nil)
	}
	actual, err := cursor.CanonicalJSON(original)
	if err != nil || !bytes.Equal(actual, canonical) {
		return nil, unavailable(nil)
	}
	responseArgs, err := cursor.CanonicalJSON(v.call.Arguments)
	if err != nil || !bytes.Equal(responseArgs, canonical) {
		return nil, unavailable(nil)
	}
	if expected.BindingSnapshot.RunnerBinding != (tc.RunnerBinding{}) {
		if _, err = toolCallMountID(ctx, rb, original); err != nil {
			return nil, unavailable(nil)
		}
	} else if !tc.WorkArtifactBuiltinBinding(expected.BindingSnapshot.Binding) {
		return nil, unavailable(nil)
	}
	v.payload = id
	v.original = bytes.Clone(original)
	v.canonical = bytes.Clone(canonical)
	v.version = f.Version(version)
	return v, ctx.Err()
}

func (s *directTextState) observeToolCallCheckpoint(ctx context.Context, run *directTextCall) (bool, error) {
	if run == nil || run.toolFacts == nil || !run.allJoined || run.invocation == nil || run.invocation.session == nil || !run.invocation.session.Joined() {
		return false, fault(f.InvalidState)
	}
	var found bool
	cause, _ := f.NewRecoveryCause("execution.tool-call.observe", run.request.ExecutionID.String(), run.toolFacts.payload.String())
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, run.locks); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadExecution(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil || !sameDirectText(stored, run.snapshot, run.round) || stored.toolPending == nil {
			return nil
		}
		v := stored.toolPending
		if row.summary.Status != c.Running || row.summary.Version < v.version || !sameToolCallCheckpoint(v, run.toolFacts) {
			return fault(f.ConfirmationStale)
		}
		if err = s.verifyDirectTextMountUse(ctx, tx, run, row, stored, run.input); err != nil {
			return err
		}
		run.summary = row.summary.Clone()
		found = true
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return false, err
	}
	if found {
		s.mu.Lock()
		run.toolPending = true
		run.phase = "tool-pending"
		run.unresolved = nil
		run.uncertainty = ""
		s.mu.Unlock()
	}
	return found, nil
}

func toolBinding(stored *directTextRecord) (tc.ToolCallBinding, error) {
	if stored == nil || stored.toolPending == nil {
		return tc.ToolCallBinding{}, fault(f.InvalidState)
	}
	r := stored.round.Fields()
	v := stored.toolPending
	input := stored.snapshot.Fields().Context.Input().Fields()
	t, err := capturedToolByName(stored.snapshot.Fields().Context.Input(), v.call.Name)
	if err != nil {
		return tc.ToolCallBinding{}, err
	}
	actor, err := i.NewAgentRun(input.Request.Launch.ProjectID, input.Request.Launch.AgentID, input.Request.ExecutionID)
	if err != nil {
		return tc.ToolCallBinding{}, err
	}
	b := tc.ToolCallBinding{Actor: actor, ProjectID: input.Request.Launch.ProjectID, AgentID: input.Request.Launch.AgentID, ExecutionID: r.ExecutionID, RoundID: r.ID.String(), SnapshotID: r.SnapshotID.String(), InputBindingID: r.InputBindingID.String(), LogicalCallID: r.CallID, InvocationID: v.response.InvocationID, CallID: v.call.ID, ModelVisibleName: t.ModelVisibleName, Spec: tc.SpecRef{ToolID: t.ToolID, SpecRevision: t.SpecRevision}, Binding: t.BindingSnapshot.Binding, RunnerBinding: t.BindingSnapshot.RunnerBinding, Input: tc.ToolInputReference{PayloadID: v.payload.String(), SHA256: c.TriggerInputDigest(v.original), Bytes: f.Progress(len(v.original))}, CanonicalArguments: c.TriggerInputDigest(v.canonical)}
	return b, b.Validate()
}

// The historical routing columns are also used for the closed Builtin set.
// The actual backend kind remains the captured BindingSnapshot and the public
// ToolCallBinding never presents a Builtin call as a Runner operation.
func toolCallStorageBinding(t tc.ExecutionTool) tc.RunnerBinding {
	if tc.WorkArtifactBuiltinBinding(t.BindingSnapshot.Binding) {
		return tc.RunnerBinding{OperationName: t.BindingSnapshot.Binding.HandlerID, OperationRevision: "1", ProtocolMajor: 1}
	}
	return t.BindingSnapshot.RunnerBinding
}

// Observation binds all original success bytes, not only the protected input.
// Two self-consistent persisted rows are not interchangeable commit receipts.
func sameToolCallCheckpoint(actual, expected *toolCallCandidate) bool {
	if actual == nil || expected == nil || actual.payload != expected.payload || actual.version != expected.version || actual.call.ID != expected.call.ID || actual.call.Name != expected.call.Name || !actual.at.Time().Equal(expected.at.Time()) || !bytes.Equal(actual.original, expected.original) || !bytes.Equal(actual.canonical, expected.canonical) {
		return false
	}
	a, err := directTextJSON(actual.response)
	if err != nil {
		return false
	}
	b, err := directTextJSON(expected.response)
	return err == nil && bytes.Equal(a, b)
}
