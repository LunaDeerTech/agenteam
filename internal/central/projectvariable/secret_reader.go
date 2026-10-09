package projectvariable

import (
	"context"
	"encoding/json"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

func (s *SecretService) readTx(ctx context.Context, actor i.Actor, project c.ProjectID, name string, fn func(context.Context, postgres.SQLExecutor) error) error {
	st := s.state()
	cause, err := readCause("secret_" + name)
	if err != nil {
		return err
	}
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		locks := readLocks(actor, project)
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		access, err := st.deps.Projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
		if err != nil {
			return portError(err)
		}
		if access.Project().ID != project {
			return internal(nil)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, x)
	})
	if err = txError(result); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return canceled(err)
	}
	return nil
}
func (s *SecretService) GetSecretVariable(ctx context.Context, actor i.Actor, project c.ProjectID, id c.VariableID) (c.SecretVariable, error) {
	empty := c.SecretVariable{}
	ctx, _, done, err := s.begin(ctx)
	if err != nil {
		return empty, err
	}
	defer done()
	if err = readInput(actor, project); err != nil {
		return empty, err
	}
	if id.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	var out c.SecretVariable
	err = s.readTx(ctx, actor, project, "get", func(ctx context.Context, x postgres.SQLExecutor) error {
		row, err := loadSecretVariable(ctx, x, project, id, false)
		if err != nil {
			return err
		}
		if row.Variable.Fields().ID != id || row.Variable.Fields().ProjectID != project {
			return internal(nil)
		}
		out = row.Variable
		return nil
	})
	if err != nil {
		return empty, err
	}
	return out, nil
}
func secretPageBinding(project c.ProjectID, owner string) (cursor.Binding, error) {
	raw, err := json.Marshal(struct {
		Format  int
		Kind    string
		Project c.ProjectID
		Owner   string
		Type    string
	}{1, "project.secret_variables", project, owner, c.SecretVariableType})
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	scope, err := i.InProject(project)
	if err != nil {
		return cursor.Binding{}, internal(err)
	}
	return cursor.Binding{Scope: scope, QueryDigest: digest, Order: pageOrder}, nil
}
func secretPageToken(keys cursor.Keyring, binding cursor.Binding, generation int64, value c.SecretVariable) (string, error) {
	v := value.Fields()
	name, err := cursor.Text(v.Name)
	if err != nil {
		return "", err
	}
	id, err := cursor.UUID(v.ID.String())
	if err != nil {
		return "", err
	}
	return keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{name, id}, OrderGeneration: &generation})
}
func (s *SecretService) ListSecretVariables(ctx context.Context, actor i.Actor, project c.ProjectID, q f.PageRequest) (f.Page[c.SecretVariable], error) {
	empty := f.Page[c.SecretVariable]{}
	ctx, _, done, err := s.begin(ctx)
	if err != nil {
		return empty, err
	}
	defer done()
	if err = readInput(actor, project); err != nil {
		return empty, err
	}
	if q.Validate() != nil || q.Limit > c.MaxPageLimit || len(q.Cursor) > cursor.MaxTokenBytes {
		return empty, fault(f.InvalidArgument)
	}
	binding, err := secretPageBinding(project, actor.Details().UserID)
	if err != nil {
		return empty, err
	}
	st := s.state()
	out := f.Page[c.SecretVariable]{Items: []c.SecretVariable{}}
	err = s.readTx(ctx, actor, project, "list", func(ctx context.Context, x postgres.SQLExecutor) error {
		generation, err := secretGeneration(ctx, x, project)
		if err != nil {
			return err
		}
		after, err := pageAfter(st.deps.Cursors, q.Cursor, binding, generation)
		if err != nil {
			return err
		}
		query := `SELECT ` + secretVariableColumns + ` FROM agenteam_projectvariable.variables WHERE project_id=$1 AND type='secret' AND deleted_at IS NULL`
		args := []any{project.String(), q.Limit + 1}
		if after != nil {
			query += ` AND (name COLLATE "C",id)>($3::text COLLATE "C",$4::uuid)`
			args = append(args, after.name, after.id.String())
		}
		query += ` ORDER BY name COLLATE "C" ASC,id ASC LIMIT $2`
		rows, err := x.Query(ctx, query, args...)
		if err != nil {
			return unavailable(err)
		}
		defer rows.Close()
		previous := after
		for rows.Next() {
			if err = ctx.Err(); err != nil {
				return canceled(err)
			}
			row, err := scanSecretVariable(rows)
			if err != nil {
				return err
			}
			v := row.Variable.Fields()
			if row.Deleted != nil || v.ProjectID != project || previous != nil && (v.Name < previous.name || v.Name == previous.name && v.ID.String() <= previous.id.String()) {
				return internal(nil)
			}
			previous = &pagePosition{v.Name, v.ID}
			out.Items = append(out.Items, row.Variable)
			if len(out.Items) > q.Limit+1 {
				return internal(nil)
			}
		}
		if err = rows.Err(); err != nil {
			return unavailable(err)
		}
		if err = ctx.Err(); err != nil {
			return canceled(err)
		}
		if len(out.Items) > q.Limit {
			out.Items = out.Items[:q.Limit]
			out.NextCursor, err = secretPageToken(st.deps.Cursors, binding, generation, out.Items[len(out.Items)-1])
			if err != nil {
				return internal(err)
			}
		}
		raw, err := json.Marshal(out)
		if err != nil || len(raw) > c.MaxSecretListBytes {
			return internal(err)
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	return out, nil
}

func (s *SecretService) LookupSecretVariableCommand(ctx context.Context, actor i.Actor, q c.SecretVariableCommandLookupRequest) (c.SecretVariableCommandLookup, error) {
	empty := c.SecretVariableCommandLookup{}
	ctx, _, done, err := s.begin(ctx)
	if err != nil {
		return empty, err
	}
	defer done()
	if q.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	if err = readInput(actor, q.Fields().ProjectID); err != nil {
		return empty, err
	}
	request, err := secretLookupWriteRequest(actor, q)
	if err != nil {
		return empty, err
	}
	r := request.Fields()
	st := s.state()
	// Absence is observed under the original command lock and current Read.
	// It must not require a current live target, expected version or Mutate.
	var saved *secretCommandRecord
	result := st.store.WithinTx(ctx, commandCause(r.Identity), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, secretRequestBaseLocks(request)); err != nil {
			return portError(err)
		}
		x, err := st.deps.Writes.current(ctx, tx, request, i.Read)
		if err != nil {
			return err
		}
		saved, err = loadSecretCommand(ctx, x, r.ProjectID, secretRequestCommand(request), r.Identity.Key())
		if err != nil {
			return err
		}
		if saved != nil && !secretRecordMatchesRequest(saved, request) {
			return fault(f.IdempotencyKeyReused)
		}
		return nil
	})
	if err = txError(result); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, canceled(err)
	}
	if saved == nil {
		return c.NewSecretVariableCommandLookup(c.SecretLookupNotObserved, nil)
	}
	plan, err := st.deps.Writes.Discover(ctx, request)
	if err != nil {
		return empty, err
	}
	locks, err := plan.RequiredLocks()
	if err != nil {
		return empty, portError(err)
	}
	var out c.SecretVariableCommandLookup
	result = st.store.WithinTx(ctx, commandCause(r.Identity), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		observation, err := st.deps.Secrets.LookupProjectVariableWriteInTx(ctx, tx, request, plan)
		if err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		record, err := loadSecretCommand(ctx, x, r.ProjectID, secretRequestCommand(request), r.Identity.Key())
		if err != nil {
			return err
		}
		if record == nil || !secretRecordMatchesRequest(record, request) || !sameSecretObservation(record.Observation, observation) {
			return internal(nil)
		}
		out, err = c.NewSecretVariableCommandLookup(c.SecretLookupCommitted, &record.Receipt)
		return err
	})
	if err = txError(result); err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, canceled(err)
	}
	return out, nil
}

var _ c.SecretQueries = (*SecretService)(nil)
var _ interface {
	LookupSecretVariableCommand(context.Context, i.Actor, c.SecretVariableCommandLookupRequest) (c.SecretVariableCommandLookup, error)
} = (*SecretService)(nil)
