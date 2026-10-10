//go:build integration

package skill_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

// No SQL or CommitResult is fabricated. The optional hook runs after the real
// product callback in that original live Tx, before the real Store commits.
// It may inspect or reject an exact boundary; no hook grants authorization.
type skillCleanupStore struct {
	*postgres.Store
	mu    sync.RWMutex
	after func(context.Context, f.Tx) error
}

func (s *skillCleanupStore) setAfter(hook func(context.Context, f.Tx) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.after = hook
}
func (s *skillCleanupStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	return s.Store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := callback(ctx, tx); err != nil {
			return err
		}
		d := cause.Details()
		if d.Kind != f.RecoveryCause || d.Owner != "skill-cleanup" {
			return nil
		}
		s.mu.RLock()
		hook := s.after
		s.mu.RUnlock()
		if hook != nil {
			return hook(ctx, tx)
		}
		return nil
	})
}

type skillCleanupFixture struct {
	*skillObjectFixture
	store    *skillCleanupStore
	manifest pc.RequiredManifest
	s3       *minio.Client
	bucket   string
}

// All cleanup/auth/SQL/Object/backend/Audit methods are actual implementations
// in one Store. Project creation, ready and later lifecycle stage records are
// explicit upstream fixtures; no Project.Create/BeginDelete/root claim follows.
// Existing publication tests stay byte-identical and retain their unbound ports.
func newSkillCleanupFixture(t *testing.T, p *skillPG, configure ...func(map[string]string)) *skillCleanupFixture {
	t.Helper()
	if p == nil {
		p = newSkillPG(t)
	}
	v := p.newCase(t)
	store := &skillCleanupStore{Store: p.store}
	manifest := skillStopManifest(t)
	lifecycle, e := project.NewLifecycleAuthority(store, manifest, nil)
	if e != nil {
		t.Fatal(e)
	}
	v.authority, e = skill.NewAuthority(store, skillStopProjects{p.projects, lifecycle})
	if e != nil {
		t.Fatal(e)
	}
	checker, e := object.NewProjectAuditAuthority(store)
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
	cleanupProjects, e := skill.NewLifecycleAuditAuthority(projects, v.authority, facts)
	if e != nil {
		t.Fatal(e)
	}
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	keys, e := cursor.LoadKeyring(fmt.Sprintf(`{"format":1,"current_kid":"c","keys":[{"kid":"c","key_b64":%q}]}`, encoded))
	if e != nil {
		t.Fatal(e)
	}
	auditing, e := audit.New(store, keys, audit.Authorizations{Projects: cleanupProjects})
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
	bucket := "skill-cleanup-" + remote.Nonce[:12] + "-" + suffix
	if e = s3.MakeBucket(testContext(t), bucket, minio.MakeBucketOptions{Region: "us-east-1"}); e != nil {
		t.Fatal("owned bucket creation failed")
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	// Optional test-owned transport stimulus; authority, Store, native Object
	// service and every original fixture call remain unchanged.
	for _, apply := range configure {
		apply(values)
	}
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
	// Real Skills CleanupAuthority and real Project Stop/Current Cleaning gates.
	// Runtime remains nil; this fixture cannot confirm a foreign process death.
	objects, e := object.New(store, backend, spool, auditing, object.Authorizations{Planner: v.authority, Resources: v.authority, Read: v.authority, Gate: v.authority, Cleanup: v.authority, Processes: guard, ProjectStop: lifecycle})
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
	x := &skillObjectFixture{pg: p, seed: v, objects: objects, guard: guard, facts: facts, project: cleanupProjects, bundle: bundle, process: process}
	x.service = x.newService(t)
	return &skillCleanupFixture{skillObjectFixture: x, store: store, manifest: manifest, s3: s3, bucket: bucket}
}

// The Project is already initialized by the explicit ready prerequisite; this
// fixture records an accepted Delete, not an invocation of BeginDelete.
func seedCleanupStop(t *testing.T, p *skillPG, v *skillCase, manifest pc.RequiredManifest, action pc.LifecycleAction) (id.Actor, pc.LifecycleCause, pc.ScopeRef) {
	t.Helper()
	cause := pc.LifecycleCause{OperationID: testID[pc.Operation](t), Action: action, ProjectVersion: 2}
	scope := pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.request.ProjectID}
	raw, e := json.Marshal(manifest.Entries())
	if e != nil {
		t.Fatal(e)
	}
	digest, e := manifest.Digest()
	if e != nil {
		t.Fatal(e)
	}
	gate, cleanup := "archiving", "not_applicable"
	if action == pc.Delete {
		gate, cleanup = "deleting", "required"
	}
	result := p.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
		key, _ := f.ProjectLock(scope.ProjectID.String())
		if e := p.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); e != nil {
			return e
		}
		x, e := p.store.InTx(tx)
		if e != nil {
			return e
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,$4,2,'stopping',2,$5::jsonb,$6,clock_timestamp(),clock_timestamp())`, cause.OperationID.String(), scope.ProjectID.String(), v.owner.String(), string(action), raw, digest.String()); e != nil {
			return e
		}
		for _, entry := range manifest.Entries() {
			if _, e = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required',$4,1)`, cause.OperationID.String(), string(entry.Name), int64(entry.ContractVersion), cleanup); e != nil {
				return e
			}
		}
		_, e = x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle=$2,version=2,current_lifecycle_operation_id=$3,updated_at=clock_timestamp() WHERE id=$1`, scope.ProjectID.String(), gate, cause.OperationID.String())
		return e
	})
	requireCommitted(t, result)
	registration, _ := id.RegisterService(id.ProjectLifecycle)
	actorScope, _ := id.InProject(scope.ProjectID)
	actor, e := registration.Actor(cause.OperationID.String(), actorScope)
	if e != nil {
		t.Fatal(e)
	}
	return actor, cause, scope
}

func (x *skillCleanupFixture) enterCleaning(t *testing.T, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) {
	t.Helper()
	report, err := x.service.RequestStop(testContext(t), actor, cause, scope)
	if err != nil {
		t.Fatal("actual Skills RequestStop", err)
	}
	if report.Details().State != pc.Stopped {
		report, err = x.service.InspectStop(testContext(t), actor, cause, scope)
	}
	if err != nil || report.Details().State != pc.Stopped {
		t.Fatal("actual Skills stop/join", err)
	}
	operation, err := f.ParseID[oc.ProjectStopOperation](cause.OperationID.String())
	if err != nil {
		t.Fatal(err)
	}
	objectCause, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: scope.ProjectID, OperationID: operation, Action: oc.ProjectStopDelete, ProjectVersion: cause.ProjectVersion})
	if err != nil {
		t.Fatal(err)
	}
	native, err := x.objects.RequestProjectStop(testContext(t), actor, objectCause)
	for round := 0; err == nil && native.Details().State != oc.ProjectStopped && round < 16; round++ {
		native, err = x.objects.InspectProjectStop(testContext(t), actor, objectCause)
	}
	if err != nil || !native.Matches(oc.ObjectStopComponent, objectCause) || native.Details().State != oc.ProjectStopped {
		t.Fatal("actual Object stop/join", err)
	}
	// Only these two local providers ran. Other domain participant stop facts and
	// the scheduler's transition are explicit upstream fixtures, not fake joins.
	result := x.pg.store.WithinTx(testContext(t), testCause(t), func(ctx context.Context, tx f.Tx) error {
		key, _ := f.ProjectLock(scope.ProjectID.String())
		if err := x.pg.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		q, err := x.pg.store.InTx(tx)
		if err != nil {
			return err
		}
		if _, err = q.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped',version=version+1 WHERE operation_id=$1`, cause.OperationID.String()); err != nil {
			return err
		}
		tag, err := q.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='cleaning',cleanup_stage='domains',version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2 AND state='stopping'`, cause.OperationID.String(), scope.ProjectID.String())
		if err == nil && tag.RowsAffected() != 1 {
			return f.NewFault(f.InvalidState, f.NotStarted)
		}
		return err
	})
	requireCommitted(t, result)
}
