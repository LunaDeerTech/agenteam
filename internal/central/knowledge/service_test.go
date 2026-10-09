package knowledge

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestConstructorAndZeroServiceFailClosed(t *testing.T) {
	var typedNil *constructorStore
	for _, store := range []Store{nil, typedNil} {
		service, err := New(store, Dependencies{})
		var known *f.Fault
		if service != nil || !errors.As(err, &known) || known.Code != f.DependencyUnbound {
			t.Fatal("unbound constructor accepted", err)
		}
	}
	var service *Service
	if _, _, err := service.begin(context.Background()); err == nil {
		t.Fatal("nil receiver accepted")
	}
	if _, _, err := (&Service{}).begin(context.Background()); err == nil {
		t.Fatal("zero receiver accepted")
	}
}

type constructorStore struct{ Store }

type authorityStore struct {
	Store
	tx            f.Tx
	heldErr       error
	queries, held int
	row           postgres.Row
}

func (s *authorityStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx {
		return nil, errors.New("foreign Tx")
	}
	return s, nil
}
func (s *authorityStore) RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error {
	s.held++
	return s.heldErr
}
func (s *authorityStore) QueryRow(context.Context, string, ...any) postgres.Row {
	s.queries++
	if s.row != nil {
		return s.row
	}
	return rejectedRow{}
}

type rejectedRow struct{}

func (rejectedRow) Scan(...any) error { return errors.New("unexpected SQL") }

type ownerGate struct {
	pc.ProjectAuthority
	grant  pc.ProjectAccess
	err    error
	calls  int
	tx     f.Tx
	intent id.AccessIntent
}

func (p *ownerGate) RequireOwnerInTx(_ context.Context, tx f.Tx, _ id.Actor, _ id.ProjectID, intent id.AccessIntent) (pc.ProjectAccess, error) {
	p.calls++
	p.tx, p.intent = tx, intent
	return p.grant, p.err
}

func TestObjectOwnerCurrentGatePrecedesKnowledgeFacts(t *testing.T) {
	_, actor, q := queryFixture(t)
	owner, err := oc.NewObjectOwner(oc.Knowledge, newID[kc.Document](t).String(), q.project.String())
	if err != nil {
		t.Fatal(err)
	}
	tx := f.NewTx()
	store := &authorityStore{tx: tx}
	gate := &ownerGate{err: fault(f.Forbidden)}
	a, err := NewAuthority(store, gate)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.AuthorizeOwnerInTx(context.Background(), f.NewTx(), actor, owner, id.Read); err == nil || gate.calls != 0 || store.queries != 0 {
		t.Fatal("foreign Tx reached authorization/SQL")
	}
	store.heldErr = errors.New("missing tree lock")
	if _, err = a.AuthorizeOwnerInTx(context.Background(), tx, actor, owner, id.Read); err == nil || gate.calls != 0 || store.queries != 0 {
		t.Fatal("weak union reached authorization/SQL")
	}
	store.heldErr = nil
	if _, err = a.AuthorizeOwnerInTx(context.Background(), tx, actor, owner, id.Read); err != gate.err || gate.calls != 1 || store.queries != 0 || gate.tx != tx {
		t.Fatal("denied current Owner read domain facts", err)
	}
	if _, err = a.AuthorizeOwnerInTx(context.Background(), tx, actor, owner, id.Converge); err == nil || gate.calls != 1 {
		t.Fatal("ordinary Owner read promoted to convergence")
	}
	user, _ := f.ParseID[id.User](actor.Details().UserID)
	now, _ := f.NewInstant(time.Now())
	project := pc.ProjectRef{ID: q.project, OwnerUserID: user, Name: "Knowledge", NormalizedName: "knowledge", Lifecycle: pc.Active, Version: 1, CreatedAt: now, UpdatedAt: now}
	gate.grant, err = pc.NewProjectAccess(actor, project, now)
	if err != nil {
		t.Fatal(err)
	}
	gate.err = nil
	if err = a.CheckInTx(context.Background(), tx, actor, owner, id.Mutate); err != nil || gate.intent != id.Mutate || store.queries != 0 {
		t.Fatal("valid current mutation gate failed", err)
	}
	otherSession, _ := id.NewHuman(user, newID[id.Session](t))
	if err = a.CheckInTx(context.Background(), tx, otherSession, owner, id.Read); err == nil {
		t.Fatal("projection from a different Session accepted")
	}
}

func TestObjectPlansAreNotAuthorizationAndForeignCleanupIsRejected(t *testing.T) {
	_, actor, q := queryFixture(t)
	owner, _ := oc.NewObjectOwner(oc.Knowledge, newID[kc.Document](t).String(), q.project.String())
	request, err := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.PrepareReadAccess, Actor: actor, Owner: owner, Intent: id.Read})
	if err != nil {
		t.Fatal(err)
	}
	store := &authorityStore{tx: f.NewTx()}
	gate := &ownerGate{err: fault(f.Forbidden)}
	a, _ := NewAuthority(store, gate)
	deps, err := a.Discover(context.Background(), request)
	if err != nil || len(deps.Locks()) != 3 || gate.calls != 0 || store.queries != 0 {
		t.Fatal("discovery did not remain non-authorizing", err)
	}
	if err = a.ValidateInTx(context.Background(), f.NewTx(), request, deps); err == nil {
		t.Fatal("foreign transaction accepted")
	}
	if err = a.ValidateInTx(context.Background(), store.tx, request, deps); err != nil || gate.calls != 0 {
		t.Fatal("mapping validation changed authority", err)
	}
	if _, err = a.AuthorizeOwnerInTx(context.Background(), store.tx, actor, owner, id.Read); err != gate.err {
		t.Fatal("valid mapping granted Owner", err)
	}
	cause, err := oc.NewObjectCleanupCause(oc.CleanupDetails{OperationID: newID[oc.CleanupOperation](t), Owner: owner, Reason: oc.ProjectDeleted})
	if err != nil {
		t.Fatal(err)
	}
	if err = a.CheckCleanupInTx(context.Background(), store.tx, cause, newID[oc.StoredObject](t)); err == nil || store.queries != 0 {
		t.Fatal("document adapter granted Project cleanup")
	}
	if err = a.CheckProjectCleanupInTx(context.Background(), store.tx, actor, oc.ProjectCleanupCause{}); err == nil {
		t.Fatal("unbound lifecycle accepted")
	}
}

func TestUnknownKeepsActualAttemptAndPrivateCause(t *testing.T) {
	key := f.IdempotencyKey("private-canary-command-key")
	owner, err := f.NewID[struct{}]()
	if err != nil {
		t.Fatal(err)
	}
	command, err := f.NewCommandIdentity("knowledge", []string{owner.String()}, "create", key)
	if err != nil {
		t.Fatal(err)
	}
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := f.NewID[f.TransactionAttempt]()
	if err != nil {
		t.Fatal(err)
	}
	result := f.UnknownResult(attempt, cause)
	err = txError(result)
	var known *f.Fault
	var original commitFailure
	if !errors.As(err, &known) || known.Code != f.CommitUnknown || known.CauseID != attempt.String() || !errors.As(err, &original) {
		t.Fatal("unknown lost provenance", err)
	}
	if original.result.AttemptID() != attempt || original.result.Cause().Details().Kind != cause.Details().Kind {
		t.Fatal("changed physical attempt")
	}
	if strings.Contains(fmt.Sprintf("%+v %#v", err, original), string(key)) {
		t.Fatal("private command leaked")
	}
}
