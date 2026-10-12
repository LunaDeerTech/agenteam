//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// These are real local owners, not the aggregate Skills participant. The only
// controlled lifecycle prerequisite is accepted; the original StopDriver owns
// stopping and its claim. No cleanup or global participant completion is tested.
func TestAgentProjectLocalStop(t *testing.T) {
	t.Run("archive-original-mutation-and-project-isolation", func(t *testing.T) {
		v := newAgentProjectLifecycleFixture(t)
		seed, seedMeta, seedIdentity := v.request(t)
		p, actor := v.p2.project, v.p2.base.ownerBrowser.actor
		created, err := v.agents.CreateAgent(ctxFor(t), actor, seedMeta, p.ID, seed)
		if err != nil || created.Validate() != nil {
			t.Fatal("actual initial Agent create", err)
		}
		original := jsonBytes(t, created.Fields().Agent)
		foreign := v.foreignProject(t)
		input, command, _ := v.request(t)
		fields := input.Fields()
		fields.Name = "agent-stopped-before-prepare"
		input, err = ac.NewAgentCreate(fields)
		if err != nil {
			t.Fatal(err)
		}
		v.installCallBarrier(t)
		var mutationPublished atomic.Bool
		mutation := beginAgentProjectCall(t, func(ctx context.Context) error {
			receipt, err := v.agents.CreateAgent(ctx, actor, command, p.ID, input)
			mutationPublished.Store(receipt.Validate() == nil)
			return err
		})
		read := beginAgentProjectCall(t, func(ctx context.Context) error {
			config, err := v.agents.GetAgent(ctx, actor, p.ID, seed.Fields().AgentID)
			if err != nil {
				return err
			}
			raw, err := json.Marshal(config)
			if err == nil && !bytes.Equal(raw, original) {
				return errors.New("archive changed original Agent configuration")
			}
			return err
		})
		other := v.beginForeignRead(t, foreign.ID)
		cause := v.accept(t, pc.Archive)
		stopActor, scope := v.stopThroughDriver(t, cause, []*agentProjectCall{mutation, read, other}, []bool{true, false, false})
		v.requirePending(t, stopActor, cause, scope, 1)
		mutation.requireHeld(t, true)
		read.requireHeld(t, false)
		other.requireHeld(t, false)
		mutation.join(t, false)
		if mutationPublished.Load() {
			t.Fatal("canceled original mutation published a receipt")
		}
		v.requirePending(t, stopActor, cause, scope, 0)
		// Archive local join excludes healthy reads. They still finish their
		// own actual SQL calls and return their original values below.
		read.requireHeld(t, false)
		other.requireHeld(t, false)
		read.join(t, true)
		other.join(t, true)
		facts, err := agentCreateFacts(ctxFor(t), v.p2.base.raw, p.ID, input.Fields().AgentID, command.IdempotencyKey)
		if err != nil || facts != [16]int64{} {
			t.Fatal("stopped original mutation left command/configuration/reference/event facts", err)
		}
		v.checkFacts(t, seed.Fields().AgentID, seedMeta.IdempotencyKey, seedIdentity, [16]int64{1, 1, 0, 0, 2, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}, true)
		v.requirePhase(t, cause, "stopping", "terminal")
	})
	t.Run("delete-directory-and-original-return", func(t *testing.T) {
		v := newAgentProjectLifecycleFixture(t)
		seed, seedMeta, seedIdentity := v.request(t)
		p, actor := v.p2.project, v.p2.base.ownerBrowser.actor
		created, err := v.agents.CreateAgent(ctxFor(t), actor, seedMeta, p.ID, seed)
		if err != nil || created.Validate() != nil {
			t.Fatal("actual initial Agent create", err)
		}
		foreign := v.foreignProject(t)
		v.installCallBarrier(t)
		serviceRead := beginAgentProjectCall(t, func(ctx context.Context) error {
			_, err := v.agents.GetAgent(ctx, actor, p.ID, seed.Fields().AgentID)
			return err
		})
		directoryRead := beginAgentProjectCall(t, func(ctx context.Context) error {
			_, err := v.directory.GetAgent(ctx, actor, p.ID, seed.Fields().AgentID)
			return err
		})
		other := v.beginForeignRead(t, foreign.ID)
		cause := v.accept(t, pc.Delete)
		stopActor, scope := v.stopThroughDriver(t, cause, []*agentProjectCall{serviceRead, directoryRead, other}, []bool{true, true, false})
		v.requirePending(t, stopActor, cause, scope, 2)
		serviceRead.requireHeld(t, true)
		directoryRead.requireHeld(t, true)
		serviceRead.join(t, false)
		v.requirePending(t, stopActor, cause, scope, 1)
		directoryRead.requireHeld(t, true)
		directoryRead.join(t, false)
		v.requirePending(t, stopActor, cause, scope, 0)
		other.requireHeld(t, false)
		other.join(t, true)
		// Local cancellation retires neither canonical Agent nor its references.
		v.checkFacts(t, seed.Fields().AgentID, seedMeta.IdempotencyKey, seedIdentity, [16]int64{1, 1, 0, 0, 2, 1, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0}, true)
		v.requirePhase(t, cause, "stopping", "terminal")
	})
}

