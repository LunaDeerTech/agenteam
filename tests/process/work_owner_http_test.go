//go:build integration

package process_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

// The real executable owns Account login, routing, Work, Catalog and retirement.
// Only initial Project Skills publication is a disclosed task-owned fixture:
// Project creation itself runs the real service with persisted confirmation.
func TestWorkOwnerHTTPProcessRoutingAndPersistence(t *testing.T) {
	v := newModelSystemBinary(t)
	v.logSecrets = append(v.logSecrets, "root-work-private-canary", "root human decision")
	p := workRootProject(t, v)
	path := "/api/v1/projects/" + p.ID.String()
	request := func(method, suffix, key string, input any) modelSystemResponse {
		t.Helper()
		r := v.request(t, method, path+suffix, key, input, nil).want(t, 200)
		if r.header.Get("Cache-Control") != "no-store" || r.header.Get("Content-Length") == "" || r.header.Get("X-Request-ID") == "" {
			t.Fatal("root Work response lost public output boundary")
		}
		return r
	}
	decode := func(r modelSystemResponse, out any) {
		t.Helper()
		if err := json.Unmarshal(r.body, out); err != nil {
			t.Fatal("root Work typed response", err)
		}
	}
	milestoneID, sprintID, taskID, blockerID := modelSystemKey(t), modelSystemKey(t), modelSystemKey(t), modelSystemKey(t)
	milestoneKey := modelSystemKey(t)
	milestoneRequest := map[string]any{"milestone_id": milestoneID, "title": "root milestone"}
	var ms wc.StructureMutation
	decode(request("POST", "/milestones", milestoneKey, map[string]any{"request": milestoneRequest}), &ms)
	if ms.Milestone == nil || ms.Milestone.ID.String() != milestoneID {
		t.Fatal("root milestone identity")
	}
	request("GET", "/milestones", "", nil)
	request("GET", "/milestones/"+milestoneID, "", nil)
	var structureLookup wc.CommandLookup
	decode(request("POST", "/structure-commands/lookup", milestoneKey, map[string]any{"command": "work.milestone.create", "request": milestoneRequest}), &structureLookup)
	if structureLookup.State != wc.LookupCommitted || structureLookup.Result == nil || structureLookup.Result.Milestone.ID.String() != milestoneID {
		t.Fatal("root Structure receipt")
	}
	decode(request("PATCH", "/milestones/"+milestoneID, modelSystemKey(t), map[string]any{"expected_version": ms.Milestone.Version.String(), "request": map[string]any{"title": "root milestone revised"}}), &ms)
	decode(request("POST", "/milestones/"+milestoneID+"/reorder", modelSystemKey(t), map[string]any{"expected_version": ms.Milestone.Version.String(), "request": map[string]any{}}), &ms)
	var sp wc.StructureMutation
	decode(request("POST", "/sprints", modelSystemKey(t), map[string]any{"request": map[string]any{"sprint_id": sprintID, "milestone_id": milestoneID, "title": "root sprint"}}), &sp)
	request("GET", "/sprints?milestone_id="+milestoneID, "", nil)
	request("GET", "/sprints/"+sprintID, "", nil)
	decode(request("PATCH", "/sprints/"+sprintID, modelSystemKey(t), map[string]any{"expected_version": sp.Sprint.Version.String(), "request": map[string]any{"title": "root sprint revised"}}), &sp)
	decode(request("POST", "/sprints/"+sprintID+"/reorder", modelSystemKey(t), map[string]any{"expected_version": sp.Sprint.Version.String(), "request": map[string]any{"milestone_id": milestoneID}}), &sp)
	taskKey := modelSystemKey(t)
	taskRequest := map[string]any{"task_id": taskID, "sprint_id": sprintID, "title": "root task", "description": "root-work-private-canary", "type": "task", "priority": "medium"}
	var task wc.TaskMutation
	decode(request("POST", "/tasks", taskKey, map[string]any{"request": taskRequest}), &task)
	request("GET", "/tasks", "", nil)
	request("GET", "/tasks/"+taskID, "", nil)
	var taskLookup wc.TaskCommandLookup
	decode(request("POST", "/task-commands/lookup", taskKey, map[string]any{"command": "work.task.create", "request": taskRequest}), &taskLookup)
	if taskLookup.Status != wc.LookupCommitted || taskLookup.Receipt == nil || taskLookup.Receipt.Task.ID.String() != taskID {
		t.Fatal("root Task receipt")
	}
	decode(request("PATCH", "/tasks/"+taskID, modelSystemKey(t), map[string]any{"expected_version": task.Task.Version.String(), "request": map[string]any{"title": "root task revised"}}), &task)
	decode(request("POST", "/tasks/"+taskID+"/reorder", modelSystemKey(t), map[string]any{"expected_version": task.Task.Version.String(), "request": map[string]any{}}), &task)
	blockerKey, version := modelSystemKey(t), task.Task.Version.String()
	blockerRequest := map[string]any{"blocker_id": blockerID, "type": "waiting_for_human", "description": "root human decision", "metadata": map[string]any{}}
	var blocker wc.TaskBlockerMutation
	decode(request("POST", "/tasks/"+taskID+"/blockers", blockerKey, map[string]any{"expected_version": version, "request": blockerRequest}), &blocker)
	request("GET", "/tasks/"+taskID+"/blockers", "", nil)
	var blockerLookup wc.TaskBlockerCommandLookup
	decode(request("POST", "/tasks/"+taskID+"/blocker-commands/lookup", blockerKey, map[string]any{"command": "work.task.blocker.add", "expected_version": version, "request": blockerRequest}), &blockerLookup)
	if blockerLookup.Status != wc.LookupCommitted || blockerLookup.Receipt == nil || blockerLookup.Receipt.Blocker.ID.String() != blockerID {
		t.Fatal("root Blocker receipt")
	}
	decode(request("POST", "/tasks/"+taskID+"/blockers/resolve", modelSystemKey(t), map[string]any{"expected_version": blocker.Task.Version.String(), "request": map[string]any{"blocker_id": blockerID, "resolution_comment": nil}}), &blocker)
	if blocker.Blocker.ResolvedAt == nil || blocker.Task.Version <= task.Task.Version {
		t.Fatal("root Blocker resolve persistence")
	}
	// Existing production routes remain owned by Account and Model respectively.
	v.request(t, "GET", "/api/v1/session", "", nil, nil).want(t, 200)
	v.request(t, "GET", path+"/model-providers", "", nil, nil).want(t, 200)
	var count int
	if err := v.db.Connect(t).QueryRow(databaseContext(t), `SELECT count(*) FROM agenteam_work.task_blockers WHERE id=$1 AND task_id=$2 AND resolved_at IS NOT NULL`, blockerID, taskID).Scan(&count); err != nil || count != 1 {
		t.Fatal("root canonical Blocker", err)
	}
	v.stop(t, syscall.SIGTERM)
}

