//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	auditc "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	eventc "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// The Project/Model publication is a fixture prerequisite. The target Agent
// and every tested Update are created through the original runtime-off App.
func TestAgentConfigurationUpdateApp(t *testing.T) {
	for _, mode := range []string{"changed-noop-and-original-version", "current-owner-and-original-update-lookup"} {
		t.Run(mode, func(t *testing.T) { agentEditHandoff(t, mode, false) })
	}
}

func TestAgentEditWeb(t *testing.T) {
	for _, mode := range []string{"changed-noop-and-clear", "lost-update-confirmation-only-lookup"} {
		t.Run(mode, func(t *testing.T) { agentEditHandoff(t, mode, true) })
	}
}

func agentEditHandoff(t *testing.T, mode string, browser bool) {
	t.Helper()
	entered, completed := false, false
	t.Cleanup(func() {
		if !entered || !completed {
			t.Error("Agent Update original App handoff did not enter and complete")
		}
	})
	a := newAgentCreateFixture(t)
	original := a.p2.base
	process, err := a.guard.CurrentProcess()
	agentAppRequire(t, err)
	registration := agentAppRegistration(t, a)
	var username string
	agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id::text=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username))
	if original.afterOwners != nil {
		t.Fatal("original fixture already has an owner handoff")
	}
	original.afterOwners = func() {
		entered = true
		if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
			t.Fatal("Agent Update setup providers have not joined")
		}
		if _, err := a.guard.CurrentProcess(); err == nil {
			t.Fatal("Agent Update setup ProcessGuard remains held")
		}
		var stopped bool
		agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
		if !stopped {
			t.Fatal("Agent Update setup process has not retired")
		}
		w := newAgentEditWeb(t, a, mode, browser)
		defer w.dispose(t)
		withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
			defer w.close(t)
			w.start(t, address)
			if agentAppRegistration(t, a) != registration {
				t.Fatal("App changed the fixed builtin registration")
			}
			w.create(t, ctx)
			if browser {
				w.material(t, username)
				w.browser(t, ctx)
				w.close(t)
				w.verifyBrowser(t, ctx)
			} else {
				w.commands(t, ctx)
			}
		})
		completed = true
	}
}

type agentEditIntent struct {
	kind                  string
	key                   f.IdempotencyKey
	before                ac.AgentConfig
	request               ac.AgentUpdate
	rawRequest            []byte
	bodyDigest, keyDigest [sha256.Size]byte
}
type agentEditIntentKey struct{}

// Reuse the accepted owned App/TLS bridge, handler tails and Stop/Join methods.
// Only this profile's request/receipt observers and material differ.
type agentEditWeb struct {
	*agentConfigurationWeb
	target    i.AgentID
	initial   ac.AgentConfig
	current   ac.AgentMutation
	writes    []agentEditIntent
	mutations []ac.AgentMutation
	changed   int
}

func newAgentEditWeb(t *testing.T, a *agentCreateFixture, mode string, browser bool) *agentEditWeb {
	t.Helper()
	const prefix = "AGENTEAM_AGENT_EDIT_WEB"
	dist, runtime := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence, inputHash := os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if browser {
		if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
			t.Fatal("Agent Edit requires frozen dist, short runtime, evidence and input hash")
		}
		if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
			t.Fatal("frozen Agent Edit production index missing")
		}
	} else {
		runtime = ""
	}
	directory, err := os.MkdirTemp(runtime, "aew-")
	agentAppRequire(t, err)
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.RemoveAll(directory)
		t.Fatal(err)
	}
	base := &agentConfigurationWeb{a: a, mode: mode, origin: "http://" + listener.Addr().String(), directory: directory,
		evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{},
		draft: agentWebDraft{Name: "edit-agent", Description: "Original Agent description.", Instructions: "Original Agent instructions.\nKeep omitted fields unchanged."}}
	return &agentEditWeb{agentConfigurationWeb: base, target: id[i.Agent](t)}
}

