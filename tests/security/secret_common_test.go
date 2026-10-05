//go:build integration

package security_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func masterKeys(t *testing.T, current int64, versions ...int64) secret.Keyring {
	t.Helper()
	type key struct {
		Version string `json:"version"`
		Key     string `json:"key_b64"`
	}
	wire := struct {
		Format  int    `json:"format"`
		Current string `json:"current_version"`
		Keys    []key  `json:"keys"`
	}{Format: 1, Current: fmt.Sprint(current)}
	for _, v := range versions {
		material := make([]byte, 32)
		for i := range material {
			material[i] = byte(v*32 + int64(i))
		}
		wire.Keys = append(wire.Keys, key{fmt.Sprint(v), base64.StdEncoding.EncodeToString(material)})
	}
	raw, _ := json.Marshal(wire)
	result, err := secret.LoadKeyring(string(raw), auditKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type secretFixture struct {
	*auditFixture
	secret *secret.Service
	usage  *secretAuthority
}

func newSecretFixture(t *testing.T) *secretFixture {
	t.Helper()
	f := newAuditFixture(t)
	_, err := f.store.Exec(auditContext(t), `CREATE TABLE audit_fixture.secret_bindings(owner_id uuid PRIMARY KEY,credential_id uuid NOT NULL,consumer text NOT NULL,owner_kind text NOT NULL,active boolean NOT NULL DEFAULT true,retained boolean NOT NULL DEFAULT false,mcp_valid boolean NOT NULL DEFAULT true);`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.Exec(auditContext(t), `CREATE TABLE audit_fixture.secret_calls(id uuid PRIMARY KEY, owner_id uuid NOT NULL, credential_id uuid NOT NULL, snapshot_id uuid NOT NULL, consumer text NOT NULL, input_digest text NOT NULL, process_id uuid NOT NULL, fence bigint NOT NULL, current_invocation uuid NOT NULL, active boolean NOT NULL DEFAULT true);
CREATE TABLE audit_fixture.secret_invocations(id uuid PRIMARY KEY, call_id uuid NOT NULL, snapshot_id uuid NOT NULL, input_digest text NOT NULL, process_id uuid NOT NULL, fence bigint NOT NULL, ordinal bigint NOT NULL, dispatch text NOT NULL, active boolean NOT NULL DEFAULT true);`)
	if err != nil {
		t.Fatal(err)
	}
	auth := &secretAuthority{auditAuthority: f.auth}
	s, err := secret.New(f.store, masterKeys(t, 1, 1), f.service, secret.Authorizations{Sessions: f.auth, System: f.auth, Projects: auth, Usage: auth})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Initialize(auditContext(t)); err != nil {
		t.Fatal(err)
	}
	return &secretFixture{f, s, auth}
}

type secretAuthority struct {
	*auditAuthority
	planOnce      sync.Once
	planIssuer    sc.PlanIssuer
	transformPlan func(sc.UsageRequest, sc.UsageDependencies) (sc.UsageDependencies, error)
}

func (a *secretAuthority) CheckMutationInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef) error {
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	var state string
	err = e.QueryRow(ctx, `SELECT state FROM audit_fixture.projects WHERE id=$1`, ref.Details().Scope.Details().ProjectID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && state != "active" {
		return deny(foundation.ProjectNotActive)
	}
	return err
}
func (a *secretAuthority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, cause sc.LifecycleCause, id identity.ProjectID) error {
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	c := cause.Details()
	var allowed bool
	err = e.QueryRow(ctx, `SELECT state='deleting' AND stopped AND operation_id=$2 AND version=$3 FROM audit_fixture.projects WHERE id=$1`, id.String(), c.OperationID.String(), int64(c.ProjectVersion)).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
		return deny(foundation.InvalidState)
	}
	return err
}
func (a *secretAuthority) CheckReferenceInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, consumer sc.Purpose, owner string, retain bool) error {
	if actor.Details().Kind == identity.Human {
		if err := a.RequireCurrentSession(ctx, tx, actor); err != nil {
			return err
		}
		if ref.Details().Scope.Details().Kind == identity.ProjectScope {
			project, _ := foundation.ParseID[identity.Project](ref.Details().Scope.Details().ProjectID)
			if _, err := a.AuthorizeProject(ctx, tx, actor, project, identity.Mutate); err != nil {
				return err
			}
			if retain {
				if err := a.CheckMutationInTx(ctx, tx, actor, ref); err != nil {
					return err
				}
			}
		} else {
			if _, err := a.AuthorizeSystem(ctx, tx, actor, identity.Mutate); err != nil {
				return err
			}
		}
	} else if retain || actor.Details().Kind != identity.Service || actor.Details().ServiceName != identity.SecretService || actor.Details().CauseRef != owner || actor.Details().ProjectID != ref.Details().Scope.Details().ProjectID {
		return deny(foundation.Forbidden)
	}
	e, err := a.executor(tx)
	if err != nil {
		return err
	}
	var allowed bool
	err = e.QueryRow(ctx, `SELECT credential_id=$2 AND consumer=$3 AND (active OR NOT $4) FROM audit_fixture.secret_bindings WHERE owner_id=$1`, owner, ref.Details().ID.String(), string(consumer), retain).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !allowed {
		return deny(foundation.Forbidden)
	}
	return err
}
func (a *secretAuthority) AuthorizeLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	e, err := a.executor(tx)
	if err != nil {
		return sc.UseGrant{}, err
	}
	if actor.Details().Kind != identity.Service || actor.Details().ServiceName != identity.SecretService || actor.Details().CauseRef != owner.Details().ID || actor.Details().ProjectID != ref.Details().Scope.Details().ProjectID {
		return sc.UseGrant{}, deny(foundation.Forbidden)
	}
	var id, purpose, kind string
	var active, retained, mcpValid bool
	err = e.QueryRow(ctx, `SELECT credential_id::text,consumer,owner_kind,active,retained,mcp_valid FROM audit_fixture.secret_bindings WHERE owner_id=$1`, owner.Details().ID).Scan(&id, &purpose, &kind, &active, &retained, &mcpValid)
	if errors.Is(err, pgx.ErrNoRows) {
		return sc.UseGrant{}, deny(foundation.Forbidden)
	}
	if err != nil {
		return sc.UseGrant{}, err
	}
	if id != ref.Details().ID.String() || kind != string(owner.Details().Kind) {
		return sc.UseGrant{}, deny(foundation.Forbidden)
	}
	if action != sc.ReleaseLease {
		if ref.Details().Scope.Details().Kind == identity.ProjectScope {
			if err = a.CheckMutationInTx(ctx, tx, actor, ref); err != nil {
				return sc.UseGrant{}, err
			}
		}
		if action == sc.AcquireLease && !active || action == sc.ReadLease && !(active || retained) || purpose == string(sc.MCP) && !mcpValid {
			return sc.UseGrant{}, deny(foundation.Forbidden)
		}
	}
	return sc.UseGrant{Subject: actor, Consumer: sc.Purpose(purpose)}, nil
}
func (f *secretFixture) request(t *testing.T, kind sc.MutationKind, value []byte) sc.WriteRequest {
	t.Helper()
	key := foundation.IdempotencyKey(newID[struct{}](t).String())
	command, err := foundation.NewCommandIdentity("secret", []string{f.project.String(), f.actor.Details().UserID}, string(kind), key)
	if err != nil {
		t.Fatal(err)
	}
	r := sc.WriteRequest{Actor: f.actor, Scope: f.scope, Identity: command, Kind: kind, Purpose: sc.Model}
	if kind != sc.Delete {
		r.Value, err = sc.NewSecretMaterial(value)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(r.Value.Destroy)
	}
	return r
}
func (f *secretFixture) create(t *testing.T, value []byte) sc.MutationResult {
	t.Helper()
	r, err := f.secret.ExecuteWrite(auditContext(t), f.request(t, sc.Create, value))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f *secretFixture) bind(t *testing.T, ref sc.CredentialRef, purpose sc.Purpose) (sc.CredentialLeaseOwner, identity.Actor) {
	t.Helper()
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, newID[struct{}](t).String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.Exec(auditContext(t), `INSERT INTO audit_fixture.secret_bindings(owner_id,credential_id,consumer,owner_kind) VALUES($1,$2,$3,$4)`, owner.Details().ID, ref.Details().ID.String(), string(purpose), string(owner.Details().Kind)); err != nil {
		t.Fatal(err)
	}
	registration, _ := identity.RegisterService(identity.SecretService)
	actor, err := registration.Actor(owner.Details().ID, ref.Details().Scope)
	if err != nil {
		t.Fatal(err)
	}
	return owner, actor
}
func (f *secretFixture) acquire(t *testing.T, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, actor identity.Actor) sc.CredentialLease {
	t.Helper()
	var purpose string
	if err := f.store.QueryRow(auditContext(t), `SELECT consumer FROM audit_fixture.secret_bindings WHERE owner_id=$1`, owner.Details().ID).Scan(&purpose); err != nil {
		t.Fatal(err)
	}
	if purpose == string(sc.Model) && (owner.Details().Kind == sc.ModelCallOwner || owner.Details().Kind == sc.ExecutionOwner) {
		id := newID[sc.Lease](t)
		var old string
		err := f.store.QueryRow(auditContext(t), `SELECT id::text FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND owner_kind=$2 AND owner_id=$3`, ref.Details().ID.String(), string(owner.Details().Kind), owner.Details().ID).Scan(&old)
		if err == nil {
			id, err = foundation.ParseID[sc.Lease](old)
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.Model, LeaseOwner: owner, LeaseID: id, Action: sc.AcquireLeaseUsage}
		result, commit := f.applyModelUsage(t, f.secret, r)
		if commit.State() != foundation.Committed {
			t.Fatal(commit.Fault())
		}
		lease, ok := result.Lease()
		if !ok {
			t.Fatal("planned acquire omitted lease")
		}
		return lease
	}
	var lease sc.CredentialLease
	r := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		var err error
		lease, err = f.secret.AcquireCredentialLeaseInTx(ctx, tx, actor, ref, owner)
		return err
	})
	if r.State() != foundation.Committed {
		t.Fatal(r.Fault())
	}
	return lease
}
func readMaterial(t *testing.T, f *secretFixture, s *secret.Service, actor identity.Actor, id sc.LeaseID) []byte {
	t.Helper()
	request := f.modelReadRequest(t, actor, id)
	material, err := s.ReadCredentialForUsage(auditContext(t), request)
	if err != nil {
		t.Fatal(err)
	}
	defer material.Destroy()
	var value []byte
	if err = material.Use(func(v []byte) error { value = append([]byte(nil), v...); return nil }); err != nil {
		t.Fatal(err)
	}
	return value
}

