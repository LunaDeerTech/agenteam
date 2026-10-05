package contract

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func fresh[T any](t *testing.T) f.ID[T] {
	t.Helper()
	v, e := f.NewID[T]()
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func ptr[T any](v T) *T { return &v }
func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func reject(t *testing.T, e error) {
	t.Helper()
	if e == nil {
		t.Fatal("invalid input accepted")
	}
}
func instant(t *testing.T) f.Instant {
	t.Helper()
	v, e := f.NewInstant(time.Date(2026, 10, 5, 1, 2, 3, 0, time.UTC))
	must(t, e)
	return v
}
func invocation(t *testing.T) Invocation {
	t.Helper()
	p, a, e := fresh[id.Project](t), fresh[id.Agent](t), fresh[id.Execution](t)
	at := instant(t)
	return Invocation{ID: fresh[mc.Invocation](t), CallID: fresh[mc.Call](t), AttemptIndex: 1, Consumer: mc.Consumer{Kind: mc.AgentConsumer, ProjectID: p, Purpose: mc.AgentGeneration, AgentID: &a, ExecutionID: &e}, SnapshotID: fresh[mc.Snapshot](t), Identity: mc.ModelIdentity{ProviderID: fresh[mc.Provider](t), ModelID: fresh[mc.Model](t), ProviderName: "deleted-provider", ModelName: "deleted-model", ProviderModelID: "native-name", Protocol: mc.OpenAIChat, Profile: mc.OpenAIChatV1, ModelType: mc.ChatModel, AdapterRevision: "native-v1"}, ProcessID: fresh[oc.Process](t), Fence: 1, Dispatch: Sent, StartedAt: at, DispatchedAt: &at, Final: &Final{Status: Succeeded, Version: 1, FinishedAt: at}, Usage: mc.Usage{Source: mc.UnknownUsage}}
}
func emptySummary(t *testing.T) Summary {
	t.Helper()
	z := FieldSummary{Sum: ptr(mc.TokenCount(0))}
	return Summary{AsOf: instant(t), Input: z.Clone(), Output: z.Clone(), Total: z.Clone(), CachedInput: z.Clone(), CacheWrite: z.Clone(), Reasoning: z.Clone()}
}
func TestInvocationHistoricalIdentitySurvivesDeletedLiveRecords(t *testing.T) {
	r := invocation(t)
	must(t, r.Validate())
	wire, e := json.Marshal(r)
	must(t, e)
	for _, part := range []string{`"live_provider_id":null`, `"live_model_id":null`, `"provider_name":"deleted-provider"`, `"input_tokens":null`} {
		if !strings.Contains(string(wire), part) {
			t.Fatal(string(wire))
		}
	}
	for _, forbidden := range []string{"endpoint", "credential_ref", "header_overwrite", "parameters", "api_key"} {
		if strings.Contains(string(wire), forbidden) {
			t.Fatal("unsafe usage field", forbidden)
		}
	}
	c := r.Clone()
	c.LiveModelID = ptr(fresh[mc.Model](t))
	reject(t, c.Validate())
	c = r.Clone()
	c.Dispatch = DispatchUnknown
	reject(t, c.Validate())
	c.DispatchedAt = nil
	c.Final.Status = Unknown
	must(t, c.Validate())
	c.Usage = mc.Usage{Source: mc.ProviderUsage, InputTokens: ptr(mc.TokenCount(0))}
	reject(t, c.Validate())
	c = r.Clone()
	c.Dispatch = NotSent
	c.DispatchedAt = nil
	c.Final.Status = Failed
	c.Final.Error = &mc.ModelError{Category: "timeout", Dispatched: true}
	reject(t, c.Validate())
	c = r.Clone()
	*c.Consumer.AgentID = fresh[id.Agent](t)
	c.Final.Status = Failed
	if r.Final.Status != Succeeded || *r.Consumer.AgentID == *c.Consumer.AgentID {
		t.Fatal("invocation alias")
	}
}
func TestUsageSummaryUnknownZeroAndOverflow(t *testing.T) {
	must(t, (FieldSummary{Sum: ptr(mc.TokenCount(0))}).Validate())
	must(t, (FieldSummary{UnknownCount: 1}).Validate())
	reject(t, (FieldSummary{Sum: ptr(mc.TokenCount(0)), UnknownCount: 1}).Validate())
	reject(t, (FieldSummary{KnownCount: 1}).Validate())
	must(t, (FieldSummary{KnownCount: 1, Sum: ptr(mc.TokenCount(0))}).Validate())
	reject(t, (FieldSummary{KnownCount: mc.TokenCount(math.MaxInt64), UnknownCount: 1, Sum: ptr(mc.TokenCount(0))}).Validate())
	s := emptySummary(t)
	must(t, s.Validate())
	s.ConfirmedInvocations = 2
	s.DispatchUnknown = 1
	s.Succeeded = 1
	s.Failed = 1
	s.Unknown = 1
	for _, field := range []*FieldSummary{&s.Input, &s.Output, &s.Total, &s.CachedInput, &s.CacheWrite, &s.Reasoning} {
		*field = FieldSummary{KnownCount: 1, UnknownCount: 1, Sum: ptr(mc.TokenCount(9007199254740993))}
	}
	must(t, s.Validate())
	wire, e := json.Marshal(s)
	must(t, e)
	if !strings.Contains(string(wire), `"9007199254740993"`) {
		t.Fatal(string(wire))
	}
	c := s.Clone()
	*c.Input.Sum = 0
	if *s.Input.Sum == 0 {
		t.Fatal("summary alias")
	}
	c = s.Clone()
	c.Output.UnknownCount++
	reject(t, c.Validate())
	c = s.Clone()
	c.Succeeded++
	reject(t, c.Validate())
	c = s.Clone()
	c.ConfirmedInvocations = mc.TokenCount(math.MaxInt64)
	reject(t, c.Validate())
}
