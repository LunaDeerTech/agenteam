package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

const (
	SkillInstallStableKey = "builtin:install-skill"
	SkillInstallHandlerID = "skill.install"
	SkillInstallScopeID   = tc.ScopeResolverID("skill.install.project.v1")
	SkillInstallRiskID    = tc.RiskClassifierID("skill.install.publish.v1")
)

// SkillInstallService is the already specified domain call shape. An
// implementation or nonnil interface alone does not prove runtime binding.
type SkillInstallService interface {
	Install(context.Context, id.Actor, f.CommandMeta, id.ProjectID, skill.InstallRequest) (skill.InstallReceipt, error)
}

// SkillInstallCall is immutable call material supplied for a Runtime-owned
// Operation. Construction validates values only: it neither persists the
// Operation nor asserts that its SkillID, spec or actor was authorized.
type SkillInstallCall struct{ data func() skillInstallCallData }

type skillInstallCallData struct {
	actor   id.Actor
	details SkillInstallCallDetails
	request skill.InstallRequest
}

type SkillInstallCallDetails struct {
	ProjectID      id.ProjectID
	AgentID        id.AgentID
	ExecutionID    id.ExecutionID
	OperationID    string
	Spec           tc.SpecRef
	Binding        tc.BuiltinBinding
	SkillID        sc.SkillID
	Key            f.IdempotencyKey
	NormalizedName string
	PackageSHA256  f.Digest
	ManifestSHA256 f.Digest
}

// SkillInstallScope is a current-scope projection, never a reusable grant.
// Unlike CallDetails it intentionally excludes the command key.
type SkillInstallScope struct {
	ProjectID      id.ProjectID   `json:"project_id"`
	AgentID        id.AgentID     `json:"agent_id"`
	ExecutionID    id.ExecutionID `json:"execution_id"`
	OperationID    string         `json:"operation_id"`
	Action         string         `json:"action"`
	SkillID        sc.SkillID     `json:"skill_id"`
	NormalizedName string         `json:"normalized_name"`
	PackageSHA256  f.Digest       `json:"package_sha256"`
	ManifestSHA256 f.Digest       `json:"manifest_sha256"`
}

// NewSkillInstallCall must receive the original Runtime-persisted SkillID.
// There is deliberately no ID allocator, operation store or authority issuer
// in this package. Runtime must verify these projections against its own facts.
func NewSkillInstallCall(ctx context.Context, actor id.Actor, spec tc.SpecRef, operationID string, persistedSkillID sc.SkillID, pkg SkillInstallPackage) (SkillInstallCall, error) {
	if err := skillInstallContext(ctx); err != nil {
		return SkillInstallCall{}, err
	}
	if actor.Validate() != nil || spec.Validate() != nil || persistedSkillID.Validate() != nil || pkg.Validate() != nil {
		return SkillInstallCall{}, skillInstallInvalid()
	}
	a := actor.Details()
	if a.Kind != id.AgentRun {
		return SkillInstallCall{}, f.NewFault(f.Forbidden, f.NotStarted)
	}
	project, err := f.ParseID[id.Project](a.ProjectID)
	if err != nil {
		return SkillInstallCall{}, skillInstallInvalid()
	}
	agent, err := f.ParseID[id.Agent](a.AgentID)
	if err != nil {
		return SkillInstallCall{}, skillInstallInvalid()
	}
	execution, err := f.ParseID[id.Execution](a.ExecutionID)
	if err != nil {
		return SkillInstallCall{}, skillInstallInvalid()
	}
	key, err := SkillInstallKey(operationID)
	if err != nil {
		return SkillInstallCall{}, err
	}
	p, _ := pkg.Package()
	request, err := skill.NewInstallRequest(ctx, persistedSkillID, p)
	if err != nil {
		return SkillInstallCall{}, err
	}
	facts, _ := pkg.Facts()
	d := skillInstallCallData{
		actor:   actor,
		request: request,
		details: SkillInstallCallDetails{
			ProjectID: project, AgentID: agent, ExecutionID: execution,
			OperationID: operationID, Spec: spec,
			Binding: tc.BuiltinBinding{HandlerID: SkillInstallHandlerID, ContractRevision: 1},
			SkillID: persistedSkillID, Key: key, NormalizedName: facts.NormalizedName,
			PackageSHA256: facts.PackageSHA256, ManifestSHA256: facts.ManifestSHA256,
		},
	}
	if err = ctx.Err(); err != nil {
		return SkillInstallCall{}, err
	}
	return SkillInstallCall{data: func() skillInstallCallData { return d }}, nil
}

