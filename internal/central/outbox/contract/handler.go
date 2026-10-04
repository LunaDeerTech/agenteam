package contract

import (
	"context"
	"fmt"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"io"
	"log/slog"
)

type HandlerEffect string

const (
	CanonicalConverge HandlerEffect = "canonical_converge"
	DomainIngress     HandlerEffect = "domain_ingress"
)

func (e HandlerEffect) Valid() bool { return e == CanonicalConverge || e == DomainIngress }

type OrderingPolicy string

const (
	VersionGuarded     OrderingPolicy = "version_guarded"
	CanonicalReconcile OrderingPolicy = "canonical_reconcile"
)

func (p OrderingPolicy) Valid() bool { return p == VersionGuarded || p == CanonicalReconcile }

type Subscription struct {
	EventType event.StableName
	Versions  []uint32
}
type HandlerDefinition struct {
	ID            event.StableName
	Subscriptions []Subscription
	Effect        HandlerEffect
	Ordering      OrderingPolicy
	Handler       Handler
}
type Handler interface {
	Prepare(context.Context, event.Event) (HandlerPlan, error)
	ValidateInTx(context.Context, foundation.Tx, event.Event, HandlerPlan) error
	HandleInTx(context.Context, foundation.Tx, event.Event, HandlerPlan) Result
}
type handlerPlanData struct {
	handler      event.StableName
	event        foundation.Digest
	dependencies Dependencies
}
type HandlerPlan struct{ data func() handlerPlanData }

func NewHandlerPlan(handler event.StableName, e event.Event, d Dependencies) (HandlerPlan, error) {
	digest, err := EventDigest(e)
	if handler.Validate() != nil || err != nil || d.Validate() != nil {
		return HandlerPlan{}, invalid()
	}
	p := handlerPlanData{handler, digest, d}
	return HandlerPlan{func() handlerPlanData { return p }}, nil
}
func (p HandlerPlan) Matches(handler event.StableName, e event.Event) bool {
	d, err := EventDigest(e)
	return err == nil && p.data != nil && p.data().handler == handler && p.data().event == d
}
func (p HandlerPlan) Dependencies() Dependencies {
	if p.data == nil {
		return Dependencies{}
	}
	return p.data().dependencies
}
func (p HandlerPlan) Locks() []foundation.LockRequest { return p.Dependencies().Locks() }
func (p HandlerPlan) Format(w fmt.State, _ rune)      { _, _ = io.WriteString(w, "outbox_handler_plan") }
func (p HandlerPlan) MarshalJSON() ([]byte, error)    { return []byte(`"outbox_handler_plan"`), nil }
func (*HandlerPlan) UnmarshalJSON([]byte) error       { return invalid() }
func (p HandlerPlan) LogValue() slog.Value            { return slog.StringValue("outbox_handler_plan") }

type ResultKind string

const (
	Acknowledged        ResultKind = "ack"
	RetryRequested      ResultKind = "retry"
	DeadLetterRequested ResultKind = "dead_letter"
)

type Result struct {
	kind   ResultKind
	reason SafeReason
	digest foundation.Digest
}

func Ack(digest foundation.Digest) Result {
	if digest.Validate() != nil {
		return Result{}
	}
	return Result{kind: Acknowledged, digest: digest}
}
func Retry(reason SafeReason) Result {
	if !reason.Valid() {
		return Result{}
	}
	return Result{kind: RetryRequested, reason: reason}
}
func Reject(reason SafeReason) Result {
	if !reason.Valid() {
		return Result{}
	}
	return Result{kind: DeadLetterRequested, reason: reason}
}
func (r Result) Kind() ResultKind          { return r.kind }
func (r Result) Reason() SafeReason        { return r.reason }
func (r Result) Digest() foundation.Digest { return r.digest }
func (r Result) Valid() bool {
	return r.kind == Acknowledged && r.digest.Validate() == nil || (r.kind == RetryRequested || r.kind == DeadLetterRequested) && r.reason.Valid()
}

type SubscriptionBoundary struct {
	EventType        event.StableName
	AcceptedVersions []uint32
	AcceptedAfter    foundation.Progress
	RegisteredAt     foundation.Instant
	New              bool
}
type Registration struct {
	HandlerID     event.StableName
	Subscriptions []SubscriptionBoundary
}
