package execution

import (
	"bytes"
	"context"
	"slices"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
)

type directTextContextKey struct{}

// The context carries an actual admitted owner, never a public receipt. Only
// this package constructs it. Its registry membership outlives uncertain
// commits and is removed only after the original Loop and cleanup return.
type directTextCall struct {
	owner                                   *directTextState
	cancel                                  context.CancelFunc
	ctx                                     context.Context
	request                                 c.PreparationRequest
	input                                   *preparationInputRecord
	snapshot                                c.DirectTextSnapshot
	round                                   c.DirectTextRound
	summary                                 c.Summary
	event                                   event.Event
	runtimeLocks                            []f.LockRequest
	locks                                   []f.LockRequest
	phase                                   string
	tx                                      f.Tx
	started, allJoined, returned, resolving bool
	retired                                 bool
	unresolved                              error
	uncertainty                             string
	invocation                              *directTextInvocation
	terminal                                *directTextTerminal
}

type directTextState struct {
	store     Store
	authority *Authority
	processes oc.ProcessAuthority
	process   oc.ProcessID
	deps      DirectTextDependencies
	mu        sync.Mutex
	stopped   bool
	calls     map[i.ExecutionID]*directTextCall
	changed   chan struct{}
}

// These copies are facts for the Model-owned matcher. They are not a grant
// that can be replayed without the original context and live transaction.
type directTextModelFacts struct {
	Snapshot  c.DirectTextSnapshot
	Round     c.DirectTextRound
	Summary   c.Summary
	ProcessID oc.ProcessID
	Locks     []f.LockRequest
}

// Planning returns original candidate facts, not a substitute for the final
// same-transaction canonical read. No public DTO can establish this owner.
func (a *Authority) directTextModelPlanningScope(ctx context.Context, retirement bool) (directTextModelFacts, error) {
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return directTextModelFacts{}, err
	}
	s := run.owner
	s.mu.Lock()
	defer s.mu.Unlock()
	valid := run.started && (run.phase == "calling" || run.phase == "closing")
	if retirement {
		valid = run.started && run.allJoined && run.terminal != nil && run.phase == "closing"
	}
	if !valid {
		return directTextModelFacts{}, fault(f.Forbidden)
	}
	return directTextModelFacts{run.snapshot, run.round, run.summary.Clone(), s.process, slices.Clone(run.runtimeLocks)}, ctx.Err()
}

func directTextRuntimeLocks(request c.PreparationRequest) ([]f.LockRequest, error) {
	locks := []f.LockRequest{projectLock(request.Launch.ProjectID), agentLock(request.Launch.AgentID, f.Shared), executionLock(request.ExecutionID)}
	if request.Launch.Trigger.Kind == "task" {
		key, err := f.ProjectScheduleLock(request.Launch.ProjectID.String())
		if err != nil {
			return nil, err
		}
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Exclusive})
	}
	return oc.NormalizeLocks(locks)
}

func (a *Authority) directTextOwner(ctx context.Context) (*directTextCall, error) {
	if a == nil || a.state == nil {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, invalid()
	}
	run, ok := ctx.Value(directTextContextKey{}).(*directTextCall)
	if !ok || run == nil || run.owner == nil {
		return nil, fault(f.Forbidden)
	}
	s := run.owner
	s.mu.Lock()
	valid := s.authority != nil && s.authority.state == a.state && s.store == a.state.store && s.calls[run.request.ExecutionID] == run
	s.mu.Unlock()
	if !valid || nilPort(s.processes) || s.processes.CurrentProcess() != s.process {
		return nil, fault(f.Forbidden)
	}
	return run, nil
}

func (a *Authority) directTextModelScope(ctx context.Context, tx f.Tx, allowClosing bool) (directTextModelFacts, error) {
	var zero directTextModelFacts
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return zero, err
	}
	s := run.owner
	s.mu.Lock()
	ready := run.started && (run.phase == "calling" || allowClosing && run.phase == "closing")
	locks := slices.Clone(run.runtimeLocks)
	snapshot, round := run.snapshot, run.round
	s.mu.Unlock()
	if !ready {
		return zero, fault(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return zero, portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return zero, portError(err)
	}
	row, err := loadExecution(ctx, x, run.request.ExecutionID)
	if err != nil {
		return zero, err
	}
	stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
	if err != nil {
		return zero, err
	}
	if row == nil || stored == nil || !((c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}).Equal(run.request)) || row.summary.Status != c.Running || row.summary.SnapshotID == nil || *row.summary.SnapshotID != snapshot.Fields().ID || !sameDirectText(stored, snapshot, round) {
		return zero, fault(f.ConfirmationStale)
	}
	if !allowClosing && row.summary.CancelRequestedAt != nil {
		return zero, fault(f.InvalidState)
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return directTextModelFacts{snapshot, round, row.summary.Clone(), s.process, locks}, nil
}

