package execution

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// Reuse only the controlled SQL/owner mechanics; replace every immutable
// capture/round/response consistently with an actual write profile. Real PG is
// separate. The original read fixture and all its assertions remain unchanged.
func newWriteFileAuthorityControl(t *testing.T, data []byte) *readFileAuthorityControl {
	t.Helper()
	x := newReadFileAuthorityControl(t)
	fields := x.input.Fields()
	reg := tc.WriteFileRegistration()
	fields.Tools[0].BindingSnapshot = tc.BuiltinExecutionBinding{RunnerBinding: reg.Binding, ScopeResolverID: reg.ScopeResolverID, RiskClassifierID: reg.RiskClassifierID, Class: reg.Class}
	var err error
	x.input, err = ec.NewPreparationInput(fields)
	if err != nil {
		t.Fatal(err)
	}
	x.preparation[11], x.preparation[12] = x.input.CanonicalBytes(), string(x.input.Digest())
	builder, err := NewContextBuilder(contextBuilderProvider(t))
	if err != nil {
		t.Fatal(err)
	}
	captured, err := builder.BuildContext(context.Background(), x.input)
	if err != nil {
		t.Fatal(err)
	}
	sf := x.stored.snapshot.Fields()
	sf.Context = captured
	x.stored.snapshot, err = ec.NewDirectTextSnapshot(sf)
	if err != nil {
		t.Fatal(err)
	}
	rf := x.stored.round.Fields()
	rf.ContextDigest = captured.Digest()
	tool := mc.Tool{Name: fields.Tools[0].ModelVisibleName, Description: reg.Definition.Description, InputSchema: reg.Definition.InputSchema, OutputSchema: reg.Definition.OutputSchema}
	rf.Input.Digest, err = ec.ToolCallInputDigest(context.Background(), rf.Messages, tool)
	if err != nil {
		t.Fatal(err)
	}
	round, err := ec.NewToolCallRound(ec.ToolCallRoundFields{Round: rf, Tool: tool})
	if err != nil {
		t.Fatal(err)
	}
	x.stored.round = toolExecutionRound{round}
	args := struct {
		Mount     string `json:"mount_id"`
		Path      string `json:"path"`
		Data      string `json:"data_base64"`
		Overwrite bool   `json:"overwrite"`
	}{fields.Mounts.Mounts[0].ID.String(), "write-canary.bin", base64.StdEncoding.EncodeToString(data), false}
	original, err := json.MarshalIndent(args, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := cursor.CanonicalJSON(original)
	if err != nil {
		t.Fatal(err)
	}
	v := x.stored.toolPending
	v.original, v.canonical = original, canonical
	v.call.Arguments = bytes.Clone(original)
	v.response.Message.Parts[0].ToolCall = &v.call
	x.driver.driver.state.tools.binding = reg.Binding
	return x
}

func TestExecutionWriteFileFactsMaximumOriginalAndProfile(t *testing.T) {
	data := bytes.Repeat([]byte{0, 255, 13, 10}, p.MaxWriteFileBytes/4)
	x := newWriteFileAuthorityControl(t, data)
	e := x.input.Fields().Request.ExecutionID
	b, err := x.driver.ReadToolCall(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	original := x.stored.toolPending.original
	if len(original) <= 32<<10 || len(original) > 1<<20 || b.Input.Bytes != f.Progress(len(original)) || b.Input.SHA256 != ec.TriggerInputDigest(original) || b.CanonicalArguments != ec.TriggerInputDigest(x.stored.toolPending.canonical) || b.RunnerBinding != tc.WriteFileRegistration().Binding {
		t.Fatal("complete original write identity was not preserved")
	}
	args, err := tc.DecodeWriteFileToolArguments(context.Background(), original)
	if err != nil || !bytes.Equal(args.Arguments.Data, data) {
		t.Fatal("full write argument was not retained")
	}
	clear(args.Arguments.Data)
	if _, err = x.driver.DiscoverReadFileCall(context.Background(), e); err == nil {
		t.Fatal("write call entered read authority")
	}
	if _, err = toolCallMountID(context.Background(), tc.ReadFileRegistration().Binding, original); err == nil {
		t.Fatal("read decoder accepted write")
	}
	old := newReadFileAuthorityControl(t)
	if _, err = old.driver.DiscoverWriteFileCall(context.Background(), old.input.Fields().Request.ExecutionID); err == nil {
		t.Fatal("read driver entered write authority")
	}
	if _, err = toolCallMountID(context.Background(), tc.WriteFileRegistration().Binding, old.stored.toolPending.original); err == nil {
		t.Fatal("write decoder accepted read")
	}
}

func TestExecutionWriteFileAuthorityBorrowExpires(t *testing.T) {
	for _, mode := range []string{"success", "error", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			x := newWriteFileAuthorityControl(t, []byte{0, 255, 13, 10})
			plan, err := x.driver.DiscoverWriteFileCall(context.Background(), x.input.Fields().Request.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			b := plan.Binding()
			original := bytes.Clone(x.stored.toolPending.original)
			locks := plan.RequiredLocks()
			locks[0].Mode = f.Exclusive
			if plan.RequiredLocks()[0].Mode != f.Shared {
				t.Fatal("lock plan escaped")
			}
			cause, _ := f.NewRecoveryCause("write-authority-control", b.ExecutionID.String(), b.Input.PayloadID)
			result := x.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := x.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				var borrowed []byte
				var held context.Context
				calls := 0
				q := ac.ExecutionConfigurationRequest{Actor: b.Actor, ProjectID: b.ProjectID, AgentID: b.AgentID, ExecutionID: b.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
				sentinel := errors.New("rejected")
				err := x.driver.WithWriteFileCallInTx(ctx, tx, plan, func(live context.Context, got tc.ToolCallBinding, raw []byte) error {
					calls++
					if !sameToolCallBinding(got, b) || !bytes.Equal(raw, original) || x.authority.RequireExecutionConfigurationInTx(live, tx, q) != nil {
						t.Fatal("original bytes/current witness missing")
					}
					borrowed, held = raw, context.WithoutCancel(live)
					raw[0] = '!'
					if mode == "error" {
						return sentinel
					}
					if mode == "cancel" {
						cancel()
					}
					return nil
				})
				if calls != 1 || !bytes.Equal(borrowed, make([]byte, len(original))) || !bytes.Equal(x.stored.toolPending.original, original) {
					t.Fatal("loan survived or changed protected source")
				}
				requireCode(t, x.authority.RequireExecutionConfigurationInTx(held, tx, q), f.Forbidden)
				if mode == "success" && err != nil || mode == "error" && !errors.Is(err, sentinel) || mode == "cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal("callback result changed")
				}
				return nil
			})
			if err := commitError(result); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type forgedWriteFilePlan struct{ tc.WriteFileCallPlan }

func TestExecutionWriteFileAuthorityRejectsStaleOrForeign(t *testing.T) {
	for _, mode := range []string{"public-plan", "read-plan", "other-driver", "foreign-tx", "missing-lock", "cancel-marker", "revoked-mount", "revoked-tool", "changed-protected-bytes"} {
		t.Run(mode, func(t *testing.T) {
			x := newWriteFileAuthorityControl(t, []byte("private-canary"))
			plan, err := x.driver.DiscoverWriteFileCall(context.Background(), x.input.Fields().Request.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			b := plan.Binding()
			cause, _ := f.NewRecoveryCause("write-authority-reject", b.ExecutionID.String(), b.Input.PayloadID)
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
					plan = forgedWriteFilePlan{plan}
				case "read-plan":
					plan = &readFileCallPlan{call: plan.(*writeFileCallPlan).call}
				case "other-driver":
					driver = &ToolCallDriver{driver: x.driver.driver}
				case "foreign-tx":
					tx = f.NewTx()
				case "cancel-marker":
					x.cancelled = true
				case "revoked-mount", "revoked-tool":
					fields := x.agent.Fields()
					if mode == "revoked-mount" {
						fields.AllowedMountIDs = fields.AllowedMountIDs[:0]
					} else {
						fields.AllowedToolIDs = fields.AllowedToolIDs[:0]
					}
					x.agent, err = ac.NewAgentConfig(fields)
					if err != nil {
						t.Fatal(err)
					}
				case "changed-protected-bytes":
					x.corrupt = true
				}
				calls := 0
				err = driver.WithWriteFileCallInTx(ctx, tx, plan, func(context.Context, tc.ToolCallBinding, []byte) error { calls++; return nil })
				if err == nil || calls != 0 {
					t.Fatal("invalid write authority reached consumer")
				}
				return nil
			})
			if err := commitError(result); err != nil {
				t.Fatal(err)
			}
		})
	}
}
