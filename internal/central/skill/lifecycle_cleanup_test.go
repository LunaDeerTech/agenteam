package skill

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// This store is an explicit transactional double. It exercises the real
// Service/Authority and typed plans; it proves neither SQL nor D05 retirement.
type cleanupTestStore struct {
	*initializationReadStore
	r                            initializationRow
	c                            *cleanupRow
	serving, core, native, gated bool
	work                         map[string][]any
	attempts                     map[string]bool
	unknownAt                    int
	rollbackUnknown              bool
	failDelete                   string
	deleted                      []string
}

func (s *cleanupTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if _, err := s.initializationReadStore.InTx(tx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *cleanupTestStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	var prior *cleanupRow
	if s.c != nil {
		copy := *s.c
		prior = &copy
	}
	row, serving, core, native, gated := s.row, s.serving, s.core, s.native, s.gated
	work := make(map[string][]any, len(s.work))
	for k, v := range s.work {
		work[k] = append([]any(nil), v...)
	}
	attempts := make(map[string]bool, len(s.attempts))
	for k, v := range s.attempts {
		attempts[k] = v
	}
	s.unknown = s.txs+1 == s.unknownAt
	result := s.initializationReadStore.WithinTx(ctx, cause, fn)
	if result.State() == f.NotCommitted || result.State() == f.Unknown && s.rollbackUnknown {
		s.c, s.row, s.serving, s.core, s.native, s.gated, s.work, s.attempts = prior, row, serving, core, native, gated, work, attempts
	}
	return result
}

func (s *cleanupTestStore) QueryRow(ctx context.Context, q string, args ...any) postgres.Row {
	value := func(v ...any) postgres.Row { return skillRowValues{values: v} }
	switch {
	case strings.HasPrefix(q, "SELECT skill_id::text FROM agenteam_skill.installations"):
		return skillRowValues{err: pgx.ErrNoRows}
	case strings.HasPrefix(q, "SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.agent_assignment_heads"):
		return value(true) // This original builtin fixture owns no Agent assignments.
	case strings.HasPrefix(q, "SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.initializations"):
		return value(!s.core && s.c == nil && len(s.work) == 0 && len(s.attempts) == 0)
	case strings.HasPrefix(q, "SELECT NOT EXISTS(SELECT 1 FROM agenteam_skill.work"):
		if strings.Contains(q, "phase<>'joined'") {
			joined := true
			for _, w := range s.work {
				if w[5] != "joined" {
					joined = false
				}
			}
			return value(joined)
		}
		return value(len(s.work) == 0 && len(s.attempts) == 1 && s.attempts[s.r.attempt.String()])
	case strings.HasPrefix(q, "SELECT COALESCE(array_agg"):
		var values []string
		switch {
		case strings.Contains(q, "FROM agenteam_skill.cleanup"):
			if s.c != nil {
				values = append(values, s.c.id.String())
			}
		case strings.Contains(q, "FROM agenteam_skill.work"):
			for k, w := range s.work {
				if w[5] == "joined" {
					values = append(values, k)
				}
			}
		case strings.Contains(q, "FROM agenteam_skill.object_attempts"):
			for k := range s.attempts {
				if k != s.r.attempt.String() {
					values = append(values, k)
				}
			}
		default:
			return skillRowValues{err: errors.New("unexpected discovery")}
		}
		sort.Strings(values)
		if len(values) > 33 {
			values = values[:33]
		}
		return value(values)
	case strings.Contains(q, "FROM agenteam_skill.cleanup"):
		if s.c == nil {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		c := s.c
		return value(c.id.String(), c.project.String(), c.cause.OperationID.String(), int64(c.cause.ProjectVersion), string(c.cause.Action), c.skill.String(), c.revision.String(), c.object.String(), c.upload.String(), string(c.phase), int64(c.version), s.r.created.Time(), s.r.updated.Time())
	case strings.HasPrefix(q, "SELECT s.creation_id"):
		if !s.core || !s.attempts[s.r.attempt.String()] {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		r := s.r
		return value(r.request.CreationID.String(), r.revision.String(), r.bundle.name, AddSkillsNormalizedName, r.bundle.description, true, int64(1), int64(1), s.serving, int64(1), r.object.String(), int64(1), r.created.Time(), r.updated.Time(), r.attempt.String(), stateID[oc.Process](50).String())
	case strings.HasPrefix(q, "SELECT project_id::text FROM agenteam_skill.initializations"):
		if !s.core {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		return value(s.r.request.ProjectID.String())
	case strings.HasPrefix(q, "SELECT process_id::text FROM agenteam_skill.object_attempts"):
		if !s.attempts[args[0].(string)] {
			return skillRowValues{err: pgx.ErrNoRows}
		}
		return value(stateID[oc.Process](50).String())
	case strings.Contains(q, "FROM agenteam_skill.work"):
		if w, ok := s.work[args[0].(string)]; ok {
			return skillRowValues{values: w}
		}
		return skillRowValues{err: pgx.ErrNoRows}
	case strings.Contains(q, "FROM agenteam_skill.initializations"):
		return s.row
	}
	return skillRowValues{err: errors.New("unexpected cleanup query")}
}

func (s *cleanupTestStore) Exec(_ context.Context, q string, args ...any) (pgconn.CommandTag, error) {
	if !s.live {
		return pgconn.CommandTag{}, errors.New("write outside original transaction")
	}
	switch {
	case strings.HasPrefix(q, "INSERT INTO agenteam_skill.cleanup"):
		key, _ := f.ParseID[oc.CleanupOperation](args[0].(string))
		op, _ := f.ParseID[pc.Operation](args[2].(string))
		s.c = &cleanupRow{id: key, project: s.r.request.ProjectID, cause: pc.LifecycleCause{OperationID: op, Action: pc.Delete, ProjectVersion: f.Version(args[3].(int64))}, skill: s.r.skill, revision: s.r.revision, object: s.r.object, upload: s.r.upload, phase: cleanupGated, version: 1}
	case strings.HasPrefix(q, "UPDATE agenteam_skill.skills"):
		s.serving = false
	case strings.HasPrefix(q, "UPDATE agenteam_skill.cleanup"):
		s.c.phase = cleanupPhase(args[2].(string))
		s.c.version++
	case strings.HasPrefix(q, "DELETE FROM agenteam_skill.work"):
		delete(s.work, args[0].(string))
		s.deleted = append(s.deleted, "work")
	case strings.HasPrefix(q, "DELETE FROM agenteam_skill.object_attempts") && strings.Contains(q, "attempt_id<>$8"):
		delete(s.attempts, args[0].(string))
		s.deleted = append(s.deleted, "history_attempt")
	case strings.HasPrefix(q, "DELETE FROM agenteam_skill."):
		table := strings.Fields(q)[2]
		if s.native {
			return pgconn.CommandTag{}, errors.New("local anchor deleted before native same-Tx purge")
		}
		if s.failDelete == table {
			return pgconn.CommandTag{}, errors.New("controlled final local delete failure")
		}
		s.deleted = append(s.deleted, table)
		switch table {
		case "agenteam_skill.cleanup":
			s.c = nil
		case "agenteam_skill.object_attempts":
			delete(s.attempts, args[0].(string))
		case "agenteam_skill.initializations":
			s.core = false
			s.row = skillRowValues{err: pgx.ErrNoRows}
		}
	default:
		return pgconn.CommandTag{}, errors.New("unexpected cleanup write")
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type cleanupTestProjects struct {
	ProjectPorts
	store  *cleanupTestStore
	actor  id.Actor
	cause  pc.LifecycleCause
	calls  int
	denied error
}

func (p *cleanupTestProjects) ValidateLifecycleInTx(ctx context.Context, tx f.Tx, actor id.Actor, cause pc.LifecycleCause, participant pc.ParticipantName, phase pc.OperationPhase) error {
	p.calls++
	if !p.store.live || tx != p.store.tx || !actor.Equal(p.actor) || cause != p.cause || participant != pc.SkillsParticipant || phase != pc.CleanupPhase {
		return errors.New("wrong original lifecycle request")
	}
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{projectLock(p.store.r.request.ProjectID, f.Exclusive)}); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return p.denied
}

type cleanupTestObjects struct {
	ObjectPorts
	store                      *cleanupTestStore
	authority                  *Authority
	issuer                     oc.AccessIssuer
	releases, physical, purges int
	releaseError               error
	started, release           chan struct{}
}

func (o *cleanupTestObjects) DiscoverAccess(ctx context.Context, r oc.AccessRequest) (oc.AccessLockPlan, error) {
	if o.store.live {
		return oc.AccessLockPlan{}, errors.New("discovery inside SQL")
	}
	deps, err := o.authority.Discover(ctx, r)
	if err != nil {
		return oc.AccessLockPlan{}, err
	}
	return oc.NewAccessLockPlan(o.issuer, oc.AccessPlanDetails{Request: r, DependencyRequest: r, Dependencies: deps, DomainBinding: o.store.r.semantic, Objects: []oc.ObjectID{o.store.r.object}, Locks: deps.Locks()})
}
func (o *cleanupTestObjects) AcquireAccessPlansInTx(ctx context.Context, tx f.Tx, plans []oc.AccessLockPlan, extra []f.LockRequest) (oc.LockedAccess, error) {
	locks := append([]f.LockRequest(nil), extra...)
	for _, p := range plans {
		locks = append(locks, p.Details().Locks...)
	}
	locks, err := oc.NormalizeAccessLocks(locks)
	if err != nil {
		return oc.LockedAccess{}, err
	}
	if err = o.store.AcquireAll(ctx, tx, locks); err != nil {
		return oc.LockedAccess{}, err
	}
	return oc.NewLockedAccess(o.issuer, tx, plans, extra)
}
func (o *cleanupTestObjects) validate(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	if !locked.Matches(o.issuer, tx, plan, plan.Details().Request) {
		return invalid()
	}
	if err := o.authority.ValidateInTx(ctx, tx, plan.Details().Request, plan.Details().Dependencies); err != nil {
		return err
	}
	return o.authority.CheckCleanupInTx(ctx, tx, cause, object)
}
func (o *cleanupTestObjects) ReleaseForCleanupInTx(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID, plan oc.AccessLockPlan, locked oc.LockedAccess) error {
	o.releases++
	if err := o.validate(ctx, tx, cause, object, plan, locked); err != nil {
		return err
	}
	o.store.gated = true
	return o.releaseError
}
func (o *cleanupTestObjects) DeleteUnreferencedWithinBudget(ctx context.Context, cause oc.ObjectCleanupCause, object oc.ObjectID) (oc.CleanupResult, error) {
	o.physical++
	if o.store.live || !o.store.gated || o.store.c == nil || o.store.c.id != cause.Details().OperationID || o.store.r.object != object {
		return oc.CleanupResult{}, errors.New("physical before original committed gate")
	}
	if d, ok := ctx.Deadline(); !ok || time.Until(d) > 2*time.Second {
		return oc.CleanupResult{}, errors.New("detached cleanup budget")
	}
	if o.started != nil {
		close(o.started)
		<-o.release
	}
	if ctx.Err() != nil {
		return oc.CleanupResult{}, ctx.Err()
	}
	return oc.CleanupResult{State: oc.CleanupCompleted, OperationID: cause.Details().OperationID}, nil
}
func (o *cleanupTestObjects) PurgeDeletedObjectMetadataInTx(ctx context.Context, tx f.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID, plan oc.AccessLockPlan, locked oc.LockedAccess) (oc.ObjectMetadataPurgeResult, error) {
	o.purges++
	if err := o.validate(ctx, tx, cause, object, plan, locked); err != nil {
		return oc.ObjectMetadataPurgeResult{}, err
	}
	if o.store.c.phase != cleanupCompleted || len(o.store.work) != 0 || len(o.store.attempts) != 1 {
		return oc.ObjectMetadataPurgeResult{}, errors.New("purge before local histories retire")
	}
	o.store.native = false
	return oc.ObjectMetadataPurgeResult{State: oc.CleanupCompleted, OperationID: cause.Details().OperationID, ObjectID: object}, nil
}

func cleanupFixture(t *testing.T) (*Service, *cleanupTestStore, *cleanupTestObjects, *cleanupTestProjects, pc.ScopeRef) {
	t.Helper()
	s, base, _, _, request := readFixture(t)
	r, err := loadInitialization(context.Background(), base, request.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	store := &cleanupTestStore{initializationReadStore: base, r: *r, serving: true, core: true, native: true, work: map[string][]any{}, attempts: map[string]bool{r.attempt.String(): true}}
	cause := pc.LifecycleCause{OperationID: stateID[pc.Operation](210), Action: pc.Delete, ProjectVersion: 4}
	actor, err := cleanupActor(request.ProjectID, cause)
	if err != nil {
		t.Fatal(err)
	}
	p := &cleanupTestProjects{store: store, actor: actor, cause: cause}
	a, err := NewAuthority(store, p)
	if err != nil {
		t.Fatal(err)
	}
	o := &cleanupTestObjects{store: store, authority: a, issuer: oc.NewAccessIssuer()}
	s.state().authority = a
	s.state().objects = o
	return s, store, o, p, pc.ScopeRef{Kind: pc.ProjectScope, ProjectID: request.ProjectID}
}

func TestSkillCleanupAtomicGateAndOriginalUnknown(t *testing.T) {
	for _, name := range []string{"permission", "release_rollback", "unknown_committed", "unknown_not_committed", "original_work_pending"} {
		t.Run(name, func(t *testing.T) {
			s, store, o, p, scope := cleanupFixture(t)
			sentinel := fault(f.Forbidden)
			switch name {
			case "permission":
				p.denied = sentinel
			case "release_rollback":
				o.releaseError = sentinel
			case "unknown_committed", "unknown_not_committed":
				store.unknownAt = 2
				store.rollbackUnknown = name == "unknown_not_committed"
			case "original_work_pending":
				store.work["held"] = []any{nil, nil, nil, nil, nil, "running"}
			}
			report, err := s.Cleanup(context.Background(), p.actor, p.cause, scope, nil)
			if name == "original_work_pending" {
				if err != nil || report.Details().State != pc.CleanupPending || o.releases != 0 {
					t.Fatal("pending work released", err)
				}
				return
			}
			if err == nil || o.physical != 0 || o.purges != 0 {
				t.Fatal("unconfirmed gate escaped to I/O", err)
			}
			if strings.HasPrefix(name, "unknown_") {
				result, ok := UnknownAttempt(err)
				if !ok || result.AttemptID() != stateID[f.TransactionAttempt](70) || result.State() != f.Unknown {
					t.Fatal("lost original attempt", err)
				}
				if (store.c == nil) != store.rollbackUnknown {
					t.Fatal("double did not preserve claimed Unknown branch")
				}
				store.unknownAt = 0
				before := o.releases
				if _, err = s.Cleanup(context.Background(), p.actor, p.cause, scope, nil); err != nil {
					t.Fatal(err)
				}
				want := before
				if store.rollbackUnknown {
					want++
				}
				if o.releases != want || o.physical != 1 {
					t.Fatal("known gate replayed or physical omitted", o.releases, o.physical)
				}
			} else if !errors.Is(err, sentinel) || store.c != nil || !store.serving || store.gated {
				t.Fatal("atomic gate rollback or error changed", err)
			}
		})
	}
}

func TestSkillCleanupBoundedHistoryAndAtomicFinal(t *testing.T) {
	s, store, o, p, scope := cleanupFixture(t)
	if report, err := s.Cleanup(context.Background(), p.actor, p.cause, scope, nil); err != nil || report.Details().State != pc.CleanupPending || store.c.phase != cleanupCompleted {
		t.Fatal("physical completion", err)
	}
	for i := 0; i < 65; i++ {
		key := stateID[skillWork](i + 80)
		at := store.r.created.Time()
		store.work[key.String()] = []any{key.String(), scope.ProjectID.String(), store.r.skill.String(), stateID[oc.Process](50).String(), "package_reader", "joined", int64(1), at, &at}
	}
	for _, remaining := range []int{33, 1, 0} {
		before := len(store.deleted)
		report, err := s.Cleanup(context.Background(), p.actor, p.cause, scope, nil)
		if err != nil || report.Details().State != pc.CleanupPending || len(store.work) != remaining || len(store.deleted)-before > 32 || !store.native || !store.core {
			t.Fatal("bounded committed history", remaining, err)
		}
	}
	store.failDelete = "agenteam_skill.skills"
	if _, err := s.Cleanup(context.Background(), p.actor, p.cause, scope, nil); err == nil || !store.native || !store.core || store.c == nil {
		t.Fatal("last anchors failed to roll back", err)
	}
	store.failDelete = ""
	store.deleted = nil
	store.unknownAt = store.txs + 3
	if _, err := s.Cleanup(context.Background(), p.actor, p.cause, scope, nil); err == nil {
		t.Fatal("last Unknown reported success")
	} else if _, ok := UnknownAttempt(err); !ok {
		t.Fatal(err)
	}
	if store.native || store.core || store.c != nil {
		t.Fatal("controlled committed Unknown missing final effect")
	}
	want := []string{"agenteam_skill.cleanup", "agenteam_skill.revisions", "agenteam_skill.skills", "agenteam_skill.object_attempts", "agenteam_skill.initializations"}
	if !reflect.DeepEqual(store.deleted, want) {
		t.Fatal("wrong local anchor order", store.deleted)
	}
	store.unknownAt = 0
	calls := []int{o.releases, o.physical, o.purges}
	report, err := s.Cleanup(context.Background(), p.actor, p.cause, scope, nil)
	if err != nil || report.Details().State != pc.CleanupCompleted || !reflect.DeepEqual(calls, []int{o.releases, o.physical, o.purges}) {
		t.Fatal("all-empty replay called missing Object", err)
	}
}

func TestSkillCleanupCancellationRetainsActualCall(t *testing.T) {
	s, _, o, p, scope := cleanupFixture(t)
	o.started = make(chan struct{})
	o.release = make(chan struct{})
	done := make(chan error, 1)
	go func() { _, err := s.Cleanup(context.Background(), p.actor, p.cause, scope, nil); done <- err }()
	select {
	case <-o.started:
	case <-time.After(time.Second):
		t.Fatal("physical not reached")
	}
	if _, err := s.beginCleanup(context.Background(), scope.ProjectID); err == nil {
		t.Fatal("concurrent cleanup admission")
	}
	s.Stop()
	if s.Joined() {
		t.Fatal("cancelled physical call called joined")
	}
	close(o.release)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("original cancellation lost", err)
		}
	case <-time.After(time.Second):
		t.Fatal("actual physical call failed to return")
	}
	if !s.Joined() {
		t.Fatal("actual returned call retained after end")
	}
}
