//go:build integration

package project_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// The tap only observes or corrupts input to the real Audit Service. It never
// returns a successful receipt in place of Audit or mints a Secret witness.
type bindingAuditTap struct {
	next   *audit.Service
	before func(context.Context, foundation.Tx, ac.Entry, ac.AppendKey) (context.Context, ac.Entry, ac.AppendKey)
	after  func(ac.Entry, error)
	ctx    context.Context
	tx     foundation.Tx
	entry  ac.Entry
	key    ac.AppendKey
	err    error
	calls  int
}

func (a *bindingAuditTap) AppendInTx(ctx context.Context, tx foundation.Tx, entry ac.Entry, key ac.AppendKey) (ac.AppendReceipt, error) {
	if entry.Fields().Scope.Details().Kind == identity.ProjectScope && key.Details().Producer == ac.SecretProducer {
		a.ctx, a.tx, a.entry, a.key = ctx, tx, entry, key
		a.calls++
		if a.before != nil {
			ctx, entry, key = a.before(ctx, tx, entry, key)
		}
	}
	receipt, err := a.next.AppendInTx(ctx, tx, entry, key)
	a.err = err
	if a.after != nil {
		a.after(entry, err)
	}
	return receipt, err
}

type bindingObservedStore struct {
	*postgres.Store
	acquires, transactions int
}

func (s *bindingObservedStore) AcquireAll(ctx context.Context, tx foundation.Tx, locks []foundation.LockRequest) error {
	s.acquires++
	return s.Store.AcquireAll(ctx, tx, locks)
}
func (s *bindingObservedStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	s.transactions++
	return s.Store.WithinTx(ctx, cause, fn)
}

type bindingBundle struct {
	store    *postgres.Store
	accounts *account.Authority
	projects *project.Authority
	delegate *project.SecretAuthority
	checker  *secret.ProjectAuditAuthority
	audit    *audit.Service
	tap      *bindingAuditTap
	secret   *secret.Service
	usage    *bindingUsage
}

type bindingFixture struct {
	*r3Fixture
	project c.ProjectRef
	scope   identity.Scope
	bundle  *bindingBundle
}

func newBindingFixture(t *testing.T) *bindingFixture {
	t.Helper()
	f := &bindingFixture{r3Fixture: newR3Fixture(t)}
	f.project, _, _ = f.create(t, f.owner, "SecretBinding")
	f.scope, _ = identity.InProject(f.project.ID)
	// Only the unavailable consumption domain is isolated. Project, Account,
	// Secret and Audit use their real tables and formal authorities.
	f.sql(t, `CREATE TABLE project_fixture.secret_usage(owner_id uuid PRIMARY KEY,project_id uuid NOT NULL,credential_id uuid NOT NULL,owner_kind text NOT NULL,purpose text NOT NULL,active boolean NOT NULL DEFAULT true,subject_user uuid,subject_session uuid,CHECK((subject_user IS NULL)=(subject_session IS NULL)))`)
	f.sql(t, `CREATE TABLE project_fixture.secret_calls(id uuid PRIMARY KEY,owner_id uuid NOT NULL,credential_id uuid NOT NULL,snapshot_id uuid NOT NULL,input_digest text NOT NULL,process_id uuid NOT NULL,fence bigint NOT NULL,current_invocation uuid NOT NULL,active boolean NOT NULL DEFAULT true);
CREATE TABLE project_fixture.secret_invocations(id uuid PRIMARY KEY,call_id uuid NOT NULL,snapshot_id uuid NOT NULL,input_digest text NOT NULL,process_id uuid NOT NULL,fence bigint NOT NULL,ordinal bigint NOT NULL,dispatch text NOT NULL,active boolean NOT NULL DEFAULT true);`)
	f.bundle = f.bindStore(t, f.raw, f.raw)
	return f
}

