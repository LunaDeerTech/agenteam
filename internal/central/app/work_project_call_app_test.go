//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

// The ordinary default bindAccounts graph owns both HTTP and shutdown. No
// library fixture replaces Project, Work, the guard, or a lifecycle registry.
func TestWorkProjectCallDefaultApp(t *testing.T) {
	t.Run("default-http-original-owner", testWorkAppHTTP)
	t.Run("committed-return-before-root-drain", testWorkAppOriginalReturn)
}

type workAppReturn struct {
	callID   string
	result   f.CommitResult
	returned chan struct{}
}

type workAppStore struct {
	*postgres.Store
	mu      sync.Mutex
	project string
	entered chan workAppReturn
	release chan struct{}
	once    sync.Once
	stopped chan struct{}
	stop    sync.Once
}

func (s *workAppStore) unhold() { s.once.Do(func() { close(s.release) }) }
func (s *workAppStore) StopAdmission() {
	s.stop.Do(func() { close(s.stopped) })
	s.Store.StopAdmission()
}

// Preserve the real BEGIN, callback, COMMIT and result. The selected technical
// return has already physically committed before this outer Store returns.
// The observer never masks an acknowledgement or cancels a database statement.
func (s *workAppStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	d := cause.Details()
	s.mu.Lock()
	selected := s.project != "" && d.Kind == f.RecoveryCause && d.Owner == "work-call-return" && d.CheckpointRef == s.project
	if selected {
		s.project = ""
	}
	s.mu.Unlock()
	result := s.Store.WithinTx(ctx, cause, fn)
	if selected {
		returned := make(chan struct{})
		defer close(returned)
		s.entered <- workAppReturn{d.RecoveryRunID, result, returned}
		<-s.release
	}
	return result
}

type workAppCall struct {
	ID         string     `json:"id"`
	Project    string     `json:"project_id"`
	Process    string     `json:"process_id"`
	Instance   string     `json:"instance_id"`
	Lane       string     `json:"lane"`
	Class      string     `json:"class"`
	Phase      string     `json:"phase"`
	Registered time.Time  `json:"registered_at"`
	Closed     *time.Time `json:"closed_at"`
}

type workAppFixture struct {
	root     *modelRootApp
	store    *workAppStore
	assembly *accountAssembly
	planning *workPlanningAssembly
	projects *projectCommandWork
	objects  *objectAssembly
	ctx      context.Context
	actor    id.Actor
	cookie   string
	csrf     string
	address  string
	client   *http.Client
	claim    *os.File
	admin    *pgx.Conn
	a, b     id.ProjectID
	instance string
	signal   sync.Once
}

func workAppCheck(t *testing.T, stage string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var fault *f.Fault
	if errors.As(err, &fault) {
		t.Fatalf("Work App %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
	}
	t.Fatalf("Work App %s failed", stage)
}

func workAppJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	workAppCheck(t, "encode-original-request", err)
	return raw
}

