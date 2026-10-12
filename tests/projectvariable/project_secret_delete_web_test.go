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
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	auditc "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	eventc "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// The browser cancels without a write, receives a real referenced-Secret Busy,
// clears the Agent allowlist through its existing UI and then deletes the Secret.
// P2 preparation and Secret material stay in Go; the exercised root is the real
// runtime-off App, with no Agent/reference/Secret business rows seeded by SQL.
func TestProjectSecretDeleteWeb(t *testing.T) {
	t.Run("busy-reference-then-delete", func(t *testing.T) {
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("Secret Delete original App handoff incomplete")
			}
		})
		a := newAgentCreateFixture(t)
		original := a.p2.base
		process, err := a.guard.CurrentProcess()
		agentAppRequire(t, err)
		secrets := agentSecretWebSeed(t, a)
		var username string
		agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id::text=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
		if original.afterOwners != nil {
			t.Fatal("Secret Delete owner handoff already occupied")
		}
		original.afterOwners = func() {
			entered = true
			if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
				t.Fatal("Secret Delete preparation providers have not joined")
			}
			if _, err := a.guard.CurrentProcess(); err == nil {
				t.Fatal("Secret Delete preparation guard remains held")
			}
			var stopped bool
			agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
			if !stopped {
				t.Fatal("Secret Delete preparation process has not retired")
			}
			w := newProjectSecretDeleteWeb(t, a, secrets)
			defer w.dispose(t)
			withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
				defer w.close(t)
				w.start(t, address)
				w.prepare(t, ctx)
				w.material(t, username)
				w.browser(t, ctx)
				w.verify(t, ctx)
				w.close(t)
			})
			completed = true
		}
	})
}

type projectSecretDeleteIntent struct {
	kind      string
	key       f.IdempotencyKey
	keyDigest [sha256.Size]byte
}
type projectSecretDeleteIntentKey struct{}

type projectSecretDeleteWeb struct {
	*agentSecretWeb
	baseline                string
	busy                    *projectSecretDeleteIntent
	clearing                *agentSecretIntent
	busyVerified, cleared   bool
	intent                  *projectSecretDeleteIntent
	deleted                 vc.SecretVariableMutation
	beforeLists, afterLists int
	targetReads, otherReads int
	responses               int
}

func newProjectSecretDeleteWeb(t *testing.T, a *agentCreateFixture, secrets []vc.SecretVariable) *projectSecretDeleteWeb {
	t.Helper()
	const prefix = "AGENTEAM_PROJECT_SECRET_DELETE_WEB"
	dist, runtime, evidence, inputHash := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME"), os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" || len(secrets) != 2 {
		t.Fatal("Secret Delete requires frozen assets/runtime/evidence/input and two safe Secrets")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Secret Delete frozen index missing")
	}
	directory, err := os.MkdirTemp(runtime, "psdw-")
	agentAppRequire(t, err)
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.RemoveAll(directory)
		t.Fatal("Secret Delete owned listener unavailable")
	}
	base := &agentConfigurationWeb{a: a, mode: "busy-reference-then-delete", origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}, draft: agentWebDraft{Name: "secret-delete-agent", Description: "Temporary reference for the Secret deletion test.", Instructions: "Use only explicitly allowed project Secrets."}}
	return &projectSecretDeleteWeb{agentSecretWeb: &agentSecretWeb{agentEditWeb: &agentEditWeb{agentConfigurationWeb: base}, secrets: secrets}}
}

func (w *projectSecretDeleteWeb) collection() string {
	return variableHTTPPath(w.a.p2.project.ID, "/secret-variables")
}
func (w *projectSecretDeleteWeb) secretPath(n int) string {
	return w.collection() + "/" + w.secrets[n].Fields().ID.String()
}