func (v *agentProjectLifecycleFixture) beginForeignRead(t *testing.T, project i.ProjectID) *agentProjectCall {
	t.Helper()
	return beginAgentProjectCall(t, func(ctx context.Context) error {
		page, err := v.directory.ListAgents(ctx, v.p2.base.ownerBrowser.actor, project, f.PageRequest{Limit: 10})
		if err == nil && (page.Items == nil || len(page.Items) != 0 || page.NextCursor != "") {
			return errors.New("foreign Project directory result changed")
		}
		return err
	})
}

// Keep all original waits. The phase barrier is after the actual database
// COMMIT but before the original Store method returns; the provider barrier is
// the same boundary for its own authorization transaction. The provider's
// unchanged two-second step only waits for immediate in-memory checks here.
func (v *agentProjectLifecycleFixture) stopThroughDriver(t *testing.T, cause pc.LifecycleCause, calls []*agentProjectCall, canceled []bool) (i.Actor, pc.ScopeRef) {
	t.Helper()
	if len(calls) != len(canceled) {
		t.Fatal("invalid selected original calls")
	}
	v.requirePhase(t, cause, "accepted", "")
	phaseGate, providerGate := newPhaseRelease(), newPhaseRelease()
	phaseEntered, providerEntered := make(chan struct{}), make(chan struct{})
	var phaseCount, providerCount, steps atomic.Int32
	var phaseResult, providerResult f.CommitResult
	s := v.p2.base.tracked
	s.mu.Lock()
	s.afterResult = func(c f.TransactionCause, result f.CommitResult) {
		d := c.Details()
		if d.Kind == f.JobCause && d.JobType == "project-lifecycle" && d.JobID == cause.OperationID.String() && phaseCount.Add(1) == 1 {
			phaseResult = result
			close(phaseEntered)
			<-phaseGate.done
		}
		if d.Kind == f.RecoveryCause && d.Owner == "agent-project-stop" && providerCount.Add(1) == 1 {
			providerResult = result
			close(providerEntered)
			<-providerGate.done
		}
	}
	s.mu.Unlock()
	t.Cleanup(func() { s.mu.Lock(); s.afterResult = nil; s.mu.Unlock() })
	type observation struct {
		actor  i.Actor
		scope  pc.ScopeRef
		report agent.ProjectCallStopReport
	}
	observed := make(chan observation, 1)
	driver, err := project.NewLifecycleStopDriver(s, v.lifecycle, fixtureProcess{v.process}, func(ctx context.Context, actor i.Actor, actual pc.LifecycleCause, scope pc.ScopeRef) error {
		steps.Add(1)
		if actual != cause || scope.ProjectID != v.p2.project.ID {
			return errors.New("StopDriver substituted original lifecycle identity")
		}
		report, err := v.stopper.RequestStop(ctx, actor, actual, scope)
		if err != nil {
			return err
		}
		observed <- observation{actor, scope, report}
		// nil reports this local step's actual return, never participant join.
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		driver.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := driver.Drain(ctx); err != nil {
			t.Error("original Agent lifecycle driver did not drain", err)
		}
	})
	run := phaseRun(t, driver, v.p2.project.ID, cause.OperationID, context.Background(), phaseGate, providerGate)
	phaseAwait(t, phaseEntered)
	if phaseResult.State() != f.Committed || steps.Load() != 0 {
		t.Fatal("provider ran before the original phase COMMIT returned")
	}
	for _, call := range calls {
		call.requireHeld(t, false)
	}
	v.requirePhase(t, cause, "stopping", "running")
	phaseGate.release()
	phaseAwait(t, providerEntered)
	if providerResult.State() != f.Committed || steps.Load() != 1 {
		t.Fatal("actual provider authorization COMMIT missing")
	}
	for _, call := range calls {
		call.requireHeld(t, false)
	}
	select {
	case <-observed:
		t.Fatal("provider published before its original Store return")
	default:
	}
	providerGate.release()
	phaseAwait(t, run.done)
	if run.err != nil || steps.Load() != 1 || phaseCount.Load() != 2 || providerCount.Load() != 1 {
		t.Fatal("actual StopDriver/provider/checkpoint round", run.err)
	}
	var result observation
	select {
	case result = <-observed:
	default:
		t.Fatal("original provider returned no report")
	}
	pending := 0
	for n, call := range calls {
		call.requireHeld(t, canceled[n])
		if canceled[n] {
			pending++
		}
	}
	if !result.report.Matches(cause, result.scope) || result.report.Details().PendingCalls != pending || result.report.Details().LocalJoined {
		t.Fatal("actual provider fabricated local join while original calls remain")
	}
	v.requirePhase(t, cause, "stopping", "terminal")
	return result.actor, result.scope
}
