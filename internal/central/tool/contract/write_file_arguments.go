package contract

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// WriteFileToolArguments contains only the logical mount and protected write
// intent. Workspace/Runner identities are resolved by their actual owner.
type WriteFileToolArguments struct {
	MountID   id.MountID
	Arguments p.WriteFileArguments
}

func (v WriteFileToolArguments) Validate() error {
	if v.MountID.Validate() != nil || v.Arguments.Validate() != nil {
		return operationInvalid()
	}
	return nil
}

// DecodeWriteFileToolArguments uses the same strict JSON grammar as Registry,
// with a separate payload budget. Schema's 48KiB and read's 32KiB gates remain
// unchanged. This is not the final whole-message bound: the actual Runner
// request must still pass protocol NewMessage/Encode with its real environment.
func DecodeWriteFileToolArguments(ctx context.Context, raw []byte) (WriteFileToolArguments, error) {
	if ctx == nil || len(raw) == 0 || len(raw) > p.MaxMessageBytes || !utf8.Valid(raw) {
		return WriteFileToolArguments{}, operationInvalid()
	}
	if err := ctx.Err(); err != nil {
		return WriteFileToolArguments{}, err
	}
	if err := checkUnicodeEscapes(ctx, raw); err != nil {
		return WriteFileToolArguments{}, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := schemaValue(ctx, d, 0)
	if err != nil {
		return WriteFileToolArguments{}, err
	}
	if _, err = d.Token(); err != io.EOF {
		return WriteFileToolArguments{}, operationInvalid()
	}
	fields, ok := v.(map[string]any)
	if !ok || len(fields) != 4 {
		return WriteFileToolArguments{}, operationInvalid()
	}
	mount, mountOK := fields["mount_id"].(string)
	path, pathOK := fields["path"].(string)
	encoded, dataOK := fields["data_base64"].(string)
	overwrite, overwriteOK := fields["overwrite"].(bool)
	if !mountOK || !pathOK || !dataOK || !overwriteOK || len(encoded) > p.MaxWriteFileEncodedBytes {
		return WriteFileToolArguments{}, operationInvalid()
	}
	mountID, err := f.ParseID[id.Mount](mount)
	if err != nil {
		return WriteFileToolArguments{}, operationInvalid()
	}
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) > p.MaxWriteFileBytes || base64.StdEncoding.EncodeToString(data) != encoded {
		clear(data)
		return WriteFileToolArguments{}, operationInvalid()
	}
	out := WriteFileToolArguments{MountID: mountID, Arguments: p.WriteFileArguments{Path: path, Data: data, Overwrite: overwrite}}
	if err = out.Validate(); err == nil {
		err = ctx.Err()
	}
	if err != nil {
		clear(data)
		return WriteFileToolArguments{}, err
	}
	return out, nil
}

func (WriteFileToolArguments) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "write_file_tool_arguments")
}
func (WriteFileToolArguments) LogValue() slog.Value {
	return slog.StringValue("write_file_tool_arguments")
}
func (WriteFileToolArguments) MarshalJSON() ([]byte, error) {
	return []byte(`"write_file_tool_arguments"`), nil
}
