//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strconv"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/jackc/pgx/v5/pgconn"
)

// This author integration test uses the formal App, PG, Object ProcessGuard,
// management HTTP API, Secret leases and outbound proxy. Only provider DNS and
// CA facts are supplied by the owned fixture; no authority or transaction is
// replaced. It requires the isolated dependency chain from test-security.sh.
func TestModelManagementProxyReplayAndCredentialDeletion(t *testing.T) {
	nonce := guardID[struct{}](t).String()
	host := "management-" + nonce + ".fixture.test"
	providerSecret := "owned-provider-" + guardID[struct{}](t).String()
	proxySecret := "owned-proxy-" + guardID[struct{}](t).String()
	security := previewCertificate(t, t.TempDir(), host)
	addresses, err := net.InterfaceAddrs()
	previewCheck(t, "owned management provider interface", err)
	var private netip.Addr
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr().Is4() && outbound.Classify(prefix.Addr()) == outbound.Private {
			private = prefix.Addr()
			break
		}
	}
	if !private.IsValid() {
		t.Fatal("owned private interface required for management provider")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(private.String(), "0"))
	previewCheck(t, "owned management provider listener", err)
	port := listener.Addr().(*net.TCPAddr).Port
	origin := "https://" + net.JoinHostPort(host, strconv.Itoa(port))
	prefix := "/owned/" + nonce
	const providerModel = "owned-management-chat"
	var requests, discoveries, tests, closed atomic.Int64
	var cancelNext atomic.Bool
	var cancelledPeer atomic.Value
	bodyReady, handlerJoined, releaseBody := make(chan struct{}), make(chan struct{}), make(chan struct{})
	socketClosed := make(chan struct{}, 1)
	provider := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.TLS == nil || r.Host != net.JoinHostPort(host, strconv.Itoa(port)) || r.Header.Get("Authorization") != "Bearer "+providerSecret || r.Header.Get("Proxy-Authorization") != "" || r.URL.RawQuery != "" {
			t.Error("provider lost verified TLS, original host or credential separation")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.EscapedPath() {
		case "GET " + prefix + "/models":
			discoveries.Add(1)
			_, _ = io.WriteString(w, `{"data":[{"id":"`+providerModel+`"}]}`)
		case "POST " + prefix + "/chat/completions":
			tests.Add(1)
			body, err := io.ReadAll(io.LimitReader(r.Body, 4097))
			defer clear(body)
			var payload map[string]any
			if err != nil || len(body) > 4096 || json.Unmarshal(body, &payload) != nil || len(payload) != 4 || payload["model"] != providerModel || payload["max_completion_tokens"] != float64(16) || payload["stream"] != false || !reflect.DeepEqual(payload["messages"], []any{map[string]any{"role": "user", "content": "Reply OK."}}) {
				t.Error("management test did not use the fixed bounded chat request")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if cancelNext.CompareAndSwap(true, false) {
				defer close(handlerJoined)
				cancelledPeer.Store(r.RemoteAddr)
				_, _ = io.WriteString(w, `{"choices":[`)
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Error("owned provider could not flush the cancellation body barrier")
					return
				}
				close(bodyReady)
				select {
				case <-r.Context().Done():
				case <-releaseBody:
				}
				return
			}
			_, _ = io.WriteString(w, `{"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`)
		default:
			t.Error("management contacted an unowned provider route")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	_ = provider.Listener.Close()
	provider.Listener = listener
	provider.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{security.Certificate}}
	provider.Config.ConnState = func(conn net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed.Add(1)
			if peer, ok := cancelledPeer.Load().(string); ok && peer == conn.RemoteAddr().String() {
				select {
				case socketClosed <- struct{}{}:
				default:
				}
			}
		}
	}
	provider.StartTLS()
	t.Cleanup(func() { close(releaseBody); provider.Close() })
	proxyAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("owned-management:"+proxySecret))
	gateway := startRootRouteGateway(t, listener.Addr().String(), proxyAuth)
	a := newModelRootApp(t, "3s", func(a *modelRootApp, deps *dependencies) {
		a.cfg = guardConfiguration(t, append(guardEnvironment(t, a.db),
			"AGENTEAM_CENTRAL_PUBLIC_ORIGIN=http://localhost:8080",
			"AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=3s",
			"AGENTEAM_CENTRAL_OUTBOUND_CA_FILE="+security.CAFile))
		deps.outboundConstruct = func(_ context.Context, _ config.Config, runtime *outboundRuntime) error {
			runtime.resolver = managementProcessResolver{host: host, address: private}
			return nil
		}
	})
	browser := independentProxyLogin(t, a)
	key := func() string { return guardID[struct{}](t).String() }
	call := func(method, path, idempotency string, input any) []byte {
		t.Helper()
		raw := browser.call(t, method, "/api/v1/system/"+path, idempotency, input, nil, http.StatusOK)
		independentProxyNoSecret(t, raw, providerSecret, proxySecret, proxyAuth)
		return raw
	}
	call(http.MethodPut, "outbound-policy", key(), map[string]any{
		"expected_version": "1", "rule_mode": "allowlist",
		"rules": []any{map[string]any{"target": private.String() + "/32", "ports": []int{port}}},
	})
	gatewayAddress := gateway.listener.Addr().(*net.TCPAddr)
	call(http.MethodPut, "outbound-policy/proxy", key(), map[string]any{
		"expected_version": "1", "mode": "proxy", "credential_action": "keep", "password": proxySecret,
		"gateway": map[string]any{"protocol": "http", "host": gatewayAddress.IP.String(), "port": gatewayAddress.Port, "username": "owned-management"},
	})
	var credential struct {
		ID      string `json:"credential_id"`
		Version string `json:"version"`
	}
	previewCheck(t, "create model credential", json.Unmarshal(call(http.MethodPost, "model-credentials", key(), map[string]string{"value": providerSecret}), &credential))
	if credential.ID == "" || credential.Version != "1" {
		t.Fatal("formal model credential creation returned no current identity")
	}
	var receipt mc.CommandReceipt
	previewCheck(t, "save management provider", json.Unmarshal(call(http.MethodPost, "model-providers", key(), map[string]any{"input": map[string]any{
		"name": "Owned management provider", "protocol": mc.OpenAICompatible, "base_url": origin + prefix,
		"enabled": true, "credential_ref": credential.ID, "options": map[string]any{},
	}}), &receipt))
	providerID, providerVersion := receipt.ResourceID, receipt.Version.String()
	if providerID == "" || providerVersion != "1" {
		t.Fatal("provider was not saved at its initial version")
	}
	previewCheck(t, "save management model", json.Unmarshal(call(http.MethodPost, "models", key(), map[string]any{
		"provider_id": providerID, "input": map[string]any{
			"name": "Owned management chat", "provider_model_id": providerModel, "type": mc.ChatModel, "enabled": true,
			"parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{},
			"capabilities": map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false,
				"input_modalities": []string{"text"}, "output_modalities": []string{"text"}, "reasoning_efforts": []string{},
				"structured_output_modes": []string{}, "context_length": nil, "max_output": nil},
		},
	}), &receipt))
	modelID, modelVersion := receipt.ResourceID, receipt.Version.String()
	if modelID == "" || modelVersion != "1" {
		t.Fatal("model was not saved at its initial version")
	}
	previewCheck(t, "save family embedding model", json.Unmarshal(call(http.MethodPost, "models", key(), map[string]any{
		"provider_id": providerID, "input": map[string]any{
			"name": "Owned management embedding", "provider_model_id": "owned-management-embedding", "type": mc.EmbeddingModel, "enabled": true,
			"parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{},
			"capabilities": map[string]any{"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false,
				"input_modalities": []string{"text"}, "output_modalities": []string{"vector"}, "reasoning_efforts": []string{},
				"structured_output_modes": []string{}, "context_length": nil, "max_output": nil},
		},
	}), &receipt))
	embeddingID, embeddingVersion := receipt.ResourceID, receipt.Version.String()
	if embeddingID == "" || embeddingID == modelID || embeddingVersion != "1" {
		t.Fatal("family embedding model was not saved as a separate initial record")
	}
	refusedDeletion := browser.call(t, http.MethodDelete, "/api/v1/system/model-providers/"+providerID, key(), map[string]string{"expected_version": providerVersion}, nil, http.StatusConflict)
	independentProxyNoSecret(t, refusedDeletion, providerSecret, proxySecret, proxyAuth)
	var deletionFault struct {
		Code string `json:"code"`
	}
	previewCheck(t, "decode referenced family provider refusal", json.Unmarshal(refusedDeletion, &deletionFault))
	if deletionFault.Code != "INVALID_STATE" {
		t.Fatal("referenced family provider deletion did not report its live models")
	}
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		expectConstraint := func(code, constraint, statement string, args ...any) {
			t.Helper()
			_, err := a.store.Exec(ctx, statement, args...)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != code || pgerr.ConstraintName != constraint {
				t.Fatalf("family database mutation did not fail with SQLSTATE %s constraint %s", code, constraint)
			}
		}
		// All four types are valid for the gateway family. Preserve the original
		// cross-table invariant with a valid embedding and a Chat-only provider.
		nativeProviderID := key()
		_, err := a.store.Exec(ctx, `INSERT INTO agenteam_model.providers(id,scope,project_id,name,protocol,base_url,enabled,credential_id,provider_options,version,created_at,updated_at)
 SELECT $2,scope,project_id,'Native constraint fixture','openai-chat-completions',base_url,enabled,NULL,provider_options,1,created_at,updated_at FROM agenteam_model.providers WHERE id=$1`, providerID, nativeProviderID)
		previewCheck(t, "create native protocol constraint fixture", err)
		expectConstraint("23514", "models_provider_type_check", `UPDATE agenteam_model.models SET provider_id=$2 WHERE id=$1`, embeddingID, nativeProviderID)
		_, err = a.store.Exec(ctx, `DELETE FROM agenteam_model.providers WHERE id=$1`, nativeProviderID)
		previewCheck(t, "remove native protocol constraint fixture", err)
		expectConstraint("23514", "providers_protocol_immutable", `UPDATE agenteam_model.providers SET protocol='openai-chat-completions' WHERE id=$1`, providerID)
		expectConstraint("23503", "models_provider_id_fkey", `UPDATE agenteam_model.models SET provider_id=$2 WHERE id=$1`, embeddingID, key())
		var protocol, storedCredential string
		var version int64
		var children int
		var chatIntact, embeddingIntact bool
		previewCheck(t, "family records survive refused mutations", a.store.QueryRow(ctx, `SELECT p.protocol,p.credential_id::text,p.version,
 (SELECT count(*) FROM agenteam_model.models WHERE provider_id=p.id),
 EXISTS(SELECT 1 FROM agenteam_model.models WHERE id=$2 AND provider_id=p.id AND type='chat' AND version=1),
 EXISTS(SELECT 1 FROM agenteam_model.models WHERE id=$3 AND provider_id=p.id AND type='embedding' AND version=1)
 FROM agenteam_model.providers p WHERE p.id=$1`, providerID, modelID, embeddingID).Scan(&protocol, &storedCredential, &version, &children, &chatIntact, &embeddingIntact))
		if protocol != string(mc.OpenAICompatible) || storedCredential != credential.ID || version != 1 || children != 2 || !chatIntact || !embeddingIntact {
			t.Fatal("refused family mutations changed the provider, credential reference or model records")
		}
	}()
	if requests.Load() != 0 || gateway.connects.Load() != 0 {
		t.Fatal("saving or validating family models unexpectedly contacted the provider")
	}
	discoveryPath, testPath := "model-providers/"+providerID+"/discovery", "models/"+modelID+"/test"
	discoveryBody := map[string]string{"expected_version": providerVersion}
	testBody := map[string]string{"expected_version": modelVersion, "provider_version": providerVersion}
	discoveryKey, testKey := key(), key()
	discoveryReceipt := call(http.MethodPost, discoveryPath, discoveryKey, discoveryBody)
	testReceipt := call(http.MethodPost, testPath, testKey, testBody)
	var discovered, tested model.ManagementResult
	previewCheck(t, "decode discovery receipt", json.Unmarshal(discoveryReceipt, &discovered))
	previewCheck(t, "decode test receipt", json.Unmarshal(testReceipt, &tested))
	if discovered.Outcome != "succeeded" || discovered.Reason != nil || discovered.Items == nil || len(*discovered.Items) != 1 || (*discovered.Items)[0].ProviderModelID != providerModel || discovered.ProviderID != providerID || discovered.OperationID == "" {
		t.Fatal("authenticated proxy discovery failed", discovered.Outcome, discovered.Reason)
	}
	if tested.Outcome != "succeeded" || tested.Reason != nil || tested.Items != nil || tested.ModelID != modelID || tested.OperationID == "" {
		t.Fatal("authenticated proxy model test failed", tested.Outcome, tested.Reason)
	}
	assertTraffic := func(wantTests int64) {
		t.Helper()
		if requests.Load() != wantTests+1 || discoveries.Load() != 1 || tests.Load() != wantTests || gateway.connects.Load() != wantTests+1 {
			t.Fatal("management replay, refusal or cleanup produced extra provider/proxy I/O", requests.Load(), gateway.connects.Load())
		}
	}
	assertTraffic(1)
	if !bytes.Equal(discoveryReceipt, call(http.MethodPost, discoveryPath, discoveryKey, discoveryBody)) || !bytes.Equal(testReceipt, call(http.MethodPost, testPath, testKey, testBody)) {
		t.Fatal("same-key management replay changed its durable receipt")
	}
	assertTraffic(1)
	cancelledKey := key()
	cancelNext.Store(true)
	cancelledReceipt := managementProcessCancelAndReplay(t, browser, testPath, cancelledKey, testBody, bodyReady, handlerJoined, socketClosed)
	independentProxyNoSecret(t, cancelledReceipt, providerSecret, proxySecret, proxyAuth)
	var cancelledResult model.ManagementResult
	previewCheck(t, "decode cancelled management receipt", json.Unmarshal(cancelledReceipt, &cancelledResult))
	if cancelledResult.Outcome != "failed" || cancelledResult.Reason == nil || *cancelledResult.Reason != "cancelled" || cancelledResult.Items != nil || cancelledResult.ModelID != modelID || cancelledResult.ProviderID != providerID || cancelledResult.OperationID == "" {
		t.Fatal("cancelled authenticated management request did not retain a safe terminal receipt")
	}
	if !bytes.Equal(cancelledReceipt, call(http.MethodPost, testPath, cancelledKey, testBody)) {
		t.Fatal("cancelled management replay changed its durable receipt")
	}
	assertTraffic(2)
	call(http.MethodPut, "outbound-policy", key(), map[string]any{"expected_version": "2", "rule_mode": "allowlist", "rules": []any{}})
	var denied model.ManagementResult
	previewCheck(t, "decode policy refusal", json.Unmarshal(call(http.MethodPost, discoveryPath, key(), discoveryBody), &denied))
	if denied.Outcome != "failed" || denied.Reason == nil || *denied.Reason != "policy_rejected" || denied.Items == nil || len(*denied.Items) != 0 {
		t.Fatal("current global rule refusal was not safely reported", denied.Outcome, denied.Reason)
	}
	assertTraffic(2)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var completed, released, active, proxyOperations, proxyReleased int
	previewCheck(t, "management and proxy credential actual release", a.store.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_model.management_operations WHERE credential_id=$1 AND result IS NOT NULL),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND owner_kind='model_management_request' AND released),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind IN ('model_management_request','outbound_proxy') AND NOT released),
 (SELECT count(*) FROM agenteam_outbound.proxy_credential_operations),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE owner_kind='outbound_proxy' AND released)`, credential.ID).Scan(&completed, &released, &active, &proxyOperations, &proxyReleased))
	if completed != 4 || released != 4 || active != 0 || proxyOperations != 0 || proxyReleased != 3 {
		t.Fatal("real management/proxy credentials did not release after actual wire join", completed, released, active, proxyOperations, proxyReleased)
	}
	call(http.MethodDelete, "models/"+modelID, key(), map[string]any{"expected_version": modelVersion, "replacement": nil})
	call(http.MethodDelete, "models/"+embeddingID, key(), map[string]any{"expected_version": embeddingVersion, "replacement": nil})
	call(http.MethodDelete, "model-providers/"+providerID, key(), map[string]string{"expected_version": providerVersion})
	var deleted struct {
		ID      string `json:"credential_id"`
		Deleted bool   `json:"deleted"`
	}
	previewCheck(t, "delete unreferenced model credential", json.Unmarshal(call(http.MethodDelete, "model-credentials/"+credential.ID, key(), map[string]string{"expected_version": credential.Version}), &deleted))
	if !deleted.Deleted || deleted.ID != credential.ID {
		t.Fatal("completed management receipts retained a live credential reference")
	}
	var exists bool
	previewCheck(t, "deleted credential and retained receipt facts", a.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_secret.secrets WHERE id=$1),(SELECT count(*) FROM agenteam_model.management_operations WHERE credential_id=$1 AND result IS NOT NULL)`, credential.ID).Scan(&exists, &completed))
	if exists || completed != 4 {
		t.Fatal("credential deletion destroyed history or failed to remove the credential")
	}
	if !bytes.Equal(discoveryReceipt, call(http.MethodPost, discoveryPath, discoveryKey, discoveryBody)) || !bytes.Equal(testReceipt, call(http.MethodPost, testPath, testKey, testBody)) || !bytes.Equal(cancelledReceipt, call(http.MethodPost, testPath, cancelledKey, testBody)) {
		t.Fatal("historical receipt could not be read after provider/model/credential deletion")
	}
	assertTraffic(2)
	independentProxyNoSecret(t, []byte(a.logs.String()), providerSecret, proxySecret, proxyAuth, browser.cookie, browser.csrf)
	a.signals <- syscall.SIGTERM
	await(t, a.done)
	if a.err != nil || !a.owned.accounts().Joined() {
		t.Fatal("management root runtime did not actually join", a.err)
	}
	gateway.stop(t)
	provider.Close()
	if closed.Load() != 3 {
		t.Fatal("owned provider connections did not actually close", closed.Load())
	}
}

