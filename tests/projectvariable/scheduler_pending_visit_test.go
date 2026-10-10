//go:build integration

package projectvariable_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/scheduler"
)

// These cases use the existing real Claim/Launch/Busy composition. They add
// only bounded visits, real Owner pause/resume and an original association
// callback rollback. No SQL constructs Task/Dispatch/Execution success.
func TestSchedulerPendingVisit(t *testing.T) {
	t.Run("paused-enumeration-and-resume", func(t *testing.T) {
		x, visitor := newSchedulerPendingVisitFixture(t)
		setPendingVisitEnabled(t, x.task, false, "pending-visit-pause")
		before, err := schedulerClaimSnapshot(ctxFor(t), x.task.base.raw, x.task)
		if err != nil {
			t.Fatal("paused snapshot", err)
		}
		paused, err := visitor.VisitNext(ctxFor(t), x.request.ProjectID, nil)
		if err != nil || !paused.Found || paused.After == nil || *paused.After != x.dispatch || paused.Action != scheduler.PendingVisitDeferred {
			t.Fatal("paused visit did not enumerate the original pending", err)
		}
		summary := paused.Dispatch.Summary()
		if summary.ID != x.dispatch || summary.Status != scheduler.Pending || summary.LaunchOutcome != scheduler.NotSent || summary.Version != 1 || summary.AttemptCount != 0 || summary.ExecutionID != nil {
			t.Fatal("paused visit changed the original unattempted intent")
		}
		end, err := visitor.VisitNext(ctxFor(t), x.request.ProjectID, paused.After)
		if err != nil || end.Found || end.After != nil {
			t.Fatal("bounded cursor repeated a pending row", err)
		}
		after, err := schedulerClaimSnapshot(ctxFor(t), x.task.base.raw, x.task)
		launches, lookups, created := x.launcher.observed()
		if err != nil || before != after || launches != 0 || lookups != 0 || created.ID.Validate() == nil {
			t.Fatal("paused enumeration changed persistent state or called Execution", err)
		}
		setPendingVisitEnabled(t, x.task, true, "pending-visit-resume")
		beforeTask := x.task.databaseSnapshot(t)
		launched, err := visitor.VisitNext(ctxFor(t), x.request.ProjectID, nil)
		if err != nil || !launched.Found || launched.After == nil || *launched.After != x.dispatch || launched.Action != scheduler.PendingVisitLaunch {
			t.Fatal("resume did not visit the same original intent", err)
		}
		x.requireAssociated(t, launched.Dispatch)
		stable, err := schedulerClaimSnapshot(ctxFor(t), x.task.base.raw, x.task)
		if err != nil {
			t.Fatal("associated snapshot", err)
		}
		// The committed row is no longer pending. A fresh caller-paced pass
		// must not redispatch it, even without carrying the previous cursor.
		end, err = visitor.VisitNext(ctxFor(t), x.request.ProjectID, nil)
		launches, _, _ = x.launcher.observed()
		after, snapshotErr := schedulerClaimSnapshot(ctxFor(t), x.task.base.raw, x.task)
		if err != nil || end.Found || launches != 1 || snapshotErr != nil || stable != after || beforeTask != x.task.databaseSnapshot(t) {
			t.Fatal("completed visit replay dispatched or rewrote Task/history", err, snapshotErr)
		}
	})
	t.Run("association-rollback-original-lookup", func(t *testing.T) {
		x, visitor := newSchedulerPendingVisitFixture(t)
		beforeTask := x.task.databaseSnapshot(t)
		command, err := f.NewCommandIdentity("scheduler", []string{x.request.ProjectID.String()}, "associate_launch", x.request.Meta.IdempotencyKey)
		if err != nil {
			t.Fatal(err)
		}
		marker := errors.New("pending-visit-association-callback-rollback")
		var observed, returned bool
		var physical f.CommitResult
		store := x.task.base.tracked
		store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
			if cause.Kind() != f.CommandsCause || cause.Details().Primary.Canonical() != command.Canonical() {
				return nil
			}
			_, _, created := x.launcher.observed()
			if created.ID.Validate() != nil {
				return errors.New("association preceded actual Execution return")
			}
			sql, err := store.InTx(tx)
			if err != nil {
				return err
			}
			if err = schedulerLaunchFacts(ctx, sql, x.request, created.ID, true); err != nil {
				return err
			}
			observed = true
			return marker
		})
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			if observed && cause.Kind() == f.CommandsCause && cause.Details().Primary.Canonical() == command.Canonical() {
				physical, returned = result, true
			}
		}
		store.mu.Unlock()
		clear := func() { store.setAfter(nil); store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
		defer clear()
		result, err := visitor.VisitNext(ctxFor(t), x.request.ProjectID, nil)
		clear()
		if !observed || !returned || physical.State() != f.NotCommitted || !errors.Is(physical.Fault(), marker) || !errors.Is(err, marker) || !result.Found || result.After == nil || *result.After != x.dispatch || result.Action != scheduler.PendingVisitLaunch || result.Dispatch.Summary().Status == scheduler.Launched {
			t.Fatal("original rollback was lost or uncommitted association escaped", err)
		}
		launches, lookups, created := x.launcher.observed()
		if launches != 1 || created.ID.Validate() != nil || created.Status != ec.Created {
			t.Fatal("Execution did not really commit before association rollback")
		}
		if err = schedulerLaunchFacts(ctxFor(t), x.task.base.raw, x.request, created.ID, false); err != nil {
			t.Fatal("rollback did not retain original pending and created Execution", err)
		}
		requireSchedulerLaunchObservation(t, x.task, x.request, created.ID, false)
		// Restart the existing traversal pass; there is no automatic retry or
		// fake CommitUnknown. This same handoff owns the uncertain association.
		recovered, err := visitor.VisitNext(ctxFor(t), x.request.ProjectID, nil)
		if err != nil || !recovered.Found || recovered.After == nil || *recovered.After != x.dispatch || recovered.Action != scheduler.PendingVisitLookup {
			t.Fatal("original pending did not select Lookup", err)
		}
		x.requireAssociated(t, recovered.Dispatch)
		afterLaunches, afterLookups, same := x.launcher.observed()
		if afterLaunches != 1 || afterLookups != lookups+1 || same.ID != created.ID || beforeTask != x.task.databaseSnapshot(t) {
			t.Fatal("Lookup recovery resent Launch, replaced identity or changed Work")
		}
	})
}

