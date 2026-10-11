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
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// The original published Project/Model graph hands off to the default App.
// Secret values stay in the formal Go seed; only eight-field metadata reaches
// Node. No Agent, canonical allowlist or provider reference is SQL-seeded.
func TestAgentSecretAllowlistWeb(t *testing.T) {
	for _, mode := range []string{"secret-allowlist-normal", "secret-allowlist-lost"} {
		t.Run(mode, func(t *testing.T) {
			entered, completed := false, false
			t.Cleanup(func() {
				if !entered || !completed {
					t.Error("Secret allowlist browser App handoff did not complete")
				}
			})
			a := newAgentCreateFixture(t)
			original := a.p2.base
			process, err := a.guard.CurrentProcess()
			agentAppRequire(t, err)
			registration := agentAppRegistration(t, a)
			var username string
			var ordinary bool
			agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT username,role='user' FROM agenteam_account.users WHERE id::text=$1`, original.ownerBrowser.actor.Details().UserID).Scan(&username, &ordinary))
			if !ordinary {
				t.Fatal("Secret allowlist browser Owner is not an ordinary Account")
			}
			secrets := agentSecretWebSeed(t, a)
			if original.afterOwners != nil {
				t.Fatal("Secret allowlist setup already has an App handoff")
			}
			original.afterOwners = func() {
				entered = true
				if !original.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
					t.Fatal("Secret allowlist setup providers have not retired")
				}
				if _, err := a.guard.CurrentProcess(); err == nil {
					t.Fatal("Secret allowlist setup guard remains held")
				}
				var stopped bool
				agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
				if !stopped {
					t.Fatal("Secret allowlist setup process has not retired")
				}
				w := newAgentSecretWeb(t, a, mode, secrets)
				defer w.dispose(t)
				withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
					defer w.close(t)
					w.start(t, address)
					if agentAppRegistration(t, a) != registration {
						t.Fatal("Secret allowlist App changed the builtin registration")
					}
					if mode == "secret-allowlist-lost" {
						w.seedAgent(t, ctx)
					}
					w.material(t, username)
					w.browser(t, ctx)
					w.close(t)
					w.verify(t, ctx)
				})
				completed = true
			}
		})
	}
}

func agentSecretWebSeed(t *testing.T, a *agentCreateFixture) []vc.SecretVariable {
	t.Helper()
	v := assembleSecretOwnerFixture(t, a.p2.base)
	safe := make([]vc.SecretVariable, 0, 2)
	for n := 0; n < 2; n++ {
		// Request material never crosses into Node, logs or persistent fixtures.
		value := []byte(fmt.Sprintf("owned-agent-secret-allowlist-value-%d", n+1))
		request := secretCreateInput(t, fmt.Sprintf("AGENT_BROWSER_SECRET_%d", n+1), value)
		clear(value)
		receipt, err := v.owner.CreateSecretVariable(ctxFor(t), a.p2.base.ownerBrowser.actor, meta(t, fmt.Sprintf("agent-browser-secret-seed-%d", n+1), nil), a.p2.project.ID, request)
		request.Destroy()
		if err != nil || receipt.Validate() != nil {
			t.Fatal("formal Secret metadata seed failed")
		}
		safe = append(safe, receipt.Fields().Variable.Clone())
	}
	v.owner.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := v.owner.Drain(ctx); err != nil {
		t.Fatal("original Secret seed owner Drain did not return")
	}
	v.secrets.StopMaintenance()
	return safe
}

type agentSecretIntent struct {
	kind                  string
	key                   f.IdempotencyKey
	rawRequest            []byte
	request               any
	before                ac.AgentConfig
	secrets               []i.ProjectVariableID
	bodyDigest, keyDigest [sha256.Size]byte
}
type agentSecretIntentKey struct{}
type agentSecretIngressKey struct{}
type agentSecretHost struct {
	method, uri string
	matches     bool
}
type agentSecretWeb struct {
	*agentEditWeb
	secrets              []vc.SecretVariable
	commands             []agentSecretIntent
	receipts             []ac.AgentMutation
	hosts                map[string]agentSecretHost
	proofs               map[string]map[string]any
	reads                map[f.Version]int
	secretPages, updates int
	lookupReturned       bool
}

func newAgentSecretWeb(t *testing.T, a *agentCreateFixture, mode string, secrets []vc.SecretVariable) *agentSecretWeb {
	t.Helper()
	const prefix = "AGENTEAM_AGENT_SECRET_ALLOWLIST_WEB"
	dist, runtime := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence, inputHash := os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("Secret allowlist requires frozen dist/private runtime/evidence/input hash")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Secret allowlist production index missing")
	}
	directory, err := os.MkdirTemp(runtime, "asaw-")
	agentAppRequire(t, err)
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.RemoveAll(directory)
		t.Fatal("Secret allowlist listener unavailable")
	}
	evidence = filepath.Join(evidence, "TestAgentSecretAllowlistWeb", mode)
	if err = os.MkdirAll(filepath.Dir(evidence), 0700); err == nil {
		err = os.Mkdir(evidence, 0700)
	}
	if err != nil {
		_ = listener.Close()
		_ = os.RemoveAll(directory)
		t.Fatal("Secret allowlist fresh evidence unavailable")
	}
	base := &agentConfigurationWeb{a: a, mode: mode, origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}, draft: agentWebDraft{Name: "secret-allowlist-agent", Description: "Configured by the ordinary Project Owner.", Instructions: "Use only explicitly allowed project Secrets."}}
	return &agentSecretWeb{agentEditWeb: &agentEditWeb{agentConfigurationWeb: base}, secrets: secrets, hosts: map[string]agentSecretHost{}, proofs: map[string]map[string]any{}, reads: map[f.Version]int{}}
}

func (w *agentSecretWeb) allowed(stage int) []i.ProjectVariableID {
	if stage == 3 {
		return []i.ProjectVariableID{}
	}
	return []i.ProjectVariableID{w.secrets[stage-1].Fields().ID}
}
func (w *agentSecretWeb) collection() string { return variableHTTPPath(w.a.p2.project.ID, "/agents") }

func (w *agentSecretWeb) start(t *testing.T, address string) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("Secret allowlist owned TLS verification disabled")
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
			*r = *r.WithContext(context.WithValue(r.Context(), agentSecretIngressKey{}, true))
			if err := w.observe(r); err != nil {
				w.fail(err)
				http.Error(out, "owned Secret allowlist observation failed", 500)
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
func (w *agentSecretWeb) bridge(address string, out http.ResponseWriter, r *http.Request) {
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
		w.hosts[rid] = agentSecretHost{r.Method, r.URL.RequestURI(), up.Host == w.listener.Addr().String()}
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

func (w *agentSecretWeb) createRequest(target i.AgentID) []byte {
	return jsonAgentWeb(map[string]any{"agent_id": target, "name": w.draft.Name, "description": w.draft.Description, "instructions": w.draft.Instructions, "model_ref": w.a.modelID, "inject_agents_md": false, "approval_policy": "default", "allowed_tool_ids": []string{}, "runner_mounts": []ac.RunnerMountDeclaration{}, "allowed_secret_variable_ids": w.allowed(1)})
}
func (w *agentSecretWeb) seedAgent(t *testing.T, ctx context.Context) {
	t.Helper()
	w.target = id[i.Agent](t)
	raw := w.createRequest(w.target)
	request, err := ac.DecodeAgentCreate(raw)
	agentAppRequire(t, err)
	const key f.IdempotencyKey = "secret-allowlist-browser-original-create"
	in := agentSecretIntent{kind: "create", key: key, rawRequest: raw, request: request, secrets: w.allowed(1)}
	response := w.request(t, w.a.p2.base.ownerBrowser, http.MethodPost, w.collection(), jsonAgentWeb(map[string]any{"request": json.RawMessage(raw)}), key)
	if response.status != 200 {
		t.Fatal("default App seed Create failed")
	}
	receipt, err := ac.DecodeAgentMutation(response.body)
	agentAppRequire(t, err)
	rid, err := f.ParseID[f.Request](response.header.Get("X-Request-ID"))
	agentAppRequire(t, err)
	agentAppRequire(t, w.commandFacts(ctx, in, rid, receipt))
	w.initial, w.current = receipt.Fields().Agent.Clone(), receipt.Clone()
}
func (w *agentSecretWeb) material(t *testing.T, username string) {
	t.Helper()
	var target any
	var initial any
	if w.mode == "secret-allowlist-lost" {
		target = w.target
		initial = w.initial
	}
	raw, err := json.Marshal(map[string]any{"mode": w.mode, "origin": w.origin, "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.a.p2.project.ID, "model_id": w.a.modelID, "route": "/" + username + "/" + w.a.p2.project.NormalizedName + "/settings/agents", "draft": w.draft, "secrets": w.secrets, "agent_id": target, "initial": initial, "input_hash": w.inputHash})
	agentAppRequire(t, err)
	defer clear(raw)
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "agent-secret-allowlist-material.json"), raw, 0600))
}

func (w *agentSecretWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet {
		return nil
	}
	if r.URL.RawQuery != "" || (r.Method != http.MethodPost && r.Method != http.MethodPatch) {
		return errors.New("undeclared Secret allowlist browser method")
	}
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, (512<<10)+1))
	closeErr := r.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 512<<10 || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("Secret allowlist original request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var envelope struct {
		Command  ac.CommandName  `json:"command,omitempty"`
		Target   i.AgentID       `json:"target_id,omitempty"`
		Expected *f.Version      `json:"expected_version,omitempty"`
		Request  json.RawMessage `json:"request"`
	}
	if agentWebDecode(raw, &envelope) != nil {
		return errors.New("Secret allowlist original envelope invalid")
	}
	in := agentSecretIntent{key: key, rawRequest: bytes.Clone(envelope.Request), bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.keys = append(w.keys, string(key))
	if r.Method == http.MethodPost && r.URL.Path == w.collection() {
		if w.mode != "secret-allowlist-normal" || w.creates != 0 || len(w.commands) != 0 || envelope.Command != "" || envelope.Expected != nil || envelope.Target.Validate() == nil {
			return errors.New("unexpected browser Secret allowlist Create")
		}
		request, err := ac.DecodeAgentCreate(envelope.Request)
		if err != nil {
			return errors.New("Secret allowlist Create decode failed")
		}
		target := request.Fields().AgentID
		if !runnerMountSameJSON(envelope.Request, w.createRequest(target)) {
			return errors.New("Create changed the declared draft or Secret allowlist profile")
		}
		var count int
		if err = w.a.p2.base.raw.QueryRow(r.Context(), `SELECT count(*) FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2`, w.a.p2.project.ID.String(), target.String()).Scan(&count); err != nil || count != 0 {
			return errors.New("browser Agent already existed before Create")
		}
		w.target = target
		w.creates++
		in.kind, in.request, in.secrets = "create", request, w.allowed(1)
		w.commands = append(w.commands, in)
	} else if r.Method == http.MethodPatch && r.URL.Path == w.path() {
		if len(w.commands) != len(w.receipts) || w.current.Validate() != nil || envelope.Command != "" || envelope.Target.Validate() == nil || envelope.Expected == nil || *envelope.Expected != w.current.Fields().Agent.Fields().Core.Version {
			return errors.New("Update lost the current original version")
		}
		before := w.current.Fields().Agent
		seen := w.reads[before.Fields().Core.Version]
		if seen < 1 {
			return errors.New("Update preceded current Config read")
		}
		stage := 2
		if w.mode == "secret-allowlist-normal" && w.updates == 1 {
			stage = 3
		}
		if (w.mode == "secret-allowlist-normal" && w.updates >= 2) || (w.mode == "secret-allowlist-lost" && w.updates != 0) {
			return errors.New("browser repeated a Secret allowlist Update")
		}
		for _, old := range w.commands {
			if old.key == key {
				return errors.New("browser reused an original write key")
			}
		}
		want := w.allowed(stage)
		if !runnerMountSameJSON(envelope.Request, jsonAgentWeb(map[string]any{"allowed_secret_variable_ids": want})) {
			return errors.New("Update changed untouched fields or Secret IDs")
		}
		request, err := ac.DecodeAgentUpdate(envelope.Request)
		if err != nil {
			return errors.New("Secret allowlist Update decode failed")
		}
		in.kind, in.request, in.before, in.secrets = "update", request, before.Clone(), want
		w.updates++
		w.commands = append(w.commands, in)
	} else if r.Method == http.MethodPost && r.URL.Path == w.lookupPath() {
		if w.mode != "secret-allowlist-lost" || w.updates != 1 || len(w.commands) != 1 || len(w.receipts) != 1 || w.lookups != 0 || w.cut != 1 {
			return errors.New("Lookup lacks the original cut Update")
		}
		original := w.commands[0]
		if key != original.key || envelope.Command != ac.UpdateAgentCommand || envelope.Target != w.target || envelope.Expected == nil || *envelope.Expected != original.before.Fields().Core.Version || !runnerMountSameJSON(envelope.Request, original.rawRequest) {
			return errors.New("Lookup changed original key/body/target/expected version")
		}
		in.kind, in.request, in.before, in.secrets = "lookup", original.request, original.before.Clone(), slices.Clone(original.secrets)
		w.lookups++
	} else {
		return errors.New("undeclared Secret allowlist mutation route")
	}
	*r = *r.WithContext(context.WithValue(r.Context(), agentSecretIntentKey{}, in))
	return nil
}

func (w *agentSecretWeb) commandFacts(ctx context.Context, in agentSecretIntent, rid f.ID[f.Request], receipt ac.AgentMutation) error {
	fields := receipt.Fields()
	config := fields.Agent.Fields()
	core := config.Core
	if receipt.Validate() != nil || !fields.Changed || len(fields.EventIDs) != 1 || (in.kind == "create" && (fields.RunnerMounts == nil || len(*fields.RunnerMounts) != 0) || in.kind != "create" && fields.RunnerMounts != nil) || !slices.Equal(config.AllowedSecretVariableIDs, in.secrets) || core.ID != w.target || core.ProjectID != w.a.p2.project.ID {
		return errors.New("original Secret allowlist mutation identity/mapping invalid")
	}
	wantVersion := f.Version(1)
	if in.kind == "update" {
		wantVersion = in.before.Fields().Core.Version + 1
		request, ok := in.request.(ac.AgentUpdate)
		if !ok {
			return errors.New("original Update type lost")
		}
		proposed, err := request.Proposed(in.before)
		if err != nil {
			return errors.New("original Secret patch does not bind the frozen configuration")
		}
		expected := proposed.Fields()
		expected.Core.Version = core.Version
		expected.Core.UpdatedAt = core.UpdatedAt
		if !reflect.DeepEqual(expected, config) {
			return errors.New("Update changed an untouched configuration field")
		}
	}
	if core.Version != wantVersion || core.Name != w.draft.Name || core.Description != w.draft.Description || core.Instructions != w.draft.Instructions || core.ModelRef != w.a.modelID || core.InjectAgentsMD || core.ApprovalPolicy != ac.ApprovalDefault || core.ApprovalModelRef != nil || core.ReasoningEffort != nil || core.DisplayName != nil || core.TagColor != nil || core.Lifecycle != ac.AgentActive || !slices.Equal(config.AllowedToolIDs, []i.ToolID{w.a.tool.ToolID}) || len(config.AllowedMountIDs) != 0 {
		return errors.New("Secret allowlist command changed fixed default configuration")
	}
	if in.kind == "create" && !core.CreatedAt.Time().Equal(core.UpdatedAt.Time()) {
		return errors.New("Create timestamps differ")
	}
	if in.kind == "create" {
		facts, err := agentCreateFacts(ctx, w.a.p2.base.raw, w.a.p2.project.ID, w.target, in.key)
		n := int64(len(config.AllowedSecretVariableIDs))
		if err != nil || facts != ([16]int64{1, 1, 0, n, 1, 1, 1, 1, 0, n, 1, 1, 1, 1, 1, 0}) {
			return errors.New("original Create omitted atomic provider/default Skill facts")
		}
	}
	if err := w.secretRelations(ctx, receipt, in.key); err != nil {
		return err
	}
	var canonical, providerRefs bool
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT name=$3 AND normalized_name=$4 AND description=$5 AND instructions=$6 AND NOT inject_agents_md
 AND model_id::text=$7 AND approval_policy='default' AND approval_model_id IS NULL AND reasoning_effort IS NULL AND display_name IS NULL AND tag_color IS NULL
 AND lifecycle='active' AND version=$8 AND created_at=$9 AND updated_at=$10
 FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2`, w.a.p2.project.ID.String(), w.target.String(), core.Name, core.NormalizedName, core.Description, core.Instructions, w.a.modelID.String(), int64(core.Version), core.CreatedAt.Time(), core.UpdatedAt.Time()).Scan(&canonical)
	if err != nil || !canonical {
		return errors.New("canonical Agent differs from the original receipt")
	}
	err = w.a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*)=1 AND bool_and(project_id::text=$1 AND model_id::text=$3 AND owner_version=$5 AND reasoning_effort='' AND role='agent_model') FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id::text=$2)
 AND (SELECT count(*)=1 AND bool_and(tool_id::text=$4) FROM agenteam_agent.tool_allowlist WHERE project_id::text=$1 AND agent_id::text=$2)
 AND EXISTS(SELECT 1 FROM agenteam_tool.agent_configurations h JOIN agenteam_tool.agent_references r ON r.agent_id=h.agent_id WHERE h.project_id::text=$1 AND h.agent_id::text=$2 AND h.config_version=$5 AND r.tool_id::text=$4)
 AND NOT EXISTS(SELECT 1 FROM agenteam_agent.mount_allowlist WHERE project_id::text=$1 AND agent_id::text=$2)`, w.a.p2.project.ID.String(), w.target.String(), w.a.modelID.String(), w.a.tool.ToolID.String(), int64(core.Version)).Scan(&providerRefs)
	if err != nil || !providerRefs {
		return errors.New("original configuration provider references differ")
	}
	name := ac.CreateAgentCommand
	meta := f.CommandMeta{RequestID: rid, IdempotencyKey: in.key}
	if in.kind == "update" {
		name = ac.UpdateAgentCommand
		version := in.before.Fields().Core.Version
		meta.ExpectedVersion = &version
	}
	return w.commandRecord(ctx, name, meta, in.request, receipt)
}

func (w *agentSecretWeb) commandRecord(ctx context.Context, name ac.CommandName, meta f.CommandMeta, request any, receipt ac.AgentMutation) error {
	base, project, target := w.a.p2.base, w.a.p2.project.ID, w.target
	var commandID, user, semantic string
	var input, stored []byte
	var revision int64
	var expected *int64
	var exactPostimage bool
	err := base.raw.QueryRow(ctx, `SELECT id::text,actor_user_id::text,semantic_digest,input,receipt,plan_revision,expected_version,
 after_config=receipt->'agent' AND ((command_name='agent.create' AND runner_mounts='[]'::jsonb AND receipt->'runner_mounts'='[]'::jsonb) OR (command_name='agent.update' AND runner_mounts IS NULL AND NOT (receipt ? 'runner_mounts')))
 FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND command_name=$3 AND idempotency_key=$4 AND state='completed'`, project.String(), target.String(), string(name), string(meta.IdempotencyKey)).Scan(&commandID, &user, &semantic, &input, &stored, &revision, &expected, &exactPostimage)
	if err != nil || revision < 1 || !exactPostimage || user != base.ownerBrowser.actor.Details().UserID || !runnerMountSameJSON(input, jsonAgentWeb(request)) || !runnerMountSameJSON(stored, jsonAgentWeb(receipt)) || (expected == nil) != (meta.ExpectedVersion == nil) || expected != nil && *expected != int64(*meta.ExpectedVersion) {
		return errors.New("original immutable Agent command/receipt relation")
	}
	if _, err := f.ParseID[ac.AgentCommand](commandID); err != nil {
		return errors.New("invalid original command UUID")
	}
	digest, err := ac.AgentCommandDigest(base.ownerBrowser.actor, meta, project, target, name, request)
	if err != nil || semantic != digest.String() {
		return errors.New("original command semantic digest does not bind Secret IDs and expected version")
	}
	identity, err := ac.AgentCommandIdentity(project, name, meta.IdempotencyKey)
	if err != nil {
		return err
	}
	causeDigest, err := cursor.Digest([]byte(identity.Canonical()))
	if err != nil {
		return err
	}
	var audits, events int64
	if err = base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND producer='agent' AND resource_id::text=$2 AND cause_ref=$3),
 (SELECT count(*) FROM agenteam_outbox.events WHERE project_id::text=$1 AND producer='agent' AND aggregate_id::text=$2 AND id::text=$4)`, project.String(), target.String(), causeDigest.String(), commandID).Scan(&audits, &events); err != nil {
		return err
	}
	fields := receipt.Fields()
	if !fields.Changed {
		if audits != 0 || events != 0 || len(fields.EventIDs) != 0 {
			return errors.New("no-op emitted Audit or Outbox facts")
		}
		return nil
	}
	if audits != 1 || events != 1 || len(fields.EventIDs) != 1 || fields.EventIDs[0].String() != commandID {
		return errors.New("changed Agent command lacks its exact Audit/Outbox pair")
	}
	var auditRaw, headerRaw, payloadRaw []byte
	var session string
	if err = base.raw.QueryRow(ctx, `SELECT metadata,session_id::text FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND resource_id::text=$2 AND producer='agent' AND action=$3 AND cause_ref=$4 AND ordinal=0 AND actor_kind='human' AND user_id::text=$5 AND outcome='success' AND resource_kind='agent'`, project.String(), target.String(), string(name), causeDigest.String(), user).Scan(&auditRaw, &session); err != nil || session != base.ownerBrowser.actor.Details().SessionID {
		return errors.New("Audit original current Human Session/cause")
	}
	metadata, err := auditc.DecodeMetadata(auditc.Action(name), auditRaw)
	if err != nil {
		return errors.New("typed Agent Audit decode")
	}
	auditFields, err := metadata.AgentFields()
	wantFields, operation := []string{"allowed_secret_variable_ids"}, ac.ConfigUpdated
	if name == ac.CreateAgentCommand {
		wantFields, operation = ac.EditableFields(), ac.ConfigCreated
	}
	if err != nil || auditFields.CommandID != commandID || auditFields.AgentID != target.String() || auditFields.Version != fields.Agent.Fields().Core.Version || !slices.Equal(auditFields.ChangedFields, wantFields) {
		return errors.New("typed Agent Audit does not describe the original configuration change")
	}
	if err = base.raw.QueryRow(ctx, `SELECT header,payload FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent' AND id::text=$3 AND event_type='agent.config_changed' AND schema_version=1`, project.String(), target.String(), commandID).Scan(&headerRaw, &payloadRaw); err != nil {
		return err
	}
	header, err := eventc.DecodeHeader(headerRaw)
	var payload ac.ConfigChangedPayload
	if err != nil || ac.ValidateAgentEventHeader(header) != nil || json.Unmarshal(payloadRaw, &payload) != nil || payload.Validate() != nil || header.EventID.String() != commandID || header.Scope.ProjectID.String() != project.String() || header.AggregateID.String() != target.String() || *header.AggregateVersion != fields.Agent.Fields().Core.Version || !header.OccurredAt.Time().Equal(fields.Agent.Fields().Core.UpdatedAt.Time()) || payload.CommandID.String() != commandID || payload.ActorUserID.String() != user || payload.Operation != operation || !slices.Equal(payload.ChangedFields, wantFields) {
		return errors.New("typed original Agent Event does not match Audit/receipt/current change")
	}

	return nil
}

