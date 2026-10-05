// Package contract defines safe, nullable Model Usage data. It does not query
// storage, authorize a reader, estimate tokens, or finalize an invocation.
package contract

import (
	"fmt"
	"log/slog"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

type Dispatch string

const (
	Reserved        Dispatch = "reserved"
	Authorized      Dispatch = "authorized"
	Sent            Dispatch = "sent"
	NotSent         Dispatch = "not_sent"
	DispatchUnknown Dispatch = "unknown"
)

func (d Dispatch) Valid() bool {
	return d == Reserved || d == Authorized || d == Sent || d == NotSent || d == DispatchUnknown
}

type TerminalStatus string

const (
	Succeeded TerminalStatus = "succeeded"
	Failed    TerminalStatus = "failed"
	Cancelled TerminalStatus = "cancelled"
	Unknown   TerminalStatus = "unknown"
)

func (s TerminalStatus) Valid() bool {
	return s == Succeeded || s == Failed || s == Cancelled || s == Unknown
}

type Final struct {
	Status     TerminalStatus `json:"status"`
	Version    f.Version      `json:"version"`
	FinishedAt f.Instant      `json:"finished_at"`
	Error      *mc.ModelError `json:"error,omitempty"`
}

func (r Final) Validate() error {
	if !r.Status.Valid() || r.Version.Validate() != nil || r.FinishedAt.Validate() != nil || r.Error != nil && r.Error.Validate() != nil || r.Status == Succeeded && r.Error != nil {
		return bad()
	}
	return nil
}
func (r Final) Clone() Final { r.Error = copyPtr(r.Error); return r }

type Invocation struct {
	ID                mc.InvocationID  `json:"id"`
	CallID            mc.CallID        `json:"call_id"`
	AttemptIndex      f.Sequence       `json:"attempt_index"`
	Consumer          mc.Consumer      `json:"consumer"`
	SnapshotID        mc.SnapshotID    `json:"snapshot_id"`
	Identity          mc.ModelIdentity `json:"identity"`
	LiveProviderID    *mc.ProviderID   `json:"live_provider_id"`
	LiveModelID       *mc.ModelID      `json:"live_model_id"`
	ProcessID         oc.ProcessID     `json:"process_id"`
	Fence             f.Sequence       `json:"fence"`
	Dispatch          Dispatch         `json:"dispatch"`
	StartedAt         f.Instant        `json:"started_at"`
	DispatchedAt      *f.Instant       `json:"dispatched_at"`
	Final             *Final           `json:"final"`
	Usage             mc.Usage         `json:"usage"`
	ProviderRequestID string           `json:"provider_request_id,omitempty"`
}

func (r Invocation) Validate() error {
	if r.ID.Validate() != nil || r.CallID.Validate() != nil || r.AttemptIndex.Validate() != nil || r.Consumer.Validate() != nil || r.SnapshotID.Validate() != nil || r.Identity.Validate() != nil || r.Identity.ModelType != r.Consumer.Purpose.ModelType() || r.ProcessID.Validate() != nil || r.Fence.Validate() != nil || !r.Dispatch.Valid() || r.StartedAt.Validate() != nil || r.Usage.Validate() != nil {
		return bad()
	}
	if r.LiveProviderID != nil && *r.LiveProviderID != r.Identity.ProviderID || r.LiveModelID != nil && *r.LiveModelID != r.Identity.ModelID {
		return bad()
	}
	if (r.Dispatch == Sent) != (r.DispatchedAt != nil) {
		return bad()
	}
	if r.DispatchedAt != nil && (r.DispatchedAt.Validate() != nil || r.DispatchedAt.Time().Before(r.StartedAt.Time())) {
		return bad()
	}
	if r.Dispatch != Sent && r.Usage.Source != mc.UnknownUsage {
		return bad()
	}
	if r.Final != nil {
		if r.Final.Validate() != nil || r.Final.FinishedAt.Time().Before(r.StartedAt.Time()) || r.DispatchedAt != nil && r.Final.FinishedAt.Time().Before(r.DispatchedAt.Time()) || r.Final.Status == Succeeded && r.Dispatch != Sent || r.Dispatch == NotSent && r.Final.Error != nil && r.Final.Error.Dispatched {
			return bad()
		}
	}
	if (mc.ModelError{Category: "unknown", ProviderRequestID: r.ProviderRequestID}).Validate() != nil {
		return bad()
	}
	return nil
}
func (r Invocation) Clone() Invocation {
	r.Consumer = r.Consumer.Clone()
	r.LiveProviderID = copyPtr(r.LiveProviderID)
	r.LiveModelID = copyPtr(r.LiveModelID)
	r.DispatchedAt = copyPtr(r.DispatchedAt)
	if r.Final != nil {
		x := r.Final.Clone()
		r.Final = &x
	}
	r.Usage = r.Usage.Clone()
	return r
}

type FieldSummary struct {
	Sum          *mc.TokenCount `json:"sum"`
	KnownCount   mc.TokenCount  `json:"known_count"`
	UnknownCount mc.TokenCount  `json:"unknown_count"`
}

func (s FieldSummary) Validate() error {
	n, e := add(s.KnownCount, s.UnknownCount)
	if e != nil {
		return e
	}
	if s.KnownCount == 0 && n > 0 {
		if s.Sum != nil {
			return bad()
		}
	} else if s.Sum == nil || s.Sum.Validate() != nil || s.KnownCount == 0 && *s.Sum != 0 {
		return bad()
	}
	return nil
}
func (s FieldSummary) Clone() FieldSummary { s.Sum = copyPtr(s.Sum); return s }

type Summary struct {
	ConfirmedInvocations mc.TokenCount `json:"confirmed_invocations"`
	DispatchUnknown      mc.TokenCount `json:"dispatch_unknown"`
	Succeeded            mc.TokenCount `json:"succeeded"`
	Failed               mc.TokenCount `json:"failed"`
	Cancelled            mc.TokenCount `json:"cancelled"`
	Unknown              mc.TokenCount `json:"unknown"`
	Input                FieldSummary  `json:"input"`
	Output               FieldSummary  `json:"output"`
	Total                FieldSummary  `json:"total"`
	CachedInput          FieldSummary  `json:"cached_input"`
	CacheWrite           FieldSummary  `json:"cache_write"`
	Reasoning            FieldSummary  `json:"reasoning"`
	AsOf                 f.Instant     `json:"as_of"`
}

func (s Summary) Validate() error {
	total, e := add(s.ConfirmedInvocations, s.DispatchUnknown)
	if e != nil || s.AsOf.Validate() != nil {
		return bad()
	}
	statuses := mc.TokenCount(0)
	for _, v := range []mc.TokenCount{s.Succeeded, s.Failed, s.Cancelled, s.Unknown} {
		statuses, e = add(statuses, v)
		if e != nil {
			return e
		}
	}
	if statuses > total {
		return bad()
	}
	for _, v := range []FieldSummary{s.Input, s.Output, s.Total, s.CachedInput, s.CacheWrite, s.Reasoning} {
		n, e := add(v.KnownCount, v.UnknownCount)
		if e != nil || v.Validate() != nil || n != s.ConfirmedInvocations {
			return bad()
		}
	}
	return nil
}
func (s Summary) Clone() Summary {
	s.Input = s.Input.Clone()
	s.Output = s.Output.Clone()
	s.Total = s.Total.Clone()
	s.CachedInput = s.CachedInput.Clone()
	s.CacheWrite = s.CacheWrite.Clone()
	s.Reasoning = s.Reasoning.Clone()
	return s
}
func add(a, b mc.TokenCount) (mc.TokenCount, error) {
	if a.Validate() != nil || b.Validate() != nil || int64(a) > math.MaxInt64-int64(b) {
		return 0, bad()
	}
	return a + b, nil
}
func bad() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func copyPtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func (Final) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_final")) }
func (Final) LogValue() slog.Value       { return slog.StringValue("usage_final") }

func (Invocation) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_invocation")) }
func (Invocation) LogValue() slog.Value       { return slog.StringValue("usage_invocation") }

func (FieldSummary) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_field_summary")) }
func (FieldSummary) LogValue() slog.Value       { return slog.StringValue("usage_field_summary") }

func (Summary) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("usage_summary")) }
func (Summary) LogValue() slog.Value       { return slog.StringValue("usage_summary") }
