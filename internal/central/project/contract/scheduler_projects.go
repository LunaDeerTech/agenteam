package contract

import (
	"context"
	"slices"
)

const MaxSchedulerProjectPageSize = 128

// SchedulerProjectPageRequest is an internal discovery position, not an Actor,
// access grant or durable traversal checkpoint. A new cycle starts with both
// positions nil; subsequent pages retain Through and use the last returned ID.
type SchedulerProjectPageRequest struct {
	After   *ProjectID
	Through *ProjectID
	Limit   int
}

func (r SchedulerProjectPageRequest) Validate() error {
	if r.Limit < 1 || r.Limit > MaxSchedulerProjectPageSize ||
		r.Through != nil && r.Through.Validate() != nil ||
		r.After != nil && (r.After.Validate() != nil || r.Through == nil || r.After.String() >= r.Through.String()) {
		return invalid("", "INVALID_SCHEDULER_PROJECT_PAGE")
	}
	return nil
}

func (r SchedulerProjectPageRequest) Clone() SchedulerProjectPageRequest {
	r.After, r.Through = cloneSchedulerProjectID(r.After), cloneSchedulerProjectID(r.Through)
	return r
}

// SchedulerProjectPage contains only initialized, active Project identities.
// Paused Projects and Projects without a CurrentSprint remain discoverable so
// their historical pending/associated Executions are not hidden. Membership
// may change between pages; a highwater bounds this cycle, not a database-wide
// immutable snapshot. Each actual Runner operation still checks current facts.
type SchedulerProjectPage struct {
	ProjectIDs []ProjectID
	Through    *ProjectID
	Complete   bool
}

func (p SchedulerProjectPage) Validate() error {
	if p.ProjectIDs == nil || len(p.ProjectIDs) > MaxSchedulerProjectPageSize ||
		p.Through != nil && p.Through.Validate() != nil ||
		p.Through == nil && (len(p.ProjectIDs) != 0 || !p.Complete) ||
		!p.Complete && len(p.ProjectIDs) == 0 {
		return invalid("", "INVALID_SCHEDULER_PROJECT_PAGE")
	}
	for n, id := range p.ProjectIDs {
		if id.Validate() != nil || p.Through == nil || id.String() > p.Through.String() ||
			n > 0 && p.ProjectIDs[n-1].String() >= id.String() {
			return invalid("", "INVALID_SCHEDULER_PROJECT_PAGE")
		}
	}
	if !p.Complete && p.ProjectIDs[len(p.ProjectIDs)-1] == *p.Through {
		return invalid("", "INVALID_SCHEDULER_PROJECT_PAGE")
	}
	return nil
}

func (p SchedulerProjectPage) ValidateFor(r SchedulerProjectPageRequest) error {
	if r.Validate() != nil || p.Validate() != nil || len(p.ProjectIDs) > r.Limit ||
		!p.Complete && len(p.ProjectIDs) != r.Limit ||
		r.Through != nil && (p.Through == nil || *p.Through != *r.Through) {
		return invalid("", "INVALID_SCHEDULER_PROJECT_PAGE")
	}
	if r.After != nil && len(p.ProjectIDs) != 0 && p.ProjectIDs[0].String() <= r.After.String() {
		return invalid("", "INVALID_SCHEDULER_PROJECT_PAGE")
	}
	return nil
}

func (p SchedulerProjectPage) Clone() SchedulerProjectPage {
	p.ProjectIDs = slices.Clone(p.ProjectIDs)
	p.Through = cloneSchedulerProjectID(p.Through)
	return p
}

func cloneSchedulerProjectID(v *ProjectID) *ProjectID {
	if v == nil {
		return nil
	}
	id := *v
	return &id
}

// SchedulerProjects is a trusted, Store-bound, read-only directory, not a
// public Human listing API. The provider owns each short transaction and
// returns no partial page on SQL/row/close/cancellation/commit errors. Consumers
// must finish a cycle before using absence to retire their own Runner, and
// must retain their actual execution/Unknown owners until those owners join.
type SchedulerProjects interface {
	ListSchedulerProjects(context.Context, SchedulerProjectPageRequest) (SchedulerProjectPage, error)
}
