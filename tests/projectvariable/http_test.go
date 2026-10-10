//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

type variableIntent struct {
	command                                    vc.CommandName
	project                                    vc.ProjectID
	target                                     vc.VariableID
	meta                                       f.CommandMeta
	identity                                   f.CommandIdentity
	request                                    any
	method, path, body, lookupPath, lookupBody string
}

func (variableIntent) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "private_variable_intent")
}
func (v *variableHTTPFixture) intent(t *testing.T, command vc.CommandName) variableIntent {
	t.Helper()
	r := createInput(t, "Intent_"+id[struct{}](t).String()[24:], "original-private-value")
	i := variableIntent{command: command, project: v.project.ID, target: r.Fields().ID, meta: meta(t, id[struct{}](t).String(), nil), request: r, method: "POST", path: variableHTTPPath(v.project.ID, "/variables"), lookupPath: variableHTTPPath(v.project.ID, "/variables/commands/lookup")}
	body := map[string]any{"request": r}
	lookup := map[string]any{"command": command, "request": r}
	if command != vc.CreateCommand {
		seed, e := v.service.CreateVariable(ctxFor(t), v.ownerBrowser.actor, meta(t, id[struct{}](t).String(), nil), i.project, r)
		if e != nil {
			t.Fatal(e)
		}
		version := seed.Fields().Variable.Fields().Version
		i.meta.ExpectedVersion = &version
		i.path += "/" + i.target.String()
		i.method = "PATCH"
		value := "recovered-private-value"
		i.request = updateInput(t, vc.VariableUpdateFields{Value: &value})
		body = map[string]any{"expected_version": version, "request": i.request}
		lookup = map[string]any{"command": command, "target_id": i.target, "expected_version": version, "request": i.request}
		if command == vc.DeleteCommand {
			i.request = nil
			i.method = "DELETE"
			delete(body, "request")
			delete(lookup, "request")
		}
	}
	i.body = string(jsonBytes(t, body))
	i.lookupBody = string(jsonBytes(t, lookup))
	var e error
	i.identity, e = vc.VariableCommandIdentity(i.project, i.command, i.meta.IdempotencyKey)
	if e != nil {
		t.Fatal(e)
	}
	return i
}
func requireHTTP(t *testing.T, r variableHTTPResponse) {
	t.Helper()
	if r.aborted || r.status != 200 || !json.Valid(r.body) {
		t.Fatalf("HTTP expected valid 200, status=%d aborted=%t", r.status, r.aborted)
	}
}
func requireProblem(t *testing.T, r variableHTTPResponse, code f.Code) httpapi.Problem {
	t.Helper()
	var p httpapi.Problem
	if r.aborted || json.Unmarshal(r.body, &p) != nil || p.Code != code || p.Status != r.status || p.RequestID.Validate() != nil {
		t.Fatalf("Problem status=%d code=%s wanted=%s aborted=%t", r.status, p.Code, code, r.aborted)
	}
	return p
}
func (v *variableHTTPFixture) send(t *testing.T, b variableHTTPBrowser, i variableIntent) variableHTTPResponse {
	return v.request(t, b, i.method, i.path, i.body, i.meta.IdempotencyKey)
}
func (v *variableHTTPFixture) lookupHTTP(t *testing.T, b variableHTTPBrowser, i variableIntent) variableHTTPResponse {
	return v.request(t, b, "POST", i.lookupPath, i.lookupBody, i.meta.IdempotencyKey)
}
func mutationHTTP(t *testing.T, r variableHTTPResponse) vc.VariableMutation {
	t.Helper()
	requireHTTP(t, r)
	var value vc.VariableMutation
	if json.Unmarshal(r.body, &value) != nil || value.Validate() != nil {
		t.Fatal("invalid typed mutation")
	}
	return value
}
func lookupHTTP(t *testing.T, r variableHTTPResponse) vc.VariableCommandLookup {
	t.Helper()
	requireHTTP(t, r)
	var value vc.VariableCommandLookup
	if json.Unmarshal(r.body, &value) != nil || value.Validate() != nil {
		t.Fatal("invalid typed lookup")
	}
	return value
}
func httpAsync(t *testing.T, v *variableHTTPFixture, r *http.Request) <-chan variableHTTPResponse {
	t.Helper()
	ctx, cancel := context.WithCancel(r.Context())
	r = r.WithContext(ctx)
	out := make(chan variableHTTPResponse, 1)
	done := make(chan struct{})
	go func() { defer close(done); out <- v.serve(r) }()
	t.Cleanup(func() { cancel(); await(t, done) })
	return out
}
func httpReply(t *testing.T, ch <-chan variableHTTPResponse) variableHTTPResponse {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("actual HTTP handler did not join")
	}
	return variableHTTPResponse{}
}

