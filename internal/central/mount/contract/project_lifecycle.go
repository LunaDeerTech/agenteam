package contract

import pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"

// ProjectLifecycleRegistration declares Project Mounts and Runner workspaces.
// System Runner enrollment is separate. Metadata capture or declaration does
// not prove physical cleanup, invocation ownership, or a bound participant.
func ProjectLifecycleRegistration() pc.ParticipantRegistration {
	return pc.ParticipantRegistration{
		Name: pc.RunnerManagedParticipant, ContractVersion: 1, OwnerModule: "mount",
		ReferenceKinds: []pc.ReferenceKind{"mount", "workspace"},
		CleanupAfter:   []pc.ParticipantName{pc.ExecutionParticipant},
	}
}
