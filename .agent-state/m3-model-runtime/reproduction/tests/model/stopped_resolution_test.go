//go:build integration

package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
)

// Only domain ownership is a controlled fixture. The two bound ProcessGuards,
// original resolution/lease, SQL locks, observer and retirement are production.
type stoppedResolutionConsumer struct {
	*textRuntimeConsumer
	old    *object.ProcessGuard
	issuer mc.PlanIssuer
}
type stoppedResolutionFixtureKey struct{}
type stoppedResolutionFixtureOwner struct {
	consumer *stoppedResolutionConsumer
	binding  f.Digest
}

func (c *stoppedResolutionConsumer) ValidateInTx(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, p mc.ConsumerDependencies) error {
	if err := c.textRuntimeConsumer.ValidateInTx(ctx, tx, r, p); err != nil {
		return err
	}
	if r.Action != mc.ResolveConsumer {
		return nil
	}
	x, err := c.store.InTx(tx)
	if err != nil {
		return err
	}
	var process string
	if err = x.QueryRow(ctx, `SELECT process_id::text FROM model_runtime_fixture.stopped_resolutions WHERE call_id=$1`, r.CallID.String()).Scan(&process); err != nil {
		return err
	}
	old, err := f.ParseID[oc.Process](process)
	if err != nil {
		return err
	}
	return c.old.RequireCurrentProcessInTx(ctx, tx, old)
}

