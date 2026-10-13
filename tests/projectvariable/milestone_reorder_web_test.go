//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	eventc "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// Real default App, original Account owner and formal HTTP seeds. No fault injection.
func TestMilestoneReorderWeb(t *testing.T) {
	for _, mode := range []string{"milestone-reorder-normal", "milestone-reorder-responsive"} {
		t.Run(mode, func(t *testing.T) {
			entered, completed := false, false
			t.Cleanup(func() {
				if !entered || !completed {
					t.Error("Milestone Reorder original App handoff incomplete")
				}
			})
			a := newAgentCreateFixture(t)
			original := a.p2.base
			project, err := a.p2.creator.GetSchedulerConfig(ctxFor(t), original.ownerBrowser.actor, a.p2.project.ID)
			agentAppRequire(t, err)
			if project.Config.Enabled || project.Project.CurrentSprintID != nil {
				t.Fatal("original Project must be paused without a current Sprint")
			}
			process, err := a.guard.CurrentProcess()
			agentAppRequire(t, err)
			var username string
			agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
			if original.afterOwners != nil {
				t.Fatal("original handoff occupied")
			}
			original.afterOwners = func() {
				entered = true
				if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
					t.Fatal("original setup owners not joined")
				}
				if _, err := a.guard.CurrentProcess(); err == nil {
					t.Fatal("original guard still held")
				}
				var stopped bool
				agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
				if !stopped {
					t.Fatal("original process not retired")
				}
				w := newMilestoneReorderWeb(t, a, project, mode)
				defer w.dispose(t)
				withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
					defer w.close(t)
					w.start(t, address)
					w.prepare(t, ctx)
					w.material(t, username)
					w.browser(t, ctx)
					w.verify(t, ctx)
					w.close(t)
				})
				completed = true
			}
		})
	}
}

func newMilestoneReorderWeb(t *testing.T, a *agentCreateFixture, project pc.SchedulerProject, mode string) *milestoneReorderWeb {
	t.Helper()
	const prefix = "AGENTEAM_MILESTONE_REORDER_WEB"
	dist, runtime, evidence, inputHash := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("frozen Milestone Reorder inputs required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen index missing")
	}
	directory, err := os.MkdirTemp(runtime, "mrw-")
	agentAppRequire(t, err)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(directory)
		}
	}()
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	agentAppRequire(t, err)
	base := &agentConfigurationWeb{a: a, mode: mode, origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &milestoneCreateWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: base, project: project.Clone()}}
	published = true
	return &milestoneReorderWeb{milestoneCreateWeb: w}
}
func (w *milestoneReorderWeb) start(t *testing.T, address string) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned Agent Edit TLS verification disabled")
	}
	target, err := url.Parse(w.backend.URL)
	agentAppRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport, proxy.ErrorLog = w.transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse, proxy.ErrorHandler = w.response, w.proxyError
	static := http.FileServer(http.Dir(w.dist))
	w.front = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.frontWG.Add(1)
		defer w.frontWG.Done()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if err := w.observe(r); err != nil {
				w.fail(err)
				http.Error(out, "owned Milestone Reorder observation failed", 500)
				return
			}
			proxy.ServeHTTP(out, r)
			return
		}
		if w.dist == "" || r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(out, r)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(w.dist, clean)); err == nil && info.Mode().IsRegular() {
			static.ServeHTTP(out, r)
			return
		}
		http.ServeFile(out, r, filepath.Join(w.dist, "index.html"))
	})}
	go func() { w.serve <- w.front.Serve(w.listener) }()
}

func (w *milestoneReorderWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "milestone-reorder.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "TMPDIR", "TMP", "TEMP", "NODE_COMPILE_CACHE", "PWTEST_CACHE_DIR", "XDG_CACHE_HOME":
			continue
		}
		if key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	// Keep every child temporary/cache path inside the original Dispose owner.
	for _, variable := range []struct{ key, directory string }{
		{"TMPDIR", "tmp"}, {"TMP", "tmp"}, {"TEMP", "tmp"},
		{"NODE_COMPILE_CACHE", "node-compile-cache"},
		{"PWTEST_CACHE_DIR", "playwright-cache"},
		{"XDG_CACHE_HOME", "xdg-cache"},
	} {
		directory := filepath.Join(w.directory, variable.directory)
		agentAppRequire(t, os.MkdirAll(directory, 0700))
		cmd.Env = append(cmd.Env, variable.key+"="+directory)
	}
	const prefix = "AGENTEAM_MILESTONE_REORDER_WEB"
	cmd.Env = append(cmd.Env, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Milestone Reorder Playwright did not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
	}()
	err = <-done
	joined = true
	t.Logf("Milestone Reorder Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.a.p2.base.ownerBrowser.cookie, w.a.p2.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("original Milestone Reorder browser call failed")
	}
}

