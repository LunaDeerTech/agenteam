//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	mcpfixture "github.com/LunaDeerTech/agenteam/tests/mcp/fixture"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Both catalogs first come from the real SDK. After the original App joins,
// one retained credential is invalid at the peer. The new guarded App must
// refresh the independent valid connection and retain the failed one's complete
// catalog. No fixture inserts a startup request, registration or credential row.
func TestMCPM3StartupReconciliation(t *testing.T) {
	mcpM3ConfigurationPreflight(t)
	peer := mcpfixture.New(t)
	v := newMCPM3App(t, peer, nil)
	base := "/api/v1/projects/" + v.a.String() + "/mcp"
	var original [2]mcp.Connection
	var credentials [2]sc.CredentialID
	for i, name := range []string{"Owned startup valid", "Owned startup rejected"} {
		material, err := peer.Credential()
		workAppCheck(t, "startup-owned-credential", err)
		credentials[i] = mcpM3Secret(t, v, sc.MCP, material)
		var created, connected mcp.CommandResult
		mcpM3HTTP(t, v, "startup-config", http.MethodPost, base+"/configs", map[string]any{
			"expected_version": "0", "input": map[string]any{"name": name, "description": "owned startup catalog",
				"endpoint": peer.Endpoint() + "/protected", "authentication": map[string]string{"scheme": "bearer"}}},
			guardID[f.Request](t).String(), http.StatusOK, &created)
		if created.Config == nil {
			t.Fatal("startup configuration receipt absent")
		}
		mcpM3HTTP(t, v, "connection-connect", http.MethodPost, base+"/connections/connect", map[string]any{
			"config_id": created.Config.ID, "credential_id": credentials[i], "expected_version": "0"},
			guardID[f.Request](t).String(), http.StatusOK, &connected)
		if connected.Connection == nil || connected.Connection.State != "connected" || !connected.Connection.Enabled ||
			connected.Connection.ToolsRevision == 0 || connected.Connection.ResourcesRevision == 0 {
			t.Fatal("startup original authenticated catalog missing")
		}
		original[i] = *connected.Connection
	}
	before := mcpM3StartupCatalog(t, v, original[1].ID.String())
	var changed struct {
		ID      string    `json:"credential_id"`
		Version f.Version `json:"version"`
	}
	mcpM3HTTP(t, v, "startup-credential-rotate", http.MethodPatch, base+"/credentials/"+credentials[1].String(),
		map[string]any{"expected_version": "1", "value": "owned-invalid-startup-bearer"}, guardID[f.Request](t).String(), http.StatusOK, &changed)
	if changed.ID != credentials[1].String() || changed.Version != 2 {
		t.Fatal("startup failed credential did not use the original CAS write")
	}
	mcpM3RecoverySeedJoin(t, v)
	env := append(guardEnvironment(t, v.root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080",
		"AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+peer.CAFile(),
		"AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1),
		"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms",
		"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s")
	child := mcpM3StartupChild(t, v, peer, env)
	m2RecoveryEvent(t, child, "mcp_startup_complete", nil)
	for i, old := range original {
		var version, tools, resources int64
		var errorCode, phase, requestError string
		var complete, exact, retired, released bool
		err := v.admin.QueryRow(v.ctx, `SELECT c.version,c.tools_revision,c.resources_revision,c.last_error_code,
 c.state='connected' AND c.enabled AND c.refresh_id IS NULL AND c.tools_state='ready' AND c.resources_state='ready',
 r.phase,r.error_code,r.io_started AND r.io_retired,r.lease_released,
 r.process_id=$3::uuid AND r.retirement_process_id=r.process_id AND r.project_id=c.project_id
 AND r.config_id=c.config_id AND r.config_revision=c.config_revision AND r.connection_version=c.version
 AND r.result_version=c.version AND r.runtime_binding_id=c.runtime_binding_id AND r.credential_id=c.credential_id
 AND r.credential_id=$4::text AND EXISTS(SELECT 1 FROM agenteam_secret.secret_leases l
 WHERE l.id=r.credential_lease_id::uuid AND l.credential_id=r.credential_id::uuid AND l.consumer='mcp'
 AND l.owner_kind='mcp_connection_request' AND l.owner_id=r.id::uuid AND l.released)
 FROM agenteam_mcp.connections c JOIN agenteam_mcp.startup_requests r ON r.connection_id=c.id
 WHERE c.project_id=$1 AND c.id=$2 AND r.process_id=$3::uuid`, v.a.String(), old.ID.String(), child.process, credentials[i].String()).Scan(
			&version, &tools, &resources, &errorCode, &complete, &phase, &requestError, &retired, &released, &exact)
		mcpM3QueryCheck(t, "startup-exact-original-request", err)
		if version != int64(old.Version)+1 || !complete || phase != "complete" || !retired || !released || !exact || errorCode != requestError {
			t.Fatal("startup refresh lost original current connection/request/lease tuple")
		}
		if i == 0 {
			if errorCode != "" || tools != int64(old.ToolsRevision)+1 || resources != int64(old.ResourcesRevision)+1 {
				t.Fatal("startup independent valid SDK discovery did not publish")
			}
		} else {
			// The provider returns 401 for this explicitly invalid bearer.
			// Only the adapter's existing closed failure vocabulary is accepted.
			switch protocol.Code(errorCode) {
			case protocol.AuthenticationRequired, protocol.ProtocolFailure, protocol.TransportFailure:
			default:
				t.Fatal("startup credential failure lacks a closed protocol error")
			}
			if tools != int64(old.ToolsRevision) || resources != int64(old.ResourcesRevision) || mcpM3StartupCatalog(t, v, old.ID.String()) != before {
				t.Fatal("failed startup refresh replaced the complete retained catalog/Registry")
			}
		}
	}
	var active int
	mcpM3QueryCheck(t, "startup-all-original-leases-retired", v.admin.QueryRow(v.ctx, `SELECT count(*)
 FROM agenteam_mcp.startup_requests r JOIN agenteam_secret.secret_leases l ON l.id=r.credential_lease_id::uuid
 WHERE r.process_id=$1::uuid AND NOT l.released`, child.process).Scan(&active))
	if active != 0 {
		t.Fatal("startup kept an original Secret lease")
	}
	workAppCheck(t, "startup-stop-original-app", child.cmd.Process.Signal(syscall.SIGTERM))
	usageAppWait(t, child.done, "startup-original-app-wait")
	m2RecoveryChildWaitLog(t, child, "mcp-startup-graceful")
	workAppCheck(t, "startup-original-app-return", child.err)
	m2RecoveryEvent(t, child, "mcp_startup_returned", func(event map[string]any) bool { return event["joined"] == true })
	mcpM3RecoveryGuard(t, v, child.process, false)
	t.Log("MCP_STARTUP two_original_enabled_connections=true real_sdk_success=true credential_failure_keeps_complete_catalog=true original_request_leases_retired=true app_joined=true")
}

// The snapshot includes every retained discovery row, complete registration,
// and its referenced immutable definition. It is compared only in memory and
// never printed, not substituted for the Owner's normal directory read.
func mcpM3StartupCatalog(t *testing.T, v *executionSchedulerApp, connection string) string {
	t.Helper()
	var raw string
	err := v.admin.QueryRow(v.ctx, `SELECT jsonb_build_object(
 'tools',COALESCE((SELECT jsonb_agg(to_jsonb(s) ORDER BY revision) FROM agenteam_mcp.tool_snapshots s WHERE connection_id=$1), '[]'::jsonb),
 'resources',COALESCE((SELECT jsonb_agg(to_jsonb(r) ORDER BY r.id) FROM agenteam_mcp.resource_catalog r WHERE connection_id=$1), '[]'::jsonb),
 'registrations',COALESCE((SELECT jsonb_agg(jsonb_build_array(to_jsonb(r),to_jsonb(s)) ORDER BY r.tool_id)
 FROM agenteam_tool.mcp_registrations r JOIN agenteam_tool.spec_revisions s ON s.tool_id=r.tool_id AND s.spec_revision=r.spec_revision
 WHERE r.connection_id=$1), '[]'::jsonb))::text`, connection).Scan(&raw)
	mcpM3QueryCheck(t, "startup-retained-catalog", err)
	return raw
}

func mcpM3StartupChild(t *testing.T, v *executionSchedulerApp, peer *mcpfixture.Provider, env []string) *m2RecoveryChild {
	t.Helper()
	executable, err := os.Executable()
	workAppCheck(t, "startup-test-executable", err)
	p := &m2RecoveryChild{fixtureProcess: &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestMCPM3StartupChild$", "-test.timeout=40s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = append(append([]string{}, env...), "PATH="+os.Getenv("PATH"), pgfixture.Env+"="+os.Getenv(pgfixture.Env),
		objectfixture.Env+"="+os.Getenv(objectfixture.Env), netfixture.Env+"="+os.Getenv(netfixture.Env),
		"AGENTEAM_MCP_STARTUP_CHILD=1", "AGENTEAM_MCP_STARTUP_DATABASE="+v.root.db.Name, "AGENTEAM_MCP_STARTUP_PEER="+peer.Address())
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	workAppCheck(t, "startup-child-start", p.cmd.Start())
	t.Logf("MCP_STARTUP_CHILD_START pid=%d", p.cmd.Process.Pid)
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			usageAppWait(t, p.done, "startup-owned-child-cleanup")
		}
		m2RecoveryChildWaitLog(t, p, "mcp-startup-cleanup")
		for _, event := range p.pending {
			mcpM3RecoveryDiagnostic(t, event)
			mcpM3StartupDiagnostic(t, event)
		}
		for {
			select {
			case event := <-p.stdout.events:
				mcpM3RecoveryDiagnostic(t, event)
				mcpM3StartupDiagnostic(t, event)
			default:
				return
			}
		}
	})
	event := m2RecoveryEvent(t, p, "mcp_startup_ready", nil)
	p.process, _ = event["process_id"].(string)
	_, err = f.ParseID[oc.Process](p.process)
	workAppCheck(t, "startup-original-guard-id", err)
	return p
}