func (w *projectSecretDeleteWeb) start(t *testing.T, address string) {
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.agentConfigurationWeb.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("Secret Delete owned TLS verification disabled")
	}
	target, err := url.Parse(w.backend.URL)
	agentAppRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport, proxy.ErrorLog = w.transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse = w.response
	proxy.ErrorHandler = func(out http.ResponseWriter, _ *http.Request, _ error) {
		w.fail(errors.New("Secret Delete original proxy response rejected"))
		http.Error(out, "owned proxy failed", http.StatusBadGateway)
	}
	static := http.FileServer(http.Dir(w.dist))
	w.front = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.frontWG.Add(1)
		defer w.frontWG.Done()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if r.Host != w.listener.Addr().String() || w.observe(r) != nil {
				w.fail(errors.New("Secret Delete browser ingress rejected"))
				http.Error(out, "owned observation failed", http.StatusInternalServerError)
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

func (w *projectSecretDeleteWeb) prepare(t *testing.T, ctx context.Context) {
	w.seedAgent(t, ctx)
	var err error
	w.baseline, err = w.snapshot(ctx)
	agentAppRequire(t, err)
}

func (w *projectSecretDeleteWeb) material(t *testing.T, username string) {
	route := "/" + username + "/" + w.a.p2.project.NormalizedName + "/settings/"
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "origin": w.origin, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.a.p2.project.ID, "route": route + "secrets", "target": w.secrets[0], "remaining": w.secrets[1], "agent_id": w.target, "agent_name": w.draft.Name, "agent_route": route + "agents", "initial": w.initial, "input_hash": w.inputHash})
	defer clear(raw)
	agentAppRequire(t, projectSecretDeleteSafe(raw))
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "project-secret-delete-material.json"), raw, 0600))
}

