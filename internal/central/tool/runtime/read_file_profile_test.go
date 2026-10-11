package runtime

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	model "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mount "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	runner "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func readFileProfileFixture(t *testing.T) readFileInput {
	t.Helper()
	tool := newOperationID[i.Tool](t)
	return readFileInput{
		Format: 1, Operation: "read-file", Revision: "1", ProtocolMajor: 1,
		Binding: readFileBindingData{
			Project: newOperationID[i.Project](t), Agent: newOperationID[i.Agent](t), Execution: newOperationID[i.Execution](t),
			Round: newOperationID[struct{}](t).String(), Snapshot: newOperationID[struct{}](t).String(), InputBinding: newOperationID[struct{}](t).String(),
			LogicalCall: newOperationID[model.Call](t), Invocation: newOperationID[model.Invocation](t), Call: "original-read-file",
			ModelName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), Spec: tc.SpecRef{ToolID: tool, SpecRevision: 1},
			Payload: newOperationID[struct{}](t).String(), PayloadDigest: digest([]byte("private-arguments")), PayloadBytes: 17,
			ArgumentsDigest: digest([]byte("canonical-arguments")),
		},
		Target: readFileTarget{Mount: newOperationID[i.Mount](t), Workspace: newOperationID[mount.Workspace](t), Runner: newOperationID[runner.Runner](t), WorkspaceName: "workspace-read", AgentVersion: 9007199254740993, MountVersion: 3},
	}
}

func TestReadFileOperationInputClosedProfile(t *testing.T) {
	v := readFileProfileFixture(t)
	raw, err := encodeReadFileInput(v)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeReadFileInput(raw)
	if err != nil || got != v || bytes.Contains(raw, []byte("private-arguments")) || bytes.Contains(raw, []byte("Skill")) {
		t.Fatal("read-file metadata did not preserve only its exact reference profile")
	}
	for name, bad := range map[string][]byte{
		"unknown-field": append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"unexpected":true}`)...),
		"duplicate-field": bytes.Replace(raw, []byte(`"format_version":1`), []byte(`"format_version":1,"format_version":1`), 1),
		"second-document": append(append([]byte{}, raw...), []byte(`{}`)...),
		"null-binding": bytes.Replace(raw, []byte(`"Binding":{`), []byte(`"Binding":null,"Binding":{`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeReadFileInput(bad); err == nil {
				t.Fatal("noncanonical metadata accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*readFileInput){
		"other-operation": func(v *readFileInput) { v.Operation = "execute-command" },
		"other-revision": func(v *readFileInput) { v.Revision = "2" },
		"host-path": func(v *readFileInput) { v.Target.WorkspaceName = "/tmp/private" },
		"missing-target": func(v *readFileInput) { v.Target.Workspace = mount.WorkspaceID{} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := v
			mutate(&bad)
			if _, err := encodeReadFileInput(bad); err == nil {
				t.Fatal("unsupported or incomplete read-file profile accepted")
			}
		})
	}
}

func TestReadFileOperationFingerprintAndRecord(t *testing.T) {
	v := readFileProfileFixture(t)
	want, err := readFileFingerprint(v)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*readFileInput){
		"arguments": func(v *readFileInput) { v.Binding.ArgumentsDigest = digest([]byte("different")) },
		"project": func(v *readFileInput) { v.Binding.Project = newOperationID[i.Project](t) },
		"mount": func(v *readFileInput) { v.Target.Mount = newOperationID[i.Mount](t) },
		"workspace": func(v *readFileInput) { v.Target.Workspace = newOperationID[mount.Workspace](t) },
		"runner": func(v *readFileInput) { v.Target.Runner = newOperationID[runner.Runner](t) },
		"workspace-name": func(v *readFileInput) { v.Target.WorkspaceName = "different-workspace" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := v
			mutate(&changed)
			got, err := readFileFingerprint(changed)
			if err != nil || got == want {
				t.Fatal("fingerprint lost trusted input or target identity")
			}
		})
	}
	changed := v
	changed.Target.AgentVersion++
	changed.Target.MountVersion++
	if got, err := readFileFingerprint(changed); err != nil || got != want {
		t.Fatal("admission version changed the logical target fingerprint")
	}
	raw, _ := encodeReadFileInput(v)
	r := readFileOperationRecord{ID: newOperationID[tc.Operation](t), Input: v, InputDigest: digest(raw), Fingerprint: want, Phase: "created", Version: 1}
	if r.validate() != nil {
		t.Fatal("valid created record rejected")
	}
	r.Phase, r.Version = "running", 2
	if r.validate() == nil {
		t.Fatal("running without original attempt accepted")
	}
	r.Attempt = newOperationID[tc.Attempt](t)
	if r.validate() != nil {
		t.Fatal("valid running record rejected")
	}
	r.Input.Binding.PayloadDigest = digest([]byte("substituted-input"))
	if r.validate() == nil {
		t.Fatal("changed original payload accepted under old metadata digest")
	}
}

func TestReadFileStoredResultFullRangeAndBinding(t *testing.T) {
	operation, attempt := newOperationID[tc.Operation](t), newOperationID[tc.Attempt](t)
	for _, encoding := range []string{"utf8", "base64"} {
		t.Run(encoding, func(t *testing.T) {
			// NUL is legal file data and forces the largest JSON escaping factor.
			data := strings.Repeat("\x00", p.MaxReadFileBytes)
			if encoding == "base64" {
				data = base64.StdEncoding.EncodeToString([]byte(data))
			}
			arguments := p.ReadFileArguments{Path: "fixture.bin", Offset: 0, Length: p.MaxReadFileBytes, Encoding: encoding}
			result := p.ReadFileResult{Path: arguments.Path, Offset: arguments.Offset, BytesRead: arguments.Length, EOF: true, Encoding: encoding, Data: data}
			ref, raw, err := encodeReadFileStoredResult(operation, attempt, result, arguments)
			if err != nil || len(raw) <= 32768 || len(raw) > p.MaxReadFileResultBytes {
				t.Fatal("full legal file result did not fit its dedicated payload")
			}
			got, err := decodeReadFileStoredResult(ref, raw, operation, attempt, arguments)
			if err != nil || got != result {
				t.Fatal("stored result was changed or truncated")
			}
			if _, err := decodeReadFileStoredResult(ref, raw, newOperationID[tc.Operation](t), attempt, arguments); err == nil {
				t.Fatal("foreign operation result accepted")
			}
			if _, err := decodeReadFileStoredResult(ref, raw, operation, newOperationID[tc.Attempt](t), arguments); err == nil {
				t.Fatal("foreign attempt result accepted")
			}
			wrong := arguments
			wrong.Offset++
			if _, err := decodeReadFileStoredResult(ref, raw, operation, attempt, wrong); err == nil {
				t.Fatal("result of a different read accepted")
			}
			corrupt := bytes.Clone(raw)
			corrupt[len(corrupt)-2] ^= 1
			if _, err := decodeReadFileStoredResult(ref, corrupt, operation, attempt, arguments); err == nil {
				t.Fatal("payload digest mismatch accepted")
			}
			badRef := ref
			badRef.Bytes = f.Progress(len(raw) - 1)
			if _, err := decodeReadFileStoredResult(badRef, raw, operation, attempt, arguments); err == nil {
				t.Fatal("payload length mismatch accepted")
			}
		})
	}
}
