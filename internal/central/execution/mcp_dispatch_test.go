package execution

import (
	"context"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"testing"
)

type mcpApprovalControl struct {
	gc.Gate
	calls    int
	approved bool
	retire   func()
}

func (g *mcpApprovalControl) RequireApprovedInTx(context.Context, f.Tx, tc.ToolCallBinding) error {
	g.calls++
	if g.retire != nil {
		g.retire()
	}
	if !g.approved {
		return fault(f.Forbidden)
	}
	return nil
}

// Controlled transaction mechanics isolate the admission boundary. A current
// binding or approved row alone does not lend the original dispatch callback.
func TestMCPAdmissionNeedsLiveOriginalCallbackAndCurrentApproval(t *testing.T) {
	for _, mode := range []string{"pending", "approved", "allow", "retired-during-check"} {
		t.Run(mode, func(t *testing.T) {
			x := newReadFileAuthorityControl(t)
			b, err := toolBinding(&x.stored)
			if err != nil {
				t.Fatal(err)
			}
			b.RunnerBinding = tc.RunnerBinding{}
			b.Binding = tc.BuiltinBinding{}
			b.MCPBinding = tc.MCPBinding{ProjectID: b.ProjectID, ConfigID: newTestID[struct{}](t).String(), ConnectionID: newTestID[struct{}](t).String(), RuntimeBindingID: newTestID[struct{}](t).String(), ConfigRevision: 1, RemoteToolName: "echo"}
			key, _ := f.AggregateLock(f.ExecutionAggregate, b.ExecutionID.String())
			plan := &toolCallPlan{owner: x.driver, binding: b, locks: []f.LockRequest{{Key: key, Mode: f.Exclusive}}}
			gate := &mcpApprovalControl{approved: mode != "pending"}
			x.driver.driver.state.multi = &multiRoundState{approvals: gate}
			cause, _ := f.NewRecoveryCause("mcp-admission-test", b.ExecutionID.String(), b.RoundID)
			result := x.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := x.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				if x.driver.RequireMCPAdmissionInTx(ctx, tx, b, plan) == nil {
					t.Fatal("descriptor admitted outside original callback")
				}
				w := &mcpCurrentWitness{driver: x.driver, plan: plan, tx: tx, approval: ac.ApprovalDefault}
				w.active.Store(true)
				if mode == "allow" {
					w.approval = ac.ApprovalAllow
				}
				if mode == "retired-during-check" {
					gate.retire = func() { w.active.Store(false) }
				}
				live := context.WithValue(ctx, mcpCurrentKey{}, w)
				err := x.driver.RequireMCPAdmissionInTx(live, tx, b, plan)
				want := mode == "approved" || mode == "allow"
				if (err == nil) != want {
					t.Fatal("incorrect admission decision", err)
				}
				if mode == "allow" && gate.calls != 0 || mode != "allow" && gate.calls != 1 {
					t.Fatal("incorrect approval read count")
				}
				changed := b
				changed.MCPBinding.RuntimeBindingID = newTestID[struct{}](t).String()
				if x.driver.RequireMCPAdmissionInTx(live, tx, changed, plan) == nil {
					t.Fatal("foreign runtime shadow admitted")
				}
				w.active.Store(false)
				if x.driver.RequireMCPAdmissionInTx(live, tx, b, plan) == nil {
					t.Fatal("expired callback admitted")
				}
				return nil
			})
			if err := commitError(result); err != nil {
				t.Fatal(err)
			}
		})
	}
}
