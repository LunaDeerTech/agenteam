package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sort"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type InitializationGate string

const (
	InitializationPending InitializationGate = "pending"
	Initialized           InitializationGate = "initialized"
)

func (g InitializationGate) Validate() error              { return oneOf(g, InitializationPending, Initialized) }
func (g InitializationGate) MarshalJSON() ([]byte, error) { return enumJSON(g, g.Validate()) }
func (g *InitializationGate) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, InitializationGate.Validate)
	if err == nil {
		*g = v
	}
	return err
}

// CheckOwnerGate checks only the gate after a trusted caller has established
// current Session and Owner. It neither issues a grant nor validates identity.
// Lifecycle and convergence use their own exact-cause ports, never this rule.
func CheckOwnerGate(lifecycle Lifecycle, initialization InitializationGate, intent identity.AccessIntent) error {
	if lifecycle.Validate() != nil || initialization.Validate() != nil {
		return invalid("", "INVALID_GATE")
	}
	switch intent {
	case identity.Read, identity.Mutate, identity.Launch, identity.Resume:
	default:
		return fault(foundation.Forbidden)
	}
	if initialization != Initialized || lifecycle == Deleting {
		return fault(foundation.ProjectNotActive)
	}
	if intent == identity.Read || lifecycle == Active {
		return nil
	}
	return fault(foundation.ProjectNotActive)
}

// CheckOwnerActorKind is only a dispatch rule. In particular, AgentRun IDs do
// not replace D22's still-unbound current Execution authorizer.
func CheckOwnerActorKind(actor identity.Actor) error {
	if actor.Validate() != nil {
		return fault(foundation.Unauthenticated)
	}
	switch actor.Details().Kind {
	case identity.Human:
		return nil
	case identity.AgentRun:
		return fault(foundation.DependencyUnbound)
	default:
		return fault(foundation.Forbidden)
	}
}

type LifecycleAction string

const (
	Archive LifecycleAction = "archive"
	Restore LifecycleAction = "restore"
	Delete  LifecycleAction = "delete"
)

func (a LifecycleAction) Validate() error              { return oneOf(a, Archive, Restore, Delete) }
func (a LifecycleAction) ValidateOperation() error     { return oneOf(a, Archive, Delete) }
func (a LifecycleAction) MarshalJSON() ([]byte, error) { return enumJSON(a, a.Validate()) }
func (a *LifecycleAction) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, LifecycleAction.Validate)
	if err == nil {
		*a = v
	}
	return err
}

// ValidateLifecycleTransition describes an edge, not permission to take it.
// Required stop evidence and transaction-held locks belong to the service.
func ValidateLifecycleTransition(from, to Lifecycle, action LifecycleAction, operation *OperationID) error {
	if from.Validate() != nil || to.Validate() != nil || action.Validate() != nil {
		return invalid("", "INVALID_TRANSITION")
	}
	if action == Restore {
		if from == Archived && to == Active && operation == nil {
			return nil
		}
		return invalid("", "INVALID_TRANSITION")
	}
	if operation == nil || operation.Validate() != nil {
		return invalid("/operation_id", "INVALID_ID")
	}
	if action == Archive && (from == Active && to == Archiving || from == Archiving && to == Archived) || action == Delete && (from == Active || from == Archived) && to == Deleting {
		return nil
	}
	return invalid("", "INVALID_TRANSITION")
}

type OperationState string

const (
	OperationAccepted  OperationState = "accepted"
	OperationStopping  OperationState = "stopping"
	OperationCleaning  OperationState = "cleaning"
	OperationCompleted OperationState = "completed"
	OperationFailed    OperationState = "failed"
)

func (s OperationState) Validate() error {
	return oneOf(s, OperationAccepted, OperationStopping, OperationCleaning, OperationCompleted, OperationFailed)
}
func (s OperationState) MarshalJSON() ([]byte, error) { return enumJSON(s, s.Validate()) }
func (s *OperationState) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, OperationState.Validate)
	if err == nil {
		*s = v
	}
	return err
}

