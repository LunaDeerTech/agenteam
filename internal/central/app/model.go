package app

import (
	"net/http"
	"strings"
)

// Only these complete path roots belong to the System Model boundary. Keep
// the request untouched so the existing child boundaries validate its original
// path, Host, Origin, method and body under the one outer HTTP middleware.
func systemModelRoutes(accounts, models http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, root := range [...]string{
			"/api/v1/system/model-providers",
			"/api/v1/system/models",
			"/api/v1/system/model-selection",
			"/api/v1/system/model-commands",
			"/api/v1/system/model-credentials",
			"/api/v1/system/model-credential-commands",
		} {
			if r.URL.Path == root || strings.HasPrefix(r.URL.Path, root+"/") {
				models.ServeHTTP(w, r)
				return
			}
		}
		accounts.ServeHTTP(w, r)
	})
}
