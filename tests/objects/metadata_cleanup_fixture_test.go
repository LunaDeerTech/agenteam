//go:build integration

package objects_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

// These SQL facts implement only the owning-domain port in a D05 fixture.
// They are not Skills CleanupAuthority/Project CleanupPhase implementations.
// The Object service, lock union, storage, stop, native Audit and transactions
// remain real. The later Skills final-five-row transaction is a separate gate.
type metadataCleanupAuthority struct{ *authority }

type metadataCleanupFacts struct {
	Operation, Object, Upload, Project, Owner, Skill, User, Phase string
	Version                                                       int64
}

func (a *metadataCleanupAuthority) facts(ctx context.Context, e postgres.SQLExecutor, object oc.ObjectID) (*metadataCleanupFacts, error) {
	var f metadataCleanupFacts
	err := e.QueryRow(ctx, `SELECT operation_id::text,object_id::text,upload_id::text,project_id::text,owner_id::text,skill_id::text,user_id::text,phase,project_version FROM object_fixture.metadata_cleanup WHERE object_id=$1`, object.String()).Scan(&f.Operation, &f.Object, &f.Upload, &f.Project, &f.Owner, &f.Skill, &f.User, &f.Phase, &f.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &f, err
}

func (a *metadataCleanupAuthority) dependencies(ctx context.Context, e postgres.SQLExecutor, request oc.AccessRequest) (oc.AccessDependencies, error) {
	base, err := a.authority.dependencies(ctx, e, request)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	d := request.Details()
	if d.ObjectID.Validate() != nil {
		return base, nil
	}
	f, err := a.facts(ctx, e, d.ObjectID)
	if err != nil || f == nil {
		return base, err
	}
	allowed := false
	switch d.Operation {
	case oc.ReleaseForCleanupAccess, oc.CleanupObjectAccess, oc.ClaimCleanupAccess, oc.CheckpointCleanupAccess, oc.FinalizeCleanupAccess:
		allowed = f.Phase == "gated"
	case oc.PurgeDeletedObjectMetadataAccess:
		allowed = f.Phase == "completed"
	}
	if !allowed {
		return oc.AccessDependencies{}, fault(foundation.Forbidden)
	}
	project, err := foundation.ProjectLock(f.Project)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	skill, err := foundation.AggregateLock(foundation.SkillAggregate, f.Skill)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	raw, err := json.Marshal([]any{base.Mapping(), f})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	locks := append(base.Locks(), foundation.LockRequest{Key: project, Mode: foundation.Exclusive}, foundation.LockRequest{Key: skill, Mode: foundation.Exclusive})
	return oc.NewAccessDependencies(digest(raw), locks)
}

func (a *metadataCleanupAuthority) Discover(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	return a.dependencies(ctx, a.store, request)
}

func (a *metadataCleanupAuthority) ValidateInTx(ctx context.Context, tx foundation.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	e, err := a.store.InTx(tx)
	if err != nil {
		return err
	}
	actual, err := a.dependencies(ctx, e, request)
	if err != nil {
		return err
	}
	if !actual.Equal(expected) {
		return fault(foundation.ResourceBusy)
	}
	return a.store.RequireHeldLocks(ctx, tx, actual.Locks())
}

func (a *metadataCleanupAuthority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID) error {
	if cause.Validate() != nil || cause.Details().Reason != oc.ProjectDeleted || cause.Details().Owner.Details().Kind != oc.SkillRevision {
		return fault(foundation.Forbidden)
	}
	e, err := a.store.InTx(tx)
	if err != nil {
		return err
	}
	f, err := a.facts(ctx, e, object)
	if err != nil {
		return err
	}
	d, owner := cause.Details(), cause.Details().Owner.Details()
	if f == nil || f.Operation != d.OperationID.String() || f.Owner != owner.ID || f.Project != owner.ProjectID || f.Phase != "gated" && f.Phase != "completed" {
		return fault(foundation.Forbidden)
	}
	project, _ := foundation.ProjectLock(f.Project)
	skill, _ := foundation.AggregateLock(foundation.SkillAggregate, f.Skill)
	if err = a.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: project, Mode: foundation.Exclusive}, {Key: skill, Mode: foundation.Exclusive}}); err != nil {
		return err
	}
	var current bool
	err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM object_fixture.projects p JOIN object_fixture.owners r ON r.project_id=p.id JOIN agenteam_object.uploads u ON u.owner_id=r.id JOIN agenteam_object.project_stops s ON s.project_id=p.id WHERE p.id=$1 AND p.owner_id=$2 AND p.state='deleting' AND p.operation_id=$3 AND p.version=$4 AND r.id=$5 AND r.kind='skill_revision' AND r.parent_id=$6 AND u.id=$7 AND u.object_id=$8 AND s.operation_id=$3 AND s.project_version=$4 AND s.state='stopped' AND s.action='delete')`, f.Project, f.User, f.Operation, f.Version, f.Owner, f.Skill, f.Upload, f.Object).Scan(&current)
	if err != nil {
		return err
	}
	if !current {
		return fault(foundation.Forbidden)
	}
	return nil
}

func newMetadataCleanupFixture(t *testing.T) (*objectAuditFixture, *objectStopAuthority) {
	t.Helper()
	base := newFixture(t, false)
	owner, err := oc.NewObjectOwner(oc.SkillRevision, base.owner.Details().ID, base.project.String())
	if err != nil {
		t.Fatal(err)
	}
	base.owner = owner
	base.sql(t, `UPDATE object_fixture.owners SET kind='skill_revision',parent_id=$2 WHERE id=$1`, owner.Details().ID, id[struct{}](t).String())
	base.sql(t, `CREATE TABLE object_fixture.metadata_cleanup(operation_id uuid NOT NULL,object_id uuid PRIMARY KEY,upload_id uuid NOT NULL,project_id uuid NOT NULL,owner_id uuid NOT NULL,skill_id uuid NOT NULL,user_id uuid NOT NULL,project_version bigint NOT NULL,phase text NOT NULL CHECK(phase IN ('gated','completed')))`)
	stop := newObjectStopAuthority(t, base, base.store)
	authority := &metadataCleanupAuthority{base.authority}
	return objectAuditOn(t, base, objectAuditOptions{stop: stop, planner: authority, cleanup: authority}), stop
}

func metadataCleanupCause(t *testing.T, f *objectAuditFixture, object oc.ObjectID) oc.ObjectCleanupCause {
	t.Helper()
	actor, stopped := activateObjectStop(t, f.fixture, oc.ProjectStopDelete)
	stopUntilSettled(t, f.service, actor, stopped)
	operation, err := foundation.ParseID[oc.CleanupOperation](stopped.Details().OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	cause, err := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: operation, Owner: f.owner, Reason: oc.ProjectDeleted})
	if err != nil {
		t.Fatal(err)
	}
	f.sql(t, `INSERT INTO object_fixture.metadata_cleanup(operation_id,object_id,upload_id,project_id,owner_id,skill_id,user_id,project_version,phase) SELECT $1,u.object_id,u.id,u.project_id,u.owner_id,r.parent_id,p.owner_id,p.version,'gated' FROM agenteam_object.uploads u JOIN object_fixture.owners r ON r.id=u.owner_id JOIN object_fixture.projects p ON p.id=u.project_id WHERE u.object_id=$2`, operation.String(), object.String())
	return cause
}

func metadataRelease(t *testing.T, f *objectAuditFixture, c oc.ObjectCleanupCause, object oc.ObjectID) foundation.CommitResult {
	t.Helper()
	var upload string
	if err := f.store.QueryRow(contextFor(t), `SELECT id::text FROM agenteam_object.uploads WHERE object_id=$1`, object.String()).Scan(&upload); err != nil {
		t.Fatal(err)
	}
	uid, err := foundation.ParseID[oc.Upload](upload)
	if err != nil {
		t.Fatal(err)
	}
	r, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: c, ObjectID: object, UploadID: uid})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := f.service.DiscoverAccess(contextFor(t), r)
	if err != nil {
		t.Fatal(err)
	}
	return plannedTx(f.store, f.service, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
		return f.service.ReleaseForCleanupInTx(ctx, tx, c, object, plan, locked)
	})
}

func metadataRows(t *testing.T, f *fixture, object oc.ObjectID) int {
	t.Helper()
	var n int
	err := f.store.QueryRow(contextFor(t), `SELECT
 (SELECT count(*) FROM agenteam_object.objects WHERE id=$1)
 +(SELECT count(*) FROM agenteam_object.uploads WHERE object_id=$1)
 +(SELECT count(*) FROM agenteam_object.upload_attempts WHERE object_id=$1)
 +(SELECT count(*) FROM agenteam_object.cleanup_operations WHERE object_id=$1)
 +(SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1)
 +(SELECT count(*) FROM agenteam_object.project_work WHERE object_id=$1)
 +(SELECT count(*) FROM agenteam_object.object_transfers WHERE object_id=$1)`, object.String()).Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