func (w *agentEditWeb) start(t *testing.T, address string) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned Agent Edit TLS verification disabled")
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
			if err := w.observe(r); err != nil {
				w.fail(err)
				http.Error(out, "owned edit observation failed", 500)
				return
			}
			proxy.ServeHTTP(out, r)
			return
		}
		if w.dist == "" || r.Method != http.MethodGet && r.Method != http.MethodHead {
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

func (w *agentEditWeb) path() string {
	return variableHTTPPath(w.a.p2.project.ID, "/agents/"+w.target.String()+"/configuration")
}
func (w *agentEditWeb) lookupPath() string {
	return variableHTTPPath(w.a.p2.project.ID, "/agent-commands/lookup")
}

// Direct native requests use the same verified TLS bridge and original App,
// with the configured public Origin. They cannot supply a successful response.
func (w *agentEditWeb) request(t *testing.T, actor variableHTTPBrowser, method, path string, body []byte, key f.IdempotencyKey) variableHTTPResponse {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, method, w.backend.URL+path, bytes.NewReader(body))
	agentAppRequire(t, err)
	public, err := url.Parse(w.origin)
	agentAppRequire(t, err)
	if public.Scheme != "http" || public.Host != w.listener.Addr().String() {
		t.Fatal("Agent Edit public origin differs from its owned front")
	}
	// Preserve the public authority and local cookie exactly as the browser
	// front does; the separate backend URL still supplies verified TLS.
	r.Host = public.Host
	r.AddCookie(&http.Cookie{Name: "agenteam_local_session", Value: actor.cookie, HttpOnly: true, Path: "/"})
	r.Header.Set("Origin", w.origin)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	if method != http.MethodGet && method != http.MethodHead {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", actor.csrf)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", string(key))
	}
	w.mu.Lock()
	w.keys = append(w.keys, string(key))
	w.mu.Unlock()
	client := &http.Client{Transport: w.transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(r)
	agentAppRequire(t, err)
	raw, err := w.completedResponse(response)
	agentAppRequire(t, err)
	if response.StatusCode >= http.StatusBadRequest {
		var problem struct {
			Code f.Code `json:"code"`
		}
		decoded := json.Unmarshal(raw, &problem) == nil
		_, cookieErr := r.Cookie("agenteam_local_session")
		t.Logf("Agent Edit original HTTP status=%d problem_code=%s problem_known=%t public_host_matches=%t origin_present=%t session_cookie_present=%t csrf_present=%t key_present=%t",
			response.StatusCode, problem.Code.Safe(), decoded && problem.Code.Known(), r.Host == public.Host,
			r.Header.Get("Origin") != "", cookieErr == nil, r.Header.Get("X-CSRF-Token") != "", r.Header.Get("Idempotency-Key") != "")
	}
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("original Agent Edit response omitted no-store")
	}
	return variableHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: raw}
}

func (w *agentEditWeb) completedResponse(r *http.Response) ([]byte, error) {
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
	closeErr := r.Body.Close()
	validLength := r.ContentLength == int64(len(raw))
	if r.Request.Method == http.MethodHead {
		validLength = len(raw) == 0 && r.ContentLength >= 0
	}
	if readErr != nil || closeErr != nil || len(raw) > 2<<20 || !validLength || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		return nil, errors.New("original Agent Edit TLS EOF/Close/length incomplete")
	}
	rid := r.Header.Get("X-Request-ID")
	parsed, err := f.ParseID[f.Request](rid)
	if err != nil || parsed.String() != rid || len(r.Header.Values("X-Request-ID")) != 1 {
		return nil, errors.New("original Agent Edit response identity invalid")
	}
	w.mu.Lock()
	tail := w.tails[rid]
	w.mu.Unlock()
	if tail == nil {
		return nil, errors.New("original Agent Edit handler absent")
	}
	select {
	case <-tail.done:
	case <-r.Request.Context().Done():
		return nil, r.Request.Context().Err()
	}
	w.mu.Lock()
	returned := tail.returned
	w.mu.Unlock()
	if !returned {
		return nil, errors.New("original Agent Edit handler has not returned")
	}
	return raw, nil
}

func (w *agentEditWeb) create(t *testing.T, ctx context.Context) {
	t.Helper()
	rawRequest := jsonBytes(t, map[string]any{"agent_id": w.target, "name": w.draft.Name, "description": w.draft.Description, "instructions": w.draft.Instructions,
		"model_ref": w.a.modelID, "inject_agents_md": false, "approval_policy": "default", "allowed_tool_ids": []string{}, "allowed_mount_ids": []string{}, "allowed_secret_variable_ids": []string{}})
	request, err := ac.DecodeAgentCreate(rawRequest)
	agentAppRequire(t, err)
	const key f.IdempotencyKey = "agent-edit-original-create"
	reply := w.request(t, w.a.p2.base.ownerBrowser, http.MethodPost, variableHTTPPath(w.a.p2.project.ID, "/agents"), jsonBytes(t, map[string]any{"request": json.RawMessage(rawRequest)}), key)
	if reply.status != 200 {
		t.Fatal("original App could not create edit target", reply.status)
	}
	receipt, err := ac.DecodeAgentMutation(reply.body)
	agentAppRequire(t, err)
	rid, err := f.ParseID[f.Request](reply.header.Get("X-Request-ID"))
	agentAppRequire(t, err)
	agentAppRequire(t, w.committedFacts(ctx, agentWebIntent{kind: "create", key: key, request: request, rawRequest: rawRequest}, rid, receipt))
	w.initial, w.current = receipt.Fields().Agent.Clone(), receipt.Clone()
}

