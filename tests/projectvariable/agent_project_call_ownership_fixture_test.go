//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	assembly "github.com/LunaDeerTech/agenteam/tests/testsupport/agentconfiguration"
	objectfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/objectstore"
	pgfixture "github.com/LunaDeerTech/agenteam/tests/testsupport/postgres"
	"github.com/minio/minio-go/v7"
)

type agentCallOwnershipFixture struct {
	*agentProjectLifecycleFixture
	graph *assembly.Assembly
	pair  *agent.ProjectCallOwnership
}

// The database already has a confirmed identity in the real installation
// bucket. Reuse that exact task-owned bucket; never initialize a new empty
// bucket against the confirmed database or create/copy its control marker.
func agentOwnershipStorage(bucket string) (object.StorageConfig, error) {
	remote, err := objectfixture.Load()
	if err != nil {
		return object.StorageConfig{}, err
	}
	if len(remote.Nonce) < 12 {
		return object.StorageConfig{}, errors.New("invalid owned Object nonce")
	}
	prefix := "skill-install-" + remote.Nonce[:12] + "-"
	suffix := strings.TrimPrefix(bucket, prefix)
	decoded, err := hex.DecodeString(suffix)
	if !strings.HasPrefix(bucket, prefix) || err != nil || len(decoded) != 8 || hex.EncodeToString(decoded) != suffix {
		return object.StorageConfig{}, errors.New("bucket is outside the original owned installation")
	}
	values := map[string]string{"ENDPOINT": remote.Endpoint(), "BUCKET": bucket, "ACCESS_KEY": remote.AccessKey, "SECRET_KEY": remote.SecretKey, "TLS_MODE": "verify-full", "CA_FILE": remote.CAFile}
	return object.LoadStorageConfig(func(name string) (string, bool) {
		value, ok := values[strings.TrimPrefix(name, object.EnvironmentPrefix)]
		return value, ok
	})
}

func newAgentCallOwnershipFixture(t *testing.T) *agentCallOwnershipFixture {
	t.Helper()
	var ownedPair *agent.ProjectCallOwnership
	allowRuntimeClose := func() bool { return ownedPair == nil || ownedPair.Joined() }
	var installer *toolruntime.InstallAuthority
	var processGuard *object.ProcessGuard
	var process objc.ProcessID
	var installerRetired sync.Once
	retireInstaller := func() {
		installerRetired.Do(func() {
			installer.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := installer.Drain(ctx); err != nil || !installer.Joined() {
				t.Error("actual InstallAuthority did not retire", err)
			}
		})
	}
	p2 := newAgentOwnershipInstallationFixture(t, allowRuntimeClose, func(store *hookStore, projects *project.Authority, guard *object.ProcessGuard, p objc.ProcessID) (*skill.Authority, error) {
		process = p
		processGuard = guard
		var err error
		installer, err = toolruntime.NewInstallAuthority(store, guard, p)
		if err != nil {
			return nil, err
		}
		// Construction-failure fallback; the successful composition below
		// retires this borrower before Runtime releases its original guard.
		t.Cleanup(retireInstaller)
		return skill.NewAuthorityWithInstallExecution(store, projects, installer)
	})
	t.Cleanup(retireInstaller)
	source, err := toolruntime.NewInstallSource(installer, p2.service)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := project.NewLifecycleAuthority(p2.base.tracked, p2.manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := assembly.New(p2.base.tracked, assembly.Options{Accounts: p2.base.accounts, Lifecycle: lifecycle, BuiltinSources: map[string]registry.BuiltinSource{builtin.SkillInstallStableKey: source}, InstallSourceKey: builtin.SkillInstallStableKey})
	if err != nil {
		t.Fatal(err)
	}
	v := &agentCreateFixture{p2: p2, guard: processGuard, providers: graph.Providers()}
	// Registration traverses the actual bound guard and exactly the service
	// constructed with this InstallAuthority, in the original Registry EX Tx.
	p2.base.tx(t, []f.LockRequest{registry.RegistryLock(f.Exclusive)}, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor) error {
		if err := p2.service.RequireInstallBindingInTx(ctx, tx, installer, process); err != nil {
			return err
		}
		registration, err := source.DescribeInTx(ctx, tx)
		if err != nil {
			return err
		}
		if err = source.CheckBindingInTx(ctx, tx, registration); err != nil {
			return err
		}
		v.tool, err = v.providers.Registry.ReconcileBuiltinInTx(ctx, tx, builtin.SkillInstallStableKey)
		return err
	})
	outProcess, err := f.ParseID[oc.Process](process.String())
	if err != nil {
		t.Fatal(err)
	}
	v.modelID = v.createModel(t, fixtureProcess{outProcess})
	pair, err := graph.NewProjectCallOwnership(p2.base.keys, &phaseGuardProcesses{guard: processGuard, process: process}, processGuard, process)
	if err != nil {
		t.Fatal(err)
	}
	ownedPair = pair
	v.agents = pair.Service()
	t.Cleanup(func() { drainAgentCallPair(t, pair) })
	return &agentCallOwnershipFixture{agentProjectLifecycleFixture: &agentProjectLifecycleFixture{agentCreateFixture: v, directory: pair.DirectoryReader(), lifecycle: lifecycle, stopper: pair.LocalStopper(), process: outProcess}, graph: graph, pair: pair}
}

