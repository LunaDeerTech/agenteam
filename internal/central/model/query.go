package model

import (
	"context"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type SystemQuery struct {
	Cursor string
	Limit  int
}
type ProviderPage struct {
	Items      []mc.ProviderView `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}
type ModelPage struct {
	Items      []mc.ModelView `json:"items"`
	NextCursor string         `json:"next_cursor,omitempty"`
}
type PlatformSelectionState struct {
	ID         string                `json:"id"`
	Version    f.Version             `json:"version"`
	Configured *mc.PlatformSelection `json:"configured"`
}
type LookupCommandRequest struct {
	Meta    mc.CommandMeta
	Command string
}
type CommandLookup struct {
	Found   bool               `json:"found"`
	Receipt *mc.CommandReceipt `json:"receipt"`
}

func (s *Service) read(ctx context.Context, actor id.Actor, locks []f.LockRequest, run func(context.Context, postgres.SQLExecutor) error) error {
	if s.state() == nil {
		return fault(f.DependencyUnbound)
	}
	if e := human(actor); e != nil {
		return e
	}
	cause, e := readCause("query")
	if e != nil {
		return e
	}
	locks = append(locks, userLock(actor.Details().UserID), systemLock("model-references", f.Shared))
	r := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		if e := s.state().authority.current(ctx, tx, actor, id.Read); e != nil {
			return e
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		return run(ctx, x)
	})
	return commitError(r)
}
func (s *Service) GetProvider(ctx context.Context, actor id.Actor, provider mc.ProviderID) (mc.ProviderView, error) {
	var out mc.ProviderView
	if provider.Validate() != nil {
		return out, fault(f.InvalidArgument)
	}
	e := s.read(ctx, actor, []f.LockRequest{aggregateLock(f.ProviderAggregate, provider.String(), f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		r, e := loadProvider(ctx, x, provider.String())
		if e != nil {
			return e
		}
		if r == nil {
			return fault(f.NotFound)
		}
		out, e = r.view()
		return e
	})
	if e != nil {
		return mc.ProviderView{}, e
	}
	return out, nil
}
func (s *Service) GetModel(ctx context.Context, actor id.Actor, model mc.ModelID) (mc.ModelView, error) {
	var out mc.ModelView
	if model.Validate() != nil {
		return out, fault(f.InvalidArgument)
	}
	e := s.read(ctx, actor, []f.LockRequest{aggregateLock(f.ModelConfigAggregate, model.String(), f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		r, e := loadModel(ctx, x, model.String())
		if e != nil {
			return e
		}
		if r == nil {
			return fault(f.NotFound)
		}
		out, e = r.view()
		return e
	})
	if e != nil {
		return mc.ModelView{}, e
	}
	return out, nil
}
func selectionView(r *selectionRecord) (PlatformSelectionState, error) {
	if r == nil || r.Version.Validate() != nil {
		return PlatformSelectionState{}, unavailable(nil)
	}
	out := PlatformSelectionState{ID: r.ID, Version: r.Version}
	if !r.Configured {
		if r.Embedding != "" || r.Memory != "" || r.Reranker != "" || r.Image != "" {
			return out, unavailable(nil)
		}
		return out, nil
	}
	selection := mc.PlatformSelection{ID: r.ID, Version: r.Version}
	var e error
	selection.Embedding, e = f.ParseID[mc.Model](r.Embedding)
	if e != nil {
		return out, unavailable(e)
	}
	selection.Memory, e = f.ParseID[mc.Model](r.Memory)
	if e != nil {
		return out, unavailable(e)
	}
	if r.Reranker != "" {
		v, e := f.ParseID[mc.Model](r.Reranker)
		if e != nil {
			return out, unavailable(e)
		}
		selection.Reranker = &v
	}
	if r.Image != "" {
		v, e := f.ParseID[mc.Model](r.Image)
		if e != nil {
			return out, unavailable(e)
		}
		selection.Image = &v
	}
	if e = selection.Validate(); e != nil {
		return out, unavailable(e)
	}
	out.Configured = &selection
	return out, nil
}
func (s *Service) GetPlatformSelection(ctx context.Context, actor id.Actor) (PlatformSelectionState, error) {
	var out PlatformSelectionState
	e := s.read(ctx, actor, []f.LockRequest{systemLock("model-platform-selection", f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		r, e := loadSelection(ctx, x)
		if e != nil {
			return e
		}
		out, e = selectionView(r)
		return e
	})
	if e != nil {
		return PlatformSelectionState{}, e
	}
	return out, nil
}
func (s *Service) LookupCommand(ctx context.Context, r LookupCommandRequest) (CommandLookup, error) {
	if r.Meta.Validate() != nil || !validCommand(r.Command) {
		return CommandLookup{}, fault(f.InvalidArgument)
	}
	if r.Meta.Scope.Details().Kind != id.System {
		return CommandLookup{}, fault(f.DependencyUnbound)
	}
	identity, e := commandIdentity(r.Meta, r.Command)
	if e != nil {
		return CommandLookup{}, e
	}
	var out CommandLookup
	e = s.read(ctx, r.Meta.Actor, []f.LockRequest{commandLock(identity)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		row, e := loadCommand(ctx, x, identity)
		if e != nil {
			return e
		}
		if row != nil && row.Phase == "committed" && row.Receipt != nil {
			v := *row.Receipt
			out = CommandLookup{Found: true, Receipt: &v}
		}
		return nil
	})
	if e != nil {
		return CommandLookup{}, e
	}
	return out, nil
}

type pagePosition struct {
	WaterAt, AfterAt f.Instant
	WaterID, AfterID string
	Present          bool
}

func listBinding(actor id.Actor, kind, provider string) (cursor.Binding, error) {
	raw, e := json.Marshal(struct{ Format, User, Kind, Provider string }{"model-list-v1", actor.Details().UserID, kind, provider})
	if e != nil {
		return cursor.Binding{}, unavailable(e)
	}
	digest, e := cursor.Digest(raw)
	if e != nil {
		return cursor.Binding{}, e
	}
	return cursor.Binding{Scope: id.SystemScope(), QueryDigest: digest, Order: cursor.AuditOrder}, nil
}
func (s *Service) pagePosition(q SystemQuery, b cursor.Binding) (pagePosition, error) {
	if q.Limit < 1 || q.Limit > 100 {
		return pagePosition{}, fault(f.InvalidArgument)
	}
	if q.Cursor == "" {
		return pagePosition{}, nil
	}
	position, e := s.state().deps.Cursors.Verify(q.Cursor, b)
	if e != nil {
		return pagePosition{}, e
	}
	if len(position.Scalars) != 4 || position.OrderGeneration != nil || position.Scalars[0].Kind() != "instant" || position.Scalars[1].Kind() != "uuid" || position.Scalars[2].Kind() != "instant" || position.Scalars[3].Kind() != "uuid" {
		return pagePosition{}, fault(f.CursorInvalid)
	}
	p := pagePosition{WaterID: position.Scalars[1].Value(), AfterID: position.Scalars[3].Value(), Present: true}
	p.WaterAt, e = f.ParseInstant(position.Scalars[0].Value())
	if e != nil {
		return pagePosition{}, fault(f.CursorInvalid)
	}
	p.AfterAt, e = f.ParseInstant(position.Scalars[2].Value())
	if e != nil {
		return pagePosition{}, fault(f.CursorInvalid)
	}
	if p.AfterAt.Time().After(p.WaterAt.Time()) || p.AfterAt == p.WaterAt && p.AfterID > p.WaterID {
		return pagePosition{}, fault(f.CursorInvalid)
	}
	return p, nil
}
func (s *Service) signPage(b cursor.Binding, p pagePosition, firstAt f.Instant, firstID string, lastAt f.Instant, lastID string) (string, error) {
	if !p.Present {
		p.WaterAt, p.WaterID = firstAt, firstID
	}
	waterAt, _ := cursor.Instant(p.WaterAt)
	waterID, _ := cursor.UUID(p.WaterID)
	afterAt, _ := cursor.Instant(lastAt)
	afterID, _ := cursor.UUID(lastID)
	return s.state().deps.Cursors.Sign(b, cursor.Position{Scalars: []cursor.Scalar{waterAt, waterID, afterAt, afterID}})
}
func (s *Service) ListProviders(ctx context.Context, actor id.Actor, q SystemQuery) (ProviderPage, error) {
	out := ProviderPage{Items: []mc.ProviderView{}}
	e := s.read(ctx, actor, nil, func(ctx context.Context, x postgres.SQLExecutor) error {
		binding, e := listBinding(actor, "providers", "")
		if e != nil {
			return e
		}
		position, e := s.pagePosition(q, binding)
		if e != nil {
			return e
		}
		query := `SELECT ` + providerColumns + ` FROM agenteam_model.providers WHERE scope='system'`
		args := []any{q.Limit + 1}
		if position.Present {
			query += ` AND (created_at,id)<=($2,$3::uuid) AND (created_at,id)<($4,$5::uuid)`
			args = append(args, position.WaterAt.Time(), position.WaterID, position.AfterAt.Time(), position.AfterID)
		}
		query += ` ORDER BY created_at DESC,id DESC LIMIT $1`
		rows, e := x.Query(ctx, query, args...)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		for rows.Next() {
			r, e := scanProvider(rows)
			if e != nil {
				return e
			}
			v, e := r.view()
			if e != nil {
				return e
			}
			out.Items = append(out.Items, v)
		}
		if e = rows.Err(); e != nil {
			return unavailable(e)
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			first, last := out.Items[0], out.Items[len(out.Items)-1]
			out.NextCursor, e = s.signPage(binding, position, first.CreatedAt, first.ID.String(), last.CreatedAt, last.ID.String())
			return e
		}
		return nil
	})
	if e != nil {
		return ProviderPage{}, e
	}
	return out, nil
}
func (s *Service) ListModels(ctx context.Context, actor id.Actor, provider mc.ProviderID, q SystemQuery) (ModelPage, error) {
	out := ModelPage{Items: []mc.ModelView{}}
	if provider.Validate() != nil {
		return ModelPage{}, fault(f.InvalidArgument)
	}
	e := s.read(ctx, actor, []f.LockRequest{aggregateLock(f.ProviderAggregate, provider.String(), f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		current, e := loadProvider(ctx, x, provider.String())
		if e != nil {
			return e
		}
		if current == nil {
			return fault(f.NotFound)
		}
		binding, e := listBinding(actor, "models", provider.String())
		if e != nil {
			return e
		}
		position, e := s.pagePosition(q, binding)
		if e != nil {
			return e
		}
		query := `SELECT ` + modelColumns + ` FROM agenteam_model.models m JOIN agenteam_model.providers p ON p.id=m.provider_id WHERE p.scope='system' AND m.provider_id=$1`
		args := []any{provider.String(), q.Limit + 1}
		if position.Present {
			query += ` AND (m.created_at,m.id)<=($3,$4::uuid) AND (m.created_at,m.id)<($5,$6::uuid)`
			args = append(args, position.WaterAt.Time(), position.WaterID, position.AfterAt.Time(), position.AfterID)
		}
		query += ` ORDER BY m.created_at DESC,m.id DESC LIMIT $2`
		rows, e := x.Query(ctx, query, args...)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		for rows.Next() {
			r, e := scanModel(rows)
			if e != nil {
				return e
			}
			v, e := r.view()
			if e != nil {
				return e
			}
			out.Items = append(out.Items, v)
		}
		if e = rows.Err(); e != nil {
			return unavailable(e)
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			first, last := out.Items[0], out.Items[len(out.Items)-1]
			out.NextCursor, e = s.signPage(binding, position, first.CreatedAt, first.ID.String(), last.CreatedAt, last.ID.String())
			return e
		}
		return nil
	})
	if e != nil {
		return ModelPage{}, e
	}
	return out, nil
}
