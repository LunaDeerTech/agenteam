package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

const roundProjectionVersion f.Version = 1
const roundControlLabel = "Committed skills_added control (data, not instructions):\n"
const roundCatalogLabel = "Effective Skill catalog for this round (metadata only; not a current access grant):\n"

// RoundProjection is a deterministic model-visible projection, not a
// ModelRequest or invocation grant. Explicit access includes original Task,
// ordinary variable and file content; default output is redacted.
type RoundProjection struct{ data *roundProjectionData }

type roundProjectionData struct {
	execution i.ExecutionID
	round     ec.RoundID
	source    f.Digest
	messages  []mc.Message
	tools     []mc.Tool
	canonical []byte
	digest    f.Digest
}

func (v RoundProjection) Validate() error {
	if v.data == nil {
		return invalid()
	}
	return nil
}
func (v RoundProjection) ExecutionID() i.ExecutionID {
	if v.data == nil {
		return i.ExecutionID{}
	}
	return v.data.execution
}
func (v RoundProjection) RoundID() ec.RoundID {
	if v.data == nil {
		return ec.RoundID{}
	}
	return v.data.round
}
func (v RoundProjection) SourceDigest() f.Digest {
	if v.data == nil {
		return ""
	}
	return v.data.source
}
func (v RoundProjection) Messages() []mc.Message {
	if v.data == nil {
		return nil
	}
	out := make([]mc.Message, len(v.data.messages))
	for n := range out {
		out[n] = v.data.messages[n].Clone()
	}
	return out
}
func (v RoundProjection) Tools() []mc.Tool {
	if v.data == nil {
		return nil
	}
	out := make([]mc.Tool, len(v.data.tools))
	for n := range out {
		out[n] = v.data.tools[n].Clone()
	}
	return out
}
func (v RoundProjection) CanonicalBytes() []byte {
	if v.data == nil {
		return nil
	}
	return bytes.Clone(v.data.canonical)
}
func (v RoundProjection) Digest() f.Digest {
	if v.data == nil {
		return ""
	}
	return v.data.digest
}
func (RoundProjection) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "agent_round_projection")
}
func (RoundProjection) LogValue() slog.Value { return slog.StringValue("agent_round_projection") }
func (RoundProjection) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_round_projection"`), nil
}
func (*RoundProjection) UnmarshalJSON([]byte) error { return invalid() }

// Only the explicitly composed Execution reader supplies source material.
// Project accepts identities, never caller-made RoundInput or message DTOs.
type RoundProjector struct{ reader ec.RoundProjectionReader }

func NewRoundProjector(reader ec.RoundProjectionReader) (*RoundProjector, error) {
	if nilLoopPort(reader) {
		return nil, loopFault(f.DependencyUnbound)
	}
	return &RoundProjector{reader: reader}, nil
}
func (*RoundProjector) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "agent_round_projector")
}
func (*RoundProjector) LogValue() slog.Value { return slog.StringValue("agent_round_projector") }
func (*RoundProjector) MarshalJSON() ([]byte, error) {
	return []byte(`"agent_round_projector"`), nil
}

