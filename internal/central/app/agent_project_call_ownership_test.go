package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	agentc "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// The real App constructors and original Agent entries run against a controlled
// transaction boundary. No case produces Committed, a bound guard, business
// success or a PostgreSQL claim. Unknown is an explicit port result; the real
// unbound guard rejects every callback used to attempt its later settlement.
type ownedAppCallStore struct {
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

func (s *ownedAppCallStore) WithinTx(ctx context.Context, cause f.TransactionCause, callback func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	s.causes = append(s.causes, cause)
	s.mu.Unlock()
	owner := cause.Details().Owner
	if owner == s.gate && s.entered != nil {
		s.entered <- ctx
		<-s.release // The original Store return is held even after cancellation.
	}
	if s.unknown != "" && owner == s.unknown {
		return f.UnknownResult(s.attempt, cause)
	}
	if s.checkGuard && (owner == "agent-call-register" || owner == "agent-call-return") {
		err := callback(ctx, f.NewTx())
		var fault *f.Fault
		if errors.As(err, &fault) {
			return f.NotCommittedResult(fault)
		}
	}
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}
func (*ownedAppCallStore) AcquireAll(context.Context, f.Tx, []f.LockRequest) error { return nil }
func (s *ownedAppCallStore) InTx(f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	s.sql++
	s.mu.Unlock()
	return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
}
func (s *ownedAppCallStore) snapshot() ([]f.TransactionCause, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]f.TransactionCause(nil), s.causes...), s.sql
}

// Observe actual provider Force invocation without changing its Stop/Join behavior.
type ownedAppProvider struct {
	accountWork
	forced int
}

func (p *ownedAppProvider) Force(ctx context.Context) error {
	p.forced++
	return p.accountWork.Force(ctx)
}

type ownedAppBundle struct {
	owned, legacy *agentConfigurationAssembly
	install       *agentInstallWork
	objects       *objectAssembly
	keys          cursor.Keyring
	makeOwned     func(*objectAssembly, cursor.Keyring) (*agentConfigurationAssembly, error)
}

func newOwnedAppBundle(t *testing.T, store *ownedAppCallStore) *ownedAppBundle {
	t.Helper()
	cfg := testConfig(t, "1s")
	accounts, err := account.NewAuthority(store, cfg.AccountKeyring())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := createProjectUsageAuthority(store, accounts)
	if err != nil {
		t.Fatal(err)
	}
	agents, err := createAgentConfigurationAuthority(store, projects.projects)
	if err != nil {
		t.Fatal(err)
	}
	process, err := f.NewID[objc.Process]()
	if err != nil {
		t.Fatal(err)
	}
	objects := &objectAssembly{process: process, guard: &object.ProcessGuard{}, service: &object.Service{}}
	install, err := createAgentInstall(store, objects)
	if err != nil {
		t.Fatal(err)
	}
	content, err := createKnowledgeSkillAuthoritiesWithInstall(store, projects, install.authority)
	if err != nil {
		t.Fatal(err)
	}
	skills, err := createSkills(context.Background(), content.skills, objects)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		skills.Stop()
		if err := skills.Drain(context.Background()); err != nil {
			t.Error(err)
		}
	})
	models, err := model.NewAuthority(store, model.Authorizations{Sessions: accounts, System: accounts, Projects: projects.projects})
	if err != nil {
		t.Fatal(err)
	}
	auditor, err := audit.New(store, cfg.CursorKeyring(), audit.Authorizations{Accounts: accounts, Sessions: accounts, System: accounts, Projects: content.audit, Models: models})
	if err != nil {
		t.Fatal(err)
	}
	catalog := event.NewCatalog()
	if _, err = defineWorkPlanningEvents(catalog); err != nil {
		t.Fatal(err)
	}
	events, err := agentc.RegisterAgentEvents(catalog)
	if err != nil {
		t.Fatal(err)
	}
	producer, err := agent.NewEventAuthority(agents, events)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := outbox.New(store, catalog, outbox.Authorizations{
		Producers: map[event.StableName]oc.ProducerAuthority{agentc.AgentProducer: producer},
		Projects:  projects.projects, Sessions: accounts, System: accounts, Audit: auditor,
		Processes: outboxProcessAuthority{process: process, guard: objects.guard}, Cursors: cfg.CursorKeyring(),
	})
	if err != nil {
		t.Fatal(err)
	}

	makeOwned := func(o *objectAssembly, k cursor.Keyring) (*agentConfigurationAssembly, error) {
		return createOwnedAgentConfiguration(store, projects.projects, accounts, agents, producer, models, skills, content.skills, install, auditor, journal, o, k)
	}
	b, err := makeOwned(objects, cfg.CursorKeyring())
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := createAgentConfiguration(store, projects.projects, accounts, agents, producer, models, skills, content.skills, install, auditor, journal)
	if err != nil {
		t.Fatal(err)
	}
	if rows, _ := store.snapshot(); len(rows) != 0 {
		t.Fatal("pure graph performed transaction work")
	}
	t.Cleanup(func() {
		b.StopAdmission()
		legacy.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		// A deliberately Unknown ownership row can remain unresolved in this
		// no-resource control. Do not invent success merely to empty its map.
		_ = b.Drain(ctx)
		_ = legacy.Drain(ctx)
		_ = install.Drain(ctx)
	})
	return &ownedAppBundle{b, legacy, install, objects, cfg.CursorKeyring(), makeOwned}
}
func ownedAppActor(t *testing.T) i.Actor {
	t.Helper()
	a, err := i.NewHuman(updateRootID[i.User](), updateRootID[i.Session]())
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func ownedAppCode(t *testing.T, err error, code f.Code) *f.Fault {
	t.Helper()
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != code {
		t.Fatalf("fault=%v want=%s", err, code)
	}
	return fault
}
func ownedAppReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(time.Second):
		t.Fatal("original controlled call did not reach its boundary")
		var zero T
		return zero
	}
}
func ownedAppRelease(t *testing.T, s *ownedAppCallStore) func() {
	t.Helper()
	var once sync.Once
	release := func() { once.Do(func() { close(s.release) }) }
	t.Cleanup(release)
	return release
}

