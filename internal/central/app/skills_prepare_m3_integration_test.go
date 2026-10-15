//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	skc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	runnerconfig "github.com/LunaDeerTech/agenteam/internal/runner/config"
	"github.com/LunaDeerTech/agenteam/internal/runner/identity"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const skillsPrepareM3Script = "#!/bin/sh\nset -eu\ntest \"$(cat references/check.txt)\" = 'canonical-package-input'\nprintf 'skill-package-executed\\n'\n"
const skillsPrepareM3Output = "skill-package-executed\n"
const skillsPrepareM3Entry = "---\nname: Prepare execution guide\ndescription: Read the reference and execute the explicitly requested script\n---\nPrepare this assigned package, then run scripts/run.sh in the returned directory.\n"
const skillsPrepareM3Final = "Prepared the assigned package and verified its script completed."

func TestSkillsPrepareM3Configuration(t *testing.T) {
	cfg := testConfig(t, "5s", memoryM3Settings())
	workAppCheck(t, "skill-prepare-complete-config", cfg.Validate())
	runtime, enabled := cfg.ExecutionRuntime()
	if !enabled || !runtime.ToolLoop() {
		t.Fatal("Skill prepare requires the original Tool-loop deployment")
	}
}

func TestSkillsPrepareM3RunnerConfigurationEnvironment(t *testing.T) {
	environment := skillsPrepareM3InheritedEnvironment([]string{
		"AGENTEAM_RUNNER_BINARY=/owned-fixture/runner", "AGENTEAM_RUNNER_OPERATIONS=parent-only",
		"HTTPS_PROXY=http://proxy.invalid:8080", "NO_PROXY=original.invalid", "SSL_CERT_FILE=/original-ca.pem",
	})
	environment = append(environment, "AGENTEAM_RUNNER_IDENTITY_FILE=/owned-fixture/identity.json", "AGENTEAM_RUNNER_CENTRAL_URL=https://central.invalid", "AGENTEAM_RUNNER_ID="+guardID[struct{}](t).String(), "AGENTEAM_RUNNER_ROOT_PATH=/owned-fixture/workspace", "AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT=3s", "AGENTEAM_RUNNER_OPERATIONS=read-file,write-file,run-command,prepare-skill")
	values := map[string]string{}
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	cfg, err := runnerconfig.Load(func(key string) (string, bool) { value, found := values[key]; return value, found }, environment)
	if err != nil || cfg.Operations() != "read-file,write-file,run-command,prepare-skill" {
		t.Fatal("explicit Runner environment rejected by original closed config", err)
	}
	if values["HTTPS_PROXY"] != "http://proxy.invalid:8080" || values["NO_PROXY"] != "original.invalid" || values["SSL_CERT_FILE"] != "/original-ca.pem" {
		t.Fatal("Runner filtering changed original proxy or trust configuration")
	}
}

