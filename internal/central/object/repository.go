package object

import (
	"context"
	"errors"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type objectRow struct {
	meta           oc.ObjectMeta
	partition, key string
	cleaning       bool
}
type uploadRow struct {
	id                                  oc.UploadID
	object                              oc.ObjectID
	receipt                             oc.ReceiptID
	attempt                             oc.AttemptID
	owner                               oc.ObjectOwner
	command                             foundation.IdempotencyKey
	digest                              []byte
	actor, state, disposition, creation string
	existence                           oc.OwnerExistence
	expected                            *int64
	initiator                           identity.ActorKind
	initiatorID, executionID            string
}
type attemptRow struct {
	id                     oc.AttemptID
	upload                 oc.UploadID
	object                 oc.ObjectID
	process                oc.ProcessID
	key, phase             string
	ordinal, size          int64
	digest                 foundation.Digest
	late, closed, cleaning bool
	kind                   string
	transfer               oc.TransferID
}

func executor(s *Service, tx foundation.Tx) (postgres.SQLExecutor, error) {
	e, err := s.state().store.InTx(tx)
	if err != nil {
		return nil, unavailable(err)
	}
	return e, nil
}
func loadObject(ctx context.Context, e postgres.SQLExecutor, id oc.ObjectID) (objectRow, bool, error) {
	var r objectRow
	var scope, project, storedID string
	var digest []byte
	var size, version int64
	var created time.Time
	err := e.QueryRow(ctx, `SELECT id::text,scope,partition_id::text,coalesce(project_id::text,''),media_type,byte_size,sha256,state,version,coalesce(candidate_key,''),cleaning,created_at FROM agenteam_object.objects WHERE id=$1`, id.String()).Scan(&storedID, &scope, &r.partition, &project, &r.meta.MediaType, &size, &digest, &r.meta.State, &version, &r.key, &r.cleaning, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, unavailable(err)
	}
	r.meta.ID, err = foundation.ParseID[oc.StoredObject](storedID)
	if err != nil {
		return r, false, unavailable(err)
	}
	r.meta.Scope = identity.SystemScope()
	if scope == "project" {
		p, err := foundation.ParseID[identity.Project](project)
		if err != nil {
			return r, false, unavailable(err)
		}
		r.meta.Scope, err = identity.InProject(p)
		if err != nil {
			return r, false, unavailable(err)
		}
	}
	r.meta.ByteSize = foundation.Progress(size)
	r.meta.Version = foundation.Version(version)
	r.meta.SHA256 = newDigest(digest)
	r.meta.CreatedAt, err = foundation.NewInstant(created)
	if err != nil || r.meta.Validate() != nil {
		return r, false, unavailable(err)
	}
	return r, true, nil
}

const uploadColumns = `id::text,object_id::text,receipt_id::text,coalesce(current_attempt_id::text,''),owner_kind,owner_id::text,coalesce(project_id::text,''),command_key,semantic_digest,stable_actor,state,disposition,coalesce(creation_cause,''),existence,initiator_kind,initiator_id::text,coalesce(initiator_execution_id::text,''),expected_version`

func scanUpload(row postgres.Row) (uploadRow, bool, error) {
	var r uploadRow
	var id, obj, receipt, attempt, owner, project string
	var kind oc.OwnerKind
	err := row.Scan(&id, &obj, &receipt, &attempt, &kind, &owner, &project, &r.command, &r.digest, &r.actor, &r.state, &r.disposition, &r.creation, &r.existence, &r.initiator, &r.initiatorID, &r.executionID, &r.expected)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, unavailable(err)
	}
	r.id, err = foundation.ParseID[oc.Upload](id)
	if err != nil {
		return r, false, unavailable(err)
	}
	r.object, err = foundation.ParseID[oc.StoredObject](obj)
	if err != nil {
		return r, false, unavailable(err)
	}
	r.receipt, err = foundation.ParseID[oc.Receipt](receipt)
	if err != nil {
		return r, false, unavailable(err)
	}
	if attempt != "" {
		r.attempt, err = foundation.ParseID[oc.Attempt](attempt)
		if err != nil {
			return r, false, unavailable(err)
		}
	}
	r.owner, err = oc.NewObjectOwner(kind, owner, project)
	if err != nil {
		return r, false, unavailable(err)
	}
	return r, true, nil
}
func loadCommand(ctx context.Context, e postgres.SQLExecutor, c foundation.CommandIdentity) (uploadRow, bool, error) {
	return scanUpload(e.QueryRow(ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE command_hash=$1`, commandHash(c)))
}
func loadUpload(ctx context.Context, e postgres.SQLExecutor, id oc.UploadID) (uploadRow, bool, error) {
	return scanUpload(e.QueryRow(ctx, `SELECT `+uploadColumns+` FROM agenteam_object.uploads WHERE id=$1`, id.String()))
}
func loadAttempt(ctx context.Context, e postgres.SQLExecutor, id oc.AttemptID) (attemptRow, bool, error) {
	var r attemptRow
	var aid, upload, obj, process, transfer string
	var digest []byte
	err := e.QueryRow(ctx, `SELECT id::text,upload_id::text,object_id::text,coalesce(process_id::text,''),candidate_key,phase,ordinal,byte_size,sha256,maybe_late,io_closed,cleanup_gate,kind,coalesce(transfer_id::text,'') FROM agenteam_object.upload_attempts WHERE id=$1`, id.String()).Scan(&aid, &upload, &obj, &process, &r.key, &r.phase, &r.ordinal, &r.size, &digest, &r.late, &r.closed, &r.cleaning, &r.kind, &transfer)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, unavailable(err)
	}
	r.id, err = foundation.ParseID[oc.Attempt](aid)
	if err != nil {
		return r, false, unavailable(err)
	}
	r.upload, err = foundation.ParseID[oc.Upload](upload)
	if err != nil {
		return r, false, unavailable(err)
	}
	r.object, err = foundation.ParseID[oc.StoredObject](obj)
	if err != nil {
		return r, false, unavailable(err)
	}
	if process != "" {
		r.process, err = foundation.ParseID[oc.Process](process)
		if err != nil {
			return r, false, unavailable(err)
		}
	}
	if transfer != "" {
		r.transfer, err = foundation.ParseID[oc.Transfer](transfer)
		if err != nil {
			return r, false, unavailable(err)
		}
	}
	r.digest = newDigest(digest)
	if r.digest.Validate() != nil {
		return r, false, unavailable(nil)
	}
	return r, true, nil
}
func receiptOf(u uploadRow) oc.UploadReceipt {
	if u.existence != oc.ProspectiveOwner || u.disposition != "reserved" {
		return oc.UploadReceipt{}
	}
	r, _ := oc.NewUploadReceipt(oc.ReceiptDetails{ID: u.receipt, UploadID: u.id, ObjectID: u.object, Owner: u.owner, CreationCause: u.creation})
	return r
}
func attemptOf(a attemptRow) oc.UploadAttempt {
	r, _ := oc.NewUploadAttempt(oc.AttemptDetails{ID: a.id, UploadID: a.upload, ObjectID: a.object})
	return r
}
func objectPartition(r objectRow, owner oc.ObjectOwner) bool {
	return r.partition == owner.Partition() && r.meta.Scope.Equal(owner.Scope())
}
func checkOriginal(u uploadRow, actor identity.Actor, owner oc.ObjectOwner) error {
	if !u.owner.Equal(owner) || u.actor != stableActor(actor) {
		return failure(foundation.Forbidden, nil)
	}
	return nil
}
func deleted(committed bool) error {
	state := foundation.NotStarted
	if committed {
		state = foundation.Committed
	}
	return foundation.NewFault(foundation.ResourceDeleted, state)
}
