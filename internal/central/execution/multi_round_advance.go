package execution

import (
	"context"
	"errors"
	"reflect"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// A successor is reserved durably before accepting its Model session. Unknown
// keeps these exact bytes and IDs; observation never creates a second request.
type loopSuccessor struct {
	round   loopExecutionRound
	request agentloop.DirectTextRequest
	version f.Version
	message mc.Message
	binding tc.RunnerResultBinding
}

func (s *directTextState) loopFailure(run *directTextCall, stage string, err error) (c.DirectTextReceipt, error) {
	s.mu.Lock()
	run.uncertainty = stage
	s.mu.Unlock()
	s.retain(run, err)
	return s.receipt(run), err
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
	if !run.toolDispatched {
		// Mark before I/O. An error cannot grant permission to dispatch again.
		run.toolDispatched = true
		_, err = s.multi.dispatch.Execute(run.ctx, binding)
		if err != nil {
			return s.loopFailure(run, "loop-dispatch", err)
		}
	} else {
		out, found, err := s.multi.dispatch.Lookup(ctx, binding)
		if err != nil {
			return s.loopFailure(run, "loop-dispatch", err)
		}
		if !found || (out.Phase != "succeeded" && out.Phase != "failed" && out.Phase != "cancelled") {
			return s.loopFailure(run, "loop-dispatch", fault(f.ResourceBusy))
		}
	}
	plan, err := s.multi.results.DiscoverRunnerResult(ctx, binding)
	if err != nil {
		return s.loopFailure(run, "loop-result", err)
	}
	if nilPort(plan) || !sameToolCallBinding(plan.Binding().Call, binding) {
		return s.loopFailure(run, "loop-result", fault(f.Forbidden))
	}
	locks, err := oc.NormalizeLocks(append(append([]f.LockRequest{}, run.locks...), plan.RequiredLocks()...))
	if err != nil {
		return s.loopFailure(run, "loop-result", err)
	}
	s.mu.Lock()
	run.phase = "tool-writing"
	s.mu.Unlock()
	cause, _ := f.NewJobCause("execution-loop-successor", run.request.ExecutionID.String(), run.round.Fields().ID.String())
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
		if row == nil || row.summary.Status != c.Running || row.summary.CancelRequestedAt != nil || !sameDirectText(current, run.snapshot, run.round) || current.toolPending == nil {
			return fault(f.ConfirmationStale)
		}
		if _, err = s.deps.Projects.RequirePreparingProjectInTx(ctx, tx, row.summary.ProjectID); err != nil {
			return portError(err)
		}
		if err = s.requireCurrentToolFacts(ctx, tx, run); err != nil {
			return err
		}
		// Dynamic Skill changes have their own immutable cutoff/control protocol.
		// Until that protocol is projected here, do not silently omit queued input.
		var queued bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_execution.skill_assignment_ingress WHERE execution_id=$1 AND outcome='queued')`, run.request.ExecutionID.String()).Scan(&queued); err != nil {
			return unavailable(err)
		}
		if queued {
			return fault(f.CapabilityUnsupported)
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
				next, err := newLoopSuccessor(ctx, run, b, message, at, row.summary.Version+1)
				if err != nil {
					return err
				}
				run.next = next
			}
			if !sameLoopResultBinding(run.next.binding, b) || !reflect.DeepEqual(run.next.message, message) || run.next.version != row.summary.Version+1 {
				return fault(f.ConfirmationStale)
			}
			n := run.next
			r := n.round.Fields()
			v := n.round.LoopRound.Fields()
			old := run.round.Fields()
			if err = insertDirectTextEntry(ctx, x, old.ExecutionID, old.ID, int64(v.TranscriptStart-1), "tool_result", message, r.CreatedAt); err != nil {
				return err
			}
			raw, err := directTextJSON(message)
			if err != nil {
				return err
			}
			tag, err := x.Exec(ctx, `INSERT INTO agenteam_execution.loop_tool_results(execution_id,round_id,invocation_id,call_id,operation_id,attempt_id,request_id,result_digest,terminal_digest,message_digest,transcript_sequence,checkpoint_version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, old.ExecutionID.String(), old.ID.String(), binding.InvocationID.String(), binding.CallID, b.OperationID.String(), b.AttemptID.String(), string(b.RequestID), string(b.ResultDigest), string(b.TerminalDigest), string(c.TriggerInputDigest(raw)), int64(v.TranscriptStart-1), int64(n.version), r.CreatedAt.Time())
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return unavailable(nil)
			}
			tag, err = x.Exec(ctx, `INSERT INTO agenteam_execution.rounds(id,execution_id,snapshot_id,start_id,input_binding_id,call_id,input_digest,context_digest,schema_version,content,digest,created_at,transcript_through,round_ordinal,predecessor_round_id,transcript_start) VALUES($1,$2,$3,$4,$5,$6,$7,$8,4,$9,$10,$11,$12,$13,$14,$15)`, r.ID.String(), r.ExecutionID.String(), r.SnapshotID.String(), r.StartID.String(), r.InputBindingID.String(), r.CallID.String(), string(r.Input.Digest), string(r.ContextDigest), n.round.CanonicalBytes(), string(n.round.Digest()), r.CreatedAt.Time(), int64(v.TranscriptStart-1), int64(v.Ordinal), old.ID.String(), int64(v.TranscriptStart))
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return unavailable(nil)
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
		stage := "loop-result"
		if directTextUnknown(err) {
			stage = "loop-successor"
		}
		return s.loopFailure(run, stage, err)
	}
	return s.acceptLoopSuccessor(ctx, run)
}

