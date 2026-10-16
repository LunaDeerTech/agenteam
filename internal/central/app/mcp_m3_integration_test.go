//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	mcpfixture "github.com/LunaDeerTech/agenteam/tests/mcp/fixture"
	netfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/outbound"
	"github.com/jackc/pgx/v5/pgconn"
)

// This complete original App configuration is checked before any owned
// database, object store or MCP peer is provisioned.
func TestMCPM3AppConfiguration(t *testing.T) {
	mcpM3ConfigurationPreflight(t)
	mcpM3ControlModesPreflight(t)
}

func mcpM3ControlModesPreflight(t *testing.T) {
	t.Helper()
	for _, mode := range []string{"off", "direct-text"} {
		values := map[string]string{"PUBLIC_ORIGIN": "http://localhost:8080"}
		if mode == "direct-text" {
			values["EXECUTION_RUNTIME"] = executionSchedulerAppConfig
			values["SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS"] = "2"
			values["SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF"] = "100ms"
			values["SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF"] = "1s"
		}
		cfg := testConfig(t, "5s", values)
		workAppCheck(t, "mcp-mode-config-"+mode, cfg.Validate())
		options, enabled := cfg.ExecutionRuntime()
		if enabled != (mode == "direct-text") || enabled && options.ToolLoop() {
			t.Fatal("mcp-stage=mode-config unexpected runtime profile")
		}
	}
}

func mcpM3ConfigurationPreflight(t *testing.T) {
	t.Helper()
	cfg := testConfig(t, "5s", map[string]string{
		"PUBLIC_ORIGIN":                          "http://localhost:8080",
		"EXECUTION_RUNTIME":                      strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1),
		"SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS":    "2",
		"SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF": "100ms",
		"SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF":     "1s",
	})
	workAppCheck(t, "mcp-complete-config", cfg.Validate())
	runtime, enabled := cfg.ExecutionRuntime()
	_, retry := cfg.SchedulerLaunchRetryPolicy()
	if !enabled || !runtime.ToolLoop() || !retry {
		t.Fatal("MCP fixture lost the original Tool loop, scheduler retry or isolated route")
	}
	workAppCheck(t, "mcp-runtime-config", runtime.Validate())
	_, err := mcpM3AgentIntent(guardID[id.Agent](t), guardID[mc.Model](t), []id.ToolID{guardID[id.Tool](t)})
	workAppCheck(t, "mcp-agent-intent-preflight", err)
	raw := workAppJSON(t, mcpM3ModelInput())
	var model mc.ModelInput
	workAppCheck(t, "mcp-model-input-wire-preflight", json.Unmarshal(raw, &model))
	workAppCheck(t, "mcp-model-input-preflight", model.Validate())
	var wire struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	workAppCheck(t, "mcp-model-nullable-wire-preflight", json.Unmarshal(raw, &wire))
	for _, key := range []string{"context_length", "max_output"} {
		if string(wire.Capabilities[key]) != "null" {
			t.Fatal("MCP Model HTTP nullable capability omitted")
		}
	}
	workAppCheck(t, "mcp-task-intent-preflight", mcpM3TaskRequest(guardID[wc.Task](t), guardID[pc.Sprint](t)).Validate())
}

// This first top exercises the original App, Owner HTTP, Audit and Registry
// control plane against an official SDK TLS peer. It does not claim Agent use.
func TestMCPM3OwnerConnection(t *testing.T) {
	mcpM3ConfigurationPreflight(t)
	peer := mcpfixture.New(t)
	v := newMCPM3App(t, peer, nil)
	base := "/api/v1/projects/" + v.a.String() + "/mcp"
	var created mcp.CommandResult
	mcpM3HTTP(t, v, "config-create", http.MethodPost, base+"/configs", map[string]any{"expected_version": "0", "input": map[string]any{"name": "Owned MCP", "description": "official SDK peer", "endpoint": peer.Endpoint(), "authentication": map[string]string{"scheme": "none"}}}, guardID[struct{}](t).String(), http.StatusOK, &created)
	if created.Config == nil || created.Config.Version != 1 {
		t.Fatal("mcp-stage=config-create missing canonical receipt")
	}
	key := guardID[struct{}](t).String()
	var connected mcp.CommandResult
	mcpM3HTTP(t, v, "connection-connect", http.MethodPost, base+"/connections/connect", map[string]any{"config_id": created.Config.ID, "expected_version": "0"}, key, http.StatusOK, &connected)
	if connected.Connection == nil || connected.Connection.State != "connected" || !connected.Connection.Enabled || connected.Connection.ToolsState != "ready" || connected.Connection.ResourcesState != "ready" {
		code := "missing_receipt"
		if connected.Connection != nil {
			code = connected.Connection.LastErrorCode
		}
		t.Fatalf("mcp-stage=connection-discovery safe_code=%s", code)
	}
	connection := connected.Connection
	var tools struct {
		Items []struct {
			ToolID     string `json:"tool_id"`
			RemoteName string `json:"remote_name"`
			Active     bool   `json:"active"`
		} `json:"items"`
	}
	mcpM3HTTP(t, v, "tool-directory", http.MethodGet, base+"/connections/"+connection.ID.String()+"/tools", nil, "", http.StatusOK, &tools)
	foundEcho := false
	for _, tool := range tools.Items {
		if tool.RemoteName == "echo" && tool.ToolID != "" && tool.Active {
			foundEcho = true
		}
	}
	if !foundEcho {
		t.Fatal("mcp-stage=tool-directory original discovered echo not registered")
	}
	var observed struct {
		State  string             `json:"state"`
		Result *mcp.CommandResult `json:"result"`
	}
	mcpM3HTTP(t, v, "original-command-lookup", http.MethodPost, base+"/commands/lookup", map[string]string{"command": "connection.connect"}, key, http.StatusOK, &observed)
	if observed.State != "complete" || observed.Result == nil || observed.Result.Connection == nil || observed.Result.Connection.ID != connection.ID {
		t.Fatal("mcp-stage=original-command-lookup did not return same connection")
	}
	var resources struct {
		Items []json.RawMessage `json:"items"`
	}
	mcpM3HTTP(t, v, "resource-catalog", http.MethodGet, base+"/connections/"+connection.ID.String()+"/resources", nil, "", http.StatusOK, &resources)
	if len(resources.Items) < 1 {
		t.Fatal("mcp-stage=resource-catalog empty original provider catalog")
	}
	mcpM3HTTP(t, v, "foreign-project-connection", http.MethodGet, "/api/v1/projects/"+v.b.String()+"/mcp/connections/"+connection.ID.String(), nil, "", http.StatusNotFound, nil)
	for _, action := range []string{"disable", "enable", "disconnect"} {
		var result mcp.CommandResult
		mcpM3HTTP(t, v, "connection-"+action, http.MethodPost, base+"/connections/"+connection.ID.String()+"/"+action, map[string]any{"expected_version": connection.Version}, guardID[struct{}](t).String(), http.StatusOK, &result)
		if result.Connection == nil || result.Connection.Version <= connection.Version {
			t.Fatalf("mcp-stage=connection-%s version did not advance", action)
		}
		connection = result.Connection
	}
	if connection.State != "unconnected" || connection.Enabled {
		t.Fatal("mcp-stage=disconnect current connection still enabled")
	}
	count, err := peer.CallCount()
	workAppCheck(t, "mcp-peer-call-count", err)
	if count != 0 {
		t.Fatal("mcp-stage=control-plane unexpectedly called a remote tool")
	}
	v.stopBeforeRelease(t)
	v.store.releaseDirectory()
	select {
	case <-v.root.done:
	case <-time.After(7 * time.Second):
		t.Fatal("mcp-stage=app-join original run did not return")
	}
	workAppCheck(t, "mcp-original-app-return", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("mcp-stage=app-join original owners not joined")
	}
	t.Log("MCP_OWNER_CONNECTION original_app=true owner_http=true controlled_official_sdk=true discovery=true resource_catalog=true lookup=true foreign_project_rejected=true enable_disable_disconnect=true agent_not_run=true")
}

