package runnerprotocol

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	ReadFileOperation              = "read-file"
	ReadFileRevision       Decimal = "1"
	MaxReadFileBytes               = 64 << 10
	MaxReadFileResultBytes         = 512 << 10
)

// ReadFileArguments are model-facing arguments. Workspace identity is supplied
// separately by Central's current Mount authority, never by these arguments.
type ReadFileArguments struct {
	Path     string
	Offset   int64
	Length   int
	Encoding string
}

// ReadFilePayload carries a current, authorized logical name over the existing
// authenticated Control connection. It is not itself an authorization ticket.
type ReadFilePayload struct {
	WorkspaceName string
	Arguments     ReadFileArguments
}

type ReadFileResult struct {
	Path      string
	Offset    int64
	BytesRead int
	EOF       bool
	Encoding  string
	Data      string
}

func (ReadFileArguments) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "read_file_arguments") }
func (ReadFileArguments) MarshalJSON() ([]byte, error) {
	return []byte(`"read_file_arguments"`), nil
}
func (ReadFileArguments) LogValue() slog.Value       { return slog.StringValue("read_file_arguments") }
func (ReadFilePayload) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "read_file_payload") }
func (ReadFilePayload) MarshalJSON() ([]byte, error) { return []byte(`"read_file_payload"`), nil }
func (ReadFilePayload) LogValue() slog.Value         { return slog.StringValue("read_file_payload") }
func (ReadFileResult) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "read_file_result") }
func (ReadFileResult) MarshalJSON() ([]byte, error)  { return []byte(`"read_file_result"`), nil }
func (ReadFileResult) LogValue() slog.Value          { return slog.StringValue("read_file_result") }

func readFilePath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." || len(part) > 255 || strings.ContainsAny(part, "\\\x00") {
			return false
		}
	}
	return true
}

func readFileWorkspace(value string) bool {
	if value == "" || len(value) > 255 || strings.HasSuffix(value, ".") {
		return false
	}
	for i := range len(value) {
		c := value[i]
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || i > 0 && c == '.') {
			return false
		}
	}
	base, _, _ := strings.Cut(strings.ToUpper(value), ".")
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return false
	}
	return !(len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9')
}

func (v ReadFileArguments) Validate() error {
	if !readFilePath(v.Path) || v.Offset < 0 || v.Length < 1 || v.Length > MaxReadFileBytes ||
		v.Offset > math.MaxInt64-int64(v.Length)-1 || (v.Encoding != "utf8" && v.Encoding != "base64") {
		return ErrInvalid
	}
	return nil
}

func (v ReadFilePayload) Validate() error {
	if !readFileWorkspace(v.WorkspaceName) {
		return ErrInvalid
	}
	return v.Arguments.Validate()
}

type readFileArgumentsWire struct {
	Path     string `json:"path"`
	Offset   string `json:"offset"`
	Length   int    `json:"length"`
	Encoding string `json:"encoding"`
}

type readFilePayloadWire struct {
	WorkspaceName string                `json:"workspace_name"`
	Arguments     readFileArgumentsWire `json:"arguments"`
}

func EncodeReadFilePayload(v ReadFilePayload) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	a := v.Arguments
	return json.Marshal(readFilePayloadWire{v.WorkspaceName, readFileArgumentsWire{a.Path, strconv.FormatInt(a.Offset, 10), a.Length, a.Encoding}})
}

func DecodeReadFilePayload(raw []byte) (ReadFilePayload, error) {
	root, err := parseJSON(raw)
	if err != nil {
		return ReadFilePayload{}, err
	}
	m, err := fields(root, words("workspace_name arguments"), nil)
	if err != nil {
		return ReadFilePayload{}, err
	}
	if _, err := fields(m["arguments"], words("path offset length encoding"), nil); err != nil {
		return ReadFilePayload{}, err
	}
	var wire readFilePayloadWire
	if json.Unmarshal(raw, &wire) != nil || !decimal(Decimal(wire.Arguments.Offset), math.MaxInt64, false) {
		return ReadFilePayload{}, ErrInvalid
	}
	offset, _ := strconv.ParseInt(wire.Arguments.Offset, 10, 64)
	v := ReadFilePayload{wire.WorkspaceName, ReadFileArguments{wire.Arguments.Path, offset, wire.Arguments.Length, wire.Arguments.Encoding}}
	if err := v.Validate(); err != nil {
		return ReadFilePayload{}, err
	}
	return v, nil
}

func (v ReadFileResult) Validate() error {
	if !readFilePath(v.Path) || v.Offset < 0 || v.BytesRead < 0 || v.BytesRead > MaxReadFileBytes ||
		v.Offset > math.MaxInt64-int64(v.BytesRead)-1 || v.BytesRead == 0 && !v.EOF {
		return ErrInvalid
	}
	switch v.Encoding {
	case "utf8":
		if !utf8.ValidString(v.Data) || len(v.Data) != v.BytesRead {
			return ErrInvalid
		}
	case "base64":
		if len(v.Data) > base64.StdEncoding.EncodedLen(MaxReadFileBytes) {
			return ErrInvalid
		}
		data, err := base64.StdEncoding.Strict().DecodeString(v.Data)
		if err != nil || len(data) != v.BytesRead || base64.StdEncoding.EncodeToString(data) != v.Data {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type readFileResultWire struct {
	Path      string `json:"path"`
	Offset    string `json:"offset"`
	BytesRead int    `json:"bytes_read"`
	EOF       bool   `json:"eof"`
	Encoding  string `json:"encoding"`
	Data      string `json:"data"`
}

func EncodeReadFileResult(v ReadFileResult) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(readFileResultWire{v.Path, strconv.FormatInt(v.Offset, 10), v.BytesRead, v.EOF, v.Encoding, v.Data})
	if err != nil {
		return nil, ErrInvalid
	}
	if len(raw) > MaxReadFileResultBytes {
		return nil, ErrTooLarge
	}
	return raw, nil
}

func DecodeReadFileResult(raw []byte) (ReadFileResult, error) {
	if len(raw) > MaxReadFileResultBytes {
		return ReadFileResult{}, ErrTooLarge
	}
	root, err := parseJSON(raw)
	if err != nil {
		return ReadFileResult{}, err
	}
	if _, err := fields(root, words("path offset bytes_read eof encoding data"), nil); err != nil {
		return ReadFileResult{}, err
	}
	var wire readFileResultWire
	if json.Unmarshal(raw, &wire) != nil || !decimal(Decimal(wire.Offset), math.MaxInt64, false) {
		return ReadFileResult{}, ErrInvalid
	}
	offset, _ := strconv.ParseInt(wire.Offset, 10, 64)
	v := ReadFileResult{wire.Path, offset, wire.BytesRead, wire.EOF, wire.Encoding, wire.Data}
	if err := v.Validate(); err != nil {
		return ReadFileResult{}, err
	}
	return v, nil
}
