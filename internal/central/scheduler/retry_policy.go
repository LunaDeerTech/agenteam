package scheduler

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// LaunchRetryPolicyAlgorithm identifies the exact delay calculation and the
// parameter encoding used by Identity. Changing either requires a new version.
const LaunchRetryPolicyAlgorithm = "capped_exponential_v1"

// LaunchRetryPolicy is an immutable value constructed from explicit settings.
// Its zero value is invalid. It contains no default, clock, error classifier or
// permission to retry. In particular it cannot establish a Dispatch outcome or
// a durable binding between these settings and an existing Dispatch.
type LaunchRetryPolicy struct {
	maxAttempts    int64
	initialBackoff time.Duration
	maxBackoff     time.Duration
}

func NewLaunchRetryPolicy(maxAttempts int64, initialBackoff, maxBackoff time.Duration) (LaunchRetryPolicy, error) {
	p := LaunchRetryPolicy{maxAttempts: maxAttempts, initialBackoff: initialBackoff, maxBackoff: maxBackoff}
	if err := p.Validate(); err != nil {
		return LaunchRetryPolicy{}, err
	}
	return p, nil
}

func (p LaunchRetryPolicy) Validate() error {
	if p.maxAttempts < 1 || p.initialBackoff <= 0 || p.maxBackoff < p.initialBackoff {
		return invalid()
	}
	return nil
}

func (p LaunchRetryPolicy) MaxAttempts() int64            { return p.maxAttempts }
func (p LaunchRetryPolicy) InitialBackoff() time.Duration { return p.initialBackoff }
func (p LaunchRetryPolicy) MaxBackoff() time.Duration     { return p.maxBackoff }

// Identity is SHA-256 of the fixed domain, algorithm and decimal parameters,
// in the order below, with LF separators and a final LF. Durations are exact
// integer nanoseconds, not rounded wall-clock instants or display strings.
// Equivalent settings have the same identity across process restarts. This
// fingerprint alone does not persist a policy or authorize any Launch.
func (p LaunchRetryPolicy) Identity() (f.Digest, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	raw := "agenteam.scheduler.launch_retry\nalgorithm=" + LaunchRetryPolicyAlgorithm +
		"\nmax_attempts=" + strconv.FormatInt(p.maxAttempts, 10) +
		"\ninitial_backoff_ns=" + strconv.FormatInt(int64(p.initialBackoff), 10) +
		"\nmax_backoff_ns=" + strconv.FormatInt(int64(p.maxBackoff), 10) + "\n"
	sum := sha256.Sum256([]byte(raw))
	return f.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}

// NextDelay applies only after the caller has independently established a
// temporary Launch failure. attemptCount includes the initial Launch, so its
// first failure has count 1 and uses InitialBackoff. A positive count at or
// above MaxAttempts returns (0, false, nil); exhaustion is not proof of failure
// and does not authorize changing a Task or Dispatch to a terminal state.
//
// The calculation is min(initial * 2^(attemptCount-1), max), with no jitter.
// The caller owns any wall-clock addition, instant validation and persistence;
// this function neither sleeps nor schedules another attempt.
func (p LaunchRetryPolicy) NextDelay(attemptCount int64) (time.Duration, bool, error) {
	if err := p.Validate(); err != nil {
		return 0, false, err
	}
	if attemptCount < 1 {
		return 0, false, invalid()
	}
	if attemptCount >= p.maxAttempts {
		return 0, false, nil
	}
	delay := p.initialBackoff
	for n := int64(1); n < attemptCount && delay < p.maxBackoff; n++ {
		// Compare before multiplying: even a cap of MaxInt64 nanoseconds
		// cannot overflow. Positive durations reach the cap in at most 63
		// iterations, independently of the supplied attempt count.
		if delay >= p.maxBackoff-delay {
			delay = p.maxBackoff
			break
		}
		delay *= 2
	}
	return delay, true, nil
}
