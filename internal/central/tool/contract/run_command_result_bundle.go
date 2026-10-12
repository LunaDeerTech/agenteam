package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"unicode/utf8"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

const MaxRunCommandResultBundleBytes = 1 << 20
const MaxRunCommandResultReportBytes = 32768

// RunCommandResultBundle is an explicit decoded value, never an authorization
// or evidence that a caller supplied report came from Runner. A real reader
// must first verify original persisted identities and all three stored refs.
// Streams contain UTF-8 bytes, including legal NUL; the canonical JSON encodes
// these as strings rather than dropping bytes or inventing a base64 transport.
type RunCommandResultBundle struct {
	Report p.RunCommandResult
	Stdout []byte
	Stderr []byte
}

func (v RunCommandResultBundle) Validate() error {
	if v.Report.Validate() != nil || !utf8.Valid(v.Stdout) || !utf8.Valid(v.Stderr) || len(v.Stdout) > p.MaxRunCommandStreamBytes || len(v.Stderr) > p.MaxRunCommandStreamBytes ||
		string(v.Report.StdoutBytes) != strconv.Itoa(len(v.Stdout)) || string(v.Report.StderrBytes) != strconv.Itoa(len(v.Stderr)) {
		return operationInvalid()
	}
	return nil
}

// ClassifyRunCommandResult preserves the original producer's priority: local
// I/O, timeout/cancel, incomplete output, signal, nonzero exit, then success.
// Invalid reports have no classification. This is distinct from wire success,
// which means only a known completed invocation and actual lifecycle return.
func ClassifyRunCommandResult(result p.RunCommandResult) (outcome, code string) {
	if result.Validate() != nil {
		return "", ""
	}
	if result.CompletionCode != nil {
		switch *result.CompletionCode {
		case p.InternalError:
			return "failure", string(p.InternalError)
		case p.Timeout:
			return "cancelled", string(p.Timeout)
		case p.CancelledCode:
			return "cancelled", string(p.CancelledCode)
		}
	}
	if result.Cancelled || result.TimedOut || !result.OutputComplete {
		return "failure", "output_incomplete"
	}
	if result.Signal != 0 {
		return "failure", "command_signaled"
	}
	if result.ExitCode != 0 {
		return "failure", "command_exit_nonzero"
	}
	return "success", ""
}

func ValidRunCommandResultClassification(outcome, code string) bool {
	switch outcome {
	case "success":
		return code == ""
	case "failure":
		return code == string(p.InternalError) || code == "output_incomplete" || code == "command_signaled" || code == "command_exit_nonzero"
	case "cancelled":
		return code == string(p.CancelledCode) || code == string(p.Timeout)
	}
	return false
}

// Encode uses the explicit protocol report encoder, never its default redacted
// JSON. Canonical-v1 covers the entire envelope, including nested report keys.
func EncodeRunCommandResultBundle(v RunCommandResultBundle) ([]byte, error) {
	if v.Validate() != nil {
		return nil, operationInvalid()
	}
	report, err := p.EncodeRunCommandResult(v.Report)
	if err != nil || len(report) > MaxRunCommandResultReportBytes {
		return nil, operationInvalid()
	}
	defer clear(report)
	raw, err := json.Marshal(struct {
		Report json.RawMessage `json:"report"`
		Stdout string          `json:"stdout"`
		Stderr string          `json:"stderr"`
	}{report, string(v.Stdout), string(v.Stderr)})
	if err != nil {
		return nil, operationInvalid()
	}
	defer clear(raw)
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || len(canonical) > MaxRunCommandResultBundleBytes {
		clear(canonical)
		return nil, operationInvalid()
	}
	return canonical, nil
}

// Decode accepts only the exact canonical stored envelope with three required,
// non-null fields. Duplicate/unknown keys, invalid Unicode and altered byte
// counts are rejected. The returned stream buffers belong to the caller.
func DecodeRunCommandResultBundle(ctx context.Context, raw []byte) (RunCommandResultBundle, error) {
	var zero RunCommandResultBundle
	if ctx == nil || len(raw) < 2 || len(raw) > MaxRunCommandResultBundleBytes || !utf8.Valid(raw) {
		return zero, operationInvalid()
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := checkUnicodeEscapes(ctx, raw); err != nil {
		return zero, err
	}
	canonical, err := cursor.CanonicalJSON(raw)
	defer clear(canonical)
	if err != nil || !bytes.Equal(raw, canonical) {
		return zero, operationInvalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 3 {
		return zero, operationInvalid()
	}
	report, ok := fields["report"]
	if !ok || len(report) > MaxRunCommandResultReportBytes {
		return zero, operationInvalid()
	}
	value, err := p.DecodeRunCommandResult(report)
	if err != nil {
		return zero, operationInvalid()
	}
	stdout, outOK := fields["stdout"]
	stderr, errOK := fields["stderr"]
	if !outOK || !errOK || bytes.Equal(stdout, []byte("null")) || bytes.Equal(stderr, []byte("null")) {
		return zero, operationInvalid()
	}
	var out, errout string
	if json.Unmarshal(stdout, &out) != nil || json.Unmarshal(stderr, &errout) != nil {
		return zero, operationInvalid()
	}
	v := RunCommandResultBundle{Report: value, Stdout: []byte(out), Stderr: []byte(errout)}
	if err = v.Validate(); err == nil {
		err = ctx.Err()
	}
	if err != nil {
		clear(v.Stdout)
		clear(v.Stderr)
		return zero, err
	}
	return v, nil
}

func (RunCommandResultBundle) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "tool_run_command_result_bundle")
}
func (RunCommandResultBundle) LogValue() slog.Value {
	return slog.StringValue("tool_run_command_result_bundle")
}
func (RunCommandResultBundle) MarshalJSON() ([]byte, error) {
	return []byte(`"tool_run_command_result_bundle"`), nil
}
