package project

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func skillsCleanupRecord(t *testing.T) *lifecycleRecord {
	t.Helper()
	entries := registryTestEntries()
	for i := range entries {
		if entries[i].Name == c.OutboxParticipant {
			entries[i].CleanupAfter = append(entries[i].CleanupAfter, c.SkillsParticipant)
		}
	}
	entries = append(entries, c.ParticipantRegistration{Name: c.SkillsParticipant, ContractVersion: 1, OwnerModule: "skills", CleanupAfter: []c.ParticipantName{c.SecretParticipant, c.ArtifactObjectParticipant}})
	r := &lifecycleRecord{operation: c.LifecycleOperation{Action: c.Delete, State: c.OperationCleaning}, cleanupStage: "domains", manifest: registryTestManifest(t, entries)}
	for _, e := range r.manifest.Entries() {
		state := "required"
		if e.Name == c.SecretParticipant || e.Name == c.ArtifactObjectParticipant {
			state = "completed"
		}
		r.participants = append(r.participants, lifecycleParticipantRecord{name: e.Name, version: 1, stop: "stopped", cleanup: state})
	}
	return r
}

func skillsCleanupSet(r *lifecycleRecord, name c.ParticipantName, stop, cleanup string) {
	for i := range r.participants {
		if r.participants[i].name == name {
			r.participants[i].stop, r.participants[i].cleanup = stop, cleanup
		}
	}
}

// This pure matrix proves only the additional phase/dependency rule. The
// persisted cause, row decoder and real transaction gates are exercised by
// TestProjectSkillsCleanupCurrentFacts and TestProjectSkillsCleanupTransactions.
func TestSkillsCleanupProgressBoundaries(t *testing.T) {
	for _, state := range []string{"required", "pending"} {
		t.Run(state, func(t *testing.T) {
			r := skillsCleanupRecord(t)
			skillsCleanupSet(r, c.SkillsParticipant, "stopped", state)
			if err := skillsCleanupProgress(r); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, state := range []c.OperationState{c.OperationAccepted, c.OperationStopping, c.OperationFailed, c.OperationCompleted} {
		t.Run(string(state), func(t *testing.T) {
			r := skillsCleanupRecord(t)
			r.operation.State = state
			if state == c.OperationFailed {
				r.resume = c.CleanupPhase
			}
			hasCode(t, skillsCleanupProgress(r), foundation.InvalidState)
		})
	}
	for _, stage := range []string{"outbox", "audit", "final", ""} {
		t.Run("stage_"+stage, func(t *testing.T) {
			r := skillsCleanupRecord(t)
			r.cleanupStage = stage
			hasCode(t, skillsCleanupProgress(r), foundation.InvalidState)
		})
	}
	for _, state := range []string{"completed", "failed"} {
		t.Run("skills_"+state, func(t *testing.T) {
			r := skillsCleanupRecord(t)
			skillsCleanupSet(r, c.SkillsParticipant, "stopped", state)
			hasCode(t, skillsCleanupProgress(r), foundation.InvalidState)
		})
	}
	for _, dependency := range []c.ParticipantName{c.SecretParticipant, c.ArtifactObjectParticipant} {
		for _, state := range []string{"required", "pending", "failed"} {
			t.Run(string(dependency)+"_"+state, func(t *testing.T) {
				r := skillsCleanupRecord(t)
				skillsCleanupSet(r, dependency, "stopped", state)
				hasCode(t, skillsCleanupProgress(r), foundation.InvalidState)
			})
		}
	}
	t.Run("not_stopped", func(t *testing.T) {
		r := skillsCleanupRecord(t)
		skillsCleanupSet(r, c.ArtifactObjectParticipant, "pending", "completed")
		hasCode(t, skillsCleanupProgress(r), foundation.DependencyUnavailable)
	})
	hasCode(t, skillsCleanupProgress(nil), foundation.DependencyUnavailable)
}

func TestSkillsCleanupDispatchStaysClosed(t *testing.T) {
	a := lifecycleTestAuthority(t) // Any Store access panics: all below fail before it.
	actor, _, cause := lifecycleTestActor(t)
	ctx, tx := context.Background(), foundation.NewTx()
	for _, participant := range []c.ParticipantName{c.ArtifactObjectParticipant, c.SecretParticipant, c.OutboxParticipant, c.AuditParticipant, "unregistered"} {
		hasCode(t, a.ValidateLifecycleInTx(ctx, tx, actor, cause, participant, c.CleanupPhase), foundation.DependencyUnbound)
	}
	hasCode(t, a.ValidateLifecycleInTx(ctx, tx, actor, cause, c.SkillsParticipant, c.CleanupPhase), foundation.Forbidden) // Archive.
	cause.Action = c.Delete
	hasCode(t, a.ValidateLifecycleInTx(ctx, tx, testActor(t), cause, c.SkillsParticipant, c.CleanupPhase), foundation.Forbidden)
	hasCode(t, a.ValidateLifecycleInTx(ctx, tx, actor, cause, c.SkillsParticipant, c.OperationPhase("unknown")), foundation.InvalidArgument)
}
