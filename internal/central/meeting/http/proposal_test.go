package meetinghttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type proposalSpy struct {
	reads, reviews, lookups int
	project                 i.ProjectID
	meeting                 c.MeetingID
	original                c.ReviewProposalRequest
	receipt                 c.Receipt
	found                   bool
	source                  i.ExecutionID
	creator                 c.ParticipantID
}

func (s *proposalSpy) GetProposal(_ context.Context, _ i.Actor, p i.ProjectID, m c.MeetingID) (c.ProposalView, error) {
	s.reads++
	s.project, s.meeting = p, m
	reason := "Discuss the original release plan."
	at := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	return c.ProposalView{Session: c.Session{ID: m, ProjectID: p, Status: "proposed", Version: 1, ProposalContent: &reason, CreatedAt: at, UpdatedAt: at}, SourceExecutionID: s.source, CreatedByParticipantID: s.creator}, nil
}
func (s *proposalSpy) Review(_ context.Context, _ i.Actor, r c.ReviewProposalRequest) (c.Receipt, error) {
	s.reviews++
	s.original = r
	return s.receipt, nil
}
func (s *proposalSpy) LookupReview(_ context.Context, _ i.Actor, r c.ReviewProposalRequest) (c.Receipt, bool, error) {
	s.lookups++
	s.original = r
	return s.receipt, s.found, nil
}

func proposalServer(t *testing.T) (*httptest.Server, *proposalSpy, *testBoundary, string) {
	t.Helper()
	actor, err := i.NewHuman(fresh[i.User](t), fresh[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	s := &proposalSpy{receipt: c.Receipt{MeetingID: fresh[c.Meeting](t), Version: 2}, source: fresh[i.Execution](t), creator: fresh[c.Participant](t)}
	b := &testBoundary{actor: actor}
	h := httpapi.WithRequestID(slog.New(slog.NewTextHandler(io.Discard, nil)), &proposalHandler{service: s, boundary: b})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, s, b, "/api/v1/projects/" + fresh[i.Project](t).String() + "/meetings/" + s.receipt.MeetingID.String() + "/proposal"
}

func TestProposalHTTPAuthenticatesBeforeScopeAndUsesOriginalRead(t *testing.T) {
	srv, s, b, path := proposalServer(t)
	status, raw := request(t, srv, "GET", path, "")
	if status != 200 || s.reads != 1 || s.meeting != s.receipt.MeetingID || s.project.Validate() != nil {
		t.Fatal("proposal original read was not scoped", status, s.reads)
	}
	var view c.ProposalView
	if json.Unmarshal(raw, &view) != nil || view.Session.Version != 1 || view.SourceExecutionID != s.source || view.CreatedByParticipantID != s.creator || view.Session.ProposalContent == nil {
		t.Fatal("proposal original response lost its typed identity")
	}
	for _, suffix := range []string{"?source_id=other", "/review/lookup/extra", "/"} {
		status, _ := request(t, srv, "GET", path+suffix, "")
		if status < 400 || s.reads != 1 || s.reviews != 0 || s.lookups != 0 {
			t.Fatal("proposal accepted a foreign route or query", status)
		}
	}
	b.deny = true
	if status, _ := request(t, srv, "GET", "/api/v1/projects/invalid/meetings/invalid/proposal", ""); status != 403 || s.reads != 1 {
		t.Fatal("proposal parsed scope before the original Human boundary", status)
	}
}

func TestProposalHTTPRequiresExplicitDecisionAndMatchingCommandScope(t *testing.T) {
	srv, s, _, path := proposalServer(t)
	for _, body := range []string{
		`{"expected_version":"1"}`, `{"expected_version":"1","approve":null}`, `{"expected_version":"1","approve":"false"}`,
		`{"expected_version":"1","approve":true,"project_id":"other"}`, `{"expected_version":"0","approve":true}`,
		`{"expected_version":1,"approve":true}`,
	} {
		if status, _ := request(t, srv, "POST", path+"/review", body); status != 400 || s.reviews != 0 {
			t.Fatal("invalid proposal decision reached the domain", status, s.reviews)
		}
	}
	for _, approve := range []bool{false, true} {
		raw, err := json.Marshal(map[string]any{"expected_version": f.Version(1), "approve": approve})
		if err != nil {
			t.Fatal(err)
		}
		if status, _ := request(t, srv, "POST", path+"/review", string(raw)); status != 200 || s.original.Approve != approve || s.original.MeetingID != s.receipt.MeetingID || s.original.ExpectedVersion != f.Version(1) || s.original.Meta.IdempotencyKey != "original-meeting-command" || s.original.Validate() != nil {
			t.Fatal("review lost its exact choice, target, version or original key", status)
		}
	}
	if s.reviews != 2 || s.lookups != 0 {
		t.Fatal("review used a second domain path")
	}
}

func TestProposalHTTPLookupNeverReissuesReview(t *testing.T) {
	srv, s, _, path := proposalServer(t)
	body := `{"expected_version":"1","approve":false}`
	for _, found := range []bool{false, true} {
		s.found = found
		status, raw := request(t, srv, "POST", path+"/review/lookup", body)
		var value struct {
			Status  string     `json:"status"`
			Receipt *c.Receipt `json:"receipt"`
		}
		if status != 200 || json.Unmarshal(raw, &value) != nil || s.reviews != 0 || s.original.Approve || s.original.Validate() != nil {
			t.Fatal("lookup mutated or lost its original request", status)
		}
		if found && (value.Status != "committed" || value.Receipt == nil || value.Receipt.MeetingID != s.receipt.MeetingID) || !found && (value.Status != "not_found" || value.Receipt != nil) {
			t.Fatal("lookup fabricated a commit receipt")
		}
	}
	if s.lookups != 2 {
		t.Fatal("lookup used a second domain path")
	}
	if status, _ := request(t, srv, "GET", path+"/review", ""); status != 405 || s.lookups != 2 || s.reviews != 0 {
		t.Fatal("review accepted a read method", status)
	}
}
