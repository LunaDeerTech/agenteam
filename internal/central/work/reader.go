package work

import (
	"context"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type Reader struct{ data func() *readerState }
type readerState struct {
	store     Store
	authority *Authority
	cursors   cursor.Keyring
}

func NewReader(store Store, authority *Authority, keys cursor.Keyring) (*Reader, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) {
		return nil, fault(f.DependencyUnbound)
	}
	if keys.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	st := &readerState{store: store, authority: authority, cursors: keys}
	return &Reader{data: func() *readerState { return st }}, nil
}
func (r *Reader) state() *readerState {
	if r == nil || r.data == nil {
		return nil
	}
	return r.data()
}
func readInput(ctx context.Context, actor i.Actor, project c.ProjectID) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
	}
	if err := c.ValidateActor(actor); err != nil {
		return err
	}
	if project.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	return nil
}
func (r *Reader) read(ctx context.Context, actor i.Actor, project c.ProjectID, extra []f.LockRequest, fn func(context.Context, postgres.SQLExecutor, pc.ProjectRef) error) error {
	if err := readInput(ctx, actor, project); err != nil {
		return err
	}
	st := r.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	cause, err := readCause("read")
	if err != nil {
		return err
	}
	locks, err := oc.NormalizeLocks(append([]f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared)}, extra...))
	if err != nil {
		return portError(err)
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		access, err := st.authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		ref := access.Project()
		if ref.ID != project {
			return internal(nil)
		}
		if err = checkPointer(ctx, x, ref); err != nil {
			return err
		}
		return fn(ctx, x, ref)
	})
	return txError(result)
}
func (r *Reader) GetMilestone(ctx context.Context, actor i.Actor, project c.ProjectID, id c.MilestoneID) (c.Milestone, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return c.Milestone{}, err
	}
	if id.Validate() != nil {
		return c.Milestone{}, fault(f.InvalidArgument)
	}
	var out c.Milestone
	err := r.read(ctx, actor, project, nil, func(ctx context.Context, x postgres.SQLExecutor, _ pc.ProjectRef) error {
		v, err := loadMilestone(ctx, x, project, id)
		out = v
		return err
	})
	if err != nil {
		return c.Milestone{}, err
	}
	return out, nil
}
func (r *Reader) GetSprint(ctx context.Context, actor i.Actor, project c.ProjectID, id c.SprintID) (c.Sprint, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return c.Sprint{}, err
	}
	if id.Validate() != nil {
		return c.Sprint{}, fault(f.InvalidArgument)
	}
	var out c.Sprint
	err := r.read(ctx, actor, project, []f.LockRequest{sprintLock(id.String(), f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor, ref pc.ProjectRef) error {
		v, err := loadSprint(ctx, x, project, id, ref.CurrentSprintID)
		if err != nil {
			return err
		}
		if _, err = loadMilestone(ctx, x, project, v.MilestoneID); err != nil {
			if isNotFound(err) {
				return internal(err)
			}
			return err
		}
		out = v
		return nil
	})
	if err != nil {
		return c.Sprint{}, err
	}
	return out.Clone(), nil
}
func listBinding(project c.ProjectID, user string, parent *c.MilestoneID) (cursor.Binding, error) {
	kind := "work.milestones"
	if parent != nil {
		kind = "work.sprints"
	}
	raw, err := json.Marshal(struct {
		Format  int            `json:"format"`
		Kind    string         `json:"kind"`
		Project c.ProjectID    `json:"project_id"`
		User    string         `json:"owner_user_id"`
		Parent  *c.MilestoneID `json:"milestone_id"`
	}{1, kind, project, user, parent})
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	d, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		return cursor.Binding{}, portError(err)
	}
	return cursor.Binding{Scope: scope, QueryDigest: d, Order: "manual_rank:asc,id:asc"}, nil
}
func pageAfter(keys cursor.Keyring, token string, binding cursor.Binding, generation f.Version) (string, string, error) {
	if token == "" {
		return "", "", nil
	}
	position, err := keys.Verify(token, binding)
	if err != nil {
		return "", "", err
	}
	if len(position.Scalars) != 2 || position.Scalars[0].Kind() != "text" || position.Scalars[1].Kind() != "uuid" || c.ValidateRank(position.Scalars[0].Value()) != nil || position.OrderGeneration == nil || *position.OrderGeneration < 1 {
		return "", "", fault(f.CursorInvalid)
	}
	if *position.OrderGeneration != int64(generation) {
		return "", "", fault(f.CursorStale)
	}
	return position.Scalars[0].Value(), position.Scalars[1].Value(), nil
}
func pageToken(keys cursor.Keyring, binding cursor.Binding, generation f.Version, rank, id string) (string, error) {
	text, err := cursor.Text(rank)
	if err != nil {
		return "", err
	}
	uuid, err := cursor.UUID(id)
	if err != nil {
		return "", err
	}
	g := int64(generation)
	return keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{text, uuid}, OrderGeneration: &g})
}
func (r *Reader) ListMilestones(ctx context.Context, actor i.Actor, project c.ProjectID, page f.PageRequest) (f.Page[c.Milestone], error) {
	if err := readInput(ctx, actor, project); err != nil {
		return f.Page[c.Milestone]{}, err
	}
	if page.Validate() != nil {
		return f.Page[c.Milestone]{}, fault(f.InvalidArgument)
	}
	out := f.Page[c.Milestone]{Items: []c.Milestone{}}
	err := r.read(ctx, actor, project, []f.LockRequest{rankLock(project, "", f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor, _ pc.ProjectRef) error {
		generation, err := loadGeneration(ctx, x, project, "")
		if err != nil {
			return err
		}
		binding, err := listBinding(project, actor.Details().UserID, nil)
		if err != nil {
			return err
		}
		rank, id, err := pageAfter(r.state().cursors, page.Cursor, binding, generation)
		if err != nil {
			return err
		}
		query := `SELECT ` + milestoneColumns + ` FROM agenteam_work.milestones WHERE project_id=$1 ORDER BY manual_rank,id LIMIT $2`
		args := []any{project.String(), page.Limit + 1}
		if rank != "" {
			query = `SELECT ` + milestoneColumns + ` FROM agenteam_work.milestones WHERE project_id=$1 AND (manual_rank,id)>($2,$3::uuid) ORDER BY manual_rank,id LIMIT $4`
			args = []any{project.String(), rank, id, page.Limit + 1}
		}
		rows, err := x.Query(ctx, query, args...)
		if err != nil {
			return unavailable(err)
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanMilestone(rows)
			if e != nil {
				return e
			}
			if v.ProjectID != project {
				return internal(nil)
			}
			out.Items = append(out.Items, v)
		}
		if err = rows.Err(); err != nil {
			return unavailable(err)
		}
		if len(out.Items) > page.Limit {
			last := out.Items[page.Limit-1]
			out.Items = out.Items[:page.Limit]
			out.NextCursor, err = pageToken(r.state().cursors, binding, generation, last.ManualRank, last.ID.String())
		}
		return err
	})
	if err != nil {
		return f.Page[c.Milestone]{}, err
	}
	return out, nil
}
func (r *Reader) ListSprints(ctx context.Context, actor i.Actor, project c.ProjectID, parent c.MilestoneID, page f.PageRequest) (f.Page[c.Sprint], error) {
	if err := readInput(ctx, actor, project); err != nil {
		return f.Page[c.Sprint]{}, err
	}
	if parent.Validate() != nil || page.Validate() != nil {
		return f.Page[c.Sprint]{}, fault(f.InvalidArgument)
	}
	out := f.Page[c.Sprint]{Items: []c.Sprint{}}
	err := r.read(ctx, actor, project, []f.LockRequest{rankLock(project, parent.String(), f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor, ref pc.ProjectRef) error {
		if _, err := loadMilestone(ctx, x, project, parent); err != nil {
			return err
		}
		generation, err := loadGeneration(ctx, x, project, parent.String())
		if err != nil {
			return err
		}
		binding, err := listBinding(project, actor.Details().UserID, &parent)
		if err != nil {
			return err
		}
		rank, id, err := pageAfter(r.state().cursors, page.Cursor, binding, generation)
		if err != nil {
			return err
		}
		query := `SELECT ` + sprintColumns + ` FROM agenteam_work.sprints WHERE project_id=$1 AND milestone_id=$2 ORDER BY manual_rank,id LIMIT $3`
		args := []any{project.String(), parent.String(), page.Limit + 1}
		if rank != "" {
			query = `SELECT ` + sprintColumns + ` FROM agenteam_work.sprints WHERE project_id=$1 AND milestone_id=$2 AND (manual_rank,id)>($3,$4::uuid) ORDER BY manual_rank,id LIMIT $5`
			args = []any{project.String(), parent.String(), rank, id, page.Limit + 1}
		}
		rows, err := x.Query(ctx, query, args...)
		if err != nil {
			return unavailable(err)
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanSprint(rows, ref.CurrentSprintID)
			if e != nil {
				return e
			}
			if v.ProjectID != project || v.MilestoneID != parent {
				return internal(nil)
			}
			out.Items = append(out.Items, v)
		}
		if err = rows.Err(); err != nil {
			return unavailable(err)
		}
		if len(out.Items) > page.Limit {
			last := out.Items[page.Limit-1]
			out.Items = out.Items[:page.Limit]
			out.NextCursor, err = pageToken(r.state().cursors, binding, generation, last.ManualRank, last.ID.String())
		}
		return err
	})
	if err != nil {
		return f.Page[c.Sprint]{}, err
	}
	return out, nil
}
func (r *Reader) ReadPlacementInTx(ctx context.Context, tx f.Tx, actor i.Actor, project c.ProjectID, id c.SprintID) (c.Placement, error) {
	if err := readInput(ctx, actor, project); err != nil {
		return c.Placement{}, err
	}
	if id.Validate() != nil {
		return c.Placement{}, fault(f.InvalidArgument)
	}
	st := r.state()
	if st == nil {
		return c.Placement{}, fault(f.DependencyUnbound)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return c.Placement{}, portError(err)
	}
	schedule, _ := f.ProjectScheduleLock(project.String())
	locks := []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), {Key: schedule, Mode: f.Exclusive}, sprintLock(id.String(), f.Shared)}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return c.Placement{}, portError(err)
	}
	access, err := st.authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return c.Placement{}, portError(err)
	}
	ref := access.Project()
	if ref.ID != project {
		return c.Placement{}, internal(nil)
	}
	if err = checkPointer(ctx, x, ref); err != nil {
		return c.Placement{}, err
	}
	sprint, err := loadSprint(ctx, x, project, id, ref.CurrentSprintID)
	if err != nil {
		return c.Placement{}, err
	}
	milestone, err := loadMilestone(ctx, x, project, sprint.MilestoneID)
	if err != nil {
		if isNotFound(err) {
			return c.Placement{}, internal(err)
		}
		return c.Placement{}, err
	}
	return c.Placement{Milestone: milestone, Sprint: sprint.Clone()}, nil
}

var _ c.Reader = (*Reader)(nil)
