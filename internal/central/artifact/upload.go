package artifact

import (
	"context"
	"io"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type uploadIntent struct {
	ref                                  ac.ArtifactRef
	identity                             foundation.CommandIdentity
	digest                               foundation.Digest
	originalActor, cause, state          string
	expected                             *foundation.Version
	display                              ac.Display
	media                                string
	size                                 int64
	expectedSHA                          *foundation.Digest
	sha                                  foundation.Digest
	object                               oc.ObjectID
	upload                               oc.UploadID
	attempt                              oc.AttemptID
	receipt                              oc.ReceiptID
	meta                                 oc.ObjectMeta
	execution, operation, tool, toolCall string
}

const intentColumns = `id::text,file_id::text,project_id::text,command_key,request_digest,stable_actor,creation_cause,state,expected_version,name,description,media_type,byte_size,expected_sha256,sha256,COALESCE(target_object_id::text,''),COALESCE(target_upload_id::text,''),COALESCE(target_attempt_id::text,''),COALESCE(receipt_id::text,''),COALESCE(object_version,0),object_created_at,COALESCE(execution_id::text,''),COALESCE(operation_id::text,''),COALESCE(tool_id::text,''),COALESCE(tool_call_id::text,'')`

func scanIntent(row postgres.Row) (uploadIntent, bool, error) {
	var u uploadIntent
	var id, file, project, key, object, upload, attempt, receipt string
	var digest, expectedSHA, sha []byte
	var expected *int64
	var version int64
	var created *time.Time
	err := row.Scan(&id, &file, &project, &key, &digest, &u.originalActor, &u.cause, &u.state, &expected, &u.display.Name, &u.display.Description, &u.media, &u.size, &expectedSHA, &sha, &object, &upload, &attempt, &receipt, &version, &created, &u.execution, &u.operation, &u.tool, &u.toolCall)
	if noRows(err) {
		return u, false, nil
	}
	if err != nil {
		return u, false, unavailable(err)
	}
	u.ref.ProjectID, err = foundation.ParseID[identity.Project](project)
	if err != nil {
		return u, false, unavailable(err)
	}
	u.ref.ArtifactID, err = foundation.ParseID[ac.ArtifactEntity](id)
	if err != nil {
		return u, false, unavailable(err)
	}
	u.ref.FileID, err = foundation.ParseID[ac.File](file)
	if err != nil {
		return u, false, unavailable(err)
	}
	u.identity, err = commandIdentity(u.ref.ProjectID, foundation.IdempotencyKey(key), "prepare_upload")
	if err != nil {
		return u, false, unavailable(err)
	}
	u.digest = rawDigest(digest)
	if u.digest.Validate() != nil {
		return u, false, unavailable(nil)
	}
	if expected != nil {
		v := foundation.Version(*expected)
		u.expected = &v
	}
	if len(expectedSHA) > 0 {
		d := rawDigest(expectedSHA)
		if d.Validate() != nil {
			return u, false, unavailable(nil)
		}
		u.expectedSHA = &d
	}
	if len(sha) > 0 {
		u.sha = rawDigest(sha)
		if u.sha.Validate() != nil {
			return u, false, unavailable(nil)
		}
	}
	if object != "" {
		u.object, err = foundation.ParseID[oc.StoredObject](object)
		if err != nil {
			return u, false, unavailable(err)
		}
	}
	if upload != "" {
		u.upload, err = foundation.ParseID[oc.Upload](upload)
		if err != nil {
			return u, false, unavailable(err)
		}
	}
	if attempt != "" {
		u.attempt, err = foundation.ParseID[oc.Attempt](attempt)
		if err != nil {
			return u, false, unavailable(err)
		}
	}
	if receipt != "" {
		u.receipt, err = foundation.ParseID[oc.Receipt](receipt)
		if err != nil {
			return u, false, unavailable(err)
		}
	}
	if created != nil {
		instant, err := foundation.NewInstant(*created)
		if err != nil {
			return u, false, unavailable(err)
		}
		u.meta = oc.ObjectMeta{ID: u.object, Scope: artifactOwner(u.ref).Scope(), MediaType: u.media, ByteSize: foundation.Progress(u.size), SHA256: u.sha, State: oc.Available, Version: foundation.Version(version), CreatedAt: instant}
		if u.meta.Validate() != nil {
			return u, false, unavailable(nil)
		}
	}
	return u, true, nil
}
func loadIntent(ctx context.Context, e postgres.SQLExecutor, identity foundation.CommandIdentity) (uploadIntent, bool, error) {
	return scanIntent(e.QueryRow(ctx, `SELECT `+intentColumns+` FROM agenteam_artifact.upload_intents WHERE command_hash=$1`, digestRaw(digestBytes([]byte(identity.Canonical())))))
}
func loadIntentID(ctx context.Context, e postgres.SQLExecutor, id string) (uploadIntent, bool, error) {
	return scanIntent(e.QueryRow(ctx, `SELECT `+intentColumns+` FROM agenteam_artifact.upload_intents WHERE id=$1`, id))
}
func (u uploadIntent) receiptValue() (oc.UploadReceipt, error) {
	return oc.NewUploadReceipt(oc.ReceiptDetails{ID: u.receipt, UploadID: u.upload, ObjectID: u.object, Owner: artifactOwner(u.ref), CreationCause: u.cause})
}

// BeginUpload persists a real prospective Artifact identity after current
// Human/Project authorization. It is an internal business composition port;
// production has no unauthenticated upload/Artifact route.
func (s *Service) BeginUpload(ctx context.Context, v ac.Invocation, command foundation.CommandMeta, display ac.Display, media string, length int64, expectedSHA *foundation.Digest) (ac.UploadTarget, error) {
	command = copyCommand(command)
	if v.Validate() != nil || v.Details().Actor.Details().Kind != identity.Human || command.Validate() != nil || display.Validate() != nil || length < 0 || length > oc.MaxObjectSize {
		return ac.UploadTarget{}, invalid()
	}
	media, err := oc.NormalizeMediaType(media)
	if err != nil {
		return ac.UploadTarget{}, err
	}
	var expected *foundation.Digest
	if expectedSHA != nil {
		d := *expectedSHA
		if d.Validate() != nil {
			return ac.UploadTarget{}, invalid()
		}
		expected = &d
	}
	var version *foundation.Version
	if command.ExpectedVersion != nil {
		d := *command.ExpectedVersion
		version = &d
	}
	d := v.Details()
	id, err := commandIdentity(d.ProjectID, command.IdempotencyKey, "prepare_upload")
	if err != nil {
		return ac.UploadTarget{}, invalid()
	}
	semantic, err := jsonDigest(struct {
		Actor, Project, Execution, Operation, Tool, ToolCall, Name, Description, Media string
		Size                                                                           int64
		SHA                                                                            *foundation.Digest
		Version                                                                        *foundation.Version
	}{stableActor(d.Actor), d.ProjectID.String(), d.ExecutionID, d.OperationID, d.ToolID, d.ToolCallID, display.Name, display.Description, media, length, expected, version})
	if err != nil {
		return ac.UploadTarget{}, err
	}
	before, found, err := loadIntent(ctx, s.state().store, id)
	if err != nil {
		return ac.UploadTarget{}, err
	}
	ref := before.ref
	if !found {
		ref, err = newRef(d.ProjectID)
		if err != nil {
			return ac.UploadTarget{}, err
		}
	}
	causeID, err := foundation.NewID[struct{}]()
	if err != nil {
		return ac.UploadTarget{}, unavailable(err)
	}
	cause, _ := foundation.NewCommandsCause(id)
	result := s.within(ctx, invocationSubject(v), identity.Mutate, cause, nil, createLocks(id, ref.ArtifactID.String()), func(ctx context.Context, tx foundation.Tx, _ oc.LockedAccess) error {
		e, err := s.state().store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, exists, err := loadIntent(ctx, e, id)
		if err != nil {
			return err
		}
		if exists {
			if !found || current.ref != ref {
				return failure(foundation.ResourceBusy, nil)
			}
			if current.originalActor != stableActor(d.Actor) {
				return failure(foundation.Forbidden, nil)
			}
			if current.digest != semantic {
				return failure(foundation.IdempotencyKeyReused, nil)
			}
			if current.state == "deleted" || current.state == "consumed" {
				return failure(foundation.ResourceDeleted, nil)
			}
			return nil
		}
		if found {
			return failure(foundation.ResourceBusy, nil)
		}
		if version != nil && *version != 1 {
			return failure(foundation.VersionConflict, nil)
		}
		var expectedBytes any
		if expected != nil {
			expectedBytes = digestRaw(*expected)
		}
		_, err = e.Exec(ctx, `INSERT INTO agenteam_artifact.upload_intents(id,file_id,project_id,command_hash,command_key,request_digest,stable_actor,creation_cause,state,expected_version,name,description,media_type,byte_size,expected_sha256,execution_id,operation_id,tool_id,tool_call_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'pending',$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, ref.ArtifactID.String(), ref.FileID.String(), ref.ProjectID.String(), digestRaw(digestBytes([]byte(id.Canonical()))), string(command.IdempotencyKey), digestRaw(semantic), stableActor(d.Actor), causeID.String(), version, display.Name, display.Description, media, length, expectedBytes, null(d.ExecutionID), null(d.OperationID), null(d.ToolID), null(d.ToolCallID))
		return portOrNil(err)
	})
	if err = commitError(result); err != nil {
		return ac.UploadTarget{}, err
	}
	command.ExpectedVersion = version
	return ac.UploadTarget{Reference: ref, Owner: artifactOwner(ref), Command: command}, nil
}
func matchesIntent(u uploadIntent, v ac.Invocation, target ac.UploadTarget) error {
	d := v.Details()
	if u.originalActor != stableActor(d.Actor) || u.ref.ProjectID != d.ProjectID || u.ref != target.Reference || !target.Owner.Equal(artifactOwner(u.ref)) {
		return failure(foundation.Forbidden, nil)
	}
	if u.identity.Key() != target.Command.IdempotencyKey || u.execution != d.ExecutionID || u.operation != d.OperationID || u.tool != d.ToolID || u.toolCall != d.ToolCallID {
		return failure(foundation.IdempotencyKeyReused, nil)
	}
	if (u.expected == nil) != (target.Command.ExpectedVersion == nil) || u.expected != nil && *u.expected != *target.Command.ExpectedVersion {
		return failure(foundation.IdempotencyKeyReused, nil)
	}
	if u.state == "deleted" || u.state == "consumed" || u.state == "cancel_requested" || u.state == "cancelled" {
		return failure(foundation.ResourceDeleted, nil)
	}
	return nil
}
func (s *Service) UploadPayload(ctx context.Context, v ac.Invocation, target ac.UploadTarget, body io.ReadCloser) (oc.PutResult, error) {
	target.Command = copyCommand(target.Command)
	if nilPort(body) {
		return oc.PutResult{}, invalid()
	}
	defer body.Close()
	if v.Validate() != nil || v.Details().Actor.Details().Kind != identity.Human || target.Reference.Validate() != nil || target.Owner.Validate() != nil || target.Command.Validate() != nil {
		return oc.PutResult{}, invalid()
	}
	r := s.state()
	u, ok, err := loadIntentID(ctx, r.store, target.Reference.ArtifactID.String())
	if err != nil {
		return oc.PutResult{}, err
	}
	if !ok {
		return oc.PutResult{}, failure(foundation.Forbidden, nil)
	}
	// This authorization cannot be replaced by possession of the target IDs.
	if _, err = r.owners.AuthorizeOwner(ctx, v.Details().Actor, target.Owner, identity.Mutate); err != nil {
		return oc.PutResult{}, err
	}
	if err = matchesIntent(u, v, target); err != nil {
		return oc.PutResult{}, err
	}

	p, err := r.uploads.PreparePayload(ctx, v.Details().Actor, target.Owner, u.media, u.size, u.expectedSHA, body)
	if err != nil {
		return oc.PutResult{}, err
	}
	defer r.uploads.DiscardPrepared(p)
	if u.sha.Validate() == nil && u.sha != p.Details().SHA256 {
		return oc.PutResult{}, failure(foundation.IdempotencyKeyReused, nil)
	}
	if u.state == "uploaded" {
		lookup, err := r.objects.LookupPut(ctx, v.Details().Actor, target.Owner, target.Command.IdempotencyKey)
		if err != nil {
			return oc.PutResult{}, err
		}
		if lookup.State != oc.UploadCommitted || lookup.Meta == nil || lookup.Receipt.Validate() != nil || lookup.Receipt.Details().ID != u.receipt {
			return oc.PutResult{}, failure(foundation.ResourceBusy, nil)
		}
		return oc.PutResult{Meta: *lookup.Meta, Receipt: lookup.Receipt}, nil
	}
	reserve, err := s.ownerPlan(ctx, v.Details().Actor, target.Owner, oc.ReserveAccess, oc.AccessRequestDetails{Command: &target.Command, Prepared: p})
	if err != nil {
		return oc.PutResult{}, err
	}
	cause, _ := foundation.NewCommandsCause(u.identity)
	var attempt oc.UploadAttempt
	result := s.within(ctx, invocationSubject(v), identity.Mutate, cause, []oc.AccessLockPlan{reserve}, createLocks(u.identity, u.ref.ArtifactID.String()), func(ctx context.Context, tx foundation.Tx, locked oc.LockedAccess) error {
		e, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, found, err := loadIntentID(ctx, e, u.ref.ArtifactID.String())
		if err != nil {
			return err
		}
		if !found {
			return failure(foundation.Forbidden, nil)
		}
		if err = matchesIntent(current, v, target); err != nil {
			return err
		}
		if current.sha.Validate() == nil && current.sha != p.Details().SHA256 {
			return failure(foundation.IdempotencyKeyReused, nil)
		}
		attempt, err = r.uploads.ReserveUploadInTx(ctx, tx, v.Details().Actor, target.Owner, target.Command, p, reserve, locked)
		if err != nil {
			return err
		}
		d := attempt.Details()
		_, err = e.Exec(ctx, `UPDATE agenteam_artifact.upload_intents SET target_object_id=$2,target_upload_id=$3,target_attempt_id=$4,sha256=$5 WHERE id=$1`, u.ref.ArtifactID.String(), d.ObjectID.String(), d.UploadID.String(), d.ID.String(), digestRaw(p.Details().SHA256))
		return portOrNil(err)
	})
	if err = commitError(result); err != nil {
		return oc.PutResult{}, err
	}
	verified, err := r.uploads.UploadPrepared(ctx, v.Details().Actor, target.Owner, p, attempt)
	if err != nil {
		return oc.PutResult{}, err
	}
	publish, err := s.ownerPlan(ctx, v.Details().Actor, target.Owner, oc.PublishAccess, oc.AccessRequestDetails{Attempt: verified})
	if err != nil {
		return oc.PutResult{}, err
	}
	var out oc.PutResult
	result = s.within(ctx, invocationSubject(v), identity.Mutate, cause, []oc.AccessLockPlan{publish}, createLocks(u.identity, u.ref.ArtifactID.String()), func(ctx context.Context, tx foundation.Tx, locked oc.LockedAccess) error {
		e, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, found, err := loadIntentID(ctx, e, u.ref.ArtifactID.String())
		if err != nil {
			return err
		}
		if !found {
			return failure(foundation.Forbidden, nil)
		}
		if err = matchesIntent(current, v, target); err != nil {
			return err
		}
		if current.attempt != verified.Details().ID {
			return failure(foundation.ResourceBusy, nil)
		}
		out, err = r.uploads.PublishVerifiedInTx(ctx, tx, v.Details().Actor, target.Owner, verified, publish, locked)
		if err != nil {
			return err
		}
		if out.Receipt.Validate() != nil {
			return unavailable(nil)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_artifact.upload_intents SET state='uploaded',receipt_id=$2,object_version=$3,object_created_at=$4 WHERE id=$1`, u.ref.ArtifactID.String(), out.Receipt.Details().ID.String(), int64(out.Meta.Version), out.Meta.CreatedAt.Time())
		return portOrNil(err)
	})
	if err = commitError(result); err != nil {
		return oc.PutResult{}, err
	}
	return out, nil
}

func (s *Service) CreateFromUpload(ctx context.Context, v ac.Invocation, command foundation.CommandMeta, display ac.Display, receipt oc.UploadReceipt) (ac.Metadata, error) {
	req, err := makeRequest(v, command, display, "upload", "", 0, "", oc.BusinessFileRef{}, receipt)
	if err != nil {
		return ac.Metadata{}, err
	}
	command = req.command
	_, completed, err := s.lookup(ctx, req)
	if err != nil {
		return ac.Metadata{}, err
	}
	if completed != nil {
		return *completed, nil
	}
	d := receipt.Details()
	if d.Owner.Details().Kind != oc.Artifact || d.Owner.Details().ProjectID != v.Details().ProjectID.String() {
		return ac.Metadata{}, failure(foundation.Forbidden, nil)
	}
	r := s.state()
	intent, found, err := loadIntentID(ctx, r.store, d.Owner.Details().ID)
	if err != nil {
		return ac.Metadata{}, err
	}
	if !found {
		return ac.Metadata{}, failure(foundation.Forbidden, nil)
	}
	if intent.originalActor != stableActor(v.Details().Actor) || intent.display != display || intent.cause != d.CreationCause || intent.object != d.ObjectID || intent.upload != d.UploadID || intent.receipt != d.ID || intent.state != "uploaded" {
		return ac.Metadata{}, failure(foundation.Forbidden, nil)
	}
	if (intent.expected == nil) != (command.ExpectedVersion == nil) || intent.expected != nil && *intent.expected != *command.ExpectedVersion {
		return ac.Metadata{}, failure(foundation.IdempotencyKeyReused, nil)
	}
	c, err := newCommand(req, intent.ref, oc.ResolvedSource{})
	if err != nil {
		return ac.Metadata{}, err
	}
	c.cause = intent.cause
	c.object = intent.object
	c.upload = intent.upload
	c.attempt = intent.attempt
	c.media = intent.media
	c.size = intent.size
	c.sha = intent.sha
	consume, err := s.ownerPlan(ctx, v.Details().Actor, d.Owner, oc.ConsumeAccess, oc.AccessRequestDetails{Receipt: receipt})
	if err != nil {
		return ac.Metadata{}, err
	}
	cause, _ := foundation.NewCommandsCause(req.identity, intent.identity)
	extra := append(createLocks(req.identity, c.ref.ArtifactID.String()), createLocks(intent.identity, "")...)
	var out ac.Metadata
	result := s.within(ctx, invocationSubject(v), identity.Mutate, cause, []oc.AccessLockPlan{consume}, extra, func(ctx context.Context, tx foundation.Tx, locked oc.LockedAccess) error {
		e, err := r.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		_, exists, err := loadCommand(ctx, e, req.identity)
		if err != nil {
			return err
		}
		if exists {
			return failure(foundation.ResourceBusy, nil)
		}
		u, ok, err := loadIntentID(ctx, e, intent.ref.ArtifactID.String())
		if err != nil {
			return err
		}
		if !ok || u.state != "uploaded" || u.digest != intent.digest || u.object != intent.object || u.receipt != intent.receipt {
			return failure(foundation.ResourceBusy, nil)
		}
		if _, err = r.owners.AuthorizeOwnerInTx(ctx, tx, v.Details().Actor, d.Owner, identity.Mutate); err != nil {
			return err
		}
		if err = insertCommand(ctx, e, c); err != nil {
			return err
		}
		if err = insertArtifact(ctx, e, c); err != nil {
			return err
		}
		if _, err = r.objects.ConsumeUploadInTx(ctx, tx, v.Details().Actor, d.Owner, receipt, consume, locked); err != nil {
			return err
		}
		if err = updateArtifactObject(ctx, e, c.ref, u.meta); err != nil {
			return err
		}
		row, ok, err := loadArtifact(ctx, e, c.ref.ArtifactID.String())
		if err != nil {
			return err
		}
		if !ok {
			return unavailable(nil)
		}
		if err = s.appendCreate(ctx, tx, req, c, row.meta); err != nil {
			return err
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_artifact.upload_intents SET state='consumed' WHERE id=$1`, u.ref.ArtifactID.String())
		if err != nil {
			return unavailable(err)
		}
		_, err = e.Exec(ctx, `UPDATE agenteam_artifact.commands SET state='completed',completed_at=clock_timestamp() WHERE command_hash=$1`, digestRaw(digestBytes([]byte(req.identity.Canonical()))))
		if err != nil {
			return unavailable(err)
		}
		out = row.meta
		return nil
	})
	if err = commitError(result); err != nil {
		return ac.Metadata{}, err
	}
	return out, nil
}
