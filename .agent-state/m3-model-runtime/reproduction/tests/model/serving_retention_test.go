//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Only generation business facts belong to this fixture. Its retained owner
// is private and bound to real Account/Project permissions and ProcessGuard.
// Model/Secret/Usage/Runtime/transport and every reference/lease are real.
type servingFixtureOwner struct {
	request mc.ServingSnapshotRetentionRequest
}
type servingFixtureKey struct{}
type servingFixtureAuthority struct {
	*textRuntimeConsumer
	issuer mc.PlanIssuer
}
type servingFixtureFacts struct {
	Project, Kind, Agent, Generation, Snapshot, Initiator, Process string
	State                                                          string
	Version                                                        f.Version
	Enabled                                                        bool
}

func (a *servingFixtureAuthority) facts(ctx context.Context, x postgres.SQLExecutor, owner mc.ServingSnapshotOwner) (servingFixtureFacts, error) {
	var out servingFixtureFacts
	err := x.QueryRow(ctx, `SELECT project_id::text,kind,coalesce(agent_id::text,''),id::text,snapshot_id::text,initiator::text,process_id::text,state,version,enabled FROM model_runtime_fixture.generations WHERE id=$1 AND project_id=$2`, owner.GenerationID, owner.ProjectID.String()).Scan(&out.Project, &out.Kind, &out.Agent, &out.Generation, &out.Snapshot, &out.Initiator, &out.Process, &out.State, &out.Version, &out.Enabled)
	return out, err
}
func (a *servingFixtureAuthority) proof(ctx context.Context, r mc.ServingSnapshotRetentionRequest, x postgres.SQLExecutor) (servingFixtureFacts, error) {
	deny := func() (servingFixtureFacts, error) {
		return servingFixtureFacts{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	owner, ok := ctx.Value(servingFixtureKey{}).(*servingFixtureOwner)
	if !ok || owner == nil || r.Validate() != nil {
		return deny()
	}
	want, _ := mc.ServingSnapshotRetentionBinding(owner.request)
	got, _ := mc.ServingSnapshotRetentionBinding(r)
	if got != want {
		return deny()
	}
	facts, err := a.facts(ctx, x, r.Owner)
	if err != nil {
		return deny()
	}
	agent := ""
	if r.Owner.AgentID != nil {
		agent = r.Owner.AgentID.String()
	}
	process, err := a.guard.CurrentProcess()
	if err != nil || facts.Process != process.String() || facts.Project != r.Owner.ProjectID.String() || facts.Kind != string(r.Owner.Kind) || facts.Agent != agent || facts.Generation != r.Owner.GenerationID || facts.Snapshot != r.SnapshotID.String() || facts.Initiator != r.Actor.Details().UserID || !facts.Enabled {
		return deny()
	}
	return facts, nil
}
func servingFixtureMapping(v servingFixtureFacts) f.Digest {
	// State/version are the postcondition checked under the original locks;
	// discovery intentionally precedes the business transition in that Tx.
	v.State = ""
	v.Version = 1
	return resolutionDigest(v)
}
func servingFixtureLocks(v servingFixtureFacts) []f.LockRequest {
	user, _ := f.UserLock(v.Initiator)
	project, _ := f.ProjectLock(v.Project)
	owner, _ := f.RecordLock(f.ReferenceRecordLock, "fixture-generation:"+v.Generation)
	return []f.LockRequest{{Key: user, Mode: f.Shared}, {Key: project, Mode: f.Shared}, {Key: owner, Mode: f.Exclusive}}
}
func (a *servingFixtureAuthority) DiscoverServingSnapshot(ctx context.Context, r mc.ServingSnapshotRetentionRequest) (mc.ConsumerDependencies, error) {
	v, err := a.proof(ctx, r, a.store)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.ServingSnapshotRetentionBinding(r)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: servingFixtureMapping(v), Locks: servingFixtureLocks(v)})
}
func (a *servingFixtureAuthority) ValidateServingSnapshotInTx(ctx context.Context, tx f.Tx, r mc.ServingSnapshotRetentionRequest, p mc.ConsumerDependencies) error {
	return a.validateServingSnapshot(ctx, tx, r, p, false)
}
func (a *servingFixtureAuthority) ValidateServingSnapshotObservationInTx(ctx context.Context, tx f.Tx, r mc.ServingSnapshotRetentionRequest, p mc.ConsumerDependencies) error {
	return a.validateServingSnapshot(ctx, tx, r, p, true)
}
func (a *servingFixtureAuthority) validateServingSnapshot(ctx context.Context, tx f.Tx, r mc.ServingSnapshotRetentionRequest, p mc.ConsumerDependencies, observation bool) error {
	x, err := a.store.InTx(tx)
	if err != nil {
		return err
	}
	v, err := a.proof(ctx, r, x)
	if err != nil {
		return err
	}
	binding, _ := mc.ServingSnapshotRetentionBinding(r)
	phase := v.Version == r.OwnerVersion && (r.Action == mc.RetainServingSnapshot && (v.State == "building" || v.State == "active") || r.Action == mc.ReleaseServingSnapshot && (v.State == "retired" || v.State == "failed"))
	if observation && r.Action == mc.ReleaseServingSnapshot && (v.State == "building" || v.State == "active") && (v.Version == r.OwnerVersion || v.Version+1 == r.OwnerVersion) {
		phase = true
	}
	if !p.Matches(a.issuer, binding, servingFixtureMapping(v)) || !phase {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	if err = a.store.RequireHeldLocks(ctx, tx, p.RequiredLocks()); err != nil {
		return err
	}
	if err = a.accounts.RequireCurrentSession(ctx, tx, r.Actor); err != nil {
		return err
	}
	_, err = a.projects.RequireOwnerInTx(ctx, tx, r.Actor, r.Owner.ProjectID, id.Mutate)
	return err
}

func newServingFixture(t *testing.T) (*textRuntimeFixture, *servingFixtureAuthority, *model.ServingSnapshotRetention) {
	t.Helper()
	v := newTextRuntimeFixture(t)
	_, err := v.base.raw.Exec(testContext(t), `CREATE TABLE model_runtime_fixture.generations(id uuid PRIMARY KEY,project_id uuid NOT NULL,kind text NOT NULL,agent_id uuid,snapshot_id uuid NOT NULL,initiator uuid NOT NULL,process_id uuid NOT NULL,state text NOT NULL,version bigint NOT NULL,enabled boolean NOT NULL)`)
	requireTextRuntime(t, err)
	a := &servingFixtureAuthority{textRuntimeConsumer: v.consumer, issuer: mc.NewPlanIssuer()}
	retention, err := model.NewServingSnapshotRetention(v.base.authority, a, v.base.secrets)
	requireTextRuntime(t, err)
	return v, a, retention
}
func servingFixtureRequest(t *testing.T, v *textRuntimeFixture, snapshot mc.SnapshotID, kind mc.ConsumerKind) (context.Context, mc.ServingSnapshotRetentionRequest) {
	t.Helper()
	r := mc.ServingSnapshotRetentionRequest{Actor: v.base.owner, Owner: mc.ServingSnapshotOwner{Kind: kind, ProjectID: v.base.project.ID, GenerationID: newID[struct{}](t).String()}, SnapshotID: snapshot, OwnerVersion: 1, Action: mc.RetainServingSnapshot}
	var agent any
	if kind == mc.MemoryConsumer {
		a := newID[id.Agent](t)
		r.Owner.AgentID = &a
		agent = a.String()
	}
	process, err := v.guard.CurrentProcess()
	requireTextRuntime(t, err)
	_, err = v.base.raw.Exec(testContext(t), `INSERT INTO model_runtime_fixture.generations(id,project_id,kind,agent_id,snapshot_id,initiator,process_id,state,version,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,'building',1,true)`, r.Owner.GenerationID, r.Owner.ProjectID.String(), string(kind), agent, snapshot.String(), r.Actor.Details().UserID, process.String())
	requireTextRuntime(t, err)
	return context.WithValue(testContext(t), servingFixtureKey{}, &servingFixtureOwner{r.Clone()}), r
}
func servingFixtureTx(t *testing.T, v *textRuntimeFixture, retention *model.ServingSnapshotRetention, ctx context.Context, r mc.ServingSnapshotRetentionRequest, p mc.ServingSnapshotRetentionPlan, state string, rollback bool) f.CommitResult {
	t.Helper()
	return v.base.raw.WithinTx(ctx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.raw.AcquireAll(ctx, tx, p.RequiredLocks()); err != nil {
			return err
		}
		x, err := v.base.raw.InTx(tx)
		if err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `UPDATE model_runtime_fixture.generations SET state=$2,version=$3 WHERE id=$1`, r.Owner.GenerationID, state, int64(r.OwnerVersion)); err != nil {
			return err
		}
		if err = retention.ApplyServingSnapshotRetentionInTx(ctx, tx, r, p); err != nil {
			return err
		}
		if rollback {
			return f.NewFault(f.InvalidState, f.NotStarted)
		}
		return nil
	})
}
func servingFixtureObserve(t *testing.T, v *textRuntimeFixture, retention *model.ServingSnapshotRetention, ctx context.Context, r mc.ServingSnapshotRetentionRequest, p mc.ServingSnapshotRetentionPlan) bool {
	t.Helper()
	var applied bool
	result := v.base.raw.WithinTx(ctx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.raw.AcquireAll(ctx, tx, p.RequiredLocks()); err != nil {
			return err
		}
		var err error
		applied, err = retention.ServingSnapshotRetentionAppliedInTx(ctx, tx, r, p)
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("retention observation", result.Fault())
	}
	return applied
}

