package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"unicode/utf8"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mt "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	pv "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	secret "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	skill "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

const PreparationInputSchemaVersion f.Version = 1
const MaxPreparationInputBytes = 8 << 20
const MaxPlatformPromptBytes = 64 << 10

// PlatformPrompt is an immutable component, not an assembled System Prompt.
// The actual platform producer supplies its versioned text; a validated value
// grants no tool capability or permission to start an Execution.
type PlatformPrompt struct{ data func() (string, string) }

func NewPlatformPrompt(revision, content string) (PlatformPrompt, error) {
	if len(revision) == 0 || len(revision) > 128 || len(content) == 0 || len(content) > MaxPlatformPromptBytes || !utf8.ValidString(content) || strings.TrimSpace(content) == "" || strings.ContainsRune(content, 0) {
		return PlatformPrompt{}, invalid()
	}
	for _, c := range revision {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._/-", c)) {
			return PlatformPrompt{}, invalid()
		}
	}
	return PlatformPrompt{func() (string, string) { return revision, content }}, nil
}
func (p PlatformPrompt) Validate() error {
	if p.data == nil {
		return invalid()
	}
	r, c := p.data()
	_, e := NewPlatformPrompt(r, c)
	return e
}
func (p PlatformPrompt) Revision() string {
	if p.data == nil {
		return ""
	}
	r, _ := p.data()
	return r
}
func (p PlatformPrompt) Content() string {
	if p.data == nil {
		return ""
	}
	_, c := p.data()
	return c
}
func (p PlatformPrompt) Digest() f.Digest {
	if p.data == nil {
		return ""
	}
	return TriggerInputDigest([]byte(p.Content()))
}
func (PlatformPrompt) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "platform_prompt") }
func (PlatformPrompt) LogValue() slog.Value         { return slog.StringValue("platform_prompt") }
func (PlatformPrompt) MarshalJSON() ([]byte, error) { return []byte(`"platform_prompt"`), nil }

// PreparationInputFields is an explicit internal persistence projection. It
// contains ordinary environment values and source text: never log it. These
// values cannot prove their provider calls, locks, claim or transaction. The
// Execution writer must verify that evidence before its one atomic INSERT.
type PreparationInputFields struct {
	Request        PreparationRequest
	AttemptBinding f.Digest
	CapturedAt     f.Instant
	Project        pc.ProjectRef
	Agent          ac.AgentConfig
	Trigger        CapturedTriggerInput
	Model          mc.ResolvedModel
	Tools          []tc.ExecutionTool
	Skills         skill.InitialSkillBindings
	Environment    pv.EnvironmentCapture
	Mounts         mt.ExecutionMountSet
	PlatformPrompt PlatformPrompt
}

func (PreparationInputFields) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_preparation_fields")
}
func (PreparationInputFields) LogValue() slog.Value {
	return slog.StringValue("execution_preparation_fields")
}
func (PreparationInputFields) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_preparation_fields"`), nil
}

func clonePreparationFields(v PreparationInputFields) PreparationInputFields {
	v.Request = v.Request.Clone()
	v.Project = v.Project.Clone()
	v.Agent = v.Agent.Clone()
	v.Model = v.Model.Clone()
	v.Tools = slices.Clone(v.Tools)
	v.Skills = v.Skills.Clone()
	v.Environment = v.Environment.Clone()
	v.Mounts = v.Mounts.Clone()
	return v
}

// PreparationInput is the complete captured input before external Build. It
// is not a sealed Snapshot, a running state, or a bearer capability. Execution
// ID is its unique identity; retries must observe these original bytes.
type PreparationInput struct {
	data func() (PreparationInputFields, []byte, f.Digest)
}

