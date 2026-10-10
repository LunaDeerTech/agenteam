package service

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

func locks(actor id.Actor, target *c.RunnerID, command *f.CommandIdentity, write bool) ([]f.LockRequest, error) {
	out := []f.LockRequest{}
	if command != nil {
		k, e := f.CommandLock(*command)
		if e != nil {
			return nil, fault(f.InvalidArgument)
		}
		out = append(out, f.LockRequest{Key: k, Mode: f.Exclusive})
	}
	mode := f.Shared
	if write {
		mode = f.Exclusive
	}
	global, _ := f.SystemConfigLock("runner-management")
	out = append(out, f.LockRequest{Key: global, Mode: mode})
	if target != nil {
		k, e := f.SystemConfigLock("runner-control-" + target.String())
		if e != nil {
			return nil, fault(f.InvalidArgument)
		}
		out = append(out, f.LockRequest{Key: k, Mode: mode})
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, fault(f.Unauthenticated)
	}
	user, e := f.UserLock(actor.Details().UserID)
	if e != nil {
		return nil, fault(f.Unauthenticated)
	}
	out = append(out, f.LockRequest{Key: user, Mode: f.Shared})
	sort.Slice(out, func(i, j int) bool { return f.CompareLockKeys(out[i].Key, out[j].Key) < 0 })
	return out, nil
}
func (a *Authority) authorize(ctx context.Context, tx f.Tx, actor id.Actor, intent id.AccessIntent) error {
	grant, e := a.state().system.AuthorizeSystem(ctx, tx, actor, intent)
	if e != nil {
		return portError(e)
	}
	if !grant.Matches(actor, id.SystemScope(), intent) {
		return fault(f.Forbidden)
	}
	return nil
}
func (s *Service) read(ctx context.Context, actor id.Actor, target *c.RunnerID, fn func(context.Context, postgres.SQLExecutor) error) error {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return e
	}
	defer done()
	held, e := locks(actor, target, nil, false)
	if e != nil {
		return e
	}
	run, e := f.NewID[struct{}]()
	if e != nil {
		return unavailable(e)
	}
	cause, e := f.NewRecoveryCause("runner-reader", run.String(), "")
	if e != nil {
		return unavailable(e)
	}
	st := s.state().authority.state()
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, held); e != nil {
			return portError(e)
		}
		if e := s.state().authority.authorize(ctx, tx, actor, id.Read); e != nil {
			return e
		}
		x, e := st.store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		return fn(ctx, x)
	})
	return resultError(result)
}

const runnerColumns = `r.id::text,r.name,r.description,r.tags,r.root_path,r.version,r.credential_generation,
 r.device_public_key,r.enrolled_at,r.last_seen_at,r.last_hello,r.created_at,r.updated_at,
 CASE WHEN r.incompatible THEN 'incompatible' WHEN c.hello_at IS NOT NULL AND c.lease_expires_at>clock_timestamp()
 AND c.credential_generation=r.credential_generation AND c.generation=r.connection_generation THEN 'online' ELSE 'offline' END`
const runnerTables = `agenteam_runner.runners r LEFT JOIN agenteam_runner.connections c ON c.runner_id=r.id`

