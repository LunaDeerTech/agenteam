package work

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func transitionEventTriple(s event.Summary) bool {
	return s.Header.EventType == c.TaskTransitionedName && s.Header.AggregateType == c.TaskAggregate && s.Header.SchemaVersion == c.TaskTransitionSchemaVersion
}
func validateTaskRanks(rows []rankItem) error {
	if rows == nil || len(rows) > taskGroupCap {
		return internal(nil)
	}
	seen := make(map[string]bool, len(rows))
	for n, v := range rows {
		if _, err := f.ParseID[c.Task](v.ID); err != nil || c.ValidateRank(v.Rank) != nil || seen[v.ID] || n > 0 && rows[n-1].Rank >= v.Rank {
			return internal(nil)
		}
		seen[v.ID] = true
	}
	return nil
}
func transitionEventProject(s event.Summary) (c.ProjectID, error) {
	if !transitionEventTriple(s) || s.Producer != c.WorkProducer || s.Header.Validate() != nil || s.PayloadDigest.Validate() != nil || s.Header.Scope.Kind != event.ProjectScope || s.Header.AggregateVersion == nil || s.Header.AggregateSequence != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	p, err := f.ParseID[i.Project](s.Header.Scope.ProjectID.String())
	if err != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	return p, nil
}

