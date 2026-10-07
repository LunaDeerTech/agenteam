package adapter

import (
	"context"
	"sync"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// budgetWork identifies one exact admitted handle across supported wire types.
// Only actual waitJoined completion may remove it from the shared registry.
type budgetWork interface {
	cancelWork()
	waitJoined(context.Context) error
	Joined() bool
}

type budgetState struct {
	mu       sync.Mutex
	stopped  bool
	active   map[budgetWork]identity.ProjectID
	projects map[identity.ProjectID]int
}
type Budget struct{ state *budgetState }

func NewBudget() *Budget {
	return &Budget{state: &budgetState{active: make(map[budgetWork]identity.ProjectID), projects: make(map[identity.ProjectID]int)}}
}
func (b *Budget) accept(ctx context.Context, project identity.ProjectID, mode ResponseMode, overall time.Duration) (*Exchange, error) {
	var x *Exchange
	err := b.admit(ctx, project, func() budgetWork {
		x = newExchange(ctx, mode, overall, b)
		return x
	})
	return x, err
}
func (b *Budget) admit(ctx context.Context, project identity.ProjectID, create func() budgetWork) error {
	if b == nil || b.state == nil || project.Validate() != nil {
		return invalid()
	}
	s := b.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return contextFailure(err)
	}
	if s.stopped {
		return f.NewFault(f.ShuttingDown, f.NotStarted)
	}
	if len(s.active) >= 64 || s.projects[project] >= 8 {
		return f.NewFault(f.ResourceBusy, f.NotStarted)
	}
	x := create()
	s.active[x] = project
	s.projects[project]++
	return nil
}
func (b *Budget) release(x budgetWork) {
	s := b.state
	s.mu.Lock()
	defer s.mu.Unlock()
	if project, ok := s.active[x]; ok {
		delete(s.active, x)
		s.projects[project]--
		if s.projects[project] == 0 {
			delete(s.projects, project)
		}
	}
}
func (b *Budget) StopAdmission() {
	if b == nil || b.state == nil {
		return
	}
	b.state.mu.Lock()
	b.state.stopped = true
	b.state.mu.Unlock()
}
func (b *Budget) snapshot() ([]budgetWork, bool) {
	if b == nil || b.state == nil {
		return nil, false
	}
	s := b.state
	s.mu.Lock()
	defer s.mu.Unlock()
	list := make([]budgetWork, 0, len(s.active))
	for x := range s.active {
		list = append(list, x)
	}
	return list, s.stopped
}
func (b *Budget) Drain(ctx context.Context) error {
	if b == nil || b.state == nil || ctx == nil {
		return invalid()
	}
	for {
		list, _ := b.snapshot()
		if len(list) == 0 {
			return nil
		}
		for _, x := range list {
			if err := x.waitJoined(ctx); err != nil {
				return err
			}
		}
	}
}
func (b *Budget) Force(ctx context.Context) error {
	if b == nil || b.state == nil || ctx == nil {
		return invalid()
	}
	b.StopAdmission()
	list, _ := b.snapshot()
	for _, x := range list {
		x.cancelWork()
	}
	return b.Drain(ctx)
}
func (b *Budget) Joined() bool {
	list, stopped := b.snapshot()
	if !stopped {
		return false
	}
	for _, x := range list {
		if !x.Joined() {
			return false
		}
	}
	list, stopped = b.snapshot()
	return stopped && len(list) == 0
}
