package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"math"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func decisionLock(id c.DecisionID) f.LockRequest {
	k, _ := f.AggregateLock(f.DecisionAggregate, id.String())
	return f.LockRequest{Key: k, Mode: f.Exclusive}
}

type decisionRecord struct {
	view                           c.DecisionView
	meeting                        c.MeetingID
	operation, round               string
	turn                           c.TurnID
	turnStatus, contributionStatus string
	cancelled                      bool
	currentExecution               *string
	intent                         intentRecord
}

func loadDecision(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID, m c.MeetingID, id c.DecisionID) (decisionRecord, error) {
	var v decisionRecord
	v.meeting, v.view.ID = m, id
	var contribution, execution, turn string
	var version, generation, attempt int64
	var options []byte
	err := x.QueryRow(ctx, `SELECT d.contribution_id,d.execution_id,d.operation_id,d.round_id,d.question,d.options,d.status,d.answer,d.version,c.turn_id,c.status,c.generation,c.attempt,c.current_execution_id,t.status,(t.cancel_requested OR c.cancel_requested) FROM agenteam_meeting.decision_requests d JOIN agenteam_meeting.sessions s ON s.id=d.meeting_id JOIN agenteam_meeting.contributions c ON c.id=d.contribution_id AND c.meeting_id=d.meeting_id JOIN agenteam_meeting.turns t ON t.id=c.turn_id WHERE d.id=$1 AND d.meeting_id=$2 AND s.project_id=$3`, id.String(), m.String(), p.String()).Scan(&contribution, &execution, &v.operation, &v.round, &v.view.Question, &options, &v.view.Status, &v.view.Answer, &version, &turn, &v.contributionStatus, &generation, &attempt, &v.currentExecution, &v.turnStatus, &v.cancelled)
	if errors.Is(err, pgx.ErrNoRows) {
		return v, fault(f.NotFound)
	}
	if err != nil {
		return v, portError(err)
	}
	v.view.ContributionID, err = f.ParseID[c.Contribution](contribution)
	if err != nil {
		return v, fault(f.InternalError)
	}
	v.view.ExecutionID, err = f.ParseID[i.Execution](execution)
	if err != nil {
		return v, fault(f.InternalError)
	}
	v.turn, err = f.ParseID[c.Turn](turn)
	if err != nil || json.Unmarshal(options, &v.view.Options) != nil {
		return v, fault(f.InternalError)
	}
	v.view.Version = f.Version(version)
	v.intent, err = loadIntentByContribution(ctx, x, v.view.ContributionID, f.Version(generation), f.Version(attempt))
	return v, err
}

func (s *Service) LookupRespondDecision(ctx context.Context, a i.Actor, r c.RespondDecisionRequest) (c.Receipt, bool, error) {
	if s == nil || ctx == nil {
		return c.Receipt{}, false, fault(f.InvalidArgument)
	}
	if err := human(a); err != nil {
		return c.Receipt{}, false, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, false, err
	}
	d, err := commandDigest(a, "respond-decision", r)
	if err != nil {
		return c.Receipt{}, false, err
	}
	return s.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d)
}

