package contract

import (
	"unicode/utf8"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
)

// ContributionRuntimeView is the current Contribution's public durable
// projection. Availability pending explicitly does not claim active streaming.
type ContributionRuntimeView struct {
	Execution    ContributionExecution  `json:"execution"`
	Availability ec.RuntimeAvailability `json:"availability"`
	Snapshot     *ec.RuntimeSnapshot    `json:"snapshot"`
}

func (v ContributionRuntimeView) Validate() error {
	e := v.Execution
	if e.Validate() != nil {
		return invalid()
	}
	if v.Availability == ec.RuntimePending && !e.Status.Terminal() && v.Snapshot == nil || v.Availability == ec.RuntimeNotRecorded && e.Status.Terminal() && e.Status != ec.Succeeded && v.Snapshot == nil {
		return nil
	}
	s := v.Snapshot
	if v.Availability != ec.RuntimeAvailable || !e.Status.Terminal() || s == nil || e.StartedAt == nil || e.CompletedAt == nil || s.Generation.Validate() != nil || s.ExecutionVersion != e.Version || s.IncludedUpdateProgress != 1 || (s.Schema != ec.DurableFinalTextV1 && s.Schema != ec.DurableFinalTextV2) || s.Items == nil {
		return invalid()
	}
	if e.Status != ec.Succeeded {
		if len(s.Items) == 0 {
			return nil
		}
		return invalid()
	}
	if len(s.Items) != 1 {
		return invalid()
	}
	item := s.Items[0]
	if item.ID.Validate() != nil || item.ExecutionID != e.ID || item.Seq != 1 || item.Type != "text" || item.Status != "completed" || item.Revision != 1 || item.StartedAt.Validate() != nil || item.StartedAt.Time().Before(e.StartedAt.Time()) || s.Schema == ec.DurableFinalTextV1 && item.StartedAt != *e.StartedAt || item.CompletedAt != *e.CompletedAt || item.UpdatedAt != *e.CompletedAt || item.CompletedAt.Time().Before(item.StartedAt.Time()) || item.SourceReference.Kind != "execution_round" || item.SourceReference.ID != item.ID || len(item.Data.Content) > ec.MaxRuntimeTextBytes || !utf8.ValidString(item.Data.Content) {
		return invalid()
	}
	return nil
}
