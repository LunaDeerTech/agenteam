package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func platformEmbeddingTestRequest(t *testing.T, purpose mc.Purpose) mc.ResolveRequest {
	t.Helper()
	actor, e := id.NewHuman(mustID[id.User](t), mustID[id.Session](t))
	if e != nil {
		t.Fatal(e)
	}
	owner, e := sc.NewCredentialLeaseOwner(sc.ModelCallOwner, mustID[mc.Call](t).String())
	if e != nil {
		t.Fatal(e)
	}
	c := mc.Consumer{Kind: mc.KnowledgeConsumer, ProjectID: mustID[id.Project](t), Purpose: purpose, OperationID: mustID[struct{}](t).String()}
	if purpose == mc.MemoryEmbedding {
		c.Kind = mc.MemoryConsumer
		agent := mustID[id.Agent](t)
		c.AgentID = &agent
	}
	r := mc.ResolveRequest{Actor: actor, Consumer: c, Purpose: purpose, Source: mc.CurrentSelectionSource, Selection: &mc.SelectionRef{Kind: "platform", Selector: mc.EmbeddingSelector}, LeaseOwner: owner}
	if e = r.Validate(); e != nil {
		t.Fatal(e)
	}
	return r
}

func platformEmbeddingTestProfile() (providerRecord, modelRecord) {
	return providerRecord{Input: providerInput{Name: "embedding-provider", Protocol: mc.OpenAIEmbeddings, BaseURL: "https://embedding.example/v1", Enabled: true, Options: json.RawMessage(`{}`)}}, modelRecord{Input: mc.ModelInput{Name: "embedding-model", ProviderModelID: "fixed-model", Type: mc.EmbeddingModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}}}}
}

