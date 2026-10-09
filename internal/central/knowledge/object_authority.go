package knowledge

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	ob "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

// Authority supplies Knowledge facts to Object. Discovery only identifies the
// lock union; it neither grants access nor substitutes for current Owner checks.
type Authority struct{ data func() *authorityState }
type authorityState struct {
	store    Store
	projects pc.ProjectAuthority
}

func NewAuthority(store Store, projects pc.ProjectAuthority) (*Authority, error) {
	if nilPort(store) || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	st := &authorityState{store: store, projects: projects}
	return &Authority{data: func() *authorityState { return st }}, nil
}
func (a *Authority) state() *authorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}

func knowledgeOwner(owner oc.ObjectOwner) (id.ProjectID, kc.DocumentID, error) {
	if owner.Validate() != nil {
		return id.ProjectID{}, kc.DocumentID{}, fault(f.InvalidArgument)
	}
	d := owner.Details()
	if d.Kind != oc.Knowledge {
		return id.ProjectID{}, kc.DocumentID{}, fault(f.DependencyUnbound)
	}
	p, err := f.ParseID[id.Project](d.ProjectID)
	if err != nil {
		return id.ProjectID{}, kc.DocumentID{}, fault(f.InvalidArgument)
	}
	k, err := f.ParseID[kc.Document](d.ID)
	if err != nil {
		return id.ProjectID{}, kc.DocumentID{}, fault(f.InvalidArgument)
	}
	return p, k, nil
}

func authorityLocks(actor id.Actor, project id.ProjectID, intent id.AccessIntent) ([]f.LockRequest, error) {
	if actor.Validate() == nil && actor.Details().Kind == id.Human {
		return scopeLocks(actor, project, intent != id.Read)
	}
	p, err := f.ProjectLock(project.String())
	if err != nil {
		return nil, portError(err)
	}
	t, err := f.KnowledgeTreeLock(project.String())
	if err != nil {
		return nil, portError(err)
	}
	return ob.NormalizeLocks([]f.LockRequest{{Key: p, Mode: f.Shared}, {Key: t, Mode: f.Exclusive}})
}

func (a *Authority) ownerScope(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (postgres.SQLExecutor, id.ProjectID, kc.DocumentID, error) {
	st := a.state()
	if st == nil {
		return nil, id.ProjectID{}, kc.DocumentID{}, fault(f.DependencyUnbound)
	}
	p, k, err := knowledgeOwner(owner)
	if err != nil {
		return nil, p, k, err
	}
	if err = readInput(ctx, actor, p); err != nil {
		return nil, p, k, err
	}
	if intent != id.Read && intent != id.Mutate {
		return nil, p, k, fault(f.Forbidden)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return nil, p, k, portError(err)
	}
	locks, err := authorityLocks(actor, p, intent)
	if err != nil {
		return nil, p, k, err
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, p, k, portError(err)
	}
	grant, err := st.projects.RequireOwnerInTx(ctx, tx, actor, p, intent)
	if err != nil {
		return nil, p, k, portError(err)
	}
	if !grant.Matches(actor, p) {
		return nil, p, k, internal(nil)
	}
	return x, p, k, nil
}

func (a *Authority) AuthorizeOwner(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	st := a.state()
	if st == nil {
		return oc.OwnerAuthorization{}, fault(f.DependencyUnbound)
	}
	p, _, err := knowledgeOwner(owner)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if err = readInput(ctx, actor, p); err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if intent != id.Read && intent != id.Mutate {
		return oc.OwnerAuthorization{}, fault(f.Forbidden)
	}
	locks, err := authorityLocks(actor, p, intent)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	cause, err := readCause()
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	var out oc.OwnerAuthorization
	r := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		var err error
		out, err = a.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
		return err
	})
	if err = txError(r); err != nil {
		return oc.OwnerAuthorization{}, err
	}
	return out, nil
}

