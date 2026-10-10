// Package runtime owns persistent Tool operations. This first slice prepares
// fixed install metadata; it does not dispatch a Tool or mint Execution rights.
package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// Projection fields are written explicitly. In particular Actor and upstream
// payload bytes cannot enter persistence through general-purpose marshaling.
type bindingData struct {
	Format                        int `json:"format_version"`
	Project                       id.ProjectID
	Agent                         id.AgentID
	Execution                     id.ExecutionID
	Round, Snapshot, InputBinding string
	LogicalCall                   mc.CallID
	Invocation                    mc.InvocationID
	Call, ModelName               string
	Spec                          tc.SpecRef
	Binding                       tc.BuiltinBinding
	Payload                       string
	PayloadDigest                 f.Digest
	PayloadBytes                  f.Progress
	ArgumentsDigest               f.Digest
}

func bindingProjection(v tc.ToolCallBinding) bindingData {
	return bindingData{1, v.ProjectID, v.AgentID, v.ExecutionID, v.RoundID, v.SnapshotID, v.InputBindingID, v.LogicalCallID, v.InvocationID, v.CallID, v.ModelVisibleName, v.Spec, v.Binding, v.Input.PayloadID, v.Input.SHA256, v.Input.Bytes, v.CanonicalArguments}
}

func (v bindingData) request() (tc.ToolCallBinding, error) {
	actor, err := id.NewAgentRun(v.Project, v.Agent, v.Execution)
	if err != nil || v.Format != 1 {
		return tc.ToolCallBinding{}, fail(f.InvalidState)
	}
	r := tc.ToolCallBinding{Actor: actor, ProjectID: v.Project, AgentID: v.Agent, ExecutionID: v.Execution, RoundID: v.Round, SnapshotID: v.Snapshot, InputBindingID: v.InputBinding, LogicalCallID: v.LogicalCall, InvocationID: v.Invocation, CallID: v.Call, ModelVisibleName: v.ModelName, Spec: v.Spec, Binding: v.Binding, Input: tc.ToolInputReference{PayloadID: v.Payload, SHA256: v.PayloadDigest, Bytes: v.PayloadBytes}, CanonicalArguments: v.ArgumentsDigest}
	if r.Validate() != nil {
		return tc.ToolCallBinding{}, fail(f.InvalidState)
	}
	return r, nil
}

type installData struct {
	Binding        bindingData
	NormalizedName string
	PackageDigest  f.Digest
	ManifestDigest f.Digest
}

// CanonicalInstallArguments is the fixed profile's explicit projection, not a
// general JSON Schema validator. It checks the original strict parser first.
// The returned digest can be bound to the original Execution input reference.
func CanonicalInstallArguments(ctx context.Context, raw []byte) (f.Digest, error) {
	_, digest, err := parseInstall(ctx, raw)
	return digest, err
}

func parseInstall(ctx context.Context, raw []byte) (builtin.SkillInstallPackage, f.Digest, error) {
	pkg, err := builtin.ParseSkillInstall(ctx, raw)
	if err != nil {
		return builtin.SkillInstallPackage{}, "", err
	}
	var v struct {
		Mode   string `json:"mode"`
		Source struct {
			Kind  string `json:"kind"`
			Files []struct {
				Path string `json:"path"`
				Text string `json:"utf8_text"`
			} `json:"files"`
		} `json:"source"`
	}
	// Duplicate/unknown members, Unicode and complete syntax have already been
	// checked. This projection retains file order and exact decoded text.
	if err = json.Unmarshal(raw, &v); err != nil {
		return builtin.SkillInstallPackage{}, "", fail(f.InvalidArgument)
	}
	var canonical bytes.Buffer
	encoder := json.NewEncoder(&canonical)
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(v); err != nil {
		return builtin.SkillInstallPackage{}, "", portError(err)
	}
	if err = ctx.Err(); err != nil {
		return builtin.SkillInstallPackage{}, "", err
	}
	// json.Encoder's one trailing newline is fixed for canonical-install-v1.
	return pkg, digest(canonical.Bytes()), nil
}