func TestModelM3ServingSnapshotRetention(t *testing.T) {
	v, _, retention := newServingFixture(t)
	v.wire.allow(t, true)
	key := embeddingWireScenario(t, v.wire, wireJSON(embeddingWireReply(t, []int{0}, [][]float64{{.25, -.5}}, json.RawMessage(`{"prompt_tokens":3,"total_tokens":3,"completion_tokens":0}`))))
	em := m3RuntimeModel(t, v, key, true)
	chat := m3RuntimeModel(t, v, key, false)
	m3RuntimeSelection(t, v, em, chat)
	_, first := m3RuntimeRequest(t, v, mc.KnowledgeEmbedding, mc.ResponseFormat{Kind: "text"}, nil)
	ctx, request := servingFixtureRequest(t, v, first.Model.Snapshot.ID, mc.KnowledgeConsumer)
	plan, err := retention.DiscoverServingSnapshotRetention(ctx, request)
	requireTextRuntime(t, err)
	if _, err = retention.DiscoverServingSnapshotRetention(testContext(t), request); err == nil {
		t.Fatal("naked generation DTO authorized")
	}
	for name, change := range map[string]func(*mc.ServingSnapshotRetentionRequest){
		"wrong generation": func(r *mc.ServingSnapshotRetentionRequest) { r.Owner.GenerationID = newID[struct{}](t).String() },
		"wrong project":    func(r *mc.ServingSnapshotRetentionRequest) { r.Owner.ProjectID = newID[id.Project](t) },
		"wrong snapshot":   func(r *mc.ServingSnapshotRetentionRequest) { r.SnapshotID = newID[mc.Snapshot](t) },
		"wrong version":    func(r *mc.ServingSnapshotRetentionRequest) { r.OwnerVersion++ },
	} {
		t.Run(name, func(t *testing.T) {
			wrong := request.Clone()
			change(&wrong)
			result := servingFixtureTx(t, v, retention, ctx, wrong, plan, "active", false)
			if result.State() != f.NotCommitted {
				t.Fatal("substituted request committed")
			}
			requireCode(t, result.Fault(), f.Forbidden)
		})
	}
	weak := v.base.raw.WithinTx(ctx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		// Missing the complete pre-collected set poisons the transaction even
		// when a caller mistakenly ignores the operation's returned error.
		_ = retention.ApplyServingSnapshotRetentionInTx(ctx, tx, request, plan)
		return nil
	})
	if weak.State() != f.NotCommitted {
		t.Fatal("missing retention locks did not poison transaction")
	}
	if result := servingFixtureTx(t, v, retention, ctx, request, plan, "active", true); result.State() != f.NotCommitted {
		t.Fatal("outer failure committed retention")
	}
	if servingFixtureObserve(t, v, retention, ctx, request, plan) {
		t.Fatal("rolled back reference observed")
	}
	if result := servingFixtureTx(t, v, retention, ctx, request, plan, "active", false); result.State() != f.Committed {
		t.Fatal("retain", result.Fault())
	}
	if !servingFixtureObserve(t, v, retention, ctx, request, plan) {
		t.Fatal("retained reference absent")
	}
	old, err := v.base.secrets.Metadata(testContext(t), v.base.admin, *first.Model.Snapshot.CredentialRef)
	requireTextRuntime(t, err)
	if _, err = v.core.Embed(testContext(t), first); err != nil {
		t.Fatal("first embedding", err)
	}
	m3RuntimeTerminal(t, v, first.CallID, "succeeded")
	// Drop the Provider's A reference and use B. A's only remaining live owner
	// is the serving generation, after the original call has fully joined.
	provider, err := v.base.service.GetProvider(testContext(t), v.base.admin, first.Model.Snapshot.Identity.ProviderID)
	requireTextRuntime(t, err)
	next := v.base.credential(t, sc.Model)
	provider.Input.CredentialRef = &next.CredentialRef
	receipt, err := v.base.service.UpdateProvider(testContext(t), mc.UpdateProviderRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), ID: provider.ID, ExpectedVersion: provider.Version, Input: provider.Input})
	requireTextRuntime(t, err)
	provider.Version = receipt.Version
	requireCode(t, v.base.deleteCredential(t, old), f.ResourceBusy)
	_, second := m3RuntimeGenerationRequest(t, v, mc.KnowledgeEmbedding, mc.ResponseFormat{Kind: "text"}, &request.SnapshotID, request.Owner.GenerationID, []string{textRuntimeInputCanary}, 2)
	if !second.Model.Snapshot.CredentialRef.Equal(old.CredentialRef) || second.Model.CredentialLease.LeaseID == first.Model.CredentialLease.LeaseID {
		t.Fatal("serving credential or lease rebound")
	}
	if _, err = v.core.Embed(testContext(t), second); err != nil {
		t.Fatal("old serving after credential replacement", err)
	}
	m3RuntimeTerminal(t, v, second.CallID, "succeeded")
	// Delete the live model/provider through their formal commands. The old
	// snapshot and its long-term reference must remain independently usable.
	alternate := m3RuntimeModel(t, v, key, true)
	m3RuntimeSelection(t, v, alternate, chat)
	view, err := v.base.service.GetModel(testContext(t), v.base.admin, em)
	requireTextRuntime(t, err)
	_, err = v.base.service.DeleteModel(testContext(t), mc.DeleteModelRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), ID: em, ExpectedVersion: view.Version})
	requireTextRuntime(t, err)
	_, err = v.base.service.DeleteProvider(testContext(t), mc.DeleteProviderRequest{CommandMeta: v.base.meta(t, newID[struct{}](t).String()), ID: provider.ID, ExpectedVersion: provider.Version})
	requireTextRuntime(t, err)
	requireCode(t, v.base.deleteCredential(t, old), f.ResourceBusy)
	_, third := m3RuntimeGenerationRequest(t, v, mc.KnowledgeEmbedding, mc.ResponseFormat{Kind: "text"}, &request.SnapshotID, request.Owner.GenerationID, []string{textRuntimeInputCanary}, 2)
	if _, err = v.core.Embed(testContext(t), third); err != nil {
		t.Fatal("old serving after provider deletion", err)
	}
	m3RuntimeTerminal(t, v, third.CallID, "succeeded")
	// A second business owner on the same snapshot must survive the first
	// owner's release. This also exercises Memory's own version 1 -> 2.
	memoryCtx, memory := servingFixtureRequest(t, v, request.SnapshotID, mc.MemoryConsumer)
	memoryPlan, err := retention.DiscoverServingSnapshotRetention(memoryCtx, memory)
	requireTextRuntime(t, err)
	if result := servingFixtureTx(t, v, retention, memoryCtx, memory, memoryPlan, "active", false); result.State() != f.Committed {
		t.Fatal("second owner retain", result.Fault())
	}
	release := request.Clone()
	release.Action = mc.ReleaseServingSnapshot
	releaseCtx := context.WithValue(testContext(t), servingFixtureKey{}, &servingFixtureOwner{release.Clone()})
	releasePlan, err := retention.DiscoverServingSnapshotRetention(releaseCtx, release)
	requireTextRuntime(t, err)
	if result := servingFixtureTx(t, v, retention, releaseCtx, release, releasePlan, "retired", false); result.State() != f.Committed {
		t.Fatal("first owner release", result.Fault())
	}
	if !servingFixtureObserve(t, v, retention, releaseCtx, release, releasePlan) {
		t.Fatal("release not observed")
	}
	requireCode(t, v.base.deleteCredential(t, old), f.ResourceBusy)
	memory.Action = mc.ReleaseServingSnapshot
	memory.OwnerVersion = 2
	memoryCtx = context.WithValue(testContext(t), servingFixtureKey{}, &servingFixtureOwner{memory.Clone()})
	memoryPlan, err = retention.DiscoverServingSnapshotRetention(memoryCtx, memory)
	requireTextRuntime(t, err)
	if result := servingFixtureTx(t, v, retention, memoryCtx, memory, memoryPlan, "retired", false); result.State() != f.Committed {
		t.Fatal("last owner release", result.Fault())
	}
	requireTextRuntime(t, v.base.deleteCredential(t, old))
	if !servingFixtureObserve(t, v, retention, memoryCtx, memory, memoryPlan) {
		t.Fatal("release observation required deleted metadata")
	}
	if _, err = retention.DiscoverServingSnapshotRetention(ctx, request); err == nil {
		t.Fatal("released generation retained again")
	}
	before := len(v.wire.state(t, key).Requests)
	requireTextRuntime(t, v.account.Logout(testContext(t), account.LogoutRequest{Actor: v.base.owner, Key: "retention-revoke-owner"}))
	revoked := v.base.raw.WithinTx(memoryCtx, recoveryCause(t), func(ctx context.Context, tx f.Tx) error {
		if err := v.base.raw.AcquireAll(ctx, tx, memoryPlan.RequiredLocks()); err != nil {
			return err
		}
		_, err := retention.ServingSnapshotRetentionAppliedInTx(ctx, tx, memory, memoryPlan)
		return err
	})
	if revoked.State() != f.NotCommitted {
		t.Fatal("revoked owner observed retirement")
	}
	state := v.wire.state(t, key)
	if len(state.Requests) != 3 || len(state.Requests) != before {
		t.Fatal("retention caused provider dispatch")
	}
	v.wire.settled(t, key)
}
