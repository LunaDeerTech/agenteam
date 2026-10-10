package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
)

const ExecutionContextSchemaVersion f.Version = 1
const MaxExecutionContextBytes = MaxPreparationInputBytes + 2*MaxTriggerInputBytes + 8*MaxPlatformPromptBytes

// PromptComponent is versioned source text. It is not the final System Prompt
// or a model message. Loop Context Assembly owns their eventual composition.
type PromptComponent struct{ value PlatformPrompt }

func NewPromptComponent(revision, content string) (PromptComponent, error) {
	v, err := NewPlatformPrompt(revision, content)
	if err != nil {
		return PromptComponent{}, err
	}
	return PromptComponent{v}, nil
}
func (v PromptComponent) Validate() error            { return v.value.Validate() }
func (v PromptComponent) Revision() string           { return v.value.Revision() }
func (v PromptComponent) Content() string            { return v.value.Content() }
func (v PromptComponent) Digest() f.Digest           { return v.value.Digest() }
func (PromptComponent) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "prompt_component") }
func (PromptComponent) LogValue() slog.Value         { return slog.StringValue("prompt_component") }
func (PromptComponent) MarshalJSON() ([]byte, error) { return []byte(`"prompt_component"`), nil }

// TriggerContext keeps the original typed source bytes, not a rewritten map
// or a fresh query of the source. Its provider owns decoding and validating the
// domain schema before constructing this value. The neutral envelope validates
// identity only; neither construction nor decoding grants execution authority.
type TriggerContext struct {
	data func() (PreparationRequest, CapturedTriggerInput, PromptComponent)
}

func NewTriggerContext(request PreparationRequest, source CapturedTriggerInput, component PromptComponent) (TriggerContext, error) {
	if request.Validate() != nil || source.Validate() != nil || component.Validate() != nil || source.Ref().ProviderType != request.Launch.Trigger.Kind {
		return TriggerContext{}, invalid()
	}
	request = request.Clone()
	return TriggerContext{func() (PreparationRequest, CapturedTriggerInput, PromptComponent) {
		return request.Clone(), source, component
	}}, nil
}
func (v TriggerContext) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v TriggerContext) Request() PreparationRequest {
	if v.data == nil {
		return PreparationRequest{}
	}
	r, _, _ := v.data()
	return r
}
func (v TriggerContext) Source() CapturedTriggerInput {
	if v.data == nil {
		return CapturedTriggerInput{}
	}
	_, s, _ := v.data()
	return s
}
func (v TriggerContext) Instructions() PromptComponent {
	if v.data == nil {
		return PromptComponent{}
	}
	_, _, p := v.data()
	return p
}
func (v TriggerContext) Matches(request PreparationRequest, source CapturedTriggerInput) bool {
	return v.Validate() == nil && request.Equal(v.Request()) && source.Validate() == nil && source.Ref() == v.Source().Ref() && bytes.Equal(source.Data(), v.Source().Data())
}
func (TriggerContext) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "trigger_context") }
func (TriggerContext) LogValue() slog.Value         { return slog.StringValue("trigger_context") }
func (TriggerContext) MarshalJSON() ([]byte, error) { return []byte(`"trigger_context"`), nil }
func (*TriggerContext) UnmarshalJSON([]byte) error  { return invalid() }

// Build runs after complete preparation-input commit and outside its Tx. This
// slice is pure: no source reload, network, credentials, writes or permission
// issuance. An unimplemented trigger fails explicitly instead of empty data.
type TriggerContextBuilder interface {
	BuildTriggerContext(context.Context, PreparationRequest, CapturedTriggerInput) (TriggerContext, error)
}

// ExecutionContext is an immutable Build result bound to exactly one complete
// input. It reuses that input's typed Project/Agent/Model/Tools/Skills/Environment
// and Mount snapshots, avoiding a second set of independently mutable DTOs.
// It is not a sealed Snapshot, a running Execution, or a ModelRequest.
//
// Input/CanonicalBytes are explicit internal accesses and include ordinary
// variable values and stable Secret reference/lease metadata. They must never
// be sent wholesale to a model or ordinary log. Secret material is absent;
// future model-visible projection must expose only Secret name/description,
// secret=true and availability, not credential or lease identities.
type ExecutionContext struct {
	data func() (PreparationInput, TriggerContext, []byte, f.Digest)
}

