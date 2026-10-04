package object

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type transferRow struct {
	retirementKind, pendingKind        oc.TransferEvidenceKind
	pendingRetirement                  oc.EvidenceID
	id                                 oc.TransferID
	actor                              identity.Actor
	stable                             string
	issue                              foundation.CommandMeta
	spec                               oc.TransferSpec
	object                             oc.ObjectID
	upload                             oc.UploadID
	staging, candidate                 oc.AttemptID
	lease, sourceLease                 oc.LeaseID
	sourceProcess                      oc.ProcessID
	manifest                           oc.TransferManifest
	key, phase                         string
	digest                             []byte
	expires                            time.Time
	version                            foundation.Version
	runnerGeneration, operationVersion foundation.Version
	execution                          identity.ExecutionID
	revoked                            bool
	completed, retirement              oc.EvidenceID
	completedDigest, retirementDigest  []byte
	cleanup                            bool
	leaseActive                        bool
	stagingCleaned                     bool
}

const transferColumns = `t.id::text,t.actor_json,t.stable_actor,t.issue_key,t.issue_request_id::text,t.issue_expected,t.semantic_digest,t.project_id::text,t.owner_kind,t.owner_id::text,t.runner_id::text,t.operation_id::text,t.runner_generation,t.operation_version,t.execution_id::text,t.direction,t.object_id::text,coalesce(t.upload_id::text,''),coalesce(t.upload_request_id::text,''),coalesce(t.upload_key,''),t.upload_expected,coalesce(t.staging_id::text,''),coalesce(t.candidate_id::text,''),t.lease_id::text,coalesce(t.source_lease_id::text,''),coalesce(t.source_process_id::text,''),t.media_type,t.byte_size,t.sha256,coalesce(t.candidate_key,''),t.duration_seconds,t.expires_at,t.phase,t.version,t.revoked_at IS NOT NULL,coalesce(t.completed_evidence::text,''),t.completed_digest,coalesce(t.retirement_evidence::text,''),t.retirement_digest,coalesce(t.retirement_kind,''),coalesce(t.retirement_pending::text,''),coalesce(t.retirement_pending_kind,''),t.cleanup_gate,EXISTS(SELECT 1 FROM agenteam_object.object_leases l WHERE l.id=t.lease_id AND l.state='active'),EXISTS(SELECT 1 FROM agenteam_object.upload_attempts a WHERE a.id=t.staging_id AND a.phase='cleaned')`