// Re-derive the complete persisted mutation from the original intent and the
// preimage. Historical replay does not consult a newer mutable Task.
func validateTransitionRecord(r *transitionRecord, actor i.Actor) error {
	if r == nil || c.ValidateActor(actor) != nil || r.Input.User.String() != actor.Details().UserID {
		return internal(nil)
	}
	semantic, err := r.Input.semantic(actor, r.Key)
	if err != nil || semantic != r.Semantic {
		return internal(err)
	}
	p := r.Plan
	in := r.Input
	b := p.Before
	t := p.After.Task
	if b.Validate() != nil || p.After.Validate() != nil || b.ProjectID != in.Project || b.ID != in.Task || b.Version != in.Expected || transitionEdge(b, in.Request) != nil || len(p.Groups) != 2 || p.Placement.Milestone != b.MilestoneID || p.Placement.Sprint != b.SprintID || (p.Placement.State != c.Planned && p.Placement.State != c.Current) {
		return internal(nil)
	}
	if transitionRetainsReviewer(b, in.Request) {
		if p.Agent != nil {
			return internal(nil)
		}
	} else if p.Agent == nil || p.Agent.Validate() != nil || p.Agent.ProjectID != in.Project || p.Agent.AgentID != *transitionAgent(b, in.Request) {
		return internal(nil)
	}
	version, err := transitionNext(int64(b.Version))
	if err != nil {
		return internal(err)
	}
	expected := b.Clone()
	expected.State = in.Request.TargetState
	agent := *transitionAgent(b, in.Request)
	expected.AssigneeAgentID = &agent
	expected.Version = f.Version(version)
	expected.UpdatedAt = p.Header.OccurredAt
	if expected.UpdatedAt.Time().Before(b.UpdatedAt.Time()) || expected.UpdatedAt.Time().Before(r.Created.Time()) {
		return internal(nil)
	}
	source := groupForTask(b)
	target := source
	target.State = in.Request.TargetState
	if p.Groups[0].Group != source || p.Groups[1].Group != target {
		return internal(nil)
	}
	old := p.Groups[0]
	newGroup := p.Groups[1]
	if validateTaskRanks(old.Before) != nil || validateTaskRanks(old.After) != nil || validateTaskRanks(newGroup.Before) != nil || validateTaskRanks(newGroup.After) != nil {
		return internal(nil)
	}
	remaining := make([]rankItem, 0, len(old.Before))
	found := -1
	for n, v := range old.Before {
		if v.ID == in.Task.String() {
			found = n
			if v.Rank != b.ManualRank {
				return internal(nil)
			}
		} else {
			remaining = append(remaining, v)
		}
	}
	if found < 0 || !sameValue(remaining, old.After) {
		return internal(nil)
	}
	ranks, err := rankFor(newGroup.Before, in.Task.String(), "", true)
	if err != nil || !sameValue(ranks.Items, newGroup.After) {
		return internal(err)
	}
	expected.ManualRank = ranks.Rank
	if !sameValue(expected, t) {
		return internal(nil)
	}
	oldNext, err := transitionNext(old.Generation)
	if err != nil {
		return internal(err)
	}
	next, err := transitionNext(newGroup.Generation)
	if err != nil {
		return internal(err)
	}
	if _, err = transitionNext(p.QueryGeneration); err != nil {
		return internal(err)
	}
	var previous, following string
	if found > 0 {
		previous = old.Before[found-1].ID
	}
	if found+1 < len(old.Before) {
		following = old.Before[found+1].ID
	}
	sp, err := transitionPosition(source, oldNext, previous, following)
	if err != nil {
		return err
	}
	tp, err := transitionPosition(target, next, ranks.Previous, ranks.Next)
	if err != nil {
		return err
	}
	if !sameValue(sp, p.Source) || !sameValue(tp, p.Target) {
		return internal(nil)
	}
	h, err := taskHeader(p.After.EventIDs[0], t, t.UpdatedAt)
	if err != nil {
		return err
	}
	h.EventType = c.TaskTransitionedName
	h.SchemaVersion = c.TaskTransitionSchemaVersion
	if !sameValue(h, p.Header) {
		return internal(nil)
	}
	changed := b.AssigneeAgentID == nil || *b.AssigneeAgentID != agent
	if err = validateTransitionResolutions(r); err != nil {
		return err
	}
	count := 1 + len(p.Resolutions)
	if changed {
		count++
	}
	if in.Request.Comment != nil {
		count++
	}
	if len(p.History) != count || len(p.After.TaskEventIDs) != count {
		return internal(nil)
	}
	resolveStart := 1
	if changed {
		resolveStart++
	}
	for n, h := range p.History {
		if h.Validate() != nil || h.ID != p.After.TaskEventIDs[n] || h.ProjectID != in.Project || h.TaskID != in.Task || h.TaskVersion != t.Version || h.Actor.UserID != in.User || h.OperationID != r.ID || h.CorrelationID != r.ID || !h.CreatedAt.Time().Equal(t.UpdatedAt.Time()) {
			return internal(nil)
		}
		switch {
		case n == 0:
			if h.Type != c.TaskTransitionStateChanged || h.Payload.StateChanged == nil || h.Payload.StateChanged.FromState != b.State || h.Payload.StateChanged.ToState != t.State {
				return internal(nil)
			}
		case changed && n == 1:
			if h.Type != c.TaskTransitionAssigneeChanged || h.Payload.AssigneeChanged == nil || !sameValue(h.Payload.AssigneeChanged.FromAgentID, b.AssigneeAgentID) || h.Payload.AssigneeChanged.ToAgentID != agent {
				return internal(nil)
			}
		case n >= resolveStart && n < resolveStart+len(p.Resolutions):
			v := p.Resolutions[n-resolveStart]
			if h.Type != c.TaskTransitionBlockerResolved || h.Payload.BlockerResolved == nil || h.Payload.BlockerResolved.BlockerID != v.Before.ID || h.Payload.BlockerResolved.BlockerType != v.Before.Type || h.Payload.BlockerResolved.ResolutionComment != nil {
				return internal(nil)
			}
		default:
			if in.Request.Comment == nil || h.Type != c.TaskTransitionComment || h.Payload.Comment == nil || h.Payload.Comment.Body != *in.Request.Comment {
				return internal(nil)
			}
		}
	}
	payload, err := c.DecodeTaskTransitioned(p.Payload)
	if err != nil {
		return internal(err)
	}
	expectedPayload := c.TaskTransitioned{CommandID: r.ID, Actor: c.TaskTransitionActor{UserID: in.User}, TaskEventIDs: slices.Clone(p.After.TaskEventIDs), MilestoneID: b.MilestoneID, SprintID: b.SprintID, FromState: b.State, ToState: t.State, SourcePosition: sp, TargetPosition: tp}
	if changed {
		expectedPayload.AssigneeChange = &c.TaskAssigneeChangedPayload{FromAgentID: b.AssigneeAgentID, ToAgentID: agent}
	}
	if !sameValue(payload, expectedPayload) || r.Receipt != nil && !sameValue(*r.Receipt, p.After) {
		return internal(nil)
	}
	return nil
}