func newSchedulerPendingVisitFixture(t *testing.T) (*schedulerLaunchFixture, *scheduler.PendingVisitor) {
	t.Helper()
	x := newSchedulerLaunchFixture(t)
	compensator := newBusyCompensator(t, x.task)
	visitor, err := scheduler.NewPendingVisitor(x.task.pending, x.handoff, compensator)
	if err != nil {
		t.Fatal("real same-owner pending visitor", err)
	}
	t.Cleanup(func() {
		visitor.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := visitor.Drain(ctx); err != nil || !visitor.Joined() {
			t.Error("original pending visit did not join", err)
		}
	})
	return x, visitor
}

func setPendingVisitEnabled(t *testing.T, v *taskTransitionFixture, enabled bool, key string) {
	t.Helper()
	ctx, actor, project := ctxFor(t), v.base.ownerBrowser.actor, v.base.project.ID
	before, err := v.base.projects.GetSchedulerConfig(ctx, actor, project)
	if err != nil || before.Config.Enabled == enabled {
		t.Fatal("real configuration did not need the requested transition", err)
	}
	config := before.Config.Clone()
	config.Enabled = enabled
	updated, err := v.base.projects.UpdateProject(ctx, actor, meta(t, key, &before.Project.Version), project, pc.UpdateProjectRequest{Scheduler: &config})
	if err != nil || updated.ID != project || updated.Version != before.Project.Version+1 {
		t.Fatal("real Owner pause/resume failed", err)
	}
	current, err := v.base.projects.GetSchedulerConfig(ctx, actor, project)
	if err != nil || !reflect.DeepEqual(current.Project, updated) || !reflect.DeepEqual(current.Config, config) || !reflect.DeepEqual(current.Project.CurrentSprintID, before.Project.CurrentSprintID) {
		t.Fatal("pause/resume changed more than the committed configuration", err)
	}
	v.base.project = current.Project
}
