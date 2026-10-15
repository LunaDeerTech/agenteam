package execution

import (
	"context"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type mcpRetirementControl struct {
	authority              *Authority
	plan                   *executionRecoveryRetirementPlan
	request                tc.MCPExecutionRetirementRequest
	ctx                    context.Context
	releases, observations int
	reject                 bool
}

func (p *mcpRetirementControl) DiscoverMCPExecutionRetirement(ctx context.Context, r tc.MCPExecutionRetirementRequest) (tc.MCPExecutionRetirementPlan, error) {
	if err := p.authority.CheckMCPRetirementPlan(ctx, r); err != nil {
		return nil, err
	}
	p.request, p.ctx = r, ctx
	return p.plan, nil
}
func (p *mcpRetirementControl) check(ctx context.Context, tx f.Tx, r tc.MCPExecutionRetirementRequest, plan tc.MCPExecutionRetirementPlan) error {
	if plan != p.plan || r != p.request {
		return fault(f.Forbidden)
	}
	return p.authority.CheckMCPRetirementInTx(ctx, tx, r)
}
func (p *mcpRetirementControl) ReleaseMCPExecutionInTx(ctx context.Context, tx f.Tx, r tc.MCPExecutionRetirementRequest, plan tc.MCPExecutionRetirementPlan) error {
	if err := p.check(ctx, tx, r, plan); err != nil {
		return err
	}
	p.releases++
	if p.reject {
		return fault(f.DependencyUnavailable)
	}
	return nil
}
func (p *mcpRetirementControl) MCPExecutionRetiredInTx(ctx context.Context, tx f.Tx, r tc.MCPExecutionRetirementRequest, plan tc.MCPExecutionRetirementPlan) (bool, error) {
	if err := p.check(ctx, tx, r, plan); err != nil {
		return false, err
	}
	p.observations++
	return p.releases == 1, nil
}

func TestMCPRetirementUsesOriginalCancellationAndObservesUnknown(t *testing.T) {
	for _, mode := range []string{"commit", "unknown", "failure"} {
		t.Run(mode, func(t *testing.T) {
			capture := newCompleteCaptureControl(t)
			credential := newTestID[sc.Credential](t)
			ref, err := sc.NewCredentialRef(credential, i.SystemScope())
			if err != nil {
				t.Fatal(err)
			}
			capture.resolved.Snapshot.CredentialRef = &ref
			capture.resolved.CredentialLease = &sc.CredentialLease{LeaseID: newTestID[sc.Lease](t), CredentialRef: ref}
			if err := capture.p.driver.Run(context.Background(), capture.p.execution); err != nil {
				t.Fatal(err)
			}
			s, job, ctx, store, events, _ := executionRecoveryForPreparation(t, capture.p)
			key, err := f.AggregateLock(f.CredentialRefAggregate, credential.String())
			if err != nil {
				t.Fatal(err)
			}
			provider := &executionRecoveryRetirement{t: t, authority: capture.p.launch.authority, store: store, plan: &executionRecoveryRetirementPlan{locks: []f.LockRequest{{Key: key, Mode: f.Exclusive}}}}
			s.direct.state.deps.Models, s.direct.state.deps.Environment = provider, provider
			mcp := &mcpRetirementControl{authority: capture.p.launch.authority, plan: &executionRecoveryRetirementPlan{}, reject: mode == "failure"}
			s.direct.state.deps.MCP = mcp
			store.unknown = mode == "unknown"
			before := store.writes
			summary, handled, err := s.recoverCancelledPreparation(ctx, job)
			if !handled || mcp.releases != 1 {
				t.Fatal("original cancellation missed MCP release", err)
			}
			if mode == "failure" {
				if err == nil || store.rows[job.dispatch.ExecutionID.String()][3] != "preparing" || store.writes != before || events.appends != 0 {
					t.Fatal("MCP release failure published terminal", err)
				}
				return
			}
			if mode == "unknown" {
				if err == nil || job.recovery == nil {
					t.Fatal("unknown lost original owner")
				}
				writes := store.writes
				summary, handled, err = s.recoverCancelledPreparation(ctx, job)
				if !handled || store.writes != writes || mcp.releases != 1 || mcp.observations != 1 {
					t.Fatal("unknown replayed release instead of observing")
				}
			}
			if err != nil || summary.Status != ec.Cancelled {
				t.Fatal("original retirement did not settle", err)
			}
			if mcp.authority.CheckMCPRetirementPlan(mcp.ctx, mcp.request) == nil {
				t.Fatal("retired owner remained authorized")
			}
			if mcp.authority.CheckMCPRetirementInTx(context.Background(), f.NewTx(), mcp.request) == nil {
				t.Fatal("bare terminal tuple authorized release")
			}
		})
	}
}
