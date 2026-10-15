package execution

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// A successor is reserved durably before accepting its Model session. Unknown
// keeps these exact bytes and IDs; observation never creates a second request.
type loopSuccessor struct {
	round      loopExecutionRound
	request    agentloop.DirectTextRequest
	version    f.Version
	message    mc.Message
	binding    tc.RunnerResultBinding
	approvalID string
	skills     *c.SkillRoundInput
}

func (s *directTextState) loopFailure(run *directTextCall, stage string, err error) (c.DirectTextReceipt, error) {
	s.mu.Lock()
	run.uncertainty = stage
	s.mu.Unlock()
	s.retain(run, err)
	return s.receipt(run), errors.Join(run.unresolved, err)
}
func (s *directTextState) advanceLoop(ctx context.Context, run *directTextCall) (c.DirectTextReceipt, error) {
	if s.multi == nil || !run.toolPending || run.toolFacts == nil {
		return s.receipt(run), fault(f.InvalidState)
	}
	stored, err := loadDirectTextRound(ctx, s.store, run.request.ExecutionID, run.round.Fields().ID.String())
	if err != nil {
		return s.loopFailure(run, "loop-result", err)
	}
	binding, err := toolBinding(stored)
	if err != nil {
		return s.loopFailure(run, "loop-result", err)
	}
	if !run.toolDispatched && (binding.RunnerBinding == tc.RunCommandRegistration().Binding || binding.MCPBinding.Validate() == nil) {
		if receipt, handled, err := s.beforeApprovalDispatch(ctx, run, binding); handled {
			return receipt, err
		}
	}
	var observed tc.RunnerOutcome
	if !run.toolDispatched {
		// Mark before I/O. An error cannot grant permission to dispatch again.
		run.toolDispatched = true
		out, dispatchErr := s.multi.dispatch.Execute(run.ctx, binding)
		observed = out
		if loopDispatchUnknown(binding, out, dispatchErr) {
			unknown := errors.Join(dispatchErr, f.NewFault(f.CommitUnknown, f.Unknown))
			if run.ctx.Err() != nil {
				return s.finishLoopDispatch(run, binding, unknown)
			}
			return s.loopFailure(run, "loop-dispatch", unknown)
		}
		if dispatchErr != nil || run.ctx.Err() != nil {
			return s.finishLoopDispatch(run, binding, dispatchErr)
		}
	} else {
		out, found, err := s.multi.dispatch.Lookup(ctx, binding)
		observed = out
		if err != nil {
			return s.loopFailure(run, "loop-dispatch", err)
		}
		if !found || loopDispatchUnknown(binding, out, nil) || (out.Phase != "succeeded" && out.Phase != "failed" && out.Phase != "cancelled") {
			return s.loopFailure(run, "loop-dispatch", fault(f.ResourceBusy))
		}
	}
	return s.continueLoopTool(ctx, run, binding, observed)
}

