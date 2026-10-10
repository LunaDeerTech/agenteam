package skill

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// OwnerCatalog is an additional paginated view over the same Service. The
// original one-builtin ListSkills contract and the Service lifecycle stay intact.
type OwnerCatalog struct {
	service *Service
	keys    cursor.Keyring
}

type OwnerCatalogQuery struct {
	Limit  int
	Cursor string
}

type OwnerCatalogPage struct {
	Items      []sc.Metadata
	NextCursor string
}

func NewOwnerCatalog(service *Service, keys cursor.Keyring) (*OwnerCatalog, error) {
	if service.state() == nil || keys.Validate() != nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &OwnerCatalog{service: service, keys: keys}, nil
}

func catalogBinding(actor id.Actor, project id.ProjectID, limit int) (cursor.Binding, error) {
	if actor.Validate() != nil || actor.Details().Kind != id.Human || project.Validate() != nil || limit < 1 || limit > 100 {
		return cursor.Binding{}, invalid()
	}
	scope, err := id.InProject(project)
	if err != nil {
		return cursor.Binding{}, err
	}
	raw, err := json.Marshal(struct {
		View, User string
		Limit      int
	}{"skill-owner-catalog-v1", actor.Details().UserID, limit})
	if err != nil {
		return cursor.Binding{}, unavailable(err)
	}
	digest, err := cursor.Digest(raw)
	if err != nil {
		return cursor.Binding{}, err
	}
	return cursor.Binding{Scope: scope, QueryDigest: digest, Order: "id:asc"}, nil
}

// Each page is a current-authorized, committed read, not a cross-request
// snapshot. The cursor is a position only; it never grants access. Project SH
// serializes this page with installation publication and cleanup's Project EX.
func (c *OwnerCatalog) List(ctx context.Context, actor id.Actor, project id.ProjectID, query OwnerCatalogQuery) (OwnerCatalogPage, error) {
	if c == nil || c.service.state() == nil || c.keys.Validate() != nil {
		return OwnerCatalogPage{}, fault(f.DependencyUnbound)
	}
	if query.Limit < 1 || query.Limit > 100 || len(query.Cursor) > cursor.MaxTokenBytes {
		return OwnerCatalogPage{}, invalid()
	}
	binding, err := catalogBinding(actor, project, query.Limit)
	if err != nil {
		return OwnerCatalogPage{}, err
	}
	var after string
	if query.Cursor != "" {
		position, err := c.keys.Verify(query.Cursor, binding)
		if err != nil {
			return OwnerCatalogPage{}, err
		}
		if position.OrderGeneration != nil || len(position.Scalars) != 1 || position.Scalars[0].Kind() != "uuid" {
			return OwnerCatalogPage{}, fault(f.CursorInvalid)
		}
		after = position.Scalars[0].Value()
		parsed, err := f.ParseID[pc.Skill](after)
		if err != nil || parsed.String() != after {
			return OwnerCatalogPage{}, fault(f.CursorInvalid)
		}
	}
	call, err := c.service.begin(ctx, false)
	if err != nil {
		return OwnerCatalogPage{}, err
	}
	defer c.service.end(call)
	page := OwnerCatalogPage{Items: []sc.Metadata{}}
	err = c.service.ownerReadTx(call.ctx, actor, project, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, initialized initializationRow) error {
		// A real protected publication is still required even if its ID falls
		// before this page. An empty catalogue cannot manufacture readiness.
		if _, _, err := loadPublished(ctx, x, initialized); err != nil {
			return err
		}
		ids, err := catalogIDs(ctx, x, project, after, query.Limit+1)
		if err != nil {
			return err
		}
		more := len(ids) > query.Limit
		if more {
			ids = ids[:query.Limit]
		}
		for _, target := range ids {
			var metadata sc.Metadata
			if target == initialized.skill {
				metadata, _, err = loadPublished(ctx, x, initialized)
			} else {
				var installed *installationRow
				installed, err = loadInstallationSkill(ctx, x, project, target)
				if err == nil && (installed == nil || installed.phase != installationPublished) {
					err = unavailable(nil)
				}
				if err == nil {
					metadata, _, err = loadInstalled(ctx, x, *installed)
				}
			}
			if err != nil {
				return err
			}
			page.Items = append(page.Items, metadata)
		}
		if more {
			last, err := cursor.UUID(ids[len(ids)-1].String())
			if err != nil {
				return unavailable(err)
			}
			page.NextCursor, err = c.keys.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{last}})
			return err
		}
		return nil
	})
	if err != nil {
		// Even a populated local candidate is discarded on physical Unknown.
		return OwnerCatalogPage{}, err
	}
	return page, nil
}

func catalogIDs(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, after string, limit int) ([]sc.SkillID, error) {
	rows, err := x.Query(ctx, `SELECT id::text FROM agenteam_skill.skills WHERE project_id=$1 AND serving=true AND ($2::uuid IS NULL OR id>$2::uuid) ORDER BY id ASC LIMIT $3`, project.String(), nilIfEmpty(after), limit)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	ids := make([]sc.SkillID, 0, limit)
	previous := after
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, unavailable(err)
		}
		target, err := f.ParseID[pc.Skill](raw)
		if err != nil || target.String() != raw || strings.Compare(raw, previous) <= 0 || len(ids) >= limit {
			return nil, unavailable(nil)
		}
		ids, previous = append(ids, target), raw
	}
	if err := rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	return ids, nil
}

func nilIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
