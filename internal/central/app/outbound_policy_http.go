package app

import (
	"net/http"
	"strings"
)

// Match the complete root and its slash children without normalizing any part
// of the request. The child boundary rejects unknown or ambiguous descendants.
func systemOutboundPolicyRoutes(existing, policy http.Handler) http.Handler {
	const root = "/api/v1/system/outbound-policy"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == root || strings.HasPrefix(r.URL.Path, root+"/") {
			policy.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}