func TestModelPlatformEmbeddingResolutionProfileMatrix(t *testing.T) {
	selection := mc.SelectionRef{Kind: "platform", Selector: mc.EmbeddingSelector}
	for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
		t.Run(string(purpose), func(t *testing.T) {
			r := platformEmbeddingTestRequest(t, purpose)
			p, m := platformEmbeddingTestProfile()
			const expectedContextLength mc.TokenCount = 8192
			contextLength := expectedContextLength
			for _, length := range []*mc.TokenCount{nil, &contextLength} {
				m.Input.Capabilities.ContextLength = length
				before, e := json.Marshal(m.Input)
				if e != nil || m.Input.Validate() != nil {
					t.Fatal("invalid positive profile", e)
				}
				revision, e := resolutionProfile(&p, &m, selection)
				if e != nil || revision != adapter.OpenAIEmbeddingsFloatRevision {
					t.Fatal("float profile", revision, e)
				}
				after, e := json.Marshal(m.Input)
				if e != nil || string(before) != string(after) {
					t.Fatal("profile changed configured capabilities", e)
				}
				d := resolutionSnapshotDTO{Format: 1, ID: mustID[mc.Snapshot](t), ProjectID: r.Consumer.ProjectID, Identity: mc.ModelIdentity{ProviderID: mustID[mc.Provider](t), ModelID: mustID[mc.Model](t), ProviderName: p.Input.Name, ModelName: m.Input.Name, ProviderModelID: m.Input.ProviderModelID, Protocol: p.Input.Protocol, Profile: p.Input.Protocol.Profile(), ModelType: m.Input.Type, AdapterRevision: revision}, Endpoint: p.Input.BaseURL, Parameters: m.Input.Parameters, RequestOverwrite: m.Input.RequestOverwrite, HeaderOverwrite: map[string]string{}, Capabilities: m.Input.Capabilities.Clone()}
				snapshot, e := d.snapshot()
				if e != nil || snapshot.Identity.Profile != mc.OpenAIEmbeddingsV1 || !reflect.DeepEqual(snapshot.Capabilities, m.Input.Capabilities) {
					t.Fatal("snapshot does not preserve the accepted float profile", e)
				}
				out := mc.ResolvedModel{Snapshot: snapshot, Consumer: r.Consumer, LeaseOwner: r.LeaseOwner}
				if out.Validate() != nil || fmt.Sprintf("%+v", out) != "model_resolved_model" || fmt.Sprintf("%+v", snapshot) != "model_config_snapshot" {
					t.Fatal("invalid or unsafe embedding result projection")
				}
				copy := out.Clone()
				copy.Snapshot.Parameters[0] = '['
				copy.Snapshot.Capabilities.OutputModalities[0] = "text"
				copy.Snapshot.HeaderOverwrite["X-Test"] = "mutated"
				if copy.Snapshot.Capabilities.ContextLength != nil {
					*copy.Snapshot.Capabilities.ContextLength = 1
				}
				if string(out.Snapshot.Parameters) != "{}" || out.Snapshot.Capabilities.OutputModalities[0] != "vector" || len(out.Snapshot.HeaderOverwrite) != 0 || length != nil && (out.Snapshot.Capabilities.ContextLength == nil || *out.Snapshot.Capabilities.ContextLength != expectedContextLength) {
					t.Fatal("caller mutation escaped result clone")
				}
			}
		})
	}
	for name, change := range map[string]func(*providerRecord, *modelRecord){
		"chat-protocol": func(p *providerRecord, _ *modelRecord) { p.Input.Protocol = mc.OpenAIChat },
		"jina-protocol": func(p *providerRecord, _ *modelRecord) { p.Input.Protocol = mc.JinaRerank },
		"chat-model":    func(_ *providerRecord, m *modelRecord) { m.Input.Type = mc.ChatModel },
		"options":       func(p *providerRecord, _ *modelRecord) { p.Input.Options = json.RawMessage(`{"native":true}`) },
		"parameters":    func(_ *providerRecord, m *modelRecord) { m.Input.Parameters = json.RawMessage(`{"dimensions":16}`) },
		"overwrite": func(_ *providerRecord, m *modelRecord) {
			m.Input.RequestOverwrite = json.RawMessage(`{"input":"replacement"}`)
		},
		"headers":   func(_ *providerRecord, m *modelRecord) { m.Input.HeaderOverwrite = map[string]string{"X-Test": "x"} },
		"streaming": func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.Streaming = true },
		"tools":     func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.ToolCalls = true },
		"parallel": func(_ *providerRecord, m *modelRecord) {
			m.Input.Capabilities.ToolCalls = true
			m.Input.Capabilities.ParallelToolCalls = true
		},
		"reasoning": func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.Reasoning = true },
		"efforts": func(_ *providerRecord, m *modelRecord) {
			m.Input.Capabilities.Reasoning = true
			m.Input.Capabilities.ReasoningEfforts = []string{"high"}
		},
		"structured-text": func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.StructuredOutputModes = []string{"text"} },
		"structured-json": func(_ *providerRecord, m *modelRecord) {
			m.Input.Capabilities.StructuredOutputModes = []string{"json_schema"}
		},
		"max-output":  func(_ *providerRecord, m *modelRecord) { n := mc.TokenCount(1); m.Input.Capabilities.MaxOutput = &n },
		"input-empty": func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.InputModalities = nil },
		"input-image": func(_ *providerRecord, m *modelRecord) {
			m.Input.Capabilities.InputModalities = []string{"text", "image"}
		},
		"input-vector": func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.InputModalities = []string{"vector"} },
		"output-empty": func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.OutputModalities = nil },
		"output-text":  func(_ *providerRecord, m *modelRecord) { m.Input.Capabilities.OutputModalities = []string{"text"} },
		"output-extra": func(_ *providerRecord, m *modelRecord) {
			m.Input.Capabilities.OutputModalities = []string{"vector", "text"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			p, m := platformEmbeddingTestProfile()
			change(&p, &m)
			before, e := json.Marshal([]any{p.Input, m.Input})
			if e != nil {
				t.Fatal("encode profile inputs before resolution", e)
			}
			revision, e := resolutionProfile(&p, &m, selection)
			requireCode(t, e, f.CapabilityUnsupported)
			after, e := json.Marshal([]any{p.Input, m.Input})
			if e != nil {
				t.Fatal("encode profile inputs after resolution", e)
			}
			if revision != "" || string(before) != string(after) {
				t.Fatal("unsupported configuration was stripped or published")
			}
		})
	}
	for _, provider := range []bool{false, true} {
		p, m := platformEmbeddingTestProfile()
		m.Input.Capabilities.Streaming = true
		if provider {
			p.Input.Enabled = false
		} else {
			m.Input.Enabled = false
		}
		_, e := resolutionProfile(&p, &m, selection)
		requireCode(t, e, f.InvalidState)
	}
	for _, length := range []mc.TokenCount{0, -1} {
		_, m := platformEmbeddingTestProfile()
		m.Input.Capabilities.ContextLength = &length
		if m.Input.Validate() == nil {
			t.Fatal("C0 accepted a nonpositive context length")
		}
	}
	// The new selector must neither broaden direct/Memory/Summary to embeddings
	// nor remove their accepted text/structured Chat profiles.
	for _, old := range []mc.SelectionRef{{Kind: "direct"}, {Kind: "platform", Selector: mc.MemorySelector}, {Kind: "platform", Selector: mc.MeetingSummarySelector}} {
		p, m := platformEmbeddingTestProfile()
		_, e := resolutionProfile(&p, &m, old)
		requireCode(t, e, f.CapabilityUnsupported)
		p.Input.Protocol, m.Input.Type = mc.OpenAIChat, mc.ChatModel
		m.Input.Capabilities.OutputModalities = []string{"text"}
		if old.Selector == mc.MemorySelector {
			m.Input.Capabilities.StructuredOutputModes = []string{"json_schema"}
		}
		revision, e := resolutionProfile(&p, &m, old)
		want := adapter.OpenAIChatTextRevision
		if old.Selector == mc.MemorySelector {
			want = adapter.OpenAIChatStructuredRevision
		}
		if e != nil || revision != want {
			t.Fatal("old Chat profile changed", old.Selector, e)
		}
	}
}

