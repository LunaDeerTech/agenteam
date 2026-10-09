package work

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

const workPurpose = "work.structure.append-v1"

type eventOpaque struct {
	CommandID c.CommandID `json:"command_id"`
	Revision  f.Version   `json:"plan_revision"`
}

func eventRecord(record *commandRecord, actor i.Actor, summary event.Summary) error {
	if actor.Validate() != nil || actor.Details().Kind != i.Human || summary.Producer != c.WorkProducer || summary.Header.Validate() != nil || summary.PayloadDigest.Validate() != nil || record == nil || record.Plan == nil || record.EventID == nil {
		return fault(f.Forbidden)
	}
	if record.User.String() != actor.Details().UserID {
		return fault(f.Forbidden)
	}
	if err := validateRecord(record, actor); err != nil {
		return err
	}
	raw, err := cursorPayload(record.Plan.Payload)
	if err != nil {
		return err
	}
	if !sameValue(record.Plan.Header, summary.Header) || digest(raw) != summary.PayloadDigest || *record.EventID != summary.Header.EventID {
		return fault(f.Forbidden)
	}
	return nil
}
func cursorPayload(raw []byte) ([]byte, error) {
	v, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return nil, internal(err)
	}
	return v, nil
}
func eventBinding(record *commandRecord, actor i.Actor, summary event.Summary) (f.Digest, []f.LockRequest, []byte, error) {
	if err := eventRecord(record, actor, summary); err != nil {
		return "", nil, nil, err
	}
	locks, err := record.Input.locks(record.Key)
	if err != nil {
		return "", nil, nil, err
	}
	identity, err := record.Input.identity(record.Key)
	if err != nil {
		return "", nil, nil, err
	}
	type lock struct {
		Key  string     `json:"key"`
		Mode f.LockMode `json:"mode"`
	}
	projection := make([]lock, len(locks))
	for n, v := range locks {
		projection[n] = lock{Key: v.Key.Canonical(), Mode: v.Mode}
	}
	raw, err := canonical(struct {
		Purpose   string         `json:"purpose"`
		Actor     i.ActorDetails `json:"actor"`
		Summary   event.Summary  `json:"summary"`
		Identity  string         `json:"identity"`
		CommandID c.CommandID    `json:"command_id"`
		Revision  f.Version      `json:"plan_revision"`
		Stages    [2]oc.Stage    `json:"stages"`
		Locks     []lock         `json:"locks"`
	}{workPurpose, actor.Details(), summary, identity.Canonical(), record.ID, record.Revision, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}, projection})
	if err != nil {
		return "", nil, nil, err
	}
	opaque, err := canonical(eventOpaque{record.ID, record.Revision})
	if err != nil {
		return "", nil, nil, err
	}
	return digest(raw), locks, opaque, nil
}
func (a *Authority) DiscoverAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	if taskEventTriple(summary) {
		return a.discoverTaskAppend(ctx, actor, summary)
	}
	st := a.state()
	if st == nil {
		return oc.Dependencies{}, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return oc.Dependencies{}, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return oc.Dependencies{}, canceled(err)
	}
	if c.ValidateActor(actor) != nil || summary.Producer != c.WorkProducer || summary.Header.Validate() != nil {
		return oc.Dependencies{}, fault(f.Forbidden)
	}
	record, err := loadEventCommand(ctx, st.store, summary.Header.EventID)
	if err != nil {
		return oc.Dependencies{}, err
	}
	binding, locks, opaque, err := eventBinding(record, actor, summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(st.issuer, binding, locks, opaque)
}
func sameLocks(a, b []f.LockRequest) bool {
	return slices.EqualFunc(a, b, func(x, y f.LockRequest) bool { return x.Key.Canonical() == y.Key.Canonical() && x.Mode == y.Mode })
}

