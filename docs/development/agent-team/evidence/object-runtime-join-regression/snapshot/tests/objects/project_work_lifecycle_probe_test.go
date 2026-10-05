//go:build integration

package objects_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
)

// This is ordinary lock contention after a committed work registration. It
// does not emulate a lost ROLLBACK response or preserve an abandoned backend.
func TestObjectProjectWorkPendingJoinKeepsRuntimeGuard(t *testing.T) {
	f := newFixture(t, false)
	process := id[oc.Process](t)
	spoolPath := filepath.Join(t.TempDir(), "spool")
	runtime, service, _ := runtimeAt(t, f, spoolPath, process, f.authority)
	if err := runtime.Initialize(contextFor(t)); err != nil {
		t.Fatal("normal runtime initialization", err)
	}

	body := &stopBlockedBody{entered: make(chan struct{}), release: make(chan struct{})}
	var bodyOnce, holderOnce sync.Once
	releaseBody := func() { bodyOnce.Do(func() { close(body.release) }) }
	holderRelease := make(chan struct{})
	releaseHolder := func() { holderOnce.Do(func() { close(holderRelease) }) }
	publicDone := make(chan error, 1)
	publicObserved := false
	holderDone := make(chan foundation.CommitResult, 1)
	holderStarted, holderObserved := false, false
	var holderCancel context.CancelFunc
	t.Cleanup(func() {
		releaseBody()
		releaseHolder()
		if holderCancel != nil {
			defer holderCancel()
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if holderStarted && !holderObserved {
			select {
			case <-holderDone:
				holderObserved = true
			case <-cleanup.Done():
				if holderCancel != nil {
					holderCancel()
				}
				t.Error("ordinary holder did not finish within cleanup budget")
			}
		}
		if !publicObserved {
			select {
			case <-publicDone:
				publicObserved = true
			case <-cleanup.Done():
				t.Error("local body call did not return within cleanup budget")
			}
		}
	})
	publicCtx := contextFor(t)
	go func() {
		_, err := service.PreparePayload(publicCtx, f.actor, f.owner, "text/plain", 1, nil, body)
		publicDone <- err
	}()
	select {
	case <-body.entered:
	case err := <-publicDone:
		publicObserved = true
		t.Fatal("preparation returned before local body barrier", err)
	case <-publicCtx.Done():
		t.Fatal("local body barrier not reached")
	}

	var workID string
	var unjoined bool
	if err := f.store.QueryRow(contextFor(t), `SELECT id::text,joined_at IS NULL FROM agenteam_object.project_work WHERE project_id=$1 AND process_id=$2 AND kind='preparation'`, f.project.String(), process.String()).Scan(&workID, &unjoined); err != nil || !unjoined {
		t.Fatal("work was not committed before the local Read", err)
	}
	workCommand, err := foundation.NewCommandIdentity("object", []string{workID}, "project-work", "lifetime")
	if err != nil {
		t.Fatal(err)
	}
	workKey, err := foundation.CommandLock(workCommand)
	if err != nil {
		t.Fatal(err)
	}
	cause, err := foundation.NewRecoveryCause("object", workID, "")
	if err != nil {
		t.Fatal(err)
	}
	holderCtx, cancelHolder := context.WithTimeout(contextFor(t), 15*time.Second)
	holderCancel = cancelHolder
	holderReady := make(chan int, 1)
	holderStarted = true
	go func() {
		result := f.store.WithinTx(holderCtx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if err := f.store.AcquireAll(ctx, tx, []foundation.LockRequest{{Key: workKey, Mode: foundation.Exclusive}}); err != nil {
				return err
			}
			x, err := f.store.InTx(tx)
			if err != nil {
				return err
			}
			var pid int
			if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				return err
			}
			holderReady <- pid
			select {
			case <-holderRelease:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		holderDone <- result
	}()
	var holderPID int
	select {
	case holderPID = <-holderReady:
	case result := <-holderDone:
		holderObserved = true
		t.Fatal("ordinary holder failed before acquiring work mutex", result.State())
	case <-holderCtx.Done():
		t.Fatal("ordinary holder did not acquire work mutex")
	}
	t.Logf("ordinary holder ready: work=%s backend=%d; no proxy", workID, holderPID)
	releaseBody()
	select {
	case err = <-publicDone:
		publicObserved = true
		if err == nil {
			t.Fatal("short local body unexpectedly prepared a payload")
		}
		t.Log("public preparation returned its normal short-body error:", err)
	case <-publicCtx.Done():
		t.Fatal("public preparation did not return after local body release")
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT joined_at IS NULL FROM agenteam_object.project_work WHERE id=$1`, workID).Scan(&unjoined); err != nil || !unjoined {
		t.Fatal("ordinary work mutex did not preserve pending join", err)
	}
	var holderStillInTx bool
	if err = f.store.QueryRow(contextFor(t), `SELECT state='idle in transaction' AND xact_start IS NOT NULL FROM pg_stat_activity WHERE pid=$1`, holderPID).Scan(&holderStillInTx); err != nil || !holderStillInTx {
		t.Fatal("normal holder was not still in its explicitly owned transaction", err)
	}

	runtime.StopAdmission()
	drainCtx, cancelDrain := context.WithTimeout(contextFor(t), 150*time.Millisecond)
	drainErr := runtime.Drain(drainCtx)
	cancelDrain()
	if drainErr == nil {
		t.Error("Runtime.Drain completed while real registered work was still unjoined")
	}
	var claim string
	if err = f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&claim); err != nil {
		t.Fatal(err)
	}
	if claim != "claimed" {
		t.Errorf("pending work did not keep process claim active: state=%s", claim)
	}
	claimFile, err := os.OpenFile(filepath.Join(spoolPath+".processes", process.String()+".claim"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal("open the exact owned claim file", err)
	}
	lockErr := syscall.Flock(int(claimFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if lockErr == nil {
		_ = syscall.Flock(int(claimFile.Fd()), syscall.LOCK_UN)
		t.Error("ProcessGuard flock was released before pending work joined")
	} else if !errors.Is(lockErr, syscall.EWOULDBLOCK) && !errors.Is(lockErr, syscall.EAGAIN) {
		t.Error("unexpected local flock check error", lockErr)
	}
	if err = claimFile.Close(); err != nil {
		t.Error(err)
	}
	t.Logf("while ordinary holder remains: work_unjoined=%t drain_error=%v claim=%s flock_error=%v", unjoined, drainErr, claim, lockErr)

	releaseHolder()
	select {
	case result := <-holderDone:
		holderObserved = true
		if result.State() != foundation.Committed {
			t.Fatal("ordinary holder did not finish by confirmed commit", result.State())
		}
	case <-holderCtx.Done():
		t.Fatal("released holder did not finish")
	}
	finalCtx, cancelFinal := context.WithTimeout(contextFor(t), 2*time.Second)
	defer cancelFinal()
	if err = runtime.Drain(finalCtx); err != nil {
		t.Error("runtime did not reconcile an ended work after ordinary lock release", err)
	}
	var joined bool
	if err = f.store.QueryRow(contextFor(t), `SELECT joined_at IS NOT NULL FROM agenteam_object.project_work WHERE id=$1`, workID).Scan(&joined); err != nil {
		t.Fatal(err)
	}
	if !joined {
		t.Error("runtime exited without a confirmed project_work join checkpoint")
	}
	if err = f.store.QueryRow(contextFor(t), `SELECT state FROM agenteam_object.process_claims WHERE process_id=$1`, process.String()).Scan(&claim); err != nil || claim != "stopped" {
		t.Error("confirmed joined runtime did not finalize its claim", err, claim)
	}
	t.Logf("after confirmed normal holder release: work_joined=%t claim=%s", joined, claim)
}
