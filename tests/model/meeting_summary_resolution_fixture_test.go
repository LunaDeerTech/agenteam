//go:build integration

package model_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/account/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/accountmail"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/recoverylog"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/jackc/pgx/v5"
)

// Only the unimplemented Meeting domain is isolated. Account, Project, Model,
// Secret and their transaction authorities are real and share the same Store.
// Invitation helpers below use the private restricted recovery sink and formal
// Redeem/Login; no positive identity or Session is inserted with SQL.
type meetingResolutionFixture struct {
	*currentResolutionFixture
	identity *projectUsageHTTPFixture
	strict   *meetingResolutionConsumer
}

func newMeetingResolutionFixture(t *testing.T, initialize bool) *meetingResolutionFixture {
	t.Helper()
	base, identity := meetingResolutionIdentity(t)
	_, e := base.raw.Exec(testContext(t), `CREATE SCHEMA meeting_resolution_fixture;
 CREATE TABLE meeting_resolution_fixture.meetings(id uuid PRIMARY KEY,project_id uuid NOT NULL,phase text NOT NULL,version bigint NOT NULL);
 CREATE TABLE meeting_resolution_fixture.operations(id uuid PRIMARY KEY,meeting_id uuid NOT NULL REFERENCES meeting_resolution_fixture.meetings(id),call_id uuid NOT NULL,purpose text NOT NULL,initiator uuid NOT NULL,input_id uuid NOT NULL,phase text NOT NULL,version bigint NOT NULL);
 CREATE TABLE meeting_resolution_fixture.inputs(operation_id uuid PRIMARY KEY,input_id uuid NOT NULL,snapshot_id uuid NOT NULL);`)
	if e != nil {
		t.Fatal(e)
	}
	v := bindMeetingResolution(t, base, identity)
	if initialize {
		if e = v.service.InitializeMeetingSummarySelection(testContext(t)); e != nil {
			t.Fatal(e)
		}
	}
	return v
}
func meetingResolutionIdentity(t *testing.T) (*projectConfigurationFixture, *projectUsageHTTPFixture) {
	t.Helper()
	db := newDatabase(t)
	raw := openStore(t, db.Config(t, nil))
	tracked := &projectUsageHTTPStore{Store: raw}
	base := assembleProjectConfiguration(t, db, raw, tracked)
	_, ck, _ := testKeys(t)
	catalog := ec.NewCatalog()
	revoked, err := ac.DefineSessionsRevoked(catalog)
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := ac.DefineDeliveryRequested(catalog)
	if err != nil {
		t.Fatal(err)
	}
	process := systemHTTPAccountProcess{newID[ac.Process](t)}
	processID, _ := f.ParseID[oc.Process](process.id.String())
	events, err := outbox.New(tracked, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{ac.AccountProducer: base.accounts}, Sessions: base.accounts, System: base.accounts, Audit: base.aud, Cursors: ck, Processes: liveProcess{processID}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "account-recovery.jsonl")
	sink, err := recoverylog.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	challenges, err := account.NewChallenges(base.accounts, process.id)
	if err != nil {
		t.Fatal(err)
	}
	core, err := account.New(account.Dependencies{Authority: base.accounts, Audit: base.aud, Secrets: base.secrets, Events: events, SessionsRevoked: revoked, DeliveryRequested: delivery, Processes: process, RecoveryLog: sink, Challenges: challenges})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		core.StopAdmission()
		drainErr := core.Drain(ctx)
		cancel()
		if drainErr != nil || !core.Joined() {
			t.Error("formal Account Drain did not join", drainErr)
			// Force owns a fresh cleanup lifecycle. A context return can precede
			// forceDone, which Joined does not cover. Wait for that same Force
			// coordinator before waiting for the remaining local ownership.
			forceCtx, forceCancel := context.WithTimeout(context.Background(), 3*time.Second)
			forceErr := core.Force(forceCtx)
			forceCancel()
			if forceErr != nil || !core.Joined() {
				t.Error("formal Account Force incomplete; retaining actual join ownership", forceErr)
			}
			if err := core.Force(context.Background()); err != nil {
				t.Error("formal Account Force completed with error", err)
			}
			for !core.Joined() {
				time.Sleep(10 * time.Millisecond)
			}
		}
	})
	status, err := core.Bootstrap(testContext(t))
	if err != nil || !status.Created || status.LogState != "written" {
		t.Fatal("formal Bootstrap failed", err)
	}
	password := projectUsageRecoveryMaterial(t, logPath, "bootstrap", "")
	v := &projectUsageHTTPFixture{core: core, password: password, logPath: logPath, tracked: tracked}
	t.Cleanup(password.Destroy)
	v.adminBrowser = v.login(t, "admin@mail.com")
	base.admin = v.adminBrowser.actor
	policy, err := outbound.NewPolicyService(tracked, base.aud, outbound.Authorizations{Sessions: base.accounts, System: base.accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = policy.Reload(testContext(t)); err != nil {
		t.Fatal(err)
	}
	trust, err := outbound.LoadTrustStore("")
	if err != nil {
		t.Fatal(err)
	}
	client, err := outbound.NewClient(policy, trust, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		client.StopAdmission()
		if e := client.Drain(ctx); e != nil {
			t.Error("mail transport cleanup", e)
		}
	})
	registry, err := accountmail.NewWorkRegistry(process)
	if err != nil {
		t.Fatal(err)
	}
	port, err := account.NewDeliveryPort(core, registry)
	if err != nil {
		t.Fatal(err)
	}
	v.worker, err = accountmail.New(accountmail.Dependencies{Port: port, Registry: registry, Outbound: client, Trust: trust, RecoveryLog: sink, PublicOrigin: systemHTTPOrigin})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		registry.StopAdmission()
		if e := registry.Drain(ctx); e != nil || !registry.Joined() {
			t.Error("mail material owner did not join", e)
		}
	})
	v.ownerName = "owner-" + newID[struct{}](t).String()[24:]
	v.ownerBrowser = v.invite(t, v.ownerName)
	v.otherBrowser = v.invite(t, "other-"+newID[struct{}](t).String()[24:])
	base.regular, base.owner = v.ownerBrowser.actor, v.ownerBrowser.actor
	base.project = base.createProject(t, base.owner)
	base.scope, _ = id.InProject(base.project.ID)
	return base, v
}
func bindMeetingResolution(t *testing.T, base *projectConfigurationFixture, identity *projectUsageHTTPFixture) *meetingResolutionFixture {
	t.Helper()
	_, _, sk := testKeys(t)
	store := &currentResolutionStore{sharedStore: base.store}
	consumers := &meetingResolutionConsumer{store: store, base: base, issuer: mc.NewPlanIssuer()}
	reg, _ := id.RegisterService(id.SecretService)
	authority, e := model.NewAuthority(store, model.Authorizations{Sessions: base.accounts, System: base.accounts, Projects: base.projects, Resolution: &model.ResolutionAuthorizations{Consumers: consumers, SecretService: reg}})
	if e != nil {
		t.Fatal(e)
	}
	router, e := model.NewSecretUsageRouter(authority, base.accounts)
	if e != nil {
		t.Fatal(e)
	}
	projectSecrets, e := project.NewSecretAuthority(base.projects)
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := secret.New(store, sk, base.aud, secret.Authorizations{Sessions: base.accounts, System: base.accounts, Projects: projectSecrets, Usage: router, AccountWrites: base.accounts})
	if e != nil {
		t.Fatal(e)
	}
	if e = secrets.Initialize(testContext(t)); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(secrets.StopMaintenance)
	tap := &currentResolutionUsage{UsageOperations: secrets}
	deps := base.deps
	deps.Secret = tap
	svc, e := model.New(store, authority, deps)
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.Initialize(testContext(t)); e != nil {
		t.Fatal(e)
	}
	return &meetingResolutionFixture{currentResolutionFixture: &currentResolutionFixture{projectConfigurationFixture: base, resolving: svc, resolutionAuthority: authority, resolutionSecrets: secrets, router: router, tap: tap, observed: store}, identity: identity, strict: consumers}
}