func TestAgentProjectCallAppConstructionKeepsExplicitOwnership(t *testing.T) {
	s := &ownedAppCallStore{checkGuard: true}
	v := newOwnedAppBundle(t, s)
	if v.owned.ownership == nil || v.owned.service != v.owned.ownership.Service() || v.legacy.ownership != nil {
		t.Fatal("fixed owned pair or legacy boundary lost")
	}
	root := &accountAssembly{agents: v.owned}
	if work := root.works(); len(work) != 1 || work[0] != v.owned {
		t.Fatal("Agent pair acquired multiple accountWork owners")
	}
	for _, tc := range []struct {
		name    string
		objects *objectAssembly
		keys    cursor.Keyring
	}{
		{"missing-objects", nil, v.keys}, {"missing-guard", &objectAssembly{process: v.objects.process}, v.keys},
		{"invalid-process", &objectAssembly{guard: v.objects.guard}, v.keys}, {"invalid-keys", v.objects, cursor.Keyring{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := v.makeOwned(tc.objects, tc.keys); err == nil || got != nil {
				t.Fatal("incomplete owned construction accepted")
			}
		})
	}
	actor := ownedAppActor(t)
	project, target := updateRootID[i.Project](), updateRootID[i.Agent]()
	_, err := v.legacy.service.GetAgent(context.Background(), actor, project, target)
	ownedAppCode(t, err, f.DependencyUnavailable)
	before, _ := s.snapshot()
	if len(before) != 1 || before[0].Details().Owner == "agent-call-register" {
		t.Fatal("legacy constructor enrolled in ledger")
	}
	for _, entry := range []func() error{
		func() error {
			_, err := v.owned.service.GetAgent(context.Background(), actor, project, target)
			return err
		},
		func() error {
			_, err := v.owned.ownership.DirectoryReader().GetAgent(context.Background(), actor, project, target)
			return err
		},
	} {
		ownedAppCode(t, entry(), f.DependencyUnbound)
	}
	rows, sql := s.snapshot()
	if len(rows) != 3 || sql != 0 {
		t.Fatal("unbound guard reached SQL or business", len(rows), sql)
	}
	for _, c := range rows[1:] {
		if c.Details().Owner != "agent-call-register" {
			t.Fatal("guard rejection admitted business")
		}
	}
	if _, err := agentOwnedDirectoryHandler(v.legacy, nil, "https://example.test"); err == nil {
		t.Fatal("legacy reader substituted for owned pair")
	}
	root.StopAdmission()
	if err := root.Drain(context.Background()); err != nil || !root.Joined() {
		t.Fatal("actual rejected calls did not retire", err)
	}
}

