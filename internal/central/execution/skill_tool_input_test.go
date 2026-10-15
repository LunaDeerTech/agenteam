package execution

import (
	"context"
	"errors"
	"slices"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Controlled SQL checks the new callback lifetime and the unchanged fallback.
// The actual App is still required to prove the complete current-owner chain.
type skillToolInputStore struct {
	*launchStore
	reads int
}

func (s *skillToolInputStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *skillToolInputStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.reads++
	return launchScan{err: errors.New("controlled current read rejected")}
}

type skillToolInputControl struct {
	driver   *ToolCallDriver
	store    *skillToolInputStore
	plan     *toolCallPlan
	binding  tc.ToolCallBinding
	current  *toolCurrentWitness
	bindings []sc.SkillBinding
	ctx      context.Context
}

func newSkillToolInputControl(t *testing.T) *skillToolInputControl {
	t.Helper()
	original := newReadFileAuthorityControl(t)
	b, err := toolBinding(&original.stored)
	if err != nil {
		t.Fatal("original Tool binding", err)
	}
	b.RunnerBinding = tc.RunnerBinding{}
	b.Binding = tc.BuiltinBinding{HandlerID: "skill.prepare", ContractRevision: 1}
	if err = b.Validate(); err != nil {
		t.Fatal("prepare Tool binding", err)
	}
	store := &skillToolInputStore{launchStore: &launchStore{t: t, live: true, tx: f.NewTx()}}
	store.locks = []f.LockRequest{projectLock(b.ProjectID), agentLock(b.AgentID, f.Exclusive), executionLock(b.ExecutionID)}
	d := &ToolCallDriver{driver: &DirectTextDriver{state: &directTextState{store: store, multi: &multiRoundState{}}}}
	p := &toolCallPlan{owner: d, binding: b, locks: slices.Clone(store.locks), snapshot: original.stored.snapshot.Digest(), round: original.stored.round.Digest()}
	current := &toolCurrentWitness{driver: d, tx: store.tx, locks: slices.Clone(store.locks), request: ac.ExecutionConfigurationRequest{Actor: b.Actor, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}}
	current.active.Store(true)
	bindings := []sc.SkillBinding{{SkillID: newTestID[pc.Skill](t), RevisionID: newTestID[sc.Revision](t), Revision: 1, AssignmentID: newTestID[sc.Assignment](t), AssignmentSequence: 1, Name: "input-loan", Description: "Controlled immutable input", PackageSHA256: ec.TriggerInputDigest([]byte("controlled package")), EntryPath: sc.EntryPath}}
	if err := bindings[0].Validate(); err != nil {
		t.Fatal("captured Skill binding", err)
	}
	return &skillToolInputControl{d, store, p, b, current, bindings, context.WithValue(context.Background(), toolCurrentKey{}, current)}
}

func TestSkillPrepareInputLoanSingleUseAndCallbackLifetime(t *testing.T) {
	for _, mode := range []string{"return", "consumer-error", "panic"} {
		t.Run(mode, func(t *testing.T) {
			x := newSkillToolInputControl(t)
			original := slices.Clone(x.bindings)
			var escaped context.Context
			var retained *skillToolInputLoan
			sentinel := errors.New("consumer failure")
			func() {
				if mode == "panic" {
					defer func() {
						if recover() != sentinel {
							t.Fatal("original panic was not propagated")
						}
					}()
				}
				err := x.driver.withSkillToolInputLoan(x.ctx, x.store.tx, x.plan, x.binding, x.bindings, func(ctx context.Context) error {
					escaped = context.WithoutCancel(ctx)
					retained = ctx.Value(skillToolInputKey{}).(*skillToolInputLoan)
					// Neither source nor returned slices may replace the captured facts.
					x.bindings[0].Name = "source changed"
					got, err := x.driver.ReadSkillToolBindingsInTx(ctx, x.store.tx, x.binding, x.plan)
					if err != nil || !slices.Equal(got, original) || x.store.reads != 0 {
						t.Fatal("loan reread current or lost the immutable input", err)
					}
					got[0].Name = "result changed"
					if !slices.Equal(retained.bindings, original) {
						t.Fatal("returned slice aliases the loan")
					}
					if _, err = x.driver.ReadSkillToolBindingsInTx(ctx, x.store.tx, x.binding, x.plan); err == nil || x.store.reads != 0 {
						t.Fatal("used loan fell back to fresh authorization")
					}
					if mode == "panic" {
						panic(sentinel)
					}
					if mode == "consumer-error" {
						return sentinel
					}
					return nil
				})
				if mode == "return" && err != nil || mode == "consumer-error" && err != sentinel {
					t.Fatal("callback outcome changed", err)
				}
			}()
			if retained == nil || retained.active || retained.bindings != nil {
				t.Fatal("callback return retained input storage")
			}
			if _, err := x.driver.ReadSkillToolBindingsInTx(escaped, x.store.tx, x.binding, x.plan); err == nil || x.store.reads != 0 {
				t.Fatal("expired context fell back to fresh authorization")
			}
		})
	}
}

func TestSkillPrepareInputLoanRejectsForeignOrExpiredProof(t *testing.T) {
	for _, mode := range []string{"tx", "plan", "binding", "owner", "store", "locks", "closed-tx", "current", "expired-current", "caller-cancel", "parent-cancel"} {
		t.Run(mode, func(t *testing.T) {
			x := newSkillToolInputControl(t)
			parent, cancel := context.WithCancel(x.ctx)
			defer cancel()
			err := x.driver.withSkillToolInputLoan(parent, x.store.tx, x.plan, x.binding, x.bindings, func(ctx context.Context) error {
				d, tx, b, plan := x.driver, x.store.tx, x.binding, x.plan
				switch mode {
				case "tx":
					tx = f.NewTx()
				case "plan":
					copy := *plan
					plan = &copy
				case "binding":
					b.CallID += "-other"
				case "owner":
					d = &ToolCallDriver{driver: d.driver}
				case "store":
					x.driver.driver.state.store = &skillToolInputStore{launchStore: x.store.launchStore}
				case "locks":
					x.store.locks = nil
				case "closed-tx":
					x.store.live = false
				case "current":
					ctx = context.WithValue(ctx, toolCurrentKey{}, &toolCurrentWitness{})
				case "expired-current":
					x.current.active.Store(false)
				case "caller-cancel":
					closed, stop := context.WithCancel(ctx)
					stop()
					ctx = closed
				case "parent-cancel":
					cancel()
					ctx = context.WithoutCancel(ctx)
				}
				if got, err := d.ReadSkillToolBindingsInTx(ctx, tx, b, plan); err == nil || got != nil || x.store.reads != 0 {
					t.Fatal("mismatched loan admitted or fell back", err)
				}
				return nil
			})
			if mode == "parent-cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatal("original parent cancellation lost", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSkillPrepareInputLoanRequiresOriginalCurrentAndKeepsFallback(t *testing.T) {
	x := newSkillToolInputControl(t)
	called := false
	if err := x.driver.withSkillToolInputLoan(context.Background(), x.store.tx, x.plan, x.binding, x.bindings, func(context.Context) error { called = true; return nil }); err == nil || called {
		t.Fatal("bare DTO minted a callback loan")
	}
	// The real public entry must still perform current SQL before it can mint.
	if err := x.driver.WithBuiltinToolCallInTx(x.ctx, x.store.tx, x.binding, x.plan, func(context.Context, tc.ToolCallBinding, []byte) error { called = true; return nil }); err == nil || called || x.store.reads != 1 {
		t.Fatal("failed full current check minted a loan", err)
	}
	// No loan retains the original full validation path, even with a live
	// toolCurrentWitness. That earlier witness alone is not the new proof.
	if _, err := x.driver.ReadSkillToolBindingsInTx(x.ctx, x.store.tx, x.binding, x.plan); err == nil || x.store.reads != 2 {
		t.Fatal("original fallback was bypassed", err)
	}
	for _, forged := range []any{struct{}{}, (*skillToolInputLoan)(nil)} {
		ctx := context.WithValue(x.ctx, skillToolInputKey{}, forged)
		if got, err := x.driver.ReadSkillToolBindingsInTx(ctx, x.store.tx, x.binding, x.plan); err == nil || got != nil || x.store.reads != 2 {
			t.Fatal("malformed private loan fell back", err)
		}
	}
}
