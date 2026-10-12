package execution

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// ProjectExecutionRetirement is explicitly bound to the original four-owner
// pair. Constructing another adapter cannot take over a retained same-P call.
type ProjectExecutionRetirement struct {
	pair  *projectCallState
	gate  ec.ProjectExecutionCleanupAuthority
	refs  [4]ec.ProjectExecutionRetirementReferences
	skill ec.ProjectExecutionSkillRetirementReferences
}

type executionRetirementControl struct {
	owner                                *ProjectExecutionRetirement
	actor                                i.Actor
	cause                                pc.LifecycleCause
	scope                                pc.ScopeRef
	active, confirming, touched, checked bool
	unknown                              *f.CommitResult
	plan                                 *executionRetirementPlan
	ledger                               []projectCallRecord
	origins                              []executionRetirementCallOrigin
	proven                               map[oc.ProcessID]bool
	discovery, empty                     bool
}

type executionRetirementPlan struct {
	owner      *ProjectExecutionRetirement
	control    *executionRetirementControl
	request    ec.ProjectExecutionRetirementRequest
	before     *executionRetirementBefore
	locks      []f.LockRequest
	references [4]ec.ProjectExecutionRetirementReferencePlan
	commit     ec.ProjectExecutionRetirementCommit
	applied    bool
}

func (p *executionRetirementPlan) Request() ec.ProjectExecutionRetirementRequest { return p.request }
func (p *executionRetirementPlan) RequiredLocks() []f.LockRequest                { return slices.Clone(p.locks) }

type executionRetirementContextKey struct{}
type executionRetirementContext struct {
	owner   *ProjectExecutionRetirement
	control *executionRetirementControl
}
type executionRetirementWitnessKey struct{}
type executionRetirementWitness struct {
	owner   *ProjectExecutionRetirement
	control *executionRetirementControl
	plan    *executionRetirementPlan
	tx      f.Tx
	applied bool
	active  atomic.Bool
}

func NewProjectExecutionRetirement(pair *ProjectCallOwnership, gate ec.ProjectExecutionCleanupAuthority, tools, variables ec.ProjectExecutionRetirementReferences, skills ec.ProjectExecutionSkillRetirementReferences, mounts ec.ProjectExecutionRetirementReferences) (*ProjectExecutionRetirement, error) {
	if pair == nil || pair.state == nil || nilPort(gate) || nilPort(tools) || nilPort(variables) || nilPort(skills) || nilPort(mounts) {
		return nil, fault(f.DependencyUnbound)
	}
	refs := [4]ec.ProjectExecutionRetirementReferences{tools, variables, skills, mounts}
	kinds := [4]ec.ProjectExecutionRetirementReferenceKind{ec.ProjectExecutionToolReferences, ec.ProjectExecutionVariablesReferences, ec.ProjectExecutionSkillReferences, ec.ProjectExecutionMountReferences}
	for n, ref := range refs {
		if ref.ProjectExecutionRetirementReferenceKind() != kinds[n] {
			return nil, fault(f.DependencyUnbound)
		}
	}
	// Only this explicit graph enables the post-83 provenance write. Legacy
	// whole-call constructors keep their original SQL and cannot backfill it.
	pair.state.mu.Lock()
	pair.state.retirementOrigins = true
	pair.state.mu.Unlock()
	return &ProjectExecutionRetirement{pair: pair.state, gate: gate, refs: refs, skill: skills}, nil
}
func (r *ProjectExecutionRetirement) ProjectExecutionRetirementIdentity() ec.ProjectExecutionRetirementIdentity {
	if r == nil || r.pair == nil {
		return ec.ProjectExecutionRetirementIdentity{}
	}
	return ec.ProjectExecutionRetirementIdentity{ProcessID: r.pair.process.String(), InstanceID: r.pair.instance.String()}
}