func (w *agentEditWeb) material(t *testing.T, username string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"mode": w.mode, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.a.p2.project.ID,
		"agent_id": w.target, "version": w.initial.Fields().Core.Version, "route": "/" + username + "/" + w.a.p2.project.NormalizedName + "/settings/agents", "initial": w.draft})
	agentAppRequire(t, err)
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "agent-edit-material.json"), raw, 0600))
	clear(raw)
}

func (w *agentEditWeb) updateFacts(ctx context.Context, intent agentEditIntent, rid f.ID[f.Request], receipt ac.AgentMutation) error {
	a, before, mutation := w.a, intent.before.Fields(), receipt.Fields()
	if intent.request.Validate() != nil || receipt.Validate() != nil || intent.before.Validate() != nil {
		return errors.New("invalid original Update fact")
	}
	after := mutation.Agent.Fields()
	proposed, err := intent.request.Proposed(intent.before)
	if err != nil {
		return err
	}
	var oldFields, newFields map[string]json.RawMessage
	if json.Unmarshal(jsonAgentWeb(intent.before), &oldFields) != nil || json.Unmarshal(jsonAgentWeb(proposed), &newFields) != nil {
		return errors.New("original Update fields did not encode")
	}
	changedFields := []string{}
	for _, field := range ac.EditableFields() {
		if !bytes.Equal(oldFields[field], newFields[field]) {
			changedFields = append(changedFields, field)
		}
	}
	changed := len(changedFields) != 0
	wantVersion := before.Core.Version
	if changed {
		wantVersion++
	}
	if mutation.Changed != changed || after.Core.Version != wantVersion || after.Core.ID != w.target || after.Core.ProjectID != a.p2.project.ID ||
		len(mutation.EventIDs) != map[bool]int{false: 0, true: 1}[changed] || after.Core.CreatedAt != before.Core.CreatedAt ||
		(changed && !after.Core.UpdatedAt.Time().After(before.Core.UpdatedAt.Time())) || (!changed && after.Core.UpdatedAt != before.Core.UpdatedAt) {
		return errors.New("Update receipt changed original identity/version/noop semantics")
	}
	wantFields := proposed.Fields()
	wantFields.Core.Version, wantFields.Core.UpdatedAt = wantVersion, after.Core.UpdatedAt
	want, err := ac.NewAgentConfig(wantFields)
	if err != nil || !bytes.Equal(jsonAgentWeb(want), jsonAgentWeb(mutation.Agent)) {
		return errors.New("Update receipt differs from full presence-sensitive patch")
	}
	meta := f.CommandMeta{RequestID: rid, IdempotencyKey: intent.key, ExpectedVersion: &before.Core.Version}
	digest, err := ac.AgentCommandDigest(a.p2.base.ownerBrowser.actor, meta, a.p2.project.ID, w.target, ac.UpdateAgentCommand, intent.request)
	if err != nil {
		return err
	}
	identity, err := ac.AgentCommandIdentity(a.p2.project.ID, ac.UpdateAgentCommand, intent.key)
	if err != nil {
		return err
	}
	var commandID string
	var stored, storedInput, storedBefore, storedAfter, fieldsRaw []byte
	var exact bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT id::text,input,before_config,after_config,receipt,changed_fields,
 expected_version=$6 AND add_skills_enabled IS NULL AND install_skill_enabled IS NULL AND plan_revision>=1 AND committed_at IS NOT NULL
 FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND command_name='agent.update'
 AND idempotency_key=$3 AND semantic_digest=$4 AND actor_user_id::text=$5 AND state='completed'`, a.p2.project.ID.String(), w.target.String(), string(intent.key), digest.String(), a.p2.base.ownerBrowser.actor.Details().UserID, int64(before.Core.Version)).Scan(&commandID, &storedInput, &storedBefore, &storedAfter, &stored, &fieldsRaw, &exact)
	if err != nil {
		return err
	}
	storedRequest, inputErr := ac.DecodeAgentUpdate(storedInput)
	preimage, beforeErr := ac.DecodeAgentConfig(storedBefore)
	postimage, afterErr := ac.DecodeAgentConfig(storedAfter)
	mutationStored, receiptErr := ac.DecodeAgentMutation(stored)
	var fields []string
	if inputErr != nil || beforeErr != nil || afterErr != nil || receiptErr != nil || !exact || json.Unmarshal(fieldsRaw, &fields) != nil ||
		!bytes.Equal(jsonAgentWeb(storedRequest), jsonAgentWeb(intent.request)) || !bytes.Equal(jsonAgentWeb(preimage), jsonAgentWeb(intent.before)) ||
		!bytes.Equal(jsonAgentWeb(postimage), jsonAgentWeb(mutation.Agent)) || !bytes.Equal(jsonAgentWeb(mutationStored), jsonAgentWeb(receipt)) || !slices.Equal(fields, changedFields) {
		return errors.New("durable Update command lost original input/preimage/postimage/receipt")
	}
	var canonical bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT name=$3 AND normalized_name=$4 AND description=$5 AND instructions=$6
 AND display_name IS NOT DISTINCT FROM $7::text AND tag_color IS NOT DISTINCT FROM $8::text
 AND inject_agents_md=$9 AND model_id::text=$10 AND approval_policy=$11 AND approval_model_id IS NULL AND reasoning_effort IS NULL
 AND lifecycle='active' AND version=$12 AND created_at=$13 AND updated_at=$14
 FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2`, a.p2.project.ID.String(), w.target.String(), after.Core.Name, after.Core.NormalizedName,
		after.Core.Description, after.Core.Instructions, after.Core.DisplayName, after.Core.TagColor, after.Core.InjectAgentsMD, after.Core.ModelRef.String(), string(after.Core.ApprovalPolicy), int64(wantVersion), after.Core.CreatedAt.Time(), after.Core.UpdatedAt.Time()).Scan(&canonical)
	if err != nil || !canonical {
		return errors.New("Update canonical row differs from its actual full receipt")
	}
	var refs bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*)=1 AND bool_and(project_id::text=$1 AND model_id::text=$3 AND owner_version=$5 AND reasoning_effort='' AND role='agent_model') FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id::text=$2)
 AND (SELECT count(*)=1 AND bool_and(tool_id::text=$4) FROM agenteam_agent.tool_allowlist WHERE project_id::text=$1 AND agent_id::text=$2)
 AND EXISTS(SELECT 1 FROM agenteam_tool.agent_configurations h JOIN agenteam_tool.agent_references r ON r.agent_id=h.agent_id WHERE h.project_id::text=$1 AND h.agent_id::text=$2 AND h.config_version=$5 AND r.tool_id::text=$4)
 AND EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads WHERE project_id::text=$1 AND agent_id::text=$2 AND owner_version=$5)
 AND NOT EXISTS(SELECT 1 FROM agenteam_agent.mount_allowlist WHERE project_id::text=$1 AND agent_id::text=$2)
 AND NOT EXISTS(SELECT 1 FROM agenteam_agent.secret_allowlist WHERE project_id::text=$1 AND agent_id::text=$2)
 AND EXISTS(SELECT 1 FROM agenteam_skill.agent_assignment_heads h JOIN agenteam_skill.agent_assignments s ON s.project_id=h.project_id AND s.agent_id=h.agent_id AND s.id=h.initial_assignment_id
 WHERE h.project_id::text=$1 AND h.agent_id::text=$2 AND h.add_skills_enabled AND h.assignment_sequence=1 AND s.enabled AND s.removed_at IS NULL)`, a.p2.project.ID.String(), w.target.String(), a.modelID.String(), a.tool.ToolID.String(), int64(wantVersion)).Scan(&refs)
	if err != nil {
		return err
	}
	if !refs || !slices.Equal(after.AllowedToolIDs, []i.ToolID{a.tool.ToolID}) || len(after.AllowedMountIDs) != 0 || len(after.AllowedSecretVariableIDs) != 0 {
		return errors.New("Update changed omitted default Skill/Tool or reference versions")
	}
	var auditCount, eventCount int
	err = a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND resource_id::text=$2 AND action='agent.update' AND metadata->>'command_id'=$3),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent' AND id::text=$3)`, a.p2.project.ID.String(), w.target.String(), commandID).Scan(&auditCount, &eventCount)
	if err != nil {
		return err
	}
	wantCount := 0
	if changed {
		wantCount = 1
	}
	if auditCount != wantCount || eventCount != wantCount {
		return errors.New("Update/noop duplicated or omitted Audit/Outbox")
	}
	if !changed {
		return ctx.Err()
	}
	if mutation.EventIDs[0].String() != commandID {
		return errors.New("Update event lost original command identity")
	}
	var auditRaw, headerRaw, payloadRaw []byte
	var user, session, cause string
	err = a.p2.base.raw.QueryRow(ctx, `SELECT metadata,user_id::text,session_id::text,cause_ref FROM agenteam_audit.audit_records
 WHERE project_id::text=$1 AND resource_id::text=$2 AND producer='agent' AND action='agent.update' AND metadata->>'command_id'=$3
 AND scope='project' AND actor_kind='human' AND outcome='success' AND resource_kind='agent' AND ordinal=0`, a.p2.project.ID.String(), w.target.String(), commandID).Scan(&auditRaw, &user, &session, &cause)
	if err != nil {
		return err
	}
	metadata, err := auditc.DecodeMetadata(auditc.AgentUpdate, auditRaw)
	if err != nil {
		return err
	}
	af, err := metadata.AgentFields()
	cd, causeErr := cursor.Digest([]byte(identity.Canonical()))
	if err != nil || causeErr != nil || cause != cd.String() || user != a.p2.base.ownerBrowser.actor.Details().UserID || session != a.p2.base.ownerBrowser.actor.Details().SessionID || af.AgentID != w.target.String() || af.CommandID != commandID || af.Version != wantVersion || !slices.Equal(af.ChangedFields, changedFields) {
		return errors.New("Update audit lost original Owner/command/fields")
	}
	var columns bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,event_type='agent.config_changed' AND schema_version=1 AND scope='project' AND aggregate_type='agent.config' AND aggregate_version=$4 AND aggregate_sequence IS NULL AND occurred_at=$5
 FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent' AND id::text=$3`, a.p2.project.ID.String(), w.target.String(), commandID, int64(wantVersion), after.Core.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &columns)
	if err != nil {
		return err
	}
	header, err := eventc.DecodeHeader(headerRaw)
	var payload ac.ConfigChangedPayload
	if err != nil || !columns || ac.ValidateAgentEventHeader(header) != nil || json.Unmarshal(payloadRaw, &payload) != nil || payload.Validate() != nil || header.EventID != mutation.EventIDs[0] || header.AggregateID.String() != w.target.String() || header.Scope.ProjectID.String() != a.p2.project.ID.String() || *header.AggregateVersion != wantVersion || !header.OccurredAt.Time().Equal(after.Core.UpdatedAt.Time()) || payload.CommandID.String() != commandID || payload.ActorUserID.String() != user || payload.Operation != ac.ConfigUpdated || !slices.Equal(payload.ChangedFields, changedFields) {
		return errors.New("Update Outbox differs from original command/receipt")
	}
	return ctx.Err()
}

