package usagehttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

func wireID[K any]() f.ID[K] {
	value, err := f.ParseID[K]("01900000-0000-7000-8000-000000000001")
	if err != nil {
		panic(err)
	}
	return value
}
func wirePtr[T any](value T) *T { return &value }
func wireInstant() f.Instant {
	value, err := f.ParseInstant("2026-10-07T01:02:03.123456Z")
	if err != nil {
		panic(err)
	}
	return value
}
func wireRequest(raw string) *http.Request {
	return &http.Request{Method: "GET", URL: &url.URL{RawQuery: raw}}
}
func wireQuery() uc.Query {
	return uc.Query{Filter: uc.Filter{ProjectID: wireID[id.Project]()}, Limit: 100}
}
func wireActor() id.Actor {
	actor, err := id.NewHuman(wireID[id.User](), wireID[id.Session]())
	if err != nil {
		panic(err)
	}
	return actor
}
func wireProject() pc.ProjectRef {
	return pc.ProjectRef{ID: wireID[id.Project](), OwnerUserID: wireID[id.User](), Name: "Project", NormalizedName: "project", Description: "test", Lifecycle: pc.Active, Version: 1, CreatedAt: wireInstant(), UpdatedAt: wireInstant()}
}
func wireInvocation() uc.Invocation {
	return uc.Invocation{ID: wireID[mc.Invocation](), CallID: wireID[mc.Call](), AttemptIndex: 1,
		Consumer:   mc.Consumer{Kind: mc.AgentConsumer, ProjectID: wireID[id.Project](), Purpose: mc.AgentGeneration, AgentID: wirePtr(wireID[id.Agent]()), ExecutionID: wirePtr(wireID[id.Execution]())},
		SnapshotID: wireID[mc.Snapshot](), Identity: mc.ModelIdentity{ProviderID: wireID[mc.Provider](), ModelID: wireID[mc.Model](), ProviderName: "old provider", ModelName: "old model", ProviderModelID: "old native", Protocol: mc.OpenAIChat, Profile: mc.OpenAIChatV1, ModelType: mc.ChatModel, AdapterRevision: "v1"},
		ProcessID: wireID[oc.Process](), Fence: 1, Dispatch: uc.Reserved, StartedAt: wireInstant(), Usage: mc.Usage{Source: mc.UnknownUsage}}
}
func wireSummary() uc.Summary {
	field := uc.FieldSummary{Sum: wirePtr(mc.TokenCount(0))}
	return uc.Summary{Input: field, Output: field, Total: field, CachedInput: field, CacheWrite: field, Reasoning: field, AsOf: wireInstant()}
}
func wireFault(t *testing.T, err error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("wanted safe %s, got %v", code, err)
	}
}
func wireObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func wireFields(t *testing.T, value map[string]any, fields string) {
	t.Helper()
	got := make([]string, 0, len(value))
	for name := range value {
		got = append(got, name)
	}
	want := strings.Fields(fields)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fields %v != %v", got, want)
	}
}
func wireRowObject(t *testing.T, row uc.Invocation) map[string]any {
	t.Helper()
	raw, err := encodeList(context.Background(), wireQuery(), uc.Page{Items: []uc.Invocation{row}})
	if err != nil {
		t.Fatal(err)
	}
	return wireObject(t, raw)["items"].([]any)[0].(map[string]any)
}

