package runnerprotocol_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"testing"

	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func TestReadFileWireGoldenAndLargeOffset(t *testing.T) {
	v := p.ReadFilePayload{WorkspaceName: "source", Arguments: p.ReadFileArguments{Path: "dir/file.txt", Offset: 9007199254740993, Length: 16, Encoding: "utf8"}}
	raw, err := p.EncodeReadFilePayload(v)
	want := `{"workspace_name":"source","arguments":{"path":"dir/file.txt","offset":"9007199254740993","length":16,"encoding":"utf8"}}`
	if err != nil || string(raw) != want {
		t.Fatal("read-file request wire differs", err)
	}
	decoded, err := p.DecodeReadFilePayload(raw)
	if err != nil || decoded != v {
		t.Fatal("read-file request lost exact offset", err)
	}
	result := p.ReadFileResult{Path: v.Arguments.Path, Offset: v.Arguments.Offset, BytesRead: 3, EOF: true, Encoding: "utf8", Data: "Aé"}
	raw, err = p.EncodeReadFileResult(result)
	want = `{"path":"dir/file.txt","offset":"9007199254740993","bytes_read":3,"eof":true,"encoding":"utf8","data":"Aé"}`
	if err != nil || string(raw) != want {
		t.Fatal("read-file result wire differs", err)
	}
	got, err := p.DecodeReadFileResult(raw)
	if err != nil || got != result {
		t.Fatal("read-file result round trip differs", err)
	}
}

func TestReadFileStrictPayloadAndValidation(t *testing.T) {
	good := `{"workspace_name":"source","arguments":{"path":"file","offset":"0","length":1,"encoding":"utf8"}}`
	bad := []string{
		`null`, `[]`, good + `{}`,
		strings.Replace(good, `"workspace_name":"source"`, `"workspace_name":"source","workspace_name":"other"`, 1),
		strings.Replace(good, `"workspace_name":"source"`, `"Workspace_name":"source"`, 1),
		strings.Replace(good, `"workspace_name":"source"`, `"workspace_name":null`, 1),
		strings.Replace(good, `"arguments":`, `"host_path":"/tmp","arguments":`, 1),
		strings.Replace(good, `"path":"file"`, `"path":"file","p\u0061th":"other"`, 1),
		strings.Replace(good, `"path":"file"`, `"path":"\ud800"`, 1),
		strings.Replace(good, `"path":"file"`, "\"path\":\"\xff\"", 1),
		strings.Replace(good, `"path":"file"`, `"Path":"file"`, 1),
		strings.Replace(good, `"path":"file"`, `"path":"file","mount_id":"ignored"`, 1),
		strings.Replace(good, `"path":"file",`, ``, 1),
	}
	for _, offset := range []string{`0`, `null`, `"00"`, `"-1"`, `"+1"`, `"1.0"`, `"1e0"`, `"9223372036854775807"`, `"9223372036854775808"`} {
		bad = append(bad, strings.Replace(good, `"offset":"0"`, `"offset":`+offset, 1))
	}
	for _, length := range []string{`0`, `-1`, `65537`, `1.0`, `1e0`, `"1"`, `null`} {
		bad = append(bad, strings.Replace(good, `"length":1`, `"length":`+length, 1))
	}
	for n, raw := range bad {
		if got, err := p.DecodeReadFilePayload([]byte(raw)); err == nil || got != (p.ReadFilePayload{}) {
			t.Errorf("malformed input %d did not fail atomically", n)
		}
	}
	for _, path := range []string{"", ".", "..", "/file", "dir//file", "dir/../file", "dir/./file", "dir/", "a\\b", "x\x00y", strings.Repeat("x", 256)} {
		v := p.ReadFilePayload{WorkspaceName: "source", Arguments: p.ReadFileArguments{Path: path, Length: 1, Encoding: "utf8"}}
		if raw, err := p.EncodeReadFilePayload(v); err == nil || raw != nil {
			t.Error("invalid path encoded")
		}
	}
	for _, name := range []string{"", ".hidden", "a.", "../source", "a/b", "NUL", "com1.log", "LPt9", "source space", "中文", strings.Repeat("x", 256)} {
		v := p.ReadFilePayload{WorkspaceName: name, Arguments: p.ReadFileArguments{Path: "file", Length: 1, Encoding: "utf8"}}
		if _, err := p.EncodeReadFilePayload(v); err == nil {
			t.Error("invalid workspace name encoded")
		}
	}
	for _, name := range []string{"source", "A.b", "COM0", "LPT0", strings.Repeat("x", 255)} {
		v := p.ReadFilePayload{WorkspaceName: name, Arguments: p.ReadFileArguments{Path: "dir/文件", Offset: math.MaxInt64 - 2, Length: 1, Encoding: "base64"}}
		if _, err := p.EncodeReadFilePayload(v); err != nil {
			t.Error("valid boundary rejected", err)
		}
	}
}

