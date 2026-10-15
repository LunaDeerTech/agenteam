package mcphttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
)

type oauthCommands interface {
	BeginOAuth(context.Context, mcp.Command, mcp.ConfigID) (mcp.CommandResult, error)
	CompleteOAuth(context.Context, id.Actor, f.IdempotencyKey, mcp.OAuthCallback) (mcp.CommandResult, error)
}

type oauthHTTP struct {
	service  oauthCommands
	boundary projectCredentialBoundary
}

// The callback is a same-origin POST through the original Account boundary.
// Its state locates the original session's flow inside Service; browser fields
// never select the connection, project, credential or redirect authority.
func NewOAuthHTTPHandler(service *mcp.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, fault(f.DependencyUnbound)
	}
	return &oauthHTTP{service: service, boundary: boundary}, nil
}

func oauthRoute(path string) (project string, complete, found bool) {
	if path == "/api/v1/mcp/oauth/complete" {
		return "", true, true
	}
	const prefix, suffix = "/api/v1/projects/", "/mcp/oauth/begin"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false, false
	}
	project = strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return project, false, project != "" && !strings.Contains(project, "/")
}

func HandlesOAuthPath(path string) bool { _, _, ok := oauthRoute(path); return ok }

func (h *oauthHTTP) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(request.Context(), 60*time.Second)
	defer cancel()
	if request.URL != nil {
		_, complete, found := oauthRoute(request.URL.Path)
		if found {
			request.Pattern = "/api/v1/projects/{project_id}/mcp/oauth/begin"
			if complete {
				request.Pattern = "/api/v1/mcp/oauth/complete"
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
		_, _ = w.Write(body)
	}
	if expired(ctx) || budget.controller.Flush() != nil {
		panic(http.ErrAbortHandler)
	}
	normal = true
}

func (h *oauthHTTP) execute(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if err := h.boundary.CheckRequest(w, r); err != nil {
		return nil, err
	}
	project, complete, found := oauthRoute(r.URL.Path)
	if !found {
		return nil, fault(f.NotFound)
	}
	if r.Method != http.MethodPost {
		return nil, fault(f.MethodNotAllowed)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, fault(f.InvalidArgument)
	}
	actor, err := h.boundary.RequireHuman(r)
	if err != nil {
		return nil, err
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human {
		return nil, fault(f.Forbidden)
	}
	key, err := commandKey(r)
	if err != nil {
		return nil, err
	}
	var result mcp.CommandResult
	if complete {
		var input struct {
			State  string `json:"state"`
			Code   string `json:"code"`
			Issuer string `json:"issuer"`
		}
		if err = decode(w, r, &input); err != nil {
			return nil, err
		}
		callback, e := mcp.NewOAuthCallback(input.State, input.Code, input.Issuer)
		if e != nil {
			return nil, e
		}
		result, err = h.service.CompleteOAuth(r.Context(), actor, key, callback)
	} else {
		p, e := f.ParseID[id.Project](project)
		if e != nil {
			return nil, fault(f.InvalidArgument)
		}
		scope, e := id.InProject(p)
		if e != nil {
			return nil, e
		}
		var input struct {
			ConfigID        mcp.ConfigID `json:"config_id"`
			ExpectedVersion f.Progress   `json:"expected_version"`
		}
		if err = decode(w, r, &input); err != nil {
			return nil, err
		}
		c, e := command(actor, scope, "oauth.begin", key, f.Version(input.ExpectedVersion))
		if e != nil {
			return nil, e
		}
		result, err = h.service.BeginOAuth(r.Context(), c, input.ConfigID)
	}
	if err != nil {
		return nil, err
	}
	return encode(projectCommand(result))
}
