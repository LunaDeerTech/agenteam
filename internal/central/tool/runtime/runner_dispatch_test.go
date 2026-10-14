package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

type exactReadDiscovery struct {
	readFileLookupDiscoveryControl
	calls []tc.ToolCallIdentity
}

func (a *exactReadDiscovery) DiscoverReadFileCallFor(_ context.Context, identity tc.ToolCallIdentity) (tc.ReadFileCallPlan, error) {
	a.calls = append(a.calls, identity)
	return readFileLookupPlanControl{a.binding}, nil
}

func TestRunnerExactDiscoveryRejectsWrongRoundAndMissingAuthority(t *testing.T) {
	b := readFileHandoffFixture(t).binding
	for _, mode := range []string{"exact", "earlier-round", "different-call", "different-execution", "changed-snapshot", "missing-extension"} {
		t.Run(mode, func(t *testing.T) {
			ctx, err := exactRunnerBinding(context.Background(), b)
			if err != nil {
				t.Fatal(err)
			}
			a := &exactReadDiscovery{readFileLookupDiscoveryControl: readFileLookupDiscoveryControl{binding: b}}
			var source tc.ReadFileExecutionAuthority = a
			execution := b.ExecutionID
			switch mode {
			case "earlier-round":
				a.binding.RoundID = newOperationID[struct{}](t).String()
			case "different-call":
				a.binding.CallID = "other-round-model-call"
			case "different-execution":
				execution = newOperationID[i.Execution](t)
			case "changed-snapshot":
				a.binding.SnapshotID = newOperationID[struct{}](t).String()
			case "missing-extension":
				source = readFileLookupDiscoveryControl{binding: b}
			}
			plan, err := discoverSelectedReadFileCall(ctx, source, execution)
			if mode == "exact" {
				if err != nil || !sameRunnerCallBinding(plan.Binding(), b) || len(a.calls) != 1 || a.calls[0] != b.Identity() {
					t.Fatal("exact original call was not selected", err)
				}
			} else if err == nil || plan != nil {
				t.Fatal("mismatched or unbound call was accepted")
			}
			if mode == "different-execution" && len(a.calls) != 0 {
				t.Fatal("wrong execution reached producer")
			}
		})
	}
}

type exactReadResultExecution struct{ *resultReaderExecution }

func (a *exactReadResultExecution) DiscoverReadFileCallFor(_ context.Context, identity tc.ToolCallIdentity) (tc.ReadFileCallPlan, error) {
	if !identity.Matches(a.binding) {
		return nil, fail(f.NotFound)
	}
	return readFileLookupPlanControl{a.binding}, nil
}

func TestRunnerResultUsesOriginalReaderAndSameTransaction(t *testing.T) {
	for _, mode := range []string{"complete", "current-revoked", "corrupt-result", "foreign-plan", "consumer-error"} {
		t.Run(mode, func(t *testing.T) {
			_, store, authority, _ := resultReaderFixture(t)
			read, err := NewReadFileResultReader(store, &exactReadResultExecution{authority})
			if err != nil {
				t.Fatal(err)
			}
			reader, err := NewRunnerResultReader(read, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := reader.DiscoverRunnerResult(context.Background(), authority.binding)
			if err != nil {
				t.Fatal(err)
			}
			cause, _ := inputCause(authority.binding)
			calls := 0
			consumerErr := errors.New("consumer rejected result")
			result := store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				switch mode {
				case "current-revoked":
					authority.deny = true
				case "corrupt-result":
					store.badDigest = true
				case "foreign-plan":
					reader, err = NewRunnerResultReader(read, nil, nil)
					if err != nil {
						return err
					}
				}
				beforeTx, beforeAcquire := store.txs, store.acquires
				err := reader.WithRunnerResultInTx(ctx, tx, plan, func(loan context.Context, binding tc.RunnerResultBinding, part mc.ToolResultPart) error {
					calls++
					if loan != ctx || !store.current || binding.Validate() != nil || !sameRunnerCallBinding(binding.Call, authority.binding) || part.CallID != authority.binding.CallID || part.IsError || len(part.Parts) != 1 || part.Parts[0].Text == nil || part.Parts[0].Text.Text != string(store.payload) {
						t.Fatal("original result or live authority was lost")
					}
					if mode == "consumer-error" {
						return consumerErr
					}
					return nil
				})
				if beforeTx != store.txs || beforeAcquire != store.acquires {
					t.Fatal("result bridge began another transaction or acquired locks")
				}
				if mode == "consumer-error" && (err == nil || strings.Contains(err.Error(), consumerErr.Error())) {
					t.Fatal("consumer failure escaped the original safe error boundary")
				}
				return err
			})
			if mode == "complete" {
				if result.State() != f.Committed || calls != 1 {
					t.Fatal("complete original result was not consumed")
				}
			} else if result.State() == f.Committed || mode != "consumer-error" && calls != 0 {
				t.Fatal("unauthorized or corrupt result reached consumer")
			}
		})
	}
}

