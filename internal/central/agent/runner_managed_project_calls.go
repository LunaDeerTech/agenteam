package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mtc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// RunnerManagedProjectCalls is a dedicated view of the original call owner.
// It creates no business service, grants no business permission, and never
// substitutes the Runner-managed issuer for the original domain's stop issuer.
type RunnerManagedProjectCalls struct {
	original *ProjectCallOwnership
	store    Store
	guard    *object.ProcessGuard
	process  oc.ProcessID
	stop     mtc.ProjectStopAuthority
	cleanup  mtc.ProjectCleanupAuthority
	mu       sync.Mutex
	controls map[i.ProjectID]*runnerManagedObservation
}
type runnerManagedObservation struct {
	actor     i.Actor
	cause     pc.LifecycleCause
	scope     pc.ScopeRef
	active    bool
	raw       *f.CommitResult
	first     error
	before    []byte
	validated bool
	done      func()
}

func NewRunnerManagedProjectCalls(original *ProjectCallOwnership, store Store, guard *object.ProcessGuard, expected oc.ProcessID, stop mtc.ProjectStopAuthority, cleanup mtc.ProjectCleanupAuthority) (*RunnerManagedProjectCalls, error) {
	if original == nil || original.state == nil || nilPort(store) || guard == nil || expected.Validate() != nil || nilPort(stop) || nilPort(cleanup) {
		return nil, fault(f.DependencyUnbound)
	}
	r := &RunnerManagedProjectCalls{original: original, store: store, guard: guard, process: expected, stop: stop, cleanup: cleanup, controls: map[i.ProjectID]*runnerManagedObservation{}}
	if err := r.RequireBinding(store, guard, expected); err != nil {
		return nil, err
	}
	s := original.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous := s.runnerManaged; previous != nil {
		if reflect.TypeOf(previous.stop) != reflect.TypeOf(stop) || !reflect.TypeOf(stop).Comparable() || previous.stop != stop || reflect.TypeOf(previous.cleanup) != reflect.TypeOf(cleanup) || !reflect.TypeOf(cleanup).Comparable() || previous.cleanup != cleanup {
			return nil, fault(f.DependencyUnbound)
		}
		return previous, nil
	}
	s.runnerManaged = r
	return r, nil
}
func (r *RunnerManagedProjectCalls) RequireBinding(store any, guard *object.ProcessGuard, expected oc.ProcessID) error {
	if r == nil || r.original == nil || r.original.state == nil || nilPort(store) || guard == nil {
		return fault(f.DependencyUnbound)
	}
	s := r.original.state
	if reflect.TypeOf(store) != reflect.TypeOf(r.store) || !reflect.TypeOf(store).Comparable() || store != r.store || reflect.TypeOf(s.store) != reflect.TypeOf(r.store) || !reflect.TypeOf(s.store).Comparable() || s.store != r.store || s.proof != guard || r.guard != guard || s.process != expected || r.process != expected {
		return fault(f.DependencyUnbound)
	}
	return nil
}
func runnerManagedArguments(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if ctx == nil || actor.Validate() != nil || cause.Validate() != nil || cause.Action != pc.Delete || pc.RequireProjectScope(scope) != nil {
		return invalid()
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.ProjectLifecycle || d.ProjectID != scope.ProjectID.String() || d.CauseRef != cause.OperationID.String() {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}
func runnerManagedLocks(p i.ProjectID) []f.LockRequest {
	pk, _ := f.ProjectLock(p.String())
	sk, _ := f.ProjectScheduleLock(p.String())
	return []f.LockRequest{{Key: pk, Mode: f.Exclusive}, {Key: sk, Mode: f.Exclusive}}
}
func (r *RunnerManagedProjectCalls) ProjectControlJoined(p i.ProjectID) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.controls[p] == nil
}
func (r *RunnerManagedProjectCalls) Joined() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.controls) == 0
}
func (r *RunnerManagedProjectCalls) RequestProjectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return r.observe(ctx, actor, cause, scope, true)
}
func (r *RunnerManagedProjectCalls) InspectProjectStop(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (pc.StopReport, error) {
	return r.observe(ctx, actor, cause, scope, false)
}
func (r *RunnerManagedProjectCalls) observe(ctx context.Context, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, request bool) (pc.StopReport, error) {
	if r == nil || r.original == nil {
		return pc.StopReport{}, fault(f.DependencyUnbound)
	}
	if err := runnerManagedArguments(ctx, actor, cause, scope); err != nil {
		return pc.StopReport{}, err
	}
	r.mu.Lock()
	o := r.controls[scope.ProjectID]
	if o != nil && (o.active || !o.actor.Equal(actor) || o.cause != cause || !o.scope.Equal(scope)) {
		r.mu.Unlock()
		return pc.StopReport{}, fault(f.ResourceBusy)
	}
	if o == nil {
		if len(r.controls) >= 4 {
			r.mu.Unlock()
			return pc.StopReport{}, fault(f.ResourceBusy)
		}
		// The original Service also owns this control's actual return, including
		// every retained ambiguous read, across process Stop/Drain.
		owned, done, err := r.original.state.service.begin(ctx)
		if err != nil {
			r.mu.Unlock()
			return pc.StopReport{}, err
		}
		ctx = owned
		o = &runnerManagedObservation{actor: actor, cause: cause, scope: scope, done: done}
		r.controls[scope.ProjectID] = o
	}
	o.active = true
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		done := o.done
		retained := o.raw != nil
		r.mu.Unlock()
		if !retained {
			done()
		}
		r.mu.Lock()
		o.active = false
		if !retained && r.controls[scope.ProjectID] == o {
			delete(r.controls, scope.ProjectID)
		}
		r.mu.Unlock()
	}()
	txCause, _ := f.NewRecoveryCause("agent-runner-managed-stop", cause.OperationID.String(), scope.ProjectID.String())
	confirming := o.raw != nil
	if confirming {
		txCause = o.raw.Cause()
	}
	var raw []byte
	var rows []projectCallRecord
	validated := false
	var cancelOriginal func()
	result := r.store.WithinTx(ctx, txCause, func(run context.Context, tx f.Tx) error {
		if err := r.store.AcquireAll(run, tx, runnerManagedLocks(scope.ProjectID)); err != nil {
			return portError(err)
		}
		if err := r.guard.RequireCurrentProcessInTx(run, tx, r.process); err != nil {
			return portError(err)
		}
		// Consume only the caller's original Runner-managed loan. Never reborrow a
		// parent scope and never invoke the legacy Skills/Execution stop gate.
		if err := r.stop.RequireProjectStopInTx(run, tx, actor, cause, scope); err != nil {
			return portError(err)
		}
		x, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		raw, rows, err = r.snapshot(run, x, scope.ProjectID)
		if err != nil {
			return err
		}
		// This original attempt only read. Rechecking the same issuer under its
		// original cause confirms it without any write or cancellation; old call
		// rows may have legitimately progressed while the outer return was held.
		if request && !confirming {
			cancelOriginal = r.captureCancellation(scope.ProjectID)
		}
		validated = true
		return run.Err()
	})
	details := pc.StopDetails{State: pc.StopPending, SafeReason: pc.ReasonWorkPending}
	if confirming {
		if result.State() != f.Committed || !validated || ctx.Err() != nil {
			return pc.StopReport{}, o.first
		}
		r.mu.Lock()
		o.raw = nil
		r.mu.Unlock()
		// Confirmation has actually returned; this round does not cancel or claim
		// Stopped. A later explicit invocation observes current original tails.
		return pc.NewStopReport(pc.RunnerManagedParticipant, cause, scope, details)
	}
	if result.State() == f.Unknown {
		r.mu.Lock()
		saved := result
		o.raw = &saved
		o.first = commitError(result)
		o.before = bytes.Clone(raw)
		o.validated = validated
		r.mu.Unlock()
		return pc.StopReport{}, o.first
	}
	if err := commitError(result); err != nil {
		return pc.StopReport{}, err
	}
	if !validated || ctx.Err() != nil {
		return pc.StopReport{}, fault(f.ResourceBusy)
	}
	if cancelOriginal != nil {
		cancelOriginal()
	}
	if r.localReturned(scope.ProjectID) && r.rowsReturned(rows) {
		details = pc.StopDetails{State: pc.Stopped}
	}
	return pc.NewStopReport(pc.RunnerManagedParticipant, cause, scope, details)
}
func (r *RunnerManagedProjectCalls) snapshot(ctx context.Context, x postgres.SQLExecutor, p i.ProjectID) ([]byte, []projectCallRecord, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(record ORDER BY id),'[]'::jsonb) FROM (SELECT id,`+projectCallProjection+` AS record FROM agenteam_agent.project_calls WHERE project_id=$1 AND NOT(process_id=$2::uuid AND instance_id=$3::uuid AND phase='joined') ORDER BY id LIMIT 257) bounded`, p.String(), r.process.String(), r.original.state.instance.String()).Scan(&raw)
	if err != nil {
		return nil, nil, unavailable(err)
	}
	var rows []projectCallRecord
	if len(raw) > 4<<20 || json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > 256 {
		return nil, nil, fault(f.ResourceBusy)
	}
	for n, row := range rows {
		if !row.valid() || row.Project != p || n > 0 && rows[n-1].ID.String() >= row.ID.String() {
			return nil, nil, unavailable(nil)
		}
	}
	return raw, rows, nil
}
func (r *RunnerManagedProjectCalls) rowsReturned(rows []projectCallRecord) bool {
	s := r.original.state
	for _, row := range rows {
		if row.Phase != "joined" || row.Process != s.process || row.Instance != s.instance {
			return false
		}
	}
	return true
}

// This method observes complete original local tails plus durable ownership.
// It runs inside the caller's original cleanup Tx and does not write or settle
// a business Unknown. A foreign instance remains pending without its proof.
func (r *RunnerManagedProjectCalls) RequireProjectCallsReturnedInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
	if r == nil {
		return fault(f.DependencyUnbound)
	}
	if err := runnerManagedArguments(ctx, actor, cause, scope); err != nil {
		return err
	}
	if !tx.Valid() {
		return invalid()
	}
	if err := r.store.RequireHeldLocks(ctx, tx, runnerManagedLocks(scope.ProjectID)); err != nil {
		return portError(err)
	}
	if err := r.guard.RequireCurrentProcessInTx(ctx, tx, r.process); err != nil {
		return portError(err)
	}
	if err := r.cleanup.RequireProjectCleanupInTx(ctx, tx, actor, cause, scope); err != nil {
		return portError(err)
	}
	if !r.ProjectControlJoined(scope.ProjectID) || !r.localReturned(scope.ProjectID) {
		return fault(f.ResourceBusy)
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	_, rows, err := r.snapshot(ctx, x, scope.ProjectID)
	if err != nil {
		return err
	}
	if !r.rowsReturned(rows) || !r.localReturned(scope.ProjectID) {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}

func (r *RunnerManagedProjectCalls) localReturned(p i.ProjectID) bool {
	s := r.original.state
	a, b := s.service.state.calls, s.directory.state.calls
	a.mu.Lock()
	b.mu.Lock()
	s.mu.Lock()
	defer a.mu.Unlock()
	defer b.mu.Unlock()
	defer s.mu.Unlock()
	for _, owner := range s.owners {
		if owner.record.Project == p {
			return false
		}
	}
	for _, set := range []*callSet{a, b} {
		for call := range set.active {
			if call.project == p {
				return false
			}
		}
	}
	return true
}
func (r *RunnerManagedProjectCalls) captureCancellation(p i.ProjectID) func() {
	s := r.original.state
	a, b := s.service.state.calls, s.directory.state.calls
	a.mu.Lock()
	b.mu.Lock()
	var acalls, bcalls []*ownedCall
	for call := range a.active {
		if call.project == p {
			acalls = append(acalls, call)
		}
	}
	for call := range b.active {
		if call.project == p {
			bcalls = append(bcalls, call)
		}
	}
	b.mu.Unlock()
	a.mu.Unlock()
	return func() {
		a.mu.Lock()
		b.mu.Lock()
		cancelCapturedProjectCalls(a, acalls)
		cancelCapturedProjectCalls(b, bcalls)
		b.mu.Unlock()
		a.mu.Unlock()
	}
}
func (r *RunnerManagedProjectCalls) ReadRunnerManagedConfigurationInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, agent i.AgentID) (ac.AgentConfig, error) {
	if agent.Validate() != nil {
		return ac.AgentConfig{}, invalid()
	}
	if err := r.RequireProjectCallsReturnedInTx(ctx, tx, actor, cause, scope); err != nil {
		return ac.AgentConfig{}, err
	}
	if err := r.store.RequireHeldLocks(ctx, tx, []f.LockRequest{agentLock(agent, f.Exclusive)}); err != nil {
		return ac.AgentConfig{}, portError(err)
	}
	x, err := r.store.InTx(tx)
	if err != nil {
		return ac.AgentConfig{}, portError(err)
	}
	value, err := initializedAgentState(ctx, x, scope.ProjectID, agent, false)
	if err != nil {
		return ac.AgentConfig{}, err
	}
	if err = ctx.Err(); err != nil {
		return ac.AgentConfig{}, err
	}
	return value.Clone(), nil
}