func prepareInput(ctx context.Context, b tc.ToolCallBinding, raw []byte) (installData, error) {
	if err := contextError(ctx); err != nil {
		return installData{}, err
	}
	if err := b.Validate(); err != nil {
		return installData{}, err
	}
	if len(raw) > builtin.MaxSkillInstallArguments {
		return installData{}, fail(f.PayloadTooLarge)
	}
	if int64(len(raw)) != int64(b.Input.Bytes) || digest(raw) != b.Input.SHA256 {
		return installData{}, fail(f.ConfirmationStale)
	}
	pkg, canonical, err := parseInstall(ctx, raw)
	if err != nil {
		return installData{}, err
	}
	if canonical != b.CanonicalArguments {
		return installData{}, fail(f.ConfirmationStale)
	}
	facts, err := pkg.Facts()
	if err != nil {
		return installData{}, err
	}
	if facts.NormalizedName == skill.AddSkillsNormalizedName {
		return installData{}, fail(f.Forbidden)
	}
	return installData{bindingProjection(b), facts.NormalizedName, facts.PackageSHA256, facts.ManifestSHA256}, nil
}

func digest(raw []byte) f.Digest {
	v := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(v[:]))
}
func encode(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > 32768 {
		return nil, fail(f.InvalidState)
	}
	return raw, nil
}

type operationRecord struct {
	ID          tc.OperationID
	Input       installData
	InputDigest f.Digest
	SkillID     sc.SkillID
	Key         f.IdempotencyKey
	Fingerprint f.Digest
	Phase       string
	Version     f.Version
}

func fingerprint(input installData, skill sc.SkillID) (f.Digest, error) {
	raw, err := encode(struct {
		Format string
		Input  installData
		Skill  sc.SkillID
	}{"canonical-v1", input, skill})
	if err != nil {
		return "", err
	}
	return digest(raw), nil
}

func (r operationRecord) validate() error {
	if r.ID.Validate() != nil || r.SkillID.Validate() != nil || r.Version.Validate() != nil || r.Input.PackageDigest.Validate() != nil || r.Input.ManifestDigest.Validate() != nil {
		return fail(f.InvalidState)
	}
	if _, err := r.Input.Binding.request(); err != nil {
		return err
	}
	name, err := sc.NormalizeName(r.Input.NormalizedName)
	if err != nil || name != r.Input.NormalizedName || name == skill.AddSkillsNormalizedName {
		return fail(f.InvalidState)
	}
	key, err := builtin.SkillInstallKey(r.ID.String())
	if err != nil || key != r.Key {
		return fail(f.InvalidState)
	}
	raw, err := encode(r.Input)
	if err != nil || digest(raw) != r.InputDigest {
		return fail(f.InvalidState)
	}
	fp, err := fingerprint(r.Input, r.SkillID)
	if err != nil || fp != r.Fingerprint {
		return fail(f.InvalidState)
	}
	// This first reader must refuse states whose result/recovery contract has
	// not yet been implemented, rather than inventing a terminal replay.
	if r.Phase != "created" || r.Version != 1 {
		return fail(f.InvalidState)
	}
	return nil
}

// PreparedOperation is an observed metadata fact, not an authorization or an
// attempt permit. The complete typed value is immutable and safe by default.
type PreparedOperation struct{ data func() operationRecord }
type PreparedFacts struct {
	ID          tc.OperationID
	SkillID     sc.SkillID
	Key         f.IdempotencyKey
	Fingerprint f.Digest
	Version     f.Version
}

func (v PreparedOperation) Facts() (PreparedFacts, error) {
	if v.data == nil {
		return PreparedFacts{}, fail(f.InvalidState)
	}
	r := v.data()
	if err := r.validate(); err != nil {
		return PreparedFacts{}, err
	}
	return PreparedFacts{r.ID, r.SkillID, r.Key, r.Fingerprint, r.Version}, nil
}
func (PreparedOperation) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "prepared_tool_operation")
}
func (PreparedOperation) LogValue() slog.Value { return slog.StringValue("prepared_tool_operation") }
func (PreparedOperation) MarshalJSON() ([]byte, error) {
	return []byte(`"prepared_tool_operation"`), nil
}
func (PreparedFacts) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "prepared_tool_facts") }
func (PreparedFacts) LogValue() slog.Value         { return slog.StringValue("prepared_tool_facts") }
func (PreparedFacts) MarshalJSON() ([]byte, error) { return []byte(`"prepared_tool_facts"`), nil }
