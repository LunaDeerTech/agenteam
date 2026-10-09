//go:build integration

package skill_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

// This forwards the original context, transaction and typed inputs to the real
// private witness checker. Counting calls cannot grant or replace permission.
type observedSkillObjectFacts struct {
	real  *object.ProjectAuditAuthority
	calls atomic.Int64
}

func (o *observedSkillObjectFacts) CheckProjectAuditInTx(ctx context.Context, tx f.Tx, entry ac.Entry, key ac.AppendKey) error {
	o.calls.Add(1)
	return o.real.CheckProjectAuditInTx(ctx, tx, entry, key)
}

type skillObjectFixture struct {
	pg      *skillPG
	seed    *skillCase
	service *skill.Service
	objects *object.Service
	guard   *object.ProcessGuard
	facts   *observedSkillObjectFacts
	project ac.ProjectAuthority
	bundle  skill.BuiltinBundle
	process oc.ProcessID
}

// All Skill/Object/Audit implementations and the private Object witness are
// real and share the same Store. Only upstream Project/Creation and Human
// account facts are seeds. No Runtime, production root, lifecycle participant,
// permissive authority, or fake successful Object operation is installed.
func newSkillObjectFixture(t *testing.T) *skillObjectFixture {
	t.Helper()
	p := newSkillPG(t)
	v := p.newCase(t)
	checker, e := object.NewProjectAuditAuthority(p.store)
	if e != nil {
		t.Fatal(e)
	}
	facts := &observedSkillObjectFacts{real: checker}
	mapping, e := skill.NewInitializationAuditFacts(v.authority, facts)
	if e != nil {
		t.Fatal(e)
	}
	projects, e := project.NewInitializationAuditAuthority(p.projects, mapping)
	if e != nil {
		t.Fatal(e)
	}
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	keys, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, encoded))
	if e != nil {
		t.Fatal(e)
	}
	auditing, e := audit.New(p.store, keys, audit.Authorizations{Projects: projects})
	if e != nil {
		t.Fatal(e)
	}
	remote, e := objectfixture.Load()
	if e != nil {
		t.Fatal("owned Object fixture required")
	}
	s3, transport, e := remote.Client()
	if e != nil {
		t.Fatal("owned Object client unavailable")
	}
	t.Cleanup(transport.CloseIdleConnections)
	suffix, e := pgfixture.RandomHex(8)
	if e != nil {
		t.Fatal(e)
	}
	bucket := "d05-" + remote.Nonce[:12] + "-" + suffix
	if e = s3.MakeBucket(testContext(t), bucket, minio.MakeBucketOptions{Region: "us-east-1"}); e != nil {
		t.Fatal("owned bucket creation failed")
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	config, e := object.LoadStorageConfig(func(name string) (string, bool) {
		value, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return value, ok
	})
	if e != nil {
		t.Fatal("owned Object configuration invalid")
	}
	backend, e := object.NewBackend(config)
	if e != nil {
		t.Fatal("owned Object backend unavailable")
	}
	process := testID[oc.Process](t)
	spool, e := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), process)
	if e != nil {
		_ = backend.Close()
		t.Fatal(e)
	}
	guard, e := object.OpenProcessGuard(spool, process)
	if e != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(e)
	}
	// Unbound guard resources may be closed here. We do not attach a Runtime or
	// claim ConfirmStopped for a prior process; those recovery paths remain shut.
	t.Cleanup(func() {
		if e := guard.Close(); e != nil {
			t.Error("constructor guard close", e)
		}
	})
	// Cleanup stays explicitly unbound: this top does not grant irreversible
	// Project deletion or foreign-process recovery through a permissive adapter.
	objects, e := object.New(p.store, backend, spool, auditing, object.Authorizations{Planner: v.authority, Resources: v.authority, Read: v.authority, Gate: v.authority, Processes: guard})
	if e != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(e)
	}
	t.Cleanup(func() {
		objects.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := objects.Drain(ctx); e != nil {
			_ = objects.Force(ctx)
			t.Error("Object actual drain", e)
		}
	})
	if e = objects.Initialize(testContext(t)); e != nil {
		t.Fatal("Object initialization", e)
	}
	bundle, e := skill.AddSkills(testContext(t))
	if e != nil {
		t.Fatal(e)
	}
	x := &skillObjectFixture{pg: p, seed: v, objects: objects, guard: guard, facts: facts, project: projects, bundle: bundle, process: process}
	x.service = x.newService(t)
	return x
}

