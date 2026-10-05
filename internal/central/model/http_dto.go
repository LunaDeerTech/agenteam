package model

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// Presence is explicit: false, an empty object/array, and a nullable reference
// are valid values but cannot silently replace a missing field.
type httpProviderInput struct {
	Name          *string         `json:"name"`
	Protocol      *mc.Protocol    `json:"protocol"`
	BaseURL       *string         `json:"base_url"`
	Enabled       *bool           `json:"enabled"`
	CredentialRef json.RawMessage `json:"credential_ref"`
	Options       json.RawMessage `json:"options"`
}

func (d httpProviderInput) input() (mc.ProviderInput, error) {
	if d.Name == nil || d.Protocol == nil || d.BaseURL == nil || d.Enabled == nil || !httpObject(d.Options) {
		return mc.ProviderInput{}, fault(f.InvalidArgument)
	}
	credential, err := httpNullableID[sc.Credential](d.CredentialRef)
	if err != nil {
		return mc.ProviderInput{}, err
	}
	var ref *sc.CredentialRef
	if credential != nil {
		v, err := sc.NewCredentialRef(*credential, id.SystemScope())
		if err != nil {
			return mc.ProviderInput{}, err
		}
		ref = &v
	}
	return mc.ProviderInput{Name: *d.Name, Protocol: *d.Protocol, BaseURL: *d.BaseURL, Enabled: *d.Enabled, CredentialRef: ref, Options: d.Options}, nil
}

type httpCapabilities struct {
	ToolCalls             *bool           `json:"tool_calls"`
	ParallelToolCalls     *bool           `json:"parallel_tool_calls"`
	Streaming             *bool           `json:"streaming"`
	Reasoning             *bool           `json:"reasoning"`
	InputModalities       []string        `json:"input_modalities"`
	OutputModalities      []string        `json:"output_modalities"`
	ReasoningEfforts      []string        `json:"reasoning_efforts"`
	StructuredOutputModes []string        `json:"structured_output_modes"`
	ContextLength         json.RawMessage `json:"context_length"`
	MaxOutput             json.RawMessage `json:"max_output"`
}

func (d httpCapabilities) input() (mc.Capabilities, error) {
	if d.ToolCalls == nil || d.ParallelToolCalls == nil || d.Streaming == nil || d.Reasoning == nil || d.InputModalities == nil || d.OutputModalities == nil || d.ReasoningEfforts == nil || d.StructuredOutputModes == nil {
		return mc.Capabilities{}, fault(f.InvalidArgument)
	}
	contextLength, err := httpNullableCount(d.ContextLength)
	if err != nil {
		return mc.Capabilities{}, err
	}
	maxOutput, err := httpNullableCount(d.MaxOutput)
	if err != nil {
		return mc.Capabilities{}, err
	}
	return mc.Capabilities{ToolCalls: *d.ToolCalls, ParallelToolCalls: *d.ParallelToolCalls, Streaming: *d.Streaming, Reasoning: *d.Reasoning, InputModalities: d.InputModalities, OutputModalities: d.OutputModalities, ReasoningEfforts: d.ReasoningEfforts, StructuredOutputModes: d.StructuredOutputModes, ContextLength: contextLength, MaxOutput: maxOutput}, nil
}

type httpModelInput struct {
	Name             *string           `json:"name"`
	ProviderModelID  *string           `json:"provider_model_id"`
	Type             *mc.ModelType     `json:"type"`
	Enabled          *bool             `json:"enabled"`
	Parameters       json.RawMessage   `json:"parameters"`
	RequestOverwrite json.RawMessage   `json:"request_overwrite"`
	HeaderOverwrite  map[string]string `json:"header_overwrite"`
	Capabilities     *httpCapabilities `json:"capabilities"`
}

func (d httpModelInput) input() (mc.ModelInput, error) {
	if d.Name == nil || d.ProviderModelID == nil || d.Type == nil || d.Enabled == nil || !httpObject(d.Parameters) || !httpObject(d.RequestOverwrite) || d.HeaderOverwrite == nil || d.Capabilities == nil {
		return mc.ModelInput{}, fault(f.InvalidArgument)
	}
	c, err := d.Capabilities.input()
	if err != nil {
		return mc.ModelInput{}, err
	}
	return mc.ModelInput{Name: *d.Name, ProviderModelID: *d.ProviderModelID, Type: *d.Type, Enabled: *d.Enabled, Parameters: d.Parameters, RequestOverwrite: d.RequestOverwrite, HeaderOverwrite: d.HeaderOverwrite, Capabilities: c}, nil
}

func httpObject(v json.RawMessage) bool {
	v = bytes.TrimSpace(v)
	return len(v) > 1 && v[0] == '{'
}

func httpNullableID[T any](v json.RawMessage) (*f.ID[T], error) {
	if len(v) == 0 {
		return nil, fault(f.InvalidArgument)
	}
	if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		return nil, nil
	}
	var out f.ID[T]
	if json.Unmarshal(v, &out) != nil || out.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	return &out, nil
}

