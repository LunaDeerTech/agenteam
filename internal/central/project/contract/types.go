// Package contract defines Project values and consumer ports. Pure validation
// and gate rules do not authenticate a Session, establish Owner authority, or
// prove that an external participant has stopped or initialized a resource.
package contract

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ProjectID = identity.ProjectID
type Creation struct{}
type Operation struct{}
type Sprint struct{}
type Meeting struct{}
type Skill struct{}
type CreationID = foundation.ID[Creation]
type OperationID = foundation.ID[Operation]
type SprintID = foundation.ID[Sprint]
type MeetingID = foundation.ID[Meeting]
type SkillID = foundation.ID[Skill]

type Lifecycle string

const (
	Active    Lifecycle = "active"
	Archiving Lifecycle = "archiving"
	Archived  Lifecycle = "archived"
	Deleting  Lifecycle = "deleting"
)

func (s Lifecycle) Validate() error              { return oneOf(s, Active, Archiving, Archived, Deleting) }
func (s Lifecycle) MarshalJSON() ([]byte, error) { return enumJSON(s, s.Validate()) }
func (s *Lifecycle) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, Lifecycle.Validate)
	if err == nil {
		*s = v
	}
	return err
}

// SafeReason is a closed operational reason, never an adapter error message.
type SafeReason string

const (
	ReasonDependencyUnbound     SafeReason = "dependency_unbound"
	ReasonDependencyUnavailable SafeReason = "dependency_unavailable"
	ReasonWorkPending           SafeReason = "work_pending"
	ReasonOutcomeUnknown        SafeReason = "outcome_unknown"
	ReasonOperationFailed       SafeReason = "operation_failed"
)

func (r SafeReason) Validate() error {
	return oneOf(r, ReasonDependencyUnbound, ReasonDependencyUnavailable, ReasonWorkPending, ReasonOutcomeUnknown, ReasonOperationFailed)
}
func (r SafeReason) MarshalJSON() ([]byte, error) { return enumJSON(r, r.Validate()) }
func (r *SafeReason) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, SafeReason.Validate)
	if err == nil {
		*r = v
	}
	return err
}

type ProjectRef struct {
	ID              ProjectID           `json:"id"`
	OwnerUserID     identity.UserID     `json:"owner_user_id"`
	Name            string              `json:"name"`
	NormalizedName  string              `json:"normalized_name"`
	Description     string              `json:"description"`
	Lifecycle       Lifecycle           `json:"lifecycle"`
	Version         foundation.Version  `json:"version"`
	CurrentSprintID *SprintID           `json:"current_sprint_id"`
	CreatedAt       foundation.Instant  `json:"created_at"`
	UpdatedAt       foundation.Instant  `json:"updated_at"`
	ArchivedAt      *foundation.Instant `json:"archived_at,omitempty"`
}

func (p ProjectRef) Validate() error {
	normalized, err := NormalizeName(p.Name)
	if err != nil || normalized != p.NormalizedName || p.ID.Validate() != nil || p.OwnerUserID.Validate() != nil || ValidateDescription(p.Description) != nil || p.Lifecycle.Validate() != nil || p.Version.Validate() != nil || !validTimes(p.CreatedAt, p.UpdatedAt) {
		return invalid("", "INVALID_PROJECT")
	}
	if p.CurrentSprintID != nil && p.CurrentSprintID.Validate() != nil {
		return invalid("/current_sprint_id", "INVALID_ID")
	}
	if p.ArchivedAt != nil && (p.ArchivedAt.Validate() != nil || p.ArchivedAt.Time().Before(p.CreatedAt.Time()) || p.ArchivedAt.Time().After(p.UpdatedAt.Time())) || p.Lifecycle == Archived && p.ArchivedAt == nil || (p.Lifecycle == Active || p.Lifecycle == Archiving) && p.ArchivedAt != nil {
		return invalid("/archived_at", "INVALID_STATE")
	}
	return nil
}
func (p ProjectRef) MarshalJSON() ([]byte, error) {
	type wire ProjectRef
	return checkedJSON(wire(p), p.Validate())
}
func (p *ProjectRef) UnmarshalJSON(raw []byte) error {
	type wire ProjectRef
	v, err := decodeFields[wire](raw, []string{"id", "owner_user_id", "name", "normalized_name", "description", "lifecycle", "version", "created_at", "updated_at"}, []string{"current_sprint_id", "archived_at"}, []string{"current_sprint_id", "archived_at"})
	if err != nil {
		return err
	}
	value := ProjectRef(v)
	if err = value.Validate(); err == nil {
		*p = value
	}
	return err
}

