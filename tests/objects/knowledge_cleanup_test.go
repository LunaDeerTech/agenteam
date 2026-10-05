//go:build integration

package objects_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/minio/minio-go/v7"
)

// These are isolated owning-domain facts for the D05 shared-port boundary,
// not a Knowledge service implementation or a permissive cleanup authority.
// All upload, publication, object gate, reader and storage work remains real.
type knowledgeCleanupAuthority struct{ *authority }

func (a *knowledgeCleanupAuthority) Discover(ctx context.Context, request oc.AccessRequest) (oc.AccessDependencies, error) {
	dependencies, e := a.authority.Discover(ctx, request)
	if e != nil {
		return oc.AccessDependencies{}, e
	}
	return knowledgeCleanupDependencies(request, dependencies)
}

func (a *knowledgeCleanupAuthority) ValidateInTx(ctx context.Context, tx foundation.Tx, request oc.AccessRequest, expected oc.AccessDependencies) error {
	x, e := a.exec(tx)
	if e != nil {
		return e
	}
	dependencies, e := a.dependencies(ctx, x, request)
	if e != nil {
		return e
	}
	actual, e := knowledgeCleanupDependencies(request, dependencies)
	if e != nil {
		return e
	}
	if !expected.Equal(actual) {
		return fault(foundation.ResourceBusy)
	}
	return nil
}

func knowledgeCleanupDependencies(request oc.AccessRequest, dependencies oc.AccessDependencies) (oc.AccessDependencies, error) {
	d := request.Details()
	if d.Kind != oc.CleanupReleaseAccess && d.Kind != oc.ObjectCleanupAccess || d.Cleanup.Validate() != nil || d.Cleanup.Details().Owner.Details().Kind != oc.Knowledge {
		return dependencies, nil
	}
	tree, e := foundation.KnowledgeTreeLock(d.Cleanup.Details().Owner.Details().ProjectID)
	if e != nil {
		return oc.AccessDependencies{}, e
	}
	// The owning domain supplies its tree mutation lock before the caller's
	// one complete acquisition; Object must not guess this business lock.
	return oc.NewAccessDependencies(dependencies.Mapping(), append(dependencies.Locks(), foundation.LockRequest{Key: tree, Mode: foundation.Exclusive}))
}

