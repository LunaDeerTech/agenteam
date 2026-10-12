package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/projectvariable"
	vc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// Real App factories and both actual service entries use this controlled Store
// return boundary. It never produces Committed or successful business. Unknown
// is an explicit port result, not a PostgreSQL ack loss. The concrete unbound
// guard rejects every attempted callback before SQL, including later settlement.
type variableAppCallStore struct {
	*postgres.Store
	mu            sync.Mutex
	causes        []f.TransactionCause
	sql           int
	gate, unknown string
	entered       chan context.Context
	release       chan struct{}
	attempt       f.ID[f.TransactionAttempt]
	checkGuard    bool
}

func (s *variableAppCallStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.causes = append(s.causes, cause)
	gate, unknown, entered, release, checkGuard := s.gate, s.unknown, s.entered, s.release, s.checkGuard
	s.mu.Unlock()
	owner := cause.Details().Owner
	if owner == gate && entered != nil {
		entered <- ctx
		<-release // Cancellation cannot replace this original return.
	}
	if owner == unknown && unknown != "" {
		return f.UnknownResult(s.attempt, cause)
	}
	if checkGuard && (owner == "projectvariable-call-register" || owner == "projectvariable-call-return") {
		err := callback(ctx, f.NewTx())
		var fault *f.Fault
		if errors.As(err, &fault) {
			return f.NotCommittedResult(fault)
		}
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func (*variableAppCallStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error { return nil }
func (s *variableAppCallStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	s.sql++
	s.mu.Unlock()
	return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
}
func (s *variableAppCallStore) snapshot() ([]f.TransactionCause, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]f.TransactionCause(nil), s.causes...), s.sql
}

type variableAppBundle struct {
	owned         *projectVariableCallAssembly
	legacy        *projectVariablesAssembly
	legacySecret  *projectSecretVariablesAssembly
	objects       *objectAssembly
	writes        *projectvariable.SecretWriteAuthority
	foreignWrites *projectvariable.SecretWriteAuthority
	makeOwned     func(*objectAssembly, *projectvariable.SecretWriteAuthority) (*projectVariableCallAssembly, error)
}

func newVariableAppBundle(t *testing.T, store *variableAppCallStore) *variableAppBundle {
	t.Helper()
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := createProjectUsage(cfg, store, accounts)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := createProjectSecretWriteAuthority(store, projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	models, err := model.NewAuthority(store, model.Authorizations{Sessions: accounts, System: accounts, Projects: projects.projects})
	if err != nil {
		t.Fatal(err)
	}
	usage, err := model.NewSecretUsageRouter(models, accounts)
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: projects.projects})
	if err != nil {
		t.Fatal(err)
	}
	secrets, err := createSecret(cfg, store, auditor, accounts, usage, projects.projects, writes)
	if err != nil || secrets.Status().Available {
		t.Fatal("D04 construction was not pure", err)
	}
	process := updateRootID[objc.Process]()
	objects := &objectAssembly{process: process, guard: &object.ProcessGuard{}, service: &object.Service{}}
	catalog := event.NewCatalog()
	ordinaryEvents, err := vc.RegisterVariableEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	secretEvents, err := vc.RegisterSecretVariableEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := outbox.New(store, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{vc.VariableProducer: projects.variables},
		Projects:  projects.projects, Sessions: accounts, System: accounts, Audit: auditor,
		Processes: outboxProcessAuthority{process: process, guard: objects.guard}, Cursors: cfg.CursorKeyring(),
	})
	if err != nil {
		t.Fatal(err)
	}
	makeOwned := func(o *objectAssembly, w *projectvariable.SecretWriteAuthority) (*projectVariableCallAssembly, error) {
		return createOwnedProjectVariables(cfg, store, projects.variables, w, secrets, projects.projects, accounts, auditor, journal, ordinaryEvents, secretEvents, o)
	}
	b, err := makeOwned(objects, writes)
	if err != nil || b == nil {
		t.Fatal("owned pair construction", err)
	}
	legacy, err := createProjectVariables(cfg, store, projects.variables, projects.projects, accounts, auditor, journal, ordinaryEvents)
	if err != nil {
		t.Fatal(err)
	}
	legacySecret, err := createProjectSecretVariables(cfg, store, projects.variables, writes, secrets, projects.projects, accounts, auditor, journal, secretEvents)
	if err != nil {
		t.Fatal(err)
	}
	foreignWrites, err := createProjectSecretWriteAuthority(&workRootStore{}, projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	if causes, sql := store.snapshot(); len(causes) != 0 || sql != 0 {
		t.Fatal("construction performed transaction work")
	}
	t.Cleanup(func() {
		b.StopAdmission()
		legacy.StopAdmission()
		legacySecret.StopAdmission()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		// Deliberately Unknown rows cannot be settled by an unbound guard. No
		// resource or goroutine is hidden by leaving that honest state retained.
		_ = b.Drain(ctx)
		_ = legacy.Drain(ctx)
		_ = legacySecret.Drain(ctx)
	})
	return &variableAppBundle{b, legacy, legacySecret, objects, writes, foreignWrites, makeOwned}
}

