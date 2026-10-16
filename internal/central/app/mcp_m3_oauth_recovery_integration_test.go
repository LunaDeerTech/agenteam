//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	mcpfixture "github.com/LunaDeerTech/agenteam/tests/mcp/fixture"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const mcpOAuthColdOrigin = "https://app.fixture.test"

// The first cold window stops after the original retirement identity commits,
// before Secret prepares/applies that Delete. The authorization server has
// already returned its one real code exchange. Every business row is produced
// by the original App; only SIGKILL+Wait ends the held original return.
func TestMCPM3OAuthColdOriginalDelete(t *testing.T) {
	mcpM3OAuthCold(t, false)
}

// This second window covers a real token publication, including its original
// follow-on connection request, before the SDK call can acquire that lease.
func TestMCPM3OAuthColdPublishedConnection(t *testing.T) {
	mcpM3OAuthCold(t, true)
}

// Preserve the original owner/Guard return gates before observing PostgreSQL's
// asynchronous backend departure. A one-shot pg_stat_activity read may still
// see the just-closed connection; neither elapsed time nor Wait alone passes
// this gate. The observation stays within the original fixture context.
func mcpM3RecoverySeedJoin(t *testing.T, v *executionSchedulerApp) {
	t.Helper()
	v.stopBeforeRelease(t)
	v.store.releaseDirectory()
	usageAppWait(t, v.directory.returned, "mcp-seed-manager-original-return")
	usageAppWait(t, v.root.done, "mcp-seed-root-original-return")
	workAppCheck(t, "mcp-seed-original-root", v.root.err)
	if !v.runtime.Joined() || !v.runtime.executionCalls.Joined() || !v.runtime.schedulerCalls.Joined() || !v.planning.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("MCP seed App lost its original owners")
	}
	workAppCheck(t, "mcp-seed-original-guard-release", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	workAppCheck(t, "mcp-seed-guard-observer-unlock", syscall.Flock(int(v.claim.Fd()), syscall.LOCK_UN))
	ctx, cancel := context.WithTimeout(v.ctx, 2*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var stopped bool
	var connections, observations int
	for {
		err := v.admin.QueryRow(ctx, `SELECT coalesce((SELECT state='stopped' AND stopped_at IS NOT NULL
 FROM agenteam_object.process_claims WHERE process_id=$1::uuid),false),
 (SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND application_name='agenteam')`, v.objects.process.String()).Scan(&stopped, &connections)
		if err != nil {
			t.Logf("MCP_SEED_EXIT stopped=%t connections=%d observations=%d observation_failed=true", stopped, connections, observations)
			mcpM3QueryCheck(t, "mcp-seed-exit-facts", err)
		}
		observations++
		if observations == 1 || stopped && connections == 0 {
			t.Logf("MCP_SEED_EXIT stopped=%t connections=%d observations=%d", stopped, connections, observations)
		}
		if stopped && connections == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("MCP seed exit facts unconfirmed: stopped=%t connections=%d observations=%d", stopped, connections, observations)
		case <-tick.C:
		}
	}
}

