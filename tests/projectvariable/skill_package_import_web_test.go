//go:build integration && linux

package projectvariable_test

import (
	"archive/zip"
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
	"strings"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// A real disk file is the input, never an editor draft or generated frontmatter
// in the browser. Leading/trailing spaces and Unicode distinguish byte-preserving
// import from trim/normalization. This is the supported one-file text package.
const skillImportFile = "---\nname: Imported guide\ndescription: An existing single-file package\n---\n# Existing instructions\n\n  保留原始空格与 UTF-8。  \nUse the original file.\n"

type skillImportIntent struct {
	Request struct {
		SkillID sc.SkillID `json:"skill_id"`
		Mode    string     `json:"mode"`
		Source  struct {
			Kind  string `json:"kind"`
			Files []struct {
				Path string `json:"path"`
				Text string `json:"utf8_text"`
			} `json:"files"`
		} `json:"source"`
	} `json:"request"`
}

type skillImportWeb struct {
	*agentEditWeb
	project                     pc.ProjectRef
	username, process           string
	intent                      skillImportIntent
	key                         f.IdempotencyKey
	body, receipt, packageBytes []byte
	posts, packages, responses  int
}

func TestSkillPackageImportWeb(t *testing.T) {
	t.Run("existing-file-import-and-fixed-revision-download", func(t *testing.T) {
		entered, completed := false, false
		t.Cleanup(func() {
			if !entered || !completed {
				t.Error("Skill import original App handoff incomplete")
			}
		})
		a := newAgentCreateFixture(t)
		old := a.p2.base
		process, err := a.guard.CurrentProcess()
		agentAppRequire(t, err)
		var username string
		agentAppRequire(t, old.raw.QueryRow(ctxFor(t), `SELECT username FROM agenteam_account.users WHERE id=$1`, old.ownerBrowser.actor.Details().UserID).Scan(&username))
		if old.afterOwners != nil {
			t.Fatal("Skill import handoff already occupied")
		}
		old.afterOwners = func() {
			entered = true
			if !old.core.Joined() || !a.agents.Joined() || !a.p2.service.Joined() {
				t.Fatal("original setup owners have not returned")
			}
			if _, err := a.guard.CurrentProcess(); err == nil {
				t.Fatal("original setup guard remains held")
			}
			w := newSkillImportWeb(t, a, username)
			defer w.dispose(t)
			withAgentConfigurationApp(t, a, process.String(), w.origin, func(ctx context.Context, address string) {
				defer w.close(t)
				agentAppRequire(t, old.raw.QueryRow(ctx, `SELECT process_id::text FROM agenteam_object.process_claims WHERE process_id<>$1 AND state='claimed'`, process.String()).Scan(&w.process))
				w.start(t, address)
				// Public default creation supplies this project's actual protected
				// Skill initialization. The old setup Project is not the UI target.
				input := pc.CreateProjectRequest{ProjectID: id[i.Project](t), Name: "skill-import", Description: "Existing package import"}
				response := w.request(t, old.ownerBrowser, http.MethodPost, "/api/v1/projects", jsonBytes(t, input), "skill-import-project")
				w.project = projectCreationAppReady(t, response, http.StatusCreated, input, old.ownerBrowser.actor)
				projectCreationAppPublished(t, a, ctx, input.ProjectID, "skill-import-project", w.process)
				w.material(t)
				w.browser(t, ctx)
				// Browser actual Wait and all front handlers precede assertions.
				w.closeFront(t)
				w.verify(t, ctx)
			})
			projectCreationAppReturned(t, a, ctxFor(t), w.project.ID, w.process, 0, 0)
			completed = true
			t.Log("SKILL_IMPORT existing_disk_file=true default_project=true original_bytes=true lookup=true fixed_revision_package=true original_node_app_returned=true")
		}
	})
}

func newSkillImportWeb(t *testing.T, a *agentCreateFixture, username string) *skillImportWeb {
	t.Helper()
	const prefix = "AGENTEAM_SKILL_IMPORT_WEB"
	dist, runtime := os.Getenv(prefix+"_DIST"), os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	evidence, inputHash := os.Getenv(prefix+"_EVIDENCE"), os.Getenv(prefix+"_INPUT_HASH")
	if !filepath.IsAbs(dist) || !filepath.IsAbs(runtime) || len(runtime) > 45 || !filepath.IsAbs(evidence) || len(inputHash) != 64 || strings.Trim(inputHash, "0123456789abcdef") != "" {
		t.Fatal("owned Skill import assets/runtime/input required")
	}
	if info, err := os.Stat(filepath.Join(dist, "index.html")); err != nil || !info.Mode().IsRegular() {
		t.Fatal("Skill import assets missing")
	}
	directory, err := os.MkdirTemp(runtime, "ski-")
	agentAppRequire(t, err)
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(directory)
		}
	}()
	agentAppRequire(t, os.Chmod(directory, 0700))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	agentAppRequire(t, err)
	defer func() {
		if !published {
			_ = listener.Close()
		}
	}()
	base := &agentConfigurationWeb{a: a, mode: "existing-file-import-and-fixed-revision-download", origin: "http://" + listener.Addr().String(), directory: directory, evidence: evidence, inputHash: inputHash, dist: dist, listener: listener, serve: make(chan error, 1), tails: map[string]*reviewWebTail{}}
	w := &skillImportWeb{agentEditWeb: &agentEditWeb{agentConfigurationWeb: base}, username: username}
	agentAppRequire(t, os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte(skillImportFile), 0600))
	published = true
	return w
}