func (c *stoppedResolutionConsumer) proof(ctx context.Context, r mc.StoppedResolutionRequest) error {
	w, ok := ctx.Value(stoppedResolutionFixtureKey{}).(*stoppedResolutionFixtureOwner)
	binding, err := mc.StoppedResolutionBinding(r)
	if !ok || w == nil || w.consumer != c || err != nil || w.binding != binding {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (c *stoppedResolutionConsumer) stoppedFacts(ctx context.Context, x postgres.SQLExecutor, r mc.StoppedResolutionRequest) (textRuntimeFacts, error) {
	v, err := c.facts(ctx, x, r.Resolve.Consumer.OperationID)
	if err != nil {
		return v, err
	}
	var process string
	if err = x.QueryRow(ctx, `SELECT process_id::text FROM model_runtime_fixture.stopped_resolutions WHERE call_id=$1`, r.CallID.String()).Scan(&process); err != nil {
		return v, err
	}
	var actual, expected any
	if json.Unmarshal(v.Resolve, &actual) != nil || json.Unmarshal(resolutionStable(r.Resolve), &expected) != nil || !reflect.DeepEqual(actual, expected) || process != r.PreparedProcessID.String() || v.Call != r.CallID.String() || v.Project != r.Resolve.Consumer.ProjectID.String() || v.Input != r.Input {
		return v, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return v, nil
}
func (c *stoppedResolutionConsumer) DiscoverStoppedResolution(ctx context.Context, r mc.StoppedResolutionRequest) (mc.ConsumerDependencies, error) {
	if err := c.proof(ctx, r); err != nil {
		return mc.ConsumerDependencies{}, err
	}
	v, err := c.stoppedFacts(ctx, c.store, r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, _ := mc.StoppedResolutionBinding(r)
	return mc.NewConsumerDependencies(c.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: resolutionDigest(v), Locks: textRuntimeLocks(v)})
}
func (c *stoppedResolutionConsumer) ValidateStoppedResolutionInTx(ctx context.Context, tx f.Tx, r mc.StoppedResolutionRequest, p mc.ConsumerDependencies) error {
	if err := c.proof(ctx, r); err != nil {
		return err
	}
	x, err := c.store.InTx(tx)
	if err != nil {
		return err
	}
	v, err := c.stoppedFacts(ctx, x, r)
	if err != nil {
		return err
	}
	binding, _ := mc.StoppedResolutionBinding(r)
	if !p.Matches(c.issuer, binding, resolutionDigest(v)) {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	if err = c.store.RequireHeldLocks(ctx, tx, textRuntimeLocks(v)); err != nil {
		return err
	}
	process, err := c.guard.CurrentProcess()
	if err != nil {
		return err
	}
	return c.guard.RequireCurrentProcessInTx(ctx, tx, process)
}
func (*stoppedResolutionConsumer) DiscoverStoppedCall(context.Context, mc.StoppedCallObservationRequest) (mc.ConsumerDependencies, error) {
	return mc.ConsumerDependencies{}, f.NewFault(f.Forbidden, f.NotStarted)
}
func (*stoppedResolutionConsumer) ValidateStoppedCallInTx(context.Context, f.Tx, mc.StoppedCallObservationRequest, mc.ConsumerDependencies) error {
	return f.NewFault(f.Forbidden, f.NotStarted)
}

func stoppedResolutionContext(t *testing.T, c *stoppedResolutionConsumer, r mc.StoppedResolutionRequest) context.Context {
	t.Helper()
	binding, err := mc.StoppedResolutionBinding(r)
	requireTextRuntime(t, err)
	return context.WithValue(testContext(t), stoppedResolutionFixtureKey{}, &stoppedResolutionFixtureOwner{c, binding})
}

// A Spool has one exclusive owner, so the two real guards must be sequential.
// Reuse the exact database/bucket/spool identity only after every original
// borrower and the original Spool have actually joined and closed.
func restartStoppedResolutionRuntime(t *testing.T, v *textRuntimeFixture, consumer *stoppedResolutionConsumer) {
	t.Helper()
	old := v.guard
	v.close(t)
	if t.Failed() {
		t.Fatal("original runtime graph did not close before restart")
	}
	if _, err := old.CurrentProcess(); err == nil {
		t.Fatal("original guard still current after actual graph join")
	}
	v.core, v.usageOwner, v.ledger, v.retirement = nil, nil, nil, nil
	v.objects, v.guard, v.backend, v.spool = nil, nil, nil, nil
	values, err := objectfixture.Environment(testContext(t), v.base.db.Name)
	requireTextRuntime(t, err)
	env := map[string]string{}
	for _, item := range values {
		key, value, _ := strings.Cut(item, "=")
		env[key] = value
	}
	lookup := func(key string) (string, bool) { value, ok := env[key]; return value, ok }
	config, err := object.LoadStorageConfig(lookup)
	requireTextRuntime(t, err)
	v.backend, err = object.NewBackend(config)
	requireTextRuntime(t, err)
	process := newID[oc.Process](t)
	v.spool, err = object.OpenSpool(env[object.EnvironmentPrefix+"SPOOL_DIR"], process)
	requireTextRuntime(t, err)
	v.guard, err = object.OpenProcessGuard(v.spool, process)
	requireTextRuntime(t, err)
	objects, err := object.New(v.base.raw, v.backend, v.spool, v.base.aud, object.Authorizations{Processes: v.guard})
	requireTextRuntime(t, err)
	endpoint, err := object.LoadTransferEndpoint(lookup, config)
	requireTextRuntime(t, err)
	transfers, err := object.NewTransferService(objects, nil, endpoint)
	requireTextRuntime(t, err)
	v.objects, err = object.NewRuntime(objects, v.guard, transfers)
	requireTextRuntime(t, err)
	requireTextRuntime(t, v.objects.Initialize(testContext(t)))
	consumer.guard = v.guard
	modelActor, _ := id.RegisterService(id.ModelRuntime)
	secretActor, _ := id.RegisterService(id.SecretService)
	outboundActor, _ := id.RegisterService(id.OutboundService)
	authority, err := model.NewRuntimeAuthority(v.base.raw, model.RuntimeAuthorizations{Consumers: consumer, StoppedConsumers: consumer, Process: v.guard, ModelRuntime: modelActor, SecretService: secretActor, OutboundService: outboundActor})
	requireTextRuntime(t, err)
	router, err := model.NewRuntimeSecretUsageRouter(v.base.authority, authority, v.base.accounts)
	requireTextRuntime(t, err)
	projects, err := project.NewSecretAuthority(v.base.projects)
	requireTextRuntime(t, err)
	_, cursors, keys := testKeys(t)
	v.base.secrets, err = secret.New(v.base.raw, keys, v.base.aud, secret.Authorizations{Sessions: v.base.accounts, System: v.base.accounts, Projects: projects, Usage: router, AccountWrites: v.base.accounts})
	requireTextRuntime(t, err)
	requireTextRuntime(t, v.base.secrets.Initialize(testContext(t)))
	usageAuthority, err := usage.NewAuthority(v.base.raw, usage.Authorizations{Sessions: v.base.accounts, Projects: v.base.projects, Invocations: authority})
	requireTextRuntime(t, err)
	manifest, err := pc.NewRequiredManifest(projectConfigurationEntries(1, false))
	requireTextRuntime(t, err)
	lifecycle, err := project.NewLifecycleAuthority(v.base.raw, manifest, nil)
	requireTextRuntime(t, err)
	usageStop, err := project.NewUsageStopAuthority(lifecycle, v.base.raw)
	requireTextRuntime(t, err)
	v.usageOwner, err = usage.NewProjectCallOwnership(v.base.raw, usageAuthority, usage.Dependencies{Cursors: cursors}, v.guard, process, usageStop)
	requireTextRuntime(t, err)
	v.ledger = v.usageOwner.Service()
	requireTextRuntime(t, v.ledger.Initialize(testContext(t)))
	v.wire.budget = wire.NewBudget()
	v.wire.adapter = v.wire.newAdapter(t, v.wire.budget)
	v.reader = &textRuntimeReadTap{CredentialUsageReader: v.base.secrets}
	v.core, err = model.NewRuntime(v.base.raw, authority, model.RuntimeDependencies{Usage: v.ledger, SecretReader: v.reader, SecretUsage: v.base.secrets, Adapter: v.wire.adapter})
	requireTextRuntime(t, err)
	requireTextRuntime(t, v.core.Initialize(testContext(t)))
	v.retirement, err = model.NewModelCallLeaseRetirement(authority, v.base.secrets)
	requireTextRuntime(t, err)
}

func seedStoppedResolution(t *testing.T, v *textRuntimeFixture, process oc.ProcessID) mc.StoppedResolutionRequest {
	t.Helper()
	call := newID[mc.Call](t)
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, call.String())
	requireTextRuntime(t, err)
	resolve := mc.ResolveRequest{Actor: v.base.owner, Consumer: mc.Consumer{Kind: mc.MeetingConsumer, ProjectID: v.base.project.ID, MeetingID: newID[struct{}](t).String(), OperationID: newID[struct{}](t).String(), Purpose: mc.MeetingSummaryInitial}, Purpose: mc.MeetingSummaryInitial, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.MeetingSummarySelector}, LeaseOwner: owner}
	messages := []mc.Message{{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: textRuntimeInputCanary}}}}}
	digest, err := model.TextInputDigest(messages)
	requireTextRuntime(t, err)
	input := mc.InputIdentity{OperationID: resolve.Consumer.OperationID, Digest: digest, SchemaVersion: 1}
	deadline, _ := f.NewInstant(time.Now().Add(time.Minute))
	attempts := f.Sequence(1)
	policy := mc.RetryPolicy{Class: mc.BoundedRetry, Deadline: &deadline, MaxAttempts: &attempts}
	result := v.base.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		x, err := v.base.raw.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO model_runtime_fixture.operations(id,project_id,meeting_id,call_id,initiator,messages,input_data,policy_data,resolve_data,version,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1,true)`, resolve.Consumer.OperationID, resolve.Consumer.ProjectID.String(), resolve.Consumer.MeetingID, call.String(), resolve.Actor.Details().UserID, resolutionJSON(messages), resolutionJSON(input), resolutionJSON(policy), resolutionStable(resolve))
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO model_runtime_fixture.stopped_resolutions(call_id,process_id) VALUES($1,$2)`, call.String(), process.String())
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("original domain source/process capture", result.Fault())
	}
	return mc.StoppedResolutionRequest{Resolve: resolve, CallID: call, Input: input, PreparedProcessID: process}
}

