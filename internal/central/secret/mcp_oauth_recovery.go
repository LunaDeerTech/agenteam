package secret

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync/atomic"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type mcpOAuthRecoveryRedacted struct{}

func (mcpOAuthRecoveryRedacted) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "mcp_oauth_delete_recovery")
}
func (mcpOAuthRecoveryRedacted) MarshalJSON() ([]byte, error) {
	return []byte(`"mcp_oauth_delete_recovery"`), nil
}
func (*mcpOAuthRecoveryRedacted) UnmarshalJSON([]byte) error { return invalid() }
func (mcpOAuthRecoveryRedacted) LogValue() slog.Value {
	return slog.StringValue("mcp_oauth_delete_recovery")
}

type mcpOAuthDeletePlan struct {
	mcpOAuthRecoveryRedacted
	owner        *mcpOAuthWriterState
	authority    sc.MCPOAuthDeleteRecoveryAuthority
	request      sc.MCPOAuthWriteRequest
	dependencies sc.WriteDependencies
	locks        []f.LockRequest
	command      f.Digest
}

func (p *mcpOAuthDeletePlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}

type mcpOAuthDeleteContinuation struct {
	mcpOAuthRecoveryRedacted
	plan *mcpOAuthDeletePlan
	used atomic.Bool
}

func (p *mcpOAuthDeleteContinuation) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return p.plan.RequiredLocks()
}

type preparedMCPOAuthDeleteRecovery struct {
	mcpOAuthRecoveryRedacted
	plan  *mcpOAuthDeletePlan
	write *preparedMCPOAuthWrite
}

func (p *preparedMCPOAuthDeleteRecovery) RequiredLocks() []f.LockRequest {
	if p == nil || p.write == nil {
		return nil
	}
	return p.write.RequiredLocks()
}

func (w *MCPOAuthWriter) DiscoverMCPOAuthDeleteRecovery(ctx context.Context, r sc.MCPOAuthWriteRequest) (sc.MCPOAuthDeleteRecoveryPlan, error) {
	state, err := w.state()
	if err != nil {
		return nil, err
	}
	if ctx == nil || sc.ValidateMCPOAuthDeleteRecoveryRequest(r) != nil {
		return nil, invalid()
	}
	authority, ok := state.authority.(sc.MCPOAuthDeleteRecoveryAuthority)
	if !ok || nilPort(authority) {
		return nil, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	// Discovery consumes the original bound domain's live private plan. It
	// does not seal a command, reserve a nonce, or touch credential material.
	plan, err := w.DiscoverMCPOAuthWrite(ctx, r.WithoutMaterial())
	if err != nil {
		return nil, err
	}
	v := r.Fields()
	locks, err := mutationLocks(v.Identity, v.Ref, v.Actor)
	if err != nil {
		return nil, err
	}
	command, err := cursor.Digest([]byte(v.Identity.Canonical()))
	if err != nil {
		return nil, invalid()
	}
	return &mcpOAuthDeletePlan{owner: state, authority: authority, request: r.WithoutMaterial(), dependencies: plan, locks: append(locks, plan.Locks()...), command: command}, nil
}

func (w *MCPOAuthWriter) ownedOAuthDeletePlan(raw sc.MCPOAuthDeleteRecoveryPlan) (*mcpOAuthDeletePlan, error) {
	state, err := w.state()
	if err != nil {
		return nil, err
	}
	p, ok := raw.(*mcpOAuthDeletePlan)
	if !ok || p == nil || p.owner != state || nilPort(p.authority) || sc.ValidateMCPOAuthDeleteRecoveryRequest(p.request) != nil || !validMCPOAuthPlan(p.request, p.dependencies) {
		return nil, failure(PreparationRequired, f.ResourceBusy, nil)
	}
	return p, nil
}

func (p *mcpOAuthDeletePlan) require(ctx context.Context, tx f.Tx) (postgres.SQLExecutor, error) {
	if ctx == nil || !tx.Valid() {
		return nil, invalid()
	}
	store := p.owner.service.state().store
	x, err := store.InTx(tx)
	if err != nil {
		return nil, unavailable(err)
	}
	if err = store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return nil, unavailable(err)
	}
	if err = p.authority.ValidateMCPOAuthDeleteRecoveryInTx(ctx, tx, p.request, p.dependencies); err != nil {
		return nil, plannedAuthorization(err)
	}
	return x, nil
}