// Control-plane configuration and Secret receipts are available without the
// Tool-loop executor. These two small cases reuse the original App fixtures;
// the full credential CRUD and actual Agent chain have separate evidence.
func TestMCPM3OwnerControlModes(t *testing.T) {
	mcpM3ControlModesPreflight(t)
	for _, mode := range []string{"off", "direct-text"} {
		t.Run(mode, func(t *testing.T) {
			var v *executionSchedulerApp
			if mode == "off" {
				v = &executionSchedulerApp{workAppFixture: newWorkAppFixture(t)}
				if _, enabled := v.root.cfg.ExecutionRuntime(); enabled || v.assembly.executions != nil {
					t.Fatal("mcp-stage=control-mode runtime-off fixture enabled execution")
				}
			} else {
				v = newExecutionSchedulerApp(t)
				options, enabled := v.root.cfg.ExecutionRuntime()
				if !enabled || options.ToolLoop() || v.runtime.mcpTools != nil || v.runtime.mcpCapture != nil {
					t.Fatal("mcp-stage=control-mode direct-text fixture enabled MCP execution")
				}
			}
			base := "/api/v1/projects/" + v.a.String() + "/mcp"
			var configured mcp.CommandResult
			mcpM3HTTP(t, v, "mode-config-create", http.MethodPost, base+"/configs", map[string]any{"expected_version": "0", "input": map[string]any{"name": "Owner control mode", "description": "No execution requested", "endpoint": "https://mcp.fixture.test/unused", "authentication": map[string]string{"scheme": "bearer"}}}, guardID[struct{}](t).String(), http.StatusOK, &configured)
			if configured.Config == nil || configured.Config.Version != 1 || configured.Config.ProjectID != v.a.String() {
				t.Fatal("mcp-stage=mode-config-create original receipt absent")
			}
			key := guardID[struct{}](t).String()
			var receipt struct {
				ID      string    `json:"credential_id"`
				Purpose string    `json:"purpose"`
				Version f.Version `json:"version"`
				Deleted bool      `json:"deleted"`
			}
			mcpM3HTTP(t, v, "mode-credential-create", http.MethodPost, base+"/credentials", map[string]any{"value": "owned-mode-credential"}, key, http.StatusOK, &receipt)
			if _, err := f.ParseID[sc.Credential](receipt.ID); err != nil || receipt.Purpose != "mcp" || receipt.Version != 1 || receipt.Deleted {
				t.Fatal("mcp-stage=mode-credential-create safe receipt invalid")
			}
			var metadata struct {
				ID      string    `json:"credential_id"`
				Purpose string    `json:"purpose"`
				Version f.Version `json:"version"`
			}
			mcpM3HTTP(t, v, "mode-credential-get", http.MethodGet, base+"/credentials/"+receipt.ID, nil, "", http.StatusOK, &metadata)
			var lookup struct {
				Observed bool `json:"observed"`
				Result   struct {
					ID      string    `json:"credential_id"`
					Purpose string    `json:"purpose"`
					Version f.Version `json:"version"`
				} `json:"result"`
			}
			mcpM3HTTP(t, v, "mode-credential-lookup", http.MethodPost, base+"/credential-commands/lookup", map[string]any{"kind": "create"}, key, http.StatusOK, &lookup)
			if metadata.ID != receipt.ID || metadata.Purpose != "mcp" || metadata.Version != 1 || !lookup.Observed || lookup.Result != metadata {
				t.Fatal("mcp-stage=mode-credential-lookup original receipt changed")
			}
			mcpM3CredentialFacts(t, v, receipt.ID, 1, 1, 1)
			v.requestStop()
			if v.store != nil {
				v.store.releaseDirectory()
			}
			usageAppWait(t, v.root.done, "mcp-control-mode-original-run")
			workAppCheck(t, "mcp-control-mode-original-return", v.root.err)
			if !v.assembly.Joined() || !v.root.owned.producersJoined() || v.runtime != nil && !v.runtime.Joined() {
				t.Fatal("mcp-stage=mode-join original App owners not joined")
			}
			t.Log("MCP_OWNER_CONTROL_MODE actual_app=true config_create=true credential_create_get_original_lookup=true tool_execution_disabled=true original_owners_joined=true")
		})
	}
}

