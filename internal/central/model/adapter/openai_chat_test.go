package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
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

func TestOpenAIChatPinnedProfileManifest(t *testing.T) {
	raw, err := os.ReadFile("testdata/openai-chat-text-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Revision, Protocol, Profile, Commit string
		Sources                             []struct{ Path, URL, SHA256, GitBlob string }
		UsageProjection                     map[string]string `json:"usage_projection"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if p.Revision != OpenAIChatTextRevision || p.Protocol != string(mc.OpenAIChat) || p.Profile != string(mc.OpenAIChatV1) || p.Commit != "becc1d20eed83c1b8d85e15dc131a372d9dc7813" || len(p.Sources) != 17 || p.UsageProjection["prompt_tokens_details.cache_write_tokens"] != "cache_write_tokens" {
		t.Fatal("profile lost its exact adopted source identity")
	}
	seen := map[string]bool{}
	for _, source := range p.Sources {
		if seen[source.Path] || len(source.SHA256) != 64 || !strings.Contains(source.URL, "/blob/"+p.Commit+"/"+source.Path) {
			t.Fatal("invalid source provenance")
		}
		seen[source.Path] = true
	}
}

func TestOpenAIChatEncodedBodyBound(t *testing.T) {
	r, o := unitInput(t)
	r.Messages[0].Parts[0].Text.Text = strings.Repeat("<", 3<<20)
	if req, _, err := prepare(r, o); req != nil {
		t.Fatal("escaped body bypassed encoded byte cap")
	} else {
		requireModel(t, err, "provider_error", "wire_limit_exceeded")
	}
}
func unitInput(t *testing.T) (Request, CallOptions) {
	t.Helper()
	actor, err := id.NewHuman(fresh[id.User](t), fresh[id.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	key, err := ac.NewAppendKey(ac.AccessProducer, fresh[struct{}](t).String(), 0)
	if err != nil {
		t.Fatal(err)
	}
	call, err := outbound.NewCallContext(actor, id.SystemScope(), key, ac.Associations{})
	if err != nil {
		t.Fatal(err)
	}
	material, err := sc.NewSecretMaterial([]byte("wire-material-canary"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(material.Destroy)
	r := Request{Snapshot: mc.ConfigSnapshot{ID: fresh[mc.Snapshot](t), Identity: mc.ModelIdentity{ProviderID: fresh[mc.Provider](t), ModelID: fresh[mc.Model](t), ProviderName: "provider", ModelName: "chat", ProviderModelID: "native-model", Protocol: mc.OpenAIChat, Profile: mc.OpenAIChatV1, ModelType: mc.ChatModel, AdapterRevision: OpenAIChatTextRevision}, Endpoint: "https://provider.example/base%2Fsegment/", Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}, Streaming: true}}, Messages: []mc.Message{{Role: "user", Parts: []mc.MessagePart{{Text: &mc.TextPart{Text: "  text & 世界\n"}}}}}, ToolChoice: mc.ToolChoice{Kind: "none"}, ResponseFormat: mc.ResponseFormat{Kind: "text"}, Mode: JSONResponse}
	return r, CallOptions{ProjectID: fresh[id.Project](t), Context: call, Credential: material, Limits: outbound.Limits{Overall: time.Second}}
}
func requireModel(t *testing.T, err error, category mc.ErrorCategory, code string) *mc.ModelError {
	t.Helper()
	var m *mc.ModelError
	if !errors.As(err, &m) || m.Category != category || m.Code != code || m.Validate() != nil {
		t.Fatalf("model error got %v, want %s/%s", err, category, code)
	}
	return m
}
func requireFault(t *testing.T, err error, code f.Code) {
	t.Helper()
	var v *f.Fault
	if !errors.As(err, &v) || v.Code != code || v.CommitState != f.NotStarted {
		t.Fatalf("fault got %v, want %s/not_started", err, code)
	}
}

func TestOpenAIChatRequestEncodingAndClosedInputs(t *testing.T) {
	r, o := unitInput(t)
	r.Mode = SSEResponse
	max := mc.TokenCount(9007199254740993)
	r.Snapshot.Capabilities.MaxOutput = &max
	req, _, err := prepare(r, o)
	if err != nil {
		t.Fatal(err)
	}
	r.Messages[0].Parts[0].Text.Text = "mutated"
	*r.Snapshot.Capabilities.MaxOutput = 1
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	req.Body.Close()
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil {
		t.Fatal("body")
	}
	if req.URL.RequestURI() != "/base%2Fsegment/chat/completions" || req.Method != "POST" || req.Header.Get("Authorization") != "" || req.Header.Get("Accept") != "text/event-stream" || len(body) != 5 || string(body["max_completion_tokens"]) != "9007199254740993" || string(body["stream_options"]) != `{"include_usage":true}` || strings.Contains(string(raw), "mutated") {
		t.Fatalf("incorrect wire request %s", raw)
	}
	for _, tc := range []struct {
		name        string
		mutate      func(*Request, *CallOptions)
		unsupported bool
	}{
		{"revision", func(r *Request, _ *CallOptions) { r.Snapshot.Identity.AdapterRevision = "other" }, true},
		{"tools", func(r *Request, _ *CallOptions) {
			r.Tools = []mc.Tool{{Name: "tool", InputSchema: json.RawMessage(`{}`)}}
		}, true},
		{"reasoning", func(r *Request, _ *CallOptions) { r.Snapshot.Capabilities.Reasoning = true }, true},
		{"parallel_tools", func(r *Request, _ *CallOptions) {
			r.Snapshot.Capabilities.ToolCalls = true
			r.Snapshot.Capabilities.ParallelToolCalls = true
		}, true},
		{"tool_choice", func(r *Request, _ *CallOptions) { r.ToolChoice.Kind = "auto" }, true},
		{"structured", func(r *Request, _ *CallOptions) {
			r.ResponseFormat = mc.ResponseFormat{Kind: "json_schema", Name: "typed", Schema: json.RawMessage(`{}`)}
		}, true},
		{"structured_capability", func(r *Request, _ *CallOptions) {
			r.Snapshot.Capabilities.StructuredOutputModes = []string{"text", "json_schema"}
		}, true},
		{"image_modality", func(r *Request, _ *CallOptions) { r.Snapshot.Capabilities.InputModalities = []string{"text", "image"} }, true},
		{"custom_header", func(r *Request, _ *CallOptions) {
			r.Snapshot.HeaderOverwrite = map[string]string{"X-Custom": "unsupported"}
		}, true},
		{"media_part", func(r *Request, o *CallOptions) {
			ref, err := oc.NewBusinessFileRef(oc.BusinessFileDetails{Kind: oc.ArtifactFile, ProjectID: o.ProjectID, ArtifactID: fresh[struct{}](t).String(), FileID: fresh[struct{}](t).String()})
			if err != nil {
				t.Fatal(err)
			}
			r.Messages[0].Parts[0] = mc.MessagePart{Image: &mc.MediaPart{Ref: ref, MediaType: "image/png"}}
		}, true},
		{"tool_result", func(r *Request, _ *CallOptions) {
			r.Messages[0] = mc.Message{Role: "tool", Parts: []mc.MessagePart{{ToolResult: &mc.ToolResultPart{CallID: "call1", Parts: []mc.ResultPart{{Text: &mc.TextPart{Text: "value"}}}}}}}
		}, true},
		{"parameters", func(r *Request, _ *CallOptions) { r.Snapshot.Parameters = json.RawMessage(`{"temperature":0}`) }, true},
		{"overwrite", func(r *Request, _ *CallOptions) { r.Snapshot.RequestOverwrite = json.RawMessage(`{"custom":1}`) }, true},
		{"multiple_parts", func(r *Request, _ *CallOptions) {
			r.Messages[0].Parts = append(r.Messages[0].Parts, mc.MessagePart{Text: &mc.TextPart{Text: "other"}})
		}, true},
		{"metadata", func(r *Request, _ *CallOptions) {
			r.Messages[0] = mc.Message{Role: "assistant", Parts: []mc.MessagePart{{Metadata: &mc.ProviderMetadataPart{AdapterID: "a", Version: "v", OpaqueRef: "r"}}}}
		}, true},
		{"no_stream_capability", func(r *Request, _ *CallOptions) { r.Mode = SSEResponse; r.Snapshot.Capabilities.Streaming = false }, true},
		{"invalid_utf8", func(r *Request, _ *CallOptions) { r.Messages[0].Parts[0].Text.Text = string([]byte{255}) }, false},
		{"zero_budget", func(_ *Request, o *CallOptions) { o.Limits.Overall = 0 }, false},
		{"oversized_budget", func(_ *Request, o *CallOptions) { o.Limits.Overall = 121 * time.Second }, false},
		{"idle_budget", func(_ *Request, o *CallOptions) { o.Limits.ReadIdle = 2 * time.Second }, false},
		{"unavailable_material", func(_ *Request, o *CallOptions) { o.Credential = sc.SecretMaterial{} }, false},
		{"query", func(r *Request, _ *CallOptions) { r.Snapshot.Endpoint += "?key=hidden" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, o := unitInput(t)
			tc.mutate(&r, &o)
			req, _, err := prepare(r, o)
			if req != nil {
				t.Fatal("unsupported/invalid request created")
			}
			if tc.unsupported {
				requireModel(t, err, "unsupported_feature", "wire_unsupported_feature")
			} else {
				requireFault(t, err, f.InvalidArgument)
			}
		})
	}
	for _, path := range []string{"/base//inner/", "/base/./inner/", "/base/%2e/", ""} {
		t.Run("path="+path, func(t *testing.T) {
			r, o := unitInput(t)
			r.Snapshot.Endpoint = "https://provider.example" + path
			req, _, err := prepare(r, o)
			if err != nil {
				t.Fatal(err)
			}
			defer req.Body.Close()
			if req.URL.RequestURI() != strings.TrimSuffix(path, "/")+"/chat/completions" {
				t.Fatal(req.URL.RequestURI())
			}
		})
	}
}

type nilResolver chan struct{}

func (nilResolver) Lookup(context.Context, string) ([]netip.Addr, error) {
	panic("nil resolver must not be called")
}
func TestOpenAIChatInvalidConstructorAndSafeViews(t *testing.T) {
	var ch nilResolver
	for _, tc := range []struct {
		transport Transport
		budget    *Budget
	}{{Transport{}, nil}, {Transport{}, &Budget{}}, {Transport{Resolver: ch}, NewBudget()}, {Transport{Policy: &outbound.PolicyService{}}, NewBudget()}} {
		if _, err := NewOpenAIChat(tc.transport, tc.budget); err == nil {
			t.Fatal("invalid constructor accepted")
		}
	}
	r, o := unitInput(t)
	r.Messages[0].Parts[0].Text.Text = "wire-secret-answer-canary"
	for _, value := range []any{r, o, Result{Text: "wire-secret-answer-canary"}, Event{Text: "wire-secret-answer-canary"}, Observation{Decision: &outbound.Decision{Origin: "wire-secret-answer-canary"}}, &Exchange{result: Result{Text: "wire-secret-answer-canary"}}} {
		for _, format := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x"} {
			if strings.Contains(fmt.Sprintf(format, value), "canary") {
				t.Fatal("format leak")
			}
		}
		raw, err := json.Marshal(value)
		if err != nil || strings.Contains(string(raw), "canary") {
			t.Fatal("JSON leak", err)
		}
		var out strings.Builder
		slog.New(slog.NewJSONHandler(&out, nil)).Info("test", "value", value)
		if strings.Contains(out.String(), "canary") {
			t.Fatal("log leak")
		}
	}
}
