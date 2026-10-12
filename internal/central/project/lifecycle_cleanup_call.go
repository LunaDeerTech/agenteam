package project

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Active Run calls and the bounded original finalizer rounds share admission.
// The latter retain an ambiguous registration even if it rolled back to the
// preceding terminal claim. Neither entry authorizes SQL or replaces a claim.
type lifecycleCleanupCalls struct {
	mu           sync.Mutex
	active       map[c.ProjectID]*lifecycleCleanupCall
	finals       map[c.ProjectID]*lifecycleFinalizeRound
	executions   map[c.ProjectID]*executionCleanupRound
	projectCalls map[*call]c.ProjectID
}

type lifecycleCleanupCall struct{ project c.ProjectID }

func (a *LifecycleAuthority) beginCleanupCall(ctx context.Context, project c.ProjectID, final ...*lifecycleFinalizeRound) (func(), error) {
	if len(final) > 1 {
		return nil, invalid()
	}
	var permitted *lifecycleFinalizeRound
	if len(final) == 1 {
		permitted = final[0]
	}
	return a.beginCleanupOwnedCall(ctx, project, permitted, nil)
}

func (a *LifecycleAuthority) beginCleanupOwnedCall(ctx context.Context, project c.ProjectID, permitted *lifecycleFinalizeRound, execution *executionCleanupRound) (func(), error) {
	if !a.bound() {
		return nil, fault(f.DependencyUnbound)
	}
	if ctx == nil || project.Validate() != nil {
		return nil, invalid()
	}
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	st := &a.cleanupCalls
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	if st.finals[project] != nil && st.finals[project] != permitted || st.executions[project] != nil && st.executions[project] != execution {
		return nil, fault(f.ResourceBusy)
	}
	count := len(st.finals)
	for p := range st.executions {
		if st.finals[p] == nil {
			count++
		}
	}
	for p := range st.active {
		if st.finals[p] == nil && st.executions[p] == nil {
			count++
		}
	}
	if st.active[project] != nil || st.finals[project] == nil && st.executions[project] == nil && count >= 4 {
		return nil, fault(f.ResourceBusy)
	}
	if st.active == nil {
		st.active = make(map[c.ProjectID]*lifecycleCleanupCall)
	}
	call := &lifecycleCleanupCall{project: project}
	st.active[project] = call
	return func() {
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.active[project] == call {
			delete(st.active, project)
		}
	}, nil
}

func (a *LifecycleAuthority) retainExecutionCleanup(round *executionCleanupRound) error {
	st := &a.cleanupCalls
	st.mu.Lock()
	defer st.mu.Unlock()
	if round == nil || st.active[round.round.claim.project] == nil || st.executions[round.round.claim.project] != nil && st.executions[round.round.claim.project] != round {
		return fault(f.ResourceBusy)
	}
	if st.executions == nil {
		st.executions = make(map[c.ProjectID]*executionCleanupRound)
	}
	st.executions[round.round.claim.project] = round
	return nil
}

func (a *LifecycleAuthority) releaseExecutionCleanup(round *executionCleanupRound) {
	if round == nil {
		return
	}
	st := &a.cleanupCalls
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.executions[round.round.claim.project] == round {
		delete(st.executions, round.round.claim.project)
	}
}

func (a *LifecycleAuthority) retainFinalize(round *lifecycleFinalizeRound) error {
	st := &a.cleanupCalls
	st.mu.Lock()
	defer st.mu.Unlock()
	if round == nil || st.active[round.claim.project] == nil || st.finals[round.claim.project] != nil && st.finals[round.claim.project] != round {
		return fault(f.ResourceBusy)
	}
	for _, project := range st.projectCalls {
		if project == round.claim.project {
			return fault(f.ResourceBusy)
		}
	}
	if st.finals == nil {
		st.finals = make(map[c.ProjectID]*lifecycleFinalizeRound)
	}
	st.finals[round.claim.project] = round
	return nil
}

func (a *LifecycleAuthority) releaseFinalize(round *lifecycleFinalizeRound) {
	if round == nil {
		return
	}
	st := &a.cleanupCalls
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.finals[round.claim.project] == round {
		delete(st.finals, round.claim.project)
	}
}
