package app

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	artifacthttp "github.com/LunaDeerTech/agenteam/internal/central/artifact/http"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// The HTTP view consumes the pair already installed after Object.Initialize.
// It never constructs or replaces a domain owner.
func artifactReadHandler(original *artifactCallAssembly, core *account.Service, origin string) (http.Handler, error) {
	if original == nil || original.pair == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	boundary, err := account.NewHTTPBoundary(core, origin)
	if err != nil {
		return nil, err
	}
	return artifacthttp.NewHTTPHandler(original.pair.Service(), boundary)
}

func artifactReadRoutes(existing, reads http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if artifacthttp.HandlesPath(r.URL.Path) {
			reads.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}