func (f *bindingFixture) bindStore(t *testing.T, store, checkerStore *postgres.Store) *bindingBundle {
	t.Helper()
	b := &bindingBundle{store: store}
	ak, ck := keys(t)
	var err error
	b.accounts, err = account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	b.checker, err = secret.NewProjectAuditAuthority(checkerStore)
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := f.registry.Manifest()
	lifecycle, err := project.NewLifecycleAuthority(store, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	b.projects, err = project.NewAuthority(store, project.AuthorityDependencies{Sessions: b.accounts, Routes: b.accounts, Lifecycle: lifecycle, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.SecretProducer: b.checker}})
	if err != nil {
		t.Fatal(err)
	}
	b.delegate, err = project.NewSecretAuthority(b.projects)
	if err != nil {
		t.Fatal(err)
	}
	b.audit, err = audit.New(store, ck, audit.Authorizations{Sessions: b.accounts, System: b.accounts, Accounts: b.accounts, Projects: b.projects})
	if err != nil {
		t.Fatal(err)
	}
	b.tap = &bindingAuditTap{next: b.audit}
	b.usage = &bindingUsage{store: store}
	k, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))), ck)
	if err != nil {
		t.Fatal(err)
	}
	b.secret, err = secret.New(store, k, b.tap, secret.Authorizations{Sessions: b.accounts, System: b.accounts, Projects: b.delegate, Usage: b.usage})
	if err != nil {
		t.Fatal(err)
	}
	if err = b.secret.Initialize(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.secret.StopMaintenance)
	return b
}

func (f *bindingFixture) proxyStore(t *testing.T, address net.Addr) *postgres.Store {
	t.Helper()
	u, err := url.Parse(f.db.Fixture.URL(f.db.Name))
	if err != nil {
		t.Fatal(err)
	}
	u.Host = address.String()
	return openStore(t, f.db.Config(t, map[string]string{"URL": u.String(), "TLS_MODE": "disable"}))
}

func (f *bindingFixture) request(t *testing.T, actor identity.Actor, kind sc.MutationKind, value string) sc.WriteRequest {
	t.Helper()
	command, err := foundation.NewCommandIdentity("secret", []string{f.project.ID.String(), actor.Details().UserID}, string(kind), foundation.IdempotencyKey(id[struct{}](t).String()))
	if err != nil {
		t.Fatal(err)
	}
	r := sc.WriteRequest{Actor: actor, Scope: f.scope, Identity: command, Kind: kind, Purpose: sc.Model}
	if kind != sc.Delete {
		r.Value, err = sc.NewSecretMaterial([]byte(value))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(r.Value.Destroy)
	}
	return r
}

func (f *bindingFixture) credential(t *testing.T) sc.MutationResult {
	t.Helper()
	r, err := f.bundle.secret.ExecuteWrite(ctxFor(t), f.request(t, f.owner, sc.Create, "binding-secret"))
	if err != nil {
		t.Fatal("real Project Secret create", err)
	}
	return r
}

type bindingCounts struct{ canonical, receipts, audits int }

