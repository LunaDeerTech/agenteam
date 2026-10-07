package app

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	projecthttp "github.com/LunaDeerTech/agenteam/internal/central/project/http"
)

// The existing Usage assembly owns the sole Project Authority. This additional
// reader neither constructs a command Service nor adds a startup initializer.
func createProjectRead(cfg config.Config, db database, authority *project.Authority) (*project.Reader, error) {
	store, ok := db.(project.Store)
	if !ok || runtimeInformationNil(store) {
		return nil, foundation.NewFault(foundation.DependencyUnbound, foundation.NotStarted)
	}
	return project.NewReader(store, authority, cfg.CursorKeyring())
}
func projectReadHandler(reader *project.Reader, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return projecthttp.NewHTTPHandler(reader, boundary)
}
func projectReadRoutes(existing, reads http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if projecthttp.HandlesPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}