// SkillInstallKey derives a scalar key; it does not establish that an Operation
// exists. Until Tool Runtime owns a canonical typed OperationID, the D01 UUID
// projection is checked without introducing a competing identity marker.
func SkillInstallKey(operationID string) (f.IdempotencyKey, error) {
	if _, err := f.ParseID[struct{}](operationID); err != nil {
		return "", skillInstallInvalid()
	}
	key := f.IdempotencyKey("tool.skill.install:" + operationID)
	if key.Validate() != nil {
		return "", skillInstallInvalid()
	}
	return key, nil
}

func (c SkillInstallCall) Validate() error {
	if c.data == nil {
		return skillInstallInvalid()
	}
	return c.data().request.Validate()
}

func (c SkillInstallCall) Details() (SkillInstallCallDetails, error) {
	if err := c.Validate(); err != nil {
		return SkillInstallCallDetails{}, err
	}
	return c.data().details, nil
}

func (c SkillInstallCall) CurrentScope(ctx context.Context) (SkillInstallScope, error) {
	if err := skillInstallContext(ctx); err != nil {
		return SkillInstallScope{}, err
	}
	d, err := c.Details()
	if err != nil {
		return SkillInstallScope{}, err
	}
	return SkillInstallScope{d.ProjectID, d.AgentID, d.ExecutionID, d.OperationID,
		"skill.install.create", d.SkillID, d.NormalizedName, d.PackageSHA256, d.ManifestSHA256}, nil
}

func (SkillInstallScope) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "skill_install_scope") }
func (SkillInstallScope) LogValue() slog.Value       { return slog.StringValue("skill_install_scope") }

// Risks is only this exact publish-only v1 classifier. An empty list grants no
// Capability, Policy, Project, Execution or Service permission.
func (c SkillInstallCall) Risks(ctx context.Context) ([]string, error) {
	if err := skillInstallContext(ctx); err != nil {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return []string{}, nil
}

// ReusableScopeSupported is false for the defined v1; Runtime handles any
// required one-time approval for the original Operation/fingerprint.
func (c SkillInstallCall) ReusableScopeSupported() bool { return false }

func (SkillInstallCall) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "skill_install_call") }
func (SkillInstallCall) LogValue() slog.Value       { return slog.StringValue("skill_install_call") }
func (SkillInstallCall) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_install_call"`), nil
}
func (*SkillInstallCall) UnmarshalJSON([]byte) error { return skillInstallInvalid() }
func (SkillInstallCallDetails) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "skill_install_call_details")
}
func (SkillInstallCallDetails) LogValue() slog.Value {
	return slog.StringValue("skill_install_call_details")
}
func (SkillInstallCallDetails) MarshalJSON() ([]byte, error) {
	return []byte(`"skill_install_call_details"`), nil
}

// ErrSkillInstallReceipt is safe for the future Tool adapter to map to
// backend_contract_violation. It makes no assertion about commit outcome.
var ErrSkillInstallReceipt = errors.New("skill_install_receipt_mismatch")

// ProjectSkillInstallReceipt validates and projects a domain receipt. It does
// not prove publication/commit; only the actual Service can supply that fact.
func ProjectSkillInstallReceipt(ctx context.Context, call SkillInstallCall, receipt skill.InstallReceipt) (json.RawMessage, error) {
	if err := skillInstallContext(ctx); err != nil {
		return nil, err
	}
	if err := call.Validate(); err != nil {
		return nil, err
	}
	want := call.data().details
	if receipt.Validate() != nil || receipt.ProjectID != want.ProjectID || receipt.SkillID != want.SkillID || receipt.PackageSHA256 != want.PackageSHA256 {
		return nil, ErrSkillInstallReceipt
	}
	raw, err := json.Marshal(struct {
		SkillID  sc.SkillID `json:"skill_id"`
		Revision f.Revision `json:"revision"`
		Version  f.Version  `json:"version"`
	}{receipt.SkillID, receipt.Revision, receipt.Version})
	if err != nil || len(raw) > 256 {
		return nil, ErrSkillInstallReceipt
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}