func newWorkAppFixture(t *testing.T) *workAppFixture {
	t.Helper()
	v := &workAppFixture{store: &workAppStore{entered: make(chan workAppReturn, 1), release: make(chan struct{}), stopped: make(chan struct{})}, client: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 8 * time.Second}}
	v.root = newModelRootApp(t, "5s", func(root *modelRootApp, deps *dependencies) {
		deps.open = func(ctx context.Context, cfg postgres.Config) (database, error) {
			raw, err := postgres.Open(ctx, cfg)
			root.store.Store, v.store.Store = raw, raw
			return v.store, err
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	v.ctx = ctx
	// Register before any seed/observation can fail. Never drop the fixture DB
	// or spool under a still-returning original App owner.
	t.Cleanup(func() {
		v.store.unhold()
		v.requestStop()
		select {
		case <-v.root.done:
		case <-time.After(7 * time.Second):
			t.Error("original Work App did not return; retaining its dependencies")
			v.root.cancel()
			<-v.root.done
		}
		v.client.CloseIdleConnections()
		cancel()
		if v.claim != nil {
			if err := syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Error("App returned before original guard release; retaining fixture")
				for err != nil {
					if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
						select {}
					}
					err = syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX)
				}
			}
			_ = syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN)
			_ = v.claim.Close()
		}
	})
	v.address = v.root.address(t)
	v.admin = v.root.db.Connect(t)
	if _, enabled := v.root.cfg.ExecutionRuntime(); enabled {
		t.Fatal("Work App fixture enabled Execution Runtime")
	}
	v.assembly = v.root.owned.accounts().(*accountAssembly)
	v.assembly.mu.Lock()
	v.planning, _ = v.assembly.planning.(*workPlanningAssembly)
	v.projects, _ = v.assembly.projects.(*projectCommandWork)
	v.assembly.mu.Unlock()
	v.objects, _ = v.root.owned.objects().(*objectAssembly)
	if v.planning == nil || v.planning.ownership == nil || v.projects == nil || v.objects == nil || v.objects.guard == nil {
		t.Fatal("default root omitted its original Work/Project/Object owner")
	}
	p := v.planning.ownership
	if v.planning.structure != p.Service() || v.planning.tasks != p.TaskService() || v.planning.blockers != p.BlockerService() || v.planning.transitions != p.TaskTransitionService() || v.planning.sprints != p.SprintLifecycleService() || v.planning.structureReader != p.Reader() || v.planning.taskReader != p.TaskReader() || v.planning.blockerReader != p.BlockerReader() || len(v.planning.commands) != 1 || v.planning.commands[0] != p {
		t.Fatal("default HTTP and shutdown did not receive the same original five-service/three-reader pair")
	}
	var err error
	v.claim, err = os.OpenFile(filepath.Join(v.root.cfg.Objects().SpoolDirectory()+".processes", v.objects.process.String()+".claim"), os.O_RDWR, 0)
	workAppCheck(t, "open-original-guard-observation", err)
	v.requireGuard(t)
	login, err := fixtureAccountLoginResponse(ctx, v.root.cfg, v.root.core)
	workAppCheck(t, "formal-login", err)
	var material sc.SecretMaterial
	err = login.UseCookie(func(raw []byte) error { var e error; material, e = sc.NewSecretMaterial(raw); return e })
	closed := login.Close(ctx)
	defer material.Destroy()
	workAppCheck(t, "login-cookie", err)
	workAppCheck(t, "login-original-close", closed)
	v.actor, err = v.root.core.Authenticate(ctx, material)
	workAppCheck(t, "current-session", err)
	session, err := v.root.core.GetSession(ctx, material)
	workAppCheck(t, "session-csrf", err)
	defer session.CSRF.Destroy()
	workAppCheck(t, "cookie-material", material.Use(func(raw []byte) error { v.cookie = string(raw); return nil }))
	workAppCheck(t, "csrf-material", session.CSRF.Use(func(raw []byte) error { v.csrf = string(raw); return nil }))
	for n, target := range []*id.ProjectID{&v.a, &v.b} {
		*target = guardID[id.Project](t)
		name := "work-app-a"
		if n == 1 {
			name = "work-app-b"
		}
		created, err := v.projects.service.CreateProject(ctx, v.actor, f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: f.IdempotencyKey(guardID[struct{}](t).String())}, pc.CreateProjectRequest{ProjectID: *target, Name: name})
		workAppCheck(t, "installed-project-create", err)
		if created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != *target || created.Operation != nil {
			t.Fatal("installed Project and original Skill initializer did not return ready")
		}
	}
	return v
}

func (v *workAppFixture) requestStop() { v.signal.Do(func() { v.root.signals <- syscall.SIGTERM }) }
func (v *workAppFixture) requireGuard(t *testing.T) {
	t.Helper()
	err := syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN)
		t.Fatal("original App released its process guard before its Work return")
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatal("original process guard observation failed")
	}
}

