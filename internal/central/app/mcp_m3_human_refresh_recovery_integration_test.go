//go:build integration && linux

package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	mcpfixture "github.com/LunaDeerTech/agenteam/tests/mcp/fixture"
)

// A real Owner Refresh reaches one issuer token rotation, then stops after its
// original Secret write binding commits but before Secret prepares that write.
// The Owner has explicitly disabled this connected configuration, so restart
// isolates this Human request's recovery from the independent startup refresh.
func TestMCPM3OAuthColdHumanRefresh(t *testing.T) {
	mcpM3ConfigurationPreflight(t)
	peer := mcpfixture.New(t)
	_, err := peer.SetOAuthLifetime(context.Background(), 45)
	workAppCheck(t, "human-refresh-owned-lifetime", err)
	v := newMCPM3AppWithOrigin(t, peer, nil, mcpOAuthColdOrigin)
	scope, _ := id.InProject(v.a)
	identity, err := mcp.CommandIdentity(v.actor, scope, "config.create", f.IdempotencyKey(guardID[f.Request](t).String()))
	workAppCheck(t, "human-refresh-config-command", err)
	created, err := v.runtime.mcp.CreateConfig(v.ctx, mcp.Command{Actor: v.actor, Scope: scope, Identity: identity},
		mcp.ConfigInput{Name: "owned-cold-human-refresh", Endpoint: peer.OAuthEndpoint(), Authentication: protocol.Authentication{Scheme: "oauth"}})
	workAppCheck(t, "human-refresh-config", err)
	if created.Config == nil {
		t.Fatal("Human Refresh configuration receipt absent")
	}
	mcpM3RecoverySeedJoin(t, v)
	env := append(guardEnvironment(t, v.root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+mcpOAuthColdOrigin,
		"AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+peer.CAFile(),
		"AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1),
		"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms",
		"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s")
	first, address := mcpM3OAuthColdChild(t, v, peer, env, "hold-human-refresh")
	base := "/api/v1/projects/" + v.a.String() + "/mcp"
	started, err := mcpM3OAuthColdRequest(v.ctx, v.client, address, v.cookie, v.csrf, base+"/oauth/begin",
		guardID[f.Request](t).String(), workAppJSON(t, map[string]any{"config_id": created.Config.ID, "expected_version": "0"}))
	workAppCheck(t, "human-refresh-begin", err)
	if started.OAuth == nil || started.Connection == nil {
		t.Fatal("Human Refresh original OAuth flow absent")
	}
	callback, err := peer.AuthorizeOAuth(v.ctx, started.OAuth.AuthorizationURL)
	workAppCheck(t, "human-refresh-authorize", err)
	state, code, issuer := callback.Fields()
	completeBody := workAppJSON(t, map[string]string{"state": state, "code": code, "issuer": issuer})
	connected, err := mcpM3OAuthColdRequest(v.ctx, v.client, address, v.cookie, v.csrf, "/api/v1/mcp/oauth/complete",
		guardID[f.Request](t).String(), completeBody)
	clear(completeBody)
	workAppCheck(t, "human-refresh-complete", err)
	if connected.Connection == nil || connected.Connection.State != "connected" || !connected.Connection.Enabled ||
		connected.Connection.ToolsRevision == 0 || connected.Connection.ResourcesRevision == 0 {
		t.Fatal("Human Refresh requires the real completed SDK connection")
	}
	connectionPath := base + "/connections/" + connected.Connection.ID.String()
	disabled, err := mcpM3OAuthColdRequest(v.ctx, v.client, address, v.cookie, v.csrf, connectionPath+"/disable",
		guardID[f.Request](t).String(), workAppJSON(t, map[string]any{"expected_version": connected.Connection.Version}))
	workAppCheck(t, "human-refresh-owner-disable", err)
	if disabled.Connection == nil || disabled.Connection.Enabled || disabled.Connection.State != "connected" ||
		disabled.Connection.ID != connected.Connection.ID || disabled.Connection.Version != connected.Connection.Version+1 {
		t.Fatal("Human Refresh fixture did not use the original Owner disable command")
	}
	_, err = peer.SetOAuthLifetime(v.ctx, 3600)
	workAppCheck(t, "human-refresh-next-issued-lifetime", err)
	var expiry time.Time
	var secretVersion int64
	err = v.admin.QueryRow(v.ctx, `SELECT expires_at,secret_version FROM agenteam_mcp.oauth_tokens
 WHERE connection_id=$1 AND credential_id=$2`, disabled.Connection.ID.String(), disabled.Connection.CredentialID).Scan(&expiry, &secretVersion)
	mcpM3QueryCheck(t, "human-refresh-original-expiry", err)
	delay := time.Until(expiry) + 25*time.Millisecond
	if delay <= 0 || delay > 46*time.Second {
		t.Fatal("Human Refresh natural expiry is outside its owned bounded window")
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-v.ctx.Done():
		t.Fatal("Human Refresh fixture ended before original expiry")
	}
	refreshKey := guardID[f.Request](t).String()
	requestCtx, cancel := context.WithTimeout(v.ctx, 20*time.Second)
	defer cancel()
	returned := make(chan error, 1)
	httpDone := make(chan struct{})
	refreshBody := workAppJSON(t, map[string]any{"expected_version": disabled.Connection.Version})
	t.Cleanup(func() {
		cancel()
		usageAppWait(t, httpDone, "human-refresh-original-http-cleanup")
		select {
		case returnedErr := <-returned:
			var fault *f.Fault
			code, state := "", ""
			if errors.As(returnedErr, &fault) {
				code, state = string(fault.Code.Safe()), string(fault.CommitState.Safe())
			}
			t.Logf("MCP_HUMAN_REFRESH_RETURN failed=%t code=%s state=%s", returnedErr != nil, code, state)
		default:
		}
	})
	go func() {
		defer close(httpDone)
		defer clear(refreshBody)
		_, err := mcpM3OAuthColdRequest(requestCtx, v.client, address, v.cookie, v.csrf, connectionPath+"/refresh", refreshKey, refreshBody)
		returned <- err
	}()
	m2RecoveryEvent(t, first, "mcp_human_refresh_return_held", nil)
	var refresh, outer, lease, binding string
	var originalReads int
	var exact bool
	err = v.admin.QueryRow(v.ctx, `SELECT e.id,r.id,r.credential_lease_id,e.secret_write_binding,
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=e.project_id::uuid AND action='secret.resolve'),
 e.phase='claimed' AND NOT e.io_retired AND NOT e.lease_released AND e.process_required AND e.process_id=$2::uuid
 AND e.flow_id IS NULL AND e.credential_lease_id IS NULL AND e.credential_version=$3
 AND e.project_id=r.project_id AND e.config_id=r.config_id AND e.config_revision=r.config_revision
 AND e.connection_id=r.connection_id AND e.connection_version=r.connection_version
 AND e.user_id=r.user_id AND e.session_id=r.session_id AND e.credential_id=r.credential_id
 AND r.phase='pending' AND NOT r.io_retired AND NOT r.lease_released
 AND c.version=r.connection_version AND c.version=$4 AND c.refresh_id=r.id AND NOT c.enabled
 AND c.project_id=r.project_id AND c.config_id=r.config_id AND c.config_revision=r.config_revision
 AND c.credential_id=r.credential_id AND l.credential_id=r.credential_id::uuid AND l.consumer='mcp'
 AND l.owner_kind='mcp_connection_request' AND l.owner_id=r.id::uuid AND NOT l.released
 AND token.secret_version=$3 AND token.expires_at<=clock_timestamp() AND secret.version=$3
 AND cmd.phase='pending' AND cmd.response IS NULL AND cmd.command_name='connection.refresh'
 AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_command_receipts WHERE credential_id=r.credential_id::uuid AND mutation_kind='update')
 FROM agenteam_mcp.oauth_requests e JOIN agenteam_mcp.connection_requests r ON r.command_digest=e.command_digest
 JOIN agenteam_mcp.connections c ON c.id=r.connection_id JOIN agenteam_mcp.commands cmd ON cmd.command_digest=r.command_digest
 JOIN agenteam_secret.secret_leases l ON l.id=r.credential_lease_id::uuid
 JOIN agenteam_mcp.oauth_tokens token ON token.credential_id=r.credential_id
 JOIN agenteam_secret.secrets secret ON secret.id=r.credential_id::uuid
 WHERE e.operation='refresh' AND e.project_id=$1`, v.a.String(), first.process, secretVersion, int64(disabled.Connection.Version)+1).Scan(
		&refresh, &outer, &lease, &binding, &originalReads, &exact)
	mcpM3QueryCheck(t, "human-refresh-original-held-tuple", err)
	if !exact || originalReads == 0 || f.Digest(binding).Validate() != nil {
		t.Fatal("Human Refresh original token/outer lease/command tuple is not the held boundary")
	}
	before, err := peer.OAuthState(v.ctx)
	workAppCheck(t, "human-refresh-original-provider", err)
	if before.Registrations != 1 || before.Exchanges != 1 || before.Refreshes != 1 || before.Accepted == 0 {
		t.Fatal("Human Refresh boundary did not follow the one actual issuer rotation")
	}
	mcpM3RecoveryGuard(t, v, first.process, true)
	mcpM3KillRecovery(t, v, first)
	select {
	case err = <-returned:
		if err == nil {
			t.Fatal("held Human Refresh returned success")
		}
	case <-requestCtx.Done():
		t.Fatal("original Human Refresh HTTP caller failed to join after SIGKILL")
	}
	second, recoveredAddress := mcpM3OAuthColdChild(t, v, peer, env, "recover-human-refresh")
	if second.process == first.process {
		t.Fatal("Human Refresh cold recovery reused the original Guard")
	}
	m2RecoveryEvent(t, second, "mcp_human_refresh_cleanup_complete", nil)
	err = v.admin.QueryRow(v.ctx, `SELECT
 e.process_id=$2::uuid AND e.phase='unknown' AND e.error_code='mcp_cancelled' AND e.io_retired AND e.lease_released
 AND e.secret_write_binding=$3 AND e.credential_version=$4 AND e.result_credential_id IS NULL
 AND e.result_credential_version IS NULL AND e.credential_lease_id IS NULL AND e.flow_id IS NULL
 AND r.id=$5 AND r.credential_lease_id=$6::text AND r.phase='abandoned' AND r.io_retired AND r.lease_released
 AND e.project_id=r.project_id AND e.config_id=r.config_id AND e.config_revision=r.config_revision
 AND e.connection_id=r.connection_id AND e.connection_version=r.connection_version
 AND e.user_id=r.user_id AND e.session_id=r.session_id AND e.credential_id=r.credential_id
 AND c.project_id=r.project_id AND c.config_id=r.config_id AND c.config_revision=r.config_revision
 AND c.version=r.connection_version AND c.credential_id=r.credential_id AND c.refresh_id IS NULL
 AND NOT c.enabled AND c.state='connected' AND c.last_error_code='mcp_cancelled'
 AND l.credential_id=r.credential_id::uuid AND l.consumer='mcp' AND l.owner_kind='mcp_connection_request'
 AND l.owner_id=r.id::uuid AND l.released AND token.secret_version=$4 AND secret.version=$4
 AND cmd.phase='pending' AND cmd.response IS NULL AND cmd.command_name='connection.refresh'
 AND (SELECT count(*) FROM agenteam_mcp.connection_requests WHERE command_digest=e.command_digest)=1
 AND (SELECT count(*) FROM agenteam_mcp.oauth_requests WHERE operation='refresh' AND credential_id=e.credential_id)=1
 AND (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=e.project_id::uuid AND action='secret.resolve')=$7
 AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_command_receipts WHERE credential_id=e.credential_id::uuid AND mutation_kind='update')
 FROM agenteam_mcp.oauth_requests e JOIN agenteam_mcp.connection_requests r ON r.command_digest=e.command_digest
 JOIN agenteam_mcp.connections c ON c.id=r.connection_id JOIN agenteam_mcp.commands cmd ON cmd.command_digest=r.command_digest
 JOIN agenteam_secret.secret_leases l ON l.id=$6::uuid
 JOIN agenteam_mcp.oauth_tokens token ON token.credential_id=r.credential_id
 JOIN agenteam_secret.secrets secret ON secret.id=r.credential_id::uuid WHERE e.id=$1`,
		refresh, first.process, binding, secretVersion, outer, lease, originalReads).Scan(&exact)
	mcpM3QueryCheck(t, "human-refresh-canonical-cold-tail", err)
	if !exact {
		t.Fatal("Human Refresh recovery lost its original Unknown/lease/pointer tuple")
	}
	var observation struct {
		State  string          `json:"state"`
		Result json.RawMessage `json:"result"`
	}
	err = mcpM3OAuthColdDecode(v.ctx, v.client, recoveredAddress, v.cookie, v.csrf, base+"/commands/lookup", refreshKey,
		workAppJSON(t, map[string]string{"command": "connection.refresh"}), &observation)
	workAppCheck(t, "human-refresh-original-session-lookup", err)
	if observation.State != "stopped" || len(observation.Result) != 0 {
		t.Fatal("Human Refresh original intent did not expose stopped without a remote result")
	}
	after, err := peer.OAuthState(v.ctx)
	workAppCheck(t, "human-refresh-no-repeated-io", err)
	if after != before {
		t.Fatal("Human Refresh cold recovery repeated issuer or SDK I/O")
	}
	workAppCheck(t, "human-refresh-recovered-stop", second.cmd.Process.Signal(syscall.SIGTERM))
	usageAppWait(t, second.done, "human-refresh-recovered-original-wait")
	m2RecoveryChildWaitLog(t, second, "human-refresh-graceful")
	workAppCheck(t, "human-refresh-recovered-root", second.err)
	m2RecoveryEvent(t, second, "mcp_oauth_cold_returned", func(event map[string]any) bool { return event["joined"] == true })
	mcpM3RecoveryGuard(t, v, second.process, false)
	t.Log("MCP_HUMAN_REFRESH_COLD original_process=true original_outer_lease_released=true original_pending_command=true stopped_without_result=true no_secret_read=true no_issuer_retry=true recovered_app_joined=true")
}

func (s *mcpOAuthColdStore) holdHumanRefresh(ctx context.Context, request, process string) {
	var exact bool
	err := s.Store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_requests e
 JOIN agenteam_mcp.connection_requests r ON r.command_digest=e.command_digest
 JOIN agenteam_mcp.commands cmd ON cmd.command_digest=e.command_digest
 WHERE e.id=$1 AND e.operation='refresh' AND e.process_id=$2::uuid AND e.phase='claimed'
 AND e.secret_write_binding IS NOT NULL AND NOT e.io_retired AND NOT e.lease_released
 AND e.flow_id IS NULL AND e.credential_lease_id IS NULL AND cmd.command_name='connection.refresh'
 AND cmd.phase='pending' AND cmd.response IS NULL AND e.project_id=r.project_id AND e.config_id=r.config_id
 AND e.config_revision=r.config_revision AND e.connection_id=r.connection_id AND e.connection_version=r.connection_version
 AND e.user_id=r.user_id AND e.session_id=r.session_id AND e.credential_id=r.credential_id
 AND r.phase='pending' AND NOT r.io_retired AND NOT r.lease_released)`, request, process).Scan(&exact)
	if err != nil {
		mcpM3RecoveryQueryFailure(err)
		return
	}
	if exact && s.held.CompareAndSwap(false, true) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_human_refresh_return_held"})
		select {} // Original binding commit returns only after the parent kills this owner.
	}
}

func mcpM3ObserveHumanRefresh(t *testing.T, store *mcpOAuthColdStore, process string, done <-chan struct{}, runErr func() error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	var previous [4]bool
	sampled := false
	for {
		var phase [4]bool
		err := store.QueryRow(ctx, `WITH original AS (
 SELECT e.phase,e.io_retired,e.lease_released,r.phase AS outer_phase,r.io_retired AS outer_retired,
 r.lease_released AS outer_released,c.refresh_id,r.id
 FROM agenteam_mcp.oauth_requests e JOIN agenteam_mcp.connection_requests r ON r.command_digest=e.command_digest
 JOIN agenteam_mcp.commands cmd ON cmd.command_digest=e.command_digest JOIN agenteam_mcp.connections c ON c.id=r.connection_id
 WHERE e.operation='refresh' AND e.process_id<>$1::uuid AND cmd.command_name='connection.refresh')
 SELECT count(*)=1,COALESCE(bool_and(phase='unknown' AND io_retired AND lease_released),false),
 COALESCE(bool_and(outer_phase='abandoned' AND outer_retired AND outer_released),false),
 COALESCE(bool_and(refresh_id IS DISTINCT FROM id),false) FROM original`, process).Scan(&phase[0], &phase[1], &phase[2], &phase[3])
		if err != nil {
			mcpM3RecoveryQueryFailure(err)
		}
		m2RecoveryCheck(t, "human-refresh-observer", err)
		if !sampled || phase != previous {
			t.Logf("MCP_HUMAN_REFRESH_PHASE original=%t retired=%t outer_retired=%t detached=%t", phase[0], phase[1], phase[2], phase[3])
		}
		previous, sampled = phase, true
		if phase == [4]bool{true, true, true, true} {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": "mcp_human_refresh_cleanup_complete"})
			return
		}
		select {
		case <-ctx.Done():
			m2RecoveryCheck(t, "human-refresh-observer", ctx.Err())
		case <-done:
			m2RecoveryCheck(t, "human-refresh-root-return", runErr())
			t.Fatal("Human Refresh App exited before the original outer lease retired")
		case <-tick.C:
		}
	}
}
