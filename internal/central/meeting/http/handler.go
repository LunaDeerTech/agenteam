// Package meetinghttp exposes Human Owner Meeting commands and views.
// Background execution is owned by the Meeting runtime, never the HTTP request.
package meetinghttp

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/meeting"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type boundary interface {
	CheckRequest(http.ResponseWriter, *http.Request) error
	RequireHuman(*http.Request) (i.Actor, error)
	WriteProblem(http.ResponseWriter, *http.Request, error)
}
type service interface {
	List(context.Context, i.Actor, i.ProjectID) ([]c.Session, error)
	GetDetail(context.Context, i.Actor, i.ProjectID, c.MeetingID) (c.Detail, error)
	GetContributionExecution(context.Context, i.Actor, i.ProjectID, c.MeetingID, c.ContributionID) (c.ContributionExecution, error)
	Start(context.Context, i.Actor, c.StartRequest) (c.Receipt, error)
	Send(context.Context, i.Actor, c.SendRequest) (c.Receipt, error)
	LookupStart(context.Context, i.Actor, c.StartRequest) (c.Receipt, bool, error)
	LookupSend(context.Context, i.Actor, c.SendRequest) (c.Receipt, bool, error)
	Control(context.Context, i.Actor, c.ControlRequest) (c.Receipt, error)
	LookupControl(context.Context, i.Actor, c.ControlRequest) (c.Receipt, bool, error)
	RespondDecision(context.Context, i.Actor, c.RespondDecisionRequest) (c.Receipt, error)
	LookupRespondDecision(context.Context, i.Actor, c.RespondDecisionRequest) (c.Receipt, bool, error)
	UpdateParticipants(context.Context, i.Actor, c.UpdateParticipantsRequest) (c.Receipt, error)
	LookupUpdateParticipants(context.Context, i.Actor, c.UpdateParticipantsRequest) (c.Receipt, bool, error)
	UpdateReferences(context.Context, i.Actor, c.UpdateReferencesRequest) (c.Receipt, error)
	LookupUpdateReferences(context.Context, i.Actor, c.UpdateReferencesRequest) (c.Receipt, bool, error)
}
type handler struct {
	service  service
	boundary boundary
}

func NewHTTPHandler(s *meeting.Service, b *account.HTTPBoundary) (http.Handler, error) {
	if s == nil || b == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &handler{s, b}, nil
}

type route struct{ project, meeting, action, contribution string }

