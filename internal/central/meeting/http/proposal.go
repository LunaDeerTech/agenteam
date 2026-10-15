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

type proposalService interface {
	GetProposal(context.Context, i.Actor, i.ProjectID, c.MeetingID) (c.ProposalView, error)
	Review(context.Context, i.Actor, c.ReviewProposalRequest) (c.Receipt, error)
	LookupReview(context.Context, i.Actor, c.ReviewProposalRequest) (c.Receipt, bool, error)
}

type proposalHandler struct {
	service  proposalService
	boundary boundary
}

// NewProposalHTTPHandler exposes the original domain review command. Inbox
// remains a read projection and is never a second owner of review decisions.
func NewProposalHTTPHandler(s *meeting.ProposalService, b *account.HTTPBoundary) (http.Handler, error) {
	if s == nil || b == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	return &proposalHandler{service: s, boundary: b}, nil
}

func proposalRouteFor(path string) route {
	if !strings.HasPrefix(path, "/api/v1/projects/") {
		return route{}
	}
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/projects/"), "/")
	if len(parts) < 4 || len(parts) > 6 || parts[1] != "meetings" || parts[3] != "proposal" {
		return route{}
	}
	for _, part := range parts {
		if part == "" {
			return route{}
		}
	}
	switch {
	case len(parts) == 4:
		return route{project: parts[0], meeting: parts[2], action: "proposal"}
	case len(parts) == 5 && parts[4] == "review":
		return route{project: parts[0], meeting: parts[2], action: "proposal-review"}
	case len(parts) == 6 && parts[4] == "review" && parts[5] == "lookup":
		return route{project: parts[0], meeting: parts[2], action: "proposal-lookup"}
	default:
		return route{}
	}
}

func HandlesProposalPath(path string) bool { return proposalRouteFor(path).action != "" }

func proposalPattern(path route) string {
	const base = "/api/v1/projects/{project_id}/meetings/{meeting_id}/proposal"
	switch path.action {
	case "proposal":
		return base
	case "proposal-review":
		return base + "/review"
	case "proposal-lookup":
		return base + "/review/lookup"
	default:
		return ""
	}
}

func (h *proposalHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r == nil {
		panic(http.ErrAbortHandler)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	path := route{}
	if r.URL != nil {
		path = proposalRouteFor(r.URL.Path)
	}
	r.Pattern = proposalPattern(path)
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
	body, err := h.execute(w, r, path)
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

func (h *proposalHandler) execute(w http.ResponseWriter, r *http.Request, path route) ([]byte, error) {
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
	method := http.MethodPost
	if path.action == "proposal" {
		method = http.MethodGet
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		return nil, f.NewFault(f.MethodNotAllowed, f.NotStarted)
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		return nil, invalid()
	}
	project, err := f.ParseID[i.Project](path.project)
	if err != nil {
		return nil, invalid()
	}
	meetingID, err := f.ParseID[c.Meeting](path.meeting)
	if err != nil {
		return nil, invalid()
	}
	var value any
	if method == http.MethodGet {
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			return nil, invalid()
		}
		value, err = h.service.GetProposal(r.Context(), actor, project, meetingID)
	} else {
		keys := headerValues(r, "Idempotency-Key")
		if len(keys) != 1 {
			return nil, invalid()
		}
		meta := f.CommandMeta{RequestID: httpapi.RequestID(r.Context()), IdempotencyKey: f.IdempotencyKey(keys[0])}
		if meta.Validate() != nil {
			return nil, invalid()
		}
		var body struct {
			Expected f.Version `json:"expected_version"`
			Approve  *bool     `json:"approve"`
		}
		if err := httpapi.DecodeJSON(w, r, &body, 16<<10); err != nil {
			return nil, err
		}
		// A missing/null decision must never silently become rejection.
		if body.Approve == nil {
			return nil, invalid()
		}
		command := c.ReviewProposalRequest{ProjectID: project, MeetingID: meetingID, ExpectedVersion: body.Expected, Approve: *body.Approve, Meta: meta}
		if err := command.Validate(); err != nil {
			return nil, err
		}
		if path.action == "proposal-review" {
			value, err = h.service.Review(r.Context(), actor, command)
		} else {
			var receipt c.Receipt
			var found bool
			receipt, found, err = h.service.LookupReview(r.Context(), actor, command)
			value = lookupResult(receipt, found)
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