func (x *skillObjectFixture) newService(t *testing.T) *skill.Service {
	t.Helper()
	s, e := skill.New(skill.Dependencies{Authority: x.seed.authority, Objects: x.objects, Processes: x.guard, ProcessID: x.process, Bundle: x.bundle})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if e := s.Drain(ctx); e != nil || !s.Joined() {
			t.Error("Skill actual drain", e)
		}
	})
	return s
}

func TestSkillObjectInitializationPublication(t *testing.T) {
	x := newSkillObjectFixture(t)
	v, p := x.seed, x.pg
	complete, e := x.service.InitializeProjectSkills(testContext(t), v.actor, v.request)
	if e != nil || complete.State != pc.InitializationCompleted || !complete.Matches(v.request) {
		t.Fatal("real Object-backed Skill publication", e)
	}
	skillID := *complete.AddSkillsID
	var objectID, uploadID, attemptID, revisionID, digest, media, initiator string
	var size int64
	var canonical, published, audits, work, live int
	check := func(t *testing.T) {
		t.Helper()
		e := p.store.QueryRow(testContext(t), `SELECT i.object_id::text,i.upload_id::text,i.current_attempt_id::text,i.revision_id::text,encode(o.sha256,'hex'),o.media_type,o.byte_size,u.initiator_id::text,
 (SELECT count(*) FROM agenteam_object.object_references r WHERE r.object_id=o.id AND r.owner_kind='skill_revision' AND r.owner_id=i.revision_id AND r.kind='canonical'),
 (SELECT count(*) FROM agenteam_object.uploads q WHERE q.id=i.upload_id AND q.state='committed' AND q.disposition='attached' AND q.initiator_kind='service' AND q.initiator_execution_id IS NULL),
 (SELECT count(*) FROM agenteam_audit.audit_records a WHERE a.project_id=i.project_id AND a.action='object.upload.complete' AND a.resource_id=o.id),
 (SELECT count(*) FROM agenteam_skill.work w WHERE w.project_id=i.project_id),
 (SELECT count(*) FROM agenteam_skill.work w WHERE w.project_id=i.project_id AND w.phase<>'joined')
 FROM agenteam_skill.initializations i JOIN agenteam_skill.skills s ON s.id=i.skill_id JOIN agenteam_skill.revisions r ON r.id=i.revision_id
 JOIN agenteam_object.objects o ON o.id=i.object_id JOIN agenteam_object.uploads u ON u.id=i.upload_id
 WHERE i.project_id=$1 AND i.phase='published' AND s.protected AND s.serving AND s.current_revision=1 AND r.object_id=o.id AND o.state='available'`, v.request.ProjectID.String()).Scan(&objectID, &uploadID, &attemptID, &revisionID, &digest, &media, &size, &initiator, &canonical, &published, &audits, &work, &live)
		if e != nil || "sha256:"+digest != string(skill.AddSkillsPackageSHA256) || media != sc.PackageMediaType || size <= 0 || initiator != v.request.CreationID.String() || canonical != 1 || published != 1 || audits != 1 || live != 0 {
			t.Fatal("real canonical Object/Skill/Audit relation", e)
		}
	}
	t.Run("publication_private_witness_and_replay", func(t *testing.T) {
		check(t)
		if work != 1 || x.facts.calls.Load() != 1 || len(v.objects.steps) != 0 {
			t.Fatal("publication bypassed exact witness or used controlled Object")
		}
		originalObject, originalUpload, originalAttempt := objectID, uploadID, attemptID
		fresh := x.newService(t)
		observed, e := fresh.InspectProjectSkills(testContext(t), v.actor, v.request)
		if e != nil || observed.State != pc.InitializationCompleted || *observed.AddSkillsID != skillID {
			t.Fatal("fresh service durable inspection", e)
		}
		replay, e := fresh.InitializeProjectSkills(testContext(t), v.actor, v.request)
		if e != nil || replay.State != pc.InitializationCompleted || *replay.AddSkillsID != skillID {
			t.Fatal("original initialization replay", e)
		}
		check(t)
		if objectID != originalObject || uploadID != originalUpload || attemptID != originalAttempt || work != 1 || x.facts.calls.Load() != 1 {
			t.Fatal("replay replaced original object or repeated publication")
		}
	})
	initializeReaderAccountKeys(t, p)
	owner := seedReaderHuman(t, p, v.owner, "object-skill-owner", "user")
	seedReaderReadyProject(t, p, v, skillID)
	t.Run("real_package_eof_and_close", func(t *testing.T) {
		body, e := x.service.OpenPackage(testContext(t), owner, v.request.ProjectID, skillID, 1)
		if e != nil {
			t.Fatal("open real immutable package", e)
		}
		t.Cleanup(func() { _ = body.Close() })
		content, e := x.bundle.Package()
		if e != nil {
			t.Fatal(e)
		}
		want, e := content.Bytes()
		if e != nil {
			t.Fatal(e)
		}
		got, readErr := io.ReadAll(body)
		if readErr != nil || !bytes.Equal(got, want) || body.Joined() {
			t.Fatal("real package EOF bytes or premature local join", readErr)
		}
		if e = body.Close(); e != nil || !body.Joined() {
			t.Fatal("real package Close/join", e)
		}
		var activeReaders int
		e = p.store.QueryRow(testContext(t), `SELECT count(*) FROM agenteam_object.object_leases WHERE object_id=$1 AND owner_kind='reader' AND state='active'`, objectID).Scan(&activeReaders)
		if e != nil || activeReaders != 0 {
			t.Fatal("Object reader lease remained active", e)
		}
		check(t)
		if work != 2 {
			t.Fatal("original reader work was not retained and joined")
		}
	})
	t.Run("exact_public_fields_cannot_forge_private_witness", func(t *testing.T) {
		obj, e := f.ParseID[oc.StoredObject](objectID)
		if e != nil {
			t.Fatal(e)
		}
		key, e := ac.NewAppendKey(ac.ObjectProducer, attemptID, 0)
		if e != nil {
			t.Fatal(e)
		}
		scope, _ := id.InProject(v.request.ProjectID)
		registration, _ := id.RegisterService(id.ObjectService)
		actor, e := registration.Actor(attemptID, scope)
		if e != nil {
			t.Fatal(e)
		}
		meta, e := ac.ObjectMetadata(ac.ObjectUploadFailed, ac.ObjectMetadataFields{ObjectID: objectID, InitiatorKind: id.Service, InitiatorID: v.request.CreationID.String(), MediaType: sc.PackageMediaType, ByteSize: f.Progress(size), Phase: ac.FailedPhase, Reason: ac.StorageUnavailable})
		if e != nil {
			t.Fatal(e)
		}
		resource, _ := ac.NewResource(ac.ObjectResource, objectID)
		entry, e := ac.NewEntry(ac.EntryFields{Scope: scope, Actor: actor, Action: ac.ObjectUploadFailed, Outcome: ac.Unknown, Resource: resource, Metadata: meta})
		if e != nil {
			t.Fatal(e)
		}
		command, _ := f.NewCommandIdentity("skills", []string{v.request.ProjectID.String()}, "initialize", v.request.InitializationKey)
		commandKey, _ := f.CommandLock(command)
		projectKey, _ := f.ProjectLock(v.request.ProjectID.String())
		skillKey, _ := f.AggregateLock(f.SkillAggregate, skillID.String())
		objectKey, _ := f.AggregateLock(f.ObjectAggregate, obj.String())
		before := x.facts.calls.Load()
		var denied error
		r := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
			if e := p.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: commandKey, Mode: f.Exclusive}, {Key: projectKey, Mode: f.Exclusive}, {Key: skillKey, Mode: f.Exclusive}, {Key: objectKey, Mode: f.Exclusive}}); e != nil {
				return e
			}
			denied = x.project.CheckAppendInTx(ctx, tx, entry, key)
			return denied
		})
		var known *f.Fault
		if r.State() != f.NotCommitted || !errors.As(denied, &known) || known.Code != f.Forbidden || x.facts.calls.Load() != before+1 {
			t.Fatal("public metadata bypassed real private witness, or denial never reached checker")
		}
	})
}
