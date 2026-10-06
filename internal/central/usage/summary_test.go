package usage

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

func TestUsageCompletedReadRetainsCommitOutcome(t *testing.T) {
	if err := completedRead(context.Background(), f.CommittedResult()); err != nil {
		t.Fatal("live committed read rejected", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, tc := range []struct {
		ctx  context.Context
		want error
	}{{cancelled, context.Canceled}, {expired, context.DeadlineExceeded}} {
		err := completedRead(tc.ctx, f.CommittedResult())
		var fault *f.Fault
		if !errors.Is(err, tc.want) || !errors.As(err, &fault) || fault.Code != f.DependencyUnavailable || fault.CommitState != f.Committed {
			t.Fatal("cancelled committed read lost its actual commit or cause", err)
		}
	}
	rejected := f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotStarted))
	var fault *f.Fault
	if err := completedRead(cancelled, rejected); !errors.As(err, &fault) || fault.Code != f.Forbidden || fault.CommitState != f.NotCommitted {
		t.Fatal("cancellation obscured original rejection", err)
	}
	attempt, _ := f.NewID[f.TransactionAttempt]()
	owner, _ := f.NewID[struct{}]()
	cause, _ := f.NewRecoveryCause("usage-read-test", owner.String(), "")
	var unknown *UnknownError
	if err := completedRead(cancelled, f.UnknownResult(attempt, cause)); !errors.As(err, &unknown) || unknown.AttemptID() != attempt || unknown.Cause().Kind() != cause.Kind() || unknown.Cause().Details().Owner != cause.Details().Owner || unknown.Cause().Details().RecoveryRunID != owner.String() {
		t.Fatal("cancellation obscured Unknown identity", err)
	}
}

func TestUsageSummaryNullableContributionsAndOverflow(t *testing.T) {
	now, _ := f.NewInstant(time.Now())
	zero := mc.TokenCount(0)
	five := mc.TokenCount(5)
	sent := uc.Invocation{Dispatch: uc.Sent, Usage: mc.Usage{Source: mc.ProviderUsage, InputTokens: &zero, OutputTokens: &five}}
	base := emptySummary(now)
	out, e := summaryDelta(base, nil, &sent, now)
	if e != nil || out.ConfirmedInvocations != 1 || out.Input.Sum == nil || *out.Input.Sum != 0 || out.Input.KnownCount != 1 || out.Total.Sum != nil || out.Total.UnknownCount != 1 {
		t.Fatal("zero/unknown distinction", e)
	}
	unknown := uc.Invocation{Dispatch: uc.Sent, Usage: mc.Usage{Source: mc.UnknownUsage}}
	out, e = summaryDelta(out, nil, &unknown, now)
	if e != nil || out.Input.KnownCount != 1 || out.Input.UnknownCount != 1 || out.Total.UnknownCount != 2 {
		t.Fatal("mixed denominators", e)
	}
	final := sent.Clone()
	final.Final = &uc.Final{Status: uc.Failed, Version: 1, FinishedAt: now}
	out, e = summaryDelta(out, &sent, &final, now)
	if e != nil || out.ConfirmedInvocations != 2 || out.Failed != 1 || *out.Output.Sum != 5 {
		t.Fatal("final delta", e)
	}
	notSent := uc.Invocation{Dispatch: uc.NotSent, Final: &uc.Final{Status: uc.Failed}}
	unchanged, e := summaryDelta(out, nil, &notSent, now)
	if e != nil || unchanged.Failed != 1 || unchanged.ConfirmedInvocations != 2 {
		t.Fatal("unsent counted", e)
	}
	dispatchUnknown := uc.Invocation{Dispatch: uc.DispatchUnknown, Final: &uc.Final{Status: uc.Unknown}}
	out, e = summaryDelta(out, nil, &dispatchUnknown, now)
	if e != nil || out.DispatchUnknown != 1 || out.Unknown != 1 || out.ConfirmedInvocations != 2 {
		t.Fatal("dispatch unknown conflated", e)
	}
	max := mc.TokenCount(math.MaxInt64)
	huge := sent.Clone()
	huge.Usage.InputTokens = &max
	s, e := summaryDelta(base, nil, &huge, now)
	if e != nil {
		t.Fatal(e)
	}
	one := mc.TokenCount(1)
	sent.Usage.InputTokens = &one
	_, e = summaryDelta(s, nil, &sent, now)
	expectFault(t, e, f.InvalidState)
	_, e = deltaNumber(max, 0, 1)
	expectFault(t, e, f.InvalidState)
	_, e = deltaNumber(0, 1, 0)
	expectFault(t, e, f.InvalidState)
}

func TestUsageTransitionsPreserveKnownFacts(t *testing.T) {
	now, _ := f.NewInstant(time.Now())
	n := mc.TokenCount(0)
	old := uc.Invocation{Dispatch: uc.Sent, DispatchedAt: &now, Usage: mc.Usage{Source: mc.ProviderUsage, InputTokens: &n}}
	cleared := old.Clone()
	cleared.Usage = mc.Usage{Source: mc.UnknownUsage}
	expectFault(t, transition(old, cleared, uc.ObserveAction), f.InvalidState)
	changed := old.Clone()
	later, _ := f.NewInstant(now.Time().Add(time.Second))
	changed.DispatchedAt = &later
	expectFault(t, transition(old, changed, uc.ObserveAction), f.InvalidState)
	reserved := uc.Invocation{Dispatch: uc.Reserved}
	expectFault(t, transition(reserved, old, uc.ObserveAction), f.InvalidState)
}