func (w *agentEditWeb) counts(ctx context.Context) ([5]int64, error) {
	var v [5]int64
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT version FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2),
 (SELECT count(*) FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2),
 (SELECT count(*) FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND command_name='agent.update' AND state='completed'),
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND resource_id::text=$2 AND producer='agent'),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent')`, w.a.p2.project.ID.String(), w.target.String()).Scan(&v[0], &v[1], &v[2], &v[3], &v[4])
	return v, err
}

func agentEditLookupBody(t *testing.T, target i.AgentID, in agentEditIntent) []byte {
	return jsonBytes(t, map[string]any{"command": ac.UpdateAgentCommand, "target_id": target, "expected_version": in.before.Fields().Core.Version, "request": json.RawMessage(in.rawRequest)})
}

func (w *agentEditWeb) nativeUpdate(t *testing.T, ctx context.Context, key f.IdempotencyKey, patch string) (agentEditIntent, ac.AgentMutation) {
	t.Helper()
	request, err := ac.DecodeAgentUpdate([]byte(patch))
	agentAppRequire(t, err)
	in := agentEditIntent{kind: "update", key: key, before: w.current.Fields().Agent.Clone(), request: request, rawRequest: []byte(patch)}
	body := jsonBytes(t, map[string]any{"expected_version": in.before.Fields().Core.Version, "request": json.RawMessage(patch)})
	reply := w.request(t, w.a.p2.base.ownerBrowser, http.MethodPatch, w.path(), body, key)
	if reply.status != 200 {
		t.Fatal("original App Update failed", reply.status)
	}
	receipt, err := ac.DecodeAgentMutation(reply.body)
	agentAppRequire(t, err)
	rid, err := f.ParseID[f.Request](reply.header.Get("X-Request-ID"))
	agentAppRequire(t, err)
	agentAppRequire(t, w.updateFacts(ctx, in, rid, receipt))
	w.current = receipt.Clone()
	return in, receipt
}

