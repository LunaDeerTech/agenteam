//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// Only Knowledge/Memory canonical facts are isolated. Positive identities use
// the accepted Bootstrap/Invite/Redeem/Login helper, never inserted Sessions.
// These tables do not specify the future D13/D14 production schema.
type platformEmbeddingResolutionFixture struct {
	*currentResolutionFixture
	identity *projectUsageHTTPFixture
	strict   *platformEmbeddingResolutionConsumer
}

func newPlatformEmbeddingResolutionFixture(t *testing.T) *platformEmbeddingResolutionFixture {
	t.Helper()
	base, identity := meetingResolutionIdentity(t)
	base.sql(t, `CREATE SCHEMA platform_embedding_fixture;
 CREATE TABLE platform_embedding_fixture.agents(id uuid PRIMARY KEY,project_id uuid NOT NULL,phase text NOT NULL,version bigint NOT NULL);
 CREATE TABLE platform_embedding_fixture.subjects(id uuid PRIMARY KEY,project_id uuid NOT NULL,kind text NOT NULL,agent_id uuid REFERENCES platform_embedding_fixture.agents(id),phase text NOT NULL,version bigint NOT NULL);
 CREATE TABLE platform_embedding_fixture.inputs(id uuid PRIMARY KEY,subject_id uuid NOT NULL REFERENCES platform_embedding_fixture.subjects(id),digest text NOT NULL,schema_version bigint NOT NULL,phase text NOT NULL,version bigint NOT NULL);
 CREATE TABLE platform_embedding_fixture.operations(id uuid PRIMARY KEY,subject_id uuid NOT NULL REFERENCES platform_embedding_fixture.subjects(id),call_id uuid NOT NULL,purpose text NOT NULL,initiator uuid NOT NULL,build_target text NOT NULL,input_id uuid NOT NULL REFERENCES platform_embedding_fixture.inputs(id),input_digest text NOT NULL,input_schema bigint NOT NULL,input_version bigint NOT NULL,phase text NOT NULL,version bigint NOT NULL);
 CREATE TABLE platform_embedding_fixture.accepted(operation_id uuid PRIMARY KEY REFERENCES platform_embedding_fixture.operations(id),input_id uuid NOT NULL,digest text NOT NULL,schema_version bigint NOT NULL,input_version bigint NOT NULL,snapshot_id uuid NOT NULL);`)
	return bindPlatformEmbeddingResolution(t, base, identity)
}

func bindPlatformEmbeddingResolution(t *testing.T, base *projectConfigurationFixture, identity *projectUsageHTTPFixture) *platformEmbeddingResolutionFixture {
	t.Helper()
	_, _, sk := testKeys(t)
	store := &currentResolutionStore{sharedStore: base.store}
	consumers := &platformEmbeddingResolutionConsumer{store: store, base: base, issuer: mc.NewPlanIssuer()}
	reg, e := id.RegisterService(id.SecretService)
	if e != nil {
		t.Fatal(e)
	}
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
	return &platformEmbeddingResolutionFixture{currentResolutionFixture: &currentResolutionFixture{projectConfigurationFixture: base, resolving: svc, resolutionAuthority: authority, resolutionSecrets: secrets, router: router, tap: tap, observed: store}, identity: identity, strict: consumers}
}

type platformEmbeddingResolutionFacts struct {
	Subject, Project, Kind, SubjectAgent, SubjectPhase               string
	SubjectVersion                                                   int64
	Agent, AgentProject, AgentPhase                                  string
	AgentVersion                                                     int64
	Operation, Call, Purpose, Initiator, BuildTarget, OperationPhase string
	OperationVersion                                                 int64
	Input, InputSubject, InputDigest, InputPhase, ExpectedDigest     string
	InputSchema, InputVersion, ExpectedSchema, ExpectedVersion       int64
}

type platformEmbeddingResolutionConsumer struct {
	store                   model.Store
	base                    *projectConfigurationFixture
	issuer                  mc.PlanIssuer
	factsChecked, validated atomic.Int64
}

