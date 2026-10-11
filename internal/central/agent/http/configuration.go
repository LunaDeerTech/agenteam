package agenthttp

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	c "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const configurationMutationBudget = 30 * time.Second

// The public constructor requires the real Service. The private port only
// permits controlled HTTP tests; authorization remains in the original Tx.
type configurationCommands interface {
	CreateAgent(context.Context, i.Actor, f.CommandMeta, i.ProjectID, c.AgentCreate) (c.AgentMutation, error)
	UpdateAgent(context.Context, i.Actor, f.CommandMeta, i.ProjectID, i.AgentID, c.AgentUpdate) (c.AgentMutation, error)
	GetAgent(context.Context, i.Actor, i.ProjectID, i.AgentID) (c.AgentConfig, error)
	LookupAgentCommand(context.Context, i.Actor, i.ProjectID, c.CommandName, f.IdempotencyKey, f.Digest) (c.AgentCommandLookup, error)
}
type configurationHandler struct {
	commands configurationCommands
	boundary directoryBoundary
}

func NewConfigurationHTTPHandler(service *agent.Service, boundary *account.HTTPBoundary) (http.Handler, error) {
	if service == nil || boundary == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &configurationHandler{service, boundary}, nil
}

type configurationResource uint8

const (
	noConfiguration configurationResource = iota
	configurationCreate
	configurationRead
	configurationLookup
)

type configurationRoute struct {
	kind            configurationResource
	project, target string
}

func parseConfigurationRoute(path string) configurationRoute {
	if !strings.HasPrefix(path, directoryPrefix) {
		return configurationRoute{}
	}
	parts := strings.Split(strings.TrimPrefix(path, directoryPrefix), "/")
	for _, part := range parts {
		if part == "" {
			return configurationRoute{}
		}
	}
	switch {
	case len(parts) == 2 && parts[1] == "agents":
		return configurationRoute{kind: configurationCreate, project: parts[0]}
	case len(parts) == 4 && parts[1] == "agents" && parts[3] == "configuration":
		return configurationRoute{kind: configurationRead, project: parts[0], target: parts[2]}
	case len(parts) == 3 && parts[1] == "agent-commands" && parts[2] == "lookup":
		return configurationRoute{kind: configurationLookup, project: parts[0]}
	default:
		return configurationRoute{}
	}
}

// Compose this dispatch before Directory. Only POST on the shared collection
// belongs here; the existing GET/HEAD directory keeps its nine-field contract.
// Exclusive configuration paths stay here for their own closed method errors.
func HandlesConfigurationRequest(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	route := parseConfigurationRoute(r.URL.Path)
	return route.kind == configurationRead || route.kind == configurationLookup || route.kind == configurationCreate && r.Method == http.MethodPost
}

func (v configurationRoute) pattern() string {
	switch v.kind {
	case configurationCreate:
		return directoryPrefix + "{project_id}/agents"
	case configurationRead:
		return directoryPrefix + "{project_id}/agents/{agent_id}/configuration"
	case configurationLookup:
		return directoryPrefix + "{project_id}/agent-commands/lookup"
	default:
		return ""
	}
}
func (v configurationRoute) allow() string {
	if v.kind == configurationRead {
		return "GET, HEAD, PATCH"
	}
	return "POST"
}

func (h *configurationHandler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request == nil {
		panic(http.ErrAbortHandler)
	}
	route := configurationRoute{}
	if request.URL != nil {
		route = parseConfigurationRoute(request.URL.Path)
	}
	request.Pattern = route.pattern()
	limit := readBudget
	if route.kind == configurationCreate && request.Method == http.MethodPost || route.kind == configurationRead && request.Method == http.MethodPatch {
		limit = configurationMutationBudget
	}
	ctx, cancel := context.WithTimeout(request.Context(), limit)
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

func (h *configurationHandler) execute(w http.ResponseWriter, r *http.Request, route configurationRoute) ([]byte, error) {
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
	if actor.Validate() != nil || actor.Details().Kind != i.Human {
		return nil, f.NewFault(f.Unauthenticated, f.NotStarted)
	}
	if httpapi.RequestID(r.Context()).Validate() != nil {
		return nil, invalidInput()
	}
	if route.kind == noConfiguration {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	allowed := r.Method == http.MethodPost
	if route.kind == configurationRead {
		allowed = r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodPatch
	}
	if !allowed {
		w.Header().Set("Allow", route.allow())
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	project, err := f.ParseID[i.Project](route.project)
	if err != nil || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalidInput()
	}
	if route.kind == configurationRead && r.Method != http.MethodPatch {
		target, err := f.ParseID[i.Agent](route.target)
		if err != nil {
			return nil, invalidInput()
		}
		if err = emptyBody(r); err != nil {
			return nil, err
		}
		out, err := h.commands.GetAgent(r.Context(), actor, project, target)
		if err != nil {
			return nil, err
		}
		if !configurationMatches(out, project, target) {
			return nil, badProjection()
		}
		return encodeDirectory(r.Context(), out, configurationBodyLimit)
	}
	intent, err := decodeConfigurationIntent(w, r, route)
	if err != nil {
		return nil, err
	}
	digest, err := c.AgentCommandDigest(actor, intent.meta, project, intent.target, intent.command, intent.value())
	if err != nil {
		return nil, err
	}
	if route.kind == configurationLookup {
		out, err := h.commands.LookupAgentCommand(r.Context(), actor, project, intent.command, intent.meta.IdempotencyKey, digest)
		if err != nil {
			return nil, err
		}
		if out.Validate() != nil {
			return nil, badProjection()
		}
		if receipt := out.Receipt(); receipt != nil && !intent.matches(*receipt, project) {
			return nil, badProjection()
		}
		return encodeDirectory(r.Context(), out, configurationBodyLimit)
	}
	var out c.AgentMutation
	if intent.command == c.UpdateAgentCommand {
		out, err = h.commands.UpdateAgent(r.Context(), actor, intent.meta, project, intent.target, intent.update)
	} else {
		out, err = h.commands.CreateAgent(r.Context(), actor, intent.meta, project, intent.request)
	}
	if err != nil {
		return nil, err
	}
	// A successful writer cannot be changed into a known-not-started error if
	// its response is invalid or cannot be delivered. The original key survives.
	if !intent.matches(out, project) {
		panic(http.ErrAbortHandler)
	}
	raw, err := encodeDirectory(r.Context(), out, configurationBodyLimit)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	return raw, nil
}