func TestAgentProjectCallAppBothOriginalEntriesBlockProviderForce(t *testing.T) {
	s := &ownedAppCallStore{gate: "agent-call-register", entered: make(chan context.Context, 2), release: make(chan struct{})}
	v := newOwnedAppBundle(t, s)
	release := ownedAppRelease(t, s)
	actor := ownedAppActor(t)
	project, target := updateRootID[i.Project](), updateRootID[i.Agent]()
	service, directory := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := v.owned.service.GetAgent(context.Background(), actor, project, target)
		service <- err
	}()
	serviceCtx := ownedAppReceive(t, s.entered)
	go func() {
		_, err := v.owned.ownership.DirectoryReader().GetAgent(context.Background(), actor, project, target)
		directory <- err
	}()
	directoryCtx := ownedAppReceive(t, s.entered)
	shared := &runtimeExternalProbe{}
	install := &ownedAppProvider{accountWork: v.install}
	root := &accountAssembly{agents: v.owned, agentInstall: install, skills: shared}
	root.StopAdmission()
	if serviceCtx.Err() == nil || directoryCtx.Err() == nil {
		t.Fatal("Stop omitted an actual Agent lane")
	}
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if root.Force(expired) == nil || root.Joined() || v.owned.Joined() || install.forced != 0 || shared.forced != 0 {
		t.Fatal("held original return retired a provider")
	}
	select {
	case <-service:
		t.Fatal("Service escaped its actual Store return")
	default:
	}
	select {
	case <-directory:
		t.Fatal("Directory escaped its actual Store return")
	default:
	}
	_, err := v.owned.ownership.DirectoryReader().GetAgent(context.Background(), actor, project, target)
	ownedAppCode(t, err, f.ShuttingDown)
	release()
	if ownedAppReceive(t, service) == nil || ownedAppReceive(t, directory) == nil {
		t.Fatal("NotCommitted became successful business")
	}
	if err := root.Drain(context.Background()); err != nil || !root.Joined() {
		t.Fatal("actual returns did not release the pair", err)
	}
	rows, _ := s.snapshot()
	if len(rows) != 2 {
		t.Fatal("stop reopened admission or ran business", len(rows))
	}
}

func TestAgentProjectCallAppRegistryUnknownWaitsForOriginalReturn(t *testing.T) {
	s := &ownedAppCallStore{gate: "app.agent-configuration", unknown: "app.agent-configuration", entered: make(chan context.Context, 1), release: make(chan struct{}), attempt: updateRootID[f.TransactionAttempt]()}
	v := newOwnedAppBundle(t, s)
	release := ownedAppRelease(t, s)
	done := make(chan error, 1)
	go func() { done <- v.owned.Initialize(context.Background()) }()
	original := ownedAppReceive(t, s.entered)
	shared := &runtimeExternalProbe{}
	root := &accountAssembly{agents: v.owned, agentInstall: v.install, skills: shared}
	root.StopAdmission()
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	if root.Force(expired) == nil || original.Err() == nil || root.Joined() || shared.forced != 0 {
		t.Fatal("Registry cancellation replaced original return")
	}
	owned := &resources{stopping: true, accountService: root}
	guard := &runtimeGuardProbe{}
	owned.forceObjects(expired, guard)
	if guard.forced != 0 {
		t.Fatal("Registry original return released guard")
	}
	release()
	fault := ownedAppCode(t, ownedAppReceive(t, done), f.CommitUnknown)
	if fault.CommitState != f.Unknown || fault.CauseID != s.attempt.String() {
		t.Fatal("Registry lost original Unknown identity")
	}
	if err := root.Drain(context.Background()); err != nil || !root.Joined() {
		t.Fatal("returned Registry work retained phantom ownership", err)
	}
	owned.forceObjects(expired, guard)
	if guard.forced != 1 {
		t.Fatal("fully joined owners did not reach root guard gate")
	}
}

