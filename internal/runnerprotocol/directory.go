package runnerprotocol

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	ListDirectoryOperation          = "list-directory"
	FindFilesOperation              = "find-files"
	DirectoryRevision       Decimal = "1"
	MaxDirectoryEntries             = 128
	MaxDirectoryResultBytes         = 512 << 10
)

// DirectoryArguments contains only operation arguments. Central supplies the
// current authorized logical workspace name separately from these values.
type DirectoryArguments struct {
	Path    string
	Limit   int
	Pattern string
	Match   string
}

type DirectoryPayload struct {
	WorkspaceName string
	Arguments     DirectoryArguments
}

type DirectoryEntry struct {
	Name string
	Kind string
}

type ListDirectoryResult struct {
	Path      string
	Entries   []DirectoryEntry
	Truncated bool
}

type FindFilesResult struct {
	Path      string
	Paths     []string
	Truncated bool
}

func (DirectoryArguments) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "directory_arguments")
}
func (DirectoryArguments) MarshalJSON() ([]byte, error) { return []byte(`"directory_arguments"`), nil }
func (DirectoryArguments) LogValue() slog.Value         { return slog.StringValue("directory_arguments") }
func (DirectoryPayload) Format(w fmt.State, _ rune)     { _, _ = io.WriteString(w, "directory_payload") }
func (DirectoryPayload) MarshalJSON() ([]byte, error)   { return []byte(`"directory_payload"`), nil }
func (DirectoryPayload) LogValue() slog.Value           { return slog.StringValue("directory_payload") }
func (DirectoryEntry) Format(w fmt.State, _ rune)       { _, _ = io.WriteString(w, "directory_entry") }
func (DirectoryEntry) MarshalJSON() ([]byte, error)     { return []byte(`"directory_entry"`), nil }
func (DirectoryEntry) LogValue() slog.Value             { return slog.StringValue("directory_entry") }
func (ListDirectoryResult) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "list_directory_result")
}
func (ListDirectoryResult) MarshalJSON() ([]byte, error) {
	return []byte(`"list_directory_result"`), nil
}
func (ListDirectoryResult) LogValue() slog.Value     { return slog.StringValue("list_directory_result") }
func (FindFilesResult) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "find_files_result") }
func (FindFilesResult) MarshalJSON() ([]byte, error) { return []byte(`"find_files_result"`), nil }
func (FindFilesResult) LogValue() slog.Value         { return slog.StringValue("find_files_result") }

func directoryPath(value string) bool { return value == "." || readFilePath(value) }
func directoryComponent(value string) bool {
	return len(value) <= 255 && !strings.Contains(value, "/") && readFilePath(value)
}

