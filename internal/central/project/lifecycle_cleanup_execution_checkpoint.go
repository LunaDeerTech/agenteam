package project

import (
	"bytes"
	"encoding/json"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// One original transaction, never an accumulated history/body in a checkpoint.
// Applied means the whole E and all fixed references retired atomically. Empty
// also proves the concrete owner's legal closed-call tail has been removed.
type executionRetirementCheckpoint struct {
	Stage   string                                `json:"stage"`
	Binding *ec.ProjectExecutionRetirementBinding `json:"binding"`
	Commit  ec.ProjectExecutionRetirementCommit   `json:"commit"`
	Owner   ec.ProjectExecutionRetirementIdentity `json:"owner"`
}

func (v executionRetirementCheckpoint) validate(cause c.LifecycleCause, p c.ProjectID) error {
	if cause.Validate() != nil || cause.Action != c.Delete || p.Validate() != nil || v.Commit.Validate() != nil || v.Owner.Validate() != nil || v.Commit.ProjectID != p || v.Commit.OperationID != cause.OperationID || v.Commit.ProjectVersion != cause.ProjectVersion {
		return invalid()
	}
	if v.Stage == "empty" {
		if v.Binding != nil {
			return invalid()
		}
		return nil
	}
	if v.Stage != "applied" || v.Binding == nil || v.Binding.Validate() != nil || v.Binding.ProjectID != p {
		return invalid()
	}
	return nil
}

func decodeExecutionRetirementCheckpoint(cp *c.CleanupCheckpoint, cause c.LifecycleCause, p c.ProjectID) (executionRetirementCheckpoint, error) {
	var v executionRetirementCheckpoint
	if cp == nil {
		return v, nil
	}
	if !cp.Matches(c.ExecutionParticipant, cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: p}) || cp.Schema() != 1 || len(cp.Bytes()) > 4096 {
		return v, invalid()
	}
	if err := decodeLifecycleJSON(cp.Bytes(), &v); err != nil {
		return v, err
	}
	if err := v.validate(cause, p); err != nil {
		return v, err
	}
	raw, err := json.Marshal(v)
	if err != nil || !bytes.Equal(raw, cp.Bytes()) {
		return v, invalid()
	}
	return v, nil
}

func executionRetirementReport(round lifecycleCleanupRound, v executionRetirementCheckpoint) (c.CleanupReport, error) {
	if err := v.validate(round.cause, round.claim.project); err != nil {
		return c.CleanupReport{}, err
	}
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 4096 {
		return c.CleanupReport{}, invalid()
	}
	cp, err := c.NewCleanupCheckpoint(c.ExecutionParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, 1, raw)
	if err != nil {
		return c.CleanupReport{}, err
	}
	d := c.CleanupDetails{State: c.CleanupPending, SafeReason: c.ReasonWorkPending, Checkpoint: &cp}
	if v.Stage == "empty" {
		d.State = c.CleanupCompleted
		d.SafeReason = ""
	}
	return c.NewCleanupReport(c.ExecutionParticipant, round.cause, c.ScopeRef{Kind: c.ProjectScope, ProjectID: round.claim.project}, d)
}

func executionRetirementRoundReport(round lifecycleCleanupRound, report c.CleanupReport) error {
	d := report.Details()
	if d.Checkpoint == nil {
		return unavailable(nil)
	}
	v, err := decodeExecutionRetirementCheckpoint(d.Checkpoint, round.cause, round.claim.project)
	if err != nil {
		return err
	}
	if (d.State == c.CleanupCompleted) != (v.Stage == "empty") || d.State == c.CleanupCompleted && d.SafeReason != "" {
		return unavailable(nil)
	}
	return nil
}
