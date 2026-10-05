package contract

import (
	"encoding/json"
	"testing"

	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func TestConfigurationCannotHideTransportOrSecrets(t *testing.T) {
	p := ProviderInput{Name: "p", Protocol: OpenAIChat, BaseURL: "https://example.test/v1", Options: json.RawMessage(`{}`)}
	must(t, p.Validate())
	for _, u := range []string{"https://user:password@example.test", "https://example.test/?token=x", "file:///tmp/x", "https://example.test/#x", "https://example.test/?"} {
		v := p
		v.BaseURL = u
		reject(t, v.Validate())
	}
	for _, key := range []string{"Authorization", "Host", "Cookie", "X-Api-Key", "Content-Length", "Connection"} {
		reject(t, overwrite(json.RawMessage(`{}`), map[string]string{key: "secret"}))
	}
	reject(t, overwrite(json.RawMessage(`{}`), map[string]string{"X-Foo": "ok", "x-foo": "changed"}))
	reject(t, overwrite(json.RawMessage(`{"messages":[]}`), nil))
	reject(t, overwrite(json.RawMessage(`{"temperature":1,"temperature":0}`), nil))
	must(t, overwrite(json.RawMessage(`{"temperature":0.2}`), map[string]string{"X-Feature": "v1"}))
}
func TestSelectionIsClosedAndNoSummaryDefault(t *testing.T) {
	x := setup(t)
	r := SelectionRequest{x.c, ptr(x.snapshot.Identity.ModelID), SelectionRef{Kind: "direct"}}
	must(t, r.Validate())
	reject(t, (SelectionResult{}).ValidateFor(r))
	r.Consumer.Kind = KnowledgeConsumer
	r.Consumer.Purpose = KnowledgeEmbedding
	r.Consumer.AgentID = nil
	r.Consumer.ExecutionID = nil
	r.Consumer.OperationID = fresh[struct{}](t).String()
	r.ModelRef = nil
	r.Selection = SelectionRef{Kind: "platform", Selector: EmbeddingSelector}
	must(t, r.Validate())
	reject(t, (SelectionResult{SelectionVersion: ptrVersion(1)}).ValidateFor(r))
	r.Consumer.Purpose = Rerank
	r.Selection.Selector = RerankerSelector
	must(t, (SelectionResult{SelectionVersion: ptrVersion(1)}).ValidateFor(r))
	reject(t, (ProjectModelSettings{ProjectID: x.c.ProjectID, Version: 1}).Validate())
	r.Selection.ProjectID = &x.c.ProjectID
	reject(t, r.Validate())
	pScope, e := id.InProject(x.c.ProjectID)
	must(t, e)
	provider := ProviderView{ID: fresh[Provider](t), Scope: pScope, Input: ProviderInput{Name: "p", Protocol: OpenAIEmbeddings, BaseURL: "https://example.test/v1", Options: json.RawMessage(`{}`)}, Version: 1, CreatedAt: x.now, UpdatedAt: x.now}
	reject(t, provider.Validate())
}

// These are URL syntax checks, not DNS resolution or outbound authorization.
func TestEndpointPortAndFragmentStructure(t *testing.T) {
	x := setup(t)
	cases := []struct {
		endpoint string
		valid    bool
	}{
		{"https://provider.example/v1", true},
		{"https://provider.example:1/v1", true},
		{"https://provider.example:443/v1", true},
		{"https://provider.example:65535/v1", true},
		{"https://provider.example/v1%23literal", true},
		{"https://[2001:db8::1]:443/v1", true},
		{"https://provider.example:0/v1", false},
		{"https://provider.example:65536/v1", false},
		{"https://provider.example:9999999999999999999999/v1", false},
		{"https://provider.example:/v1", false},
		{"https://[2001:db8::1]:/v1", false},
		{"https://provider.example/v1#", false},
		{"https://provider.example/v1#fragment", false},
	}
	for _, tc := range cases {
		t.Run(tc.endpoint, func(t *testing.T) {
			provider := ProviderInput{Name: "provider", Protocol: OpenAIChat, BaseURL: tc.endpoint, Options: json.RawMessage(`{}`)}
			snapshot := x.snapshot.Clone()
			snapshot.Endpoint = tc.endpoint
			for name, err := range map[string]error{"provider": provider.Validate(), "snapshot": snapshot.Validate()} {
				if (err == nil) != tc.valid {
					t.Errorf("%s Validate accepted=%t; want %t", name, err == nil, tc.valid)
				}
			}
		})
	}
}
