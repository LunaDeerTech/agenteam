//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	workhttp "github.com/LunaDeerTech/agenteam/internal/central/work/http"
)

func TestTaskHumanHTTP(t *testing.T) {
	t.Run("transfer-lookup-replay-and-get", func(t *testing.T) {
		v := newTaskHumanHTTPFixture(t)
		browser := v.domain.base.ownerBrowser
		body, lookup, key := v.intent(t)
		reply := v.request(t, browser, "POST", v.transferPath(), body, key)
		receipt := v.requireTransition(t, reply)
		stored, err := v.domain.committedFacts(ctxFor(t), v.domain.base.raw, key)
		if err != nil || !bytes.Equal(jsonBytes(t, receipt), jsonBytes(t, stored)) {
			t.Fatal("HTTP receipt differs from actual Task/history/Outbox/command facts", err)
		}
		before := v.domain.databaseSnapshot(t)
		found := v.request(t, browser, "POST", v.lookupPath(), lookup, key)
		v.requireLookup(t, found, receipt)
		replay := v.request(t, browser, "POST", v.transferPath(), body, key)
		if replay.status != http.StatusOK || !bytes.Equal(replay.body, reply.body) {
			t.Fatal("HTTP same-key replay changed the original receipt")
		}
		current := v.request(t, browser, "GET", v.taskPath(), "", "")
		var task wc.Task
		if current.status != http.StatusOK || json.Unmarshal(current.body, &task) != nil || !bytes.Equal(jsonBytes(t, task), jsonBytes(t, receipt.Task)) {
			t.Fatal("HTTP current Task differs from the original committed transfer")
		}
		if v.domain.databaseSnapshot(t) != before {
			t.Fatal("Lookup/replay/Get changed Task, order, history, Outbox or Activity")
		}
	})
	t.Run("owner-csrf-and-new-session-lookup", func(t *testing.T) {
		v := newTaskHumanHTTPFixture(t)
		base := v.domain.base
		body, lookup, key := v.intent(t)
		before := v.domain.databaseSnapshot(t)
		wrongCSRF := base.ownerBrowser
		wrongCSRF.csrf = "invalid-task-http-csrf"
		for _, pathBody := range [][2]string{{v.transferPath(), body}, {v.lookupPath(), lookup}} {
			v.requireProblem(t, v.request(t, wrongCSRF, "POST", pathBody[0], pathBody[1], key), 403, f.CSRFFailed)
			v.requireProblem(t, v.request(t, base.otherBrowser, "POST", pathBody[0], pathBody[1], key), 404, f.NotFound)
		}
		if v.domain.databaseSnapshot(t) != before {
			t.Fatal("rejected browser mutated protected Task facts")
		}
		var planned int64
		if err := base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_work.task_transition_commands WHERE project_id=$1`, base.project.ID.String()).Scan(&planned); err != nil || planned != 0 {
			t.Fatal("rejected browser created a transition command", err)
		}
		// Keep the full original intent before sending. Later recovery uses it,
		// not the current Task/version or a digest exposed by a success response.
		receipt := v.requireTransition(t, v.request(t, base.ownerBrowser, "POST", v.transferPath(), body, key))
		changed := strings.Replace(body, `"target_state":"todo"`, `"target_state":"blocked"`, 1)
		if changed == body {
			t.Fatal("different-intent control did not change the original request")
		}
		v.requireProblem(t, v.request(t, base.ownerBrowser, "POST", v.transferPath(), changed, key), 409, f.IdempotencyKeyReused)
		if err := base.core.Logout(ctxFor(t), account.LogoutRequest{Actor: base.ownerBrowser.actor, Key: "task-http-logout"}); err != nil {
			t.Fatal("formal Logout failed", err)
		}
		for _, pathBody := range [][2]string{{v.transferPath(), body}, {v.lookupPath(), lookup}} {
			v.requireProblem(t, v.request(t, base.ownerBrowser, "POST", pathBody[0], pathBody[1], key), 401, f.SessionRevoked)
		}
		fresh := base.login(t, base.ownerBrowser.email)
		if fresh.actor.Details().UserID != base.ownerBrowser.actor.Details().UserID || fresh.actor.Details().SessionID == base.ownerBrowser.actor.Details().SessionID {
			t.Fatal("recovery did not use a new real Session of the same User")
		}
		beforeRecovery := v.domain.databaseSnapshot(t)
		activity := func() string {
			t.Helper()
			var value string
			if err := base.raw.QueryRow(ctxFor(t), `SELECT last_activity_at::text FROM agenteam_account.sessions WHERE id=$1::uuid`, fresh.actor.Details().SessionID).Scan(&value); err != nil {
				t.Fatal("current recovery Session activity unavailable", err)
			}
			return value
		}
		beforeActivity := activity()
		v.requireLookup(t, v.request(t, fresh, "POST", v.lookupPath(), lookup, key), receipt)
		if v.domain.databaseSnapshot(t) != beforeRecovery || activity() != beforeActivity {
			t.Fatal("current-session Lookup rewrote the original committed business facts")
		}
		stored, err := v.domain.committedFacts(ctxFor(t), base.raw, key)
		if err != nil || !bytes.Equal(jsonBytes(t, stored), jsonBytes(t, receipt)) {
			t.Fatal("new Session lookup lost the original committed facts", err)
		}
	})
}

type taskHumanHTTPFixture struct {
	domain *taskTransitionFixture
	server *httptest.Server
	done   chan bool
}

// Reuse the real Account -> P2 Object/Skill initializer -> default-enabled
// Agent chain. Only the Work HTTP graph below is new; no successful business
// fact, Agent, empty occupancy or current Sprint is seeded through SQL.
func newTaskHumanHTTPFixture(t *testing.T) *taskHumanHTTPFixture {
	t.Helper()
	a := newAgentCreateFixture(t)
	request, commandMeta, _ := a.request(t)
	receipt, err := a.agents.CreateAgent(ctxFor(t), a.p2.base.ownerBrowser.actor, commandMeta, a.p2.project.ID, request)
	if err != nil || receipt.Validate() != nil || receipt.Fields().Agent.Fields().Core.ID != request.Fields().AgentID {
		t.Fatal("formal Agent creation for HTTP assignment", err)
	}
	base := *a.p2.base
	base.project, base.projectAuthority = a.p2.project, a.providers.Projects
	d := &taskTransitionFixture{agent: a, base: &base, agentID: request.Fields().AgentID}
	d.authority, err = work.NewAuthority(base.tracked, base.projectAuthority)
	if err != nil {
		t.Fatal(err)
	}
	catalog := event.NewCatalog()
	structures, err := wc.RegisterWorkEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := wc.RegisterTaskEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	blockerEvents, err := wc.RegisterTaskBlockerEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	d.transitionEvents, err = wc.RegisterTaskTransitionEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	d.box, err = outbox.New(base.tracked, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{wc.WorkProducer: d.authority},
		Sessions:  base.accounts, System: base.accounts, Projects: base.projectAuthority,
		Audit: base.audit, Cursors: base.keys, Processes: fixtureProcess{id[oc.Process](t)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var owned []interface {
		Stop()
		Drain(context.Context) error
	}
	t.Cleanup(func() {
		for _, service := range owned {
			service.Stop()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		for _, service := range owned {
			if err := service.Drain(ctx); err != nil {
				t.Error("original Work HTTP service did not join", err)
			}
		}
	})
	d.structure, err = work.New(base.tracked, work.Dependencies{Authority: d.authority, Events: d.box, WorkEvents: structures, Activity: base.accounts})
	if err != nil {
		t.Fatal(err)
	}
	owned = append(owned, d.structure)
	d.structureReader, err = work.NewReader(base.tracked, d.authority, base.keys)
	if err != nil {
		t.Fatal(err)
	}
	d.taskReader, err = work.NewTaskReader(base.tracked, d.authority, d.structureReader, base.keys)
	if err != nil {
		t.Fatal(err)
	}
	d.pending, err = scheduler.NewPendingAuthority(base.tracked)
	if err != nil {
		t.Fatal(err)
	}
	d.tasks, err = work.NewTask(base.tracked, work.TaskDependencies{Authority: d.authority, Structure: d.structureReader, Events: d.box, TaskEvents: tasks, Activity: base.accounts, Pending: d.pending})
	if err != nil {
		t.Fatal(err)
	}
	owned = append(owned, d.tasks)
	blockers, err := work.NewBlocker(base.tracked, work.BlockerDependencies{Authority: d.authority, Structure: d.structureReader, Events: d.box, BlockerEvents: blockerEvents, Activity: base.accounts})
	if err != nil {
		t.Fatal(err)
	}
	owned = append(owned, blockers)
	blockerReader, err := work.NewBlockerReader(base.tracked, d.authority, base.keys)
	if err != nil {
		t.Fatal(err)
	}
	occupancy, err := execution.NewWorkOccupancy(base.tracked)
	if err != nil {
		t.Fatal(err)
	}
	d.transitions, err = work.NewTaskTransition(base.tracked, work.TaskTransitionDependencies{
		Structure: d.structureReader, Authority: d.authority, Events: d.box, TaskEvents: d.transitionEvents,
		Activity: base.accounts, Agents: a.providers.Agents, Occupancy: occupancy, Pending: d.pending,
	})
	if err != nil {
		t.Fatal(err)
	}
	owned = append(owned, d.transitions)

	// Bind before starting TLS; the real Account boundary owns this exact
	// loopback HTTPS origin. Server.Client trusts only the owned test certificate.
	s := httptest.NewUnstartedServer(nil)
	v := &taskHumanHTTPFixture{domain: d, server: s, done: make(chan bool, 1)}
	t.Cleanup(s.Close) // actual Serve/connection/handler Wait, before Work Drain
	boundary, err := account.NewHTTPBoundary(base.core, "https://"+s.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := workhttp.NewHTTPHandler(workhttp.Bindings{
		Structure: d.structure, StructureReader: d.structureReader, Tasks: d.tasks, TaskReader: d.taskReader,
		Blockers: blockers, BlockerReader: blockerReader, Transitions: d.transitions,
	}, boundary)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := httpapi.Handler(nil, handler)
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		normal := false
		defer func() { v.done <- normal }()
		wrapped.ServeHTTP(w, r)
		normal = true
	})
	s.StartTLS()
	if s.Client().Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("TLS fixture disabled certificate verification")
	}

	milestone := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: "HTTP assignment"}
	if _, err = d.structure.CreateMilestone(ctxFor(t), base.ownerBrowser.actor, meta(t, "http-milestone", nil), base.project.ID, milestone); err != nil {
		t.Fatal(err)
	}
	sprint := wc.CreateSprintRequest{SprintID: id[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "Planned, no Scheduler"}
	if _, err = d.structure.CreateSprint(ctxFor(t), base.ownerBrowser.actor, meta(t, "http-sprint", nil), base.project.ID, sprint); err != nil {
		t.Fatal(err)
	}
	create := wc.TaskCreate{TaskID: id[wc.Task](t), SprintID: sprint.SprintID, Title: "Assign through real HTTPS", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Description: "Current Human Owner", Plan: "No Execution is launched"}
	body := string(jsonBytes(t, map[string]any{"request": create}))
	response := v.request(t, base.ownerBrowser, "POST", variableHTTPPath(base.project.ID, "/tasks"), body, "http-task-create")
	var created wc.TaskMutation
	if response.status != 200 || json.Unmarshal(response.body, &created) != nil || created.Validate() != nil || !created.Changed || created.Task.ID != create.TaskID || created.Task.State != wc.TaskStateBacklog || created.Task.AssigneeAgentID != nil || created.Task.Version != 1 {
		t.Fatal("real HTTP Create did not publish the original unassigned backlog Task")
	}
	d.task = created.Task
	return v
}

func (v *taskHumanHTTPFixture) taskPath() string {
	return variableHTTPPath(v.domain.base.project.ID, "/tasks/"+v.domain.task.ID.String())
}
func (v *taskHumanHTTPFixture) transferPath() string { return v.taskPath() + "/transfer" }
func (v *taskHumanHTTPFixture) lookupPath() string {
	return variableHTTPPath(v.domain.base.project.ID, "/task-transition-commands/lookup")
}
func (v *taskHumanHTTPFixture) intent(t *testing.T) (string, string, f.IdempotencyKey) {
	t.Helper()
	request, meta, _ := v.domain.transferRequest(t)
	body := jsonBytes(t, map[string]any{"expected_version": *meta.ExpectedVersion, "request": request})
	lookup := jsonBytes(t, map[string]any{"command": wc.TaskTransitionTransfer, "target_id": v.domain.task.ID, "expected_version": *meta.ExpectedVersion, "request": request})
	return string(body), string(lookup), meta.IdempotencyKey
}

func (v *taskHumanHTTPFixture) request(t *testing.T, browser variableHTTPBrowser, method, path, body string, key f.IdempotencyKey) variableHTTPResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := *v.server.Client()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar = jar
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	origin, err := url.Parse(v.server.URL)
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(origin, []*http.Cookie{{Name: "__Host-agenteam_session", Value: browser.cookie, Secure: true, HttpOnly: true, Path: "/", SameSite: http.SameSiteLaxMode}})
	r, err := http.NewRequestWithContext(ctx, method, v.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", v.server.URL)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if method != "GET" && method != "HEAD" {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", browser.csrf)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", string(key))
	}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal("owned TLS request did not return a response")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 || response.ContentLength != int64(len(raw)) || response.TLS == nil || len(response.TLS.VerifiedChains) == 0 {
		t.Fatal("original TLS response did not complete bounded EOF/Close/Content-Length")
	}
	select {
	case normal := <-v.done:
		if !normal {
			t.Fatal("original HTTP handler aborted after response")
		}
	case <-ctx.Done():
		t.Fatal("original HTTP handler did not return after response EOF")
	}
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Request-ID") == "" {
		t.Fatal("original response security metadata missing")
	}
	return variableHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: raw}
}

func (v *taskHumanHTTPFixture) requireTransition(t *testing.T, reply variableHTTPResponse) wc.TaskTransitionMutation {
	t.Helper()
	r, err := wc.DecodeTaskTransitionMutation(reply.body)
	if reply.status != 200 || err != nil || r.Task.ID != v.domain.task.ID || r.Task.ProjectID != v.domain.base.project.ID || r.Task.Version != 2 || r.Task.State != wc.TaskStateTodo || r.Task.AssigneeAgentID == nil || *r.Task.AssigneeAgentID != v.domain.agentID {
		t.Fatal("HTTP transfer did not confirm the exact current-Owner assignment")
	}
	return r
}
func (v *taskHumanHTTPFixture) requireLookup(t *testing.T, reply variableHTTPResponse, original wc.TaskTransitionMutation) {
	t.Helper()
	found, err := wc.DecodeTaskTransitionLookup(reply.body)
	if reply.status != 200 || err != nil || found.Status != wc.LookupCommitted || found.Receipt == nil || !bytes.Equal(jsonBytes(t, *found.Receipt), jsonBytes(t, original)) {
		t.Fatal("HTTP Lookup lost the original receipt")
	}
}
func (v *taskHumanHTTPFixture) requireProblem(t *testing.T, reply variableHTTPResponse, status int, code f.Code) {
	t.Helper()
	var problem struct {
		Code f.Code `json:"code"`
	}
	if reply.status != status || json.Unmarshal(reply.body, &problem) != nil || problem.Code != code || strings.Contains(string(reply.body), "invalid-task-http-csrf") {
		t.Fatal("HTTP rejection did not preserve its safe typed error", reply.status, problem.Code)
	}
}
