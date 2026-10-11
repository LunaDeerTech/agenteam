package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Repository rows and upstream domain data are controlled here. The real
// decoders, discovery, transaction/lock checks and scoped Execution authority
// run unchanged. This does not establish PostgreSQL or physical Runner facts.
type readFileAuthorityControl struct {
	*launchStore
	authority   *Authority
	driver      *ToolCallDriver
	input       ec.PreparationInput
	preparation []any
	stored      directTextRecord
	agent       ac.AgentConfig
	cancelled   bool
	corrupt     bool
	toolErr     error
}

func newReadFileAuthorityControl(t *testing.T) *readFileAuthorityControl {
	t.Helper()
	r := newPreparationMountControl(t)
	if err := r.p.driver.Run(context.Background(), r.p.execution); err != nil {
		t.Fatal(err)
	}
	preparation := append([]any(nil), r.p.store.inputs[r.p.execution.String()]...)
	prior, err := scanPreparationInput(launchScan{values: preparation})
	if err != nil || prior == nil {
		t.Fatal("controlled preparation", err)
	}
	fields := prior.input.Fields()
	registration := tc.ReadFileRegistration()
	toolID := newTestID[i.Tool](t)
	name := "tool_" + strings.ReplaceAll(toolID.String(), "-", "")
	fields.Tools = []tc.ExecutionTool{{ToolID: toolID, SpecRevision: 1, ModelVisibleName: name, BindingSnapshot: tc.BuiltinExecutionBinding{RunnerBinding: registration.Binding, ScopeResolverID: registration.ScopeResolverID, RiskClassifierID: registration.RiskClassifierID, Class: registration.Class}}}
	agent := fields.Agent.Fields()
	agent.AllowedToolIDs = []i.ToolID{toolID}
	fields.Agent, err = ac.NewAgentConfig(agent)
	if err != nil {
		t.Fatal(err)
	}
	input, err := ec.NewPreparationInput(fields)
	if err != nil {
		t.Fatal(err)
	}
	preparation[11], preparation[12] = input.CanonicalBytes(), string(input.Digest())
	builder, err := NewContextBuilder(contextBuilderProvider(t))
	if err != nil {
		t.Fatal(err)
	}
	captured, err := builder.BuildContext(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := runtimeModelCandidate(t)
	sf := base.Snapshot.Fields()
	sf.Context = captured
	sf.CreatedAt, _ = f.NewInstant(fields.CapturedAt.Time().Add(time.Second))
	snapshot, err := ec.NewDirectTextSnapshot(sf)
	if err != nil {
		t.Fatal(err)
	}
	rf := base.Round.Fields()
	rf.ExecutionID, rf.Input.ExecutionID = fields.Request.ExecutionID, &fields.Request.ExecutionID
	rf.ContextDigest, rf.CreatedAt = captured.Digest(), sf.CreatedAt
	rf.Input.SchemaVersion = 2
	definition := registration.Definition
	tool := mc.Tool{Name: name, Description: definition.Description, InputSchema: definition.InputSchema, OutputSchema: definition.OutputSchema}
	rf.Input.Digest, err = ec.ToolCallInputDigest(context.Background(), rf.Messages, tool)
	if err != nil {
		t.Fatal(err)
	}
	round, err := ec.NewToolCallRound(ec.ToolCallRoundFields{Round: rf, Tool: tool})
	if err != nil {
		t.Fatal(err)
	}
	original := []byte(fmt.Sprintf(`{ "mount_id":%q, "path":"private-file-canary.txt", "offset":"0", "length":32, "encoding":"utf8" }`, r.metadata.ID.String()))
	canonical, err := cursor.CanonicalJSON(original)
	if err != nil {
		t.Fatal(err)
	}
	call := mc.ToolCallPart{ID: "original-read-file-call", Name: name, Arguments: json.RawMessage(original)}
	response := mc.ModelResponse{CallID: rf.CallID, InvocationID: newTestID[mc.Invocation](t), Message: mc.Message{Role: "assistant", Parts: []mc.MessagePart{{ToolCall: &call}}}, FinishReason: "tool_calls", Usage: mc.Usage{Source: mc.UnknownUsage}}
	if response.Validate() != nil {
		t.Fatal("controlled response")
	}
	x := &readFileAuthorityControl{launchStore: &launchStore{t: t, rows: map[string][]any{}}, input: input, preparation: preparation, agent: fields.Agent, stored: directTextRecord{snapshot: snapshot, round: toolExecutionRound{round}, startedVersion: 2, startedEvent: newTestID[event.EventIdentity](t), toolPending: &toolCallCandidate{payload: newTestID[toolArgumentPayload](t), response: response, call: call, original: original, canonical: canonical, at: sf.CreatedAt, version: 3}}}
	x.authority, err = NewAuthority(x, launchProjects{fixture: r.p.launch}, nil)
	if err != nil {
		t.Fatal(err)
	}
	x.driver = &ToolCallDriver{driver: &DirectTextDriver{state: &directTextState{store: x, authority: x.authority, deps: DirectTextDependencies{Projects: x, Agents: x}, tools: &toolCallDependencies{metadata: x}}}}
	return x
}

func (s *readFileAuthorityControl) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *readFileAuthorityControl) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if ctx.Err() != nil {
		return launchScan{err: ctx.Err()}
	}
	sf, rf, v := s.stored.snapshot.Fields(), s.stored.round.Fields(), s.stored.toolPending
	q := s.input.Fields().Request
	switch {
	case strings.Contains(query, "FROM agenteam_execution.executions WHERE"):
		raw, _ := json.Marshal(q.Launch)
		digest, _ := q.Launch.Digest()
		at, sid := sf.CreatedAt.Time(), sf.ID.String()
		var cancel *time.Time
		if s.cancelled {
			cancel = &at
		}
		return launchScan{values: []any{q.ExecutionID.String(), q.Launch.ProjectID.String(), q.Launch.AgentID.String(), "running", int64(3), cancel, &sid, at, &at, (*time.Time)(nil), raw, q.Launch.Meta.IdempotencyKey.String(), q.Launch.Meta.RequestID.String(), string(digest)}}
	case strings.Contains(query, "FROM agenteam_execution.snapshots s JOIN"):
		raw, _ := directTextJSON(v.response)
		digest, at := string(ec.TriggerInputDigest(raw)), v.at.Time()
		return launchScan{values: []any{s.stored.snapshot.CanonicalBytes(), string(s.stored.snapshot.Digest()), q.ExecutionID.String(), q.Launch.ProjectID.String(), q.Launch.AgentID.String(), sf.ID.String(), sf.StartID.String(), sf.ProcessID.String(), string(sf.Context.InputDigest()), string(sf.Context.Digest()), sf.CreatedAt.Time(), int64(2), s.stored.startedEvent.String(), s.stored.round.CanonicalBytes(), string(s.stored.round.Digest()), rf.ID.String(), rf.InputBindingID.String(), rf.CallID.String(), string(rf.Input.Digest), string(rf.ContextDigest), rf.CreatedAt.Time(), int64(2), (*string)(nil), (*int64)(nil), (*string)(nil), (*string)(nil), raw, &digest, (*time.Time)(nil), true, &at}}
	case strings.Contains(query, "FROM agenteam_execution.tool_calls t JOIN"):
		tool := s.input.Fields().Tools[0]
		binding := tool.BindingSnapshot.RunnerBinding
		original := bytes.Clone(v.original)
		if s.corrupt {
			original[0] = '!'
		}
		return launchScan{values: []any{v.payload.String(), v.response.InvocationID.String(), v.call.ID, v.call.Name, tool.ToolID.String(), int64(tool.SpecRevision), binding.OperationName, binding.OperationRevision, int64(binding.ProtocolMajor), int64(v.version), v.at.Time(), rf.ID.String(), rf.InputBindingID.String(), rf.CallID.String(), sf.ID.String(), q.Launch.ProjectID.String(), q.Launch.AgentID.String(), original, string(ec.TriggerInputDigest(v.original)), bytes.Clone(v.canonical), string(ec.TriggerInputDigest(v.canonical))}}
	case strings.Contains(query, "FROM agenteam_execution.transcript_entries"):
		kind, message := "input", rf.Messages[1]
		if args[1].(int64) == 2 {
			kind, message = "assistant", v.response.Message
		}
		raw, _ := directTextJSON(message)
		return launchScan{values: []any{rf.ID.String(), kind, raw, string(ec.TriggerInputDigest(raw)), sf.CreatedAt.Time()}}
	case strings.Contains(query, "FROM agenteam_execution.preparation_inputs"):
		return launchScan{values: s.preparation}
	default:
		s.t.Fatal("unexpected authority SQL")
		return launchScan{}
	}
}
func (s *readFileAuthorityControl) RequirePreparingProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.ProjectRef, error) {
	if project != s.input.Fields().Project.ID {
		return pc.ProjectRef{}, fault(f.Forbidden)
	}
	return s.input.Fields().Project, s.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(project)})
}
func (s *readFileAuthorityControl) ReadExecutionConfigurationInTx(ctx context.Context, tx f.Tx, request ac.ExecutionConfigurationRequest) (ac.AgentConfig, error) {
	if err := s.authority.RequireExecutionConfigurationInTx(ctx, tx, request); err != nil {
		return ac.AgentConfig{}, err
	}
	return s.agent, nil
}
func (s *readFileAuthorityControl) RequireExecutionToolFactsInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolFactsRequest) error {
	if err := request.Validate(); err != nil {
		return err
	}
	if err := s.RequireHeldLocks(ctx, tx, request.RequiredLocks()); err != nil {
		return err
	}
	return s.toolErr
}
func (s *readFileAuthorityControl) ReadDefinitionInTx(context.Context, f.Tx, tc.SpecRef) (tc.Definition, error) {
	s.t.Fatal("read-only authority unexpectedly resolved a definition")
	return tc.Definition{}, fault(f.InvalidState)
}