func eventProject(summary event.Summary) (c.ProjectID, error) {
	h := summary.Header
	if summary.Producer != c.WorkProducer || h.Validate() != nil || summary.PayloadDigest.Validate() != nil || h.SchemaVersion != c.WorkSchemaVersion || h.Scope.Kind != event.ProjectScope || h.AggregateVersion == nil || h.AggregateSequence != nil || !(h.EventType == c.MilestoneChangedName && h.AggregateType == c.MilestoneAggregate || h.EventType == c.SprintChangedName && h.AggregateType == c.SprintAggregate) {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	project, err := f.ParseID[i.Project](h.Scope.ProjectID.String())
	if err != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	return project, nil
}

func (a *Authority) ValidateAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	if taskEventTriple(summary) {
		return a.validateTaskAppendInTx(ctx, tx, actor, summary, deps, stage)
	}
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if !stage.Valid() || c.ValidateActor(actor) != nil || summary.Producer != c.WorkProducer || summary.Header.Validate() != nil || deps.Validate() != nil {
		return fault(f.Forbidden)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	// Dependencies are immutable, but a different producer instance can mint
	// its own valid value. Reject that issuer before consulting any Work row.
	if !deps.Matches(st.issuer, deps.Binding()) {
		return fault(f.Forbidden)
	}
	project, err := eventProject(summary)
	if err != nil {
		return err
	}
	if err = st.store.RequireHeldLocks(ctx, tx, deps.Locks()); err != nil {
		return portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared)}); err != nil {
		return portError(err)
	}
	access, err := st.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return portError(err)
	}
	if access.Project().ID != project {
		return internal(nil)
	}
	// Opaque has no key or body; the private issuer/digest closes every field.
	opaque, err := decodePrivate[eventOpaque](deps.Opaque(), 16384, []string{"command_id", "plan_revision"})
	if err != nil || opaque.CommandID.Validate() != nil || opaque.Revision.Validate() != nil {
		return fault(f.Forbidden)
	}
	record, err := loadEventCommand(ctx, x, summary.Header.EventID)
	if err != nil {
		return err
	}
	binding, locks, expectedOpaque, err := eventBinding(record, actor, summary)
	if err != nil {
		return err
	}
	if !deps.Matches(st.issuer, binding) || !sameLocks(deps.Locks(), locks) || !slices.Equal(deps.Opaque(), expectedOpaque) || opaque.CommandID != record.ID || opaque.Revision != record.Revision {
		return fault(f.Forbidden)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if record.Project != project {
		return internal(nil)
	}
	if stage == oc.CurrentAccess {
		return nil
	}
	if record.State != "planned" {
		return fault(f.Forbidden)
	}
	access, err = st.projects.RequireOwnerInTx(ctx, tx, actor, record.Project, i.Mutate)
	if err != nil {
		return portError(err)
	}
	if access.Project().ID != record.Project {
		return internal(nil)
	}
	return verifyPostimage(ctx, x, access.Project(), record)
}
func verifyPostimage(ctx context.Context, x postgres.SQLExecutor, ref pc.ProjectRef, record *commandRecord) error {
	if record.Plan == nil || !record.Plan.After.Changed || record.State != "planned" {
		return fault(f.Forbidden)
	}
	if err := checkPointer(ctx, x, ref); err != nil {
		return err
	}
	plan := record.Plan
	if plan.After.Milestone != nil {
		v, err := loadMilestone(ctx, x, record.Project, plan.After.Milestone.ID)
		if err != nil {
			return err
		}
		if !sameValue(v, *plan.After.Milestone) {
			return fault(f.Forbidden)
		}
		if record.Command == c.MilestoneCreate {
			generation, err := loadGeneration(ctx, x, record.Project, v.ID.String())
			if err != nil {
				return err
			}
			if generation != 1 {
				return fault(f.Forbidden)
			}
		}
	} else {
		v, err := loadSprint(ctx, x, record.Project, plan.After.Sprint.ID, ref.CurrentSprintID)
		if err != nil {
			return err
		}
		if !sameValue(v, *plan.After.Sprint) {
			return fault(f.Forbidden)
		}
		if _, err = loadMilestone(ctx, x, record.Project, v.MilestoneID); err != nil {
			return err
		}
	}
	if plan.GroupBefore == nil {
		return nil
	}
	generation, err := loadGeneration(ctx, x, record.Project, record.Input.parent())
	if err != nil {
		return err
	}
	next, err := nextVersion(*plan.GroupBefore)
	if err != nil || generation != next {
		return fault(f.Forbidden)
	}
	rows, err := loadRanks(ctx, x, record.Project, record.Input.parent())
	if err != nil {
		return err
	}
	target := -1
	for n, row := range rows {
		if c.ValidateRank(row.Rank) != nil || n > 0 && rows[n-1].Rank >= row.Rank {
			return internal(nil)
		}
		if row.ID == record.Input.Target {
			target = n
		}
	}
	if target < 0 {
		return fault(f.Forbidden)
	}
	previous, following := "", ""
	if target > 0 {
		previous = rows[target-1].ID
	}
	if target+1 < len(rows) {
		following = rows[target+1].ID
	}
	if record.Command.IsSprint() {
		var payload c.SprintChanged
		if json.Unmarshal(plan.Payload, &payload) != nil || payload.Position == nil {
			return internal(nil)
		}
		expectedPrevious, expectedNext := "", ""
		if payload.Position.PreviousID != nil {
			expectedPrevious = payload.Position.PreviousID.String()
		}
		if payload.Position.NextID != nil {
			expectedNext = payload.Position.NextID.String()
		}
		if previous != expectedPrevious || following != expectedNext || payload.Position.OrderGeneration != generation {
			return fault(f.Forbidden)
		}
	} else {
		var payload c.MilestoneChanged
		if json.Unmarshal(plan.Payload, &payload) != nil || payload.Position == nil {
			return internal(nil)
		}
		expectedPrevious, expectedNext := "", ""
		if payload.Position.PreviousID != nil {
			expectedPrevious = payload.Position.PreviousID.String()
		}
		if payload.Position.NextID != nil {
			expectedNext = payload.Position.NextID.String()
		}
		if previous != expectedPrevious || following != expectedNext || payload.Position.OrderGeneration != generation {
			return fault(f.Forbidden)
		}
	}
	return nil
}
