package artifact

import (
	"context"
	"errors"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/artifact/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// Controlled rows and original Tx/lock tokens exercise this identity-only
// adapter. They do not stand in for a real Project grant or PostgreSQL test.
type fileReferenceTestStore struct {
	Store
	tx           f.Tx
	project      i.ProjectID
	file         ac.FileID
	artifact     ac.ArtifactID
	held         []f.LockRequest
	transactions int
	present      bool
}

func (s *fileReferenceTestStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if !tx.Valid() || tx != s.tx {
		return nil, errors.New("foreign transaction")
	}
	return s, nil
}
func (s *fileReferenceTestStore) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	s.transactions++
	return f.NotCommittedResult(f.NewFault(f.Forbidden, f.NotCommitted))
}
func (s *fileReferenceTestStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	if strings.HasPrefix(query, "SELECT EXISTS(SELECT 1 FROM agenteam_artifact.artifacts") {
		return cleanupOwnerRow{values: []any{len(args) == 2 && args[0] == s.project.String() && args[1] == s.artifact.String() && s.present}}
	}
	if strings.Contains(query, "FROM agenteam_artifact.cleanup") {
		return cleanupOwnerRow{values: []any{false}}
	}
	return cleanupOwnerRow{err: errors.New("unexpected identity query")}
}
func (s *fileReferenceTestStore) RequireHeldLocks(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	for _, want := range locks {
		found := false
		for _, have := range s.held {
			if f.CompareLockKeys(have.Key, want.Key) == 0 && (have.Mode == f.Exclusive || have.Mode == want.Mode) {
				found = true
			}
		}
		if !found {
			return f.NewFault(f.Forbidden, f.NotStarted)
		}
	}
	return ctx.Err()
}

type fileReferenceTestAuthority struct {
	ac.Authority
	store  *fileReferenceTestStore
	actor  i.Actor
	deny   bool
	checks int
}

