//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	skc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

func TestSkillsM3InstallColdConfiguration(t *testing.T) {
	TestSkillsM3AssignmentConfiguration(t)
}

// Real original publication commits while the Tool remains running. Only the
// old child's SIGKILL/Wait releases its return barrier. A different default
// App then recovers the same Execution, Attempt and original package receipt.
func TestSkillsM3AppInstallColdPublished(t *testing.T) {
	TestSkillsM3InstallColdConfiguration(t)
	network, err := netfixture.Load()
	skillsAssignmentCheck(t, "cold-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	skillsAssignmentCheck(t, "cold-peer-address", err)
	var environment []string
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		environment = guardEnvironment(t, v.root.db)
		for key, value := range skillsM3Settings() {
			environment = append(environment, "AGENTEAM_CENTRAL_"+key+"="+value)
		}
		environment = append(environment, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		v.root.cfg = guardConfiguration(t, environment)
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			transport = actual
			return nil
		}
	})
	stage := "provision"
	defer func() {
		if t.Failed() {
			skillsM3Diagnose(t, v, stage)
		}
	}()
	if transport == nil {
		t.Fatal("cold fixture lost original outbound")
	}
	m1AllowOwnedPeer(t, v, transport, network)
	w := &skillsM3CatalogWorkflow{v: v, network: network, tools: map[string]id.ToolID{}}
	for _, name := range []string{"install-skill", "list-skills", "read-skill", "assign-skill"} {
		var raw string
		skillsAssignmentCheck(t, "cold-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, "builtin:"+name).Scan(&raw))
		w.tools[name], err = f.ParseID[id.Tool](raw)
		skillsAssignmentCheck(t, "cold-tool-id", err)
	}
	install := map[string]any{"mode": "create", "source": map[string]any{"kind": "text_files", "files": []map[string]string{{"path": "SKILL.md", "utf8_text": skillsM3Text}}}}
	scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		skillsM3ToolReply(t, w.tools["install-skill"], install, 1), skillsM3TextReply(t, "The original installation is available after recovery.", 2),
	}})
	skillsAssignmentCheck(t, "cold-scenario", err)
	model := skillsM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agent := w.agent(t, v.a, model, "cold-install-agent", true)
	sprint := memoryM3CreateSprint(t, v)
	task := memoryM3CreateTask(t, v, sprint.ID, agent, "Install the checklist once and report the original receipt.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	skillsAssignmentCheck(t, "cold-start-sprint", err)
	var initialExecutions int
	skillsAssignmentCheck(t, "cold-provisioning-empty", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_execution.executions WHERE project_id=$1`, v.a.String()).Scan(&initialExecutions))
	if initialExecutions != 0 {
		t.Fatal("provisioning escaped original directory barrier")
	}
	// Stop the real provisioning owner before allowing a child to acquire its
	// own guard. Its stopped manager cannot become the actual installer.
	v.stopBeforeRelease(t)
	v.store.releaseDirectory()
	usageAppWait(t, v.directory.returned, "cold-provision-directory-return")
	usageAppWait(t, v.root.done, "cold-provision-root-return")
	skillsAssignmentCheck(t, "cold-provision-exit", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("provisioning owners not joined")
	}
	skillsAssignmentGuardReleased(t, v.workAppFixture)
	processWorkflow := &m1Workflow{app: v, environment: environment, network: network}
	stage = "original-published-barrier"
	first := skillsM3StartInstallColdChild(t, processWorkflow, "hold-published")
	m2RecoveryEvent(t, first, "skills_install_published_held", nil)
	var operation, execution, attempt, process, request, skill, canonical, origin string
	var running bool
	// Tool IDs are text domains; Skill IDs are UUID domains. Cast only at the
	// cross-domain source FK boundary, preserving the original typed columns.
	skillsAssignmentCheck(t, "cold-original-publication", v.admin.QueryRow(v.ctx, `SELECT o.operation_id,o.execution_id,a.attempt_id,a.process_id,a.backend_request_id,o.skill_id,
 jsonb_build_array(to_jsonb(i),i.xmin::text)::text,
 jsonb_build_array(o.operation_id,o.project_id,o.agent_id,o.execution_id,o.logical_call_id,o.invocation_id,o.call_id,o.tool_id,o.spec_revision,o.input_data,o.input_digest,o.skill_id,o.backend_key,o.fingerprint,a.attempt_id,a.process_id,a.backend_request_id,a.ordinal,a.fence)::text,
 o.phase='running' AND o.version=2 AND a.phase='running' AND NOT a.retired AND i.phase='published'
 FROM agenteam_skill.installations i JOIN agenteam_tool.operations o ON o.operation_id=i.tool_operation_id::text JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id WHERE o.project_id=$1 AND o.agent_id=$2`, v.a.String(), agent.String()).Scan(&operation, &execution, &attempt, &process, &request, &skill, &canonical, &origin, &running))
	if !running || process != first.process {
		t.Fatal("barrier did not preserve original running Tool and published Skill")
	}
	m2RecoveryGuardHeld(t, processWorkflow, first.process)
	stage = "original-sigkill-wait"
	m2KillRecoveryChild(t, processWorkflow, first)
	stage = "new-process-canonical-lookup"
	second := skillsM3StartInstallColdChild(t, processWorkflow, "observe")
	if second.process == first.process {
		t.Fatal("restart reused original process")
	}
	actual := memoryM3AwaitExecution(t, v, task.ID)
	if actual.String() != execution {
		t.Fatal("cold recovery replaced original Execution")
	}
	var afterCanonical, afterOrigin string
	var exact bool
	var installations, uploads, attempts, assignments, liveWork int
	skillsAssignmentCheck(t, "cold-original-terminal", v.admin.QueryRow(v.ctx, `SELECT
 jsonb_build_array(to_jsonb(i),i.xmin::text)::text,
 jsonb_build_array(o.operation_id,o.project_id,o.agent_id,o.execution_id,o.logical_call_id,o.invocation_id,o.call_id,o.tool_id,o.spec_revision,o.input_data,o.input_digest,o.skill_id,o.backend_key,o.fingerprint,a.attempt_id,a.process_id,a.backend_request_id,a.ordinal,a.fence)::text,
 o.active_attempt_id=$2 AND a.process_id=$3 AND a.backend_request_id=$4 AND a.ordinal=1 AND a.fence=1 AND a.retired AND a.phase='succeeded' AND o.phase='succeeded' AND o.version=3 AND o.terminal_data=a.outcome_data,
 (SELECT count(*) FROM agenteam_skill.installations x WHERE x.tool_operation_id::text=o.operation_id),
 (SELECT count(*) FROM agenteam_skill.installation_attempts x WHERE x.tool_attempt_id::text=a.attempt_id),
 (SELECT count(*) FROM agenteam_tool.attempts x WHERE x.operation_id=o.operation_id),
 (SELECT count(*) FROM agenteam_skill.agent_assignments x WHERE x.skill_id::text=o.skill_id),
 (SELECT count(*) FROM agenteam_skill.work x WHERE x.skill_id::text=o.skill_id AND x.phase<>'joined')
 FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id JOIN agenteam_skill.installations i ON i.tool_operation_id::text=o.operation_id WHERE o.operation_id=$1`, operation, attempt, process, request).Scan(&afterCanonical, &afterOrigin, &exact, &installations, &uploads, &attempts, &assignments, &liveWork))
	if canonical != afterCanonical || origin != afterOrigin || !exact || installations != 1 || uploads != 1 || attempts != 1 || assignments != 0 || liveWork != 0 {
		t.Fatalf("cold original facts canonical=%t origin=%t exact=%t installs=%d uploads=%d attempts=%d assignments=%d pending_work=%d", canonical == afterCanonical, origin == afterOrigin, exact, installations, uploads, attempts, assignments, liveWork)
	}
	stage = "normal-new-app-join"
	m2StopRecoveryChild(t, processWorkflow, second)
	state := processWorkflow.joinedPeer(t, scenario, 2, 2)
	var receipt struct {
		SkillID  skc.SkillID `json:"skill_id"`
		Revision f.Revision  `json:"revision"`
		Version  f.Version   `json:"version"`
	}
	skillsAssignmentCheck(t, "cold-model-consumed-receipt", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[1].Body)), &receipt))
	if receipt.SkillID.String() != skill || receipt.Revision != 1 || receipt.Version != 1 {
		t.Fatal("new Model did not consume original receipt")
	}
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	defer cancel()
	for {
		var count int
		skillsAssignmentCheck(t, "cold-new-process-connections", v.admin.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam'`).Scan(&count), ctx)
		if count == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("new App connections did not exit")
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Log("SKILLS_INSTALL_COLD published=true actual_old_sigkill_wait=true new_guard=true original_execution_attempt_process=true canonical_read_only=true install_upload_once=true no_auto_assignment=true same_e_model_receipt=true app_joined=true reserved_cold_not_covered=true external_model=false")
}