func (w *MCPOAuthWriter) ObserveMCPOAuthDeleteRecoveryInTx(ctx context.Context, tx f.Tx, raw sc.MCPOAuthDeleteRecoveryPlan) (sc.MCPOAuthDeleteRecoveryObservation, error) {
	var empty sc.MCPOAuthDeleteRecoveryObservation
	p, err := w.ownedOAuthDeletePlan(raw)
	if err != nil {
		return empty, err
	}
	x, err := p.require(ctx, tx)
	if err != nil {
		return empty, err
	}
	result, found, err := p.receipt(ctx, x)
	if err != nil {
		return empty, err
	}
	if found {
		return sc.MCPOAuthDeleteRecoveryObservation{Found: true, Result: result}, nil
	}
	// Missing receipt is insufficient. The original writer is proven stopped
	// by the authority above, the command lock waited out its transaction,
	// and the exact current MCP version must still exist. No value is opened.
	v := p.request.Fields()
	metadata, _, err := loadMetadata(ctx, x, v.Ref)
	if err != nil {
		return empty, err
	}
	if metadata.Purpose != sc.MCP || !metadata.CredentialRef.Equal(v.Ref) || metadata.Version != v.ExpectedVersion {
		return empty, failure(Conflict, f.VersionConflict, nil)
	}
	return sc.MCPOAuthDeleteRecoveryObservation{Continuation: &mcpOAuthDeleteContinuation{plan: p}}, nil
}

