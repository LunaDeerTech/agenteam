package app

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

type capability struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type diagnostics struct {
	Ready        bool         `json:"ready"`
	Capabilities []capability `json:"capabilities"`
}

func diagnosticRouter() http.Handler {
	router := httpapi.NewRouter()
	router.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		_ = httpapi.WriteJSON(w, r, http.StatusOK, struct {
			Status string `json:"status"`
		}{"alive"})
	})
	router.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteProblem(w, r, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted))
	})
	router.HandleFunc("GET /diagnostics", func(w http.ResponseWriter, r *http.Request) {
		_ = httpapi.WriteJSON(w, r, http.StatusOK, diagnostics{Ready: false, Capabilities: []capability{
			{"postgresql", "unbound"}, {"pgvector", "unbound"}, {"migrations", "unbound"}, {"secret", "unbound"}, {"object_storage", "unbound"}, {"identity", "unbound"}, {"runner_protocol", "unbound"},
		}})
	})
	return router
}
