package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5/pgconn"
)

// Only SQL rows and the foreign Tool-result port are controlled here. Actual
// Execution discovery, private history proof, locks, projection and checkpoint
// SQL execute. This is not proof of durable PostgreSQL or Runner publication.
type commandConsumptionControl struct {
	*readFileAuthorityControl
	version            int64
	consumption, entry []any
	projectErr         error
	projectChecks      int
	result             tc.RunCommandResultBinding
	raw                []byte
	alter              string
}

type commandConsumptionPlan struct {
	binding tc.RunCommandResultBinding
	locks   []f.LockRequest
}

func (p *commandConsumptionPlan) Binding() tc.RunCommandResultBinding { return p.binding }
func (p *commandConsumptionPlan) RequiredLocks() []f.LockRequest {
	return append([]f.LockRequest(nil), p.locks...)
}

func newCommandConsumptionControl(t *testing.T) (*commandConsumptionControl, *ToolResultConsumer) {
	t.Helper()
	x := newRunCommandAuthorityControl(t)
	call, err := toolBinding(&x.stored)
	if err != nil {
		t.Fatal(err)
	}
	report := p.RunCommandResult{State: "completed", ExitCode: 23, DurationMS: "7", StdoutBytes: "5", StderrBytes: "4", OutputComplete: true, ProcessGroupTerminationConfirmed: true}
	raw, err := tc.EncodeRunCommandResultBundle(tc.RunCommandResultBundle{Report: report, Stdout: []byte("out\x00x"), Stderr: []byte("err\n")})
	if err != nil {
		t.Fatal(err)
	}
	digest := c.TriggerInputDigest([]byte("original-operation"))
	s := &commandConsumptionControl{readFileAuthorityControl: x, version: 3, raw: raw, result: tc.RunCommandResultBinding{
		Call: call, OperationID: newTestID[tc.Operation](t), AttemptID: newTestID[tc.Attempt](t), RequestID: p.ID(newTestID[struct{}](t).String()),
		InputDigest: digest, Fingerprint: digest, TerminalDigest: digest, Outcome: "failure", Code: "command_exit_nonzero", Attempted: true, ResultDigest: c.TriggerInputDigest(raw), ResultBytes: f.Progress(len(raw)),
	}}
	x.driver.driver.state.store = s
	x.driver.driver.state.deps.Projects = s
	owner, err := NewRunCommandResultConsumer(x.driver, s)
	if err != nil {
		t.Fatal(err)
	}
	return s, owner
}
func (s *commandConsumptionControl) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *commandConsumptionControl) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	switch {
	case strings.HasPrefix(q, "SELECT binding,binding_digest FROM agenteam_execution.tool_result_consumptions"):
		if s.consumption == nil {
			return launchScan{}
		}
		return launchScan{values: []any{s.consumption[11], s.consumption[12]}}
	case strings.Contains(q, "FROM agenteam_execution.tool_result_consumptions"):
		return launchScan{values: s.consumption}
	case strings.Contains(q, "FROM agenteam_execution.transcript_entries") && args[1].(int64) == 3:
		return launchScan{values: s.entry}
	case strings.Contains(q, "FROM agenteam_execution.executions WHERE"):
		row := s.readFileAuthorityControl.QueryRow(ctx, q, args...).(launchScan)
		row.values[4] = s.version
		return row
	default:
		return s.readFileAuthorityControl.QueryRow(ctx, q, args...)
	}
}
func (s *commandConsumptionControl) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !s.live || ctx.Err() != nil {
		s.t.Fatal("write outside original transaction")
	}
	s.writes++
	switch {
	case strings.Contains(q, "INSERT INTO agenteam_execution.transcript_entries"):
		if args[1] != int64(3) || s.entry != nil {
			s.t.Fatal("duplicate or wrong checkpoint entry")
		}
		s.entry = []any{args[2], args[3], bytes.Clone(args[4].([]byte)), args[5], args[6]}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(q, "INSERT INTO agenteam_execution.tool_result_consumptions"):
		if s.consumption != nil {
			s.t.Fatal("duplicate checkpoint receipt")
		}
		s.consumption = []any{args[1], args[2], args[3], args[4], args[5], args[6], args[7], args[8], args[9], args[10], args[11], bytes.Clone(args[12].([]byte)), args[13], args[14], int64(3), args[15], args[16]}
		return pgconn.NewCommandTag("INSERT 0 1"), nil
	case strings.Contains(q, "UPDATE agenteam_execution.executions SET version=version+1"):
		if args[1] != s.version || s.cancelled {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		s.version++
		return pgconn.NewCommandTag("UPDATE 1"), nil
	default:
		s.t.Fatal("unexpected result write")
		return pgconn.CommandTag{}, nil
	}
}
func (s *commandConsumptionControl) RequirePreparingProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.ProjectRef, error) {
	s.projectChecks++
	if s.projectErr != nil {
		return pc.ProjectRef{}, s.projectErr
	}
	return s.readFileAuthorityControl.RequirePreparingProjectInTx(ctx, tx, project)
}
func (s *commandConsumptionControl) DiscoverRunCommandResult(ctx context.Context, e i.ExecutionID) (tc.RunCommandResultPlan, error) {
	plan, err := s.driver.DiscoverCommittedRunCommandCall(ctx, e)
	if err != nil {
		return nil, err
	}
	return &commandConsumptionPlan{binding: s.result, locks: plan.RequiredLocks()}, nil
}
func (s *commandConsumptionControl) WithRunCommandResultInTx(ctx context.Context, tx f.Tx, plan tc.RunCommandResultPlan, fn func(context.Context, tc.RunCommandResultBinding, []byte) error) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	if err := s.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); err != nil {
		return err
	}
	raw := bytes.Clone(s.raw)
	defer clear(raw)
	actual := s.result
	if s.alter == "request-id" {
		actual.RequestID = p.ID(newTestID[struct{}](s.t).String())
	}
	return fn(ctx, actual, raw)
}

