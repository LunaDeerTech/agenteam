package contract

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type InitializationRequest struct {
	CreationID        CreationID                `json:"creation_id"`
	ProjectID         ProjectID                 `json:"project_id"`
	InitializationKey foundation.IdempotencyKey `json:"initialization_key"`
}

func (r InitializationRequest) Validate() error {
	if r.CreationID.Validate() != nil || r.ProjectID.Validate() != nil || r.InitializationKey.Validate() != nil {
		return invalid("", "INVALID_INITIALIZATION")
	}
	return nil
}
func (r InitializationRequest) MarshalJSON() ([]byte, error) {
	type wire InitializationRequest
	return checkedJSON(wire(r), r.Validate())
}
func (r *InitializationRequest) UnmarshalJSON(raw []byte) error {
	type wire InitializationRequest
	v, err := decodeFields[wire](raw, []string{"creation_id", "project_id", "initialization_key"}, nil, nil)
	if err != nil {
		return err
	}
	value := InitializationRequest(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

type InitializationState string

const (
	InitializationResultPending InitializationState = "pending"
	InitializationCompleted     InitializationState = "completed"
	InitializationFailed        InitializationState = "failed"
)

func (s InitializationState) Validate() error {
	return oneOf(s, InitializationResultPending, InitializationCompleted, InitializationFailed)
}
func (s InitializationState) MarshalJSON() ([]byte, error) { return enumJSON(s, s.Validate()) }
func (s *InitializationState) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, InitializationState.Validate)
	if err == nil {
		*s = v
	}
	return err
}

type InitializationResult struct {
	State       InitializationState  `json:"state"`
	CreationID  CreationID           `json:"creation_id"`
	ProjectID   ProjectID            `json:"project_id"`
	AddSkillsID *SkillID             `json:"add_skills_id,omitempty"`
	Revision    *foundation.Revision `json:"revision,omitempty"`
	SafeReason  SafeReason           `json:"safe_reason,omitempty"`
}