func TestProjectUsageWireStrictQueries(t *testing.T) {
	project := wireID[id.Project]()
	q, err := listQuery(wireRequest(""), project)
	if err != nil || q.Limit != 50 || q.Cursor != "" || q.Filter.From != nil || q.Filter.To != nil {
		t.Fatal("default query altered original window", err)
	}
	values := url.Values{"consumer_kind": {"agent"}, "agent_id": {wireID[id.Agent]().String()}, "execution_id": {wireID[id.Execution]().String()}, "meeting_id": {wireID[pc.Meeting]().String()}, "purpose": {"agent_compaction"}, "provider_id": {wireID[mc.Provider]().String()}, "model_id": {wireID[mc.Model]().String()}, "status": {"cancelled"}, "from": {"2026-10-01T00:00:00+02:00"}, "to": {"2026-10-07T01:00:00.123456Z"}, "limit": {"100"}, "cursor": {"opaque-not-verified-here"}}
	q, err = listQuery(wireRequest(values.Encode()), project)
	if err != nil || q.Validate() != nil || q.Filter.From.String() != "2026-09-30T22:00:00.000000Z" || q.Filter.MeetingID != values.Get("meeting_id") || q.Cursor != values.Get("cursor") {
		t.Fatal("typed filters changed", err)
	}
	for _, group := range []uc.GroupBy{uc.ByConsumer, uc.ByAgent, uc.ByModel, uc.ByProvider, uc.ByExecution, uc.ByMeeting, uc.ByPurpose, uc.ByDay} {
		values.Set("group_by", string(group))
		got, err := aggregateQuery(wireRequest(values.Encode()), project)
		if err != nil || got.Validate() != nil || got.GroupBy != group || got.Limit != 100 {
			t.Fatal("aggregate filters/group rejected", group, err)
		}
	}
	for _, kind := range []mc.ConsumerKind{mc.AgentConsumer, mc.MeetingConsumer, mc.KnowledgeConsumer, mc.MemoryConsumer, mc.ToolConsumer} {
		if q, err := listQuery(wireRequest("consumer_kind="+string(kind)), project); err != nil || *q.Filter.ConsumerKind != kind {
			t.Fatal(kind, err)
		}
	}
	for _, purpose := range wirePurposes() {
		if q, err := listQuery(wireRequest("purpose="+string(purpose)), project); err != nil || *q.Filter.Purpose != purpose {
			t.Fatal(purpose, err)
		}
	}
	for _, status := range []uc.TerminalStatus{uc.Succeeded, uc.Failed, uc.Cancelled, uc.Unknown} {
		if q, err := listQuery(wireRequest("status="+string(status)), project); err != nil || *q.Filter.Status != status {
			t.Fatal(status, err)
		}
	}
	for _, raw := range []string{"username=admin&project_name=Project", "user%6eame=Admin&project_name=Project-._9"} {
		u, p, err := resolveQuery(wireRequest(raw))
		if err != nil || u == "" || p == "" {
			t.Fatal("current admin route rejected", err)
		}
	}
}
func TestProjectUsageWireQueryRejectionsAndBounds(t *testing.T) {
	bad := []string{"x=1", "limit=0", "limit=101", "limit=01", "limit=+1", "limit=-1", "limit=1.0", "limit=1e1", "limit=１２", "limit=1&limit=2", "limit=1&li%6dit=2", "cursor=", "cursor=a&cursor=b", "limit=1&", "&limit=1", "limit=1&&cursor=x", "limit", "=value", "limit=%", "limit=%ff", "limit=%00", "%ff=a", "%00=a", "limit=1;x=2", "consumer_kind=invalid", "purpose=invalid", "status=sent", "agent_id=invalid", "execution_id=invalid", "provider_id=invalid", "model_id=invalid", "meeting_id=invalid", "from=2026-10-07T00:00:00Z", "to=2026-10-07T00:00:00Z", "from=2026-10-07T00:00:00Z&to=2026-10-07T00:00:00Z", "from=2026-10-08T00:00:00Z&to=2026-10-07T00:00:00Z", "from=2026-10-01T00:00:00.1234567Z&to=2026-10-07T00:00:00Z", "group_by=day"}
	for i, raw := range bad {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			q, err := listQuery(wireRequest(raw), wireID[id.Project]())
			wireFault(t, err, f.InvalidArgument)
			if !reflect.DeepEqual(q, uc.Query{}) {
				t.Fatal("bad query retained candidate")
			}
		})
	}
	for _, r := range []*http.Request{nil, {}, {URL: &url.URL{ForceQuery: true}}} {
		_, err := listQuery(r, wireID[id.Project]())
		wireFault(t, err, f.InvalidArgument)
	}
	for _, raw := range []string{"", "group_by=", "group_by=sql", "group_by=day&group_by=model", "group_by=day,model"} {
		_, err := aggregateQuery(wireRequest(raw), wireID[id.Project]())
		wireFault(t, err, f.InvalidArgument)
	}
	for _, raw := range []string{"username=admin", "project_name=project", "username=admin&project_name=.", "username=admin&project_name=..", "username=admin&project_name=a%2fb", "username=admin&project_name=a%5cb", "username=admin&project_name=%252e", "username=admin&project_name=%00", "username=admin&project_name=project&cursor=x", "username=admin&project_name=project&project_id=x", "username=ab&project_name=project", "username=-admin&project_name=project"} {
		u, p, err := resolveQuery(wireRequest(raw))
		wireFault(t, err, f.InvalidArgument)
		if u != "" || p != "" {
			t.Fatal("bad resolve candidate")
		}
	}
	q, err := listQuery(wireRequest("cursor="+strings.Repeat("x", maxCursorBytes)), wireID[id.Project]())
	if err != nil || len(q.Cursor) != maxCursorBytes {
		t.Fatal("maximum cursor", err)
	}
	for _, value := range []string{strings.Repeat("x", maxCursorBytes+1), strings.Repeat("é", maxCursorBytes/2+1)} {
		_, err := listQuery(wireRequest("cursor="+url.QueryEscape(value)), wireID[id.Project]())
		wireFault(t, err, f.CursorInvalid)
		if strings.Contains(err.Error(), value) {
			t.Fatal("cursor leaked")
		}
	}
	for _, n := range []int{maxQueryBytes, maxQueryBytes + 1} {
		_, err := strictQuery(wireRequest("provider_id="+strings.Repeat("x", n-len("provider_id="))), false, false)
		if n == maxQueryBytes && err != nil {
			t.Fatal("raw bound inclusive", err)
		}
		if n > maxQueryBytes {
			wireFault(t, err, f.InvalidArgument)
		}
	}
	for _, raw := range []string{strings.ToUpper(wireID[id.Agent]().String()), "01900000-0000-4000-8000-000000000001"} {
		if raw == wireID[id.Agent]().String() {
			raw = strings.Replace(raw, "01900000", "019a0000", 1)
			raw = strings.ToUpper(raw)
		}
		_, err := listQuery(wireRequest("agent_id="+raw), wireID[id.Project]())
		wireFault(t, err, f.InvalidArgument)
	}
}