func TestRunnerDispatchMissingBindingsFailClosed(t *testing.T) {
	if _, err := NewRunnerExecutor(nil, nil, nil); err == nil {
		t.Fatal("empty executor accepted")
	}
	if _, err := NewRunnerResultReader(nil, nil, nil); err == nil {
		t.Fatal("empty result reader accepted")
	}
	if (&RunnerExecutor{}).Joined() || (&RunnerExecutor{}).Drain(context.Background()) == nil {
		t.Fatal("unbound executor claimed joined ownership")
	}
	for _, b := range []tc.ToolCallBinding{readFileHandoffFixture(t).binding, writeFileHandoffFixture(t).binding, runCommandHandoffFixture(t).binding} {
		executor := &RunnerExecutor{}
		if _, err := executor.Execute(context.Background(), b); err == nil {
			t.Fatal("missing route became execution")
		}
		if _, found, err := executor.Lookup(context.Background(), b); err == nil || found {
			t.Fatal("missing route became observation")
		}
		if _, err := (&RunnerResultReader{}).DiscoverRunnerResult(context.Background(), b); err == nil {
			t.Fatal("missing result reader was accepted")
		}
	}
	_, _, _, builtin, _ := operationFixture(t)
	_, err := (&RunnerExecutor{}).Execute(context.Background(), builtin)
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.DependencyUnbound {
		t.Fatal("valid unbound builtin was not distinguished from invalid arguments")
	}
}

type exactWriteResultExecution struct{ *committedWriteControl }

func (a *exactWriteResultExecution) DiscoverCommittedWriteFileCallFor(_ context.Context, identity tc.ToolCallIdentity) (tc.CommittedWriteFileCallPlan, error) {
	if !identity.Matches(a.binding) {
		return nil, fail(f.NotFound)
	}
	return committedWritePlanControl{a.binding}, nil
}

type exactCommandResultExecution struct{ *committedCommandControl }

func (a *exactCommandResultExecution) DiscoverCommittedRunCommandCallFor(_ context.Context, identity tc.ToolCallIdentity) (tc.CommittedRunCommandCallPlan, error) {
	if !identity.Matches(a.binding) {
		return nil, fail(f.NotFound)
	}
	return committedCommandPlanControl{a.binding}, nil
}