func (c *platformEmbeddingResolutionConsumer) facts(ctx context.Context, x postgres.SQLExecutor, operation string) (platformEmbeddingResolutionFacts, error) {
	var d platformEmbeddingResolutionFacts
	e := x.QueryRow(ctx, `SELECT s.id::text,s.project_id::text,s.kind,coalesce(s.agent_id::text,''),s.phase,s.version,
 coalesce(a.id::text,''),coalesce(a.project_id::text,''),coalesce(a.phase,''),coalesce(a.version,0),
 o.id::text,o.call_id::text,o.purpose,o.initiator::text,o.build_target,o.phase,o.version,
 i.id::text,i.subject_id::text,i.digest,i.phase,o.input_digest,i.schema_version,i.version,o.input_schema,o.input_version
 FROM platform_embedding_fixture.operations o JOIN platform_embedding_fixture.subjects s ON s.id=o.subject_id
 JOIN platform_embedding_fixture.inputs i ON i.id=o.input_id LEFT JOIN platform_embedding_fixture.agents a ON a.id=s.agent_id WHERE o.id=$1`, operation).Scan(&d.Subject, &d.Project, &d.Kind, &d.SubjectAgent, &d.SubjectPhase, &d.SubjectVersion, &d.Agent, &d.AgentProject, &d.AgentPhase, &d.AgentVersion, &d.Operation, &d.Call, &d.Purpose, &d.Initiator, &d.BuildTarget, &d.OperationPhase, &d.OperationVersion, &d.Input, &d.InputSubject, &d.InputDigest, &d.InputPhase, &d.ExpectedDigest, &d.InputSchema, &d.InputVersion, &d.ExpectedSchema, &d.ExpectedVersion)
	if errors.Is(e, pgx.ErrNoRows) {
		return d, nil
	}
	return d, e
}

func platformEmbeddingResolutionLocks(r mc.ResolveRequest, d platformEmbeddingResolutionFacts) []f.LockRequest {
	var out []f.LockRequest
	add := func(key f.LockKey, e error) {
		if e != nil {
			panic(e)
		}
		out = append(out, f.LockRequest{Key: key, Mode: f.Shared})
	}
	add(f.UserLock(r.Actor.Details().UserID))
	add(f.ProjectLock(r.Consumer.ProjectID.String()))
	if r.Consumer.AgentID != nil {
		add(f.AgentLock(r.Consumer.AgentID.String()))
	}
	if d.Subject != "" {
		if d.Kind == "knowledge" {
			add(f.KnowledgeTreeLock(d.Subject))
		} else {
			add(f.AggregateLock(f.MemoryAggregate, d.Subject))
		}
	}
	add(f.AggregateLock(f.OperationAggregate, r.Consumer.OperationID))
	if d.Input != "" {
		add(f.RecordLock(f.ReferenceRecordLock, "embedding-input:"+d.Input))
	}
	key, e := f.RecordLock(f.ReferenceRecordLock, "embedding-operation:"+r.Consumer.OperationID)
	if e != nil {
		panic(e)
	}
	out = append(out, f.LockRequest{Key: key, Mode: f.Exclusive})
	sort.Slice(out, func(i, j int) bool { return f.CompareLockKeys(out[i].Key, out[j].Key) < 0 })
	return out
}