// The only deterministic peer is Model HTTP. Installation and assignment use
// real Owner boundaries. The Runner is the original command binary; Object
// signs a real GET and the Runner creates the initially absent workspace.
func TestSkillsPrepareM3AppRunCommand(t *testing.T) {
	TestSkillsPrepareM3Configuration(t)
	trace := &skillsPrepareM3Transactions{}
	// Reuse only M1's original App/Runner TLS owners and its formal relaunch
	// window. No review Agent, precreated workspace, or direct Tool is used.
	w, _ := newM1WorkflowWithReviewWindow(t, trace.observe, true, nil, 100000)
	v := w.app
	stage := "app"
	defer func() {
		trace.finish(t)
		if t.Failed() {
			t.Logf("M3 Skill prepare stopped stage=%s", stage)
			skillsPrepareM3ConnectionDiagnosis(t, w)
			skillsPrepareM3Diagnose(t, w)
			w.diagnoseResult(t, 0, w.execution.String(), "")
		}
	}()
	w.seed(t)
	var rawTool string
	workAppCheck(t, "skill-prepare-registered-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:prepare-skill'`).Scan(&rawTool))
	prepareTool, err := f.ParseID[id.Tool](rawTool)
	workAppCheck(t, "skill-prepare-tool-id", err)
	stage = "owner-install"
	skillID := guardID[pc.Skill](t)
	var receipt struct {
		SkillID  skc.SkillID `json:"skill_id"`
		Revision f.Revision  `json:"revision"`
		Version  f.Version   `json:"version"`
	}
	v.httpSeed(t, "/skills", map[string]any{"request": map[string]any{"skill_id": skillID, "mode": "create", "source": map[string]any{"kind": "text_files", "files": []map[string]string{
		{"path": "SKILL.md", "utf8_text": skillsPrepareM3Entry}, {"path": "scripts/run.sh", "utf8_text": skillsPrepareM3Script}, {"path": "references/check.txt", "utf8_text": "canonical-package-input\n"},
	}}}}, &receipt)
	if receipt.SkillID != skillID || receipt.Revision != 1 || receipt.Version != 1 {
		t.Fatal("formal Owner installation receipt differs")
	}
	var revisionRaw string
	workAppCheck(t, "skill-prepare-published-revision", v.admin.QueryRow(v.ctx, `SELECT id::text FROM agenteam_skill.revisions WHERE project_id::text=$1::text AND skill_id::text=$2::text AND revision=1`, v.a.String(), skillID.String()).Scan(&revisionRaw))
	revisionID, err := f.ParseID[skc.Revision](revisionRaw)
	workAppCheck(t, "skill-prepare-revision-id", err)
	relative, err := p.SkillRevisionPath(p.ID(skillID.String()), p.ID(revisionID.String()))
	workAppCheck(t, "skill-prepare-contract-path", err)
	// Future replies are configured only before any Task can capture the URL.
	w.scenario, err = w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		skillsPrepareM3TextReply(t, "Not yet configured.", 1), skillsPrepareM3TextReply(t, "Not yet configured.", 2), skillsPrepareM3TextReply(t, skillsPrepareM3Final, 3),
	}})
	workAppCheck(t, "skill-prepare-model-peer", err)
	defer func() { _ = w.network.Release(context.Background(), w.scenario) }()
	model := skillsPrepareM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+w.scenario)
	disabled := false
	v.agent = guardID[id.Agent](t)
	allowed := []id.ToolID{prepareTool, w.tools[tc.RunnerRunCommandKey]}
	slices.SortFunc(allowed, func(a, b id.ToolID) int { return strings.Compare(a.String(), b.String()) })
	declarations := []ac.RunnerMountDeclaration{{RunnerID: w.runnerID, Workspace: "skill-work"}}
	create, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: v.agent, Name: "skill-prepare-agent", Description: "Assigned package execution", Instructions: "Prepare the explicitly assigned skill, execute its script using the returned relative directory, then report the actual result.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: allowed, AllowedMountIDs: nil, AllowedSecretVariableIDs: []id.ProjectVariableID{}, RunnerMounts: &declarations, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled, InjectAgentsMD: false})
	workAppCheck(t, "skill-prepare-agent-intent", err)
	created, err := v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, create)
	workAppCheck(t, "skill-prepare-agent-create", err)
	mounts := created.Fields().RunnerMounts
	if mounts == nil || len(*mounts) != 1 {
		t.Fatal("original Agent has no exact Runner mount")
	}
	w.mount = (*mounts)[0].MountID
	w.workspace = filepath.Join(w.root, v.a.String(), v.agent.String(), (*mounts)[0].Workspace)
	if _, err = os.Lstat(w.workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Skill workspace was fabricated before Runner preparation")
	}
	stage = "owner-assignment"
	response, err := v.request(v.ctx, "POST", "/api/v1/projects/"+v.a.String()+"/agents/"+v.agent.String()+"/skills/assign", workAppJSON(t, map[string]any{"expected_version": f.Version(1), "request": map[string]any{"skill_id": skillID, "enabled": true}}), guardID[struct{}](t).String())
	workAppCheck(t, "skill-prepare-owner-assign", err)
	if response.status != 200 {
		t.Fatalf("Skill assignment HTTP status=%d", response.status)
	}
	var assigned skc.AssignmentMutation
	workAppCheck(t, "skill-prepare-assignment-receipt", json.Unmarshal(response.body, &assigned))
	if assigned.Validate() != nil || !assigned.Changed || assigned.AgentVersion != 2 {
		t.Fatal("original Owner assignment did not apply")
	}
	workAppCheck(t, "skill-prepare-future-first", w.network.UpdateFutureWire(v.ctx, w.scenario, 1, skillsPrepareM3ToolReply(t, prepareTool, map[string]any{"skill_id": skillID, "mount_id": w.mount}, 1)))
	hold := 0
	second := skillsPrepareM3ToolReply(t, w.tools[tc.RunnerRunCommandKey], map[string]any{"mount_id": w.mount, "mode": "argv", "program": "/bin/sh", "args": []string{"scripts/run.sh"}, "cwd": relative}, 2)
	second.HoldAfter = &hold
	workAppCheck(t, "skill-prepare-future-second", w.network.UpdateFutureWire(v.ctx, w.scenario, 2, second))
	stage = "runner-enroll"
	trace.start()
	skillsPrepareM3StartRunner(t, w)
	sprint := memoryM3CreateSprint(t, v)
	w.task = memoryM3CreateTask(t, v, sprint.ID, v.agent, "Prepare the assigned Skill package and execute scripts/run.sh using its returned directory.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	workAppCheck(t, "skill-prepare-start-sprint", err)
	v.store.releaseDirectory()
	stage = "actual-prepare"
	state := skillsPrepareM3AwaitPeer(t, w, 2)
	var prepared struct {
		SkillID    skc.SkillID    `json:"skill_id"`
		RevisionID skc.RevisionID `json:"revision_id"`
		Revision   f.Revision     `json:"revision"`
		MountID    id.MountID     `json:"mount_id"`
		Path       string         `json:"path"`
		Reused     bool           `json:"reused"`
	}
	workAppCheck(t, "skill-prepare-model-received-result", json.Unmarshal([]byte(skillsPrepareM3LastTool(t, state.Requests[1].Body)), &prepared))
	if prepared.SkillID != skillID || prepared.RevisionID != revisionID || prepared.Revision != 1 || prepared.MountID != w.mount || prepared.Path != relative || prepared.Reused {
		t.Fatal("actual prepared identity/path was not delivered to the next Model call")
	}
	for name, want := range map[string]string{"SKILL.md": skillsPrepareM3Entry, "scripts/run.sh": skillsPrepareM3Script, "references/check.txt": "canonical-package-input\n"} {
		path := filepath.Join(w.workspace, filepath.FromSlash(prepared.Path), filepath.FromSlash(name))
		got, err := os.ReadFile(path)
		workAppCheck(t, "skill-prepare-original-files", err)
		info, err := os.Lstat(path)
		workAppCheck(t, "skill-prepare-original-file-mode", err)
		if !bytes.Equal(got, []byte(want)) || !info.Mode().IsRegular() || info.Mode().Perm() != 0444 {
			t.Fatal("actual prepared package content or readonly file mode differs")
		}
	}
	var reports int
	workAppCheck(t, "skill-prepare-no-implicit-execute", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_tool.command_results WHERE project_id=$1`, v.a.String()).Scan(&reports))
	if reports != 0 {
		t.Fatal("preparation implicitly executed a script")
	}
	workAppCheck(t, "skill-prepare-release-original-model", w.network.Release(v.ctx, w.scenario))
	stage = "actual-run-command"
	w.execution = memoryM3AwaitExecution(t, v, w.task.ID)
	skillsPrepareM3Facts(t, w, skillID, revisionID)
	state = w.joinedPeer(t, w.scenario, 3, 3)
	bundle, err := tc.DecodeRunCommandResultBundle(v.ctx, []byte(skillsPrepareM3LastTool(t, state.Requests[2].Body)))
	workAppCheck(t, "skill-prepare-model-received-command", err)
	if string(bundle.Stdout) != skillsPrepareM3Output || len(bundle.Stderr) != 0 || bundle.Report.ExitCode != 0 || !bundle.Report.OutputComplete || !bundle.Report.ProcessGroupTerminationConfirmed {
		t.Fatal("next Model did not consume the real complete script result")
	}
	for _, request := range state.Requests {
		if strings.Contains(request.Body, "X-Amz-") || strings.Contains(request.Body, "Signature=") || strings.Contains(request.Body, "owned-skills-fixture-credential") {
			t.Fatal("private transfer or credential material entered Model input")
		}
	}
	stage = "original-join"
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	err = v.runtime.Drain(drain)
	cancel()
	workAppCheck(t, "skill-prepare-original-runtime-drain", err)
	w.stopRunner(t)
	v.requestStop()
	usageAppWait(t, v.directory.returned, "skill-prepare-original-directory-return")
	usageAppWait(t, v.root.done, "skill-prepare-original-app-return")
	workAppCheck(t, "skill-prepare-original-app-exit", v.root.err)
	if !v.runtime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() || !v.planning.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("prepare App returned before original owners joined")
	}
	taskExecutionGuardReleased(t, v.workAppFixture)
	t.Log("M3_SKILL_PREPARE actual_owner_install_assignment=true real_object_get_runner_package=true original_run_command_exit0=true model_rounds=3 original_transfer_lease_released=true original_app_runner_joined=true external_model=false")
}

// This observes the original Store result after the callback has returned. It
// neither wraps authority providers nor alters commit/dispatch behavior. Keep
// only closed stage names and codes; transaction identities and raw causes are
// deliberately unavailable to the diagnostic output.
type skillsPrepareM3Transactions struct {
	mu      sync.Mutex
	active  bool
	counts  [4]int
	results []skillsPrepareM3Transaction
}

type skillsPrepareM3Transaction struct {
	stage              string
	ordinal            int
	state              f.CommitState
	code               f.Code
	database, sqlState string
	deadline, canceled bool
}

func (s *skillsPrepareM3Transactions) start() {
	s.mu.Lock()
	s.active = true
	s.mu.Unlock()
}

func (s *skillsPrepareM3Transactions) observe(cause f.TransactionCause, result f.CommitResult) {
	d := cause.Details()
	if d.Kind != f.RecoveryCause {
		return
	}
	index := -1
	switch d.Owner {
	case "skill.prepare":
		index = 0
	case "runner-skill-prepare":
		index = 1
	case "object":
		index = 2
	case "runner-connection":
		index = 3
	}
	if index < 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active {
		return
	}
	s.counts[index]++
	// Count the original connection checks, retaining only their failures so
	// periodic successful checks cannot crowd the business stages out of the
	// fixed trace budget. This observer never changes the original result.
	if index == 3 && result.State() == f.Committed {
		return
	}
	if len(s.results) >= 64 {
		return
	}
	v := skillsPrepareM3Transaction{stage: d.Owner, ordinal: s.counts[index], state: result.State().Safe()}
	if fault := result.Fault(); fault != nil {
		v.code = fault.Code.Safe()
		v.deadline, v.canceled = errors.Is(fault, context.DeadlineExceeded), errors.Is(fault, context.Canceled)
		var database *postgres.Error
		if errors.As(fault, &database) {
			// Only the provider's closed diagnostic code and validated SQLSTATE;
			// no driver message, field, detail, SQL, parameter or cause string.
			switch database.Code() {
			case postgres.LockFailed, postgres.LockOrderViolation, postgres.LockUpgradeForbidden, postgres.LockNotHeld,
				postgres.InvalidLock, postgres.TransactionNested, postgres.TransactionConcurrentUse, postgres.TransactionRowsOpen,
				postgres.TransactionExpired, postgres.InvalidTransaction, postgres.TransactionBeginFailed,
				postgres.TransactionCommitFailed, postgres.SQLFailed, postgres.ConnectionFailed, postgres.AdmissionStopped:
				v.database = string(database.Code())
			default:
				v.database = "other"
			}
			v.sqlState = database.SQLState()
		}
	}
	s.results = append(s.results, v)
}

func (s *skillsPrepareM3Transactions) finish(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	s.active = false
	counts, results := s.counts, slices.Clone(s.results)
	s.mu.Unlock()
	if !t.Failed() {
		return
	}
	t.Logf("M3 Skill prepare original transaction counts skill=%d mount=%d object=%d connection=%d", counts[0], counts[1], counts[2], counts[3])
	for _, result := range results {
		t.Logf("M3 Skill prepare original transaction stage=%s ordinal=%d state=%s safe_code=%s database_code=%s sqlstate=%s deadline=%t canceled=%t", result.stage, result.ordinal, result.state, result.code, result.database, result.sqlState, result.deadline, result.canceled)
	}
}

// Called by the test body's defer, before its registered cleanup cancels the
// Runner or App. Only the Runner's closed event vocabulary and public current
// generation/capability facts are emitted; its raw private log stays private.
func skillsPrepareM3ConnectionDiagnosis(t *testing.T, w *m1Workflow) {
	t.Helper()
	if w.runner == nil {
		return
	}
	raw, overflow := w.runner.stderr.snapshot()
	defer clear(raw)
	events := 0
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		var event struct {
			Event string `json:"event"`
			State string `json:"state"`
			Time  string `json:"time"`
		}
		if json.Unmarshal(line, &event) != nil || event.Event != "runner_connection" {
			continue
		}
		switch event.State {
		case "connecting", "connected", "disconnected", "incompatible":
		default:
			continue
		}
		events++
		if events > 32 {
			continue
		}
		at := "unavailable"
		if parsed, err := time.Parse(time.RFC3339Nano, event.Time); err == nil {
			at = parsed.UTC().Format(time.RFC3339Nano)
		}
		t.Logf("M3 Skill prepare Runner connection before_original_cleanup=true ordinal=%d state=%s time=%s", events, event.State, at)
	}
	t.Logf("M3 Skill prepare Runner connection events=%d log_overflow=%t before_original_cleanup=true", events, overflow)
	var generation int64
	var connections int
	var capability, hello, live bool
	err := w.app.admin.QueryRow(w.app.ctx, `SELECT r.connection_generation,
 COALESCE((r.last_hello->'capabilities') ? 'prepare-skill',false),
 (SELECT count(*) FROM agenteam_runner.connections c WHERE c.runner_id=r.id),
 EXISTS(SELECT 1 FROM agenteam_runner.connections c WHERE c.runner_id=r.id AND c.generation=r.connection_generation AND c.hello_at IS NOT NULL),
 EXISTS(SELECT 1 FROM agenteam_runner.connections c WHERE c.runner_id=r.id AND c.generation=r.connection_generation AND c.lease_expires_at>clock_timestamp())
 FROM agenteam_runner.runners r WHERE r.id=$1`, w.runnerID.String()).Scan(&generation, &capability, &connections, &hello, &live)
	if err != nil {
		t.Log("M3 Skill prepare Runner current observation available=false before_original_cleanup=true")
		return
	}
	t.Logf("M3 Skill prepare Runner current before_original_cleanup=true generation=%d connections=%d capability=%t hello=%t live=%t", generation, connections, capability, hello, live)
}

// The tools adapter emits text parts, while the legacy text path may emit a
// string. Decode either formal shape without logging provider input or bodies.
func skillsPrepareM3LastTool(t *testing.T, body string) string {
	t.Helper()
	var request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	workAppCheck(t, "skill-prepare-peer-request", json.Unmarshal([]byte(body), &request))
	for n := len(request.Messages) - 1; n >= 0; n-- {
		if request.Messages[n].Role != "tool" {
			continue
		}
		var text string
		if json.Unmarshal(request.Messages[n].Content, &text) == nil {
			return text
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(request.Messages[n].Content, &parts) != nil || len(parts) != 1 || parts[0].Type != "text" {
			t.Fatal("original Model Tool result is not one text part")
		}
		return parts[0].Text
	}
	t.Fatal("original Model request has no Tool result")
	return ""
}

// Read only closed status/code fields and stage counts before fixture cleanup.
// No body, argument, signed material, endpoint or arbitrary error is printed.
func skillsPrepareM3Diagnose(t *testing.T, w *m1Workflow) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rows, err := w.app.admin.Query(ctx, `SELECT phase,COALESCE(outcome,''),COALESCE(convert_from(terminal_data,'UTF8')::jsonb->>'Code','') FROM agenteam_tool.operations WHERE project_id=$1 AND operation_kind='skill.prepare' ORDER BY operation_id LIMIT 4`, w.app.a.String())
	if err != nil {
		t.Log("M3 Skill prepare original terminal diagnostic unavailable")
		return
	}
	for rows.Next() {
		var phase, outcome, code string
		if rows.Scan(&phase, &outcome, &code) != nil {
			t.Log("M3 Skill prepare original terminal diagnostic unavailable")
			break
		}
		if !slices.Contains([]string{"created", "running", "succeeded", "failed", "cancelled"}, phase) || !slices.Contains([]string{"", "success", "error", "unknown", "cancelled"}, outcome) {
			t.Log("M3 Skill prepare original terminal diagnostic shape invalid")
			continue
		}
		safeCode := "none"
		if code != "" {
			safeCode = string(f.Code(code).Safe())
		}
		t.Logf("M3 Skill prepare original terminal phase=%s outcome=%s safe_code=%s", phase, outcome, safeCode)
	}
	rows.Close()
	var total, complete, retired, active, revoked int
	err = w.app.admin.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE t.phase='complete'),count(*) FILTER(WHERE t.retirement_evidence IS NOT NULL),count(*) FILTER(WHERE l.state='active'),count(*) FILTER(WHERE t.revoked_at IS NOT NULL) FROM agenteam_object.object_transfers t JOIN agenteam_object.object_leases l ON l.id=t.lease_id WHERE t.project_id=$1 AND t.direction='get' AND t.owner_kind='skill_revision'`, w.app.a.String()).Scan(&total, &complete, &retired, &active, &revoked)
	if err != nil {
		t.Log("M3 Skill prepare original Transfer diagnostic unavailable")
		return
	}
	t.Logf("M3 Skill prepare original Transfer counts total=%d complete=%d retired=%d active_leases=%d revoked=%d", total, complete, retired, active, revoked)
}

