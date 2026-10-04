//go:build integration

package outbox_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

type lifecycleAuthority struct {
	base               *authority
	issuer             oc.PlanIssuer
	currentKey, newKey foundation.LockKey
	newPlans           atomic.Int64
}

func (a *lifecycleAuthority) validateStopEvent(ctx context.Context, tx foundation.Tx, actor identity.Actor, project string) error {
	d := actor.Details()
	if d.Kind != identity.Service || d.ServiceName != identity.ProjectLifecycle || d.ProjectID != project {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	x, _ := a.base.store.InTx(tx)
	var allowed bool
	if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM outbox_fixture.lifecycle l JOIN outbox_fixture.authority a ON a.id=l.project_id WHERE l.project_id=$1 AND l.actor_cause=$2 AND l.action='delete' AND NOT a.active)`, project, d.CauseRef).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	return nil
}

type lifecycleProducer struct{ authority *lifecycleAuthority }

func (p lifecycleProducer) DiscoverAppend(ctx context.Context, actor identity.Actor, e event.Summary) (oc.Dependencies, error) {
	return p.authority.base.DiscoverAppend(ctx, actor, e)
}
func (p lifecycleProducer) ValidateAppendInTx(ctx context.Context, tx foundation.Tx, actor identity.Actor, e event.Summary, d oc.Dependencies, stage oc.Stage) error {
	if stage == oc.NewFact && actor.Details().Kind == identity.Service {
		if err := p.authority.base.ValidateAppendInTx(ctx, tx, actor, e, d, oc.CurrentAccess); err != nil {
			return err
		}
		return p.authority.validateStopEvent(ctx, tx, actor, e.Header.Scope.ProjectID.String())
	}
	return p.authority.base.ValidateAppendInTx(ctx, tx, actor, e, d, stage)
}

func (a *lifecycleAuthority) Discover(ctx context.Context, r oc.ProjectRequest) (oc.Dependencies, error) {
	if r.Details().Kind == oc.LifecycleProject {
		b, e := oc.LifecycleBinding(r)
		if e != nil {
			return oc.Dependencies{}, e
		}
		return oc.NewDependencies(a.issuer, b, nil, nil)
	}
	if r.Details().Kind == oc.RequeueProject {
		b, e := oc.RequeueBinding(r)
		if e != nil {
			return oc.Dependencies{}, e
		}
		key, mode := a.currentKey, foundation.Shared
		if r.Details().Stage == oc.NewFact {
			a.newPlans.Add(1)
			key, mode = a.newKey, foundation.Exclusive
		}
		return oc.NewDependencies(a.issuer, b, []foundation.LockRequest{{Key: key, Mode: mode}}, nil)
	}
	return a.base.Discover(ctx, r)
}
func (a *lifecycleAuthority) ValidateInTx(ctx context.Context, tx foundation.Tx, r oc.ProjectRequest, d oc.Dependencies) error {
	v := r.Details()
	if v.Kind == oc.RequeueProject {
		b, _ := oc.RequeueBinding(r)
		if !d.Matches(a.issuer, b) {
			return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
		}
		locks := []foundation.LockRequest{{Key: a.currentKey, Mode: foundation.Shared}}
		if v.Stage == oc.NewFact {
			locks = append(locks, foundation.LockRequest{Key: a.newKey, Mode: foundation.Exclusive})
		}
		if e := a.base.store.RequireHeldLocks(ctx, tx, locks); e != nil {
			return e
		}
		x, _ := a.base.store.InTx(tx)
		var active, visible bool
		if e := x.QueryRow(ctx, `SELECT active,visible FROM outbox_fixture.authority WHERE id=$1`, v.ProjectID.String()).Scan(&active, &visible); e != nil {
			return e
		}
		if !visible {
			return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
		}
		if v.Stage == oc.NewFact && !active {
			return foundation.NewFault(foundation.ProjectNotActive, foundation.NotStarted)
		}
		return nil
	}
	if v.Kind != oc.LifecycleProject {
		if v.Kind == oc.AppendProject && v.Stage == oc.NewFact && v.Actor.Details().Kind == identity.Service {
			if !d.Matches(a.base.issuer, projectBinding(r)) {
				return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
			}
			return a.validateStopEvent(ctx, tx, v.Actor, v.ProjectID.String())
		}
		if v.Kind == oc.DeliverProject && v.Delivery.Effect == oc.DomainIngress {
			x, _ := a.base.store.InTx(tx)
			var active bool
			if err := x.QueryRow(ctx, `SELECT active FROM outbox_fixture.authority WHERE id=$1`, v.ProjectID.String()).Scan(&active); err != nil {
				return err
			}
			if !active {
				return foundation.NewFault(foundation.ProjectNotActive, foundation.NotStarted)
			}
		}
		return a.base.ValidateInTx(ctx, tx, r, d)
	}
	b, _ := oc.LifecycleBinding(r)
	if !d.Matches(a.issuer, b) {
		return foundation.NewFault(foundation.InvalidArgument, foundation.NotStarted)
	}
	key, _ := foundation.ProjectLock(v.ProjectID.String())
	if e := a.base.store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{{Key: key, Mode: foundation.Shared}}); e != nil {
		return e
	}
	x, _ := a.base.store.InTx(tx)
	var operation, action, actorCause string
	var version int64
	var others, active bool
	if e := x.QueryRow(ctx, `SELECT l.operation_id::text,l.action,l.version,l.actor_cause,l.others,a.active FROM outbox_fixture.lifecycle l JOIN outbox_fixture.authority a ON a.id=l.project_id WHERE project_id=$1`, v.ProjectID.String()).Scan(&operation, &action, &version, &actorCause, &others, &active); e != nil {
		return e
	}
	c := v.Lifecycle.Details()
	if operation != c.OperationID.String() || action != string(c.Action) || version != int64(c.ProjectVersion) || actorCause != v.Actor.Details().CauseRef || active {
		return foundation.NewFault(foundation.Forbidden, foundation.NotStarted)
	}
	if v.LifecycleStep == oc.LifecycleCleanup && !others {
		return foundation.NewFault(foundation.ResourceBusy, foundation.NotStarted)
	}
	return nil
}
func lifecycleService(t *testing.T, f *fixture, processes oc.ProcessAuthority, stores ...outbox.Store) (*outbox.Service, *lifecycleAuthority) {
	t.Helper()
	_, _ = administration(t, f, f.store, false)
	f.sql(t, `CREATE TABLE outbox_fixture.lifecycle(project_id uuid PRIMARY KEY,operation_id uuid NOT NULL,action text NOT NULL,version bigint NOT NULL,actor_cause text NOT NULL,others boolean NOT NULL DEFAULT false)`)
	// Deliberately reverse the two aggregate identities: a second late AcquireAll
	// cannot safely replace the required pre-collected union.
	low, _ := foundation.AggregateLock(foundation.ExecutionAggregate, "01900000-0000-7000-8000-000000000001")
	high, _ := foundation.AggregateLock(foundation.ExecutionAggregate, "01900000-0000-7000-8000-000000000002")
	a := &lifecycleAuthority{base: f.auth, issuer: oc.NewPlanIssuer(), currentKey: high, newKey: low}
	keys, e := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if e != nil {
		t.Fatal(e)
	}
	auth := adminAuthority{f}
	auditor, e := audit.New(f.store, keys, audit.Authorizations{Sessions: auth, System: auth, Projects: auth})
	if e != nil {
		t.Fatal(e)
	}
	var storage outbox.Store = lifecycleDiagnosticStore{f.store, t}
	if len(stores) > 0 {
		storage = stores[0]
	}
	svc, e := outbox.New(storage, f.cat, outbox.Authorizations{Producers: map[event.StableName]oc.ProducerAuthority{"fixture": lifecycleProducer{a}}, Projects: a, Processes: processes, Sessions: auth, System: auth, Audit: auditor, Cursors: keys})
	if e != nil {
		t.Fatal(e)
	}
	return svc, a
}
func lifecycleCause(t *testing.T, f *fixture, action oc.LifecycleAction, version foundation.Version) (identity.Actor, oc.LifecycleCause) {
	t.Helper()
	project, _ := foundation.ParseID[identity.Project](f.project.String())
	c, e := oc.NewLifecycleCause(oc.LifecycleDetails{ProjectID: project, OperationID: id[oc.LifecycleOperation](t), Action: action, ProjectVersion: version})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(c.Details())
	role, _ := identity.RegisterService(identity.ProjectLifecycle)
	scope, _ := identity.InProject(project)
	actor, e := role.Actor(oc.DigestBytes(raw).String(), scope)
	if e != nil {
		t.Fatal(e)
	}
	f.sql(t, `INSERT INTO outbox_fixture.lifecycle(project_id,operation_id,action,version,actor_cause) VALUES($1,$2,$3,$4,$5) ON CONFLICT(project_id) DO UPDATE SET operation_id=excluded.operation_id,action=excluded.action,version=excluded.version,actor_cause=excluded.actor_cause,others=false`, project.String(), c.Details().OperationID.String(), string(action), int64(version), actor.Details().CauseRef)
	f.sql(t, `UPDATE outbox_fixture.authority SET active=false WHERE id=$1`, project.String())
	return actor, c
}
func TestOutboxLifecycleUnattemptedArchiveRestoreAndExplicitRequeue(t *testing.T) {
	f := newFixture(t)
	svc, a := lifecycleService(t, f, f.auth)
	ingress, canonical := f.handler("lifecycle.ingress"), f.handler("lifecycle.canonical")
	ingressDef := ingress.definition(1)
	ingressDef.Effect = oc.DomainIngress
	for _, d := range []oc.HandlerDefinition{ingressDef, canonical.definition(1)} {
		if _, e := svc.RegisterHandler(ctxFor(t), d); e != nil {
			t.Fatal(e)
		}
	}
	e := distinctEvent(t, f, "before archive")
	_, commit := f.append(t, svc, f.store, f.actor, e)
	state(t, commit, foundation.Committed)
	delivery := f.delivery(t, e, string(ingress.name))
	actor, cause := lifecycleCause(t, f, oc.ArchiveProject, 2)
	report, err := svc.RequestStop(ctxFor(t), actor, cause)
	if err != nil || !report.Stopped {
		t.Fatalf("stop without other participant completion: %v %v", report, err)
	}
	if _, err = svc.InspectStop(ctxFor(t), actor, cause); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE id=$1 AND phase='dead_letter' AND safe_reason='project_stopped' AND current_attempt_id IS NULL AND fence=0 AND cycle_attempts=0 AND lifetime_attempts=0`, delivery.String()) != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts`) != 0 || f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE handler_id=$1 AND phase='pending'`, string(canonical.name)) != 1 {
		t.Fatal("archive invented attempts or stopped canonical projection")
	}
	project, _ := foundation.ParseID[identity.Project](f.project.String())
	scope, _ := identity.InProject(project)
	page, err := svc.QueryDiagnostics(ctxFor(t), f.actor, scope, oc.DiagnosticsFilter{HandlerID: ingress.name}, foundation.PageRequest{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Summary.DeadLetterDeliveries != 1 || len(page.Summary.HandlerLatency) != 0 || len(page.Summary.RecentErrors) != 1 || page.Summary.RecentErrors[0].AttemptID != nil {
		t.Fatal("zero-attempt statistics fabricated callback facts")
	}
	_, err = svc.Requeue(ctxFor(t), f.actor, commandMeta(t), delivery, 2, oc.OperatorRetry)
	code(t, err, foundation.ProjectNotActive)
	f.sql(t, `UPDATE outbox_fixture.authority SET active=true WHERE id=$1`, f.project.String())
	f.sql(t, `UPDATE outbox_fixture.lifecycle SET version=3 WHERE project_id=$1`, f.project.String())
	_, err = svc.RequestStop(ctxFor(t), actor, cause)
	code(t, err, foundation.Forbidden)
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE id=$1 AND phase='dead_letter'`, delivery.String()) != 1 {
		t.Fatal("Restore revived terminal delivery")
	}
	meta := commandMeta(t)
	receipt, err := svc.Requeue(ctxFor(t), f.actor, meta, delivery, 2, oc.OperatorRetry)
	if err != nil || receipt.Cycle != 1 || receipt.Version != 3 {
		t.Fatalf("explicit zero-attempt redrive: %+v %v", receipt, err)
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts`) != 0 || f.count(t, `SELECT count(*) FROM agenteam_audit.audit_records WHERE action='outbox.delivery.requeue'`) != 1 {
		t.Fatal("requeue fabricated attempt or missed Audit")
	}
	plans := a.newPlans.Load()
	f.sql(t, `UPDATE outbox_fixture.authority SET active=false WHERE id=$1`, f.project.String())
	meta.RequestID = id[foundation.Request](t)
	again, err := svc.Requeue(ctxFor(t), f.actor, meta, delivery, 2, oc.OperatorRetry)
	if err != nil || again != receipt || a.newPlans.Load() != plans {
		t.Fatal("historical receipt required new-fact planning", err)
	}
	f.sql(t, `UPDATE outbox_fixture.authority SET active=true WHERE id=$1`, f.project.String())
	f.svc = svc
	r := startRuntime(t, f, []oc.HandlerDefinition{ingressDef, canonical.definition(1)}, outbox.Options{PollInterval: time.Millisecond})
	waitOutbox(t, func() bool {
		return f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE phase='succeeded'`) == 2
	})
	r.StopClaims()
	if err = r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE id=$1 AND fence=1 AND cycle_attempts=1 AND lifetime_attempts=1`, delivery.String()) != 1 || ingress.calls.Load() != 1 {
		t.Fatal("first actual claim did not start at fence/attempt one")
	}
}
func TestOutboxLifecycleDeleteGateBatchFairnessAndIsolation(t *testing.T) {
	f := newFixture(t)
	svc, _ := lifecycleService(t, f, f.auth)
	h := f.handler("cleanup.handler")
	if _, e := svc.RegisterHandler(ctxFor(t), h.definition(1)); e != nil {
		t.Fatal(e)
	}
	var first, last event.Event
	for i := 0; i < 105; i++ {
		e := distinctEvent(t, f, fmt.Sprint("private cleanup payload ", i))
		if i == 0 {
			first = e
		}
		last = e
		_, c := f.append(t, svc, f.store, f.actor, e)
		state(t, c, foundation.Committed)
		if i < 100 {
			f.claim(t, f.delivery(t, e, string(h.name)))
		}
	}
	system := f.event(t, false, 1, "other scope")
	_, c := f.append(t, svc, f.store, f.actor, system)
	state(t, c, foundation.Committed)
	actor, cause := lifecycleCause(t, f, oc.DeleteProject, 2)
	stop, err := svc.RequestStop(ctxFor(t), actor, cause)
	if err != nil || stop.Stopped || stop.Pending != 105 {
		t.Fatalf("protected stop: %+v %v", stop, err)
	}
	_, err = svc.Cleanup(ctxFor(t), actor, cause)
	code(t, err, foundation.ResourceBusy)
	f.sql(t, `UPDATE outbox_fixture.lifecycle SET others=true WHERE project_id=$1`, f.project.String())
	report, err := svc.Cleanup(ctxFor(t), actor, cause)
	if err != nil {
		t.Fatal(err)
	}
	// Stop already advanced the persisted scan through the protected first page;
	// Cleanup must reach the unprotected suffix, without claiming foreign death.
	if report.Removed != 5 || report.Remaining != 100 || report.Completed {
		t.Fatalf("protected prefix starved suffix: %+v", report)
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1`, first.Header().EventID.String()) != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1`, last.Header().EventID.String()) != 0 || f.count(t, `SELECT count(*) FROM agenteam_outbox.events WHERE id=$1`, system.Header().EventID.String()) != 1 {
		t.Fatal("cleanup proof or scope violated")
	}
	// Even a current active gate supplied by a changed upstream state cannot
	// reopen the local irreversible delete gate.
	f.sql(t, `UPDATE outbox_fixture.authority SET active=true WHERE id=$1`, f.project.String())
	_, c = f.append(t, svc, f.store, f.actor, distinctEvent(t, f, "late"))
	state(t, c, foundation.NotCommitted)
	code(t, c.Fault(), foundation.ResourceDeleted)
}

// Failure diagnostics expose only fixed error codes and SQL source positions,
// never a query, payload, credentials, or PostgreSQL's value-bearing detail.
type lifecycleDiagnosticStore struct {
	*postgres.Store
	t *testing.T
}

func (s lifecycleDiagnosticStore) WithinTx(ctx context.Context, c foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	r := s.Store.WithinTx(ctx, c, fn)
	if r.State() != foundation.Committed {
		var pe *postgres.Error
		var pg *pgconn.PgError
		var code postgres.Code
		var state string
		var pos int32
		if errors.As(r.Fault(), &pe) {
			code = pe.Code()
			state = pe.SQLState()
		}
		if errors.As(r.Fault(), &pg) {
			pos = pg.Position
		}
		s.t.Logf("lifecycle transaction phase=%s state=%s database_code=%s sqlstate=%s position=%d", c.Details().Owner, r.State(), code, state, pos)
	}
	return r
}

func TestOutboxLifecycleActualClaimWinsStopAndMustJoin(t *testing.T) {
	f := newFixture(t)
	svc, _ := lifecycleService(t, f, f.auth)
	f.svc = svc
	held := &concurrencyState{by: map[event.StableName]int{}, maxBy: map[event.StableName]int{}, entered: make(chan event.StableName, 4), release: make(chan struct{})}
	base := f.handler("stop.claimed")
	handler := &heldHandler{base: base, state: held}
	definition := base.definition(1)
	definition.Effect = oc.DomainIngress
	definition.Handler = handler
	r := startRuntime(t, f, []oc.HandlerDefinition{definition}, outbox.Options{PollInterval: time.Millisecond})
	event := distinctEvent(t, f, "admitted effect")
	_, result := f.append(t, svc, f.store, f.actor, event)
	state(t, result, foundation.Committed)
	select {
	case <-held.entered:
	case <-ctxFor(t).Done():
		t.Fatal("actual handler not admitted")
	}
	released := false
	defer func() {
		if !released {
			close(held.release)
		}
	}()
	delivery := f.delivery(t, event, string(base.name))
	var attempt string
	if err := f.store.QueryRow(ctxFor(t), `SELECT current_attempt_id::text FROM agenteam_outbox.deliveries WHERE id=$1`, delivery.String()).Scan(&attempt); err != nil {
		t.Fatal(err)
	}
	actor, cause := lifecycleCause(t, f, oc.ArchiveProject, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	report, err := svc.RequestStop(ctx, actor, cause)
	cancel()
	if err == nil || report.Stopped {
		t.Fatal("uncooperative callback falsely stopped")
	}
	if f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE id=$1 AND current_attempt_id=$2 AND fence=1 AND phase='processing' AND lifetime_attempts=1`, delivery.String(), attempt) != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.attempts WHERE joined_at IS NOT NULL`) != 0 {
		t.Fatal("claim winner overwritten by zero-attempt stop")
	}
	close(held.release)
	released = true
	// A genuine callback join allows the durable stop to converge. Repeating this
	// operation does not silently increase the original caller's expired budget.
	waitOutbox(t, func() bool {
		report, err = svc.RequestStop(ctxFor(t), actor, cause)
		return err == nil && report.Stopped
	})
	r.StopClaims()
	if err = r.Drain(ctxFor(t)); err != nil {
		t.Fatal(err)
	}
	if f.count(t, `SELECT count(*) FROM outbox_fixture.effects`) != 0 || handler.calls.Load() != 1 || f.count(t, `SELECT count(*) FROM agenteam_outbox.deliveries WHERE id=$1 AND phase='dead_letter' AND current_attempt_id IS NOT NULL`, delivery.String()) != 1 {
		t.Fatal("cancelled callback committed, repeated, or lost its actual attempt")
	}
}
