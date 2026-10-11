//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	agenthttp "github.com/LunaDeerTech/agenteam/internal/central/agent/http"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestAgentDirectoryHTTP(t *testing.T) {
	t.Run("normal-directory-pagination", func(t *testing.T) {
		v := newAgentDirectoryHTTPFixture(t)
		browser := v.agent.p2.base.ownerBrowser
		before := v.snapshot(t, browser)
		allReply := v.request(t, browser, http.MethodGet, v.listPath())
		all := v.page(t, allReply)
		if len(all.Items) != len(v.expected) || all.NextCursor != "" {
			t.Fatal("default directory did not return the actual initialized Agents")
		}
		for n, entry := range all.Items {
			v.requireEntry(t, jsonBytes(t, entry), v.expected[n])
		}
		cursor := ""
		for n, expected := range v.expected {
			path := v.listPath() + "?limit=1"
			if cursor != "" {
				path += "&cursor=" + url.QueryEscape(cursor)
			}
			page := v.page(t, v.request(t, browser, http.MethodGet, path))
			if len(page.Items) != 1 || page.Items[0].ID != expected.ID || (page.NextCursor == "") != (n == len(v.expected)-1) {
				t.Fatal("created_at/id descending cursor skipped, repeated or invented an Agent")
			}
			v.requireEntry(t, jsonBytes(t, page.Items[0]), expected)
			cursor = page.NextCursor
			get := v.request(t, browser, http.MethodGet, v.getPath(expected.ID))
			if get.status != http.StatusOK {
				t.Fatal("current directory Get did not return its Agent", get.status)
			}
			v.requireEntry(t, get.body, expected)
			head := v.request(t, browser, http.MethodHead, v.getPath(expected.ID))
			if head.status != http.StatusOK || len(head.body) != 0 || head.header.Get("Content-Length") != strconv.Itoa(len(get.body)) || head.header.Get("Content-Type") != get.header.Get("Content-Type") {
				t.Fatal("Agent HEAD did not retain the GET representation metadata")
			}
		}
		head := v.request(t, browser, http.MethodHead, v.listPath())
		if head.status != http.StatusOK || len(head.body) != 0 || head.header.Get("Content-Length") != strconv.Itoa(len(allReply.body)) {
			t.Fatal("directory HEAD did not retain complete GET metadata")
		}
		if after := v.snapshot(t, browser); after != before {
			t.Fatal("directory reads changed Agent commands, Outbox, Execution or Session Activity")
		}
	})
	t.Run("current-owner-boundary", func(t *testing.T) {
		v := newAgentDirectoryHTTPFixture(t)
		base := v.agent.p2.base
		first := v.page(t, v.request(t, base.ownerBrowser, http.MethodGet, v.listPath()+"?limit=1"))
		if len(first.Items) != 1 || first.NextCursor == "" {
			t.Fatal("real pagination control has no continuation")
		}
		nextPath := v.listPath() + "?limit=1&cursor=" + url.QueryEscape(first.NextCursor)
		before := v.snapshot(t, base.ownerBrowser)
		for _, path := range []string{v.listPath(), nextPath, v.getPath(v.expected[0].ID), v.getPath(id[i.Agent](t))} {
			v.problem(t, v.request(t, base.otherBrowser, http.MethodGet, path), http.StatusNotFound, f.NotFound)
		}
		v.problem(t, v.request(t, base.ownerBrowser, http.MethodGet, v.getPath(id[i.Agent](t))), http.StatusNotFound, f.NotFound)
		if v.snapshot(t, base.ownerBrowser) != before {
			t.Fatal("foreign Owner or missing Agent reads changed the target")
		}
		// The other scope is a second real P2 Project, not the inherited fixture's
		// older controlled-initializer Project and not an invented cursor grant.
		other, err := v.agent.p2.creator.CreateProject(ctxFor(t), base.ownerBrowser.actor, meta(t, "directory-other-project", nil), pc.CreateProjectRequest{ProjectID: id[i.Project](t), Name: "directory-other", Description: "Second real directory scope"})
		if err != nil || other.State != pc.CreationReady || other.Project == nil {
			t.Fatal("formal second P2 Project creation", err)
		}
		otherPath := variableHTTPPath(other.Project.ID, "/agents")
		empty := v.page(t, v.request(t, base.ownerBrowser, http.MethodGet, otherPath))
		if empty.Items == nil || len(empty.Items) != 0 || empty.NextCursor != "" {
			t.Fatal("real empty Project directory fabricated entries")
		}
		v.problem(t, v.request(t, base.ownerBrowser, http.MethodGet, otherPath+"?limit=1&cursor="+url.QueryEscape(first.NextCursor)), http.StatusBadRequest, f.CursorInvalid)
		v.problem(t, v.request(t, base.ownerBrowser, http.MethodGet, otherPath+"/"+v.expected[0].ID.String()), http.StatusNotFound, f.NotFound)
		if err = base.core.Logout(ctxFor(t), account.LogoutRequest{Actor: base.ownerBrowser.actor, Key: "agent-directory-logout"}); err != nil {
			t.Fatal("formal original Session logout", err)
		}
		v.problem(t, v.request(t, base.ownerBrowser, http.MethodGet, nextPath), http.StatusUnauthorized, f.SessionRevoked)
		v.problem(t, v.request(t, base.ownerBrowser, http.MethodHead, v.getPath(v.expected[0].ID)), http.StatusUnauthorized, "")
		fresh := base.login(t, base.ownerBrowser.email)
		if fresh.actor.Details().UserID != base.ownerBrowser.actor.Details().UserID || fresh.actor.Details().SessionID == base.ownerBrowser.actor.Details().SessionID {
			t.Fatal("new current Owner Session was not actually established")
		}
		before = v.snapshot(t, fresh)
		next := v.page(t, v.request(t, fresh, http.MethodGet, nextPath))
		if len(next.Items) != 1 || next.Items[0].ID != v.expected[1].ID {
			t.Fatal("cursor was mistaken for old Session authority or lost its original position")
		}
		if v.snapshot(t, fresh) != before {
			t.Fatal("new Session directory read changed business facts or Activity")
		}
	})
}

