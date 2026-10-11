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

const readFileOperationKind = "runner.read-file"

// This format is separate from bindingData/installData. Neither adding Runner
// support nor reading a new row changes the original install encoding/digest.
// These private records are not authority and cannot be supplied to dispatch.
type readFileBindingData struct {
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

func (b readFileBindingData) validate() error {
	if b.Project.Validate() != nil || b.Agent.Validate() != nil || b.Execution.Validate() != nil ||
		b.LogicalCall.Validate() != nil || b.Invocation.Validate() != nil || b.Spec.Validate() != nil ||
		b.PayloadDigest.Validate() != nil || b.ArgumentsDigest.Validate() != nil || b.PayloadBytes < 1 || b.PayloadBytes > 32<<10 {
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

type readFileTarget struct {
	Mount         i.MountID
	Workspace     mount.WorkspaceID
	Runner        runner.RunnerID
	WorkspaceName string
	AgentVersion  f.Version
	MountVersion  f.Version
}

func (v readFileTarget) validate() error {
	if v.Mount.Validate() != nil || v.Workspace.Validate() != nil || v.Runner.Validate() != nil ||
		!mount.ValidWorkspaceName(v.WorkspaceName) || v.AgentVersion.Validate() != nil || v.MountVersion.Validate() != nil {
		return fail(f.InvalidState)
	}
	return nil
}

// Only identities and original protected-input references are persisted here.
// The actual relative path/read arguments remain owned by Execution. Target
// fields must come from the real current Mount resolver, never model arguments.
type readFileInput struct {
	Format        int `json:"format_version"`
	Binding       readFileBindingData
	Operation     string
	Revision      string
	ProtocolMajor int
	Target        readFileTarget
}

func (v readFileInput) validate() error {
	if v.Format != 1 || v.Operation != p.ReadFileOperation || v.Revision != string(p.ReadFileRevision) || v.ProtocolMajor != 1 ||
		v.Binding.validate() != nil || v.Target.validate() != nil {
		return fail(f.InvalidState)
	}
	return nil
}

func encodeReadFileInput(v readFileInput) ([]byte, error) {
	if err := v.validate(); err != nil {
		return nil, err
	}
	return encode(v)
}

func decodeReadFileInput(raw []byte) (readFileInput, error) {
	var v readFileInput
	if len(raw) < 2 || len(raw) > 32768 {
		return v, fail(f.InvalidState)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return readFileInput{}, fail(f.InvalidState)
	}
	canonical, err := encodeReadFileInput(v)
	if err != nil || !bytes.Equal(raw, canonical) {
		return readFileInput{}, fail(f.InvalidState)
	}
	return v, nil
}

func readFileFingerprint(v readFileInput) (f.Digest, error) {
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

type readFileOperationRecord struct {
	ID          tc.OperationID
	Input       readFileInput
	InputDigest f.Digest
	Fingerprint f.Digest
	Phase       string
	Version     f.Version
	Attempt     tc.AttemptID
}

func (r readFileOperationRecord) validate() error {
	raw, err := encodeReadFileInput(r.Input)
	if err != nil || r.ID.Validate() != nil || digest(raw) != r.InputDigest {
		return fail(f.InvalidState)
	}
	fingerprint, err := readFileFingerprint(r.Input)
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

type readFileResultReference struct {
	Operation tc.OperationID
	Attempt   tc.AttemptID
	SHA256    f.Digest
	Bytes     f.Progress
}

func (r readFileResultReference) validate() error {
	if r.Operation.Validate() != nil || r.Attempt.Validate() != nil || r.SHA256.Validate() != nil || r.Bytes < 2 || r.Bytes > p.MaxReadFileResultBytes {
		return fail(f.InvalidState)
	}
	return nil
}

// This explicit encoding is the only payload representation. Default protocol
// formatting/JSON remains redacted; storage does not introduce an HTTP reader.
func encodeReadFileStoredResult(operation tc.OperationID, attempt tc.AttemptID, result p.ReadFileResult, arguments p.ReadFileArguments) (readFileResultReference, []byte, error) {
	if operation.Validate() != nil || attempt.Validate() != nil || arguments.Validate() != nil || result.Validate() != nil ||
		result.Path != arguments.Path || result.Offset != arguments.Offset || result.Encoding != arguments.Encoding ||
		result.BytesRead > arguments.Length || !result.EOF && result.BytesRead != arguments.Length {
		return readFileResultReference{}, nil, fail(f.InvalidState)
	}
	raw, err := p.EncodeReadFileResult(result)
	if err != nil {
		return readFileResultReference{}, nil, fail(f.InvalidState)
	}
	ref := readFileResultReference{operation, attempt, digest(raw), f.Progress(len(raw))}
	return ref, raw, nil
}

func decodeReadFileStoredResult(ref readFileResultReference, raw []byte, operation tc.OperationID, attempt tc.AttemptID, arguments p.ReadFileArguments) (p.ReadFileResult, error) {
	if ref.validate() != nil || ref.Operation != operation || ref.Attempt != attempt || int64(len(raw)) != int64(ref.Bytes) || digest(raw) != ref.SHA256 {
		return p.ReadFileResult{}, fail(f.InvalidState)
	}
	result, err := p.DecodeReadFileResult(raw)
	if err != nil {
		return p.ReadFileResult{}, fail(f.InvalidState)
	}
	want, canonical, err := encodeReadFileStoredResult(operation, attempt, result, arguments)
	if err != nil || want != ref || !bytes.Equal(canonical, raw) {
		return p.ReadFileResult{}, fail(f.InvalidState)
	}
	return result, nil
}
