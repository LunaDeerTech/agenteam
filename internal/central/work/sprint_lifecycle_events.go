package work

import (
	"context"
	"encoding/json"
	"slices"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func sprintStartEventTriple(s event.Summary) bool {
	return s.Header.EventType == c.SprintStartedName && s.Header.AggregateType == c.SprintAggregate && s.Header.SchemaVersion == c.SprintLifecycleSchemaVersion
}
func sprintStartEventProject(s event.Summary) (c.ProjectID, error) {
	if !sprintStartEventTriple(s) || s.Producer != c.WorkProducer || s.Header.Validate() != nil || s.PayloadDigest.Validate() != nil || s.Header.Scope.Kind != event.ProjectScope || s.Header.AggregateVersion == nil || s.Header.AggregateSequence != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	p, err := f.ParseID[i.Project](s.Header.Scope.ProjectID.String())
	if err != nil {
		return c.ProjectID{}, fault(f.Forbidden)
	}
	return p, nil
}
func sprintStartEventBinding(r *sprintStartRecord, actor i.Actor, summary event.Summary) (f.Digest, []f.LockRequest, []byte, error) {
	p, err := sprintStartEventProject(summary)
	if err != nil {
		return "", nil, nil, err
	}
	if err = validateSprintStartRecord(r, actor); err != nil {
		return "", nil, nil, err
	}
	payload, err := cursorPayload(r.Plan.Payload)
	if err != nil {
		return "", nil, nil, err
	}
	if r.Input.Project != p || !sameValue(r.Plan.Header, summary.Header) || digest(payload) != summary.PayloadDigest {
		return "", nil, nil, fault(f.Forbidden)
	}
	locks, err := r.Input.locks(r.Key)
	if err != nil {
		return "", nil, nil, err
	}
	id, err := c.SprintStartIdentity(p, r.Key)
	if err != nil {
		return "", nil, nil, err
	}
	type lock struct {
		Key  string
		Mode f.LockMode
	}
	projection := make([]lock, len(locks))
	for n, l := range locks {
		projection[n] = lock{l.Key.Canonical(), l.Mode}
	}
	raw, err := canonical(struct {
		Purpose   string
		Actor     i.ActorDetails
		Identity  string
		CommandID c.SprintStartCommandID
		Revision  f.Version
		Summary   event.Summary
		Plan      sprintStartPlan
		Locks     []lock
	}{"work.sprint-start.append-v1", actor.Details(), id.Canonical(), r.ID, r.Revision, summary, r.Plan, projection})
	if err != nil {
		return "", nil, nil, err
	}
	opaque, err := canonical(struct {
		ID       c.SprintStartCommandID `json:"id"`
		Revision f.Version              `json:"revision"`
	}{r.ID, r.Revision})
	if err != nil {
		return "", nil, nil, err
	}
	return digest(raw), locks, opaque, nil
}
func (a *Authority) discoverSprintStartAppend(ctx context.Context, actor i.Actor, summary event.Summary) (oc.Dependencies, error) {
	if a.state() == nil {
		return oc.Dependencies{}, fault(f.DependencyUnbound)
	}
	p, err := sprintStartEventProject(summary)
	if err != nil {
		return oc.Dependencies{}, err
	}
	if err = readInput(ctx, actor, p); err != nil {
		return oc.Dependencies{}, err
	}
	cause, err := readCause("sprint-start-append")
	if err != nil {
		return oc.Dependencies{}, err
	}
	st := a.state()
	var out oc.Dependencies
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(p, f.Shared)}); err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		access, err := st.projects.RequireOwnerInTx(ctx, tx, actor, p, i.Read)
		if err != nil {
			return portError(err)
		}
		if !access.Matches(actor, p) {
			return fault(f.Forbidden)
		}
		r, err := loadSprintStartEvent(ctx, x, summary.Header.EventID)
		if err != nil {
			return err
		}
		binding, locks, opaque, err := sprintStartEventBinding(r, actor, summary)
		if err != nil {
			return err
		}
		out, err = oc.NewDependencies(st.issuer, binding, locks, opaque)
		return portError(err)
	})
	if err = txError(result); err != nil {
		return oc.Dependencies{}, err
	}
	return out, nil
}
func (a *Authority) validateSprintStartAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary event.Summary, deps oc.Dependencies, stage oc.Stage) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !stage.Valid() || c.ValidateActor(actor) != nil || deps.Validate() != nil || !deps.Matches(st.issuer, deps.Binding()) {
		return fault(f.Forbidden)
	}
	p, err := sprintStartEventProject(summary)
	if err != nil {
		return err
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, deps.Locks()); err != nil {
		return portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(p, f.Exclusive), taskScheduleLock(p, f.Exclusive), sprintLock(summary.Header.AggregateID.String(), f.Exclusive)}); err != nil {
		return portError(err)
	}
	access, err := st.projects.RequireOwnerInTx(ctx, tx, actor, p, i.Read)
	if err != nil {
		return portError(err)
	}
	if !access.Matches(actor, p) {
		return fault(f.Forbidden)
	}
	r, err := loadSprintStartEvent(ctx, x, summary.Header.EventID)
	if err != nil {
		return err
	}
	binding, locks, opaque, err := sprintStartEventBinding(r, actor, summary)
	if err != nil {
		return err
	}
	if !deps.Matches(st.issuer, binding) || !sameLocks(deps.Locks(), locks) || !slices.Equal(deps.Opaque(), opaque) {
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
	access, err = st.projects.RequireOwnerInTx(ctx, tx, actor, p, i.Mutate)
	if err != nil {
		return portError(err)
	}
	if !sameValue(access.Project(), r.Plan.After.Project) {
		return fault(f.Forbidden)
	}
	actual, err := loadSprint(ctx, x, p, r.Input.Sprint, access.Project().CurrentSprintID)
	if err != nil {
		return err
	}
	if !sameValue(actual, r.Plan.After.Sprint) {
		return fault(f.Forbidden)
	}
	return nil
}
func jsonSprintStartActor(s c.Sprint) ([]byte, error) {
	if s.Validate() != nil || s.StartedBy == nil {
		return nil, internal(nil)
	}
	raw, err := json.Marshal(s.StartedBy)
	return raw, err
}

