//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestProjectLifecycleLocalStopRound(t *testing.T) {
	v := newVariableLifecycleFixture(t)
	process := id[oc.Process](t)
	t.Run("committed-phase-and-local-return", func(t *testing.T) {
		p := v.project
		foreign, _, _ := v.createProject(t, v.ownerBrowser.actor, "phase-foreign")
		plain, secretInput, foreignInput := createInput(t, "PHASE_PLAIN", "value"), secretCreateInput(t, "PHASE_SECRET", []byte("value")), createInput(t, "PHASE_FOREIGN", "value")
		pm, sm, fm := meta(t, "phase-plain", nil), meta(t, "phase-secret", nil), meta(t, "phase-foreign", nil)
		calls := []*lifecycleOriginalCall{
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, err := v.ordinary.CreateVariable(ctx, v.ownerBrowser.actor, pm, p.ID, plain)
				return err
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, err := v.secretOwner.CreateSecretVariable(ctx, v.ownerBrowser.actor, sm, p.ID, secretInput)
				return err
			}),
			beginLifecycleCall(t, func(ctx context.Context) error {
				_, err := v.ordinary.CreateVariable(ctx, v.ownerBrowser.actor, fm, foreign.ID, foreignInput)
				return err
			}),
		}
		cause := phaseAccepted(t, v, p)
		phaseGate, stepGate, checkpointGate := newPhaseRelease(), newPhaseRelease(), newPhaseRelease()
		phaseEntered, stepEntered, checkpointEntered := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var txNumber atomic.Int32
		v.tracked.setAfter(func(ctx context.Context, tx f.Tx, c f.TransactionCause) error {
			if c.Details().Owner != "project-lifecycle" {
				return nil
			}
			switch txNumber.Add(1) {
			case 1:
				close(phaseEntered)
				<-phaseGate.done
			case 2:
				close(checkpointEntered)
				<-checkpointGate.done
			}
			return nil
		})
		t.Cleanup(func() { v.tracked.setAfter(nil) })
		step := func(ctx context.Context, actor i.Actor, actual pc.LifecycleCause, scope pc.ScopeRef) error {
			if actual != cause || scope.ProjectID != p.ID {
				return errors.New("phase substituted original cause")
			}
			report, err := v.stopper.RequestStop(ctx, actor, actual, scope)
			if err != nil {
				return err
			}
			if !report.Matches(cause, scope) || report.Details().PendingCalls != 2 || report.Details().LocalJoined {
				return errors.New("local original calls missing")
			}
			close(stepEntered)
			<-stepGate.done
			report, err = v.stopper.InspectStop(ctx, actor, actual, scope)
			if err != nil {
				return err
			}
			if !report.Matches(cause, scope) || !report.Details().LocalJoined || report.Details().PendingCalls != 0 {
				return errors.New("original local calls not joined")
			}
			return nil
		}
		driver := phaseDriver(t, v, process, v.lifecycle, step)
		run := phaseRun(t, driver, p.ID, cause.OperationID, context.Background(), phaseGate, stepGate, checkpointGate)
		phaseAwait(t, phaseEntered)
		phaseState(t, v, cause, "accepted", "", 0)
		for _, call := range calls {
			call.requireCanceled(t, false)
		}
		select {
		case <-stepEntered:
			t.Fatal("provider called before phase COMMIT")
		default:
		}
		phaseGate.release()
		phaseAwait(t, stepEntered)
		phaseState(t, v, cause, "stopping", "running", 1)
		calls[0].requireCanceled(t, true)
		calls[1].requireCanceled(t, true)
		calls[2].requireCanceled(t, false)
		phaseMustPending(t, driver, run)
		// A second driver with the same process has no original local return proof.
		other := phaseDriver(t, v, process, v.lifecycle, func(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) error {
			return errors.New("unexpected competing provider")
		})
		err := other.Run(ctxFor(t), p.ID, cause.OperationID)
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.ResourceBusy {
			t.Fatal("live claim taken over", err)
		}
		calls[0].join(t, false)
		calls[1].join(t, false)
		stepGate.release()
		phaseAwait(t, checkpointEntered)
		phaseState(t, v, cause, "stopping", "running", 1)
		phaseMustPending(t, driver, run)
		checkpointGate.release()
		phaseAwait(t, run.done)
		if run.err != nil {
			t.Fatal("phase round", run.err)
		}
		phaseState(t, v, cause, "stopping", "terminal", 1)
		calls[2].requireCanceled(t, false)
		calls[2].join(t, true)
	})
	t.Run("rollback-and-frozen-manifest", func(t *testing.T) {
		p, _, _ := v.createProject(t, v.ownerBrowser.actor, "phase-reject")
		cause := phaseAccepted(t, v, p)
		var calls atomic.Int32
		step := func(context.Context, i.Actor, pc.LifecycleCause, pc.ScopeRef) error { calls.Add(1); return nil }
		driver := phaseDriver(t, v, process, v.lifecycle, step)
		v.tracked.setAfter(func(_ context.Context, _ f.Tx, c f.TransactionCause) error {
			if c.Details().Owner == "project-lifecycle" {
				return f.NewFault(f.DependencyUnavailable, f.NotStarted)
			}
			return nil
		})
		t.Cleanup(func() { v.tracked.setAfter(nil) })
		if err := driver.Run(ctxFor(t), p.ID, cause.OperationID); err == nil {
			t.Fatal("real callback rollback passed")
		}
		phaseState(t, v, cause, "accepted", "", 0)
		if calls.Load() != 0 {
			t.Fatal("rolled-back phase called provider")
		}
		v.tracked.setAfter(nil)
		entries := v.manifest.Entries()
		for n := range entries {
			entries[n].ContractVersion++
		}
		different, err := pc.NewRequiredManifest(entries)
		if err != nil {
			t.Fatal(err)
		}
		wrongAuthority, err := project.NewLifecycleAuthority(v.tracked, different, nil)
		if err != nil {
			t.Fatal(err)
		}
		wrong := phaseDriver(t, v, process, wrongAuthority, step)
		if err = wrong.Run(ctxFor(t), p.ID, cause.OperationID); err == nil {
			t.Fatal("unknown frozen contract accepted")
		}
		phaseState(t, v, cause, "accepted", "", 0)
		if calls.Load() != 0 {
			t.Fatal("missing frozen binding called provider")
		}
	})
	t.Run("claim-fencing-and-terminal-attempt", func(t *testing.T) {
		p, _, _ := v.createProject(t, v.ownerBrowser.actor, "phase-fence")
		cause := phaseAccepted(t, v, p)
		step := func(ctx context.Context, actor i.Actor, actual pc.LifecycleCause, scope pc.ScopeRef) error {
			report, err := v.stopper.InspectStop(ctx, actor, actual, scope)
			if err != nil {
				return err
			}
			if actual != cause || !report.Matches(cause, scope) || !report.Details().LocalJoined {
				return errors.New("wrong current local observation")
			}
			return nil
		}
		first := phaseDriver(t, v, process, v.lifecycle, step)
		if err := first.Run(ctxFor(t), p.ID, cause.OperationID); err != nil {
			t.Fatal(err)
		}
		phaseState(t, v, cause, "stopping", "terminal", 1)
		stale := phaseDriver(t, v, process, v.lifecycle, step)
		fresh := phaseDriver(t, v, process, v.lifecycle, step)
		type fenceMarker struct{}
		entered := make(chan struct{})
		release := newPhaseRelease()
		var once sync.Once
		var observedPID atomic.Int32
		key, _ := f.ProjectLock(p.ID.String())
		v.tracked.mu.Lock()
		v.tracked.beforeLocks = func(ctx context.Context, tx f.Tx, locks []f.LockRequest) error {
			if ctx.Value(fenceMarker{}) != true {
				return nil
			}
			for _, lock := range locks {
				if lock.Mode == f.Exclusive && f.CompareLockKeys(lock.Key, key) == 0 {
					x, err := v.tracked.InTx(tx)
					if err != nil {
						return err
					}
					var pid int32
					if err = x.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
						return err
					}
					if pid <= 0 {
						return errors.New("original phase BEGIN PID missing")
					}
					observedPID.Store(pid)
					once.Do(func() { close(entered); <-release.done })
				}
			}
			return nil
		}
		v.tracked.mu.Unlock()
		t.Cleanup(func() { v.tracked.mu.Lock(); v.tracked.beforeLocks = nil; v.tracked.mu.Unlock() })
		pending := phaseRun(t, stale, p.ID, cause.OperationID, context.WithValue(context.Background(), fenceMarker{}, true), release)
		phaseAwait(t, entered)
		if observedPID.Load() <= 0 {
			t.Fatal("stale round did not enter its original BEGIN")
		}
		if err := fresh.Run(ctxFor(t), p.ID, cause.OperationID); err != nil {
			t.Fatal("confirmed terminal attempt could not advance fence", err)
		}
		phaseState(t, v, cause, "stopping", "terminal", 2)
		release.release()
		phaseAwait(t, pending.done)
		var fault *f.Fault
		if !errors.As(pending.err, &fault) || fault.Code != f.ResourceBusy {
			t.Fatal("stale fence replaced current attempt", pending.err)
		}
		phaseState(t, v, cause, "stopping", "terminal", 2)
	})
}
