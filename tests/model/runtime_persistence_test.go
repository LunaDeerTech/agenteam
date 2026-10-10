//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// This fixture owns only missing consumer operation/input facts. It is not a
// production Meeting, Agent, F1 or authorization implementation. Runtime calls,
// attempts, Usage, Secret leases and wire outcomes are never inserted by it.
// Real Account/Project authorities gate every human operation; technical tails
// must match the original accepted Runtime facts in the same Store/Tx.
type textRuntimeConsumer struct {
	store    *postgres.Store
	accounts *account.Authority
	projects *project.Authority
	guard    *object.ProcessGuard
	issuer   mc.PlanIssuer
}

type textRuntimeFacts struct {
	Operation, Project, Meeting, Call, Initiator string
	Messages                                     []mc.Message
	Input                                        mc.InputIdentity
	Policy                                       mc.RetryPolicy
	Resolve                                      json.RawMessage
	Snapshot, Lease                              string
	Version                                      int64
	Enabled                                      bool
}

func (c *textRuntimeConsumer) facts(ctx context.Context, x postgres.SQLExecutor, operation string) (textRuntimeFacts, error) {
	var v textRuntimeFacts
	var messages, input, policy []byte
	err := x.QueryRow(ctx, `SELECT id::text,project_id::text,meeting_id::text,call_id::text,initiator::text,messages,input_data,policy_data,resolve_data,coalesce(snapshot_id::text,''),coalesce(lease_id::text,''),version,enabled FROM model_runtime_fixture.operations WHERE id=$1`, operation).Scan(&v.Operation, &v.Project, &v.Meeting, &v.Call, &v.Initiator, &messages, &input, &policy, &v.Resolve, &v.Snapshot, &v.Lease, &v.Version, &v.Enabled)
	if err != nil {
		return textRuntimeFacts{}, f.NewFault(f.Forbidden, f.NotStarted).WithCause(err)
	}
	defer clear(messages)
	if json.Unmarshal(messages, &v.Messages) != nil || json.Unmarshal(input, &v.Input) != nil || json.Unmarshal(policy, &v.Policy) != nil {
		return textRuntimeFacts{}, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	digest, err := model.TextInputDigest(v.Messages)
	if err != nil || digest != v.Input.Digest || v.Input.OperationID != v.Operation || v.Input.SchemaVersion != 1 || v.Policy.Validate() != nil || v.Version < 1 {
		return textRuntimeFacts{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return v, nil
}

func textRuntimeLocks(v textRuntimeFacts) []f.LockRequest {
	var locks []f.LockRequest
	add := func(key f.LockKey, err error) {
		if err != nil {
			panic("invalid owned fixture lock")
		}
		locks = append(locks, f.LockRequest{Key: key, Mode: f.Shared})
	}
	add(f.UserLock(v.Initiator))
	add(f.ProjectLock(v.Project))
	add(f.AggregateLock(f.MeetingAggregate, v.Meeting))
	add(f.AggregateLock(f.OperationAggregate, v.Operation))
	key, err := f.RecordLock(f.ReferenceRecordLock, "model-runtime-fixture:"+v.Operation)
	if err != nil {
		panic("invalid owned fixture record lock")
	}
	locks = append(locks, f.LockRequest{Key: key, Mode: f.Exclusive})
	sort.Slice(locks, func(i, j int) bool { return f.CompareLockKeys(locks[i].Key, locks[j].Key) < 0 })
	return locks
}

func (c *textRuntimeConsumer) Discover(ctx context.Context, r mc.ConsumerRequest) (mc.ConsumerDependencies, error) {
	if r.Validate() != nil || r.Consumer.Kind != mc.MeetingConsumer {
		return mc.ConsumerDependencies{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	v, err := c.facts(ctx, c.store, r.Consumer.OperationID)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.ConsumerBinding(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(c.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: resolutionDigest(v), Locks: textRuntimeLocks(v), RetryPolicy: &v.Policy})
}

func (c *textRuntimeConsumer) ValidateInTx(ctx context.Context, tx f.Tx, r mc.ConsumerRequest, plan mc.ConsumerDependencies) error {
	deny := func() error { return f.NewFault(f.Forbidden, f.NotStarted) }
	if r.Validate() != nil || r.Consumer.Kind != mc.MeetingConsumer {
		return deny()
	}
	x, err := c.store.InTx(tx)
	if err != nil {
		return err
	}
	v, err := c.facts(ctx, x, r.Consumer.OperationID)
	if err != nil {
		return err
	}
	if err = c.store.RequireHeldLocks(ctx, tx, textRuntimeLocks(v)); err != nil {
		return err
	}
	binding, err := mc.ConsumerBinding(r)
	if err != nil || !plan.Matches(c.issuer, binding, resolutionDigest(v)) || !v.Enabled || v.Project != r.Consumer.ProjectID.String() || v.Meeting != r.Consumer.MeetingID || v.Operation != r.Consumer.OperationID || r.Consumer.Purpose != mc.MeetingSummaryInitial || r.CallID == nil || v.Call != r.CallID.String() || r.LeaseOwner.Details().Kind != sc.ModelCallOwner || r.LeaseOwner.Details().ID != v.Call {
		return deny()
	}
	technical := r.Action == mc.FinalizeConsumer || r.Action == mc.RetireConsumer
	actor := r.Actor.Details()
	if !technical {
		if actor.Kind != id.Human || actor.UserID != v.Initiator {
			return deny()
		}
		if err = c.accounts.RequireCurrentSession(ctx, tx, r.Actor); err != nil {
			return err
		}
		if _, err = c.projects.RequireOwnerInTx(ctx, tx, r.Actor, r.Consumer.ProjectID, id.Mutate); err != nil {
			return err
		}
	} else if actor.Kind != id.Service || actor.ServiceName != id.ModelRuntime || actor.ProjectID != v.Project {
		return deny()
	}
	if r.Action == mc.ResolveConsumer {
		if r.Resolve == nil {
			return deny()
		}
		var stored, expected any
		if json.Unmarshal(v.Resolve, &stored) != nil || json.Unmarshal(resolutionStable(*r.Resolve), &expected) != nil || !reflect.DeepEqual(stored, expected) {
			return deny()
		}
		return nil
	}
	if v.Snapshot != r.SnapshotID.String() || r.LeaseID == nil || v.Lease != r.LeaseID.String() {
		return deny()
	}
	if r.Action != mc.RetireConsumer && (r.Input == nil || *r.Input != v.Input) {
		return deny()
	}
	if r.Action == mc.InvokeConsumer {
		return nil
	}
	// The technical actor is not a substitute for accepted call/attempt facts.
	// These facts are read only; Runtime remains their sole producer.
	var invocation, process, phase, initiator, snapshot, lease string
	var fence int64
	var retired bool
	err = x.QueryRow(ctx, `SELECT c.invocation_id::text,c.process_id::text,c.fence,c.phase,c.retired,c.request_data#>>'{Initiator,UserID}',c.snapshot_id::text,c.lease_id::text FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id AND a.call_id=c.id WHERE c.id=$1 AND c.project_id=$2`, v.Call, v.Project).Scan(&invocation, &process, &fence, &phase, &retired, &initiator, &snapshot, &lease)
	if err != nil {
		return f.NewFault(f.Forbidden, f.NotStarted).WithCause(err)
	}
	actual, err := c.guard.CurrentProcess()
	if err != nil || actual.String() != process || fence != 1 || retired || initiator != v.Initiator || snapshot != v.Snapshot || lease != v.Lease {
		return deny()
	}
	if technical && actor.CauseRef != invocation {
		return deny()
	}
	if r.Action == mc.RetireConsumer {
		if phase != "succeeded" && phase != "failed" && phase != "cancelled" && phase != "unknown" {
			return deny()
		}
		return nil
	}
	if r.Attempt == nil || r.Attempt.InvocationID.String() != invocation || r.Attempt.ProcessID.String() != process || r.Attempt.AttemptIndex != 1 || r.Attempt.Fence != 1 {
		return deny()
	}
	return nil
}

type textRuntimeReadTap struct {
	sc.CredentialUsageReader
	mu        sync.Mutex
	materials []sc.SecretMaterial
}

func (v *textRuntimeReadTap) ReadCredentialForUsage(ctx context.Context, request sc.UsageRequest) (sc.SecretMaterial, error) {
	material, err := v.CredentialUsageReader.ReadCredentialForUsage(ctx, request)
	if err == nil {
		v.mu.Lock()
		v.materials = append(v.materials, material) // same owned cell; never copy its value
		v.mu.Unlock()
	}
	return material, err
}

func (v *textRuntimeReadTap) requireDestroyed(t *testing.T) {
	t.Helper()
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.materials) != 1 {
		t.Fatal("expected one actual committed credential read")
	}
	for _, material := range v.materials {
		called := false
		err := material.Use(func([]byte) error { called = true; return nil })
		if err == nil || called {
			t.Fatal("credential still usable after original Exchange retirement")
		}
	}
}

func (v *textRuntimeFixture) request(t *testing.T, key string) mc.ModelRequest {
	t.Helper()
	credential := v.base.credential(t, sc.Model)
	providerInput := projectProviderInput()
	providerInput.BaseURL = "https://fixture.test:8443/case/" + key
	providerInput.CredentialRef = &credential.CredentialRef
	providerReceipt, err := v.base.service.CreateProvider(testContext(t), mc.CreateProviderRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), Input: providerInput})
	if err != nil {
		t.Fatal("real credential-backed Provider create", err)
	}
	providerID, _ := f.ParseID[mc.Provider](providerReceipt.ResourceID)
	input := projectModelInput()
	input.ProviderModelID = "fixture-native-model"
	input.Capabilities = mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}, Streaming: true}
	receipt, err := v.base.service.CreateModel(testContext(t), mc.CreateModelRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), ProviderID: providerID, Input: input})
	if err != nil {
		t.Fatal("real text Model create", err)
	}
	modelID, _ := f.ParseID[mc.Model](receipt.ResourceID)
	call := newID[mc.Call](t)
	owner, err := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, call.String())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := v.base.service.GetMeetingSummarySelection(testContext(t), v.base.admin)
	if err != nil {
		t.Fatal("real platform selection read", err)
	}
	_, err = v.base.service.UpdateMeetingSummarySelection(testContext(t), mc.UpdateMeetingSummarySelectionRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), SelectionID: selection.ID, ExpectedVersion: selection.Version, Model: modelID})
	if err != nil {
		t.Fatal("real platform summary selection update", err)
	}
	resolve := mc.ResolveRequest{Actor: v.base.owner, Consumer: mc.Consumer{Kind: mc.MeetingConsumer, ProjectID: v.base.project.ID, MeetingID: newID[struct{}](t).String(), OperationID: newID[struct{}](t).String(), Purpose: mc.MeetingSummaryInitial}, Purpose: mc.MeetingSummaryInitial, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.MeetingSummarySelector}, LeaseOwner: owner}
	messages := []mc.Message{{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: textRuntimeInputCanary}}}}}
	digest, err := model.TextInputDigest(messages)
	if err != nil {
		t.Fatal(err)
	}
	identity := mc.InputIdentity{OperationID: resolve.Consumer.OperationID, Digest: digest, SchemaVersion: 1}
	deadline, _ := f.NewInstant(time.Now().Add(30 * time.Second))
	attempts := f.Sequence(1)
	policy := mc.RetryPolicy{Class: mc.BoundedRetry, Deadline: &deadline, MaxAttempts: &attempts, Categories: []mc.ErrorCategory{}}
	_, err = v.base.raw.Exec(testContext(t), `INSERT INTO model_runtime_fixture.operations(id,project_id,meeting_id,call_id,initiator,messages,input_data,policy_data,resolve_data,version,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,1,true)`, resolve.Consumer.OperationID, resolve.Consumer.ProjectID.String(), resolve.Consumer.MeetingID, call.String(), resolve.Actor.Details().UserID, resolutionJSON(messages), resolutionJSON(identity), resolutionJSON(policy), resolutionStable(resolve))
	if err != nil {
		t.Fatal("canonical consumer input creation", err)
	}
	plan, err := v.base.service.DiscoverResolve(testContext(t), resolve)
	if err != nil {
		t.Fatal("real Model discovery", err)
	}
	var resolved mc.ResolvedModel
	result := v.base.raw.WithinTx(testContext(t), recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.raw.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		resolved, err = v.base.service.ResolveModelInTx(ctx, tx, resolve, plan)
		if err != nil {
			return err
		}
		x, err := v.base.raw.InTx(tx)
		if err != nil {
			return err
		}
		if resolved.CredentialLease == nil {
			return f.NewFault(f.DependencyUnbound, f.NotStarted)
		}
		_, err = x.Exec(ctx, `UPDATE model_runtime_fixture.operations SET snapshot_id=$2,lease_id=$3,version=version+1 WHERE id=$1`, resolve.Consumer.OperationID, resolved.Snapshot.ID.String(), resolved.CredentialLease.LeaseID.String())
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("real snapshot+consumer input transaction not committed", result.State())
	}
	request := mc.ModelRequest{Actor: v.base.owner, CallID: call, Consumer: resolve.Consumer, Model: resolved, Input: identity, Messages: messages, ToolChoice: mc.ToolChoice{Kind: "none"}, ResponseFormat: mc.ResponseFormat{Kind: "text"}, RetryClass: mc.BoundedRetry}
	if request.Validate() != nil {
		t.Fatal("invalid owned Runtime request")
	}
	return request
}