func NewExecutionContext(input PreparationInput, trigger TriggerContext) (ExecutionContext, error) {
	if input.Validate() != nil || !trigger.Matches(input.Fields().Request, input.Fields().Trigger) {
		return ExecutionContext{}, invalid()
	}
	raw, err := json.Marshal(executionContextWire{ExecutionContextSchemaVersion, input.Digest(), input.CanonicalBytes(), triggerContextWireFrom(trigger)})
	if err != nil {
		return ExecutionContext{}, invalid()
	}
	if len(raw) > MaxExecutionContextBytes {
		return ExecutionContext{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return ExecutionContext{}, invalid()
	}
	if len(raw) > MaxExecutionContextBytes {
		return ExecutionContext{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	digest := TriggerInputDigest(raw)
	return ExecutionContext{func() (PreparationInput, TriggerContext, []byte, f.Digest) {
		return input, trigger, bytes.Clone(raw), digest
	}}, nil
}
func (v ExecutionContext) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v ExecutionContext) Input() PreparationInput {
	if v.data == nil {
		return PreparationInput{}
	}
	p, _, _, _ := v.data()
	return p
}
func (v ExecutionContext) InputDigest() f.Digest { return v.Input().Digest() }
func (v ExecutionContext) Trigger() TriggerContext {
	if v.data == nil {
		return TriggerContext{}
	}
	_, t, _, _ := v.data()
	return t
}
func (v ExecutionContext) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	_, _, b, _ := v.data()
	return b
}
func (v ExecutionContext) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	_, _, _, d := v.data()
	return d
}
func (v ExecutionContext) PlatformPrompt() PlatformPrompt { return v.Input().Fields().PlatformPrompt }
func (v ExecutionContext) AgentInstructions() string {
	return v.Input().Fields().Agent.Fields().Core.Instructions
}
func (ExecutionContext) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "execution_context") }
func (ExecutionContext) LogValue() slog.Value         { return slog.StringValue("execution_context") }
func (ExecutionContext) MarshalJSON() ([]byte, error) { return []byte(`"execution_context"`), nil }
func (*ExecutionContext) UnmarshalJSON([]byte) error  { return invalid() }

type builtTriggerWire struct {
	ExecutionID  i.ExecutionID `json:"execution_id"`
	LaunchDigest f.Digest      `json:"launch_digest"`
	Purpose      string        `json:"purpose"`
	Source       triggerWire   `json:"source"`
	Instructions promptWire    `json:"instructions"`
}

func triggerContextWireFrom(v TriggerContext) builtTriggerWire {
	r, s, p := v.data()
	digest, _ := r.Launch.Digest()
	return builtTriggerWire{r.ExecutionID, digest, r.Launch.Purpose, triggerWire{s.Ref(), s.Data()}, promptWire{p.Revision(), p.Content(), p.Digest()}}
}

type executionContextWire struct {
	SchemaVersion f.Version        `json:"schema_version"`
	InputDigest   f.Digest         `json:"input_digest"`
	Input         json.RawMessage  `json:"preparation_input"`
	Trigger       builtTriggerWire `json:"trigger_context"`
}

// Decode restores this version's exact immutable Build result. It does not
// invoke a provider or grant source/read/running permission. The Snapshot
// owner must bind these bytes to its original input and authorized Build.
func DecodeExecutionContext(raw []byte) (ExecutionContext, error) {
	if len(raw) > MaxExecutionContextBytes {
		return ExecutionContext{}, f.NewFault(f.PayloadTooLarge, f.NotStarted)
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || !bytes.Equal(canonical, raw) {
		return ExecutionContext{}, invalid()
	}
	var w executionContextWire
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&w) != nil || w.SchemaVersion != ExecutionContextSchemaVersion {
		return ExecutionContext{}, invalid()
	}
	input, err := DecodePreparationInput(w.Input)
	if err != nil || input.Digest() != w.InputDigest {
		return ExecutionContext{}, invalid()
	}
	r := input.Fields().Request
	launchDigest, _ := r.Launch.Digest()
	if w.Trigger.ExecutionID != r.ExecutionID || w.Trigger.LaunchDigest != launchDigest || w.Trigger.Purpose != r.Launch.Purpose {
		return ExecutionContext{}, invalid()
	}
	source, err := NewCapturedTriggerInput(w.Trigger.Source.Ref, w.Trigger.Source.Data)
	if err != nil {
		return ExecutionContext{}, invalid()
	}
	component, err := NewPromptComponent(w.Trigger.Instructions.Revision, w.Trigger.Instructions.Content)
	if err != nil || component.Digest() != w.Trigger.Instructions.Digest {
		return ExecutionContext{}, invalid()
	}
	trigger, err := NewTriggerContext(r, source, component)
	if err != nil {
		return ExecutionContext{}, invalid()
	}
	out, err := NewExecutionContext(input, trigger)
	if err != nil || !bytes.Equal(out.CanonicalBytes(), raw) {
		return ExecutionContext{}, invalid()
	}
	return out, nil
}
