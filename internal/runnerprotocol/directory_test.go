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

func TestDirectoryPayloadGoldenAndClosedVariants(t *testing.T) {
	for _, tc := range []struct {
		op     string
		args   p.DirectoryArguments
		golden string
	}{
		{p.ListDirectoryOperation, p.DirectoryArguments{Path: ".", Limit: 8}, `{"workspace_name":"source","arguments":{"path":".","limit":8}}`},
		{p.FindFilesOperation, p.DirectoryArguments{Path: "src", Limit: 8, Pattern: "src/*.go", Match: "path"}, `{"workspace_name":"source","arguments":{"path":"src","limit":8,"pattern":"src/*.go","match":"path"}}`},
	} {
		t.Run(tc.op, func(t *testing.T) {
			want := p.DirectoryPayload{WorkspaceName: "source", Arguments: tc.args}
			raw, err := p.EncodeDirectoryPayload(tc.op, want)
			if err != nil || string(raw) != tc.golden {
				t.Fatal("directory payload golden differs", err)
			}
			got, err := p.DecodeDirectoryPayload(tc.op, raw)
			if err != nil || got != want {
				t.Fatal("directory payload round trip differs", err)
			}
			for n, bad := range []string{
				`null`, `[]`, tc.golden + `{}`,
				strings.Replace(tc.golden, `"workspace_name":"source"`, `"workspace_name":null`, 1),
				strings.Replace(tc.golden, `"workspace_name":"source"`, `"workspace_name":"source","workspace_name":"other"`, 1),
				strings.Replace(tc.golden, `"arguments":`, `"mount_id":"caller-id","arguments":`, 1),
				strings.Replace(tc.golden, `"arguments":`, `"Arguments":`, 1),
				strings.Replace(tc.golden, `"limit":8`, `"limit":8,"li\u006dit":9`, 1),
				strings.Replace(tc.golden, `"limit":8`, `"limit":null`, 1),
				strings.Replace(tc.golden, `"limit":8`, `"limit":1.0`, 1),
				strings.Replace(tc.golden, `"limit":8`, `"limit":1e0`, 1),
				strings.Replace(tc.golden, `"limit":8`, `"limit":"8"`, 1),
				strings.Replace(tc.golden, `"limit":8`, `"Limit":8`, 1),
				strings.Replace(tc.golden, `"limit":8`, `"limit":8,"unknown":false`, 1),
				strings.Replace(tc.golden, `"workspace_name":"source"`, `"workspace_name":"\ud800"`, 1),
				strings.Replace(tc.golden, `"workspace_name":"source"`, "\"workspace_name\":\"\xff\"", 1),
			} {
				if value, err := p.DecodeDirectoryPayload(tc.op, []byte(bad)); err == nil || value != (p.DirectoryPayload{}) {
					t.Errorf("invalid payload %d did not fail atomically", n)
				}
			}
		})
	}
	list := `{"workspace_name":"source","arguments":{"path":".","limit":8}}`
	find := `{"workspace_name":"source","arguments":{"path":".","limit":8,"pattern":"*","match":"name"}}`
	for _, tc := range []struct{ op, raw string }{
		{p.ListDirectoryOperation, find},
		{p.ListDirectoryOperation, strings.Replace(list, `"limit":8`, `"limit":8,"pattern":"","match":""`, 1)},
		{p.FindFilesOperation, list},
		{p.FindFilesOperation, strings.Replace(find, `,"pattern":"*"`, ``, 1)},
		{p.FindFilesOperation, strings.Replace(find, `,"match":"name"`, ``, 1)},
		{"read-file", list},
	} {
		if _, err := p.DecodeDirectoryPayload(tc.op, []byte(tc.raw)); err == nil {
			t.Error("cross-operation/missing-field payload accepted")
		}
	}
}