// Durable terminal metadata and physical owner retirement are separate gates.
// A Lookup receipt alone cannot authorize the successor Model invocation.
func (s *directTextState) continueLoopTool(ctx context.Context, run *directTextCall, binding tc.ToolCallBinding, observed tc.RunnerOutcome) (c.DirectTextReceipt, error) {
	lifecycle, ok := s.multi.dispatch.(tc.RunnerCallLifecycle)
	if !ok {
		return s.loopFailure(run, "loop-dispatch", fault(f.DependencyUnbound))
	}
	wait, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	joinErr := lifecycle.JoinCall(wait, binding)
	cancel()
	if joinErr != nil {
		return s.loopFailure(run, "loop-dispatch", joinErr)
	}
	if run.ctx.Err() != nil {
		return s.finishLoopDispatch(run, binding, run.ctx.Err())
	}
	if payloadErr := loopPayloadGate(binding, observed); payloadErr != nil {
		if directTextUnknown(payloadErr) || (observed.Phase != "succeeded" && observed.Phase != "failed" && observed.Phase != "cancelled") {
			return s.loopFailure(run, "loop-dispatch", payloadErr)
		}
		return s.finishLoopDispatch(run, binding, payloadErr)
	}
	plan, err := s.multi.results.DiscoverRunnerResult(ctx, binding)
	if err != nil {
		return s.loopFailure(run, "loop-result", err)
	}
	if nilPort(plan) || !sameToolCallBinding(plan.Binding().Call, binding) || !loopResultMatchesObservation(binding, observed, plan.Binding()) {
		return s.loopFailure(run, "loop-result", fault(f.Forbidden))
	}
	decisionLocks, err := s.meetingDecisionResultLocks(ctx, plan.Binding())
	if err != nil {
		return s.loopFailure(run, "loop-result", err)
	}
	skillPlan, err := s.discoverSkillRound(ctx, run, s.multi.skills)
	if err != nil {
		return s.loopFailure(run, "loop-result", err)
	}
	locks, err := oc.NormalizeLocks(append(append(append(append([]f.LockRequest{}, run.locks...), plan.RequiredLocks()...), decisionLocks...), skillPlan.RequiredLocks()...))
	if err != nil {
		return s.loopFailure(run, "loop-result", err)
	}
	s.mu.Lock()
	run.phase = "tool-writing"
	s.mu.Unlock()
	cause, _ := f.NewJobCause("execution-loop-successor", run.request.ExecutionID.String(), run.round.Fields().ID.String())
	canonicalCancelled := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := requireProjectCallInTx(ctx, tx, s.store); err != nil {
			return err
		}
		if err := s.multi.guard.RequireCurrentProcessInTx(ctx, tx, s.multiProcess()); err != nil {
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
		current, err := loadDirectText(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil || row.summary.Status != c.Running || !sameDirectText(current, run.snapshot, run.round) || current.toolPending == nil {
			return fault(f.ConfirmationStale)
		}
		if row.summary.CancelRequestedAt != nil {
			canonicalCancelled = true
			return context.Canceled
		}
		if _, err = s.deps.Projects.RequirePreparingProjectInTx(ctx, tx, row.summary.ProjectID); err != nil {
			return portError(err)
		}
		if err = s.requireCurrentToolFacts(ctx, tx, run); err != nil {
			return err
		}
		transition, err := s.consumeSkillRoundInTx(ctx, tx, run, skillPlan)
		if err != nil {
			return err
		}
		return s.multi.results.WithRunnerResultInTx(ctx, tx, plan, func(ctx context.Context, b tc.RunnerResultBinding, part mc.ToolResultPart) error {
			if !sameToolCallBinding(b.Call, binding) || !sameLoopResultBinding(b, plan.Binding()) || part.CallID != binding.CallID {
				return fault(f.Forbidden)
			}
			message := mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &part}}}
			if message.Validate() != nil {
				return invalid()
			}
			if run.next == nil {
				at, err := captureTime(ctx, x)
				if err != nil {
					return err
				}
				next, err := newLoopSuccessor(ctx, run, b, message, at, row.summary.Version+1, transition)
				if err != nil {
					return err
				}
				run.next = next
			}
			if !sameLoopResultBinding(run.next.binding, b) || !sameLoopResultMessage(run.next.message, message) || run.next.version != row.summary.Version+1 {
				return fault(f.ConfirmationStale)
			}
			n := run.next
			r := n.round.Fields()
			v := n.round.LoopRound.Fields()
			old := run.round.Fields()
			toolSequence := roundAssistantSequence(run.round) + 1
			if err = validateSuccessorSkills(n, transition, toolSequence+1); err != nil {
				return err
			}
			if err = insertDirectTextEntry(ctx, x, old.ExecutionID, old.ID, int64(toolSequence), "tool_result", message, r.CreatedAt); err != nil {
				return err
			}
			raw, err := directTextJSON(message)
			if err != nil {
				return err
			}
			tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.loop_tool_results(execution_id,round_id,invocation_id,call_id,operation_id,attempt_id,request_id,result_digest,terminal_digest,message_digest,transcript_sequence,checkpoint_version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, old.ExecutionID.String(), old.ID.String(), binding.InvocationID.String(), binding.CallID, b.OperationID.String(), b.AttemptID.String(), string(b.RequestID), string(b.ResultDigest), string(b.TerminalDigest), string(c.TriggerInputDigest(raw)), int64(toolSequence), int64(n.version), r.CreatedAt.Time())
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return unavailable(nil)
			}
			if err = s.recordMeetingDecisionResultInTx(ctx, tx, b, part, c.TriggerInputDigest(raw), f.Progress(toolSequence), n.version, r.CreatedAt); err != nil {
				return err
			}
			tag, err = x.Exec(ctx, `INSERT INTO agenteam_execution.rounds(id,execution_id,snapshot_id,start_id,input_binding_id,call_id,input_digest,context_digest,schema_version,content,digest,created_at,transcript_through,round_ordinal,predecessor_round_id,transcript_start) VALUES($1,$2,$3,$4,$5,$6,$7,$8,4,$9,$10,$11,$12,$13,$14,$15)`, r.ID.String(), r.ExecutionID.String(), r.SnapshotID.String(), r.StartID.String(), r.InputBindingID.String(), r.CallID.String(), string(r.Input.Digest), string(r.ContextDigest), n.round.CanonicalBytes(), string(n.round.Digest()), r.CreatedAt.Time(), int64(v.TranscriptStart-1), int64(v.Ordinal), old.ID.String(), int64(v.TranscriptStart))
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return unavailable(nil)
			}
			if err = insertSuccessorSkills(ctx, x, n); err != nil {
				return err
			}
			tag, err = x.Exec(ctx, `UPDATE agenteam_execution.executions SET version=version+1,updated_at=GREATEST($3,updated_at+interval '1 microsecond') WHERE id=$1 AND version=$2 AND status='running' AND cancel_requested_at IS NULL`, r.ExecutionID.String(), int64(row.summary.Version), r.CreatedAt.Time())
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return fault(f.ConfirmationStale)
			}
			return ctx.Err()
		})
	})
	if err = commitError(result); err != nil {
		if canonicalCancelled && result.State() == f.NotCommitted {
			return s.finishLoopDispatch(run, binding, context.Canceled)
		}
		stage := "loop-result"
		if directTextUnknown(err) {
			stage = "loop-successor"
		}
		return s.loopFailure(run, stage, err)
	}
	s.publishRuntime(run)
	return s.acceptLoopSuccessor(ctx, run)
}