func httpNullableCount(v json.RawMessage) (*mc.TokenCount, error) {
	if len(v) == 0 {
		return nil, fault(f.InvalidArgument)
	}
	if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
		return nil, nil
	}
	var out mc.TokenCount
	if json.Unmarshal(v, &out) != nil || out <= 0 {
		return nil, fault(f.InvalidArgument)
	}
	return &out, nil
}

func httpRawNullable[T any](v *T) json.RawMessage {
	if v == nil {
		return json.RawMessage("null")
	}
	b, _ := json.Marshal(v)
	return b
}

func httpProviderDTO(v mc.ProviderView) any {
	p := v.Input
	var credential *sc.CredentialID
	if p.CredentialRef != nil {
		value := p.CredentialRef.Details().ID
		credential = &value
	}
	return struct {
		ID        mc.ProviderID     `json:"id"`
		Input     httpProviderInput `json:"input"`
		Version   f.Version         `json:"version"`
		CreatedAt f.Instant         `json:"created_at"`
		UpdatedAt f.Instant         `json:"updated_at"`
	}{v.ID, httpProviderInput{&p.Name, &p.Protocol, &p.BaseURL, &p.Enabled, httpRawNullable(credential), p.Options}, v.Version, v.CreatedAt, v.UpdatedAt}
}

func httpModelDTO(v mc.ModelView) any {
	p, c := v.Input, v.Input.Capabilities
	caps := &httpCapabilities{&c.ToolCalls, &c.ParallelToolCalls, &c.Streaming, &c.Reasoning, append([]string{}, c.InputModalities...), append([]string{}, c.OutputModalities...), append([]string{}, c.ReasoningEfforts...), append([]string{}, c.StructuredOutputModes...), httpRawNullable(c.ContextLength), httpRawNullable(c.MaxOutput)}
	headers := p.HeaderOverwrite
	if headers == nil {
		headers = map[string]string{}
	}
	return struct {
		ID         mc.ModelID     `json:"id"`
		ProviderID mc.ProviderID  `json:"provider_id"`
		Input      httpModelInput `json:"input"`
		Version    f.Version      `json:"version"`
		CreatedAt  f.Instant      `json:"created_at"`
		UpdatedAt  f.Instant      `json:"updated_at"`
	}{v.ID, v.ProviderID, httpModelInput{&p.Name, &p.ProviderModelID, &p.Type, &p.Enabled, p.Parameters, p.RequestOverwrite, headers, caps}, v.Version, v.CreatedAt, v.UpdatedAt}
}

type httpConfiguredSelection struct {
	Embedding mc.ModelID  `json:"embedding"`
	Memory    mc.ModelID  `json:"memory"`
	Reranker  *mc.ModelID `json:"reranker"`
	Image     *mc.ModelID `json:"image"`
}

func httpSelectionDTO(v PlatformSelectionState) any {
	var configured *httpConfiguredSelection
	if c := v.Configured; c != nil {
		configured = &httpConfiguredSelection{c.Embedding, c.Memory, c.Reranker, c.Image}
	}
	return struct {
		ID         string                   `json:"id"`
		Version    f.Version                `json:"version"`
		Configured *httpConfiguredSelection `json:"configured"`
	}{v.ID, v.Version, configured}
}

type httpPage struct {
	Items      []any   `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func httpNextCursor(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func httpCommandKey(r *http.Request) (f.IdempotencyKey, error) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) != 1 || f.IdempotencyKey(values[0]).Validate() != nil {
		return "", fault(f.InvalidArgument)
	}
	return f.IdempotencyKey(values[0]), nil
}

func httpListQuery(r *http.Request, models bool) (SystemQuery, mc.ProviderID, error) {
	zero := mc.ProviderID{}
	if r.URL.RawQuery != "" {
		for _, part := range strings.Split(r.URL.RawQuery, "&") {
			if part == "" {
				return SystemQuery{}, zero, fault(f.InvalidArgument)
			}
		}
	}
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || r.URL.ForceQuery && r.URL.RawQuery == "" {
		return SystemQuery{}, zero, fault(f.InvalidArgument)
	}
	for key, values := range q {
		if key != "cursor" && key != "limit" && !(models && key == "provider_id") || len(values) != 1 || values[0] == "" {
			return SystemQuery{}, zero, fault(f.InvalidArgument)
		}
	}
	out := SystemQuery{Cursor: q.Get("cursor"), Limit: 50}
	if raw := q.Get("limit"); raw != "" {
		if len(raw) > 3 || raw[0] == '0' {
			return SystemQuery{}, zero, fault(f.InvalidArgument)
		}
		for _, c := range raw {
			if c < '0' || c > '9' {
				return SystemQuery{}, zero, fault(f.InvalidArgument)
			}
		}
		out.Limit, err = strconv.Atoi(raw)
		if err != nil || out.Limit < 1 || out.Limit > 100 {
			return SystemQuery{}, zero, fault(f.InvalidArgument)
		}
	}
	if models {
		zero, err = f.ParseID[mc.Provider](q.Get("provider_id"))
		if err != nil {
			return SystemQuery{}, mc.ProviderID{}, fault(f.InvalidArgument)
		}
	}
	return out, zero, nil
}
