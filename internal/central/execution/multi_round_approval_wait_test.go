package execution

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type approvalDecisionControl struct {
	gc.Gate
	resolve func(context.Context, tc.ToolCallBinding, string) (gc.Waiting, error)
}

func (g *approvalDecisionControl) Resolve(ctx context.Context, b tc.ToolCallBinding, ref string) (gc.Waiting, error) {
	return g.resolve(ctx, b, ref)
}

func approvalDecisionCandidate(t *testing.T) gc.Waiting {
	t.Helper()
	digest, err := f.ParseDigest("sha256:" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	return gc.Waiting{RequestID: newTestID[gc.RequestIdentity](t).String(), Status: gc.Pending, OperationID: newTestID[tc.Operation](t), Fingerprint: digest}
}

func TestMultiRoundApprovalWaitKeepsOriginalCallback(t *testing.T) {
	for _, decision := range []string{gc.Approved, gc.Rejected} {
		t.Run(decision, func(t *testing.T) {
			waiting := approvalDecisionCandidate(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			calls := 0
			gate := &approvalDecisionControl{resolve: func(ctx context.Context, _ tc.ToolCallBinding, ref string) (gc.Waiting, error) {
				if ref != waiting.RequestID {
					t.Error("wait changed original approval reference")
				}
				calls++
				if calls == 1 {
					return waiting, nil
				}
				close(entered)
				select {
				case <-release:
					resolved := waiting
					resolved.Status = decision
					return resolved, nil
				case <-ctx.Done():
					return gc.Waiting{}, ctx.Err()
				}
			}}
			state := &directTextState{multi: &multiRoundState{approvals: gate}}
			type result struct {
				waiting gc.Waiting
				err     error
			}
			done := make(chan result, 1)
			go func() {
				w, err := state.awaitApprovalDecision(ctx, &directTextCall{ctx: ctx}, tc.ToolCallBinding{}, waiting.RequestID)
				done <- result{w, err}
			}()
			select {
			case <-entered:
			case r := <-done:
				t.Fatal("pending decision ended original callback", r.err)
			case <-ctx.Done():
				t.Fatal("original approval callback did not reach next observation")
			}
			close(release)
			r := <-done
			if r.err != nil || r.waiting.Status != decision || r.waiting.OperationID != waiting.OperationID || calls != 2 {
				t.Fatal("decision did not resume the same operation", r.err)
			}
		})
	}
}

func TestMultiRoundApprovalWaitPreservesFailureAndCancellation(t *testing.T) {
	for _, mode := range []string{"lookup failure", "reference mismatch", "run cancellation", "caller cancellation"} {
		t.Run(mode, func(t *testing.T) {
			waiting := approvalDecisionCandidate(t)
			runCtx, cancelRun := context.WithCancel(context.Background())
			defer cancelRun()
			waitCtx, cancelWait := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelWait()
			original := errors.New("original approval read failure")
			calls := 0
			gate := &approvalDecisionControl{resolve: func(context.Context, tc.ToolCallBinding, string) (gc.Waiting, error) {
				calls++
				switch mode {
				case "lookup failure":
					return gc.Waiting{}, original
				case "reference mismatch":
					waiting.RequestID = newTestID[gc.RequestIdentity](t).String()
				case "run cancellation":
					cancelRun()
				case "caller cancellation":
					cancelWait()
				}
				return waiting, nil
			}}
			state := &directTextState{multi: &multiRoundState{approvals: gate}}
			_, err := state.awaitApprovalDecision(waitCtx, &directTextCall{ctx: runCtx}, tc.ToolCallBinding{}, waiting.RequestID)
			if calls != 1 || err == nil {
				t.Fatal("failure was hidden or retried", err, calls)
			}
			switch mode {
			case "lookup failure":
				if !errors.Is(err, original) {
					t.Fatal("lost original error", err)
				}
			case "reference mismatch":
				requireCode(t, err, f.Forbidden)
			default:
				if !errors.Is(err, context.Canceled) {
					t.Fatal("lost cancellation", err)
				}
			}
			if mode == "caller cancellation" && runCtx.Err() != nil {
				t.Fatal("temporary recovery caller cancelled the original run")
			}
		})
	}
}