func mcpM3OAuthCold(t *testing.T, published bool) {
	mcpM3ConfigurationPreflight(t)
	source, err := postgres.EmbeddedSource()
	workAppCheck(t, "oauth-cold-source", err)
	if source.Target() < 142 {
		t.Fatal("OAuth cold cleanup requires the continuous source through 142")
	}
	peer := mcpfixture.New(t)
	v := newMCPM3AppWithOrigin(t, peer, nil, mcpOAuthColdOrigin)
	scope, _ := id.InProject(v.a)
	key := f.IdempotencyKey(guardID[f.Request](t).String())
	identity, err := mcp.CommandIdentity(v.actor, scope, "config.create", key)
	workAppCheck(t, "oauth-cold-config-command", err)
	created, err := v.runtime.mcp.CreateConfig(v.ctx, mcp.Command{Actor: v.actor, Scope: scope, Identity: identity},
		mcp.ConfigInput{Name: "owned-cold-oauth", Endpoint: peer.OAuthEndpoint(), Authentication: protocol.Authentication{Scheme: "oauth"}})
	workAppCheck(t, "oauth-cold-config", err)
	if created.Config == nil {
		t.Fatal("OAuth cold config receipt absent")
	}
	// Provisioning is fully joined before either original child owns a Guard.
	mcpM3RecoverySeedJoin(t, v)
	env := append(guardEnvironment(t, v.root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+mcpOAuthColdOrigin,
		"AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+peer.CAFile(),
		"AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1),
		"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms",
		"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s")
	mode, heldEvent := "hold-delete", "mcp_oauth_delete_return_held"
	exchangePhase, deletePhase, recoveredPhase := "claimed", "claimed", "unknown"
	if published {
		mode, heldEvent = "hold-publication", "mcp_oauth_publication_return_held"
		exchangePhase, deletePhase, recoveredPhase = "complete", "complete", "complete"
	}
	first, address := mcpM3OAuthColdChild(t, v, peer, env, mode)
	beginKey := guardID[f.Request](t).String()
	beginBody := workAppJSON(t, map[string]any{"config_id": created.Config.ID, "expected_version": "0"})
	started, err := mcpM3OAuthColdRequest(v.ctx, v.client, address, v.cookie, v.csrf,
		"/api/v1/projects/"+v.a.String()+"/mcp/oauth/begin", beginKey, beginBody)
	workAppCheck(t, "oauth-cold-begin", err)
	if started.OAuth == nil || started.Connection == nil || started.Connection.State != "auth_required" {
		t.Fatal("OAuth cold original ready flow absent")
	}
	callback, err := peer.AuthorizeOAuth(v.ctx, started.OAuth.AuthorizationURL)
	workAppCheck(t, "oauth-cold-authorize", err)
	state, code, issuer := callback.Fields()
	completeBody := workAppJSON(t, map[string]any{"state": state, "code": code, "issuer": issuer})
	defer clear(completeBody)
	completeKey := guardID[f.Request](t).String()
	requestCtx, cancel := context.WithTimeout(v.ctx, 20*time.Second)
	defer cancel()
	returned := make(chan error, 1)
	httpDone := make(chan struct{})
	originalBody := append([]byte(nil), completeBody...)
	t.Cleanup(func() {
		cancel()
		usageAppWait(t, httpDone, "oauth-cold-original-http-cleanup")
		select {
		case returnedErr := <-returned:
			var fault *f.Fault
			code, state := "", ""
			if errors.As(returnedErr, &fault) {
				code, state = string(fault.Code.Safe()), string(fault.CommitState.Safe())
			}
			t.Logf("MCP_OAUTH_COMPLETE_RETURN failed=%t code=%s state=%s", returnedErr != nil, code, state)
		default:
		}
	})
	go func() {
		defer close(httpDone)
		defer clear(originalBody)
		_, err := mcpM3OAuthColdRequest(requestCtx, v.client, address, v.cookie, v.csrf,
			"/api/v1/mcp/oauth/complete", completeKey, originalBody)
		returned <- err
	}()
	m2RecoveryEvent(t, first, heldEvent, nil)
	var exchange, deletion, credential, original, binding string
	var exact bool
	err = v.admin.QueryRow(v.ctx, `SELECT e.id,d.id,e.credential_id,e.process_id::text,d.secret_write_binding,
 e.phase=$2 AND e.io_retired AND e.lease_released AND d.phase=$3 AND d.io_retired AND d.lease_released
 AND e.process_id=d.process_id AND d.command_digest=e.command_digest AND d.config_id=e.config_id
 AND d.config_revision=e.config_revision AND d.connection_id=e.connection_id AND d.connection_version=e.connection_version
 AND d.user_id=e.user_id AND d.session_id=e.session_id AND d.credential_version=e.credential_version
 AND (($4 AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secrets s WHERE s.id=e.credential_id::uuid))
 OR (NOT $4 AND EXISTS(SELECT 1 FROM agenteam_secret.secrets s WHERE s.id=e.credential_id::uuid
 AND s.version=e.credential_version AND s.purpose='mcp' AND s.scope='project' AND s.project_id=e.project_id::uuid)))
 AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_leases l WHERE l.credential_id=e.credential_id::uuid AND NOT l.released)
 FROM agenteam_mcp.oauth_requests e JOIN agenteam_mcp.oauth_requests d ON d.flow_id=e.flow_id AND d.operation='retire'
 WHERE e.project_id=$1 AND e.operation='exchange'`, v.a.String(), exchangePhase, deletePhase, published).Scan(&exchange, &deletion, &credential, &original, &binding, &exact)
	mcpM3QueryCheck(t, "oauth-cold-original-delete", err)
	if !exact || original != first.process || f.Digest(binding).Validate() != nil {
		t.Fatal("OAuth cold held return did not match its complete original tuple")
	}
	var connectionRequest, connectionLease string
	if published {
		err = v.admin.QueryRow(v.ctx, `SELECT r.id,r.credential_lease_id,
 r.project_id=e.project_id AND r.config_id=e.config_id AND r.config_revision=e.config_revision
 AND r.connection_id=e.connection_id AND r.connection_version=e.connection_version+1
 AND r.user_id=e.user_id AND r.session_id=e.session_id AND r.credential_id=e.result_credential_id
 AND r.phase='pending' AND NOT r.io_retired AND NOT r.lease_released
 AND c.project_id=r.project_id AND c.config_id=r.config_id AND c.config_revision=r.config_revision
 AND c.version=r.connection_version AND c.refresh_id=r.id AND c.credential_id=r.credential_id
 AND c.state='connecting' AND NOT c.enabled AND s.version=e.result_credential_version AND s.purpose='mcp'
 AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_leases l WHERE l.id=r.credential_lease_id::uuid)
 FROM agenteam_mcp.oauth_requests e
 JOIN agenteam_mcp.connection_requests r ON r.command_digest=e.command_digest
 JOIN agenteam_mcp.connections c ON c.id=r.connection_id
 JOIN agenteam_secret.secrets s ON s.id=r.credential_id::uuid
 WHERE e.id=$1 AND e.phase='complete'`, exchange).Scan(&connectionRequest, &connectionLease, &exact)
		mcpM3QueryCheck(t, "oauth-cold-original-publication", err)
		if !exact {
			t.Fatal("OAuth cold publication did not retain its original pending SDK tuple")
		}
	}
	before, err := peer.OAuthState(v.ctx)
	workAppCheck(t, "oauth-cold-original-provider", err)
	if before.Registrations != 1 || before.Exchanges != 1 || before.Refreshes != 0 || before.Accepted != 0 {
		t.Fatal("OAuth cold boundary did not follow one actual code exchange")
	}
	mcpM3RecoveryGuard(t, v, first.process, true)
	mcpM3KillRecovery(t, v, first)
	select {
	case err = <-returned:
		if err == nil {
			t.Fatal("held original Complete unexpectedly returned success")
		}
	case <-requestCtx.Done():
		t.Fatal("original OAuth HTTP caller did not join after SIGKILL")
	}
	second, recoveredAddress := mcpM3OAuthColdChild(t, v, peer, env, "recover")
	if second.process == first.process {
		t.Fatal("OAuth cold recovery reused the original process")
	}
	m2RecoveryEvent(t, second, "mcp_oauth_cleanup_complete", nil)
	var claims, cleanups, deletes, receipts, liveSecrets, liveLeases int
	err = v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_mcp.oauth_recovery_claims WHERE request_id=$1 AND original_process_id=$2::uuid AND process_id=$3::uuid AND phase='complete'),
 (SELECT count(*) FROM agenteam_mcp.oauth_flow_cleanups WHERE source_request_id=$1 AND delete_request_id=$4 AND source_process_id=$2::uuid AND delete_process_id=$2::uuid AND credential_id=$5::text AND secret_write_binding=$6),
 (SELECT count(*) FROM agenteam_mcp.oauth_requests WHERE project_id=$7 AND operation='retire'),
 (SELECT count(*) FROM agenteam_secret.secret_command_receipts WHERE credential_id=$5::uuid AND purpose='mcp' AND mutation_kind='delete' AND deleted),
 (SELECT count(*) FROM agenteam_secret.secrets WHERE id=$5::uuid),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$5::uuid AND NOT released),
 EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests e JOIN agenteam_mcp.oauth_requests d ON d.id=$4
 WHERE e.id=$1 AND e.process_id=$2::uuid AND e.phase=$8 AND e.io_retired AND e.lease_released
 AND d.process_id=$2::uuid AND d.phase='complete' AND d.result_credential_id=$5::text
 AND d.result_credential_version=d.credential_version+1 AND d.secret_write_binding=$6 AND d.io_retired AND d.lease_released)`,
		exchange, original, second.process, deletion, credential, binding, v.a.String(), recoveredPhase).Scan(&claims, &cleanups, &deletes, &receipts, &liveSecrets, &liveLeases, &exact)
	mcpM3QueryCheck(t, "oauth-cold-canonical-cleanup", err)
	if claims != 1 || cleanups != 1 || deletes != 1 || receipts != 1 || liveSecrets != 0 || liveLeases != 0 || !exact {
		t.Fatalf("OAuth cold facts claims=%d cleanup=%d deletes=%d receipts=%d live_secrets=%d live_leases=%d exact=%t", claims, cleanups, deletes, receipts, liveSecrets, liveLeases, exact)
	}
	if published {
		err = v.admin.QueryRow(v.ctx, `SELECT r.phase='abandoned' AND r.io_retired AND r.lease_released
 AND c.refresh_id IS NULL AND c.version=r.connection_version AND c.credential_id=r.credential_id
 AND c.state='error' AND c.last_error_code='mcp_cancelled' AND NOT c.enabled
 AND r.credential_lease_id=$2::text AND s.version=e.result_credential_version
 AND s.purpose='mcp' AND s.scope='project' AND s.project_id=e.project_id::uuid
 AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_leases l WHERE l.id=$2::uuid AND NOT l.released)
 FROM agenteam_mcp.connection_requests r
 JOIN agenteam_mcp.connections c ON c.id=r.connection_id
 JOIN agenteam_mcp.oauth_requests e ON e.id=$3 AND e.command_digest=r.command_digest
 JOIN agenteam_secret.secrets s ON s.id=r.credential_id::uuid
 WHERE r.id=$1`, connectionRequest, connectionLease, exchange).Scan(&exact)
		mcpM3QueryCheck(t, "oauth-cold-connection-detached", err)
		if !exact {
			t.Fatal("OAuth cold SDK lease or original busy pointer remained live")
		}
	}
	deleteKey, _ := f.ParseIdempotencyKey(deletion)
	deleteCommand, err := f.NewCommandIdentity("secret.mcp", []string{v.a.String(), started.Connection.ID.String(), started.OAuth.FlowID}, "delete", deleteKey)
	workAppCheck(t, "oauth-cold-original-delete-identity", err)
	deleteDigest, err := cursor.Digest([]byte(deleteCommand.Canonical()))
	workAppCheck(t, "oauth-cold-original-delete-digest", err)
	var audits int
	err = v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_audit.audit_records WHERE scope='project' AND project_id=$1::uuid
 AND producer='secret' AND action='secret.delete' AND outcome='success' AND resource_kind='secret' AND resource_id=$2::uuid
 AND actor_kind='service' AND service_name='secret' AND service_cause=$3 AND cause_ref=$4 AND ordinal=0`,
		v.a.String(), credential, deletion, string(deleteDigest)).Scan(&audits)
	mcpM3QueryCheck(t, "oauth-cold-original-delete-audit", err)
	if audits != 1 {
		t.Fatal("OAuth cold original Delete did not append exactly its private canonical audit")
	}
	var stopped struct {
		State  string          `json:"state"`
		Result json.RawMessage `json:"result"`
	}
	err = mcpM3OAuthColdDecode(v.ctx, v.client, recoveredAddress, v.cookie, v.csrf,
		"/api/v1/projects/"+v.a.String()+"/mcp/commands/lookup", completeKey,
		workAppJSON(t, map[string]string{"command": "oauth.complete"}), &stopped)
	workAppCheck(t, "oauth-cold-original-session-stopped", err)
	if stopped.State != "stopped" || len(stopped.Result) != 0 {
		t.Fatal("retired original command did not expose its distinct stopped observation")
	}
	err = v.admin.QueryRow(v.ctx, `SELECT c.phase='pending' AND c.response IS NULL
 FROM agenteam_mcp.commands c JOIN agenteam_mcp.oauth_requests e ON e.command_digest=c.command_digest
 WHERE e.id=$1`, exchange).Scan(&exact)
	mcpM3QueryCheck(t, "oauth-cold-preserved-unknown-command", err)
	if !exact {
		t.Fatal("cold observation fabricated an original remote outcome")
	}
	after, err := peer.OAuthState(v.ctx)
	workAppCheck(t, "oauth-cold-no-reexchange", err)
	if after != before {
		t.Fatal("OAuth cold cleanup repeated issuer/SDK I/O")
	}
	workAppCheck(t, "oauth-cold-stop-recovered-app", second.cmd.Process.Signal(syscall.SIGTERM))
	usageAppWait(t, second.done, "oauth-cold-recovered-original-wait")
	m2RecoveryChildWaitLog(t, second, "oauth-cold-graceful")
	workAppCheck(t, "oauth-cold-recovered-app", second.err)
	m2RecoveryEvent(t, second, "mcp_oauth_cold_returned", func(event map[string]any) bool { return event["joined"] == true })
	mcpM3RecoveryGuard(t, v, second.process, false)
	t.Logf("MCP_OAUTH_COLD window=%s actual_exchange_once=true original_delete_key=true stopped_guard=true no_secret_material_read=true original_process_retained=true recovered_app_joined=true", mode)
}

