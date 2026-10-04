package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type Store interface {
	postgres.SQLExecutor
	InTx(foundation.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, foundation.TransactionCause, func(context.Context, foundation.Tx) error) foundation.CommitResult
	AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error
}

func digestBytes(b []byte) foundation.Digest {
	sum := sha256.Sum256(b)
	return foundation.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
func jsonDigest(v any) (foundation.Digest, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", unavailable(err)
	}
	return digestBytes(b), nil
}
func digestRaw(d foundation.Digest) []byte {
	if d.Validate() != nil {
		return nil
	}
	b, _ := hex.DecodeString(d.String()[7:])
	return b
}
func rawDigest(b []byte) foundation.Digest {
	return foundation.Digest("sha256:" + hex.EncodeToString(b))
}
func noRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
func txCause() (foundation.TransactionCause, error) {
	id, err := foundation.NewID[struct{}]()
	if err != nil {
		return foundation.TransactionCause{}, unavailable(err)
	}
	return foundation.NewRecoveryCause("artifact", id.String(), "")
}
func artifactOwner(ref ac.ArtifactRef) oc.ObjectOwner {
	o, _ := oc.NewObjectOwner(oc.Artifact, ref.ArtifactID.String(), ref.ProjectID.String())
	return o
}
func artifactLock(id string) foundation.LockRequest {
	k, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "artifact:"+id)
	return foundation.LockRequest{Key: k, Mode: foundation.Exclusive}
}
func commandIdentity(project identity.ProjectID, key foundation.IdempotencyKey, action string) (foundation.CommandIdentity, error) {
	return foundation.NewCommandIdentity("artifact", []string{project.String()}, action, key)
}
func stableActor(actor identity.Actor) string {
	d := actor.Details()
	switch d.Kind {
	case identity.Human:
		return "human:" + d.UserID
	case identity.AgentRun:
		return "agent:" + d.ProjectID + ":" + d.AgentID + ":" + d.ExecutionID
	}
	return ""
}
func subject(actor identity.Actor, project identity.ProjectID) ac.AccessSubject {
	s := ac.AccessSubject{Actor: actor, ProjectID: project}
	if actor.Details().Kind == identity.AgentRun {
		s.ExecutionID = actor.Details().ExecutionID
	}
	return s
}
func invocationSubject(v ac.Invocation) ac.AccessSubject {
	d := v.Details()
	return ac.AccessSubject{Actor: d.Actor, ProjectID: d.ProjectID, ExecutionID: d.ExecutionID, OperationID: d.OperationID, ToolID: d.ToolID, ToolCallID: d.ToolCallID}
}

type artifactRow struct {
	meta           ac.Metadata
	cause          string
	tool, toolCall string
}

const artifactColumns = `id::text,file_id::text,project_id::text,object_id::text,kind,name,description,media_type,byte_size,sha256,object_version,object_created_at,version,creator_kind,creator_id::text,creation_cause,COALESCE(execution_id::text,''),COALESCE(operation_id::text,''),COALESCE(tool_id::text,''),COALESCE(tool_call_id::text,''),created_at`

func scanArtifact(row postgres.Row) (artifactRow, bool, error) {
	var out artifactRow
	var id, file, project, object, kind, name, description, media, creatorKind, creatorID, execution, operation string
	var size, objectVersion, version int64
	var sha []byte
	var objectCreated, created time.Time
	err := row.Scan(&id, &file, &project, &object, &kind, &name, &description, &media, &size, &sha, &objectVersion, &objectCreated, &version, &creatorKind, &creatorID, &out.cause, &execution, &operation, &out.tool, &out.toolCall, &created)
	if noRows(err) {
		return out, false, nil
	}
	if err != nil {
		return out, false, unavailable(err)
	}
	pid, e := foundation.ParseID[identity.Project](project)
	if e != nil {
		return out, false, unavailable(e)
	}
	aid, e := foundation.ParseID[ac.ArtifactEntity](id)
	if e != nil {
		return out, false, unavailable(e)
	}
	fid, e := foundation.ParseID[ac.File](file)
	if e != nil {
		return out, false, unavailable(e)
	}
	oid, e := foundation.ParseID[oc.StoredObject](object)
	if e != nil {
		return out, false, unavailable(e)
	}
	scope, _ := identity.InProject(pid)
	oi, e := foundation.NewInstant(objectCreated)
	if e != nil {
		return out, false, unavailable(e)
	}
	ci, e := foundation.NewInstant(created)
	if e != nil {
		return out, false, unavailable(e)
	}
	d := ac.MetadataDetails{Reference: ac.ArtifactRef{ProjectID: pid, ArtifactID: aid, FileID: fid}, Kind: ac.Kind(kind), Name: name, Description: description, Object: oc.ObjectMeta{ID: oid, Scope: scope, MediaType: media, ByteSize: foundation.Progress(size), SHA256: rawDigest(sha), State: oc.Available, Version: foundation.Version(objectVersion), CreatedAt: oi}, ExecutionID: execution, OperationID: operation, CreatedAt: ci, Version: foundation.Version(version)}
	if creatorKind == string(identity.Human) {
		d.CreatedByUserID = creatorID
	} else if creatorKind == string(identity.AgentRun) {
		d.CreatedByAgentID = creatorID
	} else {
		return out, false, unavailable(nil)
	}
	out.meta, e = ac.NewMetadata(d)
	if e != nil {
		return out, false, unavailable(e)
	}
	return out, true, nil
}
func loadArtifact(ctx context.Context, e postgres.SQLExecutor, id string) (artifactRow, bool, error) {
	return scanArtifact(e.QueryRow(ctx, `SELECT `+artifactColumns+` FROM agenteam_artifact.artifacts WHERE id=$1`, id))
}
