// Package memoryhttp exposes bounded Human Owner Memory management. Agent
// recall/retain/reflect enter through the original Unified Tool Runtime.
package memoryhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/memory"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
)

type boundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (i.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type service interface {
	List(context.Context, i.Actor, c.Namespace, memory.ListQuery) (memory.Page, error)
	History(context.Context, i.Actor, c.Namespace, c.ID, string, int) (memory.HistoryPage, error)
	Delete(context.Context, i.Actor, c.Namespace, c.ID, f.CommandMeta) (memory.DeleteReceipt, error)
	LookupDelete(context.Context, i.Actor, c.Namespace, c.ID, f.CommandMeta) (memory.DeleteReceipt, bool, error)
}
type handler struct {
	service  service
	boundary boundary
}

func NewHTTPHandler(s *memory.Service, b *account.HTTPBoundary) (http.Handler, error) {
	if s == nil || b == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{s, b}, nil
}

type route struct{ project, agent, memory, action string }

func routeFor(path string) route {
	if !strings.HasPrefix(path, "/api/v1/projects/") {
		return route{}
	}
	p := strings.Split(strings.TrimPrefix(path, "/api/v1/projects/"), "/")
	if len(p) != 4 && len(p) != 6 {
		return route{}
	}
	if p[1] != "agents" || p[3] != "memory" {
		return route{}
	}
	for _, v := range p {
		if v == "" {
			return route{}
		}
	}
	if len(p) == 4 {
		return route{project: p[0], agent: p[2], action: "list"}
	}
	if p[5] != "revisions" && p[5] != "delete" && p[5] != "delete-lookup" {
		return route{}
	}
	return route{p[0], p[2], p[4], p[5]}
}
func HandlesPath(path string) bool { return routeFor(path).action != "" }
func (v route) pattern() string {
	if v.action == "" {
		return ""
	}
	path := "/api/v1/projects/{project_id}/agents/{agent_id}/memory"
	if v.action != "list" {
		path += "/{memory_id}/" + v.action
	}
	return path
}
func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	route := route{}
	if r.URL != nil {
		route = routeFor(r.URL.Path)
	}
	r.Pattern = route.pattern()
	r = r.WithContext(ctx)
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
	body, err := h.execute(w, r, route)
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
func invalid() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func (h *handler) execute(w http.ResponseWriter, r *http.Request, route route) ([]byte, error) {
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
	if route.action == "" {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	method := http.MethodGet
	if route.action == "delete" || route.action == "delete-lookup" {
		method = http.MethodPost
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[i.Project](route.project)
	if err != nil {
		return nil, invalid()
	}
	agent, err := f.ParseID[i.Agent](route.agent)
	if err != nil {
		return nil, invalid()
	}
	n := c.Namespace{ProjectID: project, AgentID: agent}
	var value any
	if method == http.MethodGet {
		if r.ContentLength > 0 {
			return nil, invalid()
		}
		query, e := readQuery(r, route.action)
		if e != nil {
			return nil, e
		}
		if route.action == "list" {
			value, err = h.service.List(r.Context(), actor, n, query)
		} else {
			id, e := f.ParseID[c.Memory](route.memory)
			if e != nil {
				return nil, invalid()
			}
			value, err = h.service.History(r.Context(), actor, n, id, query.Before, query.Limit)
		}
	} else {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			return nil, invalid()
		}
		id, e := f.ParseID[c.Memory](route.memory)
		if e != nil {
			return nil, invalid()
		}
		var body struct {
			Expected f.Version `json:"expected_revision"`
		}
		if e := httpapi.DecodeJSON(w, r, &body, 2048); e != nil {
			return nil, e
		}
		if body.Expected.Validate() != nil {
			return nil, invalid()
		}
		keys := headerValues(r, "Idempotency-Key")
		if len(keys) != 1 {
			return nil, invalid()
		}
		meta := f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0]), ExpectedVersion: &body.Expected}
		if meta.Validate() != nil {
			return nil, invalid()
		}
		if route.action == "delete" {
			value, err = h.service.Delete(r.Context(), actor, n, id, meta)
		} else {
			receipt, found, e := h.service.LookupDelete(r.Context(), actor, n, id, meta)
			err = e
			if found {
				value = struct {
					Status  string               `json:"status"`
					Receipt memory.DeleteReceipt `json:"receipt"`
				}{"committed", receipt}
			} else {
				value = struct {
					Status string `json:"status"`
				}{"not_found"}
			}
		}
	}
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 2<<20 {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return raw, nil
}
func headerValues(r *http.Request, name string) []string {
	var out []string
	for key, values := range r.Header {
		if strings.EqualFold(key, name) {
			out = append(out, values...)
		}
	}
	return out
}
func readQuery(r *http.Request, action string) (memory.ListQuery, error) {
	q := memory.ListQuery{Limit: 50}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return q, invalid()
	}
	for key, vs := range values {
		if len(vs) != 1 {
			return q, invalid()
		}
		switch key {
		case "limit":
			v, e := strconv.Atoi(vs[0])
			if e != nil || v < 1 || v > 100 || strconv.Itoa(v) != vs[0] {
				return q, invalid()
			}
			q.Limit = v
		case "cursor":
			q.Before = vs[0]
		case "type":
			if action != "list" {
				return q, invalid()
			}
			q.Filters.Types = []c.Type{c.Type(vs[0])}
		case "tag":
			if action != "list" {
				return q, invalid()
			}
			q.Filters.Tags = []string{vs[0]}
		default:
			return q, invalid()
		}
	}
	if q.Filters.Validate() != nil {
		return q, invalid()
	}
	return q, nil
}
