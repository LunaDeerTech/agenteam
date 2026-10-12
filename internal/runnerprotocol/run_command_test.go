package runnerprotocol_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

const commandPayloadGolden = `{"workspace_name":"source","arguments":{"mode":"argv","program":"/bin/bash","args":["-c","printf '%s' 'a b'"],"cwd":"."}}`

func TestRunCommandPayloadPreservesExplicitArgv(t *testing.T) {
	want := p.RunCommandPayload{WorkspaceName: "source", Arguments: p.RunCommandArguments{Mode: "argv", Program: "/bin/bash", Args: []string{"-c", "printf '%s' 'a b'"}, CWD: "."}}
	raw, err := p.EncodeRunCommandPayload(want)
	if err != nil || string(raw) != commandPayloadGolden {
		t.Fatal("explicit shell executable or argv boundaries changed", err)
	}
	for _, input := range []string{commandPayloadGolden, ` {"arguments":{"cwd":".","args":["-c","printf '%s' 'a b'"],"program":"/bin/bash","mode":"argv"},"workspace_name":"source"} `} {
		got, err := p.DecodeRunCommandPayload([]byte(input))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("valid argv changed", err)
		}
		got.Arguments.Args[0] = "mutated"
		if want.Arguments.Args[0] != "-c" || string(raw) != commandPayloadGolden {
			t.Fatal("decoded argv aliases original")
		}
	}
	for _, args := range [][]string{nil, {}, {"", "a b", "$HOME", "*", ";", "中文🙂", "line\nnext"}} {
		want.Arguments.Args = args
		raw, err := p.EncodeRunCommandPayload(want)
		if err != nil || bytes.Contains(raw, []byte(`"args":null`)) {
			t.Fatal("empty or literal argv rejected", err)
		}
		got, err := p.DecodeRunCommandPayload(raw)
		if err != nil || len(got.Arguments.Args) != len(args) {
			t.Fatal("argv cardinality changed", err)
		}
		for i := range args {
			if got.Arguments.Args[i] != args[i] {
				t.Fatal("argv was expanded or reparsed")
			}
		}
	}
	bad := []string{
		`null`, `[]`, commandPayloadGolden + `{}`,
		strings.Replace(commandPayloadGolden, `"workspace_name":"source"`, `"workspace_name":null`, 1),
		strings.Replace(commandPayloadGolden, `"workspace_name":"source",`, ``, 1),
		strings.Replace(commandPayloadGolden, `"workspace_name":"source"`, `"workspace_name":"source","workspace_name":"other"`, 1),
		strings.Replace(commandPayloadGolden, `"mode":"argv"`, `"mode":"shell"`, 1),
		strings.Replace(commandPayloadGolden, `"mode":"argv",`, ``, 1),
		strings.Replace(commandPayloadGolden, `"mode":"argv"`, `"mode":"argv","m\u006fde":"argv"`, 1),
		strings.Replace(commandPayloadGolden, `"cwd":"."`, `"cwd":null`, 1),
		strings.Replace(commandPayloadGolden, `"cwd":"."`, `"cwd":"\ud800"`, 1),
		strings.Replace(commandPayloadGolden, `"cwd":"."`, "\"cwd\":\"\xff\"", 1),
		strings.Replace(commandPayloadGolden, `"cwd":"."`, `"CWD":"."`, 1),
	}
	for _, field := range []string{`"command":"echo unsafe"`, `"env":{}`, `"timeout":1`} {
		bad = append(bad, strings.Replace(commandPayloadGolden, `"cwd":"."`, `"cwd":".",`+field, 1))
	}
	for _, args := range []string{`null`, `{}`, `"echo"`, `[null]`, `[1]`, `[true]`, `["\u0000"]`, `["\ud800"]`} {
		bad = append(bad, strings.Replace(commandPayloadGolden, `["-c","printf '%s' 'a b'"]`, args, 1))
	}
	for n, input := range bad {
		got, err := p.DecodeRunCommandPayload([]byte(input))
		if err == nil || !reflect.DeepEqual(got, p.RunCommandPayload{}) {
			t.Errorf("invalid request %d did not fail atomically", n)
		}
	}
}

