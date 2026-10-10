package config

import (
	"strconv"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
)

const (
	schedulerRetryMaxAttempts    = "SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS"
	schedulerRetryInitialBackoff = "SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF"
	schedulerRetryMaxBackoff     = "SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF"
)

// SchedulerLaunchRetryPolicy returns a value copy of the explicitly configured
// policy. Absence has no default and does not enable retry. Loading these values
// neither binds a Scheduler service nor changes an existing Dispatch's policy.
func (c Config) SchedulerLaunchRetryPolicy() (scheduler.LaunchRetryPolicy, bool) {
	if c.launchRetry == nil {
		return scheduler.LaunchRetryPolicy{}, false
	}
	return *c.launchRetry, true
}

func loadSchedulerLaunchRetry(lookup LookupEnv) (*scheduler.LaunchRetryPolicy, error) {
	// Read each known key once: absence and an explicitly empty value are
	// different, and a mutable LookupEnv cannot change this load's snapshot.
	attemptsRaw, attemptsPresent := lookup(Prefix + schedulerRetryMaxAttempts)
	initialRaw, initialPresent := lookup(Prefix + schedulerRetryInitialBackoff)
	maximumRaw, maximumPresent := lookup(Prefix + schedulerRetryMaxBackoff)
	if !attemptsPresent && !initialPresent && !maximumPresent {
		return nil, nil
	}
	if !attemptsPresent {
		return nil, invalid(schedulerRetryMaxAttempts)
	}
	if !initialPresent {
		return nil, invalid(schedulerRetryInitialBackoff)
	}
	if !maximumPresent {
		return nil, invalid(schedulerRetryMaxBackoff)
	}
	attempts, err := strconv.ParseInt(attemptsRaw, 10, 64)
	if err != nil || strconv.FormatInt(attempts, 10) != attemptsRaw {
		return nil, invalid(schedulerRetryMaxAttempts)
	}
	initial, err := time.ParseDuration(initialRaw)
	if err != nil {
		return nil, invalid(schedulerRetryInitialBackoff)
	}
	maximum, err := time.ParseDuration(maximumRaw)
	if err != nil {
		return nil, invalid(schedulerRetryMaxBackoff)
	}
	policy, err := scheduler.NewLaunchRetryPolicy(attempts, initial, maximum)
	if err != nil {
		// The domain constructor owns the numeric relationship. Project only
		// its offending field; never retain raw parser input or wrapped errors.
		if attempts < 1 {
			return nil, invalid(schedulerRetryMaxAttempts)
		}
		if initial <= 0 {
			return nil, invalid(schedulerRetryInitialBackoff)
		}
		return nil, invalid(schedulerRetryMaxBackoff)
	}
	return &policy, nil
}
