//go:build integration

package projectvariable_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
	wc "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

func TestExecutionTaskContext(t *testing.T) {
	t.Run("frozen-input-after-owner-updates", func(t *testing.T) {
		x, input := committedContextInput(t)
		builder, err := execution.NewContextBuilder(work.TaskContextBuilder{})
		if err != nil {
			t.Fatal("construct actual Context builder", err)
		}
		built, err := builder.BuildContext(ctxFor(t), input)
		if err != nil || built.Validate() != nil || built.InputDigest() != input.Digest() ||
			!bytes.Equal(built.Input().CanonicalBytes(), input.CanonicalBytes()) ||
			!built.Trigger().Matches(input.Fields().Request, input.Fields().Trigger) ||
			built.Trigger().Instructions().Revision() != work.TaskWorkPromptRevision ||
			built.PlatformPrompt().Digest() != input.Fields().PlatformPrompt.Digest() ||
			built.AgentInstructions() != input.Fields().Agent.Fields().Core.Instructions {
			t.Fatal("Build lost the committed input or versioned components", err)
		}
		originalTask, err := work.DecodeTaskContext(built.Trigger())
		if err != nil || originalTask.Task().ID != x.v.task.ID || originalTask.Purpose() != "task/work" {
			t.Fatal("Work-owned Context did not preserve the actual captured Task", err)
		}
		requireContextDisplaySafety(t, built)

		// Change current business facts through their formal Owner services. The
		// already committed preparation input remains the sole Build source.
		v := x.v
		actor, projectID := v.base.ownerBrowser.actor, v.base.project.ID
		currentTask, err := v.taskReader.GetTask(ctxFor(t), actor, projectID, v.task.ID)
		if err != nil || currentTask.Version != originalTask.Task().Version {
			t.Fatal("current Task before the formal update", err)
		}
		title := "Current task changed after context capture"
		changedTask, err := v.tasks.UpdateTask(ctxFor(t), actor, meta(t, "context-current-task", &currentTask.Version),
			projectID, currentTask.ID, wc.TaskFieldsUpdate{Title: &title})
		if err != nil || changedTask.Task.Title != title || changedTask.Task.Version != currentTask.Version+1 {
			t.Fatal("formal current Task title update", err)
		}
		currentAgent, err := v.agent.agents.GetAgent(ctxFor(t), actor, projectID, v.agentID)
		if err != nil || currentAgent.Fields().Core.Version != input.Fields().Agent.Fields().Core.Version {
			t.Fatal("current Agent before the formal update", err)
		}
		instructions := "Current Agent instructions changed after context capture"
		change, err := ac.NewAgentUpdate(ac.AgentUpdateFields{Instructions: &instructions})
		if err != nil {
			t.Fatal("formal Agent instructions request", err)
		}
		version := currentAgent.Fields().Core.Version
		changedAgent, err := v.agent.agents.UpdateAgent(ctxFor(t), actor, meta(t, "context-current-agent", &version), projectID, v.agentID, change)
		if err != nil || !changedAgent.Fields().Changed || changedAgent.Fields().Agent.Fields().Core.Instructions != instructions || changedAgent.Fields().Agent.Fields().Core.Version != version+1 {
			t.Fatal("formal current Agent instructions update", err)
		}
		observedTask, err := v.taskReader.GetTask(ctxFor(t), actor, projectID, v.task.ID)
		if err != nil || observedTask.Title != title || observedTask.Version != changedTask.Task.Version {
			t.Fatal("current Task did not persist the new title", err)
		}
		observedAgent, err := v.agent.agents.GetAgent(ctxFor(t), actor, projectID, v.agentID)
		if err != nil || observedAgent.Fields().Core.Instructions != instructions || observedAgent.Fields().Core.Version != version+1 {
			t.Fatal("current Agent did not persist the new instructions", err)
		}

		persisted, err := x.readInput(ctxFor(t), v.base.raw)
		if err != nil || persisted.Digest() != input.Digest() || !bytes.Equal(persisted.CanonicalBytes(), input.CanonicalBytes()) {
			t.Fatal("Owner updates changed the original committed input", err)
		}
		rebuilt, err := builder.BuildContext(ctxFor(t), persisted)
		if err != nil || rebuilt.Digest() != built.Digest() || rebuilt.InputDigest() != built.InputDigest() || !bytes.Equal(rebuilt.CanonicalBytes(), built.CanonicalBytes()) {
			t.Fatal("Context Build reloaded current facts instead of the fixed input", err)
		}
		fixedTask, err := work.DecodeTaskContext(rebuilt.Trigger())
		if err != nil || fixedTask.Task().Title != originalTask.Task().Title || fixedTask.Task().Version != originalTask.Task().Version ||
			fixedTask.Task().Title == title || rebuilt.AgentInstructions() == instructions || rebuilt.AgentInstructions() != built.AgentInstructions() {
			t.Fatal("Context's typed Task or Agent changed with current business facts", err)
		}
		restored, err := ec.DecodeExecutionContext(rebuilt.CanonicalBytes())
		if err != nil || restored.Digest() != built.Digest() || !bytes.Equal(restored.CanonicalBytes(), built.CanonicalBytes()) {
			t.Fatal("actual Context did not round-trip its fixed internal bytes", err)
		}
		requireContextDisplaySafety(t, rebuilt)
		x.requireProviderCalls(t, true)
		x.requireAttemptReturned(t) // Execution stays preparing; no Snapshot/Running is inferred.
	})
}

