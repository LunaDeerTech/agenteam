package skill

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

type agentInitializerState struct {
	authority *authorityState
	creation  ac.NewAgentCreationAuthority
}

// AgentInitializer owns no background work or resource lifetime. Discovery is
// bounded SQL; the final operation is part of the caller's live transaction.
// It does not enable ordinary assignment, F1, or a production Agent service.
type AgentInitializer struct{ data func() *agentInitializerState }

func NewAgentInitializer(authority *Authority, creation ac.NewAgentCreationAuthority) (*AgentInitializer, error) {
	state := authority.state()
	if state == nil || nilPort(state.store) || nilPort(state.projects) || nilPort(creation) {
		return nil, fault(f.DependencyUnbound)
	}
	bound := &agentInitializerState{authority: state, creation: creation}
	return &AgentInitializer{data: func() *agentInitializerState { return bound }}, nil
}
func (a *AgentInitializer) state() *agentInitializerState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}
func (AgentInitializer) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_agent_initializer")
}
func (AgentInitializer) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_agent_initializer"`), nil
}
func (*AgentInitializer) UnmarshalJSON([]byte) error { return invalid() }
func (AgentInitializer) LogValue() slog.Value        { return slog.StringValue("skill_agent_initializer") }

// Only safe immutable mapping facts are frozen. The revision is an
// initialization observation, not a running Agent's bound_revision.
type agentPublication struct {
	project                                 id.ProjectID
	creation                                pc.CreationID
	initialization                          f.IdempotencyKey
	skill                                   sc.SkillID
	revisionID                              sc.RevisionID
	object                                  oc.ObjectID
	upload                                  oc.UploadID
	attempt                                 oc.AttemptID
	revision                                f.Revision
	initializationVersion, skillVersion     f.Version
	semantic, packageDigest, manifestDigest f.Digest
}

type agentInitializationPlanState struct {
	issuer      *agentInitializerState
	binding     f.Digest
	publication agentPublication
	creation    ac.NewAgentCreationPlan
	locks       []f.LockRequest
}
type agentInitializationPlan struct {
	data func() *agentInitializationPlanState
}

func (p *agentInitializationPlan) state() *agentInitializationPlanState {
	if p == nil || p.data == nil {
		return nil
	}
	return p.data()
}
func (p *agentInitializationPlan) RequiredLocks() []f.LockRequest {
	if s := p.state(); s != nil {
		return slices.Clone(s.locks)
	}
	return nil
}
func (agentInitializationPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_agent_initialization_plan")
}
func (agentInitializationPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_agent_initialization_plan"`), nil
}
func (*agentInitializationPlan) UnmarshalJSON([]byte) error { return invalid() }
func (agentInitializationPlan) LogValue() slog.Value {
	return slog.StringValue("skill_agent_initialization_plan")
}

func agentInitializationBinding(r ac.SkillsInitializationRequest) f.Digest {
	// Deliberate private scalar projection; generic Actor/Command encoders are
	// redacted and cannot be used to bind authorization or idempotency.
	d := r.Actor.Details()
	raw, _ := json.Marshal(struct {
		User, Session, Project, Agent, Command string
		Revision                               f.Version
		Enabled                                bool
	}{d.UserID, d.SessionID, r.ProjectID.String(), r.AgentID.String(), r.Command.Canonical(), r.PlanRevision, r.AddSkillsEnabled})
	h := sha256.Sum256(append([]byte("skill-new-agent-initialization/v1\x00"), raw...))
	return f.Digest("sha256:" + hex.EncodeToString(h[:]))
}

func agentPublicationInTx(ctx context.Context, store Store, x postgres.SQLExecutor, tx f.Tx, project id.ProjectID) (agentPublication, error) {
	r, err := loadInitialization(ctx, x, project)
	if err != nil {
		return agentPublication{}, err
	}
	if r == nil || r.phase != initializationPublished {
		return agentPublication{}, fault(f.InvalidState)
	}
	if err = store.RequireHeldLocks(ctx, tx, []f.LockRequest{skillLock(r.skill, f.Shared)}); err != nil {
		return agentPublication{}, portError(err)
	}
	m, _, err := loadPublished(ctx, x, *r)
	if err != nil {
		return agentPublication{}, err
	}
	return agentPublication{project: r.request.ProjectID, creation: r.request.CreationID, initialization: r.request.InitializationKey,
		skill: r.skill, revisionID: r.revision, object: r.object, upload: r.upload, attempt: r.attempt, revision: m.CurrentRevision,
		initializationVersion: r.version, skillVersion: m.Version, semantic: r.semantic, packageDigest: r.bundle.packageDigest, manifestDigest: r.bundle.manifestDigest}, nil
}