func variableAppActor(t *testing.T) i.Actor {
	t.Helper()
	a, err := i.NewHuman(updateRootID[i.User](), updateRootID[i.Session]())
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func variableAppFault(t *testing.T, err error, code f.Code) *f.Fault {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("fault=%v want=%s", err, code)
	}
	return fault
}
func variableAppReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(time.Second):
		t.Fatal("actual controlled call did not reach its boundary")
		var zero T
		return zero
	}
}
func variableAppRelease(t *testing.T, s *variableAppCallStore) func() {
	t.Helper()
	var once sync.Once
	release := func() { once.Do(func() { close(s.release) }) }
	t.Cleanup(release)
	return release
}
func variableAppAsync(t *testing.T, release func(), fn func() error) <-chan error {
	t.Helper()
	result, returned := make(chan error, 1), make(chan struct{})
	go func() { defer close(returned); result <- fn() }()
	t.Cleanup(func() {
		release()
		select {
		case <-returned:
		case <-time.After(time.Second):
			t.Error("original controlled goroutine did not return after release")
		}
	})
	return result
}
func variableAppRoot(v *variableAppBundle) *accountAssembly {
	return &accountAssembly{variableCalls: v.owned, variables: v.owned.variables, secretVariables: v.owned.secretVariables}
}
func variableAppRead(v *variableAppBundle, actor i.Actor, project i.ProjectID, target i.ProjectVariableID, secret bool) error {
	if secret {
		_, err := v.owned.secretVariables.service.GetSecretVariable(context.Background(), actor, project, target)
		return err
	}
	_, err := v.owned.variables.service.GetVariable(context.Background(), actor, project, target)
	return err
}

func TestVariableProjectCallAppConstructionAndLegacyViews(t *testing.T) {
	s := &variableAppCallStore{checkGuard: true}
	v := newVariableAppBundle(t, s)
	if v.owned.pair == nil || v.owned.variables.service != v.owned.pair.Service() || v.owned.secretVariables.service != v.owned.pair.SecretService() || v.owned.variables.owner != v.owned || v.owned.secretVariables.owner != v.owned || v.legacy.owner != nil || v.legacySecret.owner != nil {
		t.Fatal("same pair, concrete views or legacy boundary lost")
	}
	root := variableAppRoot(v)
	if work := root.works(); len(work) != 1 || work[0] != v.owned {
		t.Fatal("pair was registered as multiple lifecycle owners")
	}
	if v.owned.Joined() || v.owned.variables.Joined() || v.owned.secretVariables.Joined() {
		t.Fatal("construction alone reported stopped/joined")
	}
	for _, tc := range []struct {
		name    string
		objects *objectAssembly
		writes  *projectvariable.SecretWriteAuthority
	}{
		{"missing-objects", nil, v.writes},
		{"missing-guard", &objectAssembly{process: v.objects.process}, v.writes},
		{"invalid-process", &objectAssembly{guard: v.objects.guard}, v.writes},
		{"missing-write-authority", v.objects, nil},
		{"foreign-write-store", v.objects, v.foreignWrites},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := v.makeOwned(tc.objects, tc.writes); err == nil || got != nil {
				t.Fatal("incomplete owned dependency accepted")
			}
		})
	}
	actor, project, target := variableAppActor(t), updateRootID[i.Project](), updateRootID[i.ProjectVariable]()
	_, err := v.legacy.service.GetVariable(context.Background(), actor, project, target)
	variableAppFault(t, err, f.DependencyUnavailable)
	_, err = v.legacySecret.service.GetSecretVariable(context.Background(), actor, project, target)
	variableAppFault(t, err, f.DependencyUnavailable)
	before, _ := s.snapshot()
	if len(before) != 2 {
		t.Fatal("legacy real entries did not reach original Store")
	}
	for _, cause := range before {
		if cause.Details().Owner == "projectvariable-call-register" {
			t.Fatal("legacy constructor acquired a ledger")
		}
	}
	variableAppFault(t, variableAppRead(v, actor, project, target, false), f.DependencyUnbound)
	variableAppFault(t, variableAppRead(v, actor, project, target, true), f.DependencyUnbound)
	rows, sql := s.snapshot()
	if len(rows) != 4 || sql != 0 {
		t.Fatal("unbound guard reached SQL/business", len(rows), sql)
	}
	for _, cause := range rows[2:] {
		if cause.Details().Owner != "projectvariable-call-register" {
			t.Fatal("failed registration ran business")
		}
	}
	// Either concrete view must stop the same pair, including the other lane.
	v.owned.secretVariables.StopAdmission()
	variableAppFault(t, variableAppRead(v, actor, project, target, false), f.ShuttingDown)
	variableAppFault(t, variableAppRead(v, actor, project, target, true), f.ShuttingDown)
	if err := v.owned.variables.Drain(context.Background()); err != nil || !v.owned.Joined() || !v.owned.variables.Joined() || !v.owned.secretVariables.Joined() {
		t.Fatal("owned view lifecycle diverged from actual pair", err)
	}
	root.StopAdmission()
	if !root.Joined() {
		t.Fatal("root ignored actual paired join")
	}
}

