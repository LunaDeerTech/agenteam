//go:build integration

package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// This uses the default Runtime-enabled bindAccounts graph. The manager's
// first genuine empty directory transaction is held AFTER COMMIT until Stop.
// That bounded original-return control prevents discovery/Advance/Invoke;
// it is not evidence of a successful Preparation or Direct worker.
func TestExecutionSchedulerProjectCallDefaultApp(t *testing.T) {
	t.Run("default-owned-graph-and-original-launch", func(t *testing.T) {
		v := newExecutionSchedulerApp(t)
		v.seed(t)
		v.originalFacts(t, 1)
		v.otherProject(t)
		v.stopBeforeRelease(t)
		v.store.releaseDirectory()
		v.joined(t)
		t.Log("EXECUTION_SCHEDULER_DEFAULT_APP original_default_runtime=true original_four_execution_owners=true original_six_scheduler_owners_and_manager=true first_launch_created_origin=true closed_original_ledgers=true other_project=true directory_return_controlled=true advance_not_reached=true preparation_direct_workers_not_covered=true no_invoke=true no_default_registry=true")
	})
	t.Run("committed-lookup-return-before-root-drain", testExecutionSchedulerAppReturn)
}

const executionSchedulerAppConfig = `{"profile":"direct-text-v1","project_runners":{"max_projects":3,"project_page_size":64,"execution_page_size":128,"discovery_interval":"2s","tick_interval":"250ms","relaunch_skip_count":0,"launch_policy":{"schema_version":"1","denied_tool_ids":[],"allowed_resource_constraints":[]}},"associated_executor":{"max_owned":4,"recovery_interval":"500ms"},"model_agent_retry":{"initial_request_timeout":"30s","max_request_timeout":"120s","timeout_multiplier":2,"initial_backoff":"200ms","max_backoff":"3s"}}`

type executionSchedulerAppReturn struct {
	result   f.CommitResult
	call     string
	empty    bool
	returned chan struct{}
}

type executionSchedulerAppStore struct {
	*postgres.Store
	observeCommit                            func(f.TransactionCause, f.CommitResult)
	mu                                       sync.Mutex
	directorySelected                        bool
	lookupProject                            string
	directory, lookup                        chan executionSchedulerAppReturn
	directoryRelease, lookupRelease, stopped chan struct{}
	directoryOnce, lookupOnce, stopOnce      sync.Once
}

