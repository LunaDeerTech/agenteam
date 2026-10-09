// Package commandhttp exposes bounded Human Owner Knowledge tree commands.
// It consumes the existing Service and owns neither its lifetime nor its facts.
package commandhttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

const projectPrefix = "/api/v1/projects/"
const requestBudget = 2 * time.Second

type accountBoundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (id.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type commandService interface {
	UpdateDocument(context.Context, id.Actor, f.CommandMeta, id.ProjectID, kc.DocumentID, kc.UpdateRequest, *kc.SourceInput) (kc.DocumentRef, error)
	MoveDocument(context.Context, id.Actor, f.CommandMeta, id.ProjectID, kc.DocumentID, kc.MoveRequest) (kc.MoveResult, error)
	PrepareDeleteSubtree(context.Context, id.Actor, id.ProjectID, kc.DocumentID) (kc.DeletePreview, error)
	DeleteSubtree(context.Context, id.Actor, f.CommandMeta, id.ProjectID, kc.DocumentID, kc.ConfirmationToken) (kc.DeleteResult, error)
	LookupCommand(context.Context, id.Actor, kc.LookupRequest) (kc.CommandLookup, error)
}
type handler struct {
	service  commandService
	boundary accountBoundary
}

func NewHTTPHandler(service *knowledge.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{service, boundary}, nil
}

type route struct{ project, target, action string }

func routeFor(path string) route {
	if !strings.HasPrefix(path, projectPrefix) {
		return route{}
	}
	p := strings.Split(strings.TrimPrefix(path, projectPrefix), "/")
	if len(p) != 5 || p[1] != "knowledge" || p[2] != "documents" {
		return route{}
	}
	for _, s := range p {
		if s == "" {
			return route{}
		}
	}
	if p[3] == "commands" && p[4] == "lookup" {
		return route{project: p[0], action: "lookup"}
	}
	switch p[4] {
	case "rename", "move", "delete-preview", "delete-subtree":
		return route{p[0], p[3], p[4]}
	}
	return route{}
}
func HandlesPath(path string) bool { return routeFor(path).action != "" }
func (r route) pattern() string {
	if r.action == "" {
		return ""
	}
	suffix := "{document_id}/" + r.action
	if r.action == "lookup" {
		suffix = "commands/lookup"
	}
	return projectPrefix + "{project_id}/knowledge/documents/" + suffix
}

func (h *handler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(request.Context(), requestBudget)
	defer cancel()
	route := route{}
	if request.URL != nil {
		route = routeFor(request.URL.Path)
	}
	request.Pattern = route.pattern()
	r := request.WithContext(ctx)
	w = abortWriter{w}
	native := &nativeWriter{ResponseWriter: w}
	io := requestIO{controller: http.NewResponseController(native), ctx: ctx, body: r.Body}
	normal := false
	defer func() {
		panicked := recover() != nil
		err := io.finish(normal)
		if panicked || err != nil {
			panic(http.ErrAbortHandler)
		}
	}()
	if !native.prepare() || io.start() != nil {
		panic(http.ErrAbortHandler)
	}
	body, err := h.execute(w, r, route)
	if expired(ctx) || io.closeBody() != nil || expired(ctx) {
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
	if expired(ctx) || io.controller.Flush() != nil || expired(ctx) {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *handler) execute(w http.ResponseWriter, r *http.Request, route route) ([]byte, error) {
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
	if route.action == "" {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[id.Project](route.project)
	if err != nil || httpapi.RequestID(r.Context()).Validate() != nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalidInput()
	}
	in, err := decodeIntent(w, r, route)
	if err != nil {
		return nil, err
	}
	if expired(r.Context()) {
		panic(http.ErrAbortHandler)
	}
	if route.action == "delete-preview" {
		out, err := h.service.PrepareDeleteSubtree(r.Context(), actor, project, in.target)
		if err != nil {
			return nil, err
		}
		value, err := previewValue(project, in.target, out)
		if err != nil {
			return nil, err
		}
		return encodeValue(r.Context(), value)
	}
	digest, err := in.digest(actor, project)
	if err != nil {
		return nil, err
	}
	if route.action == "lookup" {
		out, err := h.service.LookupCommand(r.Context(), actor, kc.LookupRequest{ProjectID: project, Command: in.name, Key: in.meta.IdempotencyKey, SemanticDigest: digest})
		if err != nil {
			return nil, err
		}
		value, err := lookupValue(project, in, out)
		if err != nil {
			return nil, err
		}
		return encodeValue(r.Context(), value)
	}
	var value any
	switch in.name {
	case kc.Update:
		var out kc.DocumentRef
		out, err = h.service.UpdateDocument(r.Context(), actor, in.meta, project, in.target, in.update(), nil)
		if err == nil {
			value, err = renameValue(project, in, out)
			if err != nil {
				panic(http.ErrAbortHandler)
			}
		}
	case kc.Move:
		var out kc.MoveResult
		out, err = h.service.MoveDocument(r.Context(), actor, in.meta, project, in.target, in.move)
		if err == nil {
			value, err = moveValue(project, in, out)
			if err != nil {
				panic(http.ErrAbortHandler)
			}
		}
	case kc.DeleteSubtree:
		var out kc.DeleteResult
		out, err = h.service.DeleteSubtree(r.Context(), actor, in.meta, project, in.target, in.token)
		if err == nil {
			value, err = deleteValue(in.target, out)
			if err != nil {
				panic(http.ErrAbortHandler)
			}
		}
	default:
		return nil, invalidInput()
	}
	if err != nil {
		return nil, err
	}
	raw, err := encodeValue(r.Context(), value)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	return raw, nil
}