func requireContextDisplaySafety(t *testing.T, built ec.ExecutionContext) {
	t.Helper()
	fields := built.Input().Fields()
	env := fields.Environment.Fields()
	if fields.Model.CredentialLease == nil || len(env.Secrets) != 1 {
		t.Fatal("Context safety check needs the real nonempty credential and environment captures")
	}
	encoded, err := json.Marshal(built)
	if err != nil || string(encoded) != `"execution_context"` || fmt.Sprintf("%+v", built) != "execution_context" {
		t.Fatal("Context default display exposed internal data", err)
	}
	// These are versioned source components, not assembled Loop messages.
	// Internal Input/CanonicalBytes intentionally retain typed reference IDs.
	text := built.Trigger().Instructions().Content() + "\n" + built.PlatformPrompt().Content() + "\n" + built.AgentInstructions()
	for _, forbidden := range []string{
		"owned-model-capture-material", "owned-environment-capture-material",
		fields.Model.CredentialLease.CredentialRef.Details().ID.String(), fields.Model.CredentialLease.LeaseID.String(),
		env.Secrets[0].CredentialRef.Details().ID.String(), env.Secrets[0].LeaseID.String(),
	} {
		if forbidden == "" || strings.Contains(text, forbidden) || bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatal("Secret material or internal reference entered a displayed Context component")
		}
	}
	for _, plaintext := range []string{"owned-model-capture-material", "owned-environment-capture-material"} {
		if bytes.Contains(built.CanonicalBytes(), []byte(plaintext)) {
			t.Fatal("Secret material entered canonical Context")
		}
	}
}

// Reuse the accepted real source graph. This time capture commits normally;
// the observer returns nil and neither loses a receipt nor omits a provider.
func committedContextInput(t *testing.T) (*modelEnvironmentFixture, ec.PreparationInput) {
	t.Helper()
	x := newModelEnvironmentFixture(t)
	driver := x.newDriver(t, true)
	store := x.v.base.tracked
	var tentative ec.PreparationInput
	store.setAfter(func(ctx context.Context, tx f.Tx, cause f.TransactionCause) error {
		d := cause.Details()
		if tentative.Validate() == nil || d.Kind != f.JobCause || d.JobType != "execution-preparation" || d.JobID != x.created.ID.String() || x.mounts.capture != 1 {
			return nil
		}
		sql, err := store.InTx(tx)
		if err != nil {
			return err
		}
		x.attempt = d.JobAttemptID
		tentative, err = x.readInput(ctx, sql)
		return err
	})
	defer store.setAfter(nil)
	if err := driver.Run(ctxFor(t), x.created.ID); err != nil {
		t.Fatal("normal complete preparation input commit", err)
	}
	store.setAfter(nil)
	x.requireProviderCalls(t, true)
	x.requireAttemptReturned(t)
	input, err := x.readInput(ctxFor(t), x.v.base.raw)
	if err != nil || tentative.Validate() != nil || !bytes.Equal(input.CanonicalBytes(), tentative.CanonicalBytes()) {
		t.Fatal("committed preparation input differs from its real final transaction", err)
	}
	return x, input
}
