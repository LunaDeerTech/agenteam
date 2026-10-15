package secret

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// MCPOAuthWriter binds the real MCP authority once. It does not grant authority
// to callers supplying an actor or public request.
type MCPOAuthWriter struct{ data func() *mcpOAuthWriterState }
type mcpOAuthWriterState struct {
	service   *Service
	authority sc.MCPOAuthWriteAuthority
}

func NewMCPOAuthWrites(s *Service, a sc.MCPOAuthWriteAuthority) (*MCPOAuthWriter, error) {
	if s == nil || s.data == nil || s.state() == nil || nilPort(a) {
		return nil, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	state := &mcpOAuthWriterState{s, a}
	return &MCPOAuthWriter{func() *mcpOAuthWriterState { return state }}, nil
}
func (w *MCPOAuthWriter) state() (*mcpOAuthWriterState, error) {
	if w == nil || w.data == nil || w.data() == nil {
		return nil, failure(AuthorizationUnbound, f.DependencyUnbound, nil)
	}
	return w.data(), nil
}
func validMCPOAuthPlan(r sc.MCPOAuthWriteRequest, p sc.WriteDependencies) bool {
	b, e := sc.MCPOAuthWriteBinding(r)
	return e == nil && p.Validate() == nil && p.Binding() == b && len(p.Locks()) > 0
}
func (w *MCPOAuthWriter) DiscoverMCPOAuthWrite(ctx context.Context, r sc.MCPOAuthWriteRequest) (sc.WriteDependencies, error) {
	state, e := w.state()
	if e != nil {
		return sc.WriteDependencies{}, e
	}
	if r.Validate() != nil {
		return sc.WriteDependencies{}, invalid()
	}
	p, e := state.authority.DiscoverMCPOAuthWrite(ctx, r.WithoutMaterial())
	if e != nil {
		return sc.WriteDependencies{}, plannedAuthorization(e)
	}
	if !validMCPOAuthPlan(r, p) {
		return sc.WriteDependencies{}, invalid()
	}
	return p, nil
}

type mcpOAuthWriteAuthorization struct {
	owner        *mcpOAuthWriterState
	request      sc.MCPOAuthWriteRequest
	dependencies sc.WriteDependencies
}

func (p *mcpOAuthWriteAuthorization) validate(ctx context.Context, tx f.Tx) error {
	if p == nil || p.owner == nil || !validMCPOAuthPlan(p.request, p.dependencies) {
		return invalid()
	}
	if err := p.owner.authority.ValidateMCPOAuthWriteInTx(ctx, tx, p.request, p.dependencies); err != nil {
		return plannedAuthorization(err)
	}
	return nil
}

type preparedMCPOAuthWrite struct {
	owner *mcpOAuthWriterState
	write preparedWrite
}

func (p *preparedMCPOAuthWrite) Ref() sc.CredentialRef {
	if p == nil {
		return sc.CredentialRef{}
	}
	return p.write.ref
}
func (p *preparedMCPOAuthWrite) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	locks, _ := preparedLocks(p.write)
	return locks
}
func (*preparedMCPOAuthWrite) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "prepared_mcp_oauth_secret_write")
}
func (*preparedMCPOAuthWrite) MarshalJSON() ([]byte, error) {
	return []byte(`"prepared_mcp_oauth_secret_write"`), nil
}
func (*preparedMCPOAuthWrite) UnmarshalJSON([]byte) error { return invalid() }
func (*preparedMCPOAuthWrite) LogValue() slog.Value {
	return slog.StringValue("prepared_mcp_oauth_secret_write")
}
func (w *MCPOAuthWriter) PrepareMCPOAuthWrite(ctx context.Context, r sc.MCPOAuthWriteRequest, plan sc.WriteDependencies) (sc.PreparedMCPOAuthWrite, error) {
	state, e := w.state()
	if e != nil {
		return nil, e
	}
	if !validMCPOAuthPlan(r, plan) {
		return nil, invalid()
	}
	v := r.Fields()
	scope, e := id.InProject(v.ProjectID)
	if e != nil {
		return nil, invalid()
	}
	binding, _ := sc.MCPOAuthWriteBinding(r)
	p, e := state.service.prepareWriteBound(ctx, sc.WriteRequest{Actor: v.Actor, Scope: scope, Identity: v.Identity, Kind: v.Kind, Ref: v.Ref, ExpectedVersion: v.ExpectedVersion, Purpose: sc.MCP, Value: v.Value}, sc.ServiceWriteRequest{}, sc.WriteDependencies{}, binding)
	if e != nil {
		return nil, e
	}
	sealed := p.data()
	sealed.mcpOAuth = &mcpOAuthWriteAuthorization{state, r.WithoutMaterial(), plan}
	return &preparedMCPOAuthWrite{state, sealed}, nil
}
func (w *MCPOAuthWriter) owned(raw sc.PreparedMCPOAuthWrite) (*preparedMCPOAuthWrite, error) {
	state, e := w.state()
	if e != nil {
		return nil, e
	}
	p, ok := raw.(*preparedMCPOAuthWrite)
	if !ok || p == nil || p.owner != state || p.write.issuer != state.service.state() || p.write.mcpOAuth == nil || p.write.mcpOAuth.owner != state {
		return nil, failure(PreparationRequired, f.ResourceBusy, nil)
	}
	return p, nil
}
func (w *MCPOAuthWriter) ApplyMCPOAuthWriteInTx(ctx context.Context, tx f.Tx, raw sc.PreparedMCPOAuthWrite) (sc.MCPOAuthWriteObservation, error) {
	p, e := w.owned(raw)
	if e != nil {
		return sc.MCPOAuthWriteObservation{}, e
	}
	result, e := p.owner.service.applyWriteInTx(ctx, tx, p.write)
	if e != nil {
		return sc.MCPOAuthWriteObservation{}, e
	}
	return sc.MCPOAuthWriteObservation{Found: true, Result: result}, nil
}

