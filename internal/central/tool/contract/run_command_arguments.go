package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"unicode/utf8"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// RunCommandToolArguments retains only protected argv and a logical mount.
// It supplies neither host paths, environment, approval nor Runner identities.
type RunCommandToolArguments struct {
	MountID   id.MountID
	Arguments p.RunCommandArguments
}

func (v RunCommandToolArguments) Validate() error {
	if v.MountID.Validate() != nil || v.Arguments.Validate() != nil {
		return operationInvalid()
	}
	return nil
}

// DecodeRunCommandToolArguments is an independent closed profile. The raw
// budget includes JSON escaping; the original argv byte/count and path bounds
// remain authoritative. No shell parsing or executable allow/deny list is added.
// The final actual Runner envelope must still pass NewMessage/Encode.
func DecodeRunCommandToolArguments(ctx context.Context, raw []byte) (RunCommandToolArguments, error) {
	if ctx == nil || len(raw) == 0 || len(raw) > p.MaxMessageBytes || !utf8.Valid(raw) {
		return RunCommandToolArguments{}, operationInvalid()
	}
	if err := ctx.Err(); err != nil {
		return RunCommandToolArguments{}, err
	}
	if err := checkUnicodeEscapes(ctx, raw); err != nil {
		return RunCommandToolArguments{}, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := schemaValue(ctx, d, 0)
	if err != nil {
		return RunCommandToolArguments{}, err
	}
	if _, err = d.Token(); err != io.EOF {
		return RunCommandToolArguments{}, operationInvalid()
	}
	fields, ok := v.(map[string]any)
	if !ok || len(fields) != 5 {
		return RunCommandToolArguments{}, operationInvalid()
	}
	mount, mountOK := fields["mount_id"].(string)
	mode, modeOK := fields["mode"].(string)
	program, programOK := fields["program"].(string)
	cwd, cwdOK := fields["cwd"].(string)
	list, argsOK := fields["args"].([]any)
	if !mountOK || !modeOK || !programOK || !cwdOK || !argsOK || len(list) > p.MaxRunCommandArguments {
		return RunCommandToolArguments{}, operationInvalid()
	}
	mountID, err := f.ParseID[id.Mount](mount)
	if err != nil {
		return RunCommandToolArguments{}, operationInvalid()
	}
	args := make([]string, len(list))
	for n, item := range list {
		arg, ok := item.(string)
		if !ok {
			return RunCommandToolArguments{}, operationInvalid()
		}
		args[n] = arg
	}
	out := RunCommandToolArguments{MountID: mountID, Arguments: p.RunCommandArguments{Mode: mode, Program: program, Args: args, CWD: cwd}}
	if err = out.Validate(); err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return RunCommandToolArguments{}, err
	}
	return out, nil
}
func (RunCommandToolArguments) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "run_command_tool_arguments")
}
func (RunCommandToolArguments) LogValue() slog.Value {
	return slog.StringValue("run_command_tool_arguments")
}
func (RunCommandToolArguments) MarshalJSON() ([]byte, error) {
	return []byte(`"run_command_tool_arguments"`), nil
}
