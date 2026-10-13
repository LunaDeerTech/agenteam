//go:build integration

package app

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/usage"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// The original default Runtime reaches a real, non-retryable HTTP policy deny.
// No wire request is allowed. The manager's original empty discovery return
// remains controlled, as in the existing App fixture. Lifecycle acceptance is
// explicit: the default Registry remains nil. Other domains' Stop/handoff and
// Execution Completed are controlled prerequisites, never Usage business data.
func TestUsageProjectLifecycleDefaultApp(t *testing.T) {
	t.Run("policy-deny-and-original-wholecall-return", usageAppWholeReturn)
	t.Run("technical-and-rebuild-original-unknown", usageAppUnknown)
	t.Run("three-tables-and-original-cleanup-confirmation", usageAppCleanup)
}

const usageAppNoDispatch = "/usage-no-dispatch"

type usageAppObservation struct {
	result        f.CommitResult
	cause         f.TransactionCause
	before, after int64
	returned      chan struct{}
	masked        f.CommitResult
}
type usageAppHook struct {
	match     func(f.TransactionCause) bool
	selectRow func(context.Context, postgres.SQLExecutor, f.TransactionCause, int64, int64) (bool, error)
	mask      bool
	observed  chan usageAppObservation
	release   chan struct{}
	once      sync.Once
	used      bool
}

func (h *usageAppHook) open() { h.once.Do(func() { close(h.release) }) }

type usageAppStore struct {
	*executionSchedulerAppStore
	mu    sync.Mutex
	hooks []*usageAppHook
}

func (s *usageAppStore) arm(h *usageAppHook) {
	h.observed, h.release = make(chan usageAppObservation, 1), make(chan struct{})
	s.mu.Lock()
	s.hooks = append(s.hooks, h)
	s.mu.Unlock()
}
func (s *usageAppStore) releaseAll() {
	s.mu.Lock()
	hooks := append([]*usageAppHook(nil), s.hooks...)
	s.mu.Unlock()
	for _, h := range hooks {
		s.mu.Lock()
		h.used = true
		s.mu.Unlock()
		h.open()
	}
}
func usageAppWrites(ctx context.Context, x postgres.SQLExecutor) (int64, error) {
	var n int64
	err := x.QueryRow(ctx, `SELECT COALESCE(sum(n_tup_ins+n_tup_upd+n_tup_del),0)::bigint FROM pg_stat_xact_user_tables WHERE schemaname='agenteam_model' AND relname IN ('invocations','invocation_observations','execution_usage_summaries','usage_project_calls')`).Scan(&n)
	return n, err
}
func (s *usageAppStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	s.mu.Lock()
	var h *usageAppHook
	for _, candidate := range s.hooks {
		if !candidate.used && candidate.match(cause) {
			h = candidate
			break
		}
	}
	s.mu.Unlock()
	var before, after int64
	selected := false
	r := s.executionSchedulerAppStore.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if h == nil {
			return fn(ctx, tx)
		}
		x, err := s.InTx(tx)
		if err != nil {
			return err
		}
		before, err = usageAppWrites(ctx, x)
		if err != nil {
			return err
		}
		if err = fn(ctx, tx); err != nil {
			return err
		}
		after, err = usageAppWrites(ctx, x)
		if err != nil {
			return err
		}
		selected, err = h.selectRow(ctx, x, cause, before, after)
		return err
	})
	if !selected || r.State() != f.Committed {
		return r
	}
	s.mu.Lock()
	if h.used {
		s.mu.Unlock()
		return r
	}
	h.used = true
	s.mu.Unlock()
	returned := make(chan struct{})
	defer close(returned)
	masked := r
	if h.mask {
		attempt, err := f.NewID[f.TransactionAttempt]()
		if err != nil {
			return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
		}
		masked = f.UnknownResult(attempt, cause)
	}
	h.observed <- usageAppObservation{r, cause, before, after, returned, masked}
	<-h.release
	return masked
}

type usageAppCall struct {
	done chan struct{}
	err  error
}
type usageLifecycleApp struct {
	*executionSchedulerApp
	tracked           *usageAppStore
	bound             *projectUsageAssembly
	pair              *usage.ProjectCallOwnership
	acceptor          *project.Service
	accounts          *account.Authority
	stop              *project.LifecycleStopDriver
	cleaner           *project.LifecycleCleanupDriver
	processes         outboxProcessAuthority
	cause             pc.LifecycleCause
	scope             pc.ScopeRef
	report            pc.StopReport
	inspecting        bool
	calls             []*usageAppCall
	needsConfirmation bool
	dispatches        atomic.Int64
	unavailableCalls  atomic.Int64
}

