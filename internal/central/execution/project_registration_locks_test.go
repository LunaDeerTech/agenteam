package execution

import (
	"context"
	"errors"
	"sync"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// This is the original ServiceAccess seam called by the real Authority during
// owned registration. It checks the live transaction's held locks, then denies
// permission. It does not mint a Scheduler grant or prove a successful Launch.
type executionRegistrationLockGate struct {
	store      *executionCallStore
	pair       *ProjectCallOwnership
	actor      i.Actor
	key        ec.LaunchLookupKey
	method     string
	denied     error
	seen       int
	locksError error
	checked    bool
}

func (g *executionRegistrationLockGate) RequireExecutionProjectInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, agent i.AgentID, intent i.AccessIntent) error {
	g.seen++
	w, ok := ctx.Value(projectCallContextKey{}).(projectCallWitness)
	if !ok || w.state != g.pair.state || w.entry == nil || w.entry.identity.Command() != g.method || !actor.Equal(g.actor) || actor.Details().ServiceName != i.Scheduler || intent != i.Read || project != g.key.ProjectID || agent != g.key.AgentID {
		return fault(f.InvalidState)
	}
	// Build the expected business command from the original Launch key, not
	// from the technical entry identity (which is lookup_launch for Lookup).
	command, err := g.key.Command()
	if err != nil {
		return err
	}
	projectKey, err := f.ProjectLock(project.String())
	if err != nil {
		return err
	}
	agentKey, err := f.AgentLock(agent.String())
	if err != nil {
		return err
	}
	scheduleKey, err := f.ProjectScheduleLock(project.String())
	if err != nil {
		return err
	}
	commandKey, err := f.CommandLock(command)
	if err != nil {
		return err
	}
	g.locksError = g.store.RequireHeldLocks(ctx, tx, []f.LockRequest{
		{Key: projectKey, Mode: f.Shared}, {Key: agentKey, Mode: f.Shared},
		{Key: scheduleKey, Mode: f.Exclusive}, {Key: commandKey, Mode: f.Exclusive},
	})
	if g.locksError != nil {
		return g.locksError
	}
	g.checked = true
	return g.denied
}

func TestExecutionOwnedSchedulerRegistrationPreholdsLaunchCommand(t *testing.T) {
	for _, method := range []string{"launch", "lookup_launch"} {
		t.Run(method, func(t *testing.T) {
			v := newExecutionCalls(t)
			registration, err := i.RegisterService(i.Scheduler)
			if err != nil {
				t.Fatal(err)
			}
			scope, err := i.InProject(v.p)
			if err != nil {
				t.Fatal(err)
			}
			actor, err := registration.Actor(newTestID[struct{}](t).String(), scope)
			if err != nil {
				t.Fatal(err)
			}
			key := dispatchLookupKey(v.launch.request)
			digest, err := v.launch.request.Digest()
			if err != nil {
				t.Fatal(err)
			}
			gate := &executionRegistrationLockGate{store: v.store, pair: v.pair, actor: actor, key: key, method: method, denied: fault(f.Forbidden)}
			v.launch.authority.state.services = gate
			entered, release := make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			collected := false
			t.Cleanup(func() {
				unblock()
				if !collected {
					_ = associatedTestReceive(t, done)
				}
				v.store.setAfter(nil)
			})
			v.store.setAfter(func(_ context.Context, cause f.TransactionCause, result f.CommitResult) f.CommitResult {
				if d := cause.Details(); d.Owner != "execution-call-register" || d.CheckpointRef != v.p.String() || result.State() != f.NotCommitted {
					t.Error("expected the first rejected registration transaction")
				}
				close(entered)
				<-release
				return result
			})
			go func() {
				if method == "launch" {
					_, err := v.pair.Service().Launch(context.Background(), actor, v.launch.request)
					done <- err
				} else {
					_, err := v.pair.Service().LookupLaunch(context.Background(), actor, key, digest)
					done <- err
				}
			}()
			associatedTestReceive(t, entered)
			if gate.seen != 1 || !gate.checked || gate.locksError != nil {
				t.Fatal("original service permission ran without complete launch locks", gate.seen, gate.locksError)
			}
			calls, reads, writes := v.store.counts()
			if calls != 1 || reads != 0 || writes != 0 || v.launch.store.live || v.launch.store.writes != 0 || len(v.launch.store.rows) != 0 || v.launch.discoveries != 0 || v.launch.validations != 0 || v.launch.captures != 0 {
				t.Fatal("permission denial reached technical INSERT or business SQL/provider")
			}
			v.pair.Service().Stop()
			r, err := v.stop(context.Background(), false)
			requireExecutionStopState(t, r, err, pc.StopPending)
			if v.pair.Service().Joined() {
				t.Fatal("Stop replaced the original registration Store return")
			}
			select {
			case <-done:
				collected = true
				t.Fatal("public entry escaped the original Store tail")
			default:
			}
			unblock()
			err = associatedTestReceive(t, done)
			collected = true
			requireCode(t, err, f.Forbidden)
			if !errors.Is(err, gate.denied) || !v.pair.Service().Joined() {
				t.Fatal("original permission error or Service return was lost", err)
			}
			v.store.setAfter(nil)
			afterCalls, afterReads, afterWrites := v.store.counts()
			if afterCalls != calls || afterReads != reads || afterWrites != writes {
				t.Fatal("Inspect or rejected registration tail performed another SQL call")
			}
		})
	}
}