func (v *workAppFixture) calls(t *testing.T, project id.ProjectID) []workAppCall {
	t.Helper()
	var raw []byte
	workAppCheck(t, "original-call-ledger", v.admin.QueryRow(v.ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(c) ORDER BY id),'[]'::jsonb) FROM agenteam_work.project_calls c WHERE project_id=$1::uuid`, project.String()).Scan(&raw))
	var rows []workAppCall
	workAppCheck(t, "decode-call-ledger", json.Unmarshal(raw, &rows))
	return rows
}

func (v *workAppFixture) closed(t *testing.T, project id.ProjectID, before []workAppCall, lane, class string) workAppCall {
	t.Helper()
	after := v.calls(t, project)
	old := make(map[string]workAppCall, len(before))
	for _, row := range before {
		old[row.ID] = row
	}
	var added []workAppCall
	for _, row := range after {
		if previous, ok := old[row.ID]; ok {
			if !reflect.DeepEqual(previous, row) {
				t.Fatal("HTTP call rewrote earlier Work ownership history")
			}
		} else {
			added = append(added, row)
		}
	}
	if len(after) != len(before)+1 || len(added) != 1 {
		t.Fatal("one original HTTP call did not retain exactly one distinct CallID")
	}
	r := added[0]
	if _, err := f.ParseID[struct{}](r.ID); err != nil {
		t.Fatal("original Work CallID is not a typed UUID")
	}
	if _, err := f.ParseID[struct{}](r.Instance); err != nil {
		t.Fatal("original Work instance is not a typed UUID")
	}
	if r.Project != project.String() || r.Process != v.objects.process.String() || r.Lane != lane || r.Class != class || r.Phase != "business_closed" || r.Registered.IsZero() || r.Closed == nil || r.Closed.Before(r.Registered) {
		t.Fatal("original HTTP call omitted its exact closed identity")
	}
	if v.instance == "" {
		v.instance = r.Instance
	} else if v.instance != r.Instance {
		t.Fatal("default Work services/readers/Projects split across original instances")
	}
	return r
}

type workAppResponse struct {
	status int
	body   []byte
}

func (v *workAppFixture) request(ctx context.Context, method, path string, body []byte, key string) (workAppResponse, error) {
	req, err := http.NewRequestWithContext(ctx, method, v.address+path, bytes.NewReader(body))
	if err != nil {
		return workAppResponse{}, err
	}
	req.Host = "localhost:8080"
	req.Header.Set("Origin", "http://localhost:8080")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	// newModelRootApp uses the original explicit localhost HTTP origin.
	req.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: v.cookie})
	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", v.csrf)
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := v.client.Do(req)
	if err != nil {
		return workAppResponse{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 {
		return workAppResponse{}, errors.New("original HTTP body did not return")
	}
	if len(response.Header.Values("X-Request-ID")) != 1 {
		return workAppResponse{}, errors.New("HTTP request identity missing")
	}
	if _, err := f.ParseID[f.Request](response.Header.Get("X-Request-ID")); err != nil {
		return workAppResponse{}, err
	}
	return workAppResponse{response.StatusCode, raw}, nil
}

func (v *workAppFixture) call(t *testing.T, project id.ProjectID, method, suffix, lane string, body any) []byte {
	t.Helper()
	before := v.calls(t, project)
	var raw []byte
	class, key := "read", ""
	if method != http.MethodGet {
		class, key = "mutation", guardID[struct{}](t).String()
		raw = workAppJSON(t, body)
	}
	r, err := v.request(v.ctx, method, "/api/v1/projects/"+project.String()+suffix, raw, key)
	workAppCheck(t, "normal-http-original-return", err)
	if r.status != http.StatusOK {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(r.body, &problem)
		t.Fatalf("Work App HTTP lane=%s class=%s status=%d code=%s", lane, class, r.status, problem.Code.Safe())
	}
	v.closed(t, project, before, lane, class)
	return r.body
}

type workAppSeed struct {
	milestone wc.Milestone
	sprint    wc.Sprint
	task      wc.Task
	blocker   wc.TaskBlocker
}

func (v *workAppFixture) seed(t *testing.T) workAppSeed {
	t.Helper()
	var out workAppSeed
	m := wc.CreateMilestoneRequest{MilestoneID: guardID[wc.Milestone](t), Title: "Original App milestone"}
	var structure wc.StructureMutation
	workAppCheck(t, "milestone-receipt", json.Unmarshal(v.call(t, v.a, http.MethodPost, "/milestones", "structure", map[string]any{"request": m}), &structure))
	if structure.Validate() != nil || structure.Milestone == nil || structure.Milestone.ID != m.MilestoneID {
		t.Fatal("formal milestone receipt differs")
	}
	out.milestone = *structure.Milestone
	s := wc.CreateSprintRequest{SprintID: guardID[pc.Sprint](t), MilestoneID: m.MilestoneID, Title: "Original App sprint"}
	workAppCheck(t, "sprint-receipt", json.Unmarshal(v.call(t, v.a, http.MethodPost, "/sprints", "structure", map[string]any{"request": s}), &structure))
	if structure.Validate() != nil || structure.Sprint == nil || structure.Sprint.ID != s.SprintID {
		t.Fatal("formal sprint receipt differs")
	}
	out.sprint = *structure.Sprint
	q := wc.TaskCreate{TaskID: guardID[wc.Task](t), SprintID: s.SprintID, Title: "Original App task", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "No Execution is launched"}
	var task wc.TaskMutation
	workAppCheck(t, "task-receipt", json.Unmarshal(v.call(t, v.a, http.MethodPost, "/tasks", "task", map[string]any{"request": q}), &task))
	if task.Validate() != nil || !task.Changed || task.Task.ID != q.TaskID {
		t.Fatal("formal Task receipt differs")
	}
	b := wc.TaskBlockerCreate{BlockerID: guardID[wc.TaskBlockerIdentity](t), Type: wc.TaskBlockerWaitingForHuman, Description: "Original human wait", Metadata: wc.TaskBlockerMetadata{WaitingForHuman: &wc.TaskBlockerWaitingForHumanMetadata{}}}
	var blocked wc.TaskBlockerMutation
	workAppCheck(t, "blocker-receipt", json.Unmarshal(v.call(t, v.a, http.MethodPost, "/tasks/"+q.TaskID.String()+"/blockers", "blocker", map[string]any{"request": b, "expected_version": task.Task.Version}), &blocked))
	if blocked.Validate() != nil || blocked.Task.ID != q.TaskID || blocked.Blocker.ID != b.BlockerID {
		t.Fatal("formal Human blocker receipt differs")
	}
	out.task, out.blocker = blocked.Task, blocked.Blocker
	return out
}

func (v *workAppFixture) business(t *testing.T) string {
	t.Helper()
	var raw string
	workAppCheck(t, "business-snapshot", v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]') FROM agenteam_work.milestones x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]') FROM agenteam_work.sprints x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]') FROM agenteam_work.tasks x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]') FROM agenteam_work.task_blockers x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]') FROM agenteam_work.structure_commands x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]') FROM agenteam_work.task_commands x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]') FROM agenteam_work.task_blocker_commands x WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(x) ORDER BY to_jsonb(x)::text),'[]') FROM agenteam_work.task_events x WHERE project_id=$1::uuid))::text`, v.a.String()).Scan(&raw))
	return raw
}

