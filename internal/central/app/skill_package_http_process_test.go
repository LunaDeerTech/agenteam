//go:build integration

package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
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
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	skillcontract "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
)

// Only the upstream existing Project is initialized through the explicit
// library fixture. The production Project service is never replaced. Every
// Skill installation and package read enters the actual default-root listener and its real services.
func TestSkillPackageHTTPDefaultRootComposition(t *testing.T) {
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
			t.Fatalf("package root %s: %s/%s", stage, fault.Code.Safe(), fault.CommitState.Safe())
		}
		t.Fatalf("package root %s failed", stage)
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
			t.Error("original package root did not join after cleanup")
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
			t.Fatal("package root returned before listening")
		case <-startup.Done():
			t.Fatal("package root did not reach listening")
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
	created, createErr := fixture.CreateProject(ctx, actor, meta, pc.CreateProjectRequest{ProjectID: projectID, Name: "skill-package-root", Description: "Explicit existing Project fixture"})
	drainErr := fixtureWork.Drain(ctx)
	check(t, "explicit Project Create", createErr)
	check(t, "explicit Project Drain", drainErr)
	if !fixtureWork.Joined() || created.Validate() != nil || created.State != pc.CreationReady || created.Operation != nil || created.Project == nil || created.Project.ID != projectID {
		t.Fatal("explicit Project fixture did not create and retire")
	}

	conn := db.Connect(t)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 35 * time.Second}
	defer client.CloseIdleConnections()
	call := func(t *testing.T, method, path, body string, status int) ([]byte, http.Header) {
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
			request.Header.Set("Idempotency-Key", guardID[struct{}](t).String())
		}
		response, err := client.Do(request)
		check(t, "HTTP transport", err)
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, skillcontract.MaxArchiveBytes+1))
		closeErr := response.Body.Close()
		check(t, "HTTP body", readErr)
		check(t, "HTTP original Close", closeErr)
		if response.StatusCode != status || len(raw) > skillcontract.MaxArchiveBytes {
			var problem struct {
				Code f.Code `json:"code"`
			}
			_ = json.Unmarshal(raw, &problem)
			t.Fatalf("package root response: status=%d want=%d code=%s", response.StatusCode, status, problem.Code.Safe())
		}
		if response.Header.Get("Cache-Control") != "no-store" || len(response.Header.Values("X-Request-ID")) != 1 {
			t.Fatal("package root lost safe cache/request headers")
		}
		_, err = f.ParseID[f.Request](response.Header.Get("X-Request-ID"))
		check(t, "HTTP request identity", err)
		return raw, response.Header.Clone()
	}
	prefix := "/api/v1/projects/" + projectID.String() + "/skills"
	const entry = "---\nname: Native package guide\ndescription: Real root package installation\n---\n原始Skill正文。\n"
	const reference = "Native canonical reference 🌍\n"
	target := guardID[pc.Skill](t)
	input, err := json.Marshal(map[string]any{"request": map[string]any{
		"skill_id": target.String(), "mode": "create",
		"source": map[string]any{"kind": "text_files", "files": []map[string]string{
			{"path": "SKILL.md", "utf8_text": entry},
			{"path": "references/guide.txt", "utf8_text": reference},
		}},
	}})
	check(t, "installation input", err)
	install, _ := call(t, http.MethodPost, prefix, string(input), http.StatusOK)
	var receipt struct {
		SkillID  pc.SkillID `json:"skill_id"`
		Revision f.Revision `json:"revision"`
		Version  f.Version  `json:"version"`
	}
	check(t, "installation receipt", json.Unmarshal(install, &receipt))
	if receipt.SkillID != target || receipt.Revision != 1 || receipt.Version != 1 {
		t.Fatal("root installation did not confirm original revision")
	}
	listing, err := skills.service.ListSkills(ctx, actor, projectID)
	check(t, "builtin metadata", err)
	if len(listing) != 1 || !listing[0].Protected {
		t.Fatal("root protected initialization missing")
	}
	for _, item := range []struct {
		name  string
		id    pc.SkillID
		files map[string]string
	}{
		{"builtin", listing[0].ID, nil},
		{"installed", target, map[string]string{"SKILL.md": entry, "references/guide.txt": reference}},
	} {
		t.Run(item.name, func(t *testing.T) {
			detail, _ := call(t, http.MethodGet, prefix+"/"+item.id.String(), "", http.StatusOK)
			var metadata skillcontract.Metadata
			check(t, "metadata JSON", json.Unmarshal(detail, &metadata))
			if metadata.Validate() != nil || metadata.ID != item.id || metadata.ProjectID != projectID || metadata.CurrentRevision != 1 {
				t.Fatal("root metadata lost immutable revision")
			}
			path := prefix + "/" + metadata.ID.String() + "/revisions/" + strconv.FormatInt(int64(metadata.CurrentRevision), 10) + "/package"
			var digest, manifest string
			var size int64
			check(t, "canonical package facts", conn.QueryRow(ctx, `SELECT 'sha256:'||encode(o.sha256,'hex'),o.byte_size,COALESCE(i.manifest::text,convert_from(n.manifest,'UTF8'))
 FROM agenteam_skill.revisions r JOIN agenteam_object.objects o ON o.id=r.object_id
 LEFT JOIN agenteam_skill.initializations i ON i.revision_id=r.id
 LEFT JOIN agenteam_skill.installations n ON n.id=r.installation_id
 WHERE r.project_id=$1 AND r.skill_id=$2 AND r.revision=$3`, projectID.String(), item.id.String(), int64(metadata.CurrentRevision)).Scan(&digest, &size, &manifest))
			workCount := func() int {
				t.Helper()
				var count, active, leases int
				check(t, "actual reader work", conn.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND skill_id=$2 AND kind IN ('package_reader','installed_package_reader')),
 (SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND skill_id=$2 AND phase<>'joined'),
 (SELECT count(*) FROM agenteam_object.object_leases l JOIN agenteam_skill.revisions r ON r.object_id=l.object_id WHERE r.project_id=$1 AND r.skill_id=$2 AND l.owner_kind='reader' AND l.state='active')`, projectID.String(), item.id.String()).Scan(&count, &active, &leases))
				if active != 0 || leases != 0 {
					t.Fatal("root returned before original reader work and leases retired")
				}
				return count
			}
			before := workCount()
			raw, headers := call(t, http.MethodGet, path, "", http.StatusOK)
			if int64(len(raw)) != size || headers.Get("Content-Type") != skillcontract.PackageMediaType || headers.Get("Content-Disposition") != `attachment; filename="skill-package.zip"` || headers.Get("X-Content-Type-Options") != "nosniff" || headers.Get("Content-Length") != strconv.FormatInt(size, 10) || digest != fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) {
				t.Fatal("root GET differs from complete canonical ZIP or safe headers")
			}
			stored, err := skillcontract.DecodeManifest([]byte(manifest))
			check(t, "stored manifest", err)
			details, err := stored.Details()
			check(t, "manifest fields", err)
			archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
			check(t, "complete ZIP", err)
			if len(archive.File) != len(details.Files) {
				t.Fatal("root ZIP entry count differs from original manifest")
			}
			seen := map[string]bool{}
			for index, file := range archive.File {
				reader, err := file.Open()
				check(t, "ZIP entry", err)
				body, readErr := io.ReadAll(io.LimitReader(reader, skillcontract.MaxFileBytes+1))
				closeErr := reader.Close()
				check(t, "ZIP entry read", readErr)
				check(t, "ZIP entry Close", closeErr)
				expected := details.Files[index]
				if seen[file.Name] || expected.Path != file.Name || int(expected.ByteSize) != len(body) || expected.SHA256.String() != fmt.Sprintf("sha256:%x", sha256.Sum256(body)) || item.files != nil && item.files[file.Name] != string(body) {
					t.Fatal("root ZIP entry differs from manifest or original text_files")
				}
				seen[file.Name] = true
			}
			if !seen[skillcontract.EntryPath] || item.files != nil && len(seen) != len(item.files) || workCount() != before+1 {
				t.Fatal("root GET lost canonical entries or its original Close")
			}
			head, headHeaders := call(t, http.MethodHead, path, "", http.StatusOK)
			if len(head) != 0 || headHeaders.Get("Content-Length") != headers.Get("Content-Length") || headHeaders.Get("Content-Type") != headers.Get("Content-Type") || headHeaders.Get("Content-Disposition") != headers.Get("Content-Disposition") || headHeaders.Get("X-Content-Type-Options") != "nosniff" || workCount() != before+2 {
				t.Fatal("root HEAD skipped full read/Close or disagreed with GET")
			}
			missing := prefix + "/" + item.id.String() + "/revisions/2/package"
			call(t, http.MethodGet, missing, "", http.StatusNotFound)
			badHead, _ := call(t, http.MethodHead, missing, "", http.StatusNotFound)
			if len(badHead) != 0 || workCount() != before+2 {
				t.Fatal("missing revision exposed bytes or opened a package")
			}
		})
	}
	check(t, "formal Logout", core.Logout(ctx, account.LogoutRequest{Actor: actor, Key: f.IdempotencyKey(guardID[struct{}](t).String())}))
	revokedPath := prefix + "/" + target.String() + "/revisions/1/package"
	revoked, _ := call(t, http.MethodGet, revokedPath, "", http.StatusUnauthorized)
	var problem struct {
		Code f.Code `json:"code"`
	}
	check(t, "revoked Problem", json.Unmarshal(revoked, &problem))
	if problem.Code != f.SessionRevoked {
		t.Fatal("root revoked Session lost original rejection")
	}
	revokedHead, _ := call(t, http.MethodHead, revokedPath, "", http.StatusUnauthorized)
	if len(revokedHead) != 0 {
		t.Fatal("root revoked HEAD exposed a body")
	}
	var liveReaders int
	check(t, "reader lease retirement", conn.QueryRow(ctx, `SELECT count(*) FROM agenteam_object.object_leases WHERE owner_kind='reader' AND state='active'`).Scan(&liveReaders))
	if liveReaders != 0 {
		t.Fatal("original package HTTP reader did not retire")
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
		t.Fatal("original package root SIGTERM did not join")
	}
	check(t, "original root result", result)
	if !projects.Joined() || !skills.service.Joined() || !documents.Joined() || !assembly.Joined() || !owned.producersJoined() {
		t.Fatal("package root returned without original producer retirement")
	}
	check(t, "retired claim lock", syscall.Flock(int(claim.Fd()), syscall.LOCK_EX|syscall.LOCK_NB))
	check(t, "retired claim unlock", syscall.Flock(int(claim.Fd()), syscall.LOCK_UN))
	var processState string
	check(t, "retired process fact", conn.QueryRow(ctx, `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, objects.process.String()).Scan(&processState))
	if processState != "stopped" {
		t.Fatal("original ProcessGuard did not retire after actual producer join")
	}
}
