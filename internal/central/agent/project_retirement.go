package agent

import (
	"context"
	"sync"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// All owners are fixed dependencies, including owners of empty initialized
// reference sets. The consumer never commits the Project caller's transaction.
type ProjectRetirementDependencies struct {
	Work, Execution                        c.ProjectRetirementGate
	Models, Tools, Mounts, Secrets, Skills c.ProjectRetirementReferences
}

type ProjectRetirement struct {
	agents   *Authority
	projects *project.Authority
	deps     ProjectRetirementDependencies
}

func NewProjectRetirement(agents *Authority, projects *project.Authority, deps ProjectRetirementDependencies) (*ProjectRetirement, error) {
	if agents == nil || agents.state == nil || projects == nil || nilPort(deps.Work) || nilPort(deps.Execution) {
		return nil, fault(f.DependencyUnbound)
	}
	r := &ProjectRetirement{agents: agents, projects: projects, deps: deps}
	kinds := []c.ProjectRetirementReferenceKind{c.ProjectRetirementModelReferences, c.ProjectRetirementToolReferences, c.ProjectRetirementMountReferences, c.ProjectRetirementSecretReferences, c.ProjectRetirementSkillReferences}
	for n, owner := range r.references() {
		if nilPort(owner) || owner.ProjectRetirementReferenceKind() != kinds[n] {
			return nil, fault(f.DependencyUnbound)
		}
	}
	return r, nil
}

func (r *ProjectRetirement) references() []c.ProjectRetirementReferences {
	return []c.ProjectRetirementReferences{r.deps.Models, r.deps.Tools, r.deps.Mounts, r.deps.Secrets, r.deps.Skills}
}
func (r *ProjectRetirement) bound() bool {
	return r != nil && r.agents != nil && r.agents.state != nil && r.projects != nil
}

func (r *ProjectRetirement) ProjectRetirementLocks(plan c.ProjectRetirementPlan) ([]f.LockRequest, error) {
	if !r.bound() {
		return nil, fault(f.DependencyUnbound)
	}
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	locks := plan.Change.Request.RequiredLocks()
	for _, owner := range r.references() {
		more, err := owner.ProjectRetirementLocks(plan.Change.Clone())
		if err != nil {
			return nil, portError(err)
		}
		locks = append(locks, more...)
	}
	if len(locks) > 512 {
		return nil, fault(f.ResourceBusy)
	}
	return oc.NormalizeLocks(locks)
}

func (r *ProjectRetirement) original(ctx context.Context, tx f.Tx, plan c.ProjectRetirementPlan, recovery bool) (postgres.SQLExecutor, error) {
	locks, err := r.ProjectRetirementLocks(plan)
	if err != nil {
		return nil, err
	}
	if ctx == nil || !tx.Valid() {
		return nil, invalid()
	}
	if err = ctx.Err(); err != nil {
		return nil, portError(err)
	}
	x, err := r.agents.state.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	if err = r.agents.state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	if recovery {
		err = r.projects.RequireAgentRetirementAppliedInTx(ctx, tx, plan.Clone())
	} else {
		err = r.projects.RequireAgentRetirementInTx(ctx, tx, plan.Change.Request)
	}
	if err != nil {
		return nil, portError(err)
	}
	if err = checkRetirementCommand(ctx, x, plan); err != nil {
		return nil, err
	}
	return x, portError(ctx.Err())
}

// A witness lives only inside the original synchronous consumer call. It is
// never an exported DTO, a reusable token or evidence of a Store commit.
type projectRetirementWitnessKey struct{}
type projectRetirementWitness struct {
	mu                      sync.Mutex
	open, applied, recovery bool
	terminal                bool
	consumer                *ProjectRetirement
	tx                      f.Tx
	plan                    c.ProjectRetirementPlan
}

func (w *projectRetirementWitness) close() { w.mu.Lock(); w.open = false; w.mu.Unlock() }
func sameRetirementChange(a, b c.ProjectRetirementChange) bool {
	return a.Request.Actor.Equal(b.Request.Actor) && a.Request.ProjectID == b.Request.ProjectID && a.Request.AgentID == b.Request.AgentID && a.Request.Cause == b.Request.Cause && a.Request.ExpectedVersion == b.Request.ExpectedVersion && sameValue(a.Before, b.Before)
}
func (a *Authority) requireProjectRetirement(ctx context.Context, tx f.Tx, change c.ProjectRetirementChange, applied bool) error {
	if ctx == nil || change.Validate() != nil || !tx.Valid() {
		return invalid()
	}
	if a == nil || a.state == nil {
		return fault(f.DependencyUnbound)
	}
	w, _ := ctx.Value(projectRetirementWitnessKey{}).(*projectRetirementWitness)
	if w == nil || w.consumer == nil || w.consumer.agents.state != a.state || w.tx != tx || !sameRetirementChange(w.plan.Change, change) {
		return fault(f.Forbidden)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.open || w.applied != applied || w.recovery && !applied {
		return fault(f.Forbidden)
	}
	var x postgres.SQLExecutor
	var err error
	if w.terminal {
		if !applied || !w.recovery {
			return fault(f.Forbidden)
		}
		x, err = w.consumer.terminalOriginal(ctx, tx, w.plan)
	} else {
		x, err = w.consumer.original(ctx, tx, w.plan, w.recovery)
	}
	if err != nil {
		return err
	}
	if applied {
		return requireRetirementCanonicalAbsent(ctx, x, change)
	}
	current, err := loadAgent(ctx, x, change.Request.ProjectID, change.Request.AgentID)
	if err != nil {
		return err
	}
	if current == nil || !sameValue(*current, change.Before) {
		return fault(f.VersionConflict)
	}
	return portError(ctx.Err())
}
func (a *Authority) RequireProjectRetirementPreparedInTx(ctx context.Context, tx f.Tx, change c.ProjectRetirementChange) error {
	return a.requireProjectRetirement(ctx, tx, change, false)
}
func (a *Authority) RequireProjectRetirementAppliedInTx(ctx context.Context, tx f.Tx, change c.ProjectRetirementChange) error {
	return a.requireProjectRetirement(ctx, tx, change, true)
}

func (r *ProjectRetirement) RetireProjectAgentInTx(ctx context.Context, tx f.Tx, plan c.ProjectRetirementPlan) error {
	plan = plan.Clone()
	x, err := r.original(ctx, tx, plan, false)
	if err != nil {
		return err
	}
	current, err := initializedAgentState(ctx, x, plan.Change.Request.ProjectID, plan.Change.Request.AgentID, true)
	if err != nil {
		return err
	}
	if !sameValue(current, plan.Change.Before) {
		return fault(f.VersionConflict)
	}
	w := &projectRetirementWitness{open: true, consumer: r, tx: tx, plan: plan}
	defer w.close()
	run := context.WithValue(ctx, projectRetirementWitnessKey{}, w)
	for _, gate := range []c.ProjectRetirementGate{r.deps.Work, r.deps.Execution} {
		if err = gate.RequireProjectAgentRetirementInTx(run, tx, plan.Change.Clone()); err != nil {
			return portError(err)
		}
		if err = run.Err(); err != nil {
			return portError(err)
		}
	}
	if err = deleteRetirementCanonical(run, x, plan.Change); err != nil {
		return err
	}
	w.mu.Lock()
	w.applied = true
	w.mu.Unlock()
	for _, owner := range r.references() {
		if err = owner.RetireProjectAgentReferencesInTx(run, tx, plan.Change.Clone()); err != nil {
			return portError(err)
		}
		if err = run.Err(); err != nil {
			return portError(err)
		}
	}
	return nil
}

func (r *ProjectRetirement) RequireProjectAgentRetiredInTx(ctx context.Context, tx f.Tx, plan c.ProjectRetirementPlan) error {
	plan = plan.Clone()
	x, err := r.original(ctx, tx, plan, true)
	if err != nil {
		return err
	}
	if err = requireRetirementCanonicalAbsent(ctx, x, plan.Change); err != nil {
		return err
	}
	w := &projectRetirementWitness{open: true, applied: true, recovery: true, consumer: r, tx: tx, plan: plan}
	defer w.close()
	run := context.WithValue(ctx, projectRetirementWitnessKey{}, w)
	for _, owner := range r.references() {
		if err = owner.RequireProjectAgentReferencesRetiredInTx(run, tx, plan.Change.Clone()); err != nil {
			return portError(err)
		}
		if err = run.Err(); err != nil {
			return portError(err)
		}
	}
	return nil
}

func (r *ProjectRetirement) read(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, p i.ProjectID, fn func(context.Context, postgres.SQLExecutor) error) error {
	if !r.bound() {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || actor.Validate() != nil || cause.Validate() != nil || p.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	txCause, err := f.NewRecoveryCause("agent-project-retirement", cause.OperationID.String(), p.String())
	if err != nil {
		return invalid()
	}
	validated := false
	result := r.agents.state.store.WithinTx(ctx, txCause, func(ctx context.Context, tx f.Tx) error {
		if err := r.agents.state.store.AcquireAll(ctx, tx, []f.LockRequest{projectLock(p, f.Exclusive)}); err != nil {
			return portError(err)
		}
		if err := r.projects.RequireAgentRetirementScopeInTx(ctx, tx, actor, cause, p); err != nil {
			return portError(err)
		}
		x, err := r.agents.state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if nilPort(x) {
			return fault(f.DependencyUnbound)
		}
		if err = fn(ctx, x); err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return portError(err)
		}
		validated = true
		return nil
	})
	if err = commitError(result); err != nil {
		return err
	}
	if !validated {
		return unavailable(nil)
	}
	return portError(ctx.Err())
}

var _ c.ProjectRetirementConsumer = (*ProjectRetirement)(nil)
var _ c.ProjectRetirementOwner = (*Authority)(nil)