func skillsPrepareM3AwaitPeer(t *testing.T, w *m1Workflow, count int) netfixture.State {
	t.Helper()
	ctx, cancel := context.WithTimeout(w.app.ctx, 30*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		state, err := w.network.State(ctx, w.scenario)
		workAppCheck(t, "skill-prepare-peer-state", err)
		if len(state.Requests) > count {
			t.Fatal("controlled Model crossed its original hold")
		}
		if len(state.Requests) == count {
			return state
		}
		select {
		case <-ctx.Done():
			t.Fatal("Skill preparation did not reach the next Model request")
		case <-tick.C:
		}
	}
}

func skillsPrepareM3Facts(t *testing.T, w *m1Workflow, skill skc.SkillID, revision skc.RevisionID) {
	t.Helper()
	v := w.app
	var report, stdout, stderr []byte
	workAppCheck(t, "skill-prepare-command-facts", v.admin.QueryRow(v.ctx, `SELECT body_data,stdout_data,stderr_data FROM agenteam_tool.command_results WHERE execution_id=$1`, w.execution.String()).Scan(&report, &stdout, &stderr))
	actual, err := p.DecodeRunCommandResult(report)
	workAppCheck(t, "skill-prepare-command-report", err)
	if actual.ExitCode != 0 || actual.Signal != 0 || actual.Cancelled || actual.TimedOut || !actual.OutputComplete || !actual.ProcessGroupTerminationConfirmed || actual.StdoutTruncated || actual.StderrTruncated || actual.CompletionCode != nil || string(stdout) != skillsPrepareM3Output || len(stderr) != 0 {
		t.Fatal("script did not actually exit successfully with complete output")
	}
	var operations, models, closedModels, transfers, closedTransfers, activeLeases int
	err = v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id::text=$1::text AND phase='succeeded' AND outcome='success'),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_model.invocations i ON i.id=c.invocation_id WHERE i.execution_id::text=$1::text),
 (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_model.invocations i ON i.id=c.invocation_id WHERE i.execution_id::text=$1::text AND c.retired AND c.phase='succeeded' AND i.final_status='succeeded' AND i.dispatch='sent' AND NOT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls u WHERE u.call_id=c.id AND u.phase<>'business_closed')),
 (SELECT count(*) FROM agenteam_object.object_transfers WHERE execution_id::text=$1::text AND direction='get'),
 (SELECT count(*) FROM agenteam_object.object_transfers t JOIN agenteam_object.object_leases l ON l.id=t.lease_id JOIN agenteam_skill.revisions r ON r.object_id=t.object_id JOIN agenteam_object.objects o ON o.id=t.object_id WHERE t.execution_id::text=$1::text AND t.direction='get' AND t.phase='complete' AND t.retirement_evidence IS NOT NULL AND l.state='released' AND t.owner_kind='skill_revision' AND t.owner_id::text=r.id::text AND r.skill_id::text=$2::text AND r.id::text=$3::text AND t.sha256=o.sha256),
 (SELECT count(*) FROM agenteam_object.object_leases l JOIN agenteam_object.object_transfers t ON t.lease_id=l.id WHERE t.execution_id::text=$1::text AND l.state='active')`, w.execution.String(), skill.String(), revision.String()).Scan(&operations, &models, &closedModels, &transfers, &closedTransfers, &activeLeases)
	if err != nil {
		var coded interface{ SQLState() string }
		state := "unavailable"
		if errors.As(err, &coded) {
			candidate := coded.SQLState()
			valid := len(candidate) == 5
			for _, ch := range candidate {
				valid = valid && (ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'Z')
			}
			if valid {
				state = candidate
			}
		}
		t.Logf("M3 Skill prepare original durable observation sqlstate=%s", state)
	}
	workAppCheck(t, "skill-prepare-original-durable-facts", err)
	if operations != 2 || models != 3 || closedModels != 3 || transfers != 1 || closedTransfers != 1 || activeLeases != 0 {
		t.Fatalf("Skill prepare durable counts operations=%d models=%d closed_models=%d transfers=%d closed_transfers=%d active_leases=%d", operations, models, closedModels, transfers, closedTransfers, activeLeases)
	}
	response := taskExecutionGet(t, v.workAppFixture, v.client, v.cookie, "GET", taskExecutionReadPath(v.a, w.task.ID.String(), w.execution)+"/runtime-view", 200)
	var view ec.TaskRuntimeView
	workAppCheck(t, "skill-prepare-final-projection", json.Unmarshal(response.body, &view))
	if view.ValidateProjection() != nil || view.Execution.Status != ec.Succeeded || view.Snapshot == nil || len(view.Snapshot.Items) != 1 || view.Snapshot.Items[0].Data.Content != skillsPrepareM3Final {
		t.Fatal("actual final projection differs")
	}
}
func skillsPrepareM3StartRunner(t *testing.T, w *m1Workflow) {
	t.Helper()
	if w.runner != nil || w.runnerToken == "" {
		t.Fatal("M1 Runner may only be enrolled once from original material")
	}
	binary := m1RunnerBinary(t)
	// The real identity owner rejects /tmp's world-writable ancestors. Only
	// this generated private directory is removed, after process and TLS join.
	private, err := os.MkdirTemp(".", ".m1-runner-")
	workAppCheck(t, "m1-runner-private-directory", err)
	private, err = filepath.Abs(private)
	workAppCheck(t, "m1-runner-private-absolute", err)
	workAppCheck(t, "m1-runner-private-mode", os.Chmod(private, 0700))
	t.Cleanup(func() {
		if err := os.RemoveAll(private); err != nil {
			t.Error("M1 Runner private directory cleanup failed")
		}
	})
	proxy, origin, ca := w.runnerTLS(t, private)
	v := &m1RunnerProcess{cmd: exec.Command(binary, "--enroll"), done: make(chan struct{}), proxy: proxy, identityPath: filepath.Join(private, "identity.json"), configuration: identity.Configuration{CentralURL: origin, RunnerID: p.ID(w.runnerID.String()), RootPath: w.root}}
	v.cmd.Dir = private
	v.cmd.Env = append(skillsPrepareM3RunnerEnvironment(t, private), "AGENTEAM_RUNNER_IDENTITY_FILE="+v.identityPath, "AGENTEAM_RUNNER_CA_FILE="+ca, "AGENTEAM_RUNNER_CENTRAL_URL="+origin, "AGENTEAM_RUNNER_ID="+w.runnerID.String(), "AGENTEAM_RUNNER_ROOT_PATH="+w.root, "AGENTEAM_RUNNER_SHUTDOWN_TIMEOUT=3s", "AGENTEAM_RUNNER_OPERATIONS=read-file,write-file,run-command,prepare-skill")
	v.cmd.Stdout, v.cmd.Stderr = &v.stdout, &v.stderr
	read, write, err := os.Pipe()
	workAppCheck(t, "m1-runner-enrollment-pipe", err)
	defer read.Close()
	defer write.Close()
	v.cmd.Stdin = read
	workAppCheck(t, "m1-runner-original-start", v.cmd.Start())
	w.runner = v
	go func() { v.err = v.cmd.Wait(); close(v.done) }()
	t.Cleanup(func() { v.cleanup(t) })
	t.Logf("m1_runner_original_start pid=%d", v.cmd.Process.Pid)
	workAppCheck(t, "m1-runner-parent-read-close", read.Close())
	workAppCheck(t, "m1-runner-enrollment-write-bound", write.SetWriteDeadline(time.Now().Add(3*time.Second)))
	n, err := io.WriteString(write, w.runnerToken+"\n")
	if err != nil || n != len(w.runnerToken)+1 {
		t.Fatal("M1 original enrollment stdin write incomplete")
	}
	workAppCheck(t, "m1-runner-enrollment-eof", write.Close())
	v.secrets = []string{w.runnerToken, w.root, v.identityPath}
	ctx, cancel := context.WithTimeout(w.app.ctx, 12*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-v.done:
			t.Fatal("M1 original Runner exited before authenticated Hello")
		default:
		}
		if v.stderr.event("runner_connection", "state", "connected") {
			snapshot, err := w.runners.service.Get(ctx, w.app.actor, w.runnerID)
			workAppCheck(t, "m1-runner-real-online", err)
			if snapshot.Status == rc.Online && snapshot.LastHello != nil {
				if !slices.Equal(snapshot.LastHello.Capabilities, []string{p.ReadFileOperation, p.WriteFileOperation, p.RunCommandOperation, p.SkillPrepareOperation}) || len(snapshot.LastHello.FeatureFlags) != 0 || snapshot.CredentialGeneration != 1 || snapshot.PublicKeyFingerprint == nil {
					t.Fatal("M1 original Runner Hello or credential facts differ")
				}
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("M1 original Runner did not become online within its bound")
		case <-tick.C:
		}
	}
	file, err := identity.Open(v.identityPath)
	if file != nil {
		_ = file.Close()
	}
	if !errors.Is(err, identity.ErrLocked) {
		t.Fatal("M1 live Runner does not own its original identity lock")
	}
	stored, err := identity.ReadOnly(v.identityPath)
	workAppCheck(t, "m1-runner-persisted-identity", err)
	if stored.State() != identity.Active || stored.Configuration() != v.configuration {
		t.Fatal("M1 original enrollment persisted different identity facts")
	}
	raw, err := os.ReadFile(v.identityPath)
	workAppCheck(t, "m1-runner-private-identity-inspection", err)
	defer clear(raw)
	var protected struct {
		Seed string `json:"private_seed"`
	}
	if json.Unmarshal(raw, &protected) != nil || protected.Seed == "" {
		t.Fatal("M1 original identity omitted private key material")
	}
	v.secrets = append(v.secrets, protected.Seed)
}

func skillsPrepareM3RunnerEnvironment(t *testing.T, private string) []string {
	t.Helper()
	descriptor, err := objectfixture.Load()
	workAppCheck(t, "skill-prepare-owned-object-coordinates", err)
	ownedCA, err := os.ReadFile(descriptor.CAFile)
	workAppCheck(t, "skill-prepare-owned-object-ca", err)
	if !x509.NewCertPool().AppendCertsFromPEM(ownedCA) {
		t.Fatal("owned Object CA is not a certificate")
	}
	paths := []string{"/etc/ssl/certs/ca-certificates.crt", "/etc/pki/tls/certs/ca-bundle.crt", "/etc/ssl/ca-bundle.pem", "/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem"}
	if original := os.Getenv("SSL_CERT_FILE"); original != "" {
		paths = []string{original}
	}
	var roots []byte
	for _, path := range paths {
		roots, err = os.ReadFile(path)
		if err == nil && x509.NewCertPool().AppendCertsFromPEM(roots) {
			break
		}
		roots = nil
	}
	if len(roots) == 0 {
		t.Fatal("original system certificate bundle unavailable")
	}
	combined := append(append(roots, '\n'), ownedCA...)
	caFile := filepath.Join(private, "object-trust.pem")
	workAppCheck(t, "skill-prepare-owned-object-trust", os.WriteFile(caFile, combined, 0600))
	// Only this child receives the nonce-verified MinIO address as an additional
	// no-proxy entry. All external proxy routes, prior exclusions and system trust
	// roots remain in place; the real signed GET and TLS verification are unchanged.
	noProxy := os.Getenv("NO_PROXY")
	if noProxy == "" {
		noProxy = os.Getenv("no_proxy")
	}
	if noProxy != "" {
		noProxy += ","
	}
	return append(skillsPrepareM3InheritedEnvironment(os.Environ()), "SSL_CERT_FILE="+caFile, "NO_PROXY="+noProxy+descriptor.PrivateIP)
}

func skillsPrepareM3InheritedEnvironment(original []string) []string {
	var inherited []string
	for _, entry := range original {
		key, _, _ := strings.Cut(entry, "=")
		// The parent's AGENTEAM_RUNNER_BINARY is a fixture setting, not part
		// of the Runner's closed configuration. Add only this call's explicit
		// Runner settings at the process constructor above.
		if !strings.HasPrefix(key, "AGENTEAM_RUNNER_") {
			inherited = append(inherited, entry)
		}
	}
	return inherited
}

func skillsPrepareM3ToolReply(t *testing.T, tool id.ToolID, args any, ordinal int) netfixture.WireScenario {
	return skillsPrepareM3Reply(t, map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": fmt.Sprintf("skills_call_%d", ordinal), "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tool.String(), "-", ""), "arguments": string(workAppJSON(t, args))}}}}, "tool_calls", ordinal)
}
func skillsPrepareM3TextReply(t *testing.T, text string, ordinal int) netfixture.WireScenario {
	return skillsPrepareM3Reply(t, map[string]any{"role": "assistant", "content": text}, "stop", ordinal)
}
func skillsPrepareM3Reply(t *testing.T, message any, finish string, ordinal int) netfixture.WireScenario {
	return netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json", "X-Request-Id": fmt.Sprintf("skills-round-%d", ordinal)}, Chunks: [][]byte{workAppJSON(t, map[string]any{"id": "skills-fixture-response", "object": "chat.completion", "created": 1, "model": "skills-fixture", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})}}
}

func skillsPrepareM3ConfigureModel(t *testing.T, v *executionSchedulerApp, endpoint string) mc.ModelID {
	t.Helper()
	secrets, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("original Secret Service absent")
	}
	material, err := sc.NewSecretMaterial([]byte("owned-skills-fixture-credential"))
	workAppCheck(t, "skills-secret-material", err)
	identity, err := f.NewCommandIdentity("secret", []string{v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "skills-secret-command", err)
	credential, err := secrets.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: id.SystemScope(), Identity: identity, Kind: sc.Create, Purpose: sc.Model, Value: material})
	material.Destroy()
	workAppCheck(t, "skills-secret-create", err)
	var receipt mc.CommandReceipt
	meetingM3SystemCall(t, v, "POST", "model-providers", map[string]any{"input": map[string]any{"name": "Skills fixture Provider", "protocol": mc.OpenAIChat, "base_url": endpoint, "enabled": true, "credential_ref": credential.Metadata.CredentialRef.Details().ID, "options": map[string]any{}}}, &receipt)
	provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "skills-provider-id", err)
	meetingM3SystemCall(t, v, "POST", "models", map[string]any{"provider_id": provider, "input": map[string]any{"name": "Skills fixture Model", "provider_model_id": "skills-fixture", "type": mc.ChatModel, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": map[string]any{"tool_calls": true, "parallel_tool_calls": false, "streaming": true, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": mc.TokenCount(2048)}}}, &receipt)
	model, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "skills-model-id", err)
	return model
}

const skillsPrepareM3ColdFinal = "The previous preparation may have completed; its result is unknown and was not repeated."

type skillsPrepareM3ColdFixture struct {
	w             *m1Workflow
	skill         skc.SkillID
	revision      skc.RevisionID
	sprint        pc.SprintID
	sprintVersion f.Version
	disconnected  m2RecoveryRunnerConnection
}

func skillsPrepareM3ProvisionCold(t *testing.T, trace *skillsPrepareM3Transactions) skillsPrepareM3ColdFixture {
	t.Helper()
	w, _ := newM1WorkflowWithReviewWindow(t, trace.observe, true, nil, 100000)
	v := w.app
	w.seed(t)
	var rawTool string
	workAppCheck(t, "skill-prepare-registered-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key='builtin:prepare-skill'`).Scan(&rawTool))
	prepareTool, err := f.ParseID[id.Tool](rawTool)
	workAppCheck(t, "skill-prepare-tool-id", err)
	skillID := guardID[pc.Skill](t)
	var receipt struct {
		SkillID  skc.SkillID `json:"skill_id"`
		Revision f.Revision  `json:"revision"`
		Version  f.Version   `json:"version"`
	}
	v.httpSeed(t, "/skills", map[string]any{"request": map[string]any{"skill_id": skillID, "mode": "create", "source": map[string]any{"kind": "text_files", "files": []map[string]string{
		{"path": "SKILL.md", "utf8_text": skillsPrepareM3Entry}, {"path": "scripts/run.sh", "utf8_text": skillsPrepareM3Script}, {"path": "references/check.txt", "utf8_text": "canonical-package-input\n"},
		// Canonical ZIP uses Store. Keep more than the Object reader's initial
		// integrity buffer unread so OpenPackage cannot reach EOF and release
		// the physical reader lease before the requested crash window.
		{"path": "references/reader-lifetime.txt", "utf8_text": strings.Repeat("bounded reader fixture\n", 4096)},
	}}}}, &receipt)
	if receipt.SkillID != skillID || receipt.Revision != 1 || receipt.Version != 1 {
		t.Fatal("formal Owner installation receipt differs")
	}
	var revisionRaw string
	workAppCheck(t, "skill-prepare-published-revision", v.admin.QueryRow(v.ctx, `SELECT id::text FROM agenteam_skill.revisions WHERE project_id::text=$1::text AND skill_id::text=$2::text AND revision=1`, v.a.String(), skillID.String()).Scan(&revisionRaw))
	revisionID, err := f.ParseID[skc.Revision](revisionRaw)
	workAppCheck(t, "skill-prepare-revision-id", err)
	// Future replies are configured only before any Task can capture the URL.
	w.scenario, err = w.network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		skillsPrepareM3TextReply(t, "Not yet configured.", 1), skillsPrepareM3TextReply(t, skillsPrepareM3ColdFinal, 2),
	}})
	workAppCheck(t, "skill-prepare-model-peer", err)
	t.Cleanup(func() { _ = w.network.Release(context.Background(), w.scenario) })
	model := skillsPrepareM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+w.scenario)
	disabled := false
	v.agent = guardID[id.Agent](t)
	allowed := []id.ToolID{prepareTool}
	slices.SortFunc(allowed, func(a, b id.ToolID) int { return strings.Compare(a.String(), b.String()) })
	declarations := []ac.RunnerMountDeclaration{{RunnerID: w.runnerID, Workspace: "skill-work"}}
	create, err := ac.NewAgentCreate(ac.AgentCreateFields{AgentID: v.agent, Name: "skill-prepare-agent", Description: "Assigned package execution", Instructions: "Prepare the explicitly assigned skill once. If its original result is unknown, report uncertainty without retrying or executing scripts.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: allowed, AllowedMountIDs: nil, AllowedSecretVariableIDs: []id.ProjectVariableID{}, RunnerMounts: &declarations, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled, InjectAgentsMD: false})
	workAppCheck(t, "skill-prepare-agent-intent", err)
	created, err := v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, create)
	workAppCheck(t, "skill-prepare-agent-create", err)
	mounts := created.Fields().RunnerMounts
	if mounts == nil || len(*mounts) != 1 {
		t.Fatal("original Agent has no exact Runner mount")
	}
	w.mount = (*mounts)[0].MountID
	w.workspace = filepath.Join(w.root, v.a.String(), v.agent.String(), (*mounts)[0].Workspace)
	if _, err = os.Lstat(w.workspace); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Skill workspace was fabricated before Runner preparation")
	}
	response, err := v.request(v.ctx, "POST", "/api/v1/projects/"+v.a.String()+"/agents/"+v.agent.String()+"/skills/assign", workAppJSON(t, map[string]any{"expected_version": f.Version(1), "request": map[string]any{"skill_id": skillID, "enabled": true}}), guardID[struct{}](t).String())
	workAppCheck(t, "skill-prepare-owner-assign", err)
	if response.status != 200 {
		t.Fatalf("Skill assignment HTTP status=%d", response.status)
	}
	var assigned skc.AssignmentMutation
	workAppCheck(t, "skill-prepare-assignment-receipt", json.Unmarshal(response.body, &assigned))
	if assigned.Validate() != nil || !assigned.Changed || assigned.AgentVersion != 2 {
		t.Fatal("original Owner assignment did not apply")
	}

	workAppCheck(t, "prepare-cold-first-reply", w.network.UpdateFutureWire(v.ctx, w.scenario, 1, skillsPrepareM3ToolReply(t, prepareTool, map[string]any{"skill_id": skillID, "mount_id": w.mount}, 1)))
	trace.start()
	skillsPrepareM3StartRunner(t, w)
	sprint := memoryM3CreateSprint(t, v)
	w.task = memoryM3CreateTask(t, v, sprint.ID, v.agent, "Prepare this assigned Skill once. If the prior attempt is unknown, report that uncertainty without retrying or executing scripts.")
	// Leave the Sprint planned. No Scheduler can launch before the child has
	// opened the real Owner package reader and reported its original readiness.
	before := m2RecoveryRunnerConnections(&w.runner.stderr)
	taskExecutionJoinEnabled(t, v)
	disconnected := m2AwaitRecoveryRunner(t, w, nil, before, false)
	return skillsPrepareM3ColdFixture{w, skillID, revisionID, sprint.ID, sprint.Version, disconnected}
}

