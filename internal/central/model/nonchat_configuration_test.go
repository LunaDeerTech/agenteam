package model

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestNonchatConfigurationUsesOneCompatibleProvider(t *testing.T) {
	protocols := map[mc.ModelType]mc.Protocol{mc.ChatModel: mc.OpenAIChat, mc.EmbeddingModel: mc.OpenAIEmbeddings, mc.RerankerModel: mc.JinaRerank, mc.ImageModel: mc.OpenAIImages}
	for typ, protocol := range protocols {
		if !mc.OpenAICompatible.Supports(typ) || mc.OpenAICompatible.EffectiveProtocol(typ) != protocol || protocol.Profile() == "" {
			t.Fatal("type lost its concrete protocol", typ)
		}
	}
	if mc.OpenAICompatible.Supports("unknown") || mc.OpenAICompatible.EffectiveProtocol("unknown") != "" || mc.OpenAICompatible.Profile() != "" {
		t.Fatal("gateway family became a wire adapter")
	}
	h := &systemHTTP{management: &Management{}}
	result, err := h.modelCapabilities(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "https://example.test/api/v1/system/model-capabilities", nil), mc.CommandMeta{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	var got struct {
		Protocols []modelProtocolCapability `json:"protocols"`
	}
	if json.Unmarshal(raw, &got) != nil || len(got.Protocols) != 1 || got.Protocols[0].Protocol != string(mc.OpenAICompatible) || !slices.Equal(got.Protocols[0].ModelTypes, []string{"chat", "embedding", "reranker", "image_generation"}) || !got.Protocols[0].Test || !got.Protocols[0].Discovery {
		t.Fatal("four-type management projection")
	}
}

func TestNonchatModelPolicyAndPlatformSelection(t *testing.T) {
	for _, typ := range []mc.ModelType{mc.RerankerModel, mc.ImageModel} {
		t.Run(string(typ), func(t *testing.T) {
			caps := mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{}}
			role := "reranker"
			if typ == mc.ImageModel {
				caps.OutputModalities, role = []string{"image"}, "image"
			}
			input := mc.ModelInput{Name: "owned", ProviderModelID: "owned/model", Type: typ, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: caps}
			if err := modelPolicy(input, mc.OpenAICompatible); err != nil {
				t.Fatal(err)
			}
			if err := selectionCompatible(role, input, ""); err != nil {
				t.Fatal("platform selector", err)
			}
			other := "image"
			if role == "image" {
				other = "reranker"
			}
			requireCode(t, selectionCompatible(other, input, ""), f.InvalidArgument)
			for name, mutate := range map[string]func(*mc.ModelInput){
				"media-input":   func(v *mc.ModelInput) { v.Capabilities.InputModalities = []string{"text", "image"} },
				"vector-output": func(v *mc.ModelInput) { v.Capabilities.OutputModalities = []string{"vector"} },
				"text-output":   func(v *mc.ModelInput) { v.Capabilities.OutputModalities = []string{"text"} },
				"tools":         func(v *mc.ModelInput) { v.Capabilities.ToolCalls = true },
				"streaming":     func(v *mc.ModelInput) { v.Capabilities.Streaming = true },
				"reasoning":     func(v *mc.ModelInput) { v.Capabilities.Reasoning = true },
				"schema":        func(v *mc.ModelInput) { v.Capabilities.StructuredOutputModes = []string{"json_schema"} },
				"context":       func(v *mc.ModelInput) { n := mc.TokenCount(1024); v.Capabilities.ContextLength = &n },
				"max-output":    func(v *mc.ModelInput) { n := mc.TokenCount(1024); v.Capabilities.MaxOutput = &n },
				"parameters":    func(v *mc.ModelInput) { v.Parameters = json.RawMessage(`{"size":"1024x1024"}`) },
			} {
				t.Run(name, func(t *testing.T) {
					v := input.Clone()
					mutate(&v)
					requireCode(t, modelPolicy(v, mc.OpenAICompatible), f.CapabilityUnsupported)
				})
			}
			input.Enabled = false
			requireCode(t, selectionCompatible(role, input, ""), f.InvalidArgument)
		})
	}
}