// Failed bounded cleanup retains the actual pair and its concrete guard for
// the remaining native process lifetime; os.File finalization cannot unlock
// an otherwise unreachable unresolved owner. This is a failure-only hold,
// never a substitute for Joined or a retry worker.
var agentOwnershipFailedCleanup struct {
	sync.Mutex
	pairs    []*agent.ProjectCallOwnership
	runtimes []*agentOwnershipRuntime
}

// Both foreign instances recover an existing Skill/Object database. The empty
// phase-guard fixture cannot authorize that recovery. This graph uses the same
// concrete authorities and construction order as the installation prerequisite.
type agentOwnershipRuntime struct {
	backend   *object.Backend
	spool     *object.Spool
	guard     *object.ProcessGuard
	installer *toolruntime.InstallAuthority
	authority *skill.Authority
	objects   *object.Service
	runtime   *object.Runtime
	skills    *skill.Service
	pair      *agent.ProjectCallOwnership
}

func agentOwnershipManifest() (pc.RequiredManifest, error) {
	return pc.NewRequiredManifest([]pc.ParticipantRegistration{
		{Name: pc.SkillsParticipant, ContractVersion: 1, OwnerModule: "skills", ReferenceKinds: []pc.ReferenceKind{"skill-initialization", "skill-installation", "skill-package-reader", "skill-object-cleanup"}},
		{Name: pc.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []pc.ReferenceKind{"object"}, CleanupAfter: []pc.ParticipantName{pc.SkillsParticipant}},
		{Name: pc.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []pc.ReferenceKind{"secret"}, CleanupAfter: []pc.ParticipantName{pc.SkillsParticipant}},
		{Name: pc.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", CleanupAfter: []pc.ParticipantName{pc.ArtifactObjectParticipant, pc.SecretParticipant, pc.SkillsParticipant}},
		{Name: pc.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", CleanupAfter: []pc.ParticipantName{pc.OutboxParticipant}},
	})
}

// Even an error returns the partial owner; the caller immediately installs its
// cleanup before inspecting err. Construction never binds a guard or skips the
// real Runtime.Initialize recovery and storage probes.
func newAgentOwnershipRuntime(store *hookStore, accounts *account.Authority, keys cursor.Keyring, storage object.StorageConfig, path string, process objc.ProcessID) (*agentOwnershipRuntime, error) {
	r := &agentOwnershipRuntime{}
	manifest, err := agentOwnershipManifest()
	if err != nil {
		return r, err
	}
	lifecycle, err := project.NewLifecycleAuthority(store, manifest, nil)
	if err != nil {
		return r, err
	}
	facts, err := object.NewProjectAuditAuthority(store)
	if err != nil {
		return r, err
	}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: accounts, Routes: accounts, Lifecycle: lifecycle, AuditFacts: map[ac.Producer]ac.ProjectFactAuthority{ac.ObjectProducer: facts}})
	if err != nil {
		return r, err
	}
	r.backend, err = object.NewBackend(storage)
	if err != nil {
		return r, err
	}
	r.spool, err = object.OpenSpool(path, process)
	if err != nil {
		return r, err
	}
	r.guard, err = object.OpenProcessGuard(r.spool, process)
	if err != nil {
		return r, err
	}
	// The real constructor permits an unbound guard here; actual dispatch and
	// Registry binding still require this exact process after initialization.
	r.installer, err = toolruntime.NewInstallAuthority(store, r.guard, process)
	if err != nil {
		return r, err
	}
	r.authority, err = skill.NewAuthorityWithInstallExecution(store, projects, r.installer)
	if err != nil {
		return r, err
	}
	initialFacts, err := skill.NewInitializationAuditFacts(r.authority, facts)
	if err != nil {
		return r, err
	}
	initialAudit, err := project.NewInitializationAuditAuthority(projects, initialFacts)
	if err != nil {
		return r, err
	}
	cleanupAudit, err := skill.NewLifecycleAuditAuthority(initialAudit, r.authority, facts)
	if err != nil {
		return r, err
	}
	auditor, err := audit.New(store, keys, audit.Authorizations{Sessions: accounts, System: accounts, Accounts: accounts, Projects: cleanupAudit})
	if err != nil {
		return r, err
	}
	r.objects, err = object.New(store, r.backend, r.spool, auditor, object.Authorizations{Planner: r.authority, Resources: r.authority, Read: r.authority, Gate: r.authority, Cleanup: r.authority, Processes: r.guard, ProjectStop: lifecycle})
	if err != nil {
		return r, err
	}
	endpoint, err := object.LoadTransferEndpoint(func(string) (string, bool) { return "", false }, storage)
	if err != nil {
		return r, err
	}
	transfers, err := object.NewTransferService(r.objects, nil, endpoint)
	if err != nil {
		return r, err
	}
	r.runtime, err = object.NewRuntime(r.objects, r.guard, transfers)
	return r, err
}