type agentDirectoryHTTPFixture struct {
	agent    *agentCreateFixture
	reader   *agent.DirectoryReader
	expected []ac.DirectoryEntry
	server   *httptest.Server
	done     chan bool
}

func newAgentDirectoryHTTPFixture(t *testing.T) *agentDirectoryHTTPFixture {
	t.Helper()
	a := newAgentCreateFixture(t)
	v := &agentDirectoryHTTPFixture{agent: a, done: make(chan bool, 1)}
	for n := 0; n < 3; n++ {
		original, command, _ := a.request(t)
		fields := original.Fields()
		fields.Name = fmt.Sprintf("directory-agent-%d", n)
		fields.Description = fmt.Sprintf("Public directory description %d", n)
		fields.Instructions = fmt.Sprintf("Private directory instructions %d", n)
		if n != 0 {
			name, color := fmt.Sprintf("Directory display %d", n), "#12abef"
			fields.DisplayName, fields.TagColor = &name, &color
		}
		request, err := ac.NewAgentCreate(fields)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := a.agents.CreateAgent(ctxFor(t), a.p2.base.ownerBrowser.actor, command, a.p2.project.ID, request)
		if err != nil || receipt.Validate() != nil {
			t.Fatal("formal default-enabled Agent creation", err)
		}
		core := receipt.Fields().Agent.Fields().Core
		v.expected = append(v.expected, ac.DirectoryEntry{ID: core.ID, ProjectID: core.ProjectID, Name: core.Name, DisplayName: core.DisplayName, TagColor: core.TagColor, Description: core.Description, Version: core.Version, CreatedAt: core.CreatedAt, UpdatedAt: core.UpdatedAt})
	}
	slices.SortFunc(v.expected, func(a, b ac.DirectoryEntry) int {
		if order := b.CreatedAt.Time().Compare(a.CreatedAt.Time()); order != 0 {
			return order
		}
		return strings.Compare(b.ID.String(), a.ID.String())
	})
	var err error
	v.reader, err = agent.NewDirectoryReader(a.p2.base.tracked, a.providers.Agents, a.p2.base.keys)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		v.reader.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := v.reader.Drain(ctx); err != nil || !v.reader.Joined() {
			t.Error("actual directory calls did not join", err)
		}
	})
	v.server = httptest.NewUnstartedServer(nil)
	t.Cleanup(v.server.Close) // Actual Serve/connection Wait precedes reader/Agent/P2 retirement.
	boundary, err := account.NewHTTPBoundary(a.p2.base.core, "https://"+v.server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	handler, err := agenthttp.NewDirectoryHTTPHandler(v.reader, boundary)
	if err != nil {
		t.Fatal(err)
	}
	wrapped := httpapi.Handler(nil, handler)
	v.server.Config.ErrorLog = log.New(io.Discard, "", 0)
	v.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		normal := false
		defer func() { v.done <- normal }()
		wrapped.ServeHTTP(w, r)
		normal = true
	})
	v.server.StartTLS()
	if v.server.Client().Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned TLS certificate verification disabled")
	}
	return v
}