func TestAgentProjectCallAppUnknownKeepsOriginalOwnerAfterStop(t *testing.T) {
	for _, lane := range []string{"mutation", "directory"} {
		t.Run(lane, func(t *testing.T) {
			s := &ownedAppCallStore{unknown: "agent-call-register", attempt: updateRootID[f.TransactionAttempt](), checkGuard: true}
			v := newOwnedAppBundle(t, s)
			actor := ownedAppActor(t)
			project, target := updateRootID[i.Project](), updateRootID[i.Agent]()
			var err error
			if lane == "directory" {
				_, err = v.owned.ownership.DirectoryReader().GetAgent(context.Background(), actor, project, target)
			} else {
				description := "ownership control"
				request, e := agentc.NewAgentUpdate(agentc.AgentUpdateFields{Description: &description})
				if e != nil {
					t.Fatal(e)
				}
				version := f.Version(1)
				meta := f.CommandMeta{RequestID: updateRootID[f.Request](), IdempotencyKey: f.IdempotencyKey(updateRootID[struct{}]().String()), ExpectedVersion: &version}
				_, err = v.owned.service.UpdateAgent(context.Background(), actor, meta, project, target, request)
			}
			original := ownedAppCode(t, err, f.CommitUnknown)
			if original.CommitState != f.Unknown || original.CauseID != s.attempt.String() {
				t.Fatal("entry lost original registration Unknown")
			}
			shared := &runtimeExternalProbe{}
			install := &ownedAppProvider{accountWork: v.install}
			root := &accountAssembly{agents: v.owned, agentInstall: install, skills: shared}
			root.StopAdmission()
			if v.owned.Joined() || root.Joined() {
				t.Fatal("original returned Unknown disappeared from pair accounting")
			}
			if root.Drain(context.Background()) == nil || v.owned.Joined() {
				t.Fatal("unbound settlement produced Joined")
			}
			expired, cancel := context.WithCancel(context.Background())
			cancel()
			if root.Force(expired) == nil || shared.forced != 0 || install.forced != 0 {
				t.Fatal("Force bypassed retained original call")
			}
			rows, sql := s.snapshot()
			registrations, checkpoints := 0, 0
			var originalID string
			for _, cause := range rows {
				d := cause.Details()
				switch d.Owner {
				case "agent-call-register":
					registrations++
					originalID = d.RecoveryRunID
				case "agent-call-return":
					checkpoints++
				default:
					t.Fatal("Unknown registration ran business", d.Owner)
				}
			}
			if registrations != 1 || checkpoints != 2 || sql != 0 {
				t.Fatal("original-only settlement or zero-business boundary lost", registrations, checkpoints, sql)
			}
			for _, cause := range rows {
				d := cause.Details()
				if d.Owner == "agent-call-return" && d.RecoveryRunID != originalID {
					t.Fatal("Stop created a replacement CallID")
				}
			}
			if original.CauseID != s.attempt.String() || original.CommitState != f.Unknown {
				t.Fatal("Drain rewrote the original business error")
			}
		})
	}
}

func TestAgentProjectCallAppLateInstallRetainsOriginalBudget(t *testing.T) {
	s := &ownedAppCallStore{}
	v := newOwnedAppBundle(t, s)
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	root := &accountAssembly{constructing: true}
	_ = root.Force(expired)
	if root.install(context.Background(), func() { root.agents = v.owned; root.agentInstall = v.install }) {
		t.Fatal("late Agent pair escaped stopped construction")
	}
	if root.forced != expired || !v.owned.Joined() || !v.install.Joined() || root.Joined() {
		t.Fatal("late empty pair lost original force budget/constructing owner")
	}
	if err := v.owned.Initialize(context.Background()); err == nil {
		t.Fatal("late stopped pair started Registry work")
	}
	root.constructionDone()
	if !root.Joined() {
		t.Fatal("actual empty late owner did not retire")
	}
	if rows, _ := s.snapshot(); len(rows) != 0 {
		t.Fatal("late constructor or stopped Initialize performed SQL")
	}
}
