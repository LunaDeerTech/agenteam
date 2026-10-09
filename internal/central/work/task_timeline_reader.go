package work

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

var _ c.TaskTimelineReader = (*TaskReader)(nil)

func timelineFilter(v c.TaskTimelineFilter) c.TaskTimelineFilter {
	v = v.Clone()
	if v.Order == "" {
		v.Order = c.TaskTimelineDescending
	}
	all := []c.TaskTimelineEventType{c.TaskTimelineCreated, c.TaskTimelineFieldsUpdated, c.TaskTimelineBlockerAdded, c.TaskTimelineBlockerResolved}
	types := make([]c.TaskTimelineEventType, 0, len(all))
	for _, kind := range all {
		if v.Types == nil {
			types = append(types, kind)
			continue
		}
		for _, selected := range v.Types {
			if selected == kind {
				types = append(types, kind)
			}
		}
	}
	v.Types = types
	return v
}

func taskTimelineBinding(project c.ProjectID, task c.TaskID, user string, filter c.TaskTimelineFilter) (cursor.Binding, error) {
	filter = timelineFilter(filter)
	raw, err := json.Marshal(struct {
		Format  int                       `json:"format"`
		Kind    string                    `json:"kind"`
		Project c.ProjectID               `json:"project_id"`
		Task    c.TaskID                  `json:"task_id"`
		Owner   string                    `json:"owner_user_id"`
		Types   []c.TaskTimelineEventType `json:"types"`
		Order   c.TaskTimelineOrder       `json:"order"`
	}{1, "work.task-timeline", project, task, user, filter.Types, filter.Order})
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
	order := "created_at:desc,id:desc"
	if filter.Order == c.TaskTimelineAscending {
		order = "created_at:asc,id:asc"
	}
	return cursor.Binding{Scope: scope, QueryDigest: digest, Order: order}, nil
}

type taskTimelinePosition struct {
	at        f.Instant
	id        string
	watermark int64
}

func taskTimelineAfter(keys cursor.Keyring, token string, binding cursor.Binding, current f.Version) (*taskTimelinePosition, error) {
	if token == "" {
		return nil, nil
	}
	p, err := keys.Verify(token, binding)
	if err != nil {
		return nil, err
	}
	if len(p.Scalars) != 3 || p.OrderGeneration != nil || p.Scalars[0].Kind() != "instant" || p.Scalars[1].Kind() != "uuid" || p.Scalars[2].Kind() != "integer" {
		return nil, fault(f.CursorInvalid)
	}
	at, e1 := f.ParseInstant(p.Scalars[0].Value())
	w, e2 := strconv.ParseInt(p.Scalars[2].Value(), 10, 64)
	if e1 != nil || e2 != nil || w < 1 {
		return nil, fault(f.CursorInvalid)
	}
	if int64(current) < w {
		return nil, fault(f.CursorStale)
	}
	return &taskTimelinePosition{at: at, id: p.Scalars[1].Value(), watermark: w}, nil
}

func taskTimelineToken(keys cursor.Keyring, binding cursor.Binding, watermark int64, v c.TaskTimelineEvent) (string, error) {
	var at f.Instant
	var id string
	if v.Planning != nil {
		at, id = v.Planning.CreatedAt, v.Planning.ID.String()
	} else {
		at, id = v.Blocker.CreatedAt, v.Blocker.ID.String()
	}
	ts, err := cursor.Instant(at)
	if err != nil {
		return "", err
	}
	uuid, err := cursor.UUID(id)
	if err != nil {
		return "", err
	}
	return keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{ts, uuid, cursor.Integer(watermark)}})
}

