package agent

import (
	"context"
	"encoding/json"
	"reflect"

	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// DirectoryReader owns read calls only. It needs no Agent write/runtime graph.
type DirectoryReader struct{ state *directoryState }
type directoryState struct {
	store     Store
	authority *Authority
	keys      cursor.Keyring
	calls     *callSet
}

func NewDirectoryReader(store Store, authority *Authority, keys cursor.Keyring) (*DirectoryReader, error) {
	if nilPort(store) || authority == nil || authority.state == nil || !reflect.TypeOf(store).Comparable() || store != authority.state.store {
		return nil, fault(f.DependencyUnbound)
	}
	if keys.Validate() != nil {
		return nil, invalid()
	}
	return &DirectoryReader{&directoryState{store, authority, keys, newCalls()}}, nil
}
func (r *DirectoryReader) Stop() {
	if r != nil && r.state != nil {
		r.state.calls.stop()
	}
}
func (r *DirectoryReader) Drain(ctx context.Context) error {
	if r == nil || r.state == nil {
		return fault(f.DependencyUnbound)
	}
	r.Stop()
	return r.state.calls.drain(ctx)
}
func (r *DirectoryReader) Joined() bool { return r != nil && r.state != nil && r.state.calls.joined() }
func (r *DirectoryReader) read(ctx context.Context, actor i.Actor, project i.ProjectID, locks []f.LockRequest, fn func(context.Context, postgres.SQLExecutor) error) error {
	if r == nil || r.state == nil {
		return fault(f.DependencyUnbound)
	}
	ctx, done, err := r.state.calls.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	if err = currentActor(actor); err != nil {
		return err
	}
	if project.Validate() != nil {
		return invalid()
	}
	cause, err := readCause("directory")
	if err != nil {
		return err
	}
	result := r.state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := r.state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		grant, err := r.state.authority.state.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		if !grant.Matches(actor, project) {
			return fault(f.Forbidden)
		}
		x, err := r.state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, x)
	})
	// Join the actual transaction before considering cancellation. Never publish
	// a partial page when its read transaction was unknown or canceled.
	if err = commitError(result); err != nil {
		return err
	}
	return ctx.Err()
}
func directoryProjection(v c.AgentConfig) c.DirectoryEntry {
	x := v.Fields().Core
	return c.DirectoryEntry{ID: x.ID, ProjectID: x.ProjectID, Name: x.Name, DisplayName: x.DisplayName, TagColor: x.TagColor, Description: x.Description, Version: x.Version, CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt}.Clone()
}
func (r *DirectoryReader) GetAgent(ctx context.Context, actor i.Actor, project i.ProjectID, agent i.AgentID) (c.DirectoryEntry, error) {
	if agent.Validate() != nil {
		return c.DirectoryEntry{}, invalid()
	}
	var out c.DirectoryEntry
	err := r.read(ctx, actor, project, []f.LockRequest{userLock(actor, f.Shared), projectLock(project, f.Shared), agentLock(agent, f.Shared)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		v, err := initializedAgent(ctx, x, project, agent)
		if err != nil {
			return err
		}
		out = directoryProjection(v)
		return out.Validate()
	})
	if err != nil {
		return c.DirectoryEntry{}, err
	}
	return out.Clone(), nil
}

const directoryOrder = "created_at:desc,id:desc"

func directoryBinding(project i.ProjectID, owner string) (cursor.Binding, error) {
	raw, err := json.Marshal(struct {
		Format  int         `json:"format"`
		Kind    string      `json:"kind"`
		Project i.ProjectID `json:"project_id"`
		Owner   string      `json:"owner_user_id"`
		Active  bool        `json:"initialized_active"`
	}{1, "agent.directory", project, owner, true})
	if err != nil {
		return cursor.Binding{}, unavailable(err)
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, unavailable(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		return cursor.Binding{}, invalid()
	}
	return cursor.Binding{Scope: scope, QueryDigest: digest, Order: directoryOrder}, nil
}

type directoryPosition struct {
	at f.Instant
	id i.AgentID
}

func directoryAfter(keys cursor.Keyring, token string, binding cursor.Binding) (*directoryPosition, error) {
	if token == "" {
		return nil, nil
	}
	p, err := keys.Verify(token, binding)
	if err != nil {
		return nil, err
	}
	if p.OrderGeneration != nil || len(p.Scalars) != 2 || p.Scalars[0].Kind() != "instant" || p.Scalars[1].Kind() != "uuid" {
		return nil, fault(f.CursorInvalid)
	}
	at, err := f.ParseInstant(p.Scalars[0].Value())
	if err != nil {
		return nil, fault(f.CursorInvalid)
	}
	id, err := f.ParseID[i.Agent](p.Scalars[1].Value())
	if err != nil {
		return nil, fault(f.CursorInvalid)
	}
	return &directoryPosition{at, id}, nil
}
func directoryBefore(v c.DirectoryEntry, p *directoryPosition) bool {
	return p == nil || v.CreatedAt.Time().Before(p.at.Time()) || v.CreatedAt == p.at && v.ID.String() < p.id.String()
}
func (r *DirectoryReader) ListAgents(ctx context.Context, actor i.Actor, project i.ProjectID, page f.PageRequest) (f.Page[c.DirectoryEntry], error) {
	if page.Validate() != nil || len(page.Cursor) > cursor.MaxTokenBytes {
		return f.Page[c.DirectoryEntry]{}, invalid()
	}
	out := f.Page[c.DirectoryEntry]{Items: []c.DirectoryEntry{}}
	// Every Agent mutation holds Project SH/EX. Project EX protects the finite
	// page and its initialization receipts without discovering IDs and adding
	// lower-ranked locks mid-transaction. It does not grant mutation authority.
	err := r.read(ctx, actor, project, []f.LockRequest{userLock(actor, f.Shared), projectLock(project, f.Exclusive)}, func(ctx context.Context, x postgres.SQLExecutor) error {
		binding, err := directoryBinding(project, actor.Details().UserID)
		if err != nil {
			return err
		}
		after, err := directoryAfter(r.state.keys, page.Cursor, binding)
		if err != nil {
			return err
		}
		items, err := loadDirectoryPage(ctx, x, project, page.Limit, after)
		if err != nil {
			return err
		}
		out.Items = items
		if len(items) > page.Limit {
			out.Items = items[:page.Limit]
			last := out.Items[len(out.Items)-1]
			at, err := cursor.Instant(last.CreatedAt)
			if err != nil {
				return unavailable(err)
			}
			id, err := cursor.UUID(last.ID.String())
			if err != nil {
				return unavailable(err)
			}
			out.NextCursor, err = r.state.keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{at, id}})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return f.Page[c.DirectoryEntry]{}, err
	}
	return out, nil
}

var _ c.DirectoryQueries = (*DirectoryReader)(nil)
