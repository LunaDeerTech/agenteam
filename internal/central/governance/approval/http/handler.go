// Package approvalhttp exposes the unified current-Owner approval actions.
package approvalhttp

import (
	"context"
	"encoding/json"
	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/governance/approval"
	c "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const prefix = "/api/v1/projects/"

type service interface {
	Get(context.Context, i.Actor, i.ProjectID, c.RequestID) (c.Request, error)
	Decide(context.Context, i.Actor, i.ProjectID, c.RequestID, f.CommandMeta, c.Decision) (c.Receipt, error)
	LookupDecision(context.Context, i.Actor, i.ProjectID, c.RequestID, f.CommandMeta, c.Decision) (c.Lookup, error)
}
type boundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (i.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type handler struct {
	service  service
	boundary boundary
}

func NewHTTPHandler(s *approval.Service, b *account.HTTPBoundary) (http.Handler, error) {
	if s == nil || b == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{s, b}, nil
}

type route struct {
	project, id, action string
	valid               bool
}

func parseRoute(path string) route {
	if !strings.HasPrefix(path, prefix) {
		return route{}
	}
	v := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(v) < 3 || len(v) > 5 || v[0] == "" || v[1] != "approvals" || v[2] == "" {
		return route{}
	}
	r := route{project: v[0], id: v[2], valid: true}
	if len(v) > 3 {
		if v[3] != "decision" {
			return route{}
		}
		r.action = "decision"
	}
	if len(v) == 5 {
		if v[4] != "lookup" {
			return route{}
		}
		r.action = "lookup"
	}
	return r
}
func HandlesPath(path string) bool { return parseRoute(path).valid }
func invalidInput() error          { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func (h *handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	r := request.WithContext(ctx)
	route := route{}
	if r.URL != nil {
		route = parseRoute(r.URL.Path)
	}
	request.Pattern = ""
	if route.valid {
		request.Pattern = prefix + "{project_id}/approvals/{approval_id}"
		if route.action != "" {
			request.Pattern += "/decision"
		}
		if route.action == "lookup" {
			request.Pattern += "/lookup"
		}
	}
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
	if !route.valid {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if httpapi.RequestID(r.Context()).Validate() != nil {
		return nil, invalidInput()
	}
	project, err := f.ParseID[i.Project](route.project)
	if err != nil || project.String() != route.project {
		return nil, invalidInput()
	}
	id, err := f.ParseID[c.RequestIdentity](route.id)
	if err != nil || id.String() != route.id {
		return nil, invalidInput()
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalidInput()
	}
	for _, name := range []string{"Content-Encoding", "Range", "If-Range"} {
		if len(r.Header.Values(name)) != 0 {
			return nil, invalidInput()
		}
	}
	if route.action == "" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
		}
		if err = emptyBody(r); err != nil {
			return nil, err
		}
		v, err := h.service.Get(r.Context(), actor, project, id)
		if err != nil {
			return nil, err
		}
		if v.Validate() != nil || v.ID != id || v.ProjectID != project {
			return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
		}
		return json.Marshal(struct {
			Approval c.Request `json:"approval"`
		}{v})
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	in, meta, err := decodeDecision(r)
	if err != nil {
		return nil, err
	}
	matches := func(v c.Receipt) bool {
		want := c.Approved
		if in.Decision == "reject" {
			want = c.Rejected
		}
		return v.CommandID.Validate() == nil && v.Approval.Validate() == nil && v.Approval.ID == id && v.Approval.ProjectID == project && v.Approval.Status == want && v.Approval.Version == in.ExpectedVersion+1 && v.Approval.ResolvedBy != nil && v.Approval.ResolvedBy.String() == actor.Details().UserID
	}
	if route.action == "lookup" {
		v, err := h.service.LookupDecision(r.Context(), actor, project, id, meta, in)
		if err != nil {
			return nil, err
		}
		if (v.Status == "committed" && (v.Receipt == nil || !matches(*v.Receipt))) || (v.Status == "not_observed" && v.Receipt != nil) || (v.Status != "committed" && v.Status != "not_observed") {
			return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
		}
		return json.Marshal(v)
	}
	v, err := h.service.Decide(r.Context(), actor, project, id, meta, in)
	if err != nil {
		return nil, err
	}
	if !matches(v) {
		panic(http.ErrAbortHandler)
	}
	raw, err := json.Marshal(struct {
		Receipt c.Receipt `json:"receipt"`
	}{v})
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	return raw, nil
}
func decodeDecision(r *http.Request) (c.Decision, f.CommandMeta, error) {
	var in c.Decision
	var meta f.CommandMeta
	if len(r.Header.Values("Idempotency-Key")) != 1 || len(r.Header.Values("Content-Type")) != 1 || r.Body == nil {
		return in, meta, invalidInput()
	}
	key, err := f.ParseIdempotencyKey(r.Header.Get("Idempotency-Key"))
	if err != nil {
		return in, meta, invalidInput()
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || len(params) > 1 || len(params) == 1 && params["charset"] != "utf-8" {
		return in, meta, invalidInput()
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil {
		return in, meta, invalidInput()
	}
	if len(raw) > 1024 {
		return in, meta, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return in, meta, invalidInput()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(canonical, &fields) != nil || len(fields) != 2 || fields["decision"] == nil || fields["expected_version"] == nil || json.Unmarshal(canonical, &in) != nil || in.Validate() != nil {
		return in, meta, invalidInput()
	}
	meta = f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: key, ExpectedVersion: &in.ExpectedVersion}
	if meta.Validate() != nil {
		return in, meta, invalidInput()
	}
	return in, meta, nil
}