// The first HTTP response is deliberately unreadable after client cancellation.
// Only a subsequent same-key request may establish the durable server outcome.
func managementProcessCancelAndReplay(t *testing.T, browser *independentProxyBrowser, path, key string, input any, bodyReady, handlerJoined, socketClosed <-chan struct{}) []byte {
	t.Helper()
	payload, err := json.Marshal(input)
	previewCheck(t, "encode cancellable management request", err)
	defer clear(payload)
	request := func(ctx context.Context) *http.Request {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, browser.address+"/api/v1/system/"+path, bytes.NewReader(payload))
		previewCheck(t, "construct cancellable management request", err)
		r.Host = "localhost:8080"
		r.Header.Set("Origin", "http://localhost:8080")
		r.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: browser.cookie})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", browser.csrf)
		r.Header.Set("Idempotency-Key", key)
		return r
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	initial := request(ctx)
	returned := make(chan struct{})
	var requestErr error
	go func() {
		defer close(returned)
		response, err := browser.client.Do(initial)
		if response != nil {
			_ = response.Body.Close()
		}
		requestErr = err
	}()
	defer func() {
		cancel()
		select {
		case <-returned:
		case <-time.After(5 * time.Second):
			t.Error("cancelled management HTTP caller did not actually return")
		}
	}()
	select {
	case <-bodyReady:
	case <-returned:
		t.Fatal("management HTTP returned before the owned body barrier")
	case <-ctx.Done():
		t.Fatal("management HTTP did not reach its owned body barrier")
	}
	cancel()
	joined, stopJoin := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopJoin()
	// Observe all three facts without assuming a remote close precedes the
	// client's cancellation return. The original provider socket is identified
	// by its peer, independently of earlier successful requests.
	for _, event := range []<-chan struct{}{returned, handlerJoined, socketClosed} {
		select {
		case <-event:
		case <-joined.Done():
			t.Fatal("cancelled management caller, provider handler or socket did not join")
		}
	}
	if !errors.Is(requestErr, context.Canceled) {
		t.Fatal("original management HTTP caller did not observe cancellation")
	}
	replayCtx, stopReplay := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopReplay()
	for {
		response, err := browser.client.Do(request(replayCtx))
		previewCheck(t, "read cancelled management receipt", err)
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(raw) > 1<<20 || response.Header.Get("Cache-Control") != "no-store" {
			t.Fatal("cancelled management replay was not a complete bounded uncached response")
		}
		if response.StatusCode == http.StatusOK {
			return raw
		}
		var fault struct {
			Code  string `json:"code"`
			State string `json:"commit_state"`
			Retry string `json:"retry_hint"`
		}
		if response.StatusCode != http.StatusServiceUnavailable || json.Unmarshal(raw, &fault) != nil || fault.Code != "COMMIT_UNKNOWN" || fault.State != "unknown" || fault.Retry != "replay" {
			t.Fatal("cancelled management replay returned an unexpected safe fault", response.StatusCode, fault.Code, fault.State)
		}
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-timer.C:
		case <-replayCtx.Done():
			timer.Stop()
			t.Fatal("cancelled management receipt did not become terminal")
		}
	}
}

type managementProcessResolver struct {
	host    string
	address netip.Addr
}

func (r managementProcessResolver) Lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if host != r.host {
		return nil, &net.DNSError{Err: "not the owned management provider", Name: host, IsNotFound: true}
	}
	return []netip.Addr{r.address}, nil
}
