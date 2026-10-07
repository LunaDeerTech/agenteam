package project

import (
	"context"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Reader reads existing Project facts under current Session/Owner authority.
// It has no initialization, command, background work or lifecycle dependency.
type Reader struct {
	store     Store
	authority *Authority
	cursors   cursor.Keyring
}

// NewReader is pure. Store identity is the original Authority's exact Store,
// not another handle to the same database or a caller-supplied authority grant.
func NewReader(store Store, authority *Authority, cursors cursor.Keyring) (*Reader, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) {
		return nil, fault(foundation.DependencyUnbound)
	}
	if cursors.Validate() != nil {
		return nil, invalid()
	}
	return &Reader{store: store, authority: authority, cursors: cursors}, nil
}
func (r *Reader) bound() bool {
	return r != nil && !nilPort(r.store) && r.authority.state() != nil && r.cursors.Validate() == nil
}
func (r *Reader) GetProject(ctx context.Context, actor identity.Actor, id c.ProjectID) (c.ProjectRef, error) {
	if !r.bound() {
		return c.ProjectRef{}, fault(foundation.DependencyUnbound)
	}
	return readProject(ctx, r.store, r.authority, actor, id)
}
func (r *Reader) ListOwnedProjects(ctx context.Context, actor identity.Actor, request c.ListOwnedProjectsRequest, page foundation.PageRequest) (foundation.Page[c.ProjectListItem], error) {
	if !r.bound() {
		return foundation.Page[c.ProjectListItem]{}, fault(foundation.DependencyUnbound)
	}
	return readOwnedProjects(ctx, r.store, r.authority, r.cursors, actor, request, page)
}

func readProject(ctx context.Context, store Store, authority *Authority, actor identity.Actor, id c.ProjectID) (c.ProjectRef, error) {
	if e := ctx.Err(); e != nil {
		return c.ProjectRef{}, e
	}
	if e := human(actor); e != nil {
		return c.ProjectRef{}, e
	}
	if id.Validate() != nil {
		return c.ProjectRef{}, invalid()
	}
	cause, e := readCause("get")
	if e != nil {
		return c.ProjectRef{}, e
	}
	var ref c.ProjectRef
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared), projectLock(id, foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		access, e := authority.RequireOwnerInTx(ctx, tx, actor, id, identity.Read)
		if e != nil {
			return e
		}
		ref = access.Project()
		return nil
	})
	if e = commitError(result); e != nil {
		return c.ProjectRef{}, e
	}
	if err := ctx.Err(); err != nil {
		return c.ProjectRef{}, err
	}
	return ref, nil
}
func readOwnedProjects(ctx context.Context, store Store, authority *Authority, cursors cursor.Keyring, actor identity.Actor, request c.ListOwnedProjectsRequest, page foundation.PageRequest) (foundation.Page[c.ProjectListItem], error) {
	empty := foundation.Page[c.ProjectListItem]{}
	if e := ctx.Err(); e != nil {
		return empty, e
	}
	if e := human(actor); e != nil {
		return empty, e
	}
	if e := c.ValidateProjectPage(page); e != nil {
		return empty, e
	}
	filter, e := request.NormalizedFilter()
	if e != nil {
		return empty, e
	}
	owner, e := parseID[identity.User](actor.Details().UserID)
	if e != nil {
		return empty, e
	}
	query, e := c.OwnedProjectsQueryDigest(owner, request)
	if e != nil {
		return empty, e
	}
	binding := cursor.Binding{Scope: identity.SystemScope(), QueryDigest: query, Order: "created_at-desc,id-desc"}
	var afterTime any
	var afterID any
	if page.Cursor != "" {
		position, e := cursors.Verify(page.Cursor, binding)
		if e != nil {
			return empty, e
		}
		if len(position.Scalars) != 2 || position.OrderGeneration != nil || position.Scalars[0].Kind() != "instant" || position.Scalars[1].Kind() != "uuid" {
			return empty, fault(foundation.CursorInvalid)
		}
		t, e := foundation.ParseInstant(position.Scalars[0].Value())
		if e != nil {
			return empty, fault(foundation.CursorInvalid)
		}
		id, e := foundation.ParseID[identity.Project](position.Scalars[1].Value())
		if e != nil {
			return empty, fault(foundation.CursorInvalid)
		}
		afterTime, afterID = t.Time(), id.String()
	}
	names := make([]string, len(filter))
	for i, v := range filter {
		names[i] = string(v)
	}
	cause, e := readCause("list")
	if e != nil {
		return empty, e
	}
	output := empty
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		if e := authority.state().sessions.RequireCurrentSession(ctx, tx, actor); e != nil {
			return portError(e)
		}
		x, e := store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		rows, e := x.Query(ctx, `SELECT `+projectColumns+` FROM agenteam_project.projects WHERE owner_user_id=$1 AND initialized_at IS NOT NULL AND lifecycle=ANY($2::text[]) AND ($3::timestamptz IS NULL OR (created_at,id)<($3::timestamptz,$4::uuid)) ORDER BY created_at DESC,id DESC LIMIT $5`, owner.String(), names, afterTime, afterID, page.Limit+1)
		if e != nil {
			return unavailable(e)
		}
		if rows == nil {
			return unavailable(nil)
		}
		output, e = readProjectRows(rows, owner, filter, page, cursors, binding)
		if e != nil {
			return e
		}
		return nil
	})
	if e = commitError(result); e != nil {
		return empty, e
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return output, nil
}

