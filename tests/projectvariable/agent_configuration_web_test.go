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
	"sync"
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

// Project publication and the Model are setup facts. Every browser API request
// reaches the public runtime-off App after the setup graph and guard retire.
func TestAgentConfigurationWeb(t *testing.T) {
	for _, mode := range []string{"default-create-config-and-directory", "lost-create-confirmation-only-lookup"} {
		t.Run(mode, func(t *testing.T) {
			entered, completed := false, false
			t.Cleanup(func() {
				if !entered || !completed {
					t.Error("browser public App handoff did not enter and complete")
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
					t.Fatal("browser setup providers have not joined")
				}
				if _, err := a.guard.CurrentProcess(); err == nil {
					t.Fatal("browser setup still holds its ProcessGuard")
				}
				var stopped bool
				agentAppRequire(t, original.raw.QueryRow(ctxFor(t), `SELECT state='stopped' AND stopped_at IS NOT NULL FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&stopped))
				if !stopped {
					t.Fatal("browser setup process has not retired")
				}
				w := newAgentConfigurationWeb(t, a, mode, username)
				defer w.dispose(t)
				withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
					defer w.close(t)
					w.start(t, address)
					if agentAppRegistration(t, a) != registration {
						t.Fatal("App changed the original fixed builtin identity")
					}
					w.browser(t, ctx)
					w.close(t)
					w.verify(t, ctx)
				})
				completed = true
			}
		})
	}
}

type agentWebIntent struct {
	kind                  string
	key                   f.IdempotencyKey
	request               ac.AgentCreate
	rawRequest            []byte
	bodyDigest, keyDigest [sha256.Size]byte
}
type agentWebIntentKey struct{}
type agentWebDraft struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
}
type agentConfigurationWeb struct {
	a                                                            *agentCreateFixture
	mode, origin, directory, evidence, inputHash, dist           string
	draft                                                        agentWebDraft
	listener                                                     net.Listener
	front                                                        *http.Server
	backend                                                      *httptest.Server
	upstream, transport                                          *http.Transport
	serve                                                        chan error
	frontWG, backendWG                                           sync.WaitGroup
	mu                                                           sync.Mutex
	tails                                                        map[string]*reviewWebTail
	failure                                                      error
	keys                                                         []string
	intent                                                       agentWebIntent
	receipt                                                      ac.AgentMutation
	creates, lookups, cut, configurations, directoryReads, pages int
	closed                                                       bool
}

func newAgentConfigurationWeb(t *testing.T, a *agentCreateFixture, mode, username string) *agentConfigurationWeb {
	t.Helper()
	const prefix = "AGENTEAM_AGENT_CONFIGURATION_WEB"
	dist, runtime := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence, inputHash := os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("frozen dist, short owned runtime, evidence and input hash required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("frozen production index missing")
	}
	directory, err := os.MkdirTemp(runtime, "acw-")
	agentAppRequire(t, err)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(directory)
		}
	}()
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = os.RemoveAll(directory)
		t.Fatal(err)
	}
	w := &agentConfigurationWeb{a: a, mode: mode, origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}, draft: agentWebDraft{Name: "browser-agent", Description: "Created by the real Owner browser.", Instructions: "Use the explicitly configured Project capabilities."}}
	material := map[string]any{"mode": mode, "cookie": a.p2.base.ownerBrowser.cookie, "project_id": a.p2.project.ID, "model_id": a.modelID, "route": "/" + username + "/" + a.p2.project.NormalizedName + "/settings/agents", "draft": w.draft}
	raw, err := json.Marshal(material)
	if err == nil {
		err = os.WriteFile(filepath.Join(directory, "agent-configuration-material.json"), raw, 0600)
	}
	clear(raw)
	if err != nil {
		_ = listener.Close()
		_ = os.RemoveAll(directory)
		t.Fatal(err)
	}
	published = true
	return w
}

func (w *agentConfigurationWeb) fail(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failure == nil {
		w.failure = err
	}
}

func (w *agentConfigurationWeb) start(t *testing.T, address string) {
	w.startObserved(t, address, w.observe, w.response)
}

func (w *agentConfigurationWeb) startObserved(t *testing.T, address string, observe func(*http.Request) error, response func(*http.Response) error) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("owned TLS verification disabled")
	}
	target, err := url.Parse(w.backend.URL)
	agentAppRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = w.transport
	proxy.ErrorLog = log.New(io.Discard, "", 0)
	proxy.ModifyResponse = response
	proxy.ErrorHandler = w.proxyError
	static := http.FileServer(http.Dir(w.dist))
	w.front = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.frontWG.Add(1)
		defer w.frontWG.Done()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if err := observe(r); err != nil {
				w.fail(err)
				http.Error(out, "owned observation failed", 500)
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

// The bridge uses the same real App listener as the HTTP fixture. It observes
// original upstream EOF/Close, then the front verifies the owned TLS response.
func (w *agentConfigurationWeb) bridge(address string, out http.ResponseWriter, r *http.Request) {
	w.backendWG.Add(1)
	defer w.backendWG.Done()
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
	validLength := response.ContentLength == int64(len(raw))
	if r.Method == http.MethodHead {
		validLength = len(raw) == 0 && response.ContentLength >= 0
	}
	if readErr != nil || closeErr != nil || len(raw) > 2<<20 || !validLength {
		w.fail(errors.New("original App response EOF/Close/length incomplete"))
		http.Error(out, "upstream incomplete", 502)
		return
	}
	id := response.Header.Get("X-Request-ID")
	parsed, err := f.ParseID[f.Request](id)
	if err != nil || parsed.String() != id || len(response.Header.Values("X-Request-ID")) != 1 {
		w.fail(errors.New("original App response identity invalid"))
		http.Error(out, "upstream identity missing", 502)
		return
	}
	tail := &reviewWebTail{done: make(chan struct{})}
	w.mu.Lock()
	previous := w.tails[id]
	if previous == nil {
		w.tails[id] = tail
	}
	w.mu.Unlock()
	if previous != nil {
		w.fail(errors.New("original App request identity repeated"))
		http.Error(out, "repeated identity", 502)
		return
	}
	defer func() { w.mu.Lock(); tail.returned = true; w.mu.Unlock(); close(tail.done) }()
	for key, values := range response.Header {
		out.Header()[key] = append([]string(nil), values...)
	}
	out.WriteHeader(response.StatusCode)
	if r.Method != http.MethodHead {
		if _, err = out.Write(raw); err != nil {
			w.fail(errors.New("owned TLS downstream write failed"))
		}
	}
}

func agentWebDecode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("browser Agent envelope invalid")
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return errors.New("browser Agent envelope trailing bytes")
	}
	return nil
}

func (w *agentConfigurationWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPost {
		return errors.New("unexpected Agent browser mutation method")
	}
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	w.mu.Lock()
	w.keys = append(w.keys, string(key))
	w.mu.Unlock()
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, (512<<10)+1))
	closeErr := r.Body.Close()
	if readErr != nil || closeErr != nil || len(raw) > 512<<10 || key.Validate() != nil {
		return errors.New("original Agent request incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	prefix := variableHTTPPath(w.a.p2.project.ID, "/agents")
	var envelope struct {
		Request json.RawMessage `json:"request"`
		Command ac.CommandName  `json:"command,omitempty"`
	}
	if err := agentWebDecode(raw, &envelope); err != nil {
		return err
	}
	request, err := ac.DecodeAgentCreate(envelope.Request)
	if err != nil {
		return errors.New("original Agent create did not decode")
	}
	intent := agentWebIntent{key: key, request: request, rawRequest: bytes.Clone(envelope.Request), bodyDigest: sha256.Sum256(raw), keyDigest: sha256.Sum256([]byte(key))}
	switch r.URL.Path {
	case prefix:
		var fields map[string]json.RawMessage
		if json.Unmarshal(envelope.Request, &fields) != nil || len(fields) != 10 || envelope.Command != "" {
			return errors.New("browser create changed presence-sensitive defaults")
		}
		for _, name := range []string{"agent_id", "name", "description", "instructions", "model_ref", "inject_agents_md", "approval_policy", "allowed_tool_ids", "allowed_mount_ids", "allowed_secret_variable_ids"} {
			if fields[name] == nil {
				return errors.New("browser create omitted explicit draft field")
			}
		}
		v := request.Fields()
		if v.Name != w.draft.Name || v.Description != w.draft.Description || v.Instructions != w.draft.Instructions || v.InjectAgentsMD || v.ModelRef != w.a.modelID || v.ApprovalPolicy != ac.ApprovalDefault || v.ApprovalModelRef != nil || v.AddSkillsEnabled == nil || !*v.AddSkillsEnabled || v.InstallSkillEnabled == nil || !*v.InstallSkillEnabled || len(v.AllowedToolIDs) != 0 || len(v.AllowedMountIDs) != 0 || len(v.AllowedSecretVariableIDs) != 0 {
			return errors.New("browser Agent request changed fixed draft or defaults")
		}
		var count int
		if err = w.a.p2.base.raw.QueryRow(r.Context(), `SELECT count(*) FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2`, w.a.p2.project.ID.String(), v.AgentID.String()).Scan(&count); err != nil || count != 0 {
			return errors.New("browser Agent existed before original Create")
		}
		intent.kind = "create"
		w.mu.Lock()
		w.creates++
		n := w.creates
		if n == 1 {
			w.intent = intent
		}
		w.mu.Unlock()
		if n != 1 {
			return errors.New("browser retransmitted Agent Create")
		}
	case variableHTTPPath(w.a.p2.project.ID, "/agent-commands/lookup"):
		intent.kind = "lookup"
		w.mu.Lock()
		var original, current map[string]json.RawMessage
		decodeSame := json.Unmarshal(w.intent.rawRequest, &original) == nil && json.Unmarshal(envelope.Request, &current) == nil
		valid := w.mode == "lost-create-confirmation-only-lookup" && w.creates == 1 && w.lookups == 0 && w.cut == 1 && w.receipt.Validate() == nil && key == w.intent.key && envelope.Command == ac.CreateAgentCommand && decodeSame && reflect.DeepEqual(original, current)
		if valid {
			w.lookups++
		}
		w.mu.Unlock()
		if !valid {
			return errors.New("browser Lookup lost original full-presence create identity")
		}
	default:
		return errors.New("unexpected Agent browser POST")
	}
	*r = *r.WithContext(context.WithValue(r.Context(), agentWebIntentKey{}, intent))
	return nil
}

func (w *agentConfigurationWeb) response(r *http.Response) error {
	raw, err := io.ReadAll(io.LimitReader(r.Body, (2<<20)+1))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || len(raw) > 2<<20 || r.ContentLength != int64(len(raw)) || r.TLS == nil || len(r.TLS.VerifiedChains) == 0 {
		return errors.New("original verified TLS response EOF/Close/length incomplete")
	}
	id := r.Header.Get("X-Request-ID")
	parsed, err := f.ParseID[f.Request](id)
	if err != nil || parsed.String() != id {
		return errors.New("original TLS request identity invalid")
	}
	w.mu.Lock()
	tail := w.tails[id]
	w.mu.Unlock()
	if tail == nil {
		return errors.New("original TLS handler absent")
	}
	select {
	case <-tail.done:
	case <-r.Request.Context().Done():
		return r.Request.Context().Err()
	}
	w.mu.Lock()
	returned := tail.returned
	w.mu.Unlock()
	if !returned {
		return errors.New("original TLS handler did not return")
	}
	intent, write := r.Request.Context().Value(agentWebIntentKey{}).(agentWebIntent)
	if write {
		if r.StatusCode != http.StatusOK {
			return errors.New("original Agent POST did not return success")
		}
		switch intent.kind {
		case "create":
			receipt, e := ac.DecodeAgentMutation(raw)
			if e != nil {
				return errors.New("original Agent receipt did not decode")
			}
			if e = w.committedFacts(r.Request.Context(), intent, parsed, receipt); e != nil {
				return e
			}
			w.mu.Lock()
			w.receipt = receipt.Clone()
			lost := w.mode == "lost-create-confirmation-only-lookup"
			w.mu.Unlock()
			if lost {
				return &reviewWebDrop{r.Header.Clone(), len(raw)}
			}
		case "lookup":
			lookup, e := ac.DecodeAgentCommandLookup(raw)
			w.mu.Lock()
			original := w.receipt.Clone()
			w.mu.Unlock()
			if e != nil || lookup.Status() != ac.AgentLookupCommitted || lookup.Receipt() == nil || !bytes.Equal(jsonAgentWeb(*lookup.Receipt()), jsonAgentWeb(original)) {
				return errors.New("original Lookup did not recover exact receipt")
			}
		default:
			return errors.New("unexpected Agent POST proof kind")
		}
	} else if r.Request.Method == http.MethodGet && r.StatusCode == http.StatusOK {
		if err = w.observeRead(r.Request.URL.Path, raw); err != nil {
			return err
		}
	}
	if r.StatusCode == http.StatusOK && (write || r.Request.Method == http.MethodGet) {
		if err = w.proof(r, raw, intent, write); err != nil {
			return err
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func jsonAgentWeb(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return raw
}

func (w *agentConfigurationWeb) observeRead(path string, raw []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	prefix := variableHTTPPath(w.a.p2.project.ID, "/agents")
	if path == prefix {
		var page f.Page[ac.DirectoryEntry]
		var wire struct {
			Items []map[string]json.RawMessage `json:"items"`
		}
		if json.Unmarshal(raw, &page) != nil || json.Unmarshal(raw, &wire) != nil || len(wire.Items) != len(page.Items) {
			return errors.New("Agent Directory page did not decode")
		}
		for n, entry := range page.Items {
			if entry.Validate() != nil || len(wire.Items[n]) != 9 || bytes.Contains(raw, []byte(w.draft.Instructions)) || bytes.Contains(raw, []byte(w.a.modelID.String())) {
				return errors.New("Agent Directory page exposed more than safe metadata")
			}
		}
		if w.receipt.Validate() != nil {
			if len(page.Items) != 0 {
				return errors.New("browser setup unexpectedly included an Agent")
			}
			return nil
		}
		if len(page.Items) != 1 || page.Items[0].ID != w.receipt.Fields().Agent.Fields().Core.ID {
			return errors.New("Directory did not publish original Agent")
		}
		w.pages++
		return nil
	}
	if w.receipt.Validate() != nil {
		return nil
	}
	core := w.receipt.Fields().Agent.Fields().Core
	if path == prefix+"/"+core.ID.String()+"/configuration" {
		config, err := ac.DecodeAgentConfig(raw)
		var fields map[string]json.RawMessage
		if err != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 20 || !bytes.Equal(jsonAgentWeb(config), jsonAgentWeb(w.receipt.Fields().Agent)) {
			return errors.New("browser configuration differs from complete original receipt")
		}
		w.configurations++
	} else if path == prefix+"/"+core.ID.String() {
		var entry ac.DirectoryEntry
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &entry) != nil || entry.Validate() != nil || json.Unmarshal(raw, &fields) != nil || len(fields) != 9 || entry.ID != core.ID || entry.Name != core.Name || bytes.Contains(raw, []byte(w.draft.Instructions)) || bytes.Contains(raw, []byte(w.a.modelID.String())) {
			return errors.New("browser Directory leaked or changed configuration")
		}
		w.directoryReads++
	}
	return nil
}

func (w *agentConfigurationWeb) proof(r *http.Response, raw []byte, intent agentWebIntent, write bool) error {
	record := map[string]any{"input_hash": w.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "original_eof": true, "original_closed": true, "handler_returned": true}
	if write {
		record["kind"] = intent.kind
		record["request_body_sha256"] = fmt.Sprintf("%x", intent.bodyDigest)
		record["idempotency_key_sha256"] = fmt.Sprintf("%x", intent.keyDigest)
		record["committed_verified"] = true
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(w.evidence, "agent-configuration-"+w.mode+"-response-"+r.Header.Get("X-Request-ID")+".json"), encoded, 0600)
}

func (w *agentConfigurationWeb) proxyError(out http.ResponseWriter, r *http.Request, err error) {
	var drop *reviewWebDrop
	if !errors.As(err, &drop) {
		w.fail(err)
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
	if n != 1 || writeErr != nil || flushErr != nil || hijackErr != nil || closeErr != nil {
		w.fail(errors.New("original Agent receipt cut did not complete"))
		return
	}
	w.mu.Lock()
	w.cut++
	w.mu.Unlock()
}

func (w *agentConfigurationWeb) close(t *testing.T) {
	t.Helper()
	if w.closed {
		return
	}
	w.closed = true
	if w.front != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err := w.front.Shutdown(ctx)
		cancel()
		if err != nil {
			_ = w.front.Close()
			t.Error("browser front required forced close", err)
		}
		if err = <-w.serve; !errors.Is(err, http.ErrServerClosed) {
			t.Error("original browser Serve did not return", err)
		}
		w.frontWG.Wait()
	} else {
		_ = w.listener.Close()
	}
	if w.transport != nil {
		w.transport.CloseIdleConnections()
	}
	if w.backend != nil {
		w.backend.Close()
		w.backendWG.Wait()
	}
	if w.upstream != nil {
		w.upstream.CloseIdleConnections()
	}
}
func (w *agentConfigurationWeb) dispose(t *testing.T) {
	t.Helper()
	w.close(t)
	agentAppRequire(t, os.RemoveAll(w.directory))
	if _, err := os.Lstat(w.directory); !errors.Is(err, os.ErrNotExist) {
		t.Error("browser private material still exists")
	}
}

func (w *agentConfigurationWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "agent-configuration.config.js"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && key != "PLAYWRIGHT_NO_COPY_PROMPT" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	const prefix = "AGENTEAM_AGENT_CONFIGURATION_WEB"
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", prefix+"_DIST="+w.dist, prefix+"_CASE="+w.mode, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err = cmd.Start(); err != nil {
		t.Fatal("owned Agent Playwright did not start")
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
	t.Logf("Agent Configuration Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
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
		t.Fatal("original Agent browser call failed")
	}
}

func (w *agentConfigurationWeb) committedFacts(ctx context.Context, intent agentWebIntent, requestID f.ID[f.Request], receipt ac.AgentMutation) error {
	a := w.a
	request := intent.request.Fields()
	mutation := receipt.Fields()
	config := mutation.Agent.Fields()
	core := config.Core
	if receipt.Validate() != nil || !mutation.Changed || len(mutation.EventIDs) != 1 || core.ID != request.AgentID || core.ProjectID != a.p2.project.ID || core.Version != 1 || core.Lifecycle != ac.AgentActive || core.Name != request.Name || core.Description != request.Description || core.Instructions != request.Instructions || core.InjectAgentsMD || core.ModelRef != a.modelID || core.ApprovalPolicy != ac.ApprovalDefault || core.ApprovalModelRef != nil || core.ReasoningEffort != nil || core.DisplayName != nil || core.TagColor != nil || !core.CreatedAt.Time().Equal(core.UpdatedAt.Time()) || !slices.Equal(config.AllowedToolIDs, []i.ToolID{a.tool.ToolID}) || len(config.AllowedMountIDs) != 0 || len(config.AllowedSecretVariableIDs) != 0 {
		return errors.New("browser Create receipt lost fixed draft/default install binding")
	}
	meta := f.CommandMeta{RequestID: requestID, IdempotencyKey: intent.key}
	digest, err := ac.AgentCommandDigest(a.p2.base.ownerBrowser.actor, meta, a.p2.project.ID, core.ID, ac.CreateAgentCommand, intent.request)
	if err != nil {
		return err
	}
	command, err := ac.AgentCommandIdentity(a.p2.project.ID, ac.CreateAgentCommand, intent.key)
	if err != nil {
		return err
	}
	facts, err := agentCreateFacts(ctx, a.p2.base.raw, a.p2.project.ID, core.ID, intent.key)
	if err != nil {
		return err
	}
	if facts != ([16]int64{1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}) {
		return fmt.Errorf("browser Agent atomic fact counts %v", facts)
	}
	var stored []byte
	var commandID string
	var revision int64
	var exact bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT id::text,plan_revision,receipt,
 after_config=receipt->'agent' AND before_config IS NULL AND expected_version IS NULL AND add_skills_enabled AND install_skill_enabled AND committed_at IS NOT NULL
 FROM agenteam_agent.commands WHERE project_id::text=$1 AND target_id::text=$2 AND command_name='agent.create' AND idempotency_key=$3 AND semantic_digest=$4 AND actor_user_id::text=$5 AND state='completed'`, a.p2.project.ID.String(), core.ID.String(), string(intent.key), digest.String(), a.p2.base.ownerBrowser.actor.Details().UserID).Scan(&commandID, &revision, &stored, &exact)
	persisted, decodeErr := ac.DecodeAgentMutation(stored)
	if err != nil || decodeErr != nil || !exact || revision < 1 || commandID != mutation.EventIDs[0].String() || !bytes.Equal(jsonAgentWeb(persisted), jsonAgentWeb(receipt)) {
		return errors.New("browser Agent command differs from original receipt/semantic identity")
	}
	var canonical bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT name=$3 AND normalized_name=$4 AND description=$5 AND instructions=$6 AND NOT inject_agents_md
 AND model_id::text=$7 AND approval_policy='default' AND approval_model_id IS NULL AND reasoning_effort IS NULL AND display_name IS NULL AND tag_color IS NULL
 AND lifecycle='active' AND version=1 AND created_at=$8 AND updated_at=$8
 FROM agenteam_agent.agents WHERE project_id::text=$1 AND id::text=$2`, a.p2.project.ID.String(), core.ID.String(), core.Name, core.NormalizedName, core.Description, core.Instructions, a.modelID.String(), core.CreatedAt.Time()).Scan(&canonical)
	if err != nil || !canonical {
		return errors.New("browser Agent receipt differs from canonical configuration")
	}
	var refs bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT
 (SELECT count(*)=1 AND bool_and(project_id::text=$1 AND model_id::text=$3 AND owner_version=1 AND reasoning_effort='' AND role='agent_model') FROM agenteam_model.references WHERE owner_kind='agent' AND owner_id::text=$2)
 AND EXISTS(SELECT 1 FROM agenteam_agent.tool_allowlist WHERE project_id::text=$1 AND agent_id::text=$2 AND tool_id::text=$4)
 AND EXISTS(SELECT 1 FROM agenteam_tool.agent_configurations h JOIN agenteam_tool.agent_references r ON r.agent_id=h.agent_id WHERE h.project_id::text=$1 AND h.agent_id::text=$2 AND h.config_version=1 AND r.tool_id::text=$4)
 AND EXISTS(SELECT 1 FROM agenteam_mount.agent_mount_heads WHERE project_id::text=$1 AND agent_id::text=$2 AND owner_version=1)`, a.p2.project.ID.String(), core.ID.String(), a.modelID.String(), a.tool.ToolID.String()).Scan(&refs)
	if err != nil || !refs {
		return errors.New("browser Agent actual provider references differ")
	}
	var skillCommand, binding string
	var skillRevision int64
	var published bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT h.creation_command,h.plan_revision,h.request_binding,
 h.add_skills_enabled AND h.assignment_sequence=1 AND h.observed_revision=s.current_revision
 AND a.id=h.initial_assignment_id AND a.skill_id=h.initial_skill_id AND a.enabled AND a.assignment_sequence=1 AND a.removed_at IS NULL AND a.created_at=h.created_at
 AND s.protected AND s.serving AND s.name='Add Skills' AND s.normalized_name='add-skills'
 AND r.revision=h.observed_revision AND z.phase='published' AND z.bundle_id='builtin.add-skills.v1' AND z.object_id=r.object_id
 FROM agenteam_skill.agent_assignment_heads h
 JOIN agenteam_skill.agent_assignments a ON a.project_id=h.project_id AND a.agent_id=h.agent_id AND a.id=h.initial_assignment_id
 JOIN agenteam_skill.skills s ON s.project_id=h.project_id AND s.id=h.initial_skill_id
 JOIN agenteam_skill.revisions r ON r.project_id=s.project_id AND r.skill_id=s.id AND r.id=s.revision_id
 JOIN agenteam_skill.initializations z ON z.project_id=s.project_id AND z.skill_id=s.id AND z.revision_id=r.id AND z.creation_id=s.creation_id
 WHERE h.project_id::text=$1 AND h.agent_id::text=$2`, a.p2.project.ID.String(), core.ID.String()).Scan(&skillCommand, &skillRevision, &binding, &published)
	if err != nil || skillCommand != command.Canonical() || skillRevision != revision || f.Digest(binding).Validate() != nil || !published {
		return errors.New("browser default Skill did not bind original publication/command")
	}
	var auditRaw, headerRaw, payloadRaw []byte
	var auditUser, session, cause string
	err = a.p2.base.raw.QueryRow(ctx, `SELECT metadata,user_id::text,session_id::text,cause_ref FROM agenteam_audit.audit_records WHERE project_id::text=$1 AND resource_id::text=$2 AND producer='agent' AND action='agent.create' AND scope='project' AND actor_kind='human' AND outcome='success' AND resource_kind='agent' AND ordinal=0`, a.p2.project.ID.String(), core.ID.String()).Scan(&auditRaw, &auditUser, &session, &cause)
	if err != nil || auditUser != a.p2.base.ownerBrowser.actor.Details().UserID || session != a.p2.base.ownerBrowser.actor.Details().SessionID {
		return errors.New("browser Agent audit differs from original Owner Session")
	}
	metadata, err := auditc.DecodeMetadata(auditc.AgentCreate, auditRaw)
	if err != nil {
		return err
	}
	auditFields, err := metadata.AgentFields()
	causeDigest, causeErr := cursor.Digest([]byte(command.Canonical()))
	if err != nil || causeErr != nil || cause != causeDigest.String() || auditFields.AgentID != core.ID.String() || auditFields.CommandID != commandID || auditFields.Version != 1 || !slices.Equal(auditFields.ChangedFields, ac.EditableFields()) {
		return errors.New("browser Agent audit differs from original command/version")
	}
	var eventColumns bool
	err = a.p2.base.raw.QueryRow(ctx, `SELECT header,payload,id::text=$3 AND event_type='agent.config_changed' AND schema_version=1 AND scope='project' AND aggregate_type='agent.config' AND aggregate_version=1 AND aggregate_sequence IS NULL AND occurred_at=$4 FROM agenteam_outbox.events WHERE project_id::text=$1 AND aggregate_id::text=$2 AND producer='agent'`, a.p2.project.ID.String(), core.ID.String(), commandID, core.UpdatedAt.Time()).Scan(&headerRaw, &payloadRaw, &eventColumns)
	if err != nil || !eventColumns {
		return errors.New("browser Agent Outbox identity differs")
	}
	header, err := eventc.DecodeHeader(headerRaw)
	var payload ac.ConfigChangedPayload
	if err != nil || ac.ValidateAgentEventHeader(header) != nil || json.Unmarshal(payloadRaw, &payload) != nil || payload.Validate() != nil || header.EventID != mutation.EventIDs[0] || header.AggregateID.String() != core.ID.String() || header.Scope.ProjectID.String() != core.ProjectID.String() || *header.AggregateVersion != 1 || !header.OccurredAt.Time().Equal(core.UpdatedAt.Time()) || payload.CommandID.String() != commandID || payload.ActorUserID.String() != auditUser || payload.Operation != ac.ConfigCreated || !slices.Equal(payload.ChangedFields, auditFields.ChangedFields) {
		return errors.New("browser Agent event differs from original receipt")
	}
	return ctx.Err()
}

func (w *agentConfigurationWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	failure := w.failure
	creates, lookups, cut, configs, pages := w.creates, w.lookups, w.cut, w.configurations, w.pages
	intent, receipt := w.intent, w.receipt.Clone()
	for _, tail := range w.tails {
		if !tail.returned {
			failure = errors.New("original Agent TLS handler not returned")
		}
	}
	w.mu.Unlock()
	if failure != nil {
		t.Fatal("owned Agent browser observation failed", failure)
	}
	wantLookup := 0
	if w.mode == "lost-create-confirmation-only-lookup" {
		wantLookup = 1
	}
	if creates != 1 || lookups != wantLookup || cut != wantLookup || configs < 1 || pages < 1 || receipt.Validate() != nil {
		t.Fatal("browser Agent flow omitted original create/config/directory or duplicated an intent")
	}
	facts, err := agentCreateFacts(ctx, w.a.p2.base.raw, w.a.p2.project.ID, receipt.Fields().Agent.Fields().Core.ID, intent.key)
	agentAppRequire(t, err)
	if facts != ([16]int64{1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}) {
		t.Fatal("browser reads/Lookup duplicated Agent facts", facts)
	}
}