func (v *workAppFixture) joined(t *testing.T) {
	t.Helper()
	v.requestStop()
	select {
	case <-v.root.done:
	case <-time.After(7 * time.Second):
		t.Fatal("original App did not return after Work release")
	}
	workAppCheck(t, "original-root-return", v.root.err)
	if !v.planning.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("root returned without the original Work/Account owners")
	}
	workAppCheck(t, "original-guard-release", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	workAppCheck(t, "guard-observation-unlock", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN))
	var stopped, borrowersGone bool
	var executions, operations, invocations int64
	workAppCheck(t, "actual-root-exit-facts", v.admin.QueryRow(v.ctx, `SELECT
 (SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1::uuid),
 NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam'),
 (SELECT count(*) FROM agenteam_execution.executions),(SELECT count(*) FROM agenteam_tool.operations),(SELECT count(*) FROM agenteam_model.invocations)`, v.objects.process.String()).Scan(&stopped, &borrowersGone, &executions, &operations, &invocations))
	if !stopped || !borrowersGone || executions != 0 || operations != 0 || invocations != 0 {
		t.Fatal("root retained borrowers or launched unrelated work")
	}
	for _, p := range []id.ProjectID{v.a, v.b} {
		for _, row := range v.calls(t, p) {
			if row.Process != v.objects.process.String() || row.Instance != v.instance || row.Phase != "business_closed" || row.Closed == nil {
				t.Fatal("original Work call was not closed at actual App exit")
			}
		}
	}
}