func (a *fileReferenceTestAuthority) Discover(_ context.Context, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	key, _ := f.ProjectLock(subject.ProjectID.String())
	return oc.NewAccessDependencies(digestBytes([]byte("file-reference-owner")), []f.LockRequest{{Key: key, Mode: f.Shared}})
}
func (a *fileReferenceTestAuthority) DiscoverInTx(ctx context.Context, tx f.Tx, subject ac.AccessSubject) (oc.AccessDependencies, error) {
	if _, err := a.store.InTx(tx); err != nil {
		return oc.AccessDependencies{}, err
	}
	return a.Discover(ctx, subject)
}
func (a *fileReferenceTestAuthority) AuthorizeInTx(_ context.Context, _ f.Tx, subject ac.AccessSubject, intent i.AccessIntent) (i.AccessGrant, error) {
	a.checks++
	if a.deny || !subject.Actor.Equal(a.actor) || subject.ProjectID != a.store.project || intent != i.Read {
		return i.AccessGrant{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	scope, _ := i.InProject(subject.ProjectID)
	at, _ := f.ParseInstant("2026-10-14T00:00:00Z")
	return i.NewAccessGrant(subject.Actor, scope, intent, at, 1)
}

func fileReferenceFixture(t *testing.T) (*OwnerFileReferences, *fileReferenceTestStore, *fileReferenceTestAuthority) {
	t.Helper()
	store := &fileReferenceTestStore{tx: f.NewTx(), project: newTestID[i.Project](t), file: newTestID[ac.File](t), artifact: newTestID[ac.ArtifactEntity](t), present: true}
	actor, err := i.NewHuman(newTestID[i.User](t), newTestID[i.Session](t))
	if err != nil {
		t.Fatal(err)
	}
	authority := &fileReferenceTestAuthority{store: store, actor: actor}
	owners, err := NewOwnerProvider(store, authority)
	if err != nil {
		t.Fatal(err)
	}
	state := &serviceState{store: store, owners: owners, ownership: &projectCallOwnershipState{}}
	service := &Service{data: func() *serviceState { return state }}
	port, err := NewOwnerFileReferences(service)
	if err != nil {
		t.Fatal(err)
	}
	return port, store, authority
}

func TestOwnerFileReferencesKeepStableArtifactIdentityAndOriginalTransaction(t *testing.T) {
	port, store, authority := fileReferenceFixture(t)
	ctx := context.Background()
	locks, err := port.FileReferenceLocks(ctx, authority.actor, store.project, store.artifact)
	if err != nil {
		t.Fatal(err)
	}
	actual, mistaken := false, false
	for _, lock := range locks {
		actual = actual || f.CompareLockKeys(lock.Key, artifactLock(store.artifact.String()).Key) == 0
		mistaken = mistaken || f.CompareLockKeys(lock.Key, artifactLock(store.file.String()).Key) == 0
	}
	if !actual || mistaken || len(locks) != 3 {
		t.Fatal("reference did not retain its stable Artifact lock")
	}
	store.held = locks
	if err = port.RequireFileReferenceInTx(ctx, store.tx, authority.actor, store.project, store.artifact); err != nil {
		t.Fatal(err)
	}
	if authority.checks != 1 || store.transactions != 0 {
		t.Fatal("identity check bypassed owner or opened a read session")
	}
	// A later internal file version does not change the captured reference or
	// require a different lock. The adapter never reads or returns that FileID.
	store.file = newTestID[ac.File](t)
	if err = port.RequireFileReferenceInTx(ctx, store.tx, authority.actor, store.project, store.artifact); err != nil {
		t.Fatal("internal file version changed stable reference", err)
	}
	if port.RequireStore(store) != nil || port.RequireStore(&fileReferenceTestStore{}) == nil {
		t.Fatal("store binding")
	}
	if err = port.RequireFileReferenceInTx(ctx, f.NewTx(), authority.actor, store.project, store.artifact); err == nil {
		t.Fatal("foreign Tx accepted")
	}
	if _, err = port.FileReferenceLocks(ctx, authority.actor, newTestID[i.Project](t), store.artifact); err == nil {
		t.Fatal("foreign Project file accepted")
	}
	fileAsArtifact, err := f.ParseID[ac.ArtifactEntity](store.file.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = port.FileReferenceLocks(ctx, authority.actor, store.project, fileAsArtifact); err == nil {
		t.Fatal("internal FileID accepted as a stable Artifact identity")
	}
}

func TestOwnerFileReferencesRejectMissingLocksRevocationAndUnpublishedIdentity(t *testing.T) {
	port, store, authority := fileReferenceFixture(t)
	ctx := context.Background()
	locks, err := port.FileReferenceLocks(ctx, authority.actor, store.project, store.artifact)
	if err != nil {
		t.Fatal(err)
	}
	if err = port.RequireFileReferenceInTx(ctx, store.tx, authority.actor, store.project, store.artifact); err == nil {
		t.Fatal("missing original locks accepted")
	}
	if authority.checks != 0 {
		t.Fatal("missing locks reached authority")
	}
	store.held, authority.deny = locks, true
	if err = port.RequireFileReferenceInTx(ctx, store.tx, authority.actor, store.project, store.artifact); err == nil {
		t.Fatal("revoked owner accepted")
	}
	store.present, authority.deny = false, false
	if err = port.RequireFileReferenceInTx(ctx, store.tx, authority.actor, store.project, store.artifact); err == nil {
		t.Fatal("identity deleted after discovery accepted")
	}
	if _, err = port.FileReferenceLocks(ctx, authority.actor, store.project, store.artifact); err == nil {
		t.Fatal("prospective or deleted file accepted")
	}
	if _, err = port.FileReferenceLocks(ctx, i.Actor{}, store.project, store.artifact); err == nil {
		t.Fatal("untrusted actor accepted")
	}
	if _, err = NewOwnerFileReferences(nil); err == nil {
		t.Fatal("missing original Service accepted")
	}
	state := &serviceState{store: port.store, owners: port.owners}
	unowned := &Service{data: func() *serviceState { return state }}
	if _, err = NewOwnerFileReferences(unowned); err == nil {
		t.Fatal("unowned Service accepted")
	}
}