func TestRunCommandPayloadBoundsAndWholeMessage(t *testing.T) {
	want := p.RunCommandPayload{WorkspaceName: strings.Repeat("w", 255), Arguments: p.RunCommandArguments{Mode: "argv", Program: "/bin/echo", CWD: strings.Repeat(strings.Repeat("<", 255)+"/", 15) + strings.Repeat("<", 254) + "/x"}}
	// This exercises JSON expansion and the actual envelope, not just a raw
	// string-length gate. The raw argv byte budget includes Program.
	want.Arguments.Args = []string{strings.Repeat("<", 65536-len(want.Arguments.Program))}
	if len(want.Arguments.CWD) != 4096 {
		t.Fatal("bad boundary fixture")
	}
	raw, err := p.EncodeRunCommandPayload(want)
	if err != nil {
		t.Fatal("exact boundary rejected", err)
	}
	r := request()
	r.OperationName, r.OperationRevision, r.Payload = p.RunCommandOperation, p.RunCommandRevision, raw
	m, err := p.NewMessage(header(true), r)
	if err != nil {
		t.Fatal("valid complete request rejected", err)
	}
	wire, err := p.Encode(m)
	if err != nil || len(wire) > 1<<20 {
		t.Fatal("invalid complete message size", err)
	}
	decoded, err := p.Decode(wire)
	if err != nil || decoded.AllowedFrom(p.Central) != nil {
		t.Fatal("full request round trip failed", err)
	}
	got, err := p.DecodeRunCommandPayload(decoded.Payload().(p.Request).Payload)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("maximum payload changed", err)
	}
	want.Arguments.Args[0] += "x"
	if raw, err := p.EncodeRunCommandPayload(want); err == nil || raw != nil {
		t.Fatal("one excess raw argv byte accepted")
	}
	want.Arguments.Args = make([]string, 256)
	if _, err := p.EncodeRunCommandPayload(want); err != nil {
		t.Fatal("256 empty args rejected", err)
	}
	want.Arguments.Args = append(want.Arguments.Args, "")
	if _, err := p.EncodeRunCommandPayload(want); err == nil {
		t.Fatal("257 args accepted")
	}
	want.Arguments.Args = nil
	for _, cwd := range []string{"", "..", "/tmp", "a/../b", "a//b", "a/./b", "a/", "a\\b", "x\x00y", strings.Repeat("x", 256)} {
		want.Arguments.CWD = cwd
		if _, err := p.EncodeRunCommandPayload(want); err == nil {
			t.Error("unsafe cwd accepted")
		}
	}
	want.Arguments.CWD = "."
	for _, program := range []string{"", "/", "echo", "./echo", "/bin//echo", "/bin/../echo", "/bin/echo/", "/x\x00y", "/\xff", "/" + strings.Repeat("x", 4096)} {
		want.Arguments.Program = program
		if _, err := p.EncodeRunCommandPayload(want); err == nil {
			t.Error("invalid program accepted")
		}
	}
	if got, err := p.DecodeRunCommandPayload(bytes.Repeat([]byte(" "), (1<<20)+1)); err != p.ErrTooLarge || !reflect.DeepEqual(got, p.RunCommandPayload{}) {
		t.Fatal("whole-message overflow not rejected atomically")
	}
}

const commandResultGolden = `{"state":"completed","exit_code":23,"signal":0,"cancelled":false,"timed_out":false,"duration_ms":"17","stdout_bytes":"4","stderr_bytes":"0","stdout_truncated":false,"stderr_truncated":false,"output_complete":true,"process_group_termination_confirmed":true}`

func commandResult() p.RunCommandResult {
	return p.RunCommandResult{State: "completed", ExitCode: 23, DurationMS: "17", StdoutBytes: "4", StderrBytes: "0", OutputComplete: true, ProcessGroupTerminationConfirmed: true}
}

func TestRunCommandResultKeepsKnownExitAndCompletionFacts(t *testing.T) {
	v := commandResult()
	raw, err := p.EncodeRunCommandResult(v)
	if err != nil || string(raw) != commandResultGolden {
		t.Fatal("nonzero known-exit report changed", err)
	}
	reordered := strings.TrimSuffix(strings.Replace(commandResultGolden, `"state":"completed",`, ``, 1), "}") + `,"state":"completed"}`
	for _, input := range []string{commandResultGolden, reordered} {
		got, err := p.DecodeRunCommandResult([]byte(input))
		if err != nil || !reflect.DeepEqual(got, v) {
			t.Fatal("valid result member order rejected", err)
		}
	}
	for _, tc := range []struct {
		name string
		edit func(*p.RunCommandResult)
	}{
		{"zero-exit", func(v *p.RunCommandResult) { v.ExitCode = 0 }},
		{"signaled", func(v *p.RunCommandResult) { v.ExitCode, v.Signal = -1, 15 }},
		{"truncated", func(v *p.RunCommandResult) { v.StdoutBytes, v.StdoutTruncated = "65536", true }},
		{"group-unconfirmed", func(v *p.RunCommandResult) { v.ProcessGroupTerminationConfirmed = false }},
		{"cancelled-tail", func(v *p.RunCommandResult) {
			v.Cancelled, v.OutputComplete, v.CompletionCode = true, false, ptr(p.CancelledCode)
		}},
		{"timeout", func(v *p.RunCommandResult) { v.Cancelled, v.TimedOut, v.CompletionCode = true, true, ptr(p.Timeout) }},
		{"local-output-failure", func(v *p.RunCommandResult) { v.OutputComplete, v.CompletionCode = false, ptr(p.InternalError) }},
		{"internal-before-cancel", func(v *p.RunCommandResult) {
			v.Cancelled, v.TimedOut, v.CompletionCode = true, true, ptr(p.InternalError)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := commandResult()
			tc.edit(&want)
			raw, err := p.EncodeRunCommandResult(want)
			if err != nil {
				t.Fatal("known terminal rejected", err)
			}
			response := p.Response{RequestID: first, OperationID: second, Outcome: p.Success, Payload: raw}
			m, err := p.NewMessage(header(true), response)
			if err != nil {
				t.Fatal("dedicated completion report rejected by unchanged success envelope", err)
			}
			wire, err := p.Encode(m)
			if err != nil {
				t.Fatal("completion envelope encoding failed", err)
			}
			decoded, err := p.Decode(wire)
			if err != nil || decoded.AllowedFrom(p.Runner) != nil {
				t.Fatal("completion envelope round trip failed", err)
			}
			got, err := p.DecodeRunCommandResult(decoded.Payload().(p.Response).Payload)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("completion facts were erased", err)
			}
			if got.CompletionCode != nil {
				*got.CompletionCode = p.Conflict
				if *want.CompletionCode == p.Conflict {
					t.Fatal("decoded completion code aliases original")
				}
			}
		})
	}
}