func (a *AgentInitializer) DiscoverNewAgentInitialization(ctx context.Context, r ac.SkillsInitializationRequest) (ac.SkillsInitializationPlan, error) {
	state := a.state()
	if state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil || r.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	creation, err := state.creation.DiscoverNewAgentCreation(ctx, r)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(creation) {
		return nil, fault(f.DependencyUnbound)
	}
	agentKey, _ := f.AgentLock(r.AgentID.String())
	locks := append([]f.LockRequest{commandLock(r.Command), userLock(r.Actor.Details().UserID, f.Exclusive), projectLock(r.ProjectID, f.Exclusive), {Key: agentKey, Mode: f.Exclusive}}, creation.RequiredLocks()...)
	// Candidate facts are used only to predeclare a lock. The authorized Tx
	// rereads them and must hold that exact Skill lock; there is no late acquire.
	candidate, _ := loadInitialization(ctx, state.authority.store, r.ProjectID)
	if candidate != nil {
		locks = append(locks, skillLock(candidate.skill, f.Shared))
	}
	locks, err = ob.NormalizeLocks(locks)
	if err != nil {
		return nil, portError(err)
	}
	attempt, err := f.NewID[f.TransactionAttempt]()
	if err != nil {
		return nil, unavailable(err)
	}
	cause, err := f.NewJobCause("skill-agent-initialization-discover", r.AgentID.String(), attempt.String())
	if err != nil {
		return nil, err
	}
	var publication agentPublication
	var callbackErr error
	result := state.authority.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = state.authority.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := state.authority.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		grant, err := state.authority.projects.RequireOwnerInTx(ctx, tx, r.Actor, r.ProjectID, id.Read)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(r.Actor, r.ProjectID) {
			return unavailable(nil)
		}
		publication, err = agentPublicationInTx(ctx, state.authority.store, x, tx, r.ProjectID)
		return err
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return nil, callbackErr
	}
	if err = commitError(result); err != nil {
		return nil, err
	}
	p := &agentInitializationPlanState{issuer: state, binding: agentInitializationBinding(r), publication: publication, creation: creation, locks: slices.Clone(locks)}
	return &agentInitializationPlan{data: func() *agentInitializationPlanState { return p }}, nil
}

func (a *AgentInitializer) InitializeNewAgentInTx(ctx context.Context, tx f.Tx, r ac.SkillsInitializationRequest, discovered ac.SkillsInitializationPlan) error {
	state := a.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || r.Validate() != nil || !tx.Valid() {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	plan, ok := discovered.(*agentInitializationPlan)
	if !ok || plan.state() == nil {
		return fault(f.Forbidden)
	}
	p := plan.state()
	if p.issuer != state || p.binding != agentInitializationBinding(r) {
		return fault(f.Forbidden)
	}
	x, err := state.authority.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = state.authority.store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	// Read/initialized and write gates are both revalidated today. In
	// particular an archived/deleting Project cannot gain an assignment.
	for _, intent := range []id.AccessIntent{id.Read, id.Mutate} {
		grant, e := state.authority.projects.RequireOwnerInTx(ctx, tx, r.Actor, r.ProjectID, intent)
		if e != nil {
			return portError(e)
		}
		if !grant.Matches(r.Actor, r.ProjectID) {
			return unavailable(nil)
		}
	}
	if err = state.creation.CheckNewAgentCreationAppliedInTx(ctx, tx, r, p.creation); err != nil {
		return portError(err)
	}
	publication, err := agentPublicationInTx(ctx, state.authority.store, x, tx, r.ProjectID)
	if err != nil {
		return err
	}
	if publication != p.publication {
		return fault(f.ResourceBusy)
	}
	previous, err := loadAgentInitialization(ctx, x, r.ProjectID, r.AgentID)
	if err != nil {
		return err
	}
	if previous != nil {
		if previous.command != r.Command.Canonical() || previous.planRevision != r.PlanRevision || previous.binding != p.binding || previous.enabled != r.AddSkillsEnabled || previous.skill != publication.skill || previous.revision != publication.revision {
			return fault(f.Forbidden)
		}
		return nil
	}
	_, err = insertAgentInitialization(ctx, x, agentInitializationRecord{project: r.ProjectID, agent: r.AgentID, command: r.Command.Canonical(), planRevision: r.PlanRevision,
		binding: p.binding, enabled: r.AddSkillsEnabled, skill: publication.skill, revision: publication.revision})
	return err
}

var _ ac.AgentSkillsInitializer = (*AgentInitializer)(nil)
