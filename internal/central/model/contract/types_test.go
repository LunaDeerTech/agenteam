package contract

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
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
func digest(s string) f.Digest { return f.Digest("sha256:" + strings.Repeat(s, 64)) }

type fixture struct {
	actor    id.Actor
	c        Consumer
	owner    sc.CredentialLeaseOwner
	call     CallID
	inv      InvocationID
	process  oc.ProcessID
	snapshot ConfigSnapshot
	lease    sc.LeaseID
	now      f.Instant
}

func setup(t *testing.T) fixture {
	t.Helper()
	p, a, e := fresh[id.Project](t), fresh[id.Agent](t), fresh[id.Execution](t)
	actor, err := id.NewAgentRun(p, a, e)
	must(t, err)
	o, err := sc.NewCredentialLeaseOwner(sc.ExecutionOwner, e.String())
	must(t, err)
	now, err := f.NewInstant(time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	must(t, err)
	return fixture{actor: actor, c: Consumer{Kind: AgentConsumer, ProjectID: p, Purpose: AgentGeneration, AgentID: &a, ExecutionID: &e}, owner: o, call: fresh[Call](t), inv: fresh[Invocation](t), process: fresh[oc.Process](t), snapshot: ConfigSnapshot{ID: fresh[Snapshot](t), Identity: ModelIdentity{ProviderID: fresh[Provider](t), ModelID: fresh[Model](t), ProviderName: "provider", ModelName: "chat", ProviderModelID: "upstream-model", Protocol: OpenAIChat, Profile: OpenAIChatV1, ModelType: ChatModel, AdapterRevision: "native-v1"}, Endpoint: "https://provider.example/v1", Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}, lease: fresh[sc.Lease](t), now: now}
}
func (x fixture) input(t *testing.T) InputIdentity {
	return InputIdentity{ExecutionID: copyPtr(x.c.ExecutionID), RoundID: fresh[struct{}](t).String(), Digest: digest("a"), SchemaVersion: 1}
}
func (x fixture) resolve() ResolveRequest {
	return ResolveRequest{Actor: x.actor, Consumer: x.c, Source: CurrentSelectionSource, Purpose: x.c.Purpose, ModelRef: ptr(x.snapshot.Identity.ModelID), Selection: &SelectionRef{Kind: "direct"}, LeaseOwner: x.owner}
}
func (x fixture) read(t *testing.T) ConsumerRequest {
	return ConsumerRequest{Action: ReadCredentialConsumer, Actor: x.actor, Consumer: x.c, SnapshotID: x.snapshot.ID, LeaseOwner: x.owner, LeaseID: &x.lease, CallID: &x.call, Attempt: &AttemptIdentity{CallID: x.call, InvocationID: x.inv, AttemptIndex: 1, ProcessID: x.process, Fence: 1}, Input: ptr(x.input(t))}
}
func locks(t *testing.T) []f.LockRequest {
	k, e := f.SystemConfigLock("model-references")
	must(t, e)
	return []f.LockRequest{{Key: k, Mode: f.Shared}}
}
func TestTokenCountAndUsageUnknown(t *testing.T) {
	for _, n := range []TokenCount{0, 9007199254740993, math.MaxInt64} {
		b, e := json.Marshal(n)
		must(t, e)
		if string(b) != fmt.Sprintf(`"%d"`, n) {
			t.Fatal(string(b))
		}
		var out TokenCount
		must(t, json.Unmarshal(b, &out))
		if out != n {
			t.Fatal(out)
		}
	}
	for _, s := range []string{`0`, `null`, `"-1"`, `"01"`, `"+1"`, `"1.0"`, `"9223372036854775808"`, `" 1"`} {
		var n TokenCount
		reject(t, json.Unmarshal([]byte(s), &n))
	}
	must(t, (Usage{Source: UnknownUsage}).Validate())
	reject(t, (Usage{Source: UnknownUsage, TotalTokens: ptr(TokenCount(0))}).Validate())
	reject(t, (Usage{Source: ProviderUsage}).Validate())
	u := Usage{Source: ProviderUsage, InputTokens: ptr(TokenCount(0)), TotalTokens: ptr(TokenCount(7)), ReasoningTokens: ptr(TokenCount(10))}
	must(t, u.Validate())
	v := u.Clone()
	*u.InputTokens = 99
	if *v.InputTokens != 0 {
		t.Fatal("alias")
	}
	b, _ := json.Marshal(v)
	if !strings.Contains(string(b), `"output_tokens":null`) || !strings.Contains(string(b), `"input_tokens":"0"`) {
		t.Fatal(string(b))
	}
}
func TestConsumerAndSnapshotNoImplicitAuthority(t *testing.T) {
	x := setup(t)
	must(t, x.c.Validate())
	badc := x.c.Clone()
	badc.Kind = MeetingConsumer
	reject(t, badc.Validate())
	badc = x.c.Clone()
	badc.AgentID = nil
	reject(t, badc.Validate())
	must(t, (ResolvedModel{Snapshot: x.snapshot, Consumer: x.c, LeaseOwner: x.owner}).Validate())
	r := ResolvedModel{Snapshot: x.snapshot, Consumer: x.c, LeaseOwner: x.owner, CredentialLease: &sc.CredentialLease{LeaseID: x.lease}}
	reject(t, r.Validate())
	c := x.snapshot.Clone()
	c.Parameters[1] = 'x'
	if string(x.snapshot.Parameters) != `{}` {
		t.Fatal("alias")
	}
	c = x.snapshot.Clone()
	c.Identity.Protocol = AnthropicMessages
	reject(t, c.Validate())
}
func TestStructuralJSONRejectsAmbiguity(t *testing.T) {
	for _, raw := range []string{`{"x":1,"x":2}`, `{"x":{"a":1,"a":2}}`, `{} {}`, `[]`, `{"x":1e999}`, `{"x":NaN}`, strings.Repeat(`{"x":`, 33) + `0` + strings.Repeat(`}`, 33)} {
		reject(t, objectJSON([]byte(raw), 64<<10))
	}
	must(t, objectJSON([]byte(`{"x":[1,null,true,{"b":"  keep  "}]}`), 64<<10))
}

func TestConsumerPurposeMatrixAndCredentialBinding(t *testing.T) {
	x := setup(t)
	op := fresh[struct{}](t).String()
	meeting := fresh[struct{}](t).String()
	consumers := []Consumer{
		x.c,
		{Kind: MeetingConsumer, ProjectID: x.c.ProjectID, Purpose: MeetingSummaryUpdate, MeetingID: meeting, OperationID: op},
		{Kind: KnowledgeConsumer, ProjectID: x.c.ProjectID, Purpose: KnowledgeEmbedding, OperationID: op},
		{Kind: MemoryConsumer, ProjectID: x.c.ProjectID, Purpose: MemoryReflection, AgentID: x.c.AgentID, OperationID: op},
		{Kind: ToolConsumer, ProjectID: x.c.ProjectID, Purpose: ImageGeneration, OperationID: op},
	}
	for _, c := range consumers {
		must(t, c.Validate())
		c.Purpose = "unregistered"
		reject(t, c.Validate())
	}
	consumers[1].AgentID = x.c.AgentID
	reject(t, consumers[1].Validate())
	consumers[2].ExecutionID = x.c.ExecutionID
	reject(t, consumers[2].Validate())
	consumers[3].AgentID = nil
	reject(t, consumers[3].Validate())
	consumers[4].MeetingID = meeting
	reject(t, consumers[4].Validate())
	scope, e := id.InProject(x.c.ProjectID)
	must(t, e)
	ref, e := sc.NewCredentialRef(fresh[sc.Credential](t), scope)
	must(t, e)
	x.snapshot.CredentialRef = &ref
	r := ResolvedModel{Snapshot: x.snapshot, Consumer: x.c, LeaseOwner: x.owner, CredentialLease: &sc.CredentialLease{LeaseID: x.lease, CredentialRef: ref}}
	must(t, r.Validate())
	other, e := sc.NewCredentialRef(fresh[sc.Credential](t), scope)
	must(t, e)
	c := r.Clone()
	c.CredentialLease.CredentialRef = other
	reject(t, c.Validate())
	must(t, r.Validate())
	c = r.Clone()
	c.Snapshot.CredentialRef = nil
	reject(t, c.Validate())
}
