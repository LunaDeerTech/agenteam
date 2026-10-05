package model

import (
	"encoding/json"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestModelPolicyDoesNotClaimUnverifiedNativeSchemas(t *testing.T) {
	for _, protocol := range []mc.Protocol{mc.OpenAIChat, mc.AnthropicMessages, mc.OpenAIEmbeddings, mc.JinaRerank, mc.OpenAIImages} {
		p := mc.ProviderInput{Name: "metadata", Protocol: protocol, BaseURL: "https://provider.example/api", Options: json.RawMessage(`{}`)}
		if e := providerPolicy(p); e != nil {
			t.Fatal(protocol, e)
		}
		p.Options = json.RawMessage(`{"unverified_native":true}`)
		requireCode(t, providerPolicy(p), f.CapabilityUnsupported)
	}
	m := mc.ModelInput{Name: "metadata", ProviderModelID: "provider-model", Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{StructuredOutputModes: []string{"json_schema"}}}
	if e := modelPolicy(m, mc.OpenAIChat); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*mc.ModelInput){func(v *mc.ModelInput) { v.Parameters = json.RawMessage(`{"temperature":0.5}`) }, func(v *mc.ModelInput) { v.RequestOverwrite = json.RawMessage(`{"temperature":0.5}`) }, func(v *mc.ModelInput) { v.HeaderOverwrite = map[string]string{"X-Feature": "native"} }, func(v *mc.ModelInput) {
		v.Capabilities.Reasoning = true
		v.Capabilities.ReasoningEfforts = []string{"guessed"}
	}} {
		v := m.Clone()
		mutate(&v)
		requireCode(t, modelPolicy(v, mc.OpenAIChat), f.CapabilityUnsupported)
	}
	requireCode(t, modelPolicy(m, mc.OpenAIEmbeddings), f.InvalidArgument)
	m.RequestOverwrite = json.RawMessage(`{"messages":[]}`)
	if e := modelPolicy(m, mc.OpenAIChat); e == nil {
		t.Fatal("request-body overwrite accepted")
	}
}
