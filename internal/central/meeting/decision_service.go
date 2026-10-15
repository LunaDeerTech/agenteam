package meeting

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
	"github.com/jackc/pgx/v5"
)

// DecisionService waits inside the original synchronous Builtin invocation.
// It owns no execution engine, goroutine, timeout or replacement Tool call.
type DecisionService struct {
	store     Store
	authority *Authority
	tools     ec.MeetingDecisionToolAuthority
	results   ec.MeetingResultReader
	events    *DecisionEvents
	realtime  rc.Publisher
}

func NewDecisionService(store Store, a *Authority, tools ec.MeetingDecisionToolAuthority, results ec.MeetingResultReader, events *DecisionEvents, realtime rc.Publisher) (*DecisionService, error) {
	if nilPort(store) || a == nil || !sameStore(store, a.store) || nilPort(tools) || nilPort(results) || events == nil || events.authority == nil || !sameStore(store, events.authority.store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &DecisionService{store: store, authority: a, tools: tools, results: results, events: events, realtime: realtime}, nil
}

func ParseDecisionInput(raw []byte) (c.DecisionInput, error) {
	var out c.DecisionInput
	if len(raw) > 48<<10 {
		return out, fault(f.InvalidArgument)
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return out, fault(f.InvalidArgument)
	}
	d := json.NewDecoder(bytes.NewReader(canonical))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || out.Validate() != nil {
		return c.DecisionInput{}, fault(f.InvalidArgument)
	}
	if out.Options == nil {
		out.Options = []string{}
	}
	return out, nil
}

type decisionCall struct {
	call   ec.MeetingDecisionCall
	input  c.DecisionInput
	id     c.DecisionID
	intent intentRecord
	locks  []f.LockRequest
}

func (s *DecisionService) prepare(ctx context.Context, call ec.MeetingDecisionCall, input c.DecisionInput, lookup bool) (decisionCall, error) {
	var out decisionCall
	if s == nil || ctx == nil || call.Validate() != nil || input.Validate() != nil {
		return out, fault(f.InvalidArgument)
	}
	locks, err := s.tools.DiscoverMeetingDecisionTool(ctx, call, lookup)
	if err != nil {
		return out, portError(err)
	}
	intent, err := decisionIntent(ctx, s.store, call)
	if err != nil {
		return out, err
	}
	id, err := f.ParseID[c.Decision](call.OperationID.String())
	if err != nil {
		return out, fault(f.InvalidArgument)
	}
	m, err := f.ParseID[c.Meeting](intent.request.Trigger.MeetingID)
	if err != nil {
		return out, fault(f.InvalidState)
	}
	locks, err = normalize(append(locks, projectLock(call.Call.ProjectID, f.Shared), meetingLock(m, f.Exclusive), decisionLock(id)))
	if err != nil {
		return out, err
	}
	return decisionCall{call: call, input: input, id: id, intent: intent, locks: locks}, nil
}

func (s *DecisionService) transaction(ctx context.Context, v decisionCall, phase string, fn func(context.Context, f.Tx, postgres.SQLExecutor, *decisionWitness) error) error {
	cause, err := f.NewJobCause("meeting-decision", v.call.Call.ExecutionID.String(), v.call.OperationID.String())
	if err != nil {
		return portError(err)
	}
	m, _ := f.ParseID[c.Meeting](v.intent.request.Trigger.MeetingID)
	resumed := false
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, v.locks); err != nil {
			return portError(err)
		}
		if err := s.tools.RequireMeetingDecisionToolBindingInTx(ctx, tx, s.store); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		w := &decisionWitness{authority: s.authority, tx: tx, call: v.call, meeting: m, decision: v.id, phase: phase, active: true}
		defer func() { w.active = false }()
		err = fn(context.WithValue(ctx, decisionWitnessKey{}, w), tx, x, w)
		resumed = err == nil && phase == "wait" && w.phase == "resume"
		return err
	})
	if err := commitError(result); err != nil {
		return err
	}
	if phase == "create" || phase == "cancel" || resumed {
		invalidateMeeting(s.realtime, v.call.Call.ProjectID, m)
	}
	return nil
}