func (a *Authority) AuthorizeOwnerInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) (oc.OwnerAuthorization, error) {
	x, p, k, err := a.ownerScope(ctx, tx, actor, owner, intent)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	row, err := loadDocument(ctx, x, p, k)
	if err == nil {
		if row.head.Active == nil {
			return oc.OwnerAuthorization{}, fault(f.NotFound)
		}
		return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: oc.ExistingOwner, Version: row.head.Active.ContentVersion})
	}
	var known *f.Fault
	if !errors.As(err, &known) || known.Code != f.NotFound {
		return oc.OwnerAuthorization{}, err
	}
	// Missing metadata alone is never prospective authorization. A real planned
	// creation and its publication must exist for this exact original user.
	var creation, user string
	err = x.QueryRow(ctx, `SELECT c.id::text,c.actor_user_id::text
 FROM agenteam_knowledge.commands c JOIN agenteam_knowledge.publications p
 ON p.project_id=c.project_id AND p.command_id=c.id AND p.document_id=c.document_id
 WHERE c.project_id=$1 AND c.document_id=$2 AND c.command_name='create' AND c.state='planned'
	 AND p.phase IN ('planned','reserved','uploaded')`, p.String(), k.String()).Scan(&creation, &user)
	if errors.Is(err, pgx.ErrNoRows) {
		return oc.OwnerAuthorization{}, fault(f.NotFound)
	}
	if err != nil {
		return oc.OwnerAuthorization{}, unavailable(err)
	}
	if _, err = f.ParseID[command](creation); err != nil {
		return oc.OwnerAuthorization{}, internal(err)
	}
	if user != actor.Details().UserID {
		return oc.OwnerAuthorization{}, fault(f.Forbidden)
	}
	return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: oc.ProspectiveOwner, CreationCause: creation, Version: 1})
}

func (a *Authority) AuthorizeObjectReadInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, object oc.ObjectID) (oc.OwnerAuthorization, error) {
	if object.Validate() != nil {
		return oc.OwnerAuthorization{}, fault(f.InvalidArgument)
	}
	x, p, k, err := a.ownerScope(ctx, tx, actor, owner, id.Read)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	row, err := loadDocument(ctx, x, p, k)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if row.head.Active == nil || row.head.Active.ObjectID != object {
		return oc.OwnerAuthorization{}, fault(f.NotFound)
	}
	return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: id.Read, Existence: oc.ExistingOwner, Version: row.head.Active.ContentVersion, ReadObjectID: object})
}