// receipt opens only the canonical command-binding digest. Unlike preparing a
// write, this observation allocates no payload ID or encryption nonce.
func (p *mcpOAuthDeletePlan) receipt(ctx context.Context, x postgres.SQLExecutor) (sc.MutationResult, bool, error) {
	v := p.request.Fields()
	scope := v.Ref.Details().Scope
	var receiptID, credential, purpose, kind, payload string
	var version int64
	var deleted bool
	err := x.QueryRow(ctx, `SELECT id::text,credential_id::text,purpose,mutation_kind,result_version,deleted,digest_payload_id::text
 FROM agenteam_secret.secret_command_receipts WHERE scope=$1 AND scope_key=$2 AND command_digest=$3`, string(id.ProjectScope), scopeKey(scope), string(p.command)).Scan(&receiptID, &credential, &purpose, &kind, &version, &deleted, &payload)
	if errors.Is(err, pgx.ErrNoRows) {
		return sc.MutationResult{}, false, nil
	}
	if err != nil {
		return sc.MutationResult{}, false, unavailable(err)
	}
	payloadID, err := f.ParseID[payloadMarker](payload)
	if err != nil {
		return sc.MutationResult{}, false, unavailable(err)
	}
	sealed, err := loadPayload(ctx, x, payloadID)
	if err != nil {
		return sc.MutationResult{}, false, err
	}
	if sealed.ownerKind != receiptOwner || sealed.ownerID != receiptID || !sealed.scope.Equal(scope) {
		return sc.MutationResult{}, false, failure(DecryptFailed, f.DependencyUnavailable, nil)
	}
	observed, err := openEnvelope(p.owner.service.state().keys, sealed)
	if err != nil {
		return sc.MutationResult{}, false, err
	}
	defer clear(observed)
	binding, _ := sc.MCPOAuthWriteBinding(p.request)
	expected := sha256.Sum256([]byte(binding))
	defer clear(expected[:])
	if subtle.ConstantTimeCompare(observed, expected[:]) != 1 {
		return sc.MutationResult{}, false, failure(Conflict, f.IdempotencyKeyReused, nil)
	}
	if credential != v.Ref.Details().ID.String() || purpose != string(sc.MCP) || kind != string(sc.Delete) || !deleted || f.Version(version).Validate() != nil || f.Version(version) <= v.ExpectedVersion || f.Version(version)-v.ExpectedVersion != 1 {
		return sc.MutationResult{}, false, failure(Conflict, f.InvalidState, nil)
	}
	var exists bool
	if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secrets WHERE id=$1)`, credential).Scan(&exists); err != nil {
		return sc.MutationResult{}, false, unavailable(err)
	}
	if exists {
		return sc.MutationResult{}, false, failure(Conflict, f.InvalidState, nil)
	}
	return sc.MutationResult{Metadata: sc.Metadata{CredentialRef: v.Ref, Purpose: sc.MCP, Version: f.Version(version)}, Deleted: true}, true, nil
}

func (w *MCPOAuthWriter) PrepareMCPOAuthDeleteRecovery(ctx context.Context, raw sc.MCPOAuthDeleteContinuation) (sc.PreparedMCPOAuthDeleteRecovery, error) {
	continuation, ok := raw.(*mcpOAuthDeleteContinuation)
	if ctx == nil || !ok || continuation == nil {
		return nil, failure(PreparationRequired, f.ResourceBusy, nil)
	}
	p, err := w.ownedOAuthDeletePlan(continuation.plan)
	if err != nil {
		return nil, err
	}
	// Require the same still-live private domain context before reserving any
	// nonce. The final transaction rechecks the original receipt and version.
	current, err := w.DiscoverMCPOAuthWrite(ctx, p.request)
	if err != nil {
		return nil, err
	}
	if current.Binding() != p.dependencies.Binding() || current.Mapping() != p.dependencies.Mapping() || !slices.EqualFunc(current.Locks(), p.dependencies.Locks(), func(a, b f.LockRequest) bool {
		return a.Key.Canonical() == b.Key.Canonical() && a.Mode == b.Mode
	}) || !continuation.used.CompareAndSwap(false, true) {
		return nil, failure(PreparationRequired, f.ResourceBusy, nil)
	}
	sealed, err := w.PrepareMCPOAuthWrite(ctx, p.request, p.dependencies)
	if err != nil {
		return nil, err
	}
	prepared, err := w.owned(sealed)
	if err != nil {
		return nil, err
	}
	return &preparedMCPOAuthDeleteRecovery{plan: p, write: prepared}, nil
}

type mcpOAuthCleanupAuditKey struct{}
type mcpOAuthCleanupAuditWitness struct {
	plan   *mcpOAuthDeletePlan
	write  *preparedMCPOAuthWrite
	tx     f.Tx
	active atomic.Bool
}

func (w *MCPOAuthWriter) ApplyMCPOAuthDeleteRecoveryInTx(ctx context.Context, tx f.Tx, raw sc.PreparedMCPOAuthDeleteRecovery) (sc.MCPOAuthWriteObservation, error) {
	var empty sc.MCPOAuthWriteObservation
	p, ok := raw.(*preparedMCPOAuthDeleteRecovery)
	if !ok || p == nil {
		return empty, failure(PreparationRequired, f.ResourceBusy, nil)
	}
	plan, err := w.ownedOAuthDeletePlan(p.plan)
	if err != nil {
		return empty, err
	}
	prepared, err := w.owned(p.write)
	if err != nil || !sameMCPOAuthRecoveryRequest(prepared.write.mcpOAuth.request, plan.request) {
		return empty, failure(PreparationRequired, f.ResourceBusy, nil)
	}
	observation, err := w.ObserveMCPOAuthDeleteRecoveryInTx(ctx, tx, plan)
	if err != nil {
		return empty, err
	}
	if observation.Found {
		return sc.MCPOAuthWriteObservation{Found: true, Result: observation.Result}, nil
	}
	if observation.Continuation == nil {
		return empty, failure(PreparationRequired, f.ResourceBusy, nil)
	}
	proof := &mcpOAuthCleanupAuditWitness{plan: plan, write: prepared, tx: tx}
	proof.active.Store(true)
	defer proof.active.Store(false)
	ctx = context.WithValue(ctx, mcpOAuthCleanupAuditKey{}, proof)
	// Reuse the original mutation engine, Secret receipt and Audit transaction.
	// Only the proven-not-applied original Delete reaches this call.
	return w.ApplyMCPOAuthWriteInTx(ctx, tx, prepared)
}

func (a *ProjectAuditAuthority) CheckMCPOAuthCleanupAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) (bool, error) {
	if ctx == nil || ctx.Value(mcpOAuthCleanupAuditKey{}) == nil {
		return false, nil
	}
	w, ok := ctx.Value(mcpOAuthCleanupAuditKey{}).(*mcpOAuthCleanupAuditWitness)
	if !ok || w == nil || !w.active.Load() || w.plan == nil || w.plan.owner == nil || w.plan.owner.service == nil || w.write == nil || w.write.owner != w.plan.owner || w.write.write.mcpOAuth == nil || !sameMCPOAuthRecoveryRequest(w.write.write.mcpOAuth.request, w.plan.request) || w.tx != tx || a == nil || a.data == nil || !sameProjectAuditStore(a.data(), w.plan.owner.service.state().store) {
		return true, projectAuditDenied()
	}
	original, ok := ctx.Value(mcpOAuthAuditWitnessKey{}).(mcpOAuthAuditWitness)
	if !ok || original.receiptID != w.write.write.receiptID || !sameMCPOAuthRecoveryRequest(original.request, w.plan.request) {
		return true, projectAuditDenied()
	}
	v := w.plan.request.Fields()
	e := entry.Fields()
	if sc.ValidateMCPOAuthDeleteRecoveryRequest(w.plan.request) != nil || e.Action != ac.SecretDelete || !e.Actor.Equal(v.Actor) || e.Scope.Details().ProjectID != v.ProjectID.String() || e.Resource.Details().ID != v.Ref.Details().ID.String() {
		return true, projectAuditDenied()
	}
	if _, err := w.plan.require(ctx, tx); err != nil {
		return true, err
	}
	// The ordinary checker verifies the actual same-Tx Delete postimage,
	// original command receipt, private write witness and exact full tuple.
	return true, a.CheckProjectAuditInTx(ctx, tx, entry, key)
}

func sameMCPOAuthRecoveryRequest(a, b sc.MCPOAuthWriteRequest) bool {
	left, err := sc.MCPOAuthWriteBinding(a)
	right, otherErr := sc.MCPOAuthWriteBinding(b)
	return err == nil && otherErr == nil && left == right
}

var _ sc.MCPOAuthDeleteRecovery = (*MCPOAuthWriter)(nil)
var _ sc.MCPSecretCleanupAuditAuthority = (*ProjectAuditAuthority)(nil)
