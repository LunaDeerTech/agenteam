//go:build integration

package objects_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type transferFixture struct {
	*fixture
	service   *object.Service
	transfers *object.TransferService
	authority *runnerAuthority
	runner    oc.RunnerID
	operation oc.OperationID
	execution identity.ExecutionID
}
type runnerAuthority struct{ base *authority }

func newTransferFixture(t *testing.T) *transferFixture {
	t.Helper()
	f := newFixture(t, false)
	f.sql(t, `CREATE TABLE object_fixture.runners(id uuid PRIMARY KEY,generation bigint NOT NULL,active boolean NOT NULL);
 CREATE TABLE object_fixture.transfer_operations(id uuid PRIMARY KEY,runner_id uuid NOT NULL,execution_id uuid NOT NULL,owner_id uuid NOT NULL,version bigint NOT NULL,active boolean NOT NULL);
 CREATE TABLE object_fixture.transfer_outputs(operation_id uuid PRIMARY KEY,media_type text NOT NULL,byte_size bigint NOT NULL,sha256 text NOT NULL,upload_key text NOT NULL);
 CREATE TABLE object_fixture.transfer_inputs(operation_id uuid NOT NULL,object_id uuid NOT NULL,PRIMARY KEY(operation_id,object_id));
 CREATE TABLE object_fixture.transfer_evidence(id uuid PRIMARY KEY,transfer_id uuid NOT NULL,kind text NOT NULL,joined boolean NOT NULL,closed boolean NOT NULL,byte_size bigint NOT NULL,sha256 text NOT NULL,runner_id uuid NOT NULL,operation_id uuid NOT NULL,runner_generation bigint NOT NULL,operation_version bigint NOT NULL,direction text NOT NULL,object_id uuid NOT NULL);`)
	runner, operation := id[oc.Runner](t), id[oc.Operation](t)
	_, execution := executionFacts(t, f)
	f.sql(t, `INSERT INTO object_fixture.runners VALUES($1,1,true)`, runner.String())
	f.sql(t, `INSERT INTO object_fixture.transfer_operations VALUES($1,$2,$3,$4,1,true)`, operation.String(), runner.String(), execution.String(), f.owner.Details().ID)
	return transferOn(t, &transferFixture{fixture: f, runner: runner, operation: operation, execution: execution}, f.store, f.config)
}
func transferOn(t *testing.T, original *transferFixture, store *postgres.Store, config object.StorageConfig, configure ...func(*object.Authorizations, *audit.Authorizations)) *transferFixture {
	t.Helper()
	copyFixture := *original.fixture
	copyFixture.store = store
	copyFixture.config = config
	copyFixture.authority = &authority{store}
	f := &copyFixture
	auth := &runnerAuthority{f.authority}
	backend, err := object.NewBackend(f.config)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), id[oc.Process](t))
	if err != nil {
		t.Fatal(err)
	}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	objectAuth := object.Authorizations{Planner: object.NewTransferAccessPlanner(f.authority, auth), Resources: f.authority, Read: f.authority, Gate: transferProjectGate{f.authority}, Cleanup: f.authority, Leases: auth}
	auditAuth := audit.Authorizations{Projects: transferAuditAuthority{auditAuthority{f.authority}}}
	for _, change := range configure {
		change(&objectAuth, &auditAuth)
	}
	auditing, err := audit.New(f.store, keys, auditAuth)
	if err != nil {
		t.Fatal(err)
	}
	service, err := object.New(f.store, backend, spool, auditing, objectAuth)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := object.LoadTransferEndpoint(func(string) (string, bool) { return "", false }, f.config)
	if err != nil {
		t.Fatal(err)
	}
	transfers, err := object.NewTransferService(service, auth, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil {
			_ = service.Force(ctx)
			t.Error(err)
		}
	})
	if err = service.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	return &transferFixture{f, service, transfers, auth, original.runner, original.operation, original.execution}
}

type transferProjectGate struct{ *authority }

func (a transferProjectGate) CheckInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, owner oc.ObjectOwner, intent identity.AccessIntent) error {
	if intent == identity.Converge {
		e, err := a.exec(tx)
		if err != nil {
			return err
		}
		var archived bool
		if err = e.QueryRow(ctx, `SELECT state='archived' FROM object_fixture.projects WHERE id=$1`, owner.Details().ProjectID).Scan(&archived); err != nil {
			return err
		}
		if archived {
			return nil
		}
	}
	return a.authority.CheckInTx(ctx, tx, actor, owner, intent)
}

type transferAuditAuthority struct{ auditAuthority }

