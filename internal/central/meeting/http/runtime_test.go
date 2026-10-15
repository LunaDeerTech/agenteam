package meetinghttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

type runtimeSpy struct {
	reads   int
	foreign bool
	view    c.ContributionRuntimeView
}

func (s *runtimeSpy) GetContributionRuntime(_ context.Context, _ i.Actor, project i.ProjectID, meeting c.MeetingID, contribution c.ContributionID) (c.ContributionRuntimeView, error) {
	s.reads++
	value := s.view
	value.Execution.ProjectID, value.Execution.MeetingID, value.Execution.ContributionID = project, meeting, contribution
	if s.foreign {
		value.Execution.ContributionID = s.view.Execution.ContributionID
	}
	return value, nil
}

func TestMeetingRuntimeHTTPUsesCurrentMeetingRouteAndOriginalBoundary(t *testing.T) {
	actor, err := i.NewHuman(fresh[i.User](t), fresh[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	at, _ := f.NewInstant(time.Unix(1, 0).UTC())
	spy := &runtimeSpy{view: c.ContributionRuntimeView{Execution: c.ContributionExecution{ID: fresh[i.Execution](t), TurnID: fresh[c.Turn](t), ContributionID: fresh[c.Contribution](t), ParticipantID: fresh[c.Participant](t), AgentID: fresh[i.Agent](t), Generation: 2, Attempt: 3, Status: ec.Running, Version: 4, CreatedAt: at, StartedAt: &at}, Availability: ec.RuntimePending}}
	boundary := &testBoundary{actor: actor}
	handler := httpapi.WithRequestID(slog.New(slog.NewTextHandler(io.Discard, nil)), &runtimeHandler{service: spy, boundary: boundary})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	project, meeting, contribution := fresh[i.Project](t), fresh[c.Meeting](t), fresh[c.Contribution](t)
	path := "/api/v1/projects/" + project.String() + "/meetings/" + meeting.String() + "/contributions/" + contribution.String() + "/runtime"
	status, raw := request(t, server, "GET", path, "")
	var value c.ContributionRuntimeView
	if status != 200 || json.Unmarshal(raw, &value) != nil || value.Validate() != nil || value.Execution.ProjectID != project || value.Execution.MeetingID != meeting || value.Execution.ContributionID != contribution || value.Execution.ID != spy.view.Execution.ID || value.Snapshot != nil {
		t.Fatal("runtime HTTP lost original source scope", status)
	}
	for _, bad := range []struct{ method, suffix, body string }{
		{"POST", "", "{}"}, {"GET", "?execution_id=other", ""}, {"GET", "/", ""}, {"GET", "", "{}"},
	} {
		if status, _ := request(t, server, bad.method, path+bad.suffix, bad.body); status < 400 || spy.reads != 1 {
			t.Fatal("runtime accepted a foreign query/body or command", status)
		}
	}
	spy.foreign = true
	if status, _ := request(t, server, "GET", path, ""); status != 503 || spy.reads != 2 {
		t.Fatal("runtime returned a foreign Contribution projection", status)
	}
	boundary.deny = true
	if status, _ := request(t, server, "GET", "/api/v1/projects/invalid/meetings/invalid/contributions/invalid/runtime", ""); status != 403 || spy.reads != 2 {
		t.Fatal("runtime parsed identities before authenticating the original Owner", status)
	}
}
