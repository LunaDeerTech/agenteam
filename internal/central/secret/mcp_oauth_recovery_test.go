package secret

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type mcpDeleteTestKey struct{}
type mcpDeleteTestAuthority struct {
	mcpOAuthUnitAuthority
	checks int
	err    error
}

func (a *mcpDeleteTestAuthority) DiscoverMCPOAuthWrite(ctx context.Context, _ sc.MCPOAuthWriteRequest) (sc.WriteDependencies, error) {
	if ctx == nil || ctx.Value(mcpDeleteTestKey{}) != a {
		return sc.WriteDependencies{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	return a.plan, nil
}
func (a *mcpDeleteTestAuthority) ValidateMCPOAuthDeleteRecoveryInTx(ctx context.Context, _ f.Tx, _ sc.MCPOAuthWriteRequest, _ sc.WriteDependencies) error {
	a.checks++
	if ctx.Value(mcpDeleteTestKey{}) != a {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return a.err
}

type mcpDeleteTestFixture struct {
	base      *projectAuditMutationFixture
	writer    *MCPOAuthWriter
	plan      *mcpOAuthDeletePlan
	authority *mcpDeleteTestAuthority
	ctx       context.Context
}

func newMCPDeleteTest(t *testing.T) *mcpDeleteTestFixture {
	t.Helper()
	v := newMCPOAuthAuditFixture(t, sc.Delete)
	fields := v.p.mcpOAuth.request.Fields()
	fields.OwnerKind = sc.MCPOAuthFlow
	r, err := sc.NewMCPOAuthWriteRequest(fields)
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := sc.MCPOAuthWriteBinding(r)
	deps, err := sc.NewWriteDependencies(sc.NewPlanIssuer(), binding, binding, v.p.mcpOAuth.dependencies.Locks())
	if err != nil {
		t.Fatal(err)
	}
	a := &mcpDeleteTestAuthority{mcpOAuthUnitAuthority: mcpOAuthUnitAuthority{plan: deps}}
	state := &serviceState{store: v.store, keys: testKeys(t)}
	service := &Service{data: func() *serviceState { return state }}
	writer, err := NewMCPOAuthWrites(service, a)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), mcpDeleteTestKey{}, a)
	raw, err := writer.DiscoverMCPOAuthDeleteRecovery(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	p := raw.(*mcpOAuthDeletePlan)
	v.store.locks = p.RequiredLocks()
	return &mcpDeleteTestFixture{v, writer, p, a, ctx}
}

type foreignMCPDeleteHandle struct{}

func (foreignMCPDeleteHandle) RequiredLocks() []f.LockRequest { return nil }

func TestMCPOAuthDeleteRecoveryObservationNeedsOriginalAuthorityAndVersion(t *testing.T) {
	v := newMCPDeleteTest(t)
	store := v.base.store
	metadata := true
	purpose, version := string(sc.MCP), int64(7)
	store.read = func(sql string, _ []any) postgres.Row {
		switch {
		case strings.Contains(sql, "secret_command_receipts"):
			return projectAuditUnitRow{err: pgx.ErrNoRows}
		case strings.Contains(sql, "FROM agenteam_secret.secrets"):
			if !metadata {
				return projectAuditUnitRow{err: pgx.ErrNoRows}
			}
			return projectAuditUnitRow{values: []any{purpose, version, projectAuditID[payloadMarker](t).String()}}
		default:
			t.Fatal("observation accessed more than original receipt/metadata")
			return projectAuditUnitRow{}
		}
	}
	observation, err := v.writer.ObserveMCPOAuthDeleteRecoveryInTx(v.ctx, store.tx, v.plan)
	if err != nil || observation.Found || observation.Continuation == nil || store.queries != 2 {
		t.Fatal("exact stopped original Delete did not produce a continuation")
	}
	// These paths do not prepare a nonce, apply a mutation, or expose material.
	// The store intentionally has no usable Exec or transaction opener.
	for _, raw := range []sc.MCPOAuthDeleteRecoveryPlan{nil, foreignMCPDeleteHandle{}, (*mcpOAuthDeletePlan)(nil)} {
		if _, err := v.writer.ObserveMCPOAuthDeleteRecoveryInTx(v.ctx, store.tx, raw); err == nil {
			t.Fatal("forged plan accepted")
		}
	}
	owner, _ := v.writer.state()
	other, _ := NewMCPOAuthWrites(owner.service, v.authority)
	if _, err := other.ObserveMCPOAuthDeleteRecoveryInTx(v.ctx, store.tx, v.plan); err == nil {
		t.Fatal("foreign writer accepted the original plan")
	}
	queries := store.queries
	if _, err := v.writer.ObserveMCPOAuthDeleteRecoveryInTx(context.Background(), store.tx, v.plan); err == nil || store.queries != queries {
		t.Fatal("bare DTO/context reached the ledger")
	}
	locks := store.locks
	store.locks = nil
	if _, err := v.writer.ObserveMCPOAuthDeleteRecoveryInTx(v.ctx, store.tx, v.plan); err == nil || store.queries != queries {
		t.Fatal("missing original locks reached the ledger")
	}
	store.locks = locks
	for _, change := range []func(){func() { metadata = false }, func() { metadata, version = true, 8 }, func() { version, purpose = 7, string(sc.Model) }} {
		change()
		result, err := v.writer.ObserveMCPOAuthDeleteRecoveryInTx(v.ctx, store.tx, v.plan)
		if err == nil || result.Found || result.Continuation != nil {
			t.Fatal("receipt absence or changed current credential authorized continuation")
		}
	}
	for _, raw := range []sc.MCPOAuthDeleteContinuation{nil, foreignMCPDeleteHandle{}, (*mcpOAuthDeleteContinuation)(nil)} {
		if _, err := v.writer.PrepareMCPOAuthDeleteRecovery(v.ctx, raw); err == nil {
			t.Fatal("forged continuation entered preparation")
		}
	}
	if _, err := other.PrepareMCPOAuthDeleteRecovery(v.ctx, observation.Continuation); err == nil {
		t.Fatal("another writer prepared original continuation")
	}
	if _, err := v.writer.PrepareMCPOAuthDeleteRecovery(context.Background(), observation.Continuation); err == nil {
		t.Fatal("expired private context entered preparation")
	}
}

func TestMCPOAuthDeleteRecoveryReadsCanonicalReceiptWithoutPreparing(t *testing.T) {
	v := newMCPDeleteTest(t)
	p := v.plan
	binding, _ := sc.MCPOAuthWriteBinding(p.request)
	digest := sha256.Sum256([]byte(binding))
	nonce, _ := masterNonce(1)
	receiptID := projectAuditID[receiptMarker](t)
	sealed, err := seal(p.owner.service.state().keys, 2, nonce, p.request.Fields().Ref.Details().Scope, receiptOwner, receiptID.String(), projectAuditID[payloadMarker](t), digest[:])
	if err != nil {
		t.Fatal(err)
	}
	kind, present := string(sc.Delete), false
	v.base.store.read = func(sql string, _ []any) postgres.Row {
		switch {
		case strings.Contains(sql, "secret_command_receipts"):
			return projectAuditUnitRow{values: []any{receiptID.String(), p.request.Fields().Ref.Details().ID.String(), string(sc.MCP), kind, int64(8), true, sealed.id.String()}}
		case strings.Contains(sql, "secret_payloads"):
			return projectAuditUnitRow{values: []any{sealed.id.String(), "project", sealed.scope.Details().ProjectID, int16(sealed.ownerKind), sealed.ownerID, sealed.format, sealed.algorithm, sealed.dataNonce, sealed.ciphertext, sealed.wrapNonce, sealed.wrappedDEK, int64(sealed.masterVersion), int64(sealed.wrapRevision)}}
		case strings.Contains(sql, "SELECT EXISTS"):
			return projectAuditUnitRow{values: []any{present}}
		default:
			t.Fatal("receipt observation opened a value or prepared/wrote")
			return projectAuditUnitRow{}
		}
	}
	result, err := v.writer.ObserveMCPOAuthDeleteRecoveryInTx(v.ctx, v.base.store.tx, p)
	if err != nil || !result.Found || !result.Result.Deleted || result.Result.Metadata.Version != 8 || result.Continuation != nil {
		t.Fatal("canonical completed original Delete not observed")
	}
	for _, change := range []func(){func() { kind = string(sc.Update) }, func() { kind, present = string(sc.Delete), true }, func() { present = false; sealed.ciphertext[0] ^= 1 }} {
		change()
		result, err := v.writer.ObserveMCPOAuthDeleteRecoveryInTx(v.ctx, v.base.store.tx, p)
		if err == nil || result.Found || result.Continuation != nil {
			t.Fatal("noncanonical receipt authorized recovery")
		}
	}
	encoded, err := json.Marshal(p)
	if err != nil || strings.Contains(string(encoded), p.request.Fields().Ref.Details().ID.String()) {
		t.Fatal("opaque original plan serialized its identity")
	}
}

func TestMCPOAuthDeleteRecoveryPrivateAuditScopeCannotEscape(t *testing.T) {
	v := newMCPDeleteTest(t)
	base, p := v.base, v.plan
	base.p.issuer = p.owner.service.state()
	base.p.mcpOAuth = &mcpOAuthWriteAuthorization{owner: p.owner, request: p.request, dependencies: p.dependencies}
	write := &preparedMCPOAuthWrite{owner: p.owner, write: base.p}
	w := &mcpOAuthCleanupAuditWitness{plan: p, write: write, tx: base.store.tx}
	w.active.Store(true)
	ctx := context.WithValue(v.ctx, mcpOAuthCleanupAuditKey{}, w)
	ctx = mcpOAuthMutationAuditContext(ctx, base.store, base.store.tx, base.p)
	ctx = mutationAuditContext(ctx, base.store, base.store.tx, base.p, base.prior, base.result, base.entry, base.key)
	if matched, err := base.checker.CheckMCPOAuthCleanupAuditInTx(ctx, base.store.tx, base.entry, base.key); !matched || err != nil {
		t.Fatal("same-Tx private canonical cleanup audit refused")
	}
	if matched, err := base.checker.CheckMCPOAuthCleanupAuditInTx(context.Background(), base.store.tx, base.entry, base.key); matched || err != nil {
		t.Fatal("bare Service Delete selected cleanup gate")
	}
	if matched, err := base.checker.CheckMCPOAuthCleanupAuditInTx(ctx, f.NewTx(), base.entry, base.key); !matched || err == nil {
		t.Fatal("foreign Tx obtained cleanup gate")
	}
	refused := errors.New("original stopped claim refused")
	v.authority.err = refused
	if _, err := base.checker.CheckMCPOAuthCleanupAuditInTx(ctx, base.store.tx, base.entry, base.key); !errors.Is(err, refused) {
		t.Fatal("private recovery authority failure was hidden")
	}
	v.authority.err = nil
	w.active.Store(false)
	if matched, err := base.checker.CheckMCPOAuthCleanupAuditInTx(ctx, base.store.tx, base.entry, base.key); !matched || err == nil {
		t.Fatal("returned mutation witness remained usable")
	}
}
