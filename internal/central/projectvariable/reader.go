package projectvariable

import (
	"context"
	"encoding/json"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func readInput(a i.Actor, p c.ProjectID) error {
	if a.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	if p.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if a.Details().Kind == i.AgentRun {
		return fault(f.DependencyUnbound)
	}
	if a.Details().Kind != i.Human {
		return fault(f.Forbidden)
	}
	return nil
}
func readLocks(a i.Actor, p c.ProjectID) []f.LockRequest {
	locks, _ := oc.NormalizeLocks([]f.LockRequest{userLock(a.Details().UserID, f.Shared), projectLock(p, f.Shared)})
	return locks
}
func (s *Service) GetVariable(ctx context.Context, a i.Actor, p c.ProjectID, id c.VariableID) (c.Variable, error) {
	ctx, _, done, e := s.beginProject(ctx, p, readCall)
	if e != nil {
		return c.Variable{}, e
	}
	defer done()
	if e = readInput(a, p); e != nil {
		return c.Variable{}, e
	}
	if id.Validate() != nil {
		return c.Variable{}, fault(f.InvalidArgument)
	}
	cause, e := readCause("get")
	if e != nil {
		return c.Variable{}, e
	}
	st := s.state()
	var out c.Variable
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, readLocks(a, p)); e != nil {
			return portError(e)
		}
		if _, e := st.deps.Projects.RequireOwnerInTx(ctx, tx, a, p, i.Read); e != nil {
			return portError(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		out, _, e = loadVariable(ctx, x, p, id, false)
		return e
	})
	if e = txError(result); e != nil {
		return c.Variable{}, e
	}
	if e = ctx.Err(); e != nil {
		return c.Variable{}, canceled(e)
	}
	return out, nil
}

const pageOrder = "name:C:asc,id:asc"

func pageBinding(p c.ProjectID, owner string) (cursor.Binding, error) {
	raw, e := json.Marshal(struct {
		Format  int         `json:"format"`
		Kind    string      `json:"kind"`
		Project c.ProjectID `json:"project_id"`
		Owner   string      `json:"owner_user_id"`
		Type    string      `json:"type"`
	}{1, "project.variables", p, owner, c.VariableType})
	if e != nil {
		return cursor.Binding{}, internal(e)
	}
	d, e := cursor.Digest(raw)
	if e != nil {
		return cursor.Binding{}, internal(e)
	}
	scope, e := i.InProject(p)
	if e != nil {
		return cursor.Binding{}, internal(e)
	}
	return cursor.Binding{Scope: scope, QueryDigest: d, Order: pageOrder}, nil
}

type pagePosition struct {
	name string
	id   c.VariableID
}

