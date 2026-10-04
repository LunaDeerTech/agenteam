package contract

import (
	"bytes"
	"context"
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"io"
	"time"
)

type RequeueReason string

const (
	OperatorRetry      RequeueReason = "operator_retry"
	SchemaAvailable    RequeueReason = "schema_available"
	DependencyRestored RequeueReason = "dependency_restored"
)

func (r RequeueReason) Valid() bool {
	return r == OperatorRetry || r == SchemaAvailable || r == DependencyRestored
}

type RequeueReceipt struct {
	DeliveryID DeliveryID
	Cycle      foundation.Progress
	Version    foundation.Version
}
type DiagnosticsWindow string

const (
	FiveMinutes DiagnosticsWindow = "5m"
	OneHour     DiagnosticsWindow = "1h"
	OneDay      DiagnosticsWindow = "24h"
)

type DiagnosticsFilter struct {
	Window    DiagnosticsWindow
	HandlerID event.StableName
	EventType event.StableName
	Phase     Phase
}
type DiagnosticsPage struct {
	Items      []SafeDelivery     `json:"items"`
	NextCursor string             `json:"next_cursor"`
	Summary    DiagnosticsSummary `json:"summary"`
}

func (w DiagnosticsWindow) Duration() (time.Duration, error) {
	switch w {
	case FiveMinutes:
		return 5 * time.Minute, nil
	case OneHour:
		return time.Hour, nil
	case OneDay:
		return 24 * time.Hour, nil
	}
	return 0, invalid()
}
func (f DiagnosticsFilter) Normalize() (DiagnosticsFilter, error) {
	if f.Window == "" {
		f.Window = FiveMinutes
	}
	if _, err := f.Window.Duration(); err != nil {
		return DiagnosticsFilter{}, err
	}
	if f.HandlerID != "" && f.HandlerID.Validate() != nil || f.EventType != "" && f.EventType.Validate() != nil || f.Phase != "" && !f.Phase.Valid() {
		return DiagnosticsFilter{}, invalid()
	}
	return f, nil
}

type HandlerLatencySummary struct {
	HandlerID event.StableName       `json:"handler_id"`
	Count     foundation.Progress    `json:"count"`
	Unknown   foundation.Progress    `json:"unknown"`
	SumMS     foundation.DurationMS  `json:"sum_ms"`
	MaxMS     *foundation.DurationMS `json:"max_ms"`
}
type EventTypeThroughputSummary struct {
	EventType           event.StableName    `json:"event_type"`
	ProducedEvents      foundation.Progress `json:"produced_events"`
	SucceededDeliveries foundation.Progress `json:"succeeded_deliveries"`
}
type DiagnosticSafeError struct {
	At         foundation.Instant `json:"at"`
	DeliveryID DeliveryID         `json:"delivery_id"`
	AttemptID  *AttemptID         `json:"attempt_id"`
	HandlerID  event.StableName   `json:"handler_id"`
	EventType  event.StableName   `json:"event_type"`
	Reason     SafeReason         `json:"reason"`
}
type DiagnosticsSummary struct {
	Window                   DiagnosticsWindow            `json:"window"`
	AsOf                     foundation.Instant           `json:"as_of"`
	WindowStart              foundation.Instant           `json:"window_start"`
	WindowMS                 foundation.DurationMS        `json:"window_ms"`
	PendingEvents            foundation.Progress          `json:"pending_events"`
	RetryWaitDeliveries      foundation.Progress          `json:"retry_wait_deliveries"`
	FailedDeliveries         foundation.Progress          `json:"failed_deliveries"`
	DeadLetterDeliveries     foundation.Progress          `json:"dead_letter_deliveries"`
	OldestPendingAgeMS       *foundation.DurationMS       `json:"oldest_pending_age_ms"`
	HandlerLatency           []HandlerLatencySummary      `json:"handler_latency"`
	EventThroughput          []EventTypeThroughputSummary `json:"event_throughput"`
	HandlerLatencyTruncated  bool                         `json:"handler_latency_truncated"`
	EventThroughputTruncated bool                         `json:"event_throughput_truncated"`
	RecentErrors             []DiagnosticSafeError        `json:"recent_errors"`
}

