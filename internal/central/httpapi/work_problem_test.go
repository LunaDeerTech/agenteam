package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

func TestWorkProblemsPreservePublicFailureFacts(t *testing.T) {
	cases := []struct {
		code   foundation.Code
		status int
		title  string
		detail string
	}{
		{foundation.TaskNotFound, 404, "Task not found", "The requested task was not found."},
		{foundation.TaskVersionConflict, 409, "Task version conflict", "The task changed. Read it again before retrying."},
		{foundation.TaskStateInvalid, 409, "Task state invalid", "The task state does not permit this action."},
		{foundation.TaskAssigneeRequired, 409, "Task assignee required", "This action requires an assigned agent."},
		{foundation.TaskSprintInvalid, 409, "Task sprint invalid", "The task sprint does not permit this action."},
		{foundation.TaskTerminalImmutable, 409, "Task terminal immutable", "A terminal task cannot be changed."},
		{foundation.BlockerNotFound, 404, "Blocker not found", "The requested blocker was not found."},
		{foundation.BlockerAlreadyResolved, 409, "Blocker already resolved", "The blocker is already resolved."},
		{foundation.TaskDependencyCycle, 409, "Task dependency cycle", "The task dependency would create a cycle."},
	}
	for _, tc := range cases {
		for _, state := range []foundation.CommitState{foundation.NotStarted, foundation.NotCommitted, foundation.Committed, foundation.Unknown} {
			t.Run(string(tc.code)+"/"+string(state), func(t *testing.T) {
				fault := foundation.NewFault(tc.code, state).WithCause(errors.New("work-private-cause-canary"))
				fault.CauseID = "work-private-correlation-canary"
				fault.FieldErrors = []foundation.FieldError{{Path: "/expected_version", Code: "STALE_VERSION"}}
				w := httptest.NewRecorder()
				Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					WriteProblem(w, r, fault)
				})).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/projects?cursor=work-query-canary", nil))
				var body Problem
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal("missing complete business Problem", err)
				}
				if w.Code != tc.status || body.Status != tc.status || body.Code != tc.code || body.Title != tc.title || body.Detail != tc.detail || body.CommitState != state {
					t.Fatal("business failure mapping changed")
				}
				if body.Type != "urn:agenteam:problem:"+strings.ReplaceAll(strings.ToLower(string(tc.code)), "_", "-") || body.RequestID.Validate() != nil || body.RequestID.String() != w.Header().Get("X-Request-ID") {
					t.Fatal("missing stable public identity")
				}
				if len(body.FieldErrors) != 1 || body.FieldErrors[0] != fault.FieldErrors[0] || strings.Contains(w.Body.String(), "canary") {
					t.Fatal("safe field projection or private boundary changed")
				}
				wantHint := ""
				if tc.code == foundation.TaskVersionConflict {
					wantHint = "reread"
				}
				if body.RetryHint != wantHint || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
					t.Fatal("retry or complete representation headers changed")
				}
			})
		}
	}
}

func TestWorkProblemHintAndHEAD(t *testing.T) {
	for _, tc := range []struct {
		method string
		hint   string
		want   string
	}{
		{http.MethodGet, "lookup", "lookup"},
		{http.MethodGet, "unsafe-hint-canary", "reread"},
		{http.MethodHead, "", "reread"},
	} {
		t.Run(tc.method+"/"+tc.hint, func(t *testing.T) {
			w := httptest.NewRecorder()
			Handler(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fault := foundation.NewFault(foundation.TaskVersionConflict, foundation.NotCommitted)
				fault.RetryHint = tc.hint
				WriteProblem(w, r, fault)
			})).ServeHTTP(w, httptest.NewRequest(tc.method, "/api/v1", nil))
			if w.Code != http.StatusConflict {
				t.Fatal("version conflict status changed")
			}
			if tc.method == http.MethodHead {
				if w.Body.Len() != 0 || w.Header().Get("Content-Length") == "0" {
					t.Fatal("HEAD lost representation length or wrote a body")
				}
				return
			}
			var body Problem
			if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.RetryHint != tc.want {
				t.Fatal("explicit hint handling changed")
			}
		})
	}
}