// Separate columns are canonical business facts, not a serialized request.
// Input is deliberately absent from ResolveRequest and discovered through the
// operation. Both aggregate versions and that input enter the opaque mapping.
type meetingResolutionFacts struct {
	Meeting, Project, MeetingPhase                             string
	MeetingVersion                                             int64
	Operation, Call, Purpose, Initiator, Input, OperationPhase string
	OperationVersion                                           int64
}
type meetingResolutionConsumer struct {
	store     model.Store
	base      *projectConfigurationFixture
	issuer    mc.PlanIssuer
	validated atomic.Int64
}

func meetingResolutionLocks(r mc.ResolveRequest) []f.LockRequest {
	var out []f.LockRequest
	add := func(k f.LockKey, e error) {
		if e != nil {
			panic(e)
		}
		out = append(out, f.LockRequest{Key: k, Mode: f.Shared})
	}
	add(f.UserLock(r.Actor.Details().UserID))
	add(f.ProjectLock(r.Consumer.ProjectID.String()))
	add(f.AggregateLock(f.MeetingAggregate, r.Consumer.MeetingID))
	add(f.AggregateLock(f.OperationAggregate, r.Consumer.OperationID))
	k, e := f.RecordLock(f.ReferenceRecordLock, "meeting-resolution-facts:"+r.Consumer.OperationID)
	if e != nil {
		panic(e)
	}
	out = append(out, f.LockRequest{Key: k, Mode: f.Exclusive})
	sort.Slice(out, func(i, j int) bool { return f.CompareLockKeys(out[i].Key, out[j].Key) < 0 })
	return out
}
func (c *meetingResolutionConsumer) facts(ctx context.Context, x postgres.SQLExecutor, r mc.ResolveRequest) (meetingResolutionFacts, error) {
	var d meetingResolutionFacts
	e := x.QueryRow(ctx, `SELECT m.id::text,m.project_id::text,m.phase,m.version,o.id::text,o.call_id::text,o.purpose,o.initiator::text,o.input_id::text,o.phase,o.version FROM meeting_resolution_fixture.operations o JOIN meeting_resolution_fixture.meetings m ON m.id=o.meeting_id WHERE o.id=$1`, r.Consumer.OperationID).Scan(&d.Meeting, &d.Project, &d.MeetingPhase, &d.MeetingVersion, &d.Operation, &d.Call, &d.Purpose, &d.Initiator, &d.Input, &d.OperationPhase, &d.OperationVersion)
	if errors.Is(e, pgx.ErrNoRows) {
		return d, nil
	}
	return d, e
}
func (c *meetingResolutionConsumer) Discover(ctx context.Context, r mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	if r.Validate() != nil || r.Action != mc.ResolveConsumer || r.Actor.Details().Kind != id.Human || r.Consumer.Kind != mc.MeetingConsumer {
		return mc.ConsumerDependencies{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	d, e := c.facts(ctx, c.store, *r.Resolve)
	if e != nil {
		return mc.ConsumerDependencies{}, e
	}
	b, _ := mc.ConsumerBinding(r)
	return mc.NewConsumerDependencies(c.issuer, mc.ConsumerDependencyDetails{Binding: b, Mapping: resolutionDigest(d), Locks: meetingResolutionLocks(*r.Resolve)})
}
func (c *meetingResolutionConsumer) ValidateInTx(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, p mc.ConsumerDependencies) error {
	if r.Validate() != nil || r.Action != mc.ResolveConsumer || r.Actor.Details().Kind != id.Human || r.Consumer.Kind != mc.MeetingConsumer {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	x, e := c.store.InTx(tx)
	if e != nil {
		return e
	}
	if e = c.store.RequireHeldLocks(ctx, tx, meetingResolutionLocks(*r.Resolve)); e != nil {
		return e
	}
	if e = c.base.accounts.RequireCurrentSession(ctx, tx, r.Actor); e != nil {
		return e
	}
	if _, e = c.base.projects.AuthorizeProject(ctx, tx, r.Actor, r.Consumer.ProjectID, id.Mutate); e != nil {
		return e
	}
	d, e := c.facts(ctx, x, *r.Resolve)
	if e != nil {
		return e
	}
	b, _ := mc.ConsumerBinding(r)
	_, inputErr := f.ParseID[struct{}](d.Input)
	if d.Meeting != r.Consumer.MeetingID || d.Project != r.Consumer.ProjectID.String() || d.Operation != r.Consumer.OperationID || d.Call != r.Resolve.LeaseOwner.Details().ID || r.CallID == nil || d.Call != r.CallID.String() || d.Purpose != string(r.Consumer.Purpose) || d.Initiator != r.Actor.Details().UserID || d.MeetingPhase != "active" || d.OperationPhase != "pending" && d.OperationPhase != "accepted" || d.MeetingVersion < 1 || d.OperationVersion < 1 || inputErr != nil || !p.Matches(c.issuer, b, resolutionDigest(d)) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	c.validated.Add(1)
	return nil
}
func (v *meetingResolutionFixture) request(t *testing.T, purpose mc.Purpose) mc.ResolveRequest {
	t.Helper()
	r := mc.ResolveRequest{Actor: v.owner, Consumer: mc.Consumer{Kind: mc.MeetingConsumer, ProjectID: v.project.ID, MeetingID: newID[struct{}](t).String(), OperationID: newID[struct{}](t).String(), Purpose: purpose}, Purpose: purpose, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.MeetingSummarySelector}, LeaseOwner: resolutionCallOwner(t)}
	v.bindFacts(t, r)
	return r
}
func (v *meetingResolutionFixture) bindFacts(t *testing.T, r mc.ResolveRequest) {
	t.Helper()
	if r.Validate() != nil {
		t.Fatal("invalid strict Meeting request")
	}
	_, e := v.raw.Exec(testContext(t), `INSERT INTO meeting_resolution_fixture.meetings(id,project_id,phase,version)VALUES($1,$2,'active',1) ON CONFLICT(id) DO NOTHING`, r.Consumer.MeetingID, r.Consumer.ProjectID.String())
	if e != nil {
		t.Fatal(e)
	}
	_, e = v.raw.Exec(testContext(t), `INSERT INTO meeting_resolution_fixture.operations(id,meeting_id,call_id,purpose,initiator,input_id,phase,version)VALUES($1,$2,$3,$4,$5,$6,'pending',1) ON CONFLICT(id) DO UPDATE SET meeting_id=excluded.meeting_id,call_id=excluded.call_id,purpose=excluded.purpose,initiator=excluded.initiator,input_id=excluded.input_id,phase='pending',version=meeting_resolution_fixture.operations.version+1`, r.Consumer.OperationID, r.Consumer.MeetingID, r.LeaseOwner.Details().ID, string(r.Purpose), r.Actor.Details().UserID, newID[struct{}](t).String())
	if e != nil {
		t.Fatal(e)
	}
}
func (v *meetingResolutionFixture) summary(t *testing.T) mc.MeetingSummarySelection {
	t.Helper()
	s, e := v.service.GetMeetingSummarySelection(testContext(t), v.admin)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func (v *meetingResolutionFixture) choose(t *testing.T, m mc.ModelID) mc.CommandReceipt {
	t.Helper()
	s := v.summary(t)
	out, e := v.service.UpdateMeetingSummarySelection(testContext(t), mc.UpdateMeetingSummarySelectionRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), SelectionID: s.ID, ExpectedVersion: s.Version, Model: m})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func (v *meetingResolutionFixture) final(t *testing.T, r mc.ResolveRequest, p mc.ResolutionPlan, capture, fail bool) (mc.ResolvedModel, f.CommitResult) {
	t.Helper()
	var out mc.ResolvedModel
	result := v.observed.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if e := v.observed.AcquireAll(ctx, tx, p.RequiredLocks()); e != nil {
			return e
		}
		x, e := v.observed.InTx(tx)
		if e != nil {
			return e
		}
		if capture {
			if _, e = x.Exec(ctx, `INSERT INTO meeting_resolution_fixture.inputs(operation_id,input_id,snapshot_id) SELECT id,input_id,$2 FROM meeting_resolution_fixture.operations WHERE id=$1`, r.Consumer.OperationID, p.Details().SnapshotID.String()); e != nil {
				return e
			}
		}
		out, e = v.resolving.ResolveModelInTx(ctx, tx, r, p)
		if e != nil {
			return e
		}
		if fail {
			return f.NewFault(f.DependencyUnavailable, f.NotStarted)
		}
		return nil
	})
	return out, result
}
func (v *meetingResolutionFixture) atomicFacts(t *testing.T, r mc.ResolveRequest, p mc.ResolutionPlan, want int) {
	t.Helper()
	var input, snapshot, binding, lease int
	var leaseID any
	if p.Details().LeaseID != nil {
		leaseID = p.Details().LeaseID.String()
	}
	e := v.raw.QueryRow(testContext(t), `SELECT (SELECT count(*) FROM meeting_resolution_fixture.inputs WHERE operation_id=$1),(SELECT count(*) FROM agenteam_model.snapshots WHERE id=$2),(SELECT count(*) FROM agenteam_model.snapshot_bindings WHERE snapshot_id=$2),(SELECT count(*) FROM agenteam_secret.secret_leases WHERE id=$3::uuid)`, r.Consumer.OperationID, p.Details().SnapshotID.String(), leaseID).Scan(&input, &snapshot, &binding, &lease)
	wantLease := want
	if leaseID == nil {
		wantLease = 0
	}
	if e != nil || input != want || snapshot != want || binding != want || lease != wantLease {
		t.Fatalf("input/snapshot/binding/lease=%d/%d/%d/%d want=%d lease=%d: %v", input, snapshot, binding, lease, want, wantLease, e)
	}
}
