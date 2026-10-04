package contract

import (
	"context"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type RequeueReason string

const (
	OperatorRetry      RequeueReason = "operator_retry"
	SchemaAvailable    RequeueReason = "schema_available"
	DependencyRestored RequeueReason = "dependency_restored"
)

func (r RequeueReason) Valid() bool {
	return r == OperatorRetry || r == SchemaAvailable || r == DependencyRestored
}

type RequeueReceipt struct {
	DeliveryID DeliveryID
	Cycle      foundation.Progress
	Version    foundation.Version
}
type DiagnosticsWindow string

const (
	FiveMinutes DiagnosticsWindow = "5m"
	OneHour     DiagnosticsWindow = "1h"
	OneDay      DiagnosticsWindow = "24h"
)

type DiagnosticsFilter struct {
	Window    DiagnosticsWindow
	HandlerID event.StableName
	EventType event.StableName
	Phase     Phase
}
type DiagnosticsPage struct {
	Items      []SafeDelivery
	NextCursor string
}
type DeliveryAdministration interface {
	Requeue(context.Context, identity.Actor, foundation.CommandMeta, DeliveryID, foundation.Version, RequeueReason) (RequeueReceipt, error)
	InspectDelivery(context.Context, identity.Actor, identity.Scope, DeliveryID) (SafeDelivery, error)
	QueryDiagnostics(context.Context, identity.Actor, identity.Scope, DiagnosticsFilter, foundation.PageRequest) (DiagnosticsPage, error)
}
