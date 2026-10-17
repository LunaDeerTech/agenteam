package meeting

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	"github.com/jackc/pgx/v5"
)

func TestAgentToolInputRejectsForgedSourcesAndUndeclaredCommands(t *testing.T) {
	for _, test := range []struct{ handler, raw string }{
		{"meeting.request", `{"proposal_content":"Review deployment","source_execution_id":"01900000-0000-7000-8000-000000000001"}`},
		{"meeting.request", `{"proposal_content":"Review deployment","origin":{"kind":"task","id":"01900000-0000-7000-8000-000000000001"}}`},
		{"meeting.request", `{"proposal_content":" \n "}`},
		{"meeting.request", `{"proposal_content":"Review","proposal_content":"Changed"}`},
		{"meeting.request", `{"proposal_content":"Review","references":[{"kind":"task","id":"01900000-0000-7000-8000-000000000001"}]}`},
		{"meeting.list", `{"status":"deleted"}`},
		{"meeting.list", `{"limit":101}`},
		{"meeting.update", `{"proposal_content":"not yet defined"}`},
		{"meeting.comment", `{"content":"not yet defined"}`},
	} {
		if _, err := ParseAgentInput(test.handler, []byte(test.raw)); err == nil {
			t.Errorf("accepted %s %s", test.handler, test.raw)
		}
	}
	for _, test := range []struct{ handler, raw string }{
		{"meeting.list", `{}`}, {"meeting.list", `{"status":"proposed","limit":10}`}, {"meeting.request", `{"proposal_content":"Review deployment"}`},
		{"meeting.add-agent", `{"meeting_id":"01900000-0000-7000-8000-000000000001","expected_version":"2","agent_id":"01900000-0000-7000-8000-000000000002"}`},
	} {
		if _, err := ParseAgentInput(test.handler, []byte(test.raw)); err != nil {
			t.Errorf("rejected %s: %v", test.handler, err)
		}
	}
}

// Controlled authority and Store ports isolate notification ordering. This
// does not replace the separate private Builtin/Execution authority tests.
type agentNotificationStore struct {
	Store
	t                              *testing.T
	call                           c.AgentToolCall
	input                          []byte
	result                         c.AgentResult
	tx                             f.Tx
	active, found, unknown, reject bool
}

