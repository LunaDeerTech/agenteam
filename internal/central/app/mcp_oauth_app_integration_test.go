//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	mcpfixture "github.com/LunaDeerTech/agenteam/tests/mcp/fixture"
)

// This first OAuth chain calls the actual App-owned domain with its real Owner
// Session. It does not claim callback HTTP/UI, refresh or cold recovery coverage.
func TestMCPOAuthAppAuthorizationExchange(t *testing.T) {
	mcpOAuthAppAuthorizationExchange(t, false)
}

func TestMCPOAuthAppHTTPAuthorizationExchange(t *testing.T) {
	mcpOAuthAppAuthorizationExchange(t, true)
}

func mcpOAuthAppAuthorizationExchange(t *testing.T, throughHTTP bool) {
	mcpOAuthAppAuthorizationExchangeWith(t, throughHTTP, nil, nil)
}

func mcpOAuthAppAuthorizationExchangeWith(t *testing.T, throughHTTP bool,
	beforePeer func(*testing.T, *mcpfixture.Provider),
	after func(*testing.T, *executionSchedulerApp, *mcpfixture.Provider, mcp.Connection),
) {
	mcpM3ConfigurationPreflight(t)
	peer := mcpfixture.New(t)
	if beforePeer != nil {
		beforePeer(t, peer)
	}
	address, err := netip.ParseAddr(peer.Address())
	workAppCheck(t, "oauth-peer-address", err)
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		env := append(guardEnvironment(t, v.root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN=https://app.fixture.test", "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1), "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s", "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+peer.CAFile())
		v.root.cfg = guardConfiguration(t, env)
		d.outboundConstruct = func(ctx context.Context, cfg config.Config, actual *outboundRuntime) error {
			actual.resolver = appFixtureResolver{address}
			client, e := outbound.NewClient(actual.policy, cfg.OutboundTrust(), actual.resolver)
			if e != nil {
				return e
			}
			actual.client.StopAdmission()
			if e = actual.client.Drain(ctx); e != nil {
				return e
			}
			actual.client, transport = client, actual
			return nil
		}
	})
	if transport == nil || v.runtime.mcp == nil || v.assembly.mcp != v.runtime.mcp {
		t.Fatal("oauth original App process owner missing")
	}
	ports, err := outbound.SelectedPorts(8443)
	workAppCheck(t, "oauth-policy-ports", err)
	rule, err := outbound.NewRule(peer.Address()+"/32", ports)
	workAppCheck(t, "oauth-policy-rule", err)
	rules, err := outbound.NewRules(rule)
	workAppCheck(t, "oauth-policy-rules", err)
	identity, err := f.NewCommandIdentity("outbound-policy-v2", []string{v.actor.Details().UserID}, "update", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "oauth-policy-identity", err)
	before := transport.policy.Status()
	if before.Version == nil {
		t.Fatal("oauth original outbound policy missing")
	}
	_, err = transport.policy.UpdatePolicy(v.ctx, v.actor, outbound.CommandMeta{Identity: identity, ExpectedVersion: *before.Version, HTTPTraceID: guardID[f.Request](t).String()}, outbound.Allowlist, rules)
	workAppCheck(t, "oauth-policy", err)
	scope, err := id.InProject(v.a)
	workAppCheck(t, "oauth-project", err)
	command := func(name string, version f.Version) mcp.Command {
		key := f.IdempotencyKey(guardID[struct{}](t).String())
		identity, e := mcp.CommandIdentity(v.actor, scope, name, key)
		workAppCheck(t, "oauth-command", e)
		return mcp.Command{Actor: v.actor, Scope: scope, Identity: identity, ExpectedVersion: version}
	}
	service := v.runtime.mcp
	configCommand := command("config.create", 0)
	configInput := mcp.ConfigInput{Name: "owned-oauth", Endpoint: peer.OAuthEndpoint(), Authentication: protocol.Authentication{Scheme: "oauth"}}
	var created mcp.CommandResult
	if throughHTTP {
		created, err = mcpOAuthHTTPRequest(t, v, "/api/v1/projects/"+v.a.String()+"/mcp/configs", configCommand.Identity.Key(), map[string]any{"expected_version": "0", "input": map[string]any{"name": configInput.Name, "description": "", "endpoint": configInput.Endpoint, "authentication": configInput.Authentication}})
	} else {
		created, err = service.CreateConfig(v.ctx, configCommand, configInput)
	}
	workAppCheck(t, "oauth-create", err)
	if created.Config == nil {
		t.Fatal("oauth config receipt absent")
	}
	begin := command("oauth.begin", 0)
	beginCall := func() (mcp.CommandResult, error) {
		if throughHTTP {
			return mcpOAuthHTTPRequest(t, v, "/api/v1/projects/"+v.a.String()+"/mcp/oauth/begin", begin.Identity.Key(), map[string]any{"config_id": created.Config.ID, "expected_version": "0"})
		}
		return service.BeginOAuth(v.ctx, begin, created.Config.ID)
	}
	started, err := beginCall()
	workAppCheck(t, "oauth-begin", err)
	if started.OAuth == nil || started.Connection == nil || started.Connection.State != "auth_required" {
		t.Fatal("oauth ready flow absent")
	}
	repeated, err := beginCall()
	workAppCheck(t, "oauth-begin-replay", err)
	if repeated.OAuth == nil || repeated.OAuth.FlowID != started.OAuth.FlowID {
		t.Fatal("oauth begin created another flow")
	}
	callback, err := peer.AuthorizeOAuth(v.ctx, started.OAuth.AuthorizationURL)
	workAppCheck(t, "oauth-authorize", err)
	state, code, issuer := callback.Fields()
	input, err := mcp.NewOAuthCallback(state, code, issuer)
	workAppCheck(t, "oauth-callback-shape", err)
	key := f.IdempotencyKey(guardID[struct{}](t).String())
	completeCall := func(key f.IdempotencyKey) (mcp.CommandResult, error) {
		if throughHTTP {
			return mcpOAuthHTTPRequest(t, v, "/api/v1/mcp/oauth/complete", key, map[string]any{"state": state, "code": code, "issuer": issuer})
		}
		return service.CompleteOAuth(v.ctx, v.actor, key, input)
	}
	completed, err := completeCall(key)
	workAppCheck(t, "oauth-complete", err)
	if completed.Connection == nil || completed.Connection.State != "connected" || completed.Connection.ToolsState != "ready" || completed.Connection.ResourcesState != "ready" {
		t.Fatal("oauth official SDK discovery did not complete")
	}
	replay, err := completeCall(key)
	workAppCheck(t, "oauth-complete-replay", err)
	if replay.Connection == nil || replay.Connection.ID != completed.Connection.ID || replay.Connection.Version != completed.Connection.Version {
		t.Fatal("oauth callback replay changed original receipt")
	}
	if _, err = completeCall(f.IdempotencyKey(guardID[struct{}](t).String())); err == nil {
		t.Fatal("consumed authorization code accepted a new command")
	}
	stats, err := peer.OAuthState(v.ctx)
	workAppCheck(t, "oauth-peer-state", err)
	if stats.Registrations != 1 || stats.Exchanges != 1 || stats.Refreshes != 0 || stats.Accepted < 1 {
		t.Fatal("oauth original protocol count mismatch")
	}
	var requests, retired, operations, flows, tokens int
	err = v.admin.QueryRow(v.ctx, `SELECT count(*),count(*) FILTER(WHERE phase='complete' AND io_retired AND lease_released AND process_required AND process_id=$2),count(DISTINCT operation) FILTER(WHERE operation IN ('begin','exchange','retire')) FROM agenteam_mcp.oauth_requests WHERE project_id=$1`, v.a.String(), v.objects.process.String()).Scan(&requests, &retired, &operations)
	workAppCheck(t, "oauth-original-requests", err)
	err = v.admin.QueryRow(v.ctx, `SELECT (SELECT count(*) FROM agenteam_mcp.oauth_flows WHERE project_id=$1 AND phase='complete'),(SELECT count(*) FROM agenteam_mcp.oauth_tokens WHERE project_id=$1)`, v.a.String()).Scan(&flows, &tokens)
	workAppCheck(t, "oauth-original-credentials", err)
	if requests != 3 || retired != 3 || operations != 3 || flows != 1 || tokens != 1 {
		t.Fatal("oauth original request retirement mismatch")
	}
	if after != nil {
		after(t, v, peer, *completed.Connection)
	}
	v.stopBeforeRelease(t)
	v.store.releaseDirectory()
	select {
	case <-v.root.done:
	case <-time.After(7 * time.Second):
		t.Fatal("oauth original App did not return")
	}
	workAppCheck(t, "oauth-original-app-return", v.root.err)
	if !service.Joined() || !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("oauth original owners not joined")
	}
	t.Logf("MCP_OAUTH_APP original_owner=true controlled_official_sdk=true pkce=true authorization_code_once=true replay_observation=true flow_retired=true app_joined=true refresh_not_run=%t callback_http=%t", after == nil, throughHTTP)
}