func (c *platformEmbeddingResolutionConsumer) Discover(ctx context.Context, r mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	if r.Validate() != nil || r.Action != mc.ResolveConsumer || r.Actor.Details().Kind != id.Human || r.Consumer.Purpose != mc.KnowledgeEmbedding && r.Consumer.Purpose != mc.MemoryEmbedding {
		return mc.ConsumerDependencies{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	d, e := c.facts(ctx, c.store, r.Consumer.OperationID)
	if e != nil {
		return mc.ConsumerDependencies{}, e
	}
	b, e := mc.ConsumerBinding(r)
	if e != nil {
		return mc.ConsumerDependencies{}, e
	}
	return mc.NewConsumerDependencies(c.issuer, mc.ConsumerDependencyDetails{Binding: b, Mapping: resolutionDigest(d), Locks: platformEmbeddingResolutionLocks(*r.Resolve, d)})
}

func (c *platformEmbeddingResolutionConsumer) ValidateInTx(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, p mc.ConsumerDependencies) error {
	deny := func() error { return f.NewFault(f.Forbidden, f.NotStarted) }
	if r.Validate() != nil || r.Action != mc.ResolveConsumer || r.Actor.Details().Kind != id.Human {
		return deny()
	}
	x, e := c.store.InTx(tx)
	if e != nil {
		return e
	}
	b, e := mc.ConsumerBinding(r)
	if e != nil || !p.Matches(c.issuer, b, p.Details().Mapping) {
		return deny()
	}
	if e = c.store.RequireHeldLocks(ctx, tx, p.RequiredLocks()); e != nil {
		return e
	}
	if e = c.base.accounts.RequireCurrentSession(ctx, tx, r.Actor); e != nil {
		return e
	}
	if _, e = c.base.projects.AuthorizeProject(ctx, tx, r.Actor, r.Consumer.ProjectID, id.Mutate); e != nil {
		return e
	}
	d, e := c.facts(ctx, x, r.Consumer.OperationID)
	if e != nil {
		return e
	}
	c.factsChecked.Add(1)
	if !p.Matches(c.issuer, b, resolutionDigest(d)) {
		return deny()
	}
	if e = c.store.RequireHeldLocks(ctx, tx, platformEmbeddingResolutionLocks(*r.Resolve, d)); e != nil {
		return e
	}
	_, subjectErr := f.ParseID[struct{}](d.Subject)
	_, inputErr := f.ParseID[struct{}](d.Input)
	if subjectErr != nil || inputErr != nil || d.Project != r.Consumer.ProjectID.String() || d.SubjectPhase != "active" || d.SubjectVersion < 1 || d.Operation != r.Consumer.OperationID || d.Call != r.Resolve.LeaseOwner.Details().ID || r.CallID == nil || d.Call != r.CallID.String() || d.Purpose != string(r.Consumer.Purpose) || d.Initiator != r.Actor.Details().UserID || d.BuildTarget != "new_index" || d.OperationPhase != "pending" && d.OperationPhase != "accepted" || d.OperationVersion < 1 || d.InputSubject != d.Subject || d.InputPhase != "active" || f.Digest(d.InputDigest).Validate() != nil || d.InputDigest != d.ExpectedDigest || d.InputSchema < 1 || d.InputSchema != d.ExpectedSchema || d.InputVersion < 1 || d.InputVersion != d.ExpectedVersion {
		return deny()
	}
	switch r.Consumer.Purpose {
	case mc.KnowledgeEmbedding:
		if r.Consumer.Kind != mc.KnowledgeConsumer || d.Kind != "knowledge" || d.SubjectAgent != "" || d.Agent != "" {
			return deny()
		}
	case mc.MemoryEmbedding:
		if r.Consumer.Kind != mc.MemoryConsumer || r.Consumer.AgentID == nil || d.Kind != "memory" || d.SubjectAgent != r.Consumer.AgentID.String() || d.Agent != d.SubjectAgent || d.AgentProject != d.Project || d.AgentPhase != "active" || d.AgentVersion < 1 {
			return deny()
		}
	default:
		return deny()
	}
	// Accepted business input is an independent row, not a request supplied by
	// the caller. Once present, even a consistently rewritten live input cannot
	// borrow the old snapshot's authorization. InTx may see this same-Tx insert.
	var acceptedInput, acceptedDigest, acceptedSnapshot string
	var acceptedSchema, acceptedVersion int64
	e = x.QueryRow(ctx, `SELECT input_id::text,digest,schema_version,input_version,snapshot_id::text FROM platform_embedding_fixture.accepted WHERE operation_id=$1`, d.Operation).Scan(&acceptedInput, &acceptedDigest, &acceptedSchema, &acceptedVersion, &acceptedSnapshot)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	if e == nil && (acceptedInput != d.Input || acceptedDigest != d.InputDigest || acceptedSchema != d.InputSchema || acceptedVersion != d.InputVersion || acceptedSnapshot != r.SnapshotID.String()) {
		return deny()
	}
	c.validated.Add(1)
	return nil
}

func (v *platformEmbeddingResolutionFixture) request(t *testing.T, purpose mc.Purpose) mc.ResolveRequest {
	t.Helper()
	c := mc.Consumer{Kind: mc.KnowledgeConsumer, ProjectID: v.project.ID, Purpose: purpose, OperationID: newID[struct{}](t).String()}
	if purpose == mc.MemoryEmbedding {
		c.Kind = mc.MemoryConsumer
		agent := newID[id.Agent](t)
		c.AgentID = &agent
	}
	r := mc.ResolveRequest{Actor: v.owner, Consumer: c, Purpose: purpose, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.EmbeddingSelector}, LeaseOwner: resolutionCallOwner(t)}
	v.bindFacts(t, r)
	return r
}

// A replacement fixture input is freshly related to the canonical subject;
// callers never supply its digest to the Resolver as an authority shortcut.
func (v *platformEmbeddingResolutionFixture) bindFacts(t *testing.T, r mc.ResolveRequest) {
	t.Helper()
	if r.Validate() != nil {
		t.Fatal("invalid canonical embedding request")
	}
	subject, input := newID[struct{}](t).String(), newID[struct{}](t).String()
	var agent any
	if r.Consumer.AgentID != nil {
		agent = r.Consumer.AgentID.String()
		v.sql(t, `INSERT INTO platform_embedding_fixture.agents(id,project_id,phase,version)VALUES($1,$2,'active',1) ON CONFLICT(id) DO NOTHING`, agent, r.Consumer.ProjectID.String())
	}
	v.sql(t, `INSERT INTO platform_embedding_fixture.subjects(id,project_id,kind,agent_id,phase,version)VALUES($1,$2,$3,$4,'active',1)`, subject, r.Consumer.ProjectID.String(), string(r.Consumer.Kind), agent)
	digest := resolutionDigest(struct{ Subject, Input string }{subject, input}).String()
	v.sql(t, `INSERT INTO platform_embedding_fixture.inputs(id,subject_id,digest,schema_version,phase,version)VALUES($1,$2,$3,1,'active',1)`, input, subject, digest)
	v.sql(t, `INSERT INTO platform_embedding_fixture.operations(id,subject_id,call_id,purpose,initiator,build_target,input_id,input_digest,input_schema,input_version,phase,version)VALUES($1,$2,$3,$4,$5,'new_index',$6,$7,1,1,'pending',1) ON CONFLICT(id) DO UPDATE SET subject_id=excluded.subject_id,call_id=excluded.call_id,purpose=excluded.purpose,initiator=excluded.initiator,build_target=excluded.build_target,input_id=excluded.input_id,input_digest=excluded.input_digest,input_schema=excluded.input_schema,input_version=excluded.input_version,phase='pending',version=platform_embedding_fixture.operations.version+1`, r.Consumer.OperationID, subject, r.LeaseOwner.Details().ID, string(r.Purpose), r.Actor.Details().UserID, input, digest)
}

func (v *platformEmbeddingResolutionFixture) embeddingConfig(t *testing.T, credential bool) (mc.ProviderView, mc.ModelView) {
	t.Helper()
	var ref *sc.CredentialRef
	if credential {
		metadata := v.credential(t, sc.Model)
		ref = &metadata.CredentialRef
	}
	p := v.provider(t, mc.OpenAIEmbeddings, ref)
	input := mc.ModelInput{Name: "embedding current target", ProviderModelID: "explicit-embedding", Type: mc.EmbeddingModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}}}
	receipt, e := v.service.CreateModel(testContext(t), mc.CreateModelRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ProviderID: p.ID, Input: input})
	if e != nil {
		t.Fatal(e)
	}
	mid, e := f.ParseID[mc.Model](receipt.ResourceID)
	if e != nil {
		t.Fatal(e)
	}
	m, e := v.service.GetModel(testContext(t), v.admin, mid)
	if e != nil {
		t.Fatal(e)
	}
	return p, m
}

