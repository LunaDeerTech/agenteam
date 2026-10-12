package runtime

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	model "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mount "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	runner "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

const writeFileOperationKind = "runner.write-file"

// This format is separate from bindingData/installData. Neither adding Runner
// support nor reading a new row changes the original install encoding/digest.
// These private records are not authority and cannot be supplied to dispatch.
type writeFileBindingData struct {
	Project                       i.ProjectID
	Agent                         i.AgentID
	Execution                     i.ExecutionID
	Round, Snapshot, InputBinding string
	LogicalCall                   model.CallID
	Invocation                    model.InvocationID
	Call, ModelName               string
	Spec                          tc.SpecRef
	Payload                       string
	PayloadDigest                 f.Digest
	PayloadBytes                  f.Progress
	ArgumentsDigest               f.Digest
}

func (b writeFileBindingData) validate() error {
	if b.Project.Validate() != nil || b.Agent.Validate() != nil || b.Execution.Validate() != nil ||
		b.LogicalCall.Validate() != nil || b.Invocation.Validate() != nil || b.Spec.Validate() != nil ||
		b.PayloadDigest.Validate() != nil || b.ArgumentsDigest.Validate() != nil || b.PayloadBytes < 1 || b.PayloadBytes > p.MaxMessageBytes {
		return fail(f.InvalidState)
	}
	for _, value := range []string{b.Round, b.Snapshot, b.InputBinding, b.Payload} {
		if _, err := f.ParseID[struct{}](value); err != nil {
			return fail(f.InvalidState)
		}
	}
	if len(b.Call) == 0 || len(b.Call) > 256 || !utf8.ValidString(b.Call) || strings.ContainsRune(b.Call, 0) ||
		b.ModelName != "tool_"+strings.ReplaceAll(b.Spec.ToolID.String(), "-", "") {
		return fail(f.InvalidState)
	}
	return nil
}

type writeFileTarget struct {
	Mount         i.MountID
	Workspace     mount.WorkspaceID
	Runner        runner.RunnerID
	WorkspaceName string
	AgentVersion  f.Version
	MountVersion  f.Version
}

func (v writeFileTarget) validate() error {
	if v.Mount.Validate() != nil || v.Workspace.Validate() != nil || v.Runner.Validate() != nil ||
		!mount.ValidWorkspaceName(v.WorkspaceName) || v.AgentVersion.Validate() != nil || v.MountVersion.Validate() != nil {
		return fail(f.InvalidState)
	}
	return nil
}

// Only identities and original protected-input references are persisted here.
// The actual relative path and file bytes remain owned by Execution. Target
// fields must come from the real current Mount resolver, never model arguments.
type writeFileInput struct {
	Format        int `json:"format_version"`
	Binding       writeFileBindingData
	Operation     string
	Revision      string
	ProtocolMajor int
	Target        writeFileTarget
}

func (v writeFileInput) validate() error {
	if v.Format != 1 || v.Operation != p.WriteFileOperation || v.Revision != string(p.WriteFileRevision) || v.ProtocolMajor != 1 ||
		v.Binding.validate() != nil || v.Target.validate() != nil {
		return fail(f.InvalidState)
	}
	return nil
}

func encodeWriteFileInput(v writeFileInput) ([]byte, error) {
	if err := v.validate(); err != nil {
		return nil, err
	}
	return encode(v)
}

func decodeWriteFileInput(raw []byte) (writeFileInput, error) {
	var v writeFileInput
	if len(raw) < 2 || len(raw) > 32768 {
		return v, fail(f.InvalidState)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return writeFileInput{}, fail(f.InvalidState)
	}
	canonical, err := encodeWriteFileInput(v)
	if err != nil || !bytes.Equal(raw, canonical) {
		return writeFileInput{}, fail(f.InvalidState)
	}
	return v, nil
}