// Observe never prepares material, invokes Apply, or mutates the receipt/value
// ledger. The original sealed handle and complete locks identify one command.
func (w *MCPOAuthWriter) ObserveMCPOAuthWriteInTx(ctx context.Context, tx f.Tx, raw sc.PreparedMCPOAuthWrite) (sc.MCPOAuthWriteObservation, error) {
	p, e := w.owned(raw)
	if e != nil {
		return sc.MCPOAuthWriteObservation{}, e
	}
	state := p.owner.service.state()
	x, e := state.store.InTx(tx)
	if e != nil {
		return sc.MCPOAuthWriteObservation{}, unavailable(e)
	}
	if e = state.store.RequireHeldLocks(ctx, tx, p.RequiredLocks()); e != nil {
		return sc.MCPOAuthWriteObservation{}, unavailable(e)
	}
	auth := p.write.mcpOAuth
	if e = auth.owner.authority.ValidateMCPOAuthWriteObservationInTx(ctx, tx, auth.request, auth.dependencies); e != nil {
		return sc.MCPOAuthWriteObservation{}, plannedAuthorization(e)
	}
	result, found, e := p.owner.service.findReceipt(ctx, x, p.write)
	if e != nil {
		return sc.MCPOAuthWriteObservation{}, e
	}
	if found && (!result.Metadata.CredentialRef.Equal(p.Ref()) || result.Metadata.Purpose != sc.MCP) {
		return sc.MCPOAuthWriteObservation{}, failure(Busy, f.ResourceBusy, nil)
	}
	return sc.MCPOAuthWriteObservation{Found: found, Result: result}, nil
}

// This proof is created only after the exact MCP authority accepted the write
// and the shared engine wrote its canonical postimage and receipt in this Tx.
// It retains neither SecretMaterial nor sealed envelopes, a Service or keys.
type mcpOAuthAuditWitnessKey struct{}
type mcpOAuthAuditWitness struct {
	store        Store
	tx           f.Tx
	request      sc.MCPOAuthWriteRequest
	dependencies sc.WriteDependencies
	ref          sc.CredentialRef
	receiptID    f.ID[receiptMarker]
}

func (mcpOAuthAuditWitness) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "mcp_oauth_secret_audit_witness")
}
func mcpOAuthMutationAuditContext(ctx context.Context, store Store, tx f.Tx, p preparedWrite) context.Context {
	if p.mcpOAuth == nil {
		return ctx
	}
	return context.WithValue(ctx, mcpOAuthAuditWitnessKey{}, mcpOAuthAuditWitness{store, tx, p.mcpOAuth.request, p.mcpOAuth.dependencies, p.ref, p.receiptID})
}
func checkMCPOAuthMutationWitness(ctx context.Context, store Store, tx f.Tx, w projectMutationAudit) error {
	proof, ok := ctx.Value(mcpOAuthAuditWitnessKey{}).(mcpOAuthAuditWitness)
	if !ok || !sameProjectAuditStore(store, proof.store) || proof.tx != tx || !validMCPOAuthPlan(proof.request, proof.dependencies) || !proof.ref.Equal(w.ref) || proof.receiptID != w.receiptID {
		return projectAuditDenied()
	}
	v := proof.request.Fields()
	if !v.Actor.Equal(w.actor) || v.Identity.Canonical() != w.command.Canonical() || v.Kind != w.kind || v.ExpectedVersion != w.expected || w.purpose != sc.MCP || w.ref.Details().Scope.Details().Kind != id.ProjectScope || w.ref.Details().Scope.Details().ProjectID != v.ProjectID.String() || v.Kind != sc.Create && !v.Ref.Equal(w.ref) {
		return projectAuditDenied()
	}
	if e := store.RequireHeldLocks(ctx, tx, proof.dependencies.Locks()); e != nil {
		return unavailable(e)
	}
	return nil
}

var _ sc.MCPOAuthWrites = (*MCPOAuthWriter)(nil)
