//go:build integration

package skill_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/httpapi"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	outc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	skillhttp "github.com/LunaDeerTech/agenteam/internal/central/skill/http"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

// Only the existing fixture's real Bootstrap/Invitation/Redeem/Login and its
// Store are reused. Its unrelated controlled-Object Project is never read here.
// The target below uses real Project, Skill, Object/MinIO and Audit services;
// no ready, installation, revision or canonical Object is inserted by test SQL.
// This explicit library initializer does not bind production Project creation.
type skillPackageHTTPFixture struct {
	*skillOwnerHTTPFixture
	bundle skill.BuiltinBundle
}

func skillPackageHTTPAuthorities(t *testing.T, base *skillOwnerHTTPFixture) (cursor.Keyring, *account.Authority) {
	t.Helper()
	b64 := func(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }
	keys, err := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, b64(1)))
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := secret.LoadKeyring(fmt.Sprintf(`{"format":1,"current_version":"1","keys":[{"version":"1","key_b64":%q}]}`, b64(2)), keys)
	if err != nil {
		t.Fatal(err)
	}
	downloads, err := object.LoadDownloadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"d","keys":[{"kid":"d","key_b64":%q}]}`, b64(3)), keys, secrets)
	if err != nil {
		t.Fatal(err)
	}
	accountKeys, err := account.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"a","keys":[{"kid":"a","key_b64":%q}]}`, b64(4)), keys, secrets, downloads)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := account.NewAuthority(base.store, accountKeys)
	if err != nil {
		t.Fatal(err)
	}
	return keys, accounts
}

