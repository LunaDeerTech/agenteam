package executionhttp

import (
	"encoding/json"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

func unavailableOutput() error { return f.NewFault(f.DependencyUnavailable, f.NotStarted) }
func validExecution(v ec.TaskExecution, project i.ProjectID, task string) error {
	if v.ID.Validate() != nil || v.AgentID.Validate() != nil || v.ProjectID != project || v.TaskID != task || v.Purpose != "task/work" && v.Purpose != "task/review" || !v.Status.Valid() || v.Version.Validate() != nil || v.CreatedAt.Validate() != nil {
		return unavailableOutput()
	}
	for _, at := range []*f.Instant{v.StartedAt, v.CompletedAt, v.CancelRequestedAt} {
		if at != nil && (at.Validate() != nil || at.Time().Before(v.CreatedAt.Time())) {
			return unavailableOutput()
		}
	}
	if v.SnapshotID != nil && v.SnapshotID.Validate() != nil {
		return unavailableOutput()
	}
	if v.Status.Terminal() != (v.CompletedAt != nil) || (v.Status == ec.Running || v.Status == ec.Waiting || v.Status == ec.Succeeded) && (v.SnapshotID == nil || v.StartedAt == nil) || (v.Status == ec.Created || v.Status == ec.Preparing) && (v.SnapshotID != nil || v.StartedAt != nil) {
		return unavailableOutput()
	}
	if v.Reason != nil {
		reason := *v.Reason
		if !(v.Status == ec.Succeeded && reason == "completed" || v.Status == ec.Failed && (reason == "model_failed" || reason == "incomplete_response" || reason == "runtime_failed") || v.Status == ec.Cancelled && reason == "cancelled") {
			return unavailableOutput()
		}
	}
	return nil
}
func encodePage(project i.ProjectID, task string, page f.PageRequest, out ec.TaskExecutionPage) ([]byte, error) {
	if out.Items == nil || len(out.Items) > page.Limit {
		return nil, unavailableOutput()
	}
	seen := map[i.ExecutionID]bool{}
	for n, item := range out.Items {
		if validExecution(item, project, task) != nil || seen[item.ID] {
			return nil, unavailableOutput()
		}
		seen[item.ID] = true
		if n > 0 {
			last := out.Items[n-1]
			if item.CreatedAt.Time().After(last.CreatedAt.Time()) || item.CreatedAt == last.CreatedAt && item.ID.String() >= last.ID.String() {
				return nil, unavailableOutput()
			}
		}
	}
	if out.NextCursor != nil && (*out.NextCursor == "" || len(*out.NextCursor) > cursor.MaxTokenBytes || !utf8.ValidString(*out.NextCursor) || len(out.Items) != page.Limit || *out.NextCursor == page.Cursor) {
		return nil, unavailableOutput()
	}
	return json.Marshal(out)
}