func wirePurposes() []mc.Purpose {
	return []mc.Purpose{mc.AgentGeneration, mc.AgentCompaction, mc.ApprovalAuto, mc.MeetingSummaryInitial, mc.MeetingSummaryUpdate, mc.KnowledgeEmbedding, mc.MemoryEmbedding, mc.MemoryExtraction, mc.MemoryConsolidation, mc.MemoryReflection, mc.Rerank, mc.ImageGeneration}
}
func wirePurposeInvocation(purpose mc.Purpose) uc.Invocation {
	row := wireInvocation()
	row.Consumer.Purpose = purpose
	row.Identity.ModelType = purpose.ModelType()
	switch row.Identity.ModelType {
	case mc.EmbeddingModel:
		row.Identity.Protocol = mc.OpenAIEmbeddings
	case mc.RerankerModel:
		row.Identity.Protocol = mc.JinaRerank
	case mc.ImageModel:
		row.Identity.Protocol = mc.OpenAIImages
	}
	row.Identity.Profile = row.Identity.Protocol.Profile()
	switch purpose {
	case mc.AgentGeneration, mc.AgentCompaction:
	case mc.MeetingSummaryInitial, mc.MeetingSummaryUpdate:
		row.Consumer.Kind = mc.MeetingConsumer
		row.Consumer.AgentID = nil
		row.Consumer.ExecutionID = nil
		row.Consumer.MeetingID = wireID[pc.Meeting]().String()
		row.Consumer.OperationID = wireID[pc.Operation]().String()
	case mc.KnowledgeEmbedding, mc.Rerank:
		row.Consumer.Kind = mc.KnowledgeConsumer
		row.Consumer.AgentID = nil
		row.Consumer.ExecutionID = nil
		row.Consumer.OperationID = wireID[pc.Operation]().String()
	case mc.MemoryEmbedding, mc.MemoryExtraction, mc.MemoryConsolidation, mc.MemoryReflection:
		row.Consumer.Kind = mc.MemoryConsumer
		row.Consumer.ExecutionID = nil
		row.Consumer.OperationID = wireID[pc.Operation]().String()
	case mc.ApprovalAuto, mc.ImageGeneration:
		row.Consumer.Kind = mc.ToolConsumer
		row.Consumer.AgentID = nil
		row.Consumer.ExecutionID = nil
		row.Consumer.OperationID = wireID[pc.Operation]().String()
	}
	return row
}
func TestProjectUsageWireHistoricalEnumsAndClosedProjection(t *testing.T) {
	for _, purpose := range wirePurposes() {
		row := wirePurposeInvocation(purpose)
		got := wireRowObject(t, row)
		if got["purpose"] != string(purpose) || got["protocol"] != string(row.Identity.Protocol) {
			t.Fatal("historical purpose/identity lost")
		}
	}
	row := wireInvocation()
	row.Identity.Protocol = mc.AnthropicMessages
	row.Identity.Profile = mc.AnthropicMessagesV1
	wireRowObject(t, row)
	row = wirePurposeInvocation(mc.Rerank)
	row.Consumer.Kind = mc.MemoryConsumer
	row.Consumer.AgentID = wirePtr(wireID[id.Agent]())
	wireRowObject(t, row)
	for _, dispatch := range []uc.Dispatch{uc.Reserved, uc.Authorized, uc.Sent, uc.NotSent, uc.DispatchUnknown} {
		row := wireInvocation()
		row.Dispatch = dispatch
		if dispatch == uc.Sent {
			row.DispatchedAt = wirePtr(row.StartedAt)
		}
		wireRowObject(t, row)
	}
	for _, status := range []uc.TerminalStatus{uc.Succeeded, uc.Failed, uc.Cancelled, uc.Unknown} {
		row := wireInvocation()
		row.Dispatch = uc.Sent
		row.DispatchedAt = wirePtr(row.StartedAt)
		row.Final = &uc.Final{Status: status, Version: 1, FinishedAt: row.StartedAt}
		got := wireRowObject(t, row)
		wireFields(t, got["final"].(map[string]any), "status finished_at error_category")
	}
	for _, category := range strings.Fields("invalid_request authentication permission model_not_found rate_limited context_too_large provider_unavailable timeout network cancelled content_filter unsupported_feature provider_error unknown") {
		row := wireInvocation()
		row.Final = &uc.Final{Status: uc.Failed, Version: 1, FinishedAt: row.StartedAt, Error: &mc.ModelError{Category: mc.ErrorCategory(category), Code: "private_code", ProviderRequestID: "private_nested_id"}}
		got := wireRowObject(t, row)
		if got["final"].(map[string]any)["error_category"] != category {
			t.Fatal("safe category lost")
		}
	}
	got := wireRowObject(t, wireInvocation())
	wireFields(t, got, "id call_id attempt_index project_id consumer_kind purpose agent_id execution_id meeting_id provider_id model_id provider_name model_name provider_model_id protocol model_type live_provider_id live_model_id dispatch started_at dispatched_at final usage provider_request_id")
	wireFields(t, got["usage"].(map[string]any), "input_tokens output_tokens total_tokens cached_input_tokens cache_write_tokens reasoning_tokens source")
	for _, name := range []string{"meeting_id", "live_provider_id", "live_model_id", "dispatched_at", "final", "provider_request_id"} {
		if value, ok := got[name]; !ok || value != nil {
			t.Fatal("absent association not explicit null", name)
		}
	}
	if got["provider_name"] != "old provider" || got["model_name"] != "old model" || got["provider_model_id"] != "old native" {
		t.Fatal("frozen identity changed")
	}
}
func TestProjectUsageWirePrecisionNullAndEmpty(t *testing.T) {
	for _, n := range []mc.TokenCount{0, 9007199254740993, math.MaxInt64} {
		row := wireInvocation()
		row.AttemptIndex = f.Sequence(math.MaxInt64)
		row.Dispatch = uc.Sent
		row.DispatchedAt = wirePtr(row.StartedAt)
		row.Usage = mc.Usage{InputTokens: &n, Source: mc.ProviderUsage}
		got := wireRowObject(t, row)
		if got["attempt_index"] != "9223372036854775807" || got["usage"].(map[string]any)["input_tokens"] != fmt.Sprint(int64(n)) || got["usage"].(map[string]any)["total_tokens"] != nil {
			t.Fatal("precision, known zero or unknown total changed")
		}
	}
	raw, err := encodeList(context.Background(), wireQuery(), uc.Page{})
	if err != nil || string(raw) != `{"items":[],"next_cursor":null}` {
		t.Fatal("empty list", string(raw), err)
	}
	query := uc.AggregateQuery{Filter: wireQuery().Filter, GroupBy: uc.ByDay, Limit: 50}
	raw, err = encodeAggregate(context.Background(), query, uc.AggregatePage{AsOf: wireInstant()})
	if err != nil || string(raw) != `{"items":[],"next_cursor":null,"as_of":"2026-10-07T01:02:03.123456Z"}` {
		t.Fatal("empty aggregate", string(raw), err)
	}
	project := wireProject()
	project.Version = math.MaxInt64
	raw, err = encodeProject(context.Background(), wireActor(), project)
	if err != nil {
		t.Fatal(err)
	}
	got := wireObject(t, raw)
	wireFields(t, got, "id owner_user_id name normalized_name description lifecycle version current_sprint_id created_at updated_at archived_at")
	if got["version"] != "9223372036854775807" || got["current_sprint_id"] != nil || got["archived_at"] != nil {
		t.Fatal("Project precise/null fields changed")
	}
}
func TestProjectUsageWireRejectsCompleteInvocationCorruption(t *testing.T) {
	changes := map[string]func(*uc.Invocation){
		"process": func(r *uc.Invocation) { r.ProcessID = oc.ProcessID{} }, "fence": func(r *uc.Invocation) { r.Fence = 0 }, "snapshot": func(r *uc.Invocation) { r.SnapshotID = mc.SnapshotID{} }, "profile": func(r *uc.Invocation) { r.Identity.Profile = "private-invalid" }, "revision": func(r *uc.Invocation) { r.Identity.AdapterRevision = "private invalid" }, "operation": func(r *uc.Invocation) { r.Consumer.OperationID = "private-invalid" }, "error_code": func(r *uc.Invocation) {
			r.Final = &uc.Final{Status: uc.Failed, Version: 1, FinishedAt: r.StartedAt, Error: &mc.ModelError{Category: "network", Code: "private invalid"}}
		}, "final_version": func(r *uc.Invocation) { r.Final = &uc.Final{Status: uc.Failed, FinishedAt: r.StartedAt} }, "purpose": func(r *uc.Invocation) { r.Consumer.Purpose = mc.KnowledgeEmbedding }, "protocol": func(r *uc.Invocation) { r.Identity.Protocol = mc.OpenAIEmbeddings }, "live_id": func(r *uc.Invocation) {
			v, _ := f.ParseID[mc.Model]("01900000-0000-7000-8000-000000000002")
			r.LiveModelID = &v
		}, "dispatch_time": func(r *uc.Invocation) { r.Dispatch = uc.Sent }, "usage": func(r *uc.Invocation) { r.Usage.InputTokens = wirePtr(mc.TokenCount(0)) }, "request_id": func(r *uc.Invocation) { r.ProviderRequestID = "private invalid" }, "project": func(r *uc.Invocation) {
			r.Consumer.ProjectID, _ = f.ParseID[id.Project]("01900000-0000-7000-8000-000000000002")
		},
	}
	for name, change := range changes {
		for _, position := range []int{0, 1, 99} {
			t.Run(fmt.Sprintf("%s/%d", name, position), func(t *testing.T) {
				page := uc.Page{Items: make([]uc.Invocation, 100), NextCursor: "private-cursor"}
				for i := range page.Items {
					page.Items[i] = wireInvocation()
				}
				change(&page.Items[position])
				raw, err := encodeList(context.Background(), wireQuery(), page)
				wireFault(t, err, f.DependencyUnavailable)
				if raw != nil {
					t.Fatal("corrupt row leaked candidate")
				}
				var fault *f.Fault
				_ = errors.As(err, &fault)
				if fault.CommitState != f.Committed {
					t.Fatal("projection changed completed read state")
				}
			})
		}
	}
	page := uc.Page{Items: []uc.Invocation{wireInvocation(), wireInvocation()}}
	q := wireQuery()
	q.Limit = 1
	if raw, err := encodeList(context.Background(), q, page); err == nil || raw != nil {
		t.Fatal("lookahead row leaked into HTTP page")
	}
	for _, cursor := range []string{strings.Repeat("x", 8193), "\x00", "\xff"} {
		if raw, err := encodeList(context.Background(), wireQuery(), uc.Page{NextCursor: cursor}); err == nil || raw != nil {
			t.Fatal("invalid result cursor accepted")
		}
	}
}
func wireGroup(by uc.GroupBy) uc.GroupKey {
	key := uc.GroupKey{By: by}
	switch by {
	case uc.ByDay:
		key.Day = wirePtr("2026-10-07")
	case uc.ByConsumer:
		key.ID = wirePtr("agent")
	case uc.ByPurpose:
		key.ID = wirePtr("agent_generation")
	default:
		key.ID = wirePtr(wireID[id.Agent]().String())
	}
	return key
}
func TestProjectUsageWireAggregateOriginalValidation(t *testing.T) {
	for _, by := range []uc.GroupBy{uc.ByConsumer, uc.ByAgent, uc.ByModel, uc.ByProvider, uc.ByExecution, uc.ByMeeting, uc.ByPurpose, uc.ByDay} {
		q := uc.AggregateQuery{Filter: wireQuery().Filter, GroupBy: by, Limit: 100}
		p := uc.AggregatePage{Items: []uc.GroupSummary{{Key: wireGroup(by), Summary: wireSummary()}}, AsOf: wireInstant()}
		raw, err := encodeAggregate(context.Background(), q, p)
		if err != nil {
			t.Fatal(by, err)
		}
		got := wireObject(t, raw)
		wireFields(t, got, "items next_cursor as_of")
		row := got["items"].([]any)[0].(map[string]any)
		wireFields(t, row, "key summary")
		wireFields(t, row["key"].(map[string]any), "by id day")
		wireFields(t, row["summary"].(map[string]any), "confirmed_invocations dispatch_unknown succeeded failed cancelled unknown input output total cached_input cache_write reasoning as_of")
		if by == uc.ByAgent || by == uc.ByExecution || by == uc.ByMeeting {
			p.Items[0].Key.ID = nil
			if _, err = encodeAggregate(context.Background(), q, p); err != nil {
				t.Fatal("null group lost", err)
			}
		}
	}
	q := uc.AggregateQuery{Filter: wireQuery().Filter, GroupBy: uc.ByModel, Limit: 100}
	for name, change := range map[string]func(*uc.AggregatePage){"as_of": func(p *uc.AggregatePage) { p.Items[0].Summary.AsOf = f.Instant{} }, "denominator": func(p *uc.AggregatePage) { p.Items[0].Summary.Input.UnknownCount = 1 }, "overflow": func(p *uc.AggregatePage) {
		p.Items[0].Summary.ConfirmedInvocations = math.MaxInt64
		p.Items[0].Summary.DispatchUnknown = 1
	}, "negative": func(p *uc.AggregatePage) { p.Items[0].Summary.Failed = -1 }, "unknown_sum": func(p *uc.AggregatePage) { p.Items[0].Summary.Input.Sum = nil }, "group": func(p *uc.AggregatePage) { p.Items[0].Key.By = uc.ByAgent }, "null_model": func(p *uc.AggregatePage) { p.Items[0].Key.ID = nil }, "extra_day": func(p *uc.AggregatePage) { p.Items[0].Key.Day = wirePtr("2026-10-07") }, "page_size": func(p *uc.AggregatePage) { p.Items = make([]uc.GroupSummary, 101) }, "cursor": func(p *uc.AggregatePage) { p.NextCursor = strings.Repeat("x", 8193) }} {
		t.Run(name, func(t *testing.T) {
			p := uc.AggregatePage{Items: []uc.GroupSummary{{Key: wireGroup(uc.ByModel), Summary: wireSummary()}}, AsOf: wireInstant()}
			change(&p)
			raw, err := encodeAggregate(context.Background(), q, p)
			wireFault(t, err, f.DependencyUnavailable)
			if raw != nil {
				t.Fatal("bad aggregate candidate")
			}
		})
	}
}
func TestProjectUsageWireProjectFullValidationAndActor(t *testing.T) {
	for name, change := range map[string]func(*pc.ProjectRef){"owner": func(p *pc.ProjectRef) { p.OwnerUserID, _ = f.ParseID[id.User]("01900000-0000-7000-8000-000000000002") }, "normalized_name": func(p *pc.ProjectRef) { p.NormalizedName = "other" }, "description": func(p *pc.ProjectRef) { p.Description = "\x00" }, "sprint": func(p *pc.ProjectRef) { p.CurrentSprintID = &pc.SprintID{} }, "version": func(p *pc.ProjectRef) { p.Version = 0 }, "archived": func(p *pc.ProjectRef) { p.Lifecycle = pc.Archived }} {
		t.Run(name, func(t *testing.T) {
			p := wireProject()
			change(&p)
			raw, err := encodeProject(context.Background(), wireActor(), p)
			wireFault(t, err, f.DependencyUnavailable)
			if raw != nil {
				t.Fatal("bad Project candidate")
			}
		})
	}
	agent, _ := id.NewAgentRun(wireID[id.Project](), wireID[id.Agent](), wireID[id.Execution]())
	for _, actor := range []id.Actor{{}, agent} {
		if raw, err := encodeProject(context.Background(), actor, wireProject()); raw != nil || err == nil {
			t.Fatal("non-Human Project candidate")
		}
	}
}

