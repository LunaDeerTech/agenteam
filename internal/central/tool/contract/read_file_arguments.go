package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// ReadFileToolArguments preserves only the model's logical mount and bounded
// relative read intent. Workspace and actual Runner identities are never input.
type ReadFileToolArguments struct {
	MountID   id.MountID
	Arguments p.ReadFileArguments
}

func (v ReadFileToolArguments) Validate() error {
	if v.MountID.Validate() != nil || v.Arguments.Validate() != nil {
		return operationInvalid()
	}
	return nil
}

func DecodeReadFileToolArguments(ctx context.Context, raw []byte) (ReadFileToolArguments, error) {
	var out ReadFileToolArguments
	if ctx == nil || len(raw) > 32<<10 {
		return out, operationInvalid()
	}
	canonical, err := canonicalSchema(ctx, raw)
	if err != nil {
		return out, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(canonical, &fields) != nil || len(fields) != 5 {
		return out, operationInvalid()
	}
	for _, key := range []string{"mount_id", "path", "offset", "length", "encoding"} {
		value, ok := fields[key]
		if !ok || bytes.Equal(value, []byte("null")) {
			return out, operationInvalid()
		}
	}
	var mount, offset string
	if json.Unmarshal(fields["mount_id"], &mount) != nil || json.Unmarshal(fields["path"], &out.Arguments.Path) != nil || json.Unmarshal(fields["offset"], &offset) != nil || json.Unmarshal(fields["length"], &out.Arguments.Length) != nil || json.Unmarshal(fields["encoding"], &out.Arguments.Encoding) != nil {
		return ReadFileToolArguments{}, operationInvalid()
	}
	out.MountID, err = f.ParseID[id.Mount](mount)
	if err != nil {
		return ReadFileToolArguments{}, operationInvalid()
	}
	out.Arguments.Offset, err = strconv.ParseInt(offset, 10, 64)
	if err != nil || strconv.FormatInt(out.Arguments.Offset, 10) != offset || out.Validate() != nil {
		return ReadFileToolArguments{}, operationInvalid()
	}
	return out, ctx.Err()
}

func (ReadFileToolArguments) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "read_file_tool_arguments")
}
func (ReadFileToolArguments) LogValue() slog.Value {
	return slog.StringValue("read_file_tool_arguments")
}
func (ReadFileToolArguments) MarshalJSON() ([]byte, error) {
	return []byte(`"read_file_tool_arguments"`), nil
}