func TestModelPlatformEmbeddingResolutionEntryGuards(t *testing.T) {
	store := &noIOStore{}
	svc, e := New(store, resolutionTestAuthority(t, store), testDependencies(t))
	if e != nil {
		t.Fatal(e)
	}
	unbound, e := New(store, pureAuthority(t, store), testDependencies(t))
	if e != nil {
		t.Fatal(e)
	}
	for _, purpose := range []mc.Purpose{mc.KnowledgeEmbedding, mc.MemoryEmbedding} {
		t.Run(string(purpose), func(t *testing.T) {
			r := platformEmbeddingTestRequest(t, purpose)
			if resolutionVariant(r) != nil {
				t.Fatal("embedding variant rejected")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			p, e := svc.DiscoverResolve(ctx, r)
			if !errors.Is(e, context.Canceled) || !reflect.DeepEqual(p, mc.ResolutionPlan{}) {
				t.Fatal("canceled plan published", e)
			}
			out, e := svc.ResolveModel(ctx, r)
			if !errors.Is(e, context.Canceled) || !reflect.DeepEqual(out, mc.ResolvedModel{}) {
				t.Fatal("canceled result published", e)
			}
			out, e = svc.ResolveModelInTx(ctx, f.Tx{}, r, mc.ResolutionPlan{})
			if !errors.Is(e, context.Canceled) || !reflect.DeepEqual(out, mc.ResolvedModel{}) {
				t.Fatal("canceled in-Tx result published", e)
			}
			selected, e := svc.SelectModel(ctx, r.Actor, mc.SelectionRequest{Consumer: r.Consumer, Selection: *r.Selection})
			if !errors.Is(e, context.Canceled) || !reflect.DeepEqual(selected, mc.SelectionResult{}) {
				t.Fatal("canceled selection published", e)
			}
			_, e = svc.DiscoverResolve(nil, r)
			requireCode(t, e, f.InvalidArgument)
			p, e = unbound.DiscoverResolve(context.Background(), r)
			requireCode(t, e, f.DependencyUnbound)
			if !reflect.DeepEqual(p, mc.ResolutionPlan{}) {
				t.Fatal("unbound plan published")
			}
			out, e = unbound.ResolveModel(context.Background(), r)
			requireCode(t, e, f.DependencyUnbound)
			if !reflect.DeepEqual(out, mc.ResolvedModel{}) {
				t.Fatal("unbound result published")
			}
			out, e = svc.ResolveModelInTx(context.Background(), f.Tx{}, r, mc.ResolutionPlan{})
			requireCode(t, e, f.Forbidden)
			if !reflect.DeepEqual(out, mc.ResolvedModel{}) {
				t.Fatal("zero plan authorized a result")
			}
			for name, mutate := range map[string]func(*mc.ResolveRequest){
				"reasoning":          func(c *mc.ResolveRequest) { c.ReasoningEffort = "high" },
				"model-ref":          func(c *mc.ResolveRequest) { v := mustID[mc.Model](t); c.ModelRef = &v },
				"selection-project":  func(c *mc.ResolveRequest) { c.Selection.ProjectID = &c.Consumer.ProjectID },
				"version-zero":       func(c *mc.ResolveRequest) { v := f.Version(0); c.Selection.Version = &v },
				"wrong-selector":     func(c *mc.ResolveRequest) { c.Selection.Selector = mc.MemorySelector },
				"wrong-purpose":      func(c *mc.ResolveRequest) { c.Purpose = mc.Rerank },
				"missing-operation":  func(c *mc.ResolveRequest) { c.Consumer.OperationID = "" },
				"execution":          func(c *mc.ResolveRequest) { v := mustID[id.Execution](t); c.Consumer.ExecutionID = &v },
				"meeting":            func(c *mc.ResolveRequest) { c.Consumer.MeetingID = mustID[struct{}](t).String() },
				"serving-id":         func(c *mc.ResolveRequest) { v := mustID[mc.Snapshot](t); c.ServingSnapshotID = &v },
				"serving-generation": func(c *mc.ResolveRequest) { c.ServingGenerationID = mustID[struct{}](t).String() },
				"execution-owner": func(c *mc.ResolveRequest) {
					c.LeaseOwner, _ = sc.NewCredentialLeaseOwner(sc.ExecutionOwner, mustID[id.Execution](t).String())
				},
				"agent-shape": func(c *mc.ResolveRequest) {
					if c.Consumer.Kind == mc.MemoryConsumer {
						c.Consumer.AgentID = nil
					} else {
						v := mustID[id.Agent](t)
						c.Consumer.AgentID = &v
					}
				},
			} {
				t.Run(name, func(t *testing.T) {
					copy := r.Clone()
					mutate(&copy)
					if copy.Validate() == nil {
						t.Fatal("malformed request accepted")
					}
					p, e := svc.DiscoverResolve(context.Background(), copy)
					requireCode(t, e, f.InvalidArgument)
					if !reflect.DeepEqual(p, mc.ResolutionPlan{}) {
						t.Fatal("malformed plan published")
					}
					_, e = unbound.DiscoverResolve(context.Background(), copy)
					requireCode(t, e, f.InvalidArgument)
				})
			}
			serving := r.Clone()
			serving.Source, serving.Selection = mc.ServingSnapshotSource, nil
			snapshot := mustID[mc.Snapshot](t)
			serving.ServingSnapshotID, serving.ServingGenerationID = &snapshot, mustID[struct{}](t).String()
			if serving.Validate() != nil {
				t.Fatal("legal serving structure changed")
			}
			_, e = svc.DiscoverResolve(context.Background(), serving)
			requireCode(t, e, f.DependencyUnbound)
			registration, _ := id.RegisterService(id.SecretService)
			actor, _ := registration.Actor(mustID[struct{}](t).String(), id.SystemScope())
			_, e = svc.SelectModel(context.Background(), actor, mc.SelectionRequest{Consumer: r.Consumer, Selection: *r.Selection})
			requireCode(t, e, f.DependencyUnbound)
			if fmt.Sprintf("%+v", r) != "model_resolve_request" {
				t.Fatal("request unsafe projection")
			}
			copy := r.Clone()
			copy.Selection.Selector = mc.MemorySelector
			if r.Selection.Selector != mc.EmbeddingSelector {
				t.Fatal("selection clone aliases caller")
			}
		})
	}
	for _, purpose := range []mc.Purpose{mc.AgentGeneration, mc.AgentCompaction, mc.ApprovalAuto} {
		r := resolutionTestRequest(t)
		r.Purpose, r.Consumer.Purpose = purpose, purpose
		if purpose == mc.ApprovalAuto {
			r.Actor, _ = id.NewHuman(mustID[id.User](t), mustID[id.Session](t))
			r.Consumer.Kind, r.Consumer.AgentID = mc.ToolConsumer, nil
			r.Consumer.OperationID = mustID[struct{}](t).String()
		}
		if r.Validate() != nil || resolutionVariant(r) != nil {
			t.Fatal("legal direct agent/tool Chat variant narrowed", purpose)
		}
	}
	for _, purpose := range []mc.Purpose{mc.MemoryExtraction, mc.MemoryConsolidation, mc.MemoryReflection} {
		r := platformEmbeddingTestRequest(t, mc.MemoryEmbedding)
		r.Purpose, r.Consumer.Purpose, r.Selection.Selector = purpose, purpose, mc.MemorySelector
		if r.Validate() != nil || resolutionVariant(r) != nil {
			t.Fatal("old Memory variant changed", purpose)
		}
	}
	for _, purpose := range []mc.Purpose{mc.MeetingSummaryInitial, mc.MeetingSummaryUpdate} {
		r := meetingResolutionTestRequest(t)
		r.Purpose, r.Consumer.Purpose = purpose, purpose
		if r.Validate() != nil || resolutionVariant(r) != nil {
			t.Fatal("old Summary variant changed", purpose)
		}
		r.Selection.Kind, r.Selection.ProjectID = "project_summary", &r.Consumer.ProjectID
		if r.Validate() != nil {
			t.Fatal("old project Summary structure changed")
		}
		_, e = svc.DiscoverResolve(context.Background(), r)
		requireCode(t, e, f.DependencyUnbound)
	}
	r := platformEmbeddingTestRequest(t, mc.KnowledgeEmbedding)
	r.Purpose, r.Consumer.Purpose, r.Selection.Selector = mc.Rerank, mc.Rerank, mc.RerankerSelector
	if r.Validate() != nil {
		t.Fatal("legal Rerank structure changed")
	}
	_, e = svc.DiscoverResolve(context.Background(), r)
	requireCode(t, e, f.CapabilityUnsupported)
	r.Consumer.Kind, r.Purpose, r.Consumer.Purpose, r.Selection.Selector = mc.ToolConsumer, mc.ImageGeneration, mc.ImageGeneration, mc.ImageSelector
	if r.Validate() != nil {
		t.Fatal("legal Image structure changed")
	}
	_, e = svc.DiscoverResolve(context.Background(), r)
	requireCode(t, e, f.CapabilityUnsupported)
}
