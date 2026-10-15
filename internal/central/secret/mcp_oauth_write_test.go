package secret

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

func newMCPOAuthAuditFixture(t *testing.T, kind sc.MutationKind) *projectAuditMutationFixture {
	t.Helper()
	v := newProjectAuditMutation(t, kind, false)
	project, _ := f.ParseID[id.Project](v.p.scope.Details().ProjectID)
	connection := projectAuditID[struct{}](t).String()
	reg, _ := id.RegisterService(id.SecretService)
	v.p.actor, _ = reg.Actor(projectAuditID[struct{}](t).String(), v.p.scope)
	v.p.command, _ = f.NewCommandIdentity("secret.mcp", []string{project.String(), connection, connection}, string(kind), "oauth-test-command")
	v.p.purpose = sc.MCP
	v.result.Metadata.Purpose = sc.MCP
	if kind != sc.Create {
		v.prior.Purpose = sc.MCP
	}
	material, _ := sc.NewSecretMaterial([]byte("oauth-private-test-material"))
	defer material.Destroy()
	fields := sc.MCPOAuthWriteFields{Actor: v.p.actor, Identity: v.p.command, ProjectID: project, ConnectionID: connection, ConfigRevision: 1, Kind: kind, ExpectedVersion: v.p.expected, OwnerKind: sc.MCPOAuthToken, OwnerID: connection, Value: material}
	if kind != sc.Create {
		fields.Ref = v.p.ref
	}
	request, err := sc.NewMCPOAuthWriteRequest(fields)
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := sc.MCPOAuthWriteBinding(request)
	owner, _ := f.RecordLock(f.ReferenceRecordLock, "oauth-fixture:"+connection)
	plan, _ := sc.NewWriteDependencies(sc.NewPlanIssuer(), binding, binding, []f.LockRequest{{Key: owner, Mode: f.Exclusive}})
	v.p.mcpOAuth = &mcpOAuthWriteAuthorization{request: request.WithoutMaterial(), dependencies: plan}
	entry := v.entry.Fields()
	entry.Actor = v.p.actor
	v.entry, err = ac.NewEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	v.key, _ = audit.CommandAppendKey(ac.SecretProducer, v.p.command, 0)
	v.store.locks, _ = preparedLocks(v.p)
	ctx := mcpOAuthMutationAuditContext(context.Background(), v.store, v.store.tx, v.p)
	v.ctx = mutationAuditContext(ctx, v.store, v.store.tx, v.p, v.prior, v.result, v.entry, v.key)
	return v
}
func TestMCPOAuthAuditRequiresPrivateCallAndCanonicalFacts(t *testing.T) {
	for _, kind := range []sc.MutationKind{sc.Create, sc.Update, sc.Delete} {
		t.Run(string(kind), func(t *testing.T) {
			v := newMCPOAuthAuditFixture(t, kind)
			if err := v.checker.CheckProjectAuditInTx(v.ctx, v.store.tx, v.entry, v.key); err != nil {
				t.Fatal(err)
			}
			// Generic service DTO and the ordinary mutation witness do not grant OAuth authority.
			ctx := mutationAuditContext(context.Background(), v.store, v.store.tx, v.p, v.prior, v.result, v.entry, v.key)
			projectAuditCode(t, v.checker.CheckProjectAuditInTx(ctx, v.store.tx, v.entry, v.key), f.Forbidden)
			for _, change := range []func(*mcpOAuthAuditWitness){func(p *mcpOAuthAuditWitness) { p.tx = f.NewTx() }, func(p *mcpOAuthAuditWitness) { p.receiptID = projectAuditID[receiptMarker](t) }, func(p *mcpOAuthAuditWitness) { p.store = &projectAuditUnitStore{} }, func(p *mcpOAuthAuditWitness) { p.request = sc.MCPOAuthWriteRequest{} }} {
				p := v.ctx.Value(mcpOAuthAuditWitnessKey{}).(mcpOAuthAuditWitness)
				change(&p)
				projectAuditCode(t, v.checker.CheckProjectAuditInTx(context.WithValue(v.ctx, mcpOAuthAuditWitnessKey{}, p), v.store.tx, v.entry, v.key), f.Forbidden)
			}
			locks := v.store.locks
			v.store.locks = locks[:len(locks)-1]
			projectAuditCode(t, v.checker.CheckProjectAuditInTx(v.ctx, v.store.tx, v.entry, v.key), f.DependencyUnavailable)
			v.store.locks = locks
			read := v.store.read
			v.store.read = func(sql string, args []any) postgres.Row {
				row := read(sql, args)
				r := row.(projectAuditUnitRow)
				if len(r.values) == 9 {
					r.values[2] = "wrong-command"
				}
				return r
			}
			projectAuditCode(t, v.checker.CheckProjectAuditInTx(v.ctx, v.store.tx, v.entry, v.key), f.Forbidden)
		})
	}
	// A private MCP marker must never turn a failing Human check into a fallback grant.
	human := newProjectAuditMutation(t, sc.Create, false)
	projectAuditCode(t, human.checker.CheckProjectAuditInTx(context.WithValue(human.ctx, mcpOAuthAuditWitnessKey{}, mcpOAuthAuditWitness{}), human.store.tx, human.entry, human.key), f.Forbidden)
}

