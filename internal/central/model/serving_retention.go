package model

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// ServingSnapshotRetention binds one business generation to one immutable
// snapshot. Its Secret reference survives configuration replacement and is
// independent of each invocation's shorter credential lease.
type ServingSnapshotRetention struct {
	authority *Authority
	consumers mc.ServingSnapshotAuthority
	secrets   sc.UsageOperations
	observer  sc.ModelServingSnapshotReferenceObservation
}

func NewServingSnapshotRetention(authority *Authority, consumers mc.ServingSnapshotAuthority, secrets sc.UsageOperations) (*ServingSnapshotRetention, error) {
	observer, ok := secrets.(sc.ModelServingSnapshotReferenceObservation)
	if authority.state() == nil || nilPort(consumers) || nilPort(secrets) || !ok || nilPort(observer) {
		return nil, fault(f.DependencyUnbound)
	}
	return &ServingSnapshotRetention{authority, consumers, secrets, observer}, nil
}

type servingRetentionPlan struct {
	owner     *ServingSnapshotRetention
	request   mc.ServingSnapshotRetentionRequest
	binding   f.Digest
	mapping   f.Digest
	consumer  mc.ConsumerDependencies
	snapshot  mc.ConfigSnapshot
	digest    f.Digest
	reference string
	usage     sc.UsageRequest
	secret    sc.UsageDependencies
	locks     []f.LockRequest
}