func TestDirectoryArgumentBoundaries(t *testing.T) {
	for _, operation := range []string{p.ListDirectoryOperation, p.FindFilesOperation} {
		base := p.DirectoryArguments{Path: ".", Limit: 1}
		if operation == p.FindFilesOperation {
			base.Pattern, base.Match = "*", "name"
		}
		for _, badPath := range []string{"", "..", "/src", "src/", "src//x", "src/./x", "src/../x", "x\\y", "a\x00b", strings.Repeat("x", 256)} {
			args := base
			args.Path = badPath
			if args.Validate(operation) == nil {
				t.Error("invalid relative path accepted")
			}
		}
		for _, limit := range []int{0, -1, 129} {
			args := base
			args.Limit = limit
			if args.Validate(operation) == nil {
				t.Error("invalid limit accepted")
			}
		}
		for _, name := range []string{"", "../source", ".hidden", "a.", "con", "LPT9.log", "x/y"} {
			if _, err := p.EncodeDirectoryPayload(operation, p.DirectoryPayload{WorkspaceName: name, Arguments: base}); err == nil {
				t.Error("invalid workspace encoded")
			}
		}
		for _, name := range []string{"source", "A.b", "COM0", strings.Repeat("x", 255)} {
			if _, err := p.EncodeDirectoryPayload(operation, p.DirectoryPayload{WorkspaceName: name, Arguments: base}); err != nil {
				t.Error("valid workspace rejected", err)
			}
		}
	}
	for _, args := range []p.DirectoryArguments{
		{Path: ".", Limit: 1, Pattern: "*"}, {Path: ".", Limit: 1, Match: "name"},
	} {
		if args.Validate(p.ListDirectoryOperation) == nil {
			t.Error("list accepted find-only arguments")
		}
	}
	for _, pattern := range []string{"", "**", "a/**/b", "[", "\\", "x\x00y", "\xff", strings.Repeat("x", 257)} {
		if (p.DirectoryArguments{Path: ".", Limit: 1, Pattern: pattern, Match: "name"}).Validate(p.FindFilesOperation) == nil {
			t.Error("invalid glob accepted")
		}
	}
	for _, args := range []p.DirectoryArguments{
		{Path: ".", Limit: 128, Pattern: "[a-z]?.go", Match: "name"},
		{Path: "src", Limit: 1, Pattern: "src/*.go", Match: "path"},
		{Path: ".", Limit: 1, Pattern: `literal\*.txt`, Match: "name"},
		{Path: ".", Limit: 1, Pattern: strings.Repeat("x", 256), Match: "name"},
	} {
		if args.Validate(p.FindFilesOperation) != nil {
			t.Error("valid local glob contract rejected")
		}
	}
	for _, args := range []p.DirectoryArguments{
		{Path: ".", Limit: 1, Pattern: "src/*", Match: "name"},
		{Path: ".", Limit: 1, Pattern: "/src/*", Match: "path"},
		{Path: ".", Limit: 1, Pattern: "*", Match: "unknown"},
	} {
		if args.Validate(p.FindFilesOperation) == nil {
			t.Error("invalid match mode/pattern accepted")
		}
	}
}

func TestDirectoryResultsStrictAndEmpty(t *testing.T) {
	for _, want := range []p.ListDirectoryResult{
		{Path: ".", Entries: []p.DirectoryEntry{}},
		{Path: "src", Entries: []p.DirectoryEntry{{Name: "a", Kind: "directory"}, {Name: "b", Kind: "file"}, {Name: "c", Kind: "other"}, {Name: "d", Kind: "symlink"}}, Truncated: true},
	} {
		raw, err := p.EncodeListDirectoryResult(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.DecodeListDirectoryResult(raw)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("list result changed", err)
		}
	}
	for _, want := range []p.FindFilesResult{
		{Path: ".", Paths: []string{}}, {Path: "src", Paths: []string{"src/a.go", "src/nested/文.go"}, Truncated: true},
	} {
		raw, err := p.EncodeFindFilesResult(want)
		if err != nil {
			t.Fatal(err)
		}
		got, err := p.DecodeFindFilesResult(raw)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("find result changed", err)
		}
	}
	list := `{"path":".","entries":[{"name":"a","kind":"file"}],"truncated":false}`
	for n, bad := range []string{
		strings.Replace(list, `[{"name":"a","kind":"file"}]`, `null`, 1),
		strings.Replace(list, `"truncated":false`, `"truncated":null`, 1),
		strings.Replace(list, `,"truncated":false`, ``, 1),
		strings.Replace(list, `"name":"a"`, `"name":null`, 1),
		strings.Replace(list, `"name":"a"`, `"name":"a","Name":"b"`, 1),
		strings.Replace(list, `"kind":"file"`, `"kind":"file","kind":"file"`, 1),
		strings.Replace(list, `"kind":"file"`, `"kind":"socket"`, 1),
		strings.Replace(list, `"name":"a"`, `"name":"../a"`, 1),
		strings.Replace(list, `"name":"a"`, `"name":"a/b"`, 1),
		strings.Replace(list, `"name":"a"`, `"name":"\ud800"`, 1),
		`{"path":".","entries":[{"name":"b","kind":"file"},{"name":"a","kind":"file"}],"truncated":false}`,
		`{"path":".","entries":[{"name":"a","kind":"file"},{"name":"a","kind":"file"}],"truncated":false}`,
	} {
		if value, err := p.DecodeListDirectoryResult([]byte(bad)); err == nil || value.Entries != nil || value.Path != "" {
			t.Errorf("bad list %d was accepted", n)
		}
	}
	for n, bad := range []string{
		`{"path":".","paths":null,"truncated":false}`,
		`{"path":".","paths":[null],"truncated":false}`,
		`{"path":".","paths":[],"truncated":true}`,
		`{"path":".","paths":["b","a"],"truncated":false}`,
		`{"path":".","paths":["a","a"],"truncated":false}`,
		`{"path":"src","paths":["src"],"truncated":false}`,
		`{"path":"src","paths":["src-other/a"],"truncated":false}`,
		`{"path":".","paths":["./a"],"truncated":false}`,
		`{"path":".","paths":["../a"],"truncated":false}`,
		`{"path":".","paths":[],"truncated":false,"cursor":"x"}`,
	} {
		if value, err := p.DecodeFindFilesResult([]byte(bad)); err == nil || value.Paths != nil || value.Path != "" {
			t.Errorf("bad find %d was accepted", n)
		}
	}
}