func newUsageLifecycleApp(t *testing.T) *usageLifecycleApp {
	t.Helper()
	v := &usageLifecycleApp{}
	v.executionSchedulerApp = newExecutionSchedulerAppWith(t, func(d *dependencies, original *executionSchedulerApp) {
		v.tracked = &usageAppStore{executionSchedulerAppStore: original.store}
		open := d.open
		d.open = func(ctx context.Context, cfg postgres.Config) (database, error) {
			_, err := open(ctx, cfg)
			return v.tracked, err
		}
		d.observeProjectUsage = func(bound *projectUsageAssembly) { v.bound = bound }
		bind := d.bind
		d.bind = func(ctx context.Context, cfg config.Config, db database, r *resources, deps *dependencies) error {
			if err := bind(ctx, cfg, db, r, deps); err != nil {
				return err
			}
			a := r.accounts().(*accountAssembly)
			a.mu.Lock()
			prior := a.handler
			a.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, usageAppNoDispatch) {
					v.dispatches.Add(1)
				}
				prior.ServeHTTP(w, r)
			})
			a.mu.Unlock()
			return nil
		}
	})
	if v.bound == nil || v.bound.lifecycle == nil || v.bound.usageCalls == nil || v.assembly.usageCalls != v.bound.usageCalls || v.bound.reader != v.bound.usageCalls.pair.Service() || v.bound.reader.ProjectInvocationBorrower() != v.bound.reader {
		t.Fatal("default App split original Usage reader/Runtime borrower")
	}
	v.pair = v.bound.usageCalls.pair
	v.processes = outboxProcessAuthority{process: v.objects.process, guard: v.objects.guard}
	// Release every actual Store barrier before settling retained project work,
	// then let the original App cleanup stop the shared Runtime/guard/database.
	t.Cleanup(func() {
		v.tracked.releaseAll()
		for _, c := range v.calls {
			usageAppWait(t, c.done, "original test call cleanup")
		}
		if v.needsConfirmation {
			if v.cause.Validate() != nil {
				v.accept(t)
			}
			settled := false
			for n := 0; n < 8; n++ {
				err := v.stop.Run(v.ctx, v.a, v.cause.OperationID)
				if err == nil && v.report.Details().State == pc.Stopped {
					settled = true
					break
				}
			}
			if !settled {
				t.Error("original Usage Unknown did not settle before global Stop")
			}
		}
		if v.cleaner != nil {
			settled := false
			for n := 0; n < 8; n++ {
				if err := v.cleaner.Run(v.ctx, v.a, v.cause.OperationID); err == nil {
					settled = true
					break
				}
			}
			if !settled {
				t.Error("original cleanup Unknown did not settle before global Stop")
			}
		}
		if v.unavailableCalls.Load() != 0 {
			t.Error("controlled acceptance invoked an unavailable participant")
		}
		for _, owner := range []interface {
			Stop()
			Drain(context.Context) error
		}{v.cleaner, v.stop, v.acceptor} {
			if reflect.ValueOf(owner).IsNil() {
				continue
			}
			owner.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := owner.Drain(ctx)
			cancel()
			if err != nil {
				t.Error("original explicit lifecycle owner did not return", err)
			}
		}
	})
	v.explicitAcceptance(t)
	return v
}

// Only BeginDelete uses this Registry. No unavailable participant callback is
// executed, and it is never installed into the default App service.
type usageAppUnavailable struct {
	name   pc.ParticipantName
	called *atomic.Int64
}

