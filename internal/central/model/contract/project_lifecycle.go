package contract

import pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"

// ProjectLifecycleRegistration declares Project configuration, commands,
// resolution, snapshots and logical/runtime calls. System configuration and
// selectors remain outside this Project scope. Usage owns its own history.
// Returned slices are independent; this version declares no available adapter.
func ProjectLifecycleRegistration() pc.ParticipantRegistration {
	return pc.ParticipantRegistration{
		Name: pc.ModelParticipant, ContractVersion: 1, OwnerModule: "model",
		ReferenceKinds: []pc.ReferenceKind{"model", "model-call", "provider", "snapshot"},
		CleanupAfter:   []pc.ParticipantName{pc.SkillsParticipant, pc.ExecutionParticipant, pc.UsageParticipant},
	}
}
