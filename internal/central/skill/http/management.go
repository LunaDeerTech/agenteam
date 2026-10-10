package skillhttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
)

const installBudget = 30 * time.Second

type installer interface {
	Install(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error)
	LookupInstall(context.Context, id.Actor, id.ProjectID, f.IdempotencyKey, skill.InstallRequest) (skill.InstallReceipt, error)
}
type catalogReader interface {
	List(context.Context, id.Actor, id.ProjectID, skill.OwnerCatalogQuery) (skill.OwnerCatalogPage, error)
}
type managementHandler struct {
	service  installer
	catalog  catalogReader
	boundary accountBoundary
}

// The adapter keeps the real service, signed catalogue and Account boundary.
// It creates no Actor, installation worker, source authority or second service.
func NewManagementHTTPHandler(service *skill.Service, keys cursor.Keyring, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	catalog, err := skill.NewOwnerCatalog(service, keys)
	if err != nil {
		return nil, err
	}
	return &managementHandler{service, catalog, boundary}, nil
}

type managementRoute struct{ project, kind string }

func parseManagementRoute(path string) managementRoute {
	if !strings.HasPrefix(path, projectPrefix) {
		return managementRoute{}
	}
	parts := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] != "skills" {
		return managementRoute{}
	}
	switch {
	case len(parts) == 2:
		return managementRoute{parts[0], "install"}
	case len(parts) == 3 && parts[2] == "catalog":
		return managementRoute{parts[0], "catalog"}
	case len(parts) == 4 && parts[2] == "commands" && parts[3] == "lookup":
		return managementRoute{parts[0], "lookup"}
	default:
		return managementRoute{}
	}
}

// Collection GET/HEAD keep their original one-builtin handler. Static catalogue
// and lookup paths must dispatch before the original generic detail route.
func HandlesManagementRequest(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	route := parseManagementRoute(r.URL.Path)
	return route.kind == "catalog" || route.kind == "lookup" || route.kind == "install" && r.Method == http.MethodPost
}

func (h *managementHandler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	route := managementRoute{}
	if request.URL != nil {
		route = parseManagementRoute(request.URL.Path)
	}
	budgetDuration := installBudget
	pattern := projectPrefix + "{project_id}/skills"
	switch route.kind {
	case "catalog":
		budgetDuration = readBudget
		pattern += "/catalog"
	case "lookup":
		pattern += "/commands/lookup"
	case "":
		pattern = ""
	}
	request.Pattern = pattern
	ctx, cancel := context.WithTimeout(request.Context(), budgetDuration)
	defer cancel()
	r := request.WithContext(ctx)
	w = abortWriter{w}
	native := &nativeWriter{ResponseWriter: w}
	budget := requestIO{controller: http.NewResponseController(native), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := budget.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !native.prepare() || budget.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, err := h.execute(w, r, route)
	if expired(ctx) || budget.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}
	if expired(ctx) || budget.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *managementHandler) execute(w http.ResponseWriter, r *http.Request, route managementRoute) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	if httpapi.RequestID(r.Context()).Validate() != nil {
		return nil, invalidInput()
	}
	if route.kind == "" {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	allowed := r.Method == http.MethodPost
	methods := "POST"
	if route.kind == "catalog" {
		allowed = r.Method == http.MethodGet || r.Method == http.MethodHead
		methods = "GET, HEAD"
	}
	if !allowed {
		w.Header().Set("Allow", methods)
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[id.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	if route.kind == "catalog" {
		query, err := catalogQuery(r)
		if err != nil {
			return nil, err
		}
		if err = emptyBody(r); err != nil {
			return nil, err
		}
		page, err := h.catalog.List(r.Context(), actor, project, query)
		if err != nil {
			return nil, err
		}
		return encodeCatalog(r.Context(), project, query, page)
	}
	meta, input, err := decodeInstallation(w, r)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	var receipt skill.InstallReceipt
	if route.kind == "lookup" {
		receipt, err = h.service.LookupInstall(r.Context(), actor, project, meta.IdempotencyKey, input)
	} else {
		receipt, err = h.service.Install(r.Context(), actor, meta, project, input)
	}
	if err != nil {
		return nil, err
	}
	return encodeInstallation(r.Context(), project, input, receipt)
}
