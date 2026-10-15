package meetinghttp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
)

func fresh[K any](t *testing.T) f.ID[K] {
	t.Helper()
	id, err := f.NewID[K]()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type testBoundary struct {
	actor i.Actor
	deny  bool
}

func (b *testBoundary) CheckRequest(http.ResponseWriter, *http.Request) error {
	if b.deny {
		return f.NewFault(f.Forbidden, f.NotStarted)
	}
	return nil
}
func (b *testBoundary) RequireHuman(*http.Request) (i.Actor, error) { return b.actor, nil }
func (*testBoundary) WriteProblem(w http.ResponseWriter, r *http.Request, err error) {
	httpapi.WriteProblem(w, r, err)
}

type meetingSpy struct {
	reads, starts, sends, lookups, controls, decisions int
	participantWrites, referenceWrites                 int
	participants                                       c.UpdateParticipantsRequest
	references                                         c.UpdateReferencesRequest
	control                                            c.ControlRequest
	decision                                           c.RespondDecisionRequest
	start                                              c.StartRequest
	send                                               c.SendRequest
	receipt                                            c.Receipt
	found                                              bool
	executionReads                                     int
	executionProject                                   i.ProjectID
	executionMeeting                                   c.MeetingID
	executionContribution                              c.ContributionID
}

func (s *meetingSpy) UpdateParticipants(_ context.Context, _ i.Actor, r c.UpdateParticipantsRequest) (c.Receipt, error) {
	s.participantWrites++
	s.participants = r
	return s.receipt, nil
}
func (s *meetingSpy) LookupUpdateParticipants(_ context.Context, _ i.Actor, r c.UpdateParticipantsRequest) (c.Receipt, bool, error) {
	s.lookups++
	s.participants = r
	return s.receipt, s.found, nil
}
func (s *meetingSpy) UpdateReferences(_ context.Context, _ i.Actor, r c.UpdateReferencesRequest) (c.Receipt, error) {
	s.referenceWrites++
	s.references = r
	return s.receipt, nil
}
func (s *meetingSpy) LookupUpdateReferences(_ context.Context, _ i.Actor, r c.UpdateReferencesRequest) (c.Receipt, bool, error) {
	s.lookups++
	s.references = r
	return s.receipt, s.found, nil
}

func TestMeetingHTTPRosterAndPinnedReferencesKeepOriginalScope(t *testing.T) {
	srv, spy, boundary, base := server(t)
	path := base + "/" + spy.receipt.MeetingID.String()
	agent := fresh[i.Agent](t)
	roster := `{"expected_version":"9007199254740993","agents":["` + agent.String() + `"]}`
	refs := `{"expected_version":"9007199254740993","references":[{"kind":"link","id":"HTTPS://Example.COM:443#one","label":"Design"}]}`
	for _, tc := range []struct{ suffix, body string }{{"participants", roster}, {"references", refs}} {
		if !HandlesPath(path + "/" + tc.suffix) {
			t.Fatal("route absent", tc.suffix)
		}
		if status, _ := request(t, srv, "POST", path+"/"+tc.suffix, tc.body); status != 200 {
			t.Fatal("write", tc.suffix, status)
		}
		if status, raw := request(t, srv, "POST", path+"/"+tc.suffix+"/lookup", tc.body); status != 200 || string(raw) != `{"status":"not_found"}` {
			t.Fatal("lookup", tc.suffix, status)
		}
		if status, _ := request(t, srv, "GET", path+"/"+tc.suffix, ""); status != 405 {
			t.Fatal("metadata write GET accepted", status)
		}
	}
	if spy.participantWrites != 1 || spy.referenceWrites != 1 || spy.lookups != 2 || spy.participants.MeetingID != spy.receipt.MeetingID || spy.references.MeetingID != spy.receipt.MeetingID || spy.participants.Agents[0] != agent || spy.participants.ExpectedVersion != 9007199254740993 || spy.references.ExpectedVersion != 9007199254740993 || spy.participants.Meta.IdempotencyKey != "original-meeting-command" || spy.references.Meta.IdempotencyKey != "original-meeting-command" {
		t.Fatal("metadata command lost original identity")
	}
	for _, tc := range []struct{ suffix, body string }{
		{"participants", `{"expected_version":"1"}`},
		{"participants", `{"expected_version":"1","agents":null}`},
		{"participants", `{"expected_version":"1","agents":[],"participant_id":"forged"}`},
		{"references", `{"expected_version":"1","references":[{"kind":"link","id":"https://user:password@example.com/"}]}`},
		{"references", `{"expected_version":"1","references":[{"kind":"file","id":"storage-key"}]}`},
	} {
		if status, _ := request(t, srv, "POST", path+"/"+tc.suffix, tc.body); status != 400 {
			t.Fatal("invalid metadata accepted", tc.suffix, status)
		}
	}
	boundary.deny = true
	if status, _ := request(t, srv, "POST", path+"/participants", roster); status != 403 || spy.participantWrites != 1 || spy.referenceWrites != 1 {
		t.Fatal("metadata bypassed boundary", status)
	}
}

func (s *meetingSpy) List(context.Context, i.Actor, i.ProjectID) ([]c.Session, error) {
	s.reads++
	return []c.Session{}, nil
}
func (s *meetingSpy) GetDetail(context.Context, i.Actor, i.ProjectID, c.MeetingID) (c.Detail, error) {
	s.reads++
	return c.Detail{}, nil
}
func (s *meetingSpy) GetContributionExecution(_ context.Context, _ i.Actor, p i.ProjectID, m c.MeetingID, contribution c.ContributionID) (c.ContributionExecution, error) {
	s.executionReads++
	s.executionProject, s.executionMeeting, s.executionContribution = p, m, contribution
	return c.ContributionExecution{}, f.NewFault(f.NotFound, f.NotStarted)
}

func TestMeetingHTTPCurrentContributionExecutionIsScopedRead(t *testing.T) {
	srv, spy, boundary, base := server(t)
	meeting, contribution := fresh[c.Meeting](t), fresh[c.Contribution](t)
	path := base + "/" + meeting.String() + "/contributions/" + contribution.String() + "/execution"
	if !HandlesPath(path) || routeFor(path).pattern() != "/api/v1/projects/{project_id}/meetings/{meeting_id}/contributions/{contribution_id}/execution" {
		t.Fatal("targeted route not registered")
	}
	if status, _ := request(t, srv, "GET", path, ""); status != 404 || spy.executionReads != 1 || spy.executionMeeting != meeting || spy.executionContribution != contribution || !strings.Contains(base, spy.executionProject.String()) {
		t.Fatal("scoped read did not reach the domain exactly once", status)
	}
	for _, invalid := range []struct{ method, path, body string }{
		{"POST", path, `{}`},
		{"GET", path + "?execution_id=other", ""},
		{"GET", base + "/" + meeting.String() + "/contributions/invalid/execution", ""},
		{"GET", path, `{}`},
	} {
		if status, _ := request(t, srv, invalid.method, invalid.path, invalid.body); status < 400 || spy.executionReads != 1 {
			t.Fatal("targeted read accepted an alternate source", status)
		}
	}
	boundary.deny = true
	if status, _ := request(t, srv, "GET", path, ""); status != 403 || spy.executionReads != 1 {
		t.Fatal("authorization did not precede source read", status)
	}
}
func (s *meetingSpy) Start(_ context.Context, _ i.Actor, r c.StartRequest) (c.Receipt, error) {
	s.starts++
	s.start = r
	return s.receipt, nil
}
func (s *meetingSpy) Send(_ context.Context, _ i.Actor, r c.SendRequest) (c.Receipt, error) {
	s.sends++
	s.send = r
	return s.receipt, nil
}
func (s *meetingSpy) LookupStart(_ context.Context, _ i.Actor, r c.StartRequest) (c.Receipt, bool, error) {
	s.lookups++
	s.start = r
	return s.receipt, s.found, nil
}
func (s *meetingSpy) LookupSend(_ context.Context, _ i.Actor, r c.SendRequest) (c.Receipt, bool, error) {
	s.lookups++
	s.send = r
	return s.receipt, s.found, nil
}

func TestMeetingHTTPReplyKeepsOriginalSendAndLookupIdentity(t *testing.T) {
	srv, spy, boundary, base := server(t)
	message := fresh[c.Message](t)
	path := base + "/" + spy.receipt.MeetingID.String() + "/turns"
	body := `{"expected_version":"1","mode":"sequential","content_nodes":[{"type":"text","text":"Reply to the earlier message."}],"interrupt":false,"reply_to_message_id":"` + message.String() + `"}`
	if status, _ := request(t, srv, "POST", path, body); status != 200 || spy.sends != 1 || spy.send.ReplyTo == nil || *spy.send.ReplyTo != message {
		t.Fatal("reply did not reach original Send", status)
	}
	first := spy.send
	if status, _ := request(t, srv, "POST", path+"/lookup", body); status != 200 || spy.lookups != 1 || spy.send.ReplyTo == nil || *spy.send.ReplyTo != message || spy.send.Meta.IdempotencyKey != first.Meta.IdempotencyKey || spy.send.MeetingID != first.MeetingID {
		t.Fatal("reply lookup changed the original target or command", status)
	}
	for _, invalidBody := range []string{
		strings.Replace(body, message.String(), "invalid", 1),
		strings.Replace(body, `"reply_to_message_id":"`+message.String()+`"`, `"reply_to_message_id":{}`, 1),
		strings.Replace(body, `"reply_to_message_id":"`+message.String()+`"`, `"reply_to_message_id":"`+message.String()+`","execution_id":"forged"`, 1),
	} {
		if status, _ := request(t, srv, "POST", path, invalidBody); status != 400 || spy.sends != 1 {
			t.Fatal("invalid reply shape reached Send", status)
		}
	}
	start := `{"agents":["` + fresh[i.Agent](t).String() + `"],"mode":"sequential","content_nodes":[{"type":"text","text":"First message"}],"reply_to_message_id":"` + message.String() + `"}`
	if status, _ := request(t, srv, "POST", base, start); status != 400 || spy.starts != 0 {
		t.Fatal("new Meeting accepted another Meeting's reply identity", status)
	}
	boundary.deny = true
	if status, _ := request(t, srv, "POST", path, body); status != 403 || spy.sends != 1 {
		t.Fatal("reply bypassed original Human boundary", status)
	}
}
func (s *meetingSpy) Control(_ context.Context, _ i.Actor, r c.ControlRequest) (c.Receipt, error) {
	s.controls++
	s.control = r
	return s.receipt, nil
}
func (s *meetingSpy) LookupControl(_ context.Context, _ i.Actor, r c.ControlRequest) (c.Receipt, bool, error) {
	s.lookups++
	s.control = r
	return s.receipt, s.found, nil
}
func (s *meetingSpy) RespondDecision(_ context.Context, _ i.Actor, r c.RespondDecisionRequest) (c.Receipt, error) {
	s.decisions++
	s.decision = r
	return s.receipt, nil
}
func (s *meetingSpy) LookupRespondDecision(_ context.Context, _ i.Actor, r c.RespondDecisionRequest) (c.Receipt, bool, error) {
	s.lookups++
	s.decision = r
	return s.receipt, s.found, nil
}
func server(t *testing.T) (*httptest.Server, *meetingSpy, *testBoundary, string) {
	t.Helper()
	actor, _ := i.NewHuman(fresh[i.User](t), fresh[i.Session](t))
	b := &testBoundary{actor: actor}
	s := &meetingSpy{receipt: c.Receipt{MeetingID: fresh[c.Meeting](t), Version: 3}}
	h := httpapi.WithRequestID(slog.New(slog.NewTextHandler(io.Discard, nil)), &handler{s, b})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, s, b, "/api/v1/projects/" + fresh[i.Project](t).String() + "/meetings"
}
func request(t *testing.T, srv *httptest.Server, method, path, body string) (int, []byte) {
	t.Helper()
	r, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if method == "POST" {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "original-meeting-command")
	}
	resp, err := srv.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(resp.Body)
	closed := resp.Body.Close()
	if err != nil || closed != nil {
		t.Fatal(err, closed)
	}
	return resp.StatusCode, raw
}
func TestMeetingHTTPAuthenticatesBeforeScopeAndRejectsUnboundedQueries(t *testing.T) {
	srv, s, b, path := server(t)
	if status, raw := request(t, srv, "GET", path, ""); status != 200 || string(raw) != `{"meetings":[]}` {
		t.Fatal(status, string(raw))
	}
	if status, _ := request(t, srv, "GET", path+"?project_id=other", ""); status != 400 || s.reads != 1 {
		t.Fatal(status, s.reads)
	}
	b.deny = true
	if status, _ := request(t, srv, "GET", "/api/v1/projects/invalid/meetings", ""); status != 403 || s.reads != 1 {
		t.Fatal(status, s.reads)
	}
}
func TestStartHTTPExactBodyAndReadonlyLookup(t *testing.T) {
	srv, s, _, path := server(t)
	agent := fresh[i.Agent](t).String()
	valid := `{"agents":["` + agent + `"],"mode":"parallel","content_nodes":[{"type":"text","text":"discuss"}]}`
	bodies := []string{strings.Replace(valid, `"mode":"parallel"`, `"mode":"parallel","mode":"sequential"`, 1), strings.Replace(valid, `"mode"`, `"Mode"`, 1), strings.Replace(valid, `"text":"discuss"`, `"text":"discuss","project_id":"other"`, 1), valid}
	for n, body := range bodies {
		status, raw := request(t, srv, "POST", path, body)
		want := 400
		if n == len(bodies)-1 {
			want = 200
		}
		if status != want {
			t.Fatal(n, status, string(raw))
		}
	}
	if s.starts != 1 || s.start.Meta.IdempotencyKey != "original-meeting-command" || s.start.ProjectID.String() != strings.Split(path, "/")[4] {
		t.Fatal(s.starts, s.start.Meta.IdempotencyKey)
	}
	status, raw := request(t, srv, "POST", path+"/start/lookup", valid)
	if status != 200 || string(raw) != `{"status":"not_found"}` || s.starts != 1 || s.lookups != 1 {
		t.Fatal(status, string(raw), s.starts, s.lookups)
	}
}
func TestSendHTTPKeepsPreciseVersionAndOriginalPayload(t *testing.T) {
	srv, s, _, base := server(t)
	path := base + "/" + s.receipt.MeetingID.String() + "/turns"
	body := `{"expected_version":"9007199254740993","mode":"sequential","content_nodes":[{"type":"text","text":"next"}],"interrupt":false}`
	status, raw := request(t, srv, "POST", path, body)
	if status != 200 || s.sends != 1 || s.send.ExpectedVersion != 9007199254740993 || s.send.Meta.ExpectedVersion != nil {
		t.Fatal(status, string(raw), s.sends)
	}
	s.found = true
	status, raw = request(t, srv, "POST", path+"/lookup", body)
	var out struct {
		Status  string    `json:"status"`
		Receipt c.Receipt `json:"receipt"`
	}
	if json.Unmarshal(raw, &out) != nil || status != 200 || out.Status != "committed" || s.sends != 1 || s.lookups != 1 {
		t.Fatal(status, string(raw))
	}
	if status, _ = request(t, srv, "POST", path, strings.Replace(body, `"9007199254740993"`, `9007199254740993`, 1)); status != 400 || s.sends != 1 {
		t.Fatal(status)
	}
}