func newSkillPackageHTTPFixture(t *testing.T) *skillPackageHTTPFixture {
	t.Helper()
	base := newSkillOwnerHTTPFixture(t)
	store := base.store
	keys, accounts := skillPackageHTTPAuthorities(t, base)
	manifest, err := pc.NewRequiredManifest([]pc.ParticipantRegistration{
		{Name: pc.SkillsParticipant, ContractVersion: 1, OwnerModule: "skills", ReferenceKinds: []pc.ReferenceKind{"skill-initialization", "skill-installation", "skill-package-reader", "skill-object-cleanup"}},
		{Name: pc.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []pc.ReferenceKind{"object"}, CleanupAfter: []pc.ParticipantName{pc.SkillsParticipant}},
		{Name: pc.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []pc.ReferenceKind{"secret"}, CleanupAfter: []pc.ParticipantName{pc.SkillsParticipant}},
		{Name: pc.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", CleanupAfter: []pc.ParticipantName{pc.ArtifactObjectParticipant, pc.SecretParticipant, pc.SkillsParticipant}},
		{Name: pc.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", CleanupAfter: []pc.ParticipantName{pc.OutboxParticipant}},
	})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := project.NewLifecycleAuthority(store, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := object.NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts, Lifecycle: lifecycle, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.ObjectProducer: facts}})
	if err != nil {
		t.Fatal(err)
	}
	remote, err := objectfixture.Load()
	if err != nil {
		t.Fatal("owned Object fixture required")
	}
	s3, transport, err := remote.Client()
	if err != nil {
		t.Fatal("owned Object client unavailable")
	}
	t.Cleanup(transport.CloseIdleConnections)
	suffix, err := pgfixture.RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	bucket := "skill-install-" + remote.Nonce[:12] + "-" + suffix
	if err = s3.MakeBucket(testContext(t), bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal("owned bucket creation", err)
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	config, err := object.LoadStorageConfig(func(name string) (string, bool) {
		v, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return v, ok
	})
	if err != nil {
		t.Fatal("owned Object configuration")
	}
	backend, err := object.NewBackend(config)
	if err != nil {
		t.Fatal("Object backend", err)
	}
	process := testID[objc.Process](t)
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), process)
	if err != nil {
		_ = backend.Close()
		t.Fatal(err)
	}
	guard, err := object.OpenProcessGuard(spool, process)
	if err != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := guard.Close(); err != nil {
			t.Error("original unbound guard close", err)
		}
	})
	authority, err := skill.NewAuthority(store, projects)
	if err != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(err)
	}
	initialFacts, err := skill.NewInitializationAuditFacts(authority, facts)
	if err != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(err)
	}
	initialAudit, err := project.NewInitializationAuditAuthority(projects, initialFacts)
	if err != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(err)
	}
	cleanupAudit, err := skill.NewLifecycleAuditAuthority(initialAudit, authority, facts)
	if err != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(err)
	}
	aud, err := audit.New(store, keys, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: cleanupAudit})
	if err != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(err)
	}
	objects, err := object.New(store, backend, spool, aud, object.Authorizations{Planner: authority, Resources: authority, Read: authority, Gate: authority, Cleanup: authority, Processes: guard, ProjectStop: lifecycle})
	if err != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		objects.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := objects.Drain(ctx); err != nil {
			t.Error("original Object drain", err)
			_ = objects.Force(ctx)
		}
	})
	if err = objects.Initialize(testContext(t)); err != nil {
		t.Fatal("Object initialize", err)
	}
	bundle, err := skill.AddSkills(testContext(t))
	if err != nil {
		t.Fatal(err)
	}
	service, err := skill.New(skill.Dependencies{Authority: authority, Objects: objects, Processes: guard, ProcessID: process, Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil || !service.Joined() {
			t.Error("original Skill drain", err)
		}
	})
	catalog := event.NewCatalog()
	types, err := pc.RegisterProjectEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	outboxProcess, err := f.ParseID[outc.Process](process.String())
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]outc.ProducerAuthority{pc.ProjectProducer: projects}, Sessions: accounts, System: accounts, Projects: projects, Audit: aud, Cursors: keys, Processes: skillOwnerHTTPOutProcess{outboxProcess}})
	if err != nil {
		t.Fatal(err)
	}
	creator, err := project.New(store, project.Dependencies{Authority: projects, Activity: accounts, Audit: aud, Events: box, ProjectEvents: types, Initializer: service, Processes: skillOwnerHTTPOutProcess{outboxProcess}, Cursors: keys}, project.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		creator.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := creator.Drain(ctx); err != nil {
			t.Error("original Project drain", err)
		}
	})
	target := pc.CreateProjectRequest{ProjectID: testID[identity.Project](t), Name: "skill-package-http", Description: "owned installation prerequisite"}
	created, err := creator.CreateProject(testContext(t), base.ownerBrowser.actor, f.CommandMeta{RequestID: testID[f.Request](t), IdempotencyKey: f.IdempotencyKey(testID[struct{}](t).String())}, target)
	if err != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != target.ProjectID {
		t.Fatal("real target Project/Skills initialization", err)
	}
	var published bool
	if err = store.QueryRow(testContext(t), `SELECT EXISTS(SELECT 1 FROM agenteam_skill.initializations i JOIN agenteam_skill.skills s ON s.project_id=i.project_id AND s.creation_id=i.creation_id AND s.id=i.skill_id AND s.revision_id=i.revision_id WHERE i.project_id=$1 AND i.phase='published' AND s.protected AND s.serving)`, target.ProjectID.String()).Scan(&published); err != nil || !published {
		t.Fatal("target did not use real protected publication", err)
	}
	creator.Stop()
	if err = creator.Drain(testContext(t)); err != nil {
		t.Fatal("explicit Project initializer did not retire", err)
	}
	base.project, base.service = created.Project.ID, service
	reads, err := skillhttp.NewHTTPHandler(service, base.boundary)
	if err != nil {
		t.Fatal(err)
	}
	management, err := skillhttp.NewManagementHTTPHandler(service, keys, base.boundary)
	if err != nil {
		t.Fatal(err)
	}
	base.handler = httpapi.Handler(slog.New(slog.NewJSONHandler(base.logs, nil)), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if skillhttp.HandlesManagementRequest(r) {
			management.ServeHTTP(w, r)
			return
		}
		reads.ServeHTTP(w, r)
	}))
	return &skillPackageHTTPFixture{skillOwnerHTTPFixture: base, bundle: bundle}
}
