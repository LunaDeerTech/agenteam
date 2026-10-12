package runnerprotocol

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	RunCommandOperation                = "run-command"
	RunCommandRevision         Decimal = "1"
	MaxRunCommandArguments             = 256
	MaxRunCommandArgumentBytes         = 64 << 10
	MaxRunCommandStreamBytes           = 64 << 10
)

// RunCommandArguments selects direct argv execution, not shell-string parsing.
// An explicitly supplied shell executable is an ordinary absolute program.
// Nil Args encodes as [], but a missing or null wire args field is invalid.
type RunCommandArguments struct {
	Mode    string
	Program string
	Args    []string
	CWD     string
}

// RunCommandPayload names a logical workspace; it supplies no authorization or
// host workspace path. Environment and deadline remain envelope concerns.
type RunCommandPayload struct {
	WorkspaceName string
	Arguments     RunCommandArguments
}

// RunCommandResult is a complete invocation report, not an assertion of exit
// zero, successful business effects or rollback. The Handler must establish
// actual Start, Wait, Owner/Handle Drain and a healthy wire before publishing it.
// Counts describe UTF-8 bytes accepted by the original outgoing queue, not peer
// consumption. OutputComplete and the declared per-stream truncation differ.
type RunCommandResult struct {
	State                            string
	ExitCode                         int
	Signal                           int
	Cancelled                        bool
	TimedOut                         bool
	DurationMS                       Decimal
	StdoutBytes                      Decimal
	StderrBytes                      Decimal
	StdoutTruncated                  bool
	StderrTruncated                  bool
	OutputComplete                   bool
	ProcessGroupTerminationConfirmed bool
	CompletionCode                   *Code
}

func (RunCommandArguments) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "run_command_arguments")
}
func (RunCommandArguments) MarshalJSON() ([]byte, error) {
	return []byte(`"run_command_arguments"`), nil
}
func (RunCommandArguments) LogValue() slog.Value       { return slog.StringValue("run_command_arguments") }
func (RunCommandPayload) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "run_command_payload") }
func (RunCommandPayload) MarshalJSON() ([]byte, error) { return []byte(`"run_command_payload"`), nil }
func (RunCommandPayload) LogValue() slog.Value         { return slog.StringValue("run_command_payload") }
func (RunCommandResult) Format(w fmt.State, _ rune)    { _, _ = io.WriteString(w, "run_command_result") }
func (RunCommandResult) MarshalJSON() ([]byte, error)  { return []byte(`"run_command_result"`), nil }
func (RunCommandResult) LogValue() slog.Value          { return slog.StringValue("run_command_result") }

func (v RunCommandArguments) Validate() error {
	if v.Mode != "argv" || v.Program == "" || len(v.Program) > 4096 || !path.IsAbs(v.Program) ||
		path.Clean(v.Program) != v.Program || v.Program == "/" || !runCommandText(v.Program) ||
		(v.CWD != "." && !readFilePath(v.CWD)) {
		return ErrInvalid
	}
	if len(v.Args) > MaxRunCommandArguments {
		return ErrTooLarge
	}
	size := len(v.Program)
	for _, arg := range v.Args {
		if !runCommandText(arg) {
			return ErrInvalid
		}
		if len(arg) > MaxRunCommandArgumentBytes-size {
			return ErrTooLarge
		}
		size += len(arg)
	}
	return nil
}

func runCommandText(v string) bool { return utf8.ValidString(v) && !strings.ContainsRune(v, 0) }

func (v RunCommandPayload) Validate() error {
	if !readFileWorkspace(v.WorkspaceName) {
		return ErrInvalid
	}
	return v.Arguments.Validate()
}

type runCommandArgumentsWire struct {
	Mode    string   `json:"mode"`
	Program string   `json:"program"`
	Args    []string `json:"args"`
	CWD     string   `json:"cwd"`
}

type runCommandPayloadWire struct {
	WorkspaceName string                  `json:"workspace_name"`
	Arguments     runCommandArgumentsWire `json:"arguments"`
}

func EncodeRunCommandPayload(v RunCommandPayload) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	a := v.Arguments
	args := a.Args
	if args == nil {
		args = []string{}
	}
	raw, err := json.Marshal(runCommandPayloadWire{v.WorkspaceName, runCommandArgumentsWire{a.Mode, a.Program, args, a.CWD}})
	if err != nil {
		return nil, ErrInvalid
	}
	if len(raw) > MaxMessageBytes {
		return nil, ErrTooLarge
	}
	return raw, nil
}

