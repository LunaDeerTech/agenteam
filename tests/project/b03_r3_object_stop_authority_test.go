//go:build integration

package project_test

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	obj "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func r3ObjectRequest(t *testing.T, o c.LifecycleOperation, actor identity.Actor, component obj.ProjectStopComponent, step obj.ProjectStopStep) obj.ProjectStopRequest {
	t.Helper()
	op, _ := foundation.ParseID[obj.ProjectStopOperation](o.ID.String())
	cause, err := obj.NewProjectStopCause(obj.ProjectStopCauseDetails{ProjectID: o.ProjectID, OperationID: op, Action: obj.ProjectStopAction(o.Action), ProjectVersion: o.ProjectVersion})
	if err != nil {
		t.Fatal(err)
	}
	request, err := obj.NewProjectStopRequest(obj.ProjectStopRequestDetails{Actor: actor, Cause: cause, Component: component, Step: step})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

// This exercises Project's formal typed port, not a D05 runtime stop result.
func TestProjectStopAuthorityCurrentAndTerminal(t *testing.T) {
	f := newR3Fixture(t)
	for _, phase := range []string{"accepted", "stopping", "failed", "cleaning", "completed", "deleted"} {
		t.Run(phase, func(t *testing.T) {
			var o c.LifecycleOperation
			var actor identity.Actor
			if phase == "deleted" {
				o, actor, _ = f.deletion(t)
			} else {
				action := c.Archive
				if phase == "cleaning" {
					action = c.Delete
				}
				o, actor, _ = f.accept(t, action)
				if phase != "accepted" {
					f.phase(t, o, c.OperationState(phase))
				}
			}
			for _, component := range []obj.ProjectStopComponent{obj.ObjectStopComponent, obj.ArtifactStopComponent} {
				for _, step := range []obj.ProjectStopStep{obj.RequestProjectStopStep, obj.InspectProjectStopStep} {
					request := r3ObjectRequest(t, o, actor, component, step)
					plan, err := f.facts.DiscoverProjectStop(ctxFor(t), request)
					if err != nil {
						t.Fatal(err)
					}
					var grant obj.ProjectStopAuthorization
					err = f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
						var err error
						grant, err = f.facts.ValidateProjectStopInTx(ctx, tx, request, plan)
						if err == nil && (!grant.Matches(tx, request, plan) || grant.Matches(foundation.NewTx(), request, plan)) {
							t.Fatal("grant identity mismatch")
						}
						return err
					})
					allowed := phase == "stopping" || phase != "accepted" && step == obj.InspectProjectStopStep
					if !allowed {
						requireCode(t, err, foundation.InvalidState)
						if grant.Validate() == nil {
							t.Fatal("failed validation returned grant")
						}
						continue
					}
					if err != nil {
						t.Fatal(component, step, err)
					}
					mode := obj.ReadProjectStop
					if phase == "stopping" {
						mode = obj.ContinueProjectStop
					}
					if grant.Mode() != mode {
						t.Fatal("wrong authority mode", grant.Mode())
					}
				}
			}
			if phase == "deleted" {
				// The minimal receipt has no version. Only Object's own receipt can
				// authenticate it; Project still issues a read-only locator.
				o.ProjectVersion++
				request := r3ObjectRequest(t, o, actor, obj.ObjectStopComponent, obj.InspectProjectStopStep)
				plan, _ := f.facts.DiscoverProjectStop(ctxFor(t), request)
				err := f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
					grant, err := f.facts.ValidateProjectStopInTx(ctx, tx, request, plan)
					if err == nil && grant.Mode() != obj.ReadProjectStop {
						t.Fatal("deleted cause allowed continuation")
					}
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	o, actor, _ := f.accept(t, c.Archive)
	f.phase(t, o, c.OperationStopping)
	request := r3ObjectRequest(t, o, actor, obj.ObjectStopComponent, obj.RequestProjectStopStep)
	plan, _ := f.facts.DiscoverProjectStop(ctxFor(t), request)
	o.ProjectVersion++
	changed := r3ObjectRequest(t, o, actor, obj.ObjectStopComponent, obj.RequestProjectStopStep)
	err := f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
		_, err := f.facts.ValidateProjectStopInTx(ctx, tx, changed, plan)
		return err
	})
	requireCode(t, err, foundation.Forbidden)
	f.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active',current_lifecycle_operation_id=NULL,version=version+1 WHERE id=$1`, o.ProjectID.String())
	err = f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
		_, err := f.facts.ValidateProjectStopInTx(ctx, tx, request, plan)
		return err
	})
	requireCode(t, err, foundation.Forbidden)
}