// These plans describe test-schema facts, never production Model authority.
func (a *secretAuthority) modelPlanFacts(ctx context.Context, tx foundation.Tx, r sc.UsageRequest) (foundation.Digest, []foundation.LockRequest, error) {
	if r.Validate() != nil || r.Purpose != sc.Model || (r.Action != sc.AcquireLeaseUsage && r.Action != sc.ReleaseLeaseUsage && r.Action != sc.ReadLeaseUsage) {
		return "", nil, deny(foundation.InvalidArgument)
	}
	x, err := a.executor(tx)
	if err != nil {
		return "", nil, err
	}
	var credential, purpose, kind string
	var active, retained, mcpValid bool
	err = x.QueryRow(ctx, `SELECT credential_id::text,consumer,owner_kind,active,retained,mcp_valid FROM audit_fixture.secret_bindings WHERE owner_id=$1`, r.LeaseOwner.Details().ID).Scan(&credential, &purpose, &kind, &active, &retained, &mcpValid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, deny(foundation.Forbidden)
	}
	if err != nil {
		return "", nil, err
	}
	if credential != r.Ref.Details().ID.String() || purpose != string(r.Purpose) || kind != string(r.LeaseOwner.Details().Kind) {
		return "", nil, deny(foundation.Forbidden)
	}
	values := []any{credential, purpose, kind, active, retained, mcpValid}
	gate, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-secret-binding:"+r.LeaseOwner.Details().ID)
	locks := []foundation.LockRequest{{Key: gate, Mode: foundation.Shared}}
	if r.Action == sc.ReadLeaseUsage {
		var call, snapshot, input, process, dispatch, cOwner, cRef, cSnapshot, cInput, cProcess, current, consumer string
		var fence, ordinal, cFence int64
		var live, cLive bool
		err = x.QueryRow(ctx, `SELECT i.call_id::text,i.snapshot_id::text,i.input_digest,i.process_id::text,i.fence,i.ordinal,i.dispatch,i.active,c.owner_id::text,c.credential_id::text,c.snapshot_id::text,c.input_digest,c.process_id::text,c.fence,c.current_invocation::text,c.consumer,c.active FROM audit_fixture.secret_invocations i JOIN audit_fixture.secret_calls c ON c.id=i.call_id WHERE i.id=$1`, r.RequestID).Scan(&call, &snapshot, &input, &process, &fence, &ordinal, &dispatch, &live, &cOwner, &cRef, &cSnapshot, &cInput, &cProcess, &cFence, &current, &consumer, &cLive)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, deny(foundation.Forbidden)
		}
		if err != nil {
			return "", nil, err
		}
		if cOwner != r.LeaseOwner.Details().ID || cRef != credential || snapshot != cSnapshot || input != cInput || process != cProcess || fence != cFence || fence < 1 || ordinal < 1 || current != r.RequestID || consumer != "model-test" || !live || !cLive || dispatch != "reserved" && dispatch != "authorized" || kind == string(sc.ModelCallOwner) && call != cOwner {
			return "", nil, deny(foundation.Forbidden)
		}
		values = append(values, call, snapshot, input, process, fence, ordinal, dispatch, live, cOwner, cRef, cSnapshot, cInput, cProcess, cFence, current, consumer, cLive)
		callGate, _ := foundation.AggregateLock(foundation.OperationAggregate, call)
		invocationGate, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "fixture-model-invocation:"+r.RequestID)
		locks = append(locks, foundation.LockRequest{Key: callGate, Mode: foundation.Shared}, foundation.LockRequest{Key: invocationGate, Mode: foundation.Shared})
	}
	if r.Ref.Details().Scope.Details().Kind == identity.ProjectScope {
		key, _ := foundation.ProjectLock(r.Ref.Details().Scope.Details().ProjectID)
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	if r.LeaseOwner.Details().Kind == sc.ExecutionOwner {
		key, _ := foundation.AggregateLock(foundation.ExecutionAggregate, r.LeaseOwner.Details().ID)
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	b, _ := json.Marshal(values)
	h := sha256.Sum256(b)
	return foundation.Digest("sha256:" + hex.EncodeToString(h[:])), locks, nil
}
func (a *secretAuthority) DiscoverUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	mapping, locks, err := a.modelPlanFacts(ctx, foundation.Tx{}, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	a.planOnce.Do(func() { a.planIssuer = sc.NewPlanIssuer() })
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	plan, err := sc.NewUsageDependencies(a.planIssuer, binding, mapping, locks)
	if err == nil && a.transformPlan != nil {
		return a.transformPlan(r, plan)
	}
	return plan, err
}
func (a *secretAuthority) ValidateUsageInTx(ctx context.Context, tx foundation.Tx, r sc.UsageRequest, d sc.UsageDependencies) error {
	mapping, locks, err := a.modelPlanFacts(ctx, tx, r)
	if err != nil {
		return err
	}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return err
	}
	if !d.Matches(a.planIssuer, binding, mapping) {
		return deny(foundation.ResourceBusy)
	}
	return a.store.RequireHeldLocks(ctx, tx, locks)
}
func (f *secretFixture) modelReadRequest(t *testing.T, actor identity.Actor, id sc.LeaseID) sc.UsageRequest {
	t.Helper()
	var raw, scope, project, kind, owner string
	if err := f.store.QueryRow(auditContext(t), `SELECT credential_id::text,scope,coalesce(project_id::text,''),owner_kind,owner_id::text FROM agenteam_secret.secret_leases WHERE id=$1`, id.String()).Scan(&raw, &scope, &project, &kind, &owner); err != nil {
		t.Fatal(err)
	}
	refID, err := foundation.ParseID[sc.Credential](raw)
	if err != nil {
		t.Fatal(err)
	}
	refScope := identity.SystemScope()
	if scope == "project" {
		pid, e := foundation.ParseID[identity.Project](project)
		if e != nil {
			t.Fatal(e)
		}
		refScope, err = identity.InProject(pid)
		if err != nil {
			t.Fatal(err)
		}
	}
	ref, err := sc.NewCredentialRef(refID, refScope)
	if err != nil {
		t.Fatal(err)
	}
	leaseOwner, err := sc.NewCredentialLeaseOwner(sc.LeaseOwnerKind(kind), owner)
	if err != nil {
		t.Fatal(err)
	}
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.Model, LeaseOwner: leaseOwner, LeaseID: id, Action: sc.ReadLeaseUsage, RequestID: newID[struct{}](t).String()}
	call := newID[struct{}](t).String()
	if leaseOwner.Details().Kind == sc.ModelCallOwner {
		call = owner
	}
	snapshot, process := newID[struct{}](t).String(), newID[struct{}](t).String()
	input := "sha256:" + strings.Repeat("a", 64)
	_, err = f.store.Exec(auditContext(t), `INSERT INTO audit_fixture.secret_calls(id,owner_id,credential_id,snapshot_id,consumer,input_digest,process_id,fence,current_invocation) VALUES($1,$2,$3,$4,'model-test',$5,$6,1,$7) ON CONFLICT(id) DO UPDATE SET snapshot_id=excluded.snapshot_id,input_digest=excluded.input_digest,process_id=excluded.process_id,current_invocation=excluded.current_invocation`, call, owner, raw, snapshot, input, process, r.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.store.Exec(auditContext(t), `INSERT INTO audit_fixture.secret_invocations(id,call_id,snapshot_id,input_digest,process_id,fence,ordinal,dispatch) VALUES($1,$2,$3,$4,$5,1,1,'reserved')`, r.RequestID, call, snapshot, input, process)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f *secretFixture) applyModelUsage(t *testing.T, s *secret.Service, r sc.UsageRequest) (sc.UsageResult, foundation.CommitResult) {
	t.Helper()
	plan, err := s.DiscoverUsage(auditContext(t), r)
	if err != nil {
		var fault *foundation.Fault
		if !errors.As(err, &fault) {
			t.Fatal(err)
		}
		return sc.UsageResult{}, foundation.NotCommittedResult(fault)
	}
	var result sc.UsageResult
	commit := f.store.WithinTx(auditContext(t), txCause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		var err error
		result, err = s.ApplyUsageInTx(ctx, tx, r, plan)
		return err
	})
	return result, commit
}
func (f *secretFixture) modelLeaseRequest(actor identity.Actor, lease sc.CredentialLease, owner sc.CredentialLeaseOwner, action sc.UsageAction) sc.UsageRequest {
	return sc.UsageRequest{Actor: actor, Ref: lease.CredentialRef, Purpose: sc.Model, LeaseOwner: owner, LeaseID: lease.LeaseID, Action: action}
}
