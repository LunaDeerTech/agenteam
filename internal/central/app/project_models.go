package app

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
)

// Project configuration reads and commands share the existing Model service,
// sole Project Authority and browser boundary. No additional root is created.
func projectModelsHandler(models *model.Service, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	reads, err := model.NewProjectHTTPHandler(models, boundary)
	if err != nil {
		return nil, err
	}
	writes, err := model.NewProjectConfigurationHTTPHandler(models, boundary)
	if err != nil {
		return nil, err
	}
	return projectModelMethods(reads, writes), nil
}
func projectModelsRoutes(existing, reads http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if model.HandlesProjectHTTPPath(r.URL.Path) || model.HandlesProjectConfigurationHTTPPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}

// Shared GET/HEAD stay with the original reader; the new handler owns only
// commands, lookup and the complete Allow response for shared resources.
func projectModelMethods(reads, writes http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if model.HandlesProjectConfigurationHTTPPath(r.URL.Path) &&
			(!(r.Method == http.MethodGet || r.Method == http.MethodHead) || !model.HandlesProjectHTTPPath(r.URL.Path)) {
			writes.ServeHTTP(w, r)
			return
		}
		reads.ServeHTTP(w, r)
	})
}
