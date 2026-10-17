//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strconv"
	"strings"
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

// This independent review traverses the real App, management HTTP, PostgreSQL,
// Secret leases and verified TLS. Only provider DNS and responses are fixtures.
// Management probes do not establish a general rerank/image consumer or Artifact
// pipeline. The unchanged chat/embedding wire and Unknown cases are not rerun.
func TestModelFourTypeManagementIndependentReview(t *testing.T) {
	nonce := guardID[struct{}](t).String()
	host := "four-types-" + nonce + ".fixture.test"
	secret := "owned-four-types-" + guardID[struct{}](t).String()
	security := previewCertificate(t, t.TempDir(), host)
	addresses, err := net.InterfaceAddrs()
	previewCheck(t, "four-type private interface", err)
	var private netip.Addr
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr().Is4() && outbound.Classify(prefix.Addr()) == outbound.Private {
			private = prefix.Addr()
			break
		}
	}
	if !private.IsValid() {
		t.Fatal("owned private interface required")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(private.String(), "0"))
	previewCheck(t, "four-type provider listener", err)
	port := listener.Addr().(*net.TCPAddr).Port
	origin := "https://" + net.JoinHostPort(host, strconv.Itoa(port))
	prefix := "/owned/" + nonce
	imageURL := origin + prefix + "/result-must-not-be-fetched"
	var requests, reranks, images, downloads, closed atomic.Int64
	var invalid atomic.Bool
	var largeImage atomic.Bool
	provider := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == prefix+"/result-must-not-be-fetched" {
			downloads.Add(1)
			t.Error("management probe fetched a returned image URL")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.TLS == nil || r.Host != net.JoinHostPort(host, strconv.Itoa(port)) || r.Header.Get("Authorization") != "Bearer "+secret || r.Header.Get("Proxy-Authorization") != "" || r.URL.RawQuery != "" || r.Method != http.MethodPost {
			t.Error("provider lost TLS, fixed target or credential separation")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		body, readErr := io.ReadAll(io.LimitReader(r.Body, 4097))
		defer clear(body)
		var payload map[string]any
		if readErr != nil || len(body) > 4096 || json.Unmarshal(body, &payload) != nil {
			t.Error("management payload was not bounded JSON")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.EscapedPath() {
		case prefix + "/rerank":
			reranks.Add(1)
			want := map[string]any{"model": "owned-reranker", "query": "Which document mentions a cat?", "documents": []any{"A cat is resting.", "A tree is growing."}, "top_n": float64(1), "return_documents": false}
			if !reflect.DeepEqual(payload, want) {
				t.Error("rerank did not use the exact fixed five-field probe")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			index := 0
			if invalid.Load() {
				index = 2 // Outside the two submitted documents.
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{map[string]any{"index": index, "relevance_score": 0.9, "document": map[string]any{"text": secret}}}})
		case prefix + "/images/generations":
			images.Add(1)
			want := map[string]any{"model": "owned-image_generation", "prompt": "A plain blue circle on a white background.", "n": float64(1)}
			if !reflect.DeepEqual(payload, want) {
				t.Error("image generation did not use the exact fixed three-field probe")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			item := map[string]any{"revised_prompt": secret}
			if largeImage.Load() {
				// Valid nonempty standard base64 exceeding the old response cap.
				// Management validates the encoding, not an Artifact image codec.
				item["b64_json"] = strings.Repeat("AQID", (1<<20)/4+1)
			} else if !invalid.Load() {
				item["url"] = imageURL
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{item}})
		default:
			t.Error("management contacted an unowned provider route")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	_ = provider.Listener.Close()
	provider.Listener = listener
	provider.TLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{security.Certificate}}
	provider.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closed.Add(1)
		}
	}
	provider.StartTLS()
	t.Cleanup(provider.Close)
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
	call := func(method, path, idempotency string, input any, status int) []byte {
		t.Helper()
		raw := browser.call(t, method, "/api/v1/system/"+path, idempotency, input, nil, status)
		independentProxyNoSecret(t, raw, secret, imageURL)
		return raw
	}
	call(http.MethodPut, "outbound-policy", key(), map[string]any{
		"expected_version": "1", "rule_mode": "allowlist",
		"rules": []any{map[string]any{"target": private.String() + "/32", "ports": []int{port}}},
	}, http.StatusOK)
	var capabilities struct {
		Protocols []struct {
			Protocol   string   `json:"protocol"`
			ModelTypes []string `json:"model_types"`
			Discovery  bool     `json:"discovery"`
			Test       bool     `json:"test"`
		} `json:"protocols"`
	}
	previewCheck(t, "four-type advertised capability", json.Unmarshal(call(http.MethodGet, "model-capabilities", "", nil, 200), &capabilities))
	if len(capabilities.Protocols) != 1 || capabilities.Protocols[0].Protocol != "openai-compatible" || !capabilities.Protocols[0].Discovery || !capabilities.Protocols[0].Test || !reflect.DeepEqual(capabilities.Protocols[0].ModelTypes, []string{"chat", "embedding", "reranker", "image_generation"}) {
		t.Fatal("management capability did not advertise the four supported types")
	}
	var credential struct {
		ID string `json:"credential_id"`
	}
	previewCheck(t, "four-type credential creation", json.Unmarshal(call(http.MethodPost, "model-credentials", key(), map[string]string{"value": secret}, 200), &credential))
	if credential.ID == "" {
		t.Fatal("model credential has no identity")
	}
	var receipt mc.CommandReceipt
	previewCheck(t, "four-type provider save", json.Unmarshal(call(http.MethodPost, "model-providers", key(), map[string]any{"input": map[string]any{
		"name": "Independent four-type provider", "protocol": "openai-compatible", "base_url": origin + prefix,
		"enabled": true, "credential_ref": credential.ID, "options": map[string]any{},
	}}, 200), &receipt))
	providerID := receipt.ResourceID
	if providerID == "" || receipt.Version.String() != "1" {
		t.Fatal("provider was not durably created at version one")
	}
	models := make(map[string]string)
	for _, kind := range []string{"chat", "embedding", "reranker", "image_generation"} {
		input := fourTypeReviewInput(kind)
		previewCheck(t, "save model type "+kind, json.Unmarshal(call(http.MethodPost, "models", key(), map[string]any{"provider_id": providerID, "input": input}, 200), &receipt))
		modelID := receipt.ResourceID
		if modelID == "" || receipt.Version.String() != "1" {
			t.Fatal("model was not durably created", kind)
		}
		models[kind] = modelID
		path := "models/" + modelID
		before := call(http.MethodGet, path, "", nil, 200)
		var view struct {
			ProviderID string         `json:"provider_id"`
			Version    string         `json:"version"`
			Input      map[string]any `json:"input"`
		}
		previewCheck(t, "read model type "+kind, json.Unmarshal(before, &view))
		got, err := json.Marshal(view.Input)
		previewCheck(t, "encode canonical model input", err)
		want, err := json.Marshal(input)
		previewCheck(t, "encode submitted model input", err)
		if view.ProviderID != providerID || view.Version != "1" || !bytes.Equal(got, want) {
			t.Fatal("canonical persisted model capabilities changed", kind)
		}
		bad := fourTypeReviewInput(kind)
		caps := bad["capabilities"].(map[string]any)
		switch kind {
		case "chat":
			caps["output_modalities"] = []string{"vector"}
		case "embedding", "reranker":
			caps["streaming"] = true
		case "image_generation":
			caps["context_length"] = "1"
		}
		call(http.MethodPut, path, key(), map[string]any{"expected_version": "1", "input": bad}, http.StatusUnprocessableEntity)
		if !bytes.Equal(before, call(http.MethodGet, path, "", nil, 200)) {
			t.Fatal("rejected capability mutation changed the stored model", kind)
		}
	}
	// Migration 150 expands the family while preserving the native protocol/type
	// relation. Both values below are individually valid, so this must reach the
	// cross-table check rather than a scalar enum or foreign-key failure.
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		embeddingPath := "models/" + models["embedding"]
		before := call(http.MethodGet, embeddingPath, "", nil, 200)
		nativeID := key()
		_, err := a.store.Exec(ctx, `INSERT INTO agenteam_model.providers(id,scope,project_id,name,protocol,base_url,enabled,credential_id,provider_options,version,created_at,updated_at)
 SELECT $2,scope,project_id,'Independent native constraint fixture','openai-chat-completions',base_url,enabled,NULL,provider_options,1,created_at,updated_at FROM agenteam_model.providers WHERE id=$1`, providerID, nativeID)
		previewCheck(t, "native provider constraint fixture", err)
		_, err = a.store.Exec(ctx, `UPDATE agenteam_model.models SET provider_id=$2 WHERE id=$1`, models["embedding"], nativeID)
		var constraint *pgconn.PgError
		if !errors.As(err, &constraint) || constraint.Code != "23514" || constraint.ConstraintName != "models_provider_type_check" {
			t.Fatal("native Chat-only provider admitted an embedding or rejected it at the wrong constraint")
		}
		_, err = a.store.Exec(ctx, `DELETE FROM agenteam_model.providers WHERE id=$1`, nativeID)
		previewCheck(t, "remove native constraint fixture", err)
		if !bytes.Equal(before, call(http.MethodGet, embeddingPath, "", nil, 200)) {
			t.Fatal("native protocol/type refusal changed the original family model")
		}
	}()
	var selection struct {
		ID         string         `json:"id"`
		Version    string         `json:"version"`
		Configured map[string]any `json:"configured"`
	}
	previewCheck(t, "initial four-purpose selection", json.Unmarshal(call(http.MethodGet, "model-selection", "", nil, 200), &selection))
	if selection.ID == "" || selection.Version != "1" || selection.Configured != nil {
		t.Fatal("isolated database had an invented default selection")
	}
	wantSelection := make(map[string]any)
	for role, id := range map[string]string{"summary": models["chat"], "embedding": models["embedding"], "reranker": models["reranker"], "image": models["image_generation"]} {
		wantSelection[role] = map[string]any{"model_id": id, "reasoning": map[string]any{"mode": "default"}}
	}
	selectionInput := map[string]any{"id": selection.ID, "expected_version": "1"}
	for role, id := range wantSelection {
		selectionInput[role] = id
	}
	call(http.MethodPut, "model-selection", key(), selectionInput, 200)
	previewCheck(t, "saved four-purpose selection", json.Unmarshal(call(http.MethodGet, "model-selection", "", nil, 200), &selection))
	if selection.Version != "2" || !reflect.DeepEqual(selection.Configured, wantSelection) {
		t.Fatal("four-purpose selection did not persist all exact model identities")
	}
	if requests.Load() != 0 {
		t.Fatal("configuration or selection contacted the provider")
	}
	// The first two exchanges are successes; the next two have structurally
	// invalid protocol results. Replays of every result must perform no I/O.
	testInput := map[string]string{"expected_version": "1", "provider_version": "1"}
	keys := make(map[string]string)
	for _, badResponse := range []bool{false, true} {
		invalid.Store(badResponse)
		for _, kind := range []string{"reranker", "image_generation"} {
			path, operationKey := "models/"+models[kind]+"/test", key()
			keys[kind] = operationKey
			before := requests.Load()
			raw := call(http.MethodPost, path, operationKey, testInput, 200)
			var result model.ManagementResult
			previewCheck(t, "decode real "+kind+" probe", json.Unmarshal(raw, &result))
			maxDuration := int64(20000)
			if kind == "image_generation" {
				maxDuration = 120000
			}
			if result.OperationID == "" || result.ProviderID != providerID || result.ModelID != models[kind] || result.ModelVersion.String() != "1" || result.ProviderVersion.String() != "1" || result.Items != nil || result.DurationMS < 0 || result.DurationMS > maxDuration || result.CreatedAt.Validate() != nil {
				t.Fatal("management test returned an invalid safe receipt", kind)
			}
			if badResponse {
				if result.Outcome != "failed" || result.Reason == nil || *result.Reason != "invalid_response" {
					t.Fatal("invalid provider result did not fail safely", kind)
				}
			} else if result.Outcome != "succeeded" || result.Reason != nil {
				reason := ""
				if result.Reason != nil {
					reason = *result.Reason
				}
				t.Fatal("real provider result was not accepted", kind, result.Outcome, reason)
			}
			if !bytes.Equal(raw, call(http.MethodPost, path, operationKey, testInput, 200)) || requests.Load() != before+1 {
				t.Fatal("same-key probe replay changed receipt or repeated provider I/O", kind)
			}
		}
	}
	largeImage.Store(true)
	largeKey := key()
	largePath := "models/" + models["image_generation"] + "/test"
	largeReceipt := call(http.MethodPost, largePath, largeKey, testInput, 200)
	var largeResult model.ManagementResult
	previewCheck(t, "decode image above former response limit", json.Unmarshal(largeReceipt, &largeResult))
	if largeResult.Outcome != "succeeded" || largeResult.Reason != nil || largeResult.Items != nil || largeResult.ModelID != models["image_generation"] || largeResult.DurationMS < 0 || largeResult.DurationMS > 120000 || !bytes.Equal(largeReceipt, call(http.MethodPost, largePath, largeKey, testInput, 200)) {
		t.Fatal("bounded image above one MiB did not succeed and replay exactly")
	}
	if requests.Load() != 5 || reranks.Load() != 2 || images.Load() != 3 || downloads.Load() != 0 {
		t.Fatal("new probes used an unexpected route, count or image download")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var completed, released, active int
	previewCheck(t, "four-type credential actual release", a.store.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_model.management_operations WHERE credential_id=$1 AND result IS NOT NULL),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND owner_kind='model_management_request' AND released),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND NOT released)`, credential.ID).Scan(&completed, &released, &active))
	if completed != 5 || released != 5 || active != 0 {
		t.Fatal("management results did not release their real credential leases", completed, released, active)
	}
	// Change only the authority of this owned session after completed receipts.
	// Both historical replay and a fresh invocation must recheck current access.
	_, err = a.store.Exec(ctx, `UPDATE agenteam_account.users SET role='user' WHERE id=$1`, browser.user)
	previewCheck(t, "revoke owned system administrator", err)
	for _, kind := range []string{"reranker", "image_generation"} {
		path := "models/" + models[kind] + "/test"
		call(http.MethodPost, path, keys[kind], testInput, http.StatusForbidden)
		call(http.MethodPost, path, key(), testInput, http.StatusForbidden)
	}
	if requests.Load() != 5 {
		t.Fatal("revoked administrator triggered provider I/O")
	}
	independentProxyNoSecret(t, []byte(a.logs.String()), secret, imageURL, browser.cookie, browser.csrf)
	a.signals <- syscall.SIGTERM
	await(t, a.done)
	if a.err != nil || !a.owned.accounts().Joined() {
		t.Fatal("four-type App did not actually join", a.err)
	}
	provider.Close()
	if closed.Load() != 5 {
		t.Fatal("owned provider connections did not actually close", closed.Load())
	}
}

func fourTypeReviewInput(kind string) map[string]any {
	outputs := []string{}
	structured := []string{}
	switch kind {
	case "chat":
		outputs = []string{"text"}
		structured = []string{"text", "json_schema"}
	case "embedding":
		outputs = []string{"vector"}
	case "image_generation":
		outputs = []string{"image"}
	}
	return map[string]any{
		"notes": "Independent " + kind, "provider_model_id": "owned-" + kind, "type": kind, "enabled": true,
		"parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{},
		"capabilities": map[string]any{
			"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false,
			"input_modalities": []string{"text"}, "output_modalities": outputs, "reasoning_efforts": []any{}, "reasoning_toggle_mode": nil,
			"structured_output_modes": structured, "context_length": nil, "max_output": nil,
		},
	}
}
