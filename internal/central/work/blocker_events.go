package work

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

const blockerPurpose = "work.task-blocker.append-v1"

type blockerOpaque struct {
	Kind        string                 `json:"kind"`
	CommandID   c.TaskBlockerCommandID `json:"command_id"`
	Revision    f.Version              `json:"plan_revision"`
	TaskEventID c.TaskEventID          `json:"task_event_id"`
}

func (v *blockerOpaque) UnmarshalJSON(raw []byte) error {
	if _, e := taskPrivateObject(raw, 16384, []string{"kind", "command_id", "plan_revision", "task_event_id"}, nil); e != nil {
		return e
	}
	type wire blockerOpaque
	var n wire
	if e := json.Unmarshal(raw, &n); e != nil {
		return internal(e)
	}
	if n.Kind != "task_blocker" || n.CommandID.Validate() != nil || n.Revision.Validate() != nil || n.TaskEventID.Validate() != nil {
		return internal(nil)
	}
	*v = blockerOpaque(n)
	return nil
}
func blockerEventTriple(s event.Summary) bool {
	return s.Header.EventType == c.TaskBlockersChangedName && s.Header.AggregateType == c.TaskAggregate && s.Header.SchemaVersion == c.TaskBlockerSchemaVersion
}
func blockerEventProject(s event.Summary) (c.ProjectID, error) {
	h := s.Header
	if !blockerEventTriple(s) || s.Producer != c.WorkProducer || h.Validate() != nil || s.PayloadDigest.Validate() != nil || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || *h.AggregateVersion < 2 || h.AggregateSequence != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	p, e := f.ParseID[i.Project](h.Scope.ProjectID.String())
	if e != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	return p, nil
}

