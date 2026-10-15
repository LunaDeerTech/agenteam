// Package mcphttp exposes the Owner MCP control plane through the original
// Account browser boundary. Discovery and authorization stay in mcp.Service.
package mcphttp

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
)

type boundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	RequireSystem(*http.Request, id.AccessIntent) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type handler struct {
	service  *mcp.Service
	boundary boundary
}

func NewHTTPHandler(service *mcp.Service, accounts *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || accounts == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &handler{service, accounts}, nil
}

type route struct {
	system        bool
	project, kind string
	id, action    string
}

func parseRoute(path string) (route, bool) {
	var out route
	var tail string
	if strings.HasPrefix(path, "/api/v1/system/mcp/") {
		out.system, tail = true, strings.TrimPrefix(path, "/api/v1/system/mcp/")
	} else if strings.HasPrefix(path, "/api/v1/projects/") {
		var ok bool
		out.project, tail, ok = strings.Cut(strings.TrimPrefix(path, "/api/v1/projects/"), "/mcp/")
		if !ok || out.project == "" || strings.Contains(out.project, "/") {
			return route{}, false
		}
	} else {
		return route{}, false
	}
	parts := strings.Split(tail, "/")
	if len(parts) > 3 || len(parts) == 0 || parts[0] == "" {
		return route{}, false
	}
	out.kind = parts[0]
	if len(parts) > 1 {
		out.id = parts[1]
		if out.id == "" {
			return route{}, false
		}
	}
	if len(parts) > 2 {
		out.action = parts[2]
		if out.action == "" {
			return route{}, false
		}
	}
	switch out.kind {
	case "configs":
		return out, out.action == ""
	case "commands":
		return out, out.id == "lookup" && out.action == ""
	case "connections":
		if out.system {
			return route{}, false
		}
		switch out.action {
		case "", "resources", "tools", "refresh", "enable", "disable", "disconnect":
			return out, true
		}
	}
	return route{}, false
}

func HandlesPath(path string) bool { _, ok := parseRoute(path); return ok }
func (h *handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	limit := 60 * time.Second
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		limit = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(request.Context(), limit)
	defer cancel()
	if request.URL != nil {
		if route, ok := parseRoute(request.URL.Path); ok {
			prefix := "/api/v1/projects/{project}/mcp/"
			if route.system {
				prefix = "/api/v1/system/mcp/"
			}
			request.Pattern = prefix + route.kind
			if route.id != "" {
				request.Pattern += "/{id}"
			}
			if route.action != "" {
				request.Pattern += "/" + route.action
			}
		}
	}
	r := request.WithContext(ctx)
	w = abortWriter{w}
	budget := requestIO{controller: http.NewResponseController(w), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		if err := budget.finish(normal); panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if budget.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, err := h.execute(w, r)
	if budget.closeBody() != nil || expired(ctx) {
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
	if expired(ctx) || budget.controller.Flush() != nil {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *handler) execute(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	route, ok := parseRoute(r.URL.Path)
	if !ok {
		return nil, fault(f.NotFound)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, fault(f.InvalidArgument)
	}
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	scope := id.SystemScope()
	var err error
	if !route.system {
		project, e := f.ParseID[id.Project](route.project)
		if e != nil {
			return nil, fault(f.InvalidArgument)
		}
		scope, err = id.InProject(project)
		if err != nil {
			return nil, err
		}
	}
	var actor id.Actor
	if route.system {
		intent := id.Mutate
		if read {
			intent = id.Read
		}
		actor, err = h.boundary.RequireSystem(r, intent)
	} else {
		actor, err = h.boundary.RequireHuman(r)
	}
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if read {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			return nil, fault(f.InvalidArgument)
		}
		if r.Body != nil && r.Body != http.NoBody {
			var b [1]byte
			n, e := r.Body.Read(b[:])
			if n != 0 || e != io.EOF {
				return nil, fault(f.InvalidArgument)
			}
		}
		return h.read(r.Context(), actor, scope, route)
	}
	key, err := commandKey(r)
	if err != nil {
		return nil, err
	}
	return h.write(w, r, actor, scope, route, key)
}

func (h *handler) read(ctx context.Context, actor id.Actor, scope id.Scope, r route) ([]byte, error) {
	if r.kind == "configs" {
		if r.id == "" {
			values, err := h.service.ListConfigs(ctx, actor, scope)
			if err != nil {
				return nil, err
			}
			if values == nil {
				values = []mcp.Config{}
			}
			return encode(struct {
				Items []mcp.Config `json:"items"`
			}{values})
		}
		config, err := f.ParseID[mcp.ConfigIdentity](r.id)
		if err != nil {
			return nil, fault(f.InvalidArgument)
		}
		value, err := h.service.GetConfig(ctx, actor, scope, config)
		if err != nil {
			return nil, err
		}
		return encode(value)
	}
	if r.kind != "connections" {
		return nil, fault(f.MethodNotAllowed)
	}
	project, _ := f.ParseID[id.Project](r.project)
	if r.id == "" {
		values, err := h.service.ListConnections(ctx, actor, project)
		if err != nil {
			return nil, err
		}
		items := make([]connectionWire, 0, len(values))
		for _, value := range values {
			items = append(items, projectConnection(value))
		}
		return encode(struct {
			Items []connectionWire `json:"items"`
		}{items})
	}
	connection, err := f.ParseID[mcp.ConnectionIdentity](r.id)
	if err != nil {
		return nil, fault(f.InvalidArgument)
	}
	if r.action == "resources" {
		values, err := h.service.ListResources(ctx, actor, project, connection)
		if err != nil {
			return nil, err
		}
		items := make([]resourceWire, 0, len(values))
		for _, value := range values {
			items = append(items, projectResource(value))
		}
		return encode(struct {
			Items []resourceWire `json:"items"`
		}{items})
	}
	if r.action == "tools" {
		values, err := h.service.ListTools(ctx, actor, project, connection)
		if err != nil {
			return nil, err
		}
		items := make([]toolWire, 0, len(values))
		for _, value := range values {
			items = append(items, projectTool(value))
		}
		return encode(struct {
			Items []toolWire `json:"items"`
		}{items})
	}
	if r.action != "" {
		return nil, fault(f.MethodNotAllowed)
	}
	value, err := h.service.GetConnection(ctx, actor, project, connection)
	if err != nil {
		return nil, err
	}
	return encode(projectConnection(value))
}
