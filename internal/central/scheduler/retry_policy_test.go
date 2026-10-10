package scheduler_test

import (
	"errors"
	"math"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
)

func TestSchedulerLaunchRetryPolicyRequiresExplicitValidParameters(t *testing.T) {
	for _, input := range []struct {
		attempts int64
		initial  time.Duration
		maximum  time.Duration
	}{
		{0, time.Second, time.Second},
		{-1, time.Second, time.Second},
		{2, 0, time.Second},
		{2, -time.Second, time.Second},
		{2, time.Second, 0},
		{2, time.Second, -time.Second},
		{2, 2 * time.Second, time.Second},
	} {
		p, err := scheduler.NewLaunchRetryPolicy(input.attempts, input.initial, input.maximum)
		var issue *f.Fault
		if !errors.As(err, &issue) || issue.Code != f.InvalidArgument || issue.CommitState != f.NotStarted || p != (scheduler.LaunchRetryPolicy{}) {
			t.Fatalf("invalid settings accepted or returned partial policy: %+v", input)
		}
	}
	var zero scheduler.LaunchRetryPolicy
	if zero.Validate() == nil {
		t.Fatal("zero policy supplied defaults")
	}
	if identity, err := zero.Identity(); err == nil || identity != "" {
		t.Fatal("zero policy acquired an identity")
	}
	if delay, retry, err := zero.NextDelay(1); err == nil || retry || delay != 0 {
		t.Fatal("zero policy enabled a retry")
	}
	for _, input := range []struct {
		attempts int64
		backoff  time.Duration
	}{{1, time.Nanosecond}, {math.MaxInt64, time.Duration(math.MaxInt64)}} {
		p, err := scheduler.NewLaunchRetryPolicy(input.attempts, input.backoff, input.backoff)
		if err != nil || p.Validate() != nil || p.MaxAttempts() != input.attempts || p.InitialBackoff() != input.backoff || p.MaxBackoff() != input.backoff {
			t.Fatal("valid scalar boundary rejected or changed")
		}
	}
}

func TestSchedulerLaunchRetryPolicyCountsInitialAttemptAndCapsSafely(t *testing.T) {
	p, err := scheduler.NewLaunchRetryPolicy(6, 2*time.Second, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for n, want := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 10 * time.Second, 10 * time.Second} {
		if delay, retry, err := p.NextDelay(int64(n + 1)); err != nil || !retry || delay != want {
			t.Fatalf("attempt %d: delay=%v retry=%v err=%v", n+1, delay, retry, err)
		}
	}
	for _, count := range []int64{6, 7, math.MaxInt64} {
		if delay, retry, err := p.NextDelay(count); err != nil || retry || delay != 0 {
			t.Fatalf("exhausted count %d enabled retry", count)
		}
	}
	for _, count := range []int64{0, -1, math.MinInt64} {
		if delay, retry, err := p.NextDelay(count); err == nil || retry || delay != 0 {
			t.Fatalf("invalid count %d enabled retry", count)
		}
	}
	once, err := scheduler.NewLaunchRetryPolicy(1, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if delay, retry, err := once.NextDelay(1); err != nil || retry || delay != 0 {
		t.Fatal("maxAttempts=1 did not include the initial Launch")
	}
	for _, input := range []struct {
		initial, maximum time.Duration
		count            int64
		want             time.Duration
	}{
		{3, 7, 2, 6},
		{3, 7, 3, 7},
		{5, 5, 1, 5},
		{time.Duration(math.MaxInt64/2 + 1), time.Duration(math.MaxInt64), 2, time.Duration(math.MaxInt64)},
		{1, time.Duration(math.MaxInt64), math.MaxInt64 - 1, time.Duration(math.MaxInt64)},
	} {
		limit, err := scheduler.NewLaunchRetryPolicy(math.MaxInt64, input.initial, input.maximum)
		if err != nil {
			t.Fatal(err)
		}
		if delay, retry, err := limit.NextDelay(input.count); err != nil || !retry || delay != input.want {
			t.Fatalf("cap/overflow boundary: got=%v retry=%v err=%v want=%v", delay, retry, err, input.want)
		}
	}
}

func TestSchedulerLaunchRetryPolicyIdentityIsStableAndImmutable(t *testing.T) {
	attempts, initial, maximum := int64(6), 2*time.Second, 10*time.Second
	p, err := scheduler.NewLaunchRetryPolicy(attempts, initial, maximum)
	if err != nil {
		t.Fatal(err)
	}
	copy := p
	attempts, initial, maximum = 9, time.Nanosecond, time.Minute
	if copy.MaxAttempts() != 6 || copy.InitialBackoff() != 2*time.Second || copy.MaxBackoff() != 10*time.Second {
		t.Fatal("caller settings changed a constructed policy")
	}
	if scheduler.LaunchRetryPolicyAlgorithm != "capped_exponential_v1" {
		t.Fatal("policy algorithm changed without updating the compatibility vector")
	}
	// Fixed SHA-256 vector of the documented canonical bytes, calculated
	// independently of the Go implementation, not by calling its encoder.
	const want = f.Digest("sha256:15c902076008d31a88b82ff8c437160aec11d9e97389c9a209f8b20d3b8ad07f")
	identity, err := p.Identity()
	if err != nil || identity != want || identity.Validate() != nil {
		t.Fatal("canonical policy identity changed")
	}
	equivalent, err := scheduler.NewLaunchRetryPolicy(6, 2000*time.Millisecond, 10000*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := equivalent.Identity(); err != nil || got != identity {
		t.Fatal("equivalent units changed policy identity")
	}
	for _, values := range []struct {
		attempts int64
		initial  time.Duration
		maximum  time.Duration
	}{{7, 2 * time.Second, 10 * time.Second}, {6, 2*time.Second + 1, 10 * time.Second}, {6, 2 * time.Second, 10*time.Second + 1}, {attempts, initial, maximum}} {
		changed, err := scheduler.NewLaunchRetryPolicy(values.attempts, values.initial, values.maximum)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := changed.Identity(); err != nil || got == identity {
			t.Fatal("distinct explicit settings shared an identity")
		}
	}
	_, _, _ = p.NextDelay(3)
	if got, err := copy.Identity(); err != nil || got != want {
		t.Fatal("delay calculation mutated copied policy identity")
	}
}