func (r *ProjectExecutionRetirement) WithProjectExecutionRetirement(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, fn ec.ProjectExecutionRetirementCall) (result f.CommitResult, err error) {
	result = f.NotCommittedResult(nil)
	if r == nil || r.pair == nil || nilPort(r.gate) {
		return result, fault(f.DependencyUnbound)
	}
	if ctx == nil || fn == nil || actor.Validate() != nil || cause.Validate() != nil || cause.Action != pc.Delete || pc.RequireProjectScope(scope) != nil {
		return result, invalid()
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != scope.ProjectID.String() || d.CauseRef != cause.OperationID.String() {
		return result, fault(f.Forbidden)
	}
	s := r.pair
	s.mu.Lock()
	if s.retirements == nil {
		s.retirements = map[i.ProjectID]*executionRetirementControl{}
	}
	c := s.retirements[scope.ProjectID]
	if c != nil && (c.owner != r || c.active || !c.actor.Equal(actor) || c.cause != cause || !c.scope.Equal(scope)) {
		s.mu.Unlock()
		return result, fault(f.ResourceBusy)
	}
	if c == nil {
		if s.stopped || len(s.retirements) >= maxProjectStopControls || !r.localReturnedLocked(scope.ProjectID) {
			s.mu.Unlock()
			return result, fault(f.ResourceBusy)
		}
		c = &executionRetirementControl{owner: r, actor: actor, cause: cause, scope: scope, proven: map[oc.ProcessID]bool{}}
		s.retirements[scope.ProjectID] = c
	}
	c.active = true
	c.confirming = c.unknown != nil
	c.touched = false
	c.checked = false
	s.signalLocked()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		c.active = false
		if c.unknown == nil && s.retirements[scope.ProjectID] == c {
			delete(s.retirements, scope.ProjectID)
		}
		s.signalLocked()
		s.mu.Unlock()
	}()
	run := context.WithValue(ctx, executionRetirementContextKey{}, executionRetirementContext{r, c})
	result, err = fn(run, c.confirming)
	if result.State() == f.Unknown && result.AttemptID().Validate() != nil && err != nil {
		if original, ok := UnknownAttempt(err); ok && original.State() == f.Unknown && original.AttemptID().Validate() == nil && original.Cause().Validate() == nil {
			result = original
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.unknown != nil {
		if c.confirming && err == nil && result.State() == f.Committed && c.checked && ctx.Err() == nil {
			c.unknown = nil
		} else {
			return *c.unknown, commitError(*c.unknown)
		}
	} else if result.State() == f.Unknown && result.AttemptID().Validate() == nil {
		original := result
		c.unknown = &original
		return result, commitError(result)
	} else if err != nil && result.State() == f.Unknown && result.AttemptID().Validate() != nil && result.Cause().Validate() != nil && result.Fault() == nil {
		// A callback rejected before its transaction may return the zero value.
		// It is not an actual uncertain attempt. Discovery's original Unknown
		// has already taken precedence above, including its cause and error.
		var rejected *f.Fault
		if !errors.As(err, &rejected) {
			rejected = f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err)
		}
		result = f.NotCommittedResult(rejected)
	}
	if err != nil {
		return result, err
	}
	if err = commitError(result); err != nil {
		return result, err
	}
	if !c.touched {
		return result, fault(f.Forbidden)
	}
	return result, ctx.Err()
}

func (r *ProjectExecutionRetirement) localReturnedLocked(p i.ProjectID) bool {
	if r.pair.stops[p] != nil {
		return false
	}
	for _, e := range r.pair.owners {
		if e.record.Project == p {
			return false
		}
	}
	return true
}

func (r *ProjectExecutionRetirement) control(ctx context.Context) (*executionRetirementControl, error) {
	if r == nil || r.pair == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w, ok := ctx.Value(executionRetirementContextKey{}).(executionRetirementContext)
	if !ok || w.owner != r || w.control == nil {
		return nil, fault(f.Forbidden)
	}
	s := r.pair
	s.mu.Lock()
	defer s.mu.Unlock()
	c := w.control
	if s.retirements[c.scope.ProjectID] != c || !c.active || !r.localReturnedLocked(c.scope.ProjectID) {
		return nil, fault(f.ResourceBusy)
	}
	return c, nil
}

func (r *ProjectExecutionRetirement) requireTx(ctx context.Context, tx f.Tx, c *executionRetirementControl, locks []f.LockRequest) (postgres.SQLExecutor, error) {
	current, err := r.control(ctx)
	if err != nil {
		return nil, err
	}
	if current != c || !tx.Valid() {
		return nil, fault(f.Forbidden)
	}
	s := r.pair
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	if err = s.proof.RequireCurrentProcessInTx(ctx, tx, s.process); err != nil {
		return nil, portError(err)
	}
	if err = r.gate.RequireProjectExecutionCleanupInTx(ctx, tx, c.actor, c.cause, c.scope); err != nil {
		return nil, portError(err)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, fault(f.DependencyUnbound)
	}
	return x, nil
}
func (r *ProjectExecutionRetirement) readTx(ctx context.Context, c *executionRetirementControl, locks []f.LockRequest, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	cause, _ := f.NewRecoveryCause("execution-project-retirement-discover", c.cause.OperationID.String(), c.scope.ProjectID.String())
	result := r.pair.store.WithinTx(ctx, cause, func(run context.Context, tx f.Tx) error {
		if err := r.pair.store.AcquireAll(run, tx, locks); err != nil {
			return portError(err)
		}
		x, err := r.requireTx(run, tx, c, locks)
		if err != nil {
			return err
		}
		return fn(run, tx, x)
	})
	if result.State() == f.Unknown {
		r.pair.mu.Lock()
		if c.unknown == nil {
			original := result
			c.unknown = &original
			c.discovery = true
		}
		r.pair.mu.Unlock()
	}
	return commitError(result)
}

func (r *ProjectExecutionRetirement) DiscoverProjectExecutionRetirement(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (ec.ProjectExecutionRetirementPlan, error) {
	c, err := r.control(ctx)
	if err != nil {
		return nil, err
	}
	if !c.actor.Equal(actor) || c.cause != cause || !c.scope.Equal(scope) {
		return nil, fault(f.Forbidden)
	}
	if c.confirming && !c.discovery {
		if c.plan != nil {
			return c.plan, nil
		}
		return nil, fault(f.InvalidState)
	}
	var selected i.ExecutionID
	var agent i.AgentID
	var rows []projectCallRecord
	var origins []executionRetirementCallOrigin
	err = r.readTx(ctx, c, projectCallLocks(scope.ProjectID), func(run context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		var readErr error
		rows, readErr = listProjectCalls(run, x, scope.ProjectID)
		if readErr != nil {
			return readErr
		}
		origins, readErr = readRetirementOrigins(run, x, scope.ProjectID, rows)
		if readErr != nil {
			return readErr
		}
		selected, readErr = nextRetirementExecution(run, x, scope.ProjectID)
		if readErr != nil || selected.Validate() != nil {
			return readErr
		}
		var raw string
		if readErr = x.QueryRow(run, `SELECT agent_id::text FROM agenteam_execution.executions WHERE id=$1::text AND project_id=$2::text`, selected.String(), scope.ProjectID.String()).Scan(&raw); readErr != nil {
			return portError(readErr)
		}
		agent, readErr = f.ParseID[i.Agent](raw)
		if readErr != nil {
			return unavailable(nil)
		}
		return run.Err()
	})
	if err != nil {
		return nil, err
	}
	if err = r.proveRetirementRows(ctx, c, rows); err != nil {
		return nil, err
	}
	c.ledger = slices.Clone(rows)
	c.origins = slices.Clone(origins)
	if selected.Validate() != nil {
		// This freezes the empty-tail preimage, not an Applied receipt. A later
		// mutation can fail before its first DELETE; its Unknown still needs a
		// read-only full-same/full-absent decision using this original ledger.
		c.empty = true
		c.touched = true
		if c.confirming {
			c.checked = true
		}
		return nil, nil
	}
	locks, err := normalizeProjectCallLocks(scope.ProjectID, []f.LockRequest{agentLock(agent, f.Exclusive), executionLock(selected)})
	if err != nil {
		return nil, err
	}
	p := &executionRetirementPlan{owner: r, control: c}
	err = r.readTx(ctx, c, locks, func(run context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := r.requireRetirementRows(run, x, c); err != nil {
			return err
		}
		before, err := readRetirementBefore(run, x, selected)
		if err != nil {
			return err
		}
		if before == nil || before.record.summary.ProjectID != scope.ProjectID || before.record.summary.AgentID != agent {
			return fault(f.VersionConflict)
		}
		if err = requireOriginalExecution(before, c.ledger, c.origins); err != nil {
			return err
		}
		p.before = before
		p.request = ec.ProjectExecutionRetirementRequest{Actor: actor, Cause: cause, Binding: before.binding()}
		p.locks = slices.Clone(locks)
		wctx, closeWitness := r.retirementWitness(run, tx, c, p, false)
		defer closeWitness()
		for n, ref := range r.refs {
			plan, err := ref.DiscoverProjectExecutionReferencesInTx(wctx, tx, p.request)
			if err != nil {
				return portError(err)
			}
			if nilPort(plan) || plan.ProjectExecutionRetirementReferenceDigest().Validate() != nil {
				return fault(f.DependencyUnbound)
			}
			p.references[n] = plan
			p.locks = append(p.locks, plan.RequiredLocks()...)
		}
		p.locks, err = normalizeProjectCallLocks(scope.ProjectID, p.locks)
		return err
	})
	if err != nil {
		return nil, err
	}
	c.plan = p
	c.touched = true
	if c.confirming {
		c.checked = true
	}
	return p, ctx.Err()
}

func (r *ProjectExecutionRetirement) proveRetirementRows(ctx context.Context, c *executionRetirementControl, rows []projectCallRecord) error {
	for _, row := range rows {
		if row.Phase != "business_closed" {
			return fault(f.ResourceBusy)
		}
		if row.Process == r.pair.process {
			if row.Instance != r.pair.instance {
				return fault(f.ResourceBusy)
			}
		} else if !c.proven[row.Process] {
			if err := r.pair.proof.ConfirmStopped(ctx, row.Process); err != nil {
				return portError(err)
			}
			c.proven[row.Process] = true
		}
	}
	return ctx.Err()
}
func (r *ProjectExecutionRetirement) requireRetirementRows(ctx context.Context, x postgres.SQLExecutor, c *executionRetirementControl) error {
	rows, err := listProjectCalls(ctx, x, c.scope.ProjectID)
	if err != nil {
		return err
	}
	if !slices.Equal(rows, c.ledger) {
		return fault(f.VersionConflict)
	}
	origins, err := readRetirementOrigins(ctx, x, c.scope.ProjectID, rows)
	if err != nil {
		return err
	}
	if !slices.Equal(origins, c.origins) {
		return fault(f.VersionConflict)
	}
	return ctx.Err()
}

func (r *ProjectExecutionRetirement) retirementWitness(ctx context.Context, tx f.Tx, c *executionRetirementControl, p *executionRetirementPlan, applied bool) (context.Context, func()) {
	w := &executionRetirementWitness{owner: r, control: c, plan: p, tx: tx, applied: applied}
	w.active.Store(true)
	return context.WithValue(ctx, executionRetirementWitnessKey{}, w), func() { w.active.Store(false) }
}

func (r *ProjectExecutionRetirement) RetireProjectExecutionInTx(ctx context.Context, tx f.Tx, plan ec.ProjectExecutionRetirementPlan) error {
	c, err := r.control(ctx)
	if err != nil {
		return err
	}
	p, ok := plan.(*executionRetirementPlan)
	if !ok || p == nil || p.owner != r || p.control != c || c.plan != p || c.confirming || c.unknown != nil {
		return fault(f.Forbidden)
	}
	x, err := r.requireTx(ctx, tx, c, p.locks)
	if err != nil {
		return err
	}
	if err = r.requireRetirementRows(ctx, x, c); err != nil {
		return err
	}
	before, err := readRetirementBefore(ctx, x, p.request.Binding.ExecutionID)
	if err != nil {
		return err
	}
	if !sameRetirementBefore(before, p.before) {
		return fault(f.VersionConflict)
	}
	p.commit, err = r.gate.ProjectExecutionRetirementCommitInTx(ctx, tx, c.actor, c.cause, c.scope)
	if err != nil {
		return portError(err)
	}
	if !p.commit.Matches(p.request) {
		return fault(f.Forbidden)
	}
	wctx, closeWitness := r.retirementWitness(ctx, tx, c, p, false)
	for n, ref := range r.refs {
		if err = ref.RetireProjectExecutionReferencesInTx(wctx, tx, p.request, p.references[n]); err != nil {
			closeWitness()
			return portError(err)
		}
	}
	closeWitness()
	if err = deleteRetirementBefore(ctx, x, p.before); err != nil {
		return err
	}
	wctx, closeWitness = r.retirementWitness(ctx, tx, c, p, true)
	defer closeWitness()
	if err = r.skill.FinalizeProjectExecutionRetirementInTx(wctx, tx, p.request, p.references[2]); err != nil {
		return portError(err)
	}
	for n, ref := range r.refs {
		if err = ref.RequireProjectExecutionReferencesRetiredInTx(wctx, tx, p.request, p.references[n]); err != nil {
			return portError(err)
		}
	}
	p.applied = true
	c.touched = true
	return ctx.Err()
}

func (r *ProjectExecutionRetirement) ConfirmProjectExecutionRetirementInTx(ctx context.Context, tx f.Tx, plan ec.ProjectExecutionRetirementPlan) (bool, error) {
	c, err := r.control(ctx)
	if err != nil {
		return false, err
	}
	if !c.confirming || c.unknown == nil || c.discovery {
		return false, fault(f.Forbidden)
	}
	if plan == nil {
		return r.confirmRetirementEmpty(ctx, tx, c)
	}
	p, ok := plan.(*executionRetirementPlan)
	if !ok || p == nil || p.owner != r || p.control != c || c.plan != p {
		return false, fault(f.Forbidden)
	}
	x, err := r.requireTx(ctx, tx, c, p.locks)
	if err != nil {
		return false, err
	}
	if err = r.requireRetirementRows(ctx, x, c); err != nil {
		return false, err
	}
	current, err := readRetirementBefore(ctx, x, p.request.Binding.ExecutionID)
	if err != nil {
		return false, err
	}
	if current != nil {
		if !sameRetirementBefore(current, p.before) {
			return false, fault(f.VersionConflict)
		}
		wctx, closeWitness := r.retirementWitness(ctx, tx, c, p, false)
		defer closeWitness()
		// Re-discovery only observes each owner's exact original facts. The
		// original private plans remain the mutation inputs on a later Run.
		for n, ref := range r.refs {
			currentPlan, discoverErr := ref.DiscoverProjectExecutionReferencesInTx(wctx, tx, p.request)
			if discoverErr != nil {
				return false, portError(discoverErr)
			}
			if nilPort(currentPlan) || currentPlan.ProjectExecutionRetirementReferenceDigest() != p.references[n].ProjectExecutionRetirementReferenceDigest() {
				return false, fault(f.VersionConflict)
			}
			locks, lockErr := normalizeProjectCallLocks(c.scope.ProjectID, currentPlan.RequiredLocks())
			originalLocks, originalErr := normalizeProjectCallLocks(c.scope.ProjectID, p.references[n].RequiredLocks())
			if lockErr != nil || originalErr != nil || !sameRetirementLocks(locks, originalLocks) {
				return false, fault(f.VersionConflict)
			}
			if err = ctx.Err(); err != nil {
				return false, portError(err)
			}
		}
		c.checked = true
		c.touched = true
		return false, ctx.Err()
	}
	if !p.applied || !p.commit.Matches(p.request) {
		return false, fault(f.InvalidState)
	}
	if err = requireRetirementAbsent(ctx, x, p.request.Binding.ExecutionID); err != nil {
		return false, err
	}
	wctx, closeWitness := r.retirementWitness(ctx, tx, c, p, true)
	defer closeWitness()
	for n, ref := range r.refs {
		if err = ref.RequireProjectExecutionReferencesRetiredInTx(wctx, tx, p.request, p.references[n]); err != nil {
			return false, portError(err)
		}
	}
	c.checked = true
	c.touched = true
	return true, ctx.Err()
}

func (r *ProjectExecutionRetirement) RestoreProjectExecutionRetirement(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, binding ec.ProjectExecutionRetirementBinding, commit ec.ProjectExecutionRetirementCommit, identity ec.ProjectExecutionRetirementIdentity) (ec.ProjectExecutionRetirementPlan, error) {
	c, err := r.control(ctx)
	if err != nil {
		return nil, err
	}
	if !c.actor.Equal(actor) || c.cause != cause || !c.scope.Equal(scope) || identity != r.ProjectExecutionRetirementIdentity() {
		return nil, fault(f.ResourceBusy)
	}
	p := c.plan
	if p == nil || p.request.Binding != binding || p.commit != commit || !c.confirming {
		return nil, fault(f.ResourceBusy)
	}
	return p, nil
}

var _ ec.ProjectExecutionRetirementConsumer = (*ProjectExecutionRetirement)(nil)

func sameRetirementLocks(a, b []f.LockRequest) bool {
	if len(a) != len(b) {
		return false
	}
	for n := range a {
		if a[n].Key.Canonical() != b[n].Key.Canonical() || a[n].Mode != b[n].Mode {
			return false
		}
	}
	return true
}
