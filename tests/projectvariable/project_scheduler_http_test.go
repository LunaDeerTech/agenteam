//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	projecthttp "github.com/LunaDeerTech/agenteam/internal/central/project/http"
	"github.com/LunaDeerTech/agenteam/internal/central/project/schedulerhttp"
)

// The existing fixture supplies real Account, Project, Audit and Outbox services
// over its owned PostgreSQL database. Its persistent test Skills initializer is
// explicit: this does not establish production Project creation or a Scheduler
// runtime. Requests below use an owned TLS listener and the original handlers.
func TestProjectSchedulerOwnerHTTP(t *testing.T) {
	t.Run("configuration-historical-receipts-and-facts", func(t *testing.T) {
		v := newProjectSchedulerHTTPFixture(t)
		owner := v.base.ownerBrowser
		path := variableHTTPPath(v.base.project.ID, "/scheduler")
		lookupPath := variableHTTPPath(v.base.project.ID, "/commands/lookup")
		before := v.snapshot(t)
		initial := v.request(t, owner, "GET", path, "", "")
		v.requireCurrent(t, initial, 1, false, nil)
		head := v.request(t, owner, "HEAD", path, "", "")
		if head.status != 200 || len(head.body) != 0 || head.header.Get("Content-Length") != initial.header.Get("Content-Length") || head.header.Get("Content-Type") != initial.header.Get("Content-Type") {
			t.Fatal("HEAD lost the authenticated current representation or its length")
		}
		v.requireLookup(t, v.request(t, owner, "POST", lookupPath, `{"command":"update"}`, "scheduler-unobserved"), "scheduler-unobserved", 0)
		if v.snapshot(t) != before {
			t.Fatal("initial GET/HEAD or absent Lookup changed business facts or Session activity")
		}

		// A value beyond JavaScript's exact integer range stays a decimal string.
		limit := "9223372036854775807"
		original := projectSchedulerHTTPBody(t, 1, true, &limit)
		key := f.IdempotencyKey("scheduler-enable-bounded")
		first := v.request(t, owner, "PATCH", path, original, key)
		v.requireReceipt(t, first, 2)
		v.requireCommand(t, key, 2, []string{"scheduler_enabled", "scheduler_max_concurrency"})
		v.requireCurrent(t, v.request(t, owner, "GET", path, "", ""), 2, true, &limit)
		v.requireCounts(t, 1, 1)

		noopKey := f.IdempotencyKey("scheduler-noop")
		noop := projectSchedulerHTTPBody(t, 2, true, &limit)
		projectBefore := v.projectSnapshot(t)
		v.requireReceipt(t, v.request(t, owner, "PATCH", path, noop, noopKey), 2)
		v.requireCommand(t, noopKey, 2, nil)
		v.requireCounts(t, 2, 1)
		if v.projectSnapshot(t) != projectBefore {
			t.Fatal("no-op rewrote Project version, timestamps or configuration")
		}

		pauseKey := f.IdempotencyKey("scheduler-pause-unlimited")
		v.requireReceipt(t, v.request(t, owner, "PATCH", path, projectSchedulerHTTPBody(t, 2, false, nil), pauseKey), 3)
		v.requireCommand(t, pauseKey, 3, []string{"scheduler_enabled", "scheduler_max_concurrency"})
		v.requireCurrent(t, v.request(t, owner, "GET", path, "", ""), 3, false, nil)
		resumeKey := f.IdempotencyKey("scheduler-resume-unlimited")
		v.requireReceipt(t, v.request(t, owner, "PATCH", path, projectSchedulerHTTPBody(t, 3, true, nil), resumeKey), 4)
		v.requireCommand(t, resumeKey, 4, []string{"scheduler_enabled"})
		v.requireCounts(t, 4, 3)

		// Neither key-only Lookup nor replay can project today's configuration
		// into the historical acknowledgement. Only the explicit GET is current.
		before = v.snapshot(t)
		v.requireLookup(t, v.request(t, owner, "POST", lookupPath, `{"command":"update"}`, key), key, 2)
		v.requireLookup(t, v.request(t, owner, "POST", lookupPath, `{"command":"update"}`, noopKey), noopKey, 2)
		replay := v.request(t, owner, "PATCH", path, original, key)
		v.requireReceipt(t, replay, 2)
		if !bytes.Equal(replay.body, first.body) {
			t.Fatal("original-key replay replaced its historical acknowledgement")
		}
		v.requireReceipt(t, v.request(t, owner, "PATCH", path, noop, noopKey), 2)
		v.requireCurrent(t, v.request(t, owner, "GET", path, "", ""), 4, true, nil)
		v.requireProblem(t, v.request(t, owner, "PATCH", path, projectSchedulerHTTPBody(t, 1, false, nil), "scheduler-stale"), 409, f.VersionConflict)
		v.requireProblem(t, v.request(t, owner, "PATCH", path, projectSchedulerHTTPBody(t, 1, false, nil), key), 409, f.IdempotencyKeyReused)
		// The original metadata handler shares UpdateCommand and its key space.
		v.requireProblem(t, v.request(t, owner, "PATCH", variableHTTPPath(v.base.project.ID, ""), `{"expected_version":"1","description":"different intent"}`, key), 409, f.IdempotencyKeyReused)
		if v.snapshot(t) != before {
			t.Fatal("Lookup/replay/current reads or rejected writes changed original facts")
		}
		v.requireCounts(t, 4, 3)
	})

	t.Run("current-session-owner-and-project-gates", func(t *testing.T) {
		v := newProjectSchedulerHTTPFixture(t)
		owner := v.base.ownerBrowser
		otherProject, _, _ := v.base.createProject(t, v.base.otherBrowser.actor, "scheduler-other")
		path := variableHTTPPath(v.base.project.ID, "/scheduler")
		lookupPath := variableHTTPPath(v.base.project.ID, "/commands/lookup")
		body := projectSchedulerHTTPBody(t, 1, true, nil)
		key := f.IdempotencyKey("scheduler-current-session")
		before := v.snapshot(t)
		for _, browser := range []variableHTTPBrowser{v.base.otherBrowser, v.base.adminBrowser} {
			v.requireProblem(t, v.request(t, browser, "GET", path, "", ""), 404, f.NotFound)
			v.requireHeadError(t, v.request(t, browser, "HEAD", path, "", ""), 404)
			v.requireProblem(t, v.request(t, browser, "PATCH", path, body, key), 404, f.NotFound)
			v.requireProblem(t, v.request(t, browser, "POST", lookupPath, `{"command":"update"}`, key), 404, f.NotFound)
		}
		otherPath := variableHTTPPath(otherProject.ID, "/scheduler")
		otherBefore := v.projectSnapshotID(t, otherProject.ID)
		v.requireProblem(t, v.request(t, owner, "GET", otherPath, "", ""), 404, f.NotFound)
		v.requireProblem(t, v.request(t, owner, "PATCH", otherPath, body, key), 404, f.NotFound)
		wrongCSRF := owner
		wrongCSRF.csrf = "invalid-scheduler-csrf"
		v.requireProblem(t, v.request(t, wrongCSRF, "PATCH", path, body, key), 403, f.CSRFFailed)
		v.requireProblem(t, v.request(t, wrongCSRF, "POST", lookupPath, `{"command":"update"}`, key), 403, f.CSRFFailed)
		if v.snapshot(t) != before || v.projectSnapshotID(t, otherProject.ID) != otherBefore {
			t.Fatal("unauthorized configuration access changed business facts or Session activity")
		}
		var crossProjectWrites int64
		if err := v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update')+
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='project.update')+
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1 AND event_type='project.updated')`, otherProject.ID.String()).Scan(&crossProjectWrites); err != nil || crossProjectWrites != 0 {
			t.Fatal("cross-Project rejection retained a command, Audit or event")
		}
		v.requireReceipt(t, v.request(t, owner, "PATCH", path, body, key), 2)
		v.requireCommand(t, key, 2, []string{"scheduler_enabled"})
		if err := v.base.core.Logout(ctxFor(t), account.LogoutRequest{Actor: owner.actor, Key: "scheduler-logout"}); err != nil {
			t.Fatal("formal Scheduler Owner Logout failed")
		}
		before = v.snapshot(t)
		v.requireProblem(t, v.request(t, owner, "GET", path, "", ""), 401, f.SessionRevoked)
		v.requireHeadError(t, v.request(t, owner, "HEAD", path, "", ""), 401)
		v.requireProblem(t, v.request(t, owner, "PATCH", path, body, key), 401, f.SessionRevoked)
		v.requireProblem(t, v.request(t, owner, "POST", lookupPath, `{"command":"update"}`, key), 401, f.SessionRevoked)
		if v.snapshot(t) != before {
			t.Fatal("revoked Session changed original configuration or command facts")
		}
		fresh := v.base.login(t, owner.email)
		if fresh.actor.Details().UserID != owner.actor.Details().UserID || fresh.actor.Details().SessionID == owner.actor.Details().SessionID {
			t.Fatal("recovery did not use a new real Session of the same Owner")
		}
		before = v.snapshot(t)
		v.requireLookup(t, v.request(t, fresh, "POST", lookupPath, `{"command":"update"}`, key), key, 2)
		v.requireReceipt(t, v.request(t, fresh, "PATCH", path, body, key), 2)
		v.requireCurrent(t, v.request(t, fresh, "GET", path, "", ""), 2, true, nil)
		if v.snapshot(t) != before {
			t.Fatal("new Session recovery rewrote the original command or Project")
		}
		v.requireCounts(t, 1, 1)
	})
}

type projectSchedulerHTTPFixture struct {
	base   *variableHTTPFixture
	server *httptest.Server
	done   chan bool
}

func newProjectSchedulerHTTPFixture(t *testing.T) *projectSchedulerHTTPFixture {
	t.Helper()
	base := newVariableHTTPFixture(t)
	s := httptest.NewUnstartedServer(nil)
	v := &projectSchedulerHTTPFixture{base: base, server: s, done: make(chan bool, 1)}
	// Close waits for the original server/handlers before the existing fixture
	// drains Project, Variable, Account, delivery and owned Store resources.
	t.Cleanup(s.Close)
	boundary, err := account.NewHTTPBoundary(base.core, "https://"+s.Listener.Addr().String())
	if err != nil {
		t.Fatal("Scheduler Account boundary construction failed")
	}
	scheduler, err := schedulerhttp.NewHTTPHandler(base.projects, boundary)
	if err != nil {
		t.Fatal("Scheduler handler construction failed")
	}
	updates, err := projecthttp.NewUpdateHTTPHandler(base.projects, boundary)
	if err != nil {
		t.Fatal("original Project Update/Lookup construction failed")
	}
	wrapped := httpapi.Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if schedulerhttp.HandlesPath(r.URL.Path) {
			scheduler.ServeHTTP(w, r)
		} else {
			updates.ServeHTTP(w, r)
		}
	}))
	s.Config.ErrorLog = log.New(io.Discard, "", 0)
	s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		normal := false
		defer func() { v.done <- normal }()
		wrapped.ServeHTTP(w, r)
		normal = true
	})
	s.StartTLS()
	transport := s.Client().Transport.(*http.Transport)
	if transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned TLS certificate verification disabled")
	}
	t.Cleanup(transport.CloseIdleConnections)
	return v
}

func (v *projectSchedulerHTTPFixture) request(t *testing.T, browser variableHTTPBrowser, method, path, body string, key f.IdempotencyKey) variableHTTPResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, method, v.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal("owned Scheduler request construction failed")
	}
	rid := id[f.Request](t)
	r.Header.Set("X-Request-ID", rid.String())
	r.Header.Set("Origin", v.server.URL)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: browser.cookie, Secure: true, HttpOnly: true, Path: "/"})
	if method != http.MethodGet && method != http.MethodHead {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", browser.csrf)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", string(key))
	}
	client := *v.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(r)
	if err != nil {
		t.Fatal("original Scheduler TLS request failed")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 64<<10 || response.TLS == nil || len(response.TLS.VerifiedChains) == 0 {
		t.Fatal("original Scheduler TLS response did not reach bounded EOF/Close")
	}
	if method == http.MethodHead {
		if len(raw) != 0 || response.ContentLength <= 0 {
			t.Fatal("HEAD did not retain representation length and an empty body")
		}
	} else if response.ContentLength != int64(len(raw)) {
		t.Fatal("response length differs from original complete body")
	}
	select {
	case normal := <-v.done:
		if !normal {
			t.Fatal("original Scheduler HTTP handler aborted after response")
		}
	case <-ctx.Done():
		t.Fatal("original Scheduler HTTP handler did not return")
	}
	serverRequestID, parseErr := f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
	if parseErr != nil || serverRequestID.String() != response.Header.Get("X-Request-ID") || serverRequestID == rid || response.Header.Get("Cache-Control") != "no-store" || len(response.Header.Values("X-Request-ID")) != 1 {
		t.Fatal("Scheduler response lost its server-owned request identity or cache policy")
	}
	return variableHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: raw}
}

func projectSchedulerHTTPBody(t *testing.T, version f.Version, enabled bool, limit *string) string {
	t.Helper()
	return string(jsonBytes(t, map[string]any{"expected_version": version, "scheduler_enabled": enabled, "scheduler_max_concurrency": limit}))
}

func projectSchedulerHTTPDecode(t *testing.T, reply variableHTTPResponse, target any, members int) {
	t.Helper()
	requireHTTP(t, reply)
	if reply.header.Get("Content-Type") != "application/json" {
		t.Fatal("Scheduler success did not return the declared JSON media type")
	}
	decoder := json.NewDecoder(bytes.NewReader(reply.body))
	decoder.DisallowUnknownFields()
	var fields map[string]json.RawMessage
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF || json.Unmarshal(reply.body, &fields) != nil || len(fields) != members {
		t.Fatal("Scheduler success lost its exact closed wire representation")
	}
}

func (v *projectSchedulerHTTPFixture) requireCurrent(t *testing.T, reply variableHTTPResponse, version f.Version, enabled bool, limit *string) {
	t.Helper()
	var current struct {
		ProjectID pc.ProjectID `json:"project_id"`
		Version   f.Version    `json:"version"`
		Enabled   bool         `json:"scheduler_enabled"`
		Limit     *string      `json:"scheduler_max_concurrency"`
	}
	projectSchedulerHTTPDecode(t, reply, &current, 4)
	if current.ProjectID != v.base.project.ID || current.Version != version || current.Enabled != enabled || (current.Limit == nil) != (limit == nil) || limit != nil && *current.Limit != *limit {
		t.Fatal("GET differs from expected current configuration")
	}
	var storedVersion int64
	var storedEnabled bool
	var storedLimit *int64
	if err := v.base.raw.QueryRow(ctxFor(t), `SELECT version,scheduler_enabled,scheduler_max_concurrency FROM agenteam_project.projects WHERE id=$1`, v.base.project.ID.String()).Scan(&storedVersion, &storedEnabled, &storedLimit); err != nil || storedVersion != int64(version) || storedEnabled != enabled || (storedLimit == nil) != (limit == nil) || limit != nil && strconv.FormatInt(*storedLimit, 10) != *limit {
		t.Fatal("current HTTP configuration differs from original Project row")
	}
}

func (v *projectSchedulerHTTPFixture) requireReceipt(t *testing.T, reply variableHTTPResponse, version f.Version) {
	t.Helper()
	var ack struct {
		ProjectID pc.ProjectID `json:"project_id"`
		Version   f.Version    `json:"version"`
	}
	projectSchedulerHTTPDecode(t, reply, &ack, 2)
	if ack.ProjectID != v.base.project.ID || ack.Version != version {
		t.Fatal("Scheduler acknowledgement changed original Project/version")
	}
}

func (v *projectSchedulerHTTPFixture) requireLookup(t *testing.T, reply variableHTTPResponse, key f.IdempotencyKey, version f.Version) {
	t.Helper()
	var lookup struct {
		State  pc.LookupState    `json:"state"`
		Result *pc.CommandResult `json:"result,omitempty"`
	}
	members := 2
	if version == 0 {
		members = 1
	}
	projectSchedulerHTTPDecode(t, reply, &lookup, members)
	if version == 0 {
		if lookup.State != pc.LookupNotObserved || lookup.Result != nil {
			t.Fatal("unobserved key fabricated a command result")
		}
		return
	}
	if lookup.State != pc.LookupCommitted || lookup.Result == nil || lookup.Result.Validate() != nil || lookup.Result.Command != pc.UpdateCommand || lookup.Result.Project == nil || lookup.Result.Project.ID != v.base.project.ID || lookup.Result.Project.Version != version || bytes.Contains(reply.body, []byte("scheduler_")) {
		t.Fatal("key-only Lookup differs from historical Project acknowledgement")
	}
	stored := v.commandResult(t, key)
	if !bytes.Equal(jsonBytes(t, *lookup.Result), jsonBytes(t, stored)) {
		t.Fatal("Lookup differs from original persisted command result")
	}
}

func (v *projectSchedulerHTTPFixture) requireProblem(t *testing.T, reply variableHTTPResponse, status int, code f.Code) {
	t.Helper()
	problem := requireProblem(t, reply, code)
	if reply.status != status || problem.RequestID.String() != reply.header.Get("X-Request-ID") {
		t.Fatal("Scheduler Problem status/request identity mismatch")
	}
}

func (v *projectSchedulerHTTPFixture) requireHeadError(t *testing.T, reply variableHTTPResponse, status int) {
	t.Helper()
	if reply.status != status || len(reply.body) != 0 || reply.header.Get("Content-Type") != "application/problem+json" {
		t.Fatal("HEAD lost current authorization or exposed an error body")
	}
}

func (v *projectSchedulerHTTPFixture) commandResult(t *testing.T, key f.IdempotencyKey) pc.CommandResult {
	t.Helper()
	var raw []byte
	if err := v.base.raw.QueryRow(ctxFor(t), `SELECT safe_result FROM agenteam_project.commands WHERE project_id=$1 AND command_name='update' AND key=$2 AND state='completed' AND committed_at IS NOT NULL`, v.base.project.ID.String(), string(key)).Scan(&raw); err != nil {
		t.Fatal("original completed Update command missing")
	}
	var result pc.CommandResult
	if json.Unmarshal(raw, &result) != nil || result.Validate() != nil {
		t.Fatal("persisted Project command result invalid")
	}
	return result
}

func (v *projectSchedulerHTTPFixture) requireCommand(t *testing.T, key f.IdempotencyKey, version f.Version, changed []string) {
	t.Helper()
	result := v.commandResult(t, key)
	if result.Command != pc.UpdateCommand || result.Project == nil || result.Project.ID != v.base.project.ID || result.Project.Version != version {
		t.Fatal("persisted Update result changed original version")
	}
	var audits, events int64
	var noEvent bool
	if err := v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_audit.audit_records a WHERE a.project_id=c.project_id AND a.cause_ref=c.audit_cause),
 (SELECT count(*) FROM agenteam_outbox.events e WHERE e.id=c.event_id),
 c.event_id IS NULL AND c.plan IS NULL AND c.event_ids='[]'::jsonb
 FROM agenteam_project.commands c WHERE c.project_id=$1 AND c.command_name='update' AND c.key=$2`, v.base.project.ID.String(), string(key)).Scan(&audits, &events, &noEvent); err != nil {
		t.Fatal("original command consequences unavailable")
	}
	if len(changed) == 0 {
		if audits != 0 || events != 0 || !noEvent {
			t.Fatal("no-op produced an Audit, event or update plan")
		}
		return
	}
	if audits != 1 || events != 1 || noEvent {
		t.Fatal("changed configuration lost exactly one original Audit/event")
	}
	metadata := string(jsonBytes(t, map[string]any{"project_id": v.base.project.ID, "initiator_id": v.base.ownerBrowser.actor.Details().UserID, "project_version": version, "changed_fields": changed}))
	payload := string(jsonBytes(t, map[string]any{"changed_fields": changed}))
	var matching int64
	if err := v.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_project.commands c
 JOIN agenteam_audit.audit_records a ON a.project_id=c.project_id AND a.cause_ref=c.audit_cause
 JOIN agenteam_outbox.events e ON e.id=c.event_id
 WHERE c.project_id=$1::text::uuid AND c.command_name='update' AND c.key=$2
 AND a.scope='project' AND a.producer='project' AND a.action='project.update' AND a.outcome='success'
 AND a.actor_kind='human' AND a.user_id=$3::text::uuid AND a.session_id=$4::text::uuid
 AND a.resource_kind='project' AND a.resource_id=c.project_id AND a.ordinal=0 AND a.metadata=$5::jsonb
 AND e.scope='project' AND e.project_id=c.project_id AND e.producer='project' AND e.event_type='project.updated'
 AND e.aggregate_type='project' AND e.aggregate_id=c.project_id AND e.aggregate_version=$6
 AND e.schema_version=1 AND convert_from(e.payload,'UTF8')::jsonb=$7::jsonb
 AND c.event_ids=jsonb_build_array(e.id::text)`, v.base.project.ID.String(), string(key), v.base.ownerBrowser.actor.Details().UserID, v.base.ownerBrowser.actor.Details().SessionID, metadata, int64(version), payload).Scan(&matching); err != nil || matching != 1 {
		t.Fatal("Audit/Outbox postimages differ from original actor, command, version or exact changed fields")
	}
}

func (v *projectSchedulerHTTPFixture) requireCounts(t *testing.T, commands, changed int64) {
	t.Helper()
	var values [7]int64
	if err := v.base.raw.QueryRow(ctxFor(t), `SELECT
 (SELECT count(*) FROM agenteam_project.commands WHERE project_id=$1::text::uuid AND command_name='update'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1::text::uuid AND action='project.update'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND event_type='project.updated'),
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.sprints WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text)`, v.base.project.ID.String()).Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6]); err != nil || values != ([7]int64{commands, changed, changed, 0, 0, 0, 0}) {
		t.Fatal("Scheduler configuration created unexpected commands, Audit, Outbox, Work or Execution facts")
	}
}

func (v *projectSchedulerHTTPFixture) projectSnapshotID(t *testing.T, project pc.ProjectID) string {
	t.Helper()
	var value string
	if err := v.base.raw.QueryRow(ctxFor(t), `SELECT to_jsonb(p)::text FROM agenteam_project.projects p WHERE id=$1`, project.String()).Scan(&value); err != nil {
		t.Fatal("original Project snapshot unavailable")
	}
	return value
}

func (v *projectSchedulerHTTPFixture) projectSnapshot(t *testing.T) string {
	return v.projectSnapshotID(t, v.base.project.ID)
}

func (v *projectSchedulerHTTPFixture) snapshot(t *testing.T) string {
	t.Helper()
	var value string
	if err := v.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'commands',(SELECT coalesce(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_project.commands c WHERE project_id=$1::text::uuid),
 'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb) FROM agenteam_audit.audit_records a WHERE project_id=$1::text::uuid),
 'events',(SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY id),'[]'::jsonb) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid),
 'activity',(SELECT coalesce(jsonb_agg(jsonb_build_object('id',id,'last_activity_at',last_activity_at) ORDER BY id),'[]'::jsonb) FROM agenteam_account.sessions WHERE user_id=$2::uuid)
 )::text`, v.base.project.ID.String(), v.base.ownerBrowser.actor.Details().UserID).Scan(&value); err != nil {
		t.Fatal("original Project command snapshot unavailable")
	}
	return value
}
