package runtime

import (
	"bytes"

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

func writeFileProfileFixture(t *testing.T) writeFileInput {
	t.Helper()
	tool := newOperationID[i.Tool](t)
	return writeFileInput{
		Format: 1, Operation: "write-file", Revision: "1", ProtocolMajor: 1,
		Binding: writeFileBindingData{
			Project: newOperationID[i.Project](t), Agent: newOperationID[i.Agent](t), Execution: newOperationID[i.Execution](t),
			Round: newOperationID[struct{}](t).String(), Snapshot: newOperationID[struct{}](t).String(), InputBinding: newOperationID[struct{}](t).String(),
			LogicalCall: newOperationID[model.Call](t), Invocation: newOperationID[model.Invocation](t), Call: "original-write-file",
			ModelName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), Spec: tc.SpecRef{ToolID: tool, SpecRevision: 1},
			Payload: newOperationID[struct{}](t).String(), PayloadDigest: digest([]byte("private-arguments")), PayloadBytes: 17,
			ArgumentsDigest: digest([]byte("canonical-arguments")),
		},
		Target: writeFileTarget{Mount: newOperationID[i.Mount](t), Workspace: newOperationID[mount.Workspace](t), Runner: newOperationID[runner.Runner](t), WorkspaceName: "workspace-read", AgentVersion: 9007199254740993, MountVersion: 3},
	}
}

func TestWriteFileOperationInputClosedProfile(t *testing.T) {
	v := writeFileProfileFixture(t)
	raw, err := encodeWriteFileInput(v)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeWriteFileInput(raw)
	if err != nil || got != v || bytes.Contains(raw, []byte("private-arguments")) || bytes.Contains(raw, []byte("Skill")) {
		t.Fatal("write-file metadata did not preserve only its exact reference profile")
	}
	for name, bad := range map[string][]byte{
		"unknown-field":   append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"unexpected":true}`)...),
		"duplicate-field": bytes.Replace(raw, []byte(`"format_version":1`), []byte(`"format_version":1,"format_version":1`), 1),
		"second-document": append(append([]byte{}, raw...), []byte(`{}`)...),
		"null-binding":    bytes.Replace(raw, []byte(`"Binding":{`), []byte(`"Binding":null,"Binding":{`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeWriteFileInput(bad); err == nil {
				t.Fatal("noncanonical metadata accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*writeFileInput){
		"other-operation": func(v *writeFileInput) { v.Operation = "execute-command" },
		"other-revision":  func(v *writeFileInput) { v.Revision = "2" },
		"host-path":       func(v *writeFileInput) { v.Target.WorkspaceName = "/tmp/private" },
		"missing-target":  func(v *writeFileInput) { v.Target.Workspace = mount.WorkspaceID{} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := v
			mutate(&bad)
			if _, err := encodeWriteFileInput(bad); err == nil {
				t.Fatal("unsupported or incomplete write-file profile accepted")
			}
		})
	}
}

func TestWriteFileOperationFingerprintAndRecord(t *testing.T) {
	v := writeFileProfileFixture(t)
	large := v
	large.Binding.PayloadBytes = p.MaxMessageBytes
	if _, err := encodeWriteFileInput(large); err != nil {
		t.Fatal("write metadata rejected its separate large argument budget")
	}
	large.Binding.PayloadBytes++
	if _, err := encodeWriteFileInput(large); err == nil {
		t.Fatal("write payload reference exceeded its independent bound")
	}
	want, err := writeFileFingerprint(v)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*writeFileInput){
		"arguments":      func(v *writeFileInput) { v.Binding.ArgumentsDigest = digest([]byte("different")) },
		"project":        func(v *writeFileInput) { v.Binding.Project = newOperationID[i.Project](t) },
		"mount":          func(v *writeFileInput) { v.Target.Mount = newOperationID[i.Mount](t) },
		"workspace":      func(v *writeFileInput) { v.Target.Workspace = newOperationID[mount.Workspace](t) },
		"runner":         func(v *writeFileInput) { v.Target.Runner = newOperationID[runner.Runner](t) },
		"workspace-name": func(v *writeFileInput) { v.Target.WorkspaceName = "different-workspace" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := v
			mutate(&changed)
			got, err := writeFileFingerprint(changed)
			if err != nil || got == want {
				t.Fatal("fingerprint lost trusted input or target identity")
			}
		})
	}
	changed := v
	changed.Target.AgentVersion++
	changed.Target.MountVersion++
	if got, err := writeFileFingerprint(changed); err != nil || got != want {
		t.Fatal("admission version changed the logical target fingerprint")
	}
	raw, _ := encodeWriteFileInput(v)
	r := writeFileOperationRecord{ID: newOperationID[tc.Operation](t), Input: v, InputDigest: digest(raw), Fingerprint: want, Phase: "created", Version: 1}
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

func TestWriteFileStoredPublicationResultAndIdentity(t *testing.T) {
	operation, attempt := newOperationID[tc.Operation](t), newOperationID[tc.Attempt](t)
	args := p.WriteFileArguments{Path: "private-write.bin", Data: bytes.Repeat([]byte{0xab}, p.MaxWriteFileBytes), Overwrite: true}
	code := p.InternalError
	for _, result := range []p.WriteFileResult{
		{Path: args.Path, State: "committed", BytesWritten: len(args.Data), Durable: true},
		{Path: args.Path, State: "committed", BytesWritten: len(args.Data), Durable: false, CompletionCode: &code},
		{Path: args.Path, State: "committed", BytesWritten: len(args.Data), Durable: true, CompletionCode: &code},
	} {
		ref, raw, err := encodeWriteFileStoredResult(operation, attempt, result, args)
		if err != nil {
			t.Fatal(err)
		}
		got, err := decodeWriteFileStoredResult(ref, raw, operation, attempt, args)
		gotRaw, _ := p.EncodeWriteFileResult(got)
		if err != nil || !bytes.Equal(gotRaw, raw) || got.Durable != result.Durable || got.State != "committed" {
			t.Fatal("publication/durability/completion changed")
		}
		for _, mutate := range []func(*writeFileResultReference){
			func(v *writeFileResultReference) { v.Attempt = newOperationID[tc.Attempt](t) },
			func(v *writeFileResultReference) { v.Operation = newOperationID[tc.Operation](t) },
			func(v *writeFileResultReference) { v.Bytes++ },
			func(v *writeFileResultReference) { v.SHA256 = digest([]byte("substitution")) },
		} {
			bad := ref
			mutate(&bad)
			if _, err := decodeWriteFileStoredResult(bad, raw, operation, attempt, args); err == nil {
				t.Fatal("foreign result binding accepted")
			}
		}
		badArgs := args
		badArgs.Path = "other.bin"
		if _, err := decodeWriteFileStoredResult(ref, raw, operation, attempt, badArgs); err == nil {
			t.Fatal("wrong write path accepted")
		}
		badArgs = args
		badArgs.Data = badArgs.Data[:len(badArgs.Data)-1]
		if _, err := decodeWriteFileStoredResult(ref, raw, operation, attempt, badArgs); err == nil {
			t.Fatal("wrong write length accepted")
		}
	}
}
