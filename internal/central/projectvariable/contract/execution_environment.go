package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const MaxExecutionEnvironmentBytes = 1 << 20

type EnvironmentCaptureRequest struct {
	ProjectID   i.ProjectID
	AgentID     i.AgentID
	ExecutionID i.ExecutionID
}

func (r EnvironmentCaptureRequest) Validate() error {
	if r.ProjectID.Validate() != nil || r.AgentID.Validate() != nil || r.ExecutionID.Validate() != nil {
		return invalid("", "INVALID_ENVIRONMENT_CAPTURE")
	}
	return nil
}
func (r EnvironmentCaptureRequest) RequiredLocks() []f.LockRequest {
	if r.Validate() != nil {
		return nil
	}
	p, _ := f.ProjectLock(r.ProjectID.String())
	s, _ := f.ProjectScheduleLock(r.ProjectID.String())
	a, _ := f.AgentLock(r.AgentID.String())
	e, _ := f.AggregateLock(f.ExecutionAggregate, r.ExecutionID.String())
	return []f.LockRequest{{Key: p, Mode: f.Shared}, {Key: s, Mode: f.Exclusive}, {Key: a, Mode: f.Shared}, {Key: e, Mode: f.Exclusive}}
}

type EnvironmentDiscoveryFacts struct {
	Project          pc.ProjectRef
	AttemptBinding   f.Digest
	ScopeConstraints []json.RawMessage
}

