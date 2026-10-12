package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Schema 6 replaces, rather than embeds, the immutable-command recovery
// anchor. It can be written only in the transaction that removes both owners'
// terminal data. The original claim, actual SQL transaction and concrete pairs
// remain identifiable after the command and registered-call rows are gone.
type terminalCleanupCheckpoint struct {
	Project        string                         `json:"project_id"`
	Operation      string                         `json:"operation_id"`
	Version        f.Version                      `json:"project_version"`
	Process        string                         `json:"process_id"`
	Attempt        string                         `json:"attempt_id"`
	Fence          int64                          `json:"fence"`
	TransactionID  string                         `json:"transaction_id"`
	PreviousDigest f.Digest                       `json:"previous_checkpoint_digest"`
	AgentOwner     c.ProjectTerminalOwnerIdentity `json:"agent_owner"`
	VariablesOwner c.ProjectTerminalOwnerIdentity `json:"variables_owner"`
}

func decodeTerminalCleanupCheckpoint(cp *c.CleanupCheckpoint, cause c.LifecycleCause, project c.ProjectID) (terminalCleanupCheckpoint, error) {
	var data terminalCleanupCheckpoint
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: project}
	if cause.Validate() != nil || cause.Action != c.Delete || scope.Validate() != nil || cp == nil || !cp.Matches(c.SkillsParticipant, cause, scope) || cp.Schema() != 6 {
		return data, unavailable(nil)
	}
	raw := cp.Bytes()
	if len(raw) > 4096 {
		return data, unavailable(nil)
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return data, unavailable(err)
	}
	canonical, err := json.Marshal(data)
	if err != nil || !bytes.Equal(canonical, raw) || data.Project != project.String() || data.Operation != cause.OperationID.String() || data.Version != cause.ProjectVersion || data.Fence < 1 || data.PreviousDigest.Validate() != nil {
		return data, unavailable(err)
	}
	if _, err = f.ParseID[struct{}](data.Process); err != nil {
		return data, unavailable(err)
	}
	if _, err = f.ParseID[struct{}](data.Attempt); err != nil {
		return data, unavailable(err)
	}
	xid, err := strconv.ParseUint(data.TransactionID, 10, 64)
	if err != nil || xid == 0 || strconv.FormatUint(xid, 10) != data.TransactionID {
		return data, unavailable(err)
	}
	if data.AgentOwner.Validate() != nil || data.VariablesOwner.Validate() != nil || data.AgentOwner.Kind != c.ProjectTerminalAgentOwner || data.VariablesOwner.Kind != c.ProjectTerminalVariablesOwner || data.AgentOwner.ProcessID != data.Process || data.VariablesOwner.ProcessID != data.Process {
		return data, unavailable(nil)
	}
	return data, nil
}

func terminalCleanupReport(round lifecycleCleanupRound, agents, variables c.ProjectTerminalOwnerIdentity, transactionID string) (c.CleanupReport, error) {
	previous, _, err := decodeVariablesCleanupCheckpoint(round.checkpoint, round.cause, round.claim.project)
	if err != nil || round.checkpoint.Schema() != 5 || previous.State != c.CleanupCompleted || round.claim.phase != "running" {
		return c.CleanupReport{}, unavailable(err)
	}
	sum := sha256.Sum256(round.checkpoint.Bytes())
	data := terminalCleanupCheckpoint{
		Project: round.claim.project.String(), Operation: round.claim.operation.String(), Version: round.cause.ProjectVersion,
		Process: round.claim.process.String(), Attempt: round.claim.attempt, Fence: round.claim.fence,
		TransactionID: transactionID, PreviousDigest: f.Digest("sha256:" + hex.EncodeToString(sum[:])),
		AgentOwner: agents, VariablesOwner: variables,
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	scope := c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}
	cp, err := c.NewCleanupCheckpoint(c.SkillsParticipant, round.cause, scope, 6, raw)
	if err != nil {
		return c.CleanupReport{}, unavailable(err)
	}
	if _, err = decodeTerminalCleanupCheckpoint(&cp, round.cause, round.claim.project); err != nil {
		return c.CleanupReport{}, err
	}
	return c.NewCleanupReport(c.SkillsParticipant, round.cause, scope, c.CleanupDetails{State: c.CleanupCompleted, Checkpoint: &cp})
}

func (data terminalCleanupCheckpoint) matchesClaim(claim lifecycleStopClaim) bool {
	return data.Project == claim.project.String() && data.Operation == claim.operation.String() && data.Process == claim.process.String() && data.Attempt == claim.attempt && data.Fence == claim.fence
}
