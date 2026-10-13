package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func (a *Authority) CheckCleanupInTx(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID) error {
	return a.checkCleanup(ctx, tx, cause, object, nil)
}

func (a *Authority) checkCleanup(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID, upload *oc.UploadID) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if ctx == nil || cause.Validate() != nil || object.Validate() != nil || upload != nil && upload.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return unavailable(err)
	}
	d := cause.Details()
	p, k, err := knowledgeOwner(d.Owner)
	if err != nil {
		return err
	}
	if d.Reason == oc.ProjectDeleted {
		return a.checkProjectObjectRetirement(ctx, tx, cause, object, upload)
	}
	switch d.Reason {
	case oc.ReplacedObject, oc.CancelledUpload, oc.OwnerDeleted:
	default:
		return fault(f.Forbidden)
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	locks, err := authorityLocks(id.Actor{}, p, id.Converge)
	if err != nil {
		return err
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	if st.ownership != nil {
		if err = st.ownership.service.projectCallTx(ctx, tx, p); err != nil {
			return err
		}
	}
	var found bool
	var exactUpload any
	if upload != nil {
		exactUpload = upload.String()
	}
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.object_cleanup c
 WHERE c.id=$1 AND c.project_id=$2 AND c.document_id=$3 AND c.object_id=$4 AND c.reason=$5
 AND ($6::uuid IS NULL OR c.upload_id=$6::uuid) AND c.phase IN ('reference','object','completed')
 AND NOT EXISTS(SELECT 1 FROM agenteam_knowledge.documents d WHERE d.project_id=c.project_id
 AND d.id=c.document_id AND d.current_object_id=c.object_id))`, d.OperationID.String(), p.String(), k.String(), object.String(), string(d.Reason), exactUpload).Scan(&found)
	if err != nil {
		return unavailable(err)
	}
	if !found {
		return fault(f.Forbidden)
	}
	return nil
}

func (a *Authority) CheckProjectCleanupInTx(context.Context, f.Tx, id.Actor, oc.ProjectCleanupCause) error {
	// B03 must bind the real Project lifecycle registration and stop barrier.
	// A per-document cleanup record is never a Project deletion grant.
	return fault(f.DependencyUnbound)
}

var _ oc.CleanupAuthority = (*Authority)(nil)

type knowledgeProjectLeaf struct {
	ID             oc.CleanupID   `json:"id"`
	Operation      pc.OperationID `json:"operation_id"`
	Project        id.ProjectID   `json:"project_id"`
	ProjectVersion f.Version      `json:"project_version"`
	Document       kc.DocumentID  `json:"document_id"`
	Object         oc.ObjectID    `json:"object_id"`
	Upload         oc.UploadID    `json:"upload_id"`
	Phase          string         `json:"phase"`
	Version        f.Version      `json:"version"`
}
type knowledgeLeafUnknown struct {
	leaf          knowledgeProjectLeaf
	result        f.CommitResult
	before, after []byte
}

func (l knowledgeProjectLeaf) valid() bool {
	return l.ID.Validate() == nil && l.Operation.Validate() == nil && l.Project.Validate() == nil && l.ProjectVersion.Validate() == nil && l.Document.Validate() == nil && l.Object.Validate() == nil && l.Upload.Validate() == nil && l.Version.Validate() == nil && (l.Phase == "reference" || l.Phase == "object" || l.Phase == "metadata" || l.Phase == "completed")
}
func (l knowledgeProjectLeaf) cause() (oc.ObjectCleanupCause, error) {
	owner, err := oc.NewObjectOwner(oc.Knowledge, l.Document.String(), l.Project.String())
	if err != nil {
		return oc.ObjectCleanupCause{}, portError(err)
	}
	return oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: l.ID, Owner: owner, Reason: oc.ProjectDeleted})
}
func loadKnowledgeProjectLeaf(ctx context.Context, x postgres.SQLExecutor, p id.ProjectID, key oc.CleanupID) (*knowledgeProjectLeaf, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT to_jsonb(l)||jsonb_build_object('project_version',l.project_version::text,'version',l.version::text) FROM agenteam_knowledge.project_object_retirements l WHERE project_id=$1::uuid AND id=$2::uuid`, p.String(), key.String()).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	var l knowledgeProjectLeaf
	if json.Unmarshal(raw, &l) != nil || !l.valid() || l.Project != p || l.ID != key {
		return nil, internal(nil)
	}
	return &l, nil
}
func (a *Authority) checkProjectObjectRetirement(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID, upload *oc.UploadID) error {
	st := a.state()
	if st == nil || st.ownership == nil || st.ownership.service == nil {
		return fault(f.DependencyUnbound)
	}
	p, doc, err := knowledgeOwner(cause.Details().Owner)
	if err != nil {
		return err
	}
	w, ok := ctx.Value(projectCleanupContextKey{}).(projectCleanupWitness)
	if !ok || w.state != st.ownership || w.call == nil || w.call.scope.ProjectID != p {
		return fault(f.Forbidden)
	}
	if err = st.ownership.service.requireProjectCleanupCallInTx(ctx, tx, p, w.call.cause.OperationID); err != nil {
		return err
	}
	x, err := st.store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	l, err := loadKnowledgeProjectLeaf(ctx, x, p, cause.Details().OperationID)
	if err != nil {
		return err
	}
	if l == nil || l.Document != doc || l.Object != object || upload != nil && l.Upload != *upload || l.Phase == "completed" {
		return fault(f.Forbidden)
	}
	if l.Operation != w.call.cause.OperationID || l.ProjectVersion != w.call.cause.ProjectVersion {
		return fault(f.Forbidden)
	}
	locks, err := authorityLocks(id.Actor{}, p, id.Converge)
	if err != nil {
		return err
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	var active bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_knowledge.documents WHERE project_id=$1::uuid AND id=$2::uuid AND current_object_id=$3::uuid)`, p.String(), doc.String(), object.String()).Scan(&active); err != nil {
		return unavailable(err)
	}
	if active {
		return fault(f.Forbidden)
	}
	return ctx.Err()
}

func (s *Service) knowledgeProjectCleanupLocks(ctx context.Context, p id.ProjectID, op pc.OperationID) ([]f.LockRequest, error) {
	if ctx == nil || p.Validate() != nil || op.Validate() != nil || s.knowledgeSourceState() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	tree, err := f.KnowledgeTreeLock(p.String())
	if err != nil {
		return nil, portError(err)
	}
	return []f.LockRequest{knowledgeProjectLock(p, f.Exclusive), {Key: tree, Mode: f.Exclusive}}, nil
}

// The selected leaf and document are a bounded Knowledge-owned preimage. The
// Object owner proves its own physical/metadata state; Knowledge never reads
// or deletes Object tables to manufacture that proof.
func knowledgeLeafImage(ctx context.Context, x postgres.SQLExecutor, l knowledgeProjectLeaf) ([]byte, error) {
	var raw []byte
	err := x.QueryRow(ctx, `SELECT jsonb_build_array(
 (SELECT to_jsonb(r) FROM agenteam_knowledge.project_object_retirements r WHERE project_id=$1::uuid AND id=$2::uuid),
 (SELECT to_jsonb(d) FROM agenteam_knowledge.documents d WHERE project_id=$1::uuid AND id=$3::uuid))`, l.Project.String(), l.ID.String(), l.Document.String()).Scan(&raw)
	if err != nil {
		return nil, unavailable(err)
	}
	if len(raw) == 0 || len(raw) > 65536 {
		return nil, internal(nil)
	}
	return raw, nil
}
func (s *Service) knowledgeLeafTx(ctx context.Context, l knowledgeProjectLeaf, extra []f.LockRequest, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	st := s.knowledgeSourceState()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	locks, err := s.knowledgeProjectCleanupLocks(ctx, l.Project, l.Operation)
	if err != nil {
		return err
	}
	locks = append(locks, extra...)
	cause, _ := f.NewRecoveryCause("knowledge-project-object", l.ID.String(), l.Operation.String())
	saved := &knowledgeLeafUnknown{leaf: l}
	valid := false
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.requireProjectCleanupCallInTx(ctx, tx, l.Project, l.Operation); err != nil {
			return err
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		saved.before, err = knowledgeLeafImage(ctx, x, l)
		if err != nil {
			return err
		}
		if err = fn(ctx, tx, x); err != nil {
			return err
		}
		saved.after, err = knowledgeLeafImage(ctx, x, l)
		if err != nil {
			return err
		}
		valid = ctx.Err() == nil
		return ctx.Err()
	})
	if result.State() == f.Unknown {
		if result.AttemptID().Validate() != nil || result.Cause().Validate() != nil {
			return internal(nil)
		}
		saved.result = result
		st.mu.Lock()
		if st.leafUnknown == nil {
			st.leafUnknown = map[id.ProjectID]*knowledgeLeafUnknown{}
		}
		if st.leafUnknown[l.Project] == nil {
			st.leafUnknown[l.Project] = saved
		}
		first := st.leafUnknown[l.Project].result
		st.mu.Unlock()
		return txError(first)
	}
	if result.State() == f.Committed && (!valid || ctx.Err() != nil) {
		return unavailable(ctx.Err())
	}
	return txError(result)
}
func (s *Service) confirmKnowledgeLeaf(ctx context.Context, saved *knowledgeLeafUnknown) error {
	st := s.knowledgeSourceState()
	l := saved.leaf
	locks, err := s.knowledgeProjectCleanupLocks(ctx, l.Project, l.Operation)
	if err != nil {
		return err
	}
	valid := false
	result := st.store.WithinTx(ctx, saved.result.Cause(), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.requireProjectCleanupCallInTx(ctx, tx, l.Project, l.Operation); err != nil {
			return err
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		raw, err := knowledgeLeafImage(ctx, x, l)
		if err != nil {
			return err
		}
		if len(saved.before) == 0 || !bytes.Equal(raw, saved.before) && !bytes.Equal(raw, saved.after) {
			return fault(f.ResourceBusy)
		}
		valid = ctx.Err() == nil
		return ctx.Err()
	})
	if result.State() != f.Committed || !valid || ctx.Err() != nil {
		return errors.Join(txError(saved.result), txError(result), ctx.Err())
	}
	st.mu.Lock()
	if st.leafUnknown[l.Project] == saved {
		delete(st.leafUnknown, l.Project)
	}
	st.mu.Unlock()
	return nil
}

const knowledgeObjectCandidates = `SELECT id AS document_id,current_object_id AS object_id,current_upload_id AS upload_id FROM agenteam_knowledge.documents WHERE project_id=$1::uuid AND current_object_id IS NOT NULL
 UNION SELECT document_id,object_id,upload_id FROM agenteam_knowledge.object_cleanup WHERE project_id=$1::uuid
 UNION SELECT document_id,object_id,upload_id FROM agenteam_knowledge.publications WHERE project_id=$1::uuid AND object_id IS NOT NULL`

func (s *Service) prepareKnowledgeProjectObjects(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) (bool, error) {
	st := s.knowledgeSourceState()
	if st == nil {
		return false, fault(f.DependencyUnbound)
	}
	st.mu.Lock()
	saved := st.leafUnknown[scope.ProjectID]
	st.mu.Unlock()
	if saved != nil {
		if saved.leaf.Operation != cause.OperationID || saved.leaf.ProjectVersion != cause.ProjectVersion {
			return false, fault(f.ResourceBusy)
		}
		return false, s.confirmKnowledgeLeaf(ctx, saved)
	}
	var raw []byte
	err := st.store.QueryRow(ctx, `SELECT to_jsonb(l)||jsonb_build_object('project_version',l.project_version::text,'version',l.version::text) FROM agenteam_knowledge.project_object_retirements l WHERE project_id=$1::uuid AND phase<>'completed' ORDER BY id LIMIT 1`, scope.ProjectID.String()).Scan(&raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, unavailable(err)
	}
	var l knowledgeProjectLeaf
	if err == nil {
		if json.Unmarshal(raw, &l) != nil || !l.valid() || l.Operation != cause.OperationID || l.ProjectVersion != cause.ProjectVersion || l.Project != scope.ProjectID {
			return false, fault(f.ResourceBusy)
		}
		return false, s.advanceKnowledgeProjectLeaf(ctx, l)
	}
	var document, object, upload string
	err = st.store.QueryRow(ctx, `WITH candidates AS (`+knowledgeObjectCandidates+`) SELECT c.document_id::text,c.object_id::text,c.upload_id::text FROM candidates c
 WHERE NOT EXISTS(SELECT 1 FROM agenteam_knowledge.project_object_retirements l WHERE l.project_id=$1::uuid AND l.operation_id=$2::uuid AND l.document_id=c.document_id AND l.object_id=c.object_id AND l.upload_id=c.upload_id)
 ORDER BY c.object_id LIMIT 1`, scope.ProjectID.String(), cause.OperationID.String()).Scan(&document, &object, &upload)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, unavailable(err)
	}
	l = knowledgeProjectLeaf{Project: scope.ProjectID, Operation: cause.OperationID, ProjectVersion: cause.ProjectVersion, Phase: "reference", Version: 1}
	if l.ID, err = f.NewID[oc.CleanupOperation](); err != nil {
		return false, unavailable(err)
	}
	if l.Document, err = f.ParseID[kc.Document](document); err != nil {
		return false, internal(err)
	}
	if l.Object, err = f.ParseID[oc.StoredObject](object); err != nil {
		return false, internal(err)
	}
	if l.Upload, err = f.ParseID[oc.Upload](upload); err != nil {
		return false, internal(err)
	}
	err = s.knowledgeLeafTx(ctx, l, nil, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		var found bool
		if err := x.QueryRow(ctx, `WITH candidates AS (`+knowledgeObjectCandidates+`) SELECT EXISTS(SELECT 1 FROM candidates WHERE document_id=$2::uuid AND object_id=$3::uuid AND upload_id=$4::uuid)`, l.Project.String(), l.Document.String(), l.Object.String(), l.Upload.String()).Scan(&found); err != nil {
			return unavailable(err)
		}
		if !found {
			return fault(f.ResourceBusy)
		}
		_, err := x.Exec(ctx, `INSERT INTO agenteam_knowledge.project_object_retirements(id,operation_id,project_id,project_version,document_id,object_id,upload_id,phase,version) VALUES($1,$2,$3,$4,$5,$6,$7,'reference',1)`, l.ID.String(), l.Operation.String(), l.Project.String(), int64(l.ProjectVersion), l.Document.String(), l.Object.String(), l.Upload.String())
		if err != nil {
			return unavailable(err)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_knowledge.documents SET status='deleted',parent_document_id=NULL,title=NULL,source_kind=NULL,media_type=NULL,current_object_id=NULL,current_upload_id=NULL,indexing_status=NULL,creator_user_id=NULL,created_at=NULL,updated_at=NULL,deleted_at=clock_timestamp() WHERE project_id=$1::uuid AND id=$2::uuid AND current_object_id=$3::uuid AND current_upload_id=$4::uuid`, l.Project.String(), l.Document.String(), l.Object.String(), l.Upload.String())
		if err != nil {
			return unavailable(err)
		}
		return nil
	})
	return false, err
}

