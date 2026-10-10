package app

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	skillhttp "github.com/LunaDeerTech/agenteam/internal/central/skill/http"
)

func skillManagementHandler(service *skill.Service, keys cursor.Keyring, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return skillhttp.NewManagementHTTPHandler(service, keys, boundary)
}

func skillManagementRoutes(existing, management http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skillhttp.HandlesManagementRequest(r) {
			management.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}
