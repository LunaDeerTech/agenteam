//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/.agent-state/project-variables-independent/commitproxy"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	skc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	"github.com/jackc/pgx/v5"
)

// Only the original final Tool COMMIT is interrupted. The Skill and its Object
// already exist, and the real synchronous backend has returned. Neither case
// changes a database fact or supplies a synthetic CommitResult.
func TestSkillsM3AppInstallFinalUnknown(t *testing.T) {
	for _, mode := range []string{"commit", "rollback"} {
		t.Run(mode, func(t *testing.T) { skillsM3InstallFinalUnknown(t, mode) })
	}
}

func skillsM3InstallFinalUnknown(t *testing.T, mode string) {
	TestSkillsM3AssignmentConfiguration(t)
	network, err := netfixture.Load()
	skillsAssignmentCheck(t, "install-unknown-network", err)
	address, err := netip.ParseAddr(network.PrivateIP)
	skillsAssignmentCheck(t, "install-unknown-peer-address", err)
	var proxy *commitproxy.Proxy
	var stimulus *skillsM3InstallUnknownStore
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
			stimulus = &skillsM3InstallUnknownStore{executionSchedulerAppStore: v.store, proxy: proxy, original: make(chan skillsM3InstallUnknownFrame, 1), returned: make(chan struct{})}
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
	agent := w.agent(t, v.a, model, "install-unknown-agent", true)
	sprint := memoryM3CreateSprint(t, v)
	task := memoryM3CreateTask(t, v, sprint.ID, agent, "Install the checklist exactly once and report its receipt.")
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprint.Version), v.a, sprint.ID)
	skillsAssignmentCheck(t, "install-unknown-sprint", err)
	v.store.releaseDirectory()
	stage = "physical-final-unknown"
	usageAppWait(t, proxy.Reached(), "install-original-final-commit-frame")
	var frame skillsM3InstallUnknownFrame
	select {
	case frame = <-stimulus.original:
	case <-time.After(5 * time.Second):
		t.Fatal("original final transaction did not return")
	}
	if frame.result.State() != f.Unknown || frame.result.AttemptID().Validate() != nil || proxy.WriterPID() <= 0 {
		t.Fatal("original final transaction was not physically Unknown")
	}
	before, phase, version := skillsM3InstallToolFacts(t, v, frame.operation)
	if phase != "running" || version != 2 {
		t.Fatal("unforwarded final COMMIT changed visible Tool facts")
	}
	if mode == "rollback" {
		ctx, cancel := context.WithTimeout(v.ctx, 5*time.Second)
		skillsAssignmentCheck(t, "install-original-writer-terminate", v.root.db.Terminate(ctx, proxy.WriterPID()))
		tick := time.NewTicker(10 * time.Millisecond)
		for {
			var gone bool
			skillsAssignmentCheck(t, "install-original-writer-gone", v.admin.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM pg_catalog.pg_stat_activity WHERE pid=$1)`, proxy.WriterPID()).Scan(&gone))
			if gone {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("original writer did not exit")
			case <-tick.C:
			}
		}
		tick.Stop()
		cancel()
	}
	proxy.Release()
	if mode == "commit" {
		usageAppWait(t, proxy.Committed(), "install-original-commit-confirmed")
	}
	usageAppWait(t, proxy.HeldJoined(), "install-original-proxy-connection-joined")
	settled, phase, version := skillsM3InstallToolFacts(t, v, frame.operation)
	if mode == "commit" && (phase != "succeeded" || version != 3 || settled == before) || mode == "rollback" && (phase != "running" || version != 2 || settled != before) {
		t.Fatal("physical finalization outcome differs")
	}
	stimulus.releaseReturn() // The exact original Unknown reaches its caller.
	stage = "original-finalization-observation"
	execution := memoryM3AwaitExecution(t, v, task.ID)
	if execution.String() != frame.execution {
		t.Fatal("recovery replaced original Execution")
	}
	after, phase, version := skillsM3InstallToolFacts(t, v, frame.operation)
	if phase != "succeeded" || version != 3 || mode == "commit" && after != settled {
		t.Fatal("original finalization was not settled exactly once")
	}
	var exact bool
	var installations, uploads, attempts, assignments, liveWork int
	skillsAssignmentCheck(t, "install-original-finalized-tuple", v.admin.QueryRow(v.ctx, `SELECT
 o.active_attempt_id=$2 AND a.process_id=$3 AND a.backend_request_id=$4 AND a.ordinal=1 AND a.fence=1 AND a.retired AND a.phase='succeeded' AND o.terminal_data=$5 AND a.outcome_data=$5,
 (SELECT count(*) FROM agenteam_skill.installations i WHERE i.tool_operation_id::text=o.operation_id AND i.phase='published'),
 (SELECT count(*) FROM agenteam_skill.installation_attempts i WHERE i.tool_attempt_id::text=a.attempt_id),
 (SELECT count(*) FROM agenteam_tool.attempts a2 WHERE a2.operation_id=o.operation_id),
 (SELECT count(*) FROM agenteam_skill.agent_assignments x WHERE x.skill_id::text=o.skill_id),
 (SELECT count(*) FROM agenteam_skill.work w WHERE w.skill_id::text=o.skill_id AND w.phase<>'joined')
 FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id WHERE o.operation_id=$1`, frame.operation, frame.attempt, frame.process, frame.request, frame.terminal).Scan(&exact, &installations, &uploads, &attempts, &assignments, &liveWork))
	if !exact || installations != 1 || uploads != 1 || attempts != 1 || assignments != 0 || liveWork != 0 {
		t.Fatalf("original install exact=%t installations=%d uploads=%d attempts=%d assignments=%d live_work=%d", exact, installations, uploads, attempts, assignments, liveWork)
	}
	stage = "normal-app-join"
	w.stop(t)
	state := (&m1Workflow{app: v, network: network}).joinedPeer(t, scenario, 2, 2)
	var receipt struct {
		SkillID  skc.SkillID `json:"skill_id"`
		Revision f.Revision  `json:"revision"`
		Version  f.Version   `json:"version"`
	}
	skillsAssignmentCheck(t, "install-original-model-receipt", json.Unmarshal([]byte(skillsM3LastTool(t, state.Requests[1].Body)), &receipt))
	if receipt.SkillID.String() != frame.skill || receipt.Revision != 1 || receipt.Version != 1 {
		t.Fatal("Agent did not receive original canonical receipt")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	skillsAssignmentCheck(t, "install-final-proxy-joined", proxy.Close(ctx))
	t.Logf("SKILLS_INSTALL_FINAL_UNKNOWN mode=%s physical_unknown=true original_writer_settled=true same_attempt=true backend_once=true original_receipt=true app_joined=true skill_publish_unknown_not_covered=true cold_not_covered=true external_model=false", mode)
}

type skillsM3InstallUnknownFrame struct {
	result                                                 f.CommitResult
	operation, execution, attempt, process, request, skill string
	terminal                                               []byte
}
type skillsM3InstallUnknownStore struct {
	*executionSchedulerAppStore
	proxy    *commitproxy.Proxy
	armed    atomic.Bool
	original chan skillsM3InstallUnknownFrame
	returned chan struct{}
	once     sync.Once
}

func (s *skillsM3InstallUnknownStore) releaseReturn() { s.once.Do(func() { close(s.returned) }) }
func (s *skillsM3InstallUnknownStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	var frame skillsM3InstallUnknownFrame
	selected := false
	result := s.executionSchedulerAppStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := fn(ctx, tx); err != nil {
			return err
		}
		d := cause.Details()
		if s.armed.Load() || d.Kind != f.CommandsCause || d.Primary.Namespace() != "tool.operation" || d.Primary.Command() != "prepare" {
			return nil
		}
		owners := d.Primary.OwnerIDs()
		if len(owners) != 1 {
			return f.NewFault(f.InvalidState, f.NotCommitted)
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		var pid int32
		err = x.QueryRow(ctx, `SELECT o.operation_id,o.execution_id,a.attempt_id,a.process_id,a.backend_request_id,o.skill_id,o.terminal_data,pg_backend_pid()
 FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id
 WHERE o.execution_id=$1 AND o.skill_id IS NOT NULL AND o.phase='succeeded' AND o.version=3 AND a.retired AND a.phase='succeeded'`, owners[0]).Scan(&frame.operation, &frame.execution, &frame.attempt, &frame.process, &frame.request, &frame.skill, &frame.terminal, &pid)
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
	}
	return result
}
func skillsM3InstallToolFacts(t *testing.T, v *executionSchedulerApp, operation string) (string, string, f.Version) {
	t.Helper()
	var facts, phase string
	var version f.Version
	skillsAssignmentCheck(t, "install-exact-tool-facts", v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(to_jsonb(o),o.xmin::text,to_jsonb(a),a.xmin::text)::text,o.phase,o.version FROM agenteam_tool.operations o JOIN agenteam_tool.attempts a ON a.attempt_id=o.active_attempt_id WHERE o.operation_id=$1`, operation).Scan(&facts, &phase, &version))
	return facts, phase, version
}
