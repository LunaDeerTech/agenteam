package scheduler

import (
	"strconv"
	"strings"
	"time"
)

const retryPolicyByteLimit = 512

// An exactly zero value means a historical/unbound policy. Other invalid
// values are errors, never alternate spellings of that absence.
func encodeRetryPolicy(p LaunchRetryPolicy) ([]byte, *string, error) {
	if p == (LaunchRetryPolicy{}) {
		return nil, nil, nil
	}
	digest, err := p.Identity()
	if err != nil {
		return nil, nil, err
	}
	value := digest.String()
	return []byte(p.canonical()), &value, nil
}

func decodeRetryPolicy(raw []byte, digest *string) (LaunchRetryPolicy, error) {
	if raw == nil && digest == nil {
		return LaunchRetryPolicy{}, nil
	}
	if len(raw) == 0 || len(raw) > retryPolicyByteLimit || digest == nil {
		return LaunchRetryPolicy{}, unavailable(nil)
	}
	lines := strings.Split(string(raw), "\n")
	if len(lines) != 6 || lines[0] != "agenteam.scheduler.launch_retry" ||
		lines[1] != "algorithm="+LaunchRetryPolicyAlgorithm || lines[5] != "" {
		return LaunchRetryPolicy{}, unavailable(nil)
	}
	var values [3]int64
	for n, prefix := range []string{"max_attempts=", "initial_backoff_ns=", "max_backoff_ns="} {
		suffix, found := strings.CutPrefix(lines[n+2], prefix)
		if !found {
			return LaunchRetryPolicy{}, unavailable(nil)
		}
		value, err := strconv.ParseInt(suffix, 10, 64)
		if err != nil {
			return LaunchRetryPolicy{}, unavailable(nil)
		}
		values[n] = value
	}
	p, err := NewLaunchRetryPolicy(values[0], time.Duration(values[1]), time.Duration(values[2]))
	if err != nil || p.canonical() != string(raw) {
		return LaunchRetryPolicy{}, unavailable(nil)
	}
	identity, err := p.Identity()
	if err != nil || identity.String() != *digest {
		return LaunchRetryPolicy{}, unavailable(nil)
	}
	return p, nil
}

// RetryPolicy reports the immutable Claim-time binding, if any. A missing
// policy stays missing even when a later Coordinator has configured retries.
// The projection is neither a temporary-failure fact nor a retry permission.
func (d Dispatch) RetryPolicy() (LaunchRetryPolicy, bool) {
	if d.data == nil {
		return LaunchRetryPolicy{}, false
	}
	p := d.data().retryPolicy
	return p, p != (LaunchRetryPolicy{})
}
