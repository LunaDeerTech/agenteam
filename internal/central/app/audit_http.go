package app

import (
	"net/http"
	"strings"
)

// Preserve every original request field. The new boundary owns the complete
// Audit path root and rejects any unknown or ambiguous descendants itself.
func systemAuditRoutes(existing, auditor http.Handler) http.Handler {
	const root = "/api/v1/system/audit"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == root || strings.HasPrefix(r.URL.Path, root+"/") {
			auditor.ServeHTTP(w, r)
			return
		}
		existing.ServeHTTP(w, r)
	})
}
