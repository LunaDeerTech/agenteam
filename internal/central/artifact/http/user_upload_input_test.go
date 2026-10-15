package artifacthttp

import (
	"net/http"
	"strings"
	"testing"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

func TestUserUploadHTTPStrictIntent(t *testing.T) {
	for _, body := range []string{
		`{"name":"f","media_type":"text/plain","byte_size":4}`,
		`{"name":"f","media_type":"text/plain","byte_size":null}`,
		`{"name":"f","media_type":"text/plain","byte_size":"04"}`,
		`{"name":"f","media_type":"text/plain","byte_size":"1073741825"}`,
		`{"name":"f","media_type":"text/plain","byte_size":"4","sha256":null}`,
		`{"name":"f","media_type":"text/plain","byte_size":"4","description":null}`,
		`{"Name":"f","media_type":"text/plain","byte_size":"4"}`,
		`{"name":"f","\u006eame":"g","media_type":"text/plain","byte_size":"4"}`,
		`{"name":"f","media_type":"text/plain","byte_size":"4","receipt":"secret"}`,
		`{"name":"f","media_type":"text/plain","byte_size":"4","kind":"generated"}`,
		`{"name":"f","media_type":"text/plain","byte_size":"4","expected_version":"1"}`,
		`{"name":"\ud800","media_type":"text/plain","byte_size":"4"}`,
		uploadJSON + `{}`,
	} {
		for _, action := range []string{"begin", "lookup", "complete", "cancel"} {
			h, _, p := uploadHandler(t)
			w := commandResponse(t, h, uploadRequest(action, body))
			if w.Code != 400 || len(p.calls) != 0 {
				t.Fatal("invalid intent reached domain", body, action, w.Code)
			}
		}
	}
	for _, change := range []func(*http.Request){
		func(r *http.Request) { r.Header.Add("Idempotency-Key", "other") },
		func(r *http.Request) { r.Header["idempotency-key"] = []string{"other"} },
		func(r *http.Request) { r.Header.Set("Idempotency-Key", "") },
		func(r *http.Request) { r.Header.Add("Content-Type", "application/octet-stream") },
		func(r *http.Request) { r.Header.Set("Range", "bytes=0-3") },
		func(r *http.Request) { r.Header["Content-Encoding"] = []string{""} },
		func(r *http.Request) { r.URL.RawQuery = "owner=forged" },
		func(r *http.Request) { r.ContentLength = -1 },
		func(r *http.Request) { r.TransferEncoding = []string{"chunked"} },
		func(r *http.Request) { r.ContentLength = oc.MaxObjectSize + 1 },
		func(r *http.Request) { r.Method = http.MethodPost },
	} {
		h, _, p := uploadHandler(t)
		r := uploadRequest("payload", "data")
		change(r)
		w := commandResponse(t, h, r)
		if w.Code == 200 || len(p.calls) != 0 {
			t.Fatal("malformed payload entered service", w.Code)
		}
	}
	h, _, p := uploadHandler(t)
	w := commandResponse(t, h, uploadRequest("lookup", strings.Replace(uploadJSON, `"4"`, `"0"`, 1)))
	if w.Code != 200 || p.calls[0].input.ByteSize != 0 {
		t.Fatal("empty user file not supported")
	}
}
