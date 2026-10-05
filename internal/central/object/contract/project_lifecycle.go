package contract

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

type ProjectStopOperation struct{}
type ProjectStopOperationID = foundation.ID[ProjectStopOperation]
type ProjectStopAction string

const (
	ProjectStopArchive ProjectStopAction = "archive"
	ProjectStopDelete  ProjectStopAction = "delete"
)

func (v ProjectStopAction) Validate() error {
	return projectStopOneOf(v, ProjectStopArchive, ProjectStopDelete)
}
func (v ProjectStopAction) MarshalJSON() ([]byte, error) {
	return projectStopEnumJSON(v, v.Validate())
}
func (v *ProjectStopAction) UnmarshalJSON(raw []byte) error {
	return projectStopDecodeEnum(raw, v, ProjectStopAction.Validate)
}

type ProjectStopCauseDetails struct {
	ProjectID      identity.ProjectID
	OperationID    ProjectStopOperationID
	Action         ProjectStopAction
	ProjectVersion foundation.Version
}

// ProjectStopCause names a durable lifecycle cause, not permission to act on it.
type ProjectStopCause struct {
	data func() ProjectStopCauseDetails
}

func NewProjectStopCause(d ProjectStopCauseDetails) (ProjectStopCause, error) {
	if d.ProjectID.Validate() != nil || d.OperationID.Validate() != nil || d.Action.Validate() != nil || d.ProjectVersion.Validate() != nil {
		return ProjectStopCause{}, bad()
	}
	return ProjectStopCause{func() ProjectStopCauseDetails { return d }}, nil
}
func (c ProjectStopCause) Validate() error {
	if c.data == nil {
		return bad()
	}
	return nil
}
func (c ProjectStopCause) Details() ProjectStopCauseDetails {
	if c.data == nil {
		return ProjectStopCauseDetails{}
	}
	return c.data()
}
func (c ProjectStopCause) Equal(other ProjectStopCause) bool {
	return c.data != nil && other.data != nil && c.data() == other.data()
}
func (ProjectStopCause) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_project_stop_cause")
}
func (ProjectStopCause) MarshalJSON() ([]byte, error) {
	return []byte(`"object_project_stop_cause"`), nil
}
func (*ProjectStopCause) UnmarshalJSON([]byte) error { return bad() }
func (ProjectStopCause) LogValue() slog.Value {
	return slog.StringValue("object_project_stop_cause")
}

type ProjectStopComponent string

const (
	ObjectStopComponent   ProjectStopComponent = "object"
	ArtifactStopComponent ProjectStopComponent = "artifact"
)

func (v ProjectStopComponent) Validate() error {
	return projectStopOneOf(v, ObjectStopComponent, ArtifactStopComponent)
}
func (v ProjectStopComponent) MarshalJSON() ([]byte, error) {
	return projectStopEnumJSON(v, v.Validate())
}
func (v *ProjectStopComponent) UnmarshalJSON(raw []byte) error {
	return projectStopDecodeEnum(raw, v, ProjectStopComponent.Validate)
}

type ProjectStopStep string

const (
	RequestProjectStopStep ProjectStopStep = "request_stop"
	InspectProjectStopStep ProjectStopStep = "inspect_stop"
)

func (v ProjectStopStep) Validate() error {
	return projectStopOneOf(v, RequestProjectStopStep, InspectProjectStopStep)
}
func (v ProjectStopStep) MarshalJSON() ([]byte, error) {
	return projectStopEnumJSON(v, v.Validate())
}
func (v *ProjectStopStep) UnmarshalJSON(raw []byte) error {
	return projectStopDecodeEnum(raw, v, ProjectStopStep.Validate)
}

type ProjectStopRequestDetails struct {
	Actor     identity.Actor
	Cause     ProjectStopCause
	Component ProjectStopComponent
	Step      ProjectStopStep
}

// ProjectStopRequest is immutable discovery input. A registered service name
// and a well-formed cause never replace current same-transaction authorization.
type ProjectStopRequest struct {
	data func() ProjectStopRequestDetails
}

