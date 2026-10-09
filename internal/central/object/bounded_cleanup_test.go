package object

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

func TestBoundedCleanupRecoveryOwnsLifetimeAndOriginalBudget(t *testing.T) {
	state := &serviceState{initialized: true, operations: map[*operation]bool{}, changed: make(chan struct{})}
	s := &Service{data: func() *serviceState { return state }}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	parent, done, err := s.begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	child, finish, err := s.beginBoundedCleanup(parent.ctx)
	if err != nil || child == parent || child.ctx.Value(boundedCleanupBudgetKey{}) != parent.ctx {
		t.Fatal("recovery reused its parent or lost the caller budget", err)
	}
	before, _ := parent.ctx.Deadline()
	after, _ := child.ctx.Deadline()
	if after != before || len(state.operations) != 2 {
		t.Fatal("batch extended the original budget or escaped operation accounting")
	}
	finish()
	select {
	case <-child.done:
	default:
		t.Fatal("batch did not actually finish")
	}
	if parent.ctx.Err() != nil || len(state.operations) != 1 || !state.operations[parent] {
		t.Fatal("batch completion cancelled or removed the recovery parent")
	}
	expired, stop := context.WithCancel(parent.ctx)
	stop()
	if _, _, err := s.beginBoundedCleanup(expired); err == nil || len(state.operations) != 1 {
		t.Fatal("cancelled caller reused an admitted parent")
	}
	// The existing private startup parent remains the sole way to pass startup
	// admission to a child. This does not make ordinary unready calls admissible.
	state.initialized, parent.initializing = false, true
	child, finish, err = s.beginBoundedCleanup(parent.ctx)
	if err != nil || !child.initializing {
		t.Fatal("startup recovery lost its original private admission", err)
	}
	finish()
	if _, _, err := s.beginBoundedCleanup(ctx); err == nil {
		t.Fatal("an unrelated caller borrowed startup admission")
	}
}

func TestBoundedCleanupRecoveryRequiresOriginalCanonicalAnchor(t *testing.T) {
	v := newAuditPublishFixture(t)
	originalOwner := v.u.owner
	owner, _ := oc.NewObjectOwner(oc.SkillRevision, originalOwner.Details().ID, originalOwner.Details().ProjectID)
	v.u.owner, v.u.state, v.u.disposition = owner, "committed", "revoked"
	canonical := auditID[oc.CleanupOperation](t)
	oldRows := v.store.row
	missing, corrupt, canonicalReads := false, false, 0
	v.store.row = func(query string, args ...any) postgres.Row {
		if strings.HasPrefix(query, "SELECT c.operation_id::text") {
			canonicalReads++
			if args[0] != v.u.object.String() || args[1] != v.u.attempt.String() || args[2] != v.u.id.String() {
				t.Fatal("recovery selected another attempt's cause")
			}
			if missing {
				return auditTestRow(func(...any) error { return pgx.ErrNoRows })
			}
			if corrupt {
				return auditValues("not-a-native-operation-id")
			}
			return auditValues(canonical.String())
		}
		return oldRows(query, args...)
	}
	ctx := context.Background()
	cause, bounded, err := canonicalSkillCleanup(ctx, v.store, v.u.object)
	if err != nil || !bounded || cause.Details().OperationID != canonical || !cause.Details().Owner.Equal(owner) || cause.Details().Reason != oc.ProjectDeleted {
		t.Fatal("canonical recovery cause changed", err)
	}
	state := &serviceState{store: v.store, initialized: true, operations: map[*operation]bool{}, changed: make(chan struct{})}
	s := &Service{data: func() *serviceState { return state }}
	// An unbound current planner must reject the real bounded entry before
	// history enumeration (the embedded Query is deliberately unbound).
	result, err := s.cleanObject(ctx, v.u.object)
	auditCode(t, err, foundation.DependencyUnbound)
	if result != oc.CleanupPending || len(state.operations) != 0 {
		t.Fatal("lookup became permission or left a recovery lifetime")
	}
	for _, absent := range []bool{true, false} {
		missing, corrupt = absent, !absent
		if _, bounded, err = canonicalSkillCleanup(ctx, v.store, v.u.object); !bounded || err == nil {
			t.Fatal("missing/corrupt anchor selected unbounded fallback")
		}
	}
	missing, corrupt = false, false
	v.u.owner = originalOwner
	before := canonicalReads
	if _, bounded, err = canonicalSkillCleanup(ctx, v.store, v.u.object); bounded || err != nil || canonicalReads != before {
		t.Fatal("another Owner entered the Skills-only recovery path", err)
	}
}

func TestBoundedCleanupExpiredBudgetDoesNotStartFreshJoin(t *testing.T) {
	budget, cancel := context.WithCancel(context.Background())
	cancel()
	op := &operation{ctx: context.WithValue(context.Background(), boundedCleanupBudgetKey{}, budget)}
	h := &projectWorkHandle{operation: op}
	state := &serviceState{store: &auditTestStore{}, projectWork: map[string]*projectWorkHandle{"original": h}, cleanupRequests: map[*cleanupRequest]bool{}, changed: make(chan struct{})}
	s := &Service{data: func() *serviceState { return state }}
	// The embedded unbound Store panics if a new SQL/join tail is started.
	s.finishOperationWork(op)
	if !h.ended || len(state.projectWork) != 1 || len(state.cleanupRequests) != 0 {
		t.Fatal("actual-ended evidence lost or uncommitted retirement invented")
	}
}

