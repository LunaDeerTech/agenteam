package app

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

type capability struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
type diagnostics struct {
	Ready         bool                     `json:"ready"`
	Capabilities  []capability             `json:"capabilities"`
	Database      *postgres.DatabaseHealth `json:"database,omitempty"`
	SecurityStage string                   `json:"security_stage"`
	Secret        *secret.Status           `json:"secret,omitempty"`
}

func diagnosticRouter(monitor *healthMonitor, securityInitialized bool, secrets maintenance) http.Handler {
	router := httpapi.NewRouter()
	router.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		_ = httpapi.WriteJSON(w, r, http.StatusOK, struct {
			Status string `json:"status"`
		}{"alive"})
	})
	router.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		code := foundation.DependencyUnbound
		if _, ok := monitor.snapshot(); !ok {
			code = foundation.DependencyUnavailable
		}
		if secrets == nil || !secrets.Status().Available {
			code = foundation.DependencyUnavailable
		}
		httpapi.WriteProblem(w, r, foundation.NewFault(code, foundation.NotStarted))
	})
	router.HandleFunc("GET /diagnostics", func(w http.ResponseWriter, r *http.Request) {
		health, available := monitor.snapshot()
		securityStatus, stage := "unavailable", "unavailable"
		if securityInitialized {
			securityStatus, stage = "available", "initialized"
		}
		auditStatus := "unavailable"
		if securityInitialized && available {
			auditStatus = "available"
		}
		status := "unavailable"
		var database *postgres.DatabaseHealth
		if available {
			status = "available"
			database = &health
		}
		secretStatus := "unavailable"
		var secretState *secret.Status
		if secrets != nil {
			snapshot := secrets.Status()
			snapshot.Available = snapshot.Available && available
			secretState = &snapshot
			if snapshot.Available {
				secretStatus = "available"
			}
		}
		if secretStatus != "available" {
			stage = "unavailable"
		}
		_ = httpapi.WriteJSON(w, r, http.StatusOK, diagnostics{Ready: false, Database: database, SecurityStage: stage, Secret: secretState, Capabilities: []capability{
			{"postgresql", status}, {"pgvector", status}, {"migrations", status}, {"read_write", status}, {"cursor", securityStatus}, {"audit_storage", auditStatus}, {"audit_authorization", "unbound"}, {"secret", secretStatus}, {"secret_authorization", "unbound"}, {"outbound", "unbound"}, {"object_storage", "unbound"}, {"identity", "unbound"}, {"runner_protocol", "unbound"},
		}})
	})
	return router
}