type mcpOAuthUnitAuthority struct {
	plan          sc.WriteDependencies
	writeErr      error
	writes, reads int
}

func (a *mcpOAuthUnitAuthority) DiscoverMCPOAuthWrite(context.Context, sc.MCPOAuthWriteRequest) (sc.WriteDependencies, error) {
	return a.plan, nil
}
func (a *mcpOAuthUnitAuthority) ValidateMCPOAuthWriteInTx(context.Context, f.Tx, sc.MCPOAuthWriteRequest, sc.WriteDependencies) error {
	a.writes++
	return a.writeErr
}
func (a *mcpOAuthUnitAuthority) ValidateMCPOAuthWriteObservationInTx(context.Context, f.Tx, sc.MCPOAuthWriteRequest, sc.WriteDependencies) error {
	a.reads++
	return nil
}

type foreignMCPOAuthPrepared struct{}

func (foreignMCPOAuthPrepared) Ref() sc.CredentialRef          { return sc.CredentialRef{} }
func (foreignMCPOAuthPrepared) RequiredLocks() []f.LockRequest { return nil }
func TestMCPOAuthObservationCannotMutateAndHandlesAreIssuerBound(t *testing.T) {
	v := newMCPOAuthAuditFixture(t, sc.Create)
	state := &serviceState{store: v.store}
	service := &Service{data: func() *serviceState { return state }}
	authority := &mcpOAuthUnitAuthority{plan: v.p.mcpOAuth.dependencies, writeErr: f.NewFault(f.Forbidden, f.NotStarted)}
	writer, err := NewMCPOAuthWrites(service, authority)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := writer.state()
	v.p.issuer = state
	v.p.mcpOAuth.owner = owner
	p := &preparedMCPOAuthWrite{owner, v.p}
	v.store.read = func(string, []any) postgres.Row { return projectAuditUnitRow{err: pgx.ErrNoRows} }
	if _, err = writer.ApplyMCPOAuthWriteInTx(context.Background(), v.store.tx, p); err == nil {
		t.Fatal("canceled owner mutated")
	}
	if v.store.queries != 0 || authority.writes != 1 {
		t.Fatal("authorization did not precede ledger")
	}
	observation, err := writer.ObserveMCPOAuthWriteInTx(context.Background(), v.store.tx, p)
	if err != nil || observation.Found || authority.reads != 1 || authority.writes != 1 || v.store.queries != 1 {
		t.Fatal("read-only original observation did not remain absent")
	}
	other, _ := NewMCPOAuthWrites(service, authority)
	for _, raw := range []sc.PreparedMCPOAuthWrite{nil, foreignMCPOAuthPrepared{}, (*preparedMCPOAuthWrite)(nil)} {
		if _, err := writer.ObserveMCPOAuthWriteInTx(context.Background(), v.store.tx, raw); err == nil {
			t.Fatal("foreign handle accepted")
		}
	}
	if _, err := other.ObserveMCPOAuthWriteInTx(context.Background(), v.store.tx, p); err == nil {
		t.Fatal("another wrapper accepted original handle")
	}
	v.store.locks = nil
	if _, err := writer.ObserveMCPOAuthWriteInTx(context.Background(), v.store.tx, p); err == nil {
		t.Fatal("unlocked observation")
	}
	if authority.reads != 1 {
		t.Fatal("lock failure entered authority")
	}
	if _, err := service.ApplyPreparedWriteInTx(context.Background(), v.store.tx, PreparedWrite{func() preparedWrite { return v.p }}); err == nil {
		t.Fatal("OAuth handle entered Human port")
	}
}