func setCommandConsumptionReport(t *testing.T, s *commandConsumptionControl, cancelled, zero bool) {
	t.Helper()
	v, err := tc.DecodeRunCommandResultBundle(context.Background(), s.raw)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(v.Stdout)
	defer clear(v.Stderr)
	if cancelled {
		code := p.CancelledCode
		v.Report.CompletionCode = &code
		v.Report.Cancelled, v.Report.ExitCode, v.Report.Signal = true, -1, 15
	} else if zero {
		v.Report.ExitCode = 0
	}
	s.raw, err = tc.EncodeRunCommandResultBundle(v)
	if err != nil {
		t.Fatal(err)
	}
	s.result.Outcome, s.result.Code = tc.ClassifyRunCommandResult(v.Report)
	s.result.ResultDigest, s.result.ResultBytes = c.TriggerInputDigest(s.raw), f.Progress(len(s.raw))
}

func TestExecutionRunCommandResultHistoricalReplayAndCurrentGate(t *testing.T) {
	for _, mode := range []string{"exit23-and-history", "command-cancelled", "execution-cancelled-before-first", "project-denied", "request-id", "wrong-classification", "partial-stream"} {
		t.Run(mode, func(t *testing.T) {
			s, owner := newCommandConsumptionControl(t)
			e := s.result.Call.ExecutionID
			fields := s.agent.Fields()
			fields.AllowedToolIDs = fields.AllowedToolIDs[:0]
			fields.AllowedMountIDs = fields.AllowedMountIDs[:0]
			fields.Core.ApprovalPolicy = ac.ApprovalDefault
			var err error
			s.agent, err = ac.NewAgentConfig(fields)
			if err != nil {
				t.Fatal(err)
			}
			env := &commandEnvControl{x: s.readFileAuthorityControl, fail: fault(f.CapabilityUnsupported)}
			s.driver.driver.state.tools.currentEnvironment = env
			s.toolErr = fault(f.DependencyUnbound)
			switch mode {
			case "command-cancelled":
				setCommandConsumptionReport(t, s, true, false)
			case "execution-cancelled-before-first":
				s.cancelled = true
			case "project-denied":
				s.projectErr = fault(f.Forbidden)
			case "request-id":
				s.alter = mode
			case "wrong-classification":
				s.result.Outcome, s.result.Code = "success", ""
			case "partial-stream":
				var bundle map[string]json.RawMessage
				if json.Unmarshal(s.raw, &bundle) != nil {
					t.Fatal("fixture bundle")
				}
				bundle["stdout"] = json.RawMessage(`"out"`)
				s.raw, err = directTextJSON(bundle)
				if err != nil {
					t.Fatal(err)
				}
				s.result.ResultDigest, s.result.ResultBytes = c.TriggerInputDigest(s.raw), f.Progress(len(s.raw))
			}
			receipt, err := owner.Consume(context.Background(), e)
			if mode != "exit23-and-history" && mode != "command-cancelled" {
				if err == nil || s.writes != 0 || s.version != 3 || s.entry != nil || s.consumption != nil {
					t.Fatal("invalid first consumption wrote a checkpoint")
				}
				return
			}
			if err != nil || receipt.Version != 4 || s.version != 4 || s.writes != 3 || s.projectChecks != 1 || env.calls != 0 || s.cancelled {
				t.Fatal("known command result did not checkpoint without renewed dispatch authority", err)
			}
			var message mc.Message
			if json.Unmarshal(s.entry[2].([]byte), &message) != nil || len(message.Parts) != 1 || message.Parts[0].ToolResult == nil || len(message.Parts[0].ToolResult.Parts) != 1 || message.Parts[0].ToolResult.Parts[0].Text == nil || message.Parts[0].ToolResult.Parts[0].Text.Text != string(s.raw) || !message.Parts[0].ToolResult.IsError {
				t.Fatal("complete nonzero/cancelled report or error classification changed")
			}
			if bytes.Contains(s.entry[2].([]byte), []byte("private-command-argument")) {
				t.Fatal("original argv entered new result")
			}
			s.cancelled, s.projectErr = true, fault(f.Forbidden)
			again, err := owner.Consume(context.Background(), e)
			if err != nil || again != receipt {
				t.Fatal("completed replay renewed mutation authorization", err)
			}
			newOwner, err := NewRunCommandResultConsumer(s.driver, s)
			if err != nil {
				t.Fatal(err)
			}
			got, found, err := newOwner.Lookup(context.Background(), e)
			if err != nil || !found || got != receipt || s.projectChecks != 1 || s.writes != 3 || s.version != 4 || env.calls != 0 {
				t.Fatal("historical lookup mutated or required a current grant", err)
			}
			if err = owner.Drain(context.Background()); err != nil || !owner.Joined() {
				t.Fatal(err)
			}
			if err = newOwner.Drain(context.Background()); err != nil || !newOwner.Joined() {
				t.Fatal(err)
			}
		})
	}
}

