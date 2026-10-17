package model

import (
	"encoding/json"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	media "github.com/LunaDeerTech/agenteam/internal/central/model/media"
	"net/http"
)

type modelProtocolCapability struct {
	Protocol   string   `json:"protocol"`
	ModelTypes []string `json:"model_types"`
	Discovery  bool     `json:"discovery"`
	Test       bool     `json:"test"`
}

func (h *systemHTTP) modelCapabilities(_ http.ResponseWriter, r *http.Request, _ mc.CommandMeta) (any, error) {
	if e := managementReadInput(r); e != nil {
		return nil, e
	}
	if h.management == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return struct {
		Protocols []modelProtocolCapability `json:"protocols"`
	}{[]modelProtocolCapability{
		{"openai-compatible", []string{"chat", "embedding", "reranker", "image_generation"}, true, true},
	}}, nil
}
func (h *systemHTTP) discoverModels(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	var dto struct {
		Expected f.Version `json:"expected_version"`
	}
	if e := httpapi.DecodeJSON(w, r, &dto, 4096); e != nil {
		return nil, e
	}
	return h.management.Execute(r.Context(), m.Actor, m.Key, "discovery", r.PathValue("id"), dto.Expected, dto.Expected)
}
func (h *systemHTTP) testModel(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	var dto struct {
		Expected f.Version       `json:"expected_version"`
		Provider f.Version       `json:"provider_version"`
		Input    json.RawMessage `json:"input"`
	}
	if e := httpapi.DecodeJSON(w, r, &dto, media.MaxRequestBytes); e != nil {
		return nil, e
	}
	input, err := decodeManagementMedia(dto.Input)
	if err != nil {
		return nil, err
	}
	return h.management.Execute(r.Context(), m.Actor, m.Key, "test", r.PathValue("id"), dto.Expected, dto.Provider, input)
}
