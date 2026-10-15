//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	mcpfixture "github.com/LunaDeerTech/agenteam/tests/mcp/fixture"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
)

// The child holds the RETURN of an actual successful PutObject. All business
// rows and the original writer lease come from the installed Agent/Tool/Object
// graph. SIGKILL and the original Wait, not a rewritten row, prove owner death.
// The unresolved Tool result remains unresolved: this test does not invent a
// remote result or claim graceful retirement of that Execution.
func TestMCPM3StrandedContentColdRecovery(t *testing.T) {
	mcpM3ConfigurationPreflight(t)
	peer := mcpfixture.New(t)
	network, err := netfixture.Load()
	workAppCheck(t, "mcp-cold-network", err)
	v := newMCPM3App(t, peer, network)
	tool, _ := mcpM3MediaConnection(t, v, peer, "resource_link")
	response := workAppJSON(t, map[string]any{"id": "owned-cold-model", "object": "chat.completion", "created": 1, "model": "mcp-agent", "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "owned_cold_call", "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tool.String(), "-", ""), "arguments": `{}`}}}}, "finish_reason": "tool_calls"}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
	scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", Wire: &netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{response}}})
	workAppCheck(t, "mcp-cold-model-sequence", err)
	mcpM3ScheduleTask(t, v, "https://fixture.test:8443/case/"+scenario, []id.ToolID{tool})
	// Reuses the same test/database's exact deployment keys and Object spool.
	env := append(guardEnvironment(t, v.root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1), "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s")
	ca, err := os.ReadFile(peer.CAFile())
	workAppCheck(t, "mcp-cold-peer-roots", err)
	modelCA, err := os.ReadFile(network.CAFile)
	workAppCheck(t, "mcp-cold-model-roots", err)
	caFile := filepath.Join(t.TempDir(), "original-peer-roots.pem")
	workAppCheck(t, "mcp-cold-roots-file", os.WriteFile(caFile, append(append(ca, '\n'), modelCA...), 0600))
	env = append(env, "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+caFile)
	taskExecutionJoinEnabled(t, v)
	first := mcpM3RecoveryChild(t, v, peer, env, "hold-put")
	m2RecoveryEvent(t, first, "mcp_put_return_held", nil)
	var materialization, object, original, operation, attempt string
	var exact bool
	err = v.admin.QueryRow(v.ctx, `SELECT m.id,u.object_id,m.process_id::text,m.operation_id,m.attempt_id,
 m.state='pending' AND m.version=1 AND u.state='committed' AND u.disposition='reserved' AND u.creation_cause=m.request_id
 AND u.owner_id=m.id::uuid AND u.owner_kind='mcp_content' AND u.initiator_execution_id=m.execution_id::uuid
 AND a.process_id=m.process_id AND o.state='available'
 AND t.phase='running' AND t.process_id=m.process_id::text AND t.attempt_id=m.attempt_id
 AND EXISTS(SELECT 1 FROM agenteam_object.object_references r WHERE r.object_id=u.object_id AND r.upload_id=u.id AND r.kind='reserved')
 FROM agenteam_mcp.materializations m JOIN agenteam_object.uploads u ON u.owner_id=m.id::uuid
 JOIN agenteam_object.upload_attempts a ON a.id=u.current_attempt_id
 JOIN agenteam_object.objects o ON o.id=u.object_id JOIN agenteam_tool.attempts t ON t.attempt_id=m.attempt_id
 WHERE m.project_id=$1`, v.a.String()).Scan(&materialization, &object, &original, &operation, &attempt, &exact)
	mcpM3QueryCheck(t, "mcp-cold-original-pending-tuple", err)
	if !exact || original != first.process {
		t.Fatal("mcp-stage=cold-pending original Put return was not the held boundary")
	}
	mcpM3RecoveryGuard(t, v, first.process, true)
	mcpM3KillRecovery(t, v, first)
	second := mcpM3RecoveryChild(t, v, peer, env, "recover")
	if second.process == first.process {
		t.Fatal("mcp-stage=cold-process original ProcessGuard was replaced")
	}
	m2RecoveryEvent(t, second, "mcp_recovery_joined", nil)
	var count, references, leases, results, attempts int
	err = v.admin.QueryRow(v.ctx, `SELECT count(*),coalesce(bool_and(m.state='failed' AND m.version=2 AND m.process_id=$2::uuid AND r.original_process_id=m.process_id AND r.recovery_process_id=$3::uuid AND r.phase='complete' AND r.completed_at IS NOT NULL AND o.state='deleted'),false)
 FROM agenteam_mcp.materializations m JOIN agenteam_mcp.content_recoveries r ON r.materialization_id=m.id JOIN agenteam_object.objects o ON o.id=r.object_id WHERE m.id=$1`, materialization, original, second.process).Scan(&count, &exact)
	mcpM3QueryCheck(t, "mcp-cold-exact-recovery", err)
	mcpM3QueryCheck(t, "mcp-cold-no-reference", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$1`, object).Scan(&references))
	mcpM3QueryCheck(t, "mcp-cold-no-lease", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND state='active'`, object).Scan(&leases))
	mcpM3QueryCheck(t, "mcp-cold-no-invented-result", v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_tool.command_results WHERE operation_id=$1),(SELECT count(*) FROM agenteam_tool.attempts WHERE operation_id=$1)`, operation).Scan(&results, &attempts))
	if count != 1 || !exact || references != 0 || leases != 0 || results != 0 || attempts != 1 {
		t.Fatalf("mcp-stage=cold-facts recoveries=%d exact=%t references=%d leases=%d results=%d attempts=%d", count, exact, references, leases, results, attempts)
	}
	var retained string
	mcpM3QueryCheck(t, "mcp-cold-original-attempt", v.admin.QueryRow(v.ctx, `SELECT process_id::text FROM agenteam_tool.attempts WHERE attempt_id=$1`, attempt).Scan(&retained))
	if retained != original {
		t.Fatal("mcp-stage=cold-attempt original dispatch owner changed")
	}
	calls, err := peer.CallCount()
	workAppCheck(t, "mcp-cold-actual-sdk-count", err)
	state, err := network.State(v.ctx, scenario)
	workAppCheck(t, "mcp-cold-actual-model-count", err)
	if calls != 1 || len(state.Requests) != 1 || state.ActiveHandlers != 0 {
		t.Fatal("mcp-stage=cold-no-replay original unknown call was repeated")
	}
	// The MCP recovery owner has actually drained. The separate Execution
	// still has an unproved Tool outcome, so end that child with an exact Wait.
	mcpM3KillRecovery(t, v, second)
	t.Log("MCP_COLD original_put_return_held=true original_sigkill_wait=true new_guard=true exact_materialization_cleanup=true mcp_owner_joined=true unknown_tool_not_redispatched=true app_graceful_join_claimed=false")
}

func mcpM3RecoveryChild(t *testing.T, v *executionSchedulerApp, peer *mcpfixture.Provider, env []string, mode string) *m2RecoveryChild {
	t.Helper()
	executable, err := os.Executable()
	workAppCheck(t, "mcp-cold-executable", err)
	p := &m2RecoveryChild{fixtureProcess: &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestMCPM3ContentRecoveryChild$", "-test.timeout=80s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = append(append([]string{}, env...), "PATH="+os.Getenv("PATH"), pgfixture.Env+"="+os.Getenv(pgfixture.Env), objectfixture.Env+"="+os.Getenv(objectfixture.Env), netfixture.Env+"="+os.Getenv(netfixture.Env), "AGENTEAM_MCP_RECOVERY_CHILD="+mode, "AGENTEAM_MCP_RECOVERY_DATABASE="+v.root.db.Name, "AGENTEAM_MCP_RECOVERY_PEER="+peer.Address())
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	workAppCheck(t, "mcp-cold-child-start", p.cmd.Start())
	t.Logf("MCP_RECOVERY_CHILD_START mode=%s pid=%d", mode, p.cmd.Process.Pid)
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			usageAppWait(t, p.done, "mcp-cold-owned-child-cleanup")
		}
		m2RecoveryChildWaitLog(t, p, "mcp-cleanup")
		// Cmd.Wait has joined the original writer. Emit only this child's
		// explicitly closed diagnostic fields, never its arbitrary stderr.
		for _, event := range p.pending {
			mcpM3RecoveryDiagnostic(t, event)
		}
		for {
			select {
			case event := <-p.stdout.events:
				mcpM3RecoveryDiagnostic(t, event)
			default:
				return
			}
		}
	})
	event := m2RecoveryEvent(t, p, "mcp_recovery_ready", nil)
	p.process, _ = event["process_id"].(string)
	_, err = f.ParseID[oc.Process](p.process)
	workAppCheck(t, "mcp-cold-original-guard-id", err)
	return p
}

func mcpM3RecoveryGuard(t *testing.T, v *executionSchedulerApp, process string, held bool) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(v.root.cfg.Objects().SpoolDirectory()+".processes", process+".claim"), os.O_RDWR, 0)
	workAppCheck(t, "mcp-cold-guard-open", err)
	defer file.Close()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if held {
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			if err == nil {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			}
			t.Fatal("mcp-stage=cold-guard original process not proven live")
		}
		return
	}
	workAppCheck(t, "mcp-cold-guard-released", err)
	workAppCheck(t, "mcp-cold-guard-unlock", syscall.Flock(int(file.Fd()), syscall.LOCK_UN))
}

func mcpM3KillRecovery(t *testing.T, v *executionSchedulerApp, p *m2RecoveryChild) {
	t.Helper()
	workAppCheck(t, "mcp-cold-sigkill", p.cmd.Process.Kill())
	usageAppWait(t, p.done, "mcp-cold-original-wait")
	m2RecoveryChildWaitLog(t, p, "mcp-sigkill")
	var exit *exec.ExitError
	if !errors.As(p.err, &exit) {
		t.Fatal("mcp-stage=cold-wait actual child exit absent")
	}
	status, ok := exit.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatal("mcp-stage=cold-wait original SIGKILL not observed")
	}
	mcpM3RecoveryGuard(t, v, p.process, false)
}

type mcpM3HeldPut struct {
	oc.Objects
	held atomic.Bool
}

func (p *mcpM3HeldPut) PutObject(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, meta f.CommandMeta, media string, size int64, digest *f.Digest, body io.ReadCloser) (oc.PutResult, error) {
	result, err := p.Objects.PutObject(ctx, actor, owner, meta, media, size, digest, body)
	if err == nil && owner.Details().Kind == oc.MCPContent && p.held.CompareAndSwap(false, true) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_put_return_held"})
		select {} // Only the parent's actual SIGKILL and Wait ends this owner.
	}
	return result, err
}

func TestMCPM3ContentRecoveryChild(t *testing.T) {
	mode := os.Getenv("AGENTEAM_MCP_RECOVERY_CHILD")
	if mode == "" {
		return
	}
	if mode != "hold-put" && mode != "recover" {
		t.Fatal("invalid owned MCP child mode")
	}
	fixture, err := pgfixture.Load()
	m2RecoveryCheck(t, "database-fixture", err)
	_, err = fixture.Config(os.Getenv("AGENTEAM_MCP_RECOVERY_DATABASE"), nil)
	m2RecoveryCheck(t, "database-config", err)
	network, err := netfixture.Load()
	m2RecoveryCheck(t, "network-fixture", err)
	peer, err := netip.ParseAddr(os.Getenv("AGENTEAM_MCP_RECOVERY_PEER"))
	m2RecoveryCheck(t, "network-address", err)
	model, err := netip.ParseAddr(network.PrivateIP)
	m2RecoveryCheck(t, "network-address", err)
	if outbound.Classify(peer) != outbound.Private || outbound.Classify(model) != outbound.Private {
		t.Fatal("owned private peers required")
	}
	cfg := guardConfiguration(t, os.Environ())
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	var owned *resources
	var store *postgres.Store
	d := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			var err error
			store, err = postgres.Open(ctx, cfg)
			return store, err
		},
		bind: func(ctx context.Context, cfg config.Config, db database, actual *resources, deps *dependencies) error {
			owned = actual
			return bindAccounts(ctx, cfg, db, actual, deps)
		},
		outboundConstruct: func(ctx context.Context, cfg config.Config, actual *outboundRuntime) error {
			actual.resolver = mcpM3Resolver{"mcp.fixture.test": peer, "fixture.test": model}
			client, err := outbound.NewClientWithProxy(actual.policy, cfg.OutboundTrust(), actual.resolver, actual.routes)
			if err != nil {
				return err
			}
			actual.client.StopAdmission()
			if err = actual.client.Drain(ctx); err != nil {
				return err
			}
			actual.client = client
			return nil
		},
	}
	if mode == "hold-put" {
		d.mcpObjects = func(actual oc.Objects) oc.Objects { return &mcpM3HeldPut{Objects: actual} }
	}
	logs := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, io.MultiWriter(os.Stderr, logs))
	m2RecoveryCheck(t, "logger", err)
	done := make(chan struct{})
	var runErr error
	go func() { runErr = run(context.Background(), cfg, logger, signals, d); close(done) }()
	workRootStartupAddress(t, logs, done, func() error { return runErr })
	objects, ok := owned.objects().(*objectAssembly)
	if !ok || objects == nil {
		m2RecoveryCheck(t, "guard", f.NewFault(f.DependencyUnbound, f.NotStarted))
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_recovery_ready", "process_id": objects.process.String()})
	if mode == "recover" {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		for {
			var complete bool
			err = store.QueryRow(ctx, `SELECT count(*)=1 AND bool_and(phase='complete' AND recovery_process_id=$1::uuid) FROM agenteam_mcp.content_recoveries`, objects.process.String()).Scan(&complete)
			if err != nil {
				mcpM3RecoveryQueryFailure(err)
			}
			m2RecoveryCheck(t, "result-observer", err)
			if complete {
				break
			}
			select {
			case <-ctx.Done():
				m2RecoveryCheck(t, "result-observer", ctx.Err())
			case <-done:
				m2RecoveryCheck(t, "root-return", runErr)
				t.Fatal("MCP recovery App exited early")
			case <-tick.C:
			}
		}
		accounts, ok := owned.accounts().(*accountAssembly)
		if !ok || accounts.mcp == nil {
			m2RecoveryCheck(t, "guard", f.NewFault(f.DependencyUnbound, f.NotStarted))
		}
		accounts.mcp.StopAdmission()
		m2RecoveryCheck(t, "root-return", accounts.mcp.Drain(ctx))
		if !accounts.mcp.Joined() {
			t.Fatal("MCP recovery owner did not join")
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_recovery_joined"})
	}
	<-done
	m2RecoveryCheck(t, "root-return", runErr)
}

func mcpM3RecoveryQueryFailure(err error) {
	class, code, index := "other", "", -1
	var database *postgres.Error
	if errors.As(err, &database) {
		class = "database"
		code = mcpM3RecoveryDatabaseCode(string(database.Code()))
	}
	var scan pgx.ScanArgError
	if errors.As(err, &scan) {
		class, index = "scan", scan.ColumnIndex
	}
	if errors.Is(err, pgx.ErrNoRows) {
		class = "no_rows"
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_recovery_query_failure", "class": class, "database_code": code, "scan_column": index})
}

func mcpM3RecoveryDatabaseCode(raw string) string {
	switch postgres.Code(raw) {
	case postgres.SQLFailed, postgres.ConnectionFailed, postgres.AdmissionStopped,
		postgres.TransactionConcurrentUse, postgres.TransactionRowsOpen, postgres.InvalidTransaction,
		postgres.TransactionExpired, postgres.TransactionControlForbidden:
		return raw
	default:
		return "other"
	}
}

func mcpM3RecoveryDiagnostic(t *testing.T, event map[string]any) {
	t.Helper()
	if event["event"] != "mcp_recovery_query_failure" {
		return
	}
	class, _ := event["class"].(string)
	switch class {
	case "database", "scan", "no_rows", "other":
	default:
		class = "other"
	}
	code, _ := event["database_code"].(string)
	index, ok := event["scan_column"].(float64)
	if !ok || index < 0 || index > 64 || index != float64(int(index)) {
		index = -1
	}
	t.Logf("MCP_COLD_OBSERVER class=%s database_code=%s scan_column=%d", class, mcpM3RecoveryDatabaseCode(code), int(index))
}