func testWorkAppHTTP(t *testing.T) {
	v := newWorkAppFixture(t)
	s := v.seed(t)
	before, business := v.calls(t, v.a), v.business(t)
	var milestone wc.Milestone
	workAppCheck(t, "get-milestone", json.Unmarshal(v.call(t, v.a, http.MethodGet, "/milestones/"+s.milestone.ID.String(), "structure", nil), &milestone))
	var sprint wc.Sprint
	workAppCheck(t, "get-sprint", json.Unmarshal(v.call(t, v.a, http.MethodGet, "/sprints/"+s.sprint.ID.String(), "structure", nil), &sprint))
	var task wc.Task
	workAppCheck(t, "get-task", json.Unmarshal(v.call(t, v.a, http.MethodGet, "/tasks/"+s.task.ID.String(), "task", nil), &task))
	if !reflect.DeepEqual(milestone, s.milestone) || !reflect.DeepEqual(sprint, s.sprint) || !reflect.DeepEqual(task, s.task) {
		t.Fatal("nonempty HTTP details differ from original receipts")
	}
	for _, c := range []struct {
		suffix, lane, target string
		version              f.Version
	}{
		{"/milestones?limit=10", "structure", s.milestone.ID.String(), s.milestone.Version},
		{"/sprints?limit=10&milestone_id=" + s.milestone.ID.String(), "structure", s.sprint.ID.String(), s.sprint.Version},
		{"/tasks?limit=10&sprint_id=" + s.sprint.ID.String(), "task", s.task.ID.String(), s.task.Version},
	} {
		var page struct {
			Items []struct {
				ID      string       `json:"id"`
				Project id.ProjectID `json:"project_id"`
				Version f.Version    `json:"version"`
			} `json:"items"`
			Next string `json:"next_cursor"`
		}
		workAppCheck(t, "nonempty-http-page", json.Unmarshal(v.call(t, v.a, http.MethodGet, c.suffix, c.lane, nil), &page))
		if len(page.Items) != 1 || page.Next != "" || page.Items[0].ID != c.target || page.Items[0].Project != v.a || page.Items[0].Version != c.version {
			t.Fatal("HTTP summary lost original nonempty identity")
		}
	}
	var events f.Page[wc.TaskTimelineEvent]
	workAppCheck(t, "task-events-page", json.Unmarshal(v.call(t, v.a, http.MethodGet, "/tasks/"+s.task.ID.String()+"/events?limit=10", "task", nil), &events))
	if len(events.Items) != 2 || events.NextCursor != "" {
		t.Fatal("original create/blocker timeline differs")
	}
	found := false
	for _, event := range events.Items {
		h, err := event.Header()
		workAppCheck(t, "task-event-header", err)
		if event.Validate() != nil || h.ProjectID != v.a || h.TaskID != s.task.ID {
			t.Fatal("HTTP timeline lost original event identity")
		}
		found = found || event.Blocker != nil
	}
	var blockers f.Page[wc.TaskBlocker]
	workAppCheck(t, "task-blockers-page", json.Unmarshal(v.call(t, v.a, http.MethodGet, "/tasks/"+s.task.ID.String()+"/blockers?limit=10", "blocker", nil), &blockers))
	if !found || len(blockers.Items) != 1 || blockers.NextCursor != "" || !reflect.DeepEqual(blockers.Items[0], s.blocker) {
		t.Fatal("HTTP blocker/timeline original value differs")
	}
	after := v.calls(t, v.a)
	counts := map[string]int{}
	for _, row := range after {
		if row.Class == "read" {
			counts[row.Lane]++
		}
	}
	if len(after) != len(before)+8 || counts["structure"] != 4 || counts["task"] != 3 || counts["blocker"] != 1 || v.business(t) != business {
		t.Fatal("eight HTTP readers did not preserve their original lanes and business history")
	}
	var empty f.Page[json.RawMessage]
	workAppCheck(t, "other-project-http", json.Unmarshal(v.call(t, v.b, http.MethodGet, "/milestones?limit=10", "structure", nil), &empty))
	if len(empty.Items) != 0 || empty.NextCursor != "" {
		t.Fatal("Project B borrowed Project A's records")
	}
	v.joined(t)
	t.Log("WORK_DEFAULT_APP_HTTP original_default_graph=true five_services_three_readers_same_pair=true real_http_mutations=4 real_http_reads=8 distinct_closed_calls=true other_project=true no_runtime_invoke=true no_project_delete=true")
}

