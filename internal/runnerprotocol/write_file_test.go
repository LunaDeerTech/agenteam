package runnerprotocol_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestWriteFilePayloadStrictAndCanonicalBase64(t *testing.T) {
	want := p.WriteFilePayload{WorkspaceName: "source", Arguments: p.WriteFileArguments{Path: "dir/file", Data: []byte{0, 255, 65}, Overwrite: false}}
	raw, err := p.EncodeWriteFilePayload(want)
	golden := `{"workspace_name":"source","arguments":{"path":"dir/file","data_base64":"AP9B","overwrite":false}}`
	if err != nil || string(raw) != golden {
		t.Fatal("write request golden differs", err)
	}
	for _, input := range []string{golden, ` {"arguments":{"overwrite":false,"data_base64":"AP9B","path":"dir/file"},"workspace_name":"source"} `} {
		got, err := p.DecodeWriteFilePayload([]byte(input))
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("valid request changed", err)
		}
		got.Arguments.Data[0] = 9
		if want.Arguments.Data[0] != 0 || string(raw) != golden {
			t.Fatal("decoded bytes alias caller input")
		}
	}
	for _, data := range [][]byte{nil, {}} {
		want.Arguments.Data = data
		want.Arguments.Overwrite = true
		raw, err := p.EncodeWriteFilePayload(want)
		if err != nil || !bytes.Contains(raw, []byte(`"data_base64":""`)) {
			t.Fatal("empty file did not encode as explicit empty base64", err)
		}
		got, err := p.DecodeWriteFilePayload(raw)
		if err != nil || len(got.Arguments.Data) != 0 || !got.Arguments.Overwrite {
			t.Fatal("empty file changed", err)
		}
	}
	bad := []string{
		`null`, `[]`, golden + `{}`,
		strings.Replace(golden, `"workspace_name":"source"`, `"workspace_name":null`, 1),
		strings.Replace(golden, `"workspace_name":"source",`, ``, 1),
		strings.Replace(golden, `"workspace_name":"source"`, `"Workspace_name":"source"`, 1),
		strings.Replace(golden, `"workspace_name":"source"`, `"workspace_name":"source","workspace_name":"other"`, 1),
		strings.Replace(golden, `"arguments":`, `"host_path":"/private","arguments":`, 1),
		strings.Replace(golden, `"path":"dir/file"`, `"path":null`, 1),
		strings.Replace(golden, `"path":"dir/file",`, ``, 1),
		strings.Replace(golden, `"path":"dir/file"`, `"path":"dir/file","p\u0061th":"other"`, 1),
		strings.Replace(golden, `"path":"dir/file"`, `"path":"\ud800"`, 1),
		strings.Replace(golden, `"path":"dir/file"`, "\"path\":\"\xff\"", 1),
		strings.Replace(golden, `"data_base64":"AP9B"`, `"data_base64":null`, 1),
		strings.Replace(golden, `"data_base64":"AP9B"`, `"data_base64":[]`, 1),
		strings.Replace(golden, `"data_base64":"AP9B",`, ``, 1),
		strings.Replace(golden, `"overwrite":false`, `"overwrite":false,"mode":"0777"`, 1),
		strings.Replace(golden, `,"overwrite":false`, ``, 1),
	}
	for _, value := range []string{`null`, `0`, `"false"`, `{}`} {
		bad = append(bad, strings.Replace(golden, `"overwrite":false`, `"overwrite":`+value, 1))
	}
	for _, encoded := range []string{"/w", "_w==", "/x==", "/w===", "/w==\n", "/w==\r\n", " /w==", "????"} {
		quoted, _ := json.Marshal(encoded)
		bad = append(bad, strings.Replace(golden, `"AP9B"`, string(quoted), 1))
	}
	for n, input := range bad {
		got, err := p.DecodeWriteFilePayload([]byte(input))
		if err == nil || !reflect.DeepEqual(got, p.WriteFilePayload{}) {
			t.Errorf("invalid request %d did not fail atomically", n)
		}
	}
	for _, path := range []string{"", ".", "..", "/file", "dir//file", "dir/../file", "dir/./file", "dir/", "a\\b", "x\x00y", strings.Repeat("x", 256)} {
		want.Arguments.Path = path
		if raw, err := p.EncodeWriteFilePayload(want); err == nil || raw != nil {
			t.Error("unsafe path encoded")
		}
	}
	want.Arguments.Path = "file"
	for _, workspace := range []string{"", "../source", "a/b", "NUL", "com1.log", strings.Repeat("x", 256)} {
		want.WorkspaceName = workspace
		if raw, err := p.EncodeWriteFilePayload(want); err == nil || raw != nil {
			t.Error("unsafe workspace encoded")
		}
	}
}