func (v *textRuntimeFixture) assertFacts(t *testing.T, request mc.ModelRequest, success bool) mc.InvocationID {
	t.Helper()
	var invocation mc.InvocationID
	var invocationText, phase, dispatch, ledgerDispatch, ledgerFinal, source string
	var retired, released bool
	var attempts, callCount, ledgerCount, resolves, denies int
	var runtimeSequence, ledgerSequence int64
	var inputTokens, outputTokens, totalTokens *int64
	err := v.base.raw.QueryRow(testContext(t), `SELECT c.invocation_id::text,c.phase,c.retired,a.dispatch,a.sequence,i.dispatch,i.final_status,i.last_sequence,i.usage_source,i.input_tokens,i.output_tokens,i.total_tokens,l.released,(SELECT count(*) FROM agenteam_model.runtime_attempts WHERE call_id=c.id),(SELECT count(*) FROM agenteam_model.calls WHERE id=c.id),(SELECT count(*) FROM agenteam_model.invocations WHERE call_id=c.id),(SELECT count(*) FROM agenteam_audit.audit_records WHERE scope='system' AND project_id IS NULL AND operation_id IS NULL AND action='secret.resolve' AND request_id=c.invocation_id),(SELECT count(*) FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=c.project_id AND producer='outbound.access' AND action='outbound.access.deny' AND cause_ref=c.invocation_id::text AND service_name='outbound' AND service_cause=c.invocation_id::text AND operation_id=$2 AND metadata->>'consumer'='model') FROM agenteam_model.calls c JOIN agenteam_model.runtime_attempts a ON a.id=c.invocation_id JOIN agenteam_model.invocations i ON i.id=c.invocation_id JOIN agenteam_secret.secret_leases l ON l.id=c.lease_id WHERE c.id=$1`, request.CallID.String(), request.Consumer.OperationID).Scan(&invocationText, &phase, &retired, &dispatch, &runtimeSequence, &ledgerDispatch, &ledgerFinal, &ledgerSequence, &source, &inputTokens, &outputTokens, &totalTokens, &released, &attempts, &callCount, &ledgerCount, &resolves, &denies)
	if err != nil {
		t.Fatal("actual call/attempt/Usage/Secret facts", err)
	}
	invocation, err = f.ParseID[mc.Invocation](invocationText)
	if err != nil {
		t.Fatal(err)
	}
	wantPhase, wantDispatch, wantDenies := "failed", "not_sent", 1
	if success {
		wantPhase, wantDispatch, wantDenies = "succeeded", "sent", 0
	}
	if phase != wantPhase || ledgerFinal != wantPhase || dispatch != wantDispatch || ledgerDispatch != dispatch || !retired || !released || attempts != 1 || callCount != 1 || ledgerCount != 1 || runtimeSequence != ledgerSequence || resolves != 1 || denies != wantDenies {
		t.Fatal("canonical/ledger/lease/audit terminal mismatch")
	}
	if success {
		if source != "provider" || inputTokens == nil || *inputTokens != 3 || outputTokens == nil || *outputTokens != 2 || totalTokens == nil || *totalTokens != 5 {
			t.Fatal("reliable provider usage not retained")
		}
	} else if source != "unknown" || inputTokens != nil || outputTokens != nil || totalTokens != nil {
		t.Fatal("denied call invented usage")
	}
	var leaked bool
	// Only a boolean leaves SQL. Never print canonical request/answer/material.
	err = v.base.raw.QueryRow(testContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_model.calls WHERE id=$1 AND (strpos(request_data::text,$2)>0 OR strpos(request_data::text,$3)>0 OR strpos(request_data::text,$4)>0)) OR EXISTS(SELECT 1 FROM agenteam_model.runtime_attempts WHERE call_id=$1 AND (strpos(fact_data::text,$2)>0 OR strpos(fact_data::text,$3)>0 OR strpos(fact_data::text,$4)>0)) OR EXISTS(SELECT 1 FROM agenteam_audit.audit_records WHERE (project_id=$5 OR scope='system') AND (strpos(metadata::text,$2)>0 OR strpos(metadata::text,$3)>0 OR strpos(metadata::text,$4)>0))`, request.CallID.String(), textRuntimeInputCanary, textRuntimeAnswerCanary, "owned-test-credential-never-log", request.Consumer.ProjectID.String()).Scan(&leaked)
	if err != nil || leaked {
		t.Fatal("private content entered durable runtime/audit metadata")
	}
	return invocation
}

// Keep compile-time coverage of the real port; this tap observes the actual
// material cell after committed read and cannot authorize or fabricate it.
var _ sc.CredentialUsageReader = (*textRuntimeReadTap)(nil)
var _ mc.ConsumerAuthority = (*textRuntimeConsumer)(nil)
