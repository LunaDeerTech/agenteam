package runtime

import (
	"bytes"
	"encoding/json"
	"strconv"
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

const runCommandOperationKind = "runner.run-command"

// This format is separate from bindingData/installData. Neither adding Runner
// support nor reading a new row changes the original install encoding/digest.
// These private records are not authority and cannot be supplied to dispatch.
type runCommandBindingData struct {
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

func (b runCommandBindingData) validate() error {
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

type runCommandTarget struct {
	Mount         i.MountID
	Workspace     mount.WorkspaceID
	Runner        runner.RunnerID
	WorkspaceName string
	AgentVersion  f.Version
	MountVersion  f.Version
}

func (v runCommandTarget) validate() error {
	if v.Mount.Validate() != nil || v.Workspace.Validate() != nil || v.Runner.Validate() != nil ||
		!mount.ValidWorkspaceName(v.WorkspaceName) || v.AgentVersion.Validate() != nil || v.MountVersion.Validate() != nil {
		return fail(f.InvalidState)
	}
	return nil
}

// Only identities and original protected-input references are persisted here.
// The actual argv and cwd remain owned by Execution. Target
// fields must come from the real current Mount resolver, never model arguments.
type runCommandInput struct {
	Format        int `json:"format_version"`
	Binding       runCommandBindingData
	Operation     string
	Revision      string
	ProtocolMajor int
	Target        runCommandTarget
}

func (v runCommandInput) validate() error {
	if v.Format != 1 || v.Operation != p.RunCommandOperation || v.Revision != string(p.RunCommandRevision) || v.ProtocolMajor != 1 ||
		v.Binding.validate() != nil || v.Target.validate() != nil {
		return fail(f.InvalidState)
	}
	return nil
}

func encodeRunCommandInput(v runCommandInput) ([]byte, error) {
	if err := v.validate(); err != nil {
		return nil, err
	}
	return encode(v)
}

func decodeRunCommandInput(raw []byte) (runCommandInput, error) {
	var v runCommandInput
	if len(raw) < 2 || len(raw) > 32768 {
		return v, fail(f.InvalidState)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&v); err != nil {
		return runCommandInput{}, fail(f.InvalidState)
	}
	canonical, err := encodeRunCommandInput(v)
	if err != nil || !bytes.Equal(raw, canonical) {
		return runCommandInput{}, fail(f.InvalidState)
	}
	return v, nil
}

func runCommandFingerprint(v runCommandInput) (f.Digest, error) {
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

type runCommandOperationRecord struct {
	ID          tc.OperationID
	Input       runCommandInput
	InputDigest f.Digest
	Fingerprint f.Digest
	Phase       string
	Version     f.Version
	Attempt     tc.AttemptID
}

func (r runCommandOperationRecord) validate() error {
	raw, err := encodeRunCommandInput(r.Input)
	if err != nil || r.ID.Validate() != nil || digest(raw) != r.InputDigest {
		return fail(f.InvalidState)
	}
	fingerprint, err := runCommandFingerprint(r.Input)
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
		if r.Phase == "failed" && r.Version == 2 && r.Attempt == (tc.AttemptID{}) {
			return nil
		}
		if r.Version != 3 || r.Attempt.Validate() != nil {
			return fail(f.InvalidState)
		}
	default:
		return fail(f.InvalidState)
	}
	return nil
}

// References bind separately bounded report metadata and both original streams.
// Full output never enters operations.terminal_data or a generic safe message.
type runCommandResultReference struct {
	Operation    tc.OperationID
	Attempt      tc.AttemptID
	Request      p.ID
	SHA256       f.Digest
	Bytes        f.Progress
	StdoutSHA256 f.Digest
	StdoutBytes  f.Progress
	StderrSHA256 f.Digest
	StderrBytes  f.Progress
}

func (r runCommandResultReference) validate() error {
	if r.Operation.Validate() != nil || r.Attempt.Validate() != nil || !r.Request.Valid() || r.SHA256.Validate() != nil || r.Bytes < 2 || r.Bytes > 32768 ||
		r.StdoutSHA256.Validate() != nil || r.StderrSHA256.Validate() != nil || r.StdoutBytes < 0 || r.StdoutBytes > p.MaxRunCommandStreamBytes || r.StderrBytes < 0 || r.StderrBytes > p.MaxRunCommandStreamBytes {
		return fail(f.InvalidState)
	}
	return nil
}

func encodeRunCommandStoredResult(operation tc.OperationID, attempt tc.AttemptID, request p.ID, result p.RunCommandResult, stdout, stderr []byte) (runCommandResultReference, []byte, error) {
	if operation.Validate() != nil || attempt.Validate() != nil || !request.Valid() || result.Validate() != nil || !utf8.Valid(stdout) || !utf8.Valid(stderr) || len(stdout) > p.MaxRunCommandStreamBytes || len(stderr) > p.MaxRunCommandStreamBytes ||
		string(result.StdoutBytes) != strconv.Itoa(len(stdout)) || string(result.StderrBytes) != strconv.Itoa(len(stderr)) {
		return runCommandResultReference{}, nil, fail(f.InvalidState)
	}
	raw, err := p.EncodeRunCommandResult(result)
	if err != nil || len(raw) > 32768 {
		return runCommandResultReference{}, nil, fail(f.InvalidState)
	}
	ref := runCommandResultReference{Operation: operation, Attempt: attempt, Request: request, SHA256: digest(raw), Bytes: f.Progress(len(raw)), StdoutSHA256: digest(stdout), StdoutBytes: f.Progress(len(stdout)), StderrSHA256: digest(stderr), StderrBytes: f.Progress(len(stderr))}
	return ref, raw, nil
}

func decodeRunCommandStoredResult(ref runCommandResultReference, raw, stdout, stderr []byte, operation tc.OperationID, attempt tc.AttemptID, request p.ID) (p.RunCommandResult, error) {
	if ref.validate() != nil || ref.Operation != operation || ref.Attempt != attempt || ref.Request != request || int64(len(raw)) != int64(ref.Bytes) || digest(raw) != ref.SHA256 ||
		int64(len(stdout)) != int64(ref.StdoutBytes) || digest(stdout) != ref.StdoutSHA256 || int64(len(stderr)) != int64(ref.StderrBytes) || digest(stderr) != ref.StderrSHA256 {
		return p.RunCommandResult{}, fail(f.InvalidState)
	}
	result, err := p.DecodeRunCommandResult(raw)
	if err != nil {
		return p.RunCommandResult{}, fail(f.InvalidState)
	}
	want, canonical, err := encodeRunCommandStoredResult(operation, attempt, request, result, stdout, stderr)
	if err != nil || want != ref || !bytes.Equal(canonical, raw) {
		return p.RunCommandResult{}, fail(f.InvalidState)
	}
	return result, nil
}

// A known report is not necessarily a successful command. Code selection never
// consumes stderr or program text. The original flags remain in the report.
func classifyRunCommandResult(result p.RunCommandResult) (outcome, code string) {
	return tc.ClassifyRunCommandResult(result)
}
