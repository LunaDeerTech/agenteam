package contract

import (
	"context"
	"fmt"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ReferenceOwner struct {
	Kind      string        `json:"kind"`
	ID        string        `json:"id"`
	ProjectID *id.ProjectID `json:"project_id,omitempty"`
	Role      string        `json:"role"`
}

func (o ReferenceOwner) Validate() error {
	if !uuid(o.ID) || o.ProjectID != nil && o.ProjectID.Validate() != nil {
		return bad()
	}
	switch o.Kind {
	case "agent":
		if o.ProjectID == nil || !one(o.Role, "agent_model", "approval_model") {
			return bad()
		}
	case "project_summary":
		if o.ProjectID == nil || o.ID != o.ProjectID.String() || o.Role != "meeting_summary" {
			return bad()
		}
	case "platform_selector":
		if o.ProjectID != nil || !one(o.Role, "embedding", "memory", "reranker", "image", "meeting_summary") {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
func (o ReferenceOwner) Clone() ReferenceOwner { o.ProjectID = copyPtr(o.ProjectID); return o }

type ReferenceChange struct {
	Actor                id.Actor       `json:"-"`
	Owner                ReferenceOwner `json:"owner"`
	ExpectedOwnerVersion f.Version      `json:"expected_owner_version"`
	Before               *ModelID       `json:"before"`
	After                *ModelID       `json:"after"`
	ReasoningEffort      string         `json:"reasoning_effort,omitempty"`
}

func (r ReferenceChange) Validate() error {
	if r.Actor.Validate() != nil || r.Owner.Validate() != nil || r.ExpectedOwnerVersion.Validate() != nil || r.Before == nil && r.After == nil || r.Before != nil && r.Before.Validate() != nil || r.After != nil && r.After.Validate() != nil || r.ReasoningEffort != "" && !safeToken(r.ReasoningEffort, 32) {
		return bad()
	}
	if r.After == nil && !one(r.Owner.Role, "reranker", "image") {
		return bad()
	}
	if r.ReasoningEffort != "" && !one(r.Owner.Role, "agent_model", "approval_model", "memory") {
		return bad()
	}
	return nil
}
func (r ReferenceChange) Clone() ReferenceChange {
	r.Owner = r.Owner.Clone()
	r.Before = copyPtr(r.Before)
	r.After = copyPtr(r.After)
	return r
}
func ReferenceBinding(r ReferenceChange) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	return binding(struct {
		Actor   id.ActorDetails
		Request ReferenceChange
	}{r.Actor.Details(), r})
}
func ReplacementBinding(r DeleteModelRequest) (f.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	return binding(struct {
		Actor   id.ActorDetails
		Key     f.IdempotencyKey
		Request DeleteModelRequest
	}{r.Actor.Details(), r.Key, r})
}

type ReferencePlanDetails struct {
	Binding, Mapping f.Digest
	Locks            []f.LockRequest
	Owner            ReferenceOwner
	OwnerVersion     f.Version
	Consumer         *ConsumerDependencies
}

func (d ReferencePlanDetails) clone() ReferencePlanDetails {
	d.Locks = append([]f.LockRequest(nil), d.Locks...)
	d.Owner = d.Owner.Clone()
	d.Consumer = copyPtr(d.Consumer)
	return d
}

type ReplacementItem struct {
	Change ReferenceChange
	Plan   ReferencePlan
}
type ReplacementPlanDetails struct {
	Binding, Mapping f.Digest
	Locks            []f.LockRequest
	ModelID          ModelID
	ModelVersion     f.Version
	Replacement      *ModelID
	Items            []ReplacementItem
}

func (d ReplacementPlanDetails) clone() ReplacementPlanDetails {
	d.Locks = append([]f.LockRequest(nil), d.Locks...)
	d.Replacement = copyPtr(d.Replacement)
	d.Items = append([]ReplacementItem(nil), d.Items...)
	for n := range d.Items {
		d.Items[n].Change = d.Items[n].Change.Clone()
	}
	return d
}

type References interface {
	DiscoverReference(context.Context, id.Actor, ReferenceChange) (ReferencePlan, error)
	ApplyReferenceInTx(context.Context, f.Tx, ReferenceChange, ReferencePlan) error
	PrepareDeleteModel(context.Context, id.Actor, DeleteModelRequest) (ReplacementPlan, error)
	DeleteModelInTx(context.Context, f.Tx, DeleteModelRequest, ReplacementPlan) (CommandReceipt, error)
}

func (ReferenceOwner) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_reference_owner")) }
func (ReferenceOwner) LogValue() slog.Value       { return slog.StringValue("model_reference_owner") }

func (ReferenceChange) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_reference_change")) }
func (ReferenceChange) LogValue() slog.Value       { return slog.StringValue("model_reference_change") }

func (ReferencePlanDetails) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_reference_plan_details"))
}
func (ReferencePlanDetails) LogValue() slog.Value {
	return slog.StringValue("model_reference_plan_details")
}

func (ReplacementItem) Format(w fmt.State, _ rune) { _, _ = w.Write([]byte("model_replacement_item")) }
func (ReplacementItem) LogValue() slog.Value       { return slog.StringValue("model_replacement_item") }

func (ReplacementPlanDetails) Format(w fmt.State, _ rune) {
	_, _ = w.Write([]byte("model_replacement_plan_details"))
}
func (ReplacementPlanDetails) LogValue() slog.Value {
	return slog.StringValue("model_replacement_plan_details")
}
