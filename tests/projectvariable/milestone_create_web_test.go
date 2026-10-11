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
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	eventc "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// One original browser command creates the first Milestone in an initialized
// Project. Opening and cancelling Sprint creation does not create a Sprint.
func TestMilestoneCreateWeb(t *testing.T) {
	t.Run("first-milestone-and-sprint-entry", func(t *testing.T) {
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("Milestone Create original App handoff incomplete")
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
			w := newMilestoneCreateWeb(t, a, project)
			defer w.dispose(t)
			withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
				defer w.close(t)
				w.start(t, address)
				w.prepareEmpty(t, ctx)
				w.material(t, username)
				w.browser(t, ctx)
				w.verify(t, ctx)
				w.close(t)
			})
			completed = true
		}
	})
}

const milestoneCreateTitle = "First delivery milestone"
const milestoneCreateDescription = "Plan the first milestone.\nKeep Sprint creation explicit."

type milestoneCreateIntent struct {
	sprintWebIntent
	request   wc.CreateMilestoneRequest
	requestID f.ID[f.Request]
}
type milestoneCreateIntentKey struct{}
type milestoneCreateWeb struct {
	*sprintStartWeb
	generation f.Version
	immutable string
	created wc.StructureMutation
	createIntent milestoneCreateIntent
	creates, newReads, listReads, emptySprintLists int
}

