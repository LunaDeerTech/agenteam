package contract

import (
	"math"
	"testing"
	"time"
)

func TestAgentRetryTimingBoundsAndOwnedFields(t *testing.T) {
	fields := AgentRetryTimingFields{InitialRequestTimeout: time.Second, MaxRequestTimeout: time.Minute, TimeoutMultiplier: 2, InitialBackoff: time.Millisecond, MaxBackoff: time.Second}
	timing, err := NewAgentRetryTiming(fields)
	if err != nil || timing.Validate() != nil || timing.Fields() != fields || timing.Clone().Fields() != fields {
		t.Fatal("valid per-request timing rejected", err)
	}
	copy := timing.Fields()
	copy.InitialRequestTimeout = 0
	if timing.Fields() != fields {
		t.Fatal("caller changed timing through Fields")
	}
	if (AgentRetryTiming{}).Validate() == nil {
		t.Fatal("missing explicit timing accepted")
	}
	for _, change := range []func(*AgentRetryTimingFields){
		func(v *AgentRetryTimingFields) { v.InitialRequestTimeout = 0 },
		func(v *AgentRetryTimingFields) { v.InitialRequestTimeout = -1 },
		func(v *AgentRetryTimingFields) { v.MaxRequestTimeout = v.InitialRequestTimeout - 1 },
		func(v *AgentRetryTimingFields) { v.TimeoutMultiplier = 0 },
		func(v *AgentRetryTimingFields) { v.TimeoutMultiplier = 1 },
		func(v *AgentRetryTimingFields) { v.InitialBackoff = 0 },
		func(v *AgentRetryTimingFields) { v.InitialBackoff = -1 },
		func(v *AgentRetryTimingFields) { v.MaxBackoff = v.InitialBackoff - 1 },
	} {
		changed := fields
		change(&changed)
		out, err := NewAgentRetryTiming(changed)
		if err == nil || out.Validate() == nil {
			t.Fatal("invalid timing produced a usable value")
		}
	}
	// Equality is a valid already-capped request/backoff. Large valid values
	// remain explicit configuration; overflow protection belongs to growth.
	for _, bounded := range []AgentRetryTimingFields{
		{time.Nanosecond, time.Nanosecond, 2, time.Nanosecond, time.Nanosecond},
		{time.Duration(math.MaxInt64), time.Duration(math.MaxInt64), math.MaxUint32, time.Duration(math.MaxInt64), time.Duration(math.MaxInt64)},
	} {
		value, err := NewAgentRetryTiming(bounded)
		if err != nil || value.Fields() != bounded {
			t.Fatal("valid boundary timing changed", err)
		}
	}
}
