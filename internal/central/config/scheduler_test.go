package config

import (
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
)

func retryConfigValues(attempts, initial, maximum string) map[string]string {
	return map[string]string{
		Prefix + schedulerRetryMaxAttempts:    attempts,
		Prefix + schedulerRetryInitialBackoff: initial,
		Prefix + schedulerRetryMaxBackoff:     maximum,
	}
}

func TestSchedulerRetryConfigurationAbsentOrExplicit(t *testing.T) {
	absent, err := loadValues(nil)
	if err != nil || absent.Validate() != nil {
		t.Fatal("absent Scheduler settings changed existing configuration", err)
	}
	if policy, present := absent.SchedulerLaunchRetryPolicy(); present || policy != (scheduler.LaunchRetryPolicy{}) {
		t.Fatal("absent settings supplied a retry default")
	}
	for _, input := range []struct {
		attempts, initial, maximum string
		count                      int64
		first, cap                 time.Duration
	}{
		{"1", "1ns", "1ns", 1, time.Nanosecond, time.Nanosecond},
		{"6", "2000ms", "10s", 6, 2 * time.Second, 10 * time.Second},
		{"9223372036854775807", "1ns", "9223372036854775807ns", math.MaxInt64, time.Nanosecond, time.Duration(math.MaxInt64)},
	} {
		cfg, err := loadValues(retryConfigValues(input.attempts, input.initial, input.maximum))
		if err != nil || cfg.Validate() != nil {
			t.Fatal("valid explicit policy rejected", err)
		}
		policy, present := cfg.SchedulerLaunchRetryPolicy()
		want, err := scheduler.NewLaunchRetryPolicy(input.count, input.first, input.cap)
		if err != nil || !present || policy != want {
			t.Fatal("configuration changed the domain policy's exact parameters")
		}
		gotIdentity, err := policy.Identity()
		wantIdentity, wantErr := want.Identity()
		if err != nil || wantErr != nil || gotIdentity != wantIdentity {
			t.Fatal("configuration changed the immutable policy identity")
		}
		assertConfigProjectionSafe(t, cfg)
	}
}

func TestSchedulerRetryConfigurationRejectsPartialAndInvalid(t *testing.T) {
	reject := func(values map[string]string, field string) {
		t.Helper()
		cfg, err := loadValues(values)
		var issue *Error
		if !errors.As(err, &issue) || issue.Field() != Prefix+field || issue.Reason() != "invalid" || errors.Unwrap(err) != nil {
			t.Fatal("retry configuration lost its safe field classification", field)
		}
		if _, present := cfg.SchedulerLaunchRetryPolicy(); present || cfg.HTTPAddr() != "" {
			t.Fatal("invalid settings returned a partial configuration")
		}
		assertConfigProjectionSafe(t, err)
	}
	keys := []string{schedulerRetryMaxAttempts, schedulerRetryInitialBackoff, schedulerRetryMaxBackoff}
	// Every nonempty proper subset must fail; absence alone remains compatible.
	for mask := 1; mask < 7; mask++ {
		values := retryConfigValues("2", "100ms", "1s")
		missing := ""
		for index, key := range keys {
			if mask&(1<<index) == 0 {
				delete(values, Prefix+key)
				if missing == "" {
					missing = key
				}
			}
		}
		reject(values, missing)
	}
	for _, key := range keys {
		values := retryConfigValues("2", "100ms", "1s")
		values[Prefix+key] = ""
		reject(values, key)
	}
	for _, raw := range []string{"0", "-1", "+2", "02", "2.0", " 2", "9223372036854775808", "attempts-SENTINEL"} {
		reject(retryConfigValues(raw, "100ms", "1s"), schedulerRetryMaxAttempts)
	}
	for _, raw := range []string{"0s", "-1ns", " 1s", "9223372036854775808ns", "initial-SENTINEL"} {
		reject(retryConfigValues("2", raw, "1s"), schedulerRetryInitialBackoff)
	}
	for _, raw := range []string{"0s", "-1ns", "99ms", "1", "9223372036854775808ns", "maximum-SENTINEL"} {
		reject(retryConfigValues("2", "100ms", raw), schedulerRetryMaxBackoff)
	}
	// Unknown names are rejected by the original whitelist before any lookup.
	reads := 0
	_, err := Load(func(string) (string, bool) { reads++; return "value-SENTINEL", true }, []string{Prefix + "SCHEDULER_LAUNCH_RETRY_FUTURE=value-SENTINEL"})
	var issue *Error
	if !errors.As(err, &issue) || issue.Field() != Prefix+"*" || issue.Reason() != "unsupported" || reads != 0 {
		t.Fatal("retry configuration widened the known-key boundary")
	}
	assertConfigProjectionSafe(t, err)
}

func TestSchedulerRetryConfigurationSnapshotAndValidation(t *testing.T) {
	values := configValues(retryConfigValues("6", "2s", "10s"))
	var env []string
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	reads := make(map[string]int)
	cfg, err := Load(func(key string) (string, bool) {
		if strings.HasPrefix(key, Prefix+"SCHEDULER_LAUNCH_RETRY_") {
			reads[key]++
			if reads[key] != 1 {
				return "changed-SENTINEL", true
			}
		}
		value, present := values[key]
		return value, present
	}, env)
	if err != nil {
		t.Fatal("snapshot load failed", err)
	}
	for _, key := range []string{schedulerRetryMaxAttempts, schedulerRetryInitialBackoff, schedulerRetryMaxBackoff} {
		if reads[Prefix+key] != 1 {
			t.Fatal("a known retry key was not read exactly once")
		}
		values[Prefix+key] = "changed-SENTINEL"
	}
	policy, present := cfg.SchedulerLaunchRetryPolicy()
	if !present {
		t.Fatal("explicit policy disappeared")
	}
	identity, err := policy.Identity()
	if err != nil {
		t.Fatal(err)
	}
	// Replacing a returned value cannot change Config, including during reads.
	policy = scheduler.LaunchRetryPolicy{}
	if policy.Validate() == nil {
		t.Fatal("zero policy unexpectedly acquired defaults")
	}
	var readers sync.WaitGroup
	for range 8 {
		readers.Go(func() {
			got, ok := cfg.SchedulerLaunchRetryPolicy()
			id, e := got.Identity()
			if !ok || e != nil || id != identity || cfg.Validate() != nil || got.MaxAttempts() != 6 {
				t.Error("returned copy or input mutation changed loaded configuration")
			}
		})
	}
	readers.Wait()
	broken := cfg
	broken.launchRetry = &policy
	var issue *Error
	if !errors.As(broken.Validate(), &issue) || issue.Field() != Prefix+"SCHEDULER_LAUNCH_RETRY_*" || cfg.Validate() != nil {
		t.Fatal("Config.Validate ignored a present invalid policy or modified another Config")
	}
}
