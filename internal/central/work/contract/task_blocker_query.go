package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// TaskBlockerPageReader returns an independently authorized page. A cursor is
// bound to the current Task version; changing the requested limit is allowed.
type TaskBlockerPageReader interface {
	ListTaskBlockersPage(context.Context, i.Actor, ProjectID, TaskID, TaskBlockerStatus, f.PageRequest) (f.Page[TaskBlocker], error)
}