func routeFor(path string) route {
	if !strings.HasPrefix(path, "/api/v1/projects/") {
		return route{}
	}
	p := strings.Split(strings.TrimPrefix(path, "/api/v1/projects/"), "/")
	if len(p) < 2 || len(p) > 6 || p[1] != "meetings" {
		return route{}
	}
	for _, v := range p {
		if v == "" {
			return route{}
		}
	}
	switch {
	case len(p) == 2:
		return route{project: p[0], action: "collection"}
	case len(p) == 3:
		return route{project: p[0], meeting: p[2], action: "detail"}
	case len(p) == 4 && p[2] == "start" && p[3] == "lookup":
		return route{project: p[0], action: "start-lookup"}
	case len(p) == 4 && (p[3] == "controls" || p[3] == "decisions" || p[3] == "participants" || p[3] == "references"):
		return route{project: p[0], meeting: p[2], action: p[3]}
	case len(p) == 5 && (p[3] == "controls" || p[3] == "decisions" || p[3] == "participants" || p[3] == "references") && p[4] == "lookup":
		return route{project: p[0], meeting: p[2], action: p[3] + "-lookup"}
	case len(p) == 4 && p[3] == "turns":
		return route{project: p[0], meeting: p[2], action: "send"}
	case len(p) == 5 && p[3] == "turns" && p[4] == "lookup":
		return route{project: p[0], meeting: p[2], action: "send-lookup"}
	case len(p) == 6 && p[3] == "contributions" && p[5] == "execution":
		return route{project: p[0], meeting: p[2], contribution: p[4], action: "contribution-execution"}
	default:
		return route{}
	}
}
func HandlesPath(path string) bool { return routeFor(path).action != "" }
func (v route) pattern() string {
	path := "/api/v1/projects/{project_id}/meetings"
	switch v.action {
	case "collection":
		return path
	case "detail":
		return path + "/{meeting_id}"
	case "contribution-execution":
		return path + "/{meeting_id}/contributions/{contribution_id}/execution"
	case "controls", "decisions", "participants", "references":
		return path + "/{meeting_id}/" + v.action
	case "controls-lookup":
		return path + "/{meeting_id}/controls/lookup"
	case "decisions-lookup":
		return path + "/{meeting_id}/decisions/lookup"
	case "participants-lookup":
		return path + "/{meeting_id}/participants/lookup"
	case "references-lookup":
		return path + "/{meeting_id}/references/lookup"
	case "send":
		return path + "/{meeting_id}/turns"
	case "start-lookup":
		return path + "/start/lookup"
	case "send-lookup":
		return path + "/{meeting_id}/turns/lookup"
	default:
		return ""
	}
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
func (h *handler) execute(w http.ResponseWriter, r *http.Request, path route) ([]byte, error) {
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
	if path.action == "" {
		return nil, f.NewFault(f.NotFound, f.NotStarted)
	}
	allowed := "POST"
	readOnly := path.action == "detail" || path.action == "contribution-execution"
	if readOnly {
		allowed = "GET"
	}
	if path.action == "collection" {
		allowed = "GET, POST"
	}
	if !(r.Method == http.MethodGet && (path.action == "collection" || readOnly) || r.Method == http.MethodPost && !readOnly) {
		w.Header().Set("Allow", allowed)
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalid()
	}
	project, err := f.ParseID[i.Project](path.project)
	if err != nil {
		return nil, invalid()
	}
	var meetingID c.MeetingID
	if path.meeting != "" {
		meetingID, err = f.ParseID[c.Meeting](path.meeting)
		if err != nil {
			return nil, invalid()
		}
	}
	var value any
	if r.Method == http.MethodGet {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			return nil, invalid()
		}
		if path.action == "collection" {
			var sessions []c.Session
			sessions, err = h.service.List(r.Context(), actor, project)
			if sessions == nil {
				sessions = []c.Session{}
			}
			value = struct {
				Meetings []c.Session `json:"meetings"`
			}{sessions}
		} else if path.action == "contribution-execution" {
			contributionID, e := f.ParseID[c.Contribution](path.contribution)
			if e != nil {
				return nil, invalid()
			}
			var execution c.ContributionExecution
			execution, err = h.service.GetContributionExecution(r.Context(), actor, project, meetingID, contributionID)
			value = struct {
				Execution c.ContributionExecution `json:"execution"`
			}{execution}
		} else {
			value, err = h.service.GetDetail(r.Context(), actor, project, meetingID)
		}
	} else {
		keys := headerValues(r, "Idempotency-Key")
		if len(keys) != 1 {
			return nil, invalid()
		}
		meta := f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0])}
		if meta.Validate() != nil {
			return nil, invalid()
		}
		if path.action == "collection" || path.action == "start-lookup" {
			var body struct {
				Agents  []i.AgentID    `json:"agents"`
				Mode    string         `json:"mode"`
				Content []c.InlineNode `json:"content_nodes"`
			}
			if err := httpapi.DecodeJSON(w, r, &body, 1<<20); err != nil {
				return nil, err
			}
			command := c.StartRequest{ProjectID: project, Agents: body.Agents, Mode: body.Mode, Content: body.Content, Meta: meta}
			if err := command.Validate(); err != nil {
				return nil, err
			}
			if path.action == "collection" {
				value, err = h.service.Start(r.Context(), actor, command)
			} else {
				var receipt c.Receipt
				var found bool
				receipt, found, err = h.service.LookupStart(r.Context(), actor, command)
				value = lookupResult(receipt, found)
			}
		} else if path.action == "participants" || path.action == "participants-lookup" {
			var body struct {
				Expected f.Version   `json:"expected_version"`
				Agents   []i.AgentID `json:"agents"`
			}
			if err := httpapi.DecodeJSON(w, r, &body, 4096); err != nil {
				return nil, err
			}
			command := c.UpdateParticipantsRequest{ProjectID: project, MeetingID: meetingID, ExpectedVersion: body.Expected, Agents: body.Agents, Meta: meta}
			if err := command.Validate(); err != nil {
				return nil, err
			}
			if path.action == "participants" {
				value, err = h.service.UpdateParticipants(r.Context(), actor, command)
			} else {
				var receipt c.Receipt
				var found bool
				receipt, found, err = h.service.LookupUpdateParticipants(r.Context(), actor, command)
				value = lookupResult(receipt, found)
			}
		} else if path.action == "references" || path.action == "references-lookup" {
			var body struct {
				Expected   f.Version     `json:"expected_version"`
				References []c.Reference `json:"references"`
			}
			if err := httpapi.DecodeJSON(w, r, &body, 3<<20); err != nil {
				return nil, err
			}
			command := c.UpdateReferencesRequest{ProjectID: project, MeetingID: meetingID, ExpectedVersion: body.Expected, References: body.References, Meta: meta}
			if err := command.Validate(); err != nil {
				return nil, err
			}
			if path.action == "references" {
				value, err = h.service.UpdateReferences(r.Context(), actor, command)
			} else {
				var receipt c.Receipt
				var found bool
				receipt, found, err = h.service.LookupUpdateReferences(r.Context(), actor, command)
				value = lookupResult(receipt, found)
			}
		} else if path.action == "controls" || path.action == "controls-lookup" {
			var body struct {
				Action         string            `json:"action"`
				Expected       f.Version         `json:"expected_version"`
				TurnID         *c.TurnID         `json:"turn_id,omitempty"`
				ContributionID *c.ContributionID `json:"contribution_id,omitempty"`
			}
			if err := httpapi.DecodeJSON(w, r, &body, 4096); err != nil {
				return nil, err
			}
			command := c.ControlRequest{ProjectID: project, MeetingID: meetingID, Action: body.Action, ExpectedVersion: body.Expected, TurnID: body.TurnID, ContributionID: body.ContributionID, Meta: meta}
			if err := command.Validate(); err != nil {
				return nil, err
			}
			if path.action == "controls" {
				value, err = h.service.Control(r.Context(), actor, command)
			} else {
				var receipt c.Receipt
				var found bool
				receipt, found, err = h.service.LookupControl(r.Context(), actor, command)
				value = lookupResult(receipt, found)
			}
		} else if path.action == "decisions" || path.action == "decisions-lookup" {
			var body struct {
				DecisionID c.DecisionID `json:"decision_id"`
				Action     string       `json:"action"`
				Expected   f.Version    `json:"expected_version"`
				Answer     *string      `json:"answer,omitempty"`
			}
			if err := httpapi.DecodeJSON(w, r, &body, 48<<10); err != nil {
				return nil, err
			}
			command := c.RespondDecisionRequest{ProjectID: project, MeetingID: meetingID, DecisionID: body.DecisionID, Action: body.Action, ExpectedVersion: body.Expected, Answer: body.Answer, Meta: meta}
			if err := command.Validate(); err != nil {
				return nil, err
			}
			if path.action == "decisions" {
				value, err = h.service.RespondDecision(r.Context(), actor, command)
			} else {
				var receipt c.Receipt
				var found bool
				receipt, found, err = h.service.LookupRespondDecision(r.Context(), actor, command)
				value = lookupResult(receipt, found)
			}
		} else {
			var body struct {
				Expected  f.Version      `json:"expected_version"`
				Mode      string         `json:"mode"`
				Content   []c.InlineNode `json:"content_nodes"`
				Interrupt *bool          `json:"interrupt"`
				ReplyTo   *c.MessageID   `json:"reply_to_message_id,omitempty"`
			}
			if err := httpapi.DecodeJSON(w, r, &body, 1<<20); err != nil {
				return nil, err
			}
			if body.Interrupt == nil {
				return nil, invalid()
			}
			command := c.SendRequest{ProjectID: project, MeetingID: meetingID, ExpectedVersion: body.Expected, Mode: body.Mode, Content: body.Content, Interrupt: *body.Interrupt, ReplyTo: body.ReplyTo, Meta: meta}
			if err := command.Validate(); err != nil {
				return nil, err
			}
			if path.action == "send" {
				value, err = h.service.Send(r.Context(), actor, command)
			} else {
				var receipt c.Receipt
				var found bool
				receipt, found, err = h.service.LookupSend(r.Context(), actor, command)
				value = lookupResult(receipt, found)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > 8<<20 {
		return nil, f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return raw, nil
}
func lookupResult(receipt c.Receipt, found bool) any {
	if found {
		return struct {
			Status  string    `json:"status"`
			Receipt c.Receipt `json:"receipt"`
		}{"committed", receipt}
	}
	return struct {
		Status string `json:"status"`
	}{"not_found"}
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
