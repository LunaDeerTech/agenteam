//go:build integration

package project_test

import (
	"context"
	"testing"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/outbox"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func projectOutboxRequest(actor identity.Actor, cause oc.LifecycleCause) (oc.ProjectRequest, error) {
	return oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.LifecycleProject, ProjectID: cause.Details().ProjectID, Actor: actor, Lifecycle: cause, LifecycleStep: oc.LifecycleStop})
}

// Only the discovery timing is injected. All lifecycle plans and validations
// still come from the real Project Authority and its persisted facts.
type r3DiscoveryChange struct {
	oc.ProjectAuthority
	afterStop func()
}

func (a r3DiscoveryChange) Discover(ctx context.Context, request oc.ProjectRequest) (oc.Dependencies, error) {
	plan, err := a.ProjectAuthority.Discover(ctx, request)
	if err == nil && request.Details().LifecycleStep == oc.LifecycleStop {
		a.afterStop()
	}
	return plan, err
}

func TestOutboxLifecycleInspectionAuthorityBoundary(t *testing.T) {
	f := newR3Fixture(t)
	for _, change := range []string{"phase", "pointer"} {
		t.Run("changed after Stop plan/"+change, func(t *testing.T) {
			o, actor, cause := f.accept(t, c.Archive)
			f.phase(t, o, c.OperationStopping)
			f.sql(t, `INSERT INTO agenteam_outbox.project_lifecycle(project_id,operation_id,action,project_version,phase) VALUES($1,$2,'archive',$3,'stopping')`, o.ProjectID.String(), o.ID.String(), int64(o.ProjectVersion))
			planned := 0
			authority := r3DiscoveryChange{ProjectAuthority: f.authority, afterStop: func() {
				planned++
				if change == "phase" {
					f.phase(t, o, c.OperationFailed)
				} else {
					f.sql(t, `UPDATE agenteam_project.projects SET lifecycle='active',current_lifecycle_operation_id=NULL,version=version+1 WHERE id=$1`, o.ProjectID.String())
				}
			}}
			s, err := outbox.New(f.store, event.NewCatalog(), outbox.Authorizations{Projects: authority, Processes: f.process})
			if err != nil {
				t.Fatal(err)
			}
			before := f.outboxSnapshot(t)
			report, err := s.InspectStop(ctxFor(t), actor, cause)
			want := foundation.InvalidState
			if change == "pointer" {
				want = foundation.Forbidden
			}
			requireCode(t, err, want)
			if report.Stopped || planned != 1 || f.outboxSnapshot(t) != before {
				t.Fatal("stale plan continued or mutated receipt/delivery/claim")
			}
		})
	}
	t.Run("current stopping", func(t *testing.T) {
		o, actor, cause := f.accept(t, c.Archive)
		before := f.outboxSnapshot(t)
		_, err := f.events.RequestStop(ctxFor(t), actor, cause)
		requireCode(t, err, foundation.InvalidState)
		if f.outboxSnapshot(t) != before {
			t.Fatal("accepted created gate")
		}
		f.phase(t, o, c.OperationStopping)
		report, err := f.events.RequestStop(ctxFor(t), actor, cause)
		if err != nil || !report.Stopped {
			t.Fatal("current stop", report, err)
		}
		before = f.outboxSnapshot(t)
		report, err = f.events.InspectStop(ctxFor(t), actor, cause)
		if err != nil || !report.Stopped || f.outboxSnapshot(t) != before {
			t.Fatal("terminal replay", report, err)
		}
		f.sql(t, `UPDATE agenteam_outbox.project_lifecycle SET phase='stopping' WHERE project_id=$1`, o.ProjectID.String())
		report, err = f.events.InspectStop(ctxFor(t), actor, cause)
		if err != nil || !report.Stopped {
			t.Fatal("current continuation", report, err)
		}
	})
	for _, phase := range []string{"failed", "cleaning", "completed", "deleted"} {
		t.Run(phase, func(t *testing.T) {
			var o c.LifecycleOperation
			var actor identity.Actor
			var cause oc.LifecycleCause
			if phase == "deleted" {
				o, actor, cause = f.deletion(t)
			} else {
				action := c.Archive
				if phase == "cleaning" {
					action = c.Delete
				}
				o, actor, cause = f.accept(t, action)
				f.phase(t, o, c.OperationState(phase))
			}
			localStates := []string{"missing", "wrong action", "wrong version", "wrong operation", "stopping", "stopped"}
			// 00008 only permits archive's terminal phase to be stopped.
			// Completed is a delete cleanup receipt, not an archive projection.
			if o.Action == c.Delete {
				localStates = append(localStates, "completed")
			}
			for _, local := range localStates {
				t.Run(local, func(t *testing.T) {
					f.sql(t, `DELETE FROM agenteam_outbox.project_lifecycle WHERE project_id=$1`, o.ProjectID.String())
					if local != "missing" {
						action := o.Action
						version := o.ProjectVersion
						op := o.ID
						state := "stopped"
						switch local {
						case "wrong action":
							if action == c.Archive {
								action = c.Delete
							} else {
								action = c.Archive
							}
						case "wrong version":
							version++
						case "wrong operation":
							op = id[c.Operation](t)
						case "stopping", "completed":
							state = local
						}
						f.sql(t, `INSERT INTO agenteam_outbox.project_lifecycle(project_id,operation_id,action,project_version,phase,scan_sequence,recovery_pass,completed_at) VALUES($1,$2,$3,$4,$5,17,3,CASE WHEN $5='completed' THEN clock_timestamp() ELSE NULL END)`, o.ProjectID.String(), op.String(), string(action), int64(version), state)
					}
					before := f.outboxSnapshot(t)
					report, err := f.events.InspectStop(ctxFor(t), actor, cause)
					if local == "stopped" || local == "completed" {
						if err != nil || !report.Stopped {
							t.Fatal("exact terminal", report, err)
						}
					} else {
						requireCode(t, err, foundation.InvalidState)
						if report.Stopped {
							t.Fatal("nonterminal/mismatched receipt reported stopped")
						}
					}
					if f.outboxSnapshot(t) != before {
						t.Fatal("readonly inspect changed lifecycle/delivery/attempt/claim/processed")
					}
					_, err = f.events.RequestStop(ctxFor(t), actor, cause)
					requireCode(t, err, foundation.InvalidState)
					if f.outboxSnapshot(t) != before {
						t.Fatal("rejected stop changed state")
					}
				})
			}
			request, _ := oc.NewProjectRequest(oc.ProjectRequestDetails{Kind: oc.LifecycleProject, ProjectID: o.ProjectID, Actor: actor, Lifecycle: cause, LifecycleStep: oc.LifecycleCleanup})
			plan, err := f.authority.Discover(ctxFor(t), request)
			if err != nil {
				t.Fatal(err)
			}
			err = f.locked(t, o.ProjectID, func(ctx context.Context, tx foundation.Tx) error {
				return f.authority.ValidateInTx(ctx, tx, request, plan)
			})
			requireCode(t, err, foundation.DependencyUnbound)
		})
	}
}