func testWorkAppOriginalReturn(t *testing.T) {
	v := newWorkAppFixture(t)
	s := v.seed(t)
	before, business := v.calls(t, v.a), v.business(t)
	v.store.mu.Lock()
	v.store.project = v.a.String()
	v.store.mu.Unlock()
	done := make(chan struct{})
	var response workAppResponse
	var callErr error
	go func() {
		defer close(done)
		response, callErr = v.request(v.ctx, http.MethodGet, "/api/v1/projects/"+v.a.String()+"/tasks/"+s.task.ID.String(), nil, "")
	}()
	defer func() {
		v.store.unhold()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("original held Work HTTP did not return; retaining dependencies")
			<-done
		}
	}()
	var held workAppReturn
	select {
	case held = <-v.store.entered:
	case <-done:
		workAppCheck(t, "read-before-return-barrier", callErr)
		t.Fatal("original Work return barrier was not reached")
	case <-time.After(3 * time.Second):
		t.Fatal("original Work return barrier was not reached")
	}
	if held.result.State() != f.Committed {
		t.Fatal("selected original Work close was not physically committed")
	}
	row := v.closed(t, v.a, before, "task", "read")
	if row.ID != held.callID || v.business(t) != business {
		t.Fatal("committed technical return differs from its original read")
	}
	select {
	case <-done:
		t.Fatal("HTTP returned while its original Store return remained held")
	default:
	}
	var page f.Page[json.RawMessage]
	workAppCheck(t, "other-project-during-held-return", json.Unmarshal(v.call(t, v.b, http.MethodGet, "/milestones?limit=10", "structure", nil), &page))
	if len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatal("unrelated Project read changed")
	}
	v.requestStop()
	v.root.logs.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		v.planning.mu.Lock()
		stopped := v.planning.stopped
		v.planning.mu.Unlock()
		if stopped {
			break
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("root did not stop its installed Work owner")
		case <-v.root.done:
			t.Fatal("root returned before original Work Stop")
		}
	}
	if v.planning.Joined() || v.assembly.Joined() {
		t.Fatal("closed database row was mistaken for original Work return")
	}
	select {
	case <-v.root.done:
		t.Fatal("App returned while original Work Store was held")
	case <-v.store.stopped:
		t.Fatal("root stopped database before original Work return")
	case <-done:
		t.Fatal("HTTP returned before Store release")
	default:
	}
	v.requireGuard(t)
	v.store.unhold()
	select {
	case <-held.returned:
	case <-time.After(3 * time.Second):
		t.Fatal("released original Store did not return")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("released original HTTP did not return")
	}
	workAppCheck(t, "held-http-original-return", callErr)
	// Graceful root shutdown does not cancel the original HTTP serving context.
	// Work's local cancellation cannot rewrite this already-known read result.
	var actual wc.Task
	workAppCheck(t, "released-original-http-value", json.Unmarshal(response.body, &actual))
	if response.status != http.StatusOK || !reflect.DeepEqual(actual, s.task) {
		t.Fatal("release changed the original successful HTTP read")
	}
	if !reflect.DeepEqual(v.calls(t, v.a), appendWorkAppCall(before, row)) || v.business(t) != business {
		t.Fatal("release changed original business or ownership history")
	}
	v.joined(t)
	t.Log("WORK_DEFAULT_APP_RETURN original_close_committed=true store_return_held=true http_not_returned=true other_project_usable=true stopped_pair_not_joined=true guard_and_database_retained=true actual_root_return=true no_unknown_or_force=true")
}

func appendWorkAppCall(before []workAppCall, row workAppCall) []workAppCall {
	// SQL orders UUID identities; creation time does not order UUIDv7 entropy.
	rows := append(append([]workAppCall(nil), before...), row)
	slices.SortFunc(rows, func(a, b workAppCall) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	return rows
}