// The witness is issued only after the original Sprint UPDATE returns. It is
// tied to the same Authority, physical Tx, complete Actor and planned revision.
type sprintStartWriteKey struct{}
type sprintStartWrite struct {
	authority *Authority
	tx        f.Tx
	actor     i.ActorDetails
	id        c.SprintStartCommandID
	revision  f.Version
}

func (a *Authority) CheckSprintStartAppliedInTx(ctx context.Context, tx f.Tx, actor i.Actor, change pc.SprintStartChange) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || change.Validate() != nil || c.ValidateActor(actor) != nil {
		return fault(f.Forbidden)
	}
	witness, ok := ctx.Value(sprintStartWriteKey{}).(sprintStartWrite)
	if !ok || witness.authority != a || witness.tx != tx || !sameValue(witness.actor, actor.Details()) || witness.id.String() != change.CommandID || witness.revision != change.PlanRevision {
		return fault(f.Forbidden)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	user, err := f.ParseID[i.User](actor.Details().UserID)
	if err != nil {
		return fault(f.Forbidden)
	}
	locks, err := change.RequiredLocks(user)
	if err != nil {
		return portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	r, err := loadSprintStart(ctx, x, change.Before.ID, change.Command.Key())
	if err != nil {
		return err
	}
	if err = validateSprintStartRecord(r, actor); err != nil {
		return err
	}
	expected, err := sprintStartChange(r)
	if err != nil {
		return internal(err)
	}
	// CommandIdentity intentionally has a safe JSON form. Compare its canonical
	// value explicitly rather than relying on a serialization of that token.
	if r.State != "planned" || change.Command.Canonical() != expected.Command.Canonical() || !sameValue(change, expected) {
		return fault(f.Forbidden)
	}
	actual, err := loadSprint(ctx, x, r.Input.Project, r.Input.Sprint, &r.Input.Sprint)
	if err != nil {
		return err
	}
	if !sameValue(actual, r.Plan.After.Sprint) {
		return fault(f.Forbidden)
	}
	if err = ctx.Err(); err != nil {
		return canceled(err)
	}
	return nil
}

var _ pc.SprintStartFacts = (*Authority)(nil)
