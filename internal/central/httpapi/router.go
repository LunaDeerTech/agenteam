package httpapi

import (
	"net/http"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

// Router retains ServeMux's pattern matching, path values, HEAD fallback and
// canonical redirects, while translating its default 404/405 into Problems.
// Register all routes before serving requests.
type Router struct{ mux *http.ServeMux }

func NewRouter() *Router                                      { return &Router{mux: http.NewServeMux()} }
func (m *Router) Handle(pattern string, handler http.Handler) { m.mux.Handle(pattern, handler) }
func (m *Router) HandleFunc(pattern string, handler http.HandlerFunc) {
	m.mux.HandleFunc(pattern, handler)
}

func (m *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	handler, pattern := m.mux.Handler(r)
	if pattern != "" {
		m.mux.ServeHTTP(w, r)
		return
	}
	// An empty pattern identifies a ServeMux-generated rejection, never a
	// registered application handler. Capture only its status and Allow header.
	rejection := &routeRejection{header: make(http.Header)}
	handler.ServeHTTP(rejection, r)
	code := foundation.NotFound
	if rejection.status == http.StatusMethodNotAllowed {
		code = foundation.MethodNotAllowed
		w.Header().Set("Allow", rejection.header.Get("Allow"))
	}
	WriteProblem(w, r, foundation.NewFault(code, foundation.NotStarted))
}

type routeRejection struct {
	header http.Header
	status int
}

func (w *routeRejection) Header() http.Header { return w.header }
func (w *routeRejection) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *routeRejection) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(b), nil
}
