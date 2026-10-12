package object

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5/pgconn"
)

// Only original Store/Tx lifetime is controlled here. Real Project claims,
// deletion, backend I/O and stopped-Project maintenance are integration tests.
type cleanupOwnerStore struct {
	Store
	mu            sync.Mutex
	active        map[f.Tx]bool
	after         func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult
	skip          bool
	reads, writes int
}

func (s *cleanupOwnerStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.active[tx] {
		return nil, errors.New("original transaction ended")
	}
	return s, nil
}
func (s *cleanupOwnerStore) WithinTx(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	result := f.CommittedResult()
	if !s.skip {
		tx := f.NewTx()
		s.mu.Lock()
		s.active[tx] = true
		s.mu.Unlock()
		err := fn(ctx, tx)
		s.mu.Lock()
		delete(s.active, tx)
		s.mu.Unlock()
		if err != nil {
			result = rejectedAccess(err)
		}
	}
	if s.after != nil {
		return s.after(ctx, cause, result)
	}
	return result
}
func (s *cleanupOwnerStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	if len(locks) != 1 || locks[0].Mode != f.Exclusive || !strings.HasPrefix(locks[0].Key.Canonical(), "project:") {
		return errors.New("missing original Project EX")
	}
	return nil
}
func (s *cleanupOwnerStore) QueryRow(_ context.Context, query string, _ ...any) postgres.Row {
	return auditTestRow(func(dst ...any) error {
		if !strings.HasPrefix(query, "SELECT count(*) FROM agenteam_object.objects") {
			return errors.New("unexpected cleanup read")
		}
		s.mu.Lock()
		s.reads++
		s.mu.Unlock()
		*dst[0].(*int64) = 0
		return nil
	})
}
func (s *cleanupOwnerStore) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	s.mu.Lock()
	s.writes++
	s.mu.Unlock()
	return pgconn.CommandTag{}, errors.New("confirmation attempted mutation")
}

type cleanupOwnerAuthority struct {
	store  *cleanupOwnerStore
	reject error
}