func TestMeetingControlAndDecisionUseOriginalLookup(t *testing.T) {
	srv, s, _, path := server(t)
	path += "/" + s.receipt.MeetingID.String()
	turn, decision := fresh[c.Turn](t), fresh[c.Decision](t)
	control := `{"action":"cancel-turn","expected_version":"3","turn_id":"` + turn.String() + `"}`
	if status, _ := request(t, srv, "POST", path+"/controls", control); status != 200 || s.controls != 1 || s.control.TurnID == nil || *s.control.TurnID != turn {
		t.Fatal("cancel route", status)
	}
	if status, _ := request(t, srv, "POST", path+"/controls/lookup", control); status != 200 || s.controls != 1 || s.lookups != 1 {
		t.Fatal("control lookup dispatched mutation", status)
	}
	answer := `{"decision_id":"` + decision.String() + `","action":"answer","expected_version":"1","answer":"Proceed with B"}`
	if status, _ := request(t, srv, "POST", path+"/decisions", answer); status != 200 || s.decisions != 1 || s.decision.DecisionID != decision || s.decision.ExpectedVersion != 1 || s.decision.Answer == nil || *s.decision.Answer != "Proceed with B" {
		t.Fatal("answer route", status)
	}
	if status, _ := request(t, srv, "POST", path+"/decisions/lookup", answer); status != 200 || s.decisions != 1 || s.lookups != 2 {
		t.Fatal("decision lookup dispatched mutation", status)
	}
	for _, body := range []string{strings.Replace(answer, `"action":"answer"`, `"action":"skip"`, 1), strings.Replace(answer, `"answer":"Proceed with B"`, `"answer":" "`, 1), strings.Replace(answer, `"action":"answer"`, `"action":"resume"`, 1), strings.Replace(answer, `"action":"answer"`, `"action":"answer","execution_id":"`+fresh[i.Execution](t).String()+`"`, 1)} {
		if status, _ := request(t, srv, "POST", path+"/decisions", body); status != 400 || s.decisions != 1 {
			t.Fatal("invalid decision mutated", status)
		}
	}
}
