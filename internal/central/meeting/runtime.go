package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/realtime/contract"
	"github.com/jackc/pgx/v5"
)

type Launcher interface {
	Launch(context.Context, i.Actor, ec.LaunchRequest) (ec.LaunchResult, error)
	LookupLaunch(context.Context, i.Actor, ec.LaunchLookupKey, f.Digest) (ec.LaunchLookup, error)
}

type ExecutionResult = ec.MeetingExecutionResult
type RuntimeDependencies struct {
	Projects          ec.PreparationProjectGate
	Launcher          Launcher
	Executor          ec.MeetingAssociatedExecutor
	Results           ec.MeetingResultReader
	Authority         *Authority
	Control           ec.MeetingControl
	Summaries         SummaryGenerator
	Actor             i.ServiceRegistration
	Retirement        ec.MeetingExecutionRetirementReader
	DeletionAuthority *DeletionAuthority
	DeletionProjects  DeletionProjectGate
	LifecycleEvents   *ProposalEvents
	DecisionEvents    *DecisionEvents
	Audit             au.Appender
	Realtime          rc.Publisher
}
type Runtime struct {
	store        Store
	deps         RuntimeDependencies
	mu           sync.Mutex
	active       map[c.MeetingID]bool
	stopped      bool
	driverCancel context.CancelFunc
}

