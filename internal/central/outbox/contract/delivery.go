package contract

import (
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
)

type Delivery struct{}
type Attempt struct{}
type Process struct{}
type DeliveryID = foundation.ID[Delivery]
type AttemptID = foundation.ID[Attempt]
type ProcessID = foundation.ID[Process]
type Phase string

const (
	Pending    Phase = "pending"
	Processing Phase = "processing"
	RetryWait  Phase = "retry_wait"
	Succeeded  Phase = "succeeded"
	Failed     Phase = "failed"
	DeadLetter Phase = "dead_letter"
)

func (p Phase) Valid() bool {
	switch p {
	case Pending, Processing, RetryWait, Succeeded, Failed, DeadLetter:
		return true
	}
	return false
}

type SafeReason string

const (
	HandlerRetry         SafeReason = "handler_retry"
	Unavailable          SafeReason = "unavailable"
	Deadline             SafeReason = "deadline"
	HandlerPanic         SafeReason = "handler_panic"
	UnsupportedSchema    SafeReason = "unsupported_schema"
	InvalidEvent         SafeReason = "invalid_event"
	SourceTerminal       SafeReason = "source_terminal"
	AuthorizationChanged SafeReason = "authorization_changed"
	ProjectStopped       SafeReason = "project_stopped"
	UnboundHandler       SafeReason = "unbound_handler"
	CommitUnknown        SafeReason = "commit_unknown"
	ProcessUnconfirmed   SafeReason = "process_unconfirmed"
	Shutdown             SafeReason = "shutdown"
)

func (r SafeReason) Valid() bool {
	switch r {
	case HandlerRetry, Unavailable, Deadline, HandlerPanic, UnsupportedSchema, InvalidEvent, SourceTerminal, AuthorizationChanged, ProjectStopped, UnboundHandler, CommitUnknown, ProcessUnconfirmed, Shutdown:
		return true
	}
	return false
}

type DeliveryIdentity struct {
	EventID    event.EventID
	DeliveryID DeliveryID
	AttemptID  AttemptID
	Fence      foundation.Version
	HandlerID  event.StableName
	Scope      event.Scope
	Effect     HandlerEffect
}

func (d DeliveryIdentity) Valid() bool {
	return d.EventID.Validate() == nil && d.DeliveryID.Validate() == nil && d.AttemptID.Validate() == nil && d.Fence.Validate() == nil && d.HandlerID.Validate() == nil && d.Scope.Validate() == nil && d.Effect.Valid()
}
func (d DeliveryIdentity) CauseRef() (string, error) {
	if !d.Valid() {
		return "", invalid()
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "", invalid()
	}
	return DigestBytes(b).String(), nil
}

type ProcessedReceipt struct {
	EventID      event.EventID
	HandlerID    event.StableName
	DeliveryID   DeliveryID
	AttemptID    AttemptID
	Fence        foundation.Version
	ProcessedAt  foundation.Instant
	ResultDigest foundation.Digest
}
type SafeDelivery struct {
	ID              DeliveryID
	EventID         event.EventID
	EventType       event.StableName
	SchemaVersion   uint32
	HandlerID       event.StableName
	Scope           event.Scope
	Phase           Phase
	Version         foundation.Version
	Cycle, Attempts foundation.Progress
	NextAttemptAt   *foundation.Instant
	Reason          SafeReason
}