func (v DirectoryArguments) Validate(operation string) error {
	if !directoryPath(v.Path) || v.Limit < 1 || v.Limit > MaxDirectoryEntries {
		return ErrInvalid
	}
	switch operation {
	case ListDirectoryOperation:
		if v.Pattern != "" || v.Match != "" {
			return ErrInvalid
		}
	case FindFilesOperation:
		if len(v.Pattern) == 0 || len(v.Pattern) > 256 || !utf8.ValidString(v.Pattern) || strings.ContainsRune(v.Pattern, 0) || strings.Contains(v.Pattern, "**") {
			return ErrInvalid
		}
		switch v.Match {
		case "name":
			if strings.Contains(v.Pattern, "/") {
				return ErrInvalid
			}
		case "path":
			if strings.HasPrefix(v.Pattern, "/") {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
		if _, err := path.Match(v.Pattern, ""); err != nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func (v DirectoryPayload) Validate(operation string) error {
	if !readFileWorkspace(v.WorkspaceName) {
		return ErrInvalid
	}
	return v.Arguments.Validate(operation)
}

type listDirectoryArgumentsWire struct {
	Path  string `json:"path"`
	Limit int    `json:"limit"`
}
type findFilesArgumentsWire struct {
	Path    string `json:"path"`
	Limit   int    `json:"limit"`
	Pattern string `json:"pattern"`
	Match   string `json:"match"`
}
type directoryPayloadWire struct {
	WorkspaceName string          `json:"workspace_name"`
	Arguments     json.RawMessage `json:"arguments"`
}

func directoryArgumentFields(operation string) ([]string, error) {
	switch operation {
	case ListDirectoryOperation:
		return words("path limit"), nil
	case FindFilesOperation:
		return words("path limit pattern match"), nil
	default:
		return nil, ErrInvalid
	}
}

func EncodeDirectoryPayload(operation string, v DirectoryPayload) ([]byte, error) {
	if err := v.Validate(operation); err != nil {
		return nil, err
	}
	a := v.Arguments
	var args any = listDirectoryArgumentsWire{a.Path, a.Limit}
	if operation == FindFilesOperation {
		args = findFilesArgumentsWire{a.Path, a.Limit, a.Pattern, a.Match}
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, ErrInvalid
	}
	return json.Marshal(directoryPayloadWire{v.WorkspaceName, raw})
}

func DecodeDirectoryPayload(operation string, raw []byte) (DirectoryPayload, error) {
	required, err := directoryArgumentFields(operation)
	if err != nil {
		return DirectoryPayload{}, err
	}
	root, err := parseJSON(raw)
	if err != nil {
		return DirectoryPayload{}, err
	}
	m, err := fields(root, words("workspace_name arguments"), nil)
	if err != nil {
		return DirectoryPayload{}, err
	}
	if _, err := fields(m["arguments"], required, nil); err != nil {
		return DirectoryPayload{}, err
	}
	var wire directoryPayloadWire
	var args findFilesArgumentsWire
	if json.Unmarshal(raw, &wire) != nil || json.Unmarshal(wire.Arguments, &args) != nil {
		return DirectoryPayload{}, ErrInvalid
	}
	v := DirectoryPayload{wire.WorkspaceName, DirectoryArguments{args.Path, args.Limit, args.Pattern, args.Match}}
	if err := v.Validate(operation); err != nil {
		return DirectoryPayload{}, err
	}
	return v, nil
}

func (v ListDirectoryResult) Validate() error {
	if !directoryPath(v.Path) || v.Entries == nil || len(v.Entries) > MaxDirectoryEntries || v.Truncated && len(v.Entries) == 0 {
		return ErrInvalid
	}
	for n, entry := range v.Entries {
		if !directoryComponent(entry.Name) || n > 0 && v.Entries[n-1].Name >= entry.Name {
			return ErrInvalid
		}
		switch entry.Kind {
		case "file", "directory", "symlink", "other":
		default:
			return ErrInvalid
		}
	}
	return nil
}

func (v FindFilesResult) Validate() error {
	if !directoryPath(v.Path) || v.Paths == nil || len(v.Paths) > MaxDirectoryEntries || v.Truncated && len(v.Paths) == 0 {
		return ErrInvalid
	}
	for n, name := range v.Paths {
		if !readFilePath(name) || n > 0 && v.Paths[n-1] >= name || v.Path != "." && !strings.HasPrefix(name, v.Path+"/") {
			return ErrInvalid
		}
	}
	return nil
}

type directoryEntryWire struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}
type listDirectoryResultWire struct {
	Path      string               `json:"path"`
	Entries   []directoryEntryWire `json:"entries"`
	Truncated bool                 `json:"truncated"`
}
type findFilesResultWire struct {
	Path      string   `json:"path"`
	Paths     []string `json:"paths"`
	Truncated bool     `json:"truncated"`
}

func encodeDirectoryResult(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalid
	}
	if len(raw) > MaxDirectoryResultBytes {
		return nil, ErrTooLarge
	}
	return raw, nil
}

func EncodeListDirectoryResult(v ListDirectoryResult) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	entries := make([]directoryEntryWire, len(v.Entries))
	for n, entry := range v.Entries {
		entries[n] = directoryEntryWire{entry.Name, entry.Kind}
	}
	return encodeDirectoryResult(listDirectoryResultWire{v.Path, entries, v.Truncated})
}

func DecodeListDirectoryResult(raw []byte) (ListDirectoryResult, error) {
	if len(raw) > MaxDirectoryResultBytes {
		return ListDirectoryResult{}, ErrTooLarge
	}
	root, err := parseJSON(raw)
	if err != nil {
		return ListDirectoryResult{}, err
	}
	m, err := fields(root, words("path entries truncated"), nil)
	if err != nil {
		return ListDirectoryResult{}, err
	}
	entries, ok := m["entries"].([]any)
	if !ok {
		return ListDirectoryResult{}, ErrInvalid
	}
	for _, entry := range entries {
		if _, err := fields(entry, words("name kind"), nil); err != nil {
			return ListDirectoryResult{}, err
		}
	}
	var wire listDirectoryResultWire
	if json.Unmarshal(raw, &wire) != nil {
		return ListDirectoryResult{}, ErrInvalid
	}
	v := ListDirectoryResult{Path: wire.Path, Entries: make([]DirectoryEntry, len(wire.Entries)), Truncated: wire.Truncated}
	for n, entry := range wire.Entries {
		v.Entries[n] = DirectoryEntry{entry.Name, entry.Kind}
	}
	if err := v.Validate(); err != nil {
		return ListDirectoryResult{}, err
	}
	return v, nil
}

func EncodeFindFilesResult(v FindFilesResult) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	return encodeDirectoryResult(findFilesResultWire{v.Path, v.Paths, v.Truncated})
}

func DecodeFindFilesResult(raw []byte) (FindFilesResult, error) {
	if len(raw) > MaxDirectoryResultBytes {
		return FindFilesResult{}, ErrTooLarge
	}
	root, err := parseJSON(raw)
	if err != nil {
		return FindFilesResult{}, err
	}
	if _, err := fields(root, words("path paths truncated"), nil); err != nil {
		return FindFilesResult{}, err
	}
	var wire findFilesResultWire
	if json.Unmarshal(raw, &wire) != nil {
		return FindFilesResult{}, ErrInvalid
	}
	v := FindFilesResult{wire.Path, wire.Paths, wire.Truncated}
	if err := v.Validate(); err != nil {
		return FindFilesResult{}, err
	}
	return v, nil
}