func (s *agentNotificationStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.tx, s.active = f.NewTx(), true
	err := fn(ctx, s.tx)
	s.active = false
	if err != nil || s.reject {
		return f.NotCommittedResult(fault(f.Forbidden))
	}
	if s.unknown {
		return f.UnknownResult(summaryTestID[f.TransactionAttempt](s.t), cause)
	}
	return f.CommittedResult()
}
func (s *agentNotificationStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error { return nil }
func (s *agentNotificationStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !s.active || tx != s.tx {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *agentNotificationStore) QueryRow(_ context.Context, sql string, args ...any) postgres.Row {
	return controlEventRow(func(dest ...any) error {
		if !s.active || !strings.HasPrefix(sql, "SELECT attempt_id,request_id,") || len(args) != 1 || args[0] != s.call.OperationID.String() {
			s.t.Fatal("unexpected receipt read")
		}
		if !s.found {
			return pgx.ErrNoRows
		}
		b := s.call.Call
		values := []string{s.call.AttemptID.String(), string(s.call.RequestID), b.ProjectID.String(), b.AgentID.String(), b.ExecutionID.String(), b.CallID, b.Binding.HandlerID, digest(s.input).String()}
		for n, value := range values {
			*dest[n].(*string) = value
		}
		raw, err := json.Marshal(s.result)
		*dest[8].(*[]byte) = raw
		return err
	})
}

type agentNotificationAuthority struct{ store *agentNotificationStore }

func (a agentNotificationAuthority) DiscoverMeetingTool(context.Context, c.AgentToolCall, bool) ([]f.LockRequest, error) {
	return nil, nil
}
func (a agentNotificationAuthority) RequireMeetingToolBindingInTx(_ context.Context, tx f.Tx, store any) error {
	if store != a.store {
		return fault(f.Forbidden)
	}
	_, err := a.store.InTx(tx)
	return err
}
func (a agentNotificationAuthority) WithMeetingToolInputInTx(ctx context.Context, tx f.Tx, call c.AgentToolCall, _ bool, fn func(context.Context, []byte, c.AgentSource) error) error {
	if _, err := a.store.InTx(tx); err != nil {
		return err
	}
	if call.OperationID != a.store.call.OperationID {
		return fault(f.Forbidden)
	}
	return fn(ctx, a.store.input, c.AgentSource{ExecutionID: call.Call.ExecutionID})
}

type agentNotificationPublisher struct {
	store   *agentNotificationStore
	count   int
	project i.ProjectID
	meeting string
}

func (p *agentNotificationPublisher) InvalidateMeeting(project i.ProjectID, meeting string) {
	if p.store.active {
		p.store.t.Fatal("notification escaped before original transaction returned")
	}
	p.count++
	p.project, p.meeting = project, meeting
}
func (p *agentNotificationPublisher) InvalidateExecution(i.ProjectID, i.ExecutionID) {
	p.store.t.Fatal("Agent Meeting operation published an Execution body")
}

func TestMeetingAgentRealtimeOnlyAfterKnownOriginalReceipt(t *testing.T) {
	for _, mode := range []string{"execute", "lookup", "unknown", "rejected", "not-found", "list"} {
		t.Run(mode, func(t *testing.T) {
			project, agent, execution := summaryTestID[i.Project](t), summaryTestID[i.Agent](t), summaryTestID[i.Execution](t)
			actor, err := i.NewAgentRun(project, agent, execution)
			if err != nil {
				t.Fatal(err)
			}
			meeting, version := summaryTestID[c.Meeting](t), f.Version(1)
			in := c.AgentInput{MeetingID: &meeting, ExpectedVersion: &version, AgentID: &agent}
			handler := "meeting.add-agent"
			if mode == "list" {
				handler, in = "meeting.list", c.AgentInput{}
			}
			raw, err := canonical(in)
			if err != nil {
				t.Fatal(err)
			}
			tool := summaryTestID[i.Tool](t)
			call := c.AgentToolCall{OperationID: summaryTestID[tc.Operation](t), AttemptID: summaryTestID[tc.Attempt](t), RequestID: p.ID(summaryTestID[f.Request](t).String()), Call: tc.ToolCallBinding{Actor: actor, ProjectID: project, AgentID: agent, ExecutionID: execution, RoundID: summaryTestID[struct{}](t).String(), SnapshotID: summaryTestID[struct{}](t).String(), InputBindingID: summaryTestID[struct{}](t).String(), LogicalCallID: summaryTestID[mc.Call](t), InvocationID: summaryTestID[mc.Invocation](t), CallID: "original-meeting-call", ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), Spec: tc.SpecRef{ToolID: tool, SpecRevision: 1}, Binding: tc.BuiltinBinding{HandlerID: handler, ContractRevision: 1}, Input: tc.ToolInputReference{PayloadID: summaryTestID[struct{}](t).String(), SHA256: digest(raw), Bytes: f.Progress(len(raw))}, CanonicalArguments: digest(raw)}}
			if err := call.Validate(); err != nil {
				t.Fatal(err)
			}
			store := &agentNotificationStore{t: t, call: call, input: raw, found: mode != "not-found", unknown: mode == "unknown", reject: mode == "rejected", result: c.AgentResult{Receipt: &c.Receipt{MeetingID: meeting, Version: 2}}}
			publisher := &agentNotificationPublisher{store: store}
			service := &AgentService{store: store, deps: AgentDependencies{Tools: agentNotificationAuthority{store}, Realtime: publisher}}
			if mode == "execute" {
				_, err = service.Execute(context.Background(), call, in)
			} else {
				_, _, err = service.Lookup(context.Background(), call, in)
			}
			if (err != nil) != (mode == "unknown" || mode == "rejected") {
				t.Fatal(mode, err)
			}
			want := 0
			if mode == "execute" || mode == "lookup" {
				want = 1
			}
			if publisher.count != want || want == 1 && (publisher.project != project || publisher.meeting != meeting.String()) {
				t.Fatal("wrong original committed notification", publisher.count, publisher.project, publisher.meeting)
			}
		})
	}
}