func (s *executionSchedulerAppStore) releaseDirectory() {
	s.directoryOnce.Do(func() { close(s.directoryRelease) })
}
func (s *executionSchedulerAppStore) releaseLookup() {
	s.lookupOnce.Do(func() { close(s.lookupRelease) })
}
func (s *executionSchedulerAppStore) StopAdmission() {
	s.stopOnce.Do(func() { close(s.stopped) })
	s.Store.StopAdmission()
}
func (s *executionSchedulerAppStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	d := cause.Details()
	s.mu.Lock()
	directory := !s.directorySelected && d.Kind == f.RecoveryCause && d.Owner == "project.scheduler-projects"
	if directory {
		s.directorySelected = true
	}
	lookup := s.lookupProject != "" && d.Kind == f.RecoveryCause && d.Owner == "execution-call-return" && d.CheckpointRef == s.lookupProject
	if lookup {
		s.lookupProject = ""
	}
	s.mu.Unlock()
	empty := false
	result := s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if directory {
			x, err := s.Store.InTx(tx)
			if err != nil {
				return err
			}
			// Observe the first directory's real empty database, in the same Tx.
			return x.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM agenteam_project.projects WHERE initialized_at IS NOT NULL AND lifecycle='active')`).Scan(&empty)
		}
		return nil
	})
	if s.observeCommit != nil {
		s.observeCommit(cause, result)
	}
	if directory || lookup {
		returned := make(chan struct{})
		defer close(returned)
		observed := executionSchedulerAppReturn{result: result, call: d.RecoveryRunID, empty: empty, returned: returned}
		if directory {
			s.directory <- observed
			<-s.directoryRelease
		} else {
			s.lookup <- observed
			<-s.lookupRelease
		}
	}
	return result
}

type executionSchedulerApp struct {
	*workAppFixture
	store         *executionSchedulerAppStore
	runtime       *executionRuntimeAssembly
	directory     executionSchedulerAppReturn
	lookupDone    chan struct{}
	lookup        ec.LaunchLookup
	lookupErr     error
	agent         id.AgentID
	launchRequest ec.LaunchRequest
	execution     ec.Summary
	dispatch      scheduler.Dispatch
	digest        f.Digest
}

func newExecutionSchedulerApp(t *testing.T) *executionSchedulerApp {
	return newExecutionSchedulerAppWith(t, nil)
}

// Configure only test observation and the original Store boundary. The
// default constructor retains the same real bindAccounts graph.
func newExecutionSchedulerAppWith(t *testing.T, configure func(*dependencies, *executionSchedulerApp)) *executionSchedulerApp {
	t.Helper()
	v := &executionSchedulerApp{workAppFixture: &workAppFixture{client: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 8 * time.Second}}, store: &executionSchedulerAppStore{directory: make(chan executionSchedulerAppReturn, 1), lookup: make(chan executionSchedulerAppReturn, 1), directoryRelease: make(chan struct{}), lookupRelease: make(chan struct{}), stopped: make(chan struct{})}}
	// Same root fixture and one provisioning call; the existing constructor's
	// immutable disabled Config cannot be changed into an enabled deployment.
	db := pgfixture.NewDatabase(t)
	env := append(guardEnvironment(t, db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+executionSchedulerAppConfig,
		"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s")
	r := &modelRootApp{db: db, cfg: guardConfiguration(t, env), store: &modelRootStore{}, logs: newEventLog(), signals: make(chan os.Signal, 4), done: make(chan struct{})}
	v.root = r
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	var cancelBudget context.CancelFunc
	v.ctx, cancelBudget = context.WithTimeout(context.Background(), 90*time.Second)
	deps := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			raw, err := postgres.Open(ctx, cfg)
			r.store.Store, v.store.Store = raw, raw
			return v.store, err
		},
		bind: func(ctx context.Context, cfg config.Config, db database, owned *resources, d *dependencies) error {
			r.owned, r.store.owned = owned, owned
			return bindAccounts(ctx, cfg, db, owned, d)
		},
		observeAccount: func(core *account.Service) { r.core = core },
		listen: func(ctx context.Context, network, address string) (net.Listener, error) {
			return (&net.ListenConfig{}).Listen(ctx, network, address)
		},
	}
	if configure != nil {
		configure(&deps, v)
	}
	logger, err := logging.New(logging.Central, slog.LevelInfo, r.logs)
	workAppCheck(t, "runtime-logger", err)
	// Install cleanup before starting the original run or arming either hold.
	t.Cleanup(func() {
		v.requestStop()
		// Cancel the actual Run lifetime as a failure fallback BEFORE releasing
		// discovery: a failed assertion must not let the manager invoke work.
		if v.runtime != nil {
			v.runtime.StopAdmission()
		}
		cancel()
		v.store.releaseLookup()
		v.store.releaseDirectory()
		if v.lookupDone != nil {
			<-v.lookupDone
		}
		select {
		case <-r.done:
		case <-time.After(7 * time.Second):
			t.Error("original Runtime App has not returned; retaining dependencies")
			<-r.done
		}
		v.client.CloseIdleConnections()
		cancelBudget()
		if v.claim != nil {
			if err := syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
				t.Error("App returned before original guard release; retaining dependencies")
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
	go func() { r.err = run(ctx, r.cfg, logger, r.signals, deps); close(r.done) }()
	v.address = workRootStartupAddress(t, r.logs, r.done, func() error { return r.err })
	v.admin = db.Connect(t)
	if _, enabled := r.cfg.ExecutionRuntime(); !enabled {
		t.Fatal("real App Runtime disabled")
	}
	v.assembly = r.owned.accounts().(*accountAssembly)
	v.assembly.mu.Lock()
	v.runtime = v.assembly.executions
	v.planning, _ = v.assembly.planning.(*workPlanningAssembly)
	v.projects, _ = v.assembly.projects.(*projectCommandWork)
	v.assembly.mu.Unlock()
	v.objects, _ = r.owned.objects().(*objectAssembly)
	if v.runtime == nil || v.runtime.executionCalls == nil || v.runtime.executionRetirement == nil || v.runtime.schedulerCalls == nil || v.runtime.manager == nil || v.planning == nil || v.planning.ownership == nil || v.projects == nil || v.objects == nil {
		t.Fatal("default App omitted its original owned graph")
	}
	e, s, w := v.runtime.executionCalls, v.runtime.schedulerCalls, v.planning.ownership
	if e.Service() == nil || e.Preparation() == nil || e.Direct() == nil || e.Associated() == nil || v.runtime.executor != e.Associated() || s.Coordinator() == nil || s.Visitor() == nil || s.Handoff() == nil || s.Busy() == nil || s.Failure() == nil || s.Relaunch() == nil || v.planning.structure != w.Service() || v.planning.tasks != w.TaskService() || v.planning.transitions != w.TaskTransitionService() || v.planning.sprints != w.SprintLifecycleService() || v.planning.structureReader != w.Reader() || v.planning.taskReader != w.TaskReader() || v.planning.blockerReader != w.BlockerReader() {
		t.Fatal("default graph split original owner views")
	}
	select {
	case <-v.runtime.executor.Ready():
	default:
		t.Fatal("original executor was not Ready at listening")
	}
	select {
	case v.directory = <-v.store.directory:
	case <-time.After(5 * time.Second):
		t.Fatal("original manager empty directory return was not reached")
	}
	if v.directory.result.State() != f.Committed || !v.directory.empty {
		t.Fatal("first manager page was not a real committed empty page")
	}
	select {
	case <-v.directory.returned:
		t.Fatal("directory returned before Ready/listening assertion")
	default:
	}
	observation := v.runtime.manager.Observation()
	if !observation.Started || observation.Stopped || len(observation.Projects) != 0 {
		t.Fatal("manager bypassed its original directory return")
	}
	v.claim, err = os.OpenFile(filepath.Join(r.cfg.Objects().SpoolDirectory()+".processes", v.objects.process.String()+".claim"), os.O_RDWR, 0)
	workAppCheck(t, "runtime-original-guard", err)
	v.requireGuard(t)
	v.loginAndProjects(t)
	return v
}

func (v *executionSchedulerApp) loginAndProjects(t *testing.T) {
	t.Helper()
	login, err := fixtureAccountLoginResponse(v.ctx, v.root.cfg, v.root.core)
	workAppCheck(t, "runtime-formal-login", err)
	var material sc.SecretMaterial
	err = login.UseCookie(func(raw []byte) error { var e error; material, e = sc.NewSecretMaterial(raw); return e })
	closed := login.Close(v.ctx)
	defer material.Destroy()
	workAppCheck(t, "runtime-login-material", err)
	workAppCheck(t, "runtime-login-return", closed)
	v.actor, err = v.root.core.Authenticate(v.ctx, material)
	workAppCheck(t, "runtime-session", err)
	session, err := v.root.core.GetSession(v.ctx, material)
	workAppCheck(t, "runtime-csrf", err)
	defer session.CSRF.Destroy()
	workAppCheck(t, "runtime-cookie", material.Use(func(raw []byte) error { v.cookie = string(raw); return nil }))
	workAppCheck(t, "runtime-csrf-material", session.CSRF.Use(func(raw []byte) error { v.csrf = string(raw); return nil }))
	for n, target := range []*id.ProjectID{&v.a, &v.b} {
		*target = guardID[id.Project](t)
		name := "runtime-app-a"
		if n == 1 {
			name = "runtime-app-b"
		}
		created, err := v.projects.service.CreateProject(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), pc.CreateProjectRequest{ProjectID: *target, Name: name})
		workAppCheck(t, "runtime-original-project-create", err)
		if created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != *target || created.Operation != nil {
			t.Fatal("original Project/Skill initialization not returned ready")
		}
	}
}

func executionSchedulerAppMeta(t *testing.T, version *f.Version) f.CommandMeta {
	return f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: f.IdempotencyKey(guardID[struct{}](t).String()), ExpectedVersion: version}
}

func (v *executionSchedulerApp) httpSeed(t *testing.T, suffix string, body any, out any) {
	t.Helper()
	r, err := v.request(v.ctx, http.MethodPost, "/api/v1/projects/"+v.a.String()+suffix, workAppJSON(t, body), guardID[struct{}](t).String())
	workAppCheck(t, "runtime-http-seed-"+suffix, err)
	if r.status != http.StatusOK {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(r.body, &problem)
		t.Fatalf("runtime HTTP seed %s status=%d code=%s", suffix, r.status, problem.Code.Safe())
	}
	workAppCheck(t, "runtime-http-receipt-"+suffix, json.Unmarshal(r.body, out))
}

func (v *executionSchedulerApp) seed(t *testing.T) {
	v.seedWithProvider(t, "https://runtime-app.invalid/v1", nil)
}

func (v *executionSchedulerApp) seedWithProvider(t *testing.T, endpoint string, credential *sc.CredentialID) {
	v.seedPendingWithProvider(t, endpoint, credential)
	var err error
	v.dispatch, err = v.runtime.schedulerCalls.Handoff().LaunchOnce(v.ctx, v.a, v.dispatch.Summary().ID)
	workAppCheck(t, "runtime-original-handoff-created", err)
	if v.dispatch.Summary().ExecutionID == nil || v.dispatch.Summary().Status != scheduler.Launched {
		t.Fatal("original Handoff did not return its Created association")
	}
	var createdAt time.Time
	var eid, state string
	var version int64
	var pristine bool
	workAppCheck(t, "runtime-original-execution-row", v.admin.QueryRow(v.ctx, `SELECT id,status,version,created_at,(cancel_requested_at IS NULL AND snapshot_id IS NULL AND started_at IS NULL AND completed_at IS NULL AND request_digest=$4) FROM agenteam_execution.executions WHERE project_id=$1::text AND agent_id=$2::text AND idempotency_key=$3`, v.a.String(), v.agent.String(), v.launchRequest.Meta.IdempotencyKey.String(), v.digest.String()).Scan(&eid, &state, &version, &createdAt, &pristine))
	instant, err := f.NewInstant(createdAt)
	workAppCheck(t, "runtime-created-instant", err)
	if eid != v.dispatch.Summary().ExecutionID.String() || state != string(ec.Created) || version != 1 || !pristine {
		t.Fatal("Created row differs from original Handoff")
	}
	v.execution = ec.Summary{ID: *v.dispatch.Summary().ExecutionID, ProjectID: v.a, AgentID: v.agent, Trigger: v.launchRequest.Trigger, Purpose: v.launchRequest.Purpose, Status: ec.Created, Version: 1, CreatedAt: instant}
}

// This is the same formal setup through the original committed Task claim.
// Tests of an uncertain Launch own that later call and its original result.
func (v *executionSchedulerApp) seedPendingWithProvider(t *testing.T, endpoint string, credential *sc.CredentialID) {
	t.Helper()
	var receipt mc.CommandReceipt
	v.httpSeed(t, "/model-providers", map[string]any{"input": map[string]any{"name": "runtime-app-provider", "protocol": mc.OpenAIChat, "base_url": endpoint, "enabled": true, "credential_ref": credential, "options": map[string]any{}}}, &receipt)
	provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "runtime-provider-id", err)
	v.httpSeed(t, "/models", map[string]any{"provider_id": provider, "input": map[string]any{"notes": "runtime-app-model", "provider_model_id": "runtime-app-model", "type": mc.ChatModel, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false, "reasoning_toggle_mode": nil, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": nil}}}, &receipt)
	modelID, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "runtime-model-id", err)
	v.agent = guardID[id.Agent](t)
	disabled := false
	request, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: v.agent, Name: "runtime-app-agent", Description: "Original installed owner", Instructions: "No model invocation", ModelRef: modelID, ApprovalPolicy: ac.ApprovalAuto, ApprovalModelRef: &modelID, AllowedToolIDs: []id.ToolID{}, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled})
	workAppCheck(t, "runtime-agent-request", err)
	_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, request)
	workAppCheck(t, "runtime-original-agent-create", err)
	m := wc.CreateMilestoneRequest{MilestoneID: guardID[wc.Milestone](t), Title: "Runtime App milestone"}
	var structure wc.StructureMutation
	v.httpSeed(t, "/milestones", map[string]any{"request": m}, &structure)
	if structure.Validate() != nil || structure.Milestone == nil || structure.Milestone.ID != m.MilestoneID {
		t.Fatal("original milestone receipt differs")
	}
	s := wc.CreateSprintRequest{SprintID: guardID[pc.Sprint](t), MilestoneID: m.MilestoneID, Title: "Runtime App sprint"}
	v.httpSeed(t, "/sprints", map[string]any{"request": s}, &structure)
	if structure.Validate() != nil || structure.Sprint == nil || structure.Sprint.ID != s.SprintID {
		t.Fatal("original Sprint receipt differs")
	}
	sprint := *structure.Sprint
	q := wc.TaskCreate{TaskID: guardID[wc.Task](t), SprintID: s.SprintID, Title: "Create without invoking", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Only original Launch creates identity"}
	var task wc.TaskMutation
	v.httpSeed(t, "/tasks", map[string]any{"request": q}, &task)
	if task.Validate() != nil || task.Task.ID != q.TaskID {
		t.Fatal("original Task receipt differs")
	}
	before, err := v.projects.service.GetSchedulerConfig(v.ctx, v.actor, v.a)
	workAppCheck(t, "runtime-original-scheduler-config", err)
	limit := int64(2)
	_, err = v.projects.service.UpdateProject(v.ctx, v.actor, executionSchedulerAppMeta(t, &before.Project.Version), v.a, pc.UpdateProjectRequest{Scheduler: &pc.ProjectSchedulerConfig{Enabled: true, MaxConcurrency: &limit}})
	workAppCheck(t, "runtime-enable-project-scheduler", err)
	moved, err := v.planning.transitions.TransferTask(v.ctx, v.actor, executionSchedulerAppMeta(t, &task.Task.Version), v.a, q.TaskID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &v.agent, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	workAppCheck(t, "runtime-original-transfer", err)
	started, err := v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	workAppCheck(t, "runtime-original-sprint-start", err)
	if started.Sprint.State != wc.Current || started.Project.CurrentSprintID == nil || *started.Project.CurrentSprintID != sprint.ID {
		t.Fatal("original Sprint did not establish current pointer")
	}
	options, _ := v.root.cfg.ExecutionRuntime()
	claim := wc.TaskClaimRequest{ProjectID: v.a, TaskID: q.TaskID, AgentID: v.agent, DispatchID: guardID[scheduler.DispatchIdentity](t).String(), ExpectedTaskVersion: moved.Task.Version, CurrentSprintID: sprint.ID, Purpose: "task/work", RequestID: guardID[f.Request](t)}
	v.dispatch, err = v.runtime.schedulerCalls.Coordinator().ClaimTask(v.ctx, claim, options.ProjectRunners().LaunchPolicy)
	workAppCheck(t, "runtime-original-claim", err)
	v.launchRequest, err = v.dispatch.LaunchRequest()
	workAppCheck(t, "runtime-original-launch-request", err)
	v.digest, err = v.launchRequest.Digest()
	workAppCheck(t, "runtime-launch-digest", err)
}

type executionSchedulerAppCall struct {
	ID, Project, Process, Instance, Lane, Phase string
	Identity                                    string
	Digest                                      f.Digest
	Original                                    json.RawMessage
}

func (v *executionSchedulerApp) calls(t *testing.T, domain string) []executionSchedulerAppCall {
	t.Helper()
	if domain != "execution" && domain != "scheduler" {
		t.Fatal("invalid fixed ownership domain")
	}
	result := "NULL::jsonb"
	if domain == "execution" {
		result = "original_result"
	}
	rows, err := v.admin.Query(v.ctx, `SELECT id::text,project_id::text,process_id::text,instance_id::text,lane,phase,invocation_identity,invocation_digest,`+result+` FROM agenteam_`+domain+`.project_calls WHERE project_id=$1::uuid ORDER BY id`, v.a.String())
	workAppCheck(t, "runtime-original-"+domain+"-ledger", err)
	defer rows.Close()
	var out []executionSchedulerAppCall
	for rows.Next() {
		var row executionSchedulerAppCall
		workAppCheck(t, "runtime-original-call-row", rows.Scan(&row.ID, &row.Project, &row.Process, &row.Instance, &row.Lane, &row.Phase, &row.Identity, &row.Digest, &row.Original))
		out = append(out, row)
	}
	workAppCheck(t, "runtime-original-call-rows-return", rows.Err())
	return out
}

func (v *executionSchedulerApp) originalFacts(t *testing.T, executionCalls int) {
	t.Helper()
	e, s := v.calls(t, "execution"), v.calls(t, "scheduler")
	if len(e) != executionCalls || len(s) != 2 {
		t.Fatal("unexpected original E/S call history", len(e), len(s))
	}
	for _, rows := range [][]executionSchedulerAppCall{e, s} {
		for _, row := range rows {
			if row.Project != v.a.String() || row.Process != v.objects.process.String() || row.Instance != rows[0].Instance || row.Phase != "business_closed" || row.Digest.Validate() != nil {
				t.Fatal("original E/S identity or returned phase differs")
			}
			if _, err := f.ParseID[struct{}](row.Instance); err != nil {
				t.Fatal("invalid original instance")
			}
		}
	}
	var origin struct {
		Schema    int      `json:"schema"`
		Kind      string   `json:"kind"`
		Execution string   `json:"execution_id"`
		Subject   string   `json:"subject_id"`
		Related   string   `json:"related_id"`
		Version   int      `json:"version"`
		Digest    f.Digest `json:"digest"`
	}
	origins := 0
	for _, row := range e {
		if row.Lane != "service" {
			t.Fatal("unexpected worker entered")
		}
		if len(row.Original) > 0 && string(row.Original) != "null" {
			origins++
			workAppCheck(t, "runtime-original-result", json.Unmarshal(row.Original, &origin))
		}
	}
	if origins != 1 || origin.Schema != 1 || origin.Kind != "created" || origin.Execution != v.execution.ID.String() || origin.Subject != origin.Execution || origin.Related != "" || origin.Version != 1 || origin.Digest != v.digest {
		t.Fatal("first actual Launch omitted exact original_result provenance")
	}
	lanes := map[string]int{}
	for _, row := range s {
		lanes[row.Lane]++
	}
	if lanes["coordinator"] != 1 || lanes["handoff"] != 1 {
		t.Fatal("original Scheduler owners differ")
	}
	var executions, attempts, snapshots, invocations, operations int64
	workAppCheck(t, "runtime-no-invocation-facts", v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_execution.executions),(SELECT count(*) FROM agenteam_execution.preparation_attempts),(SELECT count(*) FROM agenteam_execution.snapshots),(SELECT count(*) FROM agenteam_model.invocations),(SELECT count(*) FROM agenteam_tool.operations)`).Scan(&executions, &attempts, &snapshots, &invocations, &operations))
	if executions != 1 || attempts != 0 || snapshots != 0 || invocations != 0 || operations != 0 || len(v.runtime.manager.Observation().Projects) != 0 {
		t.Fatal("controlled directory allowed unrequested worker or Invoke")
	}
}