// This validates the immutable proposal without consulting mutable canonical
// rows. Completed replay deliberately survives today's Task/Blocker state.
func validateBlockerRecord(r *blockerRecord, a i.Actor) error {
	if r == nil || r.Plan == nil || r.ID.Validate() != nil || r.User.String() != a.Details().UserID || r.TaskEventID.Validate() != nil || r.EventID.Validate() != nil || r.Revision.Validate() != nil || r.Created.Validate() != nil {
		return internal(nil)
	}
	semantic, e := r.Input.semantic(a, r.Key)
	if e != nil || semantic != r.Semantic || r.Input.Project != r.Project || r.Input.User != r.User || r.Input.Command != r.Command {
		return internal(e)
	}
	p := r.Plan
	in := r.Input
	out := p.After
	before := p.Before
	if out.Validate() != nil || before.Validate() != nil || before.ID != in.Target || before.ProjectID != in.Project || before.Version != in.Expected || before.State != c.TaskStateBacklog || before.AssigneeAgentID != nil || out.TaskEventID != r.TaskEventID || out.EventIDs[0] != r.EventID || p.Placement.Milestone != before.MilestoneID || p.Placement.Sprint != before.SprintID || (p.Placement.State != c.Planned && p.Placement.State != c.Current) || p.QueryGeneration < 1 {
		return internal(nil)
	}
	if _, e = taskCounter(p.QueryGeneration); e != nil {
		return internal(e)
	}
	at := out.Task.UpdatedAt
	if at.Time().Before(before.UpdatedAt.Time()) || at.Time().Before(r.Created.Time()) {
		return internal(nil)
	}
	expected := before.Clone()
	n, e := taskCounter(int64(before.Version))
	if e != nil {
		return internal(e)
	}
	expected.Version = f.Version(n)
	expected.UpdatedAt = at
	if !sameValue(expected, out.Task) {
		return internal(nil)
	}
	actor := c.TaskEventActor{Type: i.Human, UserID: in.User, Source: "task_domain"}
	var b c.TaskBlocker
	change := c.TaskBlockerAddedChange
	history := c.TaskBlockerEvent{ID: r.TaskEventID, ProjectID: r.Project, TaskID: in.Target, TaskVersion: out.Task.Version, Actor: actor, OperationID: r.ID, CorrelationID: r.ID, CreatedAt: at}
	if in.Add != nil {
		if p.BlockerBefore != nil {
			return internal(nil)
		}
		v := in.Add
		if v.Type == c.TaskBlockerRelyOn && v.Metadata.RelyOn.RelatedTaskID == in.Target {
			return internal(nil)
		}
		b = c.TaskBlocker{ID: v.BlockerID, ProjectID: r.Project, TaskID: in.Target, Type: v.Type, Description: v.Description, Metadata: v.Metadata.Clone(), CreatedAt: at, CreatedBy: actor}
		history.Type = c.TaskBlockerEventAdded
		history.Payload.Added = &c.TaskBlockerAddedPayload{BlockerID: b.ID, BlockerType: b.Type}
	} else {
		v := p.BlockerBefore
		if v == nil || v.Validate() != nil || v.ID != in.Resolve.BlockerID || v.ProjectID != r.Project || v.TaskID != in.Target || v.ResolvedAt != nil || at.Time().Before(v.CreatedAt.Time()) {
			return internal(nil)
		}
		b = v.Clone()
		b.ResolvedAt = &at
		b.ResolvedBy = &actor
		b.ResolutionComment = in.Resolve.Clone().ResolutionComment
		change = c.TaskBlockerResolvedChange
		history.Type = c.TaskBlockerEventResolved
		history.Payload.Resolved = &c.TaskBlockerResolvedPayload{BlockerID: b.ID, BlockerType: b.Type, ResolutionComment: b.ResolutionComment}
	}
	if !sameValue(b, out.Blocker) || !sameValue(history, p.TaskEvent) {
		return internal(nil)
	}
	payload := c.TaskBlockersChanged{OperationID: r.ID, ActorUserID: r.User, TaskEventID: r.TaskEventID, BlockerID: b.ID, Change: change}
	if !sameValue(payload, p.Payload) {
		return internal(nil)
	}
	h, e := blockerHeader(r.EventID, out.Task, at)
	if e != nil || !sameValue(h, p.Header) {
		return internal(e)
	}
	if r.State == "completed" {
		if r.Receipt == nil || r.Committed == nil || r.Committed.Time().Before(r.Created.Time()) || !sameValue(*r.Receipt, out) {
			return internal(nil)
		}
	} else if r.State != "planned" || r.Receipt != nil || r.Committed != nil {
		return internal(nil)
	}
	return nil
}
func blockerEventBinding(r *blockerRecord, a i.Actor, summary event.Summary) (f.Digest, []f.LockRequest, []byte, error) {
	if c.ValidateActor(a) != nil || r == nil || r.Plan == nil || r.User.String() != a.Details().UserID {
		return "", nil, nil, fault(f.Forbidden)
	}
	if _, e := blockerEventProject(summary); e != nil {
		return "", nil, nil, e
	}
	if e := validateBlockerRecord(r, a); e != nil {
		return "", nil, nil, e
	}
	raw, e := canonical(r.Plan.Payload)
	if e != nil {
		return "", nil, nil, e
	}
	if !sameValue(r.Plan.Header, summary.Header) || digest(raw) != summary.PayloadDigest || r.EventID != summary.Header.EventID {
		return "", nil, nil, fault(f.Forbidden)
	}
	locks, e := r.Input.locks(r.Key, r.Plan.Before.SprintID)
	if e != nil {
		return "", nil, nil, e
	}
	id, e := c.TaskBlockerCommandIdentity(r.Project, r.Command, r.Key)
	if e != nil {
		return "", nil, nil, e
	}
	type lock struct {
		Key  string     `json:"key"`
		Mode f.LockMode `json:"mode"`
	}
	projected := make([]lock, len(locks))
	for n, v := range locks {
		projected[n] = lock{v.Key.Canonical(), v.Mode}
	}
	raw, e = canonical(struct {
		Purpose     string                 `json:"purpose"`
		Actor       i.ActorDetails         `json:"actor"`
		Summary     event.Summary          `json:"summary"`
		Identity    string                 `json:"identity"`
		CommandID   c.TaskBlockerCommandID `json:"command_id"`
		Revision    f.Version              `json:"plan_revision"`
		TaskEventID c.TaskEventID          `json:"task_event_id"`
		Stages      [2]oc.Stage            `json:"stages"`
		Locks       []lock                 `json:"locks"`
	}{blockerPurpose, a.Details(), summary, id.Canonical(), r.ID, r.Revision, r.TaskEventID, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}, projected})
	if e != nil {
		return "", nil, nil, e
	}
	opaque, e := canonical(blockerOpaque{"task_blocker", r.ID, r.Revision, r.TaskEventID})
	if e != nil {
		return "", nil, nil, e
	}
	return digest(raw), locks, opaque, nil
}
func (a *Authority) discoverBlockerAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	st := a.state()
	if st == nil {
		return oc.Dependencies{}, fault(f.DependencyUnbound)
	}
	p, e := blockerEventProject(summary)
	if e != nil {
		return oc.Dependencies{}, e
	}
	if e = readInput(ctx, actor, p); e != nil {
		return oc.Dependencies{}, e
	}
	cause, e := readCause("task-blocker.append")
	if e != nil {
		return oc.Dependencies{}, e
	}
	var out oc.Dependencies
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locks, e := taskNormalize([]f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(p, f.Shared)})
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
		access, e := st.projects.RequireOwnerInTx(ctx, tx, actor, p, i.Read)
		if e != nil {
			return portError(e)
		}
		if access.Project().ID != p {
			return internal(nil)
		}
		r, e := loadBlockerEventCommand(ctx, x, summary.Header.EventID, actor.Details().UserID)
		if e != nil {
			return e
		}
		binding, locks, opaque, e := blockerEventBinding(r, actor, summary)
		if e != nil {
			return e
		}
		out, e = oc.NewDependencies(st.issuer, binding, locks, opaque)
		return portError(e)
	})
	if e = taskTxError(ctx, result); e != nil {
		return oc.Dependencies{}, e
	}
	return out, nil
}
func (a *Authority) validateBlockerAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	x, e := st.store.InTx(tx)
	if e != nil {
		return portError(e)
	}
	if !stage.Valid() || c.ValidateActor(actor) != nil || deps.Validate() != nil || !deps.Matches(st.issuer, deps.Binding()) {
		return fault(f.Forbidden)
	}
	p, e := blockerEventProject(summary)
	if e != nil {
		return e
	}
	var opaque blockerOpaque
	if e = opaque.UnmarshalJSON(deps.Opaque()); e != nil {
		return fault(f.Forbidden)
	}
	if e = st.store.RequireHeldLocks(ctx, tx, deps.Locks()); e != nil {
		return portError(e)
	}
	task, e := f.ParseID[c.Task](summary.Header.AggregateID.String())
	if e != nil {
		return fault(f.Forbidden)
	}
	// CurrentAccess may authorize an old completed receipt under archived Read;
	// the transaction must still hold the planned complete domain union.
	if e = st.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(p, f.Shared), taskScheduleLock(p, f.Exclusive), taskLock(task.String(), f.Exclusive)}); e != nil {
		return portError(e)
	}
	access, e := st.projects.RequireOwnerInTx(ctx, tx, actor, p, i.Read)
	if e != nil {
		return portError(e)
	}
	if access.Project().ID != p {
		return internal(nil)
	}
	r, e := loadBlockerEventCommand(ctx, x, summary.Header.EventID, actor.Details().UserID)
	if e != nil {
		return e
	}
	binding, locks, expectedOpaque, e := blockerEventBinding(r, actor, summary)
	if e != nil {
		return e
	}
	if !deps.Matches(st.issuer, binding) || !sameTaskLocks(deps.Locks(), locks) || !slices.Equal(deps.Opaque(), expectedOpaque) || r.ID != opaque.CommandID || r.Revision != opaque.Revision || r.TaskEventID != opaque.TaskEventID || r.Project != p {
		return fault(f.Forbidden)
	}
	if e = st.store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return portError(e)
	}
	if stage == oc.CurrentAccess {
		return nil
	}
	if r.State != "planned" {
		return fault(f.Forbidden)
	}
	scope, e := a.blockerScope(ctx, tx, actor, p, task, true)
	if e != nil {
		return e
	}
	access, e = st.projects.RequireOwnerInTx(ctx, tx, actor, p, i.Mutate)
	if e != nil {
		return portError(e)
	}
	if e = checkPointer(ctx, x, access.Project()); e != nil {
		return e
	}
	sprint, e := loadSprint(ctx, x, p, r.Plan.Placement.Sprint, access.Project().CurrentSprintID)
	if e != nil {
		return e
	}
	if sprint.State != r.Plan.Placement.State || sprint.MilestoneID != r.Plan.Placement.Milestone {
		return fault(f.Forbidden)
	}
	if _, e = loadMilestone(ctx, x, p, sprint.MilestoneID); e != nil {
		return e
	}
	return verifyBlockerPostimage(ctx, scope, r)
}
func verifyBlockerPostimage(ctx context.Context, s *blockerScope, r *blockerRecord) error {
	p := r.Plan
	current, e := loadTask(ctx, s.x, r.Project, r.Input.Target)
	if e != nil {
		return e
	}
	if !sameValue(current, p.After.Task) {
		return fault(f.Forbidden)
	}
	row, e := loadBlocker(ctx, s, p.After.Blocker.ID)
	if e != nil {
		return e
	}
	if row == nil || !sameValue(row.Value, p.After.Blocker) {
		return fault(f.Forbidden)
	}
	if r.Input.Add != nil {
		if row.CreatedOperation != r.ID || row.ResolvedOperation != nil {
			return fault(f.Forbidden)
		}
	} else if row.ResolvedOperation == nil || *row.ResolvedOperation != r.ID {
		return fault(f.Forbidden)
	}
	gen, e := loadTaskQueryGeneration(ctx, s.x, r.Project)
	if e != nil {
		return e
	}
	next, e := taskCounter(p.QueryGeneration)
	if e != nil || gen != next {
		return fault(f.Forbidden)
	}
	return verifyBlockerHistory(ctx, s.x, r)
}
func verifyBlockerHistory(ctx context.Context, x postgres.SQLExecutor, r *blockerRecord) error {
	var id, project, task, operation, correlation string
	var legacy *string
	var version f.Version
	var kind c.TaskBlockerEventType
	var actor, payload []byte
	var at time.Time
	e := x.QueryRow(ctx, `SELECT id::text,project_id::text,task_id::text,task_version,type,actor,operation_id::text,blocker_operation_id::text,correlation_id::text,payload,created_at FROM agenteam_work.task_events WHERE id=$1`, r.TaskEventID.String()).Scan(&id, &project, &task, &version, &kind, &actor, &legacy, &operation, &correlation, &payload, &at)
	if errors.Is(e, pgx.ErrNoRows) {
		return fault(f.Forbidden)
	}
	if e != nil {
		return taskSQL(e)
	}
	if legacy != nil {
		return fault(f.Forbidden)
	}
	h := r.Plan.TaskEvent
	var actual c.TaskEventActor
	if e = actual.UnmarshalJSON(actor); e != nil {
		return internal(e)
	}
	var decoded c.TaskBlockerEventPayload
	switch kind {
	case c.TaskBlockerEventAdded:
		decoded.Added = new(c.TaskBlockerAddedPayload)
		e = decoded.Added.UnmarshalJSON(payload)
	case c.TaskBlockerEventResolved:
		decoded.Resolved = new(c.TaskBlockerResolvedPayload)
		e = decoded.Resolved.UnmarshalJSON(payload)
	default:
		return fault(f.Forbidden)
	}
	if e != nil {
		return internal(e)
	}
	if id != h.ID.String() || project != h.ProjectID.String() || task != h.TaskID.String() || version != h.TaskVersion || kind != h.Type || !sameValue(actual, h.Actor) || operation != h.OperationID.String() || correlation != h.CorrelationID.String() || !sameValue(decoded, h.Payload) || !at.Equal(h.CreatedAt.Time()) {
		return fault(f.Forbidden)
	}
	return nil
}