func TestWriteFileBoundsAndWholeMessage(t *testing.T) {
	if p.MaxWriteFileBytes != 524288 || p.MaxWriteFileEncodedBytes != 699052 || base64.StdEncoding.EncodedLen(p.MaxWriteFileBytes) != p.MaxWriteFileEncodedBytes {
		t.Fatal("wire size contract changed")
	}
	// Exactly 4096 path bytes, each component <=255. '<' forces JSON escaping;
	// the maximum raw data and path must survive the complete v1.0 envelope.
	path := strings.Repeat(strings.Repeat("<", 255)+"/", 15) + strings.Repeat("<", 254) + "/x"
	if len(path) != 4096 {
		t.Fatal("bad boundary fixture")
	}
	want := p.WriteFilePayload{WorkspaceName: strings.Repeat("w", 255), Arguments: p.WriteFileArguments{Path: path, Data: bytes.Repeat([]byte{255}, p.MaxWriteFileBytes), Overwrite: true}}
	raw, err := p.EncodeWriteFilePayload(want)
	if err != nil {
		t.Fatal("maximum request rejected", err)
	}
	r := request()
	r.OperationName, r.OperationRevision, r.Payload = p.WriteFileOperation, p.WriteFileRevision, raw
	m, err := p.NewMessage(header(true), r)
	if err != nil {
		t.Fatal("maximum request envelope rejected", err)
	}
	wire, err := p.Encode(m)
	if err != nil || len(wire) > p.MaxMessageBytes {
		t.Fatal("complete encoded request exceeded budget", err)
	}
	message, err := p.Decode(wire)
	if err != nil || message.Header().ProtocolVersion != p.CurrentVersion() || message.AllowedFrom(p.Central) != nil {
		t.Fatal("message round trip rejected", err)
	}
	got, err := p.DecodeWriteFilePayload(message.Payload().(p.Request).Payload)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("maximum data was truncated or changed", err)
	}
	want.Arguments.Path += "x"
	if _, err := p.EncodeWriteFilePayload(want); err == nil {
		t.Fatal("4097-byte path accepted")
	}
	want.Arguments.Path = "file"
	for _, n := range []int{p.MaxWriteFileBytes + 1, p.MaxWriteFileBytes + 3} {
		want.Arguments.Data = make([]byte, n)
		if raw, err := p.EncodeWriteFilePayload(want); err != p.ErrTooLarge || raw != nil {
			t.Fatal("oversized raw data encoded", err)
		}
		encoded := base64.StdEncoding.EncodeToString(want.Arguments.Data)
		input := `{"workspace_name":"source","arguments":{"path":"file","data_base64":"` + encoded + `","overwrite":false}}`
		if got, err := p.DecodeWriteFilePayload([]byte(input)); err != p.ErrTooLarge || !reflect.DeepEqual(got, p.WriteFilePayload{}) {
			t.Fatal("oversized decoded or encoded data accepted", err)
		}
	}
	if _, err := p.DecodeWriteFilePayload(bytes.Repeat([]byte(" "), p.MaxMessageBytes+1)); err != p.ErrTooLarge {
		t.Fatal("oversized JSON accepted", err)
	}
	// Environment is independently valid and within its original decoded cap;
	// escaping it can make a valid operation payload exceed the whole-frame cap.
	r.Environment = map[string]string{}
	for n := range 8 {
		r.Environment[fmt.Sprintf("E%d", n)] = strings.Repeat("<", 8000)
	}
	large := r.Payload
	r.Payload = json.RawMessage(`{}`)
	if _, err := p.NewMessage(header(true), r); err != nil {
		t.Fatal("environment fixture is independently invalid", err)
	}
	r.Payload = large
	if _, err := p.NewMessage(header(true), r); err != p.ErrTooLarge {
		t.Fatal("whole-message budget was not enforced", err)
	}
}