func (a *knowledgeCleanupAuthority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, cause oc.ObjectCleanupCause, objectID oc.ObjectID) error {
	if !tx.Valid() || cause.Validate() != nil || cause.Details().Owner.Details().Kind != oc.Knowledge {
		return fault(foundation.Forbidden)
	}
	d := cause.Details()
	owner := d.Owner.Details()
	project, e := foundation.ProjectLock(owner.ProjectID)
	if e != nil {
		return e
	}
	tree, e := foundation.KnowledgeTreeLock(owner.ProjectID)
	if e != nil {
		return e
	}
	if e = a.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: project, Mode: foundation.Shared}, {Key: tree, Mode: foundation.Exclusive}}); e != nil {
		return e
	}
	x, e := a.store.InTx(tx)
	if e != nil {
		return e
	}
	var exact bool
	e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM object_fixture.knowledge_cleanup c JOIN agenteam_object.uploads u ON u.id=c.upload_id AND u.object_id=c.object_id WHERE c.operation_id=$1 AND c.object_id=$2 AND c.owner_id=$3 AND c.project_id=$4 AND c.owner_kind='knowledge' AND c.reason=$5)`, d.OperationID.String(), objectID.String(), owner.ID, owner.ProjectID, string(d.Reason)).Scan(&exact)
	if e != nil {
		return e
	}
	if !exact {
		return fault(foundation.Forbidden)
	}
	return nil
}

type knowledgeCleanupFixture struct {
	*fixture
	objects *object.Service
	process oc.ProcessID
}

func newKnowledgeCleanupFixture(t *testing.T, prospective bool) *knowledgeCleanupFixture {
	t.Helper()
	f := newFixture(t, prospective)
	owner, e := oc.NewObjectOwner(oc.Knowledge, f.owner.Details().ID, f.project.String())
	if e != nil {
		t.Fatal(e)
	}
	f.sql(t, `UPDATE object_fixture.owners SET kind='knowledge' WHERE id=$1`, owner.Details().ID)
	f.owner = owner
	f.sql(t, `CREATE TABLE object_fixture.knowledge_cleanup(operation_id uuid NOT NULL,object_id uuid NOT NULL,upload_id uuid NOT NULL,owner_kind text NOT NULL,owner_id uuid NOT NULL,project_id uuid NOT NULL,reason text NOT NULL,PRIMARY KEY(operation_id,object_id,reason))`)
	backend, e := object.NewBackend(f.config)
	if e != nil {
		t.Fatal(e)
	}
	process := id[oc.Process](t)
	spool, e := object.OpenSpool(filepath.Join(t.TempDir(), "knowledge-spool"), process)
	if e != nil {
		t.Fatal(e)
	}
	keys, e := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if e != nil {
		t.Fatal(e)
	}
	aud, e := audit.New(f.store, keys, audit.Authorizations{Projects: auditAuthority{f.authority}})
	if e != nil {
		t.Fatal(e)
	}
	auth := &knowledgeCleanupAuthority{f.authority}
	service, e := object.New(f.store, backend, spool, aud, object.Authorizations{Planner: auth, Resources: auth, Read: auth, Gate: auth, Cleanup: auth, Leases: auth})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		service.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if e := service.Drain(ctx); e != nil {
			_ = service.Force(ctx)
			t.Error(e)
		}
	})
	if e = service.Initialize(contextFor(t)); e != nil {
		t.Fatal(e)
	}
	return &knowledgeCleanupFixture{fixture: f, objects: service, process: process}
}
func (f *knowledgeCleanupFixture) putKnowledge(t *testing.T, key, body string) oc.PutResult {
	t.Helper()
	r, e := f.objects.PutObject(contextFor(t), f.actor, f.owner, command(t, key), "text/plain", int64(len(body)), nil, io.NopCloser(strings.NewReader(body)))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (f *knowledgeCleanupFixture) upload(t *testing.T, objectID oc.ObjectID) oc.UploadID {
	t.Helper()
	var raw string
	if e := f.store.QueryRow(contextFor(t), `SELECT id::text FROM agenteam_object.uploads WHERE object_id=$1`, objectID.String()).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	out, e := foundation.ParseID[oc.Upload](raw)
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func (f *knowledgeCleanupFixture) cleanupCause(t *testing.T, objectID oc.ObjectID, reason oc.CleanupReason) oc.ObjectCleanupCause {
	t.Helper()
	c, e := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: id[oc.CleanupOperation](t), Owner: f.owner, Reason: reason})
	if e != nil {
		t.Fatal(e)
	}
	f.recordCause(t, c, objectID, f.upload(t, objectID))
	return c
}
func (f *knowledgeCleanupFixture) recordCause(t *testing.T, c oc.ObjectCleanupCause, objectID oc.ObjectID, upload oc.UploadID) {
	t.Helper()
	d := c.Details()
	o := d.Owner.Details()
	f.sql(t, `INSERT INTO object_fixture.knowledge_cleanup(operation_id,object_id,upload_id,owner_kind,owner_id,project_id,reason) VALUES($1,$2,$3,$4,$5,$6,$7)`, d.OperationID.String(), objectID.String(), upload.String(), string(o.Kind), o.ID, o.ProjectID, string(d.Reason))
}
func (f *knowledgeCleanupFixture) release(t *testing.T, c oc.ObjectCleanupCause, objectID oc.ObjectID, upload oc.UploadID, rollback bool) error {
	t.Helper()
	request, e := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: c, ObjectID: objectID, UploadID: upload})
	if e != nil {
		return e
	}
	plan, e := f.objects.DiscoverAccess(contextFor(t), request)
	if e != nil {
		return e
	}
	r := plannedTx(f.store, f.objects, contextFor(t), cause(t), plan, func(ctx context.Context, tx foundation.Tx, p oc.AccessLockPlan, l oc.LockedAccess) error {
		if e := f.objects.ReleaseForCleanupInTx(ctx, tx, c, objectID, p, l); e != nil {
			return e
		}
		if rollback {
			return fault(foundation.InvalidState)
		}
		return nil
	})
	if r.State() == foundation.Committed {
		return nil
	}
	if r.Fault() != nil {
		return r.Fault()
	}
	return fault(foundation.CommitUnknown)
}

type knowledgePublicationState struct {
	references, operations int
	disposition            string
	cleaning               bool
}

func (f *knowledgeCleanupFixture) state(t *testing.T, objectID oc.ObjectID) knowledgePublicationState {
	t.Helper()
	var s knowledgePublicationState
	e := f.store.QueryRow(contextFor(t), `SELECT (SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1),(SELECT count(*) FROM agenteam_object.cleanup_operations WHERE object_id=$1),u.disposition,o.cleaning FROM agenteam_object.uploads u JOIN agenteam_object.objects o ON o.id=u.object_id WHERE o.id=$1`, objectID.String()).Scan(&s.references, &s.operations, &s.disposition, &s.cleaning)
	if e != nil {
		t.Fatal(e)
	}
	return s
}

func TestKnowledgeReferenceCleanupCanonicalReservedRollbackAndReplay(t *testing.T) {
	for _, reason := range []oc.CleanupReason{oc.ReplacedObject, oc.CancelledUpload, oc.OwnerDeleted} {
		for _, prospective := range []bool{false, true} {
			name := string(reason) + "/canonical"
			if prospective {
				name = string(reason) + "/reserved"
			}
			t.Run(name, func(t *testing.T) {
				f := newKnowledgeCleanupFixture(t, prospective)
				put := f.putKnowledge(t, "closed-publication", "actual object bytes")
				upload := f.upload(t, put.Meta.ID)
				cleanup := f.cleanupCause(t, put.Meta.ID, reason)
				before := f.state(t, put.Meta.ID)
				if before.references != 1 || before.cleaning || before.operations != 0 {
					t.Fatal("invalid initial publication", before)
				}
				requireCode(t, f.release(t, cleanup, put.Meta.ID, upload, true), foundation.InvalidState)
				if got := f.state(t, put.Meta.ID); got != before {
					t.Fatal("cleanup effects escaped rollback", got)
				}
				if e := f.release(t, cleanup, put.Meta.ID, upload, false); e != nil {
					t.Fatal(e)
				}
				after := f.state(t, put.Meta.ID)
				if after.references != 0 || after.disposition != "revoked" || !after.cleaning || after.operations < 1 {
					t.Fatal("publication not gated", after)
				}
				if e := f.release(t, cleanup, put.Meta.ID, upload, false); e != nil {
					t.Fatal("same cause replay", e)
				}
				if got := f.state(t, put.Meta.ID); got != after {
					t.Fatal("same cause appended cleanup", got)
				}
				// Ordinary attach must not reopen the revoked original upload.
				p := ownerPlan(t, f.objects, f.actor, f.owner, oc.AttachAccess, oc.AccessRequestDetails{ObjectID: put.Meta.ID})
				r := plannedTx(f.store, f.objects, contextFor(t), cause(t), p, func(ctx context.Context, tx foundation.Tx, p oc.AccessLockPlan, l oc.LockedAccess) error {
					_, e := f.objects.AttachObjectInTx(ctx, tx, f.actor, f.owner, put.Meta.ID, p, l)
					return e
				})
				if r.State() == foundation.Committed {
					t.Fatal("late attach reopened publication")
				}
				if prospective {
					if _, e := f.objects.OpenUploadSource(contextFor(t), f.actor, f.owner, put.Receipt); e == nil {
						t.Fatal("revoked upload opened as a new source")
					}
				}
				result, e := f.objects.DeleteUnreferenced(contextFor(t), cleanup, put.Meta.ID)
				if e != nil || result.State != oc.CleanupCompleted {
					t.Fatal("real ordinary cleanup incomplete", result.State, e)
				}
			})
		}
	}
}

func TestKnowledgeReferenceCleanupRejectsForeignAndChangedCause(t *testing.T) {
	f := newKnowledgeCleanupFixture(t, false)
	first := f.putKnowledge(t, "first", "first canonical")
	second := f.putKnowledge(t, "second", "second canonical")
	upload := f.upload(t, first.Meta.ID)
	otherUpload := f.upload(t, second.Meta.ID)
	cleanup := f.cleanupCause(t, first.Meta.ID, oc.OwnerDeleted)
	before := f.state(t, first.Meta.ID)
	beforeOther := f.state(t, second.Meta.ID)
	for _, kind := range []string{"project", "owner", "object", "upload", "cause", "reason"} {
		t.Run(kind, func(t *testing.T) {
			d := cleanup.Details()
			objectID, u := first.Meta.ID, upload
			switch kind {
			case "project":
				d.Owner, _ = oc.NewObjectOwner(oc.Knowledge, d.Owner.Details().ID, id[struct{}](t).String())
			case "owner":
				d.Owner, _ = oc.NewObjectOwner(oc.Knowledge, id[struct{}](t).String(), f.project.String())
			case "object":
				objectID, u = second.Meta.ID, otherUpload
			case "upload":
				u = otherUpload
			case "cause":
				d.OperationID = id[oc.CleanupOperation](t)
			case "reason":
				d.Reason = oc.ReplacedObject
			}
			forged, e := oc.NewObjectCleanupCause(d)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.release(t, forged, objectID, u, false); e == nil {
				t.Fatal("foreign/changed cleanup accepted")
			}
			if f.state(t, first.Meta.ID) != before || f.state(t, second.Meta.ID) != beforeOther {
				t.Fatal("rejected cleanup changed persistent facts")
			}
		})
	}
	if e := f.release(t, cleanup, first.Meta.ID, upload, false); e != nil {
		t.Fatal(e)
	}
	gated := f.state(t, first.Meta.ID)
	// Even independently registered eligible causes cannot take over a gate
	// already closed under another operation or another reason.
	for _, changeReason := range []bool{false, true} {
		d := cleanup.Details()
		if changeReason {
			d.Reason = oc.ReplacedObject
		} else {
			d.OperationID = id[oc.CleanupOperation](t)
		}
		other, _ := oc.NewObjectCleanupCause(d)
		f.recordCause(t, other, first.Meta.ID, upload)
		requireCode(t, f.release(t, other, first.Meta.ID, upload, false), foundation.Forbidden)
		if f.state(t, first.Meta.ID) != gated {
			t.Fatal("different cause changed original gate")
		}
	}
}

func TestKnowledgeReferenceCleanupDoesNotRemoveAnotherOwnerReference(t *testing.T) {
	f := newKnowledgeCleanupFixture(t, false)
	put := f.putKnowledge(t, "defensive-reference", "actual object bytes")
	cleanup := f.cleanupCause(t, put.Meta.ID, oc.ReplacedObject)
	upload := f.upload(t, put.Meta.ID)
	other, _ := oc.NewObjectOwner(oc.Knowledge, id[struct{}](t).String(), f.project.String())
	// An unexpected persisted reference is a defensive fixture, not a legal
	// cross-owner attachment: the real Attach port rejects a different owner.
	// Keep the original object, upload and project; vary only the reference owner.
	f.sql(t, `INSERT INTO agenteam_object.object_references(object_id,owner_kind,owner_id,partition_id,kind,upload_id) VALUES($1,'knowledge',$2,$3,'canonical',$4)`, put.Meta.ID.String(), other.Details().ID, f.project.String(), upload.String())
	references := func() string {
		t.Helper()
		var rows string
		if e := f.store.QueryRow(contextFor(t), `SELECT jsonb_agg(to_jsonb(r) ORDER BY owner_kind,owner_id)::text FROM agenteam_object.object_references r WHERE object_id=$1`, put.Meta.ID.String()).Scan(&rows); e != nil {
			t.Fatal(e)
		}
		return rows
	}
	before := f.state(t, put.Meta.ID)
	beforeReferences := references()
	if before.references != 2 {
		t.Fatal("defensive persisted reference missing")
	}
	requireCode(t, f.release(t, cleanup, put.Meta.ID, upload, false), foundation.ResourceBusy)
	if f.state(t, put.Meta.ID) != before || references() != beforeReferences {
		t.Fatal("cleanup deleted a different owner's reference or failed to roll back")
	}
}

func TestKnowledgeReferenceCleanupProtectsActualReaderUntilClose(t *testing.T) {
	f := newKnowledgeCleanupFixture(t, true)
	body := strings.Repeat("k", 256<<10)
	put := f.putKnowledge(t, "reader", body)
	key := f.physical(t, put.Meta.ID)
	reader, e := f.objects.OpenUploadSource(contextFor(t), f.actor, f.owner, put.Receipt)
	if e != nil {
		t.Fatal(e)
	}
	defer reader.Close()
	// Open primes only the fixed 64 KiB integrity tail. The 256 KiB payload
	// must still own this actual source lease before cleanup starts.
	var leaseID string
	if e = f.store.QueryRow(contextFor(t), `SELECT id::text FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='source' AND owner_id=id AND process_id=$2 AND state='active'`, put.Meta.ID.String(), f.process.String()).Scan(&leaseID); e != nil {
		t.Fatal("reader has no active exact source lease", e)
	}
	cleanup := f.cleanupCause(t, put.Meta.ID, oc.OwnerDeleted)
	if e = f.release(t, cleanup, put.Meta.ID, f.upload(t, put.Meta.ID), false); e != nil {
		t.Fatal(e)
	}
	result, e := f.objects.DeleteUnreferencedWithinBudget(contextFor(t), cleanup, put.Meta.ID)
	if e != nil || result.State != oc.CleanupPending {
		t.Fatal("live reader not protected", result.State, e)
	}
	if _, e = f.s3.StatObject(contextFor(t), f.bucket, key, minio.StatObjectOptions{}); e != nil {
		t.Fatal("live reader payload removed", e)
	}
	if _, e = f.objects.OpenUploadSource(contextFor(t), f.actor, f.owner, put.Receipt); e == nil {
		t.Fatal("new reader admitted after gate")
	}
	n, e := io.Copy(io.Discard, reader)
	if e != nil || n != int64(len(body)) {
		t.Fatal("existing reader failed", n, e)
	}
	if e = reader.Close(); e != nil {
		t.Fatal(e)
	}
	var released bool
	if e = f.store.QueryRow(contextFor(t), `SELECT state='released' AND released_at IS NOT NULL FROM agenteam_object.object_leases WHERE id=$1 AND object_id=$2 AND owner_kind='source' AND owner_id=id AND process_id=$3`, leaseID, put.Meta.ID.String(), f.process.String()).Scan(&released); e != nil || !released {
		t.Fatal("actual reader completion did not release its exact lease", e)
	}
	result, e = f.objects.DeleteUnreferencedWithinBudget(contextFor(t), cleanup, put.Meta.ID)
	if e != nil || result.State != oc.CleanupCompleted {
		t.Fatal("cleanup did not complete after reader join", result.State, e)
	}
	if _, e = f.s3.StatObject(contextFor(t), f.bucket, key, minio.StatObjectOptions{}); minio.ToErrorResponse(e).Code != "NoSuchKey" {
		t.Fatal("physical payload not deleted", e)
	}
}
