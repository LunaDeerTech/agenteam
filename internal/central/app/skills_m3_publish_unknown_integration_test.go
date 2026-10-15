//go:build integration && linux

package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	skc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

func TestSkillsM3AppInstallPublicationUnknown(t *testing.T) {
	t.Run("commit", func(t *testing.T) { skillsM3PublishUnknown(t, false) })
	t.Run("rollback", skillsM3PublishRollbackParent)
}

// The rollback child intentionally retains an unresolved original owner. Only
// its parent's exact SIGKILL/Wait ends that process; this is not graceful Join
// and does not grant a new process permission to recover or resend the call.
func TestSkillsM3PublishUnknownRollbackChild(t *testing.T) {
	if os.Getenv("AGENTEAM_SKILLS_PUBLICATION_CHILD") != "rollback" {
		t.Skip("launched only by the original publication rollback parent")
	}
	skillsM3PublishUnknown(t, true)
}

func skillsM3PublishUnknown(t *testing.T, rollback bool) {
	TestSkillsM3AssignmentConfiguration(t)
	network, err := netfixture.Load()
	skillsAssignmentCheck(t, "install-unknown-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	skillsAssignmentCheck(t, "install-unknown-peer-address", err)
	var proxy *commitproxy.Proxy
	var stimulus *skillsM3PublishUnknownStore
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		proxy, err = commitproxy.New(v.ctx, net.JoinHostPort("127.0.0.1", v.root.db.Fixture.Port))
		skillsAssignmentCheck(t, "install-unknown-proxy", err)
		t.Cleanup(func() {
			proxy.Release()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			skillsAssignmentCheck(t, "install-unknown-proxy-join", proxy.Close(ctx))
		})
		dsn, err := url.Parse(v.root.db.Fixture.URL(v.root.db.Name))
		skillsAssignmentCheck(t, "install-unknown-owned-database", err)
		dsn.Host = proxy.Address()
		env := slices.DeleteFunc(guardEnvironment(t, v.root.db), func(v string) bool {
			return strings.HasPrefix(v, "AGENTEAM_CENTRAL_DATABASE_CA_FILE=")
		})
		for key, value := range skillsM3Settings() {
			env = append(env, "AGENTEAM_CENTRAL_"+key+"="+value)
		}
		env = append(env, "AGENTEAM_CENTRAL_DATABASE_URL="+dsn.String(), "AGENTEAM_CENTRAL_DATABASE_TLS_MODE=disable", "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+network.CAFile)
		v.root.cfg = guardConfiguration(t, env)
		open := d.open
		d.open = func(ctx context.Context, cfg postgres.Config) (database, error) {
			actual, err := open(ctx, cfg)
			if err != nil {
				return actual, err
			}
			if actual != v.store {
				return nil, errors.New("install fixture lost original Store")
			}
			stimulus = &skillsM3PublishUnknownStore{executionSchedulerAppStore: v.store, proxy: proxy, original: make(chan skillsM3InstallUnknownFrame, 1), returned: make(chan struct{}), observed: make(chan f.CommitResult, 1)}
			return stimulus, nil
		}
		d.outboundConstruct = func(_ context.Context, _ config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			transport = actual
			return nil
		}
	})
	if transport == nil || stimulus == nil {
		t.Fatal("install fixture missing real dependencies")
	}
	t.Cleanup(func() { proxy.Release(); stimulus.releaseReturn() })
	stage := "provision"
	defer func() {
		if t.Failed() {
			if rollback {
				_ = json.NewEncoder(os.Stdout).Encode(skillsM3PublicationEvent{Event: "skills_publication_failure", Stage: stage})
			}
			skillsM3Diagnose(t, v, stage)
		}
	}()
	m1AllowOwnedPeer(t, v, transport, network)
	w := &skillsM3CatalogWorkflow{v: v, network: network, tools: map[string]id.ToolID{}}
	for _, name := range []string{"install-skill", "list-skills", "read-skill", "assign-skill"} {
		var raw string
		skillsAssignmentCheck(t, "install-unknown-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, "builtin:"+name).Scan(&raw))
		w.tools[name], err = f.ParseID[id.Tool](raw)
		skillsAssignmentCheck(t, "install-unknown-tool-id", err)
	}
	install := map[string]any{"mode": "create", "source": map[string]any{"kind": "text_files", "files": []map[string]string{{"path": "SKILL.md", "utf8_text": skillsM3Text}}}}
	scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: []netfixture.WireScenario{
		skillsM3ToolReply(t, w.tools["install-skill"], install, 1), skillsM3TextReply(t, "The original installation completed once.", 2),
	}})
	skillsAssignmentCheck(t, "install-unknown-scenario", err)
	model := skillsM3ConfigureModel(t, v, "https://fixture.test:8443/case/"+scenario)
	agent := w.agent(t, v.a, model, "publish-unknown-agent", true)
	sprint := memoryM3CreateSprint(t, v)
	task := memoryM3CreateTask(t, v, sprint.ID, agent, "Install the checklist exactly once and report its receipt.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	skillsAssignmentCheck(t, "install-unknown-sprint", err)
	v.store.releaseDirectory()

	stage = "physical-publication-unknown"
	skillsM3AwaitPublicationFrame(t, v, proxy.Reached())
	var frame skillsM3InstallUnknownFrame
	select {
	case frame = <-stimulus.original:
	case <-time.After(5 * time.Second):
		t.Fatal("original publication did not return")
	}
	if frame.result.State() != f.Unknown || frame.result.AttemptID().Validate() != nil || proxy.WriterPID() <= 0 {
		t.Fatal("publication was not physically Unknown")
	}
	before, phase, version := skillsM3InstallToolFacts(t, v, frame.operation)
	if phase != "running" || version != 2 {
		t.Fatal("publication finalized Tool prematurely")
	}
	if rollback {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		skillsAssignmentCheck(t, "publication-original-writer-terminate", v.root.db.Terminate(ctx, proxy.WriterPID()))
		tick := time.NewTicker(10 * time.Millisecond)
		for {
			var gone bool
			skillsAssignmentCheck(t, "publication-original-writer-gone", v.admin.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_stat_activity WHERE pid=$1)`, proxy.WriterPID()).Scan(&gone))
			if gone {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("publication writer did not exit")
			case <-tick.C:
			}
		}
		tick.Stop()
		cancel()
	}
	proxy.Release()
	if !rollback {
		usageAppWait(t, proxy.Committed(), "publication-original-commit-confirmed")
	}
	usageAppWait(t, proxy.HeldJoined(), "publication-original-proxy-joined")
	var canonicalBefore string
	var publicationPhase string
	skillsAssignmentCheck(t, "publication-settled-canonical", v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(to_jsonb(i),i.xmin::text)::text,i.phase FROM agenteam_skill.installations i WHERE tool_operation_id=$1::uuid`, frame.operation).Scan(&canonicalBefore, &publicationPhase))
	wantPhase := "published"
	if rollback {
		wantPhase = "reserved"
	}
	if publicationPhase != wantPhase {
		t.Fatal("unexpected physical publication outcome")
	}
	stimulus.releaseReturn()
	select {
	case observed := <-stimulus.observed:
		if rollback && (observed.State() != f.NotCommitted || observed.Fault() == nil || observed.Fault().Code != f.ResourceBusy) {
			t.Fatal("reserved canonical publication was not a pending read")
		}
		if !rollback && observed.State() != f.Committed {
			t.Fatal("published canonical read did not commit")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("original returned owner did not perform canonical lookup")
	}
	stage = "original-publication-observation"
	if !rollback {
		execution := memoryM3AwaitExecution(t, v, task.ID)
		if execution.String() != frame.execution {
			t.Fatal("publication recovery replaced original Execution")
		}
	}
	var canonicalAfter string
	var exact bool
	var installations, uploads, attempts, assignments, liveWork int
	skillsAssignmentCheck(t, "publication-original-facts", v.admin.QueryRow(v.ctx, `SELECT
 jsonb_build_array(to_jsonb(i),i.xmin::text)::text,
 o.active_attempt_id=$2 AND a.process_id=$3 AND a.backend_request_id=$4 AND a.ordinal=1 AND a.fence=1,
 (SELECT count(*) FROM agenteam_skill.installations j WHERE j.tool_operation_id=i.tool_operation_id),
 (SELECT count(*) FROM agenteam_skill.installation_attempts j WHERE j.tool_attempt_id::text=a.attempt_id),
 (SELECT count(*) FROM agenteam_tool.attempts a2 WHERE a2.operation_id=o.operation_id),
 (SELECT count(*) FROM agenteam_skill.agent_assignments j WHERE j.skill_id=i.skill_id),
 (SELECT count(*) FROM agenteam_skill.work w WHERE w.skill_id=i.skill_id AND w.phase<>'joined')
 FROM agenteam_skill.installations i JOIN agenteam_tool.operations o ON o.operation_id=i.tool_operation_id::text JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id WHERE o.operation_id=$1`, frame.operation, frame.attempt, frame.process, frame.request).Scan(&canonicalAfter, &exact, &installations, &uploads, &attempts, &assignments, &liveWork))
	if canonicalBefore != canonicalAfter || !exact || installations != 1 || uploads != 1 || attempts != 1 || assignments != 0 || liveWork != 0 {
		t.Fatalf("publication exact=%t unchanged=%t installs=%d uploads=%d attempts=%d assignments=%d live_work=%d", exact, canonicalBefore == canonicalAfter, installations, uploads, attempts, assignments, liveWork)
	}
	after, phase, version := skillsM3InstallToolFacts(t, v, frame.operation)
	if rollback {
		if before != after || phase != "running" || version != 2 {
			t.Fatal("pending publication was replaced with a Tool terminal")
		}
		ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
		pendingErr := v.assembly.agentInstall.Drain(ctx)
		cancel()
		var pendingFault *f.Fault
		if !errors.As(pendingErr, &pendingFault) || pendingFault.CommitState != f.Unknown || pendingFault.CauseID != frame.result.AttemptID().String() || v.assembly.agentInstall.Joined() {
			t.Fatal("pending publication lost its original Unknown or claimed Join")
		}
		state, err := network.State(v.ctx, scenario)
		skillsAssignmentCheck(t, "publication-pending-peer", err)
		if len(state.Requests) != 1 || stimulus.reads.Load() < 1 {
			t.Fatal("pending publication dispatched again")
		}
		// This safe event is emitted only after the actual original Unknown has
		// reached its dispatcher and a canonical reserved read has been observed.
		skillsAssignmentCheck(t, "publication-pending-event", json.NewEncoder(os.Stdout).Encode(skillsM3PublicationEvent{Event: "skills_publication_pending", Database: v.root.db.Name, Process: frame.process}))
		select {} // Parent owns exact SIGKILL and the original process Wait.
	}
	if phase != "succeeded" || version != 3 || before == after {
		t.Fatal("published receipt was not finalized")
	}
	stage = "normal-app-join"
	w.stop(t)
	state := (&m1Workflow{app: v, network: network}).joinedPeer(t, scenario, 2, 2)
	var receipt struct {
		SkillID  skc.SkillID `json:"skill_id"`
		Revision f.Revision  `json:"revision"`
		Version  f.Version   `json:"version"`
	}
	skillsAssignmentCheck(t, "publication-original-model-receipt", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[1].Body)), &receipt))
	if receipt.SkillID.String() != frame.skill || receipt.Revision != 1 || receipt.Version != 1 {
		t.Fatal("Agent did not consume canonical published receipt")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	skillsAssignmentCheck(t, "publication-proxy-joined", proxy.Close(ctx))
	t.Log("SKILLS_PUBLICATION_UNKNOWN mode=commit physical_unknown=true canonical_read=true publication_unchanged=true backend_once=true same_attempt=true original_receipt=true app_joined=true cold_not_covered=true")
}