// This is the owned loopback App transport used by the domain/HTTP fixtures;
// the externally configured Origin, Host, Session cookie and CSRF remain the
// actual HTTPS contract. It is not a browser TLS acceptance test.
func mcpM3OAuthColdRequest(ctx context.Context, client *http.Client, address, cookie, csrf, path, key string, body []byte) (mcp.CommandResult, error) {
	var result mcp.CommandResult
	err := mcpM3OAuthColdDecode(ctx, client, address, cookie, csrf, path, key, body, &result)
	return result, err
}

func mcpM3OAuthColdDecode(ctx context.Context, client *http.Client, address, cookie, csrf, path, key string, body []byte, result any) error {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, address+path, bytes.NewReader(body))
	if err != nil {
		return f.NewFault(f.InvalidArgument, f.NotStarted)
	}
	r.Host = "app.fixture.test"
	r.Header.Set("Origin", mcpOAuthColdOrigin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Idempotency-Key", key)
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: cookie})
	response, err := client.Do(r)
	if err != nil {
		return f.NewFault(f.DependencyUnavailable, f.Unknown)
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, 131073))
	closeErr := response.Body.Close()
	defer clear(raw)
	if readErr != nil || closeErr != nil || len(raw) > 131072 {
		return f.NewFault(f.DependencyUnavailable, f.Unknown)
	}
	if response.StatusCode != http.StatusOK {
		var problem struct {
			Code f.Code `json:"code"`
		}
		if json.Unmarshal(raw, &problem) != nil || !problem.Code.Known() {
			problem.Code = f.InternalError
		}
		return f.NewFault(problem.Code, f.NotStarted)
	}
	if json.Unmarshal(raw, result) != nil {
		return f.NewFault(f.InvalidState, f.NotStarted)
	}
	return nil
}