func TestMCPM3StartupChild(t *testing.T) {
	if os.Getenv("AGENTEAM_MCP_STARTUP_CHILD") != "1" {
		return
	}
	fixture, err := pgfixture.Load()
	m2RecoveryCheck(t, "database-fixture", err)
	_, err = fixture.Config(os.Getenv("AGENTEAM_MCP_STARTUP_DATABASE"), nil)
	m2RecoveryCheck(t, "database-config", err)
	peer, err := netip.ParseAddr(os.Getenv("AGENTEAM_MCP_STARTUP_PEER"))
	m2RecoveryCheck(t, "network-address", err)
	if outbound.Classify(peer) != outbound.Private {
		t.Fatal("owned private startup peer required")
	}
	cfg := guardConfiguration(t, os.Environ())
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(signals)
	var owned *resources
	var store *mcpOAuthColdStore
	d := dependencies{
		open: func(ctx context.Context, cfg postgres.Config) (database, error) {
			actual, err := postgres.Open(ctx, cfg)
			if err != nil {
				return nil, err
			}
			store = &mcpOAuthColdStore{Store: actual, mode: "startup"}
			return store, nil
		},
		bind: func(ctx context.Context, cfg config.Config, db database, actual *resources, deps *dependencies) error {
			owned = actual
			return bindAccounts(ctx, cfg, db, actual, deps)
		},
		outboundConstruct: func(ctx context.Context, cfg config.Config, actual *outboundRuntime) error {
			actual.resolver = mcpM3Resolver{"mcp.fixture.test": peer}
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
	logs := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, io.MultiWriter(os.Stderr, logs))
	m2RecoveryCheck(t, "logger", err)
	done := make(chan struct{})
	var runErr error
	go func() { runErr = run(context.Background(), cfg, logger, signals, d); close(done) }()
	_ = workRootStartupAddress(t, logs, done, func() error { return runErr })
	objects, ok := owned.objects().(*objectAssembly)
	if !ok || objects == nil || store == nil {
		t.Fatal("startup original App Guard missing")
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_startup_ready", "process_id": objects.process.String()})
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	var previous [7]int64
	sampled := false
	for {
		var complete bool
		var phase [7]int64
		// Keep the original acceptance predicate. Cache only bounded counters
		// while this context is live; timeout reporting starts no new query.
		err = store.QueryRow(ctx, `SELECT count(*)=2 AND COALESCE(bool_and(phase='complete' AND io_started AND io_retired AND lease_released),false),
 count(*),count(*) FILTER(WHERE phase='pending'),count(*) FILTER(WHERE phase='complete'),
 count(*) FILTER(WHERE phase='abandoned'),count(*) FILTER(WHERE io_started),
 count(*) FILTER(WHERE io_retired),count(*) FILTER(WHERE lease_released)
 FROM agenteam_mcp.startup_requests WHERE process_id=$1::uuid`, objects.process.String()).Scan(
			&complete, &phase[0], &phase[1], &phase[2], &phase[3], &phase[4], &phase[5], &phase[6])
		if err != nil {
			mcpM3StartupPhase(previous, sampled, true)
			mcpM3RecoveryQueryFailure(err)
		}
		m2RecoveryCheck(t, "result-observer", err)
		if (!sampled || phase != previous) && store.reports.Add(1) <= 64 {
			mcpM3StartupPhase(phase, true, false)
		}
		previous, sampled = phase, true
		if complete {
			break
		}
		select {
		case <-ctx.Done():
			mcpM3StartupPhase(previous, sampled, true)
			m2RecoveryCheck(t, "result-observer", ctx.Err())
		case <-done:
			mcpM3StartupPhase(previous, sampled, true)
			m2RecoveryCheck(t, "root-return", runErr)
			t.Fatal("startup App exited before both refreshes joined")
		case <-tick.C:
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_startup_complete"})
	<-done
	m2RecoveryCheck(t, "root-return", runErr)
	joined := owned.producersJoined()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_startup_returned", "joined": joined})
	if !joined {
		t.Fatal("startup App returned before actual owners Joined")
	}
}

func mcpM3StartupPhase(phase [7]int64, sampled, last bool) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_startup_phase", "sampled": sampled, "last": last,
		"total": phase[0], "pending": phase[1], "complete": phase[2], "abandoned": phase[3],
		"io_started": phase[4], "io_retired": phase[5], "lease_released": phase[6]})
}

func mcpM3StartupDiagnostic(t *testing.T, event map[string]any) {
	t.Helper()
	mcpOAuthColdDiagnostic(t, event)
	if event["event"] != "mcp_startup_phase" {
		return
	}
	var counts [7]int
	for i, name := range []string{"total", "pending", "complete", "abandoned", "io_started", "io_retired", "lease_released"} {
		value, ok := event[name].(float64)
		if !ok || value < 0 || value > 2 || value != float64(int(value)) {
			return
		}
		counts[i] = int(value)
	}
	sampled, _ := event["sampled"].(bool)
	last, _ := event["last"].(bool)
	t.Logf("MCP_STARTUP_PHASE sampled=%t last=%t total=%d pending=%d complete=%d abandoned=%d io_started=%d io_retired=%d lease_released=%d",
		sampled, last, counts[0], counts[1], counts[2], counts[3], counts[4], counts[5], counts[6])
}