func (r *agentOwnershipRuntime) retain() {
	if r == nil {
		return
	}
	agentOwnershipFailedCleanup.Lock()
	agentOwnershipFailedCleanup.runtimes = append(agentOwnershipFailedCleanup.runtimes, r)
	agentOwnershipFailedCleanup.Unlock()
}

func (r *agentOwnershipRuntime) drain(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if r.pair != nil {
		r.pair.Stop()
		if err := r.pair.Drain(ctx); err != nil {
			return err
		}
		if !r.pair.Joined() {
			return f.NewFault(f.ResourceBusy, f.NotStarted)
		}
	}
	if r.skills != nil {
		if err := r.skills.Drain(ctx); err != nil {
			return err
		}
		if !r.skills.Joined() {
			return f.NewFault(f.ResourceBusy, f.NotStarted)
		}
	}
	if r.installer != nil {
		r.installer.Stop()
		if err := r.installer.Drain(ctx); err != nil {
			return err
		}
		if !r.installer.Joined() {
			return f.NewFault(f.ResourceBusy, f.NotStarted)
		}
	}
	if r.runtime != nil {
		r.runtime.StopAdmission()
		return r.runtime.Drain(ctx) // Actual service return precedes guard.finish.
	}
	if r.objects != nil {
		r.objects.StopAdmission()
		if err := r.objects.Drain(ctx); err != nil {
			return err
		}
	}
	if r.guard != nil {
		if err := r.guard.Close(); err != nil {
			return err
		}
	}
	if r.objects == nil { // Otherwise Service.Drain owns these physical closes.
		if r.spool != nil {
			if err := r.spool.Close(); err != nil {
				return err
			}
		}
		if r.backend != nil {
			return r.backend.Close()
		}
	}
	return nil
}

func drainAgentCallPair(t *testing.T, pair *agent.ProjectCallOwnership) bool {
	t.Helper()
	pair.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := pair.Drain(ctx); err != nil || !pair.Joined() {
		t.Error("owned Agent pair did not join", err)
		agentOwnershipFailedCleanup.Lock()
		agentOwnershipFailedCleanup.pairs = append(agentOwnershipFailedCleanup.pairs, pair)
		agentOwnershipFailedCleanup.Unlock()
		return false
	}
	return true
}

// The first AcquireAll is durable registration, the second is the real
// business transaction. Hold only the latter, after actual BEGIN but before
// acquiring its original lock union; registration is never faked.
func (v *agentCallOwnershipFixture) installOwnershipBarrier(t *testing.T) {
	t.Helper()
	s := v.p2.base.tracked
	var mu sync.Mutex
	rounds := make(map[*agentProjectCall]int)
	s.mu.Lock()
	s.beforeLocks = func(ctx context.Context, tx f.Tx, _ []f.LockRequest) error {
		call, _ := ctx.Value(agentProjectCallKey{}).(*agentProjectCall)
		if call == nil {
			return nil
		}
		mu.Lock()
		rounds[call]++
		n := rounds[call]
		mu.Unlock()
		if n != 2 {
			return nil
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&call.pid); err != nil {
			return err
		}
		if call.pid <= 0 {
			return errors.New("owned original BEGIN PID missing")
		}
		call.ctx = ctx
		close(call.entered)
		<-call.release.done
		return nil
	}
	s.mu.Unlock()
	t.Cleanup(func() { s.mu.Lock(); s.beforeLocks = nil; s.mu.Unlock() })
}

