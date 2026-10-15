//go:build integration && linux

package app

import (
	"context"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	mcpfixture "github.com/LunaDeerTech/agenteam/tests/mcp/fixture"
)

func TestMCPOAuthRefreshConfiguration(t *testing.T) {
	mcpM3ConfigurationPreflight(t)
	source, err := postgres.EmbeddedSource()
	workAppCheck(t, "oauth-refresh-source", err)
	if source.Target() < 135 {
		t.Fatal("OAuth refresh requires the complete original source through 135")
	}
}

// The issuer creates an actual short-lived token; no database or Secret state
// is rewritten to manufacture expiry. The new assertion begins with a normal
// Owner refresh after the persisted original expiry has elapsed.
func TestMCPOAuthAppRefreshRotation(t *testing.T) {
	TestMCPOAuthRefreshConfiguration(t)
	mcpOAuthAppAuthorizationExchangeWith(t, false, func(t *testing.T, peer *mcpfixture.Provider) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// Initial discovery must pass the production 30-second token safety
		// window before this test waits for the actual persisted expiry.
		_, err := peer.SetOAuthLifetime(ctx, 45)
		workAppCheck(t, "oauth-refresh-short-token", err)
	}, func(t *testing.T, v *executionSchedulerApp, peer *mcpfixture.Provider, connection mcp.Connection) {
		var expires time.Time
		var credential, originalRequest string
		var originalVersion int64
		err := v.admin.QueryRow(v.ctx, `SELECT credential_id,secret_version,last_request_id,expires_at
 FROM agenteam_mcp.oauth_tokens WHERE project_id=$1 AND connection_id=$2`, v.a.String(), connection.ID.String()).Scan(&credential, &originalVersion, &originalRequest, &expires)
		workAppCheck(t, "oauth-refresh-original-token", err)
		if credential != connection.CredentialID || originalVersion < 1 || expires.IsZero() {
			t.Fatal("oauth refresh original token mapping missing")
		}
		// The next actual token response uses the normal issuer lifetime.
		_, err = peer.SetOAuthLifetime(v.ctx, 3600)
		workAppCheck(t, "oauth-refresh-next-token-lifetime", err)
		delay := time.Until(expires.Add(25 * time.Millisecond))
		if delay > 46*time.Second {
			t.Fatal("oauth refresh did not issue the bounded short-lived token")
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-v.ctx.Done():
				t.Fatal("oauth refresh deadline cancelled")
			}
		}
		scope, err := id.InProject(v.a)
		workAppCheck(t, "oauth-refresh-project", err)
		identity, err := mcp.CommandIdentity(v.actor, scope, "connection.refresh", f.IdempotencyKey(guardID[f.Request](t).String()))
		workAppCheck(t, "oauth-refresh-command", err)
		command := mcp.Command{Actor: v.actor, Scope: scope, Identity: identity, ExpectedVersion: connection.Version}
		input := mcp.ConnectInput{ConfigID: connection.ConfigID, CredentialID: connection.CredentialID}
		result, err := v.runtime.mcp.Refresh(v.ctx, command, input)
		workAppCheck(t, "oauth-refresh-rotation", err)
		if result.Connection == nil || result.Connection.ID != connection.ID || result.Connection.State != "connected" || result.Connection.ToolsState != "ready" || result.Connection.ResourcesState != "ready" || result.Connection.Version <= connection.Version {
			t.Fatal("oauth refresh did not republish the original SDK directory")
		}
		replay, err := v.runtime.mcp.Refresh(v.ctx, command, input)
		workAppCheck(t, "oauth-refresh-original-replay", err)
		if replay.Connection == nil || replay.Connection.ID != result.Connection.ID || replay.Connection.Version != result.Connection.Version {
			t.Fatal("oauth refresh replay changed the canonical receipt")
		}
		stats, err := peer.OAuthState(v.ctx)
		workAppCheck(t, "oauth-refresh-issuer-state", err)
		if stats.Registrations != 1 || stats.Exchanges != 1 || stats.Refreshes != 1 || stats.Accepted < 2 {
			t.Fatal("oauth refresh repeated the issuer exchange or failed real SDK authorization")
		}
		var refreshes, exactRetired, tokenCAS, outerRetired int
		err = v.admin.QueryRow(v.ctx, `SELECT count(*),count(*) FILTER(WHERE
 r.phase='complete' AND r.io_retired AND r.lease_released AND r.retired_at IS NOT NULL
 AND r.process_required AND r.process_id=$2 AND r.credential_id=$3 AND r.credential_version=$4
 AND r.result_credential_id=$3 AND r.result_credential_version=$4+1
 AND r.credential_lease_id IS NULL AND r.flow_id IS NULL AND r.startup_request_id IS NULL
 AND r.user_id=$5 AND r.session_id=$6 AND r.command_digest IS NOT NULL
 AND r.execution_id IS NULL AND r.operation_id IS NULL AND r.attempt_id IS NULL
 AND r.backend_request_id IS NULL AND r.runtime_binding_id IS NULL)
 FROM agenteam_mcp.oauth_requests r WHERE r.project_id=$1 AND r.operation='refresh'`, v.a.String(), v.objects.process.String(), credential, originalVersion, v.actor.Details().UserID, v.actor.Details().SessionID).Scan(&refreshes, &exactRetired)
		workAppCheck(t, "oauth-refresh-original-request-facts", err)
		err = v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_mcp.oauth_tokens t
 JOIN agenteam_mcp.oauth_requests r ON r.id=t.last_request_id
 JOIN agenteam_secret.secrets s ON s.id=t.credential_id::uuid
 WHERE t.credential_id=$1 AND t.project_id=$2 AND t.connection_id=$3
 AND t.secret_version=$4+1 AND t.last_request_id<>$5 AND t.refreshable
 AND t.expires_at>clock_timestamp() AND r.operation='refresh'
 AND r.result_credential_id=t.credential_id AND r.result_credential_version=t.secret_version
 AND s.version=t.secret_version AND s.purpose='mcp' AND s.scope='project' AND s.project_id=t.project_id::uuid
 AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_leases l WHERE l.credential_id=s.id AND NOT l.released)`, credential, v.a.String(), connection.ID.String(), originalVersion, originalRequest).Scan(&tokenCAS)
		workAppCheck(t, "oauth-refresh-token-cas", err)
		err = v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_mcp.connection_requests
 WHERE project_id=$1 AND connection_id=$2 AND connection_version=$3
 AND phase='complete' AND io_retired AND lease_released AND credential_id=$4`, v.a.String(), connection.ID.String(), result.Connection.Version, credential).Scan(&outerRetired)
		workAppCheck(t, "oauth-refresh-outer-retirement", err)
		if refreshes != 1 || exactRetired != 1 || tokenCAS != 1 || outerRetired != 1 {
			t.Fatalf("oauth refresh facts requests=%d retired=%d token_cas=%d outer_retired=%d", refreshes, exactRetired, tokenCAS, outerRetired)
		}
		t.Log("MCP_OAUTH_REFRESH actual_expiry=true issuer_refresh_once=true secret_cas=true original_caller=true outer_retirement_observed=true replay_no_refresh=true")
	})
}
