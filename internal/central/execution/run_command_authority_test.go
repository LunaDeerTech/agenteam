package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"reflect"
	"testing"
)

// Reuse SQL mechanics only. The immutable capture/schema and protected call
// are all replaced by the actual command profile; this is not a PG result.
func newRunCommandAuthorityControl(t *testing.T) *readFileAuthorityControl {
	t.Helper()
	x := newReadFileAuthorityControl(t)
	fields := x.input.Fields()
	reg := tc.RunCommandRegistration()
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
		Mount   string   `json:"mount_id"`
		Mode    string   `json:"mode"`
		Program string   `json:"program"`
		Args    []string `json:"args"`
		CWD     string   `json:"cwd"`
	}{fields.Mounts.Mounts[0].ID.String(), "argv", "/usr/bin/printf", []string{"%s", "private-command-argument"}, "."}
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
	agent := x.agent.Fields()
	agent.Core.ApprovalPolicy = ac.ApprovalAllow
	agent.Core.ApprovalModelRef = nil
	x.agent, err = ac.NewAgentConfig(agent)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

type commandEnvControl struct {
	x     *readFileAuthorityControl
	calls int
	fail  error
	after func(context.Context)
}

func (p *commandEnvControl) RequireEmptyInTx(ctx context.Context, tx f.Tx, r pvc.CurrentProcessEnvironmentRequest) error {
	p.calls++
	got, err := p.x.authority.RequireCurrentProcessEnvironmentInTx(ctx, tx, r)
	if err != nil {
		return err
	}
	agent := p.x.agent.Fields()
	if got.AgentVersion != agent.Core.Version || !reflect.DeepEqual(got.AllowedSecretVariableIDs, agent.AllowedSecretVariableIDs) {
		return errors.New("wrong current Agent facts")
	}
	if len(got.AllowedSecretVariableIDs) > 0 {
		got.AllowedSecretVariableIDs[0] = i.ProjectVariableID{}
		again, err := p.x.authority.RequireCurrentProcessEnvironmentInTx(ctx, tx, r)
		if err != nil || !reflect.DeepEqual(again.AllowedSecretVariableIDs, agent.AllowedSecretVariableIDs) {
			return errors.New("current facts were not copied")
		}
	}
	if p.after != nil {
		p.after(ctx)
	}
	return p.fail
}
func TestExecutionRunCommandCurrentAdmissionSeparateFromObservation(t *testing.T) {
	for _, mode := range []string{"allow-empty", "default", "auto", "ordinary-empty-value", "selected-secret", "missing-env-owner"} {
		t.Run(mode, func(t *testing.T) {
			x := newRunCommandAuthorityControl(t)
			env := &commandEnvControl{x: x}
			x.driver.driver.state.tools.currentEnvironment = env
			agent := x.agent.Fields()
			switch mode {
			case "default":
				agent.Core.ApprovalPolicy = ac.ApprovalDefault
			case "auto":
				agent.Core.ApprovalPolicy = ac.ApprovalAuto
				m := agent.Core.ModelRef
				agent.Core.ApprovalModelRef = &m
			case "ordinary-empty-value":
				env.fail = fault(f.CapabilityUnsupported)
			case "selected-secret":
				agent.AllowedSecretVariableIDs = []i.ProjectVariableID{newTestID[i.ProjectVariable](t)}
				env.fail = fault(f.CapabilityUnsupported)
			case "missing-env-owner":
				x.driver.driver.state.tools.currentEnvironment = nil
			}
			var err error
			x.agent, err = ac.NewAgentConfig(agent)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := x.driver.DiscoverRunCommandCall(context.Background(), x.input.Fields().Request.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			b := plan.Binding()
			cause, _ := f.NewRecoveryCause("command-admission-control", b.ExecutionID.String(), b.Input.PayloadID)
			result := x.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := x.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				if x.driver.RequireRunCommandAdmissionInTx(ctx, tx, plan) == nil {
					t.Fatal("admission outside original callback")
				}
				observed := 0
				// Observation is available regardless of a later environment/policy change.
				err := x.driver.WithRunCommandCallInTx(ctx, tx, plan, func(live context.Context, got tc.ToolCallBinding, raw []byte) error {
					observed++
					if !sameToolCallBinding(got, b) || !bytes.Equal(raw, x.stored.toolPending.original) {
						t.Fatal("observation changed original call")
					}
					err := x.driver.RequireRunCommandAdmissionInTx(live, tx, plan)
					if mode == "allow-empty" {
						if err != nil {
							t.Fatal(err)
						}
					} else if err == nil {
						t.Fatal("unproven current admission")
					}
					return nil
				})
				if err != nil || observed != 1 {
					t.Fatal("current admission erased historical observation", err)
				}
				if (mode == "default" || mode == "auto" || mode == "missing-env-owner") && env.calls != 0 {
					t.Fatal("unapproved command reached environment participant")
				}
				return nil
			})
			if err := commitError(result); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestExecutionRunCommandWitnessBorrowAndExactBinding(t *testing.T) {
	for _, mode := range []string{"success", "callback-error", "cancel", "retire-during-env", "wrong-call-digest", "foreign-tx", "foreign-authority"} {
		t.Run(mode, func(t *testing.T) {
			x := newRunCommandAuthorityControl(t)
			env := &commandEnvControl{x: x}
			x.driver.driver.state.tools.currentEnvironment = env
			plan, err := x.driver.DiscoverRunCommandCall(context.Background(), x.input.Fields().Request.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			b := plan.Binding()
			request, err := commandEnvironmentRequest(b)
			if err != nil {
				t.Fatal(err)
			}
			original := bytes.Clone(x.stored.toolPending.original)
			cause, _ := f.NewRecoveryCause("command-witness-control", b.ExecutionID.String(), b.Input.PayloadID)
			result := x.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := x.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				var loan []byte
				var held context.Context
				sentinel := errors.New("consumer denied")
				err := x.driver.WithRunCommandCallInTx(ctx, tx, plan, func(live context.Context, _ tc.ToolCallBinding, raw []byte) error {
					loan, held = raw, context.WithoutCancel(live)
					facts, e := x.authority.RequireCurrentProcessEnvironmentInTx(live, tx, request)
					if e != nil || facts.AgentVersion != x.agent.Fields().Core.Version {
						t.Fatal("no current owner witness")
					}
					switch mode {
					case "callback-error":
						return sentinel
					case "cancel":
						cancel()
						return nil
					case "retire-during-env":
						env.after = func(c context.Context) { c.Value(commandCurrentKey{}).(*commandCurrentWitness).retire() }
					case "wrong-call-digest":
						changed := request
						changed.CallBindingDigest = ec.TriggerInputDigest([]byte("different-binding"))
						if _, e = x.authority.RequireCurrentProcessEnvironmentInTx(live, tx, changed); e == nil {
							t.Fatal("changed identity accepted")
						}
					case "foreign-tx":
						if _, e = x.authority.RequireCurrentProcessEnvironmentInTx(live, f.NewTx(), request); e == nil {
							t.Fatal("foreign Tx accepted")
						}
					case "foreign-authority":
						other := newRunCommandAuthorityControl(t)
						if _, e = other.authority.RequireCurrentProcessEnvironmentInTx(live, tx, request); e == nil {
							t.Fatal("foreign owner accepted")
						}
					}
					e = x.driver.RequireRunCommandAdmissionInTx(live, tx, plan)
					if mode == "retire-during-env" {
						if e == nil {
							t.Fatal("retired participant published admission")
						}
					} else if e != nil {
						t.Fatal(e)
					}
					raw[0] = '!'
					return nil
				})
				if mode == "callback-error" {
					if !errors.Is(err, sentinel) {
						t.Fatal("callback error changed")
					}
				} else if mode == "cancel" {
					if !errors.Is(err, context.Canceled) {
						t.Fatal("cancellation changed")
					}
				} else if err != nil {
					t.Fatal(err)
				}
				if loan == nil || !bytes.Equal(loan, make([]byte, len(original))) || !bytes.Equal(original, x.stored.toolPending.original) {
					t.Fatal("private argument loan escaped or changed repository")
				}
				if _, err = x.authority.RequireCurrentProcessEnvironmentInTx(held, tx, request); err == nil {
					t.Fatal("current environment witness survived callback")
				}
				if x.driver.RequireRunCommandAdmissionInTx(held, tx, plan) == nil {
					t.Fatal("admission survived callback")
				}
				return nil
			})
			if err := commitError(result); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestExecutionRunCommandProfileAndIdentityRemainClosed(t *testing.T) {
	x := newRunCommandAuthorityControl(t)
	e := x.input.Fields().Request.ExecutionID
	plan, err := x.driver.DiscoverRunCommandCall(context.Background(), e)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = x.driver.DiscoverReadFileCall(context.Background(), e); err == nil {
		t.Fatal("command entered read authority")
	}
	if _, err = x.driver.DiscoverWriteFileCall(context.Background(), e); err == nil {
		t.Fatal("command entered write authority")
	}
	for _, old := range []*readFileAuthorityControl{newReadFileAuthorityControl(t), newWriteFileAuthorityControl(t, nil)} {
		if _, err = old.driver.DiscoverRunCommandCall(context.Background(), old.input.Fields().Request.ExecutionID); err == nil {
			t.Fatal("old profile entered command authority")
		}
		if _, err = toolCallMountID(context.Background(), tc.RunCommandRegistration().Binding, old.stored.toolPending.original); err == nil {
			t.Fatal("command decoder accepted old args")
		}
	}
	b := plan.Binding()
	base, err := commandEnvironmentRequest(b)
	if err != nil {
		t.Fatal(err)
	}
	mutations := map[string]func(*tc.ToolCallBinding){
		"call-id":       func(v *tc.ToolCallBinding) { v.CallID += "-different" },
		"invocation":    func(v *tc.ToolCallBinding) { v.InvocationID = newTestID[mc.Invocation](t) },
		"round":         func(v *tc.ToolCallBinding) { v.RoundID = newTestID[ec.Round](t).String() },
		"input-binding": func(v *tc.ToolCallBinding) { v.InputBindingID = newTestID[ec.Round](t).String() },
		"payload":       func(v *tc.ToolCallBinding) { v.Input.PayloadID = newTestID[ec.Round](t).String() },
		"bytes":         func(v *tc.ToolCallBinding) { v.Input.Bytes++ },
		"raw-digest":    func(v *tc.ToolCallBinding) { v.Input.SHA256 = ec.TriggerInputDigest([]byte("different-raw")) },
		"canonical-digest": func(v *tc.ToolCallBinding) {
			v.CanonicalArguments = ec.TriggerInputDigest([]byte("different-canonical"))
		},
	}
	for name, change := range mutations {
		t.Run(name, func(t *testing.T) {
			other := b
			change(&other)
			r, err := commandEnvironmentRequest(other)
			if err != nil || r.CallBindingDigest == base.CallBindingDigest {
				t.Fatal("complete original identity omitted from digest")
			}
		})
	}
}