func skillsM3StartInstallColdChild(t *testing.T, w *m1Workflow, mode string) *m2RecoveryChild {
	t.Helper()
	exe, err := os.Executable()
	skillsAssignmentCheck(t, "cold-child-executable", err)
	address, err := url.Parse(w.app.address)
	skillsAssignmentCheck(t, "cold-child-address", err)
	env := slices.DeleteFunc(os.Environ(), func(v string) bool {
		return strings.HasPrefix(v, "AGENTEAM_CENTRAL_") || strings.HasPrefix(v, "AGENTEAM_SKILLS_INSTALL_COLD_CHILD=")
	})
	configured := slices.Clone(w.environment)
	for n, value := range configured {
		if strings.HasPrefix(value, "AGENTEAM_CENTRAL_HTTP_ADDR=") {
			configured[n] = "AGENTEAM_CENTRAL_HTTP_ADDR=" + address.Host
		}
	}
	env = append(env, configured...)
	p := &m2RecoveryChild{fixtureProcess: &fixtureProcess{cmd: exec.Command(exe, "-test.run=^TestSkillsM3InstallColdChild$", "-test.timeout=80s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = append(env, "AGENTEAM_SKILLS_INSTALL_COLD_CHILD="+mode, "AGENTEAM_SKILLS_INSTALL_COLD_DATABASE="+w.app.root.db.Name)
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	skillsAssignmentCheck(t, "cold-child-start", p.cmd.Start())
	t.Logf("SKILLS_COLD_CHILD_START mode=%s pid=%d", mode, p.cmd.Process.Pid)
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			usageAppWait(t, p.done, "cold-child-cleanup-wait")
		}
		m2RecoveryChildWaitLog(t, p, "skills-cold-cleanup")
	})
	ready := m2RecoveryEvent(t, p, "m2_recovery_ready", nil)
	p.process, _ = ready["process_id"].(string)
	_, err = f.ParseID[oc.Process](p.process)
	skillsAssignmentCheck(t, "cold-child-process", err)
	return p
}

