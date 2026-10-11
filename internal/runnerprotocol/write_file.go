package runnerprotocol

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
)

const (
	WriteFileOperation               = "write-file"
	WriteFileRevision        Decimal = "1"
	MaxWriteFileBytes                = 512 << 10
	MaxWriteFileEncodedBytes         = 699052
)

// WriteFileArguments owns decoded file bytes. A nil or empty Data writes an
// empty file. This wire limit does not change the local filesystem Write limit.
type WriteFileArguments struct {
	Path      string
	Data      []byte
	Overwrite bool
}

// WriteFilePayload carries an authorized logical workspace name, not a host
// path or an authorization grant. Full messages still require NewMessage/Encode.
type WriteFilePayload struct {
	WorkspaceName string
	Arguments     WriteFileArguments
}

// WriteFileResult is exclusively a known atomic publication result. Success
// does not imply durability or absence of a later completion error. Unknown,
// not-committed failure and cancellation use the generic payload-free variants.
type WriteFileResult struct {
	Path           string
	State          string
	BytesWritten   int
	Durable        bool
	CompletionCode *Code
}

func (WriteFileArguments) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "write_file_arguments")
}
func (WriteFileArguments) MarshalJSON() ([]byte, error) {
	return []byte(`"write_file_arguments"`), nil
}
func (WriteFileArguments) LogValue() slog.Value { return slog.StringValue("write_file_arguments") }
func (WriteFilePayload) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "write_file_payload")
}
func (WriteFilePayload) MarshalJSON() ([]byte, error) {
	return []byte(`"write_file_payload"`), nil
}
func (WriteFilePayload) LogValue() slog.Value { return slog.StringValue("write_file_payload") }
func (WriteFileResult) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "write_file_result")
}
func (WriteFileResult) MarshalJSON() ([]byte, error) {
	return []byte(`"write_file_result"`), nil
}
func (WriteFileResult) LogValue() slog.Value { return slog.StringValue("write_file_result") }

func (v WriteFileArguments) Validate() error {
	if !readFilePath(v.Path) {
		return ErrInvalid
	}
	if len(v.Data) > MaxWriteFileBytes {
		return ErrTooLarge
	}
	return nil
}

func (v WriteFilePayload) Validate() error {
	if !readFileWorkspace(v.WorkspaceName) {
		return ErrInvalid
	}
	return v.Arguments.Validate()
}

type writeFileArgumentsWire struct {
	Path       string `json:"path"`
	DataBase64 string `json:"data_base64"`
	Overwrite  bool   `json:"overwrite"`
}

type writeFilePayloadWire struct {
	WorkspaceName string                 `json:"workspace_name"`
	Arguments     writeFileArgumentsWire `json:"arguments"`
}

func EncodeWriteFilePayload(v WriteFilePayload) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	a := v.Arguments
	encoded := base64.StdEncoding.EncodeToString(a.Data)
	if len(encoded) > MaxWriteFileEncodedBytes {
		return nil, ErrTooLarge
	}
	raw, err := json.Marshal(writeFilePayloadWire{v.WorkspaceName, writeFileArgumentsWire{a.Path, encoded, a.Overwrite}})
	if err != nil {
		return nil, ErrInvalid
	}
	if len(raw) > MaxMessageBytes {
		return nil, ErrTooLarge
	}
	return raw, nil
}

func DecodeWriteFilePayload(raw []byte) (WriteFilePayload, error) {
	root, err := parseJSON(raw)
	if err != nil {
		return WriteFilePayload{}, err
	}
	m, err := fields(root, words("workspace_name arguments"), nil)
	if err != nil {
		return WriteFilePayload{}, err
	}
	if _, err := fields(m["arguments"], words("path data_base64 overwrite"), nil); err != nil {
		return WriteFilePayload{}, err
	}
	var wire writeFilePayloadWire
	if json.Unmarshal(raw, &wire) != nil || !readFileWorkspace(wire.WorkspaceName) || !readFilePath(wire.Arguments.Path) {
		return WriteFilePayload{}, ErrInvalid
	}
	encoded := wire.Arguments.DataBase64
	if len(encoded) > MaxWriteFileEncodedBytes {
		return WriteFilePayload{}, ErrTooLarge
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.StdEncoding.EncodeToString(data) != encoded {
		clear(data)
		return WriteFilePayload{}, ErrInvalid
	}
	// The encoded limit alone is insufficient: MaxWriteFileBytes+1 still has
	// the same padded encoded length as MaxWriteFileBytes.
	if len(data) > MaxWriteFileBytes {
		clear(data)
		return WriteFilePayload{}, ErrTooLarge
	}
	return WriteFilePayload{wire.WorkspaceName, WriteFileArguments{wire.Arguments.Path, data, wire.Arguments.Overwrite}}, nil
}

func (v WriteFileResult) Validate() error {
	if !readFilePath(v.Path) || v.State != "committed" || v.BytesWritten < 0 || v.BytesWritten > MaxWriteFileBytes {
		return ErrInvalid
	}
	if v.CompletionCode == nil {
		if !v.Durable {
			return ErrInvalid
		}
		return nil
	}
	switch *v.CompletionCode {
	case InternalError, CancelledCode, Timeout:
		return nil
	default:
		return ErrInvalid
	}
}

type writeFileResultWire struct {
	Path           string `json:"path"`
	State          string `json:"state"`
	BytesWritten   int    `json:"bytes_written"`
	Durable        bool   `json:"durable"`
	CompletionCode *Code  `json:"completion_code,omitempty"`
}

func EncodeWriteFileResult(v WriteFileResult) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(writeFileResultWire{v.Path, v.State, v.BytesWritten, v.Durable, v.CompletionCode})
	if err != nil {
		return nil, ErrInvalid
	}
	if len(raw) > MaxMessageBytes {
		return nil, ErrTooLarge
	}
	return raw, nil
}

func DecodeWriteFileResult(raw []byte) (WriteFileResult, error) {
	root, err := parseJSON(raw)
	if err != nil {
		return WriteFileResult{}, err
	}
	if _, err := fields(root, words("path state bytes_written durable"), words("completion_code")); err != nil {
		return WriteFileResult{}, err
	}
	var wire writeFileResultWire
	if json.Unmarshal(raw, &wire) != nil {
		return WriteFileResult{}, ErrInvalid
	}
	v := WriteFileResult{wire.Path, wire.State, wire.BytesWritten, wire.Durable, wire.CompletionCode}
	if err := v.Validate(); err != nil {
		return WriteFileResult{}, err
	}
	return v, nil
}