type OperationPhase string

const (
	StopPhase    OperationPhase = "stop"
	CleanupPhase OperationPhase = "cleanup"
)

func (p OperationPhase) Validate() error              { return oneOf(p, StopPhase, CleanupPhase) }
func (p OperationPhase) MarshalJSON() ([]byte, error) { return enumJSON(p, p.Validate()) }
func (p *OperationPhase) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, OperationPhase.Validate)
	if err == nil {
		*p = v
	}
	return err
}

// Failed work resumes its persisted phase and original cause. This function
// cannot establish participant completion or alter the Project gate.
func ValidateOperationTransition(action LifecycleAction, from, to OperationState, resume OperationPhase) error {
	if action.ValidateOperation() != nil || from.Validate() != nil || to.Validate() != nil || action == Archive && (from == OperationCleaning || to == OperationCleaning) {
		return invalid("", "INVALID_TRANSITION")
	}
	if from == OperationFailed {
		if resume == StopPhase && to == OperationStopping || resume == CleanupPhase && action == Delete && to == OperationCleaning {
			return nil
		}
		return invalid("", "INVALID_TRANSITION")
	}
	if resume != "" {
		return invalid("/resume_phase", "INVALID_STATE")
	}
	if from == OperationAccepted && (to == OperationStopping || to == OperationFailed) || from == OperationStopping && (to == OperationFailed || action == Archive && to == OperationCompleted || action == Delete && to == OperationCleaning) || from == OperationCleaning && (to == OperationCompleted || to == OperationFailed) {
		return nil
	}
	return invalid("", "INVALID_TRANSITION")
}

type ScopeKind string

const (
	ProjectScope ScopeKind = "project"
	MeetingScope ScopeKind = "meeting"
)

func (k ScopeKind) Validate() error              { return oneOf(k, ProjectScope, MeetingScope) }
func (k ScopeKind) MarshalJSON() ([]byte, error) { return enumJSON(k, k.Validate()) }
func (k *ScopeKind) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, ScopeKind.Validate)
	if err == nil {
		*k = v
	}
	return err
}

type ScopeRef struct {
	Kind      ScopeKind  `json:"kind"`
	ProjectID ProjectID  `json:"project_id"`
	MeetingID *MeetingID `json:"meeting_id,omitempty"`
}

func (s ScopeRef) Validate() error {
	if s.ProjectID.Validate() != nil || s.Kind.Validate() != nil || s.Kind == ProjectScope && s.MeetingID != nil || s.Kind == MeetingScope && (s.MeetingID == nil || s.MeetingID.Validate() != nil) {
		return invalid("", "INVALID_SCOPE")
	}
	return nil
}
func (s ScopeRef) Equal(other ScopeRef) bool {
	return s.Validate() == nil && other.Validate() == nil && s.Kind == other.Kind && s.ProjectID == other.ProjectID && (s.MeetingID == nil && other.MeetingID == nil || s.MeetingID != nil && other.MeetingID != nil && *s.MeetingID == *other.MeetingID)
}
func (s ScopeRef) MarshalJSON() ([]byte, error) {
	type wire ScopeRef
	return checkedJSON(wire(s), s.Validate())
}
func (s *ScopeRef) UnmarshalJSON(raw []byte) error {
	type wire ScopeRef
	v, err := decodeFields[wire](raw, []string{"kind", "project_id"}, []string{"meeting_id"}, nil)
	if err != nil {
		return err
	}
	value := ScopeRef(v)
	if err = value.Validate(); err == nil {
		*s = value
	}
	return err
}
func cloneScope(s ScopeRef) ScopeRef {
	if s.MeetingID != nil {
		v := *s.MeetingID
		s.MeetingID = &v
	}
	return s
}

// RequireProjectScope records D08's implemented scope; Meeting is reserved for D24.
func RequireProjectScope(s ScopeRef) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.Kind != ProjectScope {
		return fault(foundation.DependencyUnbound)
	}
	return nil
}

