package skill

import (
	"bytes"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type cleanupCheckpointData struct {
	Project   string       `json:"project"`
	Operation string       `json:"operation"`
	Version   f.Version    `json:"version"`
	Cleanup   string       `json:"cleanup"`
	Phase     cleanupPhase `json:"phase"`
}

func parseCleanupCheckpoint(checkpoint *pc.CleanupCheckpoint, cause pc.LifecycleCause, scope pc.ScopeRef) (*cleanupCheckpointData, error) {
	if checkpoint == nil {
		return nil, nil
	}
	if !checkpoint.Matches(pc.SkillsParticipant, cause, scope) || checkpoint.Schema() != 1 {
		return nil, invalid()
	}
	raw := checkpoint.Bytes()
	if len(raw) == 0 || len(raw) > 1024 {
		return nil, invalid()
	}
	var data cleanupCheckpointData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, invalid()
	}
	canonical, err := json.Marshal(data)
	// These are this provider's own canonical bytes. Exact encoding also
	// refuses unknown fields, duplicate/case-folded keys and trailing data.
	if err != nil || !bytes.Equal(raw, canonical) || data.Project != scope.ProjectID.String() || data.Operation != cause.OperationID.String() || data.Version != cause.ProjectVersion || data.Phase != cleanupGated && data.Phase != cleanupPending && data.Phase != cleanupCompleted {
		return nil, invalid()
	}
	if _, err = f.ParseID[oc.CleanupOperation](data.Cleanup); err != nil {
		return nil, invalid()
	}
	return &data, nil
}

func cleanupReport(cause pc.LifecycleCause, scope pc.ScopeRef, row *cleanupRow, completed bool) (pc.CleanupReport, error) {
	details := pc.CleanupDetails{State: pc.CleanupPending, SafeReason: pc.ReasonWorkPending}
	if completed {
		details.State, details.SafeReason = pc.CleanupCompleted, ""
	} else if row != nil {
		raw, err := json.Marshal(cleanupCheckpointData{scope.ProjectID.String(), cause.OperationID.String(), cause.ProjectVersion, row.id.String(), row.phase})
		if err != nil {
			return pc.CleanupReport{}, unavailable(err)
		}
		checkpoint, err := pc.NewCleanupCheckpoint(pc.SkillsParticipant, cause, scope, 1, raw)
		if err != nil {
			return pc.CleanupReport{}, err
		}
		details.Checkpoint = &checkpoint
	}
	return pc.NewCleanupReport(pc.SkillsParticipant, cause, scope, details)
}