// Compare the durable message identity, including opaque business file refs.
// Equality is only a checkpoint check; original result authority is still
// required before this comparison can be reached.
func sameLoopResultMessage(a, b mc.Message) bool {
	left, err := mc.EncodeCanonicalMessage(a)
	if err != nil {
		return false
	}
	right, err := mc.EncodeCanonicalMessage(b)
	return err == nil && bytes.Equal(left, right)
}

func newLoopSuccessor(ctx context.Context, run *directTextCall, b tc.RunnerResultBinding, message mc.Message, at f.Instant, version f.Version, transitions ...*skillRoundTransition) (*loopSuccessor, error) {
	previous, ok := run.round.(loopExecutionRound)
	if !ok {
		return nil, invalid()
	}
	request := run.invocation.request.Request()
	request.Messages = append(request.Messages, run.toolFacts.response.Message.Clone(), message.Clone())
	request.ToolChoice = mc.ToolChoice{Kind: "auto"}
	round, err := f.NewID[c.Round]()
	if err != nil {
		return nil, unavailable(err)
	}
	var skills *c.SkillRoundInput
	start := roundAssistantSequence(previous) + 2
	if len(transitions) > 1 {
		return nil, invalid()
	}
	if len(transitions) == 1 {
		v, err := transitions[0].roundInput(round, at, start)
		if err != nil {
			return nil, err
		}
		skills = &v
		control, err := v.ModelMessage()
		if err != nil {
			return nil, err
		}
		if control != nil {
			request.Messages = append(request.Messages, control.Clone())
			start++
		}
	}
	semantic, err := mc.NewSuccessorSemantic(ctx, mc.SuccessorSemanticParts{Actor: request.Actor, Model: request.Model, Messages: request.Messages, Tools: request.Tools, ToolChoice: request.ToolChoice, ResponseFormat: request.ResponseFormat, RetryClass: request.RetryClass})
	if err != nil {
		return nil, portError(err)
	}
	input, err := f.NewID[c.InputBinding]()
	if err != nil {
		return nil, unavailable(err)
	}
	call, err := f.NewID[mc.Call]()
	if err != nil {
		return nil, unavailable(err)
	}
	request.CallID = call
	request.Input = mc.InputIdentity{ExecutionID: &run.request.ExecutionID, RoundID: round.String(), Digest: semantic.Digest(), SchemaVersion: mc.SuccessorInputSchemaVersion}
	fixed, err := agentloop.NewLoopRequest(ctx, previous.Fields().ContextDigest, request)
	if err != nil {
		return nil, portError(err)
	}
	old := previous.Fields()
	prev := old.ID
	fields := c.LoopRoundFields{Round: c.DirectTextRoundFields{ID: round, InputBindingID: input, ExecutionID: old.ExecutionID, SnapshotID: old.SnapshotID, StartID: old.StartID, CallID: call, ContextDigest: old.ContextDigest, Input: request.Input, Messages: request.Messages, CreatedAt: at}, Tools: request.Tools, Choice: request.ToolChoice, Ordinal: previous.LoopRound.Fields().Ordinal + 1, TranscriptStart: start, Predecessor: &prev}
	value, err := c.NewLoopRound(fields)
	if err != nil {
		return nil, err
	}
	return &loopSuccessor{round: loopExecutionRound{value}, request: fixed, version: version, message: message.Clone(), binding: b, skills: skills}, nil
}