func NewPreparationInput(v PreparationInputFields) (PreparationInput, error) {
	if err := validatePreparationFields(v); err != nil {
		return PreparationInput{}, err
	}
	raw, err := json.Marshal(preparationWireFrom(v))
	if err != nil {
		return PreparationInput{}, invalid()
	}
	if len(raw) > MaxPreparationInputBytes {
		return PreparationInput{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return PreparationInput{}, invalid()
	}
	if len(raw) > MaxPreparationInputBytes {
		return PreparationInput{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	v = clonePreparationFields(v)
	owned := bytes.Clone(raw)
	digest := TriggerInputDigest(owned)
	return PreparationInput{func() (PreparationInputFields, []byte, f.Digest) {
		return clonePreparationFields(v), bytes.Clone(owned), digest
	}}, nil
}
func (v PreparationInput) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v PreparationInput) Fields() PreparationInputFields {
	if v.data == nil {
		return PreparationInputFields{}
	}
	d, _, _ := v.data()
	return d
}
func (v PreparationInput) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	_, b, _ := v.data()
	return b
}
func (v PreparationInput) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	_, _, d := v.data()
	return d
}
func (PreparationInput) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "execution_preparation_input")
}
func (PreparationInput) LogValue() slog.Value { return slog.StringValue("execution_preparation_input") }
func (PreparationInput) MarshalJSON() ([]byte, error) {
	return []byte(`"execution_preparation_input"`), nil
}
func (*PreparationInput) UnmarshalJSON([]byte) error { return invalid() }

func validatePreparationFields(v PreparationInputFields) error {
	if v.Request.Validate() != nil || v.AttemptBinding.Validate() != nil || v.CapturedAt.Validate() != nil || v.CapturedAt.Time().IsZero() || v.Project.Validate() != nil || v.Agent.Validate() != nil || v.Trigger.Validate() != nil || v.Model.Validate() != nil || v.Skills.Validate() != nil || v.Environment.Validate() != nil || v.Mounts.Validate() != nil || v.PlatformPrompt.Validate() != nil || v.Tools == nil {
		return invalid()
	}
	r := v.Request
	a := v.Agent.Fields()
	e := v.Environment.Fields()
	m := v.Model
	if v.Project.ID != r.Launch.ProjectID || v.Project.Lifecycle != pc.Active || a.Core.ProjectID != v.Project.ID || a.Core.ID != r.Launch.AgentID || a.Core.Lifecycle != ac.AgentActive || v.Trigger.Ref().ProviderType != r.Launch.Trigger.Kind {
		return invalid()
	}
	// This first complete capture profile has no AGENTS.md content provider or
	// nonempty Mount runtime-reference provider. Explicit false and the real
	// Mount provider's empty, versioned set are required; nil is never empty.
	if a.Core.InjectAgentsMD || len(a.AllowedMountIDs) != 0 || v.Mounts.Mounts == nil || len(v.Mounts.Mounts) != 0 || len(r.Launch.Policy.AllowedResourceConstraints) != 0 {
		return f.NewFault(f.CapabilityUnsupported, f.NotStarted)
	}
	if v.Mounts.Request.ProjectID != v.Project.ID || v.Mounts.Request.AgentID != a.Core.ID || v.Mounts.Request.ExecutionID != r.ExecutionID || v.Mounts.AgentVersion != a.Core.Version {
		return invalid()
	}
	if v.Skills.Request.ProjectID != v.Project.ID || v.Skills.Request.AgentID != a.Core.ID || v.Skills.Request.ExecutionID != r.ExecutionID || e.Request.ProjectID != v.Project.ID || e.Request.AgentID != a.Core.ID || e.Request.ExecutionID != r.ExecutionID || e.AttemptBinding != v.AttemptBinding || e.AgentVersion != a.Core.Version {
		return invalid()
	}
	if m.Consumer.Kind != mc.AgentConsumer || m.Consumer.Purpose != mc.AgentGeneration || m.Consumer.ProjectID != v.Project.ID || m.Consumer.AgentID == nil || *m.Consumer.AgentID != a.Core.ID || m.Consumer.ExecutionID == nil || *m.Consumer.ExecutionID != r.ExecutionID || m.LeaseOwner.Details().Kind != secret.ExecutionOwner || m.LeaseOwner.Details().ID != r.ExecutionID.String() || m.Snapshot.Identity.ModelID != a.Core.ModelRef {
		return invalid()
	}
	leases := map[secret.LeaseID]bool{}
	if m.CredentialLease != nil {
		leases[m.CredentialLease.LeaseID] = true
		s := m.CredentialLease.CredentialRef.Details().Scope.Details()
		if s.Kind != i.System && (s.Kind != i.ProjectScope || s.ProjectID != v.Project.ID.String()) {
			return invalid()
		}
	}
	actualSecrets := make([]i.ProjectVariableID, 0, len(e.Secrets))
	for _, s := range e.Secrets {
		if leases[s.LeaseID] {
			return invalid()
		}
		leases[s.LeaseID] = true
		actualSecrets = append(actualSecrets, s.Variable.Fields().ID)
	}
	slices.SortFunc(actualSecrets, func(a, b i.ProjectVariableID) int { return strings.Compare(a.String(), b.String()) })
	if !slices.Equal(actualSecrets, a.AllowedSecretVariableIDs) {
		return invalid()
	}
	tools := map[i.ToolID]bool{}
	for n, t := range v.Tools {
		if t.Validate() != nil || tools[t.ToolID] || n > 0 && v.Tools[n-1].ToolID.String() >= t.ToolID.String() || slices.Contains(r.Launch.Policy.DeniedToolIDs, t.ToolID) {
			return invalid()
		}
		tools[t.ToolID] = true
		if t.BindingSnapshot.Class == tc.OrdinaryTool && !slices.Contains(a.AllowedToolIDs, t.ToolID) {
			return invalid()
		}
	}
	for _, id := range a.AllowedToolIDs {
		if !slices.Contains(r.Launch.Policy.DeniedToolIDs, id) && !tools[id] {
			return invalid()
		}
	}
	return nil
}