// This separate top exercises the new Project MCP credential HTTP surface
// through the original App/Account/Secret/Project/Audit graph. No MCP peer or
// Agent dispatch is needed to prove these Owner command and receipt semantics.
func TestMCPM3OwnerCredentials(t *testing.T) {
	mcpM3ConfigurationPreflight(t)
	v := newExecutionSchedulerAppWith(t, func(_ *dependencies, v *executionSchedulerApp) {
		env := append(guardEnvironment(t, v.root.db),
			"AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080",
			"AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s",
			"AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1),
			"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2",
			"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms",
			"AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s")
		v.root.cfg = guardConfiguration(t, env)
	})
	if v.runtime.mcp == nil {
		t.Fatal("mcp-stage=credential-app-assembly original MCP owner absent")
	}
	base := "/api/v1/projects/" + v.a.String() + "/mcp/credentials"
	lookupPath := "/api/v1/projects/" + v.a.String() + "/mcp/credential-commands/lookup"
	createKey, updateKey, deleteKey := guardID[struct{}](t).String(), guardID[struct{}](t).String(), guardID[struct{}](t).String()
	createBody := map[string]any{"value": "owned-mcp-credential"}
	var created struct {
		ID      string    `json:"credential_id"`
		Purpose string    `json:"purpose"`
		Version f.Version `json:"version"`
		Deleted bool      `json:"deleted"`
	}
	mcpM3HTTP(t, v, "credential-create", http.MethodPost, base, createBody, createKey, http.StatusOK, &created)
	if _, err := f.ParseID[sc.Credential](created.ID); err != nil || created.Purpose != "mcp" || created.Version != 1 || created.Deleted {
		t.Fatal("mcp-stage=credential-create invalid safe receipt")
	}
	path := base + "/" + created.ID
	metadata, err := v.request(v.ctx, http.MethodGet, path, nil, "")
	workAppCheck(t, "mcp-credential-get", err)
	head, err := v.request(v.ctx, http.MethodHead, path, nil, "")
	workAppCheck(t, "mcp-credential-head", err)
	if metadata.status != http.StatusOK || head.status != http.StatusOK || len(head.body) != 0 || bytes.Contains(metadata.body, []byte("owned-mcp-credential")) {
		t.Fatal("mcp-stage=credential-metadata invalid GET/HEAD or material exposure")
	}
	mcpM3HTTP(t, v, "credential-original-replay", http.MethodPost, base, createBody, createKey, http.StatusOK, nil)
	mcpM3HTTP(t, v, "credential-changed-material", http.MethodPost, base, map[string]any{"value": "changed-owned-material"}, createKey, http.StatusConflict, nil)
	mcpM3CredentialFacts(t, v, created.ID, 1, 1, 1)
	var updated, deleted struct {
		ID      string    `json:"credential_id"`
		Purpose string    `json:"purpose"`
		Version f.Version `json:"version"`
		Deleted bool      `json:"deleted"`
	}
	mcpM3HTTP(t, v, "credential-update", http.MethodPatch, path, map[string]any{"expected_version": "1", "value": "owned-mcp-rotated"}, updateKey, http.StatusOK, &updated)
	if updated.ID != created.ID || updated.Purpose != "mcp" || updated.Version != 2 || updated.Deleted {
		t.Fatal("mcp-stage=credential-update invalid original CAS receipt")
	}
	mcpM3HTTP(t, v, "credential-stale-version", http.MethodPatch, path, map[string]any{"expected_version": "1", "value": "owned-mcp-stale"}, guardID[struct{}](t).String(), http.StatusConflict, nil)
	mcpM3CredentialFacts(t, v, created.ID, 2, 2, 2)
	mcpM3HTTP(t, v, "credential-delete", http.MethodDelete, path, map[string]any{"expected_version": "2"}, deleteKey, http.StatusOK, &deleted)
	if deleted.ID != created.ID || deleted.Purpose != "mcp" || deleted.Version != 3 || !deleted.Deleted {
		t.Fatal("mcp-stage=credential-delete invalid receipt")
	}
	mcpM3HTTP(t, v, "credential-deleted-metadata", http.MethodGet, path, nil, "", http.StatusNotFound, nil)
	mcpM3CredentialFacts(t, v, created.ID, 0, 3, 3)
	for _, original := range []struct {
		key, kind, version string
		expected           f.Version
		deleted            bool
	}{{createKey, "create", "", 1, false}, {updateKey, "update", "1", 2, false}, {deleteKey, "delete", "2", 3, true}} {
		body := map[string]any{"kind": original.kind}
		if original.kind != "create" {
			body["credential_id"] = created.ID
			body["expected_version"] = original.version
		}
		var observation struct {
			Observed bool `json:"observed"`
			Result   struct {
				ID      string    `json:"credential_id"`
				Purpose string    `json:"purpose"`
				Version f.Version `json:"version"`
				Deleted bool      `json:"deleted"`
			} `json:"result"`
		}
		mcpM3HTTP(t, v, "credential-passive-lookup-"+original.kind, http.MethodPost, lookupPath, body, original.key, http.StatusOK, &observation)
		if !observation.Observed || observation.Result.ID != created.ID || observation.Result.Purpose != "mcp" || observation.Result.Version != original.expected || observation.Result.Deleted != original.deleted {
			t.Fatal("mcp-stage=credential-passive-lookup original history changed")
		}
	}
	mcpM3CredentialFacts(t, v, created.ID, 0, 3, 3)
	// Purpose is taken from the actual Secret postimage, never from browser input.
	var modelCredential struct {
		ID string `json:"credential_id"`
	}
	mcpM3HTTP(t, v, "credential-foreign-purpose-create", http.MethodPost, "/api/v1/projects/"+v.a.String()+"/model-credentials", map[string]any{"value": "owned-model-only"}, guardID[struct{}](t).String(), http.StatusOK, &modelCredential)
	mcpM3HTTP(t, v, "credential-foreign-purpose", http.MethodGet, base+"/"+modelCredential.ID, nil, "", http.StatusNotFound, nil)
	// Drop the first actual HTTP response only after the real service returned
	// and its body closed. The following original-key lookup performs no write.
	lostKey := guardID[struct{}](t).String()
	transport := v.client.Transport
	lost := &mcpM3LostCredentialResponse{next: transport, path: base}
	v.client.Transport = lost
	_, err = v.request(v.ctx, http.MethodPost, base, workAppJSON(t, map[string]any{"value": "owned-lost-response"}), lostKey)
	v.client.Transport = transport
	if err == nil || lost.calls.Load() != 1 || !lost.dropped.Load() {
		t.Fatal("mcp-stage=credential-lost-response did not exercise one original HTTP write")
	}
	var observed struct {
		Observed bool `json:"observed"`
		Result   struct {
			ID      string    `json:"credential_id"`
			Version f.Version `json:"version"`
		} `json:"result"`
	}
	mcpM3HTTP(t, v, "credential-lost-response-lookup", http.MethodPost, lookupPath, map[string]any{"kind": "create"}, lostKey, http.StatusOK, &observed)
	if !observed.Observed || observed.Result.Version != 1 {
		t.Fatal("mcp-stage=credential-lost-response original receipt absent")
	}
	mcpM3CredentialFacts(t, v, observed.Result.ID, 1, 1, 1)
	// Revoking the original session must reject its mutation replay and passive
	// lookup alike. No new Secret receipt or mutation Audit is permitted.
	mcpM3HTTP(t, v, "credential-session-revoke", http.MethodPost, "/api/v1/sessions/logout", map[string]any{}, guardID[struct{}](t).String(), http.StatusNoContent, nil)
	mcpM3HTTP(t, v, "credential-revoked-replay", http.MethodPost, base, createBody, createKey, http.StatusUnauthorized, nil)
	mcpM3HTTP(t, v, "credential-revoked-lookup", http.MethodPost, lookupPath, map[string]any{"kind": "create"}, lostKey, http.StatusUnauthorized, nil)
	mcpM3CredentialFacts(t, v, created.ID, 0, 3, 3)
	mcpM3CredentialFacts(t, v, observed.Result.ID, 1, 1, 1)
	v.requestStop()
	v.store.releaseDirectory()
	select {
	case <-v.root.done:
	case <-time.After(7 * time.Second):
		t.Fatal("mcp-stage=credential-app-join original Run not returned")
	}
	workAppCheck(t, "mcp-credential-app-return", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("mcp-stage=credential-app-join original owners not joined")
	}
	t.Log("MCP_OWNER_CREDENTIALS actual_app=true owner_http=true metadata_head=true fixed_project_purpose=true cas=true original_receipts_audit=true passive_lookup=true revoked_session_rejected=true lost_http_response_lookup=true database_unknown_not_injected=true agent_not_run=true")
}

func mcpM3CredentialFacts(t *testing.T, v *executionSchedulerApp, credential string, version, receipts, audits int64) {
	t.Helper()
	var actualVersion, actualReceipts, actualAudits int64
	err := v.admin.QueryRow(v.ctx, `SELECT
 COALESCE((SELECT version FROM agenteam_secret.secrets WHERE id=$1 AND project_id=$2 AND scope='project' AND purpose='mcp'),0),
 (SELECT count(*) FROM agenteam_secret.secret_command_receipts WHERE credential_id=$1 AND project_id=$2 AND scope='project' AND purpose='mcp'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE resource_id=$1 AND project_id=$2 AND scope='project' AND producer='secret' AND action IN ('secret.create','secret.update','secret.delete') AND outcome='success')`, credential, v.a.String()).Scan(&actualVersion, &actualReceipts, &actualAudits)
	workAppCheck(t, "mcp-credential-current-facts", err)
	if actualVersion != version || actualReceipts != receipts || actualAudits != audits {
		t.Fatalf("mcp-stage=credential-current-facts version=%d receipts=%d audits=%d", actualVersion, actualReceipts, actualAudits)
	}
}