func NewRuntime(store Store, deps RuntimeDependencies) (*Runtime, error) {
	if nilPort(store) || nilPort(deps.Projects) || nilPort(deps.Launcher) || nilPort(deps.Executor) || nilPort(deps.Results) || deps.Authority == nil || !sameStore(deps.Authority.store, store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &Runtime{store: store, deps: deps, active: map[c.MeetingID]bool{}}, nil
}

type AdvanceResult struct {
	MeetingID  c.MeetingID
	TurnID     *c.TurnID
	Phase      string
	Executions int
}

func (r *Runtime) enter(id c.MeetingID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return fault(f.InvalidState)
	}
	if r.active[id] {
		return fault(f.ResourceBusy)
	}
	r.active[id] = true
	return nil
}
func (r *Runtime) leave(id c.MeetingID) { r.mu.Lock(); delete(r.active, id); r.mu.Unlock() }
func (r *Runtime) Stop() {
	if r != nil {
		r.mu.Lock()
		r.stopped = true
		if r.driverCancel != nil {
			r.driverCancel()
		}
		r.mu.Unlock()
	}
}
func (r *Runtime) Joined() bool {
	if r == nil {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stopped && r.driverCancel == nil && len(r.active) == 0
}
func (r *Runtime) Drain(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if r.Joined() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Advance progresses one Meeting from its durable facts. It delegates every
// actual Agent run to the original AssociatedExecutor. A retained/unknown
// launch is looked up using its original key, never reissued as a probe.
func (r *Runtime) Advance(ctx context.Context, p i.ProjectID, id c.MeetingID) (AdvanceResult, error) {
	out := AdvanceResult{MeetingID: id, Phase: "idle"}
	if r == nil || ctx == nil || p.Validate() != nil || id.Validate() != nil {
		return out, fault(f.InvalidArgument)
	}
	if err := r.enter(id); err != nil {
		return out, err
	}
	defer r.leave(id)
	plan, err := r.plan(ctx, p, id)
	if err != nil {
		var known *f.Fault
		if errors.As(err, &known) && known.Code == f.ProjectNotActive {
			// Only a separately validated, already accepted deletion can use
			// its cleanup gate after the Project stops accepting new work.
			return r.advanceDeletion(ctx, p, id)
		}
		return out, err
	}
	out.TurnID = plan.turn
	out.Phase = plan.phase
	if plan.phase == "deleting" {
		return r.advanceDeletion(ctx, p, id)
	}
	if plan.phase == "finalizing" && plan.turn != nil {
		return out, r.finalize(ctx, p, id, *plan.turn)
	}
	if plan.phase == "summary-cancelling" && plan.turn != nil {
		return out, r.cancelTurnSummary(ctx, p, id, *plan.turn)
	}
	if len(plan.intents) == 1 {
		value, e := r.advanceIntent(ctx, p, id, plan.intents[0], plan.phase)
		out.Executions = value.Executions
		out.Phase = value.Phase
		return out, e
	}
	type completed struct {
		result AdvanceResult
		err    error
	}
	results := make(chan completed, len(plan.intents))
	for _, intent := range plan.intents {
		go func(intent intentRecord) {
			value, e := r.advanceIntent(ctx, p, id, intent, plan.phase)
			results <- completed{value, e}
		}(intent)
	}
	var failures []error
	for range plan.intents {
		value := <-results
		out.Executions += value.result.Executions
		if value.result.Phase == "launch_unknown" {
			out.Phase = value.result.Phase
		}
		if value.err != nil {
			failures = append(failures, value.err)
		}
	}
	return out, errors.Join(failures...)
}

// Each parallel contribution keeps its own original launch/Executor call. The
// parent Advance joins every branch, even when one branch fails or waits busy.
func (r *Runtime) advanceIntent(ctx context.Context, p i.ProjectID, id c.MeetingID, intent intentRecord, phase string) (AdvanceResult, error) {
	out := AdvanceResult{MeetingID: id, Phase: phase}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if intent.execution == nil {
		scope, e := i.InProject(p)
		if e != nil {
			return out, portError(e)
		}
		actor, e := r.deps.Actor.Actor(intent.id.String(), scope)
		if e != nil {
			return out, portError(e)
		}
		key := ec.LaunchLookupKey{ProjectID: p, AgentID: intent.request.AgentID, IdempotencyKey: intent.request.Meta.IdempotencyKey}
		var execution *ec.Summary
		if intent.unknown {
			found, e := r.deps.Launcher.LookupLaunch(ctx, actor, key, intent.requestDigest)
			if e != nil {
				return out, e
			}
			if found.Found {
				execution = found.Execution
			}
		} else {
			if e = r.markAttempt(ctx, p, id, intent.id, true); e != nil {
				return out, e
			}
			launched, e := r.deps.Launcher.Launch(ctx, actor, intent.request)
			if e != nil {
				var failure *f.Fault
				if errors.As(e, &failure) && (failure.CommitState == f.NotStarted || failure.CommitState == f.NotCommitted) {
					if reset := r.markAttempt(ctx, p, id, intent.id, false); reset != nil {
						return out, reset
					}
				}
				if errors.As(e, &failure) && failure.Code == f.AgentBusy {
					if busyErr := r.observeBusy(ctx, p, id, intent); busyErr != nil {
						return out, busyErr
					}
					return out, nil
				}
				return out, e
			}
			execution = &launched.Execution
		}
		if execution == nil {
			out.Phase = "launch_unknown"
			return out, nil
		}
		if err := r.associate(ctx, p, id, intent, *execution); err != nil {
			return out, err
		}
		value := execution.ID
		intent.execution = &value
	}
	association := intent.association()
	// AssociatedExecutor's accepted/terminal observation is scheduling data:
	// its original worker may already have returned and forgotten a terminal
	// job. Read the durable Execution through its private same-Tx authority
	// before asking the driver to advance an already completed association.
	result, e := r.readResult(ctx, p, id, intent)
	if e != nil {
		return out, e
	}
	if result.Summary.Status.Terminal() {
		return out, r.publishResult(ctx, p, id, intent, result)
	}
	if intent.cancelRequested {
		if e := r.requestCancellation(ctx, p, id, intent); e != nil {
			return out, e
		}
	}
	_, advanceErr := r.deps.Executor.AdvanceMeeting(ctx, p, association)
	out.Executions++
	result, e = r.readResult(ctx, p, id, intent)
	if e != nil {
		return out, errors.Join(advanceErr, e)
	}
	if result.Summary.Status.Terminal() {
		return out, errors.Join(advanceErr, r.publishResult(ctx, p, id, intent, result))
	}
	return out, advanceErr
}

func (v intentRecord) association() ec.AssociatedMeeting {
	var id i.ExecutionID
	if v.execution != nil {
		id = *v.execution
	}
	return ec.AssociatedMeeting{ExecutionID: id, AgentID: v.request.AgentID, Key: v.request.Meta.IdempotencyKey, Digest: v.requestDigest, Trigger: v.request.Trigger, Generation: *v.request.Lineage.ContributionGeneration, Attempt: *v.request.Lineage.ContributionAttempt}
}

type runtimePlan struct {
	turn    *c.TurnID
	phase   string
	intents []intentRecord
}

func (r *Runtime) transaction(ctx context.Context, p i.ProjectID, id c.MeetingID, action string, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	return r.transactionWithLocks(ctx, p, id, action, nil, fn)
}

func (r *Runtime) transactionWithLocks(ctx context.Context, p i.ProjectID, id c.MeetingID, action string, additional []f.LockRequest, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	locks, err := r.runtimeLocks(ctx, p, id, append(append([]f.LockRequest{}, additional...), projectLock(p, f.Shared), meetingLock(id, f.Exclusive)))
	if err != nil {
		return err
	}
	cause, err := readCause("meeting.runtime." + action)
	if err != nil {
		return err
	}
	var writes *runtimeMutation
	result := r.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := r.requireRuntimeProjectInTx(ctx, tx, p, id); err != nil {
			return err
		}
		x, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		writes = &runtimeMutation{SQLExecutor: x}
		return fn(ctx, tx, writes)
	})
	return publishRuntimeMutation(r.deps.Realtime, p, id, result, writes)
}