func (s *Service) advanceKnowledgeProjectLeaf(ctx context.Context, l knowledgeProjectLeaf) error {
	cause, err := l.cause()
	if err != nil {
		return err
	}
	switch l.Phase {
	case "reference":
		request, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: cause, ObjectID: l.Object, UploadID: l.Upload})
		if err != nil {
			return portError(err)
		}
		plan, err := s.state().deps.Objects.DiscoverAccess(ctx, request)
		if err != nil {
			return portError(err)
		}
		return s.advanceKnowledgeLeafPhase(ctx, l, "object", plan.Details().Locks, func(ctx context.Context, tx f.Tx) error {
			locked, err := s.state().deps.Objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
			if err != nil {
				return portError(err)
			}
			if err = s.state().deps.ReferenceCleanup.ReleaseForCleanupInTx(ctx, tx, cause, l.Object, plan, locked); err != nil {
				return portError(err)
			}
			x, err := s.state().store.InTx(tx)
			if err != nil {
				return portError(err)
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_knowledge.object_cleanup SET phase='object' WHERE project_id=$1::uuid AND document_id=$2::uuid AND object_id=$3::uuid AND upload_id=$4::uuid AND phase='reference'`, l.Project.String(), l.Document.String(), l.Object.String(), l.Upload.String())
			if err != nil {
				return unavailable(err)
			}
			return nil
		})
	case "object":
		result, err := s.state().ownership.reads.DeleteKnowledgeProjectObject(ctx, cause, l.Object)
		if err != nil {
			return portError(err)
		}
		if result.OperationID != l.ID || result.State != oc.CleanupCompleted || len(result.Remaining.References) != 0 || len(result.Remaining.ActiveLeases) != 0 {
			return fault(f.ResourceBusy)
		}
		return s.advanceKnowledgeLeafPhase(ctx, l, "metadata", nil, func(ctx context.Context, tx f.Tx) error {
			x, err := s.state().store.InTx(tx)
			if err != nil {
				return portError(err)
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_knowledge.object_cleanup SET phase='completed' WHERE project_id=$1::uuid AND document_id=$2::uuid AND object_id=$3::uuid AND upload_id=$4::uuid AND phase='object'`, l.Project.String(), l.Document.String(), l.Object.String(), l.Upload.String())
			if err != nil {
				return unavailable(err)
			}
			return nil
		})
	case "metadata":
		request, err := oc.NewObjectCleanupAccess(oc.AccessRequestDetails{Operation: oc.PurgeDeletedObjectMetadataAccess, Cleanup: cause, ObjectID: l.Object})
		if err != nil {
			return portError(err)
		}
		plan, err := s.state().deps.Objects.DiscoverAccess(ctx, request)
		if err != nil {
			return portError(err)
		}
		purger, ok := s.state().deps.Objects.(oc.DeletedObjectMetadataPurger)
		if !ok {
			return fault(f.DependencyUnbound)
		}
		return s.knowledgeLeafTx(ctx, l, plan.Details().Locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
			if err := requireKnowledgeLeaf(ctx, x, l); err != nil {
				return err
			}
			locked, err := s.state().deps.Objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
			if err != nil {
				return portError(err)
			}
			result, err := purger.PurgeDeletedObjectMetadataInTx(ctx, tx, cause, l.Object, plan, locked)
			if err != nil {
				return portError(err)
			}
			if result.OperationID != l.ID || result.ObjectID != l.Object {
				return internal(nil)
			}
			if result.State == oc.CleanupCompleted {
				return updateKnowledgeLeafPhase(ctx, x, l, "completed")
			}
			if result.State != oc.CleanupPending {
				return internal(nil)
			}
			return nil
		})
	default:
		return fault(f.InvalidState)
	}
}
func requireKnowledgeLeaf(ctx context.Context, x postgres.SQLExecutor, l knowledgeProjectLeaf) error {
	current, err := loadKnowledgeProjectLeaf(ctx, x, l.Project, l.ID)
	if err != nil {
		return err
	}
	if current == nil || *current != l {
		return fault(f.ResourceBusy)
	}
	return nil
}
func updateKnowledgeLeafPhase(ctx context.Context, x postgres.SQLExecutor, l knowledgeProjectLeaf, phase string) error {
	tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.project_object_retirements SET phase=$3,version=version+1 WHERE id=$1::uuid AND project_id=$2::uuid AND phase=$4 AND version=$5`, l.ID.String(), l.Project.String(), phase, l.Phase, int64(l.Version))
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}
func (s *Service) advanceKnowledgeLeafPhase(ctx context.Context, l knowledgeProjectLeaf, phase string, locks []f.LockRequest, fn func(context.Context, f.Tx) error) error {
	return s.knowledgeLeafTx(ctx, l, locks, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := requireKnowledgeLeaf(ctx, x, l); err != nil {
			return err
		}
		if fn != nil {
			if err := fn(ctx, tx); err != nil {
				return err
			}
		}
		return updateKnowledgeLeafPhase(ctx, x, l, phase)
	})
}
func (s *Service) requireKnowledgeProjectObjectsRetiredInTx(ctx context.Context, tx f.Tx, p id.ProjectID, op pc.OperationID) error {
	if err := s.requireProjectCleanupCallInTx(ctx, tx, p, op); err != nil {
		return err
	}
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	w, ok := ctx.Value(projectCleanupContextKey{}).(projectCleanupWitness)
	if !ok || w.call == nil {
		return fault(f.Forbidden)
	}
	var pending bool
	err = x.QueryRow(ctx, `WITH candidates AS (`+knowledgeObjectCandidates+`) SELECT
 EXISTS(SELECT 1 FROM candidates c WHERE NOT EXISTS(SELECT 1 FROM agenteam_knowledge.project_object_retirements l WHERE l.project_id=$1::uuid AND l.operation_id=$2::uuid AND l.document_id=c.document_id AND l.object_id=c.object_id AND l.upload_id=c.upload_id AND l.phase='completed' AND l.project_version=$3))
 OR EXISTS(SELECT 1 FROM agenteam_knowledge.project_object_retirements WHERE project_id=$1::uuid AND (operation_id<>$2::uuid OR phase<>'completed' OR project_version<>$3))`, p.String(), op.String(), int64(w.call.cause.ProjectVersion)).Scan(&pending)
	if err != nil {
		return unavailable(err)
	}
	if pending {
		return fault(f.ResourceBusy)
	}
	return ctx.Err()
}

type cleanupRecord struct {
	id       oc.CleanupID
	project  id.ProjectID
	document kc.DocumentID
	command  f.ID[command]
	object   oc.ObjectID
	upload   oc.UploadID
	reason   oc.CleanupReason
	phase    string
}

func loadCleanup(ctx context.Context, x postgres.SQLExecutor, key oc.CleanupID) (*cleanupRecord, error) {
	var operation, project, document, commandID, object, upload, reason, phase string
	query := `SELECT id::text,project_id::text,document_id::text,command_id::text,object_id::text,upload_id::text,reason,phase
 FROM agenteam_knowledge.object_cleanup WHERE id=$1`
	args := []any{key.String()}
	if p, ok := ctx.Value(cleanupProjectContextKey{}).(id.ProjectID); ok {
		query += ` AND project_id=$2::uuid`
		args = append(args, p.String())
	}
	err := x.QueryRow(ctx, query, args...).Scan(&operation, &project, &document, &commandID, &object, &upload, &reason, &phase)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	r := &cleanupRecord{reason: oc.CleanupReason(reason), phase: phase}
	if r.id, err = f.ParseID[oc.CleanupOperation](operation); err != nil {
		return nil, internal(err)
	}
	if r.project, err = f.ParseID[id.Project](project); err != nil {
		return nil, internal(err)
	}
	if r.document, err = f.ParseID[kc.Document](document); err != nil {
		return nil, internal(err)
	}
	if r.command, err = f.ParseID[command](commandID); err != nil {
		return nil, internal(err)
	}
	if r.object, err = f.ParseID[oc.StoredObject](object); err != nil {
		return nil, internal(err)
	}
	if r.upload, err = f.ParseID[oc.Upload](upload); err != nil {
		return nil, internal(err)
	}
	if r.id != key || r.reason != oc.OwnerDeleted && r.reason != oc.ReplacedObject && r.reason != oc.CancelledUpload || r.phase != "reference" && r.phase != "object" && r.phase != "completed" {
		return nil, internal(nil)
	}
	return r, nil
}
func (r cleanupRecord) cause() (oc.ObjectCleanupCause, error) {
	owner, err := oc.NewObjectOwner(oc.Knowledge, r.document.String(), r.project.String())
	if err != nil {
		return oc.ObjectCleanupCause{}, internal(err)
	}
	return oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: r.id, Owner: owner, Reason: r.reason})
}

func (s *Service) recoverCleanup(ctx context.Context, key oc.CleanupID) error {
	st := s.state()
	if st.ownership != nil {
		if p, ok := ctx.Value(cleanupProjectContextKey{}).(id.ProjectID); ok {
			if err := s.requireProjectCall(ctx, p, nil, projectCallRecovery); err != nil {
				return err
			}
		} else {
			w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
			if !ok || w.state != st.ownership || w.owner == nil {
				return fault(f.DependencyUnbound)
			}
			if err := s.requireProjectCall(ctx, w.owner.record.Project, nil, projectCallRead); err != nil {
				return err
			}
			ctx = context.WithValue(ctx, cleanupProjectContextKey{}, w.owner.record.Project)
		}
	}
	row, err := loadCleanup(ctx, st.store, key)
	if err != nil {
		return err
	}
	if row == nil || row.phase == "completed" {
		return nil
	}
	cause, err := row.cause()
	if err != nil {
		return err
	}
	transaction, err := f.NewRecoveryCause("knowledge.cleanup", key.String(), "")
	if err != nil {
		return portError(err)
	}
	if row.phase == "reference" {
		if row.reason == oc.CancelledUpload {
			registration, err := id.RegisterService(id.ObjectMaintenance)
			if err != nil {
				return internal(err)
			}
			scope, err := id.InProject(row.project)
			if err != nil {
				return internal(err)
			}
			actor, err := registration.Actor(row.id.String(), scope)
			if err != nil {
				return internal(err)
			}
			// The private D05 command key is the persisted Knowledge command UUID,
			// fixed before preparation. Never generate a new upload identity here.
			_, err = st.deps.Objects.CancelUpload(ctx, actor, cause.Details().Owner, f.IdempotencyKey(row.command.String()))
			var known *f.Fault
			if err != nil && (!errors.As(err, &known) || known.Code != f.InvalidState) {
				return portError(err)
			}
			// An already-attached upload requires ReferenceCleanup. A successful
			// cancellation is confirmed idempotently through that same exact gate.
		}
		request, err := oc.NewCleanupReleaseAccess(oc.AccessRequestDetails{Operation: oc.ReleaseForCleanupAccess, Cleanup: cause, ObjectID: row.object, UploadID: row.upload})
		if err != nil {
			return internal(err)
		}
		plan, err := st.deps.Objects.DiscoverAccess(ctx, request)
		if err != nil {
			return portError(err)
		}
		result := st.store.WithinTx(ctx, transaction, func(ctx context.Context, tx f.Tx) error {
			access, err := st.deps.Objects.AcquireAccessPlansInTx(ctx, tx, []oc.AccessLockPlan{plan}, nil)
			if err != nil {
				return portError(err)
			}
			x, err := st.store.InTx(tx)
			if err != nil {
				return portError(err)
			}
			if err = st.store.RequireHeldLocks(ctx, tx, access.Locks()); err != nil {
				return portError(err)
			}
			current, err := loadCleanup(ctx, x, key)
			if err != nil {
				return err
			}
			if current == nil || *current != *row {
				return fault(f.ResourceBusy)
			}
			if err = st.deps.ReferenceCleanup.ReleaseForCleanupInTx(ctx, tx, cause, row.object, plan, access); err != nil {
				return portError(err)
			}
			tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.object_cleanup SET phase='object' WHERE id=$1 AND phase='reference'`, key.String())
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return internal(nil)
			}
			return nil
		})
		if err = txError(result); err != nil {
			return err
		}
		row.phase = "object"
	}
	// The real cleaner owns reader/writer join and physical deletion. Pending,
	// Unknown and any error keep the exact durable work for another attempt.
	result, err := st.deps.ObjectCleanup.DeleteUnreferenced(ctx, cause, row.object)
	if err != nil {
		return portError(err)
	}
	if result.OperationID != key {
		return internal(nil)
	}
	if result.State != oc.CleanupCompleted {
		return fault(f.ResourceBusy)
	}
	if len(result.Remaining.References) != 0 || len(result.Remaining.ActiveLeases) != 0 {
		return internal(nil)
	}
	locks, err := authorityLocks(id.Actor{}, row.project, id.Converge)
	if err != nil {
		return err
	}
	commit := st.store.WithinTx(ctx, transaction, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, err := loadCleanup(ctx, x, key)
		if err != nil {
			return err
		}
		if current == nil {
			return fault(f.ResourceBusy)
		}
		if current.phase == "completed" {
			copy := *current
			copy.phase = row.phase
			if copy == *row {
				return nil
			}
		}
		if *current != *row {
			return fault(f.ResourceBusy)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.object_cleanup SET phase='completed' WHERE id=$1 AND phase='object'`, key.String())
		if err != nil {
			return unavailable(err)
		}
		if tag.RowsAffected() != 1 {
			return internal(nil)
		}
		return nil
	})
	return txError(commit)
}