func DecodeRunCommandPayload(raw []byte) (RunCommandPayload, error) {
	root, err := parseJSON(raw)
	if err != nil {
		return RunCommandPayload{}, err
	}
	m, err := fields(root, words("workspace_name arguments"), nil)
	if err != nil {
		return RunCommandPayload{}, err
	}
	a, err := fields(m["arguments"], words("mode program args cwd"), nil)
	if err != nil {
		return RunCommandPayload{}, err
	}
	// encoding/json accepts null elements in []string as empty strings. They
	// are not empty argv entries and must be rejected before unmarshalling.
	args, ok := a["args"].([]any)
	if !ok {
		return RunCommandPayload{}, ErrInvalid
	}
	for _, arg := range args {
		if _, ok := arg.(string); !ok {
			return RunCommandPayload{}, ErrInvalid
		}
	}
	var wire runCommandPayloadWire
	if json.Unmarshal(raw, &wire) != nil {
		return RunCommandPayload{}, ErrInvalid
	}
	v := RunCommandPayload{wire.WorkspaceName, RunCommandArguments{wire.Arguments.Mode, wire.Arguments.Program, wire.Arguments.Args, wire.Arguments.CWD}}
	if err := v.Validate(); err != nil {
		return RunCommandPayload{}, err
	}
	return v, nil
}

func (v RunCommandResult) Validate() error {
	if v.State != "completed" || !decimal(v.DurationMS, math.MaxInt64, false) ||
		!decimal(v.StdoutBytes, MaxRunCommandStreamBytes, false) || !decimal(v.StderrBytes, MaxRunCommandStreamBytes, false) ||
		v.Signal < 0 || v.Signal == 0 && (v.ExitCode < 0 || v.ExitCode > 255) || v.Signal > 0 && v.ExitCode != -1 ||
		v.TimedOut && !v.Cancelled {
		return ErrInvalid
	}
	if v.CompletionCode == nil {
		if v.Cancelled || v.TimedOut || !v.OutputComplete {
			return ErrInvalid
		}
		return nil
	}
	switch *v.CompletionCode {
	case InternalError:
		// A local I/O/close failure takes precedence over concurrent cancel.
		return nil
	case Timeout:
		if v.TimedOut {
			return nil
		}
	case CancelledCode:
		if v.Cancelled && !v.TimedOut {
			return nil
		}
	}
	return ErrInvalid
}

type runCommandResultWire struct {
	State                            string  `json:"state"`
	ExitCode                         int     `json:"exit_code"`
	Signal                           int     `json:"signal"`
	Cancelled                        bool    `json:"cancelled"`
	TimedOut                         bool    `json:"timed_out"`
	DurationMS                       Decimal `json:"duration_ms"`
	StdoutBytes                      Decimal `json:"stdout_bytes"`
	StderrBytes                      Decimal `json:"stderr_bytes"`
	StdoutTruncated                  bool    `json:"stdout_truncated"`
	StderrTruncated                  bool    `json:"stderr_truncated"`
	OutputComplete                   bool    `json:"output_complete"`
	ProcessGroupTerminationConfirmed bool    `json:"process_group_termination_confirmed"`
	CompletionCode                   *Code   `json:"completion_code,omitempty"`
}

func EncodeRunCommandResult(v RunCommandResult) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(runCommandResultWire(v))
	if err != nil {
		return nil, ErrInvalid
	}
	if len(raw) > MaxMessageBytes {
		return nil, ErrTooLarge
	}
	return raw, nil
}

func DecodeRunCommandResult(raw []byte) (RunCommandResult, error) {
	root, err := parseJSON(raw)
	if err != nil {
		return RunCommandResult{}, err
	}
	if _, err := fields(root, words("state exit_code signal cancelled timed_out duration_ms stdout_bytes stderr_bytes stdout_truncated stderr_truncated output_complete process_group_termination_confirmed"), words("completion_code")); err != nil {
		return RunCommandResult{}, err
	}
	var wire runCommandResultWire
	if json.Unmarshal(raw, &wire) != nil {
		return RunCommandResult{}, ErrInvalid
	}
	v := RunCommandResult(wire)
	if err := v.Validate(); err != nil {
		return RunCommandResult{}, err
	}
	return v, nil
}
