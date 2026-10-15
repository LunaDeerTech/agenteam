//go:build integration && linux

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/document"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// The browser replaces only the human HTTP mutations in the existing file
// workflow. Model, Object, Index.Run, Scheduler, tools and App exit stay real.
func TestKnowledgeM3AppOwnerFilesWeb(t *testing.T) {
	knowledgeM3OwnerFiles(t, newKnowledgeM3FilesWeb(t))
}

// Fail missing assets/tools before starting any PostgreSQL/App fixture.
func TestKnowledgeM3FilesWebPreflight(t *testing.T) {
	_ = newKnowledgeM3FilesWeb(t)
}

type knowledgeM3FilesWeb struct {
	private, evidence, dist, cli, chromium, node, origin string
	listener                                             net.Listener
}

func newKnowledgeM3FilesWeb(t *testing.T) *knowledgeM3FilesWeb {
	t.Helper()
	w := &knowledgeM3FilesWeb{
		dist: os.Getenv("AGENTEAM_KNOWLEDGE_FILES_WEB_DIST"), evidence: os.Getenv("AGENTEAM_KNOWLEDGE_FILES_WEB_EVIDENCE"),
		cli: os.Getenv("AGENTEAM_AUTH_WEB_PLAYWRIGHT_CLI"), chromium: os.Getenv("AGENTEAM_AUTH_WEB_CHROMIUM"), node: os.Getenv("AGENTEAM_AUTH_WEB_NODE"),
	}
	runtime := os.Getenv("AGENTEAM_AUTH_WEB_RUNTIME")
	for _, path := range []string{w.dist, w.evidence, runtime, w.cli, w.chromium, w.node} {
		if !filepath.IsAbs(path) {
			t.Fatal("exact private browser paths required")
		}
	}
	if len(runtime) > 45 {
		t.Fatal("short owned Chromium runtime required")
	}
	for _, path := range []string{filepath.Join(w.dist, "index.html"), w.cli, w.chromium, w.node} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("actual frozen assets and browser tools required")
		}
	}
	var installed struct{ Version string }
	packageJSON := knowledgeM3WebRead(t, filepath.Join(filepath.Dir(w.cli), "package.json"), 32<<10)
	if json.Unmarshal(packageJSON, &installed) != nil || installed.Version != "1.56.1" {
		t.Fatal("locked Playwright 1.56.1 required")
	}
	lock := knowledgeM3WebRead(t, "../../../tests/account-captcha-web/package-lock.json", 1<<20)
	installedLock := knowledgeM3WebRead(t, filepath.Join(filepath.Dir(w.cli), "../../../package-lock.json"), 1<<20)
	if !bytes.Equal(lock, installedLock) {
		t.Fatal("installed browser dependency lock differs")
	}
	var err error
	w.private, err = os.MkdirTemp(runtime, "kf-")
	workAppCheck(t, "knowledge-files-web-private", err)
	t.Cleanup(func() {
		workAppCheck(t, "knowledge-files-web-private-remove", os.RemoveAll(w.private))
		if _, err := os.Lstat(w.private); !errors.Is(err, os.ErrNotExist) {
			t.Error("private browser material remains")
		}
	})
	w.evidence = filepath.Join(w.evidence, t.Name())
	workAppCheck(t, "knowledge-files-web-evidence", os.Mkdir(w.evidence, 0700))
	w.listener, err = net.Listen("tcp", "127.0.0.1:0")
	workAppCheck(t, "knowledge-files-web-listener", err)
	t.Cleanup(func() { _ = w.listener.Close() })
	w.origin = "http://" + w.listener.Addr().String()
	return w
}

type knowledgeM3BrowserFile struct {
	Title       string `json:"title"`
	Filename    string `json:"filename"`
	Media       string `json:"media"`
	BytesBase64 string `json:"bytes_base64"`
	Text        string `json:"text"`
}

type knowledgeM3BrowserResult struct {
	InputHash string `json:"input_hash"`
	Stage     string `json:"stage"`
	Complete  bool   `json:"complete"`
	Documents []struct {
		ID      kc.DocumentID `json:"id"`
		Version f.Version     `json:"version"`
		Media   string        `json:"media"`
	} `json:"documents"`
	Downloads []struct {
		Ordinal int    `json:"ordinal"`
		SHA256  string `json:"sha256"`
	} `json:"downloads"`
	Screenshots []string `json:"screenshots"`
	Errors      int      `json:"errors"`
	ReceiptCode int      `json:"receipt_status"`
	ReceiptSize int      `json:"receipt_bytes"`
	ReceiptJSON bool     `json:"receipt_json"`
	Failure     *struct {
		VisibleDialogs  int    `json:"visible_dialogs"`
		TitleMatches    int    `json:"title_matches"`
		AutofocusExists bool   `json:"autofocus_exists"`
		AutofocusActive bool   `json:"autofocus_active"`
		AutofocusInert  bool   `json:"autofocus_inert"`
		ActiveTag       string `json:"active_tag"`
	} `json:"failure,omitempty"`
}

