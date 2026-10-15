package execution

import (
	"context"
	"errors"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type toolPolicyClosingStore struct {
	*launchStore
	run          *directTextCall
	transactions int
}

func (s *toolPolicyClosingStore) WithinTx(_ context.Context, _ f.TransactionCause, _ func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions++
	if s.run.phase != "closing" || !s.run.allJoined || !s.run.invocation.session.Joined() || !s.run.toolFailed || s.run.toolPending {
		s.t.Fatal("policy rejection did not enter the original joined terminal owner")
	}
	requireCode(s.t, s.run.invocation.callError, f.Forbidden)
	// Deliberately refuse the terminal transaction: this gate proves handoff,
	// not a fabricated durable terminal, Tool dispatch, or lease retirement.
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func TestExecutionToolCallPolicyRejectionRequiresKnownUnwrittenCheckpoint(t *testing.T) {
	for _, mode := range []string{"local-policy-not-committed", "local-policy-unknown", "other-authority-forbidden", "database-error"} {
		t.Run(mode, func(t *testing.T) {
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
			if err = session.Drain(context.Background()); err != nil || !session.Joined() {
				t.Fatal("original model did not join", err)
			}
			defer func() {
				if err := loop.Drain(context.Background()); err != nil {
					t.Error(err)
				}
			}()

			captured := tc.ExecutionTool{ToolID: newTestID[i.Tool](t), BindingSnapshot: tc.BuiltinExecutionBinding{Class: tc.OrdinaryTool}}
			currentFields := facts.Snapshot.Fields().Context.Input().Fields().Agent.Fields()
			currentFields.AllowedToolIDs = []i.ToolID{}
			current, err := ac.NewAgentConfig(currentFields)
			if err != nil {
				t.Fatal(err)
			}
			policyErr := requireCurrentCapturedTool(current, facts.Snapshot.Fields().Context.Input().Fields().Request.Launch.Policy, captured)
			var rejection *capturedToolPolicyRejection
			if !errors.As(policyErr, &rejection) {
				t.Fatal("actual local policy did not identify the rejection")
			}
			result := f.NotCommittedResult(fault(f.Forbidden))
			switch mode {
			case "local-policy-unknown":
				cause, _ := f.NewJobCause("execution-tool-call-checkpoint", facts.Snapshot.Fields().Context.Input().Fields().Request.ExecutionID.String(), "original")
				result = f.UnknownResult(newTestID[f.TransactionAttempt](t), cause)
			case "other-authority-forbidden":
				result, rejection = f.NotCommittedResult(fault(f.Forbidden)), nil
			case "database-error":
				result, rejection = f.NotCommittedResult(fault(f.DependencyUnavailable)), nil
			}

			x := newLaunchControl(t)
			store := &toolPolicyClosingStore{launchStore: x.store}
			state := &directTextState{store: store, calls: map[i.ExecutionID]*directTextCall{}, changed: make(chan struct{})}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			run := &directTextCall{owner: state, ctx: ctx, cancel: cancel, request: facts.Snapshot.Fields().Context.Input().Fields().Request, snapshot: facts.Snapshot, round: facts.Round, phase: "tool-writing", allJoined: true, started: true, summary: ec.Summary{Status: ec.Running, Version: 3}, invocation: &directTextInvocation{session: session}, toolFacts: &toolCallCandidate{payload: newTestID[toolArgumentPayload](t)}}
			store.run = run
			state.calls[run.request.ExecutionID] = run
			_, err = state.completeToolCallCheckpoint(run, result, false, rejection)
			if err == nil || run.ctx.Err() != nil || run.toolPending || store.writes != 0 {
				t.Fatal("rejection published a checkpoint or cancelled the original context")
			}
			if mode == "local-policy-not-committed" {
				if store.transactions != 1 || run.uncertainty != "closing" || run.invocation.callError != policyErr {
					t.Fatal("known local policy rejection stayed in checkpoint recovery")
				}
			} else {
				want := "tool-writing"
				if mode == "local-policy-unknown" {
					want = "tool-checkpoint"
					requireCode(t, err, f.CommitUnknown)
				}
				if store.transactions != 0 || run.uncertainty != want || run.invocation.callError != nil || run.toolFailed || run.unresolved == nil {
					t.Fatal("an uncertain or unrelated failure was converted into a terminal")
				}
			}
		})
	}
}
