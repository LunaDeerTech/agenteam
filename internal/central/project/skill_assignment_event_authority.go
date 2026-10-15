package project

import (
	"context"
	"encoding/json"
	"slices"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

const skillAssignmentEventPurpose = "project.skill-assignment.append-v1"

// Project proves only the current Owner and lifecycle gate. Outbox separately
// requires Skill's complete command/payload and live applied-fact witness;
// neither this summary nor a Project dependency is a Skill mutation proof.
func skillAssignmentEventBinding(request oc.ProjectRequest) (f.Digest, []f.LockRequest, error) {
	if request.Validate() != nil {
		return "", nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	if d.Kind != oc.AppendProject || d.Event.Producer != "skill" || h.SchemaVersion != 1 || h.AggregateType != "skill.assignment" || (h.EventType != "skill.assignment_added" && h.EventType != "skill.assignment_removed") {
		return "", nil, fault(f.DependencyUnbound)
	}
	if d.Actor.Details().Kind != id.Human || h.AggregateVersion == nil || *h.AggregateVersion < 2 || h.AggregateSequence != nil || h.Scope.Kind != event.ProjectScope || h.Scope.ProjectID.String() != d.ProjectID.String() {
		return "", nil, fault(f.Forbidden)
	}
	// Bind the same original request to both existing Outbox stages, including
	// its actual Session and entire Summary (event identity and payload digest).
	raw, err := json.Marshal(struct {
		Purpose string
		Actor   id.ActorDetails
		Summary event.Summary
		Project id.ProjectID
		Stages  [2]oc.Stage
	}{skillAssignmentEventPurpose, d.Actor.Details(), d.Event, d.ProjectID, [2]oc.Stage{oc.CurrentAccess, oc.NewFact}})
	if err != nil {
		return "", nil, invalid()
	}
	locks := []f.LockRequest{userLock(d.Actor.Details().UserID, f.Shared), projectLock(d.ProjectID, f.Shared)}
	return digest(raw), locks, nil
}

func (a *Authority) discoverSkillAssignmentEvent(ctx context.Context, request oc.ProjectRequest) (oc.Dependencies, error) {
	if ctx == nil {
		return oc.Dependencies{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return oc.Dependencies{}, portError(err)
	}
	if request.Details().Stage != oc.CurrentAccess {
		return oc.Dependencies{}, fault(f.Forbidden)
	}
	if request.Details().Actor.Details().Kind == id.AgentRun {
		facts, err := a.agentSkillAssignmentEventFacts(request)
		if err != nil {
			return oc.Dependencies{}, err
		}
		return facts.DiscoverAssignmentProjectEvent(ctx, request.Details().Actor, request.Details().Event)
	}
	binding, locks, err := skillAssignmentEventBinding(request)
	if err != nil {
		return oc.Dependencies{}, err
	}
	return oc.NewDependencies(a.state().projectIssuer, binding, locks, []byte(skillAssignmentEventPurpose))
}

func (a *Authority) validateSkillAssignmentEventInTx(ctx context.Context, tx f.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	if ctx == nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	if request.Details().Actor.Details().Kind == id.AgentRun {
		return a.validateAgentSkillAssignmentEventInTx(ctx, tx, request, deps)
	}
	binding, locks, err := skillAssignmentEventBinding(request)
	if err != nil {
		return err
	}
	if !deps.Matches(a.state().projectIssuer, binding) || string(deps.Opaque()) != skillAssignmentEventPurpose || !slices.EqualFunc(deps.Locks(), locks, func(a, b f.LockRequest) bool {
		return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode
	}) {
		return fault(f.Forbidden)
	}
	if _, err = a.state().store.InTx(tx); err != nil {
		return unavailable(err)
	}
	if err = a.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return unavailable(err)
	}
	d := request.Details()
	intent := id.Read
	if d.Stage == oc.NewFact {
		intent = id.Mutate
	}
	_, err = a.RequireOwnerInTx(ctx, tx, d.Actor, d.ProjectID, intent)
	return err
}

func (a *Authority) agentSkillAssignmentEventFacts(request oc.ProjectRequest) (sc.AssignmentProjectEventAuthority, error) {
	if request.Validate() != nil {
		return nil, invalid()
	}
	d := request.Details()
	h := d.Event.Header
	if d.Kind != oc.AppendProject || d.Actor.Details().Kind != id.AgentRun || d.Actor.Details().ProjectID != d.ProjectID.String() || d.Event.Producer != "skill" || sc.ValidateAssignmentEventHeader(h) != nil || h.Scope.ProjectID.String() != d.ProjectID.String() {
		return nil, fault(f.Forbidden)
	}
	facts, ok := a.state().auditFacts[audit.SkillProducer].(sc.AssignmentProjectEventAuthority)
	if !ok || nilPort(facts) {
		return nil, fault(f.DependencyUnbound)
	}
	return facts, nil
}

func (a *Authority) validateAgentSkillAssignmentEventInTx(ctx context.Context, tx f.Tx, request oc.ProjectRequest, deps oc.Dependencies) error {
	facts, err := a.agentSkillAssignmentEventFacts(request)
	if err != nil {
		return err
	}
	d := request.Details()
	if deps.Validate() != nil {
		return fault(f.Forbidden)
	}
	locks := append(deps.Locks(), projectLock(d.ProjectID, f.Shared))
	if err = a.state().store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return unavailable(err)
	}
	x, err := a.state().store.InTx(tx)
	if err != nil {
		return unavailable(err)
	}
	p, err := loadProject(ctx, x, d.ProjectID)
	if err != nil {
		return err
	}
	if p == nil {
		return fault(f.NotFound)
	}
	initialized := c.InitializationPending
	if p.initialized {
		initialized = c.Initialized
	}
	if err = c.CheckOwnerGate(p.ref.Lifecycle, initialized, id.Mutate); err != nil {
		return err
	}
	return portError(facts.ValidateAssignmentProjectEventInTx(ctx, tx, d.Actor, d.Event, deps, d.Stage))
}
