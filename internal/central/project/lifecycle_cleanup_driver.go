package project

import (
	"context"
	"sync"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// LifecycleCleanupDriver owns one synchronous cleanup round. The original
// constructor binds Outbox only; the explicit WithAudit constructor also binds
// Audit. Every earlier domain must already be canonically completed. Neither
// constructor supplies a complete registry or performs Project deletion. A nil
// Run result can also mean a persisted pending round. ForSkills instead binds
// only one domain substep and always leaves its combined participant pending.
type LifecycleCleanupDriver struct{ state *lifecycleCleanupState }

type lifecycleCleanupOwner struct {
	project c.ProjectID
	claim   *lifecycleStopClaim // retained only after its original Run returned
	prior   *lifecycleStopClaim // an Unknown replacement may have kept this row
}

type lifecycleCleanupState struct {
	store        Store
	authority    *LifecycleAuthority
	processes    oc.ProcessAuthority
	process      oc.ProcessID
	participant  c.ProjectLifecycleParticipant
	audit        c.ProjectLifecycleParticipant
	skills       SkillsCleanupDomain
	agents       ac.ProjectRetirementConsumer
	variables    pvc.ProjectCleanupDomain
	registration identity.ServiceRegistration
	mu           sync.Mutex
	stopped      bool
	calls        map[c.ProjectID]context.CancelFunc
	owners       map[c.OperationID]lifecycleCleanupOwner
	changed      chan struct{}
}

func NewLifecycleCleanupDriver(store Store, authority *LifecycleAuthority, processes oc.ProcessAuthority, participant c.ProjectLifecycleParticipant) (*LifecycleCleanupDriver, error) {
	if nilPort(store) || !authority.bound() || !sameStore(store, authority.store) || nilPort(processes) || nilPort(participant) {
		return nil, fault(f.DependencyUnbound)
	}
	process := processes.CurrentProcess()
	if process.Validate() != nil {
		return nil, invalid()
	}
	if name, err := cleanupParticipantName(participant); err != nil || name != c.OutboxParticipant {
		return nil, fault(f.DependencyUnbound)
	}
	registration, err := identity.RegisterService(identity.ProjectLifecycle)
	if err != nil {
		return nil, portError(err)
	}
	return &LifecycleCleanupDriver{state: &lifecycleCleanupState{store: store, authority: authority, processes: processes, process: process, participant: participant, registration: registration, calls: map[c.ProjectID]context.CancelFunc{}, owners: map[c.OperationID]lifecycleCleanupOwner{}, changed: make(chan struct{})}}, nil
}

func cleanupParticipantName(p c.ProjectLifecycleParticipant) (name c.ParticipantName, err error) {
	defer func() {
		if recover() != nil {
			name, err = "", unavailable(nil)
		}
	}()
	return p.Name(), nil
}

func (d *LifecycleCleanupDriver) Run(ctx context.Context, project c.ProjectID, operation c.OperationID) error {
	if ctx == nil || project.Validate() != nil || operation.Validate() != nil {
		return invalid()
	}
	if d == nil || d.state == nil {
		return fault(f.DependencyUnbound)
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	st := d.state
	st.mu.Lock()
	if st.stopped {
		st.mu.Unlock()
		return fault(f.ShuttingDown)
	}
	old, owned := st.owners[operation]
	_, busy := st.calls[project]
	if busy || owned && old.project != project || !owned && len(st.owners) >= 4 {
		st.mu.Unlock()
		return fault(f.ResourceBusy)
	}
	run, cancel := context.WithCancel(ctx)
	st.calls[project] = cancel
	if !owned {
		st.owners[operation] = lifecycleCleanupOwner{project: project}
	}
	st.mu.Unlock()
	var round *lifecycleCleanupRound
	settled := false
	defer func() {
		cancel()
		st.mu.Lock()
		if settled || round == nil && !owned {
			delete(st.owners, operation)
		} else if round != nil {
			claim := round.claim
			st.owners[operation] = lifecycleCleanupOwner{project: project, claim: &claim, prior: round.previous}
		}
		delete(st.calls, project)
		close(st.changed)
		st.changed = make(chan struct{})
		st.mu.Unlock()
	}()
	var replay bool
	var err error
	round, replay, err = st.start(run, project, operation)
	if err != nil {
		return err
	}
	if replay {
		settled = true
		return nil
	}
	if round.agentStep {
		settled, err = st.runAgentStep(run, *round)
		return err
	}
	report, callErr := st.invoke(run, *round)
	// The original adapter (including all domain calls) has returned. Cancellation
	// cannot substitute for that return or detach this bounded checkpoint.
	checkpoint, end := context.WithTimeout(context.WithoutCancel(run), 3*time.Second)
	finishErr := st.finish(checkpoint, *round, report, callErr)
	end()
	settled = finishErr == nil
	return lifecycleStopRoundError(finishErr, callErr)
}

type lifecycleCleanupScopeKey struct{}
type lifecycleCleanupScope struct {
	state *lifecycleCleanupState
	round lifecycleCleanupRound
	mu    sync.Mutex
	open  bool
}

func (st *lifecycleCleanupState) invoke(ctx context.Context, round lifecycleCleanupRound) (report c.CleanupReport, err error) {
	if err = ctx.Err(); err != nil {
		return report, portError(err)
	}
	participant, err := st.cleanupPort(round.participant)
	if err != nil {
		return report, err
	}
	if err = validateCleanupCheckpoint(round.participant, round.checkpoint, round.cause, round.claim.project); err != nil {
		return report, err
	}
	checkpoint := round.checkpoint
	if round.participant == c.SkillsParticipant {
		if round.variablesStep {
			if nilPort(st.variables) || round.agentStep {
				return report, fault(f.Forbidden)
			}
			progress, _, e := decodeVariablesCleanupCheckpoint(checkpoint, round.cause, round.claim.project)
			if e != nil {
				return report, e
			}
			if progress.State == c.CleanupCompleted {
				return report, fault(f.InvalidState)
			}
			participant, checkpoint = st.variables, nil
		} else {
			var progress skillsCleanupCheckpoint
			progress, checkpoint, err = st.skillsProgress(checkpoint, round.cause, round.claim.project)
			if err != nil {
				return report, err
			}
			if progress.State == c.CleanupCompleted {
				return report, fault(f.InvalidState)
			}
		}
	}
	actorScope, err := identity.InProject(round.claim.project)
	if err != nil {
		return report, invalid()
	}
	actor, err := st.registration.Actor(round.claim.operation.String(), actorScope)
	if err != nil {
		return report, invalid()
	}
	ctx, end := context.WithTimeout(ctx, 2*time.Second)
	defer end()
	witness := &lifecycleCleanupScope{state: st, round: round, open: true}
	ctx = context.WithValue(ctx, lifecycleCleanupScopeKey{}, witness)
	defer func() {
		witness.mu.Lock()
		witness.open = false
		witness.mu.Unlock()
		if recover() != nil {
			report, err = c.CleanupReport{}, unavailable(nil)
		}
	}()
	report, err = participant.Cleanup(ctx, actor, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, checkpoint)
	if err != nil {
		return c.CleanupReport{}, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return c.CleanupReport{}, portError(err)
	}
	if round.participant == c.SkillsParticipant {
		if round.variablesStep {
			report, err = variablesParentCleanupReport(round, report)
		} else {
			report, err = skillsParentCleanupReport(round, report)
			if err == nil && st.agents != nil {
				report, err = agentsSkillsCleanupReport(round, report)
			}
		}
		if err != nil {
			return c.CleanupReport{}, err
		}
	}
	if err = cleanupRoundReport(round, report); err != nil {
		return c.CleanupReport{}, err
	}
	return report, nil
}

func (a *LifecycleAuthority) requireOutboxCleanupClaimInTx(ctx context.Context, tx f.Tx, cause c.LifecycleCause, project c.ProjectID) error {
	return a.requireCleanupClaimInTx(ctx, tx, cause, project, c.OutboxParticipant)
}

func (a *LifecycleAuthority) requireCleanupClaimInTx(ctx context.Context, tx f.Tx, cause c.LifecycleCause, project c.ProjectID, participant c.ParticipantName) error {
	if ctx == nil || cause.Validate() != nil || project.Validate() != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return portError(err)
	}
	witness, _ := ctx.Value(lifecycleCleanupScopeKey{}).(*lifecycleCleanupScope)
	if witness == nil || witness.state == nil || witness.state.authority != a || witness.round.cause != cause || witness.round.claim.project != project || witness.round.participant != participant {
		return fault(f.Forbidden)
	}
	if _, err := witness.state.cleanupPort(participant); err != nil {
		return err
	}
	// Holding this mutex through the original SQL return prevents scope closure
	// from racing an already admitted exact-claim check. No detached work begins.
	witness.mu.Lock()
	defer witness.mu.Unlock()
	if !witness.open {
		return fault(f.Forbidden)
	}
	x, err := a.lifecycleExecutor(ctx, tx, project)
	if err != nil {
		return err
	}
	actual, err := loadLifecycleStopClaim(ctx, x, cause.OperationID)
	if err != nil {
		return err
	}
	if actual == nil || *actual != witness.round.claim || actual.phase != "running" {
		return fault(f.ResourceBusy)
	}
	return portError(ctx.Err())
}

func (d *LifecycleCleanupDriver) Stop() {
	if d == nil || d.state == nil {
		return
	}
	st := d.state
	st.mu.Lock()
	st.stopped = true
	for _, cancel := range st.calls {
		cancel()
	}
	st.mu.Unlock()
}

// Drain waits for this driver's actual calls and their checkpoints, not for
// the persisted operation or all Project owners to become complete. Unknown
// claims remain owned and bounded even after their original calls returned.
func (d *LifecycleCleanupDriver) Drain(ctx context.Context) error {
	if ctx == nil {
		return invalid()
	}
	if d == nil || d.state == nil {
		return nil
	}
	st := d.state
	for {
		st.mu.Lock()
		if len(st.calls) == 0 {
			st.mu.Unlock()
			return nil
		}
		changed := st.changed
		st.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