func (w *agentEditWeb) commands(t *testing.T, ctx context.Context) {
	t.Helper()
	owner := w.a.p2.base.ownerBrowser
	original, receipt := w.nativeUpdate(t, ctx, "agent-update-original", `{"name":"edit-agent-updated","display_name":"Explicit display","description":""}`)
	if receipt.Fields().Agent.Fields().Core.Instructions != w.draft.Instructions {
		t.Fatal("omitted instructions changed")
	}
	if w.mode == "changed-noop-and-original-version" {
		_, noop := w.nativeUpdate(t, ctx, "agent-update-noop", `{"name":"edit-agent-updated"}`)
		if noop.Fields().Changed || len(noop.Fields().EventIDs) != 0 || !bytes.Equal(jsonAgentWeb(noop.Fields().Agent), jsonAgentWeb(receipt.Fields().Agent)) {
			t.Fatal("same-value Update changed canonical version/time")
		}
		beforeCounts, err := w.counts(ctx)
		agentAppRequire(t, err)
		stale := jsonBytes(t, map[string]any{"expected_version": f.Version(1), "request": json.RawMessage(`{"description":"stale"}`)})
		agentAppProblem(t, w.request(t, owner, http.MethodPatch, w.path(), stale, "agent-update-stale"), http.StatusConflict, f.VersionConflict)
		wrong := jsonBytes(t, map[string]any{"command": ac.UpdateAgentCommand, "target_id": w.target, "expected_version": f.Version(1), "request": json.RawMessage(`{"name":"different-intent"}`)})
		agentAppProblem(t, w.request(t, owner, http.MethodPost, w.lookupPath(), wrong, original.key), http.StatusConflict, f.IdempotencyKeyReused)
		afterCounts, err := w.counts(ctx)
		agentAppRequire(t, err)
		if beforeCounts != ([5]int64{2, 3, 2, 2, 2}) || afterCounts != beforeCounts {
			t.Fatal("stale/foreign intent changed durable Update facts")
		}
	} else {
		// Advance current configuration, then recover the original v2 receipt.
		_, cleared := w.nativeUpdate(t, ctx, "agent-update-clear", `{"display_name":null,"instructions":""}`)
		if cleared.Fields().Agent.Fields().Core.DisplayName != nil || cleared.Fields().Agent.Fields().Core.Instructions != "" || cleared.Fields().Agent.Fields().Core.Version != 3 {
			t.Fatal("explicit nullable/text clear was not persisted")
		}
		beforeCounts, err := w.counts(ctx)
		agentAppRequire(t, err)
		foreignPatch := jsonBytes(t, map[string]any{"expected_version": f.Version(3), "request": json.RawMessage(`{"description":"forbidden"}`)})
		agentAppProblem(t, w.request(t, w.a.p2.base.otherBrowser, http.MethodPatch, w.path(), foreignPatch, "agent-update-foreign-owner"), http.StatusNotFound, f.NotFound)
		agentAppProblem(t, w.request(t, w.a.p2.base.otherBrowser, http.MethodPost, w.lookupPath(), agentEditLookupBody(t, w.target, original), original.key), http.StatusNotFound, f.NotFound)
		badCSRF := owner
		badCSRF.csrf = "invalid-agent-update-csrf"
		rejected := w.request(t, badCSRF, http.MethodPatch, w.path(), foreignPatch, "agent-update-wrong-csrf")
		if rejected.status != http.StatusForbidden {
			t.Fatal("Update bypassed current Session CSRF", rejected.status)
		}
		afterCounts, err := w.counts(ctx)
		agentAppRequire(t, err)
		if beforeCounts != ([5]int64{3, 3, 2, 3, 3}) || afterCounts != beforeCounts {
			t.Fatal("current Owner/CSRF rejection changed Agent facts")
		}
	}
	beforeCounts, err := w.counts(ctx)
	agentAppRequire(t, err)
	body := agentEditLookupBody(t, w.target, original)
	reply := w.request(t, owner, http.MethodPost, w.lookupPath(), body, original.key)
	lookup, err := ac.DecodeAgentCommandLookup(reply.body)
	agentAppRequire(t, err)
	if reply.status != 200 || lookup.Status() != ac.AgentLookupCommitted || lookup.Receipt() == nil || !bytes.Equal(jsonAgentWeb(*lookup.Receipt()), jsonAgentWeb(receipt)) {
		t.Fatal("Update Lookup replaced the original historical receipt")
	}
	get := w.request(t, owner, http.MethodGet, w.path(), nil, "")
	config, err := ac.DecodeAgentConfig(get.body)
	agentAppRequire(t, err)
	if get.status != 200 || !bytes.Equal(jsonAgentWeb(config), jsonAgentWeb(w.current.Fields().Agent)) {
		t.Fatal("current configuration Get differs from durable Update")
	}
	if w.mode == "current-owner-and-original-update-lookup" {
		logout := w.request(t, owner, http.MethodPost, "/api/v1/sessions/logout", []byte(`{}`), "agent-update-logout")
		if logout.status != http.StatusNoContent || len(logout.body) != 0 {
			t.Fatal("original Update Session failed to logout")
		}
		agentAppProblem(t, w.request(t, owner, http.MethodPost, w.lookupPath(), body, original.key), http.StatusUnauthorized, f.SessionRevoked)
	}
	afterCounts, err := w.counts(ctx)
	agentAppRequire(t, err)
	if afterCounts != beforeCounts {
		t.Fatal("original Lookup/read/Session rejection rewrote Update history")
	}
}