func (v *executionSchedulerApp) otherProject(t *testing.T) {
	t.Helper()
	var page f.Page[json.RawMessage]
	workAppCheck(t, "runtime-other-project-http", json.Unmarshal(v.workAppFixture.call(t, v.b, http.MethodGet, "/milestones?limit=10", "structure", nil), &page))
	if len(page.Items) != 0 || page.NextCursor != "" {
		t.Fatal("unrelated Project read differs")
	}
}

func (v *executionSchedulerApp) stopBeforeRelease(t *testing.T) {
	t.Helper()
	v.requestStop()
	v.root.logs.wait(t, func(e map[string]any) bool { return e["phase"] == "stopping" })
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !v.runtime.manager.Observation().Stopped {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("root did not stop original manager")
		case <-v.root.done:
			t.Fatal("root returned before original manager Store return")
		}
	}
	if v.runtime.Joined() || v.assembly.Joined() {
		t.Fatal("root lost its original directory Store return")
	}
	select {
	case <-v.root.done:
		t.Fatal("root returned before release")
	case <-v.store.stopped:
		t.Fatal("database stopped before original Runtime returned")
	default:
	}
	v.requireGuard(t)
}

func (v *executionSchedulerApp) joined(t *testing.T) {
	t.Helper()
	select {
	case <-v.directory.returned:
	case <-time.After(3 * time.Second):
		t.Fatal("original directory Store did not return")
	}
	select {
	case <-v.root.done:
	case <-time.After(7 * time.Second):
		t.Fatal("original Runtime App did not return")
	}
	workAppCheck(t, "runtime-original-root-return", v.root.err)
	if !v.runtime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() || !v.planning.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("App returned before its original owners")
	}
	workAppCheck(t, "runtime-original-guard-released", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	workAppCheck(t, "runtime-guard-observer-unlock", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN))
	var stopped, gone bool
	workAppCheck(t, "runtime-original-exit-facts", v.admin.QueryRow(v.ctx, `SELECT (SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1::uuid), NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`, v.objects.process.String()).Scan(&stopped, &gone))
	if !stopped || !gone {
		t.Fatal("original process or database borrower remained")
	}
	count := 1
	if v.lookupDone != nil {
		count++
	}
	v.originalFacts(t, count)
}

