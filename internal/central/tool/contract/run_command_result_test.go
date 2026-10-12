package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

func commandBundleFixture(out, errout []byte) RunCommandResultBundle {
	return RunCommandResultBundle{Report: p.RunCommandResult{State: "completed", DurationMS: "17", StdoutBytes: p.Decimal(strconv.Itoa(len(out))), StderrBytes: p.Decimal(strconv.Itoa(len(errout))), OutputComplete: true}, Stdout: out, Stderr: errout}
}

func TestRunCommandResultBundlePreservesCompleteCanonicalStreams(t *testing.T) {
	for _, test := range []struct {
		name        string
		out, errout []byte
	}{
		{"empty", nil, nil},
		{"unicode-nul-escaping", []byte("private-output\x00\"\\\n中文"), []byte("stderr\t\x00🙂")},
		{"maximum-escaping", make([]byte, p.MaxRunCommandStreamBytes), make([]byte, p.MaxRunCommandStreamBytes)},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := commandBundleFixture(test.out, test.errout)
			raw, err := EncodeRunCommandResultBundle(value)
			if err != nil || len(raw) > MaxRunCommandResultBundleBytes {
				t.Fatal("complete bundle encoding failed", err)
			}
			if test.name == "maximum-escaping" && len(raw) <= 524288 {
				t.Fatal("control did not exercise command-only expanded byte budget")
			}
			got, err := DecodeRunCommandResultBundle(context.Background(), raw)
			if err != nil || !bytes.Equal(got.Stdout, test.out) || !bytes.Equal(got.Stderr, test.errout) || got.Report != value.Report {
				t.Fatal("complete report or stream bytes changed", err)
			}
			again, err := EncodeRunCommandResultBundle(got)
			if err != nil || !bytes.Equal(raw, again) {
				t.Fatal("reprojection bytes changed")
			}
			if bytes.IndexByte(raw, 0) >= 0 {
				t.Fatal("raw NUL escaped the JSON string boundary")
			}
			if len(got.Stdout) != 0 {
				got.Stdout[0] ^= 1
				if bytes.Equal(got.Stdout, test.out) {
					t.Fatal("decoder aliased caller stream storage")
				}
				independent, err := DecodeRunCommandResultBundle(context.Background(), raw)
				if err != nil || !bytes.Equal(independent.Stdout, test.out) {
					t.Fatal("decoded mutation changed original canonical bytes")
				}
			}
			safe, _ := json.Marshal(value)
			if string(safe) != `"tool_run_command_result_bundle"` || fmt.Sprintf("%+v", value) != "tool_run_command_result_bundle" || value.LogValue().String() != "tool_run_command_result_bundle" {
				t.Fatal("default formatting exposed output")
			}
		})
	}
}