func (w *projectSecretDeleteWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet {
		return nil
	}
	deleting := r.Method == http.MethodDelete && r.URL.Path == w.secretPath(0)
	updating := r.Method == http.MethodPatch && r.URL.Path == w.path()
	if !deleting && !updating || r.URL.RawQuery != "" {
		return errors.New("undeclared Secret Delete browser write")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2049))
	closed := r.Body.Close()
	if err != nil || closed != nil || len(raw) > 2048 {
		return errors.New("Secret Delete request incomplete")
	}
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("Secret Delete request identity changed")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.busy != nil && w.busy.key == key || w.clearing != nil && w.clearing.key == key || w.intent != nil {
		return errors.New("Secret Delete browser reused or repeated a mutation")
	}
	if deleting {
		var body struct {
			Expected f.Version `json:"expected_version"`
		}
		if agentWebDecode(raw, &body) != nil || body.Expected != 1 {
			return errors.New("Secret Delete original expected version changed")
		}
		in := projectSecretDeleteIntent{kind: "delete", key: key, keyDigest: sha256.Sum256([]byte(key))}
		if w.busy == nil {
			in.kind = "busy"
			w.busy = &in
		} else {
			if !w.busyVerified || !w.cleared {
				return errors.New("Secret Delete retried before the real Busy/reference removal")
			}
			w.intent = &in
		}
		*r = *r.WithContext(context.WithValue(r.Context(), projectSecretDeleteIntentKey{}, in))
	} else {
		var body struct {
			Expected f.Version       `json:"expected_version"`
			Request  json.RawMessage `json:"request"`
		}
		if !w.busyVerified || w.clearing != nil || agentWebDecode(raw, &body) != nil || body.Expected != 1 || !runnerMountSameJSON(body.Request, []byte(`{"allowed_secret_variable_ids":[]}`)) {
			return errors.New("Agent UI did not submit only the original Secret allowlist clear")
		}
		request, err := ac.DecodeAgentUpdate(body.Request)
		if err != nil {
			return errors.New("Agent Secret allowlist clear is not a valid Update")
		}
		in := agentSecretIntent{kind: "update", key: key, rawRequest: bytes.Clone(body.Request), request: request, before: w.initial.Clone(), secrets: []i.ProjectVariableID{}, bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
		w.clearing = &in
		*r = *r.WithContext(context.WithValue(r.Context(), agentSecretIntentKey{}, in))
	}
	w.keys = append(w.keys, string(key))
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (w *projectSecretDeleteWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	if r.Header.Get("Cache-Control") != "no-store" || projectSecretDeleteSafe(raw) != nil {
		return errors.New("Secret Delete original safe response rejected")
	}
	in, write := r.Request.Context().Value(projectSecretDeleteIntentKey{}).(projectSecretDeleteIntent)
	update, updating := r.Request.Context().Value(agentSecretIntentKey{}).(agentSecretIntent)
	if (!write || in.kind != "busy") && r.StatusCode != http.StatusOK {
		return errors.New("Secret Delete original response status unexpected")
	}
	if write {
		if in.kind == "busy" {
			var problem httpapi.Problem
			if r.StatusCode != http.StatusConflict || agentWebDecode(raw, &problem) != nil || problem.Code != f.ResourceBusy || problem.CommitState != f.NotCommitted || problem.RequestID.String() != r.Header.Get("X-Request-ID") || !slices.Equal(problem.FieldErrors, []f.FieldError{{Path: "/variable_id", Code: "SECRET_VARIABLE_REFERENCED"}}) {
				return errors.New("referenced Secret DELETE did not return the original Busy problem")
			}
			after, err := w.snapshot(r.Request.Context())
			if err != nil || after != w.baseline {
				return errors.New("Busy Secret DELETE changed persisted Secret facts")
			}
			if err = w.secretRelations(r.Request.Context(), w.current, "secret-allowlist-browser-original-create"); err != nil {
				return err
			}
			w.mu.Lock()
			w.busyVerified = true
			w.mu.Unlock()
		} else {
			var receipt vc.SecretVariableMutation
			if json.Unmarshal(raw, &receipt) != nil || receipt.Validate() != nil {
				return errors.New("Secret Delete original receipt invalid")
			}
			if err = w.deletedFacts(r.Request.Context(), in, receipt); err != nil {
				return err
			}
			w.mu.Lock()
			w.deleted = receipt.Clone()
			w.mu.Unlock()
		}
	} else if updating {
		receipt, err := ac.DecodeAgentMutation(raw)
		if err != nil {
			return errors.New("Agent UI clear response is not an original valid mutation")
		}
		rid, err := f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
		if err != nil {
			return err
		}
		if err = w.commandFacts(r.Request.Context(), update, rid, receipt); err != nil {
			return err
		}
		after, err := w.snapshot(r.Request.Context())
		if err != nil || after != w.baseline {
			return errors.New("Agent UI reference removal changed Secret metadata or command facts")
		}
		w.mu.Lock()
		w.current, w.cleared = receipt.Clone(), true
		w.mu.Unlock()
	} else if r.Request.Method == http.MethodGet {
		if err = w.read(r.Request.URL.Path, raw); err != nil {
			return err
		}
	} else {
		return errors.New("Secret Delete response lacks its original intent")
	}
	rid := r.Header.Get("X-Request-ID")
	proof := map[string]any{"input_hash": w.inputHash, "request_id": rid, "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "handler_return_source": "owned_tls_bridge", "request_host_matches": true}
	if write {
		proof["kind"], proof["idempotency_key_sha256"], proof["committed_verified"] = "delete", fmt.Sprintf("%x", in.keyDigest), in.kind == "delete"
		if in.kind == "busy" {
			proof["rejected_verified"] = true
		} else {
			proof["request_bound"], proof["input_verified"] = true, true
		}
	} else if updating {
		proof["kind"], proof["request_body_sha256"], proof["idempotency_key_sha256"], proof["committed_verified"] = "update", fmt.Sprintf("%x", update.bodyDigest), fmt.Sprintf("%x", update.keyDigest), true
		proof["request_bound"], proof["input_verified"] = true, true
	}
	encoded, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	path := filepath.Join(w.evidence, "project-secret-delete-"+w.mode+"-response-"+rid+".json")
	if err = os.WriteFile(path+".tmp", encoded, 0600); err != nil {
		return err
	}
	if err = os.Rename(path+".tmp", path); err != nil {
		return err
	}
	w.mu.Lock()
	w.responses++
	w.mu.Unlock()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (w *projectSecretDeleteWeb) read(path string, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if path == w.collection() {
		var page f.Page[vc.SecretVariable]
		if json.Unmarshal(raw, &page) != nil || page.NextCursor != "" {
			return errors.New("Secret Delete original directory invalid")
		}
		want := w.secrets
		if w.deleted.Validate() == nil {
			want = w.secrets[1:]
			w.afterLists++
		} else {
			w.beforeLists++
		}
		if len(page.Items) != len(want) {
			return errors.New("Secret Delete directory lost or retained a Secret")
		}
		for _, expected := range want {
			if !slices.ContainsFunc(page.Items, func(actual vc.SecretVariable) bool {
				return runnerMountSameJSON(jsonAgentWeb(expected), jsonAgentWeb(actual))
			}) {
				return errors.New("Secret Delete directory changed safe metadata")
			}
		}
	} else if path == w.secretPath(0) || path == w.secretPath(1) {
		n := 0
		if path == w.secretPath(1) {
			n = 1
			w.otherReads++
		} else {
			w.targetReads++
		}
		var metadata vc.SecretVariable
		if json.Unmarshal(raw, &metadata) != nil || metadata.Validate() != nil || !runnerMountSameJSON(raw, jsonAgentWeb(w.secrets[n])) {
			return errors.New("Secret Delete independent detail changed safe metadata")
		}
	}
	return nil
}

// Only safe metadata and counts are compared. Material, protected payloads and
// their digests are neither selected nor placed in failure messages/evidence.
func (w *projectSecretDeleteWeb) snapshot(ctx context.Context) (string, error) {
	var out string
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT jsonb_build_object(
 'variables',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM agenteam_projectvariable.variables v WHERE project_id::text=$1 AND type='secret'),
 'commands',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM agenteam_projectvariable.secret_commands c WHERE project_id::text=$1),
 'history',(SELECT jsonb_agg(to_jsonb(h) ORDER BY id) FROM agenteam_projectvariable.secret_history h WHERE project_id::text=$1),
 'generation',(SELECT query_generation FROM agenteam_projectvariable.secret_project_generations WHERE project_id::text=$1),
 'd04',(SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id::text=$1),
 'payloads',(SELECT count(*) FROM agenteam_secret.secret_payloads WHERE project_id::text=$1),
 'audit',(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND action IN ('project.secret_variable.create','project.secret_variable.update','project.secret_variable.delete')),
 'events',(SELECT count(*) FROM agenteam_outbox.events WHERE project_id::text=$1 AND event_type='project.secret_variable_changed'))::text`, w.a.p2.project.ID.String()).Scan(&out)
	return out, err
}

func (w *projectSecretDeleteWeb) deletedFacts(ctx context.Context, in projectSecretDeleteIntent, receipt vc.SecretVariableMutation) error {
	d := receipt.Fields().Deleted
	if receipt.Validate() != nil || receipt.Fields().Command != vc.SecretDeleteCommand || !receipt.Fields().Changed || d == nil || d.ID != w.secrets[0].Fields().ID || d.ProjectID != w.a.p2.project.ID || d.Version != 2 {
		return errors.New("Secret Delete safe tombstone identity/version invalid")
	}
	var exact bool
	var commandID string
	var stored, metadata, headerRaw, payload []byte
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT
 v.type='secret' AND v.value IS NULL AND v.description='' AND v.version=2 AND v.deleted_at=$4 AND v.updated_at=v.deleted_at
 AND v.credential_id IS NULL AND v.credential_version IS NULL
 AND c.expected_version=1 AND NOT c.name_present AND NOT c.description_present AND NOT c.value_present
 AND c.secret_effect='delete' AND c.secret_deleted AND c.credential_version=2 AND c.actor_user_id::text=$5
 AND r.command_kind=c.command_name AND r.external_expected_version=1 AND r.effect='delete' AND r.deleted AND r.result_version=2 AND r.credential_id=c.credential_id AND r.user_id=c.actor_user_id
 AND h.version=2 AND h.kind='deleted' AND h.changed_fields='["deleted"]'::jsonb AND h.actor_user_id=c.actor_user_id AND h.occurred_at=v.deleted_at
 AND a.action=c.command_name AND a.producer='projectvariable' AND a.actor_kind='human' AND a.user_id=c.actor_user_id AND a.session_id::text=$6 AND a.outcome='success'
 AND c.audit_id::text=$7 AND c.event_id::text=$8 AND e.producer='projectvariable' AND e.event_type='project.secret_variable_changed' AND e.schema_version=1
 AND v.xmin=c.xmin AND c.xmin=h.xmin AND c.xmin=r.xmin AND c.xmin=a.xmin AND c.xmin=e.xmin
 AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secrets s WHERE s.id=c.credential_id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_secret.secret_payloads s WHERE s.owner_kind=1 AND s.owner_id=c.credential_id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_references s WHERE s.project_id=v.project_id AND s.variable_id=v.id),
 c.id::text,c.receipt,a.metadata,e.header,e.payload
 FROM agenteam_projectvariable.variables v
 JOIN agenteam_projectvariable.secret_commands c ON c.project_id=v.project_id AND c.target_id=v.id
 JOIN agenteam_secret.project_variable_receipts r ON r.id=c.d04_receipt_id AND r.project_id=c.project_id AND r.variable_id=c.target_id
 JOIN agenteam_projectvariable.secret_history h ON h.project_id=c.project_id AND h.operation_id=c.id AND h.event_id=c.event_id
 JOIN agenteam_audit.audit_records a ON a.id=c.audit_id AND a.project_id=c.project_id AND a.resource_id=c.target_id
 JOIN agenteam_outbox.events e ON e.id=c.event_id AND e.project_id=c.project_id AND e.aggregate_id=c.target_id
 WHERE v.project_id::text=$1 AND v.id::text=$2 AND c.command_name='project.secret_variable.delete' AND c.idempotency_key=$3`, d.ProjectID.String(), d.ID.String(), string(in.key), d.DeletedAt.Time(), w.a.p2.base.ownerBrowser.actor.Details().UserID, w.a.p2.base.ownerBrowser.actor.Details().SessionID, receipt.Fields().AuditID.String(), receipt.Fields().EventID.String()).Scan(&exact, &commandID, &stored, &metadata, &headerRaw, &payload)
	defer clear(stored)
	defer clear(metadata)
	defer clear(headerRaw)
	defer clear(payload)
	if err != nil || !exact || !runnerMountSameJSON(stored, jsonAgentWeb(receipt)) {
		return errors.New("Secret Delete canonical/D04/history/Audit/Outbox atomic relation failed")
	}
	var counts [6]int64
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_projectvariable.secret_commands WHERE project_id::text=$1),
 (SELECT count(*) FROM agenteam_projectvariable.secret_history WHERE project_id::text=$1),
 (SELECT count(*) FROM agenteam_secret.project_variable_receipts WHERE project_id::text=$1),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND action IN ('project.secret_variable.create','project.secret_variable.update','project.secret_variable.delete')),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id::text=$1 AND event_type='project.secret_variable_changed'),
 (SELECT query_generation FROM agenteam_projectvariable.secret_project_generations WHERE project_id::text=$1)`, d.ProjectID.String()).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4], &counts[5])
	if err != nil || counts != ([6]int64{3, 3, 3, 3, 3, 4}) {
		return errors.New("Secret Delete duplicated or omitted command facts")
	}
	audit, err := auditc.DecodeMetadata(auditc.ProjectSecretVariableDelete, metadata)
	if err != nil {
		return errors.New("Secret Delete typed Audit invalid")
	}
	fields, err := audit.ProjectSecretVariableFields()
	if err != nil || fields.VariableID != d.ID.String() || fields.Version != d.Version || !slices.Equal(fields.ChangedFields, []string{"deleted"}) {
		return errors.New("Secret Delete Audit differs from safe tombstone")
	}
	header, err := eventc.DecodeHeader(headerRaw)
	var changed vc.SecretVariableChanged
	if err != nil || json.Unmarshal(payload, &changed) != nil || changed.Validate() != nil || header.EventID != *receipt.Fields().EventID || header.Scope.ProjectID.String() != d.ProjectID.String() || header.AggregateID.String() != d.ID.String() || header.AggregateVersion == nil || *header.AggregateVersion != d.Version || !header.OccurredAt.Time().Equal(d.DeletedAt.Time()) || changed.VariableID != d.ID || changed.OperationID.String() != commandID || changed.Change != vc.Deleted || !slices.Equal(changed.ChangedFields, []string{"deleted"}) {
		return errors.New("Secret Delete Event differs from safe tombstone")
	}
	return nil
}

func (w *projectSecretDeleteWeb) browser(t *testing.T, parent context.Context) {
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "project-secret-delete.config.js"))
	cmd.Dir, cmd.SysProcAttr, cmd.WaitDelay = root, &syscall.SysProcAttr{Setpgid: true}, time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	const prefix = "AGENTEAM_PROJECT_SECRET_DELETE_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash, prefix+"_CASE="+w.mode)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal("Secret Delete Playwright did not start")
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
	t.Logf("Secret Delete Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	agentAppRequire(t, projectSecretDeleteSafe(output.Bytes()))
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.a.p2.base.ownerBrowser.cookie, w.a.p2.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("Secret Delete original browser call failed")
	}
}

func (w *projectSecretDeleteWeb) verify(t *testing.T, ctx context.Context) {
	w.mu.Lock()
	failure, intent, receipt := w.failure, w.intent, w.deleted.Clone()
	busy, cleared, clearing := w.busyVerified, w.cleared, w.clearing
	before, after, target, other, responses := w.beforeLists, w.afterLists, w.targetReads, w.otherReads, w.responses
	w.mu.Unlock()
	if failure != nil || !busy || !cleared || clearing == nil || intent == nil || receipt.Validate() != nil || before < 1 || after < 1 || target < 1 || other < 1 {
		t.Fatal("Secret Delete original browser reads/one deletion incomplete", failure)
	}
	path := filepath.Join(w.directory, "project-secret-delete-result.json")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 {
		t.Fatal("Secret Delete private browser result missing or oversized")
	}
	raw, err := os.ReadFile(path)
	agentAppRequire(t, err)
	defer clear(raw)
	agentAppRequire(t, projectSecretDeleteSafe(raw))
	var result struct {
		Completed        bool   `json:"completed"`
		InputHash        string `json:"input_hash"`
		OriginalRequests int    `json:"original_requests"`
		AllObserved      bool   `json:"all_observed"`
		Deletes          int    `json:"deletes"`
		Updates          int    `json:"updates"`
		Lookups          int    `json:"lookups"`
	}
	if agentWebDecode(raw, &result) != nil || !result.Completed || result.InputHash != w.inputHash || !result.AllObserved || result.OriginalRequests != responses || result.Deletes != 2 || result.Updates != 1 || result.Lookups != 0 {
		t.Fatal("Secret Delete original browser observer result differs from Go observations")
	}
	query := jsonBytes(t, map[string]any{"command": vc.SecretDeleteCommand, "target_id": w.secrets[0].Fields().ID, "expected_version": f.Version(1)})
	reply := w.request(t, w.a.p2.base.ownerBrowser, http.MethodPost, w.collection()+"/commands/lookup", query, intent.key)
	var found vc.SecretVariableCommandLookup
	if reply.status != http.StatusOK || json.Unmarshal(reply.body, &found) != nil || found.Validate() != nil || found.Status() != vc.SecretLookupCommitted || found.Receipt() == nil || !runnerMountSameJSON(jsonAgentWeb(*found.Receipt()), jsonAgentWeb(receipt)) {
		t.Fatal("Secret Delete original-key Lookup lost its exact receipt")
	}
	agentAppRequire(t, projectSecretDeleteSafe(reply.body))
	clear(reply.body)
	agentAppRequire(t, w.deletedFacts(ctx, *intent, receipt))
	missing := w.request(t, w.a.p2.base.ownerBrowser, http.MethodGet, w.secretPath(0), nil, "")
	var problem httpapi.Problem
	if missing.status != http.StatusNotFound || json.Unmarshal(missing.body, &problem) != nil || problem.Code != f.NotFound {
		t.Fatal("deleted Secret remains available through current Get")
	}
	agentAppRequire(t, projectSecretDeleteSafe(missing.body))
	clear(missing.body)
	agentAppRequire(t, w.secretRelations(ctx, w.current, clearing.key))
	agentAppRequire(t, projectSecretDeleteSafe([]byte(w.a.p2.base.logs.text())))
	agentAppRequire(t, filepath.WalkDir(w.evidence, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".json" && filepath.Ext(path) != ".log" {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		defer clear(raw)
		return projectSecretDeleteSafe(raw)
	}))
	t.Logf("Secret Delete browser: Busy DELETE with no Secret effects, one Agent allowlist PATCH, one successful DELETE; Go original-key Lookup, safe tombstone and independent remaining Secret Get; response_proofs=%d", responses)
}

func projectSecretDeleteSafe(raw []byte) error {
	for n := 1; n <= 2; n++ {
		material := []byte(fmt.Sprintf("owned-agent-secret-allowlist-value-%d", n))
		digest := sha256.Sum256(material)
		for _, forbidden := range [][]byte{material, []byte(base64.StdEncoding.EncodeToString(material)), []byte(fmt.Sprintf("%x", digest))} {
			if bytes.Contains(raw, forbidden) {
				clear(material)
				return errors.New("Secret material or enumerable digest escaped its Go-only boundary")
			}
		}
		clear(material)
	}
	return nil
}
