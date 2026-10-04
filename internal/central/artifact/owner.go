package artifact

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// OwnerProvider is the real Artifact side of object authorization and planning.
// It reads only Artifact-owned facts. Current identity/Project/Execution facts
// remain required external ports; an unbound port never produces a grant.
type OwnerProvider struct{ data func() ownerState }
type ownerState struct {
	store     Store
	authority ac.Authority
}

func NewOwnerProvider(store Store, authority ac.Authority) (*OwnerProvider, error) {
	if nilPort(store) {
		return nil, invalid()
	}
	d := ownerState{store, authority}
	return &OwnerProvider{func() ownerState { return d }}, nil
}
func (p *OwnerProvider) state() ownerState { return p.data() }
func (p *OwnerProvider) bound() error {
	if p == nil || p.data == nil || nilPort(p.state().authority) {
		return failure(foundation.DependencyUnbound, nil)
	}
	return nil
}
func (OwnerProvider) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "artifact_owner_provider") }
func (OwnerProvider) MarshalJSON() ([]byte, error) { return []byte(`"artifact_owner_provider"`), nil }
func (*OwnerProvider) UnmarshalJSON([]byte) error  { return invalid() }
func (OwnerProvider) LogValue() slog.Value         { return slog.StringValue("artifact_owner_provider") }

type ownerFact struct {
	id, project, cause, originalActor, object, upload, commandKey, state string
	exists                                                               bool
	version                                                              foundation.Version
}

