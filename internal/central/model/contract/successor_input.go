package contract

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const SuccessorInputSchemaVersion f.Version = 3

// This is the existing stored semantic budget, not a Model wire capability.
// The unchanged Execution wrapper enforces its own complete 25,182,208 B cap.
const MaxSuccessorSemanticBytes = (16 << 20) + (8 << 20) + (16 << 10)

// SuccessorSemanticParts is protected, explicit codec material. It supplies
// no committed-source, Invocation, credential, or current execution authority.
type SuccessorSemanticParts struct {
	Actor          i.Actor
	Model          ResolvedModel
	Messages       []Message
	Tools          []Tool
	ToolChoice     ToolChoice
	ResponseFormat ResponseFormat
	RetryClass     RetryClass
}

func (v SuccessorSemanticParts) Clone() SuccessorSemanticParts {
	r := (ModelRequest{Actor: v.Actor, Model: v.Model, Messages: v.Messages, Tools: v.Tools, ToolChoice: v.ToolChoice, ResponseFormat: v.ResponseFormat, RetryClass: v.RetryClass}).Clone()
	return SuccessorSemanticParts{r.Actor, r.Model, r.Messages, r.Tools, r.ToolChoice, r.ResponseFormat, r.RetryClass}
}

type SuccessorSemantic struct{ data *successorSemanticData }
type successorSemanticData struct {
	parts     SuccessorSemanticParts
	canonical []byte
	digest    f.Digest
}
type successorActorWire struct {
	Kind        i.ActorKind   `json:"kind"`
	ProjectID   i.ProjectID   `json:"project_id"`
	AgentID     i.AgentID     `json:"agent_id"`
	ExecutionID i.ExecutionID `json:"execution_id"`
}
type successorSemanticWire struct {
	SchemaVersion  f.Version                  `json:"schema_version"`
	Actor          successorActorWire         `json:"actor"`
	Model          successorResolvedModelWire `json:"model"`
	Messages       []Message                  `json:"messages"`
	Tools          []Tool                     `json:"tools"`
	ToolChoice     ToolChoice                 `json:"tool_choice"`
	ResponseFormat ResponseFormat             `json:"response_format"`
	RetryClass     RetryClass                 `json:"retry_class"`
}

