package model

import (
	"context"
	"net/http"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func managementReadRoute(route systemHTTPRoute) bool {
	return route.method == http.MethodGet && (route.path == "/system/model-credentials/{id}" || route.path == "/system/models/{id}/deletion-impact")
}

func managementReadRequest(r *http.Request, route systemHTTPRoute) (*http.Request, context.CancelFunc) {
	if !managementReadRoute(route) {
		return r, func() {}
	}
	ctx, cancel := context.WithTimeout(r.Context(), managementReadBudget)
	return r.WithContext(ctx), cancel
}

func managementReadInput(r *http.Request) error {
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return fault(f.InvalidArgument)
	}
	return nil
}

type httpCredentialMetadata struct {
	ID      sc.CredentialID `json:"credential_id"`
	Purpose sc.Purpose      `json:"purpose"`
	Version f.Version       `json:"version"`
}

func (h *systemHTTP) getCredentialMetadata(_ http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	if err := managementReadInput(r); err != nil {
		return nil, err
	}
	ref, err := httpCredentialRef(r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	metadata, err := h.writes.Metadata(r.Context(), m.Actor, ref)
	if err != nil {
		return nil, err
	}
	if !metadata.CredentialRef.Equal(ref) || !metadata.CredentialRef.Details().Scope.Equal(id.SystemScope()) || metadata.Purpose != sc.Model {
		return nil, fault(f.NotFound)
	}
	if metadata.CredentialRef.Validate() != nil || metadata.Version.Validate() != nil {
		return nil, unavailable(nil)
	}
	return httpCredentialMetadata{metadata.CredentialRef.Details().ID, metadata.Purpose, metadata.Version}, nil
}

func (h *systemHTTP) getModelDeletionImpact(_ http.ResponseWriter, r *http.Request, m mc.CommandMeta) (any, error) {
	if err := managementReadInput(r); err != nil {
		return nil, err
	}
	key, err := f.ParseID[mc.Model](r.PathValue("id"))
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	return h.core.GetModelDeletionImpact(r.Context(), m.Actor, key)
}