type LifecycleCause struct {
	OperationID    OperationID        `json:"operation_id"`
	Action         LifecycleAction    `json:"action"`
	ProjectVersion foundation.Version `json:"project_version"`
}

func (c LifecycleCause) Validate() error {
	if c.OperationID.Validate() != nil || c.Action.ValidateOperation() != nil || c.ProjectVersion.Validate() != nil {
		return invalid("", "INVALID_CAUSE")
	}
	return nil
}
func (c LifecycleCause) MarshalJSON() ([]byte, error) {
	type wire LifecycleCause
	return checkedJSON(wire(c), c.Validate())
}
func (c *LifecycleCause) UnmarshalJSON(raw []byte) error {
	type wire LifecycleCause
	v, err := decodeFields[wire](raw, []string{"operation_id", "action", "project_version"}, nil, nil)
	if err != nil {
		return err
	}
	value := LifecycleCause(v)
	if err = value.Validate(); err == nil {
		*c = value
	}
	return err
}

// Names/kinds become closed sets in the trusted, persisted required manifest;
// syntax alone never establishes registration or an enabled domain binding.
type ParticipantName string

const (
	ArtifactObjectParticipant ParticipantName = "artifact-object"
	SecretParticipant         ParticipantName = "secret"
	OutboxParticipant         ParticipantName = "outbox"
	AuditParticipant          ParticipantName = "audit"
	SkillsParticipant         ParticipantName = "agent-skills-variables"
)

func (n ParticipantName) Validate() error              { return event.StableName(n).Validate() }
func (n ParticipantName) MarshalJSON() ([]byte, error) { return enumJSON(n, n.Validate()) }
func (n *ParticipantName) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, ParticipantName.Validate)
	if err == nil {
		*n = v
	}
	return err
}

type ReferenceKind string

func (k ReferenceKind) Validate() error              { return event.StableName(k).Validate() }
func (k ReferenceKind) MarshalJSON() ([]byte, error) { return enumJSON(k, k.Validate()) }
func (k *ReferenceKind) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, ReferenceKind.Validate)
	if err == nil {
		*k = v
	}
	return err
}

type ResourceIdentity struct{}
type ResourceID = foundation.ID[ResourceIdentity]
type PendingRef struct {
	Participant ParticipantName `json:"participant"`
	Kind        ReferenceKind   `json:"kind"`
	ID          ResourceID      `json:"id"`
}