type authReadBarrier struct {
	io.ReadCloser
	once   sync.Once
	before func()
}

func (b *authReadBarrier) Read(p []byte) (int, error) {
	b.once.Do(b.before)
	return b.ReadCloser.Read(p)
}

func TestProjectVariableHTTPAuthorityAndPersistence(t *testing.T) {
	v := newVariableHTTPFixture(t)
	i := v.intent(t, vc.CreateCommand)
	t.Run("authentication-priority-and-real-CSRF", func(t *testing.T) {
		before := v.snapshot(t)
		for _, browser := range []variableHTTPBrowser{{}, v.otherBrowser, v.adminBrowser} {
			code := f.NotFound
			if browser.cookie == "" {
				code = f.Unauthenticated
			}
			requireProblem(t, v.send(t, browser, i), code)
		}
		missing := v.ownerBrowser
		missing.csrf = ""
		requireProblem(t, v.send(t, missing, i), f.CSRFFailed)
		r := variableHTTPRequest(ctxFor(t), v.ownerBrowser, i.method, i.path, i.body, i.meta.IdempotencyKey)
		r.Header.Set("Origin", "https://other.example.test")
		requireProblem(t, v.serve(r), f.OriginDenied)
		if before != v.snapshot(t) {
			t.Fatal("HTTP refusal changed business facts")
		}
	})
	created := mutationHTTP(t, v.send(t, v.ownerBrowser, i))
	original := created.Fields().Variable.Fields()
	for _, path := range []string{variableHTTPPath(i.project, "/variables"), i.path + "/" + i.target.String()} {
		r := v.request(t, v.ownerBrowser, "GET", path, "", "")
		requireHTTP(t, r)
		h := v.request(t, v.ownerBrowser, "HEAD", path, "", "")
		if h.aborted || h.status != 200 || len(h.body) != 0 || h.header.Get("Content-Length") == "" {
			t.Fatal("HEAD boundary")
		}
	}
	t.Run("wire-and-query-denials-preserve-facts", func(t *testing.T) {
		before := v.snapshot(t)
		for _, body := range []string{`{"request":null}`, `{"request":{},"Request":{}}`, `{"request":{},"request":{}}`, `{"expected_version":"1","request":{}}`} {
			requireProblem(t, v.request(t, v.ownerBrowser, "POST", i.path, body, f.IdempotencyKey(id[struct{}](t).String())), f.InvalidArgument)
		}
		for _, query := range []string{"?", "?limit=01", "?limit=1&limit=1", "?limit=1&%6cimit=2", "?cursor=", "?unexpected=1"} {
			requireProblem(t, v.request(t, v.ownerBrowser, "GET", i.path+query, "", ""), f.InvalidArgument)
		}
		requireProblem(t, v.request(t, v.ownerBrowser, "PUT", i.path, "{}", ""), f.MethodNotAllowed)
		requireProblem(t, v.request(t, v.ownerBrowser, "PATCH", i.path+"/"+i.target.String(), `{"expected_version":"1","request":{"value":null}}`, "null-value"), f.InvalidArgument)
		if before != v.snapshot(t) {
			t.Fatal("invalid HTTP input changed facts")
		}
	})
	t.Run("revoked-expired-and-preauthenticated-revocation", func(t *testing.T) {
		b := v.login(t, v.ownerBrowser.email)
		v.revoke(t, b)
		r := v.request(t, b, "GET", i.path, "", "")
		requireProblem(t, r, f.SessionRevoked)
		if r.header.Get("Set-Cookie") == "" {
			t.Fatal("revoked HTTP did not clear cookie")
		}
		b = v.login(t, v.ownerBrowser.email)
		next := v.intent(t, vc.CreateCommand)
		before := v.snapshot(t)
		request := variableHTTPRequest(ctxFor(t), b, next.method, next.path, next.body, next.meta.IdempotencyKey)
		request.Body = &authReadBarrier{ReadCloser: request.Body, before: func() { v.revoke(t, b) }}
		requireProblem(t, v.serve(request), f.SessionRevoked)
		if before != v.snapshot(t) {
			t.Fatal("preauthenticated Session cached into final write")
		}
	})
	t.Run("prepare-after-HTTP-auth-rechecks-current-session", func(t *testing.T) {
		b := v.login(t, v.ownerBrowser.email)
		next := v.intent(t, vc.CreateCommand)
		gate, release := prepareGate(t, v)
		v.bindService(t, v.newService(t, gate, v.accounts))
		defer v.bindService(t, v.service)
		response := httpAsync(t, v, variableHTTPRequest(ctxFor(t), b, next.method, next.path, next.body, next.meta.IdempotencyKey))
		select {
		case <-gate.reached:
		case r := <-response:
			t.Fatalf("HTTP ended before PrepareAppend status=%d", r.status)
		case <-time.After(5 * time.Second):
			t.Fatal("PrepareAppend not reached")
		}
		v.revoke(t, b)
		before := v.snapshot(t)
		release()
		requireProblem(t, httpReply(t, response), f.SessionRevoked)
		if before != v.snapshot(t) {
			t.Fatal("prepared HTTP used stale Session")
		}
	})
	t.Run("archived-current-read-original-history-only", func(t *testing.T) {
		v.archiveFixture(t, i.project, v.ownerBrowser.actor)
		before := v.snapshot(t)
		requireHTTP(t, v.request(t, v.ownerBrowser, "GET", i.path+"/"+i.target.String(), "", ""))
		got := lookupHTTP(t, v.lookupHTTP(t, v.ownerBrowser, i))
		if got.Status() != vc.LookupCommitted {
			t.Fatal("archived history missing")
		}
		sameReceipt(t, *got.Receipt(), created)
		sameReceipt(t, mutationHTTP(t, v.send(t, v.ownerBrowser, i)), created)
		requireProblem(t, v.request(t, v.ownerBrowser, "DELETE", i.path+"/"+i.target.String(), string(jsonBytes(t, map[string]any{"expected_version": original.Version})), "new-delete"), f.ProjectNotActive)
		if before != v.snapshot(t) {
			t.Fatal("archived request wrote variable facts")
		}
	})
	for _, secret := range []string{i.body, i.lookupBody, string(i.meta.IdempotencyKey), v.ownerBrowser.cookie, v.ownerBrowser.csrf, "original-private-value"} {
		if strings.Contains(v.logs.text(), secret) {
			t.Fatal("private HTTP material in log")
		}
	}
}

