package project

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Only active Run calls are retained. This local loan protects the original
// Store return; it neither authorizes SQL nor replaces the persisted claim.
type lifecycleCleanupCalls struct {
	mu     sync.Mutex
	active map[c.ProjectID]*lifecycleCleanupCall
}

type lifecycleCleanupCall struct{ project c.ProjectID }

func (a *LifecycleAuthority) beginCleanupCall(ctx context.Context, project c.ProjectID) (func(), error) {
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
	if st.active[project] != nil || len(st.active) >= 4 {
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