func newMilestoneCreateWeb(t *testing.T, a *agentCreateFixture, project pc.SchedulerProject) *milestoneCreateWeb {
	t.Helper()
	const prefix = "AGENTEAM_MILESTONE_CREATE_WEB"
	dist, runtime, evidence, inputHash := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("frozen Milestone Create inputs required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen index missing")
	}
	directory, err := os.MkdirTemp(runtime, "mcw-")
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
	base := &agentConfigurationWeb{a: a, mode: "first-milestone-and-sprint-entry", origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &milestoneCreateWeb{sprintStartWeb: &sprintStartWeb{agentConfigurationWeb: base, project: project.Clone()}}
	published = true
	return w
}
func (w *milestoneCreateWeb) request(t *testing.T, method, path string, body []byte, key f.IdempotencyKey) variableHTTPResponse {
	return (&agentEditWeb{agentConfigurationWeb: w.agentConfigurationWeb}).request(t, w.a.p2.base.ownerBrowser, method, path, body, key)
}
func (w *milestoneCreateWeb) prepareEmpty(t *testing.T, ctx context.Context) {
	var empty bool
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT
 NOT EXISTS(SELECT 1 FROM agenteam_work.milestones WHERE project_id=$1::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_work.sprints WHERE project_id=$1::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_work.tasks WHERE project_id=$1::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_work.milestone_order_groups WHERE project_id=$1::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_work.sprint_order_groups WHERE project_id=$1::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_work.sprint_start_commands WHERE project_id=$1::text::uuid)
 AND NOT EXISTS(SELECT 1 FROM agenteam_scheduler.dispatches WHERE project_id=$1::text)
 AND NOT EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1::text)`, w.project.Project.ID.String()).Scan(&empty))
	if !empty { t.Fatal("first Milestone preparation already has Work facts") }
	immutable, err := w.unchanged(ctx)
	agentAppRequire(t, err)
	// The original reader uses virtual generation 1 for an absent empty group.
	// Publish after preparation through the same response-observation mutex.
	w.mu.Lock()
	w.generation, w.immutable = 1, immutable
	w.mu.Unlock()
}
func (w *milestoneCreateWeb) material(t *testing.T, username string) {
	raw := jsonBytes(t, map[string]any{"mode":w.mode,"cookie":w.a.p2.base.ownerBrowser.cookie,"project_id":w.project.Project.ID,"project_version":w.project.Project.Version,"route":"/"+username+"/"+w.project.Project.NormalizedName+"/tasks","title":milestoneCreateTitle,"description":milestoneCreateDescription})
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory,"milestone-create-material.json"),raw,0600))
	clear(raw)
}
func (w *milestoneCreateWeb) start(t *testing.T, address string) {
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
				http.Error(out, "owned Sprint observation failed", 500)
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

func (w *milestoneCreateWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "milestone-create.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	const prefix = "AGENTEAM_MILESTONE_CREATE_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Sprint Playwright did not start")
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
	t.Logf("Milestone Create Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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
		t.Fatal("original Sprint browser call failed")
	}
}

func (w *milestoneCreateWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.Path != variableHTTPPath(w.project.Project.ID, "/milestones") || r.URL.RawQuery != "" {
		return errors.New("undeclared Milestone Create write")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (16<<10)+1))
	closed := r.Body.Close()
	if err != nil || closed != nil || len(raw) > 16<<10 {
		return errors.New("original Milestone Create request not complete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var envelope struct {
		Request wc.CreateMilestoneRequest `json:"request"`
	}
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 || agentWebDecode(raw, &envelope) != nil || envelope.Request.Validate() != nil {
		return errors.New("invalid original Milestone Create envelope")
	}
	request := envelope.Request
	if request.Title != milestoneCreateTitle || request.Description != milestoneCreateDescription {
		return errors.New("browser changed Milestone Create intent")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.creates != 0 {
		return errors.New("browser repeated Milestone Create")
	}
	in := milestoneCreateIntent{sprintWebIntent: sprintWebIntent{kind: "create", key: key, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}, request: request}
	w.creates++
	w.keys = append(w.keys, string(key))
	w.createIntent = in
	*r = *r.WithContext(context.WithValue(r.Context(), milestoneCreateIntentKey{}, in))
	return nil
}
func (w *milestoneCreateWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	intent, write := r.Request.Context().Value(milestoneCreateIntentKey{}).(milestoneCreateIntent)
	if write {
		var result wc.StructureMutation
		if r.StatusCode != 200 || json.Unmarshal(raw, &result) != nil || result.Validate() != nil {
			return errors.New("original Create response invalid")
		}
		intent.requestID, err = f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
		if err != nil {
			return err
		}
		if err = w.committedFacts(r.Request.Context(), intent, result); err != nil {
			return err
		}
		w.mu.Lock()
		w.created = result.Clone()
		w.createIntent = intent
		w.mu.Unlock()
	} else if r.Request.Method == http.MethodGet && r.StatusCode == 200 {
		if err = w.observeRead(r.Request, raw); err != nil {
			return err
		}
	}
	if r.StatusCode == 200 && (write || r.Request.Method == http.MethodGet) {
		if err = w.proof(r, raw, intent.sprintWebIntent, write); err != nil {
			return err
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *milestoneCreateWeb) proof(r *http.Response, raw []byte, intent sprintWebIntent, write bool) error {
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
	return os.WriteFile(filepath.Join(w.evidence, "milestone-create-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600)
}

func (w *milestoneCreateWeb) observeRead(r *http.Request, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.project.Project.ID
	switch r.URL.Path {
	case variableHTTPPath(p, ""):
		var project pc.ProjectRef
		if json.Unmarshal(raw,&project)!=nil || !reflect.DeepEqual(project,w.project.Project) { return errors.New("Milestone Create changed current Project/version") }
		w.projectReads++
	case variableHTTPPath(p,"/milestones"):
		var page f.Page[map[string]any]
		if json.Unmarshal(raw,&page)!=nil || page.Items==nil || page.NextCursor!="" { return errors.New("invalid original Milestone page") }
		expected:=[]map[string]any{}
		if w.created.Validate()==nil {
			item,err:=sprintCreateSummary(*w.created.Milestone,"description");if err!=nil{return err}
			expected=append(expected,item)
			w.listReads++
		}
		if !reflect.DeepEqual(page.Items,expected) { return errors.New("Milestone page lost exact original safe summary") }
	case variableHTTPPath(p,"/sprints"):
		if w.created.Validate()!=nil || r.URL.Query().Get("milestone_id")!=w.created.Milestone.ID.String() { return errors.New("Sprint list used an uncreated Milestone") }
		var page f.Page[map[string]any]
		if json.Unmarshal(raw,&page)!=nil || page.Items==nil || len(page.Items)!=0 || page.NextCursor!="" { return errors.New("opening Sprint form created a Sprint") }
		w.emptySprintLists++
	default:
		if w.created.Validate()==nil && r.URL.Path==variableHTTPPath(p,"/milestones/"+w.created.Milestone.ID.String()) {
			var item wc.Milestone
			if json.Unmarshal(raw,&item)!=nil || !reflect.DeepEqual(item,*w.created.Milestone) {return errors.New("Milestone Get differs from original complete receipt")}
			w.newReads++
		}
	}
	return nil
}
func (w *milestoneCreateWeb) unchanged(ctx context.Context) (string,error) {
	var snapshot string
	err:=w.a.p2.base.raw.QueryRow(ctx,`SELECT jsonb_build_object(
 'project',(SELECT to_jsonb(p) FROM agenteam_project.projects p WHERE id=$1::text::uuid),
 'sprints',(SELECT jsonb_agg(to_jsonb(s) ORDER BY id) FROM agenteam_work.sprints s WHERE project_id=$1::text::uuid),
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM agenteam_work.tasks t WHERE project_id=$1::text::uuid),
 'starts',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_work.sprint_start_commands c WHERE project_id=$1::text::uuid),
 'dispatches',(SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 'executions',(SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text))::text`,w.project.Project.ID.String()).Scan(&snapshot)
	return snapshot,err
}
func (w *milestoneCreateWeb) committedFacts(ctx context.Context, in milestoneCreateIntent, result wc.StructureMutation) error {
	if result.Validate() != nil || result.Command != wc.MilestoneCreate || !result.Changed || result.Milestone == nil || result.Sprint != nil || result.EventID == nil {
		return errors.New("Create lost its original complete Structure receipt")
	}
	v := result.Milestone
	expected := wc.Milestone{ID:in.request.MilestoneID,ProjectID:w.project.Project.ID,Title:in.request.Title,Description:in.request.Description,ManualRank:v.ManualRank,Version:1,CreatedAt:v.CreatedAt,UpdatedAt:v.CreatedAt}
	if !reflect.DeepEqual(*v,expected) {return errors.New("Create did not publish exact first Milestone v1")}
	actor := w.a.p2.base.ownerBrowser.actor
	semantic, err := wc.CreateMilestoneDigest(actor, f.CommandMeta{RequestID: in.requestID, IdempotencyKey: in.key}, w.project.Project.ID, in.request)
	if err != nil {
		return err
	}
	var commandID, eventID, user, digest string
	var requestRaw, receiptRaw, planRaw []byte
	var revision int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT id::text,event_id::text,actor_user_id::text,semantic_digest,request,receipt,plan,plan_revision FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid AND command_name='work.milestone.create' AND idempotency_key=$2 AND state='completed' AND committed_at IS NOT NULL`, w.project.Project.ID.String(), string(in.key)).Scan(&commandID, &eventID, &user, &digest, &requestRaw, &receiptRaw, &planRaw, &revision)
	if err != nil {
		return err
	}
	var input map[string]any
	var stored wc.StructureMutation
	var plan struct {
		BeforeMilestone *wc.Milestone        `json:"before_milestone"`
		BeforeSprint    *wc.Sprint           `json:"before_sprint"`
		After           wc.StructureMutation `json:"after"`
		GroupBefore     *f.Version           `json:"group_before"`
		Header          eventc.Header        `json:"header"`
		Payload         wc.MilestoneChanged     `json:"payload"`
	}
	expectedInput := map[string]any{"command": wc.MilestoneCreate, "project_id": w.project.Project.ID, "target_id": v.ID.String(), "actor_user_id": actor.Details().UserID, "expected_version": nil, "create_milestone": in.request, "create_sprint": nil, "update": nil, "reorder_milestone": nil, "reorder_sprint": nil}
	if json.Unmarshal(requestRaw, &input) != nil || !sprintCreateEqual(input, expectedInput) || json.Unmarshal(receiptRaw, &stored) != nil || !reflect.DeepEqual(stored, result) || json.Unmarshal(planRaw, &plan) != nil || plan.BeforeMilestone != nil || plan.BeforeSprint != nil || plan.GroupBefore == nil || *plan.GroupBefore != w.generation || !reflect.DeepEqual(plan.After, result) || revision < 1 || digest != semantic.String() || user != actor.Details().UserID || eventID != result.EventID.String() {
		return errors.New("Create command/plan differs from original identity and full intent")
	}
	h, p := plan.Header, plan.Payload
	if h.Validate() != nil || p.Validate() != nil || h.EventID != *result.EventID || h.EventType != wc.MilestoneChangedName || h.AggregateType != wc.MilestoneAggregate || h.AggregateID.String() != v.ID.String() || h.AggregateVersion == nil || *h.AggregateVersion != 1 || h.AggregateSequence != nil || h.SchemaVersion != wc.WorkSchemaVersion || h.Scope.Kind != eventc.ProjectScope || h.Scope.ProjectID.String() != w.project.Project.ID.String() || h.OccurredAt != v.CreatedAt || p.CommandID.String() != commandID || p.ActorUserID.String() != user || p.Change != wc.CreatedChange || !slices.Equal(p.ChangedFields, []wc.ChangedField{wc.DescriptionChanged, wc.RankChanged, wc.TitleChanged}) || p.Position == nil || p.Position.OrderGeneration != w.generation+1 || p.Position.PreviousID != nil || p.Position.NextID != nil {
		return errors.New("Create event lost exact first Milestone position/actor/version")
	}
	var headerRaw, payloadRaw []byte
	var eventColumns bool
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='work.milestone_changed' AND schema_version=1 AND scope='project' AND aggregate_type='work.milestone' AND aggregate_version=1 AND aggregate_sequence IS NULL AND occurred_at=$4 FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND aggregate_id=$2::text::uuid AND id=$3::text::uuid AND producer='work'`, w.project.Project.ID.String(), v.ID.String(), eventID, v.CreatedAt.Time()).Scan(&headerRaw, &payloadRaw, &eventColumns)
	if err != nil {
		return err
	}
	actualHeader, err := eventc.DecodeHeader(headerRaw)
	var actualPayload wc.MilestoneChanged
	if err != nil || !eventColumns || json.Unmarshal(payloadRaw, &actualPayload) != nil || !reflect.DeepEqual(actualHeader, h) || !reflect.DeepEqual(actualPayload, p) {
		return errors.New("Create Outbox differs from original command plan")
	}
	var canonical bool
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT title=$3 AND description=$4 AND manual_rank=$5 AND version=1 AND created_at=$6 AND updated_at=$6 FROM agenteam_work.milestones WHERE project_id=$1::text::uuid AND id=$2::text::uuid`,w.project.Project.ID.String(),v.ID.String(),v.Title,v.Description,v.ManualRank,v.CreatedAt.Time()).Scan(&canonical)
	if err != nil {
		return err
	}
	if !canonical {
		return errors.New("new canonical Milestone differs from receipt")
	}
	var facts [9]int64
	err=w.a.p2.base.raw.QueryRow(ctx,`SELECT
 (SELECT count(*) FROM agenteam_work.structure_commands WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id=$1::text::uuid AND producer='work'),
 (SELECT order_generation FROM agenteam_work.milestone_order_groups WHERE project_id=$1::text::uuid),
 (SELECT order_generation FROM agenteam_work.sprint_order_groups WHERE project_id=$1::text::uuid AND milestone_id=$2::text::uuid),
 (SELECT count(*) FROM agenteam_work.milestones WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.sprints WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_work.tasks WHERE project_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_scheduler.dispatches WHERE project_id=$1::text),
 (SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1::text)`,w.project.Project.ID.String(),v.ID.String()).Scan(&facts[0],&facts[1],&facts[2],&facts[3],&facts[4],&facts[5],&facts[6],&facts[7],&facts[8])
	if err!=nil{return err}
	if facts!=([9]int64{1,1,int64(w.generation)+1,1,1,0,0,0,0}) {return errors.New("Create duplicated facts or created a Sprint/Task/Execution")}
	snapshot, err := w.unchanged(ctx)
	if err != nil {
		return err
	}
	if snapshot != w.immutable {
		return errors.New("Create changed original current/paused/empty Work facts")
	}
	return nil
}
func (w *milestoneCreateWeb) verify(t *testing.T, ctx context.Context) {
	w.mu.Lock()
	failure := w.failure
	creates, newReads, listReads, emptyLists := w.creates, w.newReads, w.listReads, w.emptySprintLists
	in, result := w.createIntent, w.created.Clone()
	w.mu.Unlock()
	if failure != nil || creates != 1 || newReads < 1 || listReads < 1 || emptyLists < 1 || w.cut != 0 {
		t.Fatal("Milestone Create original browser facts incomplete", failure)
	}
	agentAppRequire(t, w.committedFacts(ctx, in, result))
	body := jsonBytes(t, map[string]any{"command": wc.MilestoneCreate, "request": in.request})
	reply := w.request(t, http.MethodPost, variableHTTPPath(w.project.Project.ID, "/structure-commands/lookup"), body, in.key)
	var found wc.CommandLookup
	if reply.status != 200 || json.Unmarshal(reply.body, &found) != nil || found.Validate() != nil || found.State != wc.LookupCommitted || found.Result == nil || !reflect.DeepEqual(*found.Result, result) {
		t.Fatal("original-key Milestone Create Lookup lost its original full result")
	}
	agentAppRequire(t, w.committedFacts(ctx, in, result))
}
