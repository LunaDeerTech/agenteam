package artifact

import (
	"context"
	"errors"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// This controls the live Project gate, not a real Delete claim. The service is
// owned, so a missing private cleanup scope must reach the ordinary call fence.
type cleanupMaintenanceAuthority struct{ *cleanupOwnerFixture }

func (a cleanupMaintenanceAuthority) CheckProjectCleanupInTx(ctx context.Context, tx f.Tx, actor i.Actor, cause oc.ProjectCleanupCause) error {
	fact := a.facts[cause.Details().ProjectID]
	if fact == nil {
		return failure(f.Forbidden, nil)
	}
	registration, _ := i.RegisterService(i.ObjectMaintenance)
	maintenance, err := registration.Actor(cause.Details().OperationID.String(), identityScope(cause.Details().ProjectID))
	if err != nil || !actor.Equal(fact.actor) && !actor.Equal(maintenance) {
		return failure(f.Forbidden, nil)
	}
	return a.cleanupOwnerFixture.CheckProjectCleanupInTx(ctx, tx, fact.actor, cause)
}

func TestArtifactOwnedCleanupMaintenanceScope(t *testing.T) {
	for _, name := range []string{"original-actor", "derived-maintenance", "wrong-project", "wrong-operation", "wrong-service", "missing-scope", "expired-scope", "wrong-state", "non-lifecycle", "current-gate-denied"} {
		t.Run(name, func(t *testing.T) {
			v, project := newCleanupOwnerFixture(t)
			owners, err := NewOwnerProvider(v.store, cleanupMaintenanceAuthority{v})
			if err != nil {
				t.Fatal(err)
			}
			v.state.owners = owners
			v.state.ownership = &projectCallOwnershipState{service: v.service, changed: make(chan struct{})}
			fact := v.facts[project]
			call, err := v.state.borrowProjectCleanup(fact.actor, fact.cause.Details())
			if err != nil {
				t.Fatal(err)
			}
			defer v.state.releaseProjectCleanup(call)
			ctx := context.WithValue(context.Background(), projectCleanupContextKey{}, projectCleanupContext{state: v.state, call: call})
			registration, _ := i.RegisterService(i.ObjectMaintenance)
			actor, err := registration.Actor(fact.cause.Details().OperationID.String(), identityScope(project))
			if err != nil {
				t.Fatal(err)
			}
			intent := i.Lifecycle
			switch name {
			case "original-actor":
				actor = fact.actor
			case "wrong-project":
				actor, err = registration.Actor(fact.cause.Details().OperationID.String(), identityScope(v.add(t)))
			case "wrong-operation":
				actor, err = registration.Actor(newTestID[oc.CleanupOperation](t).String(), identityScope(project))
			case "wrong-service":
				other, _ := i.RegisterService(i.ObjectService)
				actor, err = other.Actor(fact.cause.Details().OperationID.String(), identityScope(project))
			case "missing-scope":
				ctx = context.Background()
			case "expired-scope":
				v.state.releaseProjectCleanup(call)
			case "wrong-state":
				ctx = context.WithValue(ctx, projectCleanupContextKey{}, projectCleanupContext{state: &serviceState{}, call: call})
			case "non-lifecycle":
				intent = i.Converge
			case "current-gate-denied":
				v.deny = true
			}
			if err != nil {
				t.Fatal(err)
			}
			allowedScope := name == "original-actor" || name == "derived-maintenance" || name == "current-gate-denied"
			if got := v.state.originalCleanupCall(ctx, subject(actor, project), intent); got != allowedScope {
				t.Fatal("private cleanup actor scope mismatch", got)
			}
			if name == "non-lifecycle" {
				return
			}
			called := 0
			result := v.service.withinCleanup(ctx, actor, fact.cause, nil, nil, func(ctx context.Context, tx f.Tx, _ postgres.SQLExecutor, _ oc.LockedAccess) error {
				called++
				if _, err := v.store.InTx(tx); err != nil {
					return err
				}
				if err := v.store.held(ctx, tx, project); err != nil {
					return err
				}
				if v.gate[tx] != project {
					return errors.New("cleanup callback preceded the current Project gate")
				}
				return nil
			})
			if name == "original-actor" || name == "derived-maintenance" {
				if result.State() != f.Committed || called != 1 || fact.checks != 1 {
					t.Fatal("owned cleanup did not reach the original current gate", result.State(), called, fact.checks)
				}
			} else if result.State() != f.NotCommitted || called != 0 {
				t.Fatal("invalid cleanup scope reached the mutation callback", result.State(), called)
			}
			if fact.reads != 0 || fact.writes != 0 {
				t.Fatal("scope control issued unexpected business SQL")
			}
		})
	}
}