func (v *platformEmbeddingResolutionFixture) choose(t *testing.T, mid mc.ModelID) mc.CommandReceipt {
	t.Helper()
	state, e := v.service.GetPlatformSelection(testContext(t), v.admin)
	if e != nil {
		t.Fatal(e)
	}
	selection := mc.PlatformSelection{ID: state.ID, Version: state.Version, Embedding: mid}
	if state.Configured == nil {
		selection.Memory = v.resolutionModel(t, v.provider(t, mc.OpenAIChat, nil), false).ID
	} else {
		selection = state.Configured.Clone()
		selection.Embedding = mid
	}
	receipt, e := v.service.UpdatePlatformSelection(testContext(t), mc.UpdatePlatformSelectionRequest{CommandMeta: v.meta(t, newID[struct{}](t).String()), ExpectedVersion: state.Version, Selection: selection})
	if e != nil {
		t.Fatal(e)
	}
	return receipt
}

func (v *platformEmbeddingResolutionFixture) final(t *testing.T, r mc.ResolveRequest, p mc.ResolutionPlan, capture, fail bool) (mc.ResolvedModel, f.CommitResult) {
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
			if _, e = x.Exec(ctx, `INSERT INTO platform_embedding_fixture.accepted(operation_id,input_id,digest,schema_version,input_version,snapshot_id) SELECT id,input_id,input_digest,input_schema,input_version,$2 FROM platform_embedding_fixture.operations WHERE id=$1`, r.Consumer.OperationID, p.Details().SnapshotID.String()); e != nil {
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

func (v *platformEmbeddingResolutionFixture) atomicFacts(t *testing.T, r mc.ResolveRequest, p mc.ResolutionPlan, want int) {
	t.Helper()
	var accepted, snapshot, binding, lease int
	var leaseID any
	if p.Details().LeaseID != nil {
		leaseID = p.Details().LeaseID.String()
	}
	e := v.raw.QueryRow(testContext(t), `SELECT
 (SELECT count(*) FROM platform_embedding_fixture.accepted a JOIN platform_embedding_fixture.operations o ON o.id=a.operation_id WHERE a.operation_id=$1 AND a.input_id=o.input_id AND a.digest=o.input_digest AND a.schema_version=o.input_schema AND a.input_version=o.input_version AND a.snapshot_id=$2),
 (SELECT count(*) FROM agenteam_model.snapshots WHERE id=$2),
 (SELECT count(*) FROM agenteam_model.snapshot_bindings WHERE snapshot_id=$2),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE id=$3::uuid)`, r.Consumer.OperationID, p.Details().SnapshotID.String(), leaseID).Scan(&accepted, &snapshot, &binding, &lease)
	wantLease := want
	if leaseID == nil {
		wantLease = 0
	}
	if e != nil || accepted != want || snapshot != want || binding != want || lease != wantLease {
		t.Fatalf("canonical input/snapshot/binding/lease=%d/%d/%d/%d want=%d lease=%d: %v", accepted, snapshot, binding, lease, want, wantLease, e)
	}
	var total int
	if e = v.raw.QueryRow(testContext(t), `SELECT count(*) FROM platform_embedding_fixture.accepted WHERE operation_id=$1`, r.Consumer.OperationID).Scan(&total); e != nil || total != want {
		t.Fatal("unexpected unmatched acceptance row", total, e)
	}
}

func (v *platformEmbeddingResolutionFixture) noSecretRead(t *testing.T) {
	t.Helper()
	var count int
	if e := v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='secret.resolve' AND metadata->>'consumer'='model'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("resolution read material", count, e)
	}
}

// Hold exactly the named lock with a real transaction. Cleanup always receives
// the actual completion; neither a timeout nor cancellation is called a join.
func (v *platformEmbeddingResolutionFixture) hold(t *testing.T, lock f.LockRequest) (int, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(testContext(t), 5*time.Second)
	held := make(chan int, 1)
	release := make(chan struct{})
	done := make(chan f.CommitResult, 1)
	cause := recoveryCause(t)
	go func() {
		done <- v.raw.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
			if e := v.raw.AcquireAll(ctx, tx, []f.LockRequest{lock}); e != nil {
				return e
			}
			x, e := v.raw.InTx(tx)
			if e != nil {
				return e
			}
			var pid int
			if e = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				return e
			}
			held <- pid
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var once sync.Once
	finish := func() {
		once.Do(func() {
			close(release)
			result := <-done
			cancel()
			if result.State() != f.Committed {
				t.Error("holder actual completion", result.Fault())
			}
		})
	}
	t.Cleanup(finish)
	select {
	case pid := <-held:
		return pid, finish
	case <-ctx.Done():
		t.Fatal("holder did not acquire", ctx.Err())
	}
	return 0, finish
}

type platformEmbeddingPending struct {
	done   chan error
	result error
	joined bool
	cancel context.CancelFunc
}

func platformEmbeddingStart(t *testing.T, call func(context.Context) error) *platformEmbeddingPending {
	t.Helper()
	ctx, cancel := context.WithTimeout(testContext(t), 5*time.Second)
	p := &platformEmbeddingPending{done: make(chan error, 1), cancel: cancel}
	go func() { p.done <- call(ctx) }()
	t.Cleanup(func() {
		cancel()
		if !p.joined {
			p.result = <-p.done
			p.joined = true
		}
	})
	return p
}

func (p *platformEmbeddingPending) wait() error {
	if !p.joined {
		p.result = <-p.done
		p.joined = true
	}
	p.cancel()
	return p.result
}

func (v *platformEmbeddingResolutionFixture) blocked(t *testing.T, holder int, p *platformEmbeddingPending) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(testContext(t), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var pid int
		e := v.raw.QueryRow(ctx, `SELECT coalesce(min(w.pid),0) FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid WHERE w.locktype='advisory' AND NOT w.granted AND h.granted AND h.pid=$1 AND $1=ANY(pg_blocking_pids(w.pid))`, holder).Scan(&pid)
		if e != nil {
			t.Fatal("PG holder/waiter observation", e)
		}
		if pid != 0 {
			t.Logf("actual advisory holder=%d waiter=%d", holder, pid)
			return pid
		}
		select {
		case e := <-p.done:
			p.result, p.joined = e, true
			t.Fatal("waiter completed before lock observation", e)
		case <-ctx.Done():
			t.Fatal("no actual blocked PG waiter", ctx.Err())
		case <-ticker.C:
		}
	}
}
