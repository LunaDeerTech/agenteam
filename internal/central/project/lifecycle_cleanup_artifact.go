package project

import (
	"context"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
)

// NewLifecycleCleanupDriverWithArtifact binds the two fixed remaining ports to
// the existing Secret chain. Artifact's original service alone consumes Object.
// Secret and Artifact remain siblings in the manifest; this implementation's
// fixed order does not change their declared dependencies. A completed Artifact
// round advances to Outbox only when every other domain is actually completed.
// Each subsequent Run executes one Outbox round, without binding Audit/final.
func NewLifecycleCleanupDriverWithArtifact(store Store, projects *Authority, processes oc.ProcessAuthority, agents ac.ProjectRetirementConsumer, skills SkillsCleanupDomain, variables pvc.ProjectCleanupDomain, terminalAgents ac.ProjectTerminalCleanupDomain, terminalVariables c.ProjectTerminalCleanupDomain, secret, artifact, outbox c.ProjectLifecycleParticipant) (*LifecycleCleanupDriver, error) {
	for _, port := range []struct {
		value c.ProjectLifecycleParticipant
		name  c.ParticipantName
	}{{artifact, c.ArtifactObjectParticipant}, {outbox, c.OutboxParticipant}} {
		if nilPort(port.value) {
			return nil, fault(f.DependencyUnbound)
		}
		if name, err := cleanupParticipantName(port.value); err != nil || name != port.name {
			return nil, fault(f.DependencyUnbound)
		}
	}
	d, err := NewLifecycleCleanupDriverWithSecret(store, projects, processes, agents, skills, variables, terminalAgents, terminalVariables, secret)
	if err != nil {
		return nil, err
	}
	d.state.artifact, d.state.participant = artifact, outbox
	return d, nil
}

func cleanupDomainCompleted(r *lifecycleRecord, name c.ParticipantName) bool {
	if r == nil {
		return false
	}
	for _, row := range r.participants {
		if row.name == name {
			return row.cleanup == "completed"
		}
	}
	return false
}

func artifactDriverProgress(r *lifecycleRecord) (c.ParticipantName, bool, error) {
	if r == nil {
		return "", false, unavailable(nil)
	}
	for idx, row := range r.participants {
		if row.name != c.ArtifactObjectParticipant || row.cleanup != "completed" {
			continue
		}
		copyRecord := *r
		copyRecord.participants = append([]lifecycleParticipantRecord(nil), r.participants...)
		copyRecord.participants[idx].cleanup = "pending"
		return c.ArtifactObjectParticipant, true, artifactCleanupProgress(&copyRecord)
	}
	return c.ArtifactObjectParticipant, false, artifactCleanupProgress(r)
}

// The preceding Secret attempt was admitted only after both original terminal
// owner confirmations actually returned. Preserve that exact receipt in each
// Artifact scope, rather than borrowing an obsolete terminal claim as a permit.
func (st *lifecycleCleanupState) selectArtifactParent(ctx context.Context, x postgres.SQLExecutor, round *lifecycleCleanupRound, actual *lifecycleStopClaim) error {
	parent := *round
	parent.participant = c.SkillsParticipant
	_, _, cp, err := cleanupParticipantRow(ctx, x, parent)
	if err != nil {
		return err
	}
	receipt, err := decodeTerminalCleanupCheckpoint(cp, round.cause, round.claim.project)
	if err != nil {
		return err
	}
	if actual == nil || actual.fence <= receipt.Fence {
		return fault(f.ResourceBusy)
	}
	parent.participant = c.SecretParticipant
	_, attempts, secret, err := cleanupParticipantRow(ctx, x, parent)
	if err != nil {
		return err
	}
	if attempts < 1 || secret == nil {
		return unavailable(nil)
	}
	round.artifactParentCheckpoint = cp
	return portError(ctx.Err())
}