func TestExecutionRunCommandResultUnknownUsesOriginalObservation(t *testing.T) {
	s, owner := newCommandConsumptionControl(t)
	setCommandConsumptionReport(t, s, true, false)
	s.unknown = true
	e := s.result.Call.ExecutionID
	_, err := owner.Consume(context.Background(), e)
	if err == nil || owner.pending[e] == nil || s.version != 4 || s.writes != 3 || s.cancelled {
		t.Fatal("controlled committed unknown not retained", err)
	}
	original := owner.pending[e]
	if _, again := owner.Consume(context.Background(), e); again != err || s.writes != 3 {
		t.Fatal("unknown became physical retry")
	}
	before := s.consumption[5]
	s.consumption[5] = newTestID[struct{}](t).String()
	if _, _, readErr := owner.Lookup(context.Background(), e); readErr == nil || owner.pending[e] != original {
		t.Fatal("changed original RID cleared uncertainty")
	}
	s.consumption[5] = before
	s.cancelled, s.projectErr = true, fault(f.Forbidden)
	got, resolveErr := owner.ResolveUnknown(context.Background(), e)
	if resolveErr != nil || got != original.candidate.receipt || owner.pending[e] != nil || s.writes != 3 || s.projectChecks != 1 {
		t.Fatal("exact historical observation did not recover original candidate", resolveErr)
	}
	if err = owner.Drain(context.Background()); err != nil || !owner.Joined() {
		t.Fatal(err)
	}
}

// The two 68/projection readers share this exact closed loader. SQL scalar
// identities remain independent of the canonical binding; only its original
// outcome/code/attempted fields are taken from the sealed object. Projection
// then checks those fields against the complete report, not a success default.
func TestExecutionRunCommandSealedOutcomeBindsWholeResult(t *testing.T) {
	for _, mode := range []string{"exit23", "cancelled", "exit0", "changed-call", "changed-rid", "changed-result-size", "missing-code", "extra-key", "false-success", "changed-binding-digest"} {
		t.Run(mode, func(t *testing.T) {
			s, owner := newCommandConsumptionControl(t)
			if mode == "cancelled" || mode == "exit0" {
				setCommandConsumptionReport(t, s, mode == "cancelled", mode == "exit0")
			}
			if _, err := owner.Consume(context.Background(), s.result.Call.ExecutionID); err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(s.consumption[11].([]byte), &fields) != nil {
				t.Fatal("fixture binding")
			}
			switch mode {
			case "changed-call":
				fields["call_id"] = json.RawMessage(`"other-call"`)
			case "changed-rid":
				fields["request_id"] = json.RawMessage(strconv.Quote(newTestID[struct{}](t).String()))
			case "changed-result-size":
				fields["result_bytes"] = json.RawMessage(strconv.FormatInt(int64(s.result.ResultBytes)+1, 10))
			case "missing-code":
				delete(fields, "code")
			case "extra-key":
				fields["caller_committed"] = json.RawMessage(`true`)
			case "false-success":
				fields["outcome"], fields["code"] = json.RawMessage(`"success"`), json.RawMessage(`""`)
			}
			raw, err := directTextJSON(fields)
			if err != nil {
				t.Fatal(err)
			}
			s.consumption[11], s.consumption[12] = raw, string(c.TriggerInputDigest(raw))
			if mode == "changed-binding-digest" {
				s.consumption[12] = string(c.TriggerInputDigest([]byte("other")))
			}
			expected := sealedToolResultBinding(s.result)
			expected.Outcome, expected.Code, expected.Attempted = "", "", false
			bound, err := loadCommandSealedBinding(context.Background(), s, expected)
			if err == nil {
				_, err = bound.project(context.Background(), s.raw)
			}
			if mode == "exit23" || mode == "cancelled" || mode == "exit0" {
				original, originalErr := sealedToolResultBindingJSON(sealedToolResultBinding(s.result))
				got, gotErr := sealedToolResultBindingJSON(bound)
				if err != nil || originalErr != nil || gotErr != nil || !bytes.Equal(got, original) {
					t.Fatal("sealed original outcome/code changed", err)
				}
			} else if err == nil {
				t.Fatal("changed complete binding/report accepted")
			}
			if s.writes != 3 {
				t.Fatal("closed historical loader performed a write")
			}
		})
	}
}
