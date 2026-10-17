package model

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	wire "github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func TestOpenAIProviderFamilyCapturesTwoExistingRuntimeAdapters(t *testing.T) {
	chat := runtimePureRequest(t)
	p := providerRecord{ID: chat.Model.Snapshot.Identity.ProviderID.String(), Input: providerInput{Name: "Shared OpenAI", Protocol: mc.OpenAICompatible, BaseURL: chat.Model.Snapshot.Endpoint, CredentialID: chat.Model.Snapshot.CredentialRef.Details().ID.String(), Enabled: true, Options: json.RawMessage(`{}`)}}
	chatModel := modelRecord{ID: chat.Model.Snapshot.Identity.ModelID.String(), ProviderID: p.ID, Input: mc.ModelInput{Name: "Chat", ProviderModelID: "chat-model", Type: mc.ChatModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: chat.Model.Snapshot.Capabilities.Clone()}}
	embedding := modelRecord{ID: mustID[mc.Model](t).String(), ProviderID: p.ID, Input: mc.ModelInput{Name: "Embedding", ProviderModelID: "embedding-model", Type: mc.EmbeddingModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"vector"}}}}
	for _, m := range []*modelRecord{&chatModel, &embedding} {
		if e := modelPolicy(m.Input, p.Input.Protocol); e != nil {
			t.Fatal("family rejects saved supported model", e)
		}
	}
	chatIdentity := modelWireIdentity(&p, &chatModel)
	var e error
	chatIdentity.AdapterRevision, e = resolutionProfile(&p, &chatModel, mc.SelectionRef{Kind: "direct"})
	if e != nil {
		t.Fatal(e)
	}
	if chatIdentity.Protocol != mc.OpenAIChat || chatIdentity.Profile != mc.OpenAIChatV1 {
		t.Fatal("family did not capture chat adapter")
	}
	chat.Model.Snapshot.Identity = chatIdentity
	if _, e = prepareRuntimeInput(context.Background(), chat, wire.JSONResponse); e != nil {
		t.Fatal("Project consumer cannot use System family chat snapshot", e)
	}
	embedIdentity := modelWireIdentity(&p, &embedding)
	embedIdentity.AdapterRevision, e = resolutionProfile(&p, &embedding, mc.SelectionRef{Kind: "platform", Selector: mc.EmbeddingSelector})
	if e != nil {
		t.Fatal(e)
	}
	if embedIdentity.Protocol != mc.OpenAIEmbeddings || embedIdentity.Profile != mc.OpenAIEmbeddingsV1 || embedIdentity.ProviderID != chatIdentity.ProviderID {
		t.Fatal("family did not retain same provider and concrete embedding adapter")
	}
	c := mc.Consumer{Kind: mc.KnowledgeConsumer, ProjectID: chat.Consumer.ProjectID, OperationID: chat.Consumer.OperationID, Purpose: mc.KnowledgeEmbedding}
	m := chat.Model.Clone()
	m.Consumer = c
	m.Snapshot.Identity = embedIdentity
	m.Snapshot.Capabilities = embedding.Input.Capabilities.Clone()
	request := mc.EmbeddingRequest{NonchatCall: mc.NonchatCall{Actor: chat.Actor, CallID: chat.CallID, Consumer: c, Model: m, Input: chat.Input}, Texts: []string{"owned test"}, ExpectedDimensions: 3}
	request.Input.Digest, e = EmbeddingInputDigest(request.Texts, request.ExpectedDimensions)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = prepareRuntimeEmbedding(context.Background(), request); e != nil {
		t.Fatal("Project consumer cannot use System family embedding snapshot", e)
	}
	if p.Input.CredentialID != m.Snapshot.CredentialRef.Details().ID.String() || m.Snapshot.Endpoint != p.Input.BaseURL {
		t.Fatal("family split credential or endpoint")
	}
	invalid := embedIdentity
	invalid.Protocol = mc.OpenAICompatible
	invalid.Profile = ""
	if invalid.Validate() == nil {
		t.Fatal("unresolved family entered runtime identity")
	}
	if mc.OpenAICompatible.Supports(mc.ImageModel) || mc.OpenAICompatible.Supports(mc.RerankerModel) || mc.OpenAICompatible.EffectiveProtocol(mc.ImageModel) != "" {
		t.Fatal("family silently broadened adapters")
	}
}

func TestOpenAIProviderFamilySystemCreateAndCapabilities(t *testing.T) {
	var dto httpProviderInput
	if e := decodeHTTP(t, `{"name":"shared","protocol":"openai-compatible","base_url":"https://provider.example/v1","enabled":true,"credential_ref":null,"options":{}}`, &dto); e != nil {
		t.Fatal(e)
	}
	input, e := dto.input()
	if e != nil || providerPolicy(input) != nil {
		t.Fatal("System family create rejected", e)
	}
	scope, _ := id.InProject(mustID[id.Project](t))
	if providerPolicyScope(input, scope) == nil {
		t.Fatal("Project family create accidentally opened")
	}
	h := systemHTTP{management: &Management{}}
	output, e := h.modelCapabilities(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/system/model-capabilities", nil), mc.CommandMeta{})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(output)
	if e != nil {
		t.Fatal(e)
	}
	if string(raw) != `{"protocols":[{"protocol":"openai-compatible","model_types":["chat","embedding"],"discovery":true,"test":true}]}` {
		t.Fatal("capabilities do not match shared family", string(raw))
	}
	// Invalid capability combinations remain rejected under the family.
	bad := mc.ModelInput{Name: "bad", ProviderModelID: "bad", Type: mc.EmbeddingModel, Enabled: true, Parameters: json.RawMessage(`{}`), RequestOverwrite: json.RawMessage(`{}`), Capabilities: mc.Capabilities{InputModalities: []string{"text"}, OutputModalities: []string{"text"}}}
	requireCode(t, modelPolicy(bad, mc.OpenAICompatible), f.CapabilityUnsupported)
}