// Decode accepts only the exact canonical, closed encoding emitted by this
// version. Re-encoding checks every required field and spelling, including
// nested optional fields. Canonical-v1 rejects duplicate keys and invalid
// numbers; exact-byte comparison also rejects malformed scalar replacement.
func DecodePreparationInput(raw []byte) (PreparationInput, error) {
	if len(raw) > MaxPreparationInputBytes {
		return PreparationInput{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	if len(raw) == 0 {
		return PreparationInput{}, invalid()
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || !bytes.Equal(canonical, raw) {
		return PreparationInput{}, invalid()
	}
	var w preparationInputWire
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&w) != nil || w.SchemaVersion != PreparationInputSchemaVersion {
		return PreparationInput{}, invalid()
	}
	fields, err := w.fields()
	if err != nil {
		return PreparationInput{}, invalid()
	}
	v, err := NewPreparationInput(fields)
	if err != nil {
		return PreparationInput{}, err
	}
	if !bytes.Equal(v.CanonicalBytes(), raw) {
		return PreparationInput{}, invalid()
	}
	return v, nil
}

type promptWire struct {
	Revision string   `json:"revision"`
	Content  string   `json:"content"`
	Digest   f.Digest `json:"digest"`
}
type triggerWire struct {
	Ref  TriggerInputRef `json:"ref"`
	Data []byte          `json:"data"`
}
type credentialRefWire struct {
	ID    secret.CredentialID `json:"id"`
	Scope i.ScopeDetails      `json:"scope"`
}
type credentialLeaseWire struct {
	ID  secret.LeaseID    `json:"lease_id"`
	Ref credentialRefWire `json:"credential_ref"`
}

func refWire(r secret.CredentialRef) credentialRefWire {
	return credentialRefWire{r.Details().ID, r.Details().Scope.Details()}
}
func (w credentialRefWire) ref() (secret.CredentialRef, error) {
	var scope i.Scope
	switch w.Scope.Kind {
	case i.System:
		if w.Scope.ProjectID != "" || w.Scope.AgentID != "" {
			return secret.CredentialRef{}, invalid()
		}
		scope = i.SystemScope()
	case i.ProjectScope:
		p, e := f.ParseID[i.Project](w.Scope.ProjectID)
		if e != nil || w.Scope.AgentID != "" {
			return secret.CredentialRef{}, invalid()
		}
		scope, e = i.InProject(p)
		if e != nil {
			return secret.CredentialRef{}, invalid()
		}
	default:
		return secret.CredentialRef{}, invalid()
	}
	return secret.NewCredentialRef(w.ID, scope)
}

type modelSnapshotWire struct {
	ID               mc.SnapshotID      `json:"id"`
	Identity         mc.ModelIdentity   `json:"identity"`
	Endpoint         string             `json:"endpoint"`
	Parameters       json.RawMessage    `json:"parameters"`
	RequestOverwrite json.RawMessage    `json:"request_overwrite"`
	HeaderOverwrite  map[string]string  `json:"header_overwrite"`
	Capabilities     mc.Capabilities    `json:"capabilities"`
	CredentialRef    *credentialRefWire `json:"credential_ref"`
	SelectionVersion *f.Version         `json:"selection_version"`
}
type resolvedModelWire struct {
	Snapshot        modelSnapshotWire    `json:"snapshot"`
	Consumer        mc.Consumer          `json:"consumer"`
	LeaseOwner      secret.OwnerDetails  `json:"lease_owner"`
	CredentialLease *credentialLeaseWire `json:"credential_lease"`
}

func modelWire(m mc.ResolvedModel) resolvedModelWire {
	s := m.Snapshot
	w := resolvedModelWire{Snapshot: modelSnapshotWire{ID: s.ID, Identity: s.Identity, Endpoint: s.Endpoint, Parameters: s.Parameters, RequestOverwrite: s.RequestOverwrite, HeaderOverwrite: s.HeaderOverwrite, Capabilities: s.Capabilities, SelectionVersion: s.SelectionVersion}, Consumer: m.Consumer, LeaseOwner: m.LeaseOwner.Details()}
	if s.CredentialRef != nil {
		r := refWire(*s.CredentialRef)
		w.Snapshot.CredentialRef = &r
	}
	if m.CredentialLease != nil {
		w.CredentialLease = &credentialLeaseWire{m.CredentialLease.LeaseID, refWire(m.CredentialLease.CredentialRef)}
	}
	return w
}
func (w resolvedModelWire) model() (mc.ResolvedModel, error) {
	s := w.Snapshot
	o, e := secret.NewCredentialLeaseOwner(w.LeaseOwner.Kind, w.LeaseOwner.ID)
	if e != nil {
		return mc.ResolvedModel{}, invalid()
	}
	m := mc.ResolvedModel{Snapshot: mc.ConfigSnapshot{ID: s.ID, Identity: s.Identity, Endpoint: s.Endpoint, Parameters: s.Parameters, RequestOverwrite: s.RequestOverwrite, HeaderOverwrite: s.HeaderOverwrite, Capabilities: s.Capabilities, SelectionVersion: s.SelectionVersion}, Consumer: w.Consumer, LeaseOwner: o}
	if s.CredentialRef != nil {
		r, e := s.CredentialRef.ref()
		if e != nil {
			return mc.ResolvedModel{}, invalid()
		}
		m.Snapshot.CredentialRef = &r
	}
	if w.CredentialLease != nil {
		r, e := w.CredentialLease.Ref.ref()
		if e != nil {
			return mc.ResolvedModel{}, invalid()
		}
		m.CredentialLease = &secret.CredentialLease{LeaseID: w.CredentialLease.ID, CredentialRef: r}
	}
	return m, nil
}

type executionSecretWire struct {
	Variable      pv.SecretVariable `json:"variable"`
	CredentialRef credentialRefWire `json:"credential_ref"`
	LeaseID       secret.LeaseID    `json:"lease_id"`
}
type environmentWire struct {
	Request        pv.EnvironmentCaptureRequest `json:"request"`
	AttemptBinding f.Digest                     `json:"attempt_binding"`
	AgentVersion   f.Version                    `json:"agent_version"`
	Variables      []pv.Variable                `json:"variables"`
	Secrets        []executionSecretWire        `json:"secrets"`
}

func envWire(v pv.EnvironmentCapture) environmentWire {
	d := v.Fields()
	w := environmentWire{d.Request, d.AttemptBinding, d.AgentVersion, d.Variables, make([]executionSecretWire, 0, len(d.Secrets))}
	for _, s := range d.Secrets {
		w.Secrets = append(w.Secrets, executionSecretWire{s.Variable, refWire(s.CredentialRef), s.LeaseID})
	}
	return w
}
func (w environmentWire) environment() (pv.EnvironmentCapture, error) {
	d := pv.EnvironmentCaptureFields{Request: w.Request, AttemptBinding: w.AttemptBinding, AgentVersion: w.AgentVersion, Variables: w.Variables, Secrets: make([]pv.ExecutionSecretVariable, 0, len(w.Secrets))}
	for _, s := range w.Secrets {
		r, e := s.CredentialRef.ref()
		if e != nil {
			return pv.EnvironmentCapture{}, invalid()
		}
		d.Secrets = append(d.Secrets, pv.ExecutionSecretVariable{Variable: s.Variable, CredentialRef: r, LeaseID: s.LeaseID})
	}
	if w.Secrets == nil {
		d.Secrets = nil
	}
	return pv.NewEnvironmentCapture(d)
}

type preparationInputWire struct {
	SchemaVersion   f.Version                  `json:"schema_version"`
	ExecutionID     i.ExecutionID              `json:"execution_id"`
	Launch          LaunchRequest              `json:"launch"`
	RequestID       f.ID[f.Request]            `json:"request_id"`
	IdempotencyKey  f.IdempotencyKey           `json:"idempotency_key"`
	CommandIdentity string                     `json:"command_identity"`
	LaunchDigest    f.Digest                   `json:"launch_digest"`
	AttemptBinding  f.Digest                   `json:"attempt_binding"`
	CapturedAt      f.Instant                  `json:"captured_at"`
	Project         pc.ProjectRef              `json:"project"`
	Agent           ac.AgentConfig             `json:"agent"`
	Trigger         triggerWire                `json:"trigger"`
	Model           resolvedModelWire          `json:"model"`
	Tools           []tc.ExecutionTool         `json:"tools"`
	Skills          skill.InitialSkillBindings `json:"skills"`
	Environment     environmentWire            `json:"environment"`
	Mounts          mt.ExecutionMountSet       `json:"mounts"`
	PlatformPrompt  promptWire                 `json:"platform_prompt"`
}

func preparationWireFrom(v PreparationInputFields) preparationInputWire {
	r := v.Request
	command, _ := r.Launch.Command()
	digest, _ := r.Launch.Digest()
	return preparationInputWire{PreparationInputSchemaVersion, r.ExecutionID, r.Launch, r.Launch.Meta.RequestID, r.Launch.Meta.IdempotencyKey, command.Canonical(), digest, v.AttemptBinding, v.CapturedAt, v.Project, v.Agent, triggerWire{v.Trigger.Ref(), v.Trigger.Data()}, modelWire(v.Model), v.Tools, v.Skills, envWire(v.Environment), v.Mounts, promptWire{v.PlatformPrompt.Revision(), v.PlatformPrompt.Content(), v.PlatformPrompt.Digest()}}
}
func (w preparationInputWire) fields() (PreparationInputFields, error) {
	w.Launch.Meta = f.CommandMeta{RequestID: w.RequestID, IdempotencyKey: w.IdempotencyKey}
	r := PreparationRequest{ExecutionID: w.ExecutionID, Launch: w.Launch}
	if r.Validate() != nil {
		return PreparationInputFields{}, invalid()
	}
	command, _ := r.Launch.Command()
	digest, _ := r.Launch.Digest()
	if w.CommandIdentity != command.Canonical() || w.LaunchDigest != digest {
		return PreparationInputFields{}, invalid()
	}
	trigger, err := NewCapturedTriggerInput(w.Trigger.Ref, w.Trigger.Data)
	if err != nil {
		return PreparationInputFields{}, invalid()
	}
	model, err := w.Model.model()
	if err != nil {
		return PreparationInputFields{}, invalid()
	}
	env, err := w.Environment.environment()
	if err != nil {
		return PreparationInputFields{}, invalid()
	}
	prompt, err := NewPlatformPrompt(w.PlatformPrompt.Revision, w.PlatformPrompt.Content)
	if err != nil || prompt.Digest() != w.PlatformPrompt.Digest {
		return PreparationInputFields{}, invalid()
	}
	return PreparationInputFields{r, w.AttemptBinding, w.CapturedAt, w.Project, w.Agent, trigger, model, w.Tools, w.Skills, env, w.Mounts, prompt}, nil
}