func (r PendingRef) Validate() error {
	if r.Participant.Validate() != nil || r.Kind.Validate() != nil || r.ID.Validate() != nil {
		return invalid("", "INVALID_REFERENCE")
	}
	return nil
}
func (r PendingRef) MarshalJSON() ([]byte, error) {
	type wire PendingRef
	return checkedJSON(wire(r), r.Validate())
}
func (r *PendingRef) UnmarshalJSON(raw []byte) error {
	type wire PendingRef
	v, err := decodeFields[wire](raw, []string{"participant", "kind", "id"}, nil, nil)
	if err != nil {
		return err
	}
	value := PendingRef(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}
func validateRefs(refs []PendingRef, participant ParticipantName) error {
	seen := make(map[PendingRef]bool, len(refs))
	for _, ref := range refs {
		if ref.Validate() != nil || participant != "" && ref.Participant != participant || seen[ref] {
			return invalid("", "INVALID_REFERENCE")
		}
		seen[ref] = true
	}
	return nil
}

type ParticipantRegistration struct {
	Name            ParticipantName    `json:"name"`
	ContractVersion foundation.Version `json:"contract_version"`
	OwnerModule     event.StableName   `json:"owner_module"`
	ReferenceKinds  []ReferenceKind    `json:"reference_kinds"`
	CleanupAfter    []ParticipantName  `json:"cleanup_after"`
}

// RequiredManifest is an immutable description, not a registry of adapters.
// Callers must independently ensure every enabled module is represented and
// all declared versions/phases are bound before accepting an operation.
type RequiredManifest struct {
	data func() []ParticipantRegistration
}

func NewRequiredManifest(entries []ParticipantRegistration) (RequiredManifest, error) {
	copyEntries := cloneRegistrations(entries)
	sort.Slice(copyEntries, func(i, j int) bool { return copyEntries[i].Name < copyEntries[j].Name })
	byName := map[ParticipantName]ParticipantRegistration{}
	for _, entry := range copyEntries {
		if entry.Name.Validate() != nil || entry.ContractVersion.Validate() != nil || entry.OwnerModule.Validate() != nil {
			return RequiredManifest{}, invalid("", "INVALID_MANIFEST")
		}
		if _, ok := byName[entry.Name]; ok {
			return RequiredManifest{}, invalid("", "DUPLICATE_PARTICIPANT")
		}
		byName[entry.Name] = entry
		seen := map[ReferenceKind]bool{}
		for _, kind := range entry.ReferenceKinds {
			if kind.Validate() != nil || seen[kind] {
				return RequiredManifest{}, invalid("", "INVALID_MANIFEST")
			}
			seen[kind] = true
		}
	}
	for _, name := range []ParticipantName{ArtifactObjectParticipant, SecretParticipant, OutboxParticipant, AuditParticipant} {
		if _, ok := byName[name]; !ok {
			return RequiredManifest{}, fault(foundation.DependencyUnbound)
		}
	}
	visiting, visited := map[ParticipantName]bool{}, map[ParticipantName]bool{}
	var visit func(ParticipantName) bool
	visit = func(name ParticipantName) bool {
		if visiting[name] {
			return false
		}
		if visited[name] {
			return true
		}
		entry, ok := byName[name]
		if !ok {
			return false
		}
		visiting[name] = true
		seen := map[ParticipantName]bool{}
		for _, dependency := range entry.CleanupAfter {
			if seen[dependency] || !visit(dependency) {
				return false
			}
			seen[dependency] = true
		}
		visiting[name] = false
		visited[name] = true
		return true
	}
	for name := range byName {
		if !visit(name) {
			return RequiredManifest{}, invalid("", "INVALID_CLEANUP_ORDER")
		}
	}
	// Outbox is after every producer; Audit is after Outbox. Direct edges make
	// that safety requirement explicit even when additional owners are enabled.
	contains := func(names []ParticipantName, name ParticipantName) bool {
		for _, v := range names {
			if v == name {
				return true
			}
		}
		return false
	}
	for name := range byName {
		if name != OutboxParticipant && name != AuditParticipant && !contains(byName[OutboxParticipant].CleanupAfter, name) {
			return RequiredManifest{}, invalid("", "INVALID_CLEANUP_ORDER")
		}
	}
	if !contains(byName[AuditParticipant].CleanupAfter, OutboxParticipant) {
		return RequiredManifest{}, invalid("", "INVALID_CLEANUP_ORDER")
	}
	for i := range copyEntries {
		sort.Slice(copyEntries[i].ReferenceKinds, func(a, b int) bool { return copyEntries[i].ReferenceKinds[a] < copyEntries[i].ReferenceKinds[b] })
		sort.Slice(copyEntries[i].CleanupAfter, func(a, b int) bool { return copyEntries[i].CleanupAfter[a] < copyEntries[i].CleanupAfter[b] })
	}
	return RequiredManifest{func() []ParticipantRegistration { return copyEntries }}, nil
}
func cloneRegistrations(entries []ParticipantRegistration) []ParticipantRegistration {
	out := append([]ParticipantRegistration{}, entries...)
	for i := range out {
		out[i].ReferenceKinds = append([]ReferenceKind{}, out[i].ReferenceKinds...)
		out[i].CleanupAfter = append([]ParticipantName{}, out[i].CleanupAfter...)
	}
	return out
}
func (m RequiredManifest) Entries() []ParticipantRegistration {
	if m.data == nil {
		return nil
	}
	return cloneRegistrations(m.data())
}
func (m RequiredManifest) Require(names ...ParticipantName) error {
	if m.data == nil {
		return fault(foundation.DependencyUnbound)
	}
	for _, name := range names {
		found := false
		for _, entry := range m.data() {
			found = found || entry.Name == name
		}
		if !found {
			return fault(foundation.DependencyUnbound)
		}
	}
	return nil
}
func (m RequiredManifest) ValidateRefs(refs []PendingRef) error {
	if m.data == nil {
		return fault(foundation.DependencyUnbound)
	}
	if err := validateRefs(refs, ""); err != nil {
		return err
	}
	for _, ref := range refs {
		registered := false
		for _, entry := range m.data() {
			if entry.Name == ref.Participant {
				for _, kind := range entry.ReferenceKinds {
					registered = registered || kind == ref.Kind
				}
			}
		}
		if !registered {
			return invalid("", "UNREGISTERED_REFERENCE")
		}
	}
	return nil
}
func (m RequiredManifest) Digest() (foundation.Digest, error) {
	if m.data == nil {
		return "", fault(foundation.DependencyUnbound)
	}
	raw, err := json.Marshal(m.data())
	if err != nil {
		return "", invalid("", "INVALID_MANIFEST")
	}
	return digestBytes("agenteam.project.manifest.v1", raw), nil
}
func (m RequiredManifest) MarshalJSON() ([]byte, error) {
	if m.data == nil {
		return nil, invalid("", "INVALID_MANIFEST")
	}
	return json.Marshal(m.Entries())
}
func (*RequiredManifest) UnmarshalJSON([]byte) error { return invalid("", "TRUSTED_MANIFEST_REQUIRED") }

type LifecycleOperation struct {
	ID                      OperationID         `json:"id"`
	ProjectID               ProjectID           `json:"project_id"`
	Action                  LifecycleAction     `json:"action"`
	ProjectVersion          foundation.Version  `json:"project_version"`
	CompletedProjectVersion *foundation.Version `json:"completed_project_version,omitempty"`
	State                   OperationState      `json:"state"`
	Version                 foundation.Version  `json:"version"`
	RequiredParticipants    []ParticipantName   `json:"required_participants"`
	CompletedParticipants   []ParticipantName   `json:"completed_participants"`
	PendingResources        []PendingRef        `json:"pending_resources"`
	PendingRefsTruncated    bool                `json:"pending_refs_truncated"`
	SafeReason              SafeReason          `json:"safe_reason,omitempty"`
	CreatedAt               foundation.Instant  `json:"created_at"`
	UpdatedAt               foundation.Instant  `json:"updated_at"`
	CompletedAt             *foundation.Instant `json:"completed_at,omitempty"`
}

func (o LifecycleOperation) Validate() error {
	if o.ID.Validate() != nil || o.ProjectID.Validate() != nil || o.Action.ValidateOperation() != nil || o.ProjectVersion.Validate() != nil || o.State.Validate() != nil || o.Version.Validate() != nil || !validTimes(o.CreatedAt, o.UpdatedAt) || o.Action == Archive && o.State == OperationCleaning || validateReason(string(o.State), o.SafeReason) != nil {
		return invalid("", "INVALID_OPERATION")
	}
	if len(o.RequiredParticipants) == 0 || o.CompletedParticipants == nil || o.PendingResources == nil || len(o.PendingResources) > 100 || o.PendingRefsTruncated && len(o.PendingResources) != 100 {
		return invalid("", "INVALID_PROGRESS")
	}
	required := map[ParticipantName]bool{}
	for _, name := range o.RequiredParticipants {
		if name.Validate() != nil || required[name] {
			return invalid("", "INVALID_PROGRESS")
		}
		required[name] = true
	}
	completed := map[ParticipantName]bool{}
	for _, name := range o.CompletedParticipants {
		if !required[name] || completed[name] {
			return invalid("", "INVALID_PROGRESS")
		}
		completed[name] = true
	}
	if err := validateRefs(o.PendingResources, ""); err != nil {
		return err
	}
	for _, ref := range o.PendingResources {
		if !required[ref.Participant] || completed[ref.Participant] {
			return invalid("", "INVALID_PROGRESS")
		}
	}
	if o.State == OperationCompleted {
		if o.CompletedAt == nil || o.CompletedAt.Validate() != nil || o.CompletedAt.Time().Before(o.CreatedAt.Time()) || o.CompletedAt.Time().After(o.UpdatedAt.Time()) || len(completed) != len(required) || len(o.PendingResources) != 0 || o.PendingRefsTruncated {
			return invalid("", "INVALID_PROGRESS")
		}
		if o.Action == Archive && (o.CompletedProjectVersion == nil || o.CompletedProjectVersion.Validate() != nil || *o.CompletedProjectVersion <= o.ProjectVersion || *o.CompletedProjectVersion-o.ProjectVersion != 1) || o.Action == Delete && o.CompletedProjectVersion != nil {
			return invalid("", "INVALID_PROGRESS")
		}
	} else if o.CompletedAt != nil || o.CompletedProjectVersion != nil {
		return invalid("", "INVALID_PROGRESS")
	}
	return nil
}
func (o LifecycleOperation) MarshalJSON() ([]byte, error) {
	type wire LifecycleOperation
	return checkedJSON(wire(o), o.Validate())
}
func (o *LifecycleOperation) UnmarshalJSON(raw []byte) error {
	type wire LifecycleOperation
	v, err := decodeFields[wire](raw, []string{"id", "project_id", "action", "project_version", "state", "version", "required_participants", "completed_participants", "pending_resources", "pending_refs_truncated", "created_at", "updated_at"}, []string{"completed_project_version", "safe_reason", "completed_at"}, nil)
	if err != nil {
		return err
	}
	value := LifecycleOperation(v)
	if err = value.Validate(); err == nil {
		*o = value
	}
	return err
}

type LifecycleResult struct {
	Operation *LifecycleOperation     `json:"operation,omitempty"`
	Receipt   *ProjectDeletionReceipt `json:"receipt,omitempty"`
}

func (r LifecycleResult) Validate() error {
	if r.Operation != nil && r.Receipt == nil {
		// Completed deletion has removed the operation's canonical row. Public
		// responses can retain only the minimal deletion receipt, not this DTO.
		if r.Operation.Action == Delete && r.Operation.State == OperationCompleted {
			return invalid("", "DELETION_RECEIPT_REQUIRED")
		}
		return r.Operation.Validate()
	}
	if r.Operation == nil && r.Receipt != nil {
		return r.Receipt.Validate()
	}
	return invalid("", "INVALID_RESULT")
}
func (r LifecycleResult) MarshalJSON() ([]byte, error) {
	type wire LifecycleResult
	return checkedJSON(wire(r), r.Validate())
}
func (r *LifecycleResult) UnmarshalJSON(raw []byte) error {
	type wire LifecycleResult
	v, err := decodeFields[wire](raw, nil, []string{"operation", "receipt"}, nil)
	if err != nil {
		return err
	}
	value := LifecycleResult(v)
	if err = value.Validate(); err == nil {
		*r = value
	}
	return err
}

type StopState string

const (
	Stopped     StopState = "stopped"
	StopPending StopState = "pending"
	StopFailed  StopState = "failed"
)

func (s StopState) Validate() error              { return oneOf(s, Stopped, StopPending, StopFailed) }
func (s StopState) MarshalJSON() ([]byte, error) { return enumJSON(s, s.Validate()) }
func (s *StopState) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, StopState.Validate)
	if err == nil {
		*s = v
	}
	return err
}