func (r InitializationResult) Validate() error {
	if r.State.Validate() != nil || r.CreationID.Validate() != nil || r.ProjectID.Validate() != nil || validateReason(string(r.State), r.SafeReason) != nil {
		return invalid("", "INVALID_INITIALIZATION_RESULT")
	}
	if r.State == InitializationCompleted {
		if r.AddSkillsID == nil || r.AddSkillsID.Validate() != nil || r.Revision == nil || r.Revision.Validate() != nil {
			return invalid("", "INVALID_INITIALIZATION_RESULT")
		}
	} else if r.AddSkillsID != nil || r.Revision != nil {
		return invalid("", "INVALID_INITIALIZATION_RESULT")
	}
	return nil
}
func (r InitializationResult) Matches(request InitializationRequest) bool {
	return r.Validate() == nil && request.Validate() == nil && r.CreationID == request.CreationID && r.ProjectID == request.ProjectID
}
func (r InitializationResult) MarshalJSON() ([]byte, error) {
	type wire InitializationResult
	return checkedJSON(wire(r), r.Validate())
}
func (r *InitializationResult) UnmarshalJSON(raw []byte) error {
	type wire InitializationResult
	v, err := decodeFields[wire](raw, []string{"state", "creation_id", "project_id"}, []string{"add_skills_id", "revision", "safe_reason"}, nil)
	if err != nil {
		return err
	}
	value := InitializationResult(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

type InitializationReceipt struct {
	CreationID  CreationID          `json:"creation_id"`
	ProjectID   ProjectID           `json:"project_id"`
	AddSkillsID SkillID             `json:"add_skills_id"`
	Revision    foundation.Revision `json:"revision"`
}

func (r InitializationReceipt) Validate() error {
	if r.CreationID.Validate() != nil || r.ProjectID.Validate() != nil || r.AddSkillsID.Validate() != nil || r.Revision.Validate() != nil {
		return invalid("", "INVALID_INITIALIZATION_RECEIPT")
	}
	return nil
}
func (r InitializationReceipt) Matches(request InitializationRequest) bool {
	return r.Validate() == nil && request.Validate() == nil && r.CreationID == request.CreationID && r.ProjectID == request.ProjectID
}
func (r InitializationReceipt) MarshalJSON() ([]byte, error) {
	type wire InitializationReceipt
	return checkedJSON(wire(r), r.Validate())
}
func (r *InitializationReceipt) UnmarshalJSON(raw []byte) error {
	type wire InitializationReceipt
	v, err := decodeFields[wire](raw, []string{"creation_id", "project_id", "add_skills_id", "revision"}, nil, nil)
	if err != nil {
		return err
	}
	value := InitializationReceipt(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

type initializationIssuer struct{ nonzero byte }

// InitializationPlanIssuer belongs privately to one provider instance. Plans
// from another provider, including after replacement/restart, must be rediscovered.
type InitializationPlanIssuer struct{ identity *initializationIssuer }

func NewInitializationPlanIssuer() InitializationPlanIssuer {
	return InitializationPlanIssuer{&initializationIssuer{1}}
}

type InitializationConfirmationPlan struct{ data func() initializationPlanData }
type initializationPlanData struct {
	issuer  *initializationIssuer
	actor   identity.Actor
	request InitializationRequest
	receipt InitializationReceipt
	locks   []foundation.LockRequest
}

// Plan only captures a discovery. It checks request mapping, never current
// authorization or completion. The provider must separately require the
// registered project-initialization role and call ProjectAuthority in Confirm.
// That role cannot be registered on B01's unchanged identity baseline.
func (i InitializationPlanIssuer) Plan(actor identity.Actor, request InitializationRequest, receipt InitializationReceipt, locks []foundation.LockRequest) (InitializationConfirmationPlan, error) {
	if i.identity == nil || actor.Validate() != nil || actor.Details().Kind != identity.Service || request.Validate() != nil || !receipt.Matches(request) || actor.Details().ProjectID != request.ProjectID.String() || actor.Details().CauseRef != request.CreationID.String() {
		return InitializationConfirmationPlan{}, invalid("", "INVALID_INITIALIZATION_PLAN")
	}
	normalized, err := normalizePlanLocks(locks, request.ProjectID)
	if err != nil {
		return InitializationConfirmationPlan{}, err
	}
	d := initializationPlanData{i.identity, actor, request, receipt, normalized}
	return InitializationConfirmationPlan{func() initializationPlanData { return d }}, nil
}
func normalizePlanLocks(locks []foundation.LockRequest, project ProjectID) ([]foundation.LockRequest, error) {
	copyLocks := append([]foundation.LockRequest(nil), locks...)
	for _, lock := range copyLocks {
		if lock.Key.Validate() != nil || !lock.Mode.Valid() {
			return nil, invalid("/required_locks", "INVALID_LOCK")
		}
	}
	sort.Slice(copyLocks, func(i, j int) bool { return foundation.CompareLockKeys(copyLocks[i].Key, copyLocks[j].Key) < 0 })
	out := make([]foundation.LockRequest, 0, len(copyLocks))
	gate, _ := foundation.ProjectLock(project.String())
	projectEX := false
	for _, lock := range copyLocks {
		if lock.Key.Canonical() == gate.Canonical() && lock.Mode == foundation.Exclusive {
			projectEX = true
		}
		if len(out) > 0 && out[len(out)-1].Key.Canonical() == lock.Key.Canonical() {
			if lock.Mode == foundation.Exclusive {
				out[len(out)-1].Mode = foundation.Exclusive
			}
			continue
		}
		out = append(out, lock)
	}
	if !projectEX {
		return nil, invalid("/required_locks", "PROJECT_EX_REQUIRED")
	}
	return out, nil
}

// Matches is issuer/request equality only. It is not an InTx check, a lock
// assertion or an authorization grant; callers must not use it as one.
func (i InitializationPlanIssuer) Matches(plan InitializationConfirmationPlan, actor identity.Actor, request InitializationRequest) bool {
	return i.identity != nil && plan.data != nil && plan.data().issuer == i.identity && plan.data().actor.Equal(actor) && plan.data().request == request
}
func (p InitializationConfirmationPlan) RequiredLocks() []foundation.LockRequest {
	if p.data == nil {
		return nil
	}
	return append([]foundation.LockRequest(nil), p.data().locks...)
}
func (p InitializationConfirmationPlan) ProposedReceipt() InitializationReceipt {
	if p.data == nil {
		return InitializationReceipt{}
	}
	return p.data().receipt
}
func (InitializationConfirmationPlan) MarshalJSON() ([]byte, error) {
	return []byte(`"initialization_confirmation_plan"`), nil
}
func (*InitializationConfirmationPlan) UnmarshalJSON([]byte) error {
	return invalid("", "TRUSTED_PLAN_REQUIRED")
}
func (InitializationConfirmationPlan) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "initialization_confirmation_plan")
}
func (InitializationConfirmationPlan) LogValue() slog.Value {
	return slog.StringValue("initialization_confirmation_plan")
}
func (InitializationPlanIssuer) MarshalJSON() ([]byte, error) {
	return []byte(`"initialization_plan_issuer"`), nil
}
func (*InitializationPlanIssuer) UnmarshalJSON([]byte) error {
	return invalid("", "TRUSTED_ISSUER_REQUIRED")
}
func (InitializationPlanIssuer) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "initialization_plan_issuer")
}
func (InitializationPlanIssuer) LogValue() slog.Value {
	return slog.StringValue("initialization_plan_issuer")
}

// ProjectSkillInitializer has no default implementation. Missing binding is
// DEPENDENCY_UNBOUND, never an empty Add Skills success. Discovery is outside
// Tx; Confirm requires a nonzero live caller Tx, all plan locks already held,
// exact current creation gate, actual protected Add Skills, published revision
// and object mapping. It must not acquire locks, nest a Tx or perform external
// I/O. A completed result alone is not a confirmation receipt.
type ProjectSkillInitializer interface {
	InitializeProjectSkills(context.Context, identity.Actor, InitializationRequest) (InitializationResult, error)
	InspectProjectSkills(context.Context, identity.Actor, InitializationRequest) (InitializationResult, error)
	DiscoverConfirmation(context.Context, identity.Actor, InitializationRequest) (InitializationConfirmationPlan, error)
	ConfirmInitializedInTx(context.Context, foundation.Tx, identity.Actor, InitializationRequest, InitializationConfirmationPlan) (InitializationReceipt, error)
}