func (a *Authority) CheckInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, intent id.AccessIntent) error {
	if intent != id.Converge {
		_, _, _, err := a.ownerScope(ctx, tx, actor, owner, intent)
		return err
	}
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	p, k, err := knowledgeOwner(owner)
	if err != nil {
		return err
	}
	if ctx == nil || actor.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	d := actor.Details()
	if d.Kind != id.Service || d.ServiceName != id.ObjectMaintenance || d.ProjectID != p.String() {
		return fault(f.Forbidden)
	}
	operation, err := f.ParseID[oc.CleanupOperation](d.CauseRef)
	if err != nil {
		return fault(f.Forbidden)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	locks, err := authorityLocks(actor, p, id.Converge)
	if err != nil {
		return err
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	var found bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.object_cleanup c
 WHERE c.id=$1 AND c.project_id=$2 AND c.document_id=$3 AND c.reason='cancelled_upload'
 AND c.phase IN ('reference','object','completed')
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.documents d WHERE d.project_id=c.project_id
 AND d.id=c.document_id AND d.current_object_id=c.object_id))`, operation.String(), p.String(), k.String()).Scan(&found)
	if err != nil {
		return unavailable(err)
	}
	if !found {
		return fault(f.Forbidden)
	}
	return nil
}

func (a *Authority) Discover(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	st := a.state()
	if st == nil {
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	return objectDependencies(ctx, st.store, request)
}
func (a *Authority) ValidateInTx(ctx context.Context, tx f.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || request.Validate() != nil || expected.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, expected.Locks()); err != nil {
		return portError(err)
	}
	actual, err := objectDependencies(ctx, x, request)
	if err != nil {
		return err
	}
	if !actual.Equal(expected) {
		return f.NewFault(f.ResourceBusy, f.NotCommitted)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, actual.Locks()); err != nil {
		return portError(err)
	}
	// The cleanup request's UploadID has no later checker argument. Bind it here
	// under the complete union, in addition to the exact cause/object checker.
	d := request.Details()
	if d.Kind == oc.CleanupReleaseAccess {
		return a.checkCleanup(ctx, tx, d.Cleanup, d.ObjectID, &d.UploadID)
	}
	return nil
}

func objectDependencies(ctx context.Context, x postgres.SQLExecutor, request oc.AccessRequest) (oc.AccessDependencies, error) {
	if ctx == nil || request.Validate() != nil {
		return oc.AccessDependencies{}, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return oc.AccessDependencies{}, unavailable(err)
	}
	d := request.Details()
	var owner oc.ObjectOwner
	intent := d.Intent
	switch d.Kind {
	case oc.OwnerAccess, oc.ObjectReadAccess:
		owner = d.Owner
	case oc.SourceAccess:
		owner = d.Source.Details().Owner
		intent = id.Read
	case oc.ObjectCleanupAccess, oc.CleanupReleaseAccess:
		owner = d.Cleanup.Details().Owner
		intent = id.Converge
	case oc.MaintenanceAccess:
		var err error
		owner, err = knowledgeObjectOwner(ctx, x, d.ObjectID)
		if err != nil {
			return oc.AccessDependencies{}, err
		}
		intent = id.Converge
	default:
		// Execution leases, transfers and whole-Project lifecycle need their
		// actual providers; Knowledge's cleanup facts cannot authorize them.
		return oc.AccessDependencies{}, fault(f.DependencyUnbound)
	}
	p, k, err := knowledgeOwner(owner)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	locks, err := authorityLocks(d.Actor, p, intent)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	// A prospective row becoming current does not change its parent mapping.
	// The real owner checks below, not this stable mapping, prove existence.
	mapping, err := json.Marshal([]string{"knowledge.object.v1", p.String(), k.String()})
	if err != nil {
		return oc.AccessDependencies{}, internal(err)
	}
	return oc.NewAccessDependencies(ob.DigestBytes(mapping), locks)
}

func knowledgeObjectOwner(ctx context.Context, x postgres.SQLExecutor, object oc.ObjectID) (oc.ObjectOwner, error) {
	rows, err := x.Query(ctx, `SELECT DISTINCT project_id::text,document_id::text FROM (
 SELECT project_id,id AS document_id FROM agenteam_knowledge.documents WHERE current_object_id=$1
 UNION ALL SELECT project_id,document_id FROM agenteam_knowledge.publications WHERE object_id=$1
 UNION ALL SELECT project_id,document_id FROM agenteam_knowledge.object_cleanup WHERE object_id=$1) owners`, object.String())
	if err != nil {
		return oc.ObjectOwner{}, unavailable(err)
	}
	defer rows.Close()
	var owners []oc.ObjectOwner
	for rows.Next() {
		var p, k string
		if err = rows.Scan(&p, &k); err != nil {
			return oc.ObjectOwner{}, unavailable(err)
		}
		owner, err := oc.NewObjectOwner(oc.Knowledge, k, p)
		if err != nil {
			return oc.ObjectOwner{}, internal(err)
		}
		owners = append(owners, owner)
	}
	if err = rows.Err(); err != nil {
		return oc.ObjectOwner{}, unavailable(err)
	}
	if len(owners) != 1 {
		return oc.ObjectOwner{}, fault(f.DependencyUnbound)
	}
	return owners[0], nil
}

var _ oc.AccessPlanner = (*Authority)(nil)
var _ oc.ResourceAuthority = (*Authority)(nil)
var _ oc.ObjectReadAuthority = (*Authority)(nil)
var _ oc.ProjectGate = (*Authority)(nil)