func NewProjectStopRequest(d ProjectStopRequestDetails) (ProjectStopRequest, error) {
	if d.Actor.Validate() != nil || d.Cause.Validate() != nil || d.Component.Validate() != nil || d.Step.Validate() != nil {
		return ProjectStopRequest{}, bad()
	}
	a, c := d.Actor.Details(), d.Cause.Details()
	if a.Kind != identity.Service || a.ServiceName != identity.ProjectLifecycle || a.ProjectID != c.ProjectID.String() || a.CauseRef != c.OperationID.String() {
		return ProjectStopRequest{}, bad()
	}
	return ProjectStopRequest{func() ProjectStopRequestDetails { return d }}, nil
}
func (r ProjectStopRequest) Validate() error {
	if r.data == nil {
		return bad()
	}
	return nil
}
func (r ProjectStopRequest) Details() ProjectStopRequestDetails {
	if r.data == nil {
		return ProjectStopRequestDetails{}
	}
	return r.data()
}
func (r ProjectStopRequest) Equal(other ProjectStopRequest) bool {
	if r.data == nil || other.data == nil {
		return false
	}
	a, b := r.data(), other.data()
	return a.Actor.Equal(b.Actor) && a.Cause.Equal(b.Cause) && a.Component == b.Component && a.Step == b.Step
}

