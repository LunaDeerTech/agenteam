package service

import (
	"context"
	"errors"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/jackc/pgx/v5"
)

// ConfigurationDirectory is a same-transaction Project read port. It does not
// inherit the system administrator Reader or carry a grant between requests.
type ConfigurationDirectory struct {
	store    Store
	projects pc.ProjectAuthority
}

func NewConfigurationDirectory(store Store, projects pc.ProjectAuthority) (*ConfigurationDirectory, error) {
	if nilPort(store) || nilPort(projects) || !reflect.TypeOf(store).Comparable() {
		return nil, fault(f.DependencyUnbound)
	}
	return &ConfigurationDirectory{store, projects}, nil
}
func (d *ConfigurationDirectory) ConfigurationCandidateLocks(ids []c.RunnerID) ([]f.LockRequest, error) {
	if d == nil || nilPort(d.store) {
		return nil, fault(f.DependencyUnbound)
	}
	return c.ConfigurationCandidateLockRequests(ids)
}
func (d *ConfigurationDirectory) executor(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, ids []c.RunnerID) (postgres.SQLExecutor, error) {
	if ctx == nil {
		return nil, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if actor.Validate() != nil || actor.Details().Kind != i.Human {
		return nil, fault(f.Unauthenticated)
	}
	if project.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	held, err := d.ConfigurationCandidateLocks(ids)
	if err != nil {
		return nil, err
	}
	user, _ := f.UserLock(actor.Details().UserID)
	p, _ := f.ProjectLock(project.String())
	held = append(held, f.LockRequest{Key: user, Mode: f.Shared}, f.LockRequest{Key: p, Mode: f.Shared})
	x, err := d.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if err = d.store.RequireHeldLocks(ctx, tx, held); err != nil {
		return nil, portError(err)
	}
	grant, err := d.projects.RequireOwnerInTx(ctx, tx, actor, project, i.Read)
	if err != nil {
		return nil, portError(err)
	}
	if !grant.Matches(actor, project) {
		return nil, fault(f.Forbidden)
	}
	return x, nil
}

const configurationColumns = `r.id::text,r.name,r.description,r.version,
 CASE WHEN r.incompatible THEN 'incompatible' WHEN c.hello_at IS NOT NULL AND c.lease_expires_at>clock_timestamp()
 AND c.credential_generation=r.credential_generation AND c.generation=r.connection_generation THEN 'online' ELSE 'offline' END`

func scanConfigurationCandidate(row postgres.Row) (c.ConfigurationCandidate, error) {
	var out c.ConfigurationCandidate
	var raw, status string
	var version int64
	if err := row.Scan(&raw, &out.Name, &out.Description, &version, &status); err != nil {
		return out, err
	}
	var err error
	out.ID, err = f.ParseID[c.Runner](raw)
	if err != nil {
		return c.ConfigurationCandidate{}, unavailable(nil)
	}
	out.Version, out.Status = f.Version(version), c.Status(status)
	if out.Validate() != nil {
		return c.ConfigurationCandidate{}, unavailable(nil)
	}
	return out, nil
}
func (d *ConfigurationDirectory) RequireConfigurationCandidatesInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, ids []c.RunnerID) ([]c.ConfigurationCandidate, error) {
	x, err := d.executor(ctx, tx, actor, project, ids)
	if err != nil {
		return nil, err
	}
	out := make([]c.ConfigurationCandidate, 0, len(ids))
	for _, id := range ids {
		candidate, err := scanConfigurationCandidate(x.QueryRow(ctx, `SELECT `+configurationColumns+` FROM `+runnerTables+` WHERE r.id=$1`, id.String()))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fault(f.NotFound)
		}
		if err != nil {
			return nil, portError(err)
		}
		if candidate.ID != id {
			return nil, unavailable(nil)
		}
		out = append(out, candidate)
	}
	return out, ctx.Err()
}

// List requires the global management SH gate, which serializes registration
// and metadata edits. Status is a current observation, not reference validity.
func (d *ConfigurationDirectory) ListConfigurationCandidatesInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, after *c.RunnerID, limit int) ([]c.ConfigurationCandidate, error) {
	if limit < 1 || limit > 51 || after != nil && after.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	x, err := d.executor(ctx, tx, actor, project, []c.RunnerID{})
	if err != nil {
		return nil, err
	}
	var start any
	if after != nil {
		start = after.String()
	}
	rows, err := x.Query(ctx, `SELECT `+configurationColumns+` FROM `+runnerTables+` WHERE ($1::uuid IS NULL OR r.id>$1) ORDER BY r.id LIMIT $2`, start, limit)
	if err != nil {
		return nil, portError(err)
	}
	defer rows.Close()
	out := make([]c.ConfigurationCandidate, 0, limit)
	for rows.Next() {
		v, err := scanConfigurationCandidate(rows)
		if err != nil {
			return nil, portError(err)
		}
		if len(out) > 0 && out[len(out)-1].ID.String() >= v.ID.String() {
			return nil, unavailable(nil)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, portError(err)
	}
	return out, ctx.Err()
}

var _ c.ConfigurationCandidates = (*ConfigurationDirectory)(nil)
