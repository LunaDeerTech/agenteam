package work

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

type BlockerReader struct{ data func() *blockerReaderState }
type blockerReaderState struct {
	store     Store
	authority *Authority
	cursors   cursor.Keyring
}

// NewBlockerReader only binds existing providers. Its synchronous calls and
// Rows belong to the caller; the production root joins their HTTP requests.
func NewBlockerReader(store Store, authority *Authority, keys cursor.Keyring) (*BlockerReader, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) {
		return nil, fault(f.DependencyUnbound)
	}
	if keys.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	st := &blockerReaderState{store: store, authority: authority, cursors: keys}
	return &BlockerReader{data: func() *blockerReaderState { return st }}, nil
}
func (r *BlockerReader) state() *blockerReaderState {
	if r == nil || r.data == nil {
		return nil
	}
	return r.data()
}

const blockerPageOrder = "created_at:asc,id:asc"

func blockerPageBinding(project c.ProjectID, task c.TaskID, owner string, status c.TaskBlockerStatus) (cursor.Binding, error) {
	raw, err := json.Marshal(struct {
		Format  int                 `json:"format"`
		Kind    string              `json:"kind"`
		Project c.ProjectID         `json:"project_id"`
		Task    c.TaskID            `json:"task_id"`
		Owner   string              `json:"owner_user_id"`
		Status  c.TaskBlockerStatus `json:"status"`
	}{1, "work.task-blockers", project, task, owner, status})
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		return cursor.Binding{}, portError(err)
	}
	return cursor.Binding{Scope: scope, QueryDigest: digest, Order: blockerPageOrder}, nil
}

type blockerPagePosition struct {
	at f.Instant
	id c.TaskBlockerID
}

func blockerPageAfter(keys cursor.Keyring, token string, binding cursor.Binding, version int64) (*blockerPagePosition, error) {
	if token == "" {
		return nil, nil
	}
	position, err := keys.Verify(token, binding)
	if err != nil {
		return nil, err
	}
	if len(position.Scalars) != 2 || position.Scalars[0].Kind() != "instant" || position.Scalars[1].Kind() != "uuid" || position.OrderGeneration == nil || *position.OrderGeneration < 1 {
		return nil, fault(f.CursorInvalid)
	}
	at, err := f.ParseInstant(position.Scalars[0].Value())
	if err != nil {
		return nil, fault(f.CursorInvalid)
	}
	id, err := f.ParseID[c.TaskBlockerIdentity](position.Scalars[1].Value())
	if err != nil {
		return nil, fault(f.CursorInvalid)
	}
	if *position.OrderGeneration != version {
		return nil, fault(f.CursorStale)
	}
	return &blockerPagePosition{at, id}, nil
}
func blockerPageToken(keys cursor.Keyring, binding cursor.Binding, version int64, value c.TaskBlocker) (string, error) {
	at, err := cursor.Instant(value.CreatedAt)
	if err != nil {
		return "", err
	}
	id, err := cursor.UUID(value.ID.String())
	if err != nil {
		return "", err
	}
	return keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{at, id}, OrderGeneration: &version})
}

func (r *BlockerReader) ListTaskBlockersPage(ctx context.Context, actor i.Actor, project c.ProjectID, task c.TaskID, status c.TaskBlockerStatus, page f.PageRequest) (f.Page[c.TaskBlocker], error) {
	empty := f.Page[c.TaskBlocker]{}
	if err := readInput(ctx, actor, project); err != nil {
		return empty, err
	}
	if task.Validate() != nil || status.Validate() != nil || page.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	st := r.state()
	if st == nil {
		return empty, fault(f.DependencyUnbound)
	}
	cause, err := readCause("task-blockers.page")
	if err != nil {
		return empty, err
	}
	locks, err := taskNormalize([]f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared), taskScheduleLock(project, f.Shared), taskLock(task.String(), f.Shared)})
	if err != nil {
		return empty, err
	}
	out := f.Page[c.TaskBlocker]{Items: []c.TaskBlocker{}}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		// blockerScope proves this Store's live Tx and complete held locks, then
		// rechecks current Session/Owner before any Work query or cursor decode.
		scope, err := st.authority.blockerScope(ctx, tx, actor, project, task, false)
		if err != nil {
			return err
		}
		var version int64
		err = scope.x.QueryRow(ctx, `SELECT version FROM agenteam_work.tasks WHERE project_id=$1 AND id=$2`, project.String(), task.String()).Scan(&version)
		if errors.Is(err, pgx.ErrNoRows) {
			return fault(f.TaskNotFound)
		}
		if err != nil {
			return taskSQL(err)
		}
		if version < 1 {
			return internal(nil)
		}
		binding, err := blockerPageBinding(project, task, actor.Details().UserID, status)
		if err != nil {
			return err
		}
		after, err := blockerPageAfter(st.cursors, page.Cursor, binding, version)
		if err != nil {
			return err
		}
		var count int64
		if err = scope.x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2`, project.String(), task.String()).Scan(&count); err != nil {
			return taskSQL(err)
		}
		if count > blockerHistoryCap {
			return fault(f.ResourceBusy)
		}
		args := []any{project.String(), task.String()}
		where := []string{"project_id=$1", "task_id=$2"}
		bind := func(value any) string { args = append(args, value); return "$" + strconv.Itoa(len(args)) }
		if status == c.TaskBlockersUnresolved {
			where = append(where, "resolved_at IS NULL")
		}
		if status == c.TaskBlockersResolved {
			where = append(where, "resolved_at IS NOT NULL")
		}
		if after != nil {
			where = append(where, "(created_at,id)>("+bind(after.at.Time())+"::timestamptz,"+bind(after.id.String())+"::uuid)")
		}
		query := "SELECT " + blockerColumns + " FROM agenteam_work.task_blockers WHERE " + strings.Join(where, " AND ") + " ORDER BY created_at,id LIMIT " + bind(page.Limit+1)
		rows, err := scope.x.Query(ctx, query, args...)
		if err != nil {
			return taskSQL(err)
		}
		defer rows.Close()
		previous := after
		for rows.Next() {
			row, err := scanBlocker(rows)
			if err != nil {
				return err
			}
			if row == nil || len(out.Items) >= page.Limit+1 {
				return internal(nil)
			}
			value := row.Value
			if value.ProjectID != project || value.TaskID != task || status == c.TaskBlockersUnresolved && value.ResolvedAt != nil || status == c.TaskBlockersResolved && value.ResolvedAt == nil {
				return internal(nil)
			}
			if previous != nil && (value.CreatedAt.Time().Before(previous.at.Time()) || value.CreatedAt.Time().Equal(previous.at.Time()) && value.ID.String() <= previous.id.String()) {
				return internal(nil)
			}
			out.Items = append(out.Items, value.Clone())
			previous = &blockerPagePosition{value.CreatedAt, value.ID}
		}
		if err = rows.Err(); err != nil {
			return taskSQL(err)
		}
		rows.Close()
		if len(out.Items) > page.Limit {
			last := out.Items[page.Limit-1]
			out.Items = out.Items[:page.Limit]
			out.NextCursor, err = blockerPageToken(st.cursors, binding, version, last)
		}
		return err
	})
	if result.State() == f.NotCommitted && ctx.Err() != nil {
		return empty, canceled(ctx.Err())
	}
	if err = txError(result); err != nil {
		return empty, err
	}
	return out, nil
}

var _ c.TaskBlockerPageReader = (*BlockerReader)(nil)