type contributionRecord struct {
	id                  c.ContributionID
	participant         c.ParticipantID
	agent               i.AgentID
	state               string
	generation, attempt f.Version
	execution           *i.ExecutionID
	cancelRequested     bool
}

func (r *Runtime) plan(ctx context.Context, p i.ProjectID, id c.MeetingID) (runtimePlan, error) {
	out := runtimePlan{phase: "idle", intents: []intentRecord{}}
	err := r.transaction(ctx, p, id, "plan", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		session, err := loadSession(ctx, x, p, id)
		if err != nil {
			return err
		}
		if session.Deleting {
			out.phase = "deleting"
			return nil
		}
		if session.Status == "proposed" {
			return fault(f.InvalidState)
		}
		// Deletion may finish an original Summary, but cannot start a
		// historical replacement while its cleanup is still pending.
		if !session.Deleting {
			out.intents, err = loadHistoricalIntents(ctx, x, id)
			if err != nil {
				return err
			}
		}
		if len(out.intents) != 0 {
			out.phase = "revising"
		}
		var turnID, mode, status string
		var cancel bool
		// An interrupt cannot bypass the original cancellation barrier. Once
		// that Turn is cancelled, its recorded successor takes the next slot;
		// all remaining queued Turns retain their original FIFO order.
		err = x.QueryRow(ctx, `SELECT t.id,t.mode,t.status,t.cancel_requested
 FROM agenteam_meeting.turns t
 WHERE t.meeting_id=$1 AND t.status IN('queued','running','finalizing')
 ORDER BY CASE WHEN t.status IN('running','finalizing') THEN 0
  WHEN EXISTS(SELECT 1 FROM agenteam_meeting.turns previous
   WHERE previous.meeting_id=t.meeting_id AND previous.status='cancelled' AND previous.interrupted_by_id=t.id) THEN 1
  ELSE 2 END,t.created_at,t.id LIMIT 1`, id.String()).Scan(&turnID, &mode, &status, &cancel)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return portError(err)
		}
		turn, err := f.ParseID[c.Turn](turnID)
		if err != nil {
			return fault(f.InternalError)
		}
		out.turn = &turn
		if cancel || session.Deleting {
			return r.planCancellation(ctx, x, id, turn, &out)
		}
		if status == "finalizing" {
			out.phase = "finalizing"
			return nil
		}
		if status == "queued" {
			if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.turns SET status='running',version=version+1 WHERE id=$1 AND status='queued'`, turnID); err != nil {
				return portError(err)
			}
		}
		out.phase = "running"
		contributions, err := loadPendingContributions(ctx, x, turn)
		if err != nil {
			return err
		}
		if len(contributions) == 0 {
			if len(out.intents) != 0 {
				return nil
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_meeting.turns SET status='finalizing',version=version+1,result=CASE WHEN EXISTS(SELECT 1 FROM agenteam_meeting.contributions WHERE turn_id=$1 AND kind='agent' AND status IN('failed','cancelled')) THEN 'partial_failure' ELSE 'success' END WHERE id=$1 AND status='running'`, turnID)
			out.phase = "finalizing"
			return portError(err)
		}
		if mode == "sequential" {
			contributions = contributions[:1]
		}
		var commonID *c.InputID
		if mode == "parallel" {
			var raw string
			err = x.QueryRow(ctx, `SELECT l.input_id FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.contributions c ON c.id=l.contribution_id WHERE c.turn_id=$1 AND c.generation=l.generation AND c.attempt=l.attempt ORDER BY c.sort_order LIMIT 1`, turnID).Scan(&raw)
			if err == nil {
				v, e := f.ParseID[c.Input](raw)
				if e != nil {
					return fault(f.InternalError)
				}
				commonID = &v
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return portError(err)
			}
		}
		for _, contribution := range contributions {
			request := launchRequest(p, id, turn, contribution)
			existing, e := loadIntentByContribution(ctx, x, contribution.id, contribution.generation, contribution.attempt)
			if e == nil {
				existing.cancelRequested = contribution.cancelRequested
				out.intents = append(out.intents, existing)
				continue
			}
			var known *f.Fault
			if !errors.As(e, &known) || known.Code != f.NotFound {
				return e
			}
			if commonID == nil {
				frozen, e := captureContext(ctx, x, p, id, turn)
				if e != nil {
					return e
				}
				inputID, e := saveFrozenContext(ctx, x, id, frozen)
				if e != nil {
					return e
				}
				commonID = &inputID
			}
			stored, e := saveLaunchIntent(ctx, x, id, contribution.id, *commonID, request)
			if e != nil {
				return e
			}
			out.intents = append(out.intents, stored)
		}
		return nil
	})
	return out, err
}
func launchRequest(p i.ProjectID, m c.MeetingID, t c.TurnID, v contributionRecord) ec.LaunchRequest {
	generation, attempt := v.generation, v.attempt
	return ec.LaunchRequest{ProjectID: p, AgentID: v.agent, Trigger: ec.Trigger{Kind: "meeting", MeetingID: m.String(), TurnID: t.String(), ContributionID: v.id.String(), ParticipantID: v.participant.String()}, Purpose: "meeting/response", Policy: ec.Policy{SchemaVersion: 1, DeniedToolIDs: []i.ToolID{}, AllowedResourceConstraints: []json.RawMessage{}}, Lineage: ec.Lineage{ContributionGeneration: &generation, ContributionAttempt: &attempt}, Meta: f.CommandMeta{IdempotencyKey: f.IdempotencyKey("meeting:" + v.id.String() + ":" + strconv.FormatInt(int64(generation), 10) + ":" + strconv.FormatInt(int64(attempt), 10))}}
}
func loadPendingContributions(ctx context.Context, x postgres.SQLExecutor, turn c.TurnID) ([]contributionRecord, error) {
	rows, err := x.Query(ctx, `SELECT c.id,c.participant_id,p.source_id,c.status,c.generation,c.attempt,c.current_execution_id,c.cancel_requested FROM agenteam_meeting.contributions c JOIN agenteam_meeting.participants p ON p.id=c.participant_id WHERE c.turn_id=$1 AND c.kind='agent' AND c.status IN('pending','waiting_for_agent','running') ORDER BY c.sort_order`, turn.String())
	if err != nil {
		return nil, portError(err)
	}
	defer rows.Close()
	out := []contributionRecord{}
	for rows.Next() {
		var v contributionRecord
		var id, participant, agent string
		var generation, attempt int64
		var execution *string
		if err = rows.Scan(&id, &participant, &agent, &v.state, &generation, &attempt, &execution, &v.cancelRequested); err != nil {
			return nil, portError(err)
		}
		if v.id, err = f.ParseID[c.Contribution](id); err != nil {
			return nil, fault(f.InternalError)
		}
		if v.participant, err = f.ParseID[c.Participant](participant); err != nil {
			return nil, fault(f.InternalError)
		}
		if v.agent, err = f.ParseID[i.Agent](agent); err != nil {
			return nil, fault(f.InternalError)
		}
		v.generation, v.attempt = f.Version(generation), f.Version(attempt)
		if execution != nil {
			id, e := f.ParseID[i.Execution](*execution)
			if e != nil {
				return nil, fault(f.InternalError)
			}
			v.execution = &id
		}
		out = append(out, v)
	}
	return out, portError(rows.Err())
}
func loadIntentByContribution(ctx context.Context, x postgres.SQLExecutor, id c.ContributionID, generation, attempt f.Version) (intentRecord, error) {
	var raw []byte
	var requestID, key string
	err := x.QueryRow(ctx, `SELECT request_data,request_id,idempotency_key FROM agenteam_meeting.launch_intents WHERE contribution_id=$1 AND generation=$2 AND attempt=$3`, id.String(), int64(generation), int64(attempt)).Scan(&raw, &requestID, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return intentRecord{}, fault(f.NotFound)
	}
	if err != nil {
		return intentRecord{}, portError(err)
	}
	var r ec.LaunchRequest
	if json.Unmarshal(raw, &r) != nil {
		return intentRecord{}, fault(f.InternalError)
	}
	r.Meta.RequestID, err = f.ParseID[f.Request](requestID)
	if err != nil {
		return intentRecord{}, fault(f.InternalError)
	}
	r.Meta.IdempotencyKey = f.IdempotencyKey(key)
	return loadIntentForRequest(ctx, x, r)
}
func (r *Runtime) markAttempt(ctx context.Context, p i.ProjectID, m c.MeetingID, intent c.IntentID, unknown bool) error {
	return r.transaction(ctx, p, m, "launch-attempt", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		tag, err := x.Exec(ctx, `UPDATE agenteam_meeting.launch_intents SET launch_unknown=$3 WHERE meeting_id=$1 AND id=$2 AND execution_id IS NULL`, m.String(), intent.String(), unknown)
		if err != nil {
			return portError(err)
		}
		if tag.RowsAffected() != 1 {
			return fault(f.ConfirmationStale)
		}
		if unknown {
			// Leaving waiting under the same Meeting gate closes Skip's window
			// before a potentially successful Launch can be dispatched.
			_, err = x.Exec(ctx, `UPDATE agenteam_meeting.contributions SET status='pending',waiting_on_execution_id=NULL WHERE id=(SELECT contribution_id FROM agenteam_meeting.launch_intents WHERE id=$1) AND status='waiting_for_agent' AND current_execution_id IS NULL`, intent.String())
			if err != nil {
				return portError(err)
			}
		}
		return nil
	})
}
func (r *Runtime) associate(ctx context.Context, p i.ProjectID, m c.MeetingID, intent intentRecord, summary ec.Summary) error {
	if summary.ID.Validate() != nil || summary.ProjectID != p || summary.AgentID != intent.request.AgentID || summary.Trigger != intent.request.Trigger {
		return fault(f.ConfirmationStale)
	}
	return r.transaction(ctx, p, m, "associate", func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		current, err := loadIntentForRequest(ctx, x, intent.request)
		if err != nil {
			return err
		}
		if current.execution != nil {
			if *current.execution != summary.ID {
				return fault(f.ConfirmationStale)
			}
			return nil
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.launch_intents SET execution_id=$2,launch_unknown=false WHERE id=$1`, intent.id.String(), summary.ID.String()); err != nil {
			return portError(err)
		}
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.contribution_executions(execution_id,meeting_id,contribution_id,generation,attempt,status,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, summary.ID.String(), m.String(), intent.request.Trigger.ContributionID, int64(*intent.request.Lineage.ContributionGeneration), int64(*intent.request.Lineage.ContributionAttempt), string(summary.Status), now()); err != nil {
			return portError(err)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_meeting.contributions SET current_execution_id=$2,status='running',waiting_on_execution_id=NULL,started_at=COALESCE(started_at,$3) WHERE id=$1 AND generation=$4 AND attempt=$5 AND status IN('pending','waiting_for_agent')`, intent.request.Trigger.ContributionID, summary.ID.String(), now(), int64(*intent.request.Lineage.ContributionGeneration), int64(*intent.request.Lineage.ContributionAttempt))
		return portError(err)
	})
}
func (r *Runtime) publishResult(ctx context.Context, p i.ProjectID, m c.MeetingID, intent intentRecord, result ExecutionResult) error {
	s := result.Summary
	if intent.execution == nil || s.ID != *intent.execution || s.ProjectID != p || s.AgentID != intent.request.AgentID || s.Trigger != intent.request.Trigger || !s.Status.Terminal() {
		return fault(f.ConfirmationStale)
	}
	if s.Status == ec.Succeeded && (result.FinalText == nil || !c.ValidText(*result.FinalText, c.MaxContentBytes)) {
		return fault(f.InvalidState)
	}
	scope, err := i.InProject(p)
	if err != nil {
		return portError(err)
	}
	actor, err := r.deps.Actor.Actor(intent.id.String(), scope)
	if err != nil {
		return portError(err)
	}
	decisions, err := prepareDecisionCancellation(ctx, r.store, r.deps.DecisionEvents, actor, decisionEventScope{project: p, meeting: m, execution: s.ID}, []f.LockRequest{projectLock(p, f.Shared), meetingLock(m, f.Exclusive)})
	if err != nil {
		return err
	}
	return r.transactionWithLocks(ctx, p, m, "publish-result", decisions.locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		session, err := loadSession(ctx, x, p, m)
		if err != nil {
			return err
		}
		var cancelling bool
		var turnStatus string
		if err = x.QueryRow(ctx, `SELECT cancel_requested,status FROM agenteam_meeting.turns WHERE id=$1 AND meeting_id=$2`, intent.request.Trigger.TurnID, m.String()).Scan(&cancelling, &turnStatus); err != nil {
			return portError(err)
		}
		cancelling = cancelling || session.Deleting
		var current *string
		var state string
		var generation, attempt int64
		var contributionCancel bool
		err = x.QueryRow(ctx, `SELECT status,generation,attempt,current_execution_id,cancel_requested FROM agenteam_meeting.contributions WHERE id=$1 AND meeting_id=$2`, intent.request.Trigger.ContributionID, m.String()).Scan(&state, &generation, &attempt, &current, &contributionCancel)
		if err != nil {
			return portError(err)
		}
		if current == nil || *current != s.ID.String() || generation != int64(*intent.request.Lineage.ContributionGeneration) || attempt != int64(*intent.request.Lineage.ContributionAttempt) {
			return fault(f.ConfirmationStale)
		}
		if c.TerminalContribution(state) {
			return nil
		}
		cancelling = cancelling || contributionCancel
		if err = decisions.applyInTx(ctx, tx, r.store, func(ctx context.Context, tx f.Tx) error {
			if r.deps.DeletionAuthority == nil {
				return fault(f.DependencyUnbound)
			}
			if err := r.deps.DeletionAuthority.requireCurrentInTx(ctx, tx, r.store); err != nil {
				return err
			}
			return r.requireRuntimeProjectInTx(ctx, tx, p, m)
		}); err != nil {
			return err
		}
		var message *string
		next := string(s.Status)
		if cancelling {
			next = "cancelled"
		}
		if s.Status == ec.Succeeded && !cancelling {
			next = "completed"
			id, err := f.NewID[c.Message]()
			if err != nil {
				return portError(err)
			}
			raw, err := canonical([]c.InlineNode{{Type: "text", Text: *result.FinalText}})
			if err != nil {
				return err
			}
			if _, err = x.Exec(ctx, `INSERT INTO agenteam_meeting.messages(id,meeting_id,participant_id,content,content_digest,execution_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, id.String(), m.String(), intent.request.Trigger.ParticipantID, raw, digest(raw).String(), s.ID.String(), now()); err != nil {
				return portError(err)
			}
			v := id.String()
			message = &v
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.contribution_executions SET status=$2,message_id=$3 WHERE execution_id=$1`, s.ID.String(), string(s.Status), message); err != nil {
			return portError(err)
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.contributions SET status=$2,current_message_id=COALESCE($3,current_message_id),completed_at=$4 WHERE id=$1`, intent.request.Trigger.ContributionID, next, message, now()); err != nil {
			return portError(err)
		}
		if message != nil && turnStatus == "completed" && session.Summary != nil {
			// Preserve the old summary and its coverage identity until the next
			// ordinary Turn finalizes. Failure/cancellation leaves input intact.
			if _, err = x.Exec(ctx, `UPDATE agenteam_meeting.sessions SET summary=jsonb_set(summary,'{pending_update}','true'::jsonb),version=version+1,updated_at=$2 WHERE id=$1`, m.String(), now()); err != nil {
				return portError(err)
			}
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_meeting.timeline_items SET version=version+1 WHERE contribution_id=$1`, intent.request.Trigger.ContributionID)
		return portError(err)
	})
}