// Publication includes the original scheduler, Model and Object upload. Use
// that workflow's existing total budget rather than the five-second helper
// intended for an already-started close/join. Physical COMMIT and Join waits
// below retain their independent bounds.
func skillsM3AwaitPublicationFrame(t *testing.T, v *executionSchedulerApp, reached <-chan struct{}) {
	t.Helper()
	started := time.Now()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-reached:
			t.Logf("SKILLS_PUBLICATION_FRAME reached=true elapsed_ms=%d", time.Since(started).Milliseconds())
			return
		case <-v.root.done:
			t.Fatal("publication frame missing after original App returned")
		case <-v.ctx.Done():
			t.Fatalf("publication frame missing within original workflow budget elapsed_ms=%d", time.Since(started).Milliseconds())
		case <-tick.C:
			var terminal bool
			skillsAssignmentCheck(t, "publication-before-frame-state", v.admin.QueryRow(v.ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_tool.operations WHERE project_id=$1::text AND skill_id IS NOT NULL AND phase IN ('succeeded','failed','cancelled'))`, v.a.String()).Scan(&terminal))
			if terminal {
				t.Fatal("original Tool became terminal before publication fault injection")
			}
		}
	}
}

type skillsM3PublishUnknownStore struct {
	*executionSchedulerAppStore
	proxy    *commitproxy.Proxy
	armed    atomic.Bool
	original chan skillsM3InstallUnknownFrame
	returned chan struct{}
	observed chan f.CommitResult
	once     sync.Once
	reads    atomic.Int32
}

func (s *skillsM3PublishUnknownStore) releaseReturn() { s.once.Do(func() { close(s.returned) }) }
func (s *skillsM3PublishUnknownStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	var frame skillsM3InstallUnknownFrame
	selected := false
	publication := cause.Details().Kind == f.CommandsCause && cause.Details().Primary.Namespace() == "project" && cause.Details().Primary.Command() == "skill.install"
	reading := publication && s.armed.Load()
	result := s.executionSchedulerAppStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		if !publication || s.armed.Load() {
			return nil
		}
		owners := cause.Details().Primary.OwnerIDs()
		if len(owners) != 1 {
			return f.NewFault(f.InvalidState, f.NotCommitted)
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var pid int32
		err = x.QueryRow(ctx, `SELECT o.operation_id,o.execution_id,a.attempt_id,a.process_id,a.backend_request_id,o.skill_id,pg_backend_pid()
 FROM agenteam_skill.installations i JOIN agenteam_tool.operations o ON o.operation_id=i.tool_operation_id::text JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id
 WHERE i.command_key=$1 AND i.project_id=$2::uuid AND i.phase='published' AND i.actor_kind='agent_run' AND o.phase='running' AND o.version=2 AND NOT a.retired AND a.phase='running'`, cause.Details().Primary.Key().String(), owners[0]).Scan(&frame.operation, &frame.execution, &frame.attempt, &frame.process, &frame.request, &frame.skill, &pid)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if !s.armed.CompareAndSwap(false, true) {
			return nil
		}
		if err = s.proxy.Arm(pid); err != nil {
			return err
		}
		selected = true
		return nil
	})
	if selected {
		frame.result = result
		s.original <- frame
		select {
		case <-s.returned:
		case <-ctx.Done():
		}
	} else if reading {
		s.reads.Add(1)
		select {
		case s.observed <- result:
		default:
		}
	}
	return result
}

type skillsM3PublicationEvent struct {
	Event    string `json:"event"`
	Stage    string `json:"stage,omitempty"`
	Database string `json:"database"`
	Process  string `json:"process"`
}

func skillsM3PublishRollbackParent(t *testing.T) {
	TestSkillsM3AssignmentConfiguration(t)
	exe, err := os.Executable()
	skillsAssignmentCheck(t, "publication-child-executable", err)
	cmd := exec.Command(exe, "-test.run=^TestSkillsM3PublishUnknownRollbackChild$", "-test.v")
	cmd.Env = append(os.Environ(), "AGENTEAM_SKILLS_PUBLICATION_CHILD=rollback", "TMPDIR="+t.TempDir())
	pipe, err := cmd.StdoutPipe()
	skillsAssignmentCheck(t, "publication-child-stdout", err)
	cmd.Stderr = cmd.Stdout
	skillsAssignmentCheck(t, "publication-child-start", cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	t.Cleanup(func() {
		if !joined {
			_ = cmd.Process.Kill()
			<-done
		}
	})
	events := make(chan skillsM3PublicationEvent, 1)
	scanned := make(chan struct{})
	go func() {
		defer close(scanned)
		s := bufio.NewScanner(pipe)
		for s.Scan() {
			var e skillsM3PublicationEvent
			if json.Unmarshal(s.Bytes(), &e) == nil && (e.Event == "skills_publication_pending" || e.Event == "skills_publication_failure") {
				select {
				case events <- e:
				default:
				}
			}
		}
	}()
	var event skillsM3PublicationEvent
	select {
	case event = <-events:
	case waitErr := <-done:
		joined = true
		t.Fatalf("publication child exited before pending observation: success=%t", waitErr == nil)
	case <-time.After(90 * time.Second):
		t.Fatal("publication child did not reach original pending read")
	}
	if event.Event != "skills_publication_pending" {
		t.Fatalf("publication child failed stage=%s", event.Stage)
	}
	if _, err = f.ParseID[struct{}](event.Process); err != nil {
		t.Fatal("publication child process identity missing")
	}
	skillsAssignmentCheck(t, "publication-original-child-kill", cmd.Process.Kill())
	waitErr := <-done
	joined = true
	var exit *exec.ExitError
	if !errors.As(waitErr, &exit) {
		t.Fatal("publication child lacked original signal Wait")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("publication child was not stopped by exact SIGKILL")
	}
	<-scanned
	fixture, err := pgfixture.Load()
	skillsAssignmentCheck(t, "publication-owned-fixture", err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Connect revalidates the original fixture and requires its nonce-owned DB.
	db, err := fixture.Connect(ctx, event.Database)
	skillsAssignmentCheck(t, "publication-child-db", err)
	var pending bool
	skillsAssignmentCheck(t, "publication-child-still-pending", db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.installations i JOIN agenteam_tool.operations o ON o.operation_id=i.tool_operation_id::text JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id WHERE i.phase='reserved' AND a.process_id=$1 AND o.phase='running' AND o.version=2 AND NOT a.retired)`, event.Process).Scan(&pending))
	if !pending {
		t.Fatal("process exit manufactured a publication outcome")
	}
	skillsAssignmentCheck(t, "publication-child-db-close", db.Close(ctx))
	control, err := fixture.Connect(ctx, "fixture_control")
	skillsAssignmentCheck(t, "publication-cleanup-control", err)
	defer control.Close(context.Background())
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		var count int
		skillsAssignmentCheck(t, "publication-child-connections", control.QueryRow(ctx, `SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE datname=$1`, event.Database).Scan(&count))
		if count == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("killed publication child retained database connections")
		case <-tick.C:
		}
	}
	_, err = control.Exec(ctx, "DROP DATABASE "+pgx.Identifier{event.Database}.Sanitize())
	skillsAssignmentCheck(t, "publication-owned-database-cleanup", err)
	t.Log("SKILLS_PUBLICATION_UNKNOWN mode=rollback physical_unknown=true canonical_reserved=true original_tool_running=true no_resend=true exact_child_sigkill_wait=true app_joined=false cold_not_covered=true")
}