func writeFileFingerprint(v writeFileInput) (f.Digest, error) {
	if err := v.validate(); err != nil {
		return "", err
	}
	b := v.Binding
	// Current config versions are admission observations, not new logical targets.
	// Credential/connection/request/attempt identities never enter a fingerprint.
	raw, err := encode(struct {
		Format        string
		Project       i.ProjectID
		Agent         i.AgentID
		Execution     i.ExecutionID
		Spec          tc.SpecRef
		Arguments     f.Digest
		Operation     string
		Revision      string
		ProtocolMajor int
		Mount         i.MountID
		Workspace     mount.WorkspaceID
		Runner        runner.RunnerID
		WorkspaceName string
	}{"canonical-v1", b.Project, b.Agent, b.Execution, b.Spec, b.ArgumentsDigest, v.Operation, v.Revision, v.ProtocolMajor,
		v.Target.Mount, v.Target.Workspace, v.Target.Runner, v.Target.WorkspaceName})
	if err != nil {
		return "", err
	}
	raw, err = cursor.CanonicalJSON(raw)
	if err != nil {
		return "", fail(f.InvalidState)
	}
	return digest(raw), nil
}

type writeFileOperationRecord struct {
	ID          tc.OperationID
	Input       writeFileInput
	InputDigest f.Digest
	Fingerprint f.Digest
	Phase       string
	Version     f.Version
	Attempt     tc.AttemptID
}

func (r writeFileOperationRecord) validate() error {
	raw, err := encodeWriteFileInput(r.Input)
	if err != nil || r.ID.Validate() != nil || digest(raw) != r.InputDigest {
		return fail(f.InvalidState)
	}
	fingerprint, err := writeFileFingerprint(r.Input)
	if err != nil || fingerprint != r.Fingerprint {
		return fail(f.InvalidState)
	}
	switch r.Phase {
	case "created":
		if r.Version != 1 || r.Attempt != (tc.AttemptID{}) {
			return fail(f.InvalidState)
		}
	case "running":
		if r.Version != 2 || r.Attempt.Validate() != nil {
			return fail(f.InvalidState)
		}
	case "succeeded", "failed", "cancelled":
		if r.Version != 3 || r.Attempt.Validate() != nil {
			return fail(f.InvalidState)
		}
	default:
		return fail(f.InvalidState)
	}
	return nil
}

type writeFileResultReference struct {
	Operation tc.OperationID
	Attempt   tc.AttemptID
	SHA256    f.Digest
	Bytes     f.Progress
}

func (r writeFileResultReference) validate() error {
	if r.Operation.Validate() != nil || r.Attempt.Validate() != nil || r.SHA256.Validate() != nil || r.Bytes < 2 || r.Bytes > 32768 {
		return fail(f.InvalidState)
	}
	return nil
}

// This explicit encoding is the only payload representation. Default protocol
// formatting/JSON remains redacted; storage does not introduce an HTTP reader.
func encodeWriteFileStoredResult(operation tc.OperationID, attempt tc.AttemptID, result p.WriteFileResult, arguments p.WriteFileArguments) (writeFileResultReference, []byte, error) {
	if operation.Validate() != nil || attempt.Validate() != nil || arguments.Validate() != nil || result.Validate() != nil ||
		result.Path != arguments.Path || result.BytesWritten != len(arguments.Data) {
		return writeFileResultReference{}, nil, fail(f.InvalidState)
	}
	raw, err := p.EncodeWriteFileResult(result)
	if err != nil || len(raw) > 32768 {
		return writeFileResultReference{}, nil, fail(f.InvalidState)
	}
	ref := writeFileResultReference{operation, attempt, digest(raw), f.Progress(len(raw))}
	return ref, raw, nil
}

func decodeWriteFileStoredResult(ref writeFileResultReference, raw []byte, operation tc.OperationID, attempt tc.AttemptID, arguments p.WriteFileArguments) (p.WriteFileResult, error) {
	if ref.validate() != nil || ref.Operation != operation || ref.Attempt != attempt || int64(len(raw)) != int64(ref.Bytes) || digest(raw) != ref.SHA256 {
		return p.WriteFileResult{}, fail(f.InvalidState)
	}
	result, err := p.DecodeWriteFileResult(raw)
	if err != nil {
		return p.WriteFileResult{}, fail(f.InvalidState)
	}
	want, canonical, err := encodeWriteFileStoredResult(operation, attempt, result, arguments)
	if err != nil || want != ref || !bytes.Equal(canonical, raw) {
		return p.WriteFileResult{}, fail(f.InvalidState)
	}
	return result, nil
}