func TestReadFileStrictResultAndData(t *testing.T) {
	good := `{"path":"file","offset":"0","bytes_read":1,"eof":true,"encoding":"base64","data":"/w=="}`
	for n, raw := range []string{
		strings.Replace(good, `"eof":true`, `"eof":null`, 1),
		strings.Replace(good, `"eof":true,`, ``, 1),
		strings.Replace(good, `"bytes_read":1`, `"bytes_read":2`, 1),
		strings.Replace(good, `"offset":"0"`, `"offset":0`, 1),
		strings.Replace(good, `"data":"/w=="`, `"data":"/w==","data":"/w=="`, 1),
		strings.Replace(good, `"data":"/w=="`, `"data":"/x=="`, 1),
		strings.Replace(good, `"data":"/w=="`, `"data":"/w==\n"`, 1),
		strings.Replace(good, `"data":"/w=="`, `"data":"/w"`, 1),
		strings.Replace(good, `"data":"/w=="`, `"Data":"/w=="`, 1),
		strings.Replace(good, `"data":"/w=="`, `"data":"/w==","host":"hidden"`, 1),
	} {
		if result, err := p.DecodeReadFileResult([]byte(raw)); err == nil || result != (p.ReadFileResult{}) {
			t.Errorf("invalid result %d accepted", n)
		}
	}
	if _, err := p.DecodeReadFileResult([]byte(good)); err != nil {
		t.Fatal("binary content rejected", err)
	}
	for _, result := range []p.ReadFileResult{
		{Path: "file", Encoding: "utf8", EOF: true},
		{Path: "file", Encoding: "base64", EOF: true},
		{Path: strings.Repeat("<", 255), BytesRead: p.MaxReadFileBytes, Encoding: "utf8", Data: strings.Repeat("\x00", p.MaxReadFileBytes)},
	} {
		raw, err := p.EncodeReadFileResult(result)
		if err != nil || len(raw) > p.MaxReadFileResultBytes {
			t.Fatal("bounded result rejected", err)
		}
		got, err := p.DecodeReadFileResult(raw)
		if err != nil || got != result {
			t.Fatal("bounded result changed", err)
		}
	}
	for _, result := range []p.ReadFileResult{
		{Path: "file", BytesRead: 1, Encoding: "utf8", Data: "\xff"},
		{Path: "file", BytesRead: 1, Encoding: "utf8", Data: "é"},
		{Path: "file", Encoding: "utf8"},
		{Path: "file", Encoding: "utf8", EOF: true, Offset: math.MaxInt64},
	} {
		if raw, err := p.EncodeReadFileResult(result); err == nil || raw != nil {
			t.Error("invalid result encoded")
		}
	}
	if _, err := p.DecodeReadFileResult(bytes.Repeat([]byte(" "), p.MaxReadFileResultBytes+1)); err != p.ErrTooLarge {
		t.Fatal("oversized result accepted")
	}
}

func TestReadFileDefaultProjectionRedacts(t *testing.T) {
	args := p.ReadFileArguments{Path: "READ_PATH_CANARY", Length: 1, Encoding: "utf8"}
	for _, value := range []any{args, p.ReadFilePayload{WorkspaceName: "WORKSPACE_CANARY", Arguments: args}, p.ReadFileResult{Path: args.Path, Data: "CONTENT_CANARY"}} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var log bytes.Buffer
		slog.New(slog.NewJSONHandler(&log, nil)).Info("projection", "value", value)
		out := string(raw) + fmt.Sprintf("%v %+v %#v", value, value, value) + log.String()
		if strings.Contains(out, "CANARY") {
			t.Fatal("default read-file projection leaked input or content")
		}
	}
}