func (v *agentCallOwnershipFixture) registeredState(t *testing.T, process objc.ProcessID, phase string, want int) {
	t.Helper()
	var count int
	if err := v.p2.base.raw.QueryRow(ctxFor(t), `SELECT count(*) FROM agenteam_agent.project_calls WHERE project_id=$1 AND process_id=$2 AND phase=$3`, v.p2.project.ID.String(), process.String(), phase).Scan(&count); err != nil || count != want {
		t.Fatal("exact Agent call phase/count", err, count, want)
	}
}

// This driver executes the actual local provider but leaves aggregate
// participants required. The returned actor is the driver's original actor.
func (v *agentCallOwnershipFixture) enterStopping(t *testing.T, cause pc.LifecycleCause) (i.Actor, pc.ScopeRef) {
	t.Helper()
	var actor i.Actor
	var scope pc.ScopeRef
	var steps atomic.Int32
	driver, err := project.NewLifecycleStopDriver(v.p2.base.tracked, v.lifecycle, &phaseGuardProcesses{guard: v.guard, process: mustAgentGuardProcess(t, v.guard)}, func(ctx context.Context, a i.Actor, c pc.LifecycleCause, s pc.ScopeRef) error {
		if c != cause || s.ProjectID != v.p2.project.ID {
			return errors.New("ownership driver cause mismatch")
		}
		report, err := v.pair.LocalStopper().RequestStop(ctx, a, c, s)
		if err != nil {
			return err
		}
		if !report.Matches(c, s) {
			return errors.New("actual local provider returned wrong cause")
		}
		actor, scope = a, s
		steps.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := driver.Drain(ctx); err != nil {
			t.Error(err)
		}
	}()
	if err = driver.Run(ctxFor(t), v.p2.project.ID, cause.OperationID); err != nil || steps.Load() != 1 {
		t.Fatal("original stopping driver", err)
	}
	v.requirePhase(t, cause, "stopping", "terminal")
	return actor, scope
}
func mustAgentGuardProcess(t *testing.T, g *object.ProcessGuard) objc.ProcessID {
	t.Helper()
	p, err := g.CurrentProcess()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func agentOwnershipLockWait(t *testing.T, raw *postgres.Store, key f.LockKey, holder, waiter int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	bits := uint64(key.AdvisoryKey())
	for {
		var blocked bool
		err := raw.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks w JOIN pg_locks h ON h.locktype=w.locktype AND h.database=w.database AND h.classid=w.classid AND h.objid=w.objid AND h.objsubid=w.objsubid JOIN pg_stat_activity a ON a.pid=w.pid WHERE a.datname=current_database() AND w.locktype='advisory' AND w.classid::bigint=$1 AND w.objid::bigint=$2 AND w.objsubid=1 AND w.pid=$3 AND NOT w.granted AND w.mode='ExclusiveLock' AND h.pid=$4 AND h.granted AND h.mode='ShareLock' AND $4::int=ANY(pg_blocking_pids(w.pid)))`, int64(bits>>32), int64(bits&0xffffffff), waiter, holder).Scan(&blocked)
		if err != nil {
			t.Fatal("exact original Project lock observation", err)
		}
		if blocked {
			return
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("original Project SH did not block observer EX")
		}
	}
}

// This local construction retains the real Skill installation fixture graph.
// It owns both original guard-release cleanup callbacks because the older
// helper has no cleanup gate. Only actual pair.Joined permits either callback;
// a failed bounded drain keeps the guard held until this test process exits.
// The caller stops the parent test after a failed local subtest.
func newAgentOwnershipInstallationFixture(t *testing.T, allowRuntimeClose func() bool, factory func(*hookStore, *project.Authority, *object.ProcessGuard, objc.ProcessID) (*skill.Authority, error), bases ...*variableHTTPFixture) *skillInstallationFixture {
	t.Helper()
	if len(bases) > 1 || len(bases) == 1 && bases[0] == nil {
		t.Fatal("invalid optional original database fixture")
	}
	var base *variableHTTPFixture
	if len(bases) == 1 {
		base = bases[0] // A real older-prefix fixture for an in-place migration test.
	} else {
		base = newVariableHTTPFixture(t)
	}
	store := base.tracked
	manifest, err := agentOwnershipManifest()
	if err != nil {
		t.Fatal(err)
	}
	if base.artifactAssembly != nil {
		manifest = base.artifactAssembly.manifest(t, manifest)
	}
	lifecycle, err := project.NewLifecycleAuthority(store, manifest, nil)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := object.NewProjectAuditAuthority(store)
	if err != nil {
		t.Fatal(err)
	}
	projectFacts := map[ac.Producer]ac.ProjectFactAuthority{ac.ObjectProducer: facts}
	if base.artifactAssembly != nil {
		projectFacts[ac.ArtifactProducer] = base.artifactAssembly.projectAuditFacts(t, store)
	}
	projects, err := project.NewAuthority(store, project.AuthorityDependencies{Sessions: base.accounts, Routes: base.accounts, Lifecycle: lifecycle, AuditFacts: projectFacts})
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
		if !allowRuntimeClose() {
			return
		}
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
	objectAuth := object.Authorizations{Planner: authority, Resources: authority, Read: authority, Gate: authority, Cleanup: authority, Processes: guard, ProjectStop: lifecycle}
	if base.artifactAssembly != nil {
		objectAuth = base.artifactAssembly.bind(t, store, projects, authority, aud, objectAuth)
	}
	objects, err := object.New(store, backend, spool, aud, objectAuth)
	if err != nil {
		_ = spool.Close()
		_ = backend.Close()
		t.Fatal(err)
	}
	var objectRuntime *object.Runtime
	t.Cleanup(func() {
		if !allowRuntimeClose() {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if objectRuntime != nil {
			objectRuntime.StopAdmission()
			if err := objectRuntime.Drain(ctx); err != nil {
				t.Error("original Object runtime drain", err)
				_ = objectRuntime.Force(ctx)
			}
			return
		}
		objects.StopAdmission()
		if err := objects.Drain(ctx); err != nil {
			t.Error("original Object drain", err)
			_ = objects.Force(ctx)
		}
	})
	if factory == nil {
		if err = objects.Initialize(ctxFor(t)); err != nil {
			t.Fatal("Object initialize", err)
		}
	} else {
		// Only Runtime.Initialize binds the original process claim/guard.
		// An initialized Object Service alone is not a Runtime Source witness.
		endpoint, err := object.LoadTransferEndpoint(func(string) (string, bool) { return "", false }, config)
		if err != nil {
			t.Fatal(err)
		}
		transfers, err := object.NewTransferService(objects, nil, endpoint)
		if err != nil {
			t.Fatal(err)
		}
		objectRuntime, err = object.NewRuntime(objects, guard, transfers)
		if err != nil {
			t.Fatal(err)
		}
		if err = objectRuntime.Initialize(ctxFor(t)); err != nil {
			t.Fatal("real Object runtime initialize", err)
		}
		if bound, err := guard.CurrentProcess(); err != nil || bound != process {
			t.Fatal("original Runtime did not bind its exact process guard", err)
		}
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
	outboxProcess, err := f.ParseID[oc.Process](process.String())
	if err != nil {
		t.Fatal(err)
	}
	box, err := outbox.New(store, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{pc.ProjectProducer: projects}, Sessions: base.accounts, System: base.accounts, Projects: projects, Audit: aud, Cursors: base.keys, Processes: fixtureProcess{outboxProcess}})
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
	target := pc.CreateProjectRequest{ProjectID: id[i.Project](t), Name: "skill-install-fixture", Description: "owned installation prerequisite"}
	created, err := creator.CreateProject(ctxFor(t), base.ownerBrowser.actor, meta(t, id[struct{}](t).String(), nil), target)
	if err != nil || created.State != pc.CreationReady || created.Project == nil || created.Project.ID != target.ProjectID {
		t.Fatal("real target Project/Skills initialization", err)
	}
	var published bool
	if err = store.QueryRow(ctxFor(t), `SELECT EXISTS(SELECT 1 FROM agenteam_skill.initializations i JOIN agenteam_skill.skills s ON s.project_id=i.project_id AND s.creation_id=i.creation_id AND s.id=i.skill_id AND s.revision_id=i.revision_id WHERE i.project_id=$1 AND i.phase='published' AND s.protected AND s.serving)`, target.ProjectID.String()).Scan(&published); err != nil || !published {
		t.Fatal("target did not use real protected publication", err)
	}
	return &skillInstallationFixture{base: base, creator: creator, service: service, objects: objects, manifest: manifest, lifecycle: lifecycle, project: *created.Project, s3: s3, bucket: bucket}
}