func mcpM3OAuthColdChild(t *testing.T, v *executionSchedulerApp, peer *mcpfixture.Provider, env []string, mode string) (*m2RecoveryChild, string) {
	t.Helper()
	executable, err := os.Executable()
	workAppCheck(t, "oauth-cold-executable", err)
	p := &m2RecoveryChild{fixtureProcess: &fixtureProcess{cmd: exec.Command(executable, "-test.run=^TestMCPM3OAuthRecoveryChild$", "-test.timeout=70s"), stdout: newEventLog(), stderr: newEventLog(), done: make(chan struct{})}}
	p.cmd.Dir = t.TempDir()
	p.cmd.Env = append(append([]string{}, env...), "PATH="+os.Getenv("PATH"), pgfixture.Env+"="+os.Getenv(pgfixture.Env), objectfixture.Env+"="+os.Getenv(objectfixture.Env), netfixture.Env+"="+os.Getenv(netfixture.Env),
		"AGENTEAM_MCP_OAUTH_COLD_CHILD="+mode, "AGENTEAM_MCP_RECOVERY_DATABASE="+v.root.db.Name, "AGENTEAM_MCP_RECOVERY_PEER="+peer.Address())
	p.cmd.Stdout, p.cmd.Stderr = p.stdout, p.stderr
	workAppCheck(t, "oauth-cold-child-start", p.cmd.Start())
	t.Logf("MCP_OAUTH_COLD_CHILD_START mode=%s pid=%d", mode, p.cmd.Process.Pid)
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			usageAppWait(t, p.done, "oauth-cold-owned-child-cleanup")
		}
		m2RecoveryChildWaitLog(t, p, "oauth-cold-cleanup")
		for _, event := range p.pending {
			mcpM3RecoveryDiagnostic(t, event)
			mcpOAuthColdDiagnostic(t, event)
		}
		for {
			select {
			case event := <-p.stdout.events:
				mcpM3RecoveryDiagnostic(t, event)
				mcpOAuthColdDiagnostic(t, event)
			default:
				return
			}
		}
	})
	event := m2RecoveryEvent(t, p, "mcp_oauth_cold_ready", nil)
	p.process, _ = event["process_id"].(string)
	_, err = f.ParseID[oc.Process](p.process)
	workAppCheck(t, "oauth-cold-guard-id", err)
	address, _ := event["address"].(string)
	if !strings.HasPrefix(address, "http://127.0.0.1:") {
		t.Fatal("OAuth child did not return its owned loopback App listener")
	}
	return p, address
}