func (w *agentEditWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.URL.RawQuery != "" || r.Method != http.MethodPatch && r.Method != http.MethodPost {
		return errors.New("unexpected Agent Edit mutation")
	}
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, (512<<10)+1))
	closeErr := r.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 512<<10 || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("original Update request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var envelope struct {
		Command  ac.CommandName  `json:"command,omitempty"`
		Target   i.AgentID       `json:"target_id,omitempty"`
		Expected f.Version       `json:"expected_version"`
		Request  json.RawMessage `json:"request"`
	}
	if err := agentWebDecode(raw, &envelope); err != nil {
		return err
	}
	request, err := ac.DecodeAgentUpdate(envelope.Request)
	if err != nil {
		return errors.New("original Update patch invalid")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	in := agentEditIntent{key: key, request: request, rawRequest: bytes.Clone(envelope.Request), bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	w.keys = append(w.keys, string(key))
	if r.Method == http.MethodPatch && r.URL.Path == w.path() {
		in.kind, in.before = "update", w.current.Fields().Agent.Clone()
		index := len(w.writes)
		if envelope.Command != "" || envelope.Target.Validate() == nil || envelope.Expected != in.before.Fields().Core.Version || index != len(w.mutations) {
			return errors.New("Update changed target/version or overlapped original command")
		}
		for _, previous := range w.writes {
			if previous.key == key {
				return errors.New("browser repeated an Update key")
			}
		}
		var want map[string]string
		if w.mode == "lost-update-confirmation-only-lookup" {
			if index != 0 {
				return errors.New("browser retransmitted an uncertain Update")
			}
			want = map[string]string{"name": w.draft.Name + "-edited", "description": ""}
		} else {
			switch index {
			case 0:
				want = map[string]string{"name": w.draft.Name + "-edited", "description": "Browser saved Agent description.", "instructions": "Browser saved Agent instructions.\nPreserve the complete text."}
			case 1:
				want = map[string]string{"name": w.draft.Name + "-edited"}
			case 2:
				want = map[string]string{"description": "", "instructions": ""}
			default:
				return errors.New("browser added an undeclared Update")
			}
		}
		var actual map[string]string
		if json.Unmarshal(envelope.Request, &actual) != nil || !reflect.DeepEqual(actual, want) {
			return errors.New("browser changed the original touched-field patch")
		}
		w.writes = append(w.writes, in)
	} else if r.Method == http.MethodPost && r.URL.Path == w.lookupPath() {
		in.kind = "lookup"
		if w.mode != "lost-update-confirmation-only-lookup" || len(w.writes) != 1 || len(w.mutations) != 1 || w.lookups != 0 || w.cut != 1 {
			return errors.New("Lookup has no original uncertain committed Update")
		}
		original := w.writes[0]
		var a, b map[string]json.RawMessage
		if json.Unmarshal(original.rawRequest, &a) != nil || json.Unmarshal(envelope.Request, &b) != nil || !reflect.DeepEqual(a, b) || envelope.Command != ac.UpdateAgentCommand || envelope.Target != w.target || envelope.Expected != original.before.Fields().Core.Version || key != original.key {
			return errors.New("Lookup lost original Update target/version/fullpresence/key")
		}
		in.before = original.before.Clone()
		w.lookups++
	} else {
		return errors.New("Agent Edit browser used an undeclared write route")
	}
	*r = *r.WithContext(context.WithValue(r.Context(), agentEditIntentKey{}, in))
	return nil
}

func (w *agentEditWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	in, write := r.Request.Context().Value(agentEditIntentKey{}).(agentEditIntent)
	if write {
		if r.StatusCode != 200 {
			return errors.New("original browser Update/Lookup was not successful")
		}
		switch in.kind {
		case "update":
			receipt, err := ac.DecodeAgentMutation(raw)
			if err != nil {
				return err
			}
			rid, err := f.ParseID[f.Request](r.Header.Get("X-Request-ID"))
			if err != nil {
				return err
			}
			if err = w.updateFacts(r.Request.Context(), in, rid, receipt); err != nil {
				return err
			}
			w.mu.Lock()
			w.current = receipt.Clone()
			w.mutations = append(w.mutations, receipt.Clone())
			if receipt.Fields().Changed {
				w.changed++
			}
			lost := w.mode == "lost-update-confirmation-only-lookup"
			w.mu.Unlock()
			if lost {
				return &reviewWebDrop{r.Header.Clone(), len(raw)}
			}
		case "lookup":
			lookup, err := ac.DecodeAgentCommandLookup(raw)
			w.mu.Lock()
			original := w.mutations[0].Clone()
			w.mu.Unlock()
			if err != nil || lookup.Status() != ac.AgentLookupCommitted || lookup.Receipt() == nil || !bytes.Equal(jsonAgentWeb(*lookup.Receipt()), jsonAgentWeb(original)) {
				return errors.New("browser Lookup did not recover the original Update receipt")
			}
			counts, err := w.counts(r.Request.Context())
			if err != nil {
				return err
			}
			if counts != ([5]int64{2, 2, 1, 2, 2}) {
				return errors.New("browser Lookup repeated Update effects")
			}
		default:
			return errors.New("unknown Update proof kind")
		}
	} else if r.Request.Method == http.MethodGet && r.StatusCode == 200 {
		if err := w.observeRead(r.Request.URL.Path, raw); err != nil {
			return err
		}
	}
	if r.StatusCode == 200 && (write || r.Request.Method == http.MethodGet) {
		record := map[string]any{"input_hash": w.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method,
			"url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw),
			"body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
		if write {
			record["kind"], record["request_body_sha256"], record["idempotency_key_sha256"], record["committed_verified"] = in.kind, fmt.Sprintf("%x", in.bodyDigest), fmt.Sprintf("%x", in.keyDigest), true
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(w.evidence, 0700); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(w.evidence, "agent-edit-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600); err != nil {
			return err
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func (w *agentEditWeb) observeRead(path string, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	current := w.current.Fields().Agent
	if path == w.path() {
		config, err := ac.DecodeAgentConfig(raw)
		var fields map[string]json.RawMessage
		if err != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 20 || !bytes.Equal(jsonAgentWeb(config), jsonAgentWeb(current)) {
			return errors.New("browser full configuration differs from original Update receipt")
		}
		w.configurations++
	} else if path == variableHTTPPath(w.a.p2.project.ID, "/agents") {
		var page f.Page[ac.DirectoryEntry]
		var shape struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		if json.Unmarshal(raw, &page) != nil || json.Unmarshal(raw, &shape) != nil || len(page.Items) != 1 || len(shape.Items) != 1 || len(shape.Items[0]) != 9 {
			return errors.New("Agent Edit Directory is not the original safe page")
		}
		v, core := page.Items[0], current.Fields().Core
		if v.Validate() != nil || v.ID != w.target || v.ProjectID != w.a.p2.project.ID || v.Version != core.Version || v.Name != core.Name || bytes.Contains(raw, []byte(w.a.modelID.String())) {
			return errors.New("Agent Edit Directory lost current safe metadata")
		}
		w.pages++
	}
	return nil
}

func (w *agentEditWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "agent-edit.config.js"))
	cmd.Dir, cmd.SysProcAttr, cmd.WaitDelay = root, &syscall.SysProcAttr{Setpgid: true}, time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	const prefix = "AGENTEAM_AGENT_EDIT_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Agent Edit Playwright did not start")
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
	t.Logf("Agent Edit Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	safe := output.String()
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	failure := w.failure
	w.mu.Unlock()
	tokens = append(tokens, w.a.p2.base.ownerBrowser.cookie, w.a.p2.base.ownerBrowser.csrf)
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if failure != nil {
		t.Error("owned Agent Edit observation failed", failure)
	}
	if err != nil {
		t.Fatal("original Agent Edit browser call failed and was waited")
	}
}

func (w *agentEditWeb) verifyBrowser(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	failure, updates, receipts, lookups, cuts, changed, configs, pages := w.failure, len(w.writes), len(w.mutations), w.lookups, w.cut, w.changed, w.configurations, w.pages
	for _, tail := range w.tails {
		if !tail.returned {
			failure = errors.New("original Agent Edit handler remains live")
		}
	}
	current := w.current.Clone()
	w.mu.Unlock()
	if failure != nil {
		t.Fatal("Agent Edit original observation failed", failure)
	}
	wantUpdates, wantChanged, wantLookup := 3, 2, 0
	if w.mode == "lost-update-confirmation-only-lookup" {
		wantUpdates, wantChanged, wantLookup = 1, 1, 1
	}
	if updates != wantUpdates || receipts != updates || changed != wantChanged || lookups != wantLookup || cuts != wantLookup || configs < 1 || pages < 1 {
		t.Fatal("Agent Edit omitted or repeated an original command/read")
	}
	wantVersion := int64(1 + wantChanged)
	counts, err := w.counts(ctx)
	agentAppRequire(t, err)
	if counts != ([5]int64{wantVersion, int64(1 + wantUpdates), int64(wantUpdates), wantVersion, wantVersion}) || int64(current.Fields().Agent.Fields().Core.Version) != wantVersion {
		t.Fatal("Agent Edit durable facts differ after reads/Lookup", counts)
	}
	core := current.Fields().Agent.Fields().Core
	if core.Name != w.draft.Name+"-edited" || core.Description != "" || (wantLookup == 0 && core.Instructions != "") || (wantLookup == 1 && core.Instructions != w.draft.Instructions) {
		t.Fatal("Agent Edit final full/omitted/cleared text differs")
	}
}