func TestExecutionReadFileAuthorityExpiresBorrowAndWitness(t *testing.T) {
	for _, mode := range []string{"success", "callback-error", "callback-cancel"} {
		t.Run(mode, func(t *testing.T) {
			x := newReadFileAuthorityControl(t)
			plan, err := x.driver.DiscoverReadFileCall(context.Background(), x.input.Fields().Request.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			original := bytes.Clone(x.stored.toolPending.original)
			binding := plan.Binding()
			copy := plan.Binding()
			copy.Input.PayloadID = newTestID[toolArgumentPayload](t).String()
			locks := plan.RequiredLocks()
			locks[0].Mode = f.Exclusive
			if !sameToolCallBinding(plan.Binding(), binding) || plan.RequiredLocks()[0].Mode != f.Shared {
				t.Fatal("plan storage escaped")
			}
			cause, _ := f.NewRecoveryCause("read-file-authority-control", binding.ExecutionID.String(), binding.Input.PayloadID)
			result := x.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := x.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				ctx, cancel := context.WithCancel(ctx)
				defer cancel()
				var borrowed []byte
				var held context.Context
				calls := 0
				request := ac.ExecutionConfigurationRequest{Actor: binding.Actor, ProjectID: binding.ProjectID, AgentID: binding.AgentID, ExecutionID: binding.ExecutionID, Stage: ac.ExecutionConfigurationCurrent}
				sentinel := errors.New("consumer rejected")
				err := x.driver.WithReadFileCallInTx(ctx, tx, plan, func(live context.Context, b tc.ToolCallBinding, raw []byte) error {
					calls++
					if !sameToolCallBinding(b, binding) || !bytes.Equal(raw, original) || x.authority.RequireExecutionConfigurationInTx(live, tx, request) != nil {
						t.Fatal("original input or live witness missing")
					}
					borrowed, held = raw, context.WithoutCancel(live)
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
					t.Fatal("loan mutated storage or remained live")
				}
				requireCode(t, x.authority.RequireExecutionConfigurationInTx(held, tx, request), f.Forbidden)
				if mode == "success" && err != nil || mode == "callback-error" && !errors.Is(err, sentinel) || mode == "callback-cancel" && !errors.Is(err, context.Canceled) {
					t.Fatal("callback outcome lost", err)
				}
				return nil
			})
			if err := commitError(result); err != nil {
				t.Fatal(err)
			}
		})
	}
}

