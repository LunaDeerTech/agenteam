package app

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mtc "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	uc "github.com/LunaDeerTech/agenteam/internal/central/usage/contract"
)

// This is the current binary's complete Project responsibility declaration,
// not a registry of available implementations. Disabling runtime startup does
// not discard existing Scheduler/Execution history or change a version's graph.
// Destructive commands remain unbound until a complete Registry is assembled.
func defaultProjectLifecycleManifest() (pc.RequiredManifest, error) {
	entries := []pc.ParticipantRegistration{
		{Name: pc.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []pc.ReferenceKind{"artifact", "object"}, CleanupAfter: []pc.ParticipantName{pc.SkillsParticipant, pc.KnowledgeParticipant}},
		{Name: pc.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []pc.ReferenceKind{"lease", "secret"}, CleanupAfter: []pc.ParticipantName{pc.SkillsParticipant, pc.ModelParticipant}},
		{Name: pc.SkillsParticipant, ContractVersion: 1, OwnerModule: "skills", ReferenceKinds: []pc.ReferenceKind{"skill-initialization", "skill-installation", "skill-object-cleanup", "skill-package-reader"}, CleanupAfter: []pc.ParticipantName{pc.ExecutionParticipant, pc.RunnerManagedParticipant}},
		{Name: pc.WorkParticipant, ContractVersion: 1, OwnerModule: "work", ReferenceKinds: []pc.ReferenceKind{"work-call"}, CleanupAfter: []pc.ParticipantName{pc.ExecutionParticipant, pc.SchedulerParticipant}},
		{Name: pc.SchedulerParticipant, ContractVersion: 1, OwnerModule: "scheduler", ReferenceKinds: []pc.ReferenceKind{"scheduler-call"}},
		{Name: pc.ExecutionParticipant, ContractVersion: 1, OwnerModule: "execution", ReferenceKinds: []pc.ReferenceKind{"execution-call"}, CleanupAfter: []pc.ParticipantName{pc.SchedulerParticipant}},
		kc.ProjectLifecycleRegistration(),
		uc.ProjectLifecycleRegistration(),
		mc.ProjectLifecycleRegistration(),
		mtc.ProjectLifecycleRegistration(),
	}
	// Every producer has a direct edge to Outbox, including declared owners
	// whose Stop/Cleanup implementations are still unavailable.
	producers := make([]pc.ParticipantName, 0, len(entries))
	for _, entry := range entries {
		producers = append(producers, entry.Name)
	}
	entries = append(entries,
		pc.ParticipantRegistration{Name: pc.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", ReferenceKinds: []pc.ReferenceKind{"delivery", "event"}, CleanupAfter: producers},
		pc.ParticipantRegistration{Name: pc.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", ReferenceKinds: []pc.ReferenceKind{"record"}, CleanupAfter: []pc.ParticipantName{pc.OutboxParticipant}},
	)
	return pc.NewRequiredManifest(entries)
}

func createProjectLifecycleAuthority(db database) (*project.LifecycleAuthority, error) {
	store, ok := db.(project.Store)
	if !ok || runtimeInformationNil(store) {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	manifest, err := defaultProjectLifecycleManifest()
	if err != nil {
		return nil, err
	}
	return project.NewLifecycleAuthority(store, manifest, nil)
}
