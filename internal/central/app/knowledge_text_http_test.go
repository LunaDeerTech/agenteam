package app

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKnowledgeTextRootRoutes(t *testing.T) {
	const prefix = "/api/v1/projects/01900000-0000-7000-8000-000000000001/knowledge/documents"
	const document = "/01900000-0000-7000-8000-000000000002"
	for _, suffix := range []string{document + "/create-text", document + "/replace-text", "/text-commands/lookup"} {
		for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodHead} {
			t.Run(method+suffix, func(t *testing.T) {
				var got string
				handler := func(name string) http.Handler {
					return http.HandlerFunc(func(http.ResponseWriter, *http.Request) { got = name })
				}
				routes := knowledgeSkillRoutes(handler("existing"), handler("metadata"), handler("commands"), handler("content"), handler("skills"))
				routes.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, prefix+suffix, nil))
				if got != "commands" {
					t.Fatalf("text method escaped its command boundary: %s", got)
				}
			})
		}
	}
}
