package mcp

import (
	"context"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type rejectingRetirementAuthority struct{ calls int }

func (a *rejectingRetirementAuthority) CheckMCPRetirementPlan(context.Context, tc.MCPExecutionRetirementRequest) error {
	a.calls++
	return fail(f.Forbidden)
}
func (a *rejectingRetirementAuthority) CheckMCPRetirementInTx(context.Context, f.Tx, tc.MCPExecutionRetirementRequest) error {
	a.calls++
	return fail(f.Forbidden)
}

func retirementRequest(t *testing.T) tc.MCPExecutionRetirementRequest {
	t.Helper()
	project, _ := f.ParseID[id.Project]("01960aa5-0286-7000-8000-000000000001")
	agent, _ := f.ParseID[id.Agent]("01960aa5-0286-7000-8000-000000000002")
	execution, _ := f.ParseID[id.Execution]("01960aa5-0286-7000-8000-000000000003")
	r := tc.MCPExecutionRetirementRequest{ProjectID: project, AgentID: agent, ExecutionID: execution, TerminalVersion: 7}
	if err := r.Validate(); err != nil {
		t.Fatal("invalid test retirement identity")
	}
	return r
}

func TestMCPRetirementPublicTupleCannotDiscoverOrRelease(t *testing.T) {
	request := retirementRequest(t)
	authority := &rejectingRetirementAuthority{}
	// A nil Store intentionally makes any attempted protected SQL observable.
	service := &Service{}
	provider := &ExecutionRetirement{service: service, authority: authority}
	if _, err := provider.DiscoverMCPExecutionRetirement(context.Background(), request); err == nil || authority.calls != 1 {
		t.Fatal("public terminal tuple bypassed the original Execution owner")
	}
	if err := provider.ReleaseMCPExecutionInTx(context.Background(), f.Tx{}, request, nil); err == nil {
		t.Fatal("release accepted a public request without its original private plan")
	}
	foreign := &executionRetirementPlan{owner: &ExecutionRetirement{service: service}, request: request}
	if _, err := provider.MCPExecutionRetiredInTx(context.Background(), f.Tx{}, request, foreign); err == nil {
		t.Fatal("read-only confirmation accepted another provider's plan")
	}
	if _, err := NewExecutionRetirement(service, authority); err == nil {
		t.Fatal("retirement constructed without the Secret release and observation ports")
	}
}

func TestMCPRetirementSecretProofExpiresAndBindsOriginalLease(t *testing.T) {
	request := retirementRequest(t)
	lease, _ := f.ParseID[sc.Lease]("01960aa5-0286-7000-8000-000000000004")
	row := executionRuntimeLease{RuntimeBindingID: "01960aa5-0286-7000-8000-000000000005", ConnectionID: "01960aa5-0286-7000-8000-000000000006", CredentialID: "01960aa5-0286-7000-8000-000000000007", LeaseID: lease}
	entry, err := newExecutionRetirementEntry(request, []executionRuntimeLease{row}, row)
	if err != nil {
		t.Fatal("prepare retirement test usage")
	}
	service := &Service{secretIssuer: sc.NewPlanIssuer()}
	provider := &ExecutionRetirement{service: service, authority: &rejectingRetirementAuthority{}}
	plan := &executionRetirementPlan{owner: provider, request: request, rows: []executionRuntimeLease{row}, entries: []*executionRetirementEntry{entry}}
	w := &executionRetirementUse{plan: plan, entry: entry, active: true}
	ctx := context.WithValue(context.Background(), executionRetirementKey{}, w)
	if _, err = service.DiscoverUsage(context.Background(), entry.usage); err == nil {
		t.Fatal("plain UsageRequest became a release grant")
	}
	if _, err = service.executionRetirementUse(ctx, entry.usage); err != nil {
		t.Fatal("original active witness did not identify its own usage")
	}
	changed := entry.usage
	changed.LeaseID, _ = f.ParseID[sc.Lease]("01960aa5-0286-7000-8000-000000000008")
	if _, err = service.executionRetirementUse(ctx, changed); err == nil {
		t.Fatal("original witness authorized a substituted lease")
	}
	if _, err = service.AuthorizeLeaseInTx(ctx, f.Tx{}, entry.usage.Actor, entry.usage.Ref, entry.usage.LeaseOwner, sc.ReleaseLease); err == nil {
		t.Fatal("unvalidated private witness became a same-transaction release grant")
	}
	w.close()
	if _, err = service.executionRetirementUse(ctx, entry.usage); err == nil {
		t.Fatal("returned original callback left its release witness usable")
	}
}
