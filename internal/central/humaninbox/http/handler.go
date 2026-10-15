package humaninboxhttp

import (
	"context"
	"encoding/json"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/humaninbox"
	c "github.com/LunaDeerTech/agenteam/internal/central/humaninbox/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type boundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (i.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type service interface {
	List(context.Context, i.Actor, c.ListRequest) (c.Page, error)
	Rebuild(context.Context, i.Actor, i.ProjectID) error
}
type handler struct {
	service  service
	boundary boundary
}

func NewHTTPHandler(s *humaninbox.Service, b *account.HTTPBoundary) (http.Handler, error) {
	if s == nil || b == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{s, b}, nil
}
func routeFor(path string) (string, string) {
	if !strings.HasPrefix(path, "/api/v1/projects/") {
		return "", ""
	}
	p := strings.Split(strings.TrimPrefix(path, "/api/v1/projects/"), "/")
	if len(p) == 2 && p[0] != "" && p[1] == "inbox" {
		return p[0], "list"
	}
	if len(p) == 3 && p[0] != "" && p[1] == "inbox" && p[2] == "rebuild" {
		return p[0], "rebuild"
	}
	return "", ""
}
func HandlesPath(path string) bool { _, action := routeFor(path); return action != "" }
func invalid() error               { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	project, action := "", ""
	if r.URL != nil {
		project, action = routeFor(r.URL.Path)
	}
	r.Pattern = "/api/v1/projects/{project_id}/inbox"
	if action == "rebuild" {
		r.Pattern += "/rebuild"
	}
	w = abortWriter{w}
	native := &nativeWriter{ResponseWriter: w}
	request := requestIO{controller: http.NewResponseController(native), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := request.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !native.prepare() || request.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, err := h.execute(w, r, project, action)
	if expired(ctx) || request.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if err != nil {
		h.boundary.WriteProblem(w, r, err)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
	if expired(ctx) || request.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}
func (h *handler) execute(w http.ResponseWriter, r *http.Request, project, action string) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	if actor.Validate() != nil || actor.Details().Kind != i.Human {
		return nil, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	if action == "" {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	method := http.MethodGet
	if action == "rebuild" {
		method = http.MethodPost
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	p, err := f.ParseID[i.Project](project)
	if err != nil {
		return nil, invalid()
	}
	if action == "rebuild" {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			return nil, invalid()
		}
		var body struct{}
		if err = httpapi.DecodeJSON(w, r, &body, 1024); err != nil {
			return nil, err
		}
		if err = h.service.Rebuild(r.Context(), actor, p); err != nil {
			return nil, err
		}
		return []byte(`{"rebuilt":true}`), nil
	}
	if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		return nil, invalid()
	}
	query := r.URL.Query()
	for k, v := range query {
		if len(v) != 1 || k != "status" && k != "limit" && k != "before" {
			return nil, invalid()
		}
	}
	in := c.ListRequest{ProjectID: p, Status: query.Get("status")}
	if v := query.Get("limit"); v != "" {
		in.Limit, err = strconv.Atoi(v)
		if err != nil {
			return nil, invalid()
		}
	}
	if v := query.Get("before"); v != "" {
		id, e := f.ParseID[c.ItemIdentity](v)
		if e != nil {
			return nil, invalid()
		}
		in.Before = &id
	}
	out, err := h.service.List(r.Context(), actor, in)
	if err != nil {
		return nil, err
	}
	return json.Marshal(out)
}