func TestBoundedCleanupOnlyExemptsOwnReturnedCheckpoint(t *testing.T) {
	object := auditID[oc.StoredObject](t)
	state := &serviceState{projectWork: map[string]*projectWorkHandle{}}
	s := &Service{data: func() *serviceState { return state }}
	op := &operation{service: s}
	claim := cleanupClaim{id: auditID[oc.CleanupOperation](t).String(), worker: auditID[oc.CleanupOperation](t).String(), fence: 7}
	call := &boundedCleanupCall{service: s, object: object, operation: op, completed: map[string]cleanupClaim{claim.worker: claim}}
	ctx := context.WithValue(context.Background(), operationKey{}, op)
	ctx = context.WithValue(ctx, boundedCleanupKey{}, call)
	h := &projectWorkHandle{operation: op, work: projectWork{object: object, resource: claim.id, fence: 7}}
	state.projectWork[claim.worker] = h
	ids, err := s.completedCleanupWorkers(ctx, object)
	if err != nil || len(ids) != 1 || ids[0] != claim.worker || h.ended {
		t.Fatal("returned I/O was confused with operation join", err)
	}
	for _, mutate := range []func(){
		func() { h.work.fence++ },
		func() { h.work.resource = auditID[oc.CleanupOperation](t).String() },
		func() { h.operation = &operation{service: s} },
		func() { h.work.object = auditID[oc.StoredObject](t) },
	} {
		original := *h
		mutate()
		if _, err = s.completedCleanupWorkers(ctx, object); err == nil {
			t.Fatal("another physical identity borrowed this call's exemption")
		}
		*h = original
	}
	if s.boundedCleanup(context.Background(), object) != nil || s.boundedCleanup(ctx, auditID[oc.StoredObject](t)) != nil {
		t.Fatal("public/foreign context manufactured bounded-call authority")
	}
	store := &auditTestStore{row: func(_ string, args ...any) postgres.Row {
		workers, ok := args[1].([]string)
		if !ok || workers == nil || len(workers) != 0 {
			t.Fatal("empty exclusion became SQL NULL, hiding unjoined work")
		}
		return auditValues(true)
	}}
	pending, err := boundedCleanupPending(ctx, store, object, nil)
	if err != nil || !pending {
		t.Fatal("full pending predicate bypassed", err)
	}
}

type boundedCleanupAuthority struct {
	oc.CleanupAuthority
	check func(context.Context, foundation.Tx, oc.ObjectCleanupCause, oc.ObjectID) error
}

func (a boundedCleanupAuthority) CheckCleanupInTx(ctx context.Context, tx foundation.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID) error {
	return a.check(ctx, tx, cause, object)
}

func TestStoppedSkillCleanupUsesCanonicalCauseAndCurrentAuthority(t *testing.T) {
	v := newAuditPublishFixture(t)
	owner, _ := oc.NewObjectOwner(oc.SkillRevision, v.u.owner.Details().ID, v.u.owner.Details().ProjectID)
	v.u.owner, v.u.state, v.u.disposition = owner, "committed", "revoked"
	canonical := auditID[oc.CleanupOperation](t)
	oldRows := v.store.row
	missing := false
	v.store.row = func(query string, args ...any) postgres.Row {
		if strings.HasPrefix(query, "SELECT c.operation_id::text") {
			if args[0] != v.u.object.String() || args[1] != v.u.attempt.String() || args[2] != v.u.id.String() {
				t.Fatal("canonical gate lookup crossed original anchors")
			}
			if missing {
				return auditTestRow(func(...any) error { return pgx.ErrNoRows })
			}
			return auditValues(canonical.String())
		}
		return oldRows(query, args...)
	}
	ctx := context.WithValue(context.Background(), struct{ marker string }{"original"}, true)
	var calls int
	var deny error
	state := &serviceState{store: v.store}
	state.auth.Cleanup = boundedCleanupAuthority{check: func(got context.Context, tx foundation.Tx, cause oc.ObjectCleanupCause, object oc.ObjectID) error {
		calls++
		if got != ctx || tx != v.store.tx || object != v.u.object || cause.Details().OperationID != canonical || cause.Details().Reason != oc.ProjectDeleted || !cause.Details().Owner.Equal(owner) {
			t.Fatal("new claim used an old attempt cause or changed the live transaction")
		}
		return deny
	}}
	s := &Service{data: func() *serviceState { return state }}
	if allowed, err := s.authorizeStoppedSkillCleanup(ctx, v.store.tx, v.store, v.u.object); !allowed || err != nil || calls != 1 {
		t.Fatal("current canonical authorization failed", err)
	}
	deny = foundation.NewFault(foundation.Forbidden, foundation.NotCommitted)
	if allowed, err := s.authorizeStoppedSkillCleanup(ctx, v.store.tx, v.store, v.u.object); allowed || !errors.Is(err, deny) {
		t.Fatal("current gate denial was turned into allow", err)
	}
	missing = true
	before := calls
	if allowed, err := s.authorizeStoppedSkillCleanup(ctx, v.store.tx, v.store, v.u.object); allowed || err != nil || calls != before {
		t.Fatal("missing canonical gate became permission", err)
	}
}