func loadTransfer(ctx context.Context, e postgres.SQLExecutor, id oc.TransferID) (transferRow, bool, error) {
	return scanTransfer(e.QueryRow(ctx, `SELECT `+transferColumns+` FROM agenteam_object.object_transfers t WHERE t.id=$1`, id.String()))
}
func loadTransferCommand(ctx context.Context, e postgres.SQLExecutor, c foundation.CommandIdentity) (transferRow, bool, error) {
	return scanTransfer(e.QueryRow(ctx, `SELECT `+transferColumns+` FROM agenteam_object.object_transfers t WHERE issue_hash=$1`, commandHash(c)))
}
func scanTransfer(row postgres.Row) (transferRow, bool, error) {
	var r transferRow
	var pendingRetirement string
	var id, project, ownerID, runner, operation, execution, object, upload, uploadRequest, staging, candidate, lease, sourceLease, sourceProcess, issueRequest, completed, retirement, media string
	var ownerKind oc.OwnerKind
	var direction oc.TransferDirection
	var duration, size, version, rg, ov int64
	var expected, uploadExpected *int64
	var rawActor, digest []byte
	var uploadKey foundation.IdempotencyKey
	err := row.Scan(&id, &rawActor, &r.stable, &r.issue.IdempotencyKey, &issueRequest, &expected, &r.digest, &project, &ownerKind, &ownerID, &runner, &operation, &rg, &ov, &execution, &direction, &object, &upload, &uploadRequest, &uploadKey, &uploadExpected, &staging, &candidate, &lease, &sourceLease, &sourceProcess, &media, &size, &digest, &r.key, &duration, &r.expires, &r.phase, &version, &r.revoked, &completed, &r.completedDigest, &retirement, &r.retirementDigest, &r.retirementKind, &pendingRetirement, &r.pendingKind, &r.cleanup, &r.leaseActive, &r.stagingCleaned)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, false, nil
	}
	if err != nil {
		return r, false, unavailable(err)
	}
	var ad identity.ActorDetails
	if json.Unmarshal(rawActor, &ad) != nil {
		return r, false, unavailable(nil)
	}
	r.actor, err = transferActor(ad)
	if err != nil {
		return r, false, err
	}
	if r.stable != stableActor(r.actor) {
		return r, false, unavailable(nil)
	}
	r.id, err = foundation.ParseID[oc.Transfer](id)
	if err != nil {
		return r, false, unavailable(err)
	}
	r.object, err = foundation.ParseID[oc.StoredObject](object)
	if err != nil {
		return r, false, unavailable(err)
	}
	r.issue.RequestID, err = foundation.ParseID[foundation.Request](issueRequest)
	if err != nil {
		return r, false, unavailable(err)
	}
	if expected != nil {
		v := foundation.Version(*expected)
		r.issue.ExpectedVersion = &v
	}
	r.manifest, err = oc.NewTransferManifest(oc.TransferManifestDetails{MediaType: media, Length: size, SHA256: newDigest(digest)})
	if err != nil {
		return r, false, unavailable(err)
	}
	d := oc.TransferSpecDetails{Direction: direction, ExpiresInSeconds: duration}
	d.Owner, err = oc.NewObjectOwner(ownerKind, ownerID, project)
	if err != nil {
		return r, false, unavailable(err)
	}
	d.RunnerID, err = foundation.ParseID[oc.Runner](runner)
	if err != nil {
		return r, false, unavailable(err)
	}
	d.OperationID, err = foundation.ParseID[oc.Operation](operation)
	if err != nil {
		return r, false, unavailable(err)
	}
	if direction == oc.TransferPUT {
		d.Manifest = r.manifest
		d.UploadCommand = &foundation.CommandMeta{IdempotencyKey: uploadKey}
		d.UploadCommand.RequestID, err = foundation.ParseID[foundation.Request](uploadRequest)
		if err != nil {
			return r, false, unavailable(err)
		}
		if uploadExpected != nil {
			v := foundation.Version(*uploadExpected)
			d.UploadCommand.ExpectedVersion = &v
		}
		r.upload, err = foundation.ParseID[oc.Upload](upload)
		if err != nil {
			return r, false, unavailable(err)
		}
		r.staging, err = foundation.ParseID[oc.Attempt](staging)
		if err != nil {
			return r, false, unavailable(err)
		}
	} else {
		d.ObjectID = r.object
	}
	r.spec, err = oc.NewTransferSpec(d)
	if err != nil {
		return r, false, unavailable(err)
	}
	if candidate != "" {
		r.candidate, err = foundation.ParseID[oc.Attempt](candidate)
		if err != nil {
			return r, false, unavailable(err)
		}
	}
	r.lease, err = foundation.ParseID[oc.Lease](lease)
	if err != nil {
		return r, false, unavailable(err)
	}
	if sourceLease != "" {
		r.sourceLease, err = foundation.ParseID[oc.Lease](sourceLease)
		if err != nil {
			return r, false, unavailable(err)
		}
		r.sourceProcess, err = foundation.ParseID[oc.Process](sourceProcess)
		if err != nil {
			return r, false, unavailable(err)
		}
	}
	if completed != "" {
		r.completed, err = foundation.ParseID[oc.TransferEvidence](completed)
		if err != nil {
			return r, false, unavailable(err)
		}
	}
	if retirement != "" {
		r.retirement, err = foundation.ParseID[oc.TransferEvidence](retirement)
		if err != nil {
			return r, false, unavailable(err)
		}
	}
	if pendingRetirement != "" {
		r.pendingRetirement, err = foundation.ParseID[oc.TransferEvidence](pendingRetirement)
		if err != nil {
			return r, false, unavailable(err)
		}
	}
	r.execution, err = foundation.ParseID[identity.Execution](execution)
	if err != nil {
		return r, false, unavailable(err)
	}
	r.runnerGeneration = foundation.Version(rg)
	r.operationVersion = foundation.Version(ov)
	r.version = foundation.Version(version)
	return r, true, nil
}
func transferActor(d identity.ActorDetails) (identity.Actor, error) {
	var out identity.Actor
	var err error
	switch d.Kind {
	case identity.Human:
		u, e := foundation.ParseID[identity.User](d.UserID)
		if e != nil {
			return out, unavailable(e)
		}
		s, e := foundation.ParseID[identity.Session](d.SessionID)
		if e != nil {
			return out, unavailable(e)
		}
		out, err = identity.NewHuman(u, s)
	case identity.AgentRun:
		p, e := foundation.ParseID[identity.Project](d.ProjectID)
		if e != nil {
			return out, unavailable(e)
		}
		a, e := foundation.ParseID[identity.Agent](d.AgentID)
		if e != nil {
			return out, unavailable(e)
		}
		x, e := foundation.ParseID[identity.Execution](d.ExecutionID)
		if e != nil {
			return out, unavailable(e)
		}
		out, err = identity.NewAgentRun(p, a, x)
	default:
		return out, invalid()
	}
	if err != nil || out.Details() != d {
		return identity.Actor{}, unavailable(err)
	}
	return out, nil
}
func transferCommand(owner oc.ObjectOwner, key foundation.IdempotencyKey) (foundation.CommandIdentity, error) {
	return foundation.NewCommandIdentity("object-transfer", []string{owner.Details().ProjectID, owner.Details().ID}, "issue", key)
}
func transferSemantic(actor identity.Actor, command foundation.CommandMeta, spec oc.TransferSpec) ([]byte, error) {
	d := spec.Details()
	expected := ""
	if command.ExpectedVersion != nil {
		expected = command.ExpectedVersion.String()
	}
	uploadExpected := ""
	uploadKey := ""
	if d.UploadCommand != nil {
		uploadKey = string(d.UploadCommand.IdempotencyKey)
		if d.UploadCommand.ExpectedVersion != nil {
			uploadExpected = d.UploadCommand.ExpectedVersion.String()
		}
	}
	m := d.Manifest.Details()
	raw, err := json.Marshal([]any{"transfer.v1", stableActor(actor), expected, d.RunnerID.String(), d.OperationID.String(), d.Direction, d.Owner.Details(), d.ObjectID.String(), uploadKey, uploadExpected, m.MediaType, m.Length, m.SHA256.String(), d.ExpiresInSeconds})
	if err != nil {
		return nil, invalid()
	}
	h := sha256.Sum256(raw)
	return h[:], nil
}
func (r transferRow) request(actor identity.Actor, operation oc.TransferOperation, evidence *oc.TransferEvidenceRef, cancel *foundation.CommandMeta, p oc.PreparedPayload) (oc.AccessRequest, error) {
	d := oc.TransferAccessDetails{Operation: operation, Actor: actor, IssueCommand: r.issue, Spec: r.spec, ID: r.id, ObjectID: r.object, UploadID: r.upload, StagingID: r.staging, CandidateID: r.candidate, LeaseID: r.lease, SourceLeaseID: r.sourceLease, Manifest: r.manifest, Evidence: evidence, CancelCommand: cancel, Prepared: p, Version: r.version}
	if operation == oc.TransferIssue {
		d.Version = 0
	}
	if operation == oc.TransferCleanup {
		d.CleanupCause, _ = foundation.ParseID[oc.CleanupOperation](r.id.String())
	}
	request, err := oc.NewTransferAccessRequest(d)
	if err != nil {
		return oc.AccessRequest{}, err
	}
	return oc.NewTransferAccess(request)
}
func (r transferRow) status() oc.TransferStatusView {
	state := oc.TransferPending
	switch r.phase {
	case "complete":
		state = oc.TransferComplete
	case "failed":
		state = oc.TransferFailed
	case "unknown":
		state = oc.TransferUnknown
	}
	if r.revoked && state != oc.TransferComplete {
		state = oc.TransferFailed
	}
	expires, _ := foundation.NewInstant(r.expires)
	cleanup := oc.CleanupPending
	if !r.leaseActive && (r.stagingCleaned || r.spec.Details().Direction == oc.TransferGET) {
		cleanup = oc.CleanupCompleted
	}
	return oc.TransferStatusView{ID: r.id, RunnerID: r.spec.Details().RunnerID, OperationID: r.spec.Details().OperationID, ObjectID: r.object, Direction: r.spec.Details().Direction, State: state, Version: r.version, ExpiresAt: expires, Revoked: r.revoked, LeaseActive: r.leaseActive, Cleanup: cleanup}
}

func optionalTransferID[T any](id foundation.ID[T]) any {
	if id.Validate() != nil {
		return nil
	}
	return id.String()
}