// ProjectAccess is a callback-local trusted-authorizer projection. Constructing
// it checks shape/identity only; the producer must already have checked the live
// Session, Owner, gate and held locks in the same transaction. Never cache it.
type ProjectAccess struct{ data func() projectAccessData }
type projectAccessData struct {
	actor     identity.Actor
	project   ProjectRef
	checkedAt foundation.Instant
}

func NewProjectAccess(actor identity.Actor, project ProjectRef, checkedAt foundation.Instant) (ProjectAccess, error) {
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || project.Validate() != nil || actor.Details().UserID != project.OwnerUserID.String() || checkedAt.Validate() != nil {
		return ProjectAccess{}, invalid("", "INVALID_ACCESS_PROJECTION")
	}
	d := projectAccessData{actor, cloneProject(project), checkedAt}
	return ProjectAccess{func() projectAccessData { return d }}, nil
}
func cloneProject(p ProjectRef) ProjectRef {
	if p.CurrentSprintID != nil {
		v := *p.CurrentSprintID
		p.CurrentSprintID = &v
	}
	if p.ArchivedAt != nil {
		v := *p.ArchivedAt
		p.ArchivedAt = &v
	}
	return p
}
func (p ProjectAccess) Project() ProjectRef {
	if p.data == nil {
		return ProjectRef{}
	}
	return cloneProject(p.data().project)
}
func (p ProjectAccess) CheckedAt() foundation.Instant {
	if p.data == nil {
		return foundation.Instant{}
	}
	return p.data().checkedAt
}
func (p ProjectAccess) Matches(actor identity.Actor, project ProjectID) bool {
	return p.data != nil && p.data().actor.Equal(actor) && p.data().project.ID == project
}
func (ProjectAccess) MarshalJSON() ([]byte, error) { return []byte(`"project_access"`), nil }
func (*ProjectAccess) UnmarshalJSON([]byte) error  { return invalid("", "TRUSTED_ACCESS_REQUIRED") }
func (ProjectAccess) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "project_access") }
func (ProjectAccess) LogValue() slog.Value         { return slog.StringValue("project_access") }

type CreationState string

const (
	CreationAccepted     CreationState = "accepted"
	CreationInitializing CreationState = "initializing"
	CreationFailed       CreationState = "failed"
	CreationCompleted    CreationState = "completed"
)

func (s CreationState) Validate() error {
	return oneOf(s, CreationAccepted, CreationInitializing, CreationFailed, CreationCompleted)
}
func (s CreationState) MarshalJSON() ([]byte, error) { return enumJSON(s, s.Validate()) }
func (s *CreationState) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, CreationState.Validate)
	if err == nil {
		*s = v
	}
	return err
}

type CreationOperation struct {
	ID         CreationID         `json:"id"`
	ProjectID  ProjectID          `json:"project_id"`
	State      CreationState      `json:"state"`
	Version    foundation.Version `json:"version"`
	SafeReason SafeReason         `json:"safe_reason,omitempty"`
	CreatedAt  foundation.Instant `json:"created_at"`
	UpdatedAt  foundation.Instant `json:"updated_at"`
}

func (o CreationOperation) Validate() error {
	if o.ID.Validate() != nil || o.ProjectID.Validate() != nil || o.State.Validate() != nil || o.Version.Validate() != nil || !validTimes(o.CreatedAt, o.UpdatedAt) || validateReason(string(o.State), o.SafeReason) != nil {
		return invalid("", "INVALID_CREATION")
	}
	return nil
}
func (o CreationOperation) MarshalJSON() ([]byte, error) {
	type wire CreationOperation
	return checkedJSON(wire(o), o.Validate())
}
func (o *CreationOperation) UnmarshalJSON(raw []byte) error {
	type wire CreationOperation
	v, err := decodeFields[wire](raw, []string{"id", "project_id", "state", "version", "created_at", "updated_at"}, []string{"safe_reason"}, nil)
	if err != nil {
		return err
	}
	value := CreationOperation(v)
	if err = value.Validate(); err == nil {
		*o = value
	}
	return err
}

