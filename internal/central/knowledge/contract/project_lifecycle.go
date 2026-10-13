package contract

import pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"

// ProjectLifecycleRegistration declares Knowledge's documents and command,
// publication, source and cleanup history. It binds no Stop or Cleanup service.
// Changes to these metadata require a new contract version. Each call returns
// independent reference kinds; Knowledge has no cleanup predecessor.
func ProjectLifecycleRegistration() pc.ParticipantRegistration {
	return pc.ParticipantRegistration{
		Name: pc.KnowledgeParticipant, ContractVersion: 1, OwnerModule: "knowledge",
		ReferenceKinds: []pc.ReferenceKind{"document", "knowledge-command"},
	}
}