// Discovery may know a prospective identity before a command is inserted. That
// is no authorization: authorization below requires a durable row and its exact
// original actor/cause. Existence is not a parent mapping, so inserting the
// Artifact in an already-locked publication Tx does not invalidate the plan.
func ownerByID(ctx context.Context, e postgres.SQLExecutor, id string) (ownerFact, bool, error) {
	var f ownerFact
	var existence string
	var version int64
	err := e.QueryRow(ctx, `SELECT id,project_id,creation_cause,stable_actor,object_id,upload_id,command_key,state,existence,version FROM (
 SELECT id::text,project_id::text,creation_cause,''::text AS stable_actor,object_id::text,''::text AS upload_id,''::text AS command_key,'completed'::text AS state,'existing'::text AS existence,version,0 AS priority FROM agenteam_artifact.artifacts WHERE id=$1
 UNION ALL SELECT artifact_id::text,project_id::text,creation_cause,stable_actor,COALESCE(target_object_id::text,''),COALESCE(target_upload_id::text,''),command_key,state,'prospective',1,1 FROM agenteam_artifact.commands WHERE artifact_id=$1
 UNION ALL SELECT id::text,project_id::text,creation_cause,stable_actor,COALESCE(target_object_id::text,''),COALESCE(target_upload_id::text,''),command_key,state,'prospective',1,2 FROM agenteam_artifact.upload_intents WHERE id=$1
 ) f ORDER BY priority LIMIT 1`, id).Scan(&f.id, &f.project, &f.cause, &f.originalActor, &f.object, &f.upload, &f.commandKey, &f.state, &existence, &version)
	if noRows(err) {
		return f, false, nil
	}
	if err != nil {
		return f, false, unavailable(err)
	}
	f.exists = existence == "existing"
	f.version = foundation.Version(version)
	return f, true, nil
}
func ownerForObject(ctx context.Context, e postgres.SQLExecutor, id oc.ObjectID) (ownerFact, error) {
	rows, err := e.Query(ctx, `SELECT DISTINCT id FROM (SELECT id::text FROM agenteam_artifact.artifacts WHERE object_id=$1 UNION ALL SELECT artifact_id::text FROM agenteam_artifact.commands WHERE target_object_id=$1 UNION ALL SELECT id::text FROM agenteam_artifact.upload_intents WHERE target_object_id=$1) f`, id.String())
	if err != nil {
		return ownerFact{}, unavailable(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return ownerFact{}, unavailable(err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ownerFact{}, unavailable(err)
	}
	if len(ids) != 1 {
		return ownerFact{}, failure(foundation.DependencyUnbound, nil)
	}
	f, ok, err := ownerByID(ctx, e, ids[0])
	if err != nil {
		return ownerFact{}, err
	}
	if !ok || f.object != id.String() {
		return ownerFact{}, failure(foundation.ResourceBusy, nil)
	}
	return f, nil
}
func (p *OwnerProvider) Discover(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	return p.dependencies(ctx, foundation.Tx{}, request)
}
func (p *OwnerProvider) ValidateInTx(ctx context.Context, tx foundation.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	if expected.Validate() != nil {
		return invalid()
	}
	current, err := p.dependencies(ctx, tx, request)
	if err != nil {
		return err
	}
	if !current.Equal(expected) {
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotCommitted)
	}
	return nil
}
func (p *OwnerProvider) dependencies(ctx context.Context, tx foundation.Tx, request oc.AccessRequest) (oc.AccessDependencies, error) {
	if err := p.bound(); err != nil {
		return oc.AccessDependencies{}, err
	}
	if request.Validate() != nil {
		return oc.AccessDependencies{}, invalid()
	}
	e := p.state().store
	var executor postgres.SQLExecutor = e
	inTx := tx != (foundation.Tx{})
	if inTx {
		var err error
		executor, err = e.InTx(tx)
		if err != nil {
			return oc.AccessDependencies{}, portError(err)
		}
	}
	d := request.Details()
	owners := map[string]oc.ObjectOwner{}
	projects := map[string]identity.ProjectID{}
	addOwner := func(owner oc.ObjectOwner) error {
		if owner.Validate() != nil {
			return nil
		}
		od := owner.Details()
		if od.Kind != oc.Artifact {
			return failure(foundation.DependencyUnbound, nil)
		}
		f, ok, err := ownerByID(ctx, executor, od.ID)
		if err != nil {
			return err
		}
		if ok && f.project != od.ProjectID {
			return foundation.NewFault(foundation.ResourceBusy, foundation.NotCommitted)
		}
		pid, _ := foundation.ParseID[identity.Project](od.ProjectID)
		projects[od.ProjectID] = pid
		owners[od.ID] = owner
		return nil
	}
	for _, owner := range []oc.ObjectOwner{d.Owner, d.Cleanup.Details().Owner, d.Source.Details().Owner} {
		if err := addOwner(owner); err != nil {
			return oc.AccessDependencies{}, err
		}
	}
	objects := append([]oc.ObjectID{}, d.Objects...)
	if d.ObjectID.Validate() == nil {
		objects = append(objects, d.ObjectID)
	}
	// Indirect maintenance/lease requests have no caller-supplied owner. Resolve
	// them from this domain's own persisted object mapping, never object tables.
	if len(owners) == 0 {
		for _, id := range objects {
			f, err := ownerForObject(ctx, executor, id)
			if err != nil {
				return oc.AccessDependencies{}, err
			}
			o, _ := oc.NewObjectOwner(oc.Artifact, f.id, f.project)
			if err = addOwner(o); err != nil {
				return oc.AccessDependencies{}, err
			}
		}
	}
	if d.ProjectCleanup.Validate() == nil {
		pid := d.ProjectCleanup.Details().ProjectID
		projects[pid.String()] = pid
	}
	if len(projects) == 0 {
		return oc.AccessDependencies{}, failure(foundation.DependencyUnbound, nil)
	}
	var mapping []string
	var locks []foundation.LockRequest
	for _, owner := range owners {
		od := owner.Details()
		mapping = append(mapping, "owner:"+od.ID+":"+od.ProjectID)
		locks = append(locks, artifactLock(od.ID))
	}
	for _, project := range projects {
		s := subject(d.Actor, project)
		if d.Actor.Validate() != nil {
			s = ac.AccessSubject{ProjectID: project, Maintenance: true}
		}
		if s.Validate() != nil {
			return oc.AccessDependencies{}, invalid()
		}
		var deps oc.AccessDependencies
		var err error
		if inTx {
			deps, err = p.state().authority.DiscoverInTx(ctx, tx, s)
		} else {
			deps, err = p.state().authority.Discover(ctx, s)
		}
		if err != nil {
			return oc.AccessDependencies{}, portError(err)
		}
		if deps.Validate() != nil {
			return oc.AccessDependencies{}, unavailable(nil)
		}
		mapping = append(mapping, "project:"+project.String()+":"+deps.Mapping().String())
		locks = append(locks, deps.Locks()...)
	}
	sort.Strings(mapping)
	digest, err := jsonDigest(mapping)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest, locks)
}
func (p *OwnerProvider) authorize(ctx context.Context, tx foundation.Tx, s ac.AccessSubject, intent identity.AccessIntent) error {
	if err := p.bound(); err != nil {
		return err
	}
	if s.Validate() != nil || s.Maintenance {
		return invalid()
	}
	grant, err := p.state().authority.AuthorizeInTx(ctx, tx, s, intent)
	if err != nil {
		return portError(err)
	}
	scope, _ := identity.InProject(s.ProjectID)
	if !grant.Matches(s.Actor, scope, intent) {
		return failure(foundation.Forbidden, nil)
	}
	// The minimal terminal receipt outlives command/intent rows. Current authority
	// is still checked first, but no new or replayed business request can revive
	// this domain if an external adapter later returns an old active grant.
	// Exact lifecycle calls remain available for idempotent cleanup confirmation.
	if intent != identity.Lifecycle {
		e, err := p.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		var completed bool
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.cleanup WHERE project_id=$1 AND state='completed')`, s.ProjectID.String()).Scan(&completed); err != nil {
			return unavailable(err)
		}
		if completed {
			return failure(foundation.ResourceDeleted, nil)
		}
	}
	return nil
}
func (p *OwnerProvider) AuthorizeOwner(ctx context.Context, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) (oc.OwnerAuthorization, error) {
	if err := p.bound(); err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if owner.Validate() != nil || actor.Validate() != nil || owner.Details().Kind != oc.Artifact {
		return oc.OwnerAuthorization{}, invalid()
	}
	pid, _ := foundation.ParseID[identity.Project](owner.Details().ProjectID)
	s := subject(actor, pid)
	deps, err := p.state().authority.Discover(ctx, s)
	if err != nil {
		return oc.OwnerAuthorization{}, portError(err)
	}
	if deps.Validate() != nil {
		return oc.OwnerAuthorization{}, unavailable(nil)
	}
	locks := append(deps.Locks(), artifactLock(owner.Details().ID))
	cause, err := txCause()
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	var out oc.OwnerAuthorization
	r := p.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := p.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		now, err := p.state().authority.DiscoverInTx(ctx, tx, s)
		if err != nil {
			return portError(err)
		}
		if !now.Equal(deps) {
			return failure(foundation.ResourceBusy, nil)
		}
		out, err = p.AuthorizeOwnerInTx(ctx, tx, actor, owner, intent)
		return err
	})
	if err = commitError(r); err != nil {
		return oc.OwnerAuthorization{}, err
	}
	return out, nil
}
func (p *OwnerProvider) AuthorizeOwnerInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) (oc.OwnerAuthorization, error) {
	if err := p.bound(); err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if actor.Validate() != nil || owner.Validate() != nil || owner.Details().Kind != oc.Artifact {
		return oc.OwnerAuthorization{}, invalid()
	}
	e, err := p.state().store.InTx(tx)
	if err != nil {
		return oc.OwnerAuthorization{}, portError(err)
	}
	pid, _ := foundation.ParseID[identity.Project](owner.Details().ProjectID)
	if err = p.authorize(ctx, tx, subject(actor, pid), intent); err != nil {
		return oc.OwnerAuthorization{}, err
	}
	f, ok, err := ownerByID(ctx, e, owner.Details().ID)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if !ok || f.project != pid.String() {
		return oc.OwnerAuthorization{}, failure(foundation.Forbidden, nil)
	}
	if (f.state == "deleted" || f.state == "cancel_requested" || f.state == "cancelled") && intent != identity.Converge && intent != identity.Lifecycle {
		return oc.OwnerAuthorization{}, failure(foundation.ResourceDeleted, nil)
	}
	if !f.exists && actor.Details().Kind != identity.Service && f.originalActor != stableActor(actor) {
		return oc.OwnerAuthorization{}, failure(foundation.Forbidden, nil)
	}
	if actor.Details().Kind == identity.Service {
		if err = p.checkTechnicalCause(ctx, e, actor, pid); err != nil {
			return oc.OwnerAuthorization{}, err
		}
	}
	existence := oc.ProspectiveOwner
	if f.exists {
		existence = oc.ExistingOwner
	}
	return oc.NewOwnerAuthorization(oc.OwnerAuthorizationDetails{Actor: actor, Owner: owner, Intent: intent, Existence: existence, CreationCause: f.cause, Version: f.version})
}
func (p *OwnerProvider) AuthorizeObjectReadInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, id oc.ObjectID) (oc.OwnerAuthorization, error) {
	g, err := p.AuthorizeOwnerInTx(ctx, tx, actor, owner, identity.Read)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	e, err := p.state().store.InTx(tx)
	if err != nil {
		return oc.OwnerAuthorization{}, portError(err)
	}
	f, ok, err := ownerByID(ctx, e, owner.Details().ID)
	if err != nil {
		return oc.OwnerAuthorization{}, err
	}
	if !ok || !f.exists || f.object != id.String() {
		return oc.OwnerAuthorization{}, failure(foundation.Forbidden, nil)
	}
	d := g.Details()
	d.ReadObjectID = id
	return oc.NewOwnerAuthorization(d)
}
func (p *OwnerProvider) CheckInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) error {
	if owner.Validate() != nil || owner.Details().Kind != oc.Artifact {
		return invalid()
	}
	pid, _ := foundation.ParseID[identity.Project](owner.Details().ProjectID)
	if err := p.authorize(ctx, tx, subject(actor, pid), intent); err != nil {
		return err
	}
	if actor.Details().Kind == identity.Service {
		e, err := p.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return p.checkTechnicalCause(ctx, e, actor, pid)
	}
	return nil
}
func (p *OwnerProvider) checkTechnicalCause(ctx context.Context, e postgres.SQLExecutor, actor identity.Actor, pid identity.ProjectID) error {
	d := actor.Details()
	if d.Kind != identity.Service || d.ServiceName != identity.ObjectMaintenance || d.ProjectID != pid.String() {
		return failure(foundation.Forbidden, nil)
	}
	var found bool
	err := e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_artifact.cleanup WHERE project_id=$1 AND operation_id::text=$2 AND state='pending') OR EXISTS(SELECT 1 FROM agenteam_artifact.upload_intents WHERE project_id=$1 AND target_upload_id::text=$2 AND state IN('cancel_requested','cancelled')) OR EXISTS(SELECT 1 FROM agenteam_artifact.commands WHERE project_id=$1 AND target_upload_id::text=$2 AND state IN('cancel_requested','cancelled'))`, pid.String(), d.CauseRef).Scan(&found)
	if err != nil {
		return unavailable(err)
	}
	if !found {
		return failure(foundation.Forbidden, nil)
	}
	return nil
}
func (p *OwnerProvider) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, cause oc.ObjectCleanupCause, id oc.ObjectID) error {
	if err := p.bound(); err != nil {
		return err
	}
	if cause.Validate() != nil || id.Validate() != nil || cause.Details().Owner.Details().Kind != oc.Artifact {
		return invalid()
	}
	e, err := p.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	d := cause.Details()
	f, ok, err := ownerByID(ctx, e, d.Owner.Details().ID)
	if err != nil {
		return err
	}
	if !ok || f.project != d.Owner.Details().ProjectID || f.object != id.String() {
		return failure(foundation.Forbidden, nil)
	}
	if d.Reason == oc.CancelledUpload && f.upload == d.OperationID.String() && (f.state == "cancel_requested" || f.state == "cancelled") {
		// This exact cause was committed by the original current owner before the
		// Object service was allowed to revoke its reservation. It grants only
		// convergence; the current Project gate remains a required real port.
		pid, _ := foundation.ParseID[identity.Project](f.project)
		registration, _ := identity.RegisterService(identity.ObjectMaintenance)
		actor, err := registration.Actor(d.OperationID.String(), d.Owner.Scope())
		if err != nil {
			return invalid()
		}
		return p.authorize(ctx, tx, subject(actor, pid), identity.Converge)
	}
	var version int64
	err = e.QueryRow(ctx, `SELECT project_version FROM agenteam_artifact.cleanup WHERE project_id=$1 AND operation_id=$2 AND state='pending'`, f.project, d.OperationID.String()).Scan(&version)
	if noRows(err) {
		return failure(foundation.Forbidden, nil)
	}
	if err != nil {
		return unavailable(err)
	}
	pid, _ := foundation.ParseID[identity.Project](f.project)
	pc, _ := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: pid, OperationID: d.OperationID, Version: foundation.Version(version)})
	registration, _ := identity.RegisterService(identity.ObjectMaintenance)
	actor, err := registration.Actor(d.OperationID.String(), d.Owner.Scope())
	if err != nil {
		return invalid()
	}
	return portOrNil(p.state().authority.CheckProjectCleanupInTx(ctx, tx, actor, pc))
}
func (p *OwnerProvider) CheckProjectCleanupInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause oc.ProjectCleanupCause) error {
	if err := p.bound(); err != nil {
		return err
	}
	if cause.Validate() != nil {
		return invalid()
	}
	return portOrNil(p.state().authority.CheckProjectCleanupInTx(ctx, tx, actor, cause))
}
func portOrNil(err error) error {
	if err == nil {
		return nil
	}
	return portError(err)
}