type wireCancelMarshaler struct{ cancel context.CancelFunc }

func (m wireCancelMarshaler) MarshalJSON() ([]byte, error) {
	m.cancel()
	return []byte(`{"ok":true}`), nil
}
func TestProjectUsageWireCompleteEncodingAndCancellation(t *testing.T) {
	for _, n := range []int{maxRepresentationBytes - 2, maxRepresentationBytes - 1} {
		body, err := encodeRepresentation(context.Background(), strings.Repeat("x", n))
		if n == maxRepresentationBytes-2 {
			if err != nil || len(body) != maxRepresentationBytes {
				t.Fatal("inclusive representation bound", err)
			}
		} else if body != nil || err == nil {
			t.Fatal("oversized encoding leaked bytes")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	body, err := encodeRepresentation(ctx, wireCancelMarshaler{cancel})
	if body != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("late cancellation published bytes", err)
	}
	for _, run := range []func() ([]byte, error){func() ([]byte, error) { return encodeList(ctx, wireQuery(), uc.Page{}) }, func() ([]byte, error) {
		return encodeAggregate(ctx, uc.AggregateQuery{Filter: wireQuery().Filter, GroupBy: uc.ByDay, Limit: 1}, uc.AggregatePage{AsOf: wireInstant()})
	}, func() ([]byte, error) { return encodeProject(ctx, wireActor(), wireProject()) }} {
		body, err := run()
		if body != nil || !errors.Is(err, context.Canceled) {
			t.Fatal("cancelled candidate", err)
		}
	}
	if body, err := encodeRepresentation(context.Background(), make(chan int)); body != nil || err == nil {
		t.Fatal("encoding error lost")
	}
}
func wireMaximumInvocation() uc.Invocation {
	row := wireInvocation()
	row.AttemptIndex = math.MaxInt64
	row.Consumer.Purpose = mc.AgentCompaction
	row.Consumer.MeetingID = wireID[pc.Meeting]().String()
	row.Identity.ProviderName = strings.Repeat("\x01", 128)
	row.Identity.ModelName = strings.Repeat("\x01", 128)
	row.Identity.ProviderModelID = strings.Repeat("\x01", 256)
	row.Identity.Protocol = mc.AnthropicMessages
	row.Identity.Profile = mc.AnthropicMessagesV1
	row.LiveProviderID = wirePtr(row.Identity.ProviderID)
	row.LiveModelID = wirePtr(row.Identity.ModelID)
	row.Dispatch = uc.Sent
	row.DispatchedAt = wirePtr(row.StartedAt)
	row.Final = &uc.Final{Status: uc.Cancelled, Version: math.MaxInt64, FinishedAt: row.StartedAt, Error: &mc.ModelError{Category: "provider_unavailable", Code: strings.Repeat("x", 128), Dispatched: true}}
	row.ProviderRequestID = strings.Repeat("x", 256)
	max := wirePtr(mc.TokenCount(math.MaxInt64))
	row.Usage = mc.Usage{InputTokens: max, OutputTokens: max, TotalTokens: max, CachedInputTokens: max, CacheWriteTokens: max, ReasoningTokens: max, Source: mc.ProviderUsage}
	return row
}
func TestProjectUsageWireMaximumEscapedCapacity(t *testing.T) {
	row := wireMaximumInvocation()
	one, err := encodeList(context.Background(), wireQuery(), uc.Page{Items: []uc.Invocation{row}})
	if err != nil {
		t.Fatal(err)
	}
	var onePage struct {
		Items []json.RawMessage `json:"items"`
	}
	if json.Unmarshal(one, &onePage) != nil {
		t.Fatal("encoding")
	}
	if len(onePage.Items[0]) > 5376 || !bytes.Contains(one, []byte(`\u0001`)) {
		t.Fatal("row bound/default escaping failed", len(onePage.Items[0]))
	}
	page := uc.Page{Items: make([]uc.Invocation, 100), NextCursor: strings.Repeat("x", 8192)}
	for i := range page.Items {
		page.Items[i] = row
	}
	body, err := encodeList(context.Background(), wireQuery(), page)
	if err != nil || len(body) > 546048 {
		t.Fatal("fixed list capacity derivation failed", len(body), err)
	}
	project := wireProject()
	project.Name = strings.Repeat("a", 64)
	project.NormalizedName = project.Name
	project.Description = strings.Repeat("<", 8192)
	project.Version = math.MaxInt64
	project.CurrentSprintID = wirePtr(wireID[pc.Sprint]())
	project.Lifecycle = pc.Archived
	project.ArchivedAt = wirePtr(project.UpdatedAt)
	projectBody, err := encodeProject(context.Background(), wireActor(), project)
	if err != nil || len(projectBody) >= 64<<10 || !bytes.Contains(projectBody, []byte(`\u003c`)) {
		t.Fatal("Project capacity/default escaping", len(projectBody), err)
	}
	max := mc.TokenCount(math.MaxInt64)
	field := uc.FieldSummary{Sum: &max, KnownCount: max}
	summary := uc.Summary{ConfirmedInvocations: max, Succeeded: max, Input: field, Output: field, Total: field, CachedInput: field, CacheWrite: field, Reasoning: field, AsOf: wireInstant()}
	p := uc.AggregatePage{Items: make([]uc.GroupSummary, 100), NextCursor: strings.Repeat("x", 8192), AsOf: wireInstant()}
	for i := range p.Items {
		p.Items[i] = uc.GroupSummary{Key: wireGroup(uc.ByModel), Summary: summary}
	}
	aggregateBody, err := encodeAggregate(context.Background(), uc.AggregateQuery{Filter: wireQuery().Filter, GroupBy: uc.ByModel, Limit: 100}, p)
	if err != nil || len(aggregateBody) > 100*2048+8192+256 {
		t.Fatal("aggregate capacity", len(aggregateBody), err)
	}
	t.Logf("maximum legal default-escaped bytes: row=%d list100=%d aggregate100=%d project=%d", len(onePage.Items[0]), len(body), len(aggregateBody), len(projectBody))
}

func TestProjectUsageWireSchemaStructure(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "api", "openapi", "project-usage.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := wireObject(t, raw)
	if doc["openapi"] != "3.1.0" {
		t.Fatal("wrong schema dialect")
	}
	paths := doc["paths"].(map[string]any)
	if len(paths) != 3 {
		t.Fatal("unexpected resources")
	}
	for _, path := range []string{"/api/v1/projects/resolve", "/api/v1/projects/{id}/model-usage", "/api/v1/projects/{id}/model-usage/summary"} {
		methods, ok := paths[path].(map[string]any)
		if !ok || len(methods) != 2 {
			t.Fatal("resource methods", path)
		}
		for _, method := range []string{"get", "head"} {
			operation, ok := methods[method].(map[string]any)
			if !ok {
				t.Fatal("missing method", method)
			}
			if _, ok := operation["requestBody"]; ok {
				t.Fatal("request entity declared")
			}
			for _, response := range operation["responses"].(map[string]any) {
				_, content := response.(map[string]any)["content"]
				if content != (method == "get") {
					t.Fatal("HEAD body or missing GET body")
				}
			}
		}
	}
	for name, schema := range doc["components"].(map[string]any)["schemas"].(map[string]any) {
		s := schema.(map[string]any)
		if s["type"] != "object" {
			continue
		}
		if s["additionalProperties"] != false || len(s["required"].([]any)) != len(s["properties"].(map[string]any)) {
			t.Fatal("open/optional DTO fields", name)
		}
	}
}

type wireSchemaCase struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
	Value  any    `json:"value"`
	Valid  bool   `json:"valid"`
}

