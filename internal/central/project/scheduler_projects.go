package project

import (
	"context"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// SchedulerProjects is an internal identity directory. It deliberately has no
// Actor/Owner API, worker or business mutation capability. Its SQL observations
// are not authority to launch, resume, stop or delete a Project's work.
type SchedulerProjects struct{ authority *Authority }

// NewSchedulerProjects binds the original Project authority and its Store;
// construction performs no I/O and registers no background lifetime.
func NewSchedulerProjects(authority *Authority) (*SchedulerProjects, error) {
	if authority.state() == nil || nilPort(authority.state().store) {
		return nil, fault(f.DependencyUnbound)
	}
	return &SchedulerProjects{authority: authority}, nil
}

func (r *SchedulerProjects) ListSchedulerProjects(ctx context.Context, request c.SchedulerProjectPageRequest) (c.SchedulerProjectPage, error) {
	if err := ctx.Err(); err != nil {
		return c.SchedulerProjectPage{}, err
	}
	if r == nil || r.authority.state() == nil || nilPort(r.authority.state().store) {
		return c.SchedulerProjectPage{}, fault(f.DependencyUnbound)
	}
	request = request.Clone()
	if err := request.Validate(); err != nil {
		return c.SchedulerProjectPage{}, err
	}
	cause, err := readCause("scheduler-projects")
	if err != nil {
		return c.SchedulerProjectPage{}, err
	}
	store := r.authority.state().store
	var page c.SchedulerProjectPage
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		x, err := store.InTx(tx)
		if err != nil {
			return unavailable(err)
		}
		position := request.Clone()
		if position.Through == nil {
			var raw string
			err := x.QueryRow(ctx, `SELECT id::text FROM agenteam_project.projects WHERE initialized_at IS NOT NULL AND lifecycle='active' ORDER BY id DESC LIMIT 1`).Scan(&raw)
			if errors.Is(err, pgx.ErrNoRows) {
				page = c.SchedulerProjectPage{ProjectIDs: []c.ProjectID{}, Complete: true}
				return ctx.Err()
			}
			if err != nil {
				return unavailable(err)
			}
			through, err := parseID[i.Project](raw)
			if err != nil {
				return err
			}
			position.Through = &through
		}
		var after any
		if position.After != nil {
			after = position.After.String()
		}
		// These are only candidate identities. There is deliberately no launch
		// gate/Project lock, scheduler-enabled filter or CurrentSprint filter.
		// The Runner revalidates each Project under its original complete locks.
		rows, err := x.Query(ctx, `SELECT id::text,lifecycle,initialized_at IS NOT NULL FROM agenteam_project.projects WHERE initialized_at IS NOT NULL AND lifecycle='active' AND ($1::uuid IS NULL OR id>$1::uuid) AND id<=$2::uuid ORDER BY id ASC LIMIT $3`, after, position.Through.String(), position.Limit+1)
		if err != nil {
			return unavailable(err)
		}
		if rows == nil {
			return unavailable(nil)
		}
		page, err = readSchedulerProjectRows(ctx, rows, position)
		return err
	})
	// A physical Unknown retains the original attempt even if cancellation
	// arrived after the callback. No page or inferred completion escapes it.
	if err := commitError(result); err != nil {
		return c.SchedulerProjectPage{}, err
	}
	if err := ctx.Err(); err != nil {
		return c.SchedulerProjectPage{}, err
	}
	if err := page.ValidateFor(request); err != nil {
		return c.SchedulerProjectPage{}, unavailable(err)
	}
	return page.Clone(), nil
}

// Reuse the ordinary reader's private iterator boundary so controlled row
// tests can cover late Close errors without replacing Store/Project authority.
func readSchedulerProjectRows(ctx context.Context, rows projectReadRows, position c.SchedulerProjectPageRequest) (c.SchedulerProjectPage, error) {
	closed := false
	closeRows := func() {
		if !closed {
			closed = true
			rows.Close()
		}
	}
	defer closeRows()
	page := c.SchedulerProjectPage{ProjectIDs: []c.ProjectID{}, Through: position.Clone().Through, Complete: true}
	if position.Validate() != nil || position.Through == nil {
		return c.SchedulerProjectPage{}, unavailable(nil)
	}
	var previous string
	count := 0
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return c.SchedulerProjectPage{}, err
		}
		var raw, lifecycle string
		var initialized bool
		if err := rows.Scan(&raw, &lifecycle, &initialized); err != nil {
			return c.SchedulerProjectPage{}, unavailable(err)
		}
		id, err := parseID[i.Project](raw)
		if err != nil {
			return c.SchedulerProjectPage{}, err
		}
		count++
		if lifecycle != string(c.Active) || !initialized || count > position.Limit+1 ||
			previous != "" && raw <= previous || raw > position.Through.String() ||
			position.After != nil && raw <= position.After.String() {
			return c.SchedulerProjectPage{}, unavailable(nil)
		}
		previous = raw
		if count <= position.Limit {
			page.ProjectIDs = append(page.ProjectIDs, id)
		} else {
			page.Complete = false
		}
	}
	closeRows()
	if err := rows.Err(); err != nil {
		return c.SchedulerProjectPage{}, unavailable(err)
	}
	if err := ctx.Err(); err != nil {
		return c.SchedulerProjectPage{}, err
	}
	if err := page.ValidateFor(position); err != nil {
		return c.SchedulerProjectPage{}, unavailable(err)
	}
	return page, nil
}

var _ c.SchedulerProjects = (*SchedulerProjects)(nil)
