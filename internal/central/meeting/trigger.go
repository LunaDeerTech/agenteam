package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// Authority owns the durable Meeting launch intent. It does not issue Project
// or Execution permission. Composition binds it to the Project-owned gate.
type Authority struct{ store Store }

func NewAuthority(store Store) (*Authority, error) {
	if nilPort(store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &Authority{store}, nil
}

// Read accepts a historical intent; Launch also gets its current lifecycle
// checked by the Trigger under Meeting/Turn locks. Historical Read requires
// Project/Agent gates; only Launch additionally requires its command lock.
func (a *Authority) RequireMeetingIntentInTx(ctx context.Context, tx f.Tx, actor i.Actor, p i.ProjectID, agent i.AgentID, intent i.AccessIntent) error {
	if a == nil || nilPort(a.store) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || actor.Validate() != nil || p.Validate() != nil || agent.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.MeetingRuntime || d.ProjectID != p.String() || (intent != i.Read && intent != i.Launch) {
		return fault(f.Forbidden)
	}
	if _, err := f.ParseID[c.Intent](d.CauseRef); err != nil {
		return fault(f.Forbidden)
	}
	x, err := a.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(p, f.Shared), agentLock(agent)}); err != nil {
		return portError(err)
	}
	var project, storedAgent, key string
	err = x.QueryRow(ctx, `SELECT s.project_id,l.agent_id,l.idempotency_key FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.sessions s ON s.id=l.meeting_id WHERE l.id=$1`, d.CauseRef).Scan(&project, &storedAgent, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return fault(f.Forbidden)
	}
	if err != nil {
		return portError(err)
	}
	if project != p.String() || storedAgent != agent.String() {
		return fault(f.Forbidden)
	}
	if intent == i.Read {
		return ctx.Err()
	}
	command, err := (ec.LaunchLookupKey{ProjectID: p, AgentID: agent, IdempotencyKey: f.IdempotencyKey(key)}).Command()
	if err != nil {
		return fault(f.InternalError)
	}
	return portError(a.store.RequireHeldLocks(ctx, tx, []f.LockRequest{commandLock(command)}))
}

type Trigger struct {
	authority *Authority
	captures  ec.TriggerCaptureAuthority
}

func NewTrigger(a *Authority, captures ec.TriggerCaptureAuthority) (*Trigger, error) {
	if a == nil || nilPort(a.store) || nilPort(captures) {
		return nil, fault(f.DependencyUnbound)
	}
	return &Trigger{a, captures}, nil
}

type triggerPlan struct {
	owner     *Trigger
	request   ec.LaunchRequest
	execution i.ExecutionID
	actor     i.Actor
	locks     []f.LockRequest
}