func (p usageAppUnavailable) Name() pc.ParticipantName { return p.name }
func (p usageAppUnavailable) RequestStop(context.Context, id.Actor, pc.LifecycleCause, pc.ScopeRef) (pc.StopReport, error) {
	p.called.Add(1)
	return pc.StopReport{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
}
func (p usageAppUnavailable) InspectStop(context.Context, id.Actor, pc.LifecycleCause, pc.ScopeRef) (pc.StopReport, error) {
	p.called.Add(1)
	return pc.StopReport{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
}
func (p usageAppUnavailable) Cleanup(context.Context, id.Actor, pc.LifecycleCause, pc.ScopeRef, *pc.CleanupCheckpoint) (pc.CleanupReport, error) {
	p.called.Add(1)
	return pc.CleanupReport{}, f.NewFault(f.DependencyUnbound, f.NotStarted)
}

func (v *usageLifecycleApp) explicitAcceptance(t *testing.T) {
	t.Helper()
	accounts, err := account.NewAuthority(v.tracked, v.root.cfg.AccountKeyring())
	workAppCheck(t, "usage-acceptance-account", err)
	v.accounts = accounts
	catalog := event.NewCatalog()
	events, err := pc.RegisterProjectEvents(catalog)
	workAppCheck(t, "usage-acceptance-events", err)
	v.root.owned.mu.Lock()
	auditing := v.root.owned.auditService
	v.root.owned.mu.Unlock()
	journal, err := outbox.New(v.tracked, catalog, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{pc.ProjectProducer: v.bound.projects}, Projects: v.bound.projects, Sessions: accounts, System: accounts, Audit: auditing, Cursors: v.root.cfg.CursorKeyring(), Processes: v.processes})
	workAppCheck(t, "usage-acceptance-outbox", err)
	manifest, err := defaultProjectLifecycleManifest()
	workAppCheck(t, "usage-original-manifest", err)
	var bindings []project.LifecycleParticipantBinding
	for _, entry := range manifest.Entries() {
		bindings = append(bindings, project.LifecycleParticipantBinding{Registration: entry, Participant: usageAppUnavailable{entry.Name, &v.unavailableCalls}})
	}
	registry, err := project.NewLifecycleRegistry(manifest, bindings)
	workAppCheck(t, "usage-controlled-acceptance-registry", err)
	v.acceptor, err = project.New(v.tracked, project.Dependencies{Authority: v.bound.projects, Activity: accounts, Audit: auditing, Events: journal, ProjectEvents: events, Processes: v.processes, Cursors: v.root.cfg.CursorKeyring(), LifecycleRegistry: registry}, project.DefaultConfig())
	workAppCheck(t, "usage-explicit-acceptor", err)
	v.stop, err = project.NewLifecycleStopDriver(v.tracked, v.bound.lifecycle, v.processes, func(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef) error {
		var err error
		if v.inspecting {
			v.report, err = v.pair.InspectProjectStop(ctx, actor, cause, scope)
		} else {
			v.report, err = v.pair.RequestProjectStop(ctx, actor, cause, scope)
		}
		return err
	})
	workAppCheck(t, "usage-original-stop-driver", err)
}

func (v *usageLifecycleApp) seedUsage(t *testing.T) {
	t.Helper()
	scope, err := id.InProject(v.a)
	workAppCheck(t, "usage-secret-scope", err)
	command, err := f.NewCommandIdentity("secret", []string{v.a.String(), v.actor.Details().UserID}, "create", f.IdempotencyKey(guardID[struct{}](t).String()))
	workAppCheck(t, "usage-secret-command", err)
	material, err := sc.NewSecretMaterial([]byte("usage-policy-deny-private-material"))
	workAppCheck(t, "usage-secret-material", err)
	defer material.Destroy()
	secrets, ok := v.root.owned.secret().(*secret.Service)
	if !ok {
		t.Fatal("original default Secret service absent")
	}
	written, err := secrets.ExecuteWrite(v.ctx, sc.WriteRequest{Actor: v.actor, Scope: scope, Identity: command, Kind: sc.Create, Purpose: sc.Model, Value: material})
	workAppCheck(t, "usage-original-secret-create", err)
	ref := written.Metadata.CredentialRef.Details().ID
	v.seedWithProvider(t, v.address+usageAppNoDispatch, &ref)
}
func (v *usageLifecycleApp) advance(t *testing.T) ec.ExecutionAdvance {
	t.Helper()
	d := ec.AssociatedDispatch{ExecutionID: v.execution.ID, AgentID: v.agent, Key: v.launchRequest.Meta.IdempotencyKey, Digest: v.digest, DispatchID: v.dispatch.Summary().ID.String()}
	state, err := v.runtime.executor.Advance(v.ctx, v.a, d)
	// A policy deny is a genuine terminal error, never a fabricated success.
	if err != nil && state.Phase != ec.ExecutionTerminal && state.Phase != ec.ExecutionRetained {
		workAppCheck(t, "usage-original-associated-advance", err)
	}
	return state
}

// This only waits for persisted Usage/Model facts and the associated body's
// technical close, so its business snapshot is stable. It does not claim that the
// Execution worker's outer tail has returned. Advance is called once: another
// call after a terminal job is forgotten could start a new worker. Usage's own
// actual return is proved below through the original token and Stop report;
// the original App cleanup owns the remaining Execution/root return.
func (v *usageLifecycleApp) waitUsageFacts(t *testing.T) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		var ready bool
		err := v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*)=1 AND bool_and(retired AND phase='failed') FROM agenteam_model.calls WHERE project_id=$1::uuid)
 AND (SELECT count(*)=1 AND bool_and(final_status='failed') FROM agenteam_model.invocations WHERE project_id=$1::uuid)
 AND (SELECT count(*)=1 AND bool_and(phase='business_closed') FROM agenteam_model.usage_project_calls WHERE project_id=$1::uuid AND lane='invocation')
 AND (SELECT count(*)=1 AND bool_and(phase='business_closed') FROM agenteam_execution.project_calls WHERE project_id=$1::uuid AND lane='associated')`, v.a.String()).Scan(&ready)
		workAppCheck(t, "usage-original-persisted-terminal-facts", err)
		if ready {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("original policy-deny Usage/Model terminal facts were not observed")
		case <-tick.C:
		}
	}
}
func (v *usageLifecycleApp) facts(t *testing.T) {
	t.Helper()
	var valid bool
	err := v.admin.QueryRow(v.ctx, `SELECT
 (SELECT count(*)=1 AND bool_and(dispatch='not_sent' AND final_status='failed' AND usage_source='unknown' AND input_tokens IS NULL AND output_tokens IS NULL AND total_tokens IS NULL) FROM agenteam_model.invocations WHERE project_id=$1::uuid AND execution_id=$2::uuid)
 AND EXISTS(SELECT 1 FROM agenteam_model.invocation_observations WHERE project_id=$1::uuid)
 AND (SELECT count(*)=1 FROM agenteam_model.execution_usage_summaries WHERE project_id=$1::uuid AND execution_id=$2::uuid)
 AND (SELECT count(*)=1 AND bool_and(retired AND phase='failed') FROM agenteam_model.calls WHERE project_id=$1::uuid)
 AND EXISTS(SELECT 1 FROM agenteam_audit.audit_records WHERE project_id=$1::uuid AND producer='outbound.access' AND action='outbound.access.deny')`, v.a.String(), v.execution.ID.String()).Scan(&valid)
	workAppCheck(t, "usage-real-policy-deny-facts", err)
	if !valid || v.dispatches.Load() != 0 {
		t.Fatal("real policy deny did not persist canonical Usage, or dispatched HTTP")
	}
}
func (v *usageLifecycleApp) other(t *testing.T) {
	t.Helper()
	page, err := v.bound.reader.List(v.ctx, v.actor, uc.Query{Filter: uc.Filter{ProjectID: v.b}, Limit: 10})
	workAppCheck(t, "usage-other-project-reader", err)
	if len(page.Items) != 0 {
		t.Fatal("other Project Usage changed")
	}
}
func (v *usageLifecycleApp) accept(t *testing.T) {
	t.Helper()
	p, err := v.projects.service.GetProject(v.ctx, v.actor, v.a)
	workAppCheck(t, "usage-original-project-read", err)
	var username string
	user, _ := f.UserLock(v.actor.Details().UserID)
	readCause, _ := f.NewRecoveryCause("usage-app-owner-route", guardID[struct{}](t).String(), v.a.String())
	result := v.tracked.WithinTx(v.ctx, readCause, func(ctx context.Context, tx f.Tx) error {
		if err := v.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Shared}}); err != nil {
			return err
		}
		route, err := v.accounts.CurrentUserRouteInTx(ctx, tx, v.actor)
		username = route.Username
		return err
	})
	if result.State() != f.Committed {
		t.Fatal("original current owner route failed", result.Fault())
	}
	path, err := pc.NormalizeProjectPath(username, p.Name)
	workAppCheck(t, "usage-delete-path", err)
	_, unbound := v.projects.service.BeginDeleteProject(v.ctx, v.actor, executionSchedulerAppMeta(t, &p.Version), v.a, pc.DeleteProjectRequest{NormalizedCurrentPath: path, Permanent: true})
	var unavailable *f.Fault
	if !errors.As(unbound, &unavailable) || unavailable.Code != f.DependencyUnbound {
		t.Fatal("default Registry unexpectedly accepted Delete", unbound)
	}
	r, err := v.acceptor.BeginDeleteProject(v.ctx, v.actor, executionSchedulerAppMeta(t, &p.Version), v.a, pc.DeleteProjectRequest{NormalizedCurrentPath: path, Permanent: true})
	workAppCheck(t, "usage-formal-delete-acceptance", err)
	if r.Operation == nil {
		t.Fatal("formal Usage Delete operation absent")
	}
	v.cause = pc.LifecycleCause{OperationID: r.Operation.ID, Action: pc.Delete, ProjectVersion: r.Operation.ProjectVersion}
	v.scope = pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: v.a}
}
func (v *usageLifecycleApp) stopRun(t *testing.T, want pc.StopState) {
	t.Helper()
	err := v.stop.Run(v.ctx, v.a, v.cause.OperationID)
	workAppCheck(t, "usage-original-stop-return", err)
	if v.report.Details().State != want {
		t.Fatalf("Usage stop state=%s want=%s", v.report.Details().State, want)
	}
}
func (v *usageLifecycleApp) settle(t *testing.T) {
	t.Helper()
	for n := 0; n < 8; n++ {
		err := v.stop.Run(v.ctx, v.a, v.cause.OperationID)
		workAppCheck(t, "usage-original-stop-convergence", err)
		if v.report.Details().State == pc.Stopped {
			v.needsConfirmation = false
			return
		}
	}
	t.Fatal("Usage original owners did not stop in the bounded explicit rounds")
}
func usageAppWait(t *testing.T, done <-chan struct{}, stage string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal(stage + " did not actually return")
	}
}
func (v *usageLifecycleApp) start(fn func() error) *usageAppCall {
	c := &usageAppCall{done: make(chan struct{})}
	v.calls = append(v.calls, c)
	go func() { defer close(c.done); c.err = fn() }()
	return c
}
func usageAppObserve(t *testing.T, h *usageAppHook) usageAppObservation {
	t.Helper()
	select {
	case o := <-h.observed:
		if o.result.State() != f.Committed {
			t.Fatal("original observed Store did not commit")
		}
		return o
	case <-time.After(5 * time.Second):
		t.Fatal("original Usage Store return barrier was not reached")
	}
	return usageAppObservation{}
}
func usageAppOwner(owner string, project id.ProjectID) func(f.TransactionCause) bool {
	return func(c f.TransactionCause) bool {
		d := c.Details()
		return d.Kind == f.RecoveryCause && d.Owner == owner && (project.Validate() != nil || d.CheckpointRef == project.String())
	}
}
func usageAppLane(lane string) func(context.Context, postgres.SQLExecutor, f.TransactionCause, int64, int64) (bool, error) {
	return func(ctx context.Context, x postgres.SQLExecutor, c f.TransactionCause, _, _ int64) (bool, error) {
		var exists bool
		err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls WHERE id=$1::uuid AND lane=$2 AND phase='business_closed')`, c.Details().RecoveryRunID, lane).Scan(&exists)
		return exists, err
	}
}
func usageAppAny(context.Context, postgres.SQLExecutor, f.TransactionCause, int64, int64) (bool, error) {
	return true, nil
}