type mcpOAuthColdStore struct {
	*postgres.Store
	mode    string
	held    atomic.Bool
	process atomic.Value
	reports atomic.Uint32
}

func (s *mcpOAuthColdStore) WithinTx(ctx context.Context, cause f.TransactionCause, body func(context.Context, f.Tx) error) f.CommitResult {
	d := cause.Details()
	callback := body
	var callbackError error
	stage := ""
	if s.mode == "hold-publication" && d.Kind == f.CommandsCause && d.Primary.Command() == "oauth.complete" {
		stage = "publication-transaction"
	} else if (s.mode == "recover" || s.mode == "startup" || s.mode == "hold-human-refresh" || s.mode == "recover-human-refresh") && d.Kind == f.RecoveryCause {
		stage = mcpOAuthColdStage(d.Owner)
	}
	if stage != "" {
		callback = func(ctx context.Context, tx f.Tx) error {
			callbackError = body(ctx, tx)
			return callbackError
		}
	}
	result := s.Store.WithinTx(ctx, cause, callback)
	if stage != "" {
		observed := callbackError
		if fault := result.Fault(); fault != nil {
			observed = fault
		}
		s.report(stage, result.State(), observed)
	}
	if result.State() != f.Committed || s.held.Load() {
		return result
	}
	process, _ := s.process.Load().(string)
	if s.mode == "hold-human-refresh" && d.Kind == f.RecoveryCause && d.Owner == "mcp.oauth.refresh_publish" {
		s.holdHumanRefresh(ctx, d.RecoveryRunID, process)
		return result
	}
	if s.mode == "hold-publication" && d.Kind == f.CommandsCause && d.Primary.Command() == "oauth.complete" {
		// MCP persists the digest of CommandIdentity's original canonical
		// bytes. Cursor's JSON canonicalizer reorders keys and is a different
		// digest contract; it must not be used to locate this original command.
		sum := sha256.Sum256([]byte(d.Primary.Canonical()))
		command := "sha256:" + hex.EncodeToString(sum[:])
		var exact bool
		err := s.Store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests e
 JOIN agenteam_mcp.connection_requests r ON r.command_digest=e.command_digest
 JOIN agenteam_mcp.connections c ON c.id=r.connection_id
 WHERE e.command_digest=$1 AND e.process_id=$2::uuid AND e.operation='exchange' AND e.phase='complete'
 AND e.io_retired AND e.lease_released AND r.project_id=e.project_id AND r.config_id=e.config_id
 AND r.config_revision=e.config_revision AND r.connection_id=e.connection_id
 AND r.connection_version=e.connection_version+1 AND r.user_id=e.user_id AND r.session_id=e.session_id
 AND r.credential_id=e.result_credential_id AND r.phase='pending' AND NOT r.io_retired AND NOT r.lease_released
 AND c.project_id=r.project_id AND c.config_id=r.config_id AND c.config_revision=r.config_revision
 AND c.version=r.connection_version AND c.refresh_id=r.id AND c.credential_id=r.credential_id)`, command, process).Scan(&exact)
		if err != nil {
			mcpM3RecoveryQueryFailure(err)
			return result
		}
		if s.reports.Add(1) <= 64 {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_oauth_publication_gate", "exact": exact})
		}
		if exact && s.held.CompareAndSwap(false, true) {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_oauth_publication_return_held"})
			select {} // The original committed publication is held until SIGKILL/Wait.
		}
		return result
	}
	if s.mode != "hold-delete" || d.Kind != f.RecoveryCause || d.Owner != "mcp.oauth.retire" {
		return result
	}
	var exact bool
	err := s.Store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests d
 JOIN agenteam_mcp.oauth_requests e ON e.flow_id=d.flow_id AND e.operation='exchange'
 WHERE d.id=$1 AND d.operation='retire' AND d.phase='claimed' AND d.process_id=$2::uuid
 AND d.process_required AND d.secret_write_binding IS NOT NULL AND d.io_retired AND d.lease_released
 AND e.process_id=d.process_id AND e.phase='claimed' AND e.io_retired AND e.lease_released
 AND d.credential_id=e.credential_id AND d.credential_version=e.credential_version)`, d.RecoveryRunID, process).Scan(&exact)
	if err != nil {
		mcpM3RecoveryQueryFailure(err)
		return result
	}
	if exact && s.held.CompareAndSwap(false, true) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_oauth_delete_return_held"})
		select {} // Only the parent's original SIGKILL and Wait ends this owner.
	}
	return result
}

