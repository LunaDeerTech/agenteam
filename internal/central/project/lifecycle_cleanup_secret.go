package project

import (
	"bytes"
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// NewLifecycleCleanupDriverWithSecret extends the fixed Agent/Skills/Variables
// chain with Secret. Each Run still performs only one cleanup step. Secret
// completion stays in domains; Object, Outbox, Audit and finalization are not
// supplied by this constructor, and it does not bind a default registry.
func NewLifecycleCleanupDriverWithSecret(store Store, projects *Authority, processes oc.ProcessAuthority, agents ac.ProjectRetirementConsumer, skills SkillsCleanupDomain, variables pvc.ProjectCleanupDomain, terminalAgents ac.ProjectTerminalCleanupDomain, terminalVariables c.ProjectTerminalCleanupDomain, secret c.ProjectLifecycleParticipant) (*LifecycleCleanupDriver, error) {
	if nilPort(secret) {
		return nil, fault(f.DependencyUnbound)
	}
	if name, err := cleanupParticipantName(secret); err != nil || name != c.SecretParticipant {
		return nil, fault(f.DependencyUnbound)
	}
	d, err := NewLifecycleCleanupDriverWithTerminal(store, projects, processes, agents, skills, variables, terminalAgents, terminalVariables)
	if err != nil {
		return nil, err
	}
	d.state.secret = secret
	return d, nil
}

func secretDriverProgress(r *lifecycleRecord) (c.ParticipantName, bool, error) {
	if r == nil {
		return "", false, unavailable(nil)
	}
	for idx, row := range r.participants {
		if row.name != c.SecretParticipant || row.cleanup != "completed" {
			continue
		}
		// Completed Secret is only a replay of this fixed participant. Validate
		// the same dependencies without mutating the canonical record.
		copyRecord := *r
		copyRecord.participants = append([]lifecycleParticipantRecord(nil), r.participants...)
		copyRecord.participants[idx].cleanup = "pending"
		return c.SecretParticipant, true, secretCleanupProgress(&copyRecord)
	}
	return c.SecretParticipant, false, secretCleanupProgress(r)
}

// selectSecretParent runs inside the original start transaction. On the first
// Secret attempt it returns a read-only terminal confirmation round. The caller
// waits for BOTH original owner callbacks to return, then starts a second short
// transaction that compares these exact bytes and the still-original claim.
// Only that second transaction can increment Secret's attempt_count. Subsequent
// attempts retain this persisted handoff; the count is never public authority.
func (st *lifecycleCleanupState) selectSecretParent(ctx context.Context, x postgres.SQLExecutor, round *lifecycleCleanupRound, actual *lifecycleStopClaim, attempts int64, version f.Version, confirmed *lifecycleCleanupRound) (bool, error) {
	parent := *round
	parent.participant = c.SkillsParticipant
	rowVersion, _, cp, err := cleanupParticipantRow(ctx, x, parent)
	if err != nil {
		return false, err
	}
	receipt, err := decodeTerminalCleanupCheckpoint(cp, round.cause, round.claim.project)
	if err != nil {
		return false, err
	}
	if actual == nil {
		return false, fault(f.ResourceBusy)
	}
	if attempts == 0 {
		if actual.phase != "terminal" || !receipt.matchesClaim(*actual) {
			return false, fault(f.ResourceBusy)
		}
		if confirmed == nil {
			round.claim = *actual
			round.participant = c.SkillsParticipant
			round.checkpoint = cp
			round.operationVersion = version
			round.rowVersion = rowVersion
			round.terminalStep, round.terminalConfirm, round.secretParentStep = true, true, true
			return true, nil
		}
		if !confirmed.secretParentStep || !confirmed.terminalStep || !confirmed.terminalConfirm || confirmed.terminalUnapplied || confirmed.claim != *actual || confirmed.cause != round.cause || confirmed.checkpoint == nil || confirmed.checkpoint.Schema() != 6 || !bytes.Equal(confirmed.checkpoint.Bytes(), cp.Bytes()) {
			return false, fault(f.ResourceBusy)
		}
	} else if attempts < 0 || confirmed != nil || actual.fence <= receipt.Fence {
		return false, fault(f.ResourceBusy)
	}
	round.secretParentCheckpoint = cp
	return false, nil
}
