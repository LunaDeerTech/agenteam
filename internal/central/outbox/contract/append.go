package contract

import (
	"context"
	"fmt"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"io"
	"log/slog"
)

type AppendPlanDetails struct {
	Event             event.Event
	Semantic          foundation.Digest
	Producer, Project Dependencies
	Locks             []foundation.LockRequest
}
type appendPlanData struct {
	issuer  PlanIssuer
	details AppendPlanDetails
}
type AppendPlan struct{ data func() appendPlanData }

func NewAppendPlan(issuer PlanIssuer, d AppendPlanDetails) (AppendPlan, error) {
	if !issuer.Valid() || d.Event.Validate() != nil || d.Semantic.Validate() != nil || d.Producer.Validate() != nil || d.Event.Header().Scope.Kind == event.ProjectScope && d.Project.Validate() != nil {
		return AppendPlan{}, invalid()
	}
	locks, err := NormalizeLocks(d.Locks)
	if err != nil {
		return AppendPlan{}, err
	}
	d.Locks = locks
	p := appendPlanData{issuer, d}
	return AppendPlan{func() appendPlanData { return p }}, nil
}
func (p AppendPlan) Matches(issuer PlanIssuer, a identity.Actor, e event.Event) bool {
	d, err := SemanticDigest(a, e)
	return err == nil && p.data != nil && p.data().issuer == issuer && p.data().details.Semantic == d
}
func (p AppendPlan) Details() AppendPlanDetails {
	if p.data == nil {
		return AppendPlanDetails{}
	}
	d := p.data().details
	d.Locks = append([]foundation.LockRequest(nil), d.Locks...)
	return d
}
func (p AppendPlan) Locks() []foundation.LockRequest { return p.Details().Locks }
func (p AppendPlan) Format(w fmt.State, _ rune)      { _, _ = io.WriteString(w, "outbox_append_plan") }
func (p AppendPlan) MarshalJSON() ([]byte, error)    { return []byte(`"outbox_append_plan"`), nil }
func (*AppendPlan) UnmarshalJSON([]byte) error       { return invalid() }
func (p AppendPlan) LogValue() slog.Value            { return slog.StringValue("outbox_append_plan") }

type AppendReceipt struct {
	EventID  event.EventID       `json:"event_id"`
	Sequence foundation.Sequence `json:"outbox_sequence"`
}
type Appender interface {
	PrepareAppend(context.Context, identity.Actor, event.Event) (AppendPlan, error)
	AppendEventInTx(context.Context, foundation.Tx, identity.Actor, event.Event, AppendPlan) (AppendReceipt, error)
}