func TestWriteFileCommittedResultAndTerminalVariants(t *testing.T) {
	for _, want := range []p.WriteFileResult{
		{Path: "file", State: "committed", Durable: true},
		{Path: "file", State: "committed", BytesWritten: p.MaxWriteFileBytes, Durable: true},
		{Path: "file", State: "committed", BytesWritten: 1, CompletionCode: ptr(p.InternalError)},
		{Path: "file", State: "committed", BytesWritten: 1, CompletionCode: ptr(p.CancelledCode)},
		{Path: "file", State: "committed", BytesWritten: 1, CompletionCode: ptr(p.Timeout)},
		{Path: "file", State: "committed", BytesWritten: 1, Durable: true, CompletionCode: ptr(p.InternalError)},
		{Path: "file", State: "committed", BytesWritten: 1, Durable: true, CompletionCode: ptr(p.CancelledCode)},
		{Path: "file", State: "committed", BytesWritten: 1, Durable: true, CompletionCode: ptr(p.Timeout)},
	} {
		raw, err := p.EncodeWriteFileResult(want)
		if err != nil {
			t.Fatal("known publication rejected", err)
		}
		got, err := p.DecodeWriteFileResult(raw)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("publication or completion facts changed", err)
		}
		message, err := p.Decode(encode(t, p.Response{RequestID: first, OperationID: second, Outcome: p.Success, Payload: raw}, true))
		if err != nil || message.AllowedFrom(p.Runner) != nil {
			t.Fatal("known publication envelope rejected", err)
		}
		response := message.Payload().(p.Response)
		got, err = p.DecodeWriteFileResult(response.Payload)
		if err != nil || !reflect.DeepEqual(got, want) || response.Code != nil || response.SafeMessage != nil {
			t.Fatal("operation completion was moved into generic error fields", err)
		}
	}
	good := `{"path":"file","state":"committed","bytes_written":0,"durable":true}`
	raw, err := p.EncodeWriteFileResult(p.WriteFileResult{Path: "file", State: "committed", Durable: true})
	if err != nil || string(raw) != good {
		t.Fatal("committed result golden differs", err)
	}
	if _, err := p.DecodeWriteFileResult([]byte(`{"durable":true,"bytes_written":0,"state":"committed","path":"file"}`)); err != nil {
		t.Fatal("valid result key order rejected", err)
	}
	bad := []string{`null`, `[]`, good + `{}`,
		strings.Replace(good, `"path":"file",`, ``, 1),
		strings.Replace(good, `"path":"file"`, `"path":null`, 1),
		strings.Replace(good, `"state":"committed",`, ``, 1),
		strings.Replace(good, `"state":"committed"`, `"state":"unknown"`, 1),
		strings.Replace(good, `"state":"committed"`, `"state":"not_committed"`, 1),
		strings.Replace(good, `"bytes_written":0,`, ``, 1),
		strings.Replace(good, `,"durable":true`, ``, 1),
		strings.Replace(good, `"durable":true`, `"durable":false`, 1),
		strings.Replace(good, `"durable":true`, `"durable":null`, 1),
		strings.Replace(good, `"durable":true`, `"durable":"true"`, 1),
		strings.Replace(good, `"durable":true`, `"durable":true,"durable":true`, 1),
		strings.Replace(good, `"durable":true`, `"Durable":true`, 1),
		strings.Replace(good, `"durable":true`, `"durable":true,"completion_code":null`, 1),
		strings.Replace(good, `"durable":true`, `"durable":true,"completion_code":"CONFLICT"`, 1),
		strings.Replace(good, `"durable":true`, `"durable":true,"raw_error":"private"`, 1),
	}
	for _, value := range []string{`-1`, `524289`, `0.0`, `0e0`, `"0"`, `null`} {
		bad = append(bad, strings.Replace(good, `"bytes_written":0`, `"bytes_written":`+value, 1))
	}
	for n, input := range bad {
		if got, err := p.DecodeWriteFileResult([]byte(input)); err == nil || !reflect.DeepEqual(got, p.WriteFileResult{}) {
			t.Errorf("invalid committed result %d did not fail atomically", n)
		}
	}
	for _, response := range []p.Response{
		{RequestID: first, OperationID: second, Outcome: p.Failure, Code: ptr(p.Conflict), SafeMessage: ptr(p.Conflict.SafeMessage())},
		{RequestID: first, OperationID: second, Outcome: p.Cancelled, Code: ptr(p.CancelledCode), SafeMessage: ptr(p.CancelledCode.SafeMessage())},
		{RequestID: first, OperationID: second, Outcome: p.Unknown, SafeMessage: ptr(p.UnknownMessage)},
	} {
		if _, err := p.Decode(encode(t, response, true)); err != nil {
			t.Fatal("original payload-free terminal rejected", err)
		}
		response.Payload = raw
		if _, err := p.NewMessage(header(true), response); err == nil {
			t.Fatal("non-success terminal accepted a publication payload")
		}
	}
}

func TestWriteFileDefaultProjectionRedacts(t *testing.T) {
	args := p.WriteFileArguments{Path: "WRITE_PATH_CANARY", Data: []byte("WRITE_CONTENT_CANARY"), Overwrite: true}
	for _, value := range []any{args, p.WriteFilePayload{WorkspaceName: "WORKSPACE_CANARY", Arguments: args}, p.WriteFileResult{Path: args.Path, State: "committed", Durable: true}} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var log bytes.Buffer
		slog.New(slog.NewJSONHandler(&log, nil)).Info("projection", "value", value)
		out := string(raw) + fmt.Sprintf("%v %+v %#v", value, value, value) + log.String()
		if strings.Contains(out, "CANARY") || strings.Contains(out, base64.StdEncoding.EncodeToString(args.Data)) {
			t.Fatal("default write-file projection leaked file data or workspace")
		}
	}
}
