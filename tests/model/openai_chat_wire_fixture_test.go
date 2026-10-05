//go:build integration

package model_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
)

const wireCredentialCanary = "synthetic-wire-credential-canary"
const wireBodyCanary = "synthetic-provider-private-body-canary"

type wireResolver struct{ ip netip.Addr }

func (r wireResolver) Lookup(ctx context.Context, _ string) ([]netip.Addr, error) {
	return []netip.Addr{r.ip}, ctx.Err()
}

type wireNilResolver chan struct{}

func (wireNilResolver) Lookup(context.Context, string) ([]netip.Addr, error) {
	panic("typed nil must be rejected")
}

type wireFixture struct {
	net       *netfixture.Descriptor
	policy    *outbound.PolicyService
	transport wire.Transport
	actor     id.Actor
	adapter   *wire.OpenAIChat
	budget    *wire.Budget
	budgets   []*wire.Budget
	materials []sc.SecretMaterial
	cases     []string
}

// SQL only seeds owned User/Session facts. Real Account Authority, Audit and
// PolicyService decide policy mutations and network denials. These synthetic
// materials are protocol inputs, not Model Secret resolve or Invocation grants.
func newWireFixture(t *testing.T) *wireFixture {
	t.Helper()
	descriptor, err := netfixture.Load()
	if err != nil {
		t.Fatal("mandatory owned outbound fixture", err)
	}
	db := newDatabase(t)
	store := openStore(t, db.Config(t, nil))
	ak, ck, _ := testKeys(t)
	accounts, err := account.NewAuthority(store, ak)
	if err != nil {
		t.Fatal(err)
	}
	if err = accounts.Initialize(testContext(t)); err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, ck, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = aud.CheckStorage(testContext(t)); err != nil {
		t.Fatal(err)
	}
	policy, err := outbound.NewPolicyService(store, aud, outbound.Authorizations{Sessions: accounts, System: accounts})
	if err != nil {
		t.Fatal(err)
	}
	if err = policy.Reload(testContext(t)); err != nil {
		t.Fatal(err)
	}
	trust, err := outbound.LoadTrustStore(descriptor.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	v := &wireFixture{net: descriptor, policy: policy, actor: (&fixture{raw: store}).human(t, "admin")}
	v.transport = wire.Transport{Policy: policy, Trust: trust, Resolver: wireResolver{netip.MustParseAddr(descriptor.PrivateIP)}}
	v.budget = wire.NewBudget()
	v.adapter = v.newAdapter(t, v.budget)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		joined := true
		for _, b := range v.budgets {
			if err := b.Force(ctx); err != nil || !b.Joined() {
				joined = false
				t.Errorf("wire cleanup not joined: %v", err)
			}
		}
		// Borrowed materials stay live through every actual client/parser join.
		if joined {
			for _, m := range v.materials {
				m.Destroy()
			}
		}
		for _, key := range v.cases {
			v.settled(t, key)
		}
	})
	return v
}
func (v *wireFixture) newAdapter(t *testing.T, b *wire.Budget) *wire.OpenAIChat {
	t.Helper()
	a, e := wire.NewOpenAIChat(v.transport, b)
	if e != nil {
		t.Fatal(e)
	}
	v.budgets = append(v.budgets, b)
	return a
}
func (v *wireFixture) allow(t *testing.T, allowed bool) {
	t.Helper()
	rules, _ := outbound.NewRules()
	if allowed {
		ports, e := outbound.SelectedPorts(8443, 8080)
		if e != nil {
			t.Fatal(e)
		}
		rule, e := outbound.NewRule(v.net.PrivateIP+"/32", ports, false)
		if e != nil {
			t.Fatal(e)
		}
		rules, e = outbound.NewRules(rule)
		if e != nil {
			t.Fatal(e)
		}
	}
	command, e := f.NewCommandIdentity("outbound-policy", []string{v.actor.Details().UserID}, "update", f.IdempotencyKey(newID[struct{}](t).String()))
	if e != nil {
		t.Fatal(e)
	}
	status := v.policy.Status()
	if status.Version == nil {
		t.Fatal("missing policy version")
	}
	_, e = v.policy.UpdatePolicy(testContext(t), v.actor, outbound.CommandMeta{Identity: command, ExpectedVersion: *status.Version, HTTPTraceID: newID[struct{}](t).String()}, rules)
	if e != nil {
		t.Fatal(e)
	}
}
func (v *wireFixture) scenario(t *testing.T, s netfixture.WireScenario) string {
	t.Helper()
	key, e := v.net.Create(testContext(t), netfixture.ScenarioConfig{Mode: "openai_chat_wire", Wire: &s})
	if e != nil {
		t.Fatal("wire control", e)
	}
	v.cases = append(v.cases, key)
	return key
}
func (v *wireFixture) state(t *testing.T, key string) netfixture.State {
	t.Helper()
	s, e := v.net.State(testContext(t), key)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func wireWait(t *testing.T, description string, fn func() bool) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if fn() {
			return
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal(description)
		}
	}
}
func (v *wireFixture) settled(t *testing.T, key string) {
	t.Helper()
	wireWait(t, "wire handlers/connections not terminal", func() bool {
		s := v.state(t, key)
		if s.ActiveHandlers != 0 || s.CompletedHandlers != len(s.Requests) {
			return false
		}
		for _, r := range s.Requests {
			if !s.Closed[r.Connection] {
				return false
			}
		}
		return true
	})
}
func (v *wireFixture) input(t *testing.T, key string, mode wire.ResponseMode) (wire.Request, wire.CallOptions) {
	t.Helper()
	material, e := sc.NewSecretMaterial([]byte(wireCredentialCanary))
	if e != nil {
		t.Fatal(e)
	}
	v.materials = append(v.materials, material)
	keyCause, e := ac.NewAppendKey(ac.AccessProducer, newID[struct{}](t).String(), 0)
	if e != nil {
		t.Fatal(e)
	}
	call, e := outbound.NewCallContext(v.actor, id.SystemScope(), keyCause, ac.Associations{HTTPTraceID: newID[struct{}](t).String()})
	if e != nil {
		t.Fatal(e)
	}
	req := wire.Request{Snapshot: mc.ConfigSnapshot{ID: newID[mc.Snapshot](t), Identity: mc.ModelIdentity{ProviderID: newID[mc.Provider](t), ModelID: newID[mc.Model](t), ProviderName: "wire fixture", ModelName: "text fixture", ProviderModelID: "fixture-native-model", Protocol: mc.OpenAIChat, Profile: mc.OpenAIChatV1, ModelType: mc.ChatModel, AdapterRevision: wire.OpenAIChatTextRevision}, Endpoint: "https://fixture.test:8443/case/" + key, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}, Streaming: true}}, Messages: []mc.Message{{Role: "system", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "  exact 系统\n"}}}}, {Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "wire input"}}}}}, ToolChoice: mc.ToolChoice{Kind: "none"}, ResponseFormat: mc.ResponseFormat{Kind: "text"}, Mode: mode}
	return req, wire.CallOptions{ProjectID: newID[id.Project](t), Context: call, Credential: material, Limits: outbound.Limits{Overall: 20 * time.Second}}
}
func (v *wireFixture) start(t *testing.T, ctx context.Context, a *wire.OpenAIChat, r wire.Request, o wire.CallOptions) *wire.Exchange {
	t.Helper()
	x, e := a.Start(ctx, r, o)
	if e != nil {
		t.Fatal("wire Start", e)
	}
	return x
}
func wireClose(t *testing.T, x *wire.Exchange) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := x.Close(ctx); e != nil || !x.Joined() {
		t.Fatal("wire Close/join", e)
	}
}
func wireModelError(t *testing.T, e error, category mc.ErrorCategory, code string, sent, partial bool) *mc.ModelError {
	t.Helper()
	var m *mc.ModelError
	if !errors.As(e, &m) || m.Validate() != nil || m.Category != category || m.Code != code || m.Dispatched != sent || m.PartialOutput != partial {
		t.Fatalf("safe wire error got %v, expected %s/%s sent=%v partial=%v", e, category, code, sent, partial)
	}
	wireNoLeak(t, m)
	return m
}
func wireFault(t *testing.T, e error, code f.Code) {
	t.Helper()
	var fault *f.Fault
	if !errors.As(e, &fault) || fault.Code != code || fault.CommitState != f.NotStarted {
		t.Fatalf("admission fault got %v expected %s/not_started", e, code)
	}
}
func wireNoLeak(t *testing.T, value any) {
	t.Helper()
	for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		s := fmt.Sprintf(format, value)
		if strings.Contains(s, wireCredentialCanary) || strings.Contains(s, wireBodyCanary) {
			t.Fatal("unsafe format")
		}
	}
	raw, e := json.Marshal(value)
	if e != nil || strings.Contains(string(raw), "canary") {
		t.Fatal("unsafe JSON view", e)
	}
	var out strings.Builder
	slog.New(slog.NewJSONHandler(&out, nil)).Info("wire-test", "value", value)
	if strings.Contains(out.String(), "canary") {
		t.Fatal("unsafe log view")
	}
}
func wireReply(text, finish string, usage json.RawMessage, refusal any) []byte {
	value := map[string]any{"id": "chat-native-id", "object": "chat.completion", "created": 1, "model": "fixture-native-model", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": text, "refusal": refusal}, "finish_reason": finish}}}
	if usage != nil {
		value["usage"] = usage
	}
	b, _ := json.Marshal(value)
	return b
}
func wireJSON(body []byte) netfixture.WireScenario {
	return netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json; charset=utf-8", "X-Request-Id": "req_wire:1"}, Chunks: [][]byte{body}}
}
func wireChunk(delta map[string]any, finish any, usage json.RawMessage, obfuscation any, empty bool) []byte {
	choices := []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}
	if empty {
		choices = []any{}
	}
	value := map[string]any{"id": "chat-stream-id", "object": "chat.completion.chunk", "created": 1, "model": "fixture-native-model", "choices": choices, "usage": usage}
	if obfuscation != nil {
		value["obfuscation"] = obfuscation
	}
	raw, _ := json.Marshal(value)
	return append(append([]byte("data: "), raw...), []byte("\n\n")...)
}
func wireSSE(chunks ...[]byte) netfixture.WireScenario {
	return netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "text/event-stream; charset=utf-8", "X-Request-Id": "req_stream-1"}, Chunks: chunks}
}