func stoppedResolutionRows(t *testing.T, v *textRuntimeFixture) []byte {
	t.Helper()
	var raw []byte
	err := v.base.raw.QueryRow(testContext(t), `SELECT jsonb_build_array(
(SELECT jsonb_agg(to_jsonb(p)||jsonb_build_object('xmin',p.xmin::text) ORDER BY id) FROM agenteam_model.resolution_preparations p),
(SELECT jsonb_agg(to_jsonb(c)||jsonb_build_object('xmin',c.xmin::text) ORDER BY id) FROM agenteam_model.calls c),
(SELECT jsonb_agg(to_jsonb(i)||jsonb_build_object('xmin',i.xmin::text) ORDER BY id) FROM agenteam_model.invocations i),
(SELECT jsonb_agg(to_jsonb(l)||jsonb_build_object('xmin',l.xmin::text) ORDER BY id) FROM agenteam_secret.secret_leases l))`).Scan(&raw)
	requireTextRuntime(t, err)
	return raw
}

func requireSameStoppedResolution(t *testing.T, actual, expected mc.ResolvedModel) {
	t.Helper()
	if actual.Validate() != nil || expected.Validate() != nil || !actual.Consumer.Equal(expected.Consumer) || !actual.LeaseOwner.Equal(expected.LeaseOwner) {
		t.Fatal("original resolution consumer or lease owner changed")
	}
	// CredentialRef and CredentialLeaseOwner contain opaque closures. Compare
	// their complete public identities through the contract, never DeepEqual.
	a, b := actual.Snapshot, expected.Snapshot
	if (a.CredentialRef == nil) != (b.CredentialRef == nil) || a.CredentialRef != nil && !a.CredentialRef.Equal(*b.CredentialRef) {
		t.Fatal("original snapshot credential identity changed")
	}
	a.CredentialRef, b.CredentialRef = nil, nil
	if !reflect.DeepEqual(a, b) {
		t.Fatal("original immutable snapshot or selection version changed")
	}
	x, y := actual.CredentialLease, expected.CredentialLease
	if (x == nil) != (y == nil) || x != nil && (x.LeaseID != y.LeaseID || !x.CredentialRef.Equal(y.CredentialRef)) {
		t.Fatal("original credential lease identity changed")
	}
}