type CreationResultState string

const (
	CreationReady   CreationResultState = "ready"
	CreationPending CreationResultState = "accepted"
)

func (s CreationResultState) Validate() error              { return oneOf(s, CreationReady, CreationPending) }
func (s CreationResultState) MarshalJSON() ([]byte, error) { return enumJSON(s, s.Validate()) }
func (s *CreationResultState) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, CreationResultState.Validate)
	if err == nil {
		*s = v
	}
	return err
}

type CreationResult struct {
	State     CreationResultState `json:"state"`
	Project   *ProjectRef         `json:"project,omitempty"`
	Operation *CreationOperation  `json:"operation,omitempty"`
}

func (r CreationResult) Validate() error {
	switch r.State {
	case CreationReady:
		if r.Project != nil && r.Operation == nil && r.Project.Lifecycle != Deleting {
			return r.Project.Validate()
		}
	case CreationPending:
		if r.Project == nil && r.Operation != nil && r.Operation.State != CreationCompleted {
			return r.Operation.Validate()
		}
	}
	return invalid("", "INVALID_RESULT")
}
func (r CreationResult) MarshalJSON() ([]byte, error) {
	type wire CreationResult
	return checkedJSON(wire(r), r.Validate())
}
func (r *CreationResult) UnmarshalJSON(raw []byte) error {
	type wire CreationResult
	v, err := decodeFields[wire](raw, []string{"state"}, []string{"project", "operation"}, nil)
	if err != nil {
		return err
	}
	value := CreationResult(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

// DeletionReceiptData is an explicit trusted persistence projection. The
// opaque receipt's JSON deliberately omits command hashes and request digests.
type DeletionReceiptData struct {
	OperationID         OperationID
	DeletedProjectID    ProjectID
	OriginalOwnerUserID identity.UserID
	CommandKeyHash      foundation.Digest
	RequestDigest       foundation.Digest
	CompletedAt         foundation.Instant
}

func (d DeletionReceiptData) Validate() error {
	if d.OperationID.Validate() != nil || d.DeletedProjectID.Validate() != nil || d.OriginalOwnerUserID.Validate() != nil || d.CommandKeyHash.Validate() != nil || d.RequestDigest.Validate() != nil || d.CompletedAt.Validate() != nil {
		return invalid("", "INVALID_RECEIPT")
	}
	return nil
}

type ProjectDeletionReceipt struct{ data func() DeletionReceiptData }

func NewProjectDeletionReceipt(d DeletionReceiptData) (ProjectDeletionReceipt, error) {
	if err := d.Validate(); err != nil {
		return ProjectDeletionReceipt{}, err
	}
	return ProjectDeletionReceipt{func() DeletionReceiptData { return d }}, nil
}
func (r ProjectDeletionReceipt) Validate() error {
	if r.data == nil {
		return invalid("", "INVALID_RECEIPT")
	}
	return r.data().Validate()
}
func (r ProjectDeletionReceipt) Details() DeletionReceiptData {
	if r.data == nil {
		return DeletionReceiptData{}
	}
	return r.data()
}
func (r ProjectDeletionReceipt) MarshalJSON() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	d := r.data()
	return json.Marshal(struct {
		OperationID         OperationID        `json:"operation_id"`
		DeletedProjectID    ProjectID          `json:"deleted_project_id"`
		OriginalOwnerUserID identity.UserID    `json:"original_owner_user_id"`
		Status              string             `json:"status"`
		CompletedAt         foundation.Instant `json:"completed_at"`
	}{d.OperationID, d.DeletedProjectID, d.OriginalOwnerUserID, "completed", d.CompletedAt})
}
func (*ProjectDeletionReceipt) UnmarshalJSON([]byte) error {
	return invalid("", "TRUSTED_RESULT_REQUIRED")
}
func (ProjectDeletionReceipt) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_deletion_receipt")
}
func (ProjectDeletionReceipt) LogValue() slog.Value {
	return slog.StringValue("project_deletion_receipt")
}

func validateReason(state string, reason SafeReason) error {
	if reason != "" && reason.Validate() != nil || state == "failed" && reason == "" || (state == "completed" || state == "stopped") && reason != "" {
		return invalid("/safe_reason", "INVALID_STATE")
	}
	return nil
}
