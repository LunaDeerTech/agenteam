//go:build integration

package model_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

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

// Only missing consumer business tables are isolated. A real Account Session
// and Project Owner/gate check is composed with exact durable execution/input
// facts, never used as a substitute for those facts or a production consumer.
type currentResolutionFixture struct {
	*projectConfigurationFixture
	resolving           *model.Service
	consumers           *currentResolutionConsumer
	resolutionAuthority *model.Authority
	resolutionSecrets   *secret.Service
	router              *model.SecretUsageRouter
	tap                 *currentResolutionUsage
	observed            *currentResolutionStore
}
type currentResolutionConsumer struct {
	store     model.Store
	base      *projectConfigurationFixture
	issuer    mc.PlanIssuer
	validated atomic.Int64
}

func resolutionJSON(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return b
}
func resolutionDigest(v any) f.Digest {
	h := sha256.Sum256(resolutionJSON(v))
	return f.Digest("sha256:" + hex.EncodeToString(h[:]))
}
func resolutionStable(r mc.ResolveRequest) []byte {
	a := r.Actor.Details()
	if a.Kind == id.Human {
		a.SessionID = ""
	}
	return resolutionJSON(struct {
		Actor   id.ActorDetails
		Request mc.ResolveRequest
	}{a, r})
}
func resolutionUnit(r mc.ResolveRequest) string {
	return r.LeaseOwner.Details().ID + ":" + string(r.Purpose) + ":" + r.Consumer.OperationID
}
func resolutionLocks(r mc.ResolveRequest, owner id.Actor) []f.LockRequest {
	ls := []f.LockRequest{}
	add := func(k f.LockKey, e error) {
		if e != nil {
			panic(e)
		}
		ls = append(ls, f.LockRequest{Key: k, Mode: f.Shared})
	}
	add(f.UserLock(owner.Details().UserID))
	if r.Actor.Details().Kind == id.Human {
		add(f.UserLock(r.Actor.Details().UserID))
	}
	add(f.ProjectLock(r.Consumer.ProjectID.String()))
	if r.Consumer.AgentID != nil {
		add(f.AgentLock(r.Consumer.AgentID.String()))
	}
	if r.Consumer.ExecutionID != nil {
		add(f.AggregateLock(f.ExecutionAggregate, r.Consumer.ExecutionID.String()))
	}
	if r.Consumer.OperationID != "" {
		add(f.AggregateLock(f.OperationAggregate, r.Consumer.OperationID))
	}
	key, e := f.RecordLock(f.ReferenceRecordLock, "model-resolution-fixture:"+resolutionDigest(resolutionUnit(r)).String())
	if e != nil {
		panic(e)
	}
	ls = append(ls, f.LockRequest{Key: key, Mode: f.Exclusive})
	sort.Slice(ls, func(i, j int) bool { return f.CompareLockKeys(ls[i].Key, ls[j].Key) < 0 })
	return ls
}
func (c *currentResolutionConsumer) facts(ctx context.Context, x postgres.SQLExecutor, r mc.ResolveRequest) ([]byte, int64, bool, error) {
	var data []byte
	var version int64
	var enabled bool
	e := x.QueryRow(ctx, `SELECT request_data,version,enabled FROM model_resolution_fixture.consumers WHERE unit=$1`, resolutionUnit(r)).Scan(&data, &version, &enabled)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, 0, false, nil
	}
	return data, version, enabled, e
}
func (c *currentResolutionConsumer) Discover(ctx context.Context, r mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	if r.Validate() != nil || r.Action != mc.ResolveConsumer {
		return mc.ConsumerDependencies{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	data, v, on, e := c.facts(ctx, c.store, *r.Resolve)
	if e != nil {
		return mc.ConsumerDependencies{}, e
	}
	b, _ := mc.ConsumerBinding(r)
	mapping := resolutionDigest(struct {
		Data    json.RawMessage
		Version int64
		Enabled bool
	}{data, v, on})
	return mc.NewConsumerDependencies(c.issuer, mc.ConsumerDependencyDetails{Binding: b, Mapping: mapping, Locks: resolutionLocks(*r.Resolve, c.base.owner)})
}
func (c *currentResolutionConsumer) ValidateInTx(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, p mc.ConsumerDependencies) error {
	if r.Validate() != nil || r.Action != mc.ResolveConsumer {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	x, e := c.store.InTx(tx)
	if e != nil {
		return e
	}
	if e = c.store.RequireHeldLocks(ctx, tx, resolutionLocks(*r.Resolve, c.base.owner)); e != nil {
		return e
	}
	if r.Actor.Details().Kind == id.Human {
		if e = c.base.accounts.RequireCurrentSession(ctx, tx, r.Actor); e != nil {
			return e
		}
	}
	// The true Project authority supplies current gate and Owner facts; the
	// following strict row separately supplies this fixture's consumer authority.
	if _, e = c.base.projects.AuthorizeProject(ctx, tx, c.base.owner, r.Consumer.ProjectID, id.Mutate); e != nil {
		return e
	}
	data, v, on, e := c.facts(ctx, x, *r.Resolve)
	if e != nil {
		return e
	}
	b, _ := mc.ConsumerBinding(r)
	m := resolutionDigest(struct {
		Data    json.RawMessage
		Version int64
		Enabled bool
	}{data, v, on})
	var canonical any
	if len(data) > 0 && json.Unmarshal(data, &canonical) != nil {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	var expected any
	_ = json.Unmarshal(resolutionStable(*r.Resolve), &expected)
	if !on || !bytes.Equal(resolutionJSON(canonical), resolutionJSON(expected)) || !p.Matches(c.issuer, b, m) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	c.validated.Add(1)
	return nil
}

type currentResolutionUsage struct {
	sc.UsageOperations
	calls   atomic.Int64
	mu      sync.Mutex
	ctx     context.Context
	tx      f.Tx
	request sc.UsageRequest
	plan    sc.UsageDependencies
	fail    bool
}

func (s *currentResolutionUsage) ApplyUsageInTx(ctx context.Context, tx f.Tx, r sc.UsageRequest, p sc.UsageDependencies) (sc.UsageResult, error) {
	s.calls.Add(1)
	s.mu.Lock()
	s.ctx, s.tx, s.request, s.plan = ctx, tx, r, p
	fail := s.fail
	s.mu.Unlock()
	if fail {
		return sc.UsageResult{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	if r.Action != sc.AcquireLeaseUsage || r.Purpose != sc.Model {
		return sc.UsageResult{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return s.UsageOperations.ApplyUsageInTx(ctx, tx, r, p)
}

// Decoration reports an explicit API result only AFTER the real Store returns.
// It does not simulate a wire ACK loss or an unconfirmed backend rollback.
type currentResolutionStore struct {
	sharedStore
	mu                   sync.Mutex
	target, seen         int
	rollback             bool
	after                func()
	actual, returned     f.CommitResult
	marker               f.ID[f.TransactionAttempt]
	unions               [][]f.LockRequest
	afterPreparationRead func()
}

func (s *currentResolutionStore) AcquireAll(ctx context.Context, tx f.Tx, l []f.LockRequest) error {
	s.mu.Lock()
	s.unions = append(s.unions, append([]f.LockRequest(nil), l...))
	s.mu.Unlock()
	return s.sharedStore.AcquireAll(ctx, tx, l)
}
func (s *currentResolutionStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	active := cause.Kind() == f.CommandsCause && cause.Details().Primary.Namespace() == "model.resolve"
	hit := false
	if active {
		s.seen++
		hit = s.target > 0 && s.seen == s.target
	}
	rollback := s.rollback
	after := s.after
	marker := s.marker
	s.mu.Unlock()
	actual := s.sharedStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		e := fn(ctx, tx)
		if e == nil && hit && rollback {
			return f.NewFault(f.DependencyUnavailable, f.NotStarted)
		}
		return e
	})
	if hit && actual.State() == f.Committed && after != nil {
		after()
	}
	if hit {
		s.mu.Lock()
		s.actual = actual
		if marker.Validate() == nil {
			s.returned = f.UnknownResult(marker, cause)
		} else {
			s.returned = actual
		}
		returned := s.returned
		s.mu.Unlock()
		return returned
	}
	return actual
}
func newCurrentResolutionFixture(t *testing.T) *currentResolutionFixture {
	t.Helper()
	base := newProjectConfigurationFixture(t)
	_, e := base.raw.Exec(testContext(t), `CREATE SCHEMA model_resolution_fixture; CREATE TABLE model_resolution_fixture.consumers(unit text PRIMARY KEY,request_data jsonb NOT NULL,version bigint NOT NULL,enabled boolean NOT NULL); CREATE TABLE model_resolution_fixture.inputs(unit text PRIMARY KEY,snapshot_id uuid NOT NULL)`)
	if e != nil {
		t.Fatal(e)
	}
	return bindCurrentResolution(t, base)
}
func bindCurrentResolution(t *testing.T, base *projectConfigurationFixture) *currentResolutionFixture {
	t.Helper()
	_, _, sk := testKeys(t)
	store := &currentResolutionStore{sharedStore: base.store}
	consumers := &currentResolutionConsumer{store: store, base: base, issuer: mc.NewPlanIssuer()}
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
	return &currentResolutionFixture{projectConfigurationFixture: base, resolving: svc, consumers: consumers, resolutionAuthority: authority, resolutionSecrets: secrets, router: router, tap: tap, observed: store}
}
func (v *currentResolutionFixture) bind(t *testing.T, r mc.ResolveRequest) {
	t.Helper()
	if r.Validate() != nil {
		t.Fatal("invalid fixture ResolveRequest")
	}
	_, e := v.raw.Exec(testContext(t), `INSERT INTO model_resolution_fixture.consumers(unit,request_data,version,enabled)VALUES($1,$2,1,true) ON CONFLICT(unit) DO UPDATE SET request_data=excluded.request_data,version=model_resolution_fixture.consumers.version+1,enabled=true`, resolutionUnit(r), resolutionStable(r))
	if e != nil {
		t.Fatal(e)
	}
}
func (v *currentResolutionFixture) request(t *testing.T, m mc.ModelView, human bool) mc.ResolveRequest {
	t.Helper()
	agent := newID[id.Agent](t)
	execution := newID[id.Execution](t)
	actor, e := id.NewAgentRun(v.project.ID, agent, execution)
	if e != nil {
		t.Fatal(e)
	}
	if human {
		actor = v.owner
	}
	owner, _ := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, execution.String())
	r := mc.ResolveRequest{Actor: actor, Consumer: mc.Consumer{Kind: mc.AgentConsumer, ProjectID: v.project.ID, AgentID: &agent, ExecutionID: &execution, Purpose: mc.AgentGeneration}, Purpose: mc.AgentGeneration, Source: mc.CurrentSelectionSource, ModelRef: &m.ID, Selection: &mc.SelectionRef{Kind: "direct"}, LeaseOwner: owner}
	v.bind(t, r)
	return r
}
func (v *currentResolutionFixture) memory(t *testing.T, m mc.ModelView) mc.ResolveRequest {
	t.Helper()
	actor := v.owner
	agent := newID[id.Agent](t)
	owner, _ := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, newID[mc.Call](t).String())
	r := mc.ResolveRequest{Actor: actor, Consumer: mc.Consumer{Kind: mc.MemoryConsumer, ProjectID: v.project.ID, AgentID: &agent, OperationID: newID[struct{}](t).String(), Purpose: mc.MemoryExtraction}, Purpose: mc.MemoryExtraction, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.MemorySelector}, LeaseOwner: owner}
	v.bind(t, r)
	return r
}
func (v *currentResolutionFixture) config(t *testing.T, projectScope, credential bool) (mc.ProviderView, mc.ModelView, *sc.Metadata) {
	t.Helper()
	var metadata *sc.Metadata
	var ref *sc.CredentialRef
	if credential {
		var m sc.Metadata
		if projectScope {
			m = v.projectCredential(t, sc.Model)
		} else {
			m = v.credential(t, sc.Model)
		}
		metadata = &m
		ref = &m.CredentialRef
	}
	if projectScope {
		p := v.projectProvider(t, ref)
		return p, v.resolutionModel(t, p, true), metadata
	}
	p := v.provider(t, mc.OpenAIChat, ref)
	return p, v.resolutionModel(t, p, false), metadata
}
func (v *currentResolutionFixture) discover(t *testing.T, r mc.ResolveRequest) mc.ResolutionPlan {
	t.Helper()
	p, e := v.resolving.DiscoverResolve(testContext(t), r)
	if e != nil || p.Validate() != nil {
		t.Fatal("discover resolution", e)
	}
	return p
}
func (v *currentResolutionFixture) resolve(t *testing.T, r mc.ResolveRequest) mc.ResolvedModel {
	t.Helper()
	out, e := v.resolving.ResolveModel(testContext(t), r)
	if e != nil || out.Validate() != nil {
		t.Fatal("resolve current", e)
	}
	return out
}
func (v *currentResolutionFixture) count(t *testing.T, table string) int {
	t.Helper()
	allowed := map[string]bool{"resolution_preparations": true, "snapshots": true, "snapshot_bindings": true}
	if !allowed[table] {
		t.Fatal("invalid table")
	}
	var n int
	if e := v.raw.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_model.`+table).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func (v *currentResolutionFixture) final(t *testing.T, r mc.ResolveRequest, p mc.ResolutionPlan, capture, fail bool) (mc.ResolvedModel, f.CommitResult) {
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
			if _, e = x.Exec(ctx, `INSERT INTO model_resolution_fixture.inputs(unit,snapshot_id)VALUES($1,$2)`, resolutionUnit(r), p.Details().SnapshotID.String()); e != nil {
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
func resolutionNoResult(t *testing.T, out mc.ResolvedModel, e error) {
	t.Helper()
	if e == nil || out.Snapshot.ID.Validate() == nil || out.CredentialLease != nil {
		t.Fatal("unconfirmed resolution published")
	}
}
func resolutionZeroPlan(t *testing.T, p mc.ResolutionPlan, e error) {
	t.Helper()
	if e == nil || p.Validate() == nil {
		t.Fatal("unconfirmed preparation published")
	}
}

type resolutionObservedRow struct {
	postgres.Row
	after func()
}

func (r resolutionObservedRow) Scan(dest ...any) error {
	e := r.Row.Scan(dest...)
	if r.after != nil {
		r.after()
	}
	return e
}
func (s *currentResolutionStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	var after func()
	s.mu.Lock()
	if strings.HasPrefix(q, "SELECT id::text,resolution_identity,") {
		after = s.afterPreparationRead
		s.afterPreparationRead = nil
	}
	s.mu.Unlock()
	return resolutionObservedRow{s.sharedStore.QueryRow(ctx, q, args...), after}
}

func (v *currentResolutionFixture) resolutionModel(t *testing.T, p mc.ProviderView, projectScope bool) mc.ModelView {
	t.Helper()
	input := projectModelInput()
	input.Capabilities.InputModalities = []string{"text"}
	input.Capabilities.OutputModalities = []string{"text"}
	meta := v.meta(t, newID[struct{}](t).String())
	if projectScope {
		meta = v.projectMeta(t, newID[struct{}](t).String())
	}
	receipt, e := v.service.CreateModel(testContext(t), mc.CreateModelRequest{CommandMeta: meta, ProviderID: p.ID, Input: input})
	if e != nil {
		t.Fatal(e)
	}
	mid, e := f.ParseID[mc.Model](receipt.ResourceID)
	if e != nil {
		t.Fatal(e)
	}
	var out mc.ModelView
	if projectScope {
		out, e = v.service.GetProjectModel(testContext(t), v.owner, v.project.ID, mid)
	} else {
		out, e = v.service.GetModel(testContext(t), v.admin, mid)
	}
	if e != nil {
		t.Fatal(e)
	}
	return out
}
