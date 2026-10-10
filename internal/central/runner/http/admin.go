// Package runnerhttp supplies the bounded Runner HTTP boundary. Current
// authority and command recovery are always checked again by the service.
package runnerhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/runner/service"
)

const adminPrefix = "/api/v1/system/runners"

type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireSystem(*http.Request, id.AccessIntent) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type adminHandler struct {
	reader   c.Reader
	commands c.Commands
	boundary accountBoundary
}

func NewAdminHandler(s *service.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if s == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &adminHandler{reader: s, commands: s, boundary: boundary}, nil
}
func HandlesAdminPath(path string) bool {
	return path == adminPrefix || strings.HasPrefix(path, adminPrefix+"/")
}

type route struct {
	kind   string
	target c.RunnerID
}

func adminRoute(path string) route {
	if path == adminPrefix {
		return route{kind: "collection"}
	}
	if !strings.HasPrefix(path, adminPrefix+"/") {
		return route{}
	}
	parts := strings.Split(strings.TrimPrefix(path, adminPrefix+"/"), "/")
	if len(parts) < 1 || len(parts) > 3 {
		return route{}
	}
	target, e := f.ParseID[c.Runner](parts[0])
	if e != nil {
		return route{}
	}
	if len(parts) == 1 {
		return route{"runner", target}
	}
	if len(parts) == 2 && (parts[1] == "enrollment" || parts[1] == "revoke") {
		return route{parts[1], target}
	}
	if len(parts) == 3 && parts[1] == "commands" && parts[2] == "lookup" {
		return route{"lookup", target}
	}
	return route{}
}
func (v route) allow() string {
	switch v.kind {
	case "collection":
		return "GET, HEAD, POST"
	case "runner":
		return "GET, HEAD, PATCH"
	case "enrollment", "revoke", "lookup":
		return "POST"
	}
	return ""
}
func (v route) pattern() string {
	switch v.kind {
	case "collection":
		return adminPrefix
	case "runner":
		return adminPrefix + "/{runner_id}"
	case "enrollment", "revoke":
		return adminPrefix + "/{runner_id}/" + v.kind
	case "lookup":
		return adminPrefix + "/{runner_id}/commands/lookup"
	}
	return ""
}
func invalidInput() error { return f.NewFault(f.InvalidArgument, f.NotStarted) }
func (h *adminHandler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	route := route{}
	if request.URL != nil {
		route = adminRoute(request.URL.Path)
	}
	request.Pattern = route.pattern()
	budget := 30 * time.Second
	if request.Method == http.MethodGet || request.Method == http.MethodHead || route.kind == "lookup" || route.kind == "" {
		budget = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(request.Context(), budget)
	defer cancel()
	r := request.WithContext(ctx)
	w = abortWriter{w}
	native := &nativeWriter{ResponseWriter: w}
	owned := requestIO{controller: http.NewResponseController(native), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		e := owned.finish(normal)
		if panicked || e != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !native.prepare() || owned.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, e := h.execute(w, r, route)
	if expired(ctx) || owned.closeBody() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	if e != nil {
		h.boundary.WriteProblem(w, r, e)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}
	if expired(ctx) || owned.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}
func (h *adminHandler) execute(w http.ResponseWriter, r *http.Request, route route) ([]byte, error) {
	if e := h.boundary.CheckRequest(w, r); e != nil {
		return nil, e
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	intent := id.Mutate
	if r.Method == http.MethodGet || r.Method == http.MethodHead || route.kind == "lookup" {
		intent = id.Read
	}
	actor, e := h.boundary.RequireSystem(r, intent)
	if e != nil {
		return nil, e
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
	if !slices.Contains(strings.Split(route.allow(), ", "), r.Method) {
		w.Header().Set("Allow", route.allow())
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return h.read(r, actor, route)
	}
	return h.mutate(w, r, actor, route)
}
func (h *adminHandler) read(r *http.Request, a id.Actor, route route) ([]byte, error) {
	if emptyBody(r) != nil || r.URL.ForceQuery {
		return nil, invalidInput()
	}
	if route.kind == "runner" {
		if r.URL.RawQuery != "" {
			return nil, invalidInput()
		}
		v, e := h.reader.Get(r.Context(), a, route.target)
		if e != nil {
			return nil, e
		}
		return readProjection(v, c.MaxRecordBytes)
	}
	if route.kind != "collection" {
		return nil, invalidInput()
	}
	values, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil {
		return nil, invalidInput()
	}
	q := c.ListRequest{Limit: 50}
	for key, value := range values {
		if len(value) != 1 {
			return nil, invalidInput()
		}
		switch key {
		case "limit":
			v, e := strconv.Atoi(value[0])
			if e != nil || strconv.Itoa(v) != value[0] || v < 1 || v > 200 {
				return nil, invalidInput()
			}
			q.Limit = v
		case "after_id":
			v, e := f.ParseID[c.Runner](value[0])
			if e != nil {
				return nil, invalidInput()
			}
			q.After = &v
		default:
			return nil, invalidInput()
		}
	}
	v, e := h.reader.List(r.Context(), a, q)
	if e != nil {
		return nil, e
	}
	return readProjection(v, c.MaxPageBytes)
}
func commandKey(r *http.Request) (f.IdempotencyKey, error) {
	var values []string
	for name, v := range r.Header {
		if strings.EqualFold(name, "Idempotency-Key") {
			values = append(values, v...)
		}
	}
	if len(values) != 1 || f.IdempotencyKey(values[0]).Validate() != nil {
		return "", invalidInput()
	}
	return f.IdempotencyKey(values[0]), nil
}
func decodeBody[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var v T
	for name := range r.Header {
		if strings.EqualFold(name, "Content-Encoding") {
			return v, f.NewFault(f.UnsupportedMediaType, f.NotStarted)
		}
	}
	if e := httpapi.DecodeJSON(w, r, &v, c.MaxRequestBytes); e != nil {
		return v, e
	}
	return v, nil
}
func (h *adminHandler) mutate(w http.ResponseWriter, r *http.Request, a id.Actor, route route) ([]byte, error) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalidInput()
	}
	key, e := commandKey(r)
	if e != nil {
		return nil, e
	}
	var in c.Intent
	switch route.kind {
	case "collection":
		v, e := decodeBody[c.CreateRequest](w, r)
		if e != nil {
			return nil, e
		}
		in, e = c.NewCreate(v)
		if e != nil {
			return nil, e
		}
	case "runner":
		v, e := decodeBody[c.UpdateRequest](w, r)
		if e != nil {
			return nil, e
		}
		in, e = c.NewUpdate(route.target, v)
		if e != nil {
			return nil, e
		}
	case "enrollment", "revoke":
		v, e := decodeBody[c.CredentialRequest](w, r)
		if e != nil {
			return nil, e
		}
		if route.kind == "revoke" {
			in, e = c.NewRevoke(route.target, v)
		} else {
			in, e = c.NewEnrollment(route.target, v)
		}
		if e != nil {
			return nil, e
		}
	case "lookup":
		v, e := decodeBody[lookupWire](w, r)
		if e != nil {
			return nil, e
		}
		in, e = c.DecodeIntent(v.Command, route.target, v.Request)
		if e != nil {
			return nil, e
		}
		if expired(r.Context()) {
			panic(http.ErrAbortHandler)
		}
		out, e := h.commands.Lookup(r.Context(), a, key, in)
		if e != nil {
			return nil, e
		}
		state := "not_observed"
		if out.Receipt != nil {
			if out.Receipt.Validate() != nil || out.Receipt.Command != in.Command() || out.Receipt.Runner.ID != in.Target() {
				return nil, f.NewFault(f.InternalError, f.NotStarted)
			}
			state = "committed"
		}
		return readProjection(struct {
			State          string     `json:"state"`
			Receipt        *c.Receipt `json:"receipt,omitempty"`
			TokenAvailable bool       `json:"token_available"`
		}{state, out.Receipt, false}, c.MaxRecordBytes)
	default:
		return nil, invalidInput()
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	out, e := h.commands.Execute(r.Context(), a, key, in)
	if e != nil {
		return nil, e
	}
	// Execution success followed by a bad projection is an aborted response; it
	// must never be converted into a false not_committed Problem.
	if out.Receipt.Validate() != nil || out.Receipt.Command != in.Command() || out.Receipt.Runner.ID != in.Target() {
		panic(http.ErrAbortHandler)
	}
	var token *string
	var expires *f.Instant
	if out.Material != nil {
		if in.Command() != c.Create && in.Command() != c.IssueEnrollment || out.Material.ExpiresAt.Validate() != nil {
			panic(http.ErrAbortHandler)
		}
		v, e := out.Material.Token.Wire()
		if e != nil {
			panic(http.ErrAbortHandler)
		}
		token = &v
		at := out.Material.ExpiresAt
		expires = &at
	}
	body, e := json.Marshal(struct {
		Receipt        c.Receipt  `json:"receipt"`
		TokenAvailable bool       `json:"token_available"`
		Token          *string    `json:"enrollment_token,omitempty"`
		Expires        *f.Instant `json:"expires_at,omitempty"`
	}{out.Receipt, token != nil, token, expires})
	if e != nil || len(body) > c.MaxRecordBytes {
		panic(http.ErrAbortHandler)
	}
	return body, nil
}

type lookupWire struct {
	Command c.CommandName   `json:"command"`
	Request json.RawMessage `json:"request"`
}

func readProjection(v any, cap int) ([]byte, error) {
	b, e := json.Marshal(v)
	if e != nil || len(b) > cap {
		return nil, f.NewFault(f.InternalError, f.NotStarted)
	}
	return b, nil
}