// NewSuccessorSemantic owns the sole schema-3 semantic encoding algorithm.
// It preserves the delivered NRMR shape, without enabling any Runtime profile.
func NewSuccessorSemantic(ctx context.Context, parts SuccessorSemanticParts) (SuccessorSemantic, error) {
	var zero SuccessorSemantic
	if ctx == nil {
		return zero, bad()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	c := parts.Model.Consumer
	if parts.Actor.Validate() != nil || parts.Model.Validate() != nil || parts.Actor.Details().Kind != i.AgentRun || c.Kind != AgentConsumer || c.Purpose != AgentGeneration || !actorMatches(parts.Actor, c) || parts.Model.LeaseOwner.Details().Kind != sc.ExecutionOwner || !ownerMatches(parts.Model.LeaseOwner, c, nil) || len(parts.Tools) == 0 || len(parts.Tools) > 128 || len(parts.Messages) < 2 || len(parts.Messages) > 256 || parts.ToolChoice != (ToolChoice{Kind: "auto"}) || parts.ResponseFormat.Validate() != nil || parts.ResponseFormat.Kind != "text" || parts.RetryClass != AgentRetry {
		return zero, bad()
	}
	count := 0
	for _, message := range parts.Messages {
		if message.Validate() != nil {
			return zero, bad()
		}
		count += len(message.Parts)
		if count > 256 {
			return zero, bad()
		}
	}
	names := make(map[string]bool, len(parts.Tools))
	for _, tool := range parts.Tools {
		if tool.Validate() != nil || names[tool.Name] {
			return zero, bad()
		}
		names[tool.Name] = true
	}
	if ValidateToolHistory(parts.Messages) != nil {
		return zero, bad()
	}
	wire := successorSemanticWire{SuccessorInputSchemaVersion, successorActorWire{i.AgentRun, c.ProjectID, *c.AgentID, *c.ExecutionID}, successorModelWire(parts.Model), parts.Messages, parts.Tools, parts.ToolChoice, parts.ResponseFormat, parts.RetryClass}
	raw, err := successorSemanticJSON(ctx, wire)
	if err != nil {
		return zero, err
	}
	owned := parts.Clone()
	if err = ctx.Err(); err != nil {
		clear(raw)
		return zero, err
	}
	return SuccessorSemantic{&successorSemanticData{owned, raw, successorSemanticDigest(raw)}}, nil
}

func DecodeSuccessorSemantic(ctx context.Context, raw []byte) (SuccessorSemantic, error) {
	var zero SuccessorSemantic
	if ctx == nil {
		return zero, bad()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(raw) > MaxSuccessorSemanticBytes {
		return zero, bad()
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return zero, bad()
	}
	defer clear(canonical)
	if !bytes.Equal(canonical, raw) {
		return zero, bad()
	}
	var wire successorSemanticWire
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&wire) != nil || wire.SchemaVersion != SuccessorInputSchemaVersion || wire.Actor.Kind != i.AgentRun {
		return zero, bad()
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return zero, bad()
	}
	actor, err := i.NewAgentRun(wire.Actor.ProjectID, wire.Actor.AgentID, wire.Actor.ExecutionID)
	if err != nil {
		return zero, bad()
	}
	model, err := wire.Model.model()
	if err != nil {
		return zero, bad()
	}
	got, err := NewSuccessorSemantic(ctx, SuccessorSemanticParts{actor, model, wire.Messages, wire.Tools, wire.ToolChoice, wire.ResponseFormat, wire.RetryClass})
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(got.data.canonical, raw) {
		clear(got.data.canonical)
		return zero, bad()
	}
	return got, nil
}

// ValidateSuccessorRequest checks an already supplied request's complete C0
// identity and original schema-3 digest. It does not make a Runtime accept it.
// Response capability/cardinality policy remains with the explicit Loop owner.
func ValidateSuccessorRequest(ctx context.Context, request ModelRequest) (SuccessorSemantic, error) {
	var zero SuccessorSemantic
	if ctx == nil {
		return zero, bad()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if request.Validate() != nil || request.Input.SchemaVersion != SuccessorInputSchemaVersion {
		return zero, bad()
	}
	got, err := NewSuccessorSemantic(ctx, SuccessorSemanticParts{request.Actor, request.Model, request.Messages, request.Tools, request.ToolChoice, request.ResponseFormat, request.RetryClass})
	if err != nil {
		return zero, err
	}
	if got.Digest() != request.Input.Digest {
		clear(got.data.canonical)
		return zero, bad()
	}
	return got, nil
}

func (v SuccessorSemantic) Validate() error {
	if v.data == nil {
		return bad()
	}
	return nil
}
func (v SuccessorSemantic) Parts() SuccessorSemanticParts {
	if v.data == nil {
		return SuccessorSemanticParts{}
	}
	return v.data.parts.Clone()
}
func (v SuccessorSemantic) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	return bytes.Clone(v.data.canonical)
}
func (v SuccessorSemantic) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	return v.data.digest
}
func (SuccessorSemantic) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "model_successor_semantic")
}
func (SuccessorSemantic) LogValue() slog.Value { return slog.StringValue("model_successor_semantic") }
func (SuccessorSemantic) MarshalJSON() ([]byte, error) {
	return []byte(`"model_successor_semantic"`), nil
}
func (*SuccessorSemantic) UnmarshalJSON([]byte) error { return bad() }
func (SuccessorSemanticParts) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "model_successor_semantic_parts")
}
func (SuccessorSemanticParts) LogValue() slog.Value {
	return slog.StringValue("model_successor_semantic_parts")
}
func (SuccessorSemanticParts) MarshalJSON() ([]byte, error) {
	return []byte(`"model_successor_semantic_parts"`), nil
}
func (*SuccessorSemanticParts) UnmarshalJSON([]byte) error { return bad() }
func successorSemanticDigest(raw []byte) f.Digest {
	return f.Digest(fmt.Sprintf("sha256:%x", sha256.Sum256(raw)))
}

type successorCredentialRefWire struct {
	ID    sc.CredentialID `json:"id"`
	Scope i.ScopeDetails  `json:"scope"`
}
type successorCredentialLeaseWire struct {
	ID  sc.LeaseID                 `json:"lease_id"`
	Ref successorCredentialRefWire `json:"credential_ref"`
}