// Startup's Current Agent read has its own exact original-Tx proof. It does
// not make preparing usable by Model Invoke or credential reads.
func (a *Authority) directTextConfigurationScope(ctx context.Context, tx f.Tx) (directTextModelFacts, error) {
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return directTextModelFacts{}, err
	}
	s := run.owner
	s.mu.Lock()
	starting := run.phase == "starting" && run.tx == tx && !run.started
	locks := slices.Clone(run.locks)
	s.mu.Unlock()
	if !starting {
		return a.directTextModelScope(ctx, tx, false)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return directTextModelFacts{}, portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return directTextModelFacts{}, portError(err)
	}
	row, err := loadExecution(ctx, x, run.request.ExecutionID)
	if err != nil {
		return directTextModelFacts{}, err
	}
	input, err := loadPreparationInput(ctx, x, run.request.ExecutionID)
	if err != nil {
		return directTextModelFacts{}, err
	}
	stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
	if err != nil {
		return directTextModelFacts{}, err
	}
	if row == nil || row.summary.Status != c.Running || row.summary.CancelRequestedAt != nil || !((c.PreparationRequest{ExecutionID: row.summary.ID, Launch: row.launch}).Equal(run.request)) || !samePreparationInput(input, run.input) || !sameDirectText(stored, run.snapshot, run.round) {
		return directTextModelFacts{}, fault(f.ConfirmationStale)
	}
	return directTextModelFacts{run.snapshot, run.round, row.summary.Clone(), s.process, locks}, ctx.Err()
}

func (a *Authority) directTextCurrentAgent(ctx context.Context, tx f.Tx) (ac.AgentConfig, error) {
	facts, err := a.directTextModelScope(ctx, tx, false)
	if err != nil {
		return ac.AgentConfig{}, err
	}
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return ac.AgentConfig{}, err
	}
	if _, err = run.owner.deps.Projects.RequirePreparingProjectInTx(ctx, tx, facts.Summary.ProjectID); err != nil {
		return ac.AgentConfig{}, portError(err)
	}
	actor, err := i.NewAgentRun(facts.Summary.ProjectID, facts.Summary.AgentID, facts.Summary.ID)
	if err != nil {
		return ac.AgentConfig{}, err
	}
	return run.owner.deps.Agents.ReadExecutionConfigurationInTx(ctx, tx, ac.ExecutionConfigurationRequest{Actor: actor, ProjectID: facts.Summary.ProjectID, AgentID: facts.Summary.AgentID, ExecutionID: facts.Summary.ID, Stage: ac.ExecutionConfigurationCurrent})
}

// Lease retirement is stronger than allowClosing: the original concrete Loop
// has joined, and this exact transaction has already written the candidate
// terminal. It remains tentative until both domain retirements and the event
// commit with it. A terminal DTO or cancelled context cannot mint this proof.
func (a *Authority) directTextRetirementScope(ctx context.Context, tx f.Tx) (directTextModelFacts, error) {
	var zero directTextModelFacts
	run, err := a.directTextOwner(ctx)
	if err != nil {
		return zero, err
	}
	s := run.owner
	s.mu.Lock()
	valid := run.started && run.allJoined && run.phase == "terminal" && run.tx == tx && run.terminal != nil
	locks := slices.Clone(run.locks)
	terminal := run.terminal
	s.mu.Unlock()
	if !valid {
		return zero, fault(f.Forbidden)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return zero, portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return zero, portError(err)
	}
	row, err := loadExecution(ctx, x, run.request.ExecutionID)
	if err != nil {
		return zero, err
	}
	stored, err := loadDirectText(ctx, x, run.request.ExecutionID)
	if err != nil {
		return zero, err
	}
	if row == nil || stored == nil || !sameDirectText(stored, run.snapshot, run.round) || !directTextTerminalMatches(row, stored, terminal) {
		return zero, fault(f.ConfirmationStale)
	}
	return directTextModelFacts{run.snapshot, run.round, row.summary.Clone(), s.process, locks}, ctx.Err()
}

func sameDirectText(row *directTextRecord, snapshot c.DirectTextSnapshot, round c.DirectTextRound) bool {
	return row != nil && row.snapshot.Digest() == snapshot.Digest() && row.round.Digest() == round.Digest() && bytes.Equal(row.snapshot.CanonicalBytes(), snapshot.CanonicalBytes()) && bytes.Equal(row.round.CanonicalBytes(), round.CanonicalBytes())
}
