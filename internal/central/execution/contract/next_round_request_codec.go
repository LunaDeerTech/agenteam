package contract

import (
	"bytes"
	"context"
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
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

const NextRoundModelRequestSchemaVersion f.Version = 1
const NextRoundModelInputSchemaVersion f.Version = 3

// The complete request contains one existing projection (at most 16 MiB),
// its captured model (bounded by the original 8 MiB preparation input), and
// fixed identity/profile overhead. No original result budget is enlarged.
const MaxNextRoundModelRequestBytes = MaxDirectTextRoundBytes + MaxPreparationInputBytes + (16 << 10)

// Fields is a safe identity projection. Neither it nor a valid encoded value
// proves commitment or grants Model invocation/credential access.
type NextRoundModelRequestFields struct {
	SchemaVersion    f.Version
	ProjectID        i.ProjectID
	AgentID          i.AgentID
	ExecutionID      i.ExecutionID
	RoundInputID     RoundID
	RoundInputDigest f.Digest
	SnapshotID       SnapshotID
	SnapshotDigest   f.Digest
	ProjectionDigest f.Digest
	CallID           mc.CallID
	InputDigest      f.Digest
}

// Input is explicit protected codec material, not a caller-facing authority
// DTO. The Execution binder supplies it only through its committed source loan.
type NextRoundModelRequestInput struct {
	ProjectID        i.ProjectID
	AgentID          i.AgentID
	ExecutionID      i.ExecutionID
	RoundInputID     RoundID
	RoundInputDigest f.Digest
	SnapshotID       SnapshotID
	SnapshotDigest   f.Digest
	ProjectionDigest f.Digest
	CallID           mc.CallID
	Actor            i.Actor
	Model            mc.ResolvedModel
	Messages         []mc.Message
	Tools            []mc.Tool
}

type NextRoundModelRequest struct{ data *nextRoundModelRequestData }
type nextRoundModelRequestData struct {
	fields    NextRoundModelRequestFields
	request   mc.ModelRequest
	canonical []byte
	digest    f.Digest
}

// This explicit actor representation cannot silently lose Actor through
// ModelRequest's json:"-" or acquire a Human/Service identity on decoding.
type nextRoundActorWire struct {
	Kind        i.ActorKind   `json:"kind"`
	ProjectID   i.ProjectID   `json:"project_id"`
	AgentID     i.AgentID     `json:"agent_id"`
	ExecutionID i.ExecutionID `json:"execution_id"`
}
type nextRoundSemanticWire struct {
	SchemaVersion  f.Version          `json:"schema_version"`
	Actor          nextRoundActorWire `json:"actor"`
	Model          resolvedModelWire  `json:"model"`
	Messages       []mc.Message       `json:"messages"`
	Tools          []mc.Tool          `json:"tools"`
	ToolChoice     mc.ToolChoice      `json:"tool_choice"`
	ResponseFormat mc.ResponseFormat  `json:"response_format"`
	RetryClass     mc.RetryClass      `json:"retry_class"`
}
type nextRoundRequestWire struct {
	SchemaVersion    f.Version             `json:"schema_version"`
	ProjectID        i.ProjectID           `json:"project_id"`
	AgentID          i.AgentID             `json:"agent_id"`
	ExecutionID      i.ExecutionID         `json:"execution_id"`
	RoundInputID     RoundID               `json:"round_input_id"`
	RoundInputDigest f.Digest              `json:"round_input_digest"`
	SnapshotID       SnapshotID            `json:"snapshot_id"`
	SnapshotDigest   f.Digest              `json:"snapshot_digest"`
	ProjectionDigest f.Digest              `json:"projection_digest"`
	CallID           mc.CallID             `json:"call_id"`
	Input            mc.InputIdentity      `json:"input"`
	Semantic         nextRoundSemanticWire `json:"semantic"`
}

// New constructs a candidate; only the original binder's actual Committed
// return can publish it as a stored request. No resolver, Store or Model port
// is called. The old schema-1/2 input digests remain unchanged.
func NewNextRoundModelRequest(ctx context.Context, v NextRoundModelRequestInput) (NextRoundModelRequest, error) {
	var zero NextRoundModelRequest
	if ctx == nil {
		return zero, invalid()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if v.ProjectID.Validate() != nil || v.AgentID.Validate() != nil || v.ExecutionID.Validate() != nil || v.RoundInputID.Validate() != nil || v.RoundInputDigest.Validate() != nil || v.SnapshotID.Validate() != nil || v.SnapshotDigest.Validate() != nil || v.ProjectionDigest.Validate() != nil || v.CallID.Validate() != nil || v.Actor.Validate() != nil || v.Model.Validate() != nil {
		return zero, invalid()
	}
	a, consumer := v.Actor.Details(), v.Model.Consumer
	if a.Kind != i.AgentRun || a.ProjectID != v.ProjectID.String() || a.AgentID != v.AgentID.String() || a.ExecutionID != v.ExecutionID.String() || consumer.Kind != mc.AgentConsumer || consumer.Purpose != mc.AgentGeneration || consumer.ProjectID != v.ProjectID || consumer.AgentID == nil || *consumer.AgentID != v.AgentID || consumer.ExecutionID == nil || *consumer.ExecutionID != v.ExecutionID || v.Model.LeaseOwner.Details().Kind != sc.ExecutionOwner || v.Model.LeaseOwner.Details().ID != v.ExecutionID.String() || len(v.Tools) != 1 || len(v.Messages) < 5 || len(v.Messages) > 6 {
		return zero, invalid()
	}
	semantic := nextRoundSemanticWire{NextRoundModelInputSchemaVersion, nextRoundActorWire{i.AgentRun, v.ProjectID, v.AgentID, v.ExecutionID}, modelWire(v.Model), v.Messages, v.Tools, mc.ToolChoice{Kind: "auto"}, mc.ResponseFormat{Kind: "text"}, mc.AgentRetry}
	// Count before constructing JSON buffers, including escaping in every
	// original message. Then hash the complete semantic request without a
	// self-referential InputIdentity or a newly allocated logical CallID.
	inputRaw, err := nextRoundRequestJSON(ctx, semantic)
	if err != nil {
		return zero, err
	}
	defer clear(inputRaw)
	inputDigest := TriggerInputDigest(inputRaw)
	execution := v.ExecutionID
	request := mc.ModelRequest{Actor: v.Actor, CallID: v.CallID, Consumer: consumer.Clone(), Model: v.Model, Input: mc.InputIdentity{ExecutionID: &execution, RoundID: v.RoundInputID.String(), Digest: inputDigest, SchemaVersion: NextRoundModelInputSchemaVersion}, Messages: v.Messages, Tools: v.Tools, ToolChoice: semantic.ToolChoice, ResponseFormat: semantic.ResponseFormat, RetryClass: semantic.RetryClass}
	if request.Validate() != nil {
		return zero, invalid()
	}
	wire := nextRoundRequestWire{NextRoundModelRequestSchemaVersion, v.ProjectID, v.AgentID, v.ExecutionID, v.RoundInputID, v.RoundInputDigest, v.SnapshotID, v.SnapshotDigest, v.ProjectionDigest, v.CallID, request.Input, semantic}
	raw, err := nextRoundRequestJSON(ctx, wire)
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		clear(raw)
		return zero, err
	}
	fields := NextRoundModelRequestFields{NextRoundModelRequestSchemaVersion, v.ProjectID, v.AgentID, v.ExecutionID, v.RoundInputID, v.RoundInputDigest, v.SnapshotID, v.SnapshotDigest, v.ProjectionDigest, v.CallID, inputDigest}
	owned := request.Clone()
	digest := TriggerInputDigest(raw)
	if err = ctx.Err(); err != nil {
		clear(raw)
		return zero, err
	}
	return NextRoundModelRequest{&nextRoundModelRequestData{fields, owned, raw, digest}}, nil
}

func DecodeNextRoundModelRequest(ctx context.Context, raw []byte) (NextRoundModelRequest, error) {
	var zero NextRoundModelRequest
	if ctx == nil {
		return zero, invalid()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	var w nextRoundRequestWire
	if len(raw) > MaxNextRoundModelRequestBytes || directTextDecode(raw, &w) != nil || w.SchemaVersion != NextRoundModelRequestSchemaVersion || w.Semantic.SchemaVersion != NextRoundModelInputSchemaVersion || w.Semantic.Actor.Kind != i.AgentRun || w.Semantic.Actor.ProjectID != w.ProjectID || w.Semantic.Actor.AgentID != w.AgentID || w.Semantic.Actor.ExecutionID != w.ExecutionID || w.Semantic.ToolChoice != (mc.ToolChoice{Kind: "auto"}) || w.Semantic.ResponseFormat.Kind != "text" || w.Semantic.ResponseFormat.Name != "" || len(w.Semantic.ResponseFormat.Schema) != 0 || w.Semantic.RetryClass != mc.AgentRetry {
		return zero, invalid()
	}
	actor, err := i.NewAgentRun(w.ProjectID, w.AgentID, w.ExecutionID)
	if err != nil {
		return zero, invalid()
	}
	model, err := w.Semantic.Model.model()
	if err != nil {
		return zero, invalid()
	}
	v, err := NewNextRoundModelRequest(ctx, NextRoundModelRequestInput{w.ProjectID, w.AgentID, w.ExecutionID, w.RoundInputID, w.RoundInputDigest, w.SnapshotID, w.SnapshotDigest, w.ProjectionDigest, w.CallID, actor, model, w.Semantic.Messages, w.Semantic.Tools})
	if err != nil {
		return zero, err
	}
	if !bytes.Equal(v.data.canonical, raw) {
		clear(v.data.canonical)
		return zero, invalid()
	}
	return v, nil
}

func (v NextRoundModelRequest) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v NextRoundModelRequest) Fields() NextRoundModelRequestFields {
	if v.data == nil {
		return NextRoundModelRequestFields{}
	}
	return v.data.fields
}
func (v NextRoundModelRequest) Request() mc.ModelRequest {
	if v.data == nil {
		return mc.ModelRequest{}
	}
	return v.data.request.Clone()
}
func (v NextRoundModelRequest) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	return bytes.Clone(v.data.canonical)
}
func (v NextRoundModelRequest) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	return v.data.digest
}
func (v NextRoundModelRequest) InputDigest() f.Digest {
	if v.data == nil {
		return ""
	}
	return v.data.fields.InputDigest
}
func (NextRoundModelRequest) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "next_round_model_request")
}
func (NextRoundModelRequest) LogValue() slog.Value {
	return slog.StringValue("next_round_model_request")
}
func (NextRoundModelRequest) MarshalJSON() ([]byte, error) {
	return []byte(`"next_round_model_request"`), nil
}
func (*NextRoundModelRequest) UnmarshalJSON([]byte) error { return invalid() }
func (NextRoundModelRequestInput) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "next_round_model_request_input")
}
func (NextRoundModelRequestInput) LogValue() slog.Value {
	return slog.StringValue("next_round_model_request_input")
}
func (NextRoundModelRequestInput) MarshalJSON() ([]byte, error) {
	return []byte(`"next_round_model_request_input"`), nil
}