type transitionOpaque struct {
	Kind     string                    `json:"kind"`
	ID       c.TaskTransitionCommandID `json:"command_id"`
	Revision f.Version                 `json:"plan_revision"`
}

func transitionEventBinding(r *transitionRecord, actor i.Actor, s event.Summary) (f.Digest, []f.LockRequest, []byte, error) {
	if _, err := transitionEventProject(s); err != nil {
		return "", nil, nil, err
	}
	if err := validateTransitionRecord(r, actor); err != nil {
		return "", nil, nil, err
	}
	raw, err := cursorPayload(r.Plan.Payload)
	if err != nil {
		return "", nil, nil, err
	}
	if !sameValue(r.Plan.Header, s.Header) || digest(raw) != s.PayloadDigest {
		return "", nil, nil, fault(f.Forbidden)
	}
	locks, err := r.Input.locks(r.Key, r.Plan.Before)
	if err != nil {
		return "", nil, nil, err
	}
	id, err := c.TaskTransitionIdentity(r.Input.Project, r.Key)
	if err != nil {
		return "", nil, nil, err
	}
	type lock struct {
		Key  string
		Mode f.LockMode
	}
	projected := make([]lock, len(locks))
	for n, v := range locks {
		projected[n] = lock{v.Key.Canonical(), v.Mode}
	}
	raw, err = canonical(struct {
		Purpose  string
		Actor    i.ActorDetails
		Summary  event.Summary
		Command  string
		ID       c.TaskTransitionCommandID
		Revision f.Version
		Stages   [2]oc.Stage
		Locks    []lock
	}{"work.task-transition.append-v1", actor.Details(), s, id.Canonical(), r.ID, r.Revision, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}, projected})
	if err != nil {
		return "", nil, nil, err
	}
	opaque, err := canonical(transitionOpaque{"task_transition", r.ID, r.Revision})
	return digest(raw), locks, opaque, err
}
func (a *Authority) discoverTransitionAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	st := a.state()
	if st == nil {
		return oc.Dependencies{}, fault(f.DependencyUnbound)
	}
	project, err := transitionEventProject(summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	if err = readInput(ctx, actor, project); err != nil {
		return oc.Dependencies{}, err
	}
	cause, err := readCause("task-transition-append")
	if err != nil {
		return oc.Dependencies{}, err
	}
	var out oc.Dependencies
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locks, e := taskNormalize([]f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared)})
		if e != nil {
			return e
		}
		if e = st.store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		grant, e := st.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if e != nil {
			return portError(e)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		r, e := loadTransitionEvent(ctx, x, summary.Header.EventID)
		if e != nil {
			return e
		}
		binding, locks, opaque, e := transitionEventBinding(r, actor, summary)
		if e != nil {
			return e
		}
		out, e = oc.NewDependencies(st.issuer, binding, locks, opaque)
		return portError(e)
	})
	if err = taskTxError(ctx, result); err != nil {
		return oc.Dependencies{}, err
	}
	return out, nil
}
func (a *Authority) validateTransitionAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if !stage.Valid() || c.ValidateActor(actor) != nil || deps.Validate() != nil || !deps.Matches(st.issuer, deps.Binding()) {
		return fault(f.Forbidden)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	project, err := transitionEventProject(summary)
	if err != nil {
		return err
	}
	if err = st.store.RequireHeldLocks(ctx, tx, deps.Locks()); err != nil {
		return portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), taskScheduleLock(project, f.Exclusive), taskLock(summary.Header.AggregateID.String(), f.Exclusive)}); err != nil {
		return portError(err)
	}
	grant, err := st.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return fault(f.Forbidden)
	}
	r, err := loadTransitionEvent(ctx, x, summary.Header.EventID)
	if err != nil {
		return err
	}
	binding, locks, opaque, err := transitionEventBinding(r, actor, summary)
	if err != nil {
		return err
	}
	if !deps.Matches(st.issuer, binding) || !sameTaskLocks(deps.Locks(), locks) || !slices.Equal(opaque, deps.Opaque()) {
		return fault(f.Forbidden)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if stage == oc.CurrentAccess {
		return nil
	}
	if r.State != "planned" {
		return fault(f.Forbidden)
	}
	grant, err = st.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Mutate)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return fault(f.Forbidden)
	}
	if err = checkPointer(ctx, x, grant.Project()); err != nil {
		return err
	}
	sprint, err := loadSprint(ctx, x, project, r.Plan.Placement.Sprint, grant.Project().CurrentSprintID)
	if err != nil {
		return err
	}
	if sprint.State != r.Plan.Placement.State || sprint.MilestoneID != r.Plan.Placement.Milestone {
		return fault(f.Forbidden)
	}
	if _, err = loadMilestone(ctx, x, project, sprint.MilestoneID); err != nil {
		return err
	}
	return verifyTransitionPostimage(ctx, x, r)
}
func verifyTransitionPostimage(ctx context.Context, x postgres.SQLExecutor, r *transitionRecord) error {
	if err := verifyTransitionResolutions(ctx, x, r); err != nil {
		return err
	}
	p := r.Plan
	t, err := loadTask(ctx, x, r.Input.Project, r.Input.Task)
	if err != nil {
		return err
	}
	if !sameValue(t, p.After.Task) {
		return fault(f.Forbidden)
	}
	gen, err := loadTaskQueryGeneration(ctx, x, r.Input.Project)
	if err != nil {
		return err
	}
	next, err := transitionNext(p.QueryGeneration)
	if err != nil || gen != next {
		return fault(f.Forbidden)
	}
	for _, g := range p.Groups {
		rows, gen, err := loadTaskRanks(ctx, x, r.Input.Project, g.Group)
		if err != nil {
			return err
		}
		next, err := transitionNext(g.Generation)
		if err != nil || gen != next || !sameValue(rows, g.After) {
			return fault(f.Forbidden)
		}
	}
	for _, h := range p.History {
		var id, project, task, kind, operation, correlation string
		var version f.Version
		var actor, payload []byte
		var at time.Time
		if err = x.QueryRow(ctx, `SELECT id::text,project_id::text,task_id::text,task_version,type,actor,transition_operation_id::text,correlation_id::text,payload,created_at FROM agenteam_work.task_events WHERE id=$1 AND operation_id IS NULL AND blocker_operation_id IS NULL`, h.ID.String()).Scan(&id, &project, &task, &version, &kind, &actor, &operation, &correlation, &payload, &at); err != nil {
			return taskSQL(err)
		}
		raw, err := json.Marshal(h)
		if err != nil {
			return internal(err)
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return internal(nil)
		}
		var actualActor c.TaskTransitionActor
		if json.Unmarshal(actor, &actualActor) != nil {
			return internal(nil)
		}
		actual, err := cursorPayload(payload)
		if err != nil {
			return err
		}
		expected, err := cursorPayload(fields["payload"])
		if err != nil {
			return err
		}
		if id != h.ID.String() || project != h.ProjectID.String() || task != h.TaskID.String() || version != h.TaskVersion || kind != string(h.Type) || operation != r.ID.String() || correlation != r.ID.String() || !sameValue(actualActor, h.Actor) || !slices.Equal(actual, expected) || !at.Equal(h.CreatedAt.Time()) {
			return fault(f.Forbidden)
		}
	}
	var count int64
	if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_events WHERE project_id=$1 AND transition_operation_id=$2`, r.Input.Project.String(), r.ID.String()).Scan(&count); err != nil {
		return taskSQL(err)
	}
	if count != int64(len(p.History)) {
		return fault(f.Forbidden)
	}
	return nil
}