func usageAppWholeReturn(t *testing.T) {
	v := newUsageLifecycleApp(t)
	v.seedUsage(t)
	h := &usageAppHook{match: usageAppOwner("usage-call-return", v.a), selectRow: usageAppLane("invocation")}
	v.tracked.arm(h)
	v.advance(t)
	observed := usageAppObserve(t, h)
	v.facts(t)
	select {
	case <-observed.returned:
		t.Fatal("original Model wholecall return escaped its Usage Store")
	default:
	}
	v.accept(t)
	v.stopRun(t, pc.StopPending)
	v.other(t)
	v.requireGuard(t)
	h.open()
	usageAppWait(t, observed.returned, "original Model Usage technical return")
	v.waitUsageFacts(t)
	v.settle(t)
	if v.dispatches.Load() != 0 {
		t.Fatal("policy-denied Runtime dispatched a request")
	}
	t.Log("USAGE_DEFAULT_APP_RETURN original_default_reader_and_runtime_borrower=true real_policy_deny=true invocation_observations_summary=true wholecall_store_held_pending=true other_project=true actual_return_before_stopped=true http_dispatches=0 default_registry_unbound=true")
}

func usageAppSameCause(a, b f.TransactionCause) bool {
	x, y := a.Details(), b.Details()
	return x.Kind == y.Kind && x.Owner == y.Owner && x.RecoveryRunID == y.RecoveryRunID && x.CheckpointRef == y.CheckpointRef && x.JobType == y.JobType && x.JobID == y.JobID && x.JobAttemptID == y.JobAttemptID && x.Primary.Canonical() == y.Primary.Canonical() && len(x.Related) == 0 && len(y.Related) == 0 && x.EventID == y.EventID && x.HandlerName == y.HandlerName
}
func usageAppUnknownError(t *testing.T, err error, cause f.TransactionCause) *usage.UnknownError {
	t.Helper()
	var original *usage.UnknownError
	if !errors.As(err, &original) || original.AttemptID().Validate() != nil || !usageAppSameCause(original.Cause(), cause) {
		t.Fatal("original Usage Unknown cause/attempt absent", err)
	}
	return original
}
func (v *usageLifecycleApp) snapshot(t *testing.T) []byte {
	t.Helper()
	var raw []byte
	err := v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_model.invocations t WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY invocation_id,sequence),'[]'::jsonb) FROM agenteam_model.invocation_observations t WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY execution_id),'[]'::jsonb) FROM agenteam_model.execution_usage_summaries t WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_model.usage_project_calls t WHERE project_id=$1::uuid))`, v.a.String()).Scan(&raw)
	workAppCheck(t, "usage-complete-four-table-snapshot", err)
	return raw
}
func usageAppUnknown(t *testing.T) {
	v := newUsageLifecycleApp(t)
	v.seedUsage(t)
	v.advance(t)
	v.waitUsageFacts(t)
	v.facts(t)
	h := &usageAppHook{match: usageAppOwner("usage-call-return", v.a), selectRow: usageAppLane("summary_get"), mask: true}
	v.tracked.arm(h)
	v.needsConfirmation = true
	reader := v.start(func() error {
		_, err := v.bound.reader.GetExecutionSummary(v.ctx, v.actor, v.a, v.execution.ID)
		return err
	})
	o := usageAppObserve(t, h)
	h.open()
	usageAppWait(t, reader.done, "original Usage reader Unknown")
	original := usageAppUnknownError(t, reader.err, o.cause)
	if original.AttemptID() != o.masked.AttemptID() {
		t.Fatal("reader did not retain the exact masked original attempt")
	}
	// Rebuild is the business Unknown covered here. It reads the real canonical
	// invocations and summary; no Model mutation or lost network ACK is claimed.
	rh := &usageAppHook{match: usageAppOwner("model.usage.summary", id.ProjectID{}), selectRow: usageAppAny, mask: true}
	v.tracked.arm(rh)
	rebuild := v.start(func() error {
		_, err := v.bound.reader.RebuildExecutionSummary(v.ctx, v.actor, v.a, v.execution.ID)
		return err
	})
	ro := usageAppObserve(t, rh)
	rh.open()
	usageAppWait(t, rebuild.done, "original Rebuild Unknown")
	business := usageAppUnknownError(t, rebuild.err, ro.cause)
	if business.AttemptID() != ro.masked.AttemptID() {
		t.Fatal("Rebuild did not retain the exact masked original attempt")
	}
	v.accept(t)
	// Owners are traversed by their actual UUID ordering, not creation time.
	causes := []f.TransactionCause{original.Cause(), business.Cause()}
	var confirms []*usageAppHook
	for _, cause := range causes {
		cause := cause
		hook := &usageAppHook{match: func(c f.TransactionCause) bool { return usageAppSameCause(c, cause) }, selectRow: usageAppAny}
		v.tracked.arm(hook)
		confirms = append(confirms, hook)
	}
	// If Rebuild sorts first, its confirmed running owner needs one separate
	// close before the reader's confirmation. Two reads plus that one close
	// are the complete path; UUID generation order is not a contract.
	confirmed := 0
	confirmationBudget := time.NewTimer(5 * time.Second)
	defer confirmationBudget.Stop()
	for n := 0; n < 3 && confirmed < 2; n++ {
		before := v.snapshot(t)
		call := v.start(func() error { return v.stop.Run(v.ctx, v.a, v.cause.OperationID) })
		var selected *usageAppHook
		var observed usageAppObservation
		select {
		case observed = <-confirms[0].observed:
			selected = confirms[0]
		case observed = <-confirms[1].observed:
			selected = confirms[1]
		case <-call.done:
			workAppCheck(t, "usage-confirmed-owner-explicit-close", call.err)
			if confirmed != 1 || v.report.Details().State != pc.StopPending {
				t.Fatal("unexpected round between original confirmations")
			}
			continue
		case <-confirmationBudget.C:
			t.Fatal("original Unknown confirmation not reached")
		}
		if observed.result.State() != f.Committed || observed.after-observed.before != 0 || !reflect.DeepEqual(before, v.snapshot(t)) {
			t.Fatal("Usage confirmation wrote or changed its complete original image")
		}
		v.other(t)
		select {
		case <-call.done:
			t.Fatal("Stop returned before original confirmation Store")
		default:
		}
		selected.open()
		usageAppWait(t, call.done, "original read-only Usage confirmation")
		workAppCheck(t, "usage-confirmation-return", call.err)
		if v.report.Details().State != pc.StopPending {
			t.Fatal("confirmation advanced its owner in the same explicit Stop")
		}
		confirmed++
	}
	if confirmed != 2 {
		t.Fatal("both original readonly confirmations were not observed")
	}
	if original.AttemptID().Validate() != nil || business.AttemptID().Validate() != nil {
		t.Fatal("original Unknown identity changed")
	}
	v.settle(t)
	t.Log("USAGE_DEFAULT_APP_UNKNOWN original_reader_return_unknown=true original_rebuild_business_unknown=true same_tx_zero_writes=true complete_snapshot_equal=true original_confirmation_return=true next_explicit_stop=true no_model_mutation_unknown_pg=true no_network_fault=true")
}

func (v *usageLifecycleApp) cleanupEntry(t *testing.T) {
	t.Helper()
	v.accept(t)
	v.settle(t)
	key, _ := f.ProjectLock(v.a.String())
	cause, _ := f.NewRecoveryCause("usage-app-controlled-predecessors", guardID[struct{}](t).String(), v.a.String())
	r := v.tracked.WithinTx(v.ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := v.tracked.AcquireAll(ctx, tx, []f.LockRequest{{Key: key, Mode: f.Exclusive}}); err != nil {
			return err
		}
		x, err := v.tracked.InTx(tx)
		if err != nil {
			return err
		}
		// Explicit missing-owner prerequisites only. All Usage business rows
		// above were written by the original default Runtime and real providers.
		if _, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET stop_state='stopped',version=version+1 WHERE operation_id=$1::uuid`, v.cause.OperationID.String()); err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_participants SET cleanup_state='completed',version=version+1 WHERE operation_id=$1::uuid AND participant_name='execution'`, v.cause.OperationID.String()); err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_project.lifecycle_operations SET state='cleaning',cleanup_stage='domains',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid AND state='stopping'`, v.cause.OperationID.String())
		return err
	})
	if r.State() != f.Committed {
		t.Fatal("controlled prerequisite handoff failed", r.State(), r.Fault())
	}
	var err error
	v.cleaner, err = project.NewLifecycleCleanupDriverForUsage(v.tracked, v.bound.lifecycle, v.processes, v.bound.usageCalls.cleanup)
	workAppCheck(t, "usage-original-fixed-cleaner", err)
}