func successorRefWire(r sc.CredentialRef) successorCredentialRefWire {
	return successorCredentialRefWire{r.Details().ID, r.Details().Scope.Details()}
}
func (w successorCredentialRefWire) ref() (sc.CredentialRef, error) {
	var scope i.Scope
	switch w.Scope.Kind {
	case i.System:
		if w.Scope.ProjectID != "" || w.Scope.AgentID != "" {
			return sc.CredentialRef{}, bad()
		}
		scope = i.SystemScope()
	case i.ProjectScope:
		p, e := f.ParseID[i.Project](w.Scope.ProjectID)
		if e != nil || w.Scope.AgentID != "" {
			return sc.CredentialRef{}, bad()
		}
		scope, e = i.InProject(p)
		if e != nil {
			return sc.CredentialRef{}, bad()
		}
	default:
		return sc.CredentialRef{}, bad()
	}
	return sc.NewCredentialRef(w.ID, scope)
}

type successorModelSnapshotWire struct {
	ID               SnapshotID                  `json:"id"`
	Identity         ModelIdentity               `json:"identity"`
	Endpoint         string                      `json:"endpoint"`
	Parameters       json.RawMessage             `json:"parameters"`
	RequestOverwrite json.RawMessage             `json:"request_overwrite"`
	HeaderOverwrite  map[string]string           `json:"header_overwrite"`
	Capabilities     Capabilities                `json:"capabilities"`
	CredentialRef    *successorCredentialRefWire `json:"credential_ref"`
	SelectionVersion *f.Version                  `json:"selection_version"`
}
type successorResolvedModelWire struct {
	Snapshot        successorModelSnapshotWire    `json:"snapshot"`
	Consumer        Consumer                      `json:"consumer"`
	LeaseOwner      sc.OwnerDetails               `json:"lease_owner"`
	CredentialLease *successorCredentialLeaseWire `json:"credential_lease"`
}

func successorModelWire(m ResolvedModel) successorResolvedModelWire {
	s := m.Snapshot
	w := successorResolvedModelWire{Snapshot: successorModelSnapshotWire{ID: s.ID, Identity: s.Identity, Endpoint: s.Endpoint, Parameters: s.Parameters, RequestOverwrite: s.RequestOverwrite, HeaderOverwrite: s.HeaderOverwrite, Capabilities: s.Capabilities, SelectionVersion: s.SelectionVersion}, Consumer: m.Consumer, LeaseOwner: m.LeaseOwner.Details()}
	if s.CredentialRef != nil {
		r := successorRefWire(*s.CredentialRef)
		w.Snapshot.CredentialRef = &r
	}
	if m.CredentialLease != nil {
		w.CredentialLease = &successorCredentialLeaseWire{m.CredentialLease.LeaseID, successorRefWire(m.CredentialLease.CredentialRef)}
	}
	return w
}
func (w successorResolvedModelWire) model() (ResolvedModel, error) {
	s := w.Snapshot
	o, e := sc.NewCredentialLeaseOwner(w.LeaseOwner.Kind, w.LeaseOwner.ID)
	if e != nil {
		return ResolvedModel{}, bad()
	}
	m := ResolvedModel{Snapshot: ConfigSnapshot{ID: s.ID, Identity: s.Identity, Endpoint: s.Endpoint, Parameters: s.Parameters, RequestOverwrite: s.RequestOverwrite, HeaderOverwrite: s.HeaderOverwrite, Capabilities: s.Capabilities, SelectionVersion: s.SelectionVersion}, Consumer: w.Consumer, LeaseOwner: o}
	if s.CredentialRef != nil {
		r, e := s.CredentialRef.ref()
		if e != nil {
			return ResolvedModel{}, bad()
		}
		m.Snapshot.CredentialRef = &r
	}
	if w.CredentialLease != nil {
		r, e := w.CredentialLease.Ref.ref()
		if e != nil {
			return ResolvedModel{}, bad()
		}
		m.CredentialLease = &sc.CredentialLease{LeaseID: w.CredentialLease.ID, CredentialRef: r}
	}
	return m, nil
}

