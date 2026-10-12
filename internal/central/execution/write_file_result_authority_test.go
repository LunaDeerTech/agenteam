package execution

import (
	"bytes"
	"context"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestExecutionCommittedWriteCallHistoricalLoan(t *testing.T) {
	for _, mode := range []string{"revoked-and-cancelled", "callback-error", "callback-cancel"} {
		t.Run(mode, func(t *testing.T) {
			x := newWriteFileAuthorityControl(t, bytes.Repeat([]byte{0, 255}, p.MaxWriteFileBytes/2))
			plan, err := x.driver.DiscoverCommittedWriteFileCall(context.Background(), x.input.Fields().Request.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			b := plan.Binding()
			original := bytes.Clone(x.stored.toolPending.original)
			fields := x.agent.Fields()
			fields.AllowedMountIDs, fields.AllowedToolIDs = fields.AllowedMountIDs[:0], fields.AllowedToolIDs[:0]
			x.agent, err = ac.NewAgentConfig(fields)
			if err != nil {
				t.Fatal(err)
			}
			x.cancelled, x.toolErr = true, fault(f.DependencyUnbound)
			locks := plan.RequiredLocks()
			locks[0].Mode = f.Exclusive
			if plan.RequiredLocks()[0].Mode != f.Shared {
				t.Fatal("plan locks escaped")
			}
			copy := plan.Binding()
			copy.CallID = "changed"
			if !sameToolCallBinding(plan.Binding(), b) {
				t.Fatal("plan binding escaped")
			}
			cause, _ := f.NewRecoveryCause("write-result-history-control", b.ExecutionID.String(), b.Input.PayloadID)
			result := x.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := x.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				var borrowed []byte
				calls := 0
				sentinel := errors.New("consumer stopped")
				err := x.driver.WithCommittedWriteFileCallInTx(ctx, tx, plan, func(live context.Context, got tc.ToolCallBinding, raw []byte) error {
					calls++
					if !sameToolCallBinding(got, b) || !bytes.Equal(raw, original) || len(raw) <= 32<<10 {
						t.Fatal("historical original input changed")
					}
					request := ac.ExecutionConfigurationRequest{Actor: b.Actor, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
					requireCode(t, x.authority.RequireExecutionConfigurationInTx(live, tx, request), f.Forbidden)
					borrowed = raw
					raw[0] = '!'
					if mode == "callback-error" {
						return sentinel
					}
					if mode == "callback-cancel" {
						cancel()
					}
					return nil
				})
				if calls != 1 || !bytes.Equal(borrowed, make([]byte, len(original))) || !bytes.Equal(x.stored.toolPending.original, original) {
					t.Fatal("historical loan escaped or changed source")
				}
				if mode == "revoked-and-cancelled" && err != nil || mode == "callback-error" && !errors.Is(err, sentinel) || mode == "callback-cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal("original callback outcome changed", err)
				}
				return nil
			})
			if err := commitError(result); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type forgedCommittedWritePlan struct{ tc.CommittedWriteFileCallPlan }

func TestExecutionCommittedWriteCallRejectsUnboundHistory(t *testing.T) {
	for _, mode := range []string{"public-plan", "current-dispatch-plan", "other-driver", "foreign-tx", "missing-lock", "changed-snapshot", "changed-call", "changed-input", "changed-original", "read-profile"} {
		t.Run(mode, func(t *testing.T) {
			x := newWriteFileAuthorityControl(t, []byte("private-write-canary"))
			plan, err := x.driver.DiscoverCommittedWriteFileCall(context.Background(), x.input.Fields().Request.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			b := plan.Binding()
			cause, _ := f.NewRecoveryCause("write-result-history-reject", b.ExecutionID.String(), b.Input.PayloadID)
			result := x.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				locks := plan.RequiredLocks()
				if mode == "missing-lock" {
					locks = locks[:len(locks)-1]
				}
				if err := x.AcquireAll(ctx, tx, locks); err != nil {
					return err
				}
				driver := x.driver
				switch mode {
				case "public-plan":
					plan = forgedCommittedWritePlan{plan}
				case "current-dispatch-plan":
					plan = &writeFileCallPlan{call: plan.(*committedWriteFileCallPlan).call}
				case "other-driver":
					driver = &ToolCallDriver{driver: x.driver.driver}
				case "foreign-tx":
					tx = f.NewTx()
				case "changed-snapshot":
					plan.(*committedWriteFileCallPlan).call.snapshot = b.Input.SHA256
				case "changed-call":
					plan.(*committedWriteFileCallPlan).call.binding.CallID += "-other"
				case "changed-input":
					x.preparation[12] = string(b.Input.SHA256)
				case "changed-original":
					x.corrupt = true
				case "read-profile":
					x.driver.driver.state.tools.binding = tc.ReadFileRegistration().Binding
				}
				calls := 0
				err = driver.WithCommittedWriteFileCallInTx(ctx, tx, plan, func(context.Context, tc.ToolCallBinding, []byte) error { calls++; return nil })
				if err == nil || calls != 0 {
					t.Fatal("invalid history reached callback")
				}
				return nil
			})
			if err := commitError(result); err != nil {
				t.Fatal(err)
			}
		})
	}
}
