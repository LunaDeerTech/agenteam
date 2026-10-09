//go:build integration

package account_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
)

const independentProblemPrivate = "independent-problem-private-sentinel"
const independentProblemPath = "/api/v1/projects/0191ac00-4751-7234-899a-100000000001/model-providers/0191ac00-4751-7234-899a-100000000002"

func independentProblemResponse(t *testing.T, code foundation.Code, state foundation.CommitState) (*http.Response, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "http://fixture.invalid"+independentProblemPath+"?private="+independentProblemPrivate, nil)
	req.URL.Fragment = independentProblemPrivate
	req.Header.Set("Cookie", independentProblemPrivate)
	before := *req.URL
	recorder := httptest.NewRecorder()
	var traced *http.Request
	httpapi.Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traced = r
		(&account.HTTPBoundary{}).WriteProblem(w, r, foundation.NewFault(code, state).WithCause(errors.New(independentProblemPrivate)))
	})).ServeHTTP(recorder, req)
	response := recorder.Result()
	t.Cleanup(func() { _ = response.Body.Close() })
	response.Request = traced
	if traced == nil || httpapi.RequestID(traced.Context()).String() != response.Header.Get("X-Request-ID") || *req.URL != before {
		t.Fatal("formal boundary lost transport identity or changed caller URL")
	}
	raw := bytes.Clone(recorder.Body.Bytes())
	if bytes.Contains(raw, []byte(independentProblemPrivate)) || bytes.Contains(raw, []byte(independentProblemPath)) {
		t.Fatal("formal boundary reflected private material or resource path")
	}
	return response, raw
}

func independentProblemAdmit(raw []byte, response *http.Response) error {
	// observeResponse performs this strict raw gate before calling admitResponse.
	if err := projectModelsWebJSON(raw); err != nil {
		return err
	}
	return (&projectModelsWebFixture{}).admitResponse(&projectModelsWebRequest{}, response, raw)
}

func TestIndependentModelProblemBoundary(t *testing.T) {
	cases := []struct {
		code   foundation.Code
		status int
		hint   string
	}{
		{foundation.InvalidState, 409, ""}, {foundation.ResourceBusy, 409, ""},
		{foundation.Forbidden, 403, ""}, {foundation.NotFound, 404, ""},
		{foundation.ProjectNotActive, 409, ""}, {foundation.VersionConflict, 409, "reread"},
		{foundation.CommitUnknown, 503, "lookup"}, {foundation.CursorStale, 409, "reread"},
	}
	for _, c := range cases {
		t.Run(string(c.code), func(t *testing.T) {
			for _, state := range []foundation.CommitState{foundation.NotStarted, foundation.NotCommitted, foundation.Committed, foundation.Unknown} {
				response, raw := independentProblemResponse(t, c.code, state)
				var wire httpapi.Problem
				if json.Unmarshal(raw, &wire) != nil || wire.Instance != "/api/v1" || wire.Code != c.code || wire.Status != c.status || wire.CommitState != state || wire.RetryHint != c.hint {
					t.Fatal("formal Problem projection differs from independent expected fields")
				}
				before, headers, request := bytes.Clone(raw), response.Header.Clone(), response.Request
				fixture := &projectModelsWebFixture{}
				if err := fixture.admitResponse(&projectModelsWebRequest{}, response, raw); err != nil {
					t.Fatal("formal production-boundary Problem rejected")
				}
				if !bytes.Equal(before, raw) || !reflect.DeepEqual(headers, response.Header) || response.Request != request || !reflect.DeepEqual(fixture, &projectModelsWebFixture{}) {
					t.Fatal("admission rewrote original evidence or manufactured fixture facts")
				}
			}
		})
	}
}

func TestIndependentModelProblemClosedBody(t *testing.T) {
	response, raw := independentProblemResponse(t, foundation.InvalidState, foundation.NotCommitted)
	edit := func(field string, value any) []byte {
		var obj map[string]any
		if json.Unmarshal(raw, &obj) != nil {
			t.Fatal("control decode failed")
		}
		obj[field] = value
		b, err := json.Marshal(obj)
		if err != nil {
			t.Fatal("negative control encode failed")
		}
		return b
	}
	cases := []struct {
		name string
		body []byte
	}{
		{"resource-instance", edit("instance", independentProblemPath)},
		{"query-instance", edit("instance", "/api/v1?private="+independentProblemPrivate)},
		{"escaped-instance", edit("instance", "/api%2fv1")},
		{"fragment-instance", edit("instance", "/api/v1#"+independentProblemPrivate)},
		{"detail", edit("detail", independentProblemPrivate)}, {"title", edit("title", independentProblemPrivate)},
		{"type", edit("type", independentProblemPrivate)}, {"unknown-code", edit("code", independentProblemPrivate)},
		{"different-code", edit("code", "FORBIDDEN")}, {"status", edit("status", 400)},
		{"request-id", edit("request_id", "0191ac00-4751-7234-899a-100000000003")},
		{"invalid-request-id", edit("request_id", independentProblemPrivate)},
		{"state", edit("commit_state", independentProblemPrivate)},
		{"retry-hint", edit("retry_hint", independentProblemPrivate)},
		{"extra", edit("credential", independentProblemPrivate)},
		{"field-errors", edit("field_errors", []any{map[string]any{"path": "/input", "code": "PRIVATE"}})},
		{"duplicate", []byte(strings.TrimSuffix(string(raw), "}") + `,"instance":"/api/v1"}`)},
		{"trailing", append(bytes.Clone(raw), []byte(` {}`)...)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := bytes.Clone(c.body)
			err := independentProblemAdmit(c.body, response)
			if err == nil {
				t.Fatal("noncanonical or private Problem admitted")
			}
			if strings.Contains(err.Error(), independentProblemPrivate) || !bytes.Equal(before, c.body) {
				t.Fatal("rejection echoed input or rewrote retained bytes")
			}
		})
	}
}

func TestIndependentModelProblemResponseBinding(t *testing.T) {
	cases := []struct {
		name   string
		change func(*http.Response)
	}{
		{"cache", func(r *http.Response) { r.Header.Set("Cache-Control", "public") }},
		{"media", func(r *http.Response) { r.Header.Set("Content-Type", "application/json") }},
		{"missing-id", func(r *http.Response) { r.Header.Del("X-Request-ID") }},
		{"different-id", func(r *http.Response) { r.Header.Set("X-Request-ID", "0191ac00-4751-7234-899a-100000000003") }},
		{"status", func(r *http.Response) { r.StatusCode = 400 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			response, raw := independentProblemResponse(t, foundation.InvalidState, foundation.NotStarted)
			c.change(response)
			if independentProblemAdmit(raw, response) == nil {
				t.Fatal("Problem response binding mismatch admitted")
			}
		})
	}
}