func testExecutionSchedulerAppReturn(t *testing.T) {
	v := newExecutionSchedulerApp(t)
	v.seed(t)
	v.originalFacts(t, 1)
	before := v.calls(t, "execution")
	v.store.mu.Lock()
	v.store.lookupProject = v.a.String()
	v.store.mu.Unlock()
	v.lookupDone = make(chan struct{})
	go func() {
		defer close(v.lookupDone)
		v.lookup, v.lookupErr = v.runtime.executionCalls.Service().LookupLaunch(v.ctx, v.actor, ec.LaunchLookupKey{ProjectID: v.a, AgentID: v.agent, IdempotencyKey: v.launchRequest.Meta.IdempotencyKey}, v.digest)
	}()
	var held executionSchedulerAppReturn
	select {
	case held = <-v.store.lookup:
	case <-v.lookupDone:
		workAppCheck(t, "runtime-lookup-before-barrier", v.lookupErr)
		t.Fatal("lookup missed original return barrier")
	case <-time.After(5 * time.Second):
		t.Fatal("lookup return barrier not reached")
	}
	_, callIDErr := f.ParseID[struct{}](held.call)
	if held.result.State() != f.Committed || callIDErr != nil {
		t.Fatalf("original technical return state=%s original_call_valid=%t", held.result.State(), callIDErr == nil)
	}
	rows := v.calls(t, "execution")
	if len(rows) != 2 {
		t.Fatal("original Lookup did not add one call")
	}
	found := false
	for _, row := range rows {
		if row.ID == held.call {
			found = row.Phase == "business_closed" && (len(row.Original) == 0 || string(row.Original) == "null")
		} else if !reflect.DeepEqual(row, before[0]) {
			t.Fatal("Lookup rewrote original Launch provenance")
		}
	}
	if !found {
		t.Fatal("held Lookup identity is not its real closed row")
	}
	select {
	case <-v.lookupDone:
		t.Fatal("Lookup returned before Store return")
	default:
	}
	v.otherProject(t)
	v.stopBeforeRelease(t)
	if v.runtime.executionCalls.Joined() {
		t.Fatal("closed technical row was mistaken for original Execution return")
	}
	v.store.releaseDirectory()
	select {
	case <-v.directory.returned:
	case <-time.After(3 * time.Second):
		t.Fatal("stopped directory did not return")
	}
	// Removing the manager barrier must not release the still-held E owner.
	select {
	case <-v.root.done:
		t.Fatal("root dropped original E return")
	case <-v.store.stopped:
		t.Fatal("database stopped below original E owner")
	case <-v.lookupDone:
		t.Fatal("original Lookup returned before release")
	default:
	}
	v.requireGuard(t)
	v.store.releaseLookup()
	select {
	case <-held.returned:
	case <-time.After(3 * time.Second):
		t.Fatal("original lookup Store did not return")
	}
	select {
	case <-v.lookupDone:
	case <-time.After(3 * time.Second):
		t.Fatal("original Lookup did not return")
	}
	workAppCheck(t, "runtime-original-lookup-return", v.lookupErr)
	if !v.lookup.Found || v.lookup.Execution == nil || !reflect.DeepEqual(*v.lookup.Execution, v.execution) || v.lookup.RequestDigest != v.digest {
		t.Fatal("original returned Lookup receipt differs")
	}
	v.originalFacts(t, 2)
	v.joined(t)
	t.Log("EXECUTION_SCHEDULER_DEFAULT_APP_RETURN original_created_and_association_returned=true lookup_close_committed=true original_store_return_held=true other_project_http=true root_guard_database_retained=true original_lookup_and_root_returned=true no_unknown_force=true directory_return_controlled=true advance_not_reached=true preparation_direct_workers_not_covered=true no_invoke=true")
}