func TestVariableProjectCallAppBothLanesHoldOriginalReturn(t *testing.T) {
	s := &variableAppCallStore{gate: "projectvariable-call-register", entered: make(chan context.Context, 4), release: make(chan struct{})}
	v := newVariableAppBundle(t, s)
	release := variableAppRelease(t, s)
	actor, project, target := variableAppActor(t), updateRootID[i.Project](), updateRootID[i.ProjectVariable]()
	ordinary := variableAppAsync(t, release, func() error { return variableAppRead(v, actor, project, target, false) })
	ordinaryCtx := variableAppReceive(t, s.entered)
	secret := variableAppAsync(t, release, func() error { return variableAppRead(v, actor, project, target, true) })
	secretCtx := variableAppReceive(t, s.entered)
	provider := &runtimeExternalProbe{}
	root := variableAppRoot(v)
	root.projects = provider
	root.StopAdmission()
	if ordinaryCtx.Err() == nil || secretCtx.Err() == nil {
		t.Fatal("paired Stop missed an original lane")
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if root.Force(expired) == nil || root.Joined() || v.owned.variables.Joined() || v.owned.secretVariables.Joined() || provider.forced != 0 {
		t.Fatal("Force retired provider before original Store return")
	}
	resources := &resources{stopping: true, accountService: root}
	guard := &runtimeGuardProbe{}
	resources.forceObjects(expired, guard)
	if guard.forced != 0 {
		t.Fatal("held Variable lane released original guard gate")
	}
	for _, done := range []<-chan error{ordinary, secret} {
		select {
		case <-done:
			t.Fatal("cancel replaced actual Store return")
		default:
		}
	}
	variableAppFault(t, variableAppRead(v, actor, project, target, false), f.ShuttingDown)
	variableAppFault(t, variableAppRead(v, actor, project, target, true), f.ShuttingDown)
	release()
	if variableAppReceive(t, ordinary) == nil || variableAppReceive(t, secret) == nil {
		t.Fatal("NotCommitted became business success")
	}
	if err := root.Drain(context.Background()); err != nil || !root.Joined() || !v.owned.variables.Joined() || !v.owned.secretVariables.Joined() {
		t.Fatal("actual paired returns did not join", err)
	}
	resources.forceObjects(expired, guard)
	if guard.forced != 1 {
		t.Fatal("actual join did not open original guard gate")
	}
	if rows, sql := s.snapshot(); len(rows) != 2 || sql != 0 {
		t.Fatal("stopped admission or rejected registration ran business")
	}
}

func TestVariableProjectCallAppUnknownCheckpointKeepsOriginalOwner(t *testing.T) {
	for _, secret := range []bool{false, true} {
		name := "ordinary"
		if secret {
			name = "secret"
		}
		t.Run(name, func(t *testing.T) {
			s := &variableAppCallStore{unknown: "projectvariable-call-register", gate: "projectvariable-call-return", entered: make(chan context.Context, 8), release: make(chan struct{}), attempt: updateRootID[f.TransactionAttempt](), checkGuard: true}
			v := newVariableAppBundle(t, s)
			release := variableAppRelease(t, s)
			actor, project, target := variableAppActor(t), updateRootID[i.Project](), updateRootID[i.ProjectVariable]()
			done := variableAppAsync(t, release, func() error { return variableAppRead(v, actor, project, target, secret) })
			checkpointCtx := variableAppReceive(t, s.entered)
			if deadline, ok := checkpointCtx.Deadline(); !ok || time.Until(deadline) > 3*time.Second || checkpointCtx.Err() != nil {
				t.Fatal("original bounded checkpoint context missing")
			}
			root := variableAppRoot(v)
			provider := &runtimeExternalProbe{}
			root.projects = provider
			root.StopAdmission()
			expired, cancel := context.WithCancel(context.Background())
			cancel()
			if root.Force(expired) == nil || root.Joined() || v.owned.variables.Joined() || v.owned.secretVariables.Joined() || provider.forced != 0 {
				t.Fatal("held checkpoint released its owner/provider")
			}
			resources := &resources{stopping: true, accountService: root}
			guard := &runtimeGuardProbe{}
			resources.forceObjects(expired, guard)
			if guard.forced != 0 {
				t.Fatal("checkpoint original return released guard")
			}
			select {
			case <-done:
				t.Fatal("entry escaped actual checkpoint return")
			default:
			}
			release()
			original := variableAppFault(t, variableAppReceive(t, done), f.CommitUnknown)
			if original.CommitState != f.Unknown || original.CauseID != s.attempt.String() {
				t.Fatal("checkpoint rewrote original registration Unknown")
			}
			if root.Drain(context.Background()) == nil || root.Joined() || v.owned.variables.Joined() || v.owned.secretVariables.Joined() {
				t.Fatal("unbound settlement discarded retained original identity")
			}
			resources.forceObjects(expired, guard)
			if guard.forced != 0 {
				t.Fatal("returned but unresolved owner released guard")
			}
			rows, sql := s.snapshot()
			if len(rows) != 3 || sql != 0 {
				t.Fatal("expected original registration plus two original-only checkpoints", len(rows), sql)
			}
			id := rows[0].Details().RecoveryRunID
			if id == "" || rows[0].Details().Owner != "projectvariable-call-register" {
				t.Fatal("original registration identity missing")
			}
			for _, cause := range rows[1:] {
				d := cause.Details()
				if d.Owner != "projectvariable-call-return" || d.RecoveryRunID != id {
					t.Fatal("settlement created a new identity or ran business")
				}
			}
			variableAppFault(t, variableAppRead(v, actor, project, target, false), f.ShuttingDown)
			variableAppFault(t, variableAppRead(v, actor, project, target, true), f.ShuttingDown)
		})
	}
}

func TestVariableProjectCallAppLateInstallUsesOriginalForce(t *testing.T) {
	s := &variableAppCallStore{unknown: "projectvariable-call-register", attempt: updateRootID[f.TransactionAttempt](), checkGuard: true}
	v := newVariableAppBundle(t, s)
	actor, project, target := variableAppActor(t), updateRootID[i.Project](), updateRootID[i.ProjectVariable]()
	originalFault := variableAppFault(t, variableAppRead(v, actor, project, target, true), f.CommitUnknown)
	root := &accountAssembly{constructing: true}
	originalCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := root.Force(originalCtx); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.gate, s.entered, s.release = "projectvariable-call-return", make(chan context.Context, 4), make(chan struct{})
	s.mu.Unlock()
	release := variableAppRelease(t, s)
	provider := &runtimeExternalProbe{}
	done := variableAppAsync(t, release, func() error {
		if root.install(context.Background(), func() {
			root.variableCalls, root.variables, root.secretVariables, root.projects = v.owned, v.owned.variables, v.owned.secretVariables, provider
		}) {
			return errors.New("late pair escaped stopped construction")
		}
		return nil
	})
	if got := variableAppReceive(t, s.entered); got != originalCtx {
		t.Fatal("late installation renewed Force context")
	}
	work := root.works()
	if len(work) != 2 || work[0] != v.owned || work[1] != provider || provider.forced != 0 || root.Joined() {
		t.Fatal("late atomic install lost unique owner or provider ordering")
	}
	if v.owned.variables.Joined() || v.owned.secretVariables.Joined() {
		t.Fatal("late view ignored retained pair")
	}
	cancel()
	release()
	if err := variableAppReceive(t, done); err != nil {
		t.Fatal(err)
	}
	root.constructionDone()
	if root.Joined() || provider.forced != 0 || root.forced != originalCtx {
		t.Fatal("late failed settlement was retired or budget replaced")
	}
	if originalFault.CauseID != s.attempt.String() || originalFault.CommitState != f.Unknown {
		t.Fatal("late cleanup changed original outcome")
	}
	rows, sql := s.snapshot()
	if len(rows) != 3 || sql != 0 {
		t.Fatal("late install dispatched new work")
	}
	for _, cause := range rows[1:] {
		if cause.Details().Owner != "projectvariable-call-return" || cause.Details().RecoveryRunID != rows[0].Details().RecoveryRunID {
			t.Fatal("late cleanup substituted call identity")
		}
	}
}
