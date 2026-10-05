package model

import (
	"context"
	"encoding/json"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type ProjectQuery struct {
	Cursor string
	Limit  int
}

// AvailableChatModel is a selection projection, not a Provider configuration
// or a claim that its endpoint/model has been exercised.
type AvailableChatModel struct {
	ID           mc.ModelID      `json:"id"`
	ProviderID   mc.ProviderID   `json:"provider_id"`
	Scope        id.Scope        `json:"scope"`
	Name         string          `json:"name"`
	ProviderName string          `json:"provider_name"`
	Version      f.Version       `json:"version"`
	Capabilities mc.Capabilities `json:"capabilities"`
}
type AvailableChatModelPage struct {
	Items      []AvailableChatModel `json:"items"`
	NextCursor string               `json:"next_cursor,omitempty"`
}

func projectQueryScope(project id.ProjectID) (id.Scope, error) {
	scope, err := id.InProject(project)
	if err != nil {
		return id.Scope{}, fault(f.InvalidArgument)
	}
	return scope, nil
}
func projectListBinding(actor id.Actor, scope id.Scope, kind string) (cursor.Binding, error) {
	if human(actor) != nil || scope.Validate() != nil || scope.Details().Kind != id.ProjectScope {
		return cursor.Binding{}, fault(f.InvalidArgument)
	}
	switch kind {
	case "providers", "models", "available-chat":
	default:
		return cursor.Binding{}, fault(f.InvalidArgument)
	}
	raw, err := encoded(struct{ Format, User, Project, Kind, Filter string }{"model-project-list-v1", actor.Details().UserID, scope.Details().ProjectID, kind, "chat;available=provider-enabled+model-enabled"})
	if err != nil {
		return cursor.Binding{}, err
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, err
	}
	return cursor.Binding{Scope: scope, QueryDigest: digest, Order: cursor.AuditOrder}, nil
}
func (s *Service) GetProjectProvider(ctx context.Context, actor id.Actor, project id.ProjectID, provider mc.ProviderID) (mc.ProviderView, error) {
	scope, err := projectQueryScope(project)
	if err != nil || provider.Validate() != nil {
		return mc.ProviderView{}, fault(f.InvalidArgument)
	}
	var out mc.ProviderView
	err = s.readScope(ctx, actor, scope, []f.LockRequest{aggregateLock(f.ProviderAggregate, provider.String(), f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		row, err := loadProviderScope(ctx, x, provider.String(), scope)
		if err != nil {
			return err
		}
		if row == nil {
			return fault(f.NotFound)
		}
		out, err = row.view()
		return err
	})
	if err != nil {
		return mc.ProviderView{}, err
	}
	return out, nil
}
func (s *Service) GetProjectModel(ctx context.Context, actor id.Actor, project id.ProjectID, model mc.ModelID) (mc.ModelView, error) {
	scope, err := projectQueryScope(project)
	if err != nil || model.Validate() != nil {
		return mc.ModelView{}, fault(f.InvalidArgument)
	}
	var out mc.ModelView
	err = s.readScope(ctx, actor, scope, []f.LockRequest{aggregateLock(f.ModelConfigAggregate, model.String(), f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		row, err := loadModelScope(ctx, x, model.String(), scope)
		if err != nil {
			return err
		}
		if row == nil {
			return fault(f.NotFound)
		}
		out, err = row.view()
		return err
	})
	if err != nil {
		return mc.ModelView{}, err
	}
	return out, nil
}

func (s *Service) ListProjectProviders(ctx context.Context, actor id.Actor, project id.ProjectID, q ProjectQuery) (ProviderPage, error) {
	scope, err := projectQueryScope(project)
	if err != nil {
		return ProviderPage{}, err
	}
	out := ProviderPage{Items: []mc.ProviderView{}}
	err = s.readScope(ctx, actor, scope, nil, func(ctx context.Context, x postgres.SQLExecutor) error {
		binding, err := projectListBinding(actor, scope, "providers")
		if err != nil {
			return err
		}
		position, err := s.pagePosition(SystemQuery{q.Cursor, q.Limit}, binding)
		if err != nil {
			return err
		}
		query := `SELECT ` + providerColumns + ` FROM agenteam_model.providers WHERE scope='project' AND project_id=$1`
		args := []any{project.String(), q.Limit + 1}
		if position.Present {
			query += ` AND (created_at,id)<=($3,$4::uuid) AND (created_at,id)<($5,$6::uuid)`
			args = append(args, position.WaterAt.Time(), position.WaterID, position.AfterAt.Time(), position.AfterID)
		}
		query += ` ORDER BY created_at DESC,id DESC LIMIT $2`
		rows, err := x.Query(ctx, query, args...)
		if err != nil {
			return unavailable(err)
		}
		defer rows.Close()
		for rows.Next() {
			row, err := scanProviderScope(rows, scope)
			if err != nil {
				return err
			}
			v, err := row.view()
			if err != nil {
				return err
			}
			out.Items = append(out.Items, v)
		}
		if err = rows.Err(); err != nil {
			return unavailable(err)
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			first, last := out.Items[0], out.Items[q.Limit-1]
			out.NextCursor, err = s.signPage(binding, position, first.CreatedAt, first.ID.String(), last.CreatedAt, last.ID.String())
		}
		return err
	})
	if err != nil {
		return ProviderPage{}, err
	}
	return out, nil
}
func (s *Service) ListProjectModels(ctx context.Context, actor id.Actor, project id.ProjectID, q ProjectQuery) (ModelPage, error) {
	scope, err := projectQueryScope(project)
	if err != nil {
		return ModelPage{}, err
	}
	out := ModelPage{Items: []mc.ModelView{}}
	err = s.readScope(ctx, actor, scope, nil, func(ctx context.Context, x postgres.SQLExecutor) error {
		binding, err := projectListBinding(actor, scope, "models")
		if err != nil {
			return err
		}
		position, err := s.pagePosition(SystemQuery{q.Cursor, q.Limit}, binding)
		if err != nil {
			return err
		}
		query := `SELECT ` + modelColumns + ` FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE p.scope='project' AND p.project_id=$1 AND m.type='chat'`
		args := []any{project.String(), q.Limit + 1}
		if position.Present {
			query += ` AND (m.created_at,m.id)<=($3,$4::uuid) AND (m.created_at,m.id)<($5,$6::uuid)`
			args = append(args, position.WaterAt.Time(), position.WaterID, position.AfterAt.Time(), position.AfterID)
		}
		query += ` ORDER BY m.created_at DESC,m.id DESC LIMIT $2`
		rows, err := x.Query(ctx, query, args...)
		if err != nil {
			return unavailable(err)
		}
		defer rows.Close()
		for rows.Next() {
			row, err := scanModelScope(rows, scope)
			if err != nil {
				return err
			}
			v, err := row.view()
			if err != nil {
				return err
			}
			out.Items = append(out.Items, v)
		}
		if err = rows.Err(); err != nil {
			return unavailable(err)
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			first, last := out.Items[0], out.Items[q.Limit-1]
			out.NextCursor, err = s.signPage(binding, position, first.CreatedAt, first.ID.String(), last.CreatedAt, last.ID.String())
		}
		return err
	})
	if err != nil {
		return ModelPage{}, err
	}
	return out, nil
}
func (s *Service) ListAvailableChatModels(ctx context.Context, actor id.Actor, project id.ProjectID, q ProjectQuery) (AvailableChatModelPage, error) {
	scope, err := projectQueryScope(project)
	if err != nil {
		return AvailableChatModelPage{}, err
	}
	out := AvailableChatModelPage{Items: []AvailableChatModel{}}
	err = s.readScope(ctx, actor, scope, nil, func(ctx context.Context, x postgres.SQLExecutor) error {
		binding, err := projectListBinding(actor, scope, "available-chat")
		if err != nil {
			return err
		}
		position, err := s.pagePosition(SystemQuery{q.Cursor, q.Limit}, binding)
		if err != nil {
			return err
		}
		// One statement snapshot observes both enabled flags and the safe projection.
		// The global SH barrier alone does not freeze concurrent configuration edits.
		query := `SELECT m.id::text,m.provider_id::text,p.scope,p.project_id::text,m.name,p.name,m.version,m.capabilities,m.created_at FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE m.type='chat' AND m.enabled AND p.enabled AND (p.scope='system' OR (p.scope='project' AND p.project_id=$1))`
		args := []any{project.String(), q.Limit + 1}
		if position.Present {
			query += ` AND (m.created_at,m.id)<=($3,$4::uuid) AND (m.created_at,m.id)<($5,$6::uuid)`
			args = append(args, position.WaterAt.Time(), position.WaterID, position.AfterAt.Time(), position.AfterID)
		}
		query += ` ORDER BY m.created_at DESC,m.id DESC LIMIT $2`
		rows, err := x.Query(ctx, query, args...)
		if err != nil {
			return unavailable(err)
		}
		defer rows.Close()
		var times []f.Instant
		for rows.Next() {
			var v AvailableChatModel
			var model, provider, kind string
			var owner *string
			var caps []byte
			var at time.Time
			if err = rows.Scan(&model, &provider, &kind, &owner, &v.Name, &v.ProviderName, &v.Version, &caps, &at); err != nil {
				return unavailable(err)
			}
			v.ID, err = f.ParseID[mc.Model](model)
			if err != nil {
				return unavailable(err)
			}
			v.ProviderID, err = f.ParseID[mc.Provider](provider)
			if err != nil {
				return unavailable(err)
			}
			if kind == "system" && owner == nil {
				v.Scope = id.SystemScope()
			} else if kind == "project" && owner != nil && *owner == project.String() {
				v.Scope = scope
			} else {
				return unavailable(nil)
			}
			if v.Version.Validate() != nil || json.Unmarshal(caps, &v.Capabilities) != nil || v.Capabilities.Validate() != nil {
				return unavailable(nil)
			}
			v.Capabilities = v.Capabilities.Clone()
			out.Items = append(out.Items, v)
			instant, err := f.NewInstant(at)
			if err != nil {
				return unavailable(err)
			}
			times = append(times, instant)
		}
		if err = rows.Err(); err != nil {
			return unavailable(err)
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			first, last := out.Items[0], out.Items[q.Limit-1]
			out.NextCursor, err = s.signPage(binding, position, times[0], first.ID.String(), times[q.Limit-1], last.ID.String())
		}
		return err
	})
	if err != nil {
		return AvailableChatModelPage{}, err
	}
	return out, nil
}
