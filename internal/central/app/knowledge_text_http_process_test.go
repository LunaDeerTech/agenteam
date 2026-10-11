//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Only the upstream existing Project is initialized through the explicit
// library fixture. The production Project service is never replaced. Every
// text mutation enters the actual default-root listener and its real services.
func TestKnowledgeTextHTTPDefaultRootComposition(t *testing.T) {
	db := pgfixture.NewDatabase(t)
	const origin = "https://root.example.test"
	cfg := guardConfiguration(t, append(guardEnvironment(t, db),
		"AGENTEAM_CENTRAL_PUBLIC_ORIGIN="+origin,
		"AGENTEAM_CENTRAL_SHUTDOWN_TIMEOUT=5s"))
	var core *account.Service
	var owned *resources
	deps := dependencies{
		observeAccount: func(v *account.Service) { core = v },
		bind: func(ctx context.Context, cfg config.Config, store database, owner *resources, d *dependencies) error {
			owned = owner
			return bindAccounts(ctx, cfg, store, owner, d)
		},
	}
	check := func(t *testing.T, stage string, err error) {
		t.Helper()
		if err == nil {
			return
		}
		var fault *f.Fault
		if errors.As(err, &fault) {
			t.Fatalf("text root %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
		}
		t.Fatalf("text root %s failed", stage)
	}
	output := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	check(t, "logger", err)
	rootContext, cancelRoot := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 2)
	done := make(chan struct{})
	var result error
	go func() { result = run(rootContext, cfg, logger, signals, deps); close(done) }()
	t.Cleanup(func() {
		cancelRoot()
		select {
		case <-done:
		case <-time.After(7 * time.Second):
			t.Error("original text root did not join after cleanup")
		}
	})
	startup, cancelStartup := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancelStartup()
	var address string
	for address == "" {
		select {
		case event := <-output.events:
			if event["event"] == "listening" {
				address, _ = event["listen_address"].(string)
			}
		case <-done:
			check(t, "startup", result)
			t.Fatal("text root returned before listening")
		case <-startup.Done():
			t.Fatal("text root did not reach listening")
		}
	}
	if core == nil || owned == nil {
		t.Fatal("actual Account and resource owners missing")
	}
	assembly, ok := owned.accounts().(*accountAssembly)
	if !ok {
		t.Fatal("actual Account assembly missing")
	}
	assembly.mu.Lock()
	projects, projectOK := assembly.projects.(*projectCommandWork)
	skills, skillOK := assembly.skills.(*skillWork)
	documents, knowledgeOK := assembly.knowledge.(*knowledgeWork)
	assembly.mu.Unlock()
	objects, objectOK := owned.objects().(*objectAssembly)
	if !projectOK || !skillOK || !knowledgeOK || !objectOK {
		t.Fatal("actual default-root domain owners missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	login, err := fixtureAccountLoginResponse(ctx, cfg, core)
	check(t, "formal Login", err)
	var cookieMaterial sc.SecretMaterial
	useErr := login.UseCookie(func(raw []byte) error {
		var err error
		cookieMaterial, err = sc.NewSecretMaterial(raw)
		return err
	})
	closeErr := login.Close(ctx)
	defer cookieMaterial.Destroy()
	check(t, "Login cookie", useErr)
	check(t, "Login original Close", closeErr)
	actor, err := core.Authenticate(ctx, cookieMaterial)
	check(t, "current Session", err)
	session, err := core.GetSession(ctx, cookieMaterial)
	check(t, "current CSRF", err)
	defer session.CSRF.Destroy()
	var cookie, csrf string
	check(t, "cookie material", cookieMaterial.Use(func(raw []byte) error { cookie = string(raw); return nil }))
	check(t, "CSRF material", session.CSRF.Use(func(raw []byte) error { csrf = string(raw); return nil }))
	fixture := rootCompositionProjectFixture(t, cfg, owned, objects, skills.service)
	fixtureWork := &projectCommandWork{service: fixture}
	t.Cleanup(func() {
		if !fixtureWork.Joined() {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := fixtureWork.Drain(cleanup); err != nil || !fixtureWork.Joined() {
				t.Error("explicit Project fixture did not retire")
			}
		}
	})
	projectID := guardID[id.Project](t)
	meta := f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: f.IdempotencyKey(guardID[struct{}](t).String())}
	created, createErr := fixture.CreateProject(ctx, actor, meta, pc.CreateProjectRequest{ProjectID: projectID, Name: "text-http-root", Description: "Explicit existing Project fixture"})
	drainErr := fixtureWork.Drain(ctx)
	check(t, "explicit Project Create", createErr)
	check(t, "explicit Project Drain", drainErr)
	if !fixtureWork.Joined() || created.Validate() != nil || created.State != pc.CreationReady || created.Operation != nil || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("explicit Project fixture did not create and retire")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	call := func(t *testing.T, method, path, body, key string, status int) []byte {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, method, "http://"+address+path, bytes.NewBufferString(body))
		check(t, "HTTP request", err)
		request.Host = "root.example.test"
		request.Header.Set("Origin", origin)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: cookie})
		if method == http.MethodPost {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", csrf)
			request.Header.Set("Idempotency-Key", key)
		}
		response, err := client.Do(request)
		check(t, "HTTP transport", err)
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		closeErr := response.Body.Close()
		check(t, "HTTP body", readErr)
		check(t, "HTTP original Close", closeErr)
		if response.StatusCode != status || len(raw) > 1<<20 {
			var problem struct {
				Code f.Code `json:"code"`
			}
			_ = json.Unmarshal(raw, &problem)
			t.Fatalf("text root response: status=%d want=%d code=%s", response.StatusCode, status, problem.Code.Safe())
		}
		if response.Header.Get("Cache-Control") != "no-store" || response.Header.Get("Content-Length") != strconv.Itoa(len(raw)) || len(response.Header.Values("X-Request-ID")) != 1 {
			t.Fatal("text root lost bounded complete response or request identity")
		}
		_, err = f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
		check(t, "HTTP request identity", err)
		for _, field := range []string{`"object_id"`, `"upload_id"`, `"semantic_digest"`, `"session_id"`} {
			if bytes.Contains(raw, []byte(field)) {
				t.Fatal("private HTTP field escaped")
			}
		}
		return raw
	}
	encode := func(t *testing.T, value any) string {
		raw, err := json.Marshal(value)
		check(t, "input JSON", err)
		return string(raw)
	}
	prefix := "/api/v1/projects/" + projectID.String() + "/knowledge/documents"
	for _, media := range []string{kc.PlainText, kc.Markdown} {
		t.Run(media, func(t *testing.T) {
			documentID := guardID[kc.Document](t)
			path := prefix + "/" + documentID.String()
			title := "Root " + media
			original := "Original canonical 世界\n" + media
			create := encode(t, map[string]any{"parent_document_id": nil, "title": title, "media_type": media, "text": original})
			createKey := guardID[struct{}](t).String()
			created := call(t, http.MethodPost, path+"/create-text", create, createKey, http.StatusOK)
			verify := func(raw []byte, field, version string) json.RawMessage {
				t.Helper()
				var envelope map[string]json.RawMessage
				check(t, "document envelope", json.Unmarshal(raw, &envelope))
				var document map[string]any
				check(t, "document DTO", json.Unmarshal(envelope[field], &document))
				if len(envelope) != 1 || len(document) != 12 || document["id"] != documentID.String() || document["project_id"] != projectID.String() || document["parent_document_id"] != nil || document["title"] != title || document["content_version"] != version || document["media_type"] != media || document["source_kind"] != "text" {
					t.Fatal("root text document projection differs from intent")
				}
				return envelope[field]
			}
			read := func(version, text string) {
				t.Helper()
				metadata := verify(call(t, http.MethodGet, path, "", "", http.StatusOK), "active", version)
				var content struct {
					Document json.RawMessage `json:"document"`
					Text     *struct {
						Text      string `json:"text"`
						Next      string `json:"next_byte_offset"`
						Truncated bool   `json:"truncated"`
					} `json:"text"`
				}
				check(t, "content JSON", json.Unmarshal(call(t, http.MethodGet, path+"/content", "", "", http.StatusOK), &content))
				if !bytes.Equal(content.Document, metadata) || content.Text == nil || content.Text.Text != text || content.Text.Next != strconv.Itoa(len(text)) || content.Text.Truncated {
					t.Fatal("root canonical content reread differs from current metadata/bytes")
				}
			}
			lookup := func(command, request, key string, document json.RawMessage) {
				t.Helper()
				body := encode(t, map[string]any{"command": command, "document_id": documentID.String(), "request": json.RawMessage(request)})
				var result struct {
					State   string `json:"state"`
					Receipt *struct {
						Command  string          `json:"command"`
						Document json.RawMessage `json:"document"`
						Changed  bool            `json:"changed"`
					} `json:"receipt"`
				}
				check(t, "Lookup JSON", json.Unmarshal(call(t, http.MethodPost, prefix+"/text-commands/lookup", body, key, http.StatusOK), &result))
				if result.State != "committed" || result.Receipt == nil || result.Receipt.Command != command || !result.Receipt.Changed || !bytes.Equal(result.Receipt.Document, document) {
					t.Fatal("root Lookup lost the original committed text receipt")
				}
			}
			first := verify(created, "document", "1")
			read("1", original)
			lookup("create", create, createKey, first)
			if !bytes.Equal(created, call(t, http.MethodPost, path+"/create-text", create, createKey, http.StatusOK)) {
				t.Fatal("root create replay changed its receipt")
			}
			replacement := "Replacement canonical 🌍\n" + media
			update := encode(t, map[string]any{"expected_version": "1", "media_type": media, "text": replacement})
			updateKey := guardID[struct{}](t).String()
			updated := call(t, http.MethodPost, path+"/replace-text", update, updateKey, http.StatusOK)
			second := verify(updated, "document", "2")
			read("2", replacement)
			lookup("update", update, updateKey, second)
			lookup("create", create, createKey, first)
			if !bytes.Equal(updated, call(t, http.MethodPost, path+"/replace-text", update, updateKey, http.StatusOK)) {
				t.Fatal("root replacement replay changed its receipt")
			}
			var problem struct {
				Code f.Code `json:"code"`
			}
			check(t, "stale Problem", json.Unmarshal(call(t, http.MethodPost, path+"/replace-text", update, guardID[struct{}](t).String(), http.StatusConflict), &problem))
			if problem.Code != f.VersionConflict {
				t.Fatal("root stale version lost formal rejection")
			}
		})
	}
	conn := db.Connect(t)
	var liveReaders int
	check(t, "reader lease retirement", conn.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'`).Scan(&liveReaders))
	if liveReaders != 0 {
		t.Fatal("original text HTTP reader did not retire")
	}
	claim, err := os.OpenFile(filepath.Join(cfg.Objects().SpoolDirectory()+".processes", objects.process.String()+".claim"), os.O_RDWR, 0)
	check(t, "original process claim", err)
	defer claim.Close()
	err = syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
		t.Fatal("live default root released its process claim")
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		check(t, "live process claim lock", err)
	}
	client.CloseIdleConnections()
	signals <- syscall.SIGTERM
	select {
	case <-done:
	case <-time.After(7 * time.Second):
		t.Fatal("original text root SIGTERM did not join")
	}
	check(t, "original root result", result)
	if !projects.Joined() || !skills.service.Joined() || !documents.Joined() || !assembly.Joined() || !owned.producersJoined() {
		t.Fatal("text root returned without original producer retirement")
	}
	check(t, "retired claim lock", syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	check(t, "retired claim unlock", syscall.Flock(int(claim.Fd()), syscall.LOCK_UN))
	var processState string
	check(t, "retired process fact", conn.QueryRow(ctx, `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, objects.process.String()).Scan(&processState))
	if processState != "stopped" {
		t.Fatal("original ProcessGuard did not retire after actual producer join")
	}
}
