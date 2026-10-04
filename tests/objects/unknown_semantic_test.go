//go:build integration

package objects_test

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This wrapper only delays delivery of the real driver's first Unknown result.
// It does not fabricate a Tx, SQL row, commit outcome, or authorization result.
type pauseUnknownStore struct {
	*postgres.Store
	once    atomic.Bool
	reached chan foundation.CommitResult
	resume  chan struct{}
}

func (s *pauseUnknownStore) WithinTx(ctx context.Context, cause foundation.TransactionCause, fn func(context.Context, foundation.Tx) error) foundation.CommitResult {
	result := s.Store.WithinTx(ctx, cause, fn)
	if result.State() == foundation.Unknown && s.once.CompareAndSwap(false, true) {
		s.reached <- result
		select {
		case <-s.resume:
		case <-ctx.Done():
		}
	}
	return result
}

func TestObjectUnknownReservationKeepsCompleteSemanticIdentity(t *testing.T) {
	for _, change := range []string{"body", "media_type", "expected_version"} {
		t.Run(change, func(t *testing.T) { unknownReservationSemantic(t, change) })
	}
}
func unknownReservationSemantic(t *testing.T, change string) {
	f := newFixture(t, false)
	realStore, pgProxy := proxyStore(t, f, false)
	paused := &pauseUnknownStore{Store: realStore, reached: make(chan foundation.CommitResult, 1), resume: make(chan struct{})}
	backend, err := object.NewBackend(f.config)
	if err != nil {
		t.Fatal(err)
	}
	spool, err := object.OpenSpool(filepath.Join(t.TempDir(), "spool"), id[oc.Process](t))
	if err != nil {
		t.Fatal(err)
	}
	auth := &authority{store: realStore}
	keys, err := cursor.LoadKeyring(`{"format":1,"current_kid":"test","keys":[{"kid":"test","key_b64":"AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="}]}`)
	if err != nil {
		t.Fatal(err)
	}
	auditing, err := audit.New(realStore, keys, audit.Authorizations{Projects: auditAuthority{auth}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := object.New(paused, backend, spool, auditing, object.Authorizations{Planner: auth, Resources: auth, Read: auth, Gate: auth, Cleanup: auth, Leases: auth})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.StopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := service.Drain(ctx); err != nil {
			_ = service.Force(ctx)
			t.Error(err)
		}
	})
	if err = service.Initialize(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	var releaseUnknown, releasePG sync.Once
	defer releaseUnknown.Do(func() { close(paused.resume) })
	defer releasePG.Do(func() { close(pgProxy.release) })
	pgProxy.armed.Store(2) // authorization-only preparation, then reservation.
	ctx := contextFor(t)
	cmdA := command(t, "independent-unknown-digest")
	type outcome struct {
		value oc.PutResult
		err   error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := service.PutObject(ctx, f.actor, f.owner, cmdA, "text/plain", 5, nil, io.NopCloser(strings.NewReader("alpha")))
		done <- outcome{result, err}
	}()
	select {
	case <-pgProxy.reached:
	case <-time.After(5 * time.Second):
		t.Fatal("real reserve COMMIT not intercepted")
	}
	// Drop COMMIT before it reaches PostgreSQL. The helper closes the owned
	// upstream connection, so PostgreSQL rolls back that real transaction.
	releasePG.Do(func() { close(pgProxy.release) })
	select {
	case observed := <-paused.reached:
		if observed.State() != foundation.Unknown || observed.AttemptID().Validate() != nil {
			t.Fatal("did not observe an actual driver Unknown")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("driver did not report unknown reserve")
	}
	lookup, err := f.service.LookupPut(contextFor(t), f.actor, f.owner, cmdA.IdempotencyKey)
	if err != nil || lookup.State != oc.NotObserved {
		t.Fatal("original reserve did not actually roll back", err, lookup.State)
	}
	var before int64
	if err = f.store.QueryRow(contextFor(t), `SELECT count(*) FROM agenteam_object.uploads`).Scan(&before); err != nil || before != 0 {
		t.Fatal("rollback left an upload", err, before)
	}
	// An authorized request wins the same key with a different complete semantic
	// digest. Body, MIME and expected-version differences are tested separately.
	winnerBody, winnerMedia := "alpha", "text/plain"
	winnerCommand := cmdA
	switch change {
	case "body":
		winnerBody = "bravo"
	case "media_type":
		winnerMedia = "application/octet-stream"
	case "expected_version":
		version := foundation.Version(1)
		winnerCommand.ExpectedVersion = &version
	}
	winner, err := f.service.PutObject(contextFor(t), f.actor, f.owner, winnerCommand, winnerMedia, 5, nil, io.NopCloser(strings.NewReader(winnerBody)))
	if err != nil {
		t.Fatal(err)
	}
	if winner.Meta.SHA256 != digest([]byte(winnerBody)) {
		t.Fatal("winner content not established")
	}
	releaseUnknown.Do(func() { close(paused.resume) })
	select {
	case first := <-done:
		if first.err == nil {
			t.Fatalf("original request returned success for a different semantic digest: same_object=%t got_digest=%s", first.value.Meta.ID == winner.Meta.ID, first.value.Meta.SHA256)
		}
		requireCode(t, first.err, foundation.IdempotencyKeyReused)
	case <-time.After(5 * time.Second):
		t.Fatal("original request did not finish")
	}
}