func workRootProject(t *testing.T, v *modelSystemBinary) pc.ProjectRef {
	t.Helper()
	values := map[string]string{}
	for _, e := range v.env {
		k, value, ok := strings.Cut(e, "=")
		if ok {
			values[k] = value
		}
	}
	cfg, err := config.Load(func(k string) (string, bool) { v, ok := values[k]; return v, ok }, v.env)
	if err != nil {
		t.Fatal("root fixture config", err)
	}
	store, err := postgres.Open(databaseContext(t), v.db.Config(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := store.ForceClose(ctx); err != nil {
			t.Error("Project preparation store did not close", err)
		}
	}()
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	pa, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts})
	if err != nil {
		t.Fatal(err)
	}
	aud, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: pa})
	if err != nil {
		t.Fatal(err)
	}
	catalog := ec.NewCatalog()
	events, err := pc.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	processID, err := foundation.NewID[oc.Process]()
	if err != nil {
		t.Fatal(err)
	}
	processes := workRootProcess{processID}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[ec.StableName]oc.ProducerAuthority{pc.ProjectProducer: pa}, Projects: pa, Sessions: accounts, System: accounts, Processes: processes, Audit: aud, Cursors: cfg.CursorKeyring()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Exec(databaseContext(t), `CREATE SCHEMA work_owner_root_fixture; CREATE TABLE work_owner_root_fixture.skills(creation_id uuid PRIMARY KEY,project_id uuid NOT NULL UNIQUE,init_key text NOT NULL,skill_id uuid NOT NULL UNIQUE,revision bigint NOT NULL,protected boolean NOT NULL,published boolean NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	skills := &workRootSkills{store: store, authority: pa, issuer: pc.NewInitializationPlanIssuer()}
	projects, err := project.New(store, project.Dependencies{Authority: pa, Activity: accounts, Audit: aud, Events: box, ProjectEvents: events, Initializer: skills, Processes: processes, Cursors: cfg.CursorKeyring()}, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		projects.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := projects.Drain(ctx); err != nil {
			t.Error("Project preparation commands did not join", err)
		}
	}()
	// This reads an identity produced by the executable's real Login. It creates
	// no Account/Session rows and is used solely for fixture Project preparation.
	var userText, sessionText string
	if err = store.QueryRow(databaseContext(t), `SELECT user_id::text,id::text FROM agenteam_account.sessions WHERE revoked_at IS NULL ORDER BY issued_at DESC LIMIT 1`).Scan(&userText, &sessionText); err != nil {
		t.Fatal("real login identity", err)
	}
	user, err := foundation.ParseID[identity.User](userText)
	if err != nil {
		t.Fatal(err)
	}
	session, err := foundation.ParseID[identity.Session](sessionText)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := identity.NewHuman(user, session)
	if err != nil {
		t.Fatal(err)
	}
	projectID, err := foundation.NewID[identity.Project]()
	if err != nil {
		t.Fatal(err)
	}
	requestID, err := foundation.NewID[foundation.Request]()
	if err != nil {
		t.Fatal(err)
	}
	meta := foundation.CommandMeta{RequestID: requestID, IdempotencyKey: foundation.IdempotencyKey(modelSystemKey(t))}
	result, err := projects.CreateProject(databaseContext(t), actor, meta, pc.CreateProjectRequest{ProjectID: projectID, Name: "work-root", Description: "owned Project prepared through real service"})
	if err != nil || result.State != pc.CreationReady || result.Project == nil {
		t.Fatal("actual Project creation with fixture Skills", err)
	}
	return *result.Project
}

type workRootProcess struct{ id oc.ProcessID }

func (p workRootProcess) CurrentProcess() oc.ProcessID { return p.id }
func (p workRootProcess) ConfirmStopped(context.Context, oc.ProcessID) error {
	return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
}

type workRootSkills struct {
	store     project.Store
	authority *project.Authority
	issuer    pc.InitializationPlanIssuer
}

func (s *workRootSkills) InspectProjectSkills(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	if actor.Details().ServiceName != identity.ProjectInitialization || actor.Details().CauseRef != r.CreationID.String() || actor.Details().ProjectID != r.ProjectID.String() {
		return pc.InitializationResult{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	var project, key, skill string
	var revision int64
	e := s.store.QueryRow(ctx, `SELECT project_id::text,init_key,skill_id::text,revision FROM work_owner_root_fixture.skills WHERE creation_id=$1`, r.CreationID.String()).Scan(&project, &key, &skill, &revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return pc.InitializationResult{State: pc.InitializationResultPending, CreationID: r.CreationID, ProjectID: r.ProjectID, SafeReason: pc.ReasonWorkPending}, nil
	}
	if e != nil {
		return pc.InitializationResult{}, e
	}
	if project != r.ProjectID.String() || key != string(r.InitializationKey) {
		return pc.InitializationResult{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	skillID, e := foundation.ParseID[pc.Skill](skill)
	if e != nil {
		return pc.InitializationResult{}, e
	}
	rev := foundation.Revision(revision)
	return pc.InitializationResult{State: pc.InitializationCompleted, CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: &skillID, Revision: &rev}, nil
}
func (s *workRootSkills) InitializeProjectSkills(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationResult, error) {
	projectKey, _ := foundation.ProjectLock(r.ProjectID.String())
	command, _ := foundation.NewRecoveryCause("project.fixture-skill", r.CreationID.String(), "")
	result := s.store.WithinTx(ctx, command, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: projectKey, Mode: foundation.Exclusive}}); e != nil {
			return e
		}
		if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
			return e
		}
		x, e := s.store.InTx(tx)
		if e != nil {
			return e
		}
		skill, e := foundation.NewID[pc.Skill]()
		if e != nil {
			return e
		}
		_, e = x.Exec(ctx, `INSERT INTO work_owner_root_fixture.skills(creation_id,project_id,init_key,skill_id,revision,protected,published) VALUES($1,$2,$3,$4,1,true,true) ON CONFLICT(creation_id) DO NOTHING`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), skill.String())
		return e
	})
	if result.State() != foundation.Committed {
		return pc.InitializationResult{}, foundation.NewFault(foundation.DependencyUnavailable, result.State())
	}
	return s.InspectProjectSkills(ctx, actor, r)
}
func (s *workRootSkills) DiscoverConfirmation(ctx context.Context, actor identity.Actor, r pc.InitializationRequest) (pc.InitializationConfirmationPlan, error) {
	result, e := s.InspectProjectSkills(ctx, actor, r)
	if e != nil {
		return pc.InitializationConfirmationPlan{}, e
	}
	if result.State != pc.InitializationCompleted {
		return pc.InitializationConfirmationPlan{}, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	key, _ := foundation.ProjectLock(r.ProjectID.String())
	locks := []foundation.LockRequest{{Key: key, Mode: foundation.Exclusive}}
	return s.issuer.Plan(actor, r, pc.InitializationReceipt{CreationID: r.CreationID, ProjectID: r.ProjectID, AddSkillsID: *result.AddSkillsID, Revision: *result.Revision}, locks)
}
func (s *workRootSkills) ConfirmInitializedInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, r pc.InitializationRequest, plan pc.InitializationConfirmationPlan) (pc.InitializationReceipt, error) {
	if !s.issuer.Matches(plan, actor, r) {
		return pc.InitializationReceipt{}, foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	locks := plan.RequiredLocks()
	if e := s.store.RequireHeldLocks(ctx, tx, locks); e != nil {
		return pc.InitializationReceipt{}, e
	}
	if e := s.authority.ValidateInitializationInTx(ctx, tx, actor, r.CreationID, r.ProjectID, r.InitializationKey); e != nil {
		return pc.InitializationReceipt{}, e
	}
	x, e := s.store.InTx(tx)
	if e != nil {
		return pc.InitializationReceipt{}, e
	}
	receipt := plan.ProposedReceipt()
	var valid bool
	e = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM work_owner_root_fixture.skills WHERE creation_id=$1 AND project_id=$2 AND init_key=$3 AND skill_id=$4 AND revision=$5 AND protected AND published)`, r.CreationID.String(), r.ProjectID.String(), string(r.InitializationKey), receipt.AddSkillsID.String(), int64(receipt.Revision)).Scan(&valid)
	if e != nil {
		return pc.InitializationReceipt{}, e
	}
	if !valid {
		return pc.InitializationReceipt{}, foundation.NewFault(foundation.InvalidState, foundation.NotStarted)
	}
	return receipt, nil
}