func (w *skillImportWeb) material(t *testing.T) {
	t.Helper()
	raw := jsonBytes(t, map[string]any{"mode": w.mode, "cookie": w.a.p2.base.ownerBrowser.cookie, "owner_id": w.project.OwnerUserID.String(), "project_id": w.project.ID.String(), "route": "/" + w.username + "/" + w.project.NormalizedName + "/settings/skills", "input_hash": w.inputHash, "filename": "SKILL.md", "file_size": len(skillImportFile), "file_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(skillImportFile)))})
	agentAppRequire(t, os.WriteFile(filepath.Join(w.directory, "skill-import-material.json"), raw, 0600))
	clear(raw)
}

func (w *skillImportWeb) start(t *testing.T, address string) {
	t.Helper()
	w.upstream = &http.Transport{Proxy: nil, DisableKeepAlives: true}
	w.backend = httptest.NewUnstartedServer(http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) { w.bridge(address, out, r) }))
	w.backend.Config.ErrorLog = log.New(io.Discard, "", 0)
	w.backend.StartTLS()
	w.transport = w.backend.Client().Transport.(*http.Transport).Clone()
	if w.transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("Skill import TLS verification disabled")
	}
	target, err := url.Parse(w.backend.URL)
	agentAppRequire(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport, proxy.ErrorLog = w.transport, log.New(io.Discard, "", 0)
	proxy.ModifyResponse = w.response
	proxy.ErrorHandler = func(out http.ResponseWriter, r *http.Request, err error) {
		w.fail(err)
		http.Error(out, "owned import response failed", http.StatusBadGateway)
	}
	static := http.FileServer(http.Dir(w.dist))
	w.front = &http.Server{ReadHeaderTimeout: 2 * time.Second, ErrorLog: log.New(io.Discard, "", 0), Handler: http.HandlerFunc(func(out http.ResponseWriter, r *http.Request) {
		w.frontWG.Add(1)
		defer w.frontWG.Done()
		if r.Host != w.listener.Addr().String() {
			w.fail(errors.New("Skill import Host mismatch"))
			http.Error(out, "invalid host", 400)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if err := w.observe(r); err != nil {
				w.fail(err)
				http.Error(out, "invalid import request", 500)
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

func (w *skillImportWeb) observe(r *http.Request) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return nil
	}
	if r.Method != http.MethodPost || r.URL.Path != variableHTTPPath(w.project.ID, "/skills") || r.URL.RawQuery != "" || r.URL.ForceQuery {
		return errors.New("unexpected Skill import write")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	closeErr := r.Body.Close()
	if err != nil || closeErr != nil || len(raw) > 1<<20 {
		return errors.New("Skill import input incomplete")
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	var intent skillImportIntent
	key := f.IdempotencyKey(r.Header.Get("Idempotency-Key"))
	if agentWebDecode(raw, &intent) != nil || intent.Request.SkillID.Validate() != nil || intent.Request.Mode != "create" || intent.Request.Source.Kind != "text_files" || len(intent.Request.Source.Files) != 1 || intent.Request.Source.Files[0].Path != "SKILL.md" || intent.Request.Source.Files[0].Text != skillImportFile || key.Validate() != nil || len(r.Header.Values("Idempotency-Key")) != 1 {
		return errors.New("browser changed the original disk package intent")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.posts++
	if w.posts != 1 {
		return errors.New("browser repeated Skill import")
	}
	w.intent, w.key, w.body = intent, key, bytes.Clone(raw)
	w.keys = append(w.keys, string(key))
	return nil
}

func (w *skillImportWeb) response(r *http.Response) error {
	raw, err := w.completedResponse(r)
	if err != nil {
		return err
	}
	if r.StatusCode != http.StatusOK || r.Header.Get("Cache-Control") != "no-store" {
		return errors.New("Skill import response is not complete success")
	}
	path := r.Request.URL.Path
	w.mu.Lock()
	target := w.intent.Request.SkillID
	w.mu.Unlock()
	packagePath := variableHTTPPath(w.project.ID, "/skills/"+target.String()+"/revisions/1/package")
	packageRead := path == packagePath
	if r.Request.Method == http.MethodPost {
		var receipt skillInstallHTTPReceipt
		if path != variableHTTPPath(w.project.ID, "/skills") || agentWebDecode(raw, &receipt) != nil || receipt.SkillID != target || receipt.Revision != 1 || receipt.Version != 1 {
			return errors.New("Skill import receipt differs from original target")
		}
		w.mu.Lock()
		w.receipt = bytes.Clone(raw)
		w.mu.Unlock()
		if err = w.downloadMaterial(r.Request.Context()); err != nil {
			return err
		}
	} else if packageRead {
		if r.Header.Get("Content-Type") != "application/zip" || r.Header.Get("Content-Disposition") != "attachment; filename=\"skill-package.zip\"" || r.Header.Get("X-Content-Type-Options") != "nosniff" {
			return errors.New("Skill package headers differ")
		}
		if err = skillImportZip(raw); err != nil {
			return err
		}
		w.mu.Lock()
		w.packages++
		w.packageBytes = bytes.Clone(raw)
		w.mu.Unlock()
	}
	proof := map[string]any{"input_hash": w.inputHash, "request_id": r.Header.Get("X-Request-ID"), "method": r.Request.Method, "url_sha256": fmt.Sprintf("%x", sha256.Sum256([]byte(r.Request.URL.RequestURI()))), "status": r.StatusCode, "content_length": len(raw), "body_sha256": fmt.Sprintf("%x", sha256.Sum256(raw)), "request_host_matches": true, "original_eof": true, "original_closed": true, "handler_returned": true}
	if packageRead {
		proof["content_type"] = "application/zip"
		proof["content_disposition"] = "attachment; filename=\"skill-package.zip\""
		proof["cache_control"] = "no-store"
		proof["nosniff"] = true
		proof["package_verified"] = true
	}
	encoded, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(w.evidence, 0700); err != nil {
		return err
	}
	for _, prefix := range []string{"skill-import-response-", "skill-package-download-response-"} {
		if err = os.WriteFile(filepath.Join(w.evidence, prefix+r.Header.Get("X-Request-ID")+".json"), encoded, 0600); err != nil {
			return err
		}
	}
	w.mu.Lock()
	w.responses++
	w.mu.Unlock()
	r.Body = io.NopCloser(bytes.NewReader(raw))
	return nil
}

func skillImportZip(raw []byte) error {
	z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(z.File) != 1 || z.File[0].Name != "SKILL.md" || z.File[0].Mode()&os.ModeType != 0 {
		return errors.New("download is not the imported single-file package")
	}
	r, err := z.File[0].Open()
	if err != nil {
		return err
	}
	text, readErr := io.ReadAll(io.LimitReader(r, int64(len(skillImportFile)+1)))
	closeErr := r.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(text, []byte(skillImportFile)) {
		return errors.New("download rewrote original selected file bytes")
	}
	return nil
}

func (w *skillImportWeb) downloadMaterial(ctx context.Context) error {
	w.mu.Lock()
	target := w.intent.Request.SkillID
	key := w.key
	w.mu.Unlock()
	var hash string
	var size int64
	var metadata sc.Metadata
	var raw []byte
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT z.package_sha256,z.byte_size,jsonb_build_object('id',s.id,'project_id',s.project_id,'name',s.name,'normalized_name',s.normalized_name,'description',s.description,'protected',s.protected,'current_revision',s.current_revision::text,'version',s.version::text)
 FROM agenteam_skill.installations z JOIN agenteam_skill.skills s ON s.installation_id=z.id AND s.id=z.skill_id
 WHERE z.project_id=$1 AND z.skill_id=$2 AND z.command_key=$3 AND z.phase='published'`, w.project.ID.String(), target.String(), string(key)).Scan(&hash, &size, &raw)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(raw, &metadata); err != nil {
		return err
	}
	if metadata.Validate() != nil || metadata.Name != "Imported guide" || metadata.Description != "An existing single-file package" || metadata.Protected || metadata.CurrentRevision != 1 || metadata.Version != 1 || len(hash) != 71 || !strings.HasPrefix(hash, "sha256:") || size <= 0 || size > 2<<20 {
		return errors.New("installed package metadata differs from original file")
	}
	data, err := json.Marshal(map[string]any{"mode": "download", "cookie": w.a.p2.base.ownerBrowser.cookie, "project_id": w.project.ID.String(), "route": "/" + w.username + "/" + w.project.NormalizedName + "/settings/skills", "input_hash": w.inputHash, "revision": "1", "byte_size": size, "sha256": strings.TrimPrefix(hash, "sha256:"), "skill": metadata})
	if err != nil {
		return err
	}
	defer clear(data)
	return os.WriteFile(filepath.Join(w.directory, "skill-package-material.json"), data, 0600)
}

// Stop the browser-facing listener after its Node process exits, keeping the
// original verified backend alive for an explicit same-intent Lookup. The
// inherited close still owns both servers and all original handler tails.
func (w *skillImportWeb) closeFront(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := w.front.Shutdown(ctx); err != nil {
		t.Error("Skill import front needed force close", err)
		_ = w.front.Close()
	}
	if err := <-w.serve; !errors.Is(err, http.ErrServerClosed) {
		t.Error("Skill import front original Serve return", err)
	}
	w.frontWG.Wait()
	w.front = nil
}

func (w *skillImportWeb) facts(t *testing.T, ctx context.Context) string {
	t.Helper()
	var snapshot string
	var exact bool
	err := w.a.p2.base.raw.QueryRow(ctx, `SELECT
 z.phase='published' AND z.actor_user_id::text=$4 AND z.command_key=$3
 AND s.project_id=z.project_id AND s.id=z.skill_id AND s.revision_id=z.revision_id AND NOT s.protected AND s.serving AND s.current_revision=1 AND s.version=1
 AND r.installation_id=z.id AND r.skill_id=z.skill_id AND r.object_id=z.object_id AND r.revision=1
 AND o.project_id=z.project_id AND o.partition_id=z.project_id AND o.state='available' AND NOT o.cleaning
 AND o.byte_size=z.byte_size AND 'sha256:'||encode(o.sha256,'hex')=z.package_sha256
 AND u.object_id=o.id AND u.project_id=z.project_id AND u.state='committed' AND u.disposition='attached' AND u.owner_kind='skill_revision'
 AND (SELECT count(*) FROM agenteam_object.object_references ref WHERE ref.object_id=o.id AND ref.partition_id=z.project_id AND ref.owner_kind='skill_revision' AND ref.kind='canonical' AND ref.owner_id=u.owner_id AND ref.upload_id=u.id)=1
 AND (SELECT count(*) FROM agenteam_skill.installation_attempts a WHERE a.installation_id=z.id AND a.attempt_id=z.current_attempt_id AND a.object_id=o.id AND a.upload_id=u.id AND a.process_id::text=$5)=1
 AND (SELECT count(*) FROM agenteam_skill.installations WHERE project_id=z.project_id)=1
 AND (SELECT count(*) FROM agenteam_skill.skills WHERE project_id=z.project_id)=2
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.agent_assignments WHERE project_id=z.project_id)
 AND NOT EXISTS(SELECT 1 FROM agenteam_skill.work WHERE project_id=z.project_id AND phase<>'joined')
 AND NOT EXISTS(SELECT 1 FROM agenteam_object.object_leases WHERE object_id=o.id AND state='active'),
 jsonb_build_object('install',to_jsonb(z),'skill',to_jsonb(s),'revision',to_jsonb(r),'object',to_jsonb(o),'upload',to_jsonb(u),
 'refs',(SELECT jsonb_agg(to_jsonb(ref) ORDER BY ref.owner_kind,ref.owner_id) FROM agenteam_object.object_references ref WHERE ref.object_id=o.id),
 'attempts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.attempt_id) FROM agenteam_skill.installation_attempts a WHERE a.installation_id=z.id))::text
 FROM agenteam_skill.installations z JOIN agenteam_skill.skills s ON s.installation_id=z.id
 JOIN agenteam_skill.revisions r ON r.id=z.revision_id JOIN agenteam_object.objects o ON o.id=z.object_id
 JOIN agenteam_object.uploads u ON u.id=z.upload_id WHERE z.project_id=$1 AND z.skill_id=$2`, w.project.ID.String(), w.intent.Request.SkillID.String(), string(w.key), w.a.p2.base.ownerBrowser.actor.Details().UserID, w.process).Scan(&exact, &snapshot)
	agentAppRequire(t, err)
	if !exact {
		t.Fatal("Skill import lacks its original Skill/Revision/Object publication or actual read tails")
	}
	return snapshot
}

func (w *skillImportWeb) verify(t *testing.T, ctx context.Context) {
	t.Helper()
	w.mu.Lock()
	failure, posts, packages := w.failure, w.posts, w.packages
	w.mu.Unlock()
	if failure != nil || posts != 1 || packages != 1 || len(w.receipt) == 0 || len(w.packageBytes) == 0 {
		t.Fatal("Skill import browser originals incomplete", failure)
	}
	before := w.facts(t, ctx)
	response := w.request(t, w.a.p2.base.ownerBrowser, http.MethodPost, variableHTTPPath(w.project.ID, "/skills/commands/lookup"), w.body, w.key)
	if response.status != http.StatusOK || !bytes.Equal(response.body, w.receipt) {
		t.Fatal("original-key Skill Lookup changed receipt")
	}
	if after := w.facts(t, ctx); after != before {
		t.Fatal("read-only original intent Lookup changed Skill/Object publication")
	}
	var result struct {
		InputHash  string `json:"input_hash"`
		Completed  bool   `json:"completed"`
		OneCatalog bool   `json:"one_catalog"`
		OneGet     bool   `json:"one_get"`
		OnePackage bool   `json:"one_package"`
		Keyboard   bool   `json:"keyboard_focus"`
		Themes     bool   `json:"themes_layout"`
		Consumed   bool   `json:"original_consumed"`
		Published  bool   `json:"typed_published"`
		Joined     bool   `json:"observers_joined"`
		Responses  int    `json:"responses"`
		Length     int    `json:"download_length"`
		SHA        string `json:"download_sha256"`
		RequestID  string `json:"download_request_id"`
	}
	raw, err := os.ReadFile(filepath.Join(w.directory, "skill-package-result.json"))
	agentAppRequire(t, err)
	agentAppRequire(t, agentWebDecode(raw, &result))
	if result.InputHash != w.inputHash || !result.Completed || !result.OneCatalog || !result.OneGet || !result.OnePackage || !result.Keyboard || !result.Themes || !result.Consumed || !result.Published || !result.Joined || result.Responses < 3 || result.Length != len(w.packageBytes) || result.SHA != fmt.Sprintf("%x", sha256.Sum256(w.packageBytes)) {
		t.Fatal("browser saved package differs from actual original response")
	}
	var imported struct {
		Completed       bool   `json:"completed"`
		Posts           int    `json:"posts"`
		RequestID       string `json:"request_id"`
		ObserversJoined bool   `json:"observers_joined"`
	}
	raw, err = os.ReadFile(filepath.Join(w.directory, "skill-import-result.json"))
	agentAppRequire(t, err)
	agentAppRequire(t, agentWebDecode(raw, &imported))
	if !imported.Completed || imported.Posts != 1 || !imported.ObserversJoined || imported.RequestID == "" {
		t.Fatal("browser did not consume original import")
	}
}

func (w *skillImportWeb) browser(t *testing.T, parent context.Context) {
	t.Helper()
	root, err := filepath.Abs("../account-captcha-web")
	agentAppRequire(t, err)
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(root, "node_modules/@playwright/test/cli.js"), "test", "--config", filepath.Join(root, "skill-package-import.config.js"))
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
	cmd.Env = append(cmd.Env, "TMPDIR="+w.directory, "PLAYWRIGHT_NO_COPY_PROMPT=1", "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.directory, "AGENTEAM_AUTH_WEB_CHROMIUM=/usr/bin/chromium", "AGENTEAM_SKILL_IMPORT_WEB_CASE="+w.mode)
	for _, prefix := range []string{"AGENTEAM_SKILL_IMPORT_WEB", "AGENTEAM_SKILL_PACKAGE_WEB"} {
		cmd.Env = append(cmd.Env, prefix+"_DIST="+w.dist, prefix+"_EVIDENCE="+w.evidence, prefix+"_INPUT_HASH="+w.inputHash)
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err = cmd.Start(); err != nil {
		t.Fatal("Skill import Node did not start")
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
	t.Logf("Skill import Node actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	w.mu.Lock()
	tokens := append([]string(nil), w.keys...)
	w.mu.Unlock()
	tokens = append(tokens, w.a.p2.base.ownerBrowser.cookie, w.a.p2.base.ownerBrowser.csrf, skillImportFile)
	safe := output.String()
	for _, token := range tokens {
		if token != "" {
			safe = strings.ReplaceAll(safe, token, "[redacted]")
		}
	}
	t.Log(safe)
	if err != nil {
		t.Fatal("Skill import original browser failed")
	}
}