func mcpOAuthHTTPRequest(t *testing.T, v *executionSchedulerApp, path string, key f.IdempotencyKey, body any) (mcp.CommandResult, error) {
	t.Helper()
	request, err := http.NewRequestWithContext(v.ctx, http.MethodPost, v.address+path, bytes.NewReader(workAppJSON(t, body)))
	if err != nil {
		return mcp.CommandResult{}, err
	}
	origin, err := url.Parse(v.root.cfg.PublicOrigin())
	if err != nil {
		return mcp.CommandResult{}, err
	}
	request.Host = origin.Host
	request.Header.Set("Origin", origin.String())
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	request.Header.Set("X-CSRF-Token", v.csrf)
	request.Header.Set("Idempotency-Key", string(key))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: v.cookie})
	response, err := v.client.Do(request)
	if err != nil {
		return mcp.CommandResult{}, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 {
		return mcp.CommandResult{}, f.NewFault(f.DependencyUnavailable, f.Unknown)
	}
	if response.StatusCode != http.StatusOK {
		var problem struct {
			Code f.Code `json:"code"`
		}
		if json.Unmarshal(raw, &problem) != nil || !problem.Code.Known() {
			problem.Code = f.InternalError
		}
		return mcp.CommandResult{}, f.NewFault(problem.Code, f.NotStarted)
	}
	var result mcp.CommandResult
	if json.Unmarshal(raw, &result) != nil {
		return result, f.NewFault(f.InvalidState, f.NotStarted)
	}
	return result, nil
}