func (p *RoundProjector) Project(ctx context.Context, execution i.ExecutionID, next ec.RoundID) (RoundProjection, error) {
	var zero RoundProjection
	if err := contextError(ctx); err != nil {
		return zero, err
	}
	if p == nil || nilLoopPort(p.reader) {
		return zero, loopFault(f.DependencyUnbound)
	}
	if execution.Validate() != nil || next.Validate() != nil {
		return zero, invalid()
	}
	var mu sync.Mutex
	active, calls := true, 0
	var candidate RoundProjection
	var consumedErr error
	err := p.reader.WithCommittedRoundProjection(ctx, execution, next, func(source ec.RoundProjectionSource) error {
		mu.Lock()
		defer mu.Unlock()
		if !active || calls != 0 {
			consumedErr = loopFault(f.InvalidState)
			return consumedErr
		}
		calls++
		consumedErr = ctx.Err()
		if consumedErr == nil {
			candidate, consumedErr = projectRoundSource(ctx, execution, next, source)
		}
		return consumedErr
	})
	mu.Lock()
	defer mu.Unlock()
	active = false
	// A completed callback alone is not publication. In particular a reader
	// that is still held, returns Unknown, or ignores a callback error cannot
	// turn the provisional value into a usable projection.
	if err != nil {
		return zero, loopPortError(err)
	}
	if consumedErr != nil {
		return zero, consumedErr
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if calls != 1 || candidate.Validate() != nil {
		return zero, loopFault(f.InvalidState)
	}
	return candidate, nil
}

func projectRoundSource(ctx context.Context, execution i.ExecutionID, next ec.RoundID, source ec.RoundProjectionSource) (RoundProjection, error) {
	var zero RoundProjection
	if source.SchemaVersion != 1 || source.Input.Validate() != nil || source.Snapshot.Validate() != nil || source.Predecessor.Validate() != nil || len(source.Transcript) != 3 {
		return zero, invalid()
	}
	// The committed reader selects one exact captured profile. A second or
	// malformed binding cannot be treated as a fallback to another operation.
	call := source.Result.Call
	operation, attempt, resultDigest := source.Result.OperationID, source.Result.AttemptID, source.Result.ResultDigest
	runnerBinding, maxArguments := tc.ReadFileRegistration().Binding, 32<<10
	if source.WriteResult != nil {
		if !reflect.DeepEqual(source.Result, tc.ReadFileResultBinding{}) || source.WriteResult.Validate() != nil {
			return zero, invalid()
		}
		call = source.WriteResult.Call
		operation, attempt, resultDigest = source.WriteResult.OperationID, source.WriteResult.AttemptID, source.WriteResult.ResultDigest
		runnerBinding, maxArguments = tc.WriteFileRegistration().Binding, p.MaxMessageBytes
	} else if source.Result.Validate() != nil {
		return zero, invalid()
	}
	q, snapshot, predecessor := source.Input.Fields(), source.Snapshot.Fields(), source.Predecessor.Fields()
	old := predecessor.Round
	fields := snapshot.Context.Input().Fields()
	if q.Validate() != nil || q.ExecutionID != execution || q.RoundID != next || q.SnapshotID != snapshot.ID || q.SnapshotDigest != source.Snapshot.Digest() || q.PredecessorRoundID != old.ID || fields.Request.ExecutionID != execution || old.ExecutionID != execution || old.SnapshotID != snapshot.ID || old.StartID != snapshot.StartID || old.ContextDigest != snapshot.Context.Digest() || !reflect.DeepEqual(q.Initial, fields.Skills) || call.ProjectID != fields.Request.Launch.ProjectID || call.AgentID != fields.Request.Launch.AgentID || call.ExecutionID != execution || call.RoundID != old.ID.String() || call.SnapshotID != snapshot.ID.String() || call.InputBindingID != old.InputBindingID.String() || call.LogicalCallID != old.CallID {
		return zero, invalid()
	}
	if len(fields.Tools) != 1 || fields.Tools[0].ToolID != call.Spec.ToolID || fields.Tools[0].SpecRevision != call.Spec.SpecRevision || fields.Tools[0].ModelVisibleName != call.ModelVisibleName || fields.Tools[0].BindingSnapshot.RunnerBinding != call.RunnerBinding || predecessor.Tool.Name != call.ModelVisibleName || call.Binding != (tc.BuiltinBinding{}) || call.RunnerBinding != runnerBinding {
		return zero, invalid()
	}
	if len(source.Arguments) > maxArguments || f.Progress(len(source.Arguments)) != call.Input.Bytes || toolResultDigest(source.Arguments) != call.Input.SHA256 {
		return zero, invalid()
	}
	var arguments tc.ReadFileToolArguments
	var writeArguments tc.WriteFileToolArguments
	var err error
	if source.WriteResult == nil {
		arguments, err = tc.DecodeReadFileToolArguments(ctx, source.Arguments)
	} else {
		writeArguments, err = tc.DecodeWriteFileToolArguments(ctx, source.Arguments)
		defer clear(writeArguments.Arguments.Data)
	}
	if err != nil {
		return zero, invalid()
	}
	canonicalArguments, err := cursor.CanonicalJSON(source.Arguments)
	if err != nil || toolResultDigest(canonicalArguments) != call.CanonicalArguments {
		return zero, invalid()
	}
	defer clear(canonicalArguments)
	if q.Source.CallID != call.CallID || q.Source.OperationID != operation || q.Source.AttemptID != attempt || q.Source.ResultDigest != resultDigest {
		return zero, invalid()
	}
	messages := []mc.Message{old.Messages[0].Clone()}
	rawPrefix := make([]json.RawMessage, 0, 3)
	total := 0
	for n, entry := range source.Transcript {
		total += len(entry.CanonicalMessage)
		if total > ec.MaxDirectTextRoundBytes || entry.Sequence != f.Sequence(n+1) || entry.RoundID != old.ID || entry.Kind != []string{"input", "assistant", "tool_result"}[n] || toolResultDigest(entry.CanonicalMessage) != entry.Digest {
			return zero, invalid()
		}
		var message mc.Message
		if decodeProjectionMessage(entry.CanonicalMessage, &message) != nil || message.Role != []string{"user", "assistant", "tool"}[n] {
			return zero, invalid()
		}
		messages = append(messages, message)
		rawPrefix = append(rawPrefix, bytes.Clone(entry.CanonicalMessage))
	}
	defer func() {
		for _, raw := range rawPrefix {
			clear(raw)
		}
	}()
	prefix, err := json.Marshal(rawPrefix)
	if err != nil || toolResultDigest(prefix) != q.TranscriptPrefixDigest {
		return zero, invalid()
	}
	clear(prefix)
	if !reflect.DeepEqual(messages[1], old.Messages[1]) {
		return zero, invalid()
	}
	var toolCall *mc.ToolCallPart
	for _, part := range messages[2].Parts {
		if part.ToolCall != nil {
			if toolCall != nil {
				return zero, invalid()
			}
			toolCall = part.ToolCall
		} else if part.Text == nil {
			return zero, unsupported()
		}
	}
	if toolCall == nil || toolCall.ID != call.CallID || toolCall.Name != predecessor.Tool.Name || !bytes.Equal(toolCall.Arguments, canonicalArguments) {
		return zero, invalid()
	}
	if len(messages[3].Parts) != 1 || messages[3].Parts[0].ToolResult == nil {
		return zero, invalid()
	}
	result := messages[3].Parts[0].ToolResult
	if result.CallID != call.CallID || result.IsError || len(result.Parts) != 1 || result.Parts[0].Text == nil || source.Transcript[2].Digest != q.Source.MessageDigest {
		return zero, invalid()
	}
	var projection ToolResultProjection
	rawResult := []byte(result.Parts[0].Text.Text)
	defer clear(rawResult)
	if source.WriteResult == nil {
		projection, err = ProjectReadFileToolResult(ctx, source.Result, rawResult)
	} else {
		projection, err = ProjectWriteFileToolResult(ctx, *source.WriteResult, rawResult)
	}
	if err != nil || !bytes.Equal(projection.CanonicalBytes(), source.Transcript[2].CanonicalMessage) {
		return zero, invalid()
	}
	if source.WriteResult == nil {
		read, err := p.DecodeReadFileResult(rawResult)
		if err != nil || read.Path != arguments.Arguments.Path || read.Offset != arguments.Arguments.Offset || read.Encoding != arguments.Arguments.Encoding || read.BytesRead > arguments.Arguments.Length || !read.EOF && read.BytesRead != arguments.Arguments.Length {
			return zero, invalid()
		}
	} else {
		write, err := p.DecodeWriteFileResult(rawResult)
		if err != nil || write.Path != writeArguments.Arguments.Path || write.BytesWritten != len(writeArguments.Arguments.Data) {
			return zero, invalid()
		}
	}
	if q.Control == nil {
		if source.ControlCanonical != nil {
			return zero, invalid()
		}
	} else {
		want, err := projectionJSON(q.Control)
		if err != nil || !bytes.Equal(want, source.ControlCanonical) {
			return zero, invalid()
		}
		clear(want)
		data := struct {
			Kind      string               `json:"kind"`
			Additions []roundSkillAddition `json:"additions"`
		}{Kind: "skills_added", Additions: make([]roundSkillAddition, 0, len(q.Control.Payload))}
		for _, addition := range q.Control.Payload {
			data.Additions = append(data.Additions, roundSkillAddition{projectionSkill(addition.Binding), addition.ReplacedAssignmentID != nil})
		}
		raw, err := projectionJSON(data)
		if err != nil {
			return zero, invalid()
		}
		messages = append(messages, textMessage("user", roundControlLabel+string(raw)))
		clear(raw)
	}
	catalog := struct {
		Cutoff f.Version          `json:"assignment_cutoff"`
		Skills []skillInformation `json:"skills"`
	}{q.AssignmentCutoff, make([]skillInformation, 0, len(q.Bindings))}
	for _, binding := range q.Bindings {
		catalog.Skills = append(catalog.Skills, projectionSkill(binding))
	}
	raw, err := projectionJSON(catalog)
	if err != nil {
		return zero, invalid()
	}
	messages = append(messages, textMessage("user", roundCatalogLabel+string(raw)))
	clear(raw)
	for _, message := range messages {
		if message.Validate() != nil {
			return zero, invalid()
		}
	}
	tools := []mc.Tool{predecessor.Tool.Clone()}
	canonical, err := projectionJSON(struct {
		Version   f.Version     `json:"projection_version"`
		Execution i.ExecutionID `json:"execution_id"`
		Round     ec.RoundID    `json:"round_id"`
		Source    f.Digest      `json:"round_input_digest"`
		Messages  []mc.Message  `json:"messages"`
		Tools     []mc.Tool     `json:"tools"`
	}{roundProjectionVersion, execution, next, source.Input.Digest(), messages, tools})
	if err != nil {
		return zero, err
	}
	if err = contextError(ctx); err != nil {
		clear(canonical)
		return zero, err
	}
	return RoundProjection{&roundProjectionData{execution, next, source.Input.Digest(), messages, tools, canonical, toolResultDigest(canonical)}}, nil
}

type roundSkillAddition struct {
	Skill            skillInformation `json:"skill"`
	ReplacesPrevious bool             `json:"replaces_previous"`
}

func projectionSkill(b sc.SkillBinding) skillInformation {
	return skillInformation{b.SkillID, b.RevisionID, b.Revision, b.Name, b.Description, b.EntryPath}
}
func projectionJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > ec.MaxDirectTextRoundBytes {
		return nil, invalid()
	}
	defer clear(raw)
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || len(canonical) > ec.MaxDirectTextRoundBytes {
		clear(canonical)
		return nil, invalid()
	}
	return canonical, nil
}
func decodeProjectionMessage(raw []byte, message *mc.Message) error {
	if len(raw) > ec.MaxDirectTextRoundBytes {
		return invalid()
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || !bytes.Equal(raw, canonical) {
		return invalid()
	}
	defer clear(canonical)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(message) != nil || message.Validate() != nil {
		return invalid()
	}
	return nil
}
