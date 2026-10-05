package model

import (
	"bytes"
	"encoding/json"
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func normalizedObject(v json.RawMessage) json.RawMessage {
	if len(v) == 0 {
		return json.RawMessage(`{}`)
	}
	b, e := cursor.CanonicalJSON(v)
	if e != nil {
		return append(json.RawMessage(nil), v...)
	}
	return b
}
func emptyObject(v json.RawMessage) bool { return bytes.Equal(normalizedObject(v), []byte(`{}`)) }
func providerPolicy(v mc.ProviderInput) error {
	return providerPolicyScope(v, id.SystemScope())
}
func providerPolicyScope(v mc.ProviderInput, scope id.Scope) error {
	if e := v.Validate(); e != nil {
		return e
	}
	if v.CredentialRef != nil && !v.CredentialRef.Details().Scope.Equal(scope) {
		return fault(f.Forbidden)
	}
	if scope.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if scope.Details().Kind == id.ProjectScope && !v.Protocol.Supports(mc.ChatModel) {
		return fault(f.InvalidArgument)
	}
	// Native options have no verified profile schema in B01-K.
	if !emptyObject(v.Options) {
		return fault(f.CapabilityUnsupported)
	}
	return nil
}
func modelPolicy(v mc.ModelInput, protocol mc.Protocol) error {
	if e := v.Validate(); e != nil {
		return e
	}
	if !protocol.Supports(v.Type) {
		return fault(f.InvalidArgument)
	}
	if !emptyObject(v.Parameters) || !emptyObject(v.RequestOverwrite) || len(v.HeaderOverwrite) != 0 {
		return fault(f.CapabilityUnsupported)
	}
	c := v.Capabilities
	if len(c.ReasoningEfforts) > 0 {
		return fault(f.CapabilityUnsupported)
	}
	if v.Type != mc.ChatModel && (c.ToolCalls || c.ParallelToolCalls || c.Streaming || c.Reasoning || len(c.StructuredOutputModes) > 0) {
		return fault(f.CapabilityUnsupported)
	}
	for _, m := range c.InputModalities {
		if v.Type != mc.ChatModel && m != "text" {
			return fault(f.CapabilityUnsupported)
		}
	}
	for _, m := range c.OutputModalities {
		valid := v.Type == mc.ChatModel && m == "text" || v.Type == mc.EmbeddingModel && m == "vector" || v.Type == mc.ImageModel && m == "image"
		if !valid {
			return fault(f.CapabilityUnsupported)
		}
	}
	if protocol == mc.AnthropicMessages && slices.Contains(c.StructuredOutputModes, "json_schema") {
		return fault(f.CapabilityUnsupported)
	}
	return nil
}

func providerChanges(a, b providerInput) []string {
	out := []string{}
	if a.Name != b.Name {
		out = append(out, "name")
	}
	if a.Enabled != b.Enabled {
		out = append(out, "enabled")
	}
	if a.BaseURL != b.BaseURL {
		out = append(out, "base_url")
	}
	if a.CredentialID != b.CredentialID {
		out = append(out, "credential_ref")
	}
	if !bytes.Equal(normalizedObject(a.Options), normalizedObject(b.Options)) {
		out = append(out, "provider_options")
	}
	slices.Sort(out)
	return out
}
func modelChanges(a, b mc.ModelInput) []string {
	out := []string{}
	if a.Name != b.Name {
		out = append(out, "name")
	}
	if a.Enabled != b.Enabled {
		out = append(out, "enabled")
	}
	if a.ProviderModelID != b.ProviderModelID {
		out = append(out, "model_id")
	}
	for _, v := range []struct {
		name string
		a, b any
	}{{"parameters", normalizedObject(a.Parameters), normalizedObject(b.Parameters)}, {"request_overwrite", normalizedObject(a.RequestOverwrite), normalizedObject(b.RequestOverwrite)}, {"header_overwrite", a.HeaderOverwrite, b.HeaderOverwrite}, {"capabilities", a.Capabilities, b.Capabilities}} {
		x, _ := json.Marshal(v.a)
		y, _ := json.Marshal(v.b)
		if !bytes.Equal(x, y) {
			out = append(out, v.name)
		}
	}
	slices.Sort(out)
	return out
}