func scanRunner(row postgres.Row) (c.Snapshot, error) {
	var out c.Snapshot
	var rid, status string
	var tags, public, hello []byte
	var version, generation int64
	var enrolled, seen *time.Time
	var created, updated time.Time
	if e := row.Scan(&rid, &out.Name, &out.Description, &tags, &out.RootPath, &version, &generation, &public, &enrolled, &seen, &hello, &created, &updated, &status); e != nil {
		return out, e
	}
	var e error
	out.ID, e = f.ParseID[c.Runner](rid)
	if e != nil {
		return out, unavailable(e)
	}
	out.Version = f.Version(version)
	out.CredentialGeneration = f.Version(generation)
	out.Status = c.Status(status)
	out.CreatedAt, e = f.NewInstant(created)
	if e != nil {
		return out, unavailable(e)
	}
	out.UpdatedAt, e = f.NewInstant(updated)
	if e != nil {
		return out, unavailable(e)
	}
	if json.Unmarshal(tags, &out.Tags) != nil {
		return out, unavailable(nil)
	}
	if public != nil {
		if len(public) != 32 {
			return out, unavailable(nil)
		}
		var key [32]byte
		copy(key[:], public)
		fp := f.Digest(p.PublicKeyFingerprint(key))
		out.PublicKeyFingerprint = &fp
	}
	if enrolled != nil {
		v, e := f.NewInstant(*enrolled)
		if e != nil {
			return out, unavailable(e)
		}
		out.EnrolledAt = &v
	}
	if seen != nil {
		v, e := f.NewInstant(*seen)
		if e != nil {
			return out, unavailable(e)
		}
		out.LastSeenAt = &v
	}
	if hello != nil {
		var h c.HelloSnapshot
		if json.Unmarshal(hello, &h) != nil {
			return out, unavailable(nil)
		}
		out.LastHello = &h
	}
	if out.Validate() != nil {
		return out, unavailable(nil)
	}
	return out, nil
}
func loadRunner(ctx context.Context, x postgres.SQLExecutor, target c.RunnerID) (c.Snapshot, error) {
	v, e := scanRunner(x.QueryRow(ctx, `SELECT `+runnerColumns+` FROM `+runnerTables+` WHERE r.id=$1`, target.String()))
	if errors.Is(e, pgx.ErrNoRows) {
		return c.Snapshot{}, fault(f.NotFound)
	}
	if e != nil {
		return c.Snapshot{}, portError(e)
	}
	return v, nil
}
func (s *Service) Get(ctx context.Context, actor id.Actor, target c.RunnerID) (c.Snapshot, error) {
	if target.Validate() != nil {
		return c.Snapshot{}, fault(f.InvalidArgument)
	}
	var out c.Snapshot
	e := s.read(ctx, actor, &target, func(ctx context.Context, x postgres.SQLExecutor) error {
		var e error
		out, e = loadRunner(ctx, x, target)
		return e
	})
	if e != nil {
		return c.Snapshot{}, e
	}
	return out, nil
}
func (s *Service) List(ctx context.Context, actor id.Actor, q c.ListRequest) (c.Page, error) {
	if q.Validate() != nil {
		return c.Page{}, fault(f.InvalidArgument)
	}
	var after any
	if q.After != nil {
		after = q.After.String()
	}
	out := c.Page{Items: []c.Snapshot{}}
	e := s.read(ctx, actor, nil, func(ctx context.Context, x postgres.SQLExecutor) error {
		rows, e := x.Query(ctx, `SELECT `+runnerColumns+` FROM `+runnerTables+` WHERE ($1::uuid IS NULL OR r.id>$1::uuid) ORDER BY r.id LIMIT $2`, after, q.Limit+1)
		if e != nil {
			return unavailable(e)
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanRunner(rows)
			if e != nil {
				return portError(e)
			}
			out.Items = append(out.Items, v)
		}
		if rows.Err() != nil {
			return unavailable(rows.Err())
		}
		out, e = boundedPage(out.Items, q.Limit)
		return e
	})
	if e != nil {
		return c.Page{}, e
	}
	return out, nil
}

// The byte budget can produce a shorter page than limit. Its last returned ID
// remains the keyset position; the extra loaded row is never skipped.
func boundedPage(items []c.Snapshot, limit int) (c.Page, error) {
	if limit < 1 || limit > 200 {
		return c.Page{}, fault(f.InvalidArgument)
	}
	size, count := 128, 0
	for count < len(items) && count < limit {
		raw, e := json.Marshal(items[count])
		if e != nil {
			return c.Page{}, unavailable(e)
		}
		if size+len(raw)+1 > c.MaxPageBytes {
			break
		}
		size += len(raw) + 1
		count++
	}
	if count == 0 && len(items) > 0 {
		return c.Page{}, unavailable(nil)
	}
	out := c.Page{Items: append([]c.Snapshot{}, items[:count]...)}
	if count < len(items) {
		last := out.Items[count-1].ID
		out.Next = &last
	}
	if out.Validate() != nil {
		return c.Page{}, unavailable(nil)
	}
	return out, nil
}
