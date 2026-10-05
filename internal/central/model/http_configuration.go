package model

import (
	"encoding/json"
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

func (h *systemHTTP) listProviders(_ http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	q, _, err := httpListQuery(r, false)
	if err != nil {
		return nil, err
	}
	page, err := h.core.ListProviders(r.Context(), m.Actor, q)
	if err != nil {
		return nil, err
	}
	out := httpPage{Items: make([]any, 0, len(page.Items)), NextCursor: httpNextCursor(page.NextCursor)}
	for _, v := range page.Items {
		out.Items = append(out.Items, httpProviderDTO(v))
	}
	return out, nil
}

func (h *systemHTTP) getProvider(_ http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	key, err := f.ParseID[mc.Provider](r.PathValue("id"))
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	v, err := h.core.GetProvider(r.Context(), m.Actor, key)
	if err != nil {
		return nil, err
	}
	return httpProviderDTO(v), nil
}

func (h *systemHTTP) createProvider(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	var dto struct {
		Input *httpProviderInput `json:"input"`
	}
	if err := httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	if dto.Input == nil {
		return nil, fault(f.InvalidArgument)
	}
	input, err := dto.Input.input()
	if err != nil {
		return nil, err
	}
	return h.core.CreateProvider(r.Context(), mc.CreateProviderRequest{CommandMeta: m, Input: input})
}

func (h *systemHTTP) updateProvider(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	key, err := f.ParseID[mc.Provider](r.PathValue("id"))
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	var dto struct {
		Expected f.Version          `json:"expected_version"`
		Input    *httpProviderInput `json:"input"`
	}
	if err = httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	if dto.Input == nil || dto.Expected.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	input, err := dto.Input.input()
	if err != nil {
		return nil, err
	}
	return h.core.UpdateProvider(r.Context(), mc.UpdateProviderRequest{CommandMeta: m, ID: key, ExpectedVersion: dto.Expected, Input: input})
}

func (h *systemHTTP) deleteProvider(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	key, err := f.ParseID[mc.Provider](r.PathValue("id"))
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	var dto struct {
		Expected f.Version `json:"expected_version"`
	}
	if err = httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	return h.core.DeleteProvider(r.Context(), mc.DeleteProviderRequest{CommandMeta: m, ID: key, ExpectedVersion: dto.Expected})
}

func (h *systemHTTP) listModels(_ http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	q, provider, err := httpListQuery(r, true)
	if err != nil {
		return nil, err
	}
	page, err := h.core.ListModels(r.Context(), m.Actor, provider, q)
	if err != nil {
		return nil, err
	}
	out := httpPage{Items: make([]any, 0, len(page.Items)), NextCursor: httpNextCursor(page.NextCursor)}
	for _, v := range page.Items {
		out.Items = append(out.Items, httpModelDTO(v))
	}
	return out, nil
}

func (h *systemHTTP) getModel(_ http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	key, err := f.ParseID[mc.Model](r.PathValue("id"))
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	v, err := h.core.GetModel(r.Context(), m.Actor, key)
	if err != nil {
		return nil, err
	}
	return httpModelDTO(v), nil
}

func (h *systemHTTP) createModel(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	var dto struct {
		ProviderID mc.ProviderID   `json:"provider_id"`
		Input      *httpModelInput `json:"input"`
	}
	if err := httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	if dto.Input == nil || dto.ProviderID.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	input, err := dto.Input.input()
	if err != nil {
		return nil, err
	}
	return h.core.CreateModel(r.Context(), mc.CreateModelRequest{CommandMeta: m, ProviderID: dto.ProviderID, Input: input})
}

func (h *systemHTTP) updateModel(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	key, err := f.ParseID[mc.Model](r.PathValue("id"))
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	var dto struct {
		Expected f.Version       `json:"expected_version"`
		Input    *httpModelInput `json:"input"`
	}
	if err = httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	if dto.Input == nil || dto.Expected.Validate() != nil {
		return nil, fault(f.InvalidArgument)
	}
	input, err := dto.Input.input()
	if err != nil {
		return nil, err
	}
	return h.core.UpdateModel(r.Context(), mc.UpdateModelRequest{CommandMeta: m, ID: key, ExpectedVersion: dto.Expected, Input: input})
}

func (h *systemHTTP) deleteModel(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	key, err := f.ParseID[mc.Model](r.PathValue("id"))
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	var dto struct {
		Expected    f.Version       `json:"expected_version"`
		Replacement json.RawMessage `json:"replacement"`
	}
	if err = httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	replacement, err := httpNullableID[mc.Model](dto.Replacement)
	if err != nil {
		return nil, err
	}
	return h.core.DeleteModel(r.Context(), mc.DeleteModelRequest{CommandMeta: m, ID: key, ExpectedVersion: dto.Expected, Replacement: replacement})
}

func (h *systemHTTP) getSelection(_ http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	v, err := h.core.GetPlatformSelection(r.Context(), m.Actor)
	if err != nil {
		return nil, err
	}
	return httpSelectionDTO(v), nil
}

func (h *systemHTTP) updateSelection(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	var dto struct {
		ID        f.ID[struct{}]  `json:"id"`
		Expected  f.Version       `json:"expected_version"`
		Embedding mc.ModelID      `json:"embedding"`
		Memory    mc.ModelID      `json:"memory"`
		Reranker  json.RawMessage `json:"reranker"`
		Image     json.RawMessage `json:"image"`
	}
	if err := httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	reranker, err := httpNullableID[mc.Model](dto.Reranker)
	if err != nil {
		return nil, err
	}
	image, err := httpNullableID[mc.Model](dto.Image)
	if err != nil {
		return nil, err
	}
	return h.core.UpdatePlatformSelection(r.Context(), mc.UpdatePlatformSelectionRequest{CommandMeta: m, ExpectedVersion: dto.Expected, Selection: mc.PlatformSelection{ID: dto.ID.String(), Version: dto.Expected, Embedding: dto.Embedding, Memory: dto.Memory, Reranker: reranker, Image: image}})
}

func (h *systemHTTP) lookupCommand(w http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	var dto struct {
		Command string `json:"command"`
	}
	if err := httpapi.DecodeJSON(w, r, &dto, 0); err != nil {
		return nil, err
	}
	return h.core.LookupCommand(r.Context(), LookupCommandRequest{Meta: m, Command: dto.Command})
}