type milestoneReorderIntent struct {
	sprintWebIntent
	before     wc.Milestone
	request    wc.ReorderMilestoneRequest
	expected   f.Version
	generation f.Version
	requestID  f.ID[f.Request]
	position   wc.MilestonePosition
}
type milestoneReorderIntentKey struct{}
type milestoneReorderWeb struct {
	*milestoneCreateWeb
	seed, current                   wc.Milestone
	rows                            []wc.Milestone
	writes                          []milestoneReorderIntent
	results                         []wc.StructureMutation
	initialGeneration               f.Version
	fixed, siblingFacts, otherFacts string
	gets, lists, secondPages        int
	cursors                         map[string]bool
}

func (w *milestoneReorderWeb) path() string {
	return variableHTTPPath(w.project.Project.ID, "/milestones/"+w.seed.ID.String())
}
func (w *milestoneReorderWeb) prepare(t *testing.T, ctx context.Context) {
	w.prepareEmpty(t, ctx)
	// Both scopes are existing initialized Projects owned by the real browser.
	// The second is the inherited fixture's unrelated Project, never fabricated SQL.
	other := w.a.p2.base.project.ID
	if other == w.project.Project.ID {
		t.Fatal("independent B Project absent")
	}
	for _, group := range []struct {
		project pc.ProjectID
		count   int
	}{{w.project.Project.ID, 54}, {other, 2}} {
		for n := 0; n < group.count; n++ {
			request := wc.CreateMilestoneRequest{MilestoneID: id[wc.Milestone](t), Title: fmt.Sprintf("Milestone %02d", n+1), Description: "Original planning content"}
			key := f.IdempotencyKey(id[struct{}](t).String())
			reply := w.request(t, http.MethodPost, variableHTTPPath(group.project, "/milestones"), jsonBytes(t, map[string]any{"request": request}), key)
			var result wc.StructureMutation
			if reply.status != 200 || json.Unmarshal(reply.body, &result) != nil || result.Validate() != nil || result.Command != wc.MilestoneCreate || result.Milestone == nil || result.Milestone.ProjectID != group.project || result.Milestone.ID != request.MilestoneID || result.Milestone.Title != request.Title || result.Milestone.Version != 1 {
				t.Fatal("formal HTTP Milestone seed failed")
			}
			if group.project == w.project.Project.ID {
				w.rows = append(w.rows, *result.Milestone)
			}
		}
	}
	w.seed, w.current = w.rows[0], w.rows[0]
	w.cursors = map[string]bool{}
	// The listener was published only after App startup; every seed's original
	// HTTP EOF/Close and handler return are joined by request(). Capture B now.
	reply := w.request(t, http.MethodGet, variableHTTPPath(other, ""), nil, "")
	var otherCurrent pc.ProjectRef
	if reply.status != 200 || json.Unmarshal(reply.body, &otherCurrent) != nil || otherCurrent.ID != other {
		t.Fatal("B original post-startup current read failed")
	}
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT order_generation FROM agenteam_work.milestone_order_groups WHERE project_id=$1::text::uuid`, w.project.Project.ID.String()).Scan(&w.generation))
	if w.generation != 55 {
		t.Fatal("54 formal seeds did not produce generation 55")
	}
	w.initialGeneration = w.generation
	var err error
	w.fixed, err = w.unchanged(ctx)
	agentAppRequire(t, err)
	w.siblingFacts, err = w.siblings(ctx)
	agentAppRequire(t, err)
	w.otherFacts, err = w.otherSnapshot(ctx)
	agentAppRequire(t, err)
	w.requireStable(t, ctx)
}
func (w *milestoneReorderWeb) material(t *testing.T, username string) {
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.project.Project.ID, "other_project_id": w.a.p2.base.project.ID, "milestone_id": w.seed.ID, "route": "/" + username + "/" + w.project.Project.NormalizedName + "/tasks", "milestone": w.seed, "rows": w.rows, "input_hash": w.inputHash})
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "milestone-reorder-material.json"), raw, 0600))
	clear(raw)
}
func (w *milestoneReorderWeb) siblings(ctx context.Context) (string, error) {
	var value string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY id),'[]'::jsonb)::text FROM agenteam_work.milestones m WHERE project_id=$1::text::uuid AND id<>$2::text::uuid`, w.project.Project.ID.String(), w.seed.ID.String()).Scan(&value)
	return value, err
}
func (w *milestoneReorderWeb) otherSnapshot(ctx context.Context) (string, error) {
	var value string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'milestones',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agenteam_work.milestones m WHERE project_id=$1::text::uuid),
 'sprints',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_work.sprints s WHERE project_id=$1::text::uuid),
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid),
 'milestone_order',(SELECT to_jsonb(g) FROM agenteam_work.milestone_order_groups g WHERE project_id=$1::text::uuid),
 'sprint_order',(SELECT jsonb_agg(to_jsonb(g) ORDER BY milestone_id) FROM agenteam_work.sprint_order_groups g WHERE project_id=$1::text::uuid),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.structure_commands c WHERE project_id=$1::text::uuid),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM agenteam_outbox.events e WHERE project_id=$1::text::uuid),
 'audit',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agenteam_audit.audit_records a WHERE project_id=$1::text::uuid))::text`, w.a.p2.base.project.ID.String()).Scan(&value)
	return value, err
}
func (w *milestoneReorderWeb) requireStable(t *testing.T, ctx context.Context) {
	fixed, err := w.unchanged(ctx)
	agentAppRequire(t, err)
	siblings, err := w.siblings(ctx)
	agentAppRequire(t, err)
	other, err := w.otherSnapshot(ctx)
	agentAppRequire(t, err)
	if fixed != w.fixed || siblings != w.siblingFacts || other != w.otherFacts {
		t.Fatal("reorder changed Project/Sprint/Task/Execution, sibling or B facts")
	}
}
func (w *milestoneReorderWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if w.mode != "milestone-reorder-normal" || r.Method != http.MethodPost || r.URL.Path != w.path()+"/reorder" || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return errors.New("undeclared Milestone reorder write or Lookup")
	}
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, 16385))
	closeErr := r.Body.Close()
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if readErr != nil || closeErr != nil || len(raw) > 16384 || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("reorder original request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var wire struct {
		Expected f.Version                  `json:"expected_version"`
		Request  wc.ReorderMilestoneRequest `json:"request"`
	}
	var envelope map[string]json.RawMessage
	if agentWebDecode(raw, &wire) != nil || json.Unmarshal(raw, &envelope) != nil || len(envelope) != 2 || wire.Request.Validate() != nil {
		return errors.New("reorder envelope changed original field presence")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.writes) != len(w.results) || len(w.writes) >= 3 || wire.Expected != w.current.Version {
		return errors.New("reorder repeated write or changed original version")
	}
	for _, old := range w.writes {
		if old.key == key {
			return errors.New("reorder reused a prior key")
		}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(envelope["request"], &fields) != nil {
		return errors.New("reorder request absent")
	}
	var want wc.ReorderMilestoneRequest
	if len(w.writes) == 0 {
		anchor := w.rows[len(w.rows)-1].ID
		want.BeforeID = &anchor
		if len(fields) != 1 {
			return errors.New("before request omitted anchor")
		}
	} else if len(fields) != 0 {
		return errors.New("tail must omit before_id, never null")
	}
	if !reflect.DeepEqual(wire.Request, want) {
		return errors.New("reorder changed original anchor")
	}
	in := milestoneReorderIntent{sprintWebIntent: sprintWebIntent{kind: "reorder", key: key, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}, before: w.current, request: wire.Request, expected: wire.Expected, generation: w.generation}
	// Expected neighbors come from the original ordered identities, independently
	// of the server's resulting rank. No client or test computes rank tokens.
	others := make([]wc.Milestone, 0, 53)
	for _, m := range w.rows {
		if m.ID != w.seed.ID {
			others = append(others, m)
		}
	}
	if want.BeforeID != nil {
		prev := others[len(others)-2].ID
		next := others[len(others)-1].ID
		in.position = wc.MilestonePosition{PreviousID: &prev, NextID: &next, OrderGeneration: w.generation + 1}
	} else {
		prev := others[len(others)-1].ID
		in.position = wc.MilestonePosition{PreviousID: &prev, OrderGeneration: w.generation + 1}
	}
	w.writes = append(w.writes, in)
	*r = *r.WithContext(context.WithValue(r.Context(), milestoneReorderIntentKey{}, in))
	return nil
}
func (w *milestoneReorderWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	in, write := r.Request.Context().Value(milestoneReorderIntentKey{}).(milestoneReorderIntent)
	if r.StatusCode != 200 {
		return errors.New("original Milestone reorder/read failed")
	}
	if write {
		var result wc.StructureMutation
		if json.Unmarshal(raw, &result) != nil {
			return errors.New("reorder receipt decode failed")
		}
		in.requestID, err = f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
		if err != nil {
			return err
		}
		w.mu.Lock()
		index := len(w.results)
		w.mu.Unlock()
		if err = w.reorderFacts(r.Request.Context(), in, result, index < 2, true); err != nil {
			return err
		}
		w.mu.Lock()
		w.current = *result.Milestone
		w.writes[index] = in
		w.results = append(w.results, result.Clone())
		if result.Changed {
			w.generation++
			w.cursors = map[string]bool{}
		}
		others := make([]wc.Milestone, 0, 54)
		for _, m := range w.rows {
			if m.ID != w.seed.ID {
				others = append(others, m)
			}
		}
		if in.request.BeforeID != nil {
			others = append(others[:52], w.current, others[52])
		} else {
			others = append(others, w.current)
		}
		w.rows = others
		w.mu.Unlock()
	} else if r.Request.Method == http.MethodGet {
		if err = w.observeRead(r.Request, raw); err != nil {
			return err
		}
	}
	if err = w.proof(r, raw, in.sprintWebIntent, write); err != nil {
		return err
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *milestoneReorderWeb) observeRead(r *http.Request, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch r.URL.Path {
	case variableHTTPPath(w.project.Project.ID, ""):
		var p pc.ProjectRef
		if json.Unmarshal(raw, &p) != nil || !reflect.DeepEqual(p, w.project.Project) {
			return errors.New("current Project changed")
		}
	case w.path():
		var m wc.Milestone
		if json.Unmarshal(raw, &m) != nil || !reflect.DeepEqual(m, w.current) {
			return errors.New("current Get differs from reorder postimage")
		}
		w.gets++
	case variableHTTPPath(w.project.Project.ID, "/milestones"):
		q := r.URL.Query()
		if q.Get("limit") != "50" || len(q) > 2 {
			return errors.New("candidate page changed real server query")
		}
		offset := 0
		if cursor := q.Get("cursor"); cursor != "" {
			if !w.cursors[cursor] {
				return errors.New("stale or foreign candidate cursor")
			}
			offset = 50
			w.secondPages++
		}
		end := offset + 50
		if end > len(w.rows) {
			end = len(w.rows)
		}
		var page f.Page[map[string]any]
		if json.Unmarshal(raw, &page) != nil || page.Items == nil {
			return errors.New("candidate page absent")
		}
		want := make([]map[string]any, 0, end-offset)
		for _, m := range w.rows[offset:end] {
			item, e := sprintCreateSummary(m, "description")
			if e != nil {
				return e
			}
			want = append(want, item)
		}
		if !reflect.DeepEqual(page.Items, want) || (page.NextCursor != "") != (end < len(w.rows)) {
			return errors.New("candidate page differs from exact server order")
		}
		if page.NextCursor != "" {
			w.cursors[page.NextCursor] = true
		}
		w.lists++
	case variableHTTPPath(w.project.Project.ID, "/sprints"):
		var page f.Page[map[string]any]
		if r.URL.Query().Get("milestone_id") != w.seed.ID.String() || json.Unmarshal(raw, &page) != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != "" {
			return errors.New("reorder altered empty Sprint list")
		}
	}
	return nil
}

func (w *milestoneReorderWeb) reorderFacts(ctx context.Context, in milestoneReorderIntent, result wc.StructureMutation, changed, canonical bool) error {
	if result.Validate() != nil || result.Command != wc.MilestoneReorder || result.Milestone == nil || result.Sprint != nil || result.Changed != changed || (result.EventID != nil) != changed {
		return errors.New("reorder lost exact Structure receipt")
	}
	expected := in.before
	if changed {
		expected.Version++
		expected.ManualRank = result.Milestone.ManualRank
		expected.UpdatedAt = result.Milestone.UpdatedAt
		if expected.ManualRank == in.before.ManualRank {
			return errors.New("changed reorder retained prior rank")
		}
	}
	if in.expected != in.before.Version || !reflect.DeepEqual(*result.Milestone, expected) {
		return errors.New("reorder changed content or lost original version/no-op")
	}
	actor := w.a.p2.base.ownerBrowser.actor
	semantic, err := wc.ReorderMilestoneDigest(actor, f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key, ExpectedVersion: &in.expected}, w.project.Project.ID, w.seed.ID, in.request)
	if err != nil {
		return err
	}
	var commandID, user, digest string
	var eventID *string
	var requestRaw, receiptRaw, planRaw []byte
	var revision int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT id::text,event_id::text,actor_user_id::text,semantic_digest,request,receipt,plan,plan_revision FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid AND command_name='work.milestone.reorder' AND idempotency_key=$2 AND state='completed' AND committed_at IS NOT NULL`, w.project.Project.ID.String(), string(in.key)).Scan(&commandID, &eventID, &user, &digest, &requestRaw, &receiptRaw, &planRaw, &revision)
	if err != nil {
		return err
	}
	var input map[string]any
	var receipt wc.StructureMutation
	want := map[string]any{"command": wc.MilestoneReorder, "project_id": w.project.Project.ID, "target_id": w.seed.ID.String(), "actor_user_id": actor.Details().UserID, "expected_version": in.expected, "create_milestone": nil, "create_sprint": nil, "update": nil, "reorder_milestone": in.request, "reorder_sprint": nil}
	if json.Unmarshal(requestRaw, &input) != nil || !sprintCreateEqual(input, want) || json.Unmarshal(receiptRaw, &receipt) != nil || !reflect.DeepEqual(receipt, result) || revision < 1 || user != actor.Details().UserID || digest != semantic.String() {
		return errors.New("reorder command lost original actor/input/digest/receipt")
	}
	if !changed {
		if eventID != nil || len(planRaw) != 0 {
			return errors.New("no-op emitted event or plan")
		}
	} else {
		var plan struct {
			BeforeMilestone *wc.Milestone        `json:"before_milestone"`
			BeforeSprint    *wc.Sprint           `json:"before_sprint"`
			After           wc.StructureMutation `json:"after"`
			GroupBefore     *f.Version           `json:"group_before"`
			Header          eventc.Header        `json:"header"`
			Payload         wc.MilestoneChanged  `json:"payload"`
		}
		if eventID == nil || *eventID != result.EventID.String() || json.Unmarshal(planRaw, &plan) != nil || plan.BeforeMilestone == nil || !reflect.DeepEqual(*plan.BeforeMilestone, in.before) || plan.BeforeSprint != nil || plan.GroupBefore == nil || *plan.GroupBefore != in.generation || !reflect.DeepEqual(plan.After, result) {
			return errors.New("reorder plan lost original preimage/postimage/generation")
		}
		h, p := plan.Header, plan.Payload
		if h.Validate() != nil || p.Validate() != nil || h.EventID != *result.EventID || h.EventType != wc.MilestoneChangedName || h.AggregateType != wc.MilestoneAggregate || h.AggregateID.String() != w.seed.ID.String() || h.AggregateVersion == nil || *h.AggregateVersion != expected.Version || h.AggregateSequence != nil || h.SchemaVersion != wc.WorkSchemaVersion || h.Scope.Kind != eventc.ProjectScope || h.Scope.ProjectID.String() != w.project.Project.ID.String() || h.OccurredAt != expected.UpdatedAt || p.CommandID.String() != commandID || p.ActorUserID.String() != user || p.Change != wc.ReorderedChange || !reflect.DeepEqual(p.ChangedFields, []wc.ChangedField{wc.RankChanged}) || p.Position == nil || !reflect.DeepEqual(*p.Position, in.position) {
			return errors.New("reorder event lost neighbors/version/generation")
		}
		var headerRaw, payloadRaw []byte
		var columns bool
		err = w.a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='work.milestone_changed' AND schema_version=1 AND scope='project' AND aggregate_type='work.milestone' AND aggregate_version=$4 AND aggregate_sequence IS NULL AND occurred_at=$5 FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND id=$3::text::uuid AND producer='work'`, w.project.Project.ID.String(), w.seed.ID.String(), *eventID, int64(expected.Version), expected.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &columns)
		if err != nil {
			return err
		}
		actualHeader, e := eventc.DecodeHeader(headerRaw)
		var actualPayload wc.MilestoneChanged
		if e != nil || !columns || json.Unmarshal(payloadRaw, &actualPayload) != nil || !reflect.DeepEqual(actualHeader, h) || !reflect.DeepEqual(actualPayload, p) {
			return errors.New("reorder Outbox differs from original plan")
		}
	}
	if canonical {
		var matches bool
		var generation f.Version
		err = w.a.p2.base.raw.QueryRow(ctx, `SELECT title=$3 AND description=$4 AND manual_rank=$5 AND version=$6 AND created_at=$7 AND updated_at=$8 FROM agenteam_work.milestones WHERE project_id=$1::text::uuid AND id=$2::text::uuid`, w.project.Project.ID.String(), w.seed.ID.String(), expected.Title, expected.Description, expected.ManualRank, int64(expected.Version), expected.CreatedAt.Time(), expected.UpdatedAt.Time()).Scan(&matches)
		if err != nil {
			return err
		}
		if !matches {
			return errors.New("canonical target differs from reorder receipt")
		}
		err = w.a.p2.base.raw.QueryRow(ctx, `SELECT order_generation FROM agenteam_work.milestone_order_groups WHERE project_id=$1::text::uuid`, w.project.Project.ID.String()).Scan(&generation)
		if err != nil {
			return err
		}
		wantGeneration := in.generation
		if changed {
			wantGeneration++
		}
		if generation != wantGeneration {
			return errors.New("reorder/no-op changed generation incorrectly")
		}
	}
	return nil
}
func (w *milestoneReorderWeb) verify(t *testing.T, ctx context.Context) {
	w.mu.Lock()
	failure := w.failure
	writes := append([]milestoneReorderIntent(nil), w.writes...)
	results := append([]wc.StructureMutation(nil), w.results...)
	gets, lists, second := w.gets, w.lists, w.secondPages
	rows := append([]wc.Milestone(nil), w.rows...)
	tails := make([]*reviewWebTail, 0, len(w.tails))
	for _, tail := range w.tails {
		tails = append(tails, tail)
	}
	w.mu.Unlock()
	wantWrites, minGets := 3, 4
	if w.mode == "milestone-reorder-responsive" {
		wantWrites, minGets = 0, 1
	}
	if failure != nil || w.cut != 0 || len(writes) != wantWrites || len(results) != wantWrites || gets < minGets || lists < 2 || (wantWrites > 0 && second < 2) {
		t.Fatal("original reorder browser chain incomplete", failure, gets, lists, second)
	}
	for _, tail := range tails {
		select {
		case <-tail.done:
		default:
			t.Fatal("original reorder handler remains")
		}
		if !tail.returned {
			t.Fatal("original reorder handler did not return")
		}
	}
	for n, in := range writes {
		agentAppRequire(t, w.reorderFacts(ctx, in, results[n], n < 2, n == len(writes)-1))
	}
	var commands, events, milestones, generation int64
	var ordered []string
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid),(SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND producer='work'),(SELECT count(*) FROM agenteam_work.milestones WHERE project_id=$1::text::uuid),(SELECT order_generation FROM agenteam_work.milestone_order_groups WHERE project_id=$1::text::uuid),(SELECT array_agg(id::text ORDER BY manual_rank COLLATE "C",id) FROM agenteam_work.milestones WHERE project_id=$1::text::uuid)`, w.project.Project.ID.String()).Scan(&commands, &events, &milestones, &generation, &ordered))
	changed := int64(0)
	if wantWrites > 0 {
		changed = 2
	}
	if commands != 54+int64(wantWrites) || events != 54+changed || milestones != 54 || generation != int64(w.initialGeneration)+changed {
		t.Fatal("reorder duplicated command/event or changed membership/generation")
	}
	want := make([]string, len(rows))
	for n, m := range rows {
		want[n] = m.ID.String()
	}
	if !reflect.DeepEqual(ordered, want) {
		t.Fatal("final canonical order differs from independent identity sequence")
	}
	w.requireStable(t, ctx)
	t.Logf("Milestone Reorder real facts mode=%s writes=%d changed=%d candidates=53 generation=%d B_unchanged=true", w.mode, wantWrites, changed, generation)
}
func (w *milestoneReorderWeb) proof(r *http.Response, raw []byte, intent sprintWebIntent, write bool) error {
	record := map[string]any{"input_hash": w.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
	if write {
		record["kind"] = intent.kind
		record["request_body_sha256"] = fmt.Sprintf("%x", intent.bodyDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", intent.keyDigest)
		record["committed_verified"] = true
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.evidence, "milestone-reorder-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600)
}