func TestDirectoryResultEncodedBudget(t *testing.T) {
	entries := make([]p.DirectoryEntry, 129)
	paths := make([]string, 129)
	for n := range entries {
		entries[n] = p.DirectoryEntry{Name: fmt.Sprintf("%03d", n), Kind: "file"}
		paths[n] = entries[n].Name
	}
	if _, err := p.EncodeListDirectoryResult(p.ListDirectoryResult{Path: ".", Entries: entries}); err == nil {
		t.Fatal("too many entries accepted")
	}
	if _, err := p.EncodeFindFilesResult(p.FindFilesResult{Path: ".", Paths: paths}); err == nil {
		t.Fatal("too many paths accepted")
	}
	// Every decoded path is valid and total input text stays below 512 KiB;
	// the actual JSON escaping expands these control characters past the budget.
	paths = paths[:128]
	segment := strings.Repeat("\x01", 200)
	for n := range paths {
		paths[n] = fmt.Sprintf("%03d/", n) + segment + "/" + segment + "/" + segment + "/" + segment
	}
	value := p.FindFilesResult{Path: ".", Paths: paths}
	if value.Validate() != nil {
		t.Fatal("budget case did not establish valid paths")
	}
	if raw, err := p.EncodeFindFilesResult(value); err != p.ErrTooLarge || raw != nil {
		t.Fatal("actual JSON budget was not enforced")
	}
	oversized := bytes.Repeat([]byte(" "), p.MaxDirectoryResultBytes+1)
	if _, err := p.DecodeListDirectoryResult(oversized); err != p.ErrTooLarge {
		t.Fatal("oversized list accepted")
	}
	if _, err := p.DecodeFindFilesResult(oversized); err != p.ErrTooLarge {
		t.Fatal("oversized find accepted")
	}
}

func TestDirectorySafeDefaultProjection(t *testing.T) {
	args := p.DirectoryArguments{Path: "PATH_CANARY", Limit: 1, Pattern: "PATTERN_CANARY", Match: "name"}
	for _, value := range []any{args, p.DirectoryPayload{WorkspaceName: "WORKSPACE_CANARY", Arguments: args}, p.DirectoryEntry{Name: "NAME_CANARY", Kind: "file"}, p.ListDirectoryResult{Path: "PATH_CANARY", Entries: []p.DirectoryEntry{{Name: "NAME_CANARY", Kind: "file"}}}, p.FindFilesResult{Path: "PATH_CANARY", Paths: []string{"FOUND_CANARY"}}} {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		slog.New(slog.NewJSONHandler(&output, nil)).Info("projection", "value", value)
		if strings.Contains(string(raw)+fmt.Sprintf("%v %+v %#v", value, value, value)+output.String(), "CANARY") {
			t.Fatal("default directory projection leaked material")
		}
	}
}