func successorSemanticJSON(ctx context.Context, value any) ([]byte, error) {
	remaining := MaxSuccessorSemanticBytes
	if err := successorSemanticJSONSize(ctx, reflect.ValueOf(value), &remaining); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaxSuccessorSemanticBytes {
		clear(raw)
		return nil, bad()
	}
	defer clear(raw)
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || len(canonical) > MaxSuccessorSemanticBytes {
		clear(canonical)
		return nil, bad()
	}
	if err = ctx.Err(); err != nil {
		clear(canonical)
		return nil, err
	}
	return canonical, nil
}

// Private size preflight for the explicit wire structs above. Strings are
// counted without first allocating an escaped copy. Custom scalar JSON and
// RawMessage remain individually bounded, and every resulting byte is charged.
func successorSemanticJSONSize(ctx context.Context, v reflect.Value, left *int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	charge := func(n int) error {
		if n > *left {
			return f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		*left -= n
		return nil
	}
	if !v.IsValid() || (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil() {
		return charge(4)
	}
	if v.CanInterface() {
		if raw, ok := v.Interface().(json.RawMessage); ok && len(raw) > *left {
			return f.NewFault(f.PayloadTooLarge, f.NotStarted)
		}
		if m, ok := v.Interface().(json.Marshaler); ok {
			raw, err := json.Marshal(m)
			if err != nil {
				return bad()
			}
			defer clear(raw)
			return charge(len(raw))
		}
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return successorSemanticJSONSize(ctx, v.Elem(), left)
	case reflect.String:
		s := v.String()
		if !utf8.ValidString(s) {
			return bad()
		}
		if err := charge(2); err != nil {
			return err
		}
		for n := 0; n < len(s); n++ {
			if n&4095 == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			cost := 1
			switch b := s[n]; {
			case b == '"' || b == '\\' || b == '\b' || b == '\f' || b == '\n' || b == '\r' || b == '\t':
				cost = 2
			case b < 0x20 || b == '<' || b == '>' || b == '&':
				cost = 6
			case b == 0xe2 && n+2 < len(s) && s[n+1] == 0x80 && (s[n+2] == 0xa8 || s[n+2] == 0xa9):
				cost = 6
				n += 2
			}
			if err := charge(cost); err != nil {
				return err
			}
		}
		return nil
	case reflect.Struct:
		if err := charge(2); err != nil {
			return err
		}
		count := 0
		for n := 0; n < v.NumField(); n++ {
			field := v.Type().Field(n)
			if field.PkgPath != "" {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			fv := v.Field(n)
			if len(tag) > 1 && tag[1] == "omitempty" && successorJSONEmpty(fv) {
				continue
			}
			name := tag[0]
			if name == "" {
				name = field.Name
			}
			if count > 0 {
				if err := charge(1); err != nil {
					return err
				}
			}
			count++
			if err := successorSemanticJSONSize(ctx, reflect.ValueOf(name), left); err != nil {
				return err
			}
			if err := charge(1); err != nil {
				return err
			}
			if err := successorSemanticJSONSize(ctx, fv, left); err != nil {
				return err
			}
		}
		return nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return charge(4)
		}
		if err := charge(2); err != nil {
			return err
		}
		for n := 0; n < v.Len(); n++ {
			if n > 0 {
				if err := charge(1); err != nil {
					return err
				}
			}
			if err := successorSemanticJSONSize(ctx, v.Index(n), left); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if v.IsNil() {
			return charge(4)
		}
		if v.Type().Key().Kind() != reflect.String {
			return bad()
		}
		if err := charge(2); err != nil {
			return err
		}
		iterator, count := v.MapRange(), 0
		for iterator.Next() {
			if count > 0 {
				if err := charge(1); err != nil {
					return err
				}
			}
			count++
			if err := successorSemanticJSONSize(ctx, iterator.Key(), left); err != nil {
				return err
			}
			if err := charge(1); err != nil {
				return err
			}
			if err := successorSemanticJSONSize(ctx, iterator.Value(), left); err != nil {
				return err
			}
		}
		return nil
	default:
		raw, err := json.Marshal(v.Interface())
		if err != nil {
			return bad()
		}
		return charge(len(raw))
	}
}
func successorJSONEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}