func nextRoundRequestJSON(ctx context.Context, value any) ([]byte, error) {
	remaining := MaxNextRoundModelRequestBytes
	if err := nextRoundRequestJSONSize(ctx, reflect.ValueOf(value), &remaining); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) > MaxNextRoundModelRequestBytes {
		clear(raw)
		return nil, invalid()
	}
	defer clear(raw)
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || len(canonical) > MaxNextRoundModelRequestBytes {
		clear(canonical)
		return nil, invalid()
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
func nextRoundRequestJSONSize(ctx context.Context, v reflect.Value, left *int) error {
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
				return invalid()
			}
			defer clear(raw)
			return charge(len(raw))
		}
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		return nextRoundRequestJSONSize(ctx, v.Elem(), left)
	case reflect.String:
		s := v.String()
		if !utf8.ValidString(s) {
			return invalid()
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
			if len(tag) > 1 && tag[1] == "omitempty" && nextRoundJSONEmpty(fv) {
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
			if err := nextRoundRequestJSONSize(ctx, reflect.ValueOf(name), left); err != nil {
				return err
			}
			if err := charge(1); err != nil {
				return err
			}
			if err := nextRoundRequestJSONSize(ctx, fv, left); err != nil {
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
			if err := nextRoundRequestJSONSize(ctx, v.Index(n), left); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if v.IsNil() {
			return charge(4)
		}
		if v.Type().Key().Kind() != reflect.String {
			return invalid()
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
			if err := nextRoundRequestJSONSize(ctx, iterator.Key(), left); err != nil {
				return err
			}
			if err := charge(1); err != nil {
				return err
			}
			if err := nextRoundRequestJSONSize(ctx, iterator.Value(), left); err != nil {
				return err
			}
		}
		return nil
	default:
		raw, err := json.Marshal(v.Interface())
		if err != nil {
			return invalid()
		}
		return charge(len(raw))
	}
}
func nextRoundJSONEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64, reflect.Interface, reflect.Pointer:
		return v.IsZero()
	}
	return false
}