func (r *TaskReader) ListTaskEvents(ctx context.Context, actor i.Actor, project c.ProjectID, task c.TaskID, filter c.TaskTimelineFilter, page f.PageRequest) (f.Page[c.TaskTimelineEvent], error) {
	if err := readInput(ctx, actor, project); err != nil {
		return f.Page[c.TaskTimelineEvent]{}, err
	}
	if task.Validate() != nil || page.Validate() != nil || len(page.Cursor) > cursor.MaxTokenBytes {
		return f.Page[c.TaskTimelineEvent]{}, fault(f.InvalidArgument)
	}
	if err := filter.Validate(); err != nil {
		return f.Page[c.TaskTimelineEvent]{}, err
	}
	// Keep omitted types distinct for SQL: the default reader must encounter
	// unsupported future rows instead of silently filtering them out.
	allTypes := filter.Types == nil
	filter = timelineFilter(filter)
	out := f.Page[c.TaskTimelineEvent]{Items: []c.TaskTimelineEvent{}}
	err := r.read(ctx, actor, project, []f.LockRequest{taskLock(task.String(), f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		current, err := loadTask(ctx, x, project, task)
		if err != nil {
			return err
		}
		binding, err := taskTimelineBinding(project, task, actor.Details().UserID, filter)
		if err != nil {
			return err
		}
		after, err := taskTimelineAfter(r.state().cursors, page.Cursor, binding, current.Version)
		if err != nil {
			return err
		}
		watermark := int64(current.Version)
		if after != nil {
			watermark = after.watermark
		}
		args := []any{project.String(), task.String(), watermark}
		where := []string{"project_id=$1", "task_id=$2", "task_version<=$3"}
		bind := func(value any) string { args = append(args, value); return "$" + strconv.Itoa(len(args)) }
		if !allTypes {
			selected := make([]string, len(filter.Types))
			for n, kind := range filter.Types {
				selected[n] = string(kind)
			}
			where = append(where, "type=ANY("+bind(selected)+"::text[])")
		}
		direction, comparison := "DESC", "<"
		if filter.Order == c.TaskTimelineAscending {
			direction, comparison = "ASC", ">"
		}
		if after != nil {
			where = append(where, "(created_at,id)"+comparison+"("+bind(after.at.Time())+"::timestamptz,"+bind(after.id)+"::uuid)")
		}
		query := "SELECT id::text,project_id::text,task_id::text,task_version,type,actor,operation_id::text,blocker_operation_id::text,correlation_id::text,payload,created_at FROM agenteam_work.task_events WHERE " + strings.Join(where, " AND ") + " ORDER BY created_at " + direction + ",id " + direction + " LIMIT " + bind(page.Limit+1)
		rows, err := x.Query(ctx, query, args...)
		if err != nil {
			return taskSQL(err)
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanTaskTimeline(rows, project, task, watermark)
			if err != nil {
				return err
			}
			out.Items = append(out.Items, v.Clone())
			if len(out.Items) > page.Limit+1 {
				return internal(nil)
			}
		}
		if err := rows.Err(); err != nil {
			return taskSQL(err)
		}
		rows.Close()
		if len(out.Items) > page.Limit {
			out.NextCursor, err = taskTimelineToken(r.state().cursors, binding, watermark, out.Items[page.Limit-1])
			out.Items = out.Items[:page.Limit]
		}
		return err
	})
	if err != nil {
		return f.Page[c.TaskTimelineEvent]{}, err
	}
	return out, nil
}

func scanTaskTimeline(row interface{ Scan(...any) error }, project c.ProjectID, task c.TaskID, watermark int64) (c.TaskTimelineEvent, error) {
	var id, p, target, kind, correlation string
	var version int64
	var operation, blockerOperation *string
	var actor, payload []byte
	var created time.Time
	if err := row.Scan(&id, &p, &target, &version, &kind, &actor, &operation, &blockerOperation, &correlation, &payload, &created); err != nil {
		return c.TaskTimelineEvent{}, taskSQL(err)
	}
	if p != project.String() || target != task.String() || version < 1 || version > watermark {
		return c.TaskTimelineEvent{}, internal(nil)
	}
	var op string
	switch c.TaskTimelineEventType(kind) {
	case c.TaskTimelineCreated, c.TaskTimelineFieldsUpdated:
		if operation == nil || blockerOperation != nil {
			return c.TaskTimelineEvent{}, internal(nil)
		}
		op = *operation
	case c.TaskTimelineBlockerAdded, c.TaskTimelineBlockerResolved:
		if blockerOperation == nil || operation != nil {
			return c.TaskTimelineEvent{}, internal(nil)
		}
		op = *blockerOperation
	default:
		return c.TaskTimelineEvent{}, internal(nil)
	}
	if op != correlation {
		return c.TaskTimelineEvent{}, internal(nil)
	}
	at, err := f.NewInstant(created)
	if err != nil {
		return c.TaskTimelineEvent{}, internal(err)
	}
	raw, err := json.Marshal(struct {
		ID          string          `json:"id"`
		Project     string          `json:"project_id"`
		Task        string          `json:"task_id"`
		Version     f.Version       `json:"task_version"`
		Type        string          `json:"type"`
		Actor       json.RawMessage `json:"actor"`
		Operation   string          `json:"operation_id"`
		Correlation string          `json:"correlation_id"`
		Payload     json.RawMessage `json:"payload"`
		Created     f.Instant       `json:"created_at"`
	}{id, p, target, f.Version(version), kind, actor, op, correlation, payload, at})
	if err != nil {
		return c.TaskTimelineEvent{}, internal(err)
	}
	v, err := c.DecodeTaskTimelineEvent(raw)
	if err != nil {
		return c.TaskTimelineEvent{}, internal(err)
	}
	return v, nil
}