func TestRunCommandResultRejectsContradictionsAndStaysRedacted(t *testing.T) {
	bad := []string{`null`, commandResultGolden + `{}`,
		strings.Replace(commandResultGolden, `"state":"completed"`, `"state":"unknown"`, 1),
		strings.Replace(commandResultGolden, `"state":"completed"`, `"state":"completed","state":"completed"`, 1),
		strings.Replace(commandResultGolden, `"state":"completed"`, `"state":"completed","raw_error":"private"`, 1),
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(commandResultGolden), &fields); err != nil {
		t.Fatal(err)
	}
	for key := range fields {
		for _, missing := range []bool{false, true} {
			copyFields := make(map[string]json.RawMessage, len(fields))
			for k, value := range fields {
				copyFields[k] = value
			}
			if missing {
				delete(copyFields, key)
			} else {
				copyFields[key] = json.RawMessage(`null`)
			}
			raw, err := json.Marshal(copyFields)
			if err != nil {
				t.Fatal(err)
			}
			bad = append(bad, string(raw))
		}
	}
	for _, change := range [][2]string{
		{`"exit_code":23`, `"exit_code":-1`}, {`"exit_code":23`, `"exit_code":256`},
		{`"exit_code":23`, `"exit_code":2.3`}, {`"signal":0`, `"signal":9`},
		{`"signal":0`, `"signal":-1`}, {`"cancelled":false`, `"cancelled":true`},
		{`"timed_out":false`, `"timed_out":true`}, {`"output_complete":true`, `"output_complete":false`},
		{`"duration_ms":"17"`, `"duration_ms":17`}, {`"duration_ms":"17"`, `"duration_ms":"017"`},
		{`"duration_ms":"17"`, `"duration_ms":"9223372036854775808"`},
		{`"stdout_bytes":"4"`, `"stdout_bytes":"65537"`}, {`"stderr_bytes":"0"`, `"stderr_bytes":"-1"`},
		{`"state":"completed"`, `"state":"completed","completion_code":null`},
		{`"state":"completed"`, `"state":"completed","completion_code":"TIMEOUT"`},
		{`"state":"completed"`, `"state":"completed","completion_code":"CANCELLED"`},
		{`"state":"completed"`, `"state":"completed","completion_code":"CONFLICT"`},
	} {
		bad = append(bad, strings.Replace(commandResultGolden, change[0], change[1], 1))
	}
	for n, input := range bad {
		got, err := p.DecodeRunCommandResult([]byte(input))
		if err == nil || !reflect.DeepEqual(got, p.RunCommandResult{}) {
			t.Errorf("contradictory result %d did not fail atomically", n)
		}
	}
	// Dedicated reports never relax generic failure/cancelled/unknown payload
	// rules. Without an actual completion the handler must use those variants.
	for _, response := range []p.Response{
		{RequestID: first, OperationID: second, Outcome: p.Failure, Code: ptr(p.InternalError), SafeMessage: ptr(p.InternalError.SafeMessage())},
		{RequestID: first, OperationID: second, Outcome: p.Cancelled, Code: ptr(p.CancelledCode), SafeMessage: ptr(p.CancelledCode.SafeMessage())},
		{RequestID: first, OperationID: second, Outcome: p.Unknown, SafeMessage: ptr(p.UnknownMessage)},
	} {
		if _, err := p.NewMessage(header(true), response); err != nil {
			t.Fatal("generic payload-free terminal rejected", err)
		}
		response.Payload = json.RawMessage(commandResultGolden)
		if _, err := p.NewMessage(header(true), response); err == nil {
			t.Fatal("generic terminal gained command payload permission")
		}
	}
	secret := "do-not-log-command-arguments"
	payload := p.RunCommandPayload{WorkspaceName: "private-workspace", Arguments: p.RunCommandArguments{Mode: "argv", Program: "/private/program", Args: []string{secret}, CWD: "."}}
	for _, value := range []any{payload, payload.Arguments, commandResult()} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var log bytes.Buffer
		slog.New(slog.NewJSONHandler(&log, nil)).Info("safe", "value", value)
		text := fmt.Sprintf("%v %+v %#v", value, value, value) + string(raw) + log.String()
		if strings.Contains(text, secret) || strings.Contains(text, "/private") || strings.Contains(text, "private-workspace") || strings.Contains(text, "exit_code") {
			t.Fatal("default projection exposed command facts")
		}
	}
}