// This is a real process transition, not an in-process Recover masquerading as
// cold recovery. Both the GET and the reader lease originate from public APIs.
func TestSkillsPrepareM3AppRecovery(t *testing.T) {
	t.Run("cold-terminal-reader", func(t *testing.T) {
		trace := &skillsPrepareM3Transactions{}
		fixture := skillsPrepareM3ProvisionCold(t, trace)
		w, v := fixture.w, fixture.w.app
		defer func() {
			trace.finish(t)
			if t.Failed() {
				skillsPrepareM3ConnectionDiagnosis(t, w)
				skillsPrepareM3Diagnose(t, w)
			}
		}()
		first := skillsPrepareM3StartColdChild(t, fixture, "hold-reader")
		defer func() {
			if t.Failed() {
				skillsPrepareM3ColdDiagnosis(t, fixture, first)
			}
		}()
		m2AwaitRecoveryRunner(t, w, first, fixture.disconnected, true)
		response, err := v.request(v.ctx, "POST", "/api/v1/projects/"+v.a.String()+"/sprints/"+fixture.sprint.String()+"/start", workAppJSON(t, map[string]any{"expected_version": fixture.sprintVersion, "request": map[string]any{}}), guardID[struct{}](t).String())
		workAppCheck(t, "prepare-cold-start-original-sprint", err)
		if response.status != 200 {
			t.Fatalf("prepare cold Sprint start status=%d", response.status)
		}
		skillsPrepareM3AwaitHeldReader(t, v.workAppFixture, first)
		var rawExecution, original, transferID, readerID string
		var held bool
		workAppCheck(t, "prepare-cold-original-barrier", v.admin.QueryRow(v.ctx, `SELECT e.id,
    jsonb_build_array(o.operation_id,o.execution_id,o.input_data,o.input_digest,o.fingerprint,o.active_attempt_id,a.attempt_id,a.process_id,a.backend_request_id,a.ordinal,a.fence)::text,
    tr.id::text,l.id::text,
    e.status='running' AND o.phase='running' AND o.version=2 AND a.phase='running' AND NOT a.retired AND a.process_id=$2 AND l.process_id::text=$2 AND l.state='active' AND tr.phase='complete' AND tr.retirement_evidence IS NOT NULL AND octet_length(tr.retirement_digest)=32 AND lease.state='active' AND tr.revoked_at IS NULL
    FROM agenteam_execution.executions e JOIN agenteam_tool.operations o ON o.execution_id=e.id
    JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id
    JOIN agenteam_object.object_transfers tr ON tr.operation_id::text=o.operation_id
    JOIN agenteam_object.object_leases lease ON lease.id=tr.lease_id
    JOIN agenteam_object.object_leases l ON l.object_id=tr.object_id AND l.owner_kind='reader' AND l.state='active'
    WHERE e.project_id=$1 AND o.operation_kind='skill.prepare'`, v.a.String(), first.process).Scan(&rawExecution, &original, &transferID, &readerID, &held))
		if !held {
			t.Fatal("original GET/reader/Tool tuple was not durably held")
		}
		w.execution, err = f.ParseID[id.Execution](rawExecution)
		workAppCheck(t, "prepare-cold-execution", err)
		m2RecoveryGuardHeld(t, w, first.process)
		state, err := w.network.State(v.ctx, w.scenario)
		workAppCheck(t, "prepare-cold-before-peer", err)
		if len(state.Requests) != 1 {
			t.Fatal("Tool result escaped the original crash barrier")
		}
		m2KillRecoveryChild(t, w, first)
		second := skillsPrepareM3StartColdChild(t, fixture, "observe")
		if second.process == first.process {
			t.Fatal("new App reused the old guard")
		}
		m2AwaitRecoveredTerminal(t, w, second)
		state = w.joinedPeer(t, w.scenario, 2, 2)
		unknown, err := skc.DecodePrepareUnknownResult([]byte(skillsPrepareM3LastTool(t, state.Requests[1].Body)))
		workAppCheck(t, "prepare-cold-model-consumed-unknown", err)
		if unknown.SkillID != fixture.skill || unknown.RevisionID != fixture.revision || unknown.MountID != w.mount || unknown.Revision != 1 || !unknown.MayHavePrepared {
			t.Fatal("cold result replaced original identity or claimed success")
		}
		var current string
		var closed bool
		var operations, transfers, commands, models, readerWorks int
		workAppCheck(t, "prepare-cold-original-closed-facts", v.admin.QueryRow(v.ctx, `SELECT
    jsonb_build_array(o.operation_id,o.execution_id,o.input_data,o.input_digest,o.fingerprint,o.active_attempt_id,a.attempt_id,a.process_id,a.backend_request_id,a.ordinal,a.fence)::text,
    o.phase='failed' AND o.version=3 AND a.phase='unknown' AND a.retired AND o.outcome='unknown' AND tr.id::text=$3 AND tr.retirement_evidence IS NOT NULL AND lease.state='released' AND l.id::text=$4 AND l.state='released',
    (SELECT count(*) FROM agenteam_tool.operations WHERE execution_id=$1),
    (SELECT count(*) FROM agenteam_object.object_transfers WHERE execution_id::text=$1),
    (SELECT count(*) FROM agenteam_tool.command_results WHERE execution_id=$1),
    (SELECT count(*) FROM agenteam_model.calls c JOIN agenteam_model.invocations i ON i.id=c.invocation_id WHERE i.execution_id::text=$1::text AND c.phase='succeeded' AND c.retired AND i.final_status='succeeded'),
    (SELECT count(*) FROM agenteam_skill.work WHERE project_id::text=$2 AND phase<>'joined')
    FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id
    JOIN agenteam_tool.builtin_results r ON r.operation_id=o.operation_id AND r.attempt_id=a.attempt_id
    JOIN agenteam_object.object_transfers tr ON tr.operation_id::text=o.operation_id
    JOIN agenteam_object.object_leases lease ON lease.id=tr.lease_id
    JOIN agenteam_object.object_leases l ON l.id::text=$4
    WHERE o.execution_id=$1 AND o.operation_kind='skill.prepare'`, w.execution.String(), v.a.String(), transferID, readerID).Scan(&current, &closed, &operations, &transfers, &commands, &models, &readerWorks))
		if current != original || !closed || operations != 1 || transfers != 1 || commands != 0 || models != 2 || readerWorks != 0 {
			t.Fatalf("cold exact retirement mismatch same_tuple=%t closed=%t operations=%d transfers=%d commands=%d models=%d reader_works=%d", current == original, closed, operations, transfers, commands, models, readerWorks)
		}
		relative, err := p.SkillRevisionPath(p.ID(fixture.skill.String()), p.ID(fixture.revision.String()))
		workAppCheck(t, "prepare-cold-fixed-path", err)
		body, err := os.ReadFile(filepath.Join(w.workspace, relative, "scripts/run.sh"))
		workAppCheck(t, "prepare-cold-original-prepared-effect", err)
		if string(body) != skillsPrepareM3Script {
			t.Fatal("original Runner did not prepare the canonical script")
		}
		m2StopRecoveryChild(t, w, second)
		m2StopRecoveryRunner(t, w)
		t.Log("M3_SKILL_PREPARE_COLD actual_reader_held=true terminal_get_before_sigkill=true original_wait=true new_guard=true exact_tuple=true zero_redispatch=true unknown_consumed=true original_app_runner_joined=true")
	})
}