func (p *servingRetentionPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return append([]f.LockRequest(nil), p.locks...)
}
func (*servingRetentionPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "model_serving_retention_plan")
}
func (*servingRetentionPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"model_serving_retention_plan"`), nil
}
func (*servingRetentionPlan) LogValue() slog.Value {
	return slog.StringValue("model_serving_retention_plan")
}

type servingRetentionRecord struct {
	reference, agent, snapshot, digest, credential, scope, credentialProject, state string
	version, releasedVersion                                                        f.Version
}

func loadServingRetention(ctx context.Context, x postgres.SQLExecutor, owner mc.ServingSnapshotOwner) (*servingRetentionRecord, error) {
	var r servingRetentionRecord
	err := x.QueryRow(ctx, `SELECT reference_id,coalesce(agent_id::text,''),snapshot_id,snapshot_digest,coalesce(credential_id::text,''),coalesce(credential_scope,''),coalesce(credential_project_id::text,''),state,owner_version,coalesce(released_version,0) FROM agenteam_model.serving_snapshot_retentions WHERE project_id=$1 AND consumer_kind=$2 AND generation_id=$3`, owner.ProjectID.String(), string(owner.Kind), owner.GenerationID).Scan(&r.reference, &r.agent, &r.snapshot, &r.digest, &r.credential, &r.scope, &r.credentialProject, &r.state, &r.version, &r.releasedVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, unavailable(err)
	}
	return &r, nil
}

func servingRetentionSnapshot(ctx context.Context, x postgres.SQLExecutor, r mc.ServingSnapshotRetentionRequest) (mc.ConfigSnapshot, f.Digest, error) {
	var raw []byte
	var digest f.Digest
	err := x.QueryRow(ctx, `SELECT snapshot_data,snapshot_digest FROM agenteam_model.snapshots WHERE id=$1 AND project_id=$2`, r.SnapshotID.String(), r.Owner.ProjectID.String()).Scan(&raw, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return mc.ConfigSnapshot{}, "", fault(f.NotFound)
	}
	if err != nil {
		return mc.ConfigSnapshot{}, "", unavailable(err)
	}
	var dto resolutionSnapshotDTO
	if err = resolutionDecode(raw, 128<<10, &dto); err != nil {
		return mc.ConfigSnapshot{}, "", err
	}
	canonical, err := encoded(dto)
	if err != nil || hash(canonical) != digest || dto.ID != r.SnapshotID || dto.ProjectID != r.Owner.ProjectID {
		return mc.ConfigSnapshot{}, "", unavailable(err)
	}
	snapshot, err := dto.snapshot()
	if err != nil {
		return mc.ConfigSnapshot{}, "", err
	}
	want := mc.EmbeddingModel
	if r.Owner.Kind == mc.MeetingConsumer {
		want = mc.ChatModel
		if snapshot.SelectionVersion == nil {
			return mc.ConfigSnapshot{}, "", fault(f.CapabilityUnsupported)
		}
	}
	if snapshot.Identity.ModelType != want {
		return mc.ConfigSnapshot{}, "", fault(f.CapabilityUnsupported)
	}
	return snapshot, digest, nil
}

func servingRetentionLock(o mc.ServingSnapshotOwner) f.LockRequest {
	return recordLock(f.ReferenceRecordLock, "model-serving:"+o.ProjectID.String()+":"+string(o.Kind)+":"+o.GenerationID)
}

func (p *servingRetentionPlan) recordMatches(r *servingRetentionRecord) bool {
	if r == nil || r.reference != p.reference || r.agent != servingAgent(p.request.Owner) || r.snapshot != p.request.SnapshotID.String() || r.digest != string(p.digest) {
		return false
	}
	credential, scope, project := p.credential()
	if r.credential != credential || r.scope != scope || r.credentialProject != project {
		return false
	}
	if p.request.Action == mc.RetainServingSnapshot {
		return r.version == p.request.OwnerVersion
	}
	return p.request.OwnerVersion >= r.version && (r.state != "released" || r.releasedVersion == p.request.OwnerVersion)
}
func servingAgent(o mc.ServingSnapshotOwner) string {
	if o.AgentID == nil {
		return ""
	}
	return o.AgentID.String()
}
func (p *servingRetentionPlan) credential() (string, string, string) {
	if p.snapshot.CredentialRef == nil {
		return "", "", ""
	}
	d := p.snapshot.CredentialRef.Details()
	return d.ID.String(), string(d.Scope.Details().Kind), d.Scope.Details().ProjectID
}

func (s *ServingSnapshotRetention) DiscoverServingSnapshotRetention(ctx context.Context, request mc.ServingSnapshotRetentionRequest) (mc.ServingSnapshotRetentionPlan, error) {
	if err := resolutionContextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.authority.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	binding, err := mc.ServingSnapshotRetentionBinding(request)
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	consumer, err := s.consumers.DiscoverServingSnapshot(ctx, request.Clone())
	if err != nil {
		return nil, portError(err)
	}
	if consumer.Validate() != nil || consumer.Details().Binding != binding || consumer.Details().RetryPolicy != nil {
		return nil, fault(f.Forbidden)
	}
	snapshot, digest, err := servingRetentionSnapshot(ctx, s.authority.state().store, request)
	if err != nil {
		return nil, err
	}
	row, err := loadServingRetention(ctx, s.authority.state().store, request.Owner)
	if err != nil {
		return nil, err
	}
	p := &servingRetentionPlan{owner: s, request: request.Clone(), binding: binding, consumer: consumer, snapshot: snapshot.Clone(), digest: digest}
	if row == nil {
		if request.Action != mc.RetainServingSnapshot {
			return nil, fault(f.NotFound)
		}
		p.reference, err = newID()
		if err != nil {
			return nil, err
		}
	} else {
		p.reference = row.reference
		if !p.recordMatches(row) || request.Action == mc.RetainServingSnapshot && row.state != "retained" {
			return nil, fault(f.VersionConflict)
		}
	}
	p.locks, err = resolutionUnion(consumer.RequiredLocks(), []f.LockRequest{projectLock(request.Owner.ProjectID.String()), servingRetentionLock(request.Owner), recordLock(f.ReferenceRecordLock, "model-snapshot:"+request.SnapshotID.String())})
	if err != nil {
		return nil, err
	}
	raw, err := encoded(struct {
		Request, Consumer, Snapshot f.Digest
		Reference                   string
	}{binding, consumer.Details().Mapping, digest, p.reference})
	if err != nil {
		return nil, err
	}
	p.mapping = hash(raw)
	if snapshot.CredentialRef != nil {
		action := sc.ReleaseReferenceUsage
		if request.Action == mc.RetainServingSnapshot {
			action = sc.RetainReferenceUsage
		}
		p.usage = sc.UsageRequest{Actor: request.Actor, Ref: *snapshot.CredentialRef, Purpose: sc.Model, ReferenceOwner: p.reference, Action: action, Retain: request.Action == mc.RetainServingSnapshot}
		w := &servingRetentionWitness{plan: p, active: true}
		defer w.retire()
		p.secret, err = s.secrets.DiscoverUsage(context.WithValue(ctx, servingRetentionKey{}, w), p.usage)
		if err != nil {
			return nil, portError(err)
		}
		usageBinding, _ := sc.UsageBinding(p.usage)
		if p.secret.Validate() != nil || !p.secret.ProviderPlan().Matches(s.authority.state().usageIssuer, usageBinding, p.mapping) {
			return nil, fault(f.Forbidden)
		}
		p.locks, err = resolutionUnion(p.locks, p.secret.RequiredLocks())
		if err != nil {
			return nil, err
		}
	}
	if err = resolutionContextError(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *ServingSnapshotRetention) prepare(ctx context.Context, tx f.Tx, request mc.ServingSnapshotRetentionRequest, supplied mc.ServingSnapshotRetentionPlan, observation bool) (*servingRetentionPlan, error) {
	if err := resolutionContextError(ctx); err != nil {
		return nil, err
	}
	if s == nil || s.authority.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	p, ok := supplied.(*servingRetentionPlan)
	binding, err := mc.ServingSnapshotRetentionBinding(request)
	if !ok || p == nil || p.owner != s || err != nil || binding != p.binding {
		return nil, fault(f.Forbidden)
	}
	if err = p.validate(ctx, tx, observation); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *servingRetentionPlan) validate(ctx context.Context, tx f.Tx, observation bool) error {
	store := p.owner.authority.state().store
	x, err := store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	if err = store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return portError(err)
	}
	if observation {
		err = p.owner.consumers.ValidateServingSnapshotObservationInTx(ctx, tx, p.request.Clone(), p.consumer)
	} else {
		err = p.owner.consumers.ValidateServingSnapshotInTx(ctx, tx, p.request.Clone(), p.consumer)
	}
	if err != nil {
		return portError(err)
	}
	snapshot, digest, err := servingRetentionSnapshot(ctx, x, p.request)
	if err != nil {
		return err
	}
	if digest != p.digest || !resolutionEqual(snapshot, p.snapshot) {
		return fault(f.VersionConflict)
	}
	return resolutionContextError(ctx)
}

func (s *ServingSnapshotRetention) ApplyServingSnapshotRetentionInTx(ctx context.Context, tx f.Tx, request mc.ServingSnapshotRetentionRequest, supplied mc.ServingSnapshotRetentionPlan) error {
	p, err := s.prepare(ctx, tx, request, supplied, false)
	if err != nil {
		return err
	}
	x, _ := s.authority.state().store.InTx(tx)
	row, err := loadServingRetention(ctx, x, request.Owner)
	if err != nil {
		return err
	}
	if row != nil && !p.recordMatches(row) || row == nil && request.Action == mc.ReleaseServingSnapshot || row != nil && row.state == "released" && request.Action == mc.RetainServingSnapshot {
		return fault(f.VersionConflict)
	}
	if row != nil && p.snapshot.CredentialRef != nil {
		w := &servingRetentionWitness{plan: p, tx: tx, active: true}
		present, observeErr := s.observer.ModelServingSnapshotReferenceInTx(context.WithValue(ctx, servingRetentionKey{}, w), tx, p.usage, p.secret)
		w.retire()
		if observeErr != nil {
			return portError(observeErr)
		}
		if present != (row.state == "retained") {
			return fault(f.InvalidState)
		}
	}
	// Already released is an exact, read-only replay. In particular it must
	// still work after the now-unreferenced credential has been deleted.
	if row != nil && row.state == "released" {
		return resolutionContextError(ctx)
	}
	if row == nil {
		credential, scope, project := p.credential()
		_, err = x.Exec(ctx, `INSERT INTO agenteam_model.serving_snapshot_retentions(reference_id,project_id,consumer_kind,agent_id,generation_id,owner_version,snapshot_id,snapshot_digest,credential_id,credential_scope,credential_project_id,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'retained')`, p.reference, request.Owner.ProjectID.String(), string(request.Owner.Kind), null(servingAgent(request.Owner)), request.Owner.GenerationID, int64(request.OwnerVersion), request.SnapshotID.String(), string(p.digest), null(credential), null(scope), null(project))
	} else if request.Action == mc.ReleaseServingSnapshot && row.state == "retained" {
		_, err = x.Exec(ctx, `UPDATE agenteam_model.serving_snapshot_retentions SET state='released',released_version=$2,released_at=clock_timestamp() WHERE reference_id=$1 AND state='retained'`, p.reference, int64(request.OwnerVersion))
	}
	if err != nil {
		return unavailable(err)
	}
	if p.snapshot.CredentialRef != nil {
		w := &servingRetentionWitness{plan: p, tx: tx, active: true}
		defer w.retire()
		result, err := s.secrets.ApplyUsageInTx(context.WithValue(ctx, servingRetentionKey{}, w), tx, p.usage, p.secret)
		if err != nil {
			return portError(err)
		}
		if result.Action() != p.usage.Action {
			return fault(f.Forbidden)
		}
	}
	return resolutionContextError(ctx)
}

func (s *ServingSnapshotRetention) ServingSnapshotRetentionAppliedInTx(ctx context.Context, tx f.Tx, request mc.ServingSnapshotRetentionRequest, supplied mc.ServingSnapshotRetentionPlan) (bool, error) {
	p, err := s.prepare(ctx, tx, request, supplied, true)
	if err != nil {
		return false, err
	}
	x, _ := s.authority.state().store.InTx(tx)
	row, err := loadServingRetention(ctx, x, request.Owner)
	if err != nil || row == nil {
		return false, err
	}
	if !p.recordMatches(row) {
		return false, fault(f.VersionConflict)
	}
	wanted := "retained"
	if request.Action == mc.ReleaseServingSnapshot {
		wanted = "released"
	}
	if row.state != wanted {
		return false, nil
	}
	if p.snapshot.CredentialRef != nil {
		w := &servingRetentionWitness{plan: p, tx: tx, active: true, observation: true}
		defer w.retire()
		present, err := s.observer.ModelServingSnapshotReferenceInTx(context.WithValue(ctx, servingRetentionKey{}, w), tx, p.usage, p.secret)
		if err != nil {
			return false, portError(err)
		}
		if present != (wanted == "retained") {
			return false, fault(f.InvalidState)
		}
	}
	return true, resolutionContextError(ctx)
}

type servingRetentionKey struct{}
type servingRetentionWitness struct {
	plan        *servingRetentionPlan
	tx          f.Tx
	mu          sync.Mutex
	active      bool
	observation bool
}

func (w *servingRetentionWitness) retire() {
	w.mu.Lock()
	w.active = false
	w.mu.Unlock()
}
func (a *Authority) servingRetentionWitness(ctx context.Context, r sc.UsageRequest) (*servingRetentionWitness, error) {
	if err := resolutionContextError(ctx); err != nil {
		return nil, err
	}
	w, ok := ctx.Value(servingRetentionKey{}).(*servingRetentionWitness)
	if !ok || w == nil || w.plan == nil || w.plan.owner.authority != a || a.state() == nil {
		return nil, fault(f.DependencyUnbound)
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	binding, err := sc.UsageBinding(r)
	wanted, e := sc.UsageBinding(w.plan.usage)
	if !active || err != nil || e != nil || binding != wanted {
		return nil, fault(f.Forbidden)
	}
	return w, nil
}
func (a *Authority) discoverServingRetentionUsage(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := a.servingRetentionWitness(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, _ := sc.UsageBinding(r)
	return sc.NewUsageDependencies(a.state().usageIssuer, binding, w.plan.mapping, w.plan.locks)
}
func (a *Authority) validateServingRetentionUsage(ctx context.Context, tx f.Tx, r sc.UsageRequest, plan sc.UsageDependencies) error {
	w, err := a.servingRetentionWitness(ctx, r)
	if err != nil {
		return err
	}
	binding, _ := sc.UsageBinding(r)
	if !tx.Valid() || w.tx != tx || !plan.Matches(a.state().usageIssuer, binding, w.plan.mapping) {
		return fault(f.Forbidden)
	}
	return w.plan.validate(ctx, tx, w.observation)
}

var _ mc.ServingSnapshotRetention = (*ServingSnapshotRetention)(nil)