// RespondDecision commits only Human intent. The original live Builtin owner
// observes the resolution and resumes its own waiting Execution later.
func (s *Service) RespondDecision(ctx context.Context, a i.Actor, r c.RespondDecisionRequest) (c.Receipt, error) {
	if s == nil || ctx == nil {
		return c.Receipt{}, fault(f.InvalidArgument)
	}
	if err := human(a); err != nil {
		return c.Receipt{}, err
	}
	if err := r.Validate(); err != nil {
		return c.Receipt{}, err
	}
	d, err := commandDigest(a, "respond-decision", r)
	if err != nil {
		return c.Receipt{}, err
	}
	if old, found, err := s.lookup(ctx, a, r.ProjectID, r.Meta.IdempotencyKey, d); err != nil || found {
		return old, err
	}
	if s.deps.DecisionAuthority == nil || !sameStore(s.store, s.deps.DecisionAuthority.store) || nilPort(s.deps.DecisionResults) {
		return c.Receipt{}, fault(f.DependencyUnbound)
	}
	if _, err = s.GetSession(ctx, a, r.ProjectID, r.MeetingID); err != nil {
		return c.Receipt{}, err
	}
	initial, err := loadDecision(ctx, s.store, r.ProjectID, r.MeetingID, r.DecisionID)
	if err != nil {
		return c.Receipt{}, err
	}
	if initial.intent.execution == nil {
		return c.Receipt{}, fault(f.InvalidState)
	}
	command, err := commandIdentity(r.ProjectID, r.Meta.IdempotencyKey)
	if err != nil {
		return c.Receipt{}, err
	}
	locks, err := ec.MeetingExecutionReadLocks(r.ProjectID, initial.intent.association())
	if err != nil {
		return c.Receipt{}, err
	}
	locks, err = normalize(append(locks, userLock(a), meetingLock(r.MeetingID, f.Exclusive), decisionLock(r.DecisionID), commandLock(command)))
	if err != nil {
		return c.Receipt{}, err
	}
	if r.ExpectedVersion == f.Version(math.MaxInt64) {
		return c.Receipt{}, fault(f.InvalidState)
	}
	status := "skipped"
	if r.Action == "answer" {
		status = "answered"
	}
	payload, err := decisionPayloadFromIntent(r.DecisionID, initial.intent, status)
	if err != nil {
		return c.Receipt{}, err
	}
	at := now()
	resolved, err := s.deps.DecisionEvents.prepare(ctx, a, r.ProjectID, payload, r.ExpectedVersion+1, at, locks)
	if err != nil {
		return c.Receipt{}, err
	}
	locks = resolved.locks
	cause, _ := f.NewCommandsCause(command)
	var out c.Receipt
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.owner(ctx, tx, a, r.ProjectID, i.Read); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if old, found, err := lookupCommand(ctx, x, r.ProjectID, a, r.Meta.IdempotencyKey, d); err != nil || found {
			out = old
			return err
		}
		if err = s.owner(ctx, tx, a, r.ProjectID, i.Mutate); err != nil {
			return err
		}
		session, err := loadSession(ctx, x, r.ProjectID, r.MeetingID)
		if err != nil {
			return err
		}
		v, err := loadDecision(ctx, x, r.ProjectID, r.MeetingID, r.DecisionID)
		if err != nil {
			return err
		}
		if v.view.Version != r.ExpectedVersion {
			return fault(f.VersionConflict)
		}
		if session.Deleting || session.Status == "proposed" || v.cancelled || !contributionTurnActive(v.turnStatus, v.intent.request) || v.view.Status != "pending" || v.currentExecution == nil || *v.currentExecution != v.view.ExecutionID.String() || v.intent.execution == nil || *v.intent.execution != v.view.ExecutionID || v.intent.association() != initial.intent.association() {
			return fault(f.InvalidState)
		}
		w := &resultWitness{authority: s.deps.DecisionAuthority, tx: tx, project: r.ProjectID, meeting: r.MeetingID, intent: v.intent, active: true}
		defer func() { w.active = false }()
		execution, err := s.deps.DecisionResults.ReadMeetingExecutionInTx(context.WithValue(ctx, resultWitnessKey{}, w), tx, r.ProjectID, v.intent.association())
		if err != nil {
			return portError(err)
		}
		if execution.Summary.Status != ec.Waiting || execution.Summary.CancelRequestedAt != nil || execution.WaitingReason == nil || *execution.WaitingReason != "decision" || execution.WaitingReference == nil || *execution.WaitingReference != r.DecisionID.String() {
			return fault(f.InvalidState)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.decision_requests SET status=$2,answer=$3,version=version+1,resolved_at=$4,resolved_by=$5 WHERE id=$1 AND version=$6 AND status='pending'`, r.DecisionID.String(), status, r.Answer, at, a.Details().UserID, int64(r.ExpectedVersion))
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.VersionConflict)
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET version=version+1,updated_at=$2 WHERE id=$1`, r.MeetingID.String(), now()); err != nil {
			return portError(err)
		}
		out = c.Receipt{MeetingID: r.MeetingID, TurnID: &v.turn, Version: session.Version + 1}
		if err = saveCommand(ctx, x, r.ProjectID, a, r.Meta.IdempotencyKey, "respond-decision", d, out); err != nil {
			return err
		}
		return s.deps.DecisionEvents.appendInTx(ctx, tx, resolved, func(ctx context.Context, tx f.Tx) error { return s.owner(ctx, tx, a, r.ProjectID, i.Mutate) })
	})
	if err = commitError(result); err != nil {
		return c.Receipt{}, err
	}
	s.invalidateMeeting(r.ProjectID, r.MeetingID)
	return out, nil
}
