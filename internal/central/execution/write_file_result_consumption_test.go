package execution

import (
	"bytes"
	"context"
	"encoding/json"
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
type writeConsumptionControl struct {
	*readFileAuthorityControl
	version            int64
	consumption, entry []any
	projectErr         error
	projectChecks      int
	result             tc.WriteFileResultBinding
	raw                []byte
	alter              string
}

type writeConsumptionPlan struct {
	binding tc.WriteFileResultBinding
	locks   []f.LockRequest
}

func (p *writeConsumptionPlan) Binding() tc.WriteFileResultBinding { return p.binding }
func (p *writeConsumptionPlan) RequiredLocks() []f.LockRequest {
	return append([]f.LockRequest(nil), p.locks...)
}

func newWriteConsumptionControl(t *testing.T) (*writeConsumptionControl, *ToolResultConsumer) {
	t.Helper()
	x := newWriteFileAuthorityControl(t, []byte("file-data-must-not-enter-receipt"))
	call, err := toolBinding(&x.stored)
	if err != nil {
		t.Fatal(err)
	}
	code := p.InternalError
	raw, err := p.EncodeWriteFileResult(p.WriteFileResult{Path: "write-canary.bin", State: "committed", BytesWritten: len("file-data-must-not-enter-receipt"), Durable: false, CompletionCode: &code})
	if err != nil {
		t.Fatal(err)
	}
	digest := c.TriggerInputDigest([]byte("original-operation"))
	s := &writeConsumptionControl{readFileAuthorityControl: x, version: 3, raw: raw, result: tc.WriteFileResultBinding{
		Call: call, OperationID: newTestID[tc.Operation](t), AttemptID: newTestID[tc.Attempt](t), RequestID: p.ID(newTestID[struct{}](t).String()),
		InputDigest: digest, Fingerprint: digest, TerminalDigest: digest, Outcome: "success", Attempted: true, ResultDigest: c.TriggerInputDigest(raw), ResultBytes: f.Progress(len(raw)),
	}}
	x.driver.driver.state.store = s
	x.driver.driver.state.deps.Projects = s
	owner, err := NewWriteFileResultConsumer(x.driver, s)
	if err != nil {
		t.Fatal(err)
	}
	return s, owner
}
func (s *writeConsumptionControl) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.launchStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *writeConsumptionControl) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	switch {
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
func (s *writeConsumptionControl) Exec(ctx context.Context, q string, args ...any) (pgconn.CommandTag, error) {
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
func (s *writeConsumptionControl) RequirePreparingProjectInTx(ctx context.Context, tx f.Tx, project i.ProjectID) (pc.ProjectRef, error) {
	s.projectChecks++
	if s.projectErr != nil {
		return pc.ProjectRef{}, s.projectErr
	}
	return s.readFileAuthorityControl.RequirePreparingProjectInTx(ctx, tx, project)
}
func (s *writeConsumptionControl) DiscoverWriteFileResult(ctx context.Context, e i.ExecutionID) (tc.WriteFileResultPlan, error) {
	plan, err := s.driver.DiscoverCommittedWriteFileCall(ctx, e)
	if err != nil {
		return nil, err
	}
	return &writeConsumptionPlan{binding: s.result, locks: plan.RequiredLocks()}, nil
}
func (s *writeConsumptionControl) WithWriteFileResultInTx(ctx context.Context, tx f.Tx, plan tc.WriteFileResultPlan, fn func(context.Context, tc.WriteFileResultBinding, []byte) error) error {
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

func TestExecutionWriteResultHistoricalReplayAndCurrentGate(t *testing.T) {
	for _, mode := range []string{"first-and-history", "cancelled-before-first", "project-denied", "wrong-path", "wrong-length", "request-id", "unknown-publication"} {
		t.Run(mode, func(t *testing.T) {
			s, owner := newWriteConsumptionControl(t)
			e := s.result.Call.ExecutionID
			fields := s.agent.Fields()
			fields.AllowedToolIDs = fields.AllowedToolIDs[:0]
			fields.AllowedMountIDs = fields.AllowedMountIDs[:0]
			var err error
			s.agent, err = ac.NewAgentConfig(fields)
			if err != nil {
				t.Fatal(err)
			}
			s.toolErr = fault(f.DependencyUnbound)
			switch mode {
			case "cancelled-before-first":
				s.cancelled = true
			case "project-denied":
				s.projectErr = fault(f.Forbidden)
			case "request-id":
				s.alter = mode
			case "wrong-path", "wrong-length":
				v, err := p.DecodeWriteFileResult(s.raw)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "wrong-path" {
					v.Path = "other.bin"
				} else {
					v.BytesWritten--
				}
				s.raw, err = p.EncodeWriteFileResult(v)
				if err != nil {
					t.Fatal(err)
				}
				s.result.ResultDigest = c.TriggerInputDigest(s.raw)
				s.result.ResultBytes = f.Progress(len(s.raw))
			case "unknown-publication":
				s.raw = bytes.Replace(s.raw, []byte(`"committed"`), []byte(`"unknown"`), 1)
				s.result.ResultDigest = c.TriggerInputDigest(s.raw)
				s.result.ResultBytes = f.Progress(len(s.raw))
			}
			receipt, err := owner.Consume(context.Background(), e)
			if mode != "first-and-history" {
				if err == nil || s.writes != 0 || s.version != 3 || s.entry != nil || s.consumption != nil {
					t.Fatal("invalid first consumption wrote a checkpoint")
				}
				return
			}
			if err != nil || receipt.Version != 4 || s.version != 4 || s.writes != 3 || s.projectChecks != 1 {
				t.Fatal("first historical write receipt did not atomically checkpoint", err)
			}
			if bytes.Contains(s.entry[2].([]byte), []byte("file-data-must-not-enter-receipt")) {
				t.Fatal("write data entered new ToolResult")
			}
			var message mc.Message
			if json.Unmarshal(s.entry[2].([]byte), &message) != nil || len(message.Parts) != 1 || message.Parts[0].ToolResult == nil || len(message.Parts[0].ToolResult.Parts) != 1 || message.Parts[0].ToolResult.Parts[0].Text == nil || message.Parts[0].ToolResult.Parts[0].Text.Text != string(s.raw) {
				t.Fatal("durability/late completion receipt changed")
			}
			s.cancelled = true
			s.projectErr = fault(f.Forbidden)
			again, err := owner.Consume(context.Background(), e)
			if err != nil || again != receipt {
				t.Fatal("completed replay renewed mutation authorization", err)
			}
			newOwner, err := NewWriteFileResultConsumer(s.driver, s)
			if err != nil {
				t.Fatal(err)
			}
			got, found, err := newOwner.Lookup(context.Background(), e)
			if err != nil || !found || got != receipt || s.projectChecks != 1 || s.writes != 3 || s.version != 4 {
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

func TestExecutionWriteResultUnknownUsesOriginalObservation(t *testing.T) {
	s, owner := newWriteConsumptionControl(t)
	s.unknown = true
	e := s.result.Call.ExecutionID
	_, err := owner.Consume(context.Background(), e)
	if err == nil || owner.pending[e] == nil || s.version != 4 || s.writes != 3 {
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
	s.cancelled = true
	s.projectErr = fault(f.Forbidden)
	got, resolveErr := owner.ResolveUnknown(context.Background(), e)
	if resolveErr != nil || got != original.candidate.receipt || owner.pending[e] != nil || s.writes != 3 || s.projectChecks != 1 {
		t.Fatal("exact historical observation did not recover original candidate", resolveErr)
	}
	if err = owner.Drain(context.Background()); err != nil || !owner.Joined() {
		t.Fatal(err)
	}
}