// ProjectStopBinding hashes explicit primitive fields, never the safe logging
// projections of Actor, Cause or Request. It describes a request, not a grant.
func ProjectStopBinding(r ProjectStopRequest) (foundation.Digest, error) {
	if r.Validate() != nil {
		return "", bad()
	}
	d := r.data()
	c := d.Cause.Details()
	canonical := struct {
		Actor              identity.ActorDetails
		Project, Operation string
		Action             string
		Version            foundation.Version
		Component, Step    string
	}{d.Actor.Details(), c.ProjectID.String(), c.OperationID.String(), string(c.Action), c.ProjectVersion, string(d.Component), string(d.Step)}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", bad()
	}
	sum := sha256.Sum256(append([]byte("object.project-stop.request.v1\n"), raw...))
	return foundation.Digest("sha256:" + hex.EncodeToString(sum[:])), nil
}
func (ProjectStopRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_project_stop_request")
}
func (ProjectStopRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"object_project_stop_request"`), nil
}
func (*ProjectStopRequest) UnmarshalJSON([]byte) error { return bad() }
func (ProjectStopRequest) LogValue() slog.Value {
	return slog.StringValue("object_project_stop_request")
}

type ProjectStopAuthorizationMode string

const (
	ContinueProjectStop ProjectStopAuthorizationMode = "continue"
	ReadProjectStop     ProjectStopAuthorizationMode = "read_only"
)

func (v ProjectStopAuthorizationMode) Validate() error {
	return projectStopOneOf(v, ContinueProjectStop, ReadProjectStop)
}
func (v ProjectStopAuthorizationMode) MarshalJSON() ([]byte, error) {
	return projectStopEnumJSON(v, v.Validate())
}
func (v *ProjectStopAuthorizationMode) UnmarshalJSON(raw []byte) error {
	return projectStopDecodeEnum(raw, v, ProjectStopAuthorizationMode.Validate)
}

type projectStopAuthorizationData struct {
	tx           foundation.Tx
	request      ProjectStopRequest
	dependencies AccessDependencies
	mode         ProjectStopAuthorizationMode
}

// ProjectStopAuthorization is returned only by the configured current Project
// authority. It is callback-local; read_only never permits cancellation, a new
// stop gate or a new phase, including when a minimal delete receipt is present.
type ProjectStopAuthorization struct {
	data func() projectStopAuthorizationData
}

func NewProjectStopAuthorization(tx foundation.Tx, r ProjectStopRequest, deps AccessDependencies, mode ProjectStopAuthorizationMode) (ProjectStopAuthorization, error) {
	if !tx.Valid() || r.Validate() != nil || deps.Validate() != nil || mode.Validate() != nil || mode == ReadProjectStop && r.Details().Step != InspectProjectStopStep {
		return ProjectStopAuthorization{}, bad()
	}
	d := projectStopAuthorizationData{tx, r, deps, mode}
	return ProjectStopAuthorization{func() projectStopAuthorizationData { return d }}, nil
}
func (a ProjectStopAuthorization) Validate() error {
	if a.data == nil {
		return bad()
	}
	return nil
}
func (a ProjectStopAuthorization) Mode() ProjectStopAuthorizationMode {
	if a.data == nil {
		return ""
	}
	return a.data().mode
}
func (a ProjectStopAuthorization) Matches(tx foundation.Tx, r ProjectStopRequest, deps AccessDependencies) bool {
	if a.data == nil || !tx.Valid() {
		return false
	}
	d := a.data()
	return d.tx == tx && d.request.Equal(r) && d.dependencies.Equal(deps)
}
func (ProjectStopAuthorization) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_project_stop_authorization")
}
func (ProjectStopAuthorization) MarshalJSON() ([]byte, error) {
	return []byte(`"object_project_stop_authorization"`), nil
}
func (*ProjectStopAuthorization) UnmarshalJSON([]byte) error { return bad() }
func (ProjectStopAuthorization) LogValue() slog.Value {
	return slog.StringValue("object_project_stop_authorization")
}

// Discovery returns ordinary mapping/lock data, not a private issuer or proof.
// The consumer unions these locks with its own discovered resource locks once
// per Tx. Validate recomputes the complete request/current mapping, verifies
// held locks and durable cause/manifest, and consumes the caller's Tx without
// acquiring more locks or opening/committing another transaction. Private D05
// batch plans separately bind their own issuer and exact resources.
type ProjectStopAuthority interface {
	DiscoverProjectStop(context.Context, ProjectStopRequest) (AccessDependencies, error)
	ValidateProjectStopInTx(context.Context, foundation.Tx, ProjectStopRequest, AccessDependencies) (ProjectStopAuthorization, error)
}

type ProjectStopState string

const (
	ProjectStopped     ProjectStopState = "stopped"
	ProjectStopPending ProjectStopState = "pending"
	ProjectStopFailed  ProjectStopState = "failed"
)

func (v ProjectStopState) Validate() error {
	return projectStopOneOf(v, ProjectStopped, ProjectStopPending, ProjectStopFailed)
}
func (v ProjectStopState) MarshalJSON() ([]byte, error) {
	return projectStopEnumJSON(v, v.Validate())
}
func (v *ProjectStopState) UnmarshalJSON(raw []byte) error {
	return projectStopDecodeEnum(raw, v, ProjectStopState.Validate)
}

type ProjectStopReason string

const (
	ProjectStopDependencyUnbound     ProjectStopReason = "dependency_unbound"
	ProjectStopDependencyUnavailable ProjectStopReason = "dependency_unavailable"
	ProjectStopWorkPending           ProjectStopReason = "work_pending"
	ProjectStopOutcomeUnknown        ProjectStopReason = "outcome_unknown"
	ProjectStopOperationFailed       ProjectStopReason = "operation_failed"
)

func (v ProjectStopReason) Validate() error {
	return projectStopOneOf(v, ProjectStopDependencyUnbound, ProjectStopDependencyUnavailable, ProjectStopWorkPending, ProjectStopOutcomeUnknown, ProjectStopOperationFailed)
}
func (v ProjectStopReason) MarshalJSON() ([]byte, error) {
	return projectStopEnumJSON(v, v.Validate())
}
func (v *ProjectStopReason) UnmarshalJSON(raw []byte) error {
	return projectStopDecodeEnum(raw, v, ProjectStopReason.Validate)
}

type ProjectStopRefKind string

const (
	StopObjectPreparation ProjectStopRefKind = "object_preparation"
	StopObjectUpload      ProjectStopRefKind = "object_upload"
	StopObjectAttempt     ProjectStopRefKind = "object_attempt"
	StopObjectLease       ProjectStopRefKind = "object_lease"
	StopObjectTransfer    ProjectStopRefKind = "object_transfer"
	StopObjectDownload    ProjectStopRefKind = "object_download"
	StopArtifactUpload    ProjectStopRefKind = "artifact_upload"
	StopArtifactCreation  ProjectStopRefKind = "artifact_creation"
)

func (v ProjectStopRefKind) Validate() error {
	return projectStopOneOf(v, StopObjectPreparation, StopObjectUpload, StopObjectAttempt, StopObjectLease, StopObjectTransfer, StopObjectDownload, StopArtifactUpload, StopArtifactCreation)
}
func (v ProjectStopRefKind) MarshalJSON() ([]byte, error) {
	return projectStopEnumJSON(v, v.Validate())
}
func (v *ProjectStopRefKind) UnmarshalJSON(raw []byte) error {
	return projectStopDecodeEnum(raw, v, ProjectStopRefKind.Validate)
}

type ProjectStopResource struct{}
type ProjectStopResourceID = foundation.ID[ProjectStopResource]
type ProjectStopRef struct {
	Kind ProjectStopRefKind
	ID   ProjectStopResourceID
}

func (r ProjectStopRef) Validate() error {
	if r.Kind.Validate() != nil || r.ID.Validate() != nil {
		return bad()
	}
	return nil
}

type ObjectStopDetails struct {
	State                   ProjectStopState
	ActiveRefs, UnknownRefs []ProjectStopRef
	SafeReason              ProjectStopReason
}
type objectStopData struct {
	component ProjectStopComponent
	cause     ProjectStopCause
	details   ObjectStopDetails
}

// ObjectStopReport is a trusted provider projection, never proof of actual
// join by itself. Empty diagnostics do not establish stopped; the provider must
// inspect durable work and exact writer/process terminal evidence completely.
type ObjectStopReport struct{ data func() objectStopData }

func NewObjectStopReport(component ProjectStopComponent, cause ProjectStopCause, d ObjectStopDetails) (ObjectStopReport, error) {
	if component.Validate() != nil || cause.Validate() != nil || d.State.Validate() != nil || d.SafeReason != "" && d.SafeReason.Validate() != nil {
		return ObjectStopReport{}, bad()
	}
	if d.State == ProjectStopped && (len(d.ActiveRefs) != 0 || len(d.UnknownRefs) != 0 || d.SafeReason != "") || d.State == ProjectStopFailed && (d.SafeReason == "" || d.SafeReason == ProjectStopOutcomeUnknown || len(d.UnknownRefs) != 0) {
		return ObjectStopReport{}, bad()
	}
	seen := make(map[ProjectStopRef]bool, len(d.ActiveRefs)+len(d.UnknownRefs))
	for _, refs := range [][]ProjectStopRef{d.ActiveRefs, d.UnknownRefs} {
		for _, ref := range refs {
			if ref.Validate() != nil || seen[ref] || component == ObjectStopComponent && (ref.Kind == StopArtifactUpload || ref.Kind == StopArtifactCreation) {
				return ObjectStopReport{}, bad()
			}
			seen[ref] = true
		}
	}
	d = copyObjectStopDetails(d)
	data := objectStopData{component, cause, d}
	return ObjectStopReport{func() objectStopData { return data }}, nil
}
func copyObjectStopDetails(d ObjectStopDetails) ObjectStopDetails {
	d.ActiveRefs = append([]ProjectStopRef(nil), d.ActiveRefs...)
	d.UnknownRefs = append([]ProjectStopRef(nil), d.UnknownRefs...)
	return d
}
func (r ObjectStopReport) Validate() error {
	if r.data == nil {
		return bad()
	}
	return nil
}
func (r ObjectStopReport) Matches(component ProjectStopComponent, cause ProjectStopCause) bool {
	return r.data != nil && r.data().component == component && r.data().cause.Equal(cause)
}
func (r ObjectStopReport) Details() ObjectStopDetails {
	if r.data == nil {
		return ObjectStopDetails{}
	}
	return copyObjectStopDetails(r.data().details)
}
func (ObjectStopReport) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "object_project_stop_report")
}
func (ObjectStopReport) MarshalJSON() ([]byte, error) {
	return []byte(`"object_project_stop_report"`), nil
}
func (*ObjectStopReport) UnmarshalJSON([]byte) error { return bad() }
func (ObjectStopReport) LogValue() slog.Value {
	return slog.StringValue("object_project_stop_report")
}

// Request and Inspect own bounded short transactions. They validate the exact
// current lifecycle cause before capturing handles, cancel only after confirmed
// preflight commit, and revalidate before checkpoints. Cancellation/timeout is
// pending until actual join or exact process death plus writer terminal proof.
// Archive preserves published content and legal readers; delete also stops
// readers, sources and transfers. Inspect cannot create a new local stop cause.
type ObjectProjectStop interface {
	RequestProjectStop(context.Context, identity.Actor, ProjectStopCause) (ObjectStopReport, error)
	InspectProjectStop(context.Context, identity.Actor, ProjectStopCause) (ObjectStopReport, error)
}

func projectStopOneOf[T ~string](value T, allowed ...T) error {
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return bad()
}
func projectStopEnumJSON[T ~string](value T, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(string(value))
}
func projectStopDecodeEnum[T ~string](raw []byte, target *T, validate func(T) error) error {
	var text string
	if target == nil || json.Unmarshal(raw, &text) != nil {
		return bad()
	}
	value := T(text)
	if validate(value) != nil {
		return bad()
	}
	*target = value
	return nil
}
