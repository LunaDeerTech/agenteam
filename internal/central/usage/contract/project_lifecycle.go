package contract

import pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"

// ProjectLifecycleRegistration declares Project invocations, observations and
// execution usage summaries, including those stored in the Model SQL schema.
// This versioned metadata does not bind an implementation or retire a call.
func ProjectLifecycleRegistration() pc.ParticipantRegistration {
	return pc.ParticipantRegistration{
		Name: pc.UsageParticipant, ContractVersion: 1, OwnerModule: "usage",
		ReferenceKinds: []pc.ReferenceKind{"invocation"},
		CleanupAfter:   []pc.ParticipantName{pc.ExecutionParticipant},
	}
}