// This forwarding observer never changes a callback, result, authority or
// budget. Only the fixed recovery stages and closed error fields leave the
// child; SQL, arguments, request identities and error text are never emitted.
func mcpOAuthColdStage(raw string) string {
	switch raw {
	case "mcp.oauth.recovery_claim", "mcp.oauth.stopped_io", "mcp.oauth.stopped_lease",
		"mcp.oauth.cleanup_select", "mcp.oauth.cleanup_observe", "mcp.oauth.cleanup_apply",
		"mcp.oauth.connection_io", "mcp.oauth.connection_lease", "mcp.oauth.recovery_complete",
		"request-scan", "claim-scan", "candidate-query", "publication-transaction",
		"mcp.startup", "mcp.startup.dispatch", "mcp.startup.secret", "mcp.startup.publish", "mcp.startup.retire",
		"mcp.oauth.refresh", "mcp.oauth.refresh_returned", "mcp.oauth.refresh_publish", "mcp.oauth.refresh_observe",
		"mcp.oauth.refresh_stopped", "mcp.oauth.refresh_stopped_observe",
		"mcp.oauth.refresh_connection_io", "mcp.oauth.refresh_connection_lease":
		return raw
	default:
		return ""
	}
}

func (s *mcpOAuthColdStore) report(stage string, state f.CommitState, err error) {
	observing := s.mode == "recover" || s.mode == "startup" || s.mode == "hold-human-refresh" || s.mode == "recover-human-refresh" ||
		s.mode == "hold-publication" && stage == "publication-transaction"
	if !observing || mcpOAuthColdStage(stage) == "" || s.reports.Add(1) > 64 {
		return
	}
	event := map[string]any{"event": "mcp_oauth_cold_step", "stage": stage,
		"state": state.Safe(), "failed": err != nil,
		"cancelled": errors.Is(err, context.Canceled), "deadline": errors.Is(err, context.DeadlineExceeded)}
	var fault *f.Fault
	if errors.As(err, &fault) {
		event["code"] = fault.Code.Safe()
	}
	var database *postgres.Error
	if errors.As(err, &database) {
		event["database_code"] = mcpM3RecoveryDatabaseCode(string(database.Code()))
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && m2RecoverySQLState(pg.Code) {
		event["sqlstate"] = pg.Code
	}
	_ = json.NewEncoder(os.Stdout).Encode(event)
}

func (s *mcpOAuthColdStore) Query(ctx context.Context, query string, args ...any) (*postgres.Rows, error) {
	rows, err := s.Store.Query(ctx, query, args...)
	if err != nil && strings.Contains(query, "FROM agenteam_mcp.oauth_requests r") {
		s.report("candidate-query", f.NotStarted, err)
	}
	return rows, err
}

func (s *mcpOAuthColdStore) QueryRow(ctx context.Context, query string, args ...any) postgres.Row {
	row := s.Store.QueryRow(ctx, query, args...)
	stage := ""
	if strings.Contains(query, "FROM agenteam_mcp.oauth_requests WHERE id=$1 AND operation IN") {
		stage = "request-scan"
	} else if strings.Contains(query, "FROM agenteam_mcp.oauth_recovery_claims c WHERE request_id=$1") {
		stage = "claim-scan"
	}
	if s.mode != "recover" || stage == "" {
		return row
	}
	return mcpOAuthColdRow{row: row, store: s, stage: stage}
}

type mcpOAuthColdRow struct {
	row   postgres.Row
	store *mcpOAuthColdStore
	stage string
}

func (r mcpOAuthColdRow) Scan(values ...any) error {
	err := r.row.Scan(values...)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		r.store.report(r.stage, f.NotStarted, err)
	}
	return err
}

