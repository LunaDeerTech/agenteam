package skill

import (
	"bytes"
	"context"

	au "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

func assignmentEvent(events sc.AssignmentEvents, r *assignmentCommand) (ec.Event, error) {
	if r.validate() != nil || !r.Candidate.Changed {
		return ec.Event{}, fault(f.Forbidden)
	}
	c := r.Candidate
	id, err := f.ParseID[ec.EventIdentity](r.ID.String())
	if err != nil {
		return ec.Event{}, unavailable(err)
	}
	p, err := f.ParseID[ec.Project](c.After.Project.String())
	if err != nil {
		return ec.Event{}, unavailable(err)
	}
	agent, err := f.ParseID[ec.Aggregate](c.After.Agent.String())
	if err != nil {
		return ec.Event{}, unavailable(err)
	}
	kind := sc.AssignmentAddedEvent
	affected := c.Added
	if r.Input.Request.AssignmentID != nil {
		kind = sc.AssignmentRemovedEvent
		affected = c.Removed
	}
	if affected == nil {
		return ec.Event{}, unavailable(nil)
	}
	payload := sc.AssignmentEventPayload{AgentID: c.After.Agent, AssignmentID: affected.ID, AssignmentSequence: c.After.Sequence, SkillID: c.Skill, ChangeID: r.ID}
	if c.Runtime != nil {
		v := c.Runtime.Binding.Revision
		payload.FixedRevision = &v
	}
	version := c.Version
	return events.New(ec.Header{EventID: id, EventType: kind, SchemaVersion: 1, OccurredAt: c.Created, Scope: ec.Scope{Kind: ec.ProjectScope, ProjectID: p}, AggregateType: sc.AssignmentAggregate, AggregateID: agent, AggregateVersion: &version}, payload)
}
func assignmentAudit(r *assignmentCommand, actor i.Actor) (au.Entry, au.AppendKey, error) {
	var zero au.Entry
	var key au.AppendKey
	if r.validate() != nil || !r.Candidate.Changed || actor.Details().UserID != r.Input.Actor.Details().UserID {
		return zero, key, fault(f.Forbidden)
	}
	c := r.Candidate
	affected := c.Added
	enabled := false
	if affected != nil {
		enabled = affected.Enabled
	} else {
		affected = c.Removed
	}
	if affected == nil {
		return zero, key, unavailable(nil)
	}
	action := au.Action(r.Input.Request.name())
	meta, err := au.SkillAssignmentMetadata(action, au.SkillAssignmentMetadataFields{AgentID: c.After.Agent.String(), AssignmentID: affected.ID.String(), SkillID: c.Skill.String(), ChangeID: r.ID.String(), Version: c.Version, Sequence: c.After.Sequence, Enabled: enabled})
	if err != nil {
		return zero, key, err
	}
	scope, err := i.InProject(c.After.Project)
	if err != nil {
		return zero, key, err
	}
	resource, err := au.NewResource(au.AgentResource, c.After.Agent.String())
	if err != nil {
		return zero, key, err
	}
	entry, err := au.NewEntry(au.EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: au.Success, Resource: resource, Metadata: meta})
	if err != nil {
		return zero, key, err
	}
	digest, _, err := captureDigest(r.Input.Identity.Canonical())
	if err != nil {
		return zero, key, err
	}
	key, err = au.NewAppendKey(au.SkillProducer, string(digest), 0)
	return entry, key, err
}
func (a *AssignmentAuthority) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry au.Entry, key au.AppendKey) error {
	if a == nil || a.owner.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || entry.Validate() != nil || key.Validate() != nil {
		return invalid()
	}
	w, ok := ctx.Value(assignmentWitnessKey{}).(*assignmentWriter)
	if !ok || w == nil || w.owner != a || !w.active.Load() {
		return fault(f.Forbidden)
	}
	d := entry.Fields()
	if d.Scope.Details().Kind != i.ProjectScope || d.Actor.Details().Kind != i.Human {
		return fault(f.Forbidden)
	}
	project, e := f.ParseID[i.Project](d.Scope.Details().ProjectID)
	agent, e2 := f.ParseID[i.Agent](d.Resource.Details().ID)
	if e != nil || e2 != nil {
		return fault(f.Forbidden)
	}
	x, err := a.owner.state().store.InTx(tx)
	if err != nil {
		return captureError(err)
	}
	r, err := loadAssignmentByID(ctx, x, project, agent, w.id)
	if err != nil {
		return err
	}
	if _, err = a.requireWitness(ctx, tx, r); err != nil {
		return err
	}
	expected, expectedKey, err := assignmentAudit(r, d.Actor)
	if err != nil {
		return err
	}
	v := expected.Fields()
	if d.Action != v.Action || d.Outcome != v.Outcome || d.Resource.Details() != v.Resource.Details() || d.Scope.Details() != v.Scope.Details() || d.Associations != v.Associations || !bytes.Equal(d.Metadata.JSON(), v.Metadata.JSON()) || key.Details() != expectedKey.Details() {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

// This small constructor is usable before Project's producer map is frozen.
// It delegates only through the private, active same-Store Skill write witness.
type AssignmentAuditFacts struct{ store Store }

func NewAssignmentAuditFacts(store Store) (*AssignmentAuditFacts, error) {
	if nilPort(store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &AssignmentAuditFacts{store}, nil
}
func (a *AssignmentAuditFacts) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, e au.Entry, k au.AppendKey) error {
	if a == nil || nilPort(a.store) {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return invalid()
	}
	if _, err := a.store.InTx(tx); err != nil {
		return captureError(err)
	}
	w, ok := ctx.Value(assignmentWitnessKey{}).(*assignmentWriter)
	if !ok || w == nil || w.owner == nil || !w.active.Load() || w.tx != tx {
		return fault(f.Forbidden)
	}
	if _, err := w.owner.owner.state().store.InTx(tx); err != nil {
		return captureError(err)
	}
	return w.owner.CheckProjectAuditInTx(ctx, tx, e, k)
}
func assignmentEventCoordinates(actor i.Actor, summary ec.Summary) (i.ProjectID, i.AgentID, sc.AssignmentChangeID, error) {
	if actor.Validate() != nil || actor.Details().Kind != i.Human || summary.Producer != sc.AssignmentProducer || sc.ValidateAssignmentEventHeader(summary.Header) != nil {
		return i.ProjectID{}, i.AgentID{}, sc.AssignmentChangeID{}, fault(f.Forbidden)
	}
	p, e := f.ParseID[i.Project](summary.Header.Scope.ProjectID.String())
	agent, e2 := f.ParseID[i.Agent](summary.Header.AggregateID.String())
	id, e3 := f.ParseID[sc.AssignmentChange](summary.Header.EventID.String())
	if e != nil || e2 != nil || e3 != nil {
		return p, agent, id, fault(f.Forbidden)
	}
	return p, agent, id, nil
}
func (a *AssignmentAuthority) eventBinding(actor i.Actor, summary ec.Summary, r *assignmentCommand) (f.Digest, []f.LockRequest, error) {
	if r == nil || r.Input.Actor.Details().UserID != actor.Details().UserID {
		return "", nil, fault(f.Forbidden)
	}
	expected, err := assignmentEvent(a.events, r)
	if err != nil {
		return "", nil, err
	}
	one, _, e := captureDigest(expected.Summary())
	two, _, e2 := captureDigest(summary)
	if e != nil || e2 != nil || one != two {
		return "", nil, fault(f.Forbidden)
	}
	candidate, _, err := captureDigest(r.Candidate)
	if err != nil {
		return "", nil, err
	}
	binding, _, err := captureDigest(struct {
		User      string
		Event     f.Digest
		Candidate f.Digest
	}{actor.Details().UserID, one, candidate})
	if err != nil {
		return "", nil, err
	}
	locks, err := ob.NormalizeLocks(append(r.Input.agentRequest().RequiredLocks(), skillLock(r.Candidate.Skill, f.Shared)))
	return binding, locks, err
}
func (a *AssignmentAuthority) DiscoverAppend(ctx context.Context, actor i.Actor, summary ec.Summary) (ob.Dependencies, error) {
	var out ob.Dependencies
	if a == nil || a.owner.state() == nil {
		return out, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return out, invalid()
	}
	project, agent, id, err := assignmentEventCoordinates(actor, summary)
	if err != nil {
		return out, err
	}
	key, err := f.NewID[f.TransactionAttempt]()
	if err != nil {
		return out, unavailable(err)
	}
	cause, _ := f.NewJobCause("skill-assignment-event", id.String(), key.String())
	agentKey, _ := f.AgentLock(agent.String())
	base := []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), {Key: agentKey, Mode: f.Shared}}
	result := a.owner.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		st := a.owner.state()
		if err := st.store.AcquireAll(ctx, tx, base); err != nil {
			return captureError(err)
		}
		grant, err := st.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return captureError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return captureError(err)
		}
		r, err := loadAssignmentByID(ctx, x, project, agent, id)
		if err != nil {
			return err
		}
		binding, locks, err := a.eventBinding(actor, summary, r)
		if err != nil {
			return err
		}
		out, err = ob.NewDependencies(a.issuer, binding, locks, []byte(id.String()))
		return err
	})
	if err = commitError(result); err != nil {
		return ob.Dependencies{}, err
	}
	return out, ctx.Err()
}
func (a *AssignmentAuthority) ValidateAppendInTx(ctx context.Context, tx f.Tx, actor i.Actor, summary ec.Summary, deps ob.Dependencies, stage ob.Stage) error {
	if a == nil || a.owner.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || !stage.Valid() || deps.Validate() != nil || !deps.Matches(a.issuer, deps.Binding()) {
		return fault(f.Forbidden)
	}
	project, agent, id, err := assignmentEventCoordinates(actor, summary)
	if err != nil {
		return err
	}
	st := a.owner.state()
	if err = st.store.RequireHeldLocks(ctx, tx, deps.Locks()); err != nil {
		return captureError(err)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return captureError(err)
	}
	grant, err := st.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return captureError(err)
	}
	if !grant.Matches(actor, project) {
		return fault(f.Forbidden)
	}
	r, err := loadAssignmentByID(ctx, x, project, agent, id)
	if err != nil {
		return err
	}
	binding, locks, err := a.eventBinding(actor, summary, r)
	if err != nil {
		return err
	}
	if !deps.Matches(a.issuer, binding) || string(deps.Opaque()) != id.String() {
		return fault(f.Forbidden)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return captureError(err)
	}
	if stage == ob.NewFact {
		_, err = a.requireWitness(ctx, tx, r)
		return err
	}
	return ctx.Err()
}

var _ ob.ProducerAuthority = (*AssignmentAuthority)(nil)
var _ au.ProjectFactAuthority = (*AssignmentAuditFacts)(nil)
