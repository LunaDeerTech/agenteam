//go:build integration

package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
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

// This single composition case observes the original default-root instances.
// It does not replace an authority, seed domain facts, expose Project Create
// over HTTP, or certify the separately bounded Object Runtime join work.
func TestKnowledgeSkillsDefaultRootComposition(t *testing.T) {
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
	check := func(stage string, err error) {
		t.Helper()
		if err == nil {
			return
		}
		var fault *f.Fault
		if errors.As(err, &fault) {
			t.Fatalf("root composition %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
		}
		t.Fatalf("root composition %s failed", stage)
	}
	output := newEventLog()
	logger, err := logging.New(logging.Central, slog.LevelInfo, output)
	check("logger", err)
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
			t.Error("original root did not join after composition cleanup")
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
			check("startup", result)
			t.Fatal("original root returned before listening")
		case <-startup.Done():
			t.Fatal("original root did not reach listening")
		}
	}
	// The listening event synchronizes construction. These are the actual
	// installed owners; no second service or late initializer is constructed.
	if core == nil || owned == nil {
		t.Fatal("default root did not retain Account and resource owners")
	}
	assembly, ok := owned.accounts().(*accountAssembly)
	if !ok {
		t.Fatal("default Account assembly missing")
	}
	assembly.mu.Lock()
	projects, projectOK := assembly.projects.(*projectCommandWork)
	skills, skillOK := assembly.skills.(*skillWork)
	documents, knowledgeOK := assembly.knowledge.(*knowledgeWork)
	assembly.mu.Unlock()
	objects, objectOK := owned.objects().(*objectAssembly)
	if !projectOK || !skillOK || !knowledgeOK || !objectOK {
		t.Fatal("default root did not install the concrete domain owners")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	login, err := fixtureAccountLoginResponse(ctx, cfg, core)
	check("formal login", err)
	var cookieMaterial sc.SecretMaterial
	useErr := login.UseCookie(func(raw []byte) error {
		var err error
		cookieMaterial, err = sc.NewSecretMaterial(raw)
		return err
	})
	closeErr := login.Close(ctx)
	defer cookieMaterial.Destroy()
	check("login cookie", useErr)
	check("login Close", closeErr)
	actor, err := core.Authenticate(ctx, cookieMaterial)
	check("current Session", err)
	session, err := core.GetSession(ctx, cookieMaterial)
	check("session CSRF", err)
	defer session.CSRF.Destroy()
	var cookie, csrf string
	check("cookie material", cookieMaterial.Use(func(raw []byte) error { cookie = string(raw); return nil }))
	check("CSRF material", session.CSRF.Use(func(raw []byte) error { csrf = string(raw); return nil }))
	meta := func() f.CommandMeta {
		return f.CommandMeta{RequestID: guardID[f.Request](t), IdempotencyKey: f.IdempotencyKey(guardID[struct{}](t).String())}
	}
	projectID := guardID[id.Project](t)
	created, err := projects.service.CreateProject(ctx, actor, meta(), pc.CreateProjectRequest{ProjectID: projectID, Name: "root-composition", Description: "Default root initialization"})
	check("Project Create", err)
	if created.Validate() != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != projectID || created.Operation != nil {
		t.Fatal("original Project Create did not confirm ready")
	}
	// SQL only observes actual results. Creation must traverse its original
	// Inspect/Initialize/DiscoverConfirmation/Confirm ports and private Audit
	// witness; this fixture never writes initialization or readiness facts.
	conn := db.Connect(t)
	var initialized, binding bool
	var creationState, skillPhase, skillID string
	var revision int64
	err = conn.QueryRow(ctx, `SELECT p.initialized_at IS NOT NULL,c.state,s.phase,
	 c.protected_skill_id=s.skill_id AND c.protected_revision=s.revision AND c.id=s.creation_id,
	 s.skill_id::text,s.revision
	 FROM agenteam_project.projects p JOIN agenteam_project.creations c ON c.id=p.creation_id
	 JOIN agenteam_skill.initializations s ON s.project_id=p.id WHERE p.id=$1`, projectID.String()).Scan(&initialized, &creationState, &skillPhase, &binding, &skillID, &revision)
	check("initialization facts", err)
	if !initialized || !binding || creationState != "completed" || skillPhase != "published" || revision != 1 {
		t.Fatal("Project confirmation and protected Skill facts disagree")
	}
	listing, err := skills.service.ListSkills(ctx, actor, projectID)
	check("same Skill directory", err)
	if len(listing) != 1 || listing[0].Validate() != nil || !listing[0].Protected || listing[0].ProjectID != projectID || listing[0].ID.String() != skillID || int64(listing[0].CurrentRevision) != revision {
		t.Fatal("same Skill service did not expose the confirmed protected revision")
	}
	reader, err := skills.service.OpenPackage(ctx, actor, projectID, listing[0].ID, listing[0].CurrentRevision)
	check("same Skill package", err)
	defer reader.Close()
	packageMeta, err := reader.Metadata()
	check("package metadata", err)
	packageBytes, readErr := io.ReadAll(io.LimitReader(reader, int64(packageMeta.Object.ByteSize)+1))
	closeErr = reader.Close()
	check("package read", readErr)
	check("package original Close", closeErr)
	if !reader.Joined() || len(packageBytes) != int(packageMeta.Object.ByteSize) || fmt.Sprintf("sha256:%x", sha256.Sum256(packageBytes)) != packageMeta.PackageSHA256.String() {
		t.Fatal("protected package bytes or original reader retirement disagree")
	}
	const text = "Default root Knowledge 正文\n"
	source, err := kc.NewTextSource(kc.PlainText, text)
	check("Knowledge input", err)
	documentID := guardID[kc.Document](t)
	document, err := documents.service.CreateDocument(ctx, actor, meta(), kc.CreateRequest{ProjectID: projectID, DocumentID: documentID, Title: "Root content"}, source)
	check("same Knowledge create", err)
	if document.Validate() != nil || document.ID != documentID || document.ProjectID != projectID {
		t.Fatal("same Knowledge service returned a different document")
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 10 * time.Second}
	defer client.CloseIdleConnections()
	call := func(method, path string, body []byte, modify func(*http.Request), status int) ([]byte, http.Header) {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, method, "http://"+address+path, bytes.NewReader(body))
		check("HTTP request", err)
		request.Host = "root.example.test"
		request.Header.Set("Origin", origin)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.AddCookie(&http.Cookie{Name: "__Host-agenteam_session", Value: cookie})
		if method != http.MethodGet && method != http.MethodHead {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", csrf)
			request.Header.Set("Idempotency-Key", guardID[struct{}](t).String())
		}
		if modify != nil {
			modify(request)
		}
		response, err := client.Do(request)
		check("HTTP transport", err)
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		closeErr := response.Body.Close()
		check("HTTP body", readErr)
		check("HTTP original Close", closeErr)
		if response.StatusCode != status || len(raw) > 1<<20 {
			// Status and a formal code are safe diagnostics; never log bodies,
			// Session material, arbitrary adapter errors or bootstrap output.
			var problem struct {
				Code f.Code `json:"code"`
			}
			_ = json.Unmarshal(raw, &problem)
			t.Fatalf("root %s %s: status=%d want=%d code=%s", method, path, response.StatusCode, status, problem.Code.Safe())
		}
		if len(response.Header.Values("X-Request-ID")) != 1 {
			t.Fatal("root request identity was not singular")
		}
		_, err = f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
		check("HTTP request identity", err)
		return raw, response.Header.Clone()
	}
	contentPath := "/api/v1/projects/" + projectID.String() + "/knowledge/documents/" + documentID.String() + "/content"
	raw, _ := call(http.MethodGet, contentPath, nil, nil, http.StatusOK)
	var content struct {
		Document struct {
			ID        kc.DocumentID `json:"id"`
			ProjectID id.ProjectID  `json:"project_id"`
		} `json:"document"`
		Text *struct {
			Text      string     `json:"text"`
			Next      f.Progress `json:"next_byte_offset"`
			Truncated bool       `json:"truncated"`
		} `json:"text"`
		Unavailable *string `json:"unavailable"`
	}
	check("content JSON", json.Unmarshal(raw, &content))
	if content.Document.ID != documentID || content.Document.ProjectID != projectID || content.Text == nil || content.Text.Text != text || content.Text.Next != f.Progress(len(text)) || content.Text.Truncated || content.Unavailable != nil {
		t.Fatal("default-root content route did not return current bounded bytes")
	}
	head, headers := call(http.MethodHead, contentPath, nil, nil, http.StatusOK)
	if len(head) != 0 || headers.Get("Content-Length") != strconv.Itoa(len(raw)) {
		t.Fatal("default-root content HEAD disagrees with GET representation")
	}
	call(http.MethodGet, "/api/v1/projects/"+projectID.String()+"/skills", nil, nil, http.StatusOK)
	// The unchanged Avatar provider shares the same Object service. A real
	// write, reader Close, and delete exercise maintenance and cleanup routing.
	profileBytes, _ := call(http.MethodGet, "/api/v1/me", nil, nil, http.StatusOK)
	var profile struct {
		User struct {
			Version f.Version `json:"version"`
		} `json:"user"`
		Avatar *struct {
			SHA256    f.Digest   `json:"sha256"`
			ByteSize  f.Progress `json:"byte_size"`
			MediaType string     `json:"media_type"`
		} `json:"avatar"`
	}
	check("profile JSON", json.Unmarshal(profileBytes, &profile))
	check("profile version", profile.User.Version.Validate())
	var pngBody bytes.Buffer
	check("PNG fixture", png.Encode(&pngBody, image.NewNRGBA(image.Rect(0, 0, 40, 24))))
	avatarReceipt, _ := call(http.MethodPut, "/api/v1/me/avatar", pngBody.Bytes(), func(r *http.Request) {
		r.Header.Set("Content-Type", "image/png")
		r.Header.Set("If-Match", `"`+strconv.FormatInt(int64(profile.User.Version), 10)+`"`)
	}, http.StatusOK)
	check("avatar receipt JSON", json.Unmarshal(avatarReceipt, &profile))
	if profile.Avatar == nil || profile.Avatar.MediaType != "image/jpeg" {
		t.Fatal("Avatar write did not publish its original safe representation")
	}
	avatar, headers := call(http.MethodGet, "/api/v1/me/avatar", nil, nil, http.StatusOK)
	decoded, err := jpeg.Decode(bytes.NewReader(avatar))
	check("avatar JPEG", err)
	if profile.Avatar.SHA256.String() != fmt.Sprintf("sha256:%x", sha256.Sum256(avatar)) || int64(profile.Avatar.ByteSize) != int64(len(avatar)) || headers.Get("Content-Type") != "image/jpeg" || decoded.Bounds().Dx() != 40 || decoded.Bounds().Dy() != 24 {
		t.Fatal("Avatar metadata and original reader bytes disagree")
	}
	deleteBody, err := json.Marshal(map[string]any{"version": profile.User.Version})
	check("avatar delete JSON", err)
	call(http.MethodDelete, "/api/v1/me/avatar", deleteBody, nil, http.StatusNoContent)
	call(http.MethodGet, "/api/v1/me/avatar", nil, nil, http.StatusNotFound)
	var liveReaders, liveSkillWork int
	check("reader lease retirement", conn.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'`).Scan(&liveReaders))
	check("Skill work retirement", conn.QueryRow(ctx, `SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase<>'joined'`, projectID.String()).Scan(&liveSkillWork))
	if liveReaders != 0 || liveSkillWork != 0 {
		t.Fatalf("original work has not retired: readers=%d skills=%d", liveReaders, liveSkillWork)
	}
	claim, err := os.OpenFile(filepath.Join(cfg.Objects().SpoolDirectory()+".processes", objects.process.String()+".claim"), os.O_RDWR, 0)
	check("original process claim", err)
	defer claim.Close()
	err = syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(claim.Fd()), syscall.LOCK_UN)
		t.Fatal("live default root released its shared process claim")
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		check("live process claim lock", err)
	}
	client.CloseIdleConnections()
	signals <- syscall.SIGTERM
	select {
	case <-done:
	case <-time.After(7 * time.Second):
		t.Fatal("original root SIGTERM did not join")
	}
	check("original root result", result)
	if !projects.Joined() || !skills.service.Joined() || !documents.Joined() || !assembly.Joined() || !owned.producersJoined() {
		t.Fatal("root returned without original domain and producer retirement")
	}
	check("retired process claim lock", syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	check("retired process claim unlock", syscall.Flock(int(claim.Fd()), syscall.LOCK_UN))
	var processState string
	check("retired process fact", conn.QueryRow(ctx, `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, objects.process.String()).Scan(&processState))
	if processState != "stopped" {
		t.Fatal("actual process guard did not publish stopped after producer join")
	}
}