func mcpOAuthColdDiagnostic(t *testing.T, event map[string]any) {
	t.Helper()
	if event["event"] == "m2_recovery_failure" {
		m2RecoveryLogFailure(t, event)
		return
	}
	if event["event"] == "mcp_oauth_owner_return" {
		accounts, _ := event["accounts_joined"].(bool)
		bound, _ := event["mcp_bound"].(bool)
		mcp, _ := event["mcp_joined"].(bool)
		producers, _ := event["producers_joined"].(bool)
		t.Logf("MCP_OAUTH_OWNER_RETURN accounts_joined=%t mcp_bound=%t mcp_joined=%t producers_joined=%t", accounts, bound, mcp, producers)
		return
	}
	if event["event"] == "mcp_oauth_publication_gate" {
		exact, _ := event["exact"].(bool)
		t.Logf("MCP_OAUTH_PUBLICATION_GATE exact=%t", exact)
		return
	}
	if event["event"] == "mcp_oauth_cold_phase" {
		fields := []string{"complete", "claimed", "exchange", "unknown", "retired", "cleanup", "deleted", "connection_retired"}
		var values [8]bool
		for i, key := range fields {
			values[i], _ = event[key].(bool)
		}
		t.Logf("MCP_OAUTH_COLD_PHASE complete=%t claimed=%t exchange=%t unknown=%t retired=%t cleanup=%t deleted=%t connection_retired=%t",
			values[0], values[1], values[2], values[3], values[4], values[5], values[6], values[7])
		return
	}
	if event["event"] != "mcp_oauth_cold_step" {
		return
	}
	stage, _ := event["stage"].(string)
	stage = mcpOAuthColdStage(stage)
	if stage == "" {
		return
	}
	state, _ := event["state"].(string)
	code, _ := event["code"].(string)
	database, _ := event["database_code"].(string)
	sqlstate, _ := event["sqlstate"].(string)
	if !m2RecoverySQLState(sqlstate) {
		sqlstate = ""
	}
	failed, _ := event["failed"].(bool)
	cancelled, _ := event["cancelled"].(bool)
	deadline, _ := event["deadline"].(bool)
	t.Logf("MCP_OAUTH_COLD_STEP stage=%s state=%s failed=%t fault_present=%t code=%s database_code=%s sqlstate=%s cancelled=%t deadline=%t",
		stage, f.CommitState(state).Safe(), failed, code != "", f.Code(code).Safe(), mcpM3RecoveryDatabaseCode(database), sqlstate, cancelled, deadline)
}

// Observe only after the original run returns. These booleans never change its
// error or substitute for the parent's original Wait/Joined/Guard assertions.
func mcpOAuthColdReturnDiagnostic(owned *resources) {
	accounts := owned.accounts()
	mcpBound, mcpJoined := false, false
	if a, ok := accounts.(*accountAssembly); ok && a != nil {
		a.mu.Lock()
		service := a.mcp
		a.mu.Unlock()
		if service != nil {
			mcpBound, mcpJoined = true, service.Joined()
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_oauth_owner_return",
		"accounts_joined": accounts != nil && accounts.Joined(), "mcp_bound": mcpBound,
		"mcp_joined": mcpJoined, "producers_joined": owned.producersJoined()})
}

