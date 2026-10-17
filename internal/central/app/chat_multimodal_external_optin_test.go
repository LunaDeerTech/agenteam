//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/model"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// This author opt-in sends exactly one management test command through a fresh
// App's real HTTP, Secret, saved proxy and Model transport. It is not a direct
// vendor probe. Run only after the controlled multimodal gates pass, using the
// same compiled App binary and the standard task-owned PG/Object fixture runner.
//
// Required private environment (never put values in argv or repository files):
//
//	AGENTEAM_CHAT_MULTIMODAL_EXTERNAL=one-mixed-request
//	AGENTEAM_CHAT_MULTIMODAL_EXTERNAL_LEDGER=<new absolute file in a 0700 dir>
//	DEV_CHAT_BASE_URL, DEV_CHAT_MODEL, DEV_CHAT_API_KEY, HTTPS_PROXY (or https_proxy)
//
// The exclusive 0600 ledger is consumed immediately before the one test POST,
// after all formal configuration succeeds. A request failure, timeout or unknown
// completion never permits retry. Configuration failure consumes no request.
// The normal test suite skips before reading any private configuration.
func TestChatMultimodalExternalAppOptIn(t *testing.T) {
	if os.Getenv("AGENTEAM_CHAT_MULTIMODAL_EXTERNAL") != "one-mixed-request" {
		t.Skip("explicit one-request external App opt-in required")
	}
	check := func(stage string, err error) {
		t.Helper()
		if err != nil {
			t.Fatal("external App check failed:", stage)
		}
	}
	_, err := pgfixture.Load()
	check("owned PostgreSQL identity", err)
	_, err = objectfixture.Load()
	check("owned Object identity", err)
	base, err := url.Parse(os.Getenv("DEV_CHAT_BASE_URL"))
	check("private provider URL syntax", err)
	if base.Scheme != "https" || base.Hostname() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || strings.HasSuffix(strings.TrimRight(base.Path, "/"), "/chat/completions") {
		t.Fatal("external App requires an HTTPS provider base URL, not an operation URL")
	}
	providerModel, credential := os.Getenv("DEV_CHAT_MODEL"), os.Getenv("DEV_CHAT_API_KEY")
	if providerModel == "" || credential == "" {
		t.Fatal("external App private model configuration is incomplete")
	}
	proxyValue := os.Getenv("HTTPS_PROXY")
	if proxyValue == "" {
		proxyValue = os.Getenv("https_proxy")
	}
	proxy, err := url.Parse(proxyValue)
	check("private proxy URL syntax", err)
	if proxy.Scheme != "http" || proxy.Hostname() == "" || proxy.RawQuery != "" || proxy.Fragment != "" || proxy.Path != "" && proxy.Path != "/" {
		t.Fatal("external App requires the explicitly configured HTTP proxy")
	}
	proxyPort := 80
	if proxy.Port() != "" {
		proxyPort, err = strconv.Atoi(proxy.Port())
		check("proxy port", err)
	}
	if proxyPort < 1 || proxyPort > 65535 {
		t.Fatal("external App proxy port is invalid")
	}
	providerPort := 443
	if base.Port() != "" {
		providerPort, err = strconv.Atoi(base.Port())
		check("provider port", err)
	}
	if providerPort < 1 || providerPort > 65535 {
		t.Fatal("external App provider port is invalid")
	}
	ledgerPath := os.Getenv("AGENTEAM_CHAT_MULTIMODAL_EXTERNAL_LEDGER")
	parent, err := os.Lstat(filepath.Dir(ledgerPath))
	check("private ledger directory", err)
	if !filepath.IsAbs(ledgerPath) || !parent.IsDir() || parent.Mode().Perm() != 0700 {
		t.Fatal("external App ledger requires an absolute new path in a private directory")
	}
	if _, err = os.Lstat(ledgerPath); !os.IsNotExist(err) {
		t.Fatal("external App request ledger already exists or cannot be checked")
	}
	imageMarker, docMarker := externalChatMarker(t, "IMG"), externalChatMarker(t, "DOC")
	picture := externalChatMarkerPNG(t, imageMarker)
	document := knowledgeM3DOCX(t, "The document identifier is "+docMarker+".")
	defer clear(picture)
	defer clear(document)

	// No resolver/CA override: use the real SystemResolver and deployment trust.
	// The inherited proxy URL is deliberately submitted to the saved platform
	// policy; production code continues to ignore environment proxy variables.
	a := newModelRootApp(t, "3s", nil)
	browser := independentProxyLogin(t, a)
	browser.client.Timeout = 130 * time.Second
	browser.client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	call := func(method, path, key string, input any, status int) []byte {
		t.Helper()
		return externalChatAppCall(t, browser, method, path, key, input, status)
	}
	key := func() string { return guardID[struct{}](t).String() }
	call(http.MethodPut, "/api/v1/system/outbound-policy", key(), map[string]any{
		"expected_version": "1", "rule_mode": "allowlist",
		"rules": []any{map[string]any{"target": base.Hostname(), "ports": []int{providerPort}}},
	}, http.StatusOK)
	username, password := "", ""
	if proxy.User != nil {
		username = proxy.User.Username()
		var supplied bool
		password, supplied = proxy.User.Password()
		if username == "" || !supplied || password == "" {
			t.Fatal("external App proxy authentication is incomplete")
		}
	}
	proxyInput := map[string]any{
		"expected_version": "1", "mode": "proxy", "credential_action": "remove",
		"gateway": map[string]any{"protocol": "http", "host": proxy.Hostname(), "port": proxyPort, "username": username},
	}
	if username != "" {
		proxyInput["credential_action"], proxyInput["password"] = "keep", password
	}
	call(http.MethodPut, "/api/v1/system/outbound-policy/proxy", key(), proxyInput, http.StatusOK)
	var secret struct {
		ID string `json:"credential_id"`
	}
	check("formal Secret save", json.Unmarshal(call(http.MethodPost, "/api/v1/system/model-credentials", key(), map[string]string{"value": credential}, http.StatusOK), &secret))
	if secret.ID == "" {
		t.Fatal("external App Secret receipt has no identity")
	}
	var receipt mc.CommandReceipt
	check("formal provider save", json.Unmarshal(call(http.MethodPost, "/api/v1/system/model-providers", key(), map[string]any{"input": map[string]any{
		"name": "Owned external multimodal check", "protocol": "openai-compatible", "base_url": base.String(),
		"enabled": true, "credential_ref": secret.ID, "options": map[string]any{},
	}}, http.StatusOK), &receipt))
	providerID, providerVersion := receipt.ResourceID, receipt.Version.String()
	input := map[string]any{
		"notes": "Synthetic image and extracted DOCX only", "provider_model_id": providerModel, "type": "chat", "enabled": true,
		"parameters": map[string]any{}, "request_overwrite": map[string]any{}, "header_overwrite": map[string]string{},
		"capabilities": map[string]any{
			"tool_calls": false, "parallel_tool_calls": false, "streaming": false, "reasoning": false, "reasoning_toggle_mode": nil,
			"input_modalities": []string{"text", "image", "file"}, "output_modalities": []string{"text"},
			"reasoning_efforts": []any{}, "structured_output_modes": []string{"text"}, "context_length": nil, "max_output": nil,
		},
	}
	check("formal model save", json.Unmarshal(call(http.MethodPost, "/api/v1/system/models", key(), map[string]any{"provider_id": providerID, "input": input}, http.StatusOK), &receipt))
	modelID, modelVersion := receipt.ResourceID, receipt.Version.String()
	if providerID == "" || modelID == "" || providerVersion != "1" || modelVersion != "1" {
		t.Fatal("external App configuration did not produce fresh version-one identities")
	}
	command := map[string]any{
		"expected_version": modelVersion, "provider_version": providerVersion,
		"input": map[string]any{
			"text": "Read the identifier printed in the image and the identifier in the document. Reply with exactly these two identifiers separated by one space, image first. Do not add explanation.",
			"attachments": []any{
				map[string]string{"name": "synthetic-picture.png", "media_type": "image/png", "data_base64": base64.StdEncoding.EncodeToString(picture)},
				map[string]string{"name": "synthetic-document.docx", "media_type": "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "data_base64": base64.StdEncoding.EncodeToString(document)},
			},
		},
	}
	operationKey := key()
	ledger, err := os.OpenFile(ledgerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	check("exclusive one-request ledger", err)
	t.Cleanup(func() { _ = ledger.Close() })
	record := func(state string, extra map[string]any) {
		t.Helper()
		extra["state"], extra["at"] = state, time.Now().UTC().Format(time.RFC3339Nano)
		check("private ledger write", json.NewEncoder(ledger).Encode(extra))
		check("private ledger sync", ledger.Sync())
	}
	record("single_app_test_post_started", map[string]any{"operation_key": operationKey, "attachments": 2, "native_image": true, "docx_extracted": true, "pdf": false})
	// Exactly one invocation: no replay, fallback, second model or second prompt.
	raw := call(http.MethodPost, "/api/v1/system/models/"+modelID+"/test", operationKey, command, http.StatusOK)
	defer clear(raw)
	var result model.ManagementResult
	check("formal management result", json.Unmarshal(raw, &result))
	if result.OperationID == "" || result.ModelID != modelID || result.ProviderID != providerID || result.ModelVersion.String() != modelVersion || result.ProviderVersion.String() != providerVersion || result.DurationMS < 0 || result.DurationMS > 120000 {
		t.Fatal("external App result identity or bounded duration is invalid")
	}
	imageOK, documentOK := false, false
	if result.AnswerText != nil {
		imageOK = strings.Contains(*result.AnswerText, imageMarker)
		documentOK = strings.Contains(*result.AnswerText, docMarker)
	}
	safeReason := ""
	if result.Reason != nil {
		switch *result.Reason {
		case "timeout", "cancelled", "tls_failed", "response_limit", "upstream_rejected", "policy_rejected", "network_failed", "invalid_response", "unsupported_protocol", "interrupted":
			safeReason = *result.Reason
		default:
			safeReason = "unclassified"
		}
	}
	record("single_app_test_post_returned", map[string]any{
		"succeeded": result.Outcome == "succeeded", "image_marker_matched": imageOK,
		"document_marker_matched": documentOK, "answer_truncated": result.AnswerTruncated, "duration_ms": result.DurationMS, "reason": safeReason,
	})
	// Do not print vendor text, either synthetic marker, private endpoint or key.
	if result.Outcome != "succeeded" || result.Reason != nil || result.AnswerText == nil || result.AnswerTruncated || !imageOK || !documentOK {
		t.Fatal("external App mixed attachment answer did not pass the bounded exact-marker check; no retry performed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var operations, released, active int
	check("single operation and Secret lease terminal state", a.store.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_model.management_operations WHERE credential_id=$1 AND result IS NOT NULL),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND owner_kind='model_management_request' AND released),
 (SELECT count(*) FROM agenteam_secret.secret_leases WHERE credential_id=$1 AND NOT released)`, secret.ID).Scan(&operations, &released, &active))
	if operations != 1 || released != 1 || active != 0 {
		t.Fatal("external App did not retain exactly one completed operation with its Secret lease released")
	}
	for _, sensitive := range []string{credential, password, browser.cookie, browser.csrf, imageMarker, docMarker} {
		if sensitive != "" {
			independentProxyNoSecret(t, []byte(a.logs.String()), sensitive)
		}
	}
	call(http.MethodPost, "/api/v1/sessions/logout", key(), struct{}{}, http.StatusNoContent)
	call(http.MethodGet, "/api/v1/session", "", nil, http.StatusUnauthorized)
	a.signals <- syscall.SIGTERM
	await(t, a.done)
	if a.err != nil || !a.owned.accounts().Joined() || !a.owned.producersJoined() {
		t.Fatal("external App did not actually return and join")
	}
	record("app_joined", map[string]any{"operations": operations, "released_secret_leases": released, "active_secret_leases": active})
	t.Log("CHAT_MULTIMODAL_EXTERNAL_APP one_test_post=true saved_proxy=true image_marker_matched=true docx_marker_matched=true secret_lease_released=true app_joined=true pdf=false")
}

func externalChatAppCall(t *testing.T, b *independentProxyBrowser, method, path, key string, input any, want int) []byte {
	t.Helper()
	address, err := url.Parse(b.address)
	if err != nil || address.Scheme != "http" || !net.ParseIP(address.Hostname()).IsLoopback() {
		t.Fatal("external App management address is not the owned loopback listener")
	}
	var body []byte
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			t.Fatal("external App input encoding failed")
		}
	}
	defer clear(body)
	ctx, cancel := context.WithTimeout(context.Background(), 130*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, b.address+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal("external App local request construction failed")
	}
	request.Host = "localhost:8080"
	request.Header.Set("Origin", "http://localhost:8080")
	request.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: b.cookie})
	if method != http.MethodGet {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", b.csrf)
		request.Header.Set("Idempotency-Key", key)
	}
	response, err := b.client.Do(request)
	if err != nil {
		t.Fatal("external App local HTTP did not finish; no retry performed")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 1<<20 || response.StatusCode != want {
		clear(raw)
		t.Fatalf("external App local HTTP status=%d expected=%d incomplete=%t; no retry performed", response.StatusCode, want, readErr != nil || closeErr != nil || len(raw) > 1<<20)
	}
	return raw
}

func externalChatMarker(t *testing.T, prefix string) string {
	t.Helper()
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal("external App synthetic entropy unavailable")
	}
	return prefix + "-" + strings.ToUpper(hex.EncodeToString(random[:]))
}

func externalChatMarkerPNG(t *testing.T, marker string) []byte {
	t.Helper()
	// Render synthetic text directly, then enlarge with nearest-neighbor pixels.
	// No model, downloaded font, filesystem image or image-generation call.
	small := image.NewRGBA(image.Rect(0, 0, len(marker)*7+24, 45))
	draw.Draw(small, small.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	drawer := font.Drawer{Dst: small, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13, Dot: fixed.P(12, 28)}
	drawer.DrawString(marker)
	large := image.NewRGBA(image.Rect(0, 0, small.Bounds().Dx()*4, small.Bounds().Dy()*4))
	for y := 0; y < large.Bounds().Dy(); y++ {
		for x := 0; x < large.Bounds().Dx(); x++ {
			large.Set(x, y, small.At(x/4, y/4))
		}
	}
	var output bytes.Buffer
	if png.Encode(&output, large) != nil {
		t.Fatal("external App synthetic image encoding failed")
	}
	return output.Bytes()
}
