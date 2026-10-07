package model

import (
	"net/http"
	"reflect"
	"sort"
	"strings"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type SystemHTTPOptions struct{ PublicOrigin string }
type systemHTTP struct {
	core     *Service
	boundary *account.HTTPBoundary
	writes   sc.HumanWriteCommands
}
type systemHTTPRoute struct {
	method, path string
	intent       id.AccessIntent
	list         bool
	serve        func(http.ResponseWriter, *http.Request, mc.CommandMeta) (any, error)
}

func httpNil(v any) bool {
	if nilPort(v) {
		return true
	}
	r := reflect.ValueOf(v)
	return r.Kind() == reflect.Chan && r.IsNil()
}

// NewSystemHTTPHandler installs only the System configuration surface. The
// composition root owns initialization and the single shared middleware stack.
func NewSystemHTTPHandler(core *Service, accounts *account.Service, writes sc.HumanWriteCommands, options SystemHTTPOptions) (http.Handler, error) {
	s := core.state()
	if s == nil || httpNil(s.store) || s.authority.state() == nil || httpNil(writes) || httpNil(s.deps.Secret) || httpNil(s.deps.Audit) || httpNil(s.deps.Events) || s.deps.Cursors.Validate() != nil || !s.deps.ConfigurationEvents.valid() {
		return nil, fault(f.DependencyUnbound)
	}
	a := s.authority.state()
	if httpNil(a.store) || !sameStore(s.store, a.store) || httpNil(a.auth.Sessions) || httpNil(a.auth.System) {
		return nil, fault(f.DependencyUnbound)
	}
	b, err := account.NewHTTPBoundary(accounts, options.PublicOrigin)
	if err != nil {
		return nil, err
	}
	h := &systemHTTP{core, b, writes}
	groups := map[string][]systemHTTPRoute{}
	for _, route := range h.routes() {
		groups[route.path] = append(groups[route.path], route)
	}
	mux := http.NewServeMux()
	for path, routes := range groups {
		mux.HandleFunc("/api/v1"+path, func(w http.ResponseWriter, r *http.Request) {
			for _, route := range routes {
				if r.Method == route.method || r.Method == http.MethodHead && route.method == http.MethodGet {
					h.dispatch(w, r, route)
					return
				}
			}
			allowed := make([]string, 0, len(routes)+1)
			for _, route := range routes {
				allowed = append(allowed, route.method)
				if route.method == http.MethodGet {
					allowed = append(allowed, http.MethodHead)
				}
			}
			sort.Strings(allowed)
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			b.WriteProblem(w, r, fault(f.MethodNotAllowed))
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { b.WriteProblem(w, r, fault(f.NotFound)) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := b.CheckRequest(w, r); err != nil {
			b.WriteProblem(w, r, err)
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}

func (h *systemHTTP) routes() []systemHTTPRoute {
	return []systemHTTPRoute{
		{"GET", "/system/model-providers", id.Read, true, h.listProviders},
		{"POST", "/system/model-providers", id.Mutate, false, h.createProvider},
		{"GET", "/system/model-providers/{id}", id.Read, false, h.getProvider},
		{"PUT", "/system/model-providers/{id}", id.Mutate, false, h.updateProvider},
		{"DELETE", "/system/model-providers/{id}", id.Mutate, false, h.deleteProvider},
		{"GET", "/system/models", id.Read, true, h.listModels},
		{"POST", "/system/models", id.Mutate, false, h.createModel},
		{"GET", "/system/models/{id}", id.Read, false, h.getModel},
		{"GET", "/system/models/{id}/deletion-impact", id.Read, false, h.getModelDeletionImpact},
		{"PUT", "/system/models/{id}", id.Mutate, false, h.updateModel},
		{"DELETE", "/system/models/{id}", id.Mutate, false, h.deleteModel},
		{"GET", "/system/model-selection", id.Read, false, h.getSelection},
		{"PUT", "/system/model-selection", id.Mutate, false, h.updateSelection},
		{"GET", meetingSummaryHTTPPath, id.Read, false, nil},
		{"PUT", meetingSummaryHTTPPath, id.Mutate, false, nil},
		{"POST", "/system/model-commands/lookup", id.Read, false, h.lookupCommand},
		{"POST", "/system/model-credentials", id.Mutate, false, h.createCredential},
		{"GET", "/system/model-credentials/{id}", id.Read, false, h.getCredentialMetadata},
		{"PUT", "/system/model-credentials/{id}", id.Mutate, false, h.updateCredential},
		{"DELETE", "/system/model-credentials/{id}", id.Mutate, false, h.deleteCredential},
		{"POST", "/system/model-credential-commands/lookup", id.Read, false, h.lookupCredential},
	}
}

func (h *systemHTTP) dispatch(w http.ResponseWriter, r *http.Request, route systemHTTPRoute) {
	if route.path == meetingSummaryHTTPPath {
		meetingSummaryHTTP{core: h.core, boundary: h.boundary}.serveHTTP(w, r)
		return
	}
	r, cancel := managementReadRequest(r, route)
	defer cancel()
	actor, err := h.boundary.RequireSystem(r, route.intent)
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
		return
	}
	meta := mc.CommandMeta{Actor: actor, Scope: id.SystemScope()}
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete {
		meta.Key, err = httpCommandKey(r)
	}
	if err == nil && !route.list && (r.URL.RawQuery != "" || r.URL.ForceQuery) {
		err = fault(f.InvalidArgument)
	}
	var output any
	if err == nil {
		output, err = route.serve(w, r, meta)
	}
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
		return
	}
	if err := httpapi.WriteJSON(w, r, http.StatusOK, output); err != nil {
		panic(http.ErrAbortHandler)
	}
}