// External validators are opt-in, pinned by the offline evidence command. The
// ordinary Go suite needs no Python/Node installation and explicitly reports a
// skip rather than pretending that its structural check is a standard parser.
func TestProjectUsageWireSchemaStandards(t *testing.T) {
	python, node := os.Getenv("AGENTEAM_USAGE_SCHEMA_PYTHON"), os.Getenv("AGENTEAM_USAGE_SCHEMA_NODE")
	if python == "" || node == "" {
		t.Skip("set exact offline Python jsonschema and Node paths for standard-schema/ECMA checks")
	}
	dir := os.Getenv("AGENTEAM_USAGE_SCHEMA_EVIDENCE")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	cases := []wireSchemaCase{}
	add := func(name, schema string, value any, valid bool) {
		cases = append(cases, wireSchemaCase{name, schema, value, valid})
	}
	for _, purpose := range wirePurposes() {
		add("purpose/"+string(purpose), "Invocation", wireRowObject(t, wirePurposeInvocation(purpose)), true)
	}
	row := wireInvocation()
	row.Identity.Protocol = mc.AnthropicMessages
	row.Identity.Profile = mc.AnthropicMessagesV1
	add("anthropic", "Invocation", wireRowObject(t, row), true)
	row = wirePurposeInvocation(mc.Rerank)
	row.Consumer.Kind = mc.MemoryConsumer
	row.Consumer.AgentID = wirePtr(wireID[id.Agent]())
	add("memory_rerank", "Invocation", wireRowObject(t, row), true)
	for _, dispatch := range []uc.Dispatch{uc.Reserved, uc.Authorized, uc.Sent, uc.NotSent, uc.DispatchUnknown} {
		row := wireInvocation()
		row.Dispatch = dispatch
		if dispatch == uc.Sent {
			row.DispatchedAt = wirePtr(row.StartedAt)
		}
		add("dispatch/"+string(dispatch), "Invocation", wireRowObject(t, row), true)
	}
	for _, status := range []uc.TerminalStatus{uc.Succeeded, uc.Failed, uc.Cancelled, uc.Unknown} {
		row := wireInvocation()
		row.Dispatch = uc.Sent
		row.DispatchedAt = wirePtr(row.StartedAt)
		row.Final = &uc.Final{Status: status, Version: 1, FinishedAt: row.StartedAt}
		add("final/"+string(status), "Invocation", wireRowObject(t, row), true)
	}
	for _, category := range strings.Fields("invalid_request authentication permission model_not_found rate_limited context_too_large provider_unavailable timeout network cancelled content_filter unsupported_feature provider_error unknown") {
		row := wireInvocation()
		row.Final = &uc.Final{Status: uc.Failed, Version: 1, FinishedAt: row.StartedAt, Error: &mc.ModelError{Category: mc.ErrorCategory(category)}}
		add("category/"+category, "Invocation", wireRowObject(t, row), true)
	}
	add("maximum_row", "Invocation", wireRowObject(t, wireMaximumInvocation()), true)
	row = wireInvocation()
	row.Identity.ProviderName = strings.Repeat("😀", 128)
	add("unicode_name", "Invocation", wireRowObject(t, row), true)
	for _, by := range []uc.GroupBy{uc.ByConsumer, uc.ByAgent, uc.ByModel, uc.ByProvider, uc.ByExecution, uc.ByMeeting, uc.ByPurpose, uc.ByDay} {
		q := uc.AggregateQuery{Filter: wireQuery().Filter, GroupBy: by, Limit: 100}
		p := uc.AggregatePage{Items: []uc.GroupSummary{{Key: wireGroup(by), Summary: wireSummary()}}, AsOf: wireInstant()}
		raw, err := encodeAggregate(context.Background(), q, p)
		if err != nil {
			t.Fatal(err)
		}
		add("aggregate/"+string(by), "Aggregate", json.RawMessage(raw), true)
		if by == uc.ByAgent || by == uc.ByExecution || by == uc.ByMeeting {
			key := wireGroup(by)
			key.ID = nil
			add("null_group/"+string(by), "GroupKey", key, true)
		}
	}
	for _, lifecycle := range []pc.Lifecycle{pc.Active, pc.Archiving, pc.Archived, pc.Deleting} {
		project := wireProject()
		project.Lifecycle = lifecycle
		if lifecycle == pc.Archived {
			project.ArchivedAt = wirePtr(project.CreatedAt)
		}
		raw, err := encodeProject(context.Background(), wireActor(), project)
		if err != nil {
			t.Fatal(err)
		}
		add("project/"+string(lifecycle), "Project", json.RawMessage(raw), true)
	}
	for _, n := range []string{"0", "1", "9007199254740993", "9223372036854775807"} {
		add("integer/"+n, "common:NonnegativeInt64String", n, true)
		if n != "0" {
			add("positive/"+n, "common:PositiveInt64String", n, true)
		}
	}
	for i, value := range []any{"-1", "01", "1e3", "9223372036854775808", "9999999999999999999", "0\n", "1\r", "1\u2028", "1\u2029", "１２", float64(1), nil} {
		add(fmt.Sprintf("bad_integer/%d", i), "common:NonnegativeInt64String", value, false)
	}
	add("positive_zero", "common:PositiveInt64String", "0", false)
	for name, change := range map[string]func(map[string]any){"extra": func(v map[string]any) { v["snapshot_id"] = wireID[mc.Snapshot]().String() }, "missing_null": func(v map[string]any) { delete(v, "final") }, "protocol": func(v map[string]any) { v["protocol"] = "future" }, "protocol_type": func(v map[string]any) { v["protocol"] = "openai-embeddings" }, "consumer_purpose": func(v map[string]any) { v["purpose"] = "meeting_summary_update" }, "consumer_agent": func(v map[string]any) { v["agent_id"] = nil }, "consumer_execution": func(v map[string]any) { v["execution_id"] = nil }, "purpose_type": func(v map[string]any) { v["model_type"] = "image_generation" }, "sent_no_time": func(v map[string]any) { v["dispatch"] = "sent" }, "not_sent_time": func(v map[string]any) { v["dispatched_at"] = wireInstant().String() }, "unknown_usage_zero": func(v map[string]any) { v["usage"].(map[string]any)["total_tokens"] = "0" }, "provider_usage_empty": func(v map[string]any) { v["usage"].(map[string]any)["source"] = "provider" }, "unknown_usage_extra": func(v map[string]any) { v["usage"].(map[string]any)["estimate"] = "0" }, "succeeded_not_sent": func(v map[string]any) {
		v["final"] = map[string]any{"status": "succeeded", "finished_at": wireInstant().String(), "error_category": nil}
	}, "request_newline": func(v map[string]any) { v["provider_request_id"] = "id\n" }, "name_too_long": func(v map[string]any) { v["model_name"] = strings.Repeat("😀", 129) }, "name_nul": func(v map[string]any) { v["provider_name"] = "\x00" }, "attempt_zero": func(v map[string]any) { v["attempt_index"] = "0" }, "attempt_overflow": func(v map[string]any) { v["attempt_index"] = "9223372036854775808" }} {
		v := wireRowObject(t, wireInvocation())
		change(v)
		add("bad_invocation/"+name, "Invocation", v, false)
	}
	for _, category := range []any{"unknown", nil} {
		add(fmt.Sprint("succeeded_category/", category), "Final", map[string]any{"status": "succeeded", "finished_at": wireInstant().String(), "error_category": category}, category == nil)
	}
	for _, value := range []any{nil, []any{}} {
		add(fmt.Sprint("list_items/", value), "List", map[string]any{"items": value, "next_cursor": nil}, value != nil)
	}
	for _, field := range []map[string]any{{"sum": nil, "known_count": "0", "unknown_count": "1"}, {"sum": "0", "known_count": "0", "unknown_count": "0"}, {"sum": "9223372036854775807", "known_count": "1", "unknown_count": "0"}} {
		add("field_summary", "FieldSummary", field, true)
	}
	for _, field := range []map[string]any{{"sum": "0", "known_count": "0", "unknown_count": "1"}, {"sum": nil, "known_count": "0", "unknown_count": "0"}, {"sum": nil, "known_count": "1", "unknown_count": "0"}} {
		add("bad_field_summary", "FieldSummary", field, false)
	}
	for _, key := range []uc.GroupKey{{By: uc.ByDay, ID: wirePtr("day"), Day: wirePtr("2026-10-07")}, {By: uc.ByModel}, {By: uc.ByConsumer, ID: wirePtr("invalid")}, {By: uc.ByPurpose, ID: wirePtr("invalid")}, {By: uc.ByAgent, ID: wirePtr("invalid")}, {By: uc.ByModel, ID: wirePtr(wireID[mc.Model]().String()), Day: wirePtr("2026-10-07")}} {
		add("bad_group", "GroupKey", key, false)
	}
	for _, value := range []string{"id\n", "id\r", "id\u2028", "id\u2029", "é"} {
		add("bad_safe_id", "ProviderRequestID", value, false)
	}
	for _, value := range []string{".", "..", "project\n", "project\u2028", "project%", "a/b"} {
		add("bad_project_name", "ProjectName", value, false)
	}
	for _, value := range []string{"admin", "AdMin", "User-Name"} {
		add("username", "Username", value, true)
	}
	for _, value := range []string{"admin\n", "admin\u2029", "-admin", "ab"} {
		add("bad_username", "Username", value, false)
	}
	for _, value := range []string{"2026-10-07T01:02:03.123456Z\n", "2026-10-07T01:02:03.123456Z\u2028"} {
		add("bad_instant", "common:Instant", value, false)
	}
	root := filepath.Join("..", "..", "..", "..", "api", "openapi")
	doc, err := os.ReadFile(filepath.Join(root, "project-usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	common, err := os.ReadFile(filepath.Join(root, "common.json"))
	if err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(map[string]any{"document": json.RawMessage(doc), "common": json.RawMessage(common), "cases": cases})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "schema-input.json"), input, 0600); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		name, path, script string
		args               []string
	}{{"python", python, wirePythonSchema, []string{"-c"}}, {"node", node, wireNodePatterns, []string{"-e"}}} {
		if err = os.WriteFile(filepath.Join(dir, check.name+"-check.txt"), []byte(check.script), 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, check.path, append(check.args, check.script)...)
		cmd.Stdin = bytes.NewReader(input)
		output, err := cmd.CombinedOutput()
		cancel()
		if writeErr := os.WriteFile(filepath.Join(dir, check.name+"-raw.log"), output, 0600); writeErr != nil {
			t.Fatal(writeErr)
		}
		if err != nil {
			t.Fatalf("%s standard/schema check: %v\n%s", check.name, err, output)
		}
		t.Logf("%s: %s", check.name, output)
	}
}

