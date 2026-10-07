package app

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

// The root retains the original Secret service and the original Account
// boundary. All Project mutation/read authorization remains inside Secret's
// transactions through the root's sole Project Authority.
func projectCredentialsHandler(secrets *secret.Service, core *account.Service, origin string) (http.Handler, error) {
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return model.NewProjectCredentialHTTPHandler(secrets, boundary)
}
func projectCredentialsRoutes(existing, credentials http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if model.HandlesProjectCredentialHTTPPath(r.URL.Path) {
			credentials.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}