func (v EnvironmentDiscoveryFacts) Validate(r EnvironmentCaptureRequest) error {
	if r.Validate() != nil || v.Project.Validate() != nil || v.Project.ID != r.ProjectID || v.Project.Lifecycle != pc.Active || v.AttemptBinding.Validate() != nil || v.ScopeConstraints == nil {
		return invalid("", "INVALID_ENVIRONMENT_FACTS")
	}
	if len(v.ScopeConstraints) != 0 {
		return f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	return nil
}
func (v EnvironmentDiscoveryFacts) Clone() EnvironmentDiscoveryFacts {
	v.Project = v.Project.Clone()
	v.ScopeConstraints = slices.Clone(v.ScopeConstraints)
	for n := range v.ScopeConstraints {
		v.ScopeConstraints[n] = slices.Clone(v.ScopeConstraints[n])
	}
	return v
}

type EnvironmentCaptureFacts struct {
	EnvironmentDiscoveryFacts
	AgentVersion             f.Version
	AllowedSecretVariableIDs []i.ProjectVariableID
}

func (v EnvironmentCaptureFacts) Validate(r EnvironmentCaptureRequest) error {
	if err := v.EnvironmentDiscoveryFacts.Validate(r); err != nil {
		return err
	}
	if v.AgentVersion.Validate() != nil || v.AllowedSecretVariableIDs == nil || len(v.AllowedSecretVariableIDs) > MaxSecretReferences {
		return invalid("", "INVALID_ENVIRONMENT_FACTS")
	}
	for n, id := range v.AllowedSecretVariableIDs {
		if id.Validate() != nil || n > 0 && v.AllowedSecretVariableIDs[n-1].String() >= id.String() {
			return invalid("", "INVALID_ENVIRONMENT_FACTS")
		}
	}
	return nil
}
func (v EnvironmentCaptureFacts) Clone() EnvironmentCaptureFacts {
	v.EnvironmentDiscoveryFacts = v.EnvironmentDiscoveryFacts.Clone()
	v.AllowedSecretVariableIDs = slices.Clone(v.AllowedSecretVariableIDs)
	return v
}

// Execution alone proves its actual preparing call/claim/fence/process. Final
// facts require its original live Tx after Trigger and Agent capture. Identity
// projections, a Human session, or a DTO cannot replace either private proof.
type ExecutionEnvironmentAuthority interface {
	RequireEnvironmentDiscoveryInTx(context.Context, f.Tx, EnvironmentCaptureRequest) (EnvironmentDiscoveryFacts, error)
	RequireEnvironmentCaptureInTx(context.Context, f.Tx, EnvironmentCaptureRequest) (EnvironmentCaptureFacts, error)
}
type EnvironmentCapturePlan interface{ RequiredLocks() []f.LockRequest }
type ExecutionEnvironment interface {
	DiscoverExecutionEnvironment(context.Context, EnvironmentCaptureRequest) (EnvironmentCapturePlan, error)
	ResolveExecutionEnvironmentInTx(context.Context, f.Tx, EnvironmentCaptureRequest, EnvironmentCapturePlan) (EnvironmentCapture, error)
}

// Secret metadata has no value field. Stable ref/lease IDs are internal capture
// references, not model-visible credentials or permission to read them. Future
// authorized process launches resolve the then-current Secret value separately.
type ExecutionSecretVariable struct {
	Variable      SecretVariable
	CredentialRef sc.CredentialRef
	LeaseID       sc.LeaseID
}
type EnvironmentCaptureFields struct {
	Request        EnvironmentCaptureRequest
	AttemptBinding f.Digest
	AgentVersion   f.Version
	Variables      []Variable
	Secrets        []ExecutionSecretVariable
}
type EnvironmentCapture struct {
	data func() EnvironmentCaptureFields
}

func cloneEnvironmentFields(v EnvironmentCaptureFields) EnvironmentCaptureFields {
	v.Variables = slices.Clone(v.Variables)
	v.Secrets = slices.Clone(v.Secrets)
	return v
}
func NewEnvironmentCapture(v EnvironmentCaptureFields) (EnvironmentCapture, error) {
	if v.Request.Validate() != nil || v.AttemptBinding.Validate() != nil || v.AgentVersion.Validate() != nil || v.Variables == nil || v.Secrets == nil || len(v.Variables) > MaxVariables || len(v.Secrets) > MaxSecretReferences {
		return EnvironmentCapture{}, invalid("", "INVALID_ENVIRONMENT_CAPTURE")
	}
	names := map[string]bool{}
	ids := map[VariableID]bool{}
	for _, variable := range v.Variables {
		d := variable.Fields()
		if variable.Validate() != nil || d.ProjectID != v.Request.ProjectID || names[d.Name] || ids[d.ID] {
			return EnvironmentCapture{}, invalid("", "INVALID_ENVIRONMENT_CAPTURE")
		}
		names[d.Name] = true
		ids[d.ID] = true
	}
	for _, secret := range v.Secrets {
		d := secret.Variable.Fields()
		if secret.Variable.Validate() != nil || d.ProjectID != v.Request.ProjectID || secret.CredentialRef.Validate() != nil || secret.LeaseID.Validate() != nil || names[d.Name] || ids[d.ID] {
			return EnvironmentCapture{}, invalid("", "INVALID_ENVIRONMENT_CAPTURE")
		}
		scope := secret.CredentialRef.Details().Scope.Details()
		if scope.Kind != i.ProjectScope || scope.ProjectID != v.Request.ProjectID.String() {
			return EnvironmentCapture{}, invalid("", "INVALID_ENVIRONMENT_CAPTURE")
		}
		names[d.Name] = true
		ids[d.ID] = true
	}
	raw, err := json.Marshal(struct {
		Variables []Variable
		Secrets   []ExecutionSecretVariable
	}{v.Variables, v.Secrets})
	if err != nil {
		return EnvironmentCapture{}, invalid("", "INVALID_ENVIRONMENT_CAPTURE")
	}
	if len(raw) > MaxExecutionEnvironmentBytes {
		return EnvironmentCapture{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	v = cloneEnvironmentFields(v)
	return EnvironmentCapture{data: func() EnvironmentCaptureFields { return cloneEnvironmentFields(v) }}, nil
}
func (v EnvironmentCapture) Validate() error {
	if v.data == nil {
		return invalid("", "INVALID_ENVIRONMENT_CAPTURE")
	}
	_, err := NewEnvironmentCapture(v.data())
	return err
}
func (v EnvironmentCapture) Fields() EnvironmentCaptureFields {
	if v.data == nil {
		return EnvironmentCaptureFields{}
	}
	return v.data()
}
func (v EnvironmentCapture) Clone() EnvironmentCapture {
	if v.data == nil {
		return EnvironmentCapture{}
	}
	d := cloneEnvironmentFields(v.data())
	return EnvironmentCapture{data: func() EnvironmentCaptureFields { return cloneEnvironmentFields(d) }}
}
func (EnvironmentCapture) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_environment_capture")
}
func (EnvironmentCapture) LogValue() slog.Value {
	return slog.StringValue("execution_environment_capture")
}
func (EnvironmentCapture) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_environment_capture"`), nil
}