const wirePythonSchema = `import sys,json,importlib.metadata
from jsonschema import Draft202012Validator
from referencing import Registry,Resource
from referencing.jsonschema import DRAFT202012
v=json.load(sys.stdin)
doc=v['document'];doc['$id']='https://usage-schema.invalid/project-usage.json'
common=v['common'];common['$id']='https://usage-schema.invalid/common.json'
registry=Registry().with_resources([(doc['$id'],Resource.from_contents(doc,default_specification=DRAFT202012)),(common['$id'],Resource.from_contents(common,default_specification=DRAFT202012))])
for document in [doc,common]:
 for schema in document['components']['schemas'].values(): Draft202012Validator.check_schema(schema)
for c in v['cases']:
 name=c['schema']; target=common if name.startswith('common:') else doc; name=name.split(':')[-1]
 schema={'$id':target['$id'],'$ref':'#/components/schemas/'+name,'components':target['components']}
 errors=list(Draft202012Validator(schema,registry=registry).iter_errors(c['value']))
 if (not errors)!=c['valid']: raise AssertionError((c['name'],c['schema'],c['valid'],[e.message for e in errors]))
print('Draft202012Validator jsonschema='+importlib.metadata.version('jsonschema')+' referencing='+importlib.metadata.version('referencing')+' cases='+str(len(v['cases']))+' PASS; format annotations remain server typed validation')
`
const wireNodePatterns = `const fs=require('node:fs'); const v=JSON.parse(fs.readFileSync(0,'utf8'));
let compiled=0;function walk(x){if(!x||typeof x!=='object')return;for(const [k,y] of Object.entries(x)){if(k==='pattern'){new RegExp(y,'u');compiled++}else walk(y)}}walk(v.document);walk(v.common);
function check(s,x,doc){if(s.$ref){let target=doc;let ref=s.$ref;if(ref.startsWith('./common.json')){target=v.common;ref=ref.slice('./common.json'.length)}s=ref.slice(2).split('/').reduce((a,k)=>a[k],target);return check(s,x,target)}
if(s.type==='string'&&typeof x!=='string')return false;if(s.type==='null'&&x!==null)return false;if('const'in s&&x!==s.const)return false;if(s.enum&&!s.enum.includes(x))return false;
if(typeof x==='string'){if(s.pattern&&!new RegExp(s.pattern,'u').test(x))return false;if(s.minLength!==undefined&&[...x].length<s.minLength)return false;if(s.maxLength!==undefined&&[...x].length>s.maxLength)return false}
if(s.not&&check(s.not,x,doc))return false;if(s.anyOf&&!s.anyOf.some(a=>check(a,x,doc)))return false;if(s.allOf&&!s.allOf.every(a=>check(a,x,doc)))return false;return true}
let tested=0;const names=new Set(['ProviderRequestID','ProjectName','Username','common:Instant','common:NonnegativeInt64String','common:PositiveInt64String']);
for(const c of v.cases){if(!names.has(c.schema))continue;const doc=c.schema.startsWith('common:')?v.common:v.document;const name=c.schema.split(':').at(-1);const actual=check(doc.components.schemas[name],c.value,doc);if(actual!==c.valid)throw Error('ECMA scalar boundary: '+c.name);tested++}
for(const n of [128,129]){const actual=check(v.document.components.schemas.Name,'😀'.repeat(n),v.document);if(actual!==(n===128))throw Error('Unicode scalar length')}
console.log('Node '+process.version+' ECMA u-patterns='+compiled+' scalar cases='+tested+' Unicode=2 PASS');
`
