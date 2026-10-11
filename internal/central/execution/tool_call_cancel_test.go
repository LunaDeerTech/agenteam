package execution

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// This is controlled canonical SQL, not a claimed public Cancel producer.
// The second transaction is deliberately rejected: the assertion is transfer
// to the existing closing owner, never a fabricated terminal/retirement.
type toolCancelStore struct {
	*launchStore
	facts        directTextModelFacts
	run          *directTextCall
	transactions int
	unknown      bool
}

func (s *toolCancelStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, e := s.launchStore.InTx(tx); e != nil {
		return nil, e
	}
	return s, nil
}
func (s *toolCancelStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions++
	if s.transactions > 1 {
		if s.run.phase != "closing" || !s.run.allJoined || !errors.Is(s.run.invocation.callError, context.Canceled) {
			s.t.Fatal("did not enter real cancelled closing owner")
		}
		return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
	}
	result := s.launchStore.WithinTx(ctx, cause, fn)
	if s.unknown {
		return f.UnknownResult(newTestID[f.TransactionAttempt](s.t), cause)
	}
	return result
}
func (s *toolCancelStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	if !s.live || ctx.Err() != nil {
		return launchScan{err: errors.New("not live")}
	}
	sf, rf := s.facts.Snapshot.Fields(), s.facts.Round.Fields()
	q := sf.Context.Input().Fields().Request
	if strings.Contains(query, "FROM agenteam_execution.executions WHERE") {
		raw, _ := json.Marshal(q.Launch)
		digest, _ := q.Launch.Digest()
		at := sf.CreatedAt.Time()
		sid := sf.ID.String()
		return launchScan{values: []any{q.ExecutionID.String(), q.Launch.ProjectID.String(), q.Launch.AgentID.String(), "running", int64(3), &at, &sid, at, &at, (*time.Time)(nil), raw, q.Launch.Meta.IdempotencyKey.String(), q.Launch.Meta.RequestID.String(), string(digest)}}
	}
	if strings.Contains(query, "FROM agenteam_execution.snapshots s JOIN") {
		return launchScan{values: []any{s.facts.Snapshot.CanonicalBytes(), string(s.facts.Snapshot.Digest()), q.ExecutionID.String(), q.Launch.ProjectID.String(), q.Launch.AgentID.String(), sf.ID.String(), sf.StartID.String(), sf.ProcessID.String(), string(sf.Context.InputDigest()), string(sf.Context.Digest()), sf.CreatedAt.Time(), int64(2), newTestID[event.EventIdentity](s.t).String(), s.facts.Round.CanonicalBytes(), string(s.facts.Round.Digest()), rf.ID.String(), rf.InputBindingID.String(), rf.CallID.String(), string(rf.Input.Digest), string(rf.ContextDigest), rf.CreatedAt.Time(), int64(1), (*string)(nil), (*int64)(nil), (*string)(nil), (*string)(nil), []byte(nil), (*string)(nil), (*time.Time)(nil), false, (*time.Time)(nil)}}
	}
	if strings.Contains(query, "FROM agenteam_execution.transcript_entries") {
		raw, _ := directTextJSON(rf.Messages[1])
		return launchScan{values: []any{rf.ID.String(), "input", raw, string(ec.TriggerInputDigest(raw)), sf.CreatedAt.Time()}}
	}
	s.t.Fatal("unexpected SQL before cancelled checkpoint")
	return launchScan{}
}

func TestExecutionToolCallCanonicalCancelClosesOnlyAfterKnownRollback(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "known rollback", true: "unknown retained"}[unknown], func(t *testing.T) {
			facts, request := directTextDrainCandidate(t)
			model := &directTextDrainControl{t: t, canJoin: true}
			loop, err := agentloop.NewDirectTextController(model)
			if err != nil {
				t.Fatal(err)
			}
			session, err := loop.Accept(request)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = session.Result(context.Background())
			if err = session.Drain(context.Background()); err != nil {
				t.Fatal(err)
			}
			if !session.Joined() {
				t.Fatal("original handle not joined")
			}
			definition := tc.ReadFileDefinition()
			tool := mc.Tool{Name: "tool_01900000000070008000000000000901", Description: definition.Description, InputSchema: definition.InputSchema, OutputSchema: definition.OutputSchema}
			rf := facts.Round.Fields()
			rf.Input.SchemaVersion = 2
			rf.Input.Digest, err = ec.ToolCallInputDigest(context.Background(), rf.Messages, tool)
			if err != nil {
				t.Fatal(err)
			}
			round, err := ec.NewToolCallRound(ec.ToolCallRoundFields{Round: rf, Tool: tool})
			if err != nil {
				t.Fatal(err)
			}
			facts.Round = toolExecutionRound{round}
			x := newLaunchControl(t)
			store := &toolCancelStore{launchStore: x.store, facts: facts, unknown: unknown}
			state := &directTextState{store: store, tools: &toolCallDependencies{}, processes: &preparationProcesses{process: facts.ProcessID}, process: facts.ProcessID, calls: map[i.ExecutionID]*directTextCall{}, changed: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			run := &directTextCall{owner: state, ctx: ctx, cancel: cancel, request: facts.Snapshot.Fields().Context.Input().Fields().Request, snapshot: facts.Snapshot, round: facts.Round, allJoined: true, started: true, summary: ec.Summary{Status: ec.Running, Version: 3}, invocation: &directTextInvocation{session: session}, toolFacts: &toolCallCandidate{payload: newTestID[toolArgumentPayload](t)}}
			run.locks = []f.LockRequest{executionLock(run.request.ExecutionID)}
			run.runtimeLocks = run.locks
			state.calls[run.request.ExecutionID] = run
			store.run = run
			_, err = state.saveToolCallFacts(ctx, run, mc.ModelResponse{})
			if err == nil || ctx.Err() != nil || store.writes != 0 {
				t.Fatal("cancel marker published facts or cancelled original ctx")
			}
			if unknown {
				if store.transactions != 1 || run.uncertainty != "tool-checkpoint" || run.unresolved == nil {
					t.Fatal("unknown outcome was converted to cancellation")
				}
			} else if store.transactions != 2 || run.uncertainty != "closing" {
				t.Fatal("known cancel remained stuck in tool-writing")
			}
			if err = loop.Drain(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