func (s DiagnosticsSummary) Validate() error {
	duration, err := s.Window.Duration()
	if err != nil || s.AsOf.Validate() != nil || s.WindowStart.Validate() != nil || s.WindowMS.Validate() != nil || int64(s.WindowMS) != duration.Milliseconds() || !s.WindowStart.Time().Equal(s.AsOf.Time().Add(-duration)) {
		return invalid()
	}
	for _, n := range []foundation.Progress{s.PendingEvents, s.RetryWaitDeliveries, s.FailedDeliveries, s.DeadLetterDeliveries} {
		if n.Validate() != nil {
			return invalid()
		}
	}
	if (s.PendingEvents == 0) != (s.OldestPendingAgeMS == nil) || s.OldestPendingAgeMS != nil && s.OldestPendingAgeMS.Validate() != nil {
		return invalid()
	}
	if s.HandlerLatency == nil || s.EventThroughput == nil || s.RecentErrors == nil || len(s.HandlerLatency) > 128 || len(s.EventThroughput) > 128 || len(s.RecentErrors) > 20 {
		return invalid()
	}
	if s.HandlerLatencyTruncated && len(s.HandlerLatency) != 128 || s.EventThroughputTruncated && len(s.EventThroughput) != 128 {
		return invalid()
	}
	for i, h := range s.HandlerLatency {
		if h.HandlerID.Validate() != nil || i > 0 && s.HandlerLatency[i-1].HandlerID >= h.HandlerID || h.Count.Validate() != nil || h.Unknown.Validate() != nil || h.SumMS.Validate() != nil {
			return invalid()
		}
		if h.Count == 0 {
			if h.SumMS != 0 || h.MaxMS != nil {
				return invalid()
			}
		} else if h.MaxMS == nil || h.MaxMS.Validate() != nil || *h.MaxMS > h.SumMS {
			return invalid()
		}
	}
	for i, e := range s.EventThroughput {
		if e.EventType.Validate() != nil || i > 0 && s.EventThroughput[i-1].EventType >= e.EventType || e.ProducedEvents.Validate() != nil || e.SucceededDeliveries.Validate() != nil {
			return invalid()
		}
	}
	seen := map[DeliveryID]bool{}
	for i, e := range s.RecentErrors {
		if e.At.Validate() != nil || e.At.Time().Before(s.WindowStart.Time()) || !e.At.Time().Before(s.AsOf.Time()) || e.DeliveryID.Validate() != nil || e.AttemptID != nil && e.AttemptID.Validate() != nil || e.HandlerID.Validate() != nil || e.EventType.Validate() != nil || !e.Reason.Valid() || seen[e.DeliveryID] {
			return invalid()
		}
		seen[e.DeliveryID] = true
		if i > 0 {
			previous := s.RecentErrors[i-1]
			if e.At.Time().After(previous.At.Time()) || e.At.Time().Equal(previous.At.Time()) && e.DeliveryID.String() >= previous.DeliveryID.String() {
				return invalid()
			}
		}
	}
	return nil
}
func (s DiagnosticsSummary) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	type wire DiagnosticsSummary
	return json.Marshal(wire(s))
}
func (s *DiagnosticsSummary) UnmarshalJSON(raw []byte) error {
	type wire DiagnosticsSummary
	fields, err := diagnosticFields(raw, []string{"window", "as_of", "window_start", "window_ms", "pending_events", "retry_wait_deliveries", "failed_deliveries", "dead_letter_deliveries", "oldest_pending_age_ms", "handler_latency", "event_throughput", "handler_latency_truncated", "event_throughput_truncated", "recent_errors"})
	if err != nil {
		return err
	}
	for name, keys := range map[string][]string{"handler_latency": {"handler_id", "count", "unknown", "sum_ms", "max_ms"}, "event_throughput": {"event_type", "produced_events", "succeeded_deliveries"}, "recent_errors": {"at", "delivery_id", "attempt_id", "handler_id", "event_type", "reason"}} {
		var entries []json.RawMessage
		if json.Unmarshal(fields[name], &entries) != nil || len(entries) > 128 || name == "recent_errors" && len(entries) > 20 {
			return invalid()
		}
		for _, entry := range entries {
			if _, err = diagnosticFields(entry, keys); err != nil {
				return err
			}
		}
	}
	var v wire
	if json.Unmarshal(raw, &v) != nil {
		return invalid()
	}
	next := DiagnosticsSummary(v)
	if err = next.Validate(); err != nil {
		return err
	}
	*s = next
	return nil
}

// This DTO has its own structural bounds. The event payload's 64 KiB
// limit does not apply to 128+128 summary rows and 20 safe error records.
func diagnosticFields(raw []byte, keys []string) (map[string]json.RawMessage, error) {
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, invalid()
	}
	fields := make(map[string]json.RawMessage, len(keys))
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, invalid()
		}
		key, ok := token.(string)
		if !ok || !allowed[key] {
			return nil, invalid()
		}
		if _, exists := fields[key]; exists {
			return nil, invalid()
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, invalid()
		}
		nullable := key == "oldest_pending_age_ms" || key == "max_ms" || key == "attempt_id"
		if !nullable && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, invalid()
		}
		fields[key] = value
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') || len(fields) != len(keys) {
		return nil, invalid()
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, invalid()
	}
	return fields, nil
}

type DeliveryAdministration interface {
	Requeue(context.Context, identity.Actor, foundation.CommandMeta, DeliveryID, foundation.Version, RequeueReason) (RequeueReceipt, error)
	InspectDelivery(context.Context, identity.Actor, identity.Scope, DeliveryID) (SafeDelivery, error)
	QueryDiagnostics(context.Context, identity.Actor, identity.Scope, DiagnosticsFilter, foundation.PageRequest) (DiagnosticsPage, error)
}
