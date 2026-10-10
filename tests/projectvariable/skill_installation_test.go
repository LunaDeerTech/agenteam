//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	outc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

// The inherited fixture is used for actual Account Bootstrap/Invitation/
// Redeem/Login. Its older controlled-Skills Project is unrelated to this case.
// The target Project below is created with the real Skill initializer, Object
// backend, Audit and P2. No installation, attempt, receipt or serving Skill is
// inserted by test SQL. Runtime/foreign-process recovery stays unbound.
type skillInstallationFixture struct {
	base     *variableHTTPFixture
	service  *skill.Service
	objects  *object.Service
	manifest pc.RequiredManifest
	project  pc.ProjectRef
	s3       *minio.Client
	bucket   string
}

func newSkillInstallationFixture(t *testing.T) *skillInstallationFixture {
	t.Helper()
	return newSkillInstallationFixtureWithAuthority(t, nil)
}

// The optional factory binds a real execution-install producer after opening
// the actual guard and before Object/Skill construction. The nil/default path
// retains the Human fixture; it cannot register that service as a Runtime source.
func newSkillInstallationFixtureWithAuthority(t *testing.T, factory func(*hookStore, *project.Authority, *object.ProcessGuard, objc.ProcessID) (*skill.Authority, error)) *skillInstallationFixture {
	t.Helper()
	base := newVariableHTTPFixture(t) // newDatabase migrates the continuous 00001–36 source.
	store := base.tracked
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
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: base.accounts, Routes: base.accounts, Lifecycle: lifecycle, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.ObjectProducer: facts}})
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
	if err = s3.MakeBucket(ctxFor(t), bucket, minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
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
	process := id[objc.Process](t)
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
	var authority *skill.Authority
	if factory == nil {
		authority, err = skill.NewAuthority(store, projects)
	} else {
		authority, err = factory(store, projects, guard, process)
	}
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
	aud, err := audit.New(store, base.keys, audit.Authorizations{Sessions: base.accounts, System: base.accounts, Accounts: base.accounts, Projects: cleanupAudit})
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
	if err = objects.Initialize(ctxFor(t)); err != nil {
		t.Fatal("Object initialize", err)
	}
	bundle, err := skill.AddSkills(ctxFor(t))
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
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]outc.ProducerAuthority{pc.ProjectProducer: projects}, Sessions: base.accounts, System: base.accounts, Projects: projects, Audit: aud, Cursors: base.keys, Processes: fixtureProcess{outboxProcess}})
	if err != nil {
		t.Fatal(err)
	}
	creator, err := project.New(store, project.Dependencies{Authority: projects, Activity: base.accounts, Audit: aud, Events: box, ProjectEvents: types, Initializer: service, Processes: fixtureProcess{outboxProcess}, Cursors: base.keys}, project.DefaultConfig())
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
	target := pc.CreateProjectRequest{ProjectID: id[identity.Project](t), Name: "skill-install-fixture", Description: "owned installation prerequisite"}
	created, err := creator.CreateProject(ctxFor(t), base.ownerBrowser.actor, meta(t, id[struct{}](t).String(), nil), target)
	if err != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != target.ProjectID {
		t.Fatal("real target Project/Skills initialization", err)
	}
	var published bool
	if err = store.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_skill.initializations i JOIN agenteam_skill.skills s ON s.project_id=i.project_id AND s.creation_id=i.creation_id AND s.id=i.skill_id AND s.revision_id=i.revision_id WHERE i.project_id=$1 AND i.phase='published' AND s.protected AND s.serving)`, target.ProjectID.String()).Scan(&published); err != nil || !published {
		t.Fatal("target did not use real protected publication", err)
	}
	return &skillInstallationFixture{base: base, service: service, objects: objects, manifest: manifest, project: *created.Project, s3: s3, bucket: bucket}
}

// Only acceptance/remaining participant stage facts are an explicit upstream
// lifecycle fixture. The Skill and Object stops below are real calls. This is
// not a complete Project scheduler or other-participant join acceptance.
func (v *skillInstallationFixture) enterCleanup(t *testing.T) (identity.Actor, pc.LifecycleCause, pc.ScopeRef) {
	t.Helper()
	cause := pc.LifecycleCause{OperationID: id[pc.Operation](t), Action: pc.Delete, ProjectVersion: v.project.Version + 1}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.project.ID}
	raw, err := json.Marshal(v.manifest.Entries())
	if err != nil {
		t.Fatal(err)
	}
	digest, err := v.manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	lock, _ := f.ProjectLock(v.project.ID.String())
	v.base.tx(t, []f.LockRequest{{Key: lock, Mode: f.Exclusive}}, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		if _, e := x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,'delete',$4,'stopping',2,$5::jsonb,$6,clock_timestamp(),clock_timestamp())`, cause.OperationID.String(), scope.ProjectID.String(), v.base.ownerBrowser.actor.Details().UserID, int64(cause.ProjectVersion), raw, digest.String()); e != nil {
			return e
		}
		for _, entry := range v.manifest.Entries() {
			if _, e := x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required','required',1)`, cause.OperationID.String(), string(entry.Name), int64(entry.ContractVersion)); e != nil {
				return e
			}
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle='deleting',version=$2,current_lifecycle_operation_id=$3,updated_at=clock_timestamp() WHERE id=$1 AND version=$4 AND lifecycle='active'`, scope.ProjectID.String(), int64(cause.ProjectVersion), cause.OperationID.String(), int64(v.project.Version))
		if e == nil && tag.RowsAffected() != 1 {
			return f.NewFault(f.InvalidState, f.NotStarted)
		}
		return e
	})
	registration, err := identity.RegisterService(identity.ProjectLifecycle)
	if err != nil {
		t.Fatal(err)
	}
	projectScope, err := identity.InProject(scope.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := registration.Actor(cause.OperationID.String(), projectScope)
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := v.service.RequestStop(ctxFor(t), actor, cause, scope)
	if err == nil && stopped.Details().State != pc.Stopped {
		stopped, err = v.service.InspectStop(ctxFor(t), actor, cause, scope)
	}
	if err != nil || stopped.Details().State != pc.Stopped {
		t.Fatal("actual Skill stop", err)
	}
	operation, err := f.ParseID[objc.ProjectStopOperation](cause.OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	objectCause, err := objc.NewProjectStopCause(objc.ProjectStopCauseDetails{ProjectID: scope.ProjectID, OperationID: operation, Action: objc.ProjectStopDelete, ProjectVersion: cause.ProjectVersion})
	if err != nil {
		t.Fatal(err)
	}
	native, err := v.objects.RequestProjectStop(ctxFor(t), actor, objectCause)
	for round := 0; err == nil && native.Details().State != objc.ProjectStopped && round < 16; round++ {
		native, err = v.objects.InspectProjectStop(ctxFor(t), actor, objectCause)
	}
	if err != nil || !native.Matches(objc.ObjectStopComponent, objectCause) || native.Details().State != objc.ProjectStopped {
		t.Fatal("actual Object stop", err)
	}
	v.base.tx(t, []f.LockRequest{{Key: lock, Mode: f.Exclusive}}, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		if _, e := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped',version=version+1 WHERE operation_id=$1`, cause.OperationID.String()); e != nil {
			return e
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='cleaning',cleanup_stage='domains',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2 AND state='stopping'`, cause.OperationID.String(), scope.ProjectID.String())
		if e == nil && tag.RowsAffected() != 1 {
			return f.NewFault(f.InvalidState, f.NotStarted)
		}
		return e
	})
	return actor, cause, scope
}

func skillInstallationPackage(t *testing.T) skill.Package {
	t.Helper()
	files, err := sc.NewTextFiles([]sc.TextFile{{Path: sc.EntryPath, UTF8Text: "---\nname: Publish guide\ndescription: Owned ordinary installation\n---\n第一段。\nKeep original bytes.\n"}, {Path: "references/note.txt", UTF8Text: "line one\n第二行\n"}})
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := skill.BuildPackage(ctxFor(t), files)
	if err != nil {
		t.Fatal(err)
	}
	return pkg
}

func TestSkillInstallationPackageInput(t *testing.T) {
	pkg := skillInstallationPackage(t)
	if _, err := skill.NewInstallRequest(ctxFor(t), id[pc.Skill](t), pkg); err != nil {
		t.Fatal("fixture package install input", err)
	}
}

func TestSkillInstallationPersistentObject(t *testing.T) {
	pkg := skillInstallationPackage(t)
	v := newSkillInstallationFixture(t)
	actor, projectID := v.base.ownerBrowser.actor, v.project.ID
	expected, err := pkg.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	request, err := skill.NewInstallRequest(ctxFor(t), id[pc.Skill](t), pkg)
	if err != nil {
		t.Fatal(err)
	}
	command := meta(t, id[struct{}](t).String(), nil)
	var receipt skill.InstallReceipt
	var candidate string
	if !t.Run("install-read-replay", func(t *testing.T) {
		receipt, err = v.service.Install(ctxFor(t), actor, command, projectID, request)
		if err != nil || receipt.Validate() != nil || receipt.ProjectID != projectID || receipt.SkillID != request.SkillID() {
			t.Fatal("actual Human Install", err)
		}
		current, e := v.service.GetSkill(ctxFor(t), actor, projectID, receipt.SkillID)
		if e != nil || current.ID != receipt.SkillID || current.Protected || current.CurrentRevision != 1 || current.Version != 1 {
			t.Fatal("ordinary canonical metadata", e)
		}
		body, e := v.service.OpenPackage(ctxFor(t), actor, projectID, receipt.SkillID, 1)
		if e != nil {
			t.Fatal("ordinary Object reader", e)
		}
		got, readErr := io.ReadAll(body)
		closeErr := body.Close()
		if readErr != nil || closeErr != nil || !body.Joined() || !bytes.Equal(got, expected) {
			t.Fatal("original immutable package EOF/Close", readErr, closeErr)
		}
		observed, e := v.service.LookupInstall(ctxFor(t), actor, projectID, command.IdempotencyKey, request)
		if e != nil || observed != receipt {
			t.Fatal("original lookup receipt", e)
		}
		replay, e := v.service.Install(ctxFor(t), actor, command, projectID, request)
		if e != nil || replay != receipt {
			t.Fatal("same-key committed replay", e)
		}
		denied, e := v.service.Install(ctxFor(t), v.base.otherBrowser.actor, meta(t, id[struct{}](t).String(), nil), projectID, request)
		var rejected *f.Fault
		if !errors.As(e, &rejected) || rejected.Code != f.NotFound || denied != (skill.InstallReceipt{}) {
			t.Fatal("foreign current Owner entered installation", e)
		}
		var installations, attempts, revisions, references, uploads, liveWork, leases int
		e = v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_skill.installations WHERE project_id=$1),(SELECT count(*) FROM agenteam_skill.installation_attempts WHERE project_id=$1),(SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1 AND installation_id IS NOT NULL),(SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$2),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action=$3 AND resource_id=$2),(SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1 AND phase<>'joined'),(SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$2 AND state='active')`, projectID.String(), receipt.ObjectID.String(), string(ac.ObjectUploadComplete)).Scan(&installations, &attempts, &revisions, &references, &uploads, &liveWork, &leases)
		if e != nil || installations != 1 || attempts != 1 || revisions != 1 || references != 1 || uploads != 1 || liveWork != 0 || leases != 0 {
			t.Fatal("actual original publication/replay facts", e, installations, attempts, revisions, references, uploads, liveWork, leases)
		}
		if e = v.base.raw.QueryRow(ctxFor(t), `SELECT candidate_key FROM agenteam_object.objects WHERE id=$1`, receipt.ObjectID.String()).Scan(&candidate); e != nil {
			t.Fatal("original native object identity", e)
		}
	}) {
		return
	}
	t.Run("ordinary-cleanup", func(t *testing.T) {
		lifecycle, cause, scope := v.enterCleanup(t)
		var checkpoint *pc.CleanupCheckpoint
		complete := false
		for round := 0; round < 24; round++ {
			report, e := v.service.Cleanup(ctxFor(t), lifecycle, cause, scope, checkpoint)
			if e != nil {
				t.Fatal("actual bounded Skill cleanup", e)
			}
			if report.Details().State == pc.CleanupCompleted {
				complete = true
				break
			}
			checkpoint = report.Details().Checkpoint
		}
		if !complete {
			t.Fatal("bounded fixture cleanup did not complete")
		}
		var local, native, deleted int
		e := v.base.raw.QueryRow(ctxFor(t), `SELECT (SELECT count(*) FROM agenteam_skill.installations WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.installation_attempts WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.initializations WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.skills WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.revisions WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.work WHERE project_id=$1)+(SELECT count(*) FROM agenteam_skill.cleanup WHERE project_id=$1),(SELECT count(*) FROM agenteam_object.objects WHERE id=$2)+(SELECT count(*) FROM agenteam_object.object_references WHERE object_id=$2)+(SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$2)+(SELECT count(*) FROM agenteam_object.uploads WHERE object_id=$2)+(SELECT count(*) FROM agenteam_object.upload_attempts WHERE object_id=$2)+(SELECT count(*) FROM agenteam_object.cleanup_operations WHERE object_id=$2)+(SELECT count(*) FROM agenteam_object.project_work WHERE object_id=$2)+(SELECT count(*) FROM agenteam_object.object_transfers WHERE object_id=$2),(SELECT count(*) FROM agenteam_audit.audit_records WHERE project_id=$1 AND action='object.delete' AND resource_id=$2)`, projectID.String(), receipt.ObjectID.String()).Scan(&local, &native, &deleted)
		if e != nil || local != 0 || native != 0 || deleted != 1 {
			t.Fatal("real final domain anchors/audit", e, local, native, deleted)
		}
		if _, e = v.s3.StatObject(ctxFor(t), v.bucket, candidate, minio.StatObjectOptions{}); minio.ToErrorResponse(e).Code != "NoSuchKey" {
			t.Fatal("original native object was not deleted")
		}
		again, e := v.service.Cleanup(ctxFor(t), lifecycle, cause, scope, checkpoint)
		if e != nil || again.Details().State != pc.CleanupCompleted {
			t.Fatal("original completed cleanup replay", e)
		}
	})
}