func (v *usageLifecycleApp) outsideCleanupSnapshot(t *testing.T, originalBCalls []string) []byte {
	t.Helper()
	var raw []byte
	err := v.admin.QueryRow(v.ctx, `SELECT jsonb_build_array(
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_model.calls t WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_model.runtime_attempts t WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_execution.executions t WHERE project_id=$1::text),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_work.tasks t WHERE project_id=$1::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_model.invocations t WHERE project_id=$2::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY invocation_id,sequence),'[]'::jsonb) FROM agenteam_model.invocation_observations t WHERE project_id=$2::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY execution_id),'[]'::jsonb) FROM agenteam_model.execution_usage_summaries t WHERE project_id=$2::uuid),
 (SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]'::jsonb) FROM agenteam_model.usage_project_calls t WHERE project_id=$2::uuid AND id=ANY($3::uuid[])))`, v.a.String(), v.b.String(), originalBCalls).Scan(&raw)
	workAppCheck(t, "usage-preserved-domains-snapshot", err)
	return raw
}

func usageAppCleanup(t *testing.T) {
	v := newUsageLifecycleApp(t)
	v.seedUsage(t)
	v.advance(t)
	v.waitUsageFacts(t)
	v.facts(t)
	v.other(t)
	v.cleanupEntry(t)
	var originalBCalls []string
	workAppCheck(t, "usage-original-other-project-call-ids", v.admin.QueryRow(v.ctx, `SELECT ARRAY(SELECT id::text FROM agenteam_model.usage_project_calls WHERE project_id=$1::uuid ORDER BY id)`, v.b.String()).Scan(&originalBCalls))
	if len(originalBCalls) == 0 {
		t.Fatal("other Project's original reader ledger absent")
	}
	preservedBefore := v.outsideCleanupSnapshot(t, originalBCalls)
	h := &usageAppHook{match: usageAppOwner("usage-project-cleanup", v.a), mask: true, selectRow: func(_ context.Context, _ postgres.SQLExecutor, _ f.TransactionCause, before, after int64) (bool, error) {
		return after > before, nil
	}}
	v.tracked.arm(h)
	call := v.start(func() error { return v.cleaner.Run(v.ctx, v.a, v.cause.OperationID) })
	o := usageAppObserve(t, h)
	h.open()
	usageAppWait(t, call.done, "original cleanup ACKUnknown")
	var problem *f.Fault
	if !errors.As(call.err, &problem) || problem.CommitState != f.Unknown {
		t.Fatal("original Usage cleanup Unknown absent", call.err)
	}
	before := v.snapshot(t)
	confirm := &usageAppHook{match: func(c f.TransactionCause) bool { return usageAppSameCause(c, o.cause) }, selectRow: usageAppAny}
	v.tracked.arm(confirm)
	read := v.start(func() error { return v.cleaner.Run(v.ctx, v.a, v.cause.OperationID) })
	observed := usageAppObserve(t, confirm)
	if observed.after-observed.before != 0 || !reflect.DeepEqual(before, v.snapshot(t)) {
		t.Fatal("Usage cleanup confirmation changed durable image or wrote rows")
	}
	other, err := project.NewLifecycleCleanupDriverForUsage(v.tracked, v.bound.lifecycle, v.processes, v.bound.usageCalls.cleanup)
	workAppCheck(t, "usage-second-driver", err)
	err = other.Run(v.ctx, v.a, v.cause.OperationID)
	var busy *f.Fault
	if !errors.As(err, &busy) || busy.Code != f.ResourceBusy {
		t.Fatal("same-Life driver entered before original Usage confirmation returned", err)
	}
	other.Stop()
	workAppCheck(t, "usage-second-driver-drain", other.Drain(v.ctx))
	v.other(t)
	confirm.open()
	usageAppWait(t, read.done, "original cleanup confirmation")
	workAppCheck(t, "usage-cleanup-confirmation-return", read.err)
	var complete bool
	for n := 0; n < 5; n++ {
		workAppCheck(t, "usage-next-explicit-cleanup", v.cleaner.Run(v.ctx, v.a, v.cause.OperationID))
		workAppCheck(t, "usage-cleanup-checkpoint", v.admin.QueryRow(v.ctx, `SELECT cleanup_state='completed' FROM agenteam_project.lifecycle_participants WHERE operation_id=$1::uuid AND participant_name='usage'`, v.cause.OperationID.String()).Scan(&complete))
		if complete {
			break
		}
	}
	var empty, preserved bool
	workAppCheck(t, "usage-exact-final-tables", v.admin.QueryRow(v.ctx, `SELECT
 NOT EXISTS(SELECT 1 FROM agenteam_model.invocations WHERE project_id=$1::uuid) AND NOT EXISTS(SELECT 1 FROM agenteam_model.invocation_observations WHERE project_id=$1::uuid) AND NOT EXISTS(SELECT 1 FROM agenteam_model.execution_usage_summaries WHERE project_id=$1::uuid) AND NOT EXISTS(SELECT 1 FROM agenteam_model.usage_project_calls WHERE project_id=$1::uuid),
 EXISTS(SELECT 1 FROM agenteam_model.calls WHERE project_id=$1::uuid) AND EXISTS(SELECT 1 FROM agenteam_execution.executions WHERE project_id=$1::text) AND EXISTS(SELECT 1 FROM agenteam_project.projects WHERE id=$1::uuid)`, v.a.String()).Scan(&empty, &preserved))
	if !complete || !empty || !preserved || v.dispatches.Load() != 0 || !reflect.DeepEqual(preservedBefore, v.outsideCleanupSnapshot(t, originalBCalls)) {
		t.Fatal("Usage cleanup crossed its domain or failed to retire its exact history")
	}
	v.other(t)
	t.Log("USAGE_DEFAULT_APP_CLEANUP real_invocations_observations_summaries=true closed_ledger_removed=true original_unknown_same_tx_zero_writes=true original_return_before_next_run=true same_life_busy=true other_project=true model_execution_history_preserved=true controlled_execution_completed=true no_default_registry_or_full_delete=true")
}
