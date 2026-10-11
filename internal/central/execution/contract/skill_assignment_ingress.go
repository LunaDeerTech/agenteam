package contract

import sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"

// SkillAssignmentIngress records a routing decision in the original Skill
// assignment transaction. The Skill-owned port avoids a reverse dependency:
// its change DTO is not authority, and Execution must ask the real Skill owner
// to verify and protect the immutable revision before returning queued.
// No-active is durable too; replay cannot redirect an old change to a new run.
// Neither outcome applies a RoundInput or resumes a waiting Execution.
type SkillAssignmentIngress = sc.AssignmentRuntimeSink

type SkillAssignmentChange = sc.RuntimeAssignmentChange
type SkillAssignmentIngressReceipt = sc.AssignmentIngressReceipt