func (v *agentDirectoryHTTPFixture) listPath() string {
	return variableHTTPPath(v.agent.p2.project.ID, "/agents")
}
func (v *agentDirectoryHTTPFixture) getPath(id i.AgentID) string {
	return v.listPath() + "/" + id.String()
}
func (v *agentDirectoryHTTPFixture) request(t *testing.T, browser variableHTTPBrowser, method, path string) variableHTTPResponse {
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
	request, err := http.NewRequestWithContext(ctx, method, v.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", v.server.URL)
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("owned directory TLS request did not return")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	validLength := response.ContentLength == int64(len(raw))
	if method == http.MethodHead {
		validLength = len(raw) == 0 && response.ContentLength >= 0
	}
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 || !validLength || response.TLS == nil || len(response.TLS.VerifiedChains) == 0 {
		t.Fatal("directory TLS response did not finish bounded EOF/Close/Content-Length")
	}
	select {
	case normal := <-v.done:
		if !normal {
			t.Fatal("directory handler aborted after its response")
		}
	case <-ctx.Done():
		t.Fatal("directory handler did not actually return")
	}
	if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("X-Request-ID") == "" {
		t.Fatal("directory response omitted security metadata")
	}
	return variableHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: raw}
}

func (v *agentDirectoryHTTPFixture) page(t *testing.T, reply variableHTTPResponse) f.Page[ac.DirectoryEntry] {
	t.Helper()
	var page f.Page[ac.DirectoryEntry]
	if reply.status != http.StatusOK || json.Unmarshal(reply.body, &page) != nil || page.Items == nil {
		t.Fatal("directory did not return a typed nonnull page", reply.status)
	}
	return page
}
func (v *agentDirectoryHTTPFixture) requireEntry(t *testing.T, raw []byte, expected ac.DirectoryEntry) {
	t.Helper()
	var entry ac.DirectoryEntry
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &entry) != nil || entry.Validate() != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 9 || !bytes.Equal(jsonBytes(t, entry), jsonBytes(t, expected)) {
		t.Fatal("directory safe nine-field projection differs from the formal Agent receipt")
	}
	if string(fields["version"]) != strconv.Quote(expected.Version.String()) || bytes.Contains(raw, []byte("Private directory instructions")) || bytes.Contains(raw, []byte(v.agent.modelID.String())) {
		t.Fatal("directory leaked private configuration or changed version wire type")
	}
}
func (v *agentDirectoryHTTPFixture) problem(t *testing.T, reply variableHTTPResponse, status int, code f.Code) {
	t.Helper()
	var value struct {
		Code f.Code `json:"code"`
	}
	if reply.status != status || code != "" && (json.Unmarshal(reply.body, &value) != nil || value.Code != code) || code == "" && len(reply.body) != 0 {
		t.Fatal("directory rejection changed its current-Owner boundary", reply.status, value.Code)
	}
}
func (v *agentDirectoryHTTPFixture) snapshot(t *testing.T, browser variableHTTPBrowser) string {
	t.Helper()
	var value string
	err := v.agent.p2.base.raw.QueryRow(ctxFor(t), `SELECT jsonb_build_object(
 'agents',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb) FROM agenteam_agent.agents a WHERE project_id=$1::text::uuid),
 'commands',(SELECT count(*) FROM agenteam_agent.commands WHERE project_id=$1::text::uuid),
 'outbox',(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid),
 'executions',(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text),
 'activity',(SELECT last_activity_at FROM agenteam_account.sessions WHERE id=$2::text::uuid))::text`, v.agent.p2.project.ID.String(), browser.actor.Details().SessionID).Scan(&value)
	if err != nil {
		t.Fatal("actual directory read-only postconditions unavailable", err)
	}
	return value
}