func newLoopSuccessor(ctx context.Context, run *directTextCall, b tc.RunnerResultBinding, message mc.Message, at f.Instant, version f.Version) (*loopSuccessor, error) {
	previous, ok := run.round.(loopExecutionRound)
	if !ok {
		return nil, invalid()
	}
	request := run.invocation.request.Request()
	request.Messages = append(request.Messages, run.toolFacts.response.Message.Clone(), message.Clone())
	request.ToolChoice = mc.ToolChoice{Kind: "auto"}
	semantic, err := mc.NewSuccessorSemantic(ctx, mc.SuccessorSemanticParts{Actor: request.Actor, Model: request.Model, Messages: request.Messages, Tools: request.Tools, ToolChoice: request.ToolChoice, ResponseFormat: request.ResponseFormat, RetryClass: request.RetryClass})
	if err != nil {
		return nil, portError(err)
	}
	round, err := f.NewID[c.Round]()
	if err != nil {
		return nil, unavailable(err)
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
	fields := c.LoopRoundFields{Round: c.DirectTextRoundFields{ID: round, InputBindingID: input, ExecutionID: old.ExecutionID, SnapshotID: old.SnapshotID, StartID: old.StartID, CallID: call, ContextDigest: old.ContextDigest, Input: request.Input, Messages: request.Messages, CreatedAt: at}, Tools: request.Tools, Choice: request.ToolChoice, Ordinal: previous.LoopRound.Fields().Ordinal + 1, TranscriptStart: roundAssistantSequence(previous) + 2, Predecessor: &prev}
	value, err := c.NewLoopRound(fields)
	if err != nil {
		return nil, err
	}
	return &loopSuccessor{round: loopExecutionRound{value}, request: fixed, version: version, message: message.Clone(), binding: b}, nil
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
	run.invocation = &directTextInvocation{session: session, request: n.request}
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
	return s.acceptLoopSuccessor(ctx, run)
}

func sameLoopResultBinding(a, b tc.RunnerResultBinding) bool {
	return sameToolCallBinding(a.Call, b.Call) && a.OperationID == b.OperationID && a.AttemptID == b.AttemptID && a.RequestID == b.RequestID && a.InputDigest == b.InputDigest && a.Fingerprint == b.Fingerprint && a.TerminalDigest == b.TerminalDigest && a.Outcome == b.Outcome && a.Code == b.Code && a.Attempted == b.Attempted && a.ResultDigest == b.ResultDigest && a.ResultBytes == b.ResultBytes
}