func TestModelM3StoppedResolution(t *testing.T) {
	var consumer *stoppedResolutionConsumer
	v := newTextRuntimeFixtureWithPorts(t, func(base *textRuntimeConsumer) mc.ConsumerAuthority {
		consumer = &stoppedResolutionConsumer{textRuntimeConsumer: base, issuer: mc.NewPlanIssuer()}
		return consumer
	}, nil)
	old := v.guard
	consumer.old = old
	process, err := old.CurrentProcess()
	requireTextRuntime(t, err)
	_, err = v.base.raw.Exec(testContext(t), `CREATE TABLE model_runtime_fixture.stopped_resolutions(call_id uuid PRIMARY KEY REFERENCES model_runtime_fixture.operations(call_id),process_id uuid NOT NULL)`)
	requireTextRuntime(t, err)
	key := v.wire.scenario(t, wireJSON(wireReply("unused", "stop", json.RawMessage(`{"prompt_tokens":1}`), nil)))
	model := m3RuntimeModel(t, v, key, false)
	selection, err := v.base.service.GetMeetingSummarySelection(testContext(t), v.base.admin)
	requireTextRuntime(t, err)
	_, err = v.base.service.UpdateMeetingSummarySelection(testContext(t), mc.UpdateMeetingSummarySelectionRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), SelectionID: selection.ID, ExpectedVersion: selection.Version, Model: model})
	requireTextRuntime(t, err)
	requests := []mc.StoppedResolutionRequest{seedStoppedResolution(t, v, process), seedStoppedResolution(t, v, process), seedStoppedResolution(t, v, process)}
	_, err = v.base.service.DiscoverResolve(testContext(t), requests[1].Resolve)
	requireTextRuntime(t, err)
	resolved, err := v.base.service.ResolveModel(testContext(t), requests[2].Resolve)
	requireTextRuntime(t, err)
	before := stoppedResolutionRows(t, v)
	for _, r := range requests {
		out, err := v.core.LookupStoppedResolution(stoppedResolutionContext(t, consumer, r), r)
		requireCode(t, err, f.ResourceBusy)
		if out.Settled {
			t.Fatal("live original guard was treated as settled", err)
		}
	}
	restartStoppedResolutionRuntime(t, v, consumer)
	if _, err = old.CurrentProcess(); err == nil {
		t.Fatal("old guard still live")
	}
	for i, r := range requests {
		// Cancellation of the original domain work permits only this private
		// cleanup witness. It never enables a new ordinary Resolve/Begin.
		_, err = v.base.raw.Exec(testContext(t), `UPDATE model_runtime_fixture.operations SET enabled=false,version=version+1 WHERE id=$1`, r.Resolve.Consumer.OperationID)
		requireTextRuntime(t, err)
		out, err := v.core.LookupStoppedResolution(stoppedResolutionContext(t, consumer, r), r)
		requireTextRuntime(t, err)
		if out.Validate() != nil || !out.Settled || out.CallFound || (out.Model != nil) != (i == 2) {
			t.Fatal("wrong stopped resolution state", i)
		}
		if i == 2 {
			requireSameStoppedResolution(t, *out.Model, resolved)
		}
		if _, err = v.core.LookupStoppedResolution(testContext(t), r); err == nil {
			t.Fatal("bare stopped request authorized")
		}
		changed := r.Clone()
		version := f.Version(1)
		changed.Resolve.Selection.Version = &version
		if _, err = v.core.LookupStoppedResolution(stoppedResolutionContext(t, consumer, changed), changed); err == nil {
			t.Fatal("guessed selector version replaced original nil")
		}
	}
	if !bytes.Equal(before, stoppedResolutionRows(t, v)) || len(v.wire.state(t, key).Requests) != 0 {
		t.Fatal("read-only observation changed Model/Usage/Secret facts or dispatched")
	}
	_, err = v.base.raw.Exec(testContext(t), `UPDATE model_runtime_fixture.operations SET snapshot_id=$2,lease_id=$3 WHERE id=$1`, requests[2].Resolve.Consumer.OperationID, resolved.Snapshot.ID.String(), resolved.CredentialLease.LeaseID.String())
	requireTextRuntime(t, err)
	retirement := mc.ModelCallRetirementRequest{Actor: v.base.owner, Model: resolved, TerminalVersion: 2}
	plan, err := v.retirement.DiscoverModelCallRetirement(testContext(t), retirement)
	requireTextRuntime(t, err)
	result := v.base.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		return v.retirement.ReleaseModelCallInTx(ctx, tx, retirement, plan)
	})
	if result.State() != f.Committed {
		t.Fatal("original returned lease did not retire", result.Fault())
	}
	var released bool
	result = v.base.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		released, err = v.retirement.ModelCallRetiredInTx(ctx, tx, retirement, plan)
		return err
	})
	if result.State() != f.Committed || !released || len(v.wire.state(t, key).Requests) != 0 {
		t.Fatal("original lease retirement not confirmed without dispatch", result.Fault())
	}
}
