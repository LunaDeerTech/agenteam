package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func emptySummary(t *testing.T) DiagnosticsSummary {
	t.Helper()
	at, err := foundation.NewInstant(time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	start, _ := foundation.NewInstant(at.Time().Add(-5 * time.Minute))
	return DiagnosticsSummary{Window: FiveMinutes, AsOf: at, WindowStart: start, WindowMS: 300000, HandlerLatency: []HandlerLatencySummary{}, EventThroughput: []EventTypeThroughputSummary{}, RecentErrors: []DiagnosticSafeError{}}
}
func TestDiagnosticsSummaryExactIntegersAndUnknown(t *testing.T) {
	s := emptySummary(t)
	for _, n := range []int64{0, 9007199254740993, math.MaxInt64} {
		s.FailedDeliveries = foundation.Progress(n)
		max := foundation.DurationMS(n)
		s.HandlerLatency = []HandlerLatencySummary{{HandlerID: "fixture", Count: 1, Unknown: foundation.Progress(n), SumMS: max, MaxMS: &max}}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(raw, []byte(`"failed_deliveries":"`+foundation.Progress(n).String()+`"`)) {
			t.Fatal("counter was rounded or became JSON number")
		}
		var got DiagnosticsSummary
		if err = json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.FailedDeliveries != foundation.Progress(n) || got.HandlerLatency[0].Unknown != foundation.Progress(n) || *got.HandlerLatency[0].MaxMS != foundation.DurationMS(n) {
			t.Fatal("large exact value changed")
		}
	}
	s = emptySummary(t)
	s.HandlerLatency = []HandlerLatencySummary{{HandlerID: "unknown", Unknown: 2}}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"max_ms":null`)) || !bytes.Contains(raw, []byte(`"oldest_pending_age_ms":null`)) || !bytes.Contains(raw, []byte(`"event_throughput":[]`)) {
		t.Fatal("unknown/empty projection changed")
	}
	for _, bad := range []DiagnosticsSummary{
		func() DiagnosticsSummary { v := emptySummary(t); v.WindowMS = 0; return v }(),
		func() DiagnosticsSummary { v := emptySummary(t); v.PendingEvents = 1; return v }(),
		func() DiagnosticsSummary { v := emptySummary(t); v.FailedDeliveries = -1; return v }(),
		func() DiagnosticsSummary {
			v := emptySummary(t)
			v.HandlerLatency = []HandlerLatencySummary{{HandlerID: "x", Count: 1}}
			return v
		}(),
		func() DiagnosticsSummary {
			v := emptySummary(t)
			zero := foundation.DurationMS(0)
			v.HandlerLatency = []HandlerLatencySummary{{HandlerID: "x", MaxMS: &zero}}
			return v
		}(),
		func() DiagnosticsSummary { v := emptySummary(t); v.EventThroughputTruncated = true; return v }(),
	} {
		if _, err = json.Marshal(bad); err == nil {
			t.Fatal("invalid diagnostic facts serialized")
		}
	}
}
func TestDiagnosticsSummaryStrictWireAndWindow(t *testing.T) {
	base, err := json.Marshal(emptySummary(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{`-1`, `"-1"`, `"01"`, `"9223372036854775808"`, `9007199254740993`, `null`} {
		raw := bytes.Replace(base, []byte(`"failed_deliveries":"0"`), []byte(`"failed_deliveries":`+value), 1)
		var got DiagnosticsSummary
		if json.Unmarshal(raw, &got) == nil {
			t.Fatalf("unsafe number accepted: %s", value)
		}
	}
	for _, raw := range [][]byte{
		bytes.Replace(base, []byte(`"as_of"`), []byte(`"AS_OF"`), 1),
		bytes.Replace(base, []byte(`"failed_deliveries":"0"`), []byte(`"failed_deliveries":"0","FAILED_DELIVERIES":"9"`), 1),
		bytes.Replace(base, []byte(`"failed_deliveries":"0"`), []byte(`"failed_deliveries":"0","failed_deliveries":"9"`), 1),
		append(append([]byte(nil), base...), []byte(` {}`)...),
	} {
		var got DiagnosticsSummary
		if json.Unmarshal(raw, &got) == nil {
			t.Fatal("malformed/alias diagnostic JSON accepted")
		}
	}
	for _, window := range []DiagnosticsWindow{FiveMinutes, OneHour, OneDay} {
		v := emptySummary(t)
		duration, _ := window.Duration()
		v.Window = window
		v.WindowMS = foundation.DurationMS(duration.Milliseconds())
		v.WindowStart, _ = foundation.NewInstant(v.AsOf.Time().Add(-duration))
		if v.Validate() != nil {
			t.Fatal("valid fixed window rejected")
		}
	}
	f, err := (DiagnosticsFilter{}).Normalize()
	if err != nil || f.Window != FiveMinutes {
		t.Fatal("default window is not canonical")
	}
	for _, window := range []DiagnosticsWindow{"5M", "week", "0"} {
		if _, err = (DiagnosticsFilter{Window: window}).Normalize(); err == nil {
			t.Fatal("unbounded window accepted")
		}
	}
}

func TestDiagnosticsOwnWireBoundsAndNullPresence(t *testing.T) {
	s := emptySummary(t)
	for i := 0; i < 128; i++ {
		name := event.StableName(fmt.Sprintf("h%03d", i) + strings.Repeat("x", 124))
		max := foundation.DurationMS(math.MaxInt64)
		s.HandlerLatency = append(s.HandlerLatency, HandlerLatencySummary{HandlerID: name, Count: math.MaxInt64, Unknown: math.MaxInt64, SumMS: max, MaxMS: &max})
		s.EventThroughput = append(s.EventThroughput, EventTypeThroughputSummary{EventType: name, ProducedEvents: math.MaxInt64, SucceededDeliveries: math.MaxInt64})
	}

	for i := 0; i < 20; i++ {
		at, _ := foundation.NewInstant(s.AsOf.Time().Add(-time.Duration(i+1) * time.Second))
		delivery, e := foundation.NewID[Delivery]()
		if e != nil {
			t.Fatal(e)
		}
		attempt, e := foundation.NewID[Attempt]()
		if e != nil {
			t.Fatal(e)
		}
		s.RecentErrors = append(s.RecentErrors, DiagnosticSafeError{At: at, DeliveryID: delivery, AttemptID: &attempt, HandlerID: s.HandlerLatency[i].HandlerID, EventType: s.EventThroughput[i].EventType, Reason: AuthorizationChanged})
	}
	s.HandlerLatencyTruncated = true
	s.EventThroughputTruncated = true
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) <= 64<<10 {
		t.Fatal("large legal boundary not reached")
	}
	var got DiagnosticsSummary
	if err = json.Unmarshal(raw, &got); err != nil || !reflect.DeepEqual(s, got) {
		t.Fatalf("large summary round trip failed: %v", err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for key := range fields {
		if key == "oldest_pending_age_ms" {
			continue
		}
		t.Run(key, func(t *testing.T) {
			copy := make(map[string]json.RawMessage, len(fields))
			for k, v := range fields {
				copy[k] = v
			}
			copy[key] = json.RawMessage("null")
			bad, _ := json.Marshal(copy)
			before := got
			if json.Unmarshal(bad, &got) == nil {
				t.Fatal("required null accepted")
			}
			if !reflect.DeepEqual(before, got) {
				t.Fatal("failed decode mutated receiver")
			}
		})
	}
	for _, entry := range []string{
		`{"handler_id":"x","count":"0","unknown":"1","sum_ms":"0","max_ms":null,"count":"4"}`,
		`{"handler_id":"x","count":"0","unknown":"1","sum_ms":"0","max_ms":null,"COUNT":"4"}`,
		`{"handler_id":"x","count":"0","unknown":null,"sum_ms":"0","max_ms":null}`,
	} {
		base, _ := json.Marshal(emptySummary(t))
		bad := bytes.Replace(base, []byte(`"handler_latency":[]`), []byte(`"handler_latency":[`+entry+`]`), 1)
		if json.Unmarshal(bad, &got) == nil {
			t.Fatal("nested duplicate/alias/null accepted")
		}
	}
}
