//go:build integration

package model_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
)

const meetingSummarySettingsPath = "/api/v1/system/model-selection/meeting-summary"

func meetingSummarySettingsBudget(t *testing.T) {
	t.Helper()
	started := time.Now()
	t.Cleanup(func() {
		if time.Since(started) > 2*time.Minute {
			t.Error("Summary settings top including actual Cleanup exceeded 2m")
		}
	})
}

// This recorder provides only controlled deadline capability. It is used with
// real PostgreSQL/Account/Model services and never presented as a native socket.
type meetingSummarySettingsWriter struct {
	*httptest.ResponseRecorder
	truncate bool
	complete []byte
}

func (w *meetingSummarySettingsWriter) SetReadDeadline(time.Time) error  { return nil }
func (w *meetingSummarySettingsWriter) SetWriteDeadline(time.Time) error { return nil }
func (w *meetingSummarySettingsWriter) FlushError() error                { w.ResponseRecorder.Flush(); return nil }
func (w *meetingSummarySettingsWriter) Write(p []byte) (int, error) {
	w.complete = append(w.complete, p...)
	if w.truncate && len(p) > 1 {
		return w.ResponseRecorder.Write(p[:1])
	}
	return w.ResponseRecorder.Write(p)
}
func meetingSummarySettingsRaw(ctx context.Context, handler http.Handler, b systemHTTPBrowser, method, path, key string, body []byte, change func(*http.Request), truncate bool) (systemHTTPResponse, bool, []byte) {
	r := httptest.NewRequest(method, systemHTTPOrigin+path, bytes.NewReader(body)).WithContext(ctx)
	r.Header.Set("Origin", systemHTTPOrigin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	if b.cookie != "" {
		r.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: b.cookie})
	}
	if b.csrf != "" {
		r.Header.Set("X-CSRF-Token", b.csrf)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if change != nil {
		change(r)
	}
	w := &meetingSummarySettingsWriter{ResponseRecorder: httptest.NewRecorder(), truncate: truncate}
	aborted := false
	func() {
		defer func() {
			if p := recover(); p != nil {
				if p != http.ErrAbortHandler {
					panic(p)
				}
				aborted = true
			}
		}()
		handler.ServeHTTP(w, r)
	}()
	return systemHTTPResponse{w.Code, w.Header().Clone(), bytes.Clone(w.Body.Bytes())}, aborted, bytes.Clone(w.complete)
}
func meetingSummarySettingsRequest(t *testing.T, v *systemHTTPFixture, b systemHTTPBrowser, method, path, key string, body any) systemHTTPResponse {
	t.Helper()
	var data []byte
	if body != nil {
		data = summaryJSON(t, body)
	}
	r, aborted, _ := meetingSummarySettingsRaw(testContext(t), v.http, b, method, path, key, data, nil, false)
	if aborted {
		t.Fatal("controlled formal HTTP aborted")
	}
	return r
}
func meetingSummarySettingsBody(r mc.UpdateMeetingSummarySelectionRequest) map[string]any {
	return map[string]any{"id": r.SelectionID, "expected_version": r.ExpectedVersion.String(), "model": r.Model.String()}
}
func meetingSummarySettingsSafeBody(t *testing.T, name, target string, r systemHTTPResponse, captured ...mc.UpdateMeetingSummarySelectionRequest) {
	t.Helper()
	dir := os.Getenv("AGENTEAM_MEETING_SUMMARY_SETTINGS_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	stat, e := os.Stat(dir)
	if e != nil || !stat.IsDir() || stat.Mode().Perm()&0077 != 0 {
		t.Fatal("private safe body directory required")
	}
	method, ok := map[string]string{"null": "GET", "configured": "GET", "receipt": "PUT", "lookup": "POST", "deletion-impact": "GET", "unknown": "PUT", "lookup-false": "POST", "replay": "PUT"}[name]
	if !ok || len(captured) > 1 {
		t.Fatal("closed safe body name and at most one captured command required")
	}
	sum := sha256.Sum256(r.body)
	metadata := map[string]any{"method": method, "target": target, "status": r.status, "content_type": r.headers.Get("Content-Type"), "body_sha256": hex.EncodeToString(sum[:]), "source_test": t.Name(), "source_run": os.Getenv("AGENTEAM_MEETING_SUMMARY_SETTINGS_RUN")}
	if name == "unknown" {
		requestID := r.headers.Get("X-Request-ID")
		if requestID == "" {
			t.Fatal("actual Unknown response request ID header missing")
		}
		metadata["response_headers"] = map[string]string{"X-Request-ID": requestID}
	}
	if len(captured) == 1 {
		// Only the three original public command fields are exported; CommandMeta
		// (including the idempotency key and actor) never enters the evidence.
		metadata["captured_command"] = meetingSummarySettingsBody(captured[0])
	}
	if e = os.WriteFile(filepath.Join(dir, name+".json"), r.body, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, name+"-source.json"), summaryJSON(t, metadata), 0600); e != nil {
		t.Fatal(e)
	}
	t.Logf("safe body %s sha256=%x", name, sum)
}
func meetingSummarySettingsReceipt(t *testing.T, r systemHTTPResponse, request mc.UpdateMeetingSummarySelectionRequest) {
	t.Helper()
	fields := r.want(t, 200).object(t)
	if len(fields) != 4 || fields["kind"] != "model.selection.update" || fields["resource_id"] != request.SelectionID || fields["version"] != (request.ExpectedVersion+1).String() || fields["affected_references"] != "0" {
		t.Fatal("receipt not bound to original Summary command")
	}
}
