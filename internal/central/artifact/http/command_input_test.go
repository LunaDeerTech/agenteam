package artifacthttp

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestArtifactHTTPContentIngress(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"unknown-kind", `{"name":"n","media_type":"text/plain","content":"x","kind":"generated"}`, 400},
		{"caller-source", `{"name":"n","media_type":"text/plain","content":"x","source_execution_id":"private"}`, 400},
		{"caller-digest", `{"name":"n","media_type":"text/plain","content":"x","digest":"private"}`, 400},
		{"caller-expected-version", `{"name":"n","media_type":"text/plain","content":"x","expected_version":"1"}`, 400},
		{"caller-attempt", `{"name":"n","media_type":"text/plain","content":"x","attempt_id":"private"}`, 400},
		{"duplicate-decoded-key", `{"name":"n","media_type":"text/plain","content":"x","\u0063ontent":"y"}`, 400},
		{"wrong-case-key", `{"Name":"n","media_type":"text/plain","content":"x"}`, 400},
		{"missing-content", `{"name":"n","media_type":"text/plain"}`, 400},
		{"null-content", `{"name":"n","media_type":"text/plain","content":null}`, 400},
		{"null-description", `{"name":"n","media_type":"text/plain","content":"x","description":null}`, 400},
		{"lone-high-surrogate", `{"name":"n","media_type":"text/plain","content":"\ud800"}`, 400},
		{"lone-low-surrogate", `{"name":"n","media_type":"text/plain","content":"\udc00"}`, 400},
		{"invalid-utf8", "{\"name\":\"n\",\"media_type\":\"text/plain\",\"content\":\"\xff\"}", 400},
		{"filename-path", `{"name":"../n","media_type":"text/plain","content":"x"}`, 400},
		{"filename-control", `{"name":"n\u0085","media_type":"text/plain","content":"x"}`, 400},
		{"description-nul", `{"name":"n","media_type":"text/plain","content":"x","description":"\u0000"}`, 400},
		{"invalid-mime", `{"name":"n","media_type":"text/plain\r\n","content":"x"}`, 400},
		{"trailing-document", contentJSON + `{}`, 400},
		{"not-object", `[]`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, action := range []string{"create-content", "lookup", "cancel"} {
				h, _, p := contentHandler(t)
				w := commandResponse(t, h, contentRequest(action, tc.body))
				if w.Code != tc.status || len(p.calls) != 0 {
					t.Fatal("invalid intent reached Service", action, w.Code)
				}
			}
		})
	}
	for _, mode := range []string{"query", "force-query", "key-missing", "key-multiple", "key-case-duplicate", "key-whitespace", "content-type-duplicate", "encoding-empty", "encoding-gzip", "range", "if-range", "unsupported-type", "unsupported-method", "origin", "csrf"} {
		t.Run(mode, func(t *testing.T) {
			h, b, p := contentHandler(t)
			r := contentRequest("create-content", contentJSON)
			want := 400
			switch mode {
			case "query":
				r.URL.RawQuery = "secret=private"
			case "force-query":
				r.URL.ForceQuery = true
			case "key-missing":
				r.Header.Del("Idempotency-Key")
			case "key-multiple":
				r.Header.Add("Idempotency-Key", "another")
			case "key-case-duplicate":
				r.Header["idempotency-key"] = []string{"another"}
			case "key-whitespace":
				r.Header.Set("Idempotency-Key", " original")
			case "content-type-duplicate":
				r.Header.Add("Content-Type", "application/json")
			case "encoding-empty":
				r.Header["Content-Encoding"] = []string{""}
				want = 415
			case "encoding-gzip":
				r.Header.Set("Content-Encoding", "gzip")
				want = 415
			case "range":
				r.Header.Set("Range", "bytes=0-1")
			case "if-range":
				r.Header.Set("If-Range", "tag")
			case "unsupported-type":
				r.Header.Set("Content-Type", "text/plain")
				want = 415
			case "unsupported-method":
				r.Method = http.MethodGet
				want = 405
			case "origin":
				b.check = f.NewFault(f.OriginDenied, f.NotStarted)
				want = 403
			case "csrf":
				b.auth = f.NewFault(f.CSRFFailed, f.NotStarted)
				want = 403
			}
			w := commandResponse(t, h, r)
			if w.Code != want || len(p.calls) != 0 {
				t.Fatal("ingress/authority bypass", w.Code)
			}
			if mode == "unsupported-method" && w.Header().Get("Allow") != "POST" {
				t.Fatal("method contract")
			}
		})
	}
}

func TestArtifactHTTPContentInputBounds(t *testing.T) {
	encode := func(content, name, description string) string {
		raw, err := json.Marshal(map[string]string{"name": name, "description": description, "media_type": "TEXT/PLAIN; charset=UTF-8", "content": content})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	for _, tc := range []struct {
		name, body string
		valid      bool
		status     int
	}{
		{"empty", encode("", "n", ""), true, 200},
		{"paired-surrogate", `{"name":"n","media_type":"text/plain","content":"\ud83d\ude00"}`, true, 200},
		{"escaped-backslash", `{"name":"n","media_type":"text/plain","content":"\\ud800"}`, true, 200},
		{"nul-content", encode("\x00", "n", ""), true, 200},
		{"maximum-decoded-escaped", encode(strings.Repeat("\x00", ac.MaxInlineBytes), strings.Repeat("a", 255), strings.Repeat("\x01", 4096)), true, 200},
		{"oversize-decoded", encode(strings.Repeat("a", ac.MaxInlineBytes+1), "n", ""), false, 413},
		{"oversize-filename-bytes", encode("", strings.Repeat("界", 86), ""), false, 400},
		{"oversize-description-bytes", encode("", "n", strings.Repeat("界", 1366)), false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Lookup/Cancel must accept exactly the same original body as Create.
			// Keep results absent to avoid manufacturing content for a mock receipt.
			for _, action := range []string{"create-content", "lookup", "cancel"} {
				h, _, p := contentHandler(t)
				if action == "create-content" {
					p.createErr = f.NewFault(f.ResourceBusy, f.NotCommitted)
				}
				w := commandResponse(t, h, contentRequest(action, tc.body))
				if tc.valid {
					if len(p.calls) != 1 || action != "create-content" && w.Code != 200 {
						t.Fatal("legal intent cannot be recovered", action, w.Code)
					}
					if p.calls[0].media != "text/plain" && p.calls[0].media != "text/plain; charset=UTF-8" {
						t.Fatal("unexpected MIME normalization")
					}
				} else if len(p.calls) != 0 || w.Code != tc.status {
					t.Fatal("oversize input reached service", action, w.Code)
				}
			}
		})
	}
	for _, extra := range []int{0, 1} {
		t.Run(map[int]string{0: "exact-encoded-limit", 1: "over-encoded-limit"}[extra], func(t *testing.T) {
			raw := contentJSON + strings.Repeat(" ", int(maxCommandRequestBytes)-len(contentJSON)+extra)
			for _, action := range []string{"create-content", "lookup", "cancel"} {
				h, _, p := contentHandler(t)
				w := commandResponse(t, h, contentRequest(action, raw))
				if extra == 0 && (w.Code != 200 || len(p.calls) != 1) || extra == 1 && (w.Code != 413 || len(p.calls) != 0) {
					t.Fatal("encoded limit differs between write and recovery", action, w.Code)
				}
			}
		})
	}
}
