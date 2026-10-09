package object

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

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
