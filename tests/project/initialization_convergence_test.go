//go:build integration

package project_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/account"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestProjectInitializationConvergenceFacts(t *testing.T) {
	f := newInitializationConvergencePG(t)
	for _, state := range []c.CreationState{c.CreationAccepted, c.CreationInitializing, c.CreationFailed, c.CreationCompleted} {
		t.Run(string(state), func(t *testing.T) {
			v := f.seed(t, state)
			f.check(t, v, "")
			f.check(t, v, "") // Local observation can repeat without changing facts.
			before := f.snapshot(t, v)
			var oldError error
			called := false
			result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.request.ProjectID, foundation.Exclusive)}); err != nil {
					return err
				}
				called = true
				oldError = f.authority.ValidateInitializationInTx(ctx, tx, v.actor, v.request.CreationID, v.request.ProjectID, v.request.InitializationKey)
				return oldError
			})
			if !called {
				t.Fatal("old write gate was not reached", result.Fault())
			}
			if state == c.CreationAccepted || state == c.CreationFailed {
				requireCode(t, oldError, foundation.InvalidState)
				if result.State() != foundation.NotCommitted {
					t.Fatal("old write gate unexpectedly committed")
				}
			} else {
				if oldError != nil {
					t.Fatal(oldError)
				}
				convergenceCommitted(t, result)
			}
			if f.snapshot(t, v) != before {
				t.Fatal("old gate changed facts")
			}
		})
	}
	t.Run("accepted_request_metadata_mismatch", func(t *testing.T) {
		v := f.seed(t, c.CreationAccepted)
		f.change(t, v, `UPDATE agenteam_project.creations SET request_name='Different' WHERE id=$1`, v.request.CreationID.String())
		f.check(t, v, foundation.DependencyUnavailable)
	})
	t.Run("initializing_owner_mismatch", func(t *testing.T) {
		v := f.seed(t, c.CreationInitializing)
		f.change(t, v, `UPDATE agenteam_project.creations SET owner_user_id=$2 WHERE id=$1`, v.request.CreationID.String(), id[identity.User](t).String())
		f.check(t, v, foundation.DependencyUnavailable)
	})
	t.Run("failed_initialized", func(t *testing.T) {
		v := f.seed(t, c.CreationFailed)
		f.change(t, v, `UPDATE agenteam_project.projects SET initialized_at=created_at WHERE id=$1`, v.request.ProjectID.String())
		f.check(t, v, foundation.DependencyUnavailable)
	})
	t.Run("completed_uninitialized", func(t *testing.T) {
		v := f.seed(t, c.CreationCompleted)
		f.change(t, v, `UPDATE agenteam_project.projects SET initialized_at=NULL WHERE id=$1`, v.request.ProjectID.String())
		f.check(t, v, foundation.DependencyUnavailable)
	})
	for _, field := range []string{"id", "owner", "version"} {
		t.Run("completed_bad_history_"+field, func(t *testing.T) {
			v := f.seed(t, c.CreationCompleted)
			bad := v.initial
			switch field {
			case "id":
				bad.ID = id[identity.Project](t)
			case "owner":
				bad.OwnerUserID = id[identity.User](t)
			case "version":
				bad.Version = 2
			}
			if err := bad.Validate(); err != nil {
				t.Fatal("negative must be a legal but contextually wrong historical ref", err)
			}
			raw, err := json.Marshal(bad)
			if err != nil {
				t.Fatal(err)
			}
			f.change(t, v, `UPDATE agenteam_project.creations SET safe_result=$2::jsonb WHERE id=$1`, v.request.CreationID.String(), raw)
			f.check(t, v, foundation.DependencyUnavailable)
		})
	}
	t.Run("completed_current_rename_is_not_history", func(t *testing.T) {
		v := f.seed(t, c.CreationCompleted)
		f.change(t, v, `UPDATE agenteam_project.projects SET name='Renamed',normalized_name='renamed',description='later description',version=8,updated_at=clock_timestamp() WHERE id=$1`, v.request.ProjectID.String())
		f.check(t, v, "")
		var current, historical string
		if err := f.store.QueryRow(ctxFor(t), `SELECT p.name,c.safe_result->>'name' FROM agenteam_project.projects p JOIN agenteam_project.creations c ON c.id=p.creation_id WHERE p.id=$1`, v.request.ProjectID.String()).Scan(&current, &historical); err != nil {
			t.Fatal(err)
		}
		if current != "Renamed" || historical != v.initial.Name {
			t.Fatal("current metadata and initial result were conflated")
		}
	})
	for _, kind := range []string{"initialization_key", "command_key", "project_pair"} {
		t.Run("caller_mismatch_"+kind, func(t *testing.T) {
			v := f.seed(t, c.CreationAccepted)
			switch kind {
			case "initialization_key":
				v.request.InitializationKey = "another-valid-key"
			case "command_key":
				v.request.InitializationKey = foundation.IdempotencyKey("create-" + v.request.CreationID.String())
			case "project_pair":
				other := f.seed(t, c.CreationAccepted)
				v.request.ProjectID = other.request.ProjectID
				v.actor = convergenceActor(t, v.request)
			}
			f.check(t, v, foundation.Forbidden)
		})
	}
	for _, lifecycle := range []c.Lifecycle{c.Archiving, c.Archived, c.Deleting} {
		t.Run("lifecycle_"+string(lifecycle), func(t *testing.T) {
			v := f.seed(t, c.CreationCompleted)
			if lifecycle == c.Archived {
				f.change(t, v, `UPDATE agenteam_project.projects SET lifecycle='archived',archived_at=created_at WHERE id=$1`, v.request.ProjectID.String())
			} else {
				operation := id[c.Operation](t)
				action := "archive"
				if lifecycle == c.Deleting {
					action = "delete"
				}
				// Test-owned canonical lifecycle input; no participant or domain completion.
				f.change(t, v, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,$4,1,'accepted',1,'[]'::jsonb,'sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',clock_timestamp(),clock_timestamp())`, operation.String(), v.request.ProjectID.String(), v.owner.String(), action)
				f.change(t, v, `UPDATE agenteam_project.projects SET lifecycle=$2,current_lifecycle_operation_id=$3 WHERE id=$1`, v.request.ProjectID.String(), string(lifecycle), operation.String())
			}
			f.check(t, v, foundation.Forbidden)
		})
	}
}

func TestProjectInitializationConvergenceTransactionBoundary(t *testing.T) {
	f := newInitializationConvergencePG(t)
	v := f.seed(t, c.CreationAccepted)
	for _, kind := range []string{"missing", "shared", "other_project", "foreign_store", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			before := f.snapshot(t, v)
			store := f.store
			locks := []foundation.LockRequest{convergenceLock(v.request.ProjectID, foundation.Exclusive)}
			switch kind {
			case "missing":
				locks = nil
			case "shared":
				locks[0].Mode = foundation.Shared
			case "other_project":
				locks[0] = convergenceLock(id[identity.Project](t), foundation.Exclusive)
			case "foreign_store":
				store = f.other
			}
			var gateError error
			called := false
			result := store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
				if len(locks) != 0 {
					if err := store.AcquireAll(ctx, tx, locks); err != nil {
						return err
					}
				}
				if kind == "cancelled" {
					child, cancel := context.WithCancel(ctx)
					cancel()
					ctx = child
				}
				called = true
				gateError = f.authority.ValidateInitializationConvergenceInTx(ctx, tx, v.actor, v.request)
				return nil // Deliberately ignore rejection; real Store must remain poisoned.
			})
			if !called {
				t.Fatal("did not reach the gate", result.Fault())
			}
			requireCode(t, gateError, foundation.DependencyUnavailable)
			if result.State() != foundation.NotCommitted {
				t.Fatal("ignored rejected gate escaped transaction poison", result.State())
			}
			if before != f.snapshot(t, v) {
				t.Fatal("rejected gate changed facts")
			}
		})
	}
	t.Run("expired_real_transaction", func(t *testing.T) {
		var expired foundation.Tx
		before := f.snapshot(t, v)
		result := f.store.WithinTx(ctxFor(t), cause(t), func(ctx context.Context, tx foundation.Tx) error {
			expired = tx
			return f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.request.ProjectID, foundation.Exclusive)})
		})
		convergenceCommitted(t, result)
		requireCode(t, f.authority.ValidateInitializationConvergenceInTx(ctxFor(t), expired, v.actor, v.request), foundation.DependencyUnavailable)
		if before != f.snapshot(t, v) {
			t.Fatal("expired token caused side effects")
		}
	})
	t.Run("closed_store", func(t *testing.T) {
		closed := convergenceOpenStore(t, f.db)
		accountKeys, _ := keys(t)
		accounts, err := account.NewAuthority(closed, accountKeys)
		if err != nil {
			t.Fatal(err)
		}
		authority, err := project.NewAuthority(closed, project.AuthorityDependencies{Sessions: accounts})
		if err != nil {
			t.Fatal(err)
		}
		var expired foundation.Tx
		convergenceCommitted(t, closed.WithinTx(ctxFor(t), cause(t), func(_ context.Context, tx foundation.Tx) error { expired = tx; return nil }))
		if err := closed.Drain(ctxFor(t)); err != nil {
			t.Fatal("closed Store did not actually drain", err)
		}
		before := f.snapshot(t, v)
		requireCode(t, authority.ValidateInitializationConvergenceInTx(ctxFor(t), expired, v.actor, v.request), foundation.DependencyUnavailable)
		if before != f.snapshot(t, v) {
			t.Fatal("closed Store gate changed facts")
		}
	})
	for _, mode := range []foundation.LockMode{foundation.Shared, foundation.Exclusive} {
		name := "shared"
		if mode == foundation.Exclusive {
			name = "exclusive"
		}
		t.Run("caller_EX_blocks_"+name+"_after_gate_returns", func(t *testing.T) {
			convergenceLockSurvives(t, f, v, mode)
		})
	}
}

func convergenceLockSurvives(t *testing.T, f *initializationConvergencePG, v initializationConvergenceCase, mode foundation.LockMode) {
	t.Helper()
	before := f.snapshot(t, v)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	release := make(chan struct{})
	var releaseOnce sync.Once
	letGo := func() { releaseOnce.Do(func() { close(release) }) }
	ownerDone, waiterDone := make(chan struct{}), make(chan struct{})
	ownerResults, waiterResults := make(chan foundation.CommitResult, 1), make(chan foundation.CommitResult, 1)
	ownerPID, waiterPID := make(chan int, 1), make(chan int, 1)
	ownerCause, waiterCause := cause(t), cause(t)
	waiterStarted := false
	// Independent done channels prove actual join even after result consumption or Fatal.
	t.Cleanup(func() {
		cancel()
		letGo()
		<-ownerDone
		if waiterStarted {
			<-waiterDone
		}
	})
	go func() {
		defer close(ownerDone)
		ownerResults <- f.store.WithinTx(ctx, ownerCause, func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.request.ProjectID, foundation.Exclusive)}); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			var pid int
			if err := x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			if err := f.authority.ValidateInitializationConvergenceInTx(ctx, tx, v.actor, v.request); err != nil {
				return err
			}
			ownerPID <- pid // Gate returned successfully; caller's transaction remains open.
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var owner int
	select {
	case owner = <-ownerPID:
	case result := <-ownerResults:
		t.Fatal("owner exited before successful gate", result.State(), result.Fault())
	case <-ctx.Done():
		t.Fatal("owner gate did not finish")
	}
	waiterStarted = true
	go func() {
		defer close(waiterDone)
		waiterResults <- f.other.WithinTx(ctx, waiterCause, func(ctx context.Context, tx foundation.Tx) error {
			x, err := f.other.InTx(tx)
			if err != nil {
				return err
			}
			var pid int
			if err := x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			waiterPID <- pid
			return f.other.AcquireAll(ctx, tx, []foundation.LockRequest{convergenceLock(v.request.ProjectID, mode)})
		})
	}()
	var waiter int
	select {
	case waiter = <-waiterPID:
	case result := <-waiterResults:
		t.Fatal("waiter exited before lock request", result.State(), result.Fault())
	case <-ctx.Done():
		t.Fatal("waiter did not start")
	}
	if owner <= 0 || waiter <= 0 || owner == waiter {
		t.Fatal("holder identities were not distinct PostgreSQL backends")
	}
	poll := time.NewTicker(10 * time.Millisecond)
	defer poll.Stop()
	for {
		var blocked bool
		err := f.store.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity a JOIN pg_locks l ON l.pid=a.pid WHERE a.pid=$1 AND a.wait_event_type='Lock' AND l.locktype='advisory' AND NOT l.granted AND $2=ANY(pg_blocking_pids(a.pid)))`, waiter, owner).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case result := <-waiterResults:
			t.Fatal("waiter escaped the caller's still-open EX transaction", result.State(), result.Fault())
		case <-ctx.Done():
			t.Fatal("no physical lock wait on exact owner backend")
		case <-poll.C:
		}
	}
	letGo()
	for _, result := range []chan foundation.CommitResult{ownerResults, waiterResults} {
		select {
		case value := <-result:
			convergenceCommitted(t, value)
		case <-ctx.Done():
			t.Fatal("released holder did not reach transaction terminal")
		}
	}
	<-ownerDone
	<-waiterDone
	if before != f.snapshot(t, v) {
		t.Fatal("lock-survival checks wrote facts")
	}
}