type CleanupState string

const (
	CleanupCompleted CleanupState = "completed"
	CleanupPending   CleanupState = "pending"
	CleanupFailed    CleanupState = "failed"
)

func (s CleanupState) Validate() error {
	return oneOf(s, CleanupCompleted, CleanupPending, CleanupFailed)
}
func (s CleanupState) MarshalJSON() ([]byte, error) { return enumJSON(s, s.Validate()) }
func (s *CleanupState) UnmarshalJSON(raw []byte) error {
	v, err := decodeEnum(raw, CleanupState.Validate)
	if err == nil {
		*s = v
	}
	return err
}

type StopDetails struct {
	State                   StopState
	ActiveRefs, UnknownRefs []PendingRef
	SafeReason              SafeReason
}
type StopReport struct{ data func() stopData }
type stopData struct {
	participant ParticipantName
	cause       LifecycleCause
	scope       ScopeRef
	details     StopDetails
}

func NewStopReport(participant ParticipantName, cause LifecycleCause, scope ScopeRef, d StopDetails) (StopReport, error) {
	if participant.Validate() != nil || cause.Validate() != nil || scope.Validate() != nil || d.State.Validate() != nil || validateReason(string(d.State), d.SafeReason) != nil || validateRefs(d.ActiveRefs, participant) != nil || validateRefs(d.UnknownRefs, participant) != nil || d.State == Stopped && (len(d.ActiveRefs) > 0 || len(d.UnknownRefs) > 0) {
		return StopReport{}, invalid("", "INVALID_STOP_REPORT")
	}
	if validateRefs(append(append([]PendingRef{}, d.ActiveRefs...), d.UnknownRefs...), participant) != nil {
		return StopReport{}, invalid("", "INVALID_STOP_REPORT")
	}
	d.ActiveRefs = append([]PendingRef{}, d.ActiveRefs...)
	d.UnknownRefs = append([]PendingRef{}, d.UnknownRefs...)
	data := stopData{participant, cause, cloneScope(scope), d}
	return StopReport{func() stopData { return data }}, nil
}
func (r StopReport) Matches(participant ParticipantName, cause LifecycleCause, scope ScopeRef) bool {
	return r.data != nil && r.data().participant == participant && r.data().cause == cause && r.data().scope.Equal(scope)
}
func (r StopReport) Details() StopDetails {
	if r.data == nil {
		return StopDetails{}
	}
	d := r.data().details
	d.ActiveRefs = append([]PendingRef{}, d.ActiveRefs...)
	d.UnknownRefs = append([]PendingRef{}, d.UnknownRefs...)
	return d
}
func (StopReport) MarshalJSON() ([]byte, error) { return []byte(`"stop_report"`), nil }
func (*StopReport) UnmarshalJSON([]byte) error  { return invalid("", "TRUSTED_REPORT_REQUIRED") }
func (StopReport) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "stop_report") }
func (StopReport) LogValue() slog.Value         { return slog.StringValue("stop_report") }