func validateSuccessorSkills(n *loopSuccessor, transition *skillRoundTransition, sequence f.Sequence) error {
	if n == nil || n.skills == nil {
		return fault(f.InvalidState)
	}
	r := n.round.Fields()
	want, err := transition.roundInput(r.ID, r.CreatedAt, sequence)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(want, *n.skills) {
		return fault(f.ConfirmationStale)
	}
	return nil
}

func insertSuccessorSkills(ctx context.Context, x postgres.SQLExecutor, n *loopSuccessor) error {
	if n == nil || n.skills == nil {
		return fault(f.InvalidState)
	}
	v := *n.skills
	if v.ControlSequence != nil {
		raw, err := v.ControlBytes()
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.transcript_entries(execution_id,sequence,round_id,kind,message,digest,created_at,loop_entry) VALUES($1,$2,$3,'control',$4,$5,$6,true)`, v.ExecutionID.String(), int64(*v.ControlSequence), v.RoundID.String(), raw, string(c.TriggerInputDigest(raw)), v.CreatedAt.Time())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return unavailable(nil)
		}
	}
	return insertSkillRoundInput(ctx, x, v)
}

func (s *directTextState) acceptLoopSuccessor(ctx context.Context, run *directTextCall) (c.DirectTextReceipt, error) {
	n := run.next
	if n == nil {
		return s.loopFailure(run, "loop-successor", fault(f.InvalidState))
	}
	session, err := s.deps.Loop.Accept(n.request)
	if err != nil {
		return s.loopFailure(run, "loop-accept", portError(err))
	}
	s.mu.Lock()
	run.round = n.round
	run.summary.Version = n.version
	run.summary.Status = c.Running
	run.invocation = &directTextInvocation{session: session, request: n.request}
	run.cold = nil
	run.toolPending = false
	run.toolFacts = nil
	run.toolDispatched = false
	run.next = nil
	run.allJoined = false
	run.phase = "calling"
	run.unresolved = nil
	run.uncertainty = ""
	s.mu.Unlock()
	return s.callAndFinish(ctx, run, false)
}

func (s *directTextState) observeLoopSuccessor(ctx context.Context, run *directTextCall) (c.DirectTextReceipt, error) {
	if run.next == nil {
		return s.receipt(run), run.unresolved
	}
	stored, err := loadDirectTextRound(ctx, s.store, run.request.ExecutionID, run.next.round.Fields().ID.String())
	if err != nil {
		return s.receipt(run), errors.Join(run.unresolved, err)
	}
	if !sameDirectText(stored, run.snapshot, run.next.round) {
		return s.receipt(run), run.unresolved
	}
	s.publishRuntime(run)
	return s.acceptLoopSuccessor(ctx, run)
}

func sameLoopResultBinding(a, b tc.RunnerResultBinding) bool {
	return sameToolCallBinding(a.Call, b.Call) && a.OperationID == b.OperationID && a.AttemptID == b.AttemptID && a.RequestID == b.RequestID && a.InputDigest == b.InputDigest && a.Fingerprint == b.Fingerprint && a.TerminalDigest == b.TerminalDigest && a.Outcome == b.Outcome && a.Code == b.Code && a.Attempted == b.Attempted && a.ResultDigest == b.ResultDigest && a.ResultBytes == b.ResultBytes
}

// A cancellation signal is never a join proof. This observes the exact Tool
// owner after the original Execute actually returned, before shared leases can
// retire. An unresolved handoff retains the Execution's original owner.
func (s *directTextState) finishLoopDispatch(run *directTextCall, b tc.ToolCallBinding, dispatchErr error) (c.DirectTextReceipt, error) {
	lifecycle, ok := s.multi.dispatch.(tc.RunnerCallLifecycle)
	if !ok {
		return s.loopFailure(run, "loop-dispatch", fault(f.DependencyUnbound))
	}
	wait, cancel := context.WithTimeout(context.WithoutCancel(run.ctx), 3*time.Second)
	defer cancel()
	if run.ctx.Err() != nil {
		if err := lifecycle.CancelCall(wait, b); err != nil {
			return s.loopFailure(run, "loop-dispatch", err)
		}
	}
	if err := lifecycle.JoinCall(wait, b); err != nil {
		return s.loopFailure(run, "loop-dispatch", errors.Join(dispatchErr, err))
	}
	if directTextUnknown(dispatchErr) {
		return s.loopFailure(run, "loop-dispatch", dispatchErr)
	}
	if dispatchErr == nil {
		dispatchErr = run.ctx.Err()
	}
	if dispatchErr == nil {
		return s.loopFailure(run, "loop-dispatch", fault(f.InvalidState))
	}
	s.mu.Lock()
	run.toolFailed = true
	run.invocation.callError = dispatchErr
	run.invocation.response = nil
	run.phase = "closing"
	run.unresolved = nil
	run.uncertainty = ""
	s.mu.Unlock()
	return s.finish(run)
}

// This flag is projected by Tool from the original validated terminal result
// reference. Neither an error string nor a failure outcome invents a payload.
func loopPayloadGate(binding tc.ToolCallBinding, out tc.RunnerOutcome) error {
	if loopDispatchUnknown(binding, out, nil) {
		return f.NewFault(f.CommitUnknown, f.Unknown)
	}
	if out.Phase != "succeeded" && out.Phase != "failed" && out.Phase != "cancelled" {
		return fault(f.ResourceBusy)
	}
	if out.ResultAvailable {
		return nil
	}
	if out.Outcome == "cancelled" {
		return context.Canceled
	}
	return fault(f.DependencyUnavailable)
}

func loopDispatchUnknown(binding tc.ToolCallBinding, out tc.RunnerOutcome, dispatchErr error) bool {
	return directTextUnknown(dispatchErr) || out.Outcome == "unknown" && !loopPrepareUnknownCandidate(binding, out)
}

// A cold prepare can publish an honest Unknown as its immutable error result.
// These metadata only select the original Join/Discover path; they neither
// authorize reading nor replace the opaque provider's same-Tx validation.
func loopPrepareUnknownCandidate(binding tc.ToolCallBinding, out tc.RunnerOutcome) bool {
	return binding.Validate() == nil && binding.Binding == (tc.BuiltinBinding{HandlerID: "skill.prepare", ContractRevision: 1}) &&
		binding.RunnerBinding == (tc.RunnerBinding{}) && binding.MCPBinding == (tc.MCPBinding{}) &&
		out.Phase == "failed" && out.Outcome == "unknown" && out.Attempted && out.ResultAvailable &&
		out.OperationID.Validate() == nil && out.AttemptID.Validate() == nil
}

func loopResultMatchesObservation(binding tc.ToolCallBinding, out tc.RunnerOutcome, result tc.RunnerResultBinding) bool {
	if out.Outcome != "unknown" {
		return true
	}
	return loopPrepareUnknownCandidate(binding, out) && result.Validate() == nil && sameToolCallBinding(result.Call, binding) &&
		result.OperationID == out.OperationID && result.AttemptID == out.AttemptID && result.Attempted == out.Attempted &&
		result.Outcome == "unknown" && result.Code == string(f.DependencyUnavailable)
}
