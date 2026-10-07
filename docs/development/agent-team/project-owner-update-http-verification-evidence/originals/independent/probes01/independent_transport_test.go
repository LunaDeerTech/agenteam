// Private independent fixture transport; pending compile and execution.
//go:build integration

package model_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Unlike the accepted old root helper, this function preserves the exact
// caller-supplied key and encoded bytes. ctx/client timeout are scenario owners.
func ownerUpdateIndependentRequest(t *testing.T, ctx context.Context, root *projectUsageRoot, browser systemHTTPBrowser, method, path, key string, body []byte) systemHTTPResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, method, "http://"+root.address+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal("independent request construction")
	}
	req.Host = "system-http.example.test"
	req.Header.Set("Origin", systemHTTPOrigin)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if browser.cookie != "" {
		req.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: browser.cookie})
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if browser.csrf != "" {
		req.Header.Set("X-CSRF-Token", browser.csrf)
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := root.client.Do(req)
	if err != nil {
		t.Fatal("independent root transport did not complete")
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 64<<10 {
		t.Fatal("independent root response EOF/Close/size")
	}
	if len(response.Header.Values("X-Request-ID")) != 1 {
		t.Fatal("independent request identity")
	}
	return systemHTTPResponse{status: response.StatusCode, headers: response.Header.Clone(), body: raw}
}

// Only public fixture response bytes and safe provenance, never request key,
// Cookie, CSRF, credentials or a DSN. name is a private literal supplied by test.
func ownerUpdateIndependentExport(t *testing.T, dir, name, method, path, target, run, candidate, schemaFile string, response systemHTTPResponse) {
	t.Helper()
	if !filepath.IsAbs(dir) || (name != "patch" && name != "lookup") || run == "" || candidate == "" {
		t.Fatal("independent export binding")
	}
	if response.status != http.StatusOK || response.headers.Get("Content-Type") != "application/json" || response.headers.Get("Content-Length") != strconv.Itoa(len(response.body)) {
		t.Fatal("independent actual response metadata")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal("independent export directory")
	}
	schema, err := os.ReadFile(schemaFile)
	if err != nil {
		t.Fatal("independent schema read")
	}
	digest := sha256.Sum256(response.body)
	schemaDigest := sha256.Sum256(schema)
	source := map[string]any{"method": method, "path": path, "target": target, "status": response.status, "content_type": response.headers.Get("Content-Type"), "content_length": response.headers.Get("Content-Length"), "body_sha256": hex.EncodeToString(digest[:]), "schema_sha256": hex.EncodeToString(schemaDigest[:]), "run": run, "candidate": candidate, "producer_test": "TestModelProjectOwnerUpdateIndependentRootAndHistory"}
	metadata, err := json.MarshalIndent(source, "", "  ")
	if err != nil {
		t.Fatal("independent metadata encode")
	}
	for filename, data := range map[string][]byte{name + ".json": response.body, name + "-source.json": metadata} {
		file, err := os.OpenFile(filepath.Join(dir, filename), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal("independent export must be new")
		}
		n, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil || n != len(data) {
			t.Fatal("independent export actual write/close")
		}
	}
}