type lostVariableWriter struct {
	*variableHTTPRecorder
	writes int
}

func (w *lostVariableWriter) Write([]byte) (int, error) { w.writes++; return 0, io.ErrClosedPipe }
func (v *variableHTTPFixture) loseResponse(t *testing.T, i variableIntent) {
	t.Helper()
	w := &lostVariableWriter{variableHTTPRecorder: &variableHTTPRecorder{httptest.NewRecorder()}}
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
		v.handler.ServeHTTP(w, variableHTTPRequest(ctxFor(t), v.ownerBrowser, i.method, i.path, i.body, i.meta.IdempotencyKey))
	}()
	if !aborted || w.writes != 1 || w.Body.Len() != 0 || w.Code != 200 {
		t.Fatal("actual committed response not lost at sole write")
	}
}
func TestProjectVariableHTTPIntentRecovery(t *testing.T) {
	v := newVariableHTTPFixture(t)
	for _, command := range []vc.CommandName{vc.CreateCommand, vc.UpdateCommand, vc.DeleteCommand} {
		t.Run(string(command), func(t *testing.T) {
			i := v.intent(t, command)
			v.loseResponse(t, i)
			var saved []byte
			if e := v.raw.QueryRow(ctxFor(t), `SELECT receipt FROM agenteam_projectvariable.commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3 AND state='completed'`, i.project.String(), string(i.command), string(i.meta.IdempotencyKey)).Scan(&saved); e != nil {
				t.Fatal("lost response did not commit", e)
			}
			var original vc.VariableMutation
			if json.Unmarshal(saved, &original) != nil {
				t.Fatal("persistent receipt invalid")
			}
			fresh := v.login(t, v.ownerBrowser.email)
			if command != vc.DeleteCommand {
				current, e := v.service.GetVariable(ctxFor(t), fresh.actor, i.project, i.target)
				if e != nil {
					t.Fatal(e)
				}
				version := current.Fields().Version
				later := "later-current-value"
				if _, e = v.service.UpdateVariable(ctxFor(t), fresh.actor, meta(t, "later-"+id[struct{}](t).String(), &version), i.project, i.target, updateInput(t, vc.VariableUpdateFields{Value: &later})); e != nil {
					t.Fatal(e)
				}
			}
			before := v.snapshot(t)
			looked := lookupHTTP(t, v.lookupHTTP(t, fresh, i))
			if looked.Status() != vc.LookupCommitted {
				t.Fatal("original not found")
			}
			sameReceipt(t, *looked.Receipt(), original)
			sameReceipt(t, mutationHTTP(t, v.send(t, fresh, i)), original)
			if before != v.snapshot(t) {
				t.Fatal("history recovery made a second fact")
			}
			var changed map[string]any
			if json.Unmarshal([]byte(i.lookupBody), &changed) != nil {
				t.Fatal("original encoding")
			}
			if command == vc.CreateCommand {
				r := changed["request"].(map[string]any)
				r["value"] = "other-intent"
			} else {
				changed["expected_version"] = "999"
			}
			requireProblem(t, v.request(t, fresh, "POST", i.lookupPath, string(jsonBytes(t, changed)), i.meta.IdempotencyKey), f.IdempotencyKeyReused)
			absent := i
			absent.meta.IdempotencyKey = f.IdempotencyKey(id[struct{}](t).String())
			if got := lookupHTTP(t, v.lookupHTTP(t, fresh, absent)); got.Status() != vc.LookupNotObserved || got.Receipt() != nil {
				t.Fatal("absence invented a receipt")
			}
			if before != v.snapshot(t) {
				t.Fatal("Lookup changed facts")
			}
		})
	}
	t.Run("in-flight-lookup-does-not-invent-absence", func(t *testing.T) {
		i := v.intent(t, vc.CreateCommand)
		// HTTP authentication takes User SH before decoding. Complete that real
		// Cookie/CSRF check before the writer holds User EX, then release this
		// same request into Lookup while the writer's command lock is held.
		authenticated, decode := make(chan struct{}), make(chan struct{})
		var decodeOnce sync.Once
		releaseDecode := func() { decodeOnce.Do(func() { close(decode) }) }
		defer releaseDecode()
		request := variableHTTPRequest(ctxFor(t), v.ownerBrowser, "POST", i.lookupPath, i.lookupBody, i.meta.IdempotencyKey)
		request.Body = &authReadBarrier{ReadCloser: request.Body, before: func() {
			close(authenticated)
			select {
			case <-decode:
			case <-request.Context().Done():
			}
		}}
		reading := httpAsync(t, v, request)
		select {
		case <-authenticated:
		case r := <-reading:
			t.Fatalf("Lookup returned before real authentication/body boundary status=%d", r.status)
		case <-time.After(5 * time.Second):
			t.Fatal("Lookup did not reach authenticated body read")
		}
		reached, pid, release := holdVariableFinal(t, v, i.project, i.command, i.meta.IdempotencyKey)
		defer release()
		writer := httpAsync(t, v, variableHTTPRequest(ctxFor(t), v.ownerBrowser, i.method, i.path, i.body, i.meta.IdempotencyKey))
		select {
		case <-reached:
		case early := <-writer:
			t.Fatalf("writer returned early %d", early.status)
		case <-time.After(5 * time.Second):
			t.Fatal("final stage absent")
		}
		key, _ := f.CommandLock(i.identity)
		observed := observeLock(v.tracked, key)
		releaseDecode()
		select {
		case attempt := <-observed:
			v.waitLock(t, attempt, false, pid.Load())
		case r := <-reading:
			var problem httpapi.Problem
			_ = json.Unmarshal(r.body, &problem)
			t.Fatalf("Lookup returned before real lock status=%d code=%s aborted=%t", r.status, problem.Code, r.aborted)
		case <-time.After(5 * time.Second):
			t.Fatal("Lookup command lock absent")
		}
		result := httpReply(t, reading)
		if !result.aborted && result.status == 200 {
			t.Fatal("in-flight Lookup invented not_observed/result")
		}
		release()
		receipt := mutationHTTP(t, httpReply(t, writer))
		got := lookupHTTP(t, v.lookupHTTP(t, v.ownerBrowser, i))
		sameReceipt(t, *got.Receipt(), receipt)
	})
}