// Checkpoint is provider-owned versioned bytes, bound to an exact request.
// A checkpoint is progress, never permission to skip canonical reinspection.
type CleanupCheckpoint struct{ data func() checkpointData }
type checkpointData struct {
	participant ParticipantName
	cause       LifecycleCause
	scope       ScopeRef
	schema      foundation.Version
	bytes       []byte
}

func NewCleanupCheckpoint(participant ParticipantName, cause LifecycleCause, scope ScopeRef, schema foundation.Version, raw []byte) (CleanupCheckpoint, error) {
	if participant.Validate() != nil || cause.Validate() != nil || cause.Action != Delete || scope.Validate() != nil || schema.Validate() != nil || len(raw) == 0 || len(raw) > event.MaxPayloadBytes {
		return CleanupCheckpoint{}, invalid("", "INVALID_CHECKPOINT")
	}
	d := checkpointData{participant, cause, cloneScope(scope), schema, append([]byte(nil), raw...)}
	return CleanupCheckpoint{func() checkpointData { return d }}, nil
}
func (c CleanupCheckpoint) Matches(participant ParticipantName, cause LifecycleCause, scope ScopeRef) bool {
	return c.data != nil && c.data().participant == participant && c.data().cause == cause && c.data().scope.Equal(scope)
}
func (c CleanupCheckpoint) Schema() foundation.Version {
	if c.data == nil {
		return 0
	}
	return c.data().schema
}
func (c CleanupCheckpoint) Bytes() []byte {
	if c.data == nil {
		return nil
	}
	return append([]byte(nil), c.data().bytes...)
}
func (CleanupCheckpoint) MarshalJSON() ([]byte, error) { return []byte(`"cleanup_checkpoint"`), nil }
func (*CleanupCheckpoint) UnmarshalJSON([]byte) error {
	return invalid("", "TRUSTED_CHECKPOINT_REQUIRED")
}
func (CleanupCheckpoint) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "cleanup_checkpoint") }
func (CleanupCheckpoint) LogValue() slog.Value       { return slog.StringValue("cleanup_checkpoint") }