type skillsM3InstallColdStore struct {
	*postgres.Store
	mode string
	held atomic.Bool
}

func (s *skillsM3InstallColdStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := s.Store.WithinTx(ctx, cause, fn)
	d := cause.Details()
	if s.mode != "hold-published" || s.held.Load() || result.State() != f.Committed || d.Kind != f.CommandsCause || d.Primary.Namespace() != "project" || d.Primary.Command() != "skill.install" {
		return result
	}
	owners := d.Primary.OwnerIDs()
	if len(owners) != 1 {
		m2RecoveryFailure("result-owner", f.NewFault(f.InvalidState, f.NotStarted))
		return result
	}
	var exact bool
	err := s.Store.QueryRow(ctx, `SELECT o.phase='running' AND o.version=2 AND a.phase='running' AND NOT a.retired FROM agenteam_skill.installations i JOIN agenteam_tool.operations o ON o.operation_id=i.tool_operation_id::text JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id WHERE i.command_key=$1 AND i.project_id=$2::uuid AND i.phase='published' AND i.actor_kind='agent_run'`, d.Primary.Key().String(), owners[0]).Scan(&exact)
	if errors.Is(err, pgx.ErrNoRows) {
		return result
	}
	if err != nil {
		m2RecoveryFailure("result-observer", err)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_observer_failed"})
		return result
	}
	if exact && s.held.CompareAndSwap(false, true) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "skills_install_published_held"})
		select {} // Only the parent's actual SIGKILL and original Wait release it.
	}
	return result
}

func TestSkillsM3InstallColdChild(t *testing.T) {
	mode := os.Getenv("AGENTEAM_SKILLS_INSTALL_COLD_CHILD")
	if mode == "" {
		t.Skip("only the original cold parent starts this child")
	}
	if mode != "hold-published" && mode != "observe" {
		t.Fatal("unknown owned cold child mode")
	}
	fixture, err := pgfixture.Load()
	m2RecoveryCheck(t, "database-fixture", err)
	_, err = fixture.Config(os.Getenv("AGENTEAM_SKILLS_INSTALL_COLD_DATABASE"), nil)
	m2RecoveryCheck(t, "database-config", err)
	network, err := netfixture.Load()
	m2RecoveryCheck(t, "network-fixture", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	m2RecoveryCheck(t, "network-address", err)
	env := os.Environ()
	values := make(map[string]string, len(env))
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	cfg, err := config.Load(func(key string) (string, bool) { v, ok := values[key]; return v, ok }, env)
	m2RecoveryCheck(t, "config", err)
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	var owned *resources
	store := &skillsM3InstallColdStore{mode: mode}
	d := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			var err error
			store.Store, err = postgres.Open(ctx, cfg)
			if err != nil {
				m2RecoveryFailure("store-open", err)
			}
			return store, err
		},
		bind: func(ctx context.Context, cfg config.Config, db database, actual *resources, deps *dependencies) error {
			owned = actual
			err := bindAccounts(ctx, cfg, db, actual, deps)
			if err != nil {
				m2RecoveryFailure("bind", err)
			}
			return err
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
	go func() { runErr = run(context.Background(), cfg, logger, signals, d); close(done) }()
	startup := time.NewTimer(40 * time.Second)
	defer startup.Stop()
listening:
	for {
		select {
		case event := <-logs.events:
			if event["event"] == "listening" {
				break listening
			}
		case <-done:
			m2RecoveryFailure("startup-return", runErr)
			t.Fatal("cold child returned before listening")
		case <-startup.C:
			m2RecoveryCheck(t, "startup-timeout", context.DeadlineExceeded)
		}
	}
	objects, ok := owned.objects().(*objectAssembly)
	if !ok || objects == nil {
		m2RecoveryCheck(t, "guard", f.NewFault(f.DependencyUnbound, f.NotStarted))
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_ready", "process_id": objects.process.String()})
	<-done
	m2RecoveryCheck(t, "root-return", runErr)
	joined := owned.producersJoined()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "m2_recovery_returned", "joined": joined})
	if !joined {
		t.Fatal("cold App returned before its actual owners")
	}
}