// This private iterator seam preserves ownership of the real PostgreSQL Rows.
// Controlled tests exercise corrupt final/sentinel rows and actual Close tails
// without introducing a public storage or authorization substitute.
type projectReadRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close()
}

func readProjectRows(rows projectReadRows, owner identity.UserID, filter []c.Lifecycle, page foundation.PageRequest, cursors cursor.Keyring, binding cursor.Binding) (foundation.Page[c.ProjectListItem], error) {
	closed := false
	closeRows := func() {
		if !closed {
			closed = true
			rows.Close()
		}
	}
	defer closeRows()
	var output foundation.Page[c.ProjectListItem]
	var e error
	refs := []*projectRecord{}
	for rows.Next() {
		r, e := scanProject(rows)
		if e != nil {
			return foundation.Page[c.ProjectListItem]{}, e
		}
		if r == nil || len(refs) >= page.Limit+1 || r.ref.OwnerUserID != owner || !r.initialized || !slices.Contains(filter, r.ref.Lifecycle) {
			return foundation.Page[c.ProjectListItem]{}, unavailable(nil)
		}
		if len(refs) > 0 {
			previous := refs[len(refs)-1].ref
			if previous.CreatedAt.Time().Before(r.ref.CreatedAt.Time()) || previous.CreatedAt.Time().Equal(r.ref.CreatedAt.Time()) && previous.ID.String() <= r.ref.ID.String() {
				return foundation.Page[c.ProjectListItem]{}, unavailable(nil)
			}
		}
		// Validate the complete sentinel before deciding whether to discard it.
		if _, e := projectListItem(r); e != nil {
			return foundation.Page[c.ProjectListItem]{}, e
		}
		refs = append(refs, r)
	}
	closeRows()
	if e = rows.Err(); e != nil {
		return foundation.Page[c.ProjectListItem]{}, unavailable(e)
	}
	more := len(refs) > page.Limit
	if more {
		refs = refs[:page.Limit]
	}
	output.Items = make([]c.ProjectListItem, 0, len(refs))
	for _, r := range refs {
		item, e := projectListItem(r)
		if e != nil {
			return foundation.Page[c.ProjectListItem]{}, e
		}
		output.Items = append(output.Items, item)
	}
	if more {
		last := refs[len(refs)-1]
		t, _ := cursor.Instant(last.ref.CreatedAt)
		id, _ := cursor.UUID(last.ref.ID.String())
		output.NextCursor, e = cursors.Sign(binding, cursor.Position{Scalars: []cursor.Scalar{t, id}})
		if e != nil {
			return foundation.Page[c.ProjectListItem]{}, e
		}
	}
	return output, nil
}

// A retained historical operation pointer on active/archived is not a public
// operation. Deleting never exposes the stored description.
func projectListItem(r *projectRecord) (c.ProjectListItem, error) {
	item := c.ProjectListItem{ID: r.ref.ID, Name: r.ref.Name, Lifecycle: r.ref.Lifecycle, Version: r.ref.Version}
	if r.ref.Lifecycle != c.Deleting {
		description := r.ref.Description
		item.Description = &description
	}
	if r.ref.Lifecycle == c.Archiving || r.ref.Lifecycle == c.Deleting {
		if r.operation != nil {
			operation := *r.operation
			item.OperationID = &operation
		}
	}
	if item.Validate() != nil {
		return c.ProjectListItem{}, unavailable(nil)
	}
	return item, nil
}