func (a *cleanupOwnerAuthority) CheckCleanupInTx(context.Context, f.Tx, oc.ObjectCleanupCause, oc.ObjectID) error {
	return errors.New("wrong cleanup port")
}
func (a *cleanupOwnerAuthority) CheckProjectCleanupInTx(_ context.Context, tx f.Tx, _ i.Actor, _ oc.ProjectCleanupCause) error {
	if _, err := a.store.InTx(tx); err != nil {
		return err
	}
	return a.reject
}
func newCleanupOwnerControl(t *testing.T) (*Service, *cleanupOwnerStore, *cleanupOwnerAuthority, i.Actor, oc.ProjectCleanupCause) {
	t.Helper()
	store := &cleanupOwnerStore{active: map[f.Tx]bool{}}
	auth := &cleanupOwnerAuthority{store: store}
	r := &serviceState{store: store, initialized: true, operations: map[*operation]bool{}, changed: make(chan struct{}), auth: Authorizations{Cleanup: auth}}
	s := &Service{data: func() *serviceState { return r }}
	p := auditID[i.Project](t)
	op := auditID[oc.CleanupOperation](t)
	scope, _ := i.InProject(p)
	reg, _ := i.RegisterService(i.ObjectMaintenance)
	actor, err := reg.Actor(op.String(), scope)
	if err != nil {
		t.Fatal(err)
	}
	cause, err := oc.NewProjectCleanupCause(oc.ProjectCleanupDetails{ProjectID: p, OperationID: op, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	return s, store, auth, actor, cause
}
func cleanupOwnerJoined(s *Service) bool {
	r := s.state()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.workJoinedLocked()
}
func assertCleanupUnknown(t *testing.T, err error, attempt f.ID[f.TransactionAttempt]) {
	t.Helper()
	var problem *f.Fault
	var original *projectCleanupFailure
	if !errors.As(err, &problem) || problem.CommitState != f.Unknown || problem.Code != f.CommitUnknown || problem.CauseID != attempt.String() || !errors.As(err, &original) || original.result.AttemptID() != attempt {
		t.Fatal("original cleanup Unknown identity lost", err)
	}
}
func TestObjectProjectCleanupOriginalReturnAndReadConfirmation(t *testing.T) {
	s, store, _, actor, cause := newCleanupOwnerControl(t)
	attempt := auditID[f.TransactionAttempt](t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	open := func() { once.Do(func() { close(release) }) }
	defer func() { open(); joinWait(t, done) }()
	var original f.TransactionCause
	store.after = func(_ context.Context, c f.TransactionCause, r f.CommitResult) f.CommitResult {
		if c.Details().RecoveryRunID == cause.Details().OperationID.String() {
			original = c
			close(entered)
			<-release
			return f.UnknownResult(attempt, c)
		}
		return r
	}
	var firstErr error
	go func() { defer close(done); _, firstErr = s.ConfirmProjectCleanup(context.Background(), actor, cause) }()
	joinWait(t, entered)
	copyService := *s
	if _, err := copyService.CleanupProject(context.Background(), actor, cause); !hasCode(err, f.ResourceBusy) {
		t.Fatal("active original call was replaced", err)
	}
	if cleanupOwnerJoined(s) {
		t.Fatal("original Store return treated as joined")
	}
	other := cause.Details()
	other.ProjectID = auditID[i.Project](t)
	other.OperationID = auditID[oc.CleanupOperation](t)
	b, _ := oc.NewProjectCleanupCause(other)
	if _, err := s.ConfirmProjectCleanup(context.Background(), actor, b); err != nil {
		t.Fatal("unrelated Project was blocked", err)
	}
	open()
	joinWait(t, done)
	assertCleanupUnknown(t, firstErr, attempt)
	if original.Details().Owner != "object-project-cleanup-confirm" || cleanupOwnerJoined(s) {
		t.Fatal("Unknown no longer belongs to original service")
	}
	store.after = nil
	result, err := copyService.CleanupProject(context.Background(), actor, cause)
	if err != nil || result.State != oc.CleanupCompleted || store.writes != 0 || !cleanupOwnerJoined(s) {
		t.Fatal("read-only actual confirmation failed", err)
	}
}
func TestObjectProjectCleanupConfirmationKeepsFirstUnknown(t *testing.T) {
	for _, name := range []string{"gate-rejected", "not-committed", "unknown-again", "skipped-callback", "cancelled-before", "cancelled-after-callback"} {
		t.Run(name, func(t *testing.T) {
			s, store, auth, actor, cause := newCleanupOwnerControl(t)
			attempt := auditID[f.TransactionAttempt](t)
			store.after = func(_ context.Context, c f.TransactionCause, _ f.CommitResult) f.CommitResult {
				return f.UnknownResult(attempt, c)
			}
			_, err := s.ConfirmProjectCleanup(context.Background(), actor, cause)
			assertCleanupUnknown(t, err, attempt)
			store.after = nil
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch name {
			case "gate-rejected":
				auth.reject = f.NewFault(f.Forbidden, f.NotCommitted)
			case "not-committed":
				store.after = func(context.Context, f.TransactionCause, f.CommitResult) f.CommitResult {
					return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
				}
			case "unknown-again":
				store.after = func(_ context.Context, c f.TransactionCause, _ f.CommitResult) f.CommitResult {
					return f.UnknownResult(auditID[f.TransactionAttempt](t), c)
				}
			case "skipped-callback":
				store.skip = true
			case "cancelled-before":
				cancel()
			case "cancelled-after-callback":
				store.after = func(_ context.Context, _ f.TransactionCause, r f.CommitResult) f.CommitResult { cancel(); return r }
			}
			_, err = s.CleanupProject(ctx, actor, cause)
			assertCleanupUnknown(t, err, attempt)
			if cleanupOwnerJoined(s) || store.writes != 0 {
				t.Fatal("failed confirmation discarded original owner or mutated")
			}
			store.after = nil
			store.skip = false
			auth.reject = nil
			if _, err = s.CleanupProject(context.Background(), actor, cause); err != nil || !cleanupOwnerJoined(s) {
				t.Fatal("original owner could not confirm", err)
			}
		})
	}
}
func TestObjectProjectCleanupScopeAndFixedStore(t *testing.T) {
	s, store, _, actor, cause := newCleanupOwnerControl(t)
	if s.RequireProjectCleanupStore(store) != nil || !hasCode(s.RequireProjectCleanupStore(&cleanupOwnerStore{}), f.DependencyUnbound) {
		t.Fatal("fixed Store identity lost")
	}
	call, err := s.state().borrowProjectCleanup(actor, cause.Details())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), projectCleanupContextKey{}, &projectCleanupContext{owner: s.state(), call: call})
	txCause, _ := f.NewRecoveryCause("original-object", cause.Details().OperationID.String(), "")
	attempt := auditID[f.TransactionAttempt](t)
	s.captureProjectCleanup(ctx, f.UnknownResult(attempt, txCause))
	s.state().returnProjectCleanup(call)
	if s.currentProjectCleanup(ctx) != nil {
		t.Fatal("saved context survived actual call return")
	}
	changed := cause.Details()
	changed.Version++
	wrong, _ := oc.NewProjectCleanupCause(changed)
	if _, err = s.CleanupProject(context.Background(), actor, wrong); !hasCode(err, f.InvalidState) {
		t.Fatal("another cause replaced original owner", err)
	}
	if _, err = s.CleanupProject(context.Background(), actor, cause); err != nil {
		t.Fatal(err)
	}
}