type CleanupDetails struct {
	State         CleanupState
	Checkpoint    *CleanupCheckpoint
	RemainingRefs []PendingRef
	SafeReason    SafeReason
}
type CleanupReport struct{ data func() cleanupData }
type cleanupData struct {
	participant ParticipantName
	cause       LifecycleCause
	scope       ScopeRef
	details     CleanupDetails
}

func NewCleanupReport(participant ParticipantName, cause LifecycleCause, scope ScopeRef, d CleanupDetails) (CleanupReport, error) {
	if participant.Validate() != nil || cause.Validate() != nil || cause.Action != Delete || scope.Validate() != nil || d.State.Validate() != nil || validateReason(string(d.State), d.SafeReason) != nil || validateRefs(d.RemainingRefs, participant) != nil || d.Checkpoint != nil && !d.Checkpoint.Matches(participant, cause, scope) || d.State == CleanupCompleted && len(d.RemainingRefs) > 0 {
		return CleanupReport{}, invalid("", "INVALID_CLEANUP_REPORT")
	}
	d = cloneCleanupDetails(d)
	data := cleanupData{participant, cause, cloneScope(scope), d}
	return CleanupReport{func() cleanupData { return data }}, nil
}
func cloneCleanupDetails(d CleanupDetails) CleanupDetails {
	d.RemainingRefs = append([]PendingRef{}, d.RemainingRefs...)
	if d.Checkpoint != nil {
		value := *d.Checkpoint
		d.Checkpoint = &value
	}
	return d
}
func (r CleanupReport) Matches(participant ParticipantName, cause LifecycleCause, scope ScopeRef) bool {
	return r.data != nil && r.data().participant == participant && r.data().cause == cause && r.data().scope.Equal(scope)
}
func (r CleanupReport) Details() CleanupDetails {
	if r.data == nil {
		return CleanupDetails{}
	}
	return cloneCleanupDetails(r.data().details)
}
func (CleanupReport) MarshalJSON() ([]byte, error) { return []byte(`"cleanup_report"`), nil }
func (*CleanupReport) UnmarshalJSON([]byte) error  { return invalid("", "TRUSTED_REPORT_REQUIRED") }
func (CleanupReport) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "cleanup_report") }
func (CleanupReport) LogValue() slog.Value         { return slog.StringValue("cleanup_report") }

