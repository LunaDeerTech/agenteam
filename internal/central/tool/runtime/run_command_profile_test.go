package runtime

import (
	"bytes"

	"strconv"
	"strings"
	"testing"

	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	model "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	mount "github.com/LunaDeerTech/agenteam/internal/central/mount/contract"
	runner "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func runCommandProfileFixture(t *testing.T) runCommandInput {
	t.Helper()
	tool := newOperationID[i.Tool](t)
	return runCommandInput{
		Format: 1, Operation: "run-command", Revision: "1", ProtocolMajor: 1,
		Binding: runCommandBindingData{
			Project: newOperationID[i.Project](t), Agent: newOperationID[i.Agent](t), Execution: newOperationID[i.Execution](t),
			Round: newOperationID[struct{}](t).String(), Snapshot: newOperationID[struct{}](t).String(), InputBinding: newOperationID[struct{}](t).String(),
			LogicalCall: newOperationID[model.Call](t), Invocation: newOperationID[model.Invocation](t), Call: "original-run-command",
			ModelName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), Spec: tc.SpecRef{ToolID: tool, SpecRevision: 1},
			Payload: newOperationID[struct{}](t).String(), PayloadDigest: digest([]byte("private-arguments")), PayloadBytes: 17,
			ArgumentsDigest: digest([]byte("canonical-arguments")),
		},
		Target: runCommandTarget{Mount: newOperationID[i.Mount](t), Workspace: newOperationID[mount.Workspace](t), Runner: newOperationID[runner.Runner](t), WorkspaceName: "workspace-command", AgentVersion: 9007199254740993, MountVersion: 3},
	}
}

func TestRunCommandOperationInputClosedProfile(t *testing.T) {
	v := runCommandProfileFixture(t)
	raw, err := encodeRunCommandInput(v)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeRunCommandInput(raw)
	if err != nil || got != v || bytes.Contains(raw, []byte("private-arguments")) || bytes.Contains(raw, []byte("Skill")) {
		t.Fatal("run-command metadata did not preserve only its exact reference profile")
	}
	for name, bad := range map[string][]byte{
		"unknown-field":   append(append([]byte{}, raw[:len(raw)-1]...), []byte(`,"unexpected":true}`)...),
		"duplicate-field": bytes.Replace(raw, []byte(`"format_version":1`), []byte(`"format_version":1,"format_version":1`), 1),
		"second-document": append(append([]byte{}, raw...), []byte(`{}`)...),
		"null-binding":    bytes.Replace(raw, []byte(`"Binding":{`), []byte(`"Binding":null,"Binding":{`), 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeRunCommandInput(bad); err == nil {
				t.Fatal("noncanonical metadata accepted")
			}
		})
	}
	for name, mutate := range map[string]func(*runCommandInput){
		"read-file":      func(v *runCommandInput) { v.Operation = "read-file" },
		"write-file":     func(v *runCommandInput) { v.Operation = "write-file" },
		"other-revision": func(v *runCommandInput) { v.Revision = "2" },
		"host-path":      func(v *runCommandInput) { v.Target.WorkspaceName = "/tmp/private" },
		"missing-target": func(v *runCommandInput) { v.Target.Workspace = mount.WorkspaceID{} },
	} {
		t.Run(name, func(t *testing.T) {
			bad := v
			mutate(&bad)
			if _, err := encodeRunCommandInput(bad); err == nil {
				t.Fatal("unsupported or incomplete run-command profile accepted")
			}
		})
	}
}

func TestRunCommandOperationFingerprintAndRecord(t *testing.T) {
	v := runCommandProfileFixture(t)
	large := v
	large.Binding.PayloadBytes = p.MaxMessageBytes
	if _, err := encodeRunCommandInput(large); err != nil {
		t.Fatal("write metadata rejected its separate large argument budget")
	}
	large.Binding.PayloadBytes++
	if _, err := encodeRunCommandInput(large); err == nil {
		t.Fatal("write payload reference exceeded its independent bound")
	}
	want, err := runCommandFingerprint(v)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*runCommandInput){
		"arguments":      func(v *runCommandInput) { v.Binding.ArgumentsDigest = digest([]byte("different")) },
		"project":        func(v *runCommandInput) { v.Binding.Project = newOperationID[i.Project](t) },
		"mount":          func(v *runCommandInput) { v.Target.Mount = newOperationID[i.Mount](t) },
		"workspace":      func(v *runCommandInput) { v.Target.Workspace = newOperationID[mount.Workspace](t) },
		"runner":         func(v *runCommandInput) { v.Target.Runner = newOperationID[runner.Runner](t) },
		"workspace-name": func(v *runCommandInput) { v.Target.WorkspaceName = "different-workspace" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := v
			mutate(&changed)
			got, err := runCommandFingerprint(changed)
			if err != nil || got == want {
				t.Fatal("fingerprint lost trusted input or target identity")
			}
		})
	}
	changed := v
	changed.Target.AgentVersion++
	changed.Target.MountVersion++
	if got, err := runCommandFingerprint(changed); err != nil || got != want {
		t.Fatal("admission version changed the logical target fingerprint")
	}
	raw, _ := encodeRunCommandInput(v)
	r := runCommandOperationRecord{ID: newOperationID[tc.Operation](t), Input: v, InputDigest: digest(raw), Fingerprint: want, Phase: "created", Version: 1}
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

