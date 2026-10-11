//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	auditc "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	rc "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

// The setup owners retire before the original public App.Run takes over.
// Every Runner management operation then comes from the real Admin browser.
// No device connects, and no token is serialized into fixture evidence.
func TestSystemRunnerManagementWeb(t *testing.T) {
	for _, mode := range []string{"system-runner-normal", "system-runner-lost"} {
		t.Run(mode, func(t *testing.T) {
			entered, completed := false, false
			t.Cleanup(func() {
				if !entered || !completed {
					t.Error("System Runner browser App handoff incomplete")
				}
			})
			a := newAgentCreateFixture(t)
			b := a.p2.base
			process, err := a.guard.CurrentProcess()
			agentAppRequire(t, err)
			registration := agentAppRegistration(t, a)
			var admin bool
			agentAppRequire(t, b.raw.QueryRow(ctxFor(t), `SELECT role='admin' FROM agenteam_account.users WHERE id::text=$1`, b.adminBrowser.actor.Details().UserID).Scan(&admin))
			if !admin || b.afterOwners != nil {
				t.Fatal("System Runner requires the formal Admin and one App handoff")
			}
			b.afterOwners = func() {
				entered = true
				if !b.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
					t.Fatal("original setup owners remain live")
				}
				if _, err := a.guard.CurrentProcess(); err == nil {
					t.Fatal("original setup guard remains held")
				}
				var stopped bool
				agentAppRequire(t, b.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
				if !stopped {
					t.Fatal("original setup process has not retired")
				}
				w := newSystemRunnerWeb(t, a, mode)
				defer w.dispose(t)
				withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
					defer w.close(t)
					w.start(t, address)
					if agentAppRegistration(t, a) != registration {
						t.Fatal("default App changed builtin registration")
					}
					w.material(t)
					w.browser(t, ctx)
					w.close(t)
					w.verify(t, ctx)
				})
				completed = true
			}
		})
	}
}

const systemRunnerPath = "/api/v1/system/runners"

type systemRunnerIntent struct {
	kind                  string
	key                   f.IdempotencyKey
	intent                rc.Intent
	bodyDigest, keyDigest [32]byte
}
type systemRunnerIntentKey struct{}
type systemRunnerWeb struct {
	*agentEditWeb
	target                                        rc.RunnerID
	current                                       rc.Snapshot
	commands                                      []systemRunnerIntent
	receipts                                      []rc.Receipt
	hosts                                         map[string]agentRunnerHost
	proofs                                        map[string]map[string]any
	reads                                         map[f.Version]int
	lists, details, updates, enrollments, revokes int
}

func newSystemRunnerWeb(t *testing.T, a *agentCreateFixture, mode string) *systemRunnerWeb {
	t.Helper()
	const prefix = "AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB"
	dist, runtime := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence, inputHash := os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("System Runner requires frozen assets and owned paths")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("production index missing")
	}
	directory, err := os.MkdirTemp(runtime, "srmw-")
	agentAppRequire(t, err)
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.RemoveAll(directory)
		t.Fatal("owned listener unavailable")
	}
	evidence = filepath.Join(evidence, "TestSystemRunnerManagementWeb", mode)
	if err = os.MkdirAll(filepath.Dir(evidence), 0700); err == nil {
		err = os.Mkdir(evidence, 0700)
	}
	if err != nil {
		_ = listener.Close()
		_ = os.RemoveAll(directory)
		t.Fatal("fresh evidence unavailable")
	}
	base := &agentConfigurationWeb{a: a, mode: mode, origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	return &systemRunnerWeb{agentEditWeb: &agentEditWeb{agentConfigurationWeb: base}, hosts: map[string]agentRunnerHost{}, proofs: map[string]map[string]any{}, reads: map[f.Version]int{}}
}
func systemRunnerDraft() map[string]any {
	return map[string]any{"name": "Browser managed Runner", "description": "Created by the current system administrator.", "tags": []string{"browser"}, "root_path": "/owned-system-runner-fixture/workspace"}
}
func systemRunnerPatch() map[string]any {
	return map[string]any{"expected_version": "1", "name": "Renamed browser Runner", "description": "Metadata edited without connecting a device.", "tags": []string{"browser", "managed"}}
}
func (w *systemRunnerWeb) material(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"mode": w.mode, "origin": w.origin, "cookie": w.a.p2.base.adminBrowser.cookie, "route": "/system/runners", "draft": systemRunnerDraft(), "patch": systemRunnerPatch(), "input_hash": w.inputHash})
	agentAppRequire(t, err)
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "system-runner-management-material.json"), raw, 0600))
	clear(raw)
}