type skillsPrepareM3ColdStore struct {
	*postgres.Store
	held             atomic.Bool
	traceStarted     atomic.Bool
	traceEvents      atomic.Int32
	observerStarted  atomic.Int32
	observerReturned atomic.Int32
	mode             string
	project          id.ProjectID
}

func (s *skillsPrepareM3ColdStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	started := time.Now()
	result := s.Store.WithinTx(ctx, cause, fn)
	s.diagnose(cause, result, time.Since(started))
	d := cause.Details()
	if s.mode != "hold-reader" || result.State() != f.Committed || d.Kind != f.RecoveryCause || d.Owner != "object" || s.held.Load() {
		return result
	}
	var held bool
	s.observerStarted.Add(1)
	err := s.Store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_object.object_transfers t JOIN agenteam_tool.operations o ON o.operation_id=t.operation_id::text JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id WHERE t.project_id::text=$1::text AND t.owner_kind='skill_revision' AND t.direction='get' AND t.phase='complete' AND t.retirement_evidence IS NOT NULL AND octet_length(t.retirement_digest)=32 AND EXISTS(SELECT 1 FROM agenteam_object.object_leases lease WHERE lease.id=t.lease_id AND lease.state='active') AND o.operation_kind='skill.prepare' AND o.phase='running' AND a.phase='running' AND NOT a.retired AND EXISTS(SELECT 1 FROM agenteam_object.object_leases l WHERE l.object_id=t.object_id AND l.owner_kind='reader' AND l.process_id::text=a.process_id AND l.state='active'))`, s.project.String()).Scan(&held)
	s.observerReturned.Add(1)
	if err != nil {
		m2RecoveryFailure("result-observer", err)
		return result
	}
	if held && s.held.CompareAndSwap(false, true) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m3_prepare_terminal_reader_held"})
		select {} // Only the parent's actual SIGKILL/Wait ends this original callback.
	}
	return result
}

// Both observations use independent fixture read-only connections inside the
// original 15-second budget. Cancellation and Join precede this wait's return,
// including Fatal/Goexit. No original SQL, lock or transaction is interrupted.
func skillsPrepareM3AwaitHeldReader(t *testing.T, v *workAppFixture, child *m2RecoveryChild) {
	t.Helper()
	ctx, cancel := context.WithCancel(v.ctx)
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		for sample := 1; sample <= 2; sample++ {
			select {
			case <-ctx.Done():
				return
			case <-child.done:
				return
			case <-timer.C:
			}
			if sample == 1 {
				_ = child.cmd.Process.Signal(syscall.SIGUSR1)
				// Count the second deadline from the first sample, not after a
				// potentially slow observer query. Each query has its own 1s cap.
				timer.Reset(2 * time.Second)
			}
			skillsPrepareM3ObserveDatabase(t, ctx, v, sample)
		}
	}()
	defer func() { cancel(); <-joined }()
	m2RecoveryEvent(t, child, "m3_prepare_terminal_reader_held", nil)
}

// Project only closed classifications from this owned database. Original
// query text, backend PIDs, relation names, advisory keys and parameters never
// leave PostgreSQL. Session numbers and blocking edges are local to a sample.
func skillsPrepareM3ObserveDatabase(t *testing.T, ctx context.Context, v *workAppFixture, sample int) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	cfg := v.admin.Config().Copy()
	cfg.RuntimeParams["default_transaction_read_only"] = "on"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		code, state := skillsPrepareM3DatabaseObservationError(err)
		t.Logf("M3 Skill prepare PG sample=%d connected=false safe_code=%s sqlstate=%s", sample, code, state)
		return
	}
	defer func() {
		closing, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		err := conn.Close(closing)
		t.Logf("M3 Skill prepare PG sample=%d observer_closed=%t", sample, err == nil && conn.IsClosed())
	}()
	rows, err := conn.Query(ctx, `WITH sessions AS MATERIALIZED (
 SELECT pid,row_number() OVER(ORDER BY pid) AS slot,application_name='agenteam' AS app,
        state,wait_event_type,wait_event,lower(query) AS statement,xact_start,query_start,
        pg_blocking_pids(pid) AS blockers
 FROM pg_stat_activity
 WHERE datname=current_database() AND backend_type='client backend' AND pid<>pg_backend_pid()
), projected AS (
 SELECT s.slot,(SELECT count(*) FROM sessions) AS total,s.app,
 CASE WHEN s.state IN ('active','idle','idle in transaction','idle in transaction (aborted)') THEN s.state ELSE 'other' END AS state,
 CASE WHEN s.statement LIKE '%pg_advisory_xact_lock_shared%' THEN 'lock_shared'
      WHEN s.statement LIKE '%pg_advisory_xact_lock%' THEN 'lock_exclusive'
      WHEN s.statement IN ('commit','rollback','begin') THEN s.statement
      WHEN s.statement LIKE '%set_config(%lock_timeout%' THEN 'lock_timeout'
      WHEN s.statement LIKE 'insert into agenteam_object.object_transfers%' THEN 'transfer_insert'
      WHEN s.statement LIKE 'insert into agenteam_object.object_leases%' THEN 'lease_insert'
      WHEN s.statement LIKE 'insert into agenteam_object.project_work%' THEN 'object_work_insert'
      WHEN s.statement LIKE '%agenteam_object.object_transfers%' THEN 'transfer_read_or_update'
      WHEN s.statement LIKE '%agenteam_object.object_leases%' THEN 'lease_read_or_update'
      WHEN s.statement LIKE '%agenteam_object.project_work%' THEN 'object_work_read_or_update'
      WHEN s.statement LIKE '%agenteam_object.%' THEN 'object_other'
      WHEN s.statement LIKE '%agenteam_runner.%' THEN 'runner'
      WHEN s.statement LIKE '%agenteam_skill.%' THEN 'skill'
      WHEN s.statement LIKE '%agenteam_execution.%' THEN 'execution'
      WHEN s.statement LIKE '%agenteam_agent.%' THEN 'agent'
      WHEN s.statement LIKE '%agenteam_project.%' THEN 'project'
      WHEN s.statement LIKE '%agenteam_audit.%' THEN 'audit'
      WHEN s.statement LIKE '%agenteam_usage.%' THEN 'usage'
      WHEN s.statement LIKE '%agenteam_tool.%' THEN 'tool'
      WHEN s.statement LIKE '%agenteam_account.%' THEN 'account'
      ELSE 'other' END AS stage,
 CASE WHEN s.wait_event_type IS NULL THEN 'none'
      WHEN s.wait_event_type IN ('Lock','LWLock','IO','Client','IPC','Timeout','Activity') THEN s.wait_event_type ELSE 'other' END AS wait_type,
 CASE WHEN s.wait_event IS NULL THEN 'none'
      WHEN s.wait_event IN ('advisory','transactionid','tuple','relation','ClientRead','ClientWrite','WALSync','WALWrite','SyncRep','DataFileRead','DataFileWrite') THEN s.wait_event ELSE 'other' END AS wait_event,
 LEAST(80000,GREATEST(0,COALESCE(EXTRACT(EPOCH FROM clock_timestamp()-s.xact_start)*1000,0)))::bigint AS transaction_ms,
 LEAST(80000,GREATEST(0,COALESCE(EXTRACT(EPOCH FROM clock_timestamp()-s.query_start)*1000,0)))::bigint AS query_ms,
 ARRAY(SELECT b.slot FROM sessions b WHERE b.pid=ANY(s.blockers) ORDER BY b.slot) AS blocker_slots,
 (SELECT count(*) FROM unnest(s.blockers) blocker(pid) WHERE NOT EXISTS(SELECT 1 FROM sessions b WHERE b.pid=blocker.pid)) AS unmapped_blockers,
 EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=s.pid AND NOT l.granted AND l.locktype='advisory') AS advisory_wait,
 EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=s.pid AND NOT l.granted AND l.locktype IN ('tuple','transactionid')) AS row_wait,
 EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=s.pid AND NOT l.granted AND l.locktype='relation') AS relation_wait
 FROM sessions s
)
SELECT * FROM projected ORDER BY slot LIMIT 32`)
	if err != nil {
		code, state := skillsPrepareM3DatabaseObservationError(err)
		t.Logf("M3 Skill prepare PG sample=%d observed=false safe_code=%s sqlstate=%s deadline=%t cancelled=%t", sample, code, state, errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled))
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var slot, total, transactionMS, queryMS, unmapped int64
		var app, advisory, row, relation bool
		var state, stage, waitType, waitEvent string
		var blockers []int64
		if err = rows.Scan(&slot, &total, &app, &state, &stage, &waitType, &waitEvent, &transactionMS, &queryMS, &blockers, &unmapped, &advisory, &row, &relation); err != nil {
			break
		}
		count++
		t.Logf("M3 Skill prepare PG sample=%d session=%d app=%t state=%s stage=%s wait_type=%s wait_event=%s transaction_ms=%d query_ms=%d blockers=%v unmapped_blockers=%d advisory=%t row=%t relation=%t truncated=%t", sample, slot, app, state, stage, waitType, waitEvent, transactionMS, queryMS, blockers, unmapped, advisory, row, relation, total > 32)
	}
	if err == nil {
		err = rows.Err()
	}
	code, state := skillsPrepareM3DatabaseObservationError(err)
	t.Logf("M3 Skill prepare PG sample=%d sessions=%d complete=%t safe_code=%s sqlstate=%s", sample, count, err == nil, code, state)
}

func skillsPrepareM3DatabaseObservationError(err error) (string, string) {
	if err == nil {
		return "none", ""
	}
	code, state := "error", ""
	var fault *f.Fault
	if errors.As(err, &fault) {
		code = string(fault.Code.Safe())
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && m2RecoverySQLState(pg.Code) {
		state = pg.Code
	}
	return code, state
}

// Never emit or persist stack bytes: only fixed function-membership booleans
// from the original Preparer goroutine are observable. The bounded transient
// buffer (including any formatted arguments) is cleared before event output.
func (s *skillsPrepareM3ColdStore) diagnosePosition() {
	buffer := make([]byte, 2<<20)
	n := runtime.Stack(buffer, true)
	flags := map[string]bool{
		"prepare": false, "issue": false, "material": false, "sign": false,
		"access": false, "observer": false, "claim": false, "dispatch": false,
		"mutex": false, "database": false,
	}
	markers := map[string][]byte{
		"issue":    []byte("/object.(*TransferService).IssueTransfer("),
		"material": []byte("/object.(*TransferService).material("),
		"sign":     []byte("/object.(*TransferService).sign("),
		"access":   []byte("/object.(*Service).DiscoverAccess("),
		"observer": []byte("/app.(*skillsPrepareM3ColdStore).WithinTx("),
		"claim":    []byte("/mount.(*SkillPrepareResolver).Claim("),
		"dispatch": []byte("/http.(*controlSession).dispatchSkillPrepare("),
		"mutex":    []byte("Mutex).lockSlow("),
		"database": []byte("/postgres."),
	}
	for _, block := range bytes.Split(buffer[:n], []byte("\n\n")) {
		if !bytes.Contains(block, []byte("/skill.(*Preparer).Execute(")) {
			continue
		}
		flags["prepare"] = true
		for name, marker := range markers {
			flags[name] = flags[name] || bytes.Contains(block, marker)
		}
	}
	clear(buffer)
	started, returned := s.observerStarted.Load(), s.observerReturned.Load()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"event": "m3_prepare_cold_position", "positions": flags,
		"truncated": n == len(buffer), "observer_pending": started > returned,
		"observer_returned": returned > 0,
	})
}

// The parent trace belongs to the provisioning App, not this child. Reuse its
// closed error projection after the real child transaction returns; no raw
// cause, SQL, identifier, payload or signature crosses this diagnostic channel.
func (s *skillsPrepareM3ColdStore) diagnose(cause f.TransactionCause, result f.CommitResult, elapsed time.Duration) {
	if !s.traceStarted.Load() {
		return
	}
	trace := &skillsPrepareM3Transactions{active: true}
	trace.observe(cause, result)
	if len(trace.results) != 1 {
		return
	}
	ordinal := s.traceEvents.Add(1)
	if ordinal > 64 {
		return
	}
	v := trace.results[0]
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"event": "m3_prepare_cold_transaction", "ordinal": ordinal, "stage": v.stage,
		"state": v.state, "code": v.code, "database": v.database, "sqlstate": v.sqlState,
		"deadline": v.deadline, "cancelled": v.canceled, "elapsed_ms": min(int64(80000), elapsed.Milliseconds()),
	})
}

func skillsPrepareM3ColdDiagnosis(t *testing.T, fixture skillsPrepareM3ColdFixture, child *m2RecoveryChild) {
	t.Helper()
	events := slices.Clone(child.pending)
collect:
	for len(events) < 256 {
		select {
		case event := <-child.stdout.events:
			events = append(events, event)
		default:
			break collect
		}
	}
	for _, event := range events {
		if event["event"] == "m3_prepare_cold_position" {
			positions, _ := event["positions"].(map[string]any)
			for _, name := range []string{"prepare", "issue", "material", "sign", "access", "observer", "claim", "dispatch", "mutex", "database"} {
				present, _ := positions[name].(bool)
				t.Logf("M3 Skill prepare child position=%s present=%t", name, present)
			}
			truncated, _ := event["truncated"].(bool)
			pending, _ := event["observer_pending"].(bool)
			returned, _ := event["observer_returned"].(bool)
			t.Logf("M3 Skill prepare child position truncated=%t observer_pending=%t observer_returned=%t", truncated, pending, returned)
			continue
		}
		if event["event"] != "m3_prepare_cold_transaction" {
			continue
		}
		stage, _ := event["stage"].(string)
		ordinal, _ := event["ordinal"].(float64)
		if !slices.Contains([]string{"skill.prepare", "runner-skill-prepare", "object", "runner-connection"}, stage) || ordinal < 1 || ordinal > 64 {
			continue
		}
		state, _ := event["state"].(string)
		code, _ := event["code"].(string)
		safeCode := "none"
		if code != "" {
			safeCode = string(f.Code(code).Safe())
		}
		database, _ := event["database"].(string)
		if !slices.Contains([]string{"", "other", string(postgres.LockFailed), string(postgres.LockOrderViolation), string(postgres.LockUpgradeForbidden), string(postgres.LockNotHeld), string(postgres.InvalidLock), string(postgres.TransactionNested), string(postgres.TransactionConcurrentUse), string(postgres.TransactionRowsOpen), string(postgres.TransactionExpired), string(postgres.InvalidTransaction), string(postgres.TransactionBeginFailed), string(postgres.TransactionCommitFailed), string(postgres.SQLFailed), string(postgres.ConnectionFailed), string(postgres.AdmissionStopped)}, database) {
			database = "other"
		}
		sqlstate, _ := event["sqlstate"].(string)
		if sqlstate != "" && !m2RecoverySQLState(sqlstate) {
			sqlstate = "invalid"
		}
		deadline, _ := event["deadline"].(bool)
		cancelled, _ := event["cancelled"].(bool)
		elapsed, _ := event["elapsed_ms"].(float64)
		t.Logf("M3 Skill prepare child transaction ordinal=%.0f stage=%s state=%s safe_code=%s database_code=%s sqlstate=%s deadline=%t cancelled=%t elapsed_ms=%.0f", ordinal, stage, f.CommitState(state).Safe(), safeCode, database, sqlstate, deadline, cancelled, max(0, min(80000, elapsed)))
	}
	relative, err := p.SkillRevisionPath(p.ID(fixture.skill.String()), p.ID(fixture.revision.String()))
	if err != nil {
		t.Log("M3 Skill prepare child fixed path diagnostic unavailable")
		return
	}
	info, err := os.Lstat(filepath.Join(fixture.w.workspace, relative, "scripts/run.sh"))
	switch {
	case err == nil:
		t.Logf("M3 Skill prepare child original script present=true regular=%t readonly=%t", info.Mode().IsRegular(), info.Mode().Perm() == 0444)
	case errors.Is(err, os.ErrNotExist):
		t.Log("M3 Skill prepare child original script present=false")
	default:
		t.Log("M3 Skill prepare child original script diagnostic unavailable")
	}
}

func skillsPrepareM3StartColdChild(t *testing.T, fixture skillsPrepareM3ColdFixture, mode string) *m2RecoveryChild {
	t.Helper()
	w := fixture.w
	executable, err := os.Executable()
	workAppCheck(t, "prepare-cold-child-binary", err)
	address, err := url.Parse(w.app.address)
	workAppCheck(t, "prepare-cold-original-address", err)
	env := slices.Clone(w.environment)
	for n, entry := range env {
		if strings.HasPrefix(entry, "AGENTEAM_CENTRAL_HTTP_ADDR=") {
			env[n] = "AGENTEAM_CENTRAL_HTTP_ADDR=" + address.Host
		}
	}
	child := &m2RecoveryChild{fixtureProcess: &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestSkillsPrepareM3RecoveryChild$", "-test.timeout=80s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}}
	child.cmd.Dir = t.TempDir()
	actor := w.app.actor.Details()
	child.cmd.Env = append(env, "PATH="+os.Getenv("PATH"), pgfixture.Env+"="+os.Getenv(pgfixture.Env), objectfixture.Env+"="+os.Getenv(objectfixture.Env), netfixture.Env+"="+os.Getenv(netfixture.Env), "AGENTEAM_SKILL_PREPARE_COLD="+mode, "AGENTEAM_SKILL_PREPARE_DATABASE="+w.app.root.db.Name, "AGENTEAM_SKILL_PREPARE_PROJECT="+w.app.a.String(), "AGENTEAM_SKILL_PREPARE_SKILL="+fixture.skill.String(), "AGENTEAM_SKILL_PREPARE_USER="+actor.UserID, "AGENTEAM_SKILL_PREPARE_SESSION="+actor.SessionID)
	child.cmd.Stdout, child.cmd.Stderr = child.stdout, child.stderr
	workAppCheck(t, "prepare-cold-child-start", child.cmd.Start())
	t.Logf("M3_PREPARE_CHILD_START mode=%s pid=%d", mode, child.cmd.Process.Pid)
	go func() { child.err = child.cmd.Wait(); close(child.done) }()
	t.Cleanup(func() {
		defer m2RecoveryChildWaitLog(t, child, "cleanup")
		select {
		case <-child.done:
		default:
			_ = child.cmd.Process.Kill()
			usageAppWait(t, child.done, "prepare-cold-child-cleanup")
		}
	})
	ready := m2RecoveryEvent(t, child, "m2_recovery_ready", nil)
	child.process, _ = ready["process_id"].(string)
	_, err = f.ParseID[oc.Process](child.process)
	workAppCheck(t, "prepare-cold-original-guard", err)
	if mode == "hold-reader" && (ready["reader_open"] != true || ready["reader_lease_active"] != true) {
		t.Fatal("original package reader was not physically active before Task admission")
	}
	return child
}

func TestSkillsPrepareM3RecoveryChild(t *testing.T) {
	mode := os.Getenv("AGENTEAM_SKILL_PREPARE_COLD")
	if mode == "" {
		return
	}
	if mode != "hold-reader" && mode != "observe" {
		t.Fatal("invalid owned prepare recovery mode")
	}
	fixture, err := pgfixture.Load()
	m2RecoveryCheck(t, "database-fixture", err)
	_, err = fixture.Config(os.Getenv("AGENTEAM_SKILL_PREPARE_DATABASE"), nil)
	m2RecoveryCheck(t, "database-config", err)
	network, err := netfixture.Load()
	m2RecoveryCheck(t, "network-fixture", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	m2RecoveryCheck(t, "network-address", err)
	environment := os.Environ()
	values := map[string]string{}
	for _, entry := range environment {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	cfg, err := config.Load(func(key string) (string, bool) { value, found := values[key]; return value, found }, environment)
	m2RecoveryCheck(t, "config", err)
	project, err := f.ParseID[id.Project](os.Getenv("AGENTEAM_SKILL_PREPARE_PROJECT"))
	m2RecoveryCheck(t, "config", err)
	skillID, err := f.ParseID[pc.Skill](os.Getenv("AGENTEAM_SKILL_PREPARE_SKILL"))
	m2RecoveryCheck(t, "config", err)
	user, err := f.ParseID[id.User](os.Getenv("AGENTEAM_SKILL_PREPARE_USER"))
	m2RecoveryCheck(t, "config", err)
	session, err := f.ParseID[id.Session](os.Getenv("AGENTEAM_SKILL_PREPARE_SESSION"))
	m2RecoveryCheck(t, "config", err)
	actor, err := id.NewHuman(user, session)
	m2RecoveryCheck(t, "config", err)
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	diagnostics := make(chan os.Signal, 1)
	signal.Notify(diagnostics, syscall.SIGUSR1)
	defer signal.Stop(diagnostics)
	var owned *resources
	store := &skillsPrepareM3ColdStore{mode: mode, project: project}
	deps := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			var err error
			store.Store, err = postgres.Open(ctx, cfg)
			return store, err
		},
		bind: func(ctx context.Context, cfg config.Config, db database, r *resources, d *dependencies) error {
			owned = r
			return bindAccounts(ctx, cfg, db, r, d)
		},
		outboundConstruct: func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			return nil
		},
	}
	logs := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, io.MultiWriter(os.Stderr, logs))
	m2RecoveryCheck(t, "logger", err)
	done := make(chan struct{})
	var runErr error
	go func() { runErr = run(context.Background(), cfg, logger, signals, deps); close(done) }()
	timer := time.NewTimer(40 * time.Second)
	defer timer.Stop()
listening:
	for {
		select {
		case event := <-logs.events:
			if event["event"] == "listening" {
				break listening
			}
		case <-done:
			m2RecoveryCheck(t, "startup-return", runErr)
			t.Fatal("prepare child returned before listening")
		case <-timer.C:
			m2RecoveryCheck(t, "startup-timeout", context.DeadlineExceeded)
		}
	}
	objects, ok := owned.objects().(*objectAssembly)
	if !ok || objects == nil {
		m2RecoveryCheck(t, "guard", f.NewFault(f.DependencyUnbound, f.NotStarted))
	}
	var reader *skc.PackageReader
	readerLeaseActive := false
	if mode == "hold-reader" {
		accounts, ok := owned.accounts().(*accountAssembly)
		if !ok || accounts == nil {
			m2RecoveryCheck(t, "bind", f.NewFault(f.DependencyUnbound, f.NotStarted))
		}
		skills, ok := accounts.skills.(*skillWork)
		if !ok || skills == nil {
			m2RecoveryCheck(t, "bind", f.NewFault(f.DependencyUnbound, f.NotStarted))
		}
		reader, err = skills.service.OpenPackage(context.Background(), actor, project, skillID, 1)
		m2RecoveryCheck(t, "result-owner", err)
		// Keep a real unread ObjectReader and its exact process lease alive until
		// SIGKILL. No row is injected and no cancellation is treated as Close.
		defer func() {
			if reader != nil {
				_ = reader.Close()
			}
		}()
		metadata, err := reader.Metadata()
		m2RecoveryCheck(t, "result-owner-metadata", err)
		if metadata.Object.ByteSize <= oc.StreamBufferSize {
			t.Fatal("cold fixture package fits in the Object reader integrity buffer")
		}
		// A non-nil public reader alone does not prove a live storage lease:
		// small objects reach verified EOF during construction. Observe the
		// exact real object/process lease before admitting the original Task.
		readCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		var active int64
		err = store.QueryRow(readCtx, `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND process_id=$2 AND owner_kind='reader' AND state='active'`, metadata.Object.ID.String(), objects.process.String()).Scan(&active)
		cancel()
		m2RecoveryCheck(t, "result-owner-active-lease", err)
		if active != 1 {
			t.Fatal("cold fixture does not hold its exact physical package reader")
		}
		readerLeaseActive = true
	}
	store.traceStarted.Store(true)
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_ready", "process_id": objects.process.String(), "reader_open": reader != nil, "reader_lease_active": readerLeaseActive})
	diagnosed := false
running:
	for {
		select {
		case <-done:
			break running
		case <-diagnostics:
			if !diagnosed {
				diagnosed = true
				store.diagnosePosition()
			}
		}
	}
	m2RecoveryCheck(t, "root-return", runErr)
	joined := owned.producersJoined()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_returned", "joined": joined})
	if !joined {
		t.Fatal("prepare recovery returned before its real owners joined")
	}
}
