package contract

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

// RealtimeReadAuthority checks the current Human Owner and canonical Meeting
// association on every call. A nil error is a current read decision, not a
// transferable permit, an Execution snapshot or permission to dispatch work.
// Session authentication, subscription lifetimes and delivery belong to the
// shared Gateway; this port does not define another topic or stream protocol.
type RealtimeReadAuthority interface {
	RequireTimelineSubscription(context.Context, i.Actor, i.ProjectID, MeetingID) error
	RequireRuntimeSubscription(context.Context, i.Actor, i.ProjectID, MeetingID, ContributionID, f.Version, f.Version, i.ExecutionID) error
}