func (w *systemRunnerWeb) observe(r *http.Request) error {
	if r.URL.RawPath != "" || r.URL.ForceQuery || r.Header.Get("Cookie") != "agenteam_local_session="+w.a.p2.base.adminBrowser.cookie {
		return errors.New("request lacks original Admin browser identity")
	}
	if r.Method == http.MethodGet {
		if r.Header.Get("Idempotency-Key") != "" || r.ContentLength > 0 {
			return errors.New("read carries command material")
		}
		switch r.URL.Path {
		case "/api/v1/session", "/api/v1/projects":
			return nil
		case systemRunnerPath:
			if r.URL.RawQuery != "limit=50" {
				return errors.New("unexpected Runner page query")
			}
			return nil
		default:
			w.mu.Lock()
			target := w.target
			w.mu.Unlock()
			if target.Validate() == nil && r.URL.Path == systemRunnerPath+"/"+target.String() && r.URL.RawQuery == "" {
				return nil
			}
		}
		return errors.New("undeclared system read")
	}
	if r.URL.RawQuery != "" || r.Header.Get("X-CSRF-Token") != w.a.p2.base.adminBrowser.csrf || r.Header.Get("Origin") != w.origin {
		return errors.New("command lacks original browser CSRF/origin")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (32<<10)+1))
	closed := r.Body.Close()
	if err != nil || closed != nil || len(raw) > 32<<10 {
		return errors.New("original command body incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("original command key missing")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	kind := ""
	var intent rc.Intent
	if r.Method == http.MethodPost && r.URL.Path == systemRunnerPath {
		var request rc.CreateRequest
		if json.Unmarshal(raw, &request) != nil || w.creates != 0 {
			return errors.New("invalid or repeated Create")
		}
		want := systemRunnerDraft()
		want["runner_id"] = request.RunnerID.String()
		if !runnerMountSameJSON(raw, jsonAgentWeb(want)) {
			return errors.New("Create differs from explicit form")
		}
		intent, err = rc.NewCreate(request)
		w.target = request.RunnerID
		kind = "create"
		w.creates++
	} else if w.target.Validate() == nil {
		path := systemRunnerPath + "/" + w.target.String()
		switch {
		case r.Method == http.MethodPatch && r.URL.Path == path:
			if w.mode != "system-runner-normal" || w.updates != 0 || len(w.receipts) != 1 || !runnerMountSameJSON(raw, jsonAgentWeb(systemRunnerPatch())) {
				return errors.New("unexpected metadata Update")
			}
			var request rc.UpdateRequest
			err = json.Unmarshal(raw, &request)
			if err == nil {
				intent, err = rc.NewUpdate(w.target, request)
			}
			kind = "update"
			w.updates++
		case r.Method == http.MethodPost && r.URL.Path == path+"/commands/lookup":
			var request struct {
				Command rc.CommandName  `json:"command"`
				Request json.RawMessage `json:"request"`
			}
			if w.mode != "system-runner-lost" || w.cut != 1 || w.lookups != 0 || len(w.commands) != 1 || agentWebDecode(raw, &request) != nil || request.Command != rc.Create || key != w.commands[0].key {
				return errors.New("Lookup does not retain original Create")
			}
			original, _ := w.commands[0].intent.RequestJSON()
			if !runnerMountSameJSON(request.Request, original) {
				return errors.New("Lookup changed original body")
			}
			intent, err = rc.DecodeIntent(request.Command, w.target, request.Request)
			kind = "lookup"
			w.lookups++
		case r.Method == http.MethodPost && (r.URL.Path == path+"/enrollment" || r.URL.Path == path+"/revoke"):
			var request rc.CredentialRequest
			if json.Unmarshal(raw, &request) != nil || request.ExpectedVersion != w.current.Version || w.reads[w.current.Version] == 0 {
				return errors.New("credential operation lacks independent current Get/version")
			}
			if r.URL.Path == path+"/enrollment" {
				if w.enrollments != 0 || w.mode == "system-runner-lost" && w.lookups != 1 {
					return errors.New("implicit or repeated enrollment")
				}
				intent, err = rc.NewEnrollment(w.target, request)
				kind = "enrollment"
				w.enrollments++
			} else {
				if w.mode != "system-runner-normal" || w.revokes != 0 || w.enrollments != 1 {
					return errors.New("unexpected revoke")
				}
				intent, err = rc.NewRevoke(w.target, request)
				kind = "revoke"
				w.revokes++
			}
		}
	}
	if err != nil || kind == "" {
		return errors.New("unrecognized Runner management command")
	}
	in := systemRunnerIntent{kind: kind, key: key, intent: intent, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	if kind != "lookup" {
		for _, old := range w.commands {
			if old.key == key {
				return errors.New("new operation reused original key")
			}
		}
		w.commands = append(w.commands, in)
	}
	w.keys = append(w.keys, string(key))
	*r = *r.WithContext(context.WithValue(r.Context(), systemRunnerIntentKey{}, in))
	return nil
}

// Verification uses the independent SQL connection after original App EOF,
// not a synthesized service result. Only safe public command facts persist.
func (w *systemRunnerWeb) commandFacts(ctx context.Context, in systemRunnerIntent, receipt rc.Receipt, token string, expires *f.Instant) error {
	b := w.a.p2.base
	if receipt.Validate() != nil || !receipt.Changed || receipt.Command != in.intent.Command() || receipt.Runner.ID != w.target {
		return errors.New("invalid original Runner receipt")
	}
	var id, user, session, digest string
	var request, stored []byte
	err := b.raw.QueryRow(ctx, `SELECT id::text,actor_user_id::text,actor_session_id::text,semantic_digest,request,receipt FROM agenteam_runner.commands WHERE runner_id=$1 AND command_name=$2 AND idempotency_key=$3`, w.target.String(), string(in.intent.Command()), string(in.key)).Scan(&id, &user, &session, &digest, &request, &stored)
	original, _ := in.intent.RequestJSON()
	semantic, e := in.intent.Digest(b.adminBrowser.actor)
	if err != nil || e != nil || id != receipt.CommandID.String() || user != b.adminBrowser.actor.Details().UserID || session != b.adminBrowser.actor.Details().SessionID || digest != semantic.String() || !runnerMountSameJSON(original, request) || !runnerMountSameJSON(stored, jsonAgentWeb(receipt)) {
		return errors.New("original command/actor/semantic/receipt relation failed")
	}
	r := receipt.Runner
	var exact bool
	if err = b.raw.QueryRow(ctx, `SELECT name=$2 AND description=$3 AND tags=$4::jsonb AND root_path=$5 AND version=$6 AND credential_generation=$7 AND device_public_key IS NULL AND enrolled_at IS NULL AND last_seen_at IS NULL AND NOT incompatible AND connection_generation=0 AND created_at=$8 AND updated_at=$9 FROM agenteam_runner.runners WHERE id=$1`, r.ID.String(), r.Name, r.Description, string(jsonAgentWeb(r.Tags)), r.RootPath, int64(r.Version), int64(r.CredentialGeneration), r.CreatedAt.Time(), r.UpdatedAt.Time()).Scan(&exact); err != nil || !exact || r.Status != rc.Offline || r.PublicKeyFingerprint != nil || r.EnrolledAt != nil || r.LastSeenAt != nil || r.LastHello != nil {
		return errors.New("receipt differs from original offline metadata postimage")
	}
	var metadata []byte
	if err = b.raw.QueryRow(ctx, `SELECT metadata FROM agenteam_audit.audit_records WHERE producer='runner' AND cause_ref=$1 AND ordinal=0 AND project_id IS NULL AND actor_kind='human' AND user_id::text=$2 AND session_id::text=$3 AND action=$4 AND outcome='success' AND resource_kind='runner' AND resource_id::text=$5`, id, user, session, string(receipt.Command), r.ID.String()).Scan(&metadata); err != nil {
		return errors.New("typed current Admin Audit missing")
	}
	m, err := auditc.DecodeMetadata(auditc.Action(receipt.Command), metadata)
	if err != nil {
		return errors.New("invalid typed Runner Audit")
	}
	fields, err := m.RunnerFields()
	wantFields := []string{"credential"}
	if in.kind == "create" {
		wantFields = []string{"created"}
	}
	if in.kind == "update" {
		wantFields = []string{"description", "name", "tags"}
	}
	if err != nil || fields.RunnerID != r.ID.String() || fields.Version != r.Version || fields.CredentialGeneration != r.CredentialGeneration || !reflect.DeepEqual(fields.ChangedFields, wantFields) {
		return errors.New("Runner Audit metadata differs")
	}
	if in.kind == "create" || in.kind == "enrollment" {
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(token)
		if err != nil || len(decoded) != 32 || len(token) != 43 || expires == nil || expires.Validate() != nil {
			clear(decoded)
			return errors.New("first committed response lacks valid one-time material")
		}
		hash := sha256.Sum256(decoded)
		clear(decoded)
		var tokenFact, eventFact bool
		err = b.raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_runner.enrollment_tokens WHERE issued_command_id=$1 AND runner_id=$2 AND token_hash=$3 AND credential_generation=$4 AND issued_at=$5 AND expires_at=$6 AND expires_at=issued_at+interval '10 minutes' AND consumed_at IS NULL AND revoked_at IS NULL),EXISTS(SELECT 1 FROM agenteam_runner.identity_events WHERE command_id=$1 AND runner_id=$2 AND kind='enrollment_issued' AND version=$7 AND credential_generation=$4 AND public_key_fingerprint IS NULL)`, id, r.ID.String(), hash[:], int64(r.CredentialGeneration), r.UpdatedAt.Time(), expires.Time(), int64(r.Version)).Scan(&tokenFact, &eventFact)
		if err != nil || !tokenFact || !eventFact {
			return errors.New("one-time material/identity event not bound to original commit")
		}
	} else if token != "" || expires != nil {
		return errors.New("non-enrollment operation exposed material")
	}
	if in.kind == "revoke" {
		var revoked bool
		if err = b.raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_runner.identity_events WHERE command_id=$1 AND runner_id=$2 AND kind='revoked' AND version=$3 AND credential_generation=$4 AND public_key_fingerprint IS NULL)`, id, r.ID.String(), int64(r.Version), int64(r.CredentialGeneration)).Scan(&revoked); err != nil || !revoked {
			return errors.New("revoke event missing")
		}
	}
	return nil
}