func (a transferAuditAuthority) CheckAppendInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) error {
	if entry.Fields().Action == ac.ObjectTransferComplete || entry.Fields().Action == ac.ObjectTransferRevoke {
		e, err := a.exec(tx)
		if err != nil {
			return err
		}
		var archived bool
		if err = e.QueryRow(ctx, `SELECT state='archived' FROM object_fixture.projects WHERE id=$1`, entry.Fields().Scope.Details().ProjectID).Scan(&archived); err != nil {
			return err
		}
		if archived {
			return nil
		}
	}
	return a.auditAuthority.CheckAppendInTx(ctx, tx, entry, key)
}
func (a *runnerAuthority) dependencies(ctx context.Context, e postgres.SQLExecutor, r oc.TransferAccessRequest) (oc.AccessDependencies, error) {
	d := r.Details()
	spec := d.Spec.Details()
	actor := d.Actor
	if actor.Details().Kind == identity.Service {
		// Maintenance mapping comes from the same owned durable operation; no
		// business permission is inferred from the worker's Service actor.
		var user, session string
		err := e.QueryRow(ctx, `SELECT p.owner_id::text,s.id::text FROM object_fixture.projects p JOIN object_fixture.sessions s ON s.user_id=p.owner_id WHERE p.id=$1 ORDER BY s.id LIMIT 1`, spec.Owner.Details().ProjectID).Scan(&user, &session)
		if err != nil {
			return oc.AccessDependencies{}, err
		}
		u, _ := foundation.ParseID[identity.User](user)
		ss, _ := foundation.ParseID[identity.Session](session)
		actor, _ = identity.NewHuman(u, ss)
	}
	ownerRequest, err := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.PrepareAccess, Actor: actor, Owner: spec.Owner, Intent: identity.Mutate})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	base, err := a.base.dependencies(ctx, e, ownerRequest)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	var execution, owner, runner string
	err = e.QueryRow(ctx, `SELECT runner_id::text,execution_id::text,owner_id::text FROM object_fixture.transfer_operations WHERE id=$1`, spec.OperationID.String()).Scan(&runner, &execution, &owner)
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	if owner != spec.Owner.Details().ID || runner != spec.RunnerID.String() {
		return oc.AccessDependencies{}, fault(foundation.Forbidden)
	}
	locks := base.Locks()
	key, _ := foundation.AggregateLock(foundation.ExecutionAggregate, execution)
	locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	key, _ = foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-operation:"+spec.OperationID.String())
	locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	raw, err := json.Marshal([]string{base.Mapping().String(), runner, execution, owner})
	if err != nil {
		return oc.AccessDependencies{}, err
	}
	return oc.NewAccessDependencies(digest(raw), locks)
}
func (a *runnerAuthority) Discover(ctx context.Context, r oc.TransferAccessRequest) (oc.AccessDependencies, error) {
	return a.dependencies(ctx, a.base.store, r)
}
func (a *runnerAuthority) ValidateInTx(ctx context.Context, tx foundation.Tx, r oc.TransferAccessRequest, expected oc.AccessDependencies) (oc.TransferAuthorization, error) {
	e, err := a.base.exec(tx)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	actual, err := a.dependencies(ctx, e, r)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	if !actual.Equal(expected) {
		return oc.TransferAuthorization{}, fault(foundation.ResourceBusy)
	}
	d := r.Details()
	spec := d.Spec.Details()
	if d.Actor.Details().Kind == identity.Service {
		if (d.Operation != oc.TransferCleanup && d.Operation != oc.TransferConfirmTerminal) || d.Actor.Details().ServiceName != identity.ObjectMaintenance || d.Actor.Details().CauseRef != d.ID.String() || (d.Operation == oc.TransferCleanup && d.CleanupCause.String() != d.ID.String()) {
			return oc.TransferAuthorization{}, fault(foundation.Forbidden)
		}
	} else {
		if err = a.base.current(ctx, e, d.Actor); err != nil {
			return oc.TransferAuthorization{}, err
		}
	}
	var active bool
	var generation, version int64
	var execution string
	err = e.QueryRow(ctx, `SELECT r.active AND o.active AND x.active,r.generation,o.version,o.execution_id::text FROM object_fixture.runners r JOIN object_fixture.transfer_operations o ON o.runner_id=r.id JOIN object_fixture.executions x ON x.id=o.execution_id WHERE r.id=$1 AND o.id=$2 AND o.owner_id=$3`, spec.RunnerID.String(), spec.OperationID.String(), spec.Owner.Details().ID).Scan(&active, &generation, &version, &execution)
	if err != nil {
		return oc.TransferAuthorization{}, err
	}
	if !active && d.Operation != oc.TransferCancel && d.Operation != oc.TransferConfirmTerminal && d.Operation != oc.TransferCleanup {
		return oc.TransferAuthorization{}, fault(foundation.Forbidden)
	}
	if spec.Direction == oc.TransferPUT {
		var media, sha, key string
		var length int64
		if err = e.QueryRow(ctx, `SELECT media_type,byte_size,sha256,upload_key FROM object_fixture.transfer_outputs WHERE operation_id=$1`, spec.OperationID.String()).Scan(&media, &length, &sha, &key); err != nil {
			return oc.TransferAuthorization{}, fault(foundation.Forbidden)
		}
		manifest := spec.Manifest.Details()
		if media != manifest.MediaType || sha != manifest.SHA256.String() || length != manifest.Length || key != string(spec.UploadCommand.IdempotencyKey) {
			return oc.TransferAuthorization{}, fault(foundation.Forbidden)
		}
	} else {
		var allowed bool
		if err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM object_fixture.transfer_inputs WHERE operation_id=$1 AND object_id=$2)`, spec.OperationID.String(), d.ObjectID.String()).Scan(&allowed); err != nil {
			return oc.TransferAuthorization{}, err
		}
		if !allowed {
			return oc.TransferAuthorization{}, fault(foundation.Forbidden)
		}
	}
	ex, _ := foundation.ParseID[identity.Execution](execution)
	result := oc.TransferAuthorizationDetails{Request: r, RunnerGeneration: foundation.Version(generation), OperationVersion: foundation.Version(version), ExecutionID: ex}
	if d.Evidence != nil {
		var kind, sha string
		var joined, closed bool
		var size int64
		err = e.QueryRow(ctx, `SELECT kind,joined,closed,byte_size,sha256 FROM object_fixture.transfer_evidence WHERE id=$1 AND transfer_id=$2 AND runner_id=$3 AND operation_id=$4 AND runner_generation=$5 AND operation_version=$6 AND direction=$7 AND object_id=$8`, d.Evidence.ID.String(), d.ID.String(), spec.RunnerID.String(), spec.OperationID.String(), generation, version, string(spec.Direction), d.ObjectID.String()).Scan(&kind, &joined, &closed, &size, &sha)
		if errors.Is(err, pgx.ErrNoRows) {
			return oc.TransferAuthorization{}, fault(foundation.Forbidden)
		}
		if err != nil {
			return oc.TransferAuthorization{}, err
		}
		if kind != string(d.Evidence.Kind) {
			return oc.TransferAuthorization{}, fault(foundation.Forbidden)
		}
		evidenceDigest := digest([]byte(d.Evidence.ID.String() + ":" + kind))
		if kind == string(oc.TransferCompletedEvidence) {
			result.Completed = &oc.TransferCompletion{Evidence: *d.Evidence, Digest: evidenceDigest, Length: size, SHA256: foundation.Digest(sha)}
		}
		if joined && closed {
			result.Retirement = &oc.TransferRetirement{Evidence: *d.Evidence, Digest: evidenceDigest, AllRequestsJoined: true, StorageAdmissionClosed: true}
		}
	}
	return oc.NewTransferAuthorization(result)
}
func (a *runnerAuthority) AuthorizeLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, obj oc.ObjectID, owner oc.LeaseOwner, action oc.LeaseAction) error {
	if owner.Details().Kind != oc.TransferOwner {
		return a.base.AuthorizeLeaseInTx(ctx, tx, actor, obj, owner, action)
	}
	e, err := a.base.exec(tx)
	if err != nil {
		return err
	}
	if actor.Details().Kind == identity.Service {
		if action != oc.ReleaseLease || actor.Details().ServiceName != identity.ObjectMaintenance || actor.Details().CauseRef != owner.Details().ID {
			return fault(foundation.Forbidden)
		}
	} else if err = a.base.current(ctx, e, actor); err != nil {
		return err
	}
	var project string
	if err = e.QueryRow(ctx, `SELECT project_id::text FROM agenteam_object.objects WHERE id=$1`, obj.String()).Scan(&project); err != nil {
		return err
	}
	if action == oc.ReleaseLease {
		var terminal bool
		err = e.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM object_fixture.transfer_evidence WHERE transfer_id=$1 AND joined AND closed)`, owner.Details().ID).Scan(&terminal)
		if err != nil {
			return err
		}
		if !terminal {
			return fault(foundation.Forbidden)
		}
	}
	return nil
}
func (f *transferFixture) spec(t *testing.T, body []byte) oc.TransferSpec {
	t.Helper()
	m, err := oc.NewTransferManifest(oc.TransferManifestDetails{MediaType: "text/plain", Length: int64(len(body)), SHA256: digest(body)})
	if err != nil {
		t.Fatal(err)
	}
	upload := command(t, "stable-output")
	f.sql(t, `INSERT INTO object_fixture.transfer_outputs VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, f.operation.String(), m.Details().MediaType, m.Details().Length, m.Details().SHA256.String(), string(upload.IdempotencyKey))
	spec, err := oc.NewTransferSpec(oc.TransferSpecDetails{RunnerID: f.runner, OperationID: f.operation, Direction: oc.TransferPUT, Owner: f.owner, Manifest: m, UploadCommand: &upload, ExpiresInSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	return spec
}
func (f *transferFixture) evidence(t *testing.T, grant oc.TransferGrant, kind oc.TransferEvidenceKind, terminal bool) oc.TransferEvidenceRef {
	t.Helper()
	ev := oc.TransferEvidenceRef{ID: id[oc.TransferEvidence](t), Kind: kind}
	f.sql(t, `INSERT INTO object_fixture.transfer_evidence SELECT $1,t.id,$3,$4,$4,$5,$6,t.runner_id,t.operation_id,t.runner_generation,t.operation_version,t.direction,t.object_id FROM agenteam_object.object_transfers t WHERE t.id=$2`, ev.ID.String(), grant.Status.ID.String(), string(kind), terminal, int64(grant.ByteSize), grant.SHA256.String())
	return ev
}