type mcpM3LostCredentialResponse struct {
	next    http.RoundTripper
	path    string
	calls   atomic.Int32
	dropped atomic.Bool
}

func (r *mcpM3LostCredentialResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || request.URL.Path != r.path {
		return r.next.RoundTrip(request)
	}
	r.calls.Add(1)
	response, err := r.next.RoundTrip(request)
	if err != nil {
		return response, err
	}
	_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.New("owned response did not close")
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("owned response was not committed")
	}
	r.dropped.Store(true)
	return nil, io.ErrUnexpectedEOF
}

func mcpM3HTTP(t *testing.T, v *executionSchedulerApp, stage, method, path string, body any, key string, status int, out any) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw = workAppJSON(t, body)
	}
	response, err := v.request(v.ctx, method, path, raw, key)
	workAppCheck(t, "mcp-"+stage, err)
	if response.status != status {
		var problem struct {
			Code f.Code `json:"code"`
		}
		_ = json.Unmarshal(response.body, &problem)
		if stage == "connection-connect" {
			var requests, complete, bindings, registrations int64
			e := v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*) FROM agenteam_mcp.connection_requests WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_mcp.connection_requests WHERE project_id=$1 AND phase='complete'),
 (SELECT count(*) FROM agenteam_mcp.runtime_bindings WHERE project_id=$1),
 (SELECT count(*) FROM agenteam_tool.mcp_registrations WHERE project_id=$1)`, v.a.String()).Scan(&requests, &complete, &bindings, &registrations)
			if e == nil {
				t.Logf("MCP original phase facts requests=%d complete=%d runtime_bindings=%d registrations=%d", requests, complete, bindings, registrations)
			}
		}
		t.Fatalf("mcp-stage=%s http_status=%d expected=%d safe_code=%s", stage, response.status, status, problem.Code.Safe())
	}
	if out != nil {
		workAppCheck(t, "mcp-"+stage+"-decode", json.Unmarshal(response.body, out))
	}
}

// Observe only the original Store result. Driver messages, SQL/bind values,
// endpoint bodies and request/credential identities never enter diagnostics.
func mcpM3CommitObserver(t *testing.T) func(f.TransactionCause, f.CommitResult) {
	var count atomic.Int32
	identifier := regexp.MustCompile(`^[a-zA-Z0-9_.-]{0,128}$`)
	safe := func(v string) string {
		if identifier.MatchString(v) {
			return v
		}
		return "invalid_identifier"
	}
	return func(cause f.TransactionCause, result f.CommitResult) {
		d := cause.Details()
		relevant := d.Primary.Namespace() == "mcp" || d.Primary.Namespace() == "object" || d.Primary.Namespace() == "tool.operation" || d.Owner == "secret-resolution" || strings.HasPrefix(d.Owner, "mcp") || strings.HasPrefix(d.JobType, "mcp")
		if result.State() != f.Unknown && (result.State() != f.NotCommitted || !relevant) || count.Add(1) > 16 {
			return
		}
		t.Logf("MCP original transaction owner=%s job=%s command=%s state=%s", safe(d.Owner), safe(d.JobType), safe(d.Primary.Command()), result.State().Safe())
		fault := result.Fault()
		if fault == nil {
			return
		}
		t.Logf("MCP original transaction rejected owner=%s job=%s command=%s code=%s state=%s", safe(d.Owner), safe(d.JobType), safe(d.Primary.Command()), fault.Code.Safe(), fault.CommitState.Safe())
		var db *postgres.Error
		if errors.As(fault, &db) {
			t.Logf("MCP database code=%s sqlstate=%s", db.Code(), safe(db.SQLState()))
		}
		var pg *pgconn.PgError
		if errors.As(fault, &pg) {
			t.Logf("MCP database constraint sqlstate=%s table=%s constraint=%s", safe(pg.Code), safe(pg.TableName), safe(pg.ConstraintName))
		}
	}
}

type mcpM3Resolver map[string]netip.Addr

// Forward every original Object capability unchanged. These bounded stage
// observations retain the real owner, context, payload, plan and transaction;
// they never log request fields, material, provider bodies or raw causes.
type mcpM3ObservedObjects struct {
	oc.Objects
	t     *testing.T
	count atomic.Int32
}

func (o *mcpM3ObservedObjects) observe(stage string, err error) {
	if o.count.Add(1) > 24 {
		return
	}
	if err == nil {
		o.t.Logf("MCP original Object stage=%s result=success", stage)
		return
	}
	for depth := 0; err != nil && depth < 6; depth++ {
		if fault, ok := err.(*f.Fault); ok {
			o.t.Logf("MCP original Object stage=%s result=failure depth=%d code=%s state=%s", stage, depth, fault.Code.Safe(), fault.CommitState.Safe())
		} else {
			o.t.Logf("MCP original Object stage=%s result=failure depth=%d code=untyped", stage, depth)
		}
		err = errors.Unwrap(err)
	}
}
func (o *mcpM3ObservedObjects) PutObject(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, meta f.CommandMeta, media string, length int64, digest *f.Digest, body io.ReadCloser) (oc.PutResult, error) {
	result, err := o.Objects.PutObject(ctx, actor, owner, meta, media, length, digest, body)
	o.observe("put", err)
	return result, err
}
func (o *mcpM3ObservedObjects) DiscoverAccess(ctx context.Context, request oc.AccessRequest) (oc.AccessLockPlan, error) {
	plan, err := o.Objects.DiscoverAccess(ctx, request)
	o.observe("discover", err)
	return plan, err
}
func (o *mcpM3ObservedObjects) AcquireAccessPlansInTx(ctx context.Context, tx f.Tx, plans []oc.AccessLockPlan, locks []f.LockRequest) (oc.LockedAccess, error) {
	locked, err := o.Objects.AcquireAccessPlansInTx(ctx, tx, plans, locks)
	o.observe("acquire", err)
	return locked, err
}
func (o *mcpM3ObservedObjects) ConsumeUploadInTx(ctx context.Context, tx f.Tx, actor id.Actor, owner oc.ObjectOwner, receipt oc.UploadReceipt, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.ObjectReference, error) {
	reference, err := o.Objects.ConsumeUploadInTx(ctx, tx, actor, owner, receipt, plan, locked)
	o.observe("consume", err)
	return reference, err
}
func (o *mcpM3ObservedObjects) ReadObject(ctx context.Context, actor id.Actor, owner oc.ObjectOwner, object oc.ObjectID, bytes *oc.ByteRange) (*oc.ObjectReader, error) {
	reader, err := o.Objects.ReadObject(ctx, actor, owner, object, bytes)
	o.observe("read", err)
	return reader, err
}

func (r mcpM3Resolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	address, ok := r[host]
	if !ok {
		return nil, errors.New("MCP fixture hostname outside its owned peers")
	}
	return []netip.Addr{address}, nil
}

func newMCPM3App(t *testing.T, peer *mcpfixture.Provider, network *netfixture.Descriptor, observe ...func(*dependencies)) *executionSchedulerApp {
	t.Helper()
	return newMCPM3AppWithOrigin(t, peer, network, "http://localhost:8080", observe...)
}

func newMCPM3AppWithOrigin(t *testing.T, peer *mcpfixture.Provider, network *netfixture.Descriptor, origin string, observe ...func(*dependencies)) *executionSchedulerApp {
	t.Helper()
	if len(observe) > 1 {
		t.Fatal("one original MCP assembly observer allowed")
	}
	address, err := netip.ParseAddr(peer.Address())
	workAppCheck(t, "mcp-peer-address", err)
	resolver := mcpM3Resolver{"mcp.fixture.test": address}
	caFile := peer.CAFile()
	addresses := []netip.Addr{address}
	if network != nil {
		address, err := netip.ParseAddr(network.PrivateIP)
		workAppCheck(t, "mcp-model-peer-address", err)
		resolver["fixture.test"] = address
		addresses = append(addresses, address)
		roots, err := os.ReadFile(caFile)
		workAppCheck(t, "mcp-peer-ca", err)
		modelRoots, err := os.ReadFile(network.CAFile)
		workAppCheck(t, "mcp-model-peer-ca", err)
		caFile = filepath.Join(t.TempDir(), "owned-peer-roots.pem")
		workAppCheck(t, "mcp-combined-peer-ca", os.WriteFile(caFile, append(append(roots, '\n'), modelRoots...), 0600))
	}
	var transport *outboundRuntime
	v := newExecutionSchedulerAppWith(t, func(d *dependencies, v *executionSchedulerApp) {
		d.mcpObjects = func(actual oc.Objects) oc.Objects {
			return &mcpM3ObservedObjects{Objects: actual, t: t}
		}
		mcpObserver, runtimeObserver := mcpM3CommitObserver(t), m1CommitObserver(t)
		v.store.observeCommit = func(c f.TransactionCause, r f.CommitResult) {
			mcpObserver(c, r)
			if network != nil {
				runtimeObserver(c, r)
			}
		}
		env := append(guardEnvironment(t, v.root.db), "AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin, "AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s", "AGENTEAM_CENTRAL_EXECUTION_RUNTIME="+strings.Replace(executionSchedulerAppConfig, "direct-text-v1", "tool-loop-v1", 1), "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_ATTEMPTS=2", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_INITIAL_BACKOFF=100ms", "AGENTEAM_CENTRAL_SCHEDULER_LAUNCH_RETRY_MAX_BACKOFF=1s", "AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+caFile)
		v.root.cfg = guardConfiguration(t, env)
		d.outboundConstruct = func(ctx context.Context, cfg config.Config, actual *outboundRuntime) error {
			actual.resolver = resolver
			// Model builds its adapter later; MCP consumes this shared client.
			// Both retain the real policy, trust and proxy route implementation.
			client, err := outbound.NewClient(actual.policy, cfg.OutboundTrust(), actual.resolver)
			if err != nil {
				return err
			}
			actual.client.StopAdmission()
			if err = actual.client.Drain(ctx); err != nil {
				return err
			}
			actual.client, transport = client, actual
			return nil
		}
		if len(observe) == 1 && observe[0] != nil {
			observe[0](d)
		}
	})
	if transport == nil || v.runtime.mcp == nil {
		t.Fatal("mcp-stage=original-app-assembly missing real MCP owner")
	}
	ports, err := outbound.SelectedPorts(8443)
	workAppCheck(t, "mcp-policy-ports", err)
	var allow []outbound.Rule
	for _, address := range addresses {
		rule, err := outbound.NewRule(address.String()+"/32", ports, false)
		workAppCheck(t, "mcp-policy-rule", err)
		allow = append(allow, rule)
	}
	rules, err := outbound.NewRules(allow...)
	workAppCheck(t, "mcp-policy-rules", err)
	identity, err := f.NewCommandIdentity("outbound-policy", []string{v.actor.Details().UserID}, "update", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "mcp-policy-command", err)
	before := transport.policy.Status()
	if before.Version == nil {
		t.Fatal("mcp-stage=policy original policy absent")
	}
	_, err = transport.policy.UpdatePolicy(v.ctx, v.actor, outbound.CommandMeta{Identity: identity, ExpectedVersion: *before.Version, HTTPTraceID: guardID[f.Request](t).String()}, rules)
	workAppCheck(t, "mcp-policy-update", err)
	return v
}

const mcpM3FinalText = "MCP Tool 和 Resource 已读取；结果来自受控服务。"

// Only the Model HTTP peer supplies deterministic decisions. The Scheduler,
// Agent capture, unified Tool operations, Secret, SDK peer and Object I/O are
// the original production graph; this test never advances an Execution.
func TestMCPM3AgentToolsAndResources(t *testing.T) {
	mcpM3RunAgentToolsAndResources(t, "echo")
}

func TestMCPM3AgentResourceLink(t *testing.T) {
	mcpM3RunAgentToolsAndResources(t, "resource_link")
}

func mcpM3RunAgentToolsAndResources(t *testing.T, firstTool string) {
	t.Helper()
	mcpM3ConfigurationPreflight(t)
	peer := mcpfixture.New(t)
	network, err := netfixture.Load()
	workAppCheck(t, "mcp-owned-model-peer", err)
	v := newMCPM3App(t, peer, network)
	material, err := peer.Credential()
	workAppCheck(t, "mcp-owned-credential", err)
	credential := mcpM3Secret(t, v, sc.MCP, material)
	base := "/api/v1/projects/" + v.a.String() + "/mcp"
	var created, connected mcp.CommandResult
	mcpM3HTTP(t, v, "agent-config-create", http.MethodPost, base+"/configs", map[string]any{"expected_version": "0", "input": map[string]any{"name": "Authenticated MCP", "description": "official SDK peer", "endpoint": peer.Endpoint() + "/protected", "authentication": map[string]string{"scheme": "bearer"}}}, guardID[struct{}](t).String(), http.StatusOK, &created)
	if created.Config == nil {
		t.Fatal("mcp-stage=agent-config-create missing receipt")
	}
	mcpM3HTTP(t, v, "connection-connect", http.MethodPost, base+"/connections/connect", map[string]any{"config_id": created.Config.ID, "credential_id": credential, "expected_version": "0"}, guardID[struct{}](t).String(), http.StatusOK, &connected)
	if connected.Connection == nil || connected.Connection.State != "connected" || !connected.Connection.Enabled || connected.Connection.ToolsState != "ready" || connected.Connection.ResourcesState != "ready" {
		code := "missing_receipt"
		if connected.Connection != nil {
			code = connected.Connection.LastErrorCode
		}
		t.Fatalf("mcp-stage=authenticated-discovery safe_code=%s", code)
	}
	connection := connected.Connection
	var directory struct {
		Items []struct {
			ToolID     id.ToolID `json:"tool_id"`
			RemoteName string    `json:"remote_name"`
			Active     bool      `json:"active"`
		} `json:"items"`
	}
	mcpM3HTTP(t, v, "agent-tool-directory", http.MethodGet, base+"/connections/"+connection.ID.String()+"/tools", nil, "", http.StatusOK, &directory)
	var echo id.ToolID
	for _, tool := range directory.Items {
		if tool.RemoteName == firstTool && tool.Active {
			echo = tool.ToolID
		}
	}
	workAppCheck(t, "mcp-original-echo-tool-id", echo.Validate())
	toolIDs := []id.ToolID{echo}
	for _, key := range []string{"builtin:list-mcp-resources", "builtin:read-mcp-resource"} {
		var raw string
		workAppCheck(t, "mcp-original-core-tool", v.admin.QueryRow(v.ctx, `SELECT tool_id FROM agenteam_tool.identities WHERE stable_key=$1`, key).Scan(&raw))
		tool, err := f.ParseID[id.Tool](raw)
		workAppCheck(t, "mcp-original-core-tool-id", err)
		toolIDs = append(toolIDs, tool)
	}
	echoText := "owned-" + guardID[struct{}](t).String()
	sequence := mcpM3Responses(t, toolIDs, connection.ID.String(), echoText, firstTool == "resource_link")
	scenario, err := network.Create(v.ctx, netfixture.ScenarioConfig{Mode: "openai_chat_wire", WireSequence: sequence})
	workAppCheck(t, "mcp-owned-model-sequence", err)
	task := mcpM3ScheduleTask(t, v, "https://fixture.test:8443/case/"+scenario, toolIDs)
	v.store.releaseDirectory()
	execution := mcpM3AwaitAgent(t, v, task, peer, network, scenario)
	// Stop only after the original terminal result; all owners are joined while
	// the database, Object store and controlled peers are still alive.
	v.runtime.StopAdmission()
	drain, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	workAppCheck(t, "mcp-original-runtime-drain", v.runtime.Drain(drain))
	cancel()
	v.requestStop()
	usageAppWait(t, v.directory.returned, "mcp-manager-original-return")
	usageAppWait(t, v.root.done, "mcp-original-app-return")
	workAppCheck(t, "mcp-original-app-return", v.root.err)
	if !v.runtime.Joined() || !v.assembly.Joined() || !v.root.owned.producersJoined() {
		t.Fatal("mcp-stage=agent-join original owners not joined")
	}
	mcpM3CheckAgentFacts(t, v, peer, network, scenario, execution, echoText, firstTool == "resource_link")
	t.Logf("MCP_AGENT original_scheduler=true authenticated_connection=true canonical_tool_capture=true operation_attempt=true actual_tool=%s actual_resource_object_read_close=true successor_model_consumed_text=true runtime_secret_leases_retired=true original_owners_joined=true external_model=false", firstTool)
}

func mcpM3Secret(t *testing.T, v *executionSchedulerApp, purpose sc.Purpose, material sc.SecretMaterial) sc.CredentialID {
	t.Helper()
	defer material.Destroy()
	scope, err := id.InProject(v.a)
	workAppCheck(t, "mcp-secret-scope", err)
	identity, err := f.NewCommandIdentity("secret", []string{v.a.String(), v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "mcp-secret-identity", err)
	service, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("mcp-stage=secret original owner absent")
	}
	written, err := service.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: scope, Identity: identity, Kind: sc.Create, Purpose: purpose, Value: material})
	workAppCheck(t, "mcp-secret-create", err)
	return written.Metadata.CredentialRef.Details().ID
}

func mcpM3Responses(t *testing.T, tools []id.ToolID, connection, text string, resourceLink bool) []netfixture.WireScenario {
	t.Helper()
	arguments := []any{map[string]string{"text": text}, map[string]string{"connection_id": connection}, map[string]string{"connection_id": connection, "uri": "repo://owned/data"}}
	if resourceLink {
		arguments[0] = map[string]string{}
	}
	var out []netfixture.WireScenario
	for n := range 4 {
		message := map[string]any{"role": "assistant", "content": mcpM3FinalText}
		finish := "stop"
		if n < 3 {
			message["content"] = nil
			message["tool_calls"] = []any{map[string]any{"id": fmt.Sprintf("mcp_owned_%d", n), "type": "function", "function": map[string]any{"name": "tool_" + strings.ReplaceAll(tools[n].String(), "-", ""), "arguments": string(workAppJSON(t, arguments[n]))}}}
			finish = "tool_calls"
		}
		raw := workAppJSON(t, map[string]any{"id": "owned-mcp-model", "object": "chat.completion", "created": 1, "model": "mcp-agent", "choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": finish}}, "usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 10, "total_tokens": 20}})
		out = append(out, netfixture.WireScenario{Status: 200, Headers: map[string]string{"Content-Type": "application/json"}, Chunks: [][]byte{raw}})
	}
	return out
}

func mcpM3ScheduleTask(t *testing.T, v *executionSchedulerApp, endpoint string, tools []id.ToolID) wc.Task {
	t.Helper()
	material, err := sc.NewSecretMaterial([]byte("owned-mcp-model-credential"))
	workAppCheck(t, "mcp-model-secret-material", err)
	credential := mcpM3Secret(t, v, sc.Model, material)
	var receipt mc.CommandReceipt
	v.httpSeed(t, "/model-providers", map[string]any{"input": map[string]any{"name": "MCP owned Model Provider", "protocol": mc.OpenAIChat, "base_url": endpoint, "enabled": true, "credential_ref": credential, "options": map[string]any{}}}, &receipt)
	provider, err := f.ParseID[mc.Provider](receipt.ResourceID)
	workAppCheck(t, "mcp-model-provider-id", err)
	v.httpSeed(t, "/models", map[string]any{"provider_id": provider, "input": mcpM3ModelInput()}, &receipt)
	model, err := f.ParseID[mc.Model](receipt.ResourceID)
	workAppCheck(t, "mcp-model-id", err)
	v.agent = guardID[id.Agent](t)
	// Core resource tools are captured by Registry itself. Only the remote
	// MCP Tool belongs in the Agent's configurable capability allowlist.
	intent, err := mcpM3AgentIntent(v.agent, model, tools[:1])
	workAppCheck(t, "mcp-agent-intent", err)
	_, err = v.assembly.agents.service.CreateAgent(v.ctx, v.actor, executionSchedulerAppMeta(t, nil), v.a, intent)
	workAppCheck(t, "mcp-agent-create", err)
	var structure wc.StructureMutation
	milestone := wc.CreateMilestoneRequest{MilestoneID: guardID[wc.Milestone](t), Title: "MCP integration"}
	v.httpSeed(t, "/milestones", map[string]any{"request": milestone}, &structure)
	sprint := wc.CreateSprintRequest{SprintID: guardID[pc.Sprint](t), MilestoneID: milestone.MilestoneID, Title: "MCP original Agent"}
	v.httpSeed(t, "/sprints", map[string]any{"request": sprint}, &structure)
	sprintVersion := structure.Sprint.Version
	var mutation wc.TaskMutation
	request := mcpM3TaskRequest(guardID[wc.Task](t), sprint.SprintID)
	v.httpSeed(t, "/tasks", map[string]any{"request": request}, &mutation)
	before, err := v.projects.service.GetSchedulerConfig(v.ctx, v.actor, v.a)
	workAppCheck(t, "mcp-scheduler-config", err)
	limit := int64(1)
	_, err = v.projects.service.UpdateProject(v.ctx, v.actor, executionSchedulerAppMeta(t, &before.Project.Version), v.a, pc.UpdateProjectRequest{Scheduler: &pc.ProjectSchedulerConfig{Enabled: true, MaxConcurrency: &limit}})
	workAppCheck(t, "mcp-scheduler-enable", err)
	moved, err := v.planning.transitions.TransferTask(v.ctx, v.actor, executionSchedulerAppMeta(t, &mutation.Task.Version), v.a, request.TaskID, wc.TaskTransfer{TargetState: wc.TaskStateTodo, AssigneeAgentID: &v.agent, AddBlockers: []wc.TaskBlockerCreate{}, ResolveBlockerIDs: []wc.TaskBlockerID{}})
	workAppCheck(t, "mcp-task-assign", err)
	_, err = v.planning.sprints.StartSprint(v.ctx, v.actor, executionSchedulerAppMeta(t, &sprintVersion), v.a, sprint.SprintID)
	workAppCheck(t, "mcp-sprint-start", err)
	return moved.Task
}

func mcpM3AgentIntent(agent id.AgentID, model mc.ModelID, tools []id.ToolID) (ac.AgentCreate, error) {
	disabled := false
	allowed := slices.Clone(tools)
	slices.SortFunc(allowed, func(a, b id.ToolID) int { return strings.Compare(a.String(), b.String()) })
	return ac.NewAgentCreate(ac.AgentCreateFields{AgentID: agent, Name: "mcp-worker", Description: "Uses discovered MCP tools and resources", Instructions: "Call the configured echo Tool, list resources, then read repo://owned/data and report the actual returned text.", ModelRef: model, ApprovalPolicy: ac.ApprovalAllow, AllowedToolIDs: allowed, AllowedMountIDs: []id.MountID{}, AllowedSecretVariableIDs: []id.ProjectVariableID{}, AddSkillsEnabled: &disabled, InstallSkillEnabled: &disabled})
}

func mcpM3ModelInput() map[string]any {
	return map[string]any{"name": "MCP Agent Model", "provider_model_id": "mcp-agent", "type": mc.ChatModel, "enabled": true, "parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{}, "capabilities": map[string]any{"tool_calls": true, "parallel_tool_calls": false, "streaming": true, "reasoning": false, "input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{}, "structured_output_modes": []string{}, "context_length": nil, "max_output": nil}}
}

func mcpM3TaskRequest(task wc.TaskID, sprint pc.SprintID) wc.TaskCreate {
	return wc.TaskCreate{TaskID: task, SprintID: sprint, Title: "Use the MCP service", Type: wc.TaskTypeTask, Priority: wc.TaskPriorityMedium, Plan: "Echo the requested text, list MCP resources, read repo://owned/data, and report the returned content."}
}

func mcpM3AwaitAgent(t *testing.T, v *executionSchedulerApp, task wc.Task, peer *mcpfixture.Provider, network *netfixture.Descriptor, scenario string) id.ExecutionID {
	t.Helper()
	ctx, cancel := context.WithTimeout(v.ctx, 40*time.Second)
	defer cancel()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	diagnostic := &m1Workflow{app: v, task: task, network: network, scenario: scenario}
	diagnose := func(count int, raw, state string) {
		for _, project := range v.runtime.manager.Observation().Projects {
			if project.ProjectID == v.a {
				mcpM3SafeCause(t, project.Err)
			}
		}
		calls, err := peer.CallCount()
		if err != nil {
			t.Logf("MCP safe provider diagnostic error_type=%T", err)
		} else {
			t.Logf("MCP safe provider diagnostic tool_calls=%d", calls)
		}
		diagnostic.diagnoseResult(t, count, raw, state)
	}
	var count int
	var raw, state string
	for {
		err := v.admin.QueryRow(ctx, `SELECT count(*),COALESCE(min(id),''),COALESCE(min(status),'') FROM agenteam_execution.executions WHERE project_id=$1 AND launch_request->'trigger'->>'task_id'=$2`, v.a.String(), task.ID.String()).Scan(&count, &raw, &state)
		if err != nil || count > 1 || count == 1 && ec.Status(state).Terminal() && state != string(ec.Succeeded) {
			mcpM3SafeCause(t, err)
			diagnose(count, raw, state)
			workAppCheck(t, "mcp-agent-observation", err)
			t.Fatalf("mcp-stage=agent-terminal count=%d state=%s", count, state)
		}
		if count == 1 && state == string(ec.Succeeded) {
			execution, err := f.ParseID[id.Execution](raw)
			workAppCheck(t, "mcp-agent-execution-id", err)
			var view ec.TaskRuntimeView
			mcpM3HTTP(t, v, "agent-runtime-view", http.MethodGet, taskExecutionReadPath(v.a, task.ID.String(), execution)+"/runtime-view", nil, "", http.StatusOK, &view)
			workAppCheck(t, "mcp-agent-runtime-contract", view.ValidateProjection())
			if view.Execution.ID != execution || view.Execution.AgentID != v.agent || view.Snapshot == nil || view.Snapshot.Schema != ec.DurableFinalTextV2 || len(view.Snapshot.Items) != 1 || view.Snapshot.Items[0].Data.Content != mcpM3FinalText {
				t.Fatal("mcp-stage=agent-runtime-view final projection mismatch")
			}
			return execution
		}
		select {
		case <-ctx.Done():
			diagnose(count, raw, state)
			t.Fatal("mcp-stage=agent-timeout original 40s budget exhausted")
		case <-ticker.C:
		}
	}
}

// The cause may contain provider data, SQL or credentials. Traverse only the
// original error graph's types and closed Fault enums, with a fixed bound.
func mcpM3SafeCause(t *testing.T, original error) {
	t.Helper()
	queue := []error{original}
	for n := 0; len(queue) > 0 && n < 8; n++ {
		err := queue[0]
		queue = queue[1:]
		if err == nil {
			continue
		}
		if fault, ok := err.(*f.Fault); ok && fault != nil {
			t.Logf("MCP original cause depth=%d type=%T code=%s state=%s", n, err, fault.Code.Safe(), fault.CommitState.Safe())
		} else {
			t.Logf("MCP original cause depth=%d type=%T cancelled=%t deadline=%t", n, err, errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded))
		}
		if joined, ok := err.(interface{ Unwrap() []error }); ok {
			children := joined.Unwrap()
			queue = append(slices.Clone(children[:min(len(children), 8-len(queue))]), queue...)
		} else if next := errors.Unwrap(err); next != nil {
			queue = append([]error{next}, queue...)
		}
	}
}

type mcpM3CanonicalText struct {
	File struct {
		Kind      string `json:"kind"`
		ProjectID string `json:"project_id"`
		FileID    string `json:"file_id"`
	} `json:"file_ref"`
	Text      string `json:"text"`
	URI       string `json:"uri"`
	SHA256    string `json:"sha256"`
	Truncated bool   `json:"truncated"`
}

func mcpM3CheckAgentFacts(t *testing.T, v *executionSchedulerApp, peer *mcpfixture.Provider, network *netfixture.Descriptor, scenario string, execution id.ExecutionID, echo string, resourceLink bool) {
	t.Helper()
	count, err := peer.CallCount()
	workAppCheck(t, "mcp-actual-tool-call-count", err)
	if count != 1 {
		t.Fatalf("mcp-stage=actual-tool-calls count=%d expected=1", count)
	}
	state, err := network.State(v.ctx, scenario)
	workAppCheck(t, "mcp-model-wire-state", err)
	if len(state.Requests) != 4 || state.ActiveHandlers != 0 || state.CompletedHandlers != 4 {
		t.Fatalf("mcp-stage=model-wire requests=%d active=%d completed=%d", len(state.Requests), state.ActiveHandlers, state.CompletedHandlers)
	}
	for connection := range state.Connections {
		if !state.Closed[connection] {
			t.Fatal("mcp-stage=model-wire unjoined socket")
		}
	}
	// Decode the wire names explicitly; this is the actual successor request,
	// not a synthetic final answer or a domain-only canonical reference.
	var wire struct {
		Messages []struct {
			Role       string          `json:"role"`
			ToolCallID string          `json:"tool_call_id"`
			Content    json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	workAppCheck(t, "mcp-successor-wire", json.Unmarshal([]byte(state.Requests[3].Body), &wire))
	results := map[string]string{}
	for _, message := range wire.Messages {
		if message.Role != "tool" {
			continue
		}
		var parts []struct{ Type, Text string }
		workAppCheck(t, "mcp-successor-result", json.Unmarshal(message.Content, &parts))
		if len(parts) != 1 || parts[0].Type != "text" {
			t.Fatal("mcp-stage=successor-result missing original text part")
		}
		results[message.ToolCallID] = parts[0].Text
	}
	if !resourceLink && results["mcp_owned_0"] != "provider:"+echo || !strings.Contains(results["mcp_owned_1"], "repo://owned/data") {
		t.Fatal("mcp-stage=successor-result original echo/catalog bytes absent")
	}
	var projected mcpM3CanonicalText
	if err := json.Unmarshal([]byte(results["mcp_owned_2"]), &projected); err != nil {
		// Only the fixed local error vocabulary is printable. Never log the
		// provider body, model transcript, SQL arguments or credential bytes.
		code := "not_local_failure_code"
		switch results["mcp_owned_2"] {
		case "mcp_invalid_request", "mcp_invalid_response", "mcp_transport_error", "mcp_authentication_required", "mcp_protocol_version_unsupported", "mcp_protocol_error", "mcp_capability_unsupported", "mcp_content_too_large", "mcp_timeout", "mcp_cancelled", "mcp_sensitive_response", "mcp_dependency_unavailable":
			code = results["mcp_owned_2"]
		}
		var pending, ready, failed, content int
		workAppCheck(t, "mcp-resource-diagnostic-materialization", v.admin.QueryRow(v.ctx, `SELECT count(*) FILTER (WHERE state='pending'),count(*) FILTER (WHERE state='ready'),count(*) FILTER (WHERE state='failed') FROM agenteam_mcp.materializations WHERE execution_id=$1`, execution.String()).Scan(&pending, &ready, &failed))
		workAppCheck(t, "mcp-resource-diagnostic-content", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_mcp.materialized_contents c JOIN agenteam_mcp.materializations m ON m.id=c.materialization_id WHERE m.execution_id=$1`, execution.String()).Scan(&content))
		t.Fatalf("mcp-stage=successor-resource safe_code=%s pending=%d ready=%d failed=%d contents=%d", code, pending, ready, failed, content)
	}
	if projected.Text != "real MCP resource bytes" || projected.URI != "repo://owned/data" || projected.File.Kind != "mcp_file" || projected.File.ProjectID != v.a.String() || projected.SHA256 != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("real MCP resource bytes"))) || projected.Truncated {
		t.Fatal("mcp-stage=successor-resource actual canonical text/ref absent")
	}
	var content, operations, leases, secretLeases int
	var success, retired, released bool
	workAppCheck(t, "mcp-canonical-content", v.admin.QueryRow(v.ctx, `SELECT count(*) FROM agenteam_mcp.materialized_contents c JOIN agenteam_mcp.materializations m ON m.id=c.materialization_id WHERE m.execution_id=$1 AND m.project_id=$2 AND m.state='ready' AND c.content_id=$3 AND c.byte_size=$4 AND c.uri=$5`, execution.String(), v.a.String(), projected.File.FileID, len(projected.Text), projected.URI).Scan(&content))
	workAppCheck(t, "mcp-unified-operation-facts", v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(bool_and(phase='succeeded' AND outcome='success'),false) FROM agenteam_tool.operations WHERE execution_id=$1`, execution.String()).Scan(&operations, &success))
	workAppCheck(t, "mcp-runtime-retirement", v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(bool_and(retired),false) FROM agenteam_mcp.runtime_leases WHERE execution_id=$1`, execution.String()).Scan(&leases, &retired))
	workAppCheck(t, "mcp-secret-retirement", v.admin.QueryRow(v.ctx, `SELECT count(*),COALESCE(bool_and(released),false) FROM agenteam_secret.secret_leases WHERE owner_kind='execution' AND owner_id=$1::uuid AND consumer='mcp'`, execution.String()).Scan(&secretLeases, &released))
	if content != 1 || operations != 3 || !success || leases != 1 || !retired || secretLeases != 1 || !released {
		t.Fatalf("mcp-stage=durable-facts contents=%d operations=%d success=%t runtime_leases=%d retired=%t secret_leases=%d released=%t", content, operations, success, leases, retired, secretLeases, released)
	}
	if resourceLink {
		// The first remote Tool returned only a ResourceLink. Its successor
		// wire must contain a second, independently materialized canonical
		// reference and the actual read bytes, never the link metadata alone.
		builtinFile := projected.File.FileID
		projected = mcpM3CanonicalText{}
		workAppCheck(t, "mcp-link-successor-projection", json.Unmarshal([]byte(results["mcp_owned_0"]), &projected))
		if projected.Text != "real MCP resource bytes" || projected.URI != "repo://owned/data" || projected.File.Kind != "mcp_file" || projected.File.ProjectID != v.a.String() || projected.File.FileID == builtinFile || projected.SHA256 != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("real MCP resource bytes"))) || projected.Truncated {
			t.Fatal("mcp-stage=link-successor canonical resource bytes/ref absent")
		}
		var ready, linked int
		workAppCheck(t, "mcp-link-canonical-content", v.admin.QueryRow(v.ctx, `SELECT count(*) FILTER(WHERE m.state='ready'),count(*) FILTER(WHERE m.state='ready' AND c.content_id=$2 AND c.uri=$3 AND c.byte_size=$4) FROM agenteam_mcp.materialized_contents c JOIN agenteam_mcp.materializations m ON m.id=c.materialization_id WHERE m.execution_id=$1`, execution.String(), projected.File.FileID, projected.URI, len(projected.Text)).Scan(&ready, &linked))
		if ready != 2 || linked != 1 {
			t.Fatalf("mcp-stage=link-canonical-facts ready=%d exact_link_content=%d", ready, linked)
		}
	}
}