func (v decisionCall) requireInput(raw []byte) error {
	input, err := ParseDecisionInput(raw)
	if err != nil {
		return err
	}
	a, _ := canonical(input)
	b, _ := canonical(v.input)
	if !bytes.Equal(a, b) {
		return fault(f.Forbidden)
	}
	return nil
}

func (s *DecisionService) Request(ctx context.Context, call ec.MeetingDecisionCall, input c.DecisionInput) (c.DecisionResult, error) {
	v, err := s.prepare(ctx, call, input, false)
	if err != nil {
		return c.DecisionResult{}, err
	}
	payload, err := decisionPayloadFromIntent(v.id, v.intent, "pending")
	if err != nil {
		return c.DecisionResult{}, err
	}
	at := now()
	created, err := s.events.prepare(ctx, call.Call.Actor, call.Call.ProjectID, payload, 1, at, v.locks)
	if err != nil {
		return c.DecisionResult{}, err
	}
	v.locks = created.locks
	err = s.transaction(ctx, v, "create", func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, w *decisionWitness) error {
		return s.tools.WithMeetingDecisionToolInTx(ctx, tx, call, v.id.String(), false, func(ctx context.Context, raw []byte) error {
			if err := v.requireInput(raw); err != nil {
				return err
			}
			var exists bool
			if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_meeting.decision_requests WHERE operation_id=$1 OR id=$2)`, call.OperationID.String(), v.id.String()).Scan(&exists); err != nil {
				return portError(err)
			}
			if exists {
				return fault(f.ResourceBusy)
			}
			options, err := json.Marshal(v.input.Options)
			if err != nil {
				return portError(err)
			}
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.decision_requests(id,meeting_id,contribution_id,execution_id,operation_id,round_id,question,options,status,version,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'pending',1,$9)`, v.id.String(), v.intent.request.Trigger.MeetingID, v.intent.request.Trigger.ContributionID, call.Call.ExecutionID.String(), call.OperationID.String(), call.Call.RoundID, v.input.Question, options, at); err != nil {
				return portError(err)
			}
			w.phase = "enter"
			if err = s.tools.SetMeetingDecisionToolWaitingInTx(ctx, tx, call, v.id.String(), true); err != nil {
				return portError(err)
			}
			// The contribution remains running. Waiting belongs exclusively to
			// its original Execution; the Decision is its visible control object.
			_, err = x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET version=version+1,updated_at=$2 WHERE id=$1`, v.intent.request.Trigger.MeetingID, at)
			if err != nil {
				return portError(err)
			}
			return s.events.appendInTx(ctx, tx, created, func(ctx context.Context, tx f.Tx) error { return s.requireEventCallInTx(ctx, tx, v) })
		})
	})
	if err != nil {
		return c.DecisionResult{}, err
	}
	for {
		if ctx.Err() != nil {
			return c.DecisionResult{}, s.cancelOriginal(ctx, v)
		}
		var out c.DecisionResult
		resolved := false
		err = s.transaction(ctx, v, "wait", func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, w *decisionWitness) error {
			return s.tools.WithMeetingDecisionToolInTx(ctx, tx, call, v.id.String(), true, func(ctx context.Context, raw []byte) error {
				if err := v.requireInput(raw); err != nil {
					return err
				}
				record, err := loadDecision(ctx, x, call.Call.ProjectID, w.meeting, v.id)
				if err != nil {
					return err
				}
				if record.view.Status == "pending" {
					return nil
				}
				out = c.DecisionResult{DecisionID: v.id, Version: record.view.Version, Status: record.view.Status, Answer: record.view.Answer}
				if out.Validate() != nil {
					return fault(f.InvalidState)
				}
				w.phase = "resume"
				if err = s.tools.SetMeetingDecisionToolWaitingInTx(ctx, tx, call, v.id.String(), false); err != nil {
					return portError(err)
				}
				resolved = true
				return nil
			})
		})
		if err != nil {
			if ctx.Err() != nil {
				return c.DecisionResult{}, s.cancelOriginal(ctx, v)
			}
			return c.DecisionResult{}, err
		}
		if resolved {
			return out, nil
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
		}
	}
}

// Only this original Request stack can cancel its admitted decision after its
// own context is cancelled. Cleanup joins synchronously, cannot resume an
// Execution, and preserves Unknown if the terminal domain fact is uncertain.
func (s *DecisionService) cancelOriginal(ctx context.Context, v decisionCall) error {
	if ctx.Err() == nil {
		return fault(f.Forbidden)
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	m, _ := f.ParseID[c.Meeting](v.intent.request.Trigger.MeetingID)
	decisions, err := prepareDecisionCancellation(cleanup, s.store, s.events, v.call.Call.Actor, decisionEventScope{project: v.call.Call.ProjectID, meeting: m, execution: v.call.Call.ExecutionID, decision: v.id}, v.locks)
	if err != nil {
		return f.NewFault(f.CommitUnknown, f.Unknown).WithCause(err)
	}
	v.locks = decisions.locks
	err = s.transaction(cleanup, v, "cancel", func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, _ *decisionWitness) error {
		if err := decisions.applyInTx(ctx, tx, s.store, func(ctx context.Context, tx f.Tx) error { return s.requireEventCallInTx(ctx, tx, v) }); err != nil {
			return err
		}
		var status string
		if err := x.QueryRow(ctx, `SELECT status FROM agenteam_meeting.decision_requests WHERE id=$1 AND operation_id=$2 AND execution_id=$3`, v.id.String(), v.call.OperationID.String(), v.call.Call.ExecutionID.String()).Scan(&status); err != nil {
			return portError(err)
		}
		if status != "cancelled" && status != "answered" && status != "skipped" {
			return fault(f.InvalidState)
		}
		return nil
	})
	if err != nil {
		return f.NewFault(f.CommitUnknown, f.Unknown).WithCause(err)
	}
	return fault(f.InvalidState)
}

// Lookup observes only the original receipt; it never resumes waiting or
// creates another decision. An answered decision whose original Execution
// still waits remains unresolved ownership, including after process loss.
func (s *DecisionService) Lookup(ctx context.Context, call ec.MeetingDecisionCall, input c.DecisionInput) (c.DecisionResult, bool, error) {
	v, err := s.prepare(ctx, call, input, true)
	if err != nil {
		return c.DecisionResult{}, false, err
	}
	var out c.DecisionResult
	found := false
	err = s.transaction(ctx, v, "lookup", func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, w *decisionWitness) error {
		return s.tools.WithCommittedMeetingDecisionToolInTx(ctx, tx, call, func(ctx context.Context, raw []byte) error {
			if err := v.requireInput(raw); err != nil {
				return err
			}
			record, err := loadDecision(ctx, x, call.Call.ProjectID, w.meeting, v.id)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			var failure *f.Fault
			if errors.As(err, &failure) && failure.Code == f.NotFound {
				return nil
			}
			if err != nil {
				return err
			}
			if record.operation != call.OperationID.String() || record.round != call.Call.RoundID || record.view.ExecutionID != call.Call.ExecutionID {
				return fault(f.Forbidden)
			}
			if record.view.Status != "answered" && record.view.Status != "skipped" {
				return nil
			}
			rw := &resultWitness{authority: s.authority, tx: tx, project: call.Call.ProjectID, meeting: w.meeting, intent: record.intent, active: true}
			defer func() { rw.active = false }()
			execution, err := s.results.ReadMeetingExecutionInTx(context.WithValue(ctx, resultWitnessKey{}, rw), tx, call.Call.ProjectID, record.intent.association())
			if err != nil {
				return portError(err)
			}
			if execution.Summary.Status != ec.Running || execution.Summary.CancelRequestedAt != nil {
				return nil
			}
			out = c.DecisionResult{DecisionID: v.id, Version: record.view.Version, Status: record.view.Status, Answer: record.view.Answer}
			if out.Validate() != nil {
				return fault(f.InvalidState)
			}
			found = true
			return nil
		})
	})
	return out, found, err
}

func (s *DecisionService) RequireBuiltinBindingInTx(ctx context.Context, tx f.Tx, store any) error {
	if s == nil || !sameStore(s.store, store) {
		return fault(f.DependencyUnbound)
	}
	return s.tools.RequireMeetingDecisionToolBindingInTx(ctx, tx, store)
}