// Participant implementations must validate the current exact service cause
// through ProjectAuthority. Reports are typed facts, not authorization proofs;
// stopped means actual join/death and transaction termination, not cancellation.
type ProjectLifecycleParticipant interface {
	Name() ParticipantName
	RequestStop(context.Context, identity.Actor, LifecycleCause, ScopeRef) (StopReport, error)
	InspectStop(context.Context, identity.Actor, LifecycleCause, ScopeRef) (StopReport, error)
	Cleanup(context.Context, identity.Actor, LifecycleCause, ScopeRef, *CleanupCheckpoint) (CleanupReport, error)
}
type ProjectAuthority interface {
	AuthorizeProject(context.Context, foundation.Tx, identity.Actor, ProjectID, identity.AccessIntent) (identity.AccessGrant, error)
	RequireOwnerInTx(context.Context, foundation.Tx, identity.Actor, ProjectID, identity.AccessIntent) (ProjectAccess, error)
	ResolveProjectPath(context.Context, identity.Actor, string, string) (ProjectRef, error)
	ValidateLifecycleInTx(context.Context, foundation.Tx, identity.Actor, LifecycleCause, ParticipantName, OperationPhase) error
	ValidateInitializationInTx(context.Context, foundation.Tx, identity.Actor, CreationID, ProjectID, foundation.IdempotencyKey) error
}