type forgedReadFilePlan struct{ tc.ReadFileCallPlan }

func TestExecutionReadFileAuthorityRejectsBeforeConsumer(t *testing.T) {
	for _, mode := range []string{"public-plan", "other-driver", "foreign-tx", "missing-lock", "cancel-marker", "revoked-mount", "revoked-tool", "tool-rejected", "changed-protected-bytes"} {
		t.Run(mode, func(t *testing.T) {
			x := newReadFileAuthorityControl(t)
			plan, err := x.driver.DiscoverReadFileCall(context.Background(), x.input.Fields().Request.ExecutionID)
			if err != nil {
				t.Fatal(err)
			}
			b := plan.Binding()
			cause, _ := f.NewRecoveryCause("read-file-authority-control", b.ExecutionID.String(), b.Input.PayloadID)
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
					plan = forgedReadFilePlan{plan}
				case "other-driver":
					driver = &ToolCallDriver{driver: x.driver.driver}
				case "foreign-tx":
					tx = f.NewTx()
				case "cancel-marker":
					x.cancelled = true
				case "revoked-mount", "revoked-tool":
					fields := x.agent.Fields()
					if mode == "revoked-mount" {
						fields.AllowedMountIDs = []i.MountID{}
					} else {
						fields.AllowedToolIDs = []i.ToolID{}
					}
					x.agent, err = ac.NewAgentConfig(fields)
					if err != nil {
						t.Fatal(err)
					}
				case "tool-rejected":
					x.toolErr = fault(f.Forbidden)
				case "changed-protected-bytes":
					x.corrupt = true
				}
				calls := 0
				err = driver.WithReadFileCallInTx(ctx, tx, plan, func(context.Context, tc.ToolCallBinding, []byte) error { calls++; return nil })
				if err == nil || calls != 0 {
					t.Fatal("invalid current authority reached consumer", err)
				}
				return nil
			})
			if err := commitError(result); err != nil {
				t.Fatal(err)
			}
		})
	}
}