func (p *triggerPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func triggerLocks(r ec.LaunchRequest) ([]f.LockRequest, error) {
	meeting, err := f.ParseID[c.Meeting](r.Trigger.MeetingID)
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	command, err := r.Command()
	if err != nil {
		return nil, err
	}
	turn, _ := f.AggregateLock(f.TurnAggregate, r.Trigger.TurnID)
	contribution, _ := f.AggregateLock(f.ContributionAggregate, r.Trigger.ContributionID)
	return normalize([]f.LockRequest{projectLock(r.ProjectID, f.Shared), agentLock(r.AgentID), commandLock(command), meetingLock(meeting, f.Shared), {Key: turn, Mode: f.Shared}, {Key: contribution, Mode: f.Shared}})
}
func meetingRequest(r ec.LaunchRequest) error {
	if r.Validate() != nil || r.Trigger.Kind != "meeting" || r.Lineage.ContributionGeneration == nil || r.Lineage.ContributionAttempt == nil {
		return fault(f.InvalidArgument)
	}
	if len(r.Policy.AllowedResourceConstraints) != 0 {
		return fault(f.CapabilityUnsupported)
	}
	return nil
}
func (p *Trigger) DiscoverLaunch(ctx context.Context, actor i.Actor, r ec.LaunchRequest) (ec.LaunchPlan, error) {
	if p == nil || p.authority == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, fault(f.InvalidArgument)
	}
	if err := meetingRequest(r); err != nil {
		return nil, err
	}
	if actor.Validate() != nil || actor.Details().Kind != i.Service || actor.Details().ServiceName != i.MeetingRuntime || actor.Details().ProjectID != r.ProjectID.String() {
		return nil, fault(f.Forbidden)
	}
	locks, err := triggerLocks(r)
	if err != nil {
		return nil, err
	}
	return &triggerPlan{owner: p, request: r.Clone(), actor: actor, locks: locks}, ctx.Err()
}
func (p *Trigger) ValidateLaunchInTx(ctx context.Context, tx f.Tx, actor i.Actor, r ec.LaunchRequest, plan ec.LaunchPlan) (ec.LaunchPermit, error) {
	v, ok := plan.(*triggerPlan)
	if !ok || v == nil || v.owner != p || !v.actor.Equal(actor) || !sameLaunch(v.request, r) {
		return ec.LaunchPermit{}, fault(f.Forbidden)
	}
	row, err := p.current(ctx, tx, r, v.locks)
	if err != nil {
		return ec.LaunchPermit{}, err
	}
	if row.id.String() != actor.Details().CauseRef {
		return ec.LaunchPermit{}, fault(f.Forbidden)
	}
	return ec.LaunchPermit{ProviderType: "meeting", ProjectID: r.ProjectID, AgentID: r.AgentID, RequestDigest: row.requestDigest, ReferenceDigest: row.inputDigest}, nil
}
func (p *Trigger) DiscoverCapture(ctx context.Context, id i.ExecutionID, r ec.LaunchRequest) (ec.CapturePlan, error) {
	if p == nil || p.authority == nil || nilPort(p.captures) {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil || id.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	if err := meetingRequest(r); err != nil {
		return nil, err
	}
	locks, err := triggerLocks(r)
	if err != nil {
		return nil, err
	}
	return &triggerPlan{owner: p, request: r.Clone(), execution: id, locks: locks}, ctx.Err()
}
func (p *Trigger) CaptureInputInTx(ctx context.Context, tx f.Tx, id i.ExecutionID, r ec.LaunchRequest, plan ec.CapturePlan) (ec.CapturedTriggerInput, error) {
	v, ok := plan.(*triggerPlan)
	if !ok || v == nil || v.owner != p || v.execution != id || !sameLaunch(v.request, r) {
		return ec.CapturedTriggerInput{}, fault(f.Forbidden)
	}
	if _, err := p.captures.RequireTriggerCaptureInTx(ctx, tx, id, r); err != nil {
		return ec.CapturedTriggerInput{}, portError(err)
	}
	row, err := p.current(ctx, tx, r, v.locks)
	if err != nil {
		return ec.CapturedTriggerInput{}, err
	}
	if row.execution == nil || *row.execution != id {
		return ec.CapturedTriggerInput{}, fault(f.ConfirmationStale)
	}
	inputID, err := f.ParseID[ec.TriggerInput](row.inputID.String())
	if err != nil {
		return ec.CapturedTriggerInput{}, fault(f.InternalError)
	}
	return ec.NewCapturedTriggerInput(ec.TriggerInputRef{ProviderType: "meeting", SchemaVersion: 1, InputID: inputID, Digest: row.inputDigest}, row.input)
}
func sameLaunch(a, b ec.LaunchRequest) bool {
	ad, ae := a.Digest()
	bd, be := b.Digest()
	return ae == nil && be == nil && ad == bd && a.Meta.RequestID == b.Meta.RequestID && a.Meta.IdempotencyKey == b.Meta.IdempotencyKey
}
func (p *Trigger) current(ctx context.Context, tx f.Tx, r ec.LaunchRequest, locks []f.LockRequest) (intentRecord, error) {
	if err := p.authority.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return intentRecord{}, portError(err)
	}
	x, err := p.authority.store.InTx(tx)
	if err != nil {
		return intentRecord{}, portError(err)
	}
	row, err := loadIntentForRequest(ctx, x, r)
	if err != nil {
		return intentRecord{}, err
	}
	var project, meeting, turn, participant, source, kind, state, sessionStatus, contributionState string
	var generation, attempt int64
	var deleting, cancel bool
	err = x.QueryRow(ctx, `SELECT s.project_id,s.id,t.id,p.id,p.source_id,p.kind,t.status,s.status,c.status,c.generation,c.attempt,s.deleting,(t.cancel_requested OR c.cancel_requested) FROM agenteam_meeting.contributions c JOIN agenteam_meeting.sessions s ON s.id=c.meeting_id JOIN agenteam_meeting.turns t ON t.id=c.turn_id JOIN agenteam_meeting.participants p ON p.id=c.participant_id AND p.meeting_id=c.meeting_id WHERE c.id=$1`, r.Trigger.ContributionID).Scan(&project, &meeting, &turn, &participant, &source, &kind, &state, &sessionStatus, &contributionState, &generation, &attempt, &deleting, &cancel)
	if errors.Is(err, pgx.ErrNoRows) {
		return intentRecord{}, fault(f.NotFound)
	}
	if err != nil {
		return intentRecord{}, portError(err)
	}
	if project != r.ProjectID.String() || meeting != r.Trigger.MeetingID || turn != r.Trigger.TurnID || participant != r.Trigger.ParticipantID || source != r.AgentID.String() || kind != "agent" {
		return intentRecord{}, fault(f.Forbidden)
	}
	if deleting || cancel || c.TerminalContribution(contributionState) || sessionStatus == "proposed" || !contributionTurnActive(state, row.request) || generation != int64(*r.Lineage.ContributionGeneration) || attempt != int64(*r.Lineage.ContributionAttempt) {
		return intentRecord{}, fault(f.InvalidState)
	}
	return row, nil
}