func pageAfter(keys cursor.Keyring, token string, b cursor.Binding, gen int64) (*pagePosition, error) {
	if token == "" {
		return nil, nil
	}
	pos, e := keys.Verify(token, b)
	if e != nil {
		return nil, e
	}
	if len(pos.Scalars) != 2 || pos.Scalars[0].Kind() != "text" || pos.Scalars[1].Kind() != "uuid" || pos.OrderGeneration == nil || *pos.OrderGeneration < 1 {
		return nil, fault(f.CursorInvalid)
	}
	name := pos.Scalars[0].Value()
	id, e := f.ParseID[i.ProjectVariable](pos.Scalars[1].Value())
	if e != nil || c.ValidateName(name) != nil {
		return nil, fault(f.CursorInvalid)
	}
	if *pos.OrderGeneration != gen {
		return nil, fault(f.CursorStale)
	}
	return &pagePosition{name, id}, nil
}
func pageToken(keys cursor.Keyring, b cursor.Binding, gen int64, v c.VariableSummary) (string, error) {
	fields := v.Fields()
	name, e := cursor.Text(fields.Name)
	if e != nil {
		return "", e
	}
	id, e := cursor.UUID(fields.ID.String())
	if e != nil {
		return "", e
	}
	return keys.Sign(b, cursor.Position{Scalars: []cursor.Scalar{name, id}, OrderGeneration: &gen})
}
func (s *Service) ListVariables(ctx context.Context, a i.Actor, p c.ProjectID, q f.PageRequest) (f.Page[c.VariableSummary], error) {
	empty := f.Page[c.VariableSummary]{}
	ctx, _, done, e := s.beginProject(ctx, p, readCall)
	if e != nil {
		return empty, e
	}
	defer done()
	if e = readInput(a, p); e != nil {
		return empty, e
	}
	if q.Validate() != nil || q.Limit > c.MaxPageLimit || len(q.Cursor) > cursor.MaxTokenBytes {
		return empty, fault(f.InvalidArgument)
	}
	binding, e := pageBinding(p, a.Details().UserID)
	if e != nil {
		return empty, e
	}
	cause, e := readCause("list")
	if e != nil {
		return empty, e
	}
	st := s.state()
	out := f.Page[c.VariableSummary]{Items: []c.VariableSummary{}}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, readLocks(a, p)); e != nil {
			return portError(e)
		}
		if _, e := st.deps.Projects.RequireOwnerInTx(ctx, tx, a, p, i.Read); e != nil {
			return portError(e)
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		gen, e := generation(ctx, x, p)
		if e != nil {
			return e
		}
		after, e := pageAfter(st.deps.Cursors, q.Cursor, binding, gen)
		if e != nil {
			return e
		}
		const columns = `id::text,project_id::text,type,name,description,version,created_at,updated_at`
		query := `SELECT ` + columns + ` FROM agenteam_projectvariable.variables WHERE project_id=$1 AND type='variable' AND deleted_at IS NULL`
		args := []any{p.String(), q.Limit + 1}
		if after != nil {
			query += ` AND (name COLLATE "C",id)>($3::text COLLATE "C",$4::uuid)`
			args = append(args, after.name, after.id.String())
		}
		query += ` ORDER BY name COLLATE "C" ASC,id ASC LIMIT $2`
		rows, e := x.Query(ctx, query, args...)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		previous := after
		for rows.Next() {
			if e := ctx.Err(); e != nil {
				return canceled(e)
			}
			var id, project, typ, name, description string
			var version int64
			var created, updated time.Time
			if e := rows.Scan(&id, &project, &typ, &name, &description, &version, &created, &updated); e != nil {
				return unavailable(e)
			}
			v := c.VariableSummaryFields{Type: typ, Name: name, Description: description, Version: f.Version(version)}
			if v.ID, e = f.ParseID[i.ProjectVariable](id); e != nil {
				return internal(e)
			}
			if v.ProjectID, e = f.ParseID[i.Project](project); e != nil {
				return internal(e)
			}
			if v.CreatedAt, e = f.NewInstant(created); e != nil {
				return internal(e)
			}
			if v.UpdatedAt, e = f.NewInstant(updated); e != nil {
				return internal(e)
			}
			summary, e := c.NewVariableSummary(v)
			if e != nil || v.ProjectID != p {
				return internal(e)
			}
			if previous != nil && (v.Name < previous.name || v.Name == previous.name && v.ID.String() <= previous.id.String()) {
				return internal(nil)
			}
			previous = &pagePosition{v.Name, v.ID}
			out.Items = append(out.Items, summary)
			if len(out.Items) > q.Limit+1 {
				return internal(nil)
			}
		}
		if e := rows.Err(); e != nil {
			return unavailable(e)
		}
		if e := ctx.Err(); e != nil {
			return canceled(e)
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			out.NextCursor, e = pageToken(st.deps.Cursors, binding, gen, out.Items[len(out.Items)-1])
			if e != nil {
				return internal(e)
			}
		}
		return nil
	})
	if e = txError(result); e != nil {
		return empty, e
	}
	if e = ctx.Err(); e != nil {
		return empty, canceled(e)
	}
	return out, nil
}