func (w *systemRunnerWeb) counts(ctx context.Context) ([8]int64, error) {
	var v [8]int64
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_runner.commands WHERE runner_id=$1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE producer='runner' AND resource_id=$1),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1),
 (SELECT count(*) FROM agenteam_runner.enrollment_tokens WHERE runner_id=$1 AND revoked_at IS NULL AND consumed_at IS NULL),
 (SELECT count(*) FROM agenteam_runner.identity_events WHERE runner_id=$1),
 (SELECT count(*) FROM agenteam_runner.challenges),
 (SELECT count(*) FROM agenteam_runner.connections),
 (SELECT count(*) FROM agenteam_execution.executions)`, w.target.String()).Scan(&v[0], &v[1], &v[2], &v[3], &v[4], &v[5], &v[6], &v[7])
	return v, err
}

func (w *systemRunnerWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	if r.StatusCode != http.StatusOK || r.Header.Get("Cache-Control") != "no-store" {
		return errors.New("unexpected Runner browser response")
	}
	rid := r.Header.Get("X-Request-ID")
	w.mu.Lock()
	host, exists := w.hosts[rid]
	w.mu.Unlock()
	ingress, _ := r.Request.Context().Value(agentRunnerIngressKey{}).(bool)
	if !ingress || !exists || !host.matches || host.method != r.Request.Method || host.uri != r.Request.URL.RequestURI() {
		return errors.New("Host proof does not bind original request")
	}
	in, write := r.Request.Context().Value(systemRunnerIntentKey{}).(systemRunnerIntent)
	lost := false
	if write {
		if in.kind == "lookup" {
			var response struct {
				State          string     `json:"state"`
				Receipt        rc.Receipt `json:"receipt"`
				TokenAvailable bool       `json:"token_available"`
			}
			if agentWebDecode(raw, &response) != nil || response.State != "committed" || response.TokenAvailable || !runnerMountSameJSON(jsonAgentWeb(response.Receipt), jsonAgentWeb(w.receipts[0])) {
				return errors.New("Lookup did not return the original public-only receipt")
			}
			counts, e := w.counts(r.Request.Context())
			if e != nil || counts != ([8]int64{1, 1, 1, 1, 1, 0, 0, 0}) {
				return errors.New("Lookup replayed original effects")
			}
		} else {
			var response struct {
				Receipt        rc.Receipt `json:"receipt"`
				TokenAvailable bool       `json:"token_available"`
				Token          string     `json:"enrollment_token,omitempty"`
				Expires        *f.Instant `json:"expires_at,omitempty"`
			}
			if agentWebDecode(raw, &response) != nil || response.TokenAvailable != (in.kind == "create" || in.kind == "enrollment") {
				return errors.New("invalid Runner mutation envelope")
			}
			if err = w.commandFacts(r.Request.Context(), in, response.Receipt, response.Token, response.Expires); err != nil {
				return err
			}
			response.Token = "" // Never retain or serialize the one-time value.
			w.mu.Lock()
			w.current = response.Receipt.Runner.Clone()
			w.receipts = append(w.receipts, response.Receipt.Clone())
			w.mu.Unlock()
			lost = w.mode == "system-runner-lost" && in.kind == "create"
		}
	} else if r.Request.Method == http.MethodGet {
		if err = w.observeRead(r.Request.URL.Path, raw); err != nil {
			return err
		}
	} else {
		return errors.New("unobserved Runner mutation")
	}
	record := map[string]any{"input_hash": w.inputHash, "request_id": rid, "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "handler_return_source": "owned_tls_bridge", "browser_ingress_host_matches": true, "downstream_host_matches": true}
	if write {
		record["kind"] = in.kind
		record["request_body_sha256"] = fmt.Sprintf("%x", in.bodyDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", in.keyDigest)
		record["committed_verified"] = true
	}
	w.mu.Lock()
	if _, duplicate := w.proofs[rid]; duplicate {
		w.mu.Unlock()
		return errors.New("duplicate response RID")
	}
	w.proofs[rid] = record
	err = w.writeProofLocked(rid)
	w.mu.Unlock()
	if err != nil {
		return err
	}
	if lost {
		clear(raw)
		return &reviewWebDrop{r.Header.Clone(), int(r.ContentLength)}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw)) // Exact original response, not a redacted wire substitute.
	return nil
}

func (w *systemRunnerWeb) observeRead(path string, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch path {
	case "/api/v1/session":
		var session struct {
			User struct {
				ID   string `json:"id"`
				Role string `json:"role"`
			} `json:"user"`
		}
		if json.Unmarshal(raw, &session) != nil || session.User.ID != w.a.p2.base.adminBrowser.actor.Details().UserID || session.User.Role != "admin" {
			return errors.New("Session is not original current Admin")
		}
	case "/api/v1/projects":
		var page map[string]any
		if json.Unmarshal(raw, &page) != nil {
			return errors.New("invalid App projects read")
		}
	case systemRunnerPath:
		var page rc.Page
		if agentWebDecode(raw, &page) != nil || page.Validate() != nil || page.Next != nil {
			return errors.New("invalid Runner page")
		}
		want := []rc.Snapshot{}
		if w.current.Validate() == nil {
			want = append(want, w.current.Clone())
		}
		if !runnerMountSameJSON(jsonAgentWeb(page.Items), jsonAgentWeb(want)) {
			return errors.New("Runner List differs from committed current metadata")
		}
		w.lists++
	default:
		var current rc.Snapshot
		if path != systemRunnerPath+"/"+w.target.String() || json.Unmarshal(raw, &current) != nil || !runnerMountSameJSON(jsonAgentWeb(current), jsonAgentWeb(w.current)) {
			return errors.New("Runner Get differs from original committed state")
		}
		w.details++
		w.reads[current.Version]++
	}
	return nil
}

func (w *systemRunnerWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	failure := w.failure
	for _, tail := range w.tails {
		if !tail.returned {
			failure = errors.New("owned TLS handler remains live")
		}
	}
	w.mu.Unlock()
	if failure != nil {
		t.Fatal("System Runner observation failed", failure)
	}
	wantUpdates, wantRevokes, wantLookups, wantVersion := 1, 1, 0, f.Version(4)
	wantCounts := [8]int64{4, 4, 2, 0, 3, 0, 0, 0}
	if w.mode == "system-runner-lost" {
		wantUpdates, wantRevokes, wantLookups, wantVersion = 0, 0, 1, 2
		wantCounts = [8]int64{2, 2, 2, 1, 2, 0, 0, 0}
	}
	if w.creates != 1 || w.updates != wantUpdates || w.enrollments != 1 || w.revokes != wantRevokes || w.lookups != wantLookups || w.cut != wantLookups || len(w.commands) != int(wantVersion) || len(w.receipts) != len(w.commands) || w.current.Version != wantVersion || w.lists < 2 {
		t.Fatal("System Runner omitted or repeated an explicit operation")
	}
	for version := f.Version(1); version <= wantVersion; version++ {
		minimum := 1
		if version == wantVersion {
			minimum = 2
		}
		if w.reads[version] < minimum {
			t.Fatal("independent current Get or explicit final refresh missing")
		}
	}
	counts, err := w.counts(ctx)
	agentAppRequire(t, err)
	if counts != wantCounts {
		t.Fatal("final commands/Audit/material/identity/no-device facts differ")
	}
	raw, err := os.ReadFile(filepath.Join(w.directory, "system-runner-management-result.json"))
	agentAppRequire(t, err)
	var result struct {
		Completed        bool   `json:"completed"`
		InputHash        string `json:"input_hash"`
		OriginalRequests int    `json:"original_requests"`
		AllObserved      bool   `json:"all_observed"`
		Creates          int    `json:"creates"`
		Updates          int    `json:"updates"`
		Enrollments      int    `json:"enrollments"`
		Revokes          int    `json:"revokes"`
		Lookups          int    `json:"lookups"`
		Lists            int    `json:"lists"`
		Details          int    `json:"details"`
	}
	if len(raw) > 16<<10 || agentWebDecode(raw, &result) != nil || !result.Completed || result.InputHash != w.inputHash || !result.AllObserved || result.OriginalRequests != len(w.proofs) || result.Creates != w.creates || result.Updates != w.updates || result.Enrollments != w.enrollments || result.Revokes != w.revokes || result.Lookups != w.lookups || result.Lists != w.lists || result.Details != w.details {
		t.Fatal("browser consumption result differs from original Go requests")
	}
	t.Logf("System Runner Management original facts creates=%d updates=%d enrollments=%d revokes=%d lookups=%d lists=%d details=%d proofs=%d; no device connection or Execution", w.creates, w.updates, w.enrollments, w.revokes, w.lookups, w.lists, w.details, len(w.proofs))
}

func (w *systemRunnerWeb) start(t *testing.T, address string) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("System Runner owned TLS verification disabled")
	}
	target, err := url.Parse(w.backend.URL)
	agentAppRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport, proxy.ErrorLog = w.transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse, proxy.ErrorHandler = w.response, w.proxyError
	static := http.FileServer(http.Dir(w.dist))
	w.front = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.frontWG.Add(1)
		defer w.frontWG.Done()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Host != w.listener.Addr().String() {
				w.fail(errors.New("browser ingress Host mismatch"))
				http.Error(out, "owned origin mismatch", 400)
				return
			}
			*r = *r.WithContext(context.WithValue(r.Context(), agentRunnerIngressKey{}, true))
			if err := w.observe(r); err != nil {
				w.fail(err)
				http.Error(out, "owned System Runner observation failed", 500)
				return
			}
			proxy.ServeHTTP(out, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(out, r)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(w.dist, clean)); err == nil && info.Mode().IsRegular() {
			static.ServeHTTP(out, r)
			return
		}
		http.ServeFile(out, r, filepath.Join(w.dist, "index.html"))
	})}
	go func() { w.serve <- w.front.Serve(w.listener) }()
}

// The return source is the owned TLS bridge, not an invented App log field.
// Original App bytes must reach EOF/Close before the bridge publishes its RID.
func (w *systemRunnerWeb) bridge(address string, out http.ResponseWriter, r *http.Request) {
	w.backendWG.Add(1)
	defer w.backendWG.Done()
	if r.Host != w.listener.Addr().String() {
		w.fail(errors.New("downstream Host mismatch"))
		http.Error(out, "owned downstream mismatch", 400)
		return
	}
	up := r.Clone(r.Context())
	u := *r.URL
	u.Scheme, u.Host = "http", address
	up.URL, up.RequestURI = &u, ""
	response, err := w.upstream.RoundTrip(up)
	if err != nil {
		w.fail(errors.New("original App RoundTrip failed"))
		http.Error(out, "upstream unavailable", 502)
		return
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 2<<20 || response.ContentLength != int64(len(raw)) {
		w.fail(errors.New("App EOF/Close/length incomplete"))
		http.Error(out, "upstream incomplete", 502)
		return
	}
	rid := response.Header.Get("X-Request-ID")
	parsed, err := f.ParseID[f.Request](rid)
	if err != nil || parsed.String() != rid || len(response.Header.Values("X-Request-ID")) != 1 {
		w.fail(errors.New("App RID missing"))
		http.Error(out, "upstream identity missing", 502)
		return
	}
	tail := &reviewWebTail{done: make(chan struct{})}
	w.mu.Lock()
	previous := w.tails[rid]
	if previous == nil {
		w.tails[rid] = tail
		w.hosts[rid] = agentRunnerHost{r.Method, r.URL.RequestURI(), up.Host == w.listener.Addr().String()}
	}
	w.mu.Unlock()
	if previous != nil {
		w.fail(errors.New("App RID repeated"))
		http.Error(out, "repeated identity", 502)
		return
	}
	defer func() { w.mu.Lock(); tail.returned = true; w.mu.Unlock(); close(tail.done) }()
	for key, values := range response.Header {
		out.Header()[key] = append([]string(nil), values...)
	}
	out.WriteHeader(response.StatusCode)
	if _, err = out.Write(raw); err != nil {
		w.fail(errors.New("TLS downstream Write failed"))
	}
}

func (w *systemRunnerWeb) writeProofLocked(rid string) error {
	raw, err := json.Marshal(w.proofs[rid])
	if err != nil {
		return err
	}
	path := filepath.Join(w.evidence, "system-runner-management-"+w.mode+"-response-"+rid+".json")
	if err = os.WriteFile(path+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func (w *systemRunnerWeb) proxyError(out http.ResponseWriter, r *http.Request, err error) {
	var drop *reviewWebDrop
	if !errors.As(err, &drop) {
		w.fail(errors.New("owned System Runner proxy response rejected"))
		http.Error(out, "owned proxy failed", 502)
		return
	}
	for key, values := range drop.header {
		out.Header()[key] = append([]string(nil), values...)
	}
	out.Header().Set("Content-Length", fmt.Sprint(drop.length))
	out.Header().Set("Connection", "close")
	out.WriteHeader(http.StatusOK)
	n, writeErr := io.WriteString(out, "{")
	flushErr := http.NewResponseController(out).Flush()
	conn, _, hijackErr := http.NewResponseController(out).Hijack()
	var closeErr error
	if conn != nil {
		closeErr = conn.Close()
	}
	if n != 1 || writeErr != nil || flushErr != nil || hijackErr != nil || conn == nil || closeErr != nil {
		w.fail(errors.New("physical original receipt cut did not complete"))
		return
	}
	rid := drop.header.Get("X-Request-ID")
	w.mu.Lock()
	record := w.proofs[rid]
	if record == nil || record["kind"] != "create" || w.cut != 0 {
		w.mu.Unlock()
		w.fail(errors.New("physical cut lacks original Create proof"))
		return
	}
	w.cut++
	record["receipt_cut"] = true
	err = w.writeProofLocked(rid)
	w.mu.Unlock()
	if err != nil {
		w.fail(errors.New("original cut proof could not be persisted"))
	}
}

func (w *systemRunnerWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "system-runner-management.config.js"))
	cmd.Dir, cmd.SysProcAttr, cmd.WaitDelay = root, &syscall.SysProcAttr{Setpgid: true}, time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	const prefix = "AGENTEAM_SYSTEM_RUNNER_MANAGEMENT_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned System Runner Management Playwright did not start")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		cmd.Env = nil
	}()
	err = <-done
	joined = true
	t.Logf("System Runner Management Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	failure := w.failure
	w.mu.Unlock()
	tokens = append(tokens, w.a.p2.base.adminBrowser.cookie, w.a.p2.base.adminBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	safe = regexp.MustCompile(`[A-Za-z0-9_-]{43}`).ReplaceAllString(safe, "[redacted-material]")
	t.Log(safe)
	output.Reset()
	if failure != nil {
		t.Error("owned System Runner Management observation failed", failure)
	}
	if err != nil {
		t.Fatal("original System Runner Management browser call failed and was waited")
	}
}