func (f *bindingFixture) counts(t *testing.T) bindingCounts {
	t.Helper()
	var n bindingCounts
	err := f.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_secret.secrets WHERE project_id=$1),(SELECT count(*) FROM agenteam_secret.secret_command_receipts WHERE project_id=$1),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND producer='secret')`, f.project.ID.String()).Scan(&n.canonical, &n.receipts, &n.audits)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *bindingFixture) unchanged(t *testing.T, want bindingCounts) {
	t.Helper()
	if got := f.counts(t); got != want {
		t.Fatalf("durable Secret counts %v, want %v", got, want)
	}
}

func bindingLocks(actor identity.Actor, project identity.ProjectID) []foundation.LockRequest {
	locks := []foundation.LockRequest{r3Lock(project, foundation.Shared)}
	if actor.Details().Kind == identity.Human {
		key, _ := foundation.UserLock(actor.Details().UserID)
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	return locks
}

func bindingDenied(t *testing.T, result foundation.CommitResult, code foundation.Code) {
	t.Helper()
	if result.State() != foundation.NotCommitted {
		t.Fatal("rejected transaction did not roll back", result.State())
	}
	requireCode(t, result.Fault(), code)
}

func bindingNoMaterial(t *testing.T, material sc.SecretMaterial, err error, code foundation.Code) {
	t.Helper()
	defer material.Destroy()
	requireCode(t, err, code)
	if material.Use(func([]byte) error { return nil }) == nil {
		t.Fatal("unconfirmed material escaped")
	}
}

func (f *bindingFixture) renewedSession(t *testing.T) identity.Actor {
	t.Helper()
	session := id[identity.Session](t)
	f.sql(t, `INSERT INTO agenteam_account.sessions(id,user_id,token_verifier,csrf_kid,issued_at,last_activity_at,idle_seconds,absolute_expires_at) VALUES($1,$2,decode(repeat('ab',32),'hex'),'a',clock_timestamp()-interval '2 minutes',clock_timestamp()-interval '2 minutes',3600,clock_timestamp()+interval '1 hour')`, session.String(), f.owner.Details().UserID)
	user, _ := foundation.ParseID[identity.User](f.owner.Details().UserID)
	a, err := identity.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// Acceptance uses the real R2 command. Completed archive facts use the existing
// R3 fixture, with all participant states and terminal time/version fields. This
// is an Authority input; no stop runtime or cleanup completion is claimed here.
func (f *bindingFixture) gate(t *testing.T, state c.Lifecycle) c.LifecycleOperation {
	t.Helper()
	command := c.ArchiveCommand
	if state == c.Deleting {
		command = c.DeleteCommand
	}
	r, err := newR2Intent(t, f.project, command, id[struct{}](t).String()).invoke(ctxFor(t), f.service, f.owner)
	if err != nil || r.Operation == nil {
		t.Fatal("real lifecycle acceptance", err)
	}
	if state == c.Archived {
		f.phase(t, *r.Operation, c.OperationCompleted)
	}
	return *r.Operation
}

// This strict persisted Usage fixture represents only ModelCall consumption
// in these tests. It is not a production Project Usage/AgentRun provider. It
// never lends Human locks; the generic lease path must fail closed for Human.
type bindingUsage struct {
	store      *postgres.Store
	planOnce   sync.Once
	planIssuer sc.PlanIssuer
	// Only negative protocol probes override the already persisted binding's
	// subject, to prove a well-formed AgentRun grant is still not authority.
	subject identity.Actor
}

func (*bindingUsage) CheckReferenceInTx(context.Context, foundation.Tx, identity.Actor, sc.CredentialRef, sc.Purpose, string, bool) error {
	return foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
}

func (u *bindingUsage) AuthorizeLeaseInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	deny := func() (sc.UseGrant, error) {
		return sc.UseGrant{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	if actor.Validate() != nil || ref.Validate() != nil || owner.Validate() != nil || ref.Details().Scope.Details().Kind != identity.ProjectScope || owner.Details().Kind != sc.ModelCallOwner || actor.Details().Kind != identity.Service || actor.Details().ServiceName != identity.SecretService || actor.Details().CauseRef != owner.Details().ID || actor.Details().ProjectID != ref.Details().Scope.Details().ProjectID {
		return deny()
	}
	if action != sc.ReadLease && action != sc.AcquireLease && action != sc.ReleaseLease {
		return deny()
	}
	projectID, _ := foundation.ParseID[identity.Project](ref.Details().Scope.Details().ProjectID)
	locks := bindingLocks(actor, projectID)
	key, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, ref.Details().ID.String())
	mode := foundation.Exclusive
	if action == sc.ReadLease {
		mode = foundation.Shared
	}
	locks = append(locks, foundation.LockRequest{Key: key, Mode: mode})
	if err := u.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return sc.UseGrant{}, err
	}
	x, err := u.store.InTx(tx)
	if err != nil {
		return sc.UseGrant{}, err
	}
	var credential, projectIDText, kind, purpose string
	var active bool
	var user, session *string
	err = x.QueryRow(ctx, `SELECT credential_id::text,project_id::text,owner_kind,purpose,active,subject_user::text,subject_session::text FROM project_fixture.secret_usage WHERE owner_id=$1`, owner.Details().ID).Scan(&credential, &projectIDText, &kind, &purpose, &active, &user, &session)
	if errors.Is(err, pgx.ErrNoRows) {
		return deny()
	}
	if err != nil {
		return sc.UseGrant{}, err
	}
	if credential != ref.Details().ID.String() || projectIDText != projectID.String() || kind != string(owner.Details().Kind) || !sc.Purpose(purpose).Valid() || action != sc.ReleaseLease && !active {
		return deny()
	}
	subject := actor
	if user != nil {
		uid, e := foundation.ParseID[identity.User](*user)
		if e != nil {
			return deny()
		}
		sid, e := foundation.ParseID[identity.Session](*session)
		if e != nil {
			return deny()
		}
		subject, err = identity.NewHuman(uid, sid)
		if err != nil {
			return deny()
		}
	}
	if u.subject.Validate() == nil {
		subject = u.subject
	}
	return sc.UseGrant{Subject: subject, Consumer: sc.Purpose(purpose)}, nil
}

func (f *bindingFixture) lease(t *testing.T, ref sc.CredentialRef) (sc.CredentialLease, identity.Actor, sc.CredentialLeaseOwner) {
	t.Helper()
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, id[struct{}](t).String())
	if err != nil {
		t.Fatal(err)
	}
	f.sql(t, `INSERT INTO project_fixture.secret_usage(owner_id,project_id,credential_id,owner_kind,purpose) VALUES($1,$2,$3,$4,'model')`, owner.Details().ID, f.project.ID.String(), ref.Details().ID.String(), string(owner.Details().Kind))
	reg, _ := identity.RegisterService(identity.SecretService)
	actor, err := reg.Actor(owner.Details().ID, f.scope)
	if err != nil {
		t.Fatal(err)
	}
	request := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.Model, LeaseOwner: owner, LeaseID: id[sc.Lease](t), Action: sc.AcquireLeaseUsage}
	result, r := f.applyModelUsage(t, request)
	lease, ok := result.Lease()
	if r.State() == foundation.Committed && !ok {
		t.Fatal("planned acquire omitted lease")
	}
	if r.State() != foundation.Committed {
		t.Fatal("real lease acquisition", r.Fault())
	}
	return lease, actor, owner
}

var _ sc.UsageAuthority = (*bindingUsage)(nil)

// These facts remain fixture-owned; no production Model consumer is bound.
func (u *bindingUsage) modelFacts(ctx context.Context, tx foundation.Tx, r sc.UsageRequest) (foundation.Digest, []foundation.LockRequest, error) {
	if r.Validate() != nil || r.Purpose != sc.Model || (r.Action != sc.AcquireLeaseUsage && r.Action != sc.ReleaseLeaseUsage && r.Action != sc.ReadLeaseUsage) {
		return "", nil, foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	var x postgres.SQLExecutor = u.store
	if tx.Valid() {
		var err error
		x, err = u.store.InTx(tx)
		if err != nil {
			return "", nil, err
		}
	}
	var credential, project, kind, purpose, subject, session string
	var active bool
	err := x.QueryRow(ctx, `SELECT credential_id::text,project_id::text,owner_kind,purpose,active,coalesce(subject_user::text,''),coalesce(subject_session::text,'') FROM project_fixture.secret_usage WHERE owner_id=$1`, r.LeaseOwner.Details().ID).Scan(&credential, &project, &kind, &purpose, &active, &subject, &session)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	if err != nil {
		return "", nil, err
	}
	if credential != r.Ref.Details().ID.String() || project != r.Ref.Details().Scope.Details().ProjectID || kind != string(r.LeaseOwner.Details().Kind) || purpose != string(r.Purpose) {
		return "", nil, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	values := []any{credential, project, kind, purpose, active, subject, session}
	key, _ := foundation.ProjectLock(project)
	gate, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "binding-usage:"+r.LeaseOwner.Details().ID)
	locks := []foundation.LockRequest{{Key: key, Mode: foundation.Shared}, {Key: gate, Mode: foundation.Shared}}
	// Human subject is deliberately NOT a user-plan grant. The original
	// human-no-user-plan case must poison at the real Audit/Session boundary.
	if r.Action == sc.ReadLeaseUsage {
		var call, snapshot, input, process, dispatch, cOwner, cRef, cSnapshot, cInput, cProcess, current string
		var fence, ordinal, cFence int64
		var live, cLive bool
		err = x.QueryRow(ctx, `SELECT i.call_id::text,i.snapshot_id::text,i.input_digest,i.process_id::text,i.fence,i.ordinal,i.dispatch,i.active,c.owner_id::text,c.credential_id::text,c.snapshot_id::text,c.input_digest,c.process_id::text,c.fence,c.current_invocation::text,c.active FROM project_fixture.secret_invocations i JOIN project_fixture.secret_calls c ON c.id=i.call_id WHERE i.id=$1`, r.RequestID).Scan(&call, &snapshot, &input, &process, &fence, &ordinal, &dispatch, &live, &cOwner, &cRef, &cSnapshot, &cInput, &cProcess, &cFence, &current, &cLive)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
		}
		if err != nil {
			return "", nil, err
		}
		if call != r.LeaseOwner.Details().ID || cOwner != call || cRef != credential || snapshot != cSnapshot || input != cInput || process != cProcess || fence != cFence || fence < 1 || ordinal < 1 || current != r.RequestID || !live || !cLive || dispatch != "reserved" {
			return "", nil, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
		}
		values = append(values, call, snapshot, input, process, fence, ordinal, dispatch, live, cOwner, cRef, cSnapshot, cInput, cProcess, cFence, current, cLive)
		callGate, _ := foundation.AggregateLock(foundation.OperationAggregate, call)
		invocation, _ := foundation.RecordLock(foundation.ReferenceRecordLock, "binding-invocation:"+r.RequestID)
		locks = append(locks, foundation.LockRequest{Key: callGate, Mode: foundation.Shared}, foundation.LockRequest{Key: invocation, Mode: foundation.Shared})
	}
	b, _ := json.Marshal(values)
	h := sha256.Sum256(b)
	return foundation.Digest("sha256:" + hex.EncodeToString(h[:])), locks, nil
}
func (u *bindingUsage) DiscoverUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	mapping, locks, err := u.modelFacts(ctx, foundation.Tx{}, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	u.planOnce.Do(func() { u.planIssuer = sc.NewPlanIssuer() })
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	return sc.NewUsageDependencies(u.planIssuer, binding, mapping, locks)
}
func (u *bindingUsage) ValidateUsageInTx(ctx context.Context, tx foundation.Tx, r sc.UsageRequest, d sc.UsageDependencies) error {
	mapping, locks, err := u.modelFacts(ctx, tx, r)
	if err != nil {
		return err
	}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return err
	}
	if !d.Matches(u.planIssuer, binding, mapping) {
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
	}
	return u.store.RequireHeldLocks(ctx, tx, locks)
}
func (f *bindingFixture) applyModelUsage(t *testing.T, r sc.UsageRequest) (sc.UsageResult, foundation.CommitResult) {
	t.Helper()
	plan, err := f.bundle.secret.DiscoverUsage(ctxFor(t), r)
	if err != nil {
		t.Fatal(err)
	}
	var result sc.UsageResult
	commit := f.raw.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
		if err := f.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		var err error
		result, err = f.bundle.secret.ApplyUsageInTx(ctx, tx, r, plan)
		return err
	})
	return result, commit
}
func (f *bindingFixture) modelReadRequest(t *testing.T, actor identity.Actor, lease sc.CredentialLease, owner sc.CredentialLeaseOwner) sc.UsageRequest {
	t.Helper()
	r := sc.UsageRequest{Actor: actor, Ref: lease.CredentialRef, Purpose: sc.Model, LeaseOwner: owner, LeaseID: lease.LeaseID, Action: sc.ReadLeaseUsage, RequestID: id[struct{}](t).String()}
	snapshot, process := id[struct{}](t).String(), id[struct{}](t).String()
	input := "sha256:" + strings.Repeat("b", 64)
	call := owner.Details().ID
	f.sql(t, `INSERT INTO project_fixture.secret_calls(id,owner_id,credential_id,snapshot_id,input_digest,process_id,fence,current_invocation) VALUES($1,$1,$2,$3,$4,$5,1,$6) ON CONFLICT(id) DO UPDATE SET snapshot_id=excluded.snapshot_id,process_id=excluded.process_id,current_invocation=excluded.current_invocation`, call, lease.CredentialRef.Details().ID.String(), snapshot, input, process, r.RequestID)
	f.sql(t, `INSERT INTO project_fixture.secret_invocations(id,call_id,snapshot_id,input_digest,process_id,fence,ordinal,dispatch) VALUES($1,$2,$3,$4,$5,1,1,'reserved')`, r.RequestID, call, snapshot, input, process)
	return r
}

// An owned protocol observer and optional confirmed-COMMIT ACK fault. Only
// command tags, SQLSTATE, backend PID and ReadyForQuery state are retained: no
// startup credentials, cancellation keys, SQL statements, rows or Secret bytes.
type bindingPGTrace struct {
	listener      net.Listener
	upstream      string
	mu            sync.Mutex
	connections   map[net.Conn]bool
	events        []string
	next          int
	wg            sync.WaitGroup
	dropCommitACK atomic.Bool
	ackDropped    chan struct{}
}

func newBindingPGTrace(t *testing.T, upstream string) *bindingPGTrace {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &bindingPGTrace{listener: listener, upstream: upstream, connections: map[net.Conn]bool{}, ackDropped: make(chan struct{})}
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		for {
			client, err := listener.Accept()
			if err != nil {
				return
			}
			p.mu.Lock()
			p.next++
			n := p.next
			p.connections[client] = true
			p.mu.Unlock()
			p.wg.Add(1)
			go p.serve(client, n)
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		p.mu.Lock()
		for conn := range p.connections {
			_ = conn.Close()
		}
		p.mu.Unlock()
		p.wg.Wait()
	})
	return p
}

func (p *bindingPGTrace) record(n int, event string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, fmt.Sprintf("%d:%s", n, event))
}
func (p *bindingPGTrace) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}
func (p *bindingPGTrace) serve(client net.Conn, n int) {
	defer p.wg.Done()
	defer func() { _ = client.Close(); p.mu.Lock(); delete(p.connections, client); p.mu.Unlock() }()
	server, err := net.Dial("tcp", p.upstream)
	if err != nil {
		return
	}
	p.mu.Lock()
	p.connections[server] = true
	p.mu.Unlock()
	defer func() { _ = server.Close(); p.mu.Lock(); delete(p.connections, server); p.mu.Unlock() }()
	var header [4]byte
	if _, err = io.ReadFull(client, header[:]); err != nil {
		return
	}
	size := binary.BigEndian.Uint32(header[:])
	if size < 8 || size > 1<<20 {
		return
	}
	startup := make([]byte, int(size))
	copy(startup, header[:])
	if _, err = io.ReadFull(client, startup[4:]); err != nil {
		return
	}
	if binary.BigEndian.Uint32(startup[4:8]) == 80877102 {
		p.record(n, "cancel")
	}
	if _, err = server.Write(startup); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	var dropACK atomic.Bool
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			frame, err := readPGFrame(client)
			if err != nil {
				return
			}
			if frame[0] == 'Q' {
				q := strings.Trim(string(frame[5:]), "\x00; \t\r\n")
				if strings.EqualFold(q, "COMMIT") || strings.EqualFold(q, "ROLLBACK") {
					p.record(n, "send-"+strings.ToUpper(q))
				}
				if strings.EqualFold(q, "COMMIT") && p.dropCommitACK.CompareAndSwap(true, false) {
					dropACK.Store(true)
				}
			}
			if _, err = server.Write(frame); err != nil {
				return
			}
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		completion := false
		for {
			frame, err := readPGFrame(server)
			if err != nil {
				p.record(n, "backend-closed")
				return
			}
			switch frame[0] {
			case 'C':
				if string(frame[5:]) == "COMMIT\x00" || string(frame[5:]) == "ROLLBACK\x00" {
					p.record(n, "tag-"+strings.TrimRight(string(frame[5:]), "\x00"))
					completion = true
				}
				if dropACK.Load() && string(frame[5:]) == "COMMIT\x00" {
					ready, err := readPGFrame(server)
					if err != nil || ready[0] != 'Z' || len(ready) != 6 || ready[5] != 'I' {
						p.record(n, "commit-idle-unconfirmed")
						return
					}
					// The committed case drops only a proven COMMIT + idle ACK.
					// A later driver CancelRequest cannot race this transaction's
					// commit. Pending/rollback use the unchanged held-writer proxy.
					p.record(n, "commit-idle-ack-dropped")
					_ = client.Close()
					close(p.ackDropped)
					return
				}
			case 'E':
				for fields := frame[5:]; len(fields) > 1; {
					i := bytes.IndexByte(fields[1:], 0)
					if i < 0 {
						break
					}
					if fields[0] == 'C' {
						p.record(n, "sqlstate-"+string(fields[1:1+i]))
					}
					fields = fields[i+2:]
				}
			case 'Z':
				if completion {
					p.record(n, "ready-"+string(frame[5:]))
					completion = false
				}
			case 'K':
				if len(frame) >= 9 {
					p.record(n, fmt.Sprintf("backend-%d", binary.BigEndian.Uint32(frame[5:9])))
				}
			}
			if _, err = client.Write(frame); err != nil {
				return
			}
		}
	}()
	<-done
	_ = client.Close()
	_ = server.Close()
	<-done
}