func runCommandReport(stdout, stderr []byte) p.RunCommandResult {
	return p.RunCommandResult{State: "completed", ExitCode: 0, DurationMS: "17", StdoutBytes: p.Decimal(strconv.Itoa(len(stdout))), StderrBytes: p.Decimal(strconv.Itoa(len(stderr))), OutputComplete: true}
}

func TestRunCommandStoredReportAndStreamsBindIdentity(t *testing.T) {
	operation, attempt := newOperationID[tc.Operation](t), newOperationID[tc.Attempt](t)
	request := p.ID(newOperationID[struct{}](t).String())
	stdout, stderr := bytes.Repeat([]byte("x"), p.MaxRunCommandStreamBytes), bytes.Repeat([]byte("y"), p.MaxRunCommandStreamBytes)
	report := runCommandReport(stdout, stderr)
	report.ExitCode = 23
	ref, raw, err := encodeRunCommandStoredResult(operation, attempt, request, report, stdout, stderr)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeRunCommandStoredResult(ref, raw, stdout, stderr, operation, attempt, request)
	gotRaw, _ := p.EncodeRunCommandResult(got)
	if err != nil || !bytes.Equal(gotRaw, raw) || len(raw) > 32768 || ref.StdoutBytes != 65536 || ref.StderrBytes != 65536 {
		t.Fatal("complete independent report/streams lost", err)
	}
	for name, mutate := range map[string]func(*runCommandResultReference){
		"operation":     func(v *runCommandResultReference) { v.Operation = newOperationID[tc.Operation](t) },
		"attempt":       func(v *runCommandResultReference) { v.Attempt = newOperationID[tc.Attempt](t) },
		"request":       func(v *runCommandResultReference) { v.Request = p.ID(newOperationID[struct{}](t).String()) },
		"metadata-sha":  func(v *runCommandResultReference) { v.SHA256 = digest([]byte("other")) },
		"stdout-sha":    func(v *runCommandResultReference) { v.StdoutSHA256 = digest(stderr) },
		"stderr-length": func(v *runCommandResultReference) { v.StderrBytes-- },
	} {
		t.Run(name, func(t *testing.T) {
			bad := ref
			mutate(&bad)
			if _, err := decodeRunCommandStoredResult(bad, raw, stdout, stderr, operation, attempt, request); err == nil {
				t.Fatal("foreign binding accepted")
			}
		})
	}
	for _, name := range []string{"count", "invalid-utf8", "oversize", "swapped-streams", "changed-report"} {
		t.Run(name, func(t *testing.T) {
			out, errout := bytes.Clone(stdout), bytes.Clone(stderr)
			metadata := bytes.Clone(raw)
			value := report
			switch name {
			case "count":
				value.StdoutBytes = "1"
			case "invalid-utf8":
				out[0] = 0xff
			case "oversize":
				out = append(out, 'x')
				value.StdoutBytes = "65537"
			case "swapped-streams":
				out, errout = errout, out
			case "changed-report":
				value.ExitCode = 0
				metadata, _ = p.EncodeRunCommandResult(value)
			}
			if name == "count" || name == "invalid-utf8" || name == "oversize" {
				if _, _, err := encodeRunCommandStoredResult(operation, attempt, request, value, out, errout); err == nil {
					t.Fatal("untrusted report accepted")
				}
			}
			if _, err := decodeRunCommandStoredResult(ref, metadata, out, errout, operation, attempt, request); err == nil && name != "count" {
				t.Fatal("substituted content accepted")
			}
		})
	}
	empty := runCommandReport(nil, nil)
	emptyRef, emptyRaw, err := encodeRunCommandStoredResult(operation, attempt, request, empty, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeRunCommandStoredResult(emptyRef, emptyRaw, []byte{}, []byte{}, operation, attempt, request); err != nil {
		t.Fatal("true empty stream rejected", err)
	}
}