type intentRecord struct {
	id              c.IntentID
	request         ec.LaunchRequest
	requestDigest   f.Digest
	inputID         c.InputID
	inputDigest     f.Digest
	input           []byte
	execution       *i.ExecutionID
	unknown         bool
	cancelRequested bool
}

func loadIntentForRequest(ctx context.Context, x postgres.SQLExecutor, r ec.LaunchRequest) (intentRecord, error) {
	var out intentRecord
	var id, inputID, requestID, key, requestDigest, inputDigest string
	var execution *string
	var raw []byte
	err := x.QueryRow(ctx, `SELECT l.id,l.input_id,l.request_id,l.idempotency_key,l.request_digest,l.request_data,l.execution_id,l.launch_unknown,m.input_data,m.input_digest FROM agenteam_meeting.launch_intents l JOIN agenteam_meeting.inputs m ON m.id=l.input_id WHERE l.contribution_id=$1 AND l.generation=$2 AND l.attempt=$3`, r.Trigger.ContributionID, int64(*r.Lineage.ContributionGeneration), int64(*r.Lineage.ContributionAttempt)).Scan(&id, &inputID, &requestID, &key, &requestDigest, &raw, &execution, &out.unknown, &out.input, &inputDigest)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, fault(f.NotFound)
	}
	if err != nil {
		return out, portError(err)
	}
	if out.id, err = f.ParseID[c.Intent](id); err != nil {
		return out, fault(f.InternalError)
	}
	if out.inputID, err = f.ParseID[c.Input](inputID); err != nil {
		return out, fault(f.InternalError)
	}
	if json.Unmarshal(raw, &out.request) != nil {
		return out, fault(f.InternalError)
	}
	out.request.Meta.RequestID, err = f.ParseID[f.Request](requestID)
	if err != nil {
		return out, fault(f.InternalError)
	}
	out.request.Meta.IdempotencyKey = f.IdempotencyKey(key)
	out.requestDigest = f.Digest(requestDigest)
	out.inputDigest = f.Digest(inputDigest)
	if !sameLaunch(out.request, r) || out.inputDigest != digest(out.input) {
		return intentRecord{}, fault(f.ConfirmationStale)
	}
	actual, err := r.Digest()
	if err != nil || actual != out.requestDigest {
		return intentRecord{}, fault(f.ConfirmationStale)
	}
	if execution != nil {
		id, err := f.ParseID[i.Execution](*execution)
		if err != nil {
			return out, fault(f.InternalError)
		}
		out.execution = &id
	}
	return out, nil
}

var _ ec.TriggerProvider = (*Trigger)(nil)
var _ ec.TriggerCaptureProvider = (*Trigger)(nil)