func (w *agentSecretWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	if r.StatusCode != http.StatusOK || r.Request.Method != http.MethodGet && r.Request.Method != http.MethodPost && r.Request.Method != http.MethodPatch {
		return errors.New("unexpected original browser response status/method")
	}
	if r.Header.Get("Cache-Control") != "no-store" {
		return errors.New("original App omitted no-store")
	}
	rid := r.Header.Get("X-Request-ID")
	w.mu.Lock()
	host, exists := w.hosts[rid]
	w.mu.Unlock()
	ingress, _ := r.Request.Context().Value(agentSecretIngressKey{}).(bool)
	if !ingress || !exists || !host.matches || host.method != r.Request.Method || host.uri != r.Request.URL.RequestURI() {
		return errors.New("browser ingress/downstream Host proof is not same request")
	}
	in, write := r.Request.Context().Value(agentSecretIntentKey{}).(agentSecretIntent)
	lost := false
	if write {
		if in.kind == "lookup" {
			lookup, err := ac.DecodeAgentCommandLookup(raw)
			w.mu.Lock()
			original := w.receipts[0].Clone()
			w.mu.Unlock()
			if err != nil || lookup.Status() != ac.AgentLookupCommitted || lookup.Receipt() == nil || !runnerMountSameJSON(jsonAgentWeb(*lookup.Receipt()), jsonAgentWeb(original)) {
				return errors.New("Lookup differs from original completed receipt")
			}
			counts, err := w.counts(r.Request.Context())
			if err != nil || counts != ([5]int64{2, 2, 1, 2, 2}) {
				return errors.New("Lookup repeated original command effects")
			}
			if err = w.secretRelations(r.Request.Context(), original, in.key); err != nil {
				return err
			}
			w.mu.Lock()
			w.lookupReturned = true
			w.mu.Unlock()
		} else {
			receipt, err := ac.DecodeAgentMutation(raw)
			if err != nil {
				return errors.New("original Secret allowlist receipt invalid")
			}
			requestID, err := f.ParseID[f.Request](rid)
			if err != nil {
				return err
			}
			if err = w.commandFacts(r.Request.Context(), in, requestID, receipt); err != nil {
				return err
			}
			w.mu.Lock()
			w.current = receipt.Clone()
			w.receipts = append(w.receipts, receipt.Clone())
			lost = w.mode == "secret-allowlist-lost" && in.kind == "update"
			w.mu.Unlock()
		}
	} else if r.Request.Method == http.MethodGet {
		if err = w.observeRead(r.Request.URL.Path, raw); err != nil {
			return err
		}
	} else {
		return errors.New("original browser write lacks its observation")
	}
	record := map[string]any{"input_hash": w.inputHash, "request_id": rid, "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true, "handler_return_source": "owned_tls_bridge", "browser_ingress_host_matches": true, "downstream_host_matches": true}
	if write {
		record["kind"], record["request_body_sha256"], record["idempotency_key_sha256"], record["committed_verified"] = in.kind, fmt.Sprintf("%x", in.bodyDigest), fmt.Sprintf("%x", in.keyDigest), true
	}
	w.mu.Lock()
	if _, already := w.proofs[rid]; already {
		w.mu.Unlock()
		return errors.New("response proof RID repeated")
	}
	w.proofs[rid] = record
	err = w.writeProofLocked(rid)
	w.mu.Unlock()
	if err != nil {
		return err
	}
	if lost {
		return &reviewWebDrop{r.Header.Clone(), len(raw)}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}
func (w *agentSecretWeb) writeProofLocked(rid string) error {
	raw, err := json.Marshal(w.proofs[rid])
	if err != nil {
		return err
	}
	path := filepath.Join(w.evidence, "agent-secret-allowlist-"+w.mode+"-response-"+rid+".json")
	if err = os.WriteFile(path+".tmp", raw, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}
func (w *agentSecretWeb) proxyError(out http.ResponseWriter, r *http.Request, err error) {
	var drop *reviewWebDrop
	if !errors.As(err, &drop) {
		w.fail(errors.New("owned Secret allowlist proxy response rejected"))
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
	if record == nil || record["kind"] != "update" || w.cut != 0 {
		w.mu.Unlock()
		w.fail(errors.New("physical cut lacks original Update proof"))
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

// Each receipt is checked against both canonical and provider-owned facts.
// xmin ties the nonempty sets to the completed command's final transaction;
// clearing requires both sets empty and the Agent/command same final transaction.
func (w *agentSecretWeb) secretRelations(ctx context.Context, receipt ac.AgentMutation, key f.IdempotencyKey) error {
	var canonical, provider []string
	var sameTx bool
	core := receipt.Fields().Agent.Fields().Core
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT
 COALESCE((SELECT array_agg(variable_id::text ORDER BY variable_id) FROM agenteam_agent.secret_allowlist WHERE project_id::text=$1 AND agent_id::text=$2),'{}'::text[]),
 COALESCE((SELECT array_agg(variable_id::text ORDER BY variable_id) FROM agenteam_projectvariable.secret_references WHERE owner_kind='agent' AND owner_id::text=$2),'{}'::text[]),
 a.xmin=c.xmin AND a.version=$4
 AND NOT EXISTS(SELECT 1 FROM agenteam_agent.secret_allowlist s WHERE s.agent_id=a.id AND (s.project_id<>a.project_id OR s.xmin<>c.xmin))
 AND NOT EXISTS(SELECT 1 FROM agenteam_projectvariable.secret_references s WHERE s.owner_kind='agent' AND s.owner_id=a.id AND (s.project_id<>a.project_id OR s.owner_version<>a.version OR s.xmin<>c.xmin))
 FROM agenteam_agent.agents a JOIN agenteam_agent.commands c ON c.project_id=a.project_id AND c.target_id=a.id
 WHERE a.project_id::text=$1 AND a.id::text=$2 AND c.idempotency_key=$3 AND c.state='completed'`, w.a.p2.project.ID.String(), w.target.String(), string(key), int64(core.Version)).Scan(&canonical, &provider, &sameTx)
	if err != nil || !sameTx {
		return errors.New("Agent and Secret provider references did not share the completed final transaction")
	}
	want := make([]string, len(receipt.Fields().Agent.Fields().AllowedSecretVariableIDs))
	for n, id := range receipt.Fields().Agent.Fields().AllowedSecretVariableIDs {
		want[n] = id.String()
	}
	if !slices.Equal(canonical, want) || !slices.Equal(provider, want) {
		return errors.New("canonical and provider Secret reference sets differ from the original receipt")
	}
	return nil
}

func (w *agentSecretWeb) observeRead(path string, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if path == variableHTTPPath(w.a.p2.project.ID, "/secret-variables") {
		var page f.Page[vc.SecretVariable]
		var shape struct {
			Items []json.RawMessage `json:"items"`
		}
		if json.Unmarshal(raw, &page) != nil || json.Unmarshal(raw, &shape) != nil || !runnerMountSameJSON(jsonAgentWeb(page.Items), jsonAgentWeb(w.secrets)) || page.NextCursor != "" || len(shape.Items) != 2 {
			return errors.New("safe Secret candidates differ from formal metadata seed")
		}
		for _, item := range shape.Items {
			if !runnerWebFields(item, "id", "project_id", "type", "name", "description", "version", "created_at", "updated_at") {
				return errors.New("Secret candidates exposed a forbidden field")
			}
		}
		w.secretPages++
		return nil
	}
	if path == w.collection() {
		var page f.Page[ac.DirectoryEntry]
		var shape struct {
			Items []json.RawMessage `json:"items"`
		}
		if json.Unmarshal(raw, &page) != nil || json.Unmarshal(raw, &shape) != nil || page.NextCursor != "" || len(page.Items) != len(shape.Items) {
			return errors.New("Agent Directory incomplete")
		}
		if w.current.Validate() != nil {
			if len(page.Items) != 0 {
				return errors.New("Agent pre-existed browser Create")
			}
			return nil
		}
		core := w.current.Fields().Agent.Fields().Core
		if len(page.Items) != 1 || page.Items[0].Validate() != nil || page.Items[0].ID != w.target || page.Items[0].ProjectID != w.a.p2.project.ID || page.Items[0].Version != core.Version || page.Items[0].Name != core.Name || !runnerWebFields(shape.Items[0], "id", "project_id", "name", "display_name", "tag_color", "description", "version", "created_at", "updated_at") {
			return errors.New("Agent Directory lost safe current metadata")
		}
		w.pages++
		return nil
	}
	if w.target.Validate() != nil || path != w.path() {
		return nil
	}
	if w.current.Validate() != nil {
		return errors.New("current read preceded a real Agent command")
	}
	if w.mode == "secret-allowlist-lost" && w.updates == 1 && !w.lookupReturned {
		return errors.New("current read preceded original uncertain Lookup")
	}
	current := w.current.Fields().Agent
	_, err := ac.DecodeAgentConfig(raw)
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 20 || !runnerMountSameJSON(raw, jsonAgentWeb(current)) {
		return errors.New("current Config differs from original canonical state")
	}
	w.reads[current.Fields().Core.Version]++
	w.configurations++
	return nil
}

func (w *agentSecretWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "agent-secret-allowlist.config.js"))
	cmd.Dir, cmd.SysProcAttr, cmd.WaitDelay = root, &syscall.SysProcAttr{Setpgid: true}, time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	const prefix = "AGENTEAM_AGENT_SECRET_ALLOWLIST_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Agent Secret Allowlist Playwright did not start")
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
	t.Logf("Agent Secret Allowlist Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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
		t.Error("owned Agent Secret Allowlist observation failed", failure)
	}
	if err != nil {
		t.Fatal("original Agent Secret Allowlist browser call failed and was waited")
	}
}

func (w *agentSecretWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	failure := w.failure
	for _, tail := range w.tails {
		if !tail.returned {
			failure = errors.New("original owned TLS handler remains live")
		}
	}
	creates, updates, lookups, cuts := w.creates, w.updates, w.lookups, w.cut
	current := w.current.Clone()
	configs, secretPages, proofCount := w.configurations, w.secretPages, len(w.proofs)
	reads := make(map[f.Version]int, len(w.reads))
	for k, v := range w.reads {
		reads[k] = v
	}
	receipts, commands := len(w.receipts), len(w.commands)
	w.mu.Unlock()
	if failure != nil {
		t.Fatal("original Secret allowlist observation failed", failure)
	}
	wantCreates, wantUpdates, wantLookups, wantVersion := 1, 2, 0, f.Version(3)
	if w.mode == "secret-allowlist-lost" {
		wantCreates, wantUpdates, wantLookups, wantVersion = 0, 1, 1, 2
	}
	if creates != wantCreates || updates != wantUpdates || lookups != wantLookups || cuts != wantLookups || commands != wantCreates+wantUpdates || receipts != commands || current.Validate() != nil || current.Fields().Agent.Fields().Core.Version != wantVersion || secretPages < 1 {
		t.Fatal("Secret allowlist browser omitted or repeated an original operation")
	}
	for version := f.Version(1); version <= wantVersion; version++ {
		count := reads[version]
		minimum := 1
		if version == wantVersion {
			minimum = 2
		}
		if count < minimum {
			t.Fatal("current Config or explicit final read missing")
		}
	}
	counts, err := w.counts(ctx)
	agentAppRequire(t, err)
	v := int64(wantVersion)
	if counts != ([5]int64{v, v, v - 1, v, v}) {
		t.Fatal("reads/Lookup changed command, Audit or Event counts")
	}
	want := w.allowed(3)
	if wantLookups == 1 {
		want = w.allowed(2)
	}
	if !slices.Equal(current.Fields().Agent.Fields().AllowedSecretVariableIDs, want) {
		t.Fatal("final current Secret allowlist differs")
	}
	w.mu.Lock()
	lastKey := w.commands[len(w.commands)-1].key
	w.mu.Unlock()
	agentAppRequire(t, w.secretRelations(ctx, current, lastKey))
	var secrets, executions int
	agentAppRequire(t, w.a.p2.base.raw.QueryRow(ctx, `SELECT (SELECT count(*) FROM agenteam_projectvariable.variables WHERE project_id::text=$1 AND type='secret'),(SELECT count(*) FROM agenteam_execution.executions)`, w.a.p2.project.ID.String()).Scan(&secrets, &executions))
	if secrets != 2 || executions != 0 {
		t.Fatal("browser changed Secret setup or launched Execution")
	}
	path := filepath.Join(w.directory, "agent-secret-allowlist-result.json")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16<<10 {
		t.Fatal("Secret allowlist private browser result missing or oversized")
	}
	raw, err := os.ReadFile(path)
	agentAppRequire(t, err)
	var result struct {
		Completed        bool   `json:"completed"`
		InputHash        string `json:"input_hash"`
		OriginalRequests int    `json:"original_requests"`
		AllObserved      bool   `json:"all_observed"`
		Creates          int    `json:"creates"`
		Updates          int    `json:"updates"`
		Lookups          int    `json:"lookups"`
		Configurations   int    `json:"configurations"`
		SecretPages      int    `json:"secret_pages"`
	}
	if agentWebDecode(raw, &result) != nil || !result.Completed || result.InputHash != w.inputHash || !result.AllObserved || result.OriginalRequests != proofCount || result.Creates != creates || result.Updates != updates || result.Lookups != lookups || result.Configurations != configs || result.SecretPages != secretPages {
		t.Fatal("original browser request/consumer result does not match Go observations")
	}
	t.Logf("Agent Secret Allowlist original facts creates=%d updates=%d lookups=%d configurations=%d secret_pages=%d proofs=%d; all owned handler tails returned", creates, updates, lookups, configs, secretPages, proofCount)
}