func TestRunCommandResultBundleRejectsPartialOrUntrustedEnvelope(t *testing.T) {
	value := commandBundleFixture([]byte("x"), []byte("y"))
	raw, err := EncodeRunCommandResultBundle(value)
	if err != nil {
		t.Fatal(err)
	}
	canonical := func(s string) []byte {
		t.Helper()
		b, err := cursor.CanonicalJSON([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for name, bad := range map[string][]byte{
		"unknown-key":      canonical(string(raw[:len(raw)-1]) + `,"extra":true}`),
		"duplicate-key":    bytes.Replace(raw, []byte(`"stdout":"x"`), []byte(`"stdout":"x","stdout":"x"`), 1),
		"missing-stream":   canonical(strings.Replace(string(raw), `,"stdout":"x"`, "", 1)),
		"null-stream":      bytes.Replace(raw, []byte(`"stdout":"x"`), []byte(`"stdout":null`), 1),
		"nonstring-stream": bytes.Replace(raw, []byte(`"stdout":"x"`), []byte(`"stdout":1`), 1),
		"null-report":      canonical(`{"report":null,"stdout":"x","stderr":"y"}`),
		"count-mismatch":   bytes.Replace(raw, []byte(`"stdout_bytes":"1"`), []byte(`"stdout_bytes":"2"`), 1),
		"noncanonical":     append([]byte(" "), raw...),
		"second-document":  append(bytes.Clone(raw), []byte(`{}`)...),
		"lone-surrogate":   bytes.Replace(raw, []byte(`"stdout":"x"`), []byte(`"stdout":"\ud800"`), 1),
		"invalid-utf8":     bytes.Replace(raw, []byte(`"stdout":"x"`), []byte{'"', 's', 't', 'd', 'o', 'u', 't', '"', ':', '"', 0xff, '"'}, 1),
		"whole-budget":     bytes.Repeat([]byte(" "), MaxRunCommandResultBundleBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := DecodeRunCommandResultBundle(context.Background(), bad)
			if err == nil || got.Report.State != "" || got.Stdout != nil || got.Stderr != nil {
				t.Fatal("invalid envelope returned output")
			}
		})
	}
	for _, side := range []string{"stdout", "stderr"} {
		t.Run(side+"-budget", func(t *testing.T) {
			v := commandBundleFixture(nil, nil)
			if side == "stdout" {
				v.Stdout = make([]byte, p.MaxRunCommandStreamBytes+1)
				v.Report.StdoutBytes = "65537"
			} else {
				v.Stderr = make([]byte, p.MaxRunCommandStreamBytes+1)
				v.Report.StderrBytes = "65537"
			}
			if _, err := EncodeRunCommandResultBundle(v); err == nil {
				t.Fatal("stream limit enlarged by total bundle budget")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := DecodeRunCommandResultBundle(ctx, raw); err != context.Canceled {
		t.Fatal("parent cancellation ignored")
	}
}

func TestRunCommandResultClassificationPreservesProducerPriority(t *testing.T) {
	code := func(c p.Code) *p.Code { return &c }
	for _, test := range []struct {
		name          string
		change        func(*p.RunCommandResult)
		outcome, code string
	}{
		{"zero", func(*p.RunCommandResult) {}, "success", ""},
		{"nonzero", func(r *p.RunCommandResult) { r.ExitCode = 23 }, "failure", "command_exit_nonzero"},
		{"signal-before-exit", func(r *p.RunCommandResult) { r.ExitCode = -1; r.Signal = 15 }, "failure", "command_signaled"},
		{"cancel-before-exit", func(r *p.RunCommandResult) {
			r.ExitCode = 23
			r.Cancelled = true
			r.OutputComplete = false
			r.CompletionCode = code(p.CancelledCode)
		}, "cancelled", string(p.CancelledCode)},
		{"timeout-before-signal", func(r *p.RunCommandResult) {
			r.ExitCode = -1
			r.Signal = 9
			r.Cancelled = true
			r.TimedOut = true
			r.OutputComplete = false
			r.CompletionCode = code(p.Timeout)
		}, "cancelled", string(p.Timeout)},
		{"io-before-cancel", func(r *p.RunCommandResult) {
			r.Cancelled = true
			r.TimedOut = true
			r.OutputComplete = false
			r.CompletionCode = code(p.InternalError)
		}, "failure", string(p.InternalError)},
		{"truncation-not-failure", func(r *p.RunCommandResult) { r.StdoutTruncated = true }, "success", ""},
		{"invalid-report", func(r *p.RunCommandResult) { r.State = "unknown" }, "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := commandBundleFixture(nil, nil).Report
			test.change(&r)
			outcome, c := ClassifyRunCommandResult(r)
			if outcome != test.outcome || c != test.code {
				t.Fatal("classification priority changed", outcome, c)
			}
			if outcome != "" && !ValidRunCommandResultClassification(outcome, c) {
				t.Fatal("classified report excluded by binding contract")
			}
		})
	}
	for _, pair := range [][2]string{{"unknown", ""}, {"success", "command_exit_nonzero"}, {"failure", "CANCELLED"}, {"cancelled", "INTERNAL_ERROR"}, {"failure", "arbitrary"}} {
		if ValidRunCommandResultClassification(pair[0], pair[1]) {
			t.Fatal("unrecognized or mismatched outcome accepted")
		}
	}
	// A legacy incomplete classification stays recognizable for stored identity,
	// but an invalid incomplete protocol report cannot manufacture that evidence.
	if !ValidRunCommandResultClassification("failure", "output_incomplete") {
		t.Fatal("original closed classification removed")
	}
}