func (w *knowledgeM3FilesWeb) documents(t *testing.T, v *executionSchedulerApp, documents *knowledgeCallAssembly, credential sc.CredentialID) kc.DocumentRef {
	t.Helper()
	files := []knowledgeM3BrowserFile{
		{"Venus.txt", "Venus.txt", "text/plain", base64.StdEncoding.EncodeToString([]byte("Venus turns slowly.")), "Venus turns slowly."},
		{"Earth.md", "Earth.md", "text/markdown", base64.StdEncoding.EncodeToString([]byte("Earth has one moon.")), "Earth has one moon."},
		{"Mars.docx", "Mars.docx", kc.DOCX, base64.StdEncoding.EncodeToString(knowledgeM3DOCX(t, "Mars has two moons.")), "Mars has two moons."},
		{"Mercury.pdf", "Mercury.pdf", kc.PDF, base64.StdEncoding.EncodeToString(knowledgeM3PDF("Mercury's orbital period is 87 Earth days.")), "Mercury's orbital period is 87 Earth days.\n\n\f"},
	}
	const revised = "Mercury's orbital period is 88 Earth days."
	updated := knowledgeM3BrowserFile{BytesBase64: base64.StdEncoding.EncodeToString(knowledgeM3PDF(revised)), Text: revised + "\n\n\f"}
	publicInput := workAppJSON(t, struct {
		Files   []knowledgeM3BrowserFile `json:"files"`
		Updated knowledgeM3BrowserFile   `json:"updated_pdf"`
	}{files, updated})
	inputHash := fmt.Sprintf("%x", sha256.Sum256(publicInput))
	session, err := v.request(v.ctx, http.MethodGet, "/api/v1/session", nil, "")
	workAppCheck(t, "knowledge-files-web-session", err)
	var user struct {
		User struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if session.status != http.StatusOK || json.Unmarshal(session.body, &user) != nil || !regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`).MatchString(user.User.Username) {
		t.Fatal("formal browser Owner identity missing")
	}
	material := workAppJSON(t, map[string]any{"cookie": v.cookie, "route": "/" + user.User.Username + "/runtime-app-a/knowledge", "input_hash": inputHash, "files": files, "updated_pdf": updated})
	if len(material) > 16<<10 {
		t.Fatal("browser material exceeded bound")
	}
	workAppCheck(t, "knowledge-files-web-material", os.WriteFile(filepath.Join(w.private, "knowledge-files-material.json"), material, 0600))
	clear(material)
	stop := w.serve(t, v)
	result := w.browser(t, v.ctx, inputHash)
	stop()
	if !result.Complete || result.Stage != "complete" || result.Errors != 0 || len(result.Documents) != 4 || len(result.Downloads) != 4 || len(result.Screenshots) != 2 {
		t.Fatal("browser did not complete the exact four-format journey")
	}
	var pdf kc.DocumentRef
	seen := map[kc.DocumentID]bool{}
	for ordinal, file := range files {
		ref := result.Documents[ordinal]
		wantVersion, page := f.Version(1), 0
		if ordinal == 3 {
			wantVersion, page = 2, 1
		}
		if ref.ID.Validate() != nil || seen[ref.ID] || ref.Media != file.Media || ref.Version != wantVersion || result.Downloads[ordinal].Ordinal != ordinal {
			t.Fatal("browser document identity/version/media differs")
		}
		seen[ref.ID] = true
		raw, err := base64.StdEncoding.Strict().DecodeString(file.BytesBase64)
		workAppCheck(t, "knowledge-files-web-input", err)
		if result.Downloads[ordinal].SHA256 != fmt.Sprintf("%x", sha256.Sum256(raw)) {
			t.Fatal("browser download differs from original uploaded bytes")
		}
		if ordinal == 3 {
			raw, file.Text = knowledgeM3PDF(revised), updated.Text
		}
		head, err := documents.pair.Service().GetDocument(v.ctx, v.actor, v.a, ref.ID)
		workAppCheck(t, "knowledge-files-web-current-head", err)
		if head.Active == nil || head.Active.ContentVersion != wantVersion || head.Active.Title != file.Title {
			t.Fatal("browser receipt differs from current canonical document")
		}
		doc := *head.Active
		profile := knowledgeM3AwaitAutomatic(t, v, doc, "", strings.TrimRight(file.Text, "\n\f"))
		knowledgeM3RetentionFacts(t, v, profile, "active", "retained", credential)
		var parserProfile string
		workAppCheck(t, "knowledge-files-web-parser-profile", v.admin.QueryRow(v.ctx, `SELECT parser_profile FROM agenteam_knowledge.index_profiles WHERE project_id=$1 AND id=$2`, v.a.String(), profile).Scan(&parserProfile))
		if parserProfile != document.PopplerRegistryProfile {
			t.Fatal("browser workflow omitted the actual Poppler binding")
		}
		knowledgeM3FileProjection(t, v, doc, raw, file.Text, page)
		if ordinal == 3 {
			pdf = doc
			knowledgeM3FileCall(t, v, http.MethodGet, fmt.Sprintf("%s/revisions/1/source", doc.ID), nil, http.StatusConflict)
		}
	}
	t.Log("M3_KNOWLEDGE_FILES_BROWSER actual_upload_four_formats=true exact_downloads=true current_pdf_preview=true pdf_replace=true narrow_dark_keyboard=true")
	return pdf
}

func (w *knowledgeM3FilesWeb) serve(t *testing.T, v *executionSchedulerApp) func() {
	t.Helper()
	backend, err := url.Parse(v.address)
	workAppCheck(t, "knowledge-files-web-backend", err)
	transport := &http.Transport{Proxy: nil}
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.Transport = transport
	proxy.ErrorLog = slog.NewLogLogger(slog.NewTextHandler(io.Discard, nil), slog.LevelError)
	proxy.ErrorHandler = func(r http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(r, "Owned backend unavailable", http.StatusBadGateway)
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(r http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/api/") {
			proxy.ServeHTTP(r, request)
		} else if request.Method != http.MethodGet && request.Method != http.MethodHead {
			http.Error(r, "method", http.StatusMethodNotAllowed)
		} else if strings.HasPrefix(request.URL.Path, "/assets/") {
			skillWebServeAsset(r, request, w.dist)
		} else {
			http.ServeFile(r, request, filepath.Join(w.dist, "index.html"))
		}
	})}
	done := make(chan error, 1)
	go func() { done <- server.Serve(w.listener) }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			if err := server.Shutdown(v.ctx); err != nil {
				t.Error("original browser proxy exceeded the existing budget")
				_ = server.Close()
			}
			if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
				t.Error("original browser proxy Serve failed")
			}
			transport.CloseIdleConnections()
		})
	}
	t.Cleanup(stop)
	return stop
}

// Keep potentially credential-bearing Playwright diagnostics private and bounded.
type knowledgeM3BrowserOutput struct{ data []byte }

func (b *knowledgeM3BrowserOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := (64 << 10) - len(b.data)
	if len(p) > remaining {
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}

func (w *knowledgeM3FilesWeb) browser(t *testing.T, ctx context.Context, inputHash string) knowledgeM3BrowserResult {
	t.Helper()
	harness := filepath.Join(w.private, "harness")
	workAppCheck(t, "knowledge-files-web-harness", os.Mkdir(harness, 0700))
	for _, name := range []string{"knowledge-m3-files.config.js", "knowledge-m3-files.spec.js"} {
		raw := knowledgeM3WebRead(t, filepath.Join("../../../tests/account-captcha-web", name), 32<<10)
		workAppCheck(t, "knowledge-files-web-source", os.WriteFile(filepath.Join(harness, name), raw, 0600))
	}
	workAppCheck(t, "knowledge-files-web-module", os.WriteFile(filepath.Join(harness, "package.json"), []byte(`{"type":"module"}`), 0600))
	nodeModules := filepath.Dir(filepath.Dir(filepath.Dir(w.cli)))
	workAppCheck(t, "knowledge-files-web-dependencies", os.Symlink(nodeModules, filepath.Join(harness, "node_modules")))
	cmd := exec.CommandContext(ctx, w.node, w.cli, "test", "--config", filepath.Join(harness, "knowledge-m3-files.config.js"))
	cmd.Dir = harness
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key != "TMPDIR" && key != "DEBUG" && key != "PWDEBUG" && !strings.HasPrefix(key, "AGENTEAM_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "TMPDIR="+w.private, "AGENTEAM_AUTH_WEB_ORIGIN="+w.origin, "AGENTEAM_AUTH_WEB_PRIVATE="+w.private, "AGENTEAM_AUTH_WEB_CHROMIUM="+w.chromium, "PLAYWRIGHT_NO_COPY_PROMPT=1")
	var output knowledgeM3BrowserOutput
	cmd.Stdout, cmd.Stderr = &output, &output
	workAppCheck(t, "knowledge-files-web-node-start", cmd.Start())
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Cancel()
			<-done
		}
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		clear(output.data)
		cmd.Env = nil
	}()
	err := <-done
	joined = true
	t.Logf("M3_KNOWLEDGE_FILES_NODE actual_wait pid=%d success=%t", cmd.Process.Pid, err == nil)
	raw := knowledgeM3WebRead(t, filepath.Join(w.private, "knowledge-files-result.json"), 16<<10)
	var result knowledgeM3BrowserResult
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.InputHash != inputHash || !knowledgeM3BrowserStage(result.Stage) {
		t.Fatal("bounded browser result is not bound to the original inputs")
	}
	t.Logf("M3_KNOWLEDGE_FILES_BROWSER_RESULT stage=%s complete=%t documents=%d downloads=%d errors=%d", result.Stage, result.Complete, len(result.Documents), len(result.Downloads), result.Errors)
	if result.ReceiptCode < 0 || result.ReceiptCode > 599 || result.ReceiptSize < 0 || result.ReceiptSize > 2<<20 {
		t.Fatal("browser receipt observation outside explicit budget")
	}
	t.Logf("M3_KNOWLEDGE_FILES_BROWSER_WIRE status=%d bytes=%d json=%t", result.ReceiptCode, result.ReceiptSize, result.ReceiptJSON)
	if state := result.Failure; state != nil {
		if state.VisibleDialogs < 0 || state.VisibleDialogs > 10 || state.TitleMatches < 0 || state.TitleMatches > 10 || !strings.Contains("|INPUT|BUTTON|SECTION|BODY|IFRAME|OTHER|", "|"+state.ActiveTag+"|") {
			t.Fatal("browser failure observation outside closed state")
		}
		t.Logf("M3_KNOWLEDGE_FILES_BROWSER_DOM dialogs=%d title_matches=%d autofocus_exists=%t autofocus_active=%t autofocus_inert=%t active_tag=%s", state.VisibleDialogs, state.TitleMatches, state.AutofocusExists, state.AutofocusActive, state.AutofocusInert, state.ActiveTag)
	}
	if err != nil {
		// Export only source coordinates from the private assertion stack.
		// A failed locator/request's raw text and values remain private.
		locations := regexp.MustCompile(`knowledge-m3-files\.spec\.js:([0-9]{1,4}):([0-9]{1,4})`).FindAllSubmatch(output.data, 4)
		for _, location := range locations {
			t.Logf("M3_KNOWLEDGE_FILES_BROWSER_ASSERTION line=%s column=%s", location[1], location[2])
		}
	}
	for ordinal, name := range result.Screenshots {
		if ordinal > 2 || name != "knowledge-files-failure.png" && (ordinal > 1 || name != []string{"knowledge-files-1440-light.png", "knowledge-files-390-dark.png"}[ordinal]) {
			t.Fatal("unexpected public screenshot path")
		}
		image := knowledgeM3WebRead(t, filepath.Join(w.private, name), 8<<20)
		if !bytes.HasPrefix(image, []byte("\x89PNG\r\n\x1a\n")) {
			t.Fatal("browser screenshot is not PNG")
		}
		workAppCheck(t, "knowledge-files-web-export-image", os.WriteFile(filepath.Join(w.evidence, name), image, 0600))
	}
	// Marshal the closed DTO, never browser stdout or arbitrary provider data.
	workAppCheck(t, "knowledge-files-web-export-result", os.WriteFile(filepath.Join(w.evidence, "result.json"), workAppJSON(t, result), 0600))
	if err != nil || !regexp.MustCompile(`(?m)\b1 passed\b`).Match(output.data) {
		t.Fatal("locked browser did not actually pass its one scenario")
	}
	return result
}

func knowledgeM3BrowserStage(stage string) bool {
	parts := strings.Split(stage, "-")
	if len(parts) == 3 && parts[0] == "upload" && len(parts[1]) == 1 && parts[1][0] >= '0' && parts[1][0] <= '3' {
		switch parts[2] {
		case "open", "focus", "oversize", "choose", "submit", "status", "receipt", "fields", "selected", "projection":
			return true
		}
	}
	switch stage {
	case "starting", "upload-0", "upload-1", "upload-2", "upload-3", "download-0", "download-1", "download-2", "download-3", "pdf-preview", "pdf-replace", "narrow-dark", "complete":
		return true
	default:
		return false
	}
}

func knowledgeM3WebRead(t *testing.T, path string, limit int64) []byte {
	t.Helper()
	file, err := os.Open(path)
	workAppCheck(t, "knowledge-files-web-read", err)
	raw, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	closed := file.Close()
	workAppCheck(t, "knowledge-files-web-read-return", errors.Join(readErr, closed))
	if int64(len(raw)) > limit {
		t.Fatal("browser fixture exceeded explicit read bound")
	}
	return raw
}