func TestRunnerResultPreservesWriteReceiptAndCommandClassification(t *testing.T) {
	t.Run("write-known-publication", func(t *testing.T) {
		_, store, authority, original := writeResultFixture(t, false, writeFileTestCode(p.InternalError))
		write, err := NewWriteFileResultReader(store, &exactWriteResultExecution{authority})
		if err != nil {
			t.Fatal(err)
		}
		reader, err := NewRunnerResultReader(nil, write, nil)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := reader.DiscoverRunnerResult(context.Background(), authority.binding)
		if err != nil {
			t.Fatal(err)
		}
		cause, _ := inputCause(authority.binding)
		calls := 0
		result := store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
			if err := store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
				return err
			}
			return reader.WithRunnerResultInTx(ctx, tx, plan, func(_ context.Context, binding tc.RunnerResultBinding, part mc.ToolResultPart) error {
				calls++
				if binding.Validate() != nil || part.CallID != authority.binding.CallID || part.IsError || len(part.Parts) != 1 || part.Parts[0].Text == nil || part.Parts[0].Text.Text != string(store.payload) {
					t.Fatal("known publication result was lost")
				}
				got, err := p.DecodeWriteFileResult([]byte(part.Parts[0].Text.Text))
				if err != nil || got.Durable != original.Durable || got.CompletionCode == nil || *got.CompletionCode != *original.CompletionCode {
					t.Fatal("late completion fact changed", err)
				}
				return nil
			})
		})
		if result.State() != f.Committed || calls != 1 {
			t.Fatal("write result did not commit")
		}
	})
	for _, outcome := range []string{"success", "failure", "cancelled"} {
		t.Run("command-"+outcome, func(t *testing.T) {
			_, store, authority, original := commandResultFixture(t, outcome)
			command, err := NewRunCommandResultReader(store, &exactCommandResultExecution{authority})
			if err != nil {
				t.Fatal(err)
			}
			reader, err := NewRunnerResultReader(nil, nil, command)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := reader.DiscoverRunnerResult(context.Background(), authority.binding)
			if err != nil {
				t.Fatal(err)
			}
			cause, _ := inputCause(authority.binding)
			calls := 0
			result := store.WithinTx(context.Background(), cause, func(ctx context.Context, tx f.Tx) error {
				if err := store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
					return err
				}
				return reader.WithRunnerResultInTx(ctx, tx, plan, func(_ context.Context, binding tc.RunnerResultBinding, part mc.ToolResultPart) error {
					calls++
					if binding.Validate() != nil || binding.Outcome != outcome || part.CallID != authority.binding.CallID || part.IsError != (outcome != "success") || len(part.Parts) != 1 || part.Parts[0].Text == nil {
						t.Fatal("command classification changed")
					}
					got, err := tc.DecodeRunCommandResultBundle(ctx, []byte(part.Parts[0].Text.Text))
					if err != nil || got.Report.ExitCode != original.ExitCode || got.Report.Cancelled != original.Cancelled || string(got.Stdout) != string(store.stdout) || string(got.Stderr) != string(store.stderr) {
						t.Fatal("complete command result changed", err)
					}
					return nil
				})
			})
			if result.State() != f.Committed || calls != 1 {
				t.Fatal("command result did not commit")
			}
		})
	}
}

func TestRunnerLifecycleWaitsForOriginalLookups(t *testing.T) {
	store := &operationStoreControl{t: t}
	read := &readFileAuthorityState{store: store}
	write := &writeFileAuthorityState{store: store}
	command := &runCommandAuthorityState{store: store}
	readCtx, readReturned, _ := read.beginLookup(context.Background())
	writeCtx, writeReturned, _ := write.beginLookup(context.Background())
	commandCtx, commandReturned, _ := command.beginLookup(context.Background())
	r, err := NewRunnerExecutor(
		&ReadFileExecutor{data: func() *readFileExecutorData { return &readFileExecutorData{authority: read} }},
		&WriteFileExecutor{data: func() *writeFileExecutorData { return &writeFileExecutorData{authority: write} }},
		&RunCommandExecutor{data: func() *runCommandExecutorData { return &runCommandExecutorData{authority: command} }},
	)
	if err != nil {
		t.Fatal(err)
	}
	r.Stop()
	if readCtx.Err() != context.Canceled || writeCtx.Err() != context.Canceled || commandCtx.Err() != context.Canceled || r.Joined() {
		t.Fatal("Stop omitted original calls or claimed early join")
	}
	readReturned()
	writeReturned()
	deadline, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	err = r.Drain(deadline)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || r.Joined() {
		t.Fatal("Drain ignored the original command reader")
	}
	commandReturned()
	if err := r.Drain(context.Background()); err != nil || !r.Joined() {
		t.Fatal("actual return did not retire original owners", err)
	}
}