func TestMCPM3OAuthRecoveryChild(t *testing.T) {
	mode := os.Getenv("AGENTEAM_MCP_OAUTH_COLD_CHILD")
	if mode == "" {
		return
	}
	if mode != "hold-delete" && mode != "hold-publication" && mode != "recover" && mode != "hold-human-refresh" && mode != "recover-human-refresh" {
		t.Fatal("unknown owned OAuth recovery mode")
	}
	fixture, err := pgfixture.Load()
	m2RecoveryCheck(t, "database-fixture", err)
	_, err = fixture.Config(os.Getenv("AGENTEAM_MCP_RECOVERY_DATABASE"), nil)
	m2RecoveryCheck(t, "database-config", err)
	peer, err := netip.ParseAddr(os.Getenv("AGENTEAM_MCP_RECOVERY_PEER"))
	m2RecoveryCheck(t, "network-address", err)
	if outbound.Classify(peer) != outbound.Private {
		t.Fatal("owned private OAuth peer required")
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
			store = &mcpOAuthColdStore{Store: actual, mode: mode}
			return store, nil
		},
		bind: func(ctx context.Context, cfg config.Config, db database, actual *resources, deps *dependencies) error {
			owned = actual
			return bindAccounts(ctx, cfg, db, actual, deps)
		},
		outboundConstruct: func(ctx context.Context, cfg config.Config, actual *outboundRuntime) error {
			actual.resolver = mcpM3Resolver{"mcp.fixture.test": peer}
			client, err := outbound.NewClient(actual.policy, cfg.OutboundTrust(), actual.resolver)
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
	address := workRootStartupAddress(t, logs, done, func() error { return runErr })
	objects, ok := owned.objects().(*objectAssembly)
	if !ok || objects == nil || store == nil {
		t.Fatal("OAuth cold original App Guard missing")
	}
	store.process.Store(objects.process.String())
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_oauth_cold_ready", "process_id": objects.process.String(), "address": address})
	if mode == "recover" {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		tick := time.NewTicker(25 * time.Millisecond)
		defer tick.Stop()
		var previous [8]bool
		sampled := false
		for {
			var phase [8]bool
			// Diagnostic fields extend the original read only. The acceptance
			// condition remains exactly its first complete-claim predicate.
			err = store.QueryRow(ctx, `WITH original AS (
 SELECT id,flow_id,phase,io_retired,lease_released FROM agenteam_mcp.oauth_requests
 WHERE operation='exchange' AND process_id<>$1::uuid)
 SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_recovery_claims c
 JOIN agenteam_mcp.oauth_requests e ON e.id=c.request_id WHERE c.process_id=$1::uuid AND c.phase='complete' AND e.operation='exchange'),
 EXISTS(SELECT 1 FROM agenteam_mcp.oauth_recovery_claims c JOIN original e ON e.id=c.request_id WHERE c.process_id=$1::uuid AND c.phase='claimed'),
 EXISTS(SELECT 1 FROM original), EXISTS(SELECT 1 FROM original WHERE phase='unknown'),
 EXISTS(SELECT 1 FROM original WHERE io_retired AND lease_released),
 EXISTS(SELECT 1 FROM agenteam_mcp.oauth_flow_cleanups c JOIN original e ON e.id=c.source_request_id),
 EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests d JOIN original e ON e.flow_id=d.flow_id WHERE d.operation='retire' AND d.phase='complete'),
 EXISTS(SELECT 1 FROM agenteam_mcp.connection_requests r JOIN agenteam_mcp.oauth_requests e ON e.command_digest=r.command_digest
 WHERE e.operation='exchange' AND e.process_id<>$1::uuid AND r.io_retired AND r.lease_released)`, objects.process.String()).Scan(
				&phase[0], &phase[1], &phase[2], &phase[3], &phase[4], &phase[5], &phase[6], &phase[7])
			if err != nil {
				mcpM3RecoveryQueryFailure(err)
			}
			m2RecoveryCheck(t, "result-observer", err)
			if (!sampled || phase != previous) && store.reports.Add(1) <= 64 {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_oauth_cold_phase",
					"complete": phase[0], "claimed": phase[1], "exchange": phase[2], "unknown": phase[3],
					"retired": phase[4], "cleanup": phase[5], "deleted": phase[6], "connection_retired": phase[7]})
			}
			previous, sampled = phase, true
			if phase[0] {
				break
			}
			select {
			case <-ctx.Done():
				m2RecoveryCheck(t, "result-observer", ctx.Err())
			case <-done:
				mcpOAuthColdReturnDiagnostic(owned)
				m2RecoveryCheck(t, "root-return", runErr)
				t.Fatal("OAuth recovery App returned before cleanup")
			case <-tick.C:
			}
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_oauth_cleanup_complete"})
	}
	if mode == "recover-human-refresh" {
		mcpM3ObserveHumanRefresh(t, store, objects.process.String(), done, func() error { return runErr })
	}
	<-done
	mcpOAuthColdReturnDiagnostic(owned)
	m2RecoveryCheck(t, "root-return", runErr)
	joined := owned.producersJoined()
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_oauth_cold_returned", "joined": joined})
	if !joined {
		t.Fatal("OAuth recovery returned before actual owner Join")
	}
}
