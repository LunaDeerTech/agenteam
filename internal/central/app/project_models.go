package app

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
)

// Project reads use the already constructed Model service. The root injects
// its sole Project Authority into that service's original Model Authority.
func projectModelsHandler(models *model.Service, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return model.NewProjectHTTPHandler(models, boundary)
}
func projectModelsRoutes(existing, reads http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if model.HandlesProjectHTTPPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}
