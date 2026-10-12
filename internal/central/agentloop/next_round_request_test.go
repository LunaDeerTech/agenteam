package agentloop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// These sources exercise the pure codec/projection boundary only. They are
// not committed-source authority, actual Model calls, or a substitute for PG.
func nextRoundRequestParts(v ec.NextRoundModelRequest) ec.NextRoundModelRequestInput {
	fields, r := v.Fields(), v.Request()
	return ec.NextRoundModelRequestInput{
		ProjectID: fields.ProjectID, AgentID: fields.AgentID, ExecutionID: fields.ExecutionID,
		RoundInputID: fields.RoundInputID, RoundInputDigest: fields.RoundInputDigest,
		SnapshotID: fields.SnapshotID, SnapshotDigest: fields.SnapshotDigest,
		ProjectionDigest: fields.ProjectionDigest, CallID: fields.CallID,
		Actor: r.Actor, Model: r.Model, Messages: r.Messages, Tools: r.Tools,
	}
}

func TestNextRoundRequestPreservesProfilesAndStableIdentity(t *testing.T) {
	for _, name := range []string{"read", "read-control", "write-maximum", "command-escaped-nonzero"} {
		t.Run(name, func(t *testing.T) {
			var source ec.RoundProjectionSource
			switch name {
			case "read":
				source = roundProjectionFixture(t, false)
			case "read-control":
				source = roundProjectionFixture(t, true)
			case "write-maximum":
				source = writeRoundProjectionFixture(t, bytes.Repeat([]byte{0, 255}, p.MaxWriteFileBytes/2), nil)
			case "command-escaped-nonzero":
				stdout, stderr := bytes.Repeat([]byte{0}, p.MaxRunCommandStreamBytes), bytes.Repeat([]byte{1}, p.MaxRunCommandStreamBytes)
				report := commandToolResultTestReport(stdout, stderr)
				report.ExitCode = 23
				source = commandRoundProjectionFixture(t, tc.RunCommandResultBundle{Report: report, Stdout: stdout, Stderr: stderr}, "failure", "command_exit_nonzero")
			}
			q := source.Input.Fields()
			call := requestID[mc.Call](t, 2401)
			got, err := BuildNextRoundModelRequest(context.Background(), source, call)
			requestOK(t, err)
			requestOK(t, got.Validate())
			r := got.Request()
			projection, err := projectRoundSource(context.Background(), q.ExecutionID, q.RoundID, source)
			requestOK(t, err)
			if !reflect.DeepEqual(r.Messages, projection.Messages()) || !reflect.DeepEqual(r.Tools, projection.Tools()) || r.Input.SchemaVersion != 3 || r.Input.RoundID != q.RoundID.String() || r.CallID != call || r.ToolChoice != (mc.ToolChoice{Kind: "auto"}) || r.ResponseFormat.Kind != "text" || r.RetryClass != mc.AgentRetry || got.Fields().ProjectionDigest != projection.Digest() || got.Fields().RoundInputDigest != source.Input.Digest() {
				t.Fatal("next request changed original projection, identity, or explicit profile")
			}
			captured := source.Snapshot.Fields().Context.Input().Fields()
			actor, err := i.NewAgentRun(captured.Request.Launch.ProjectID, captured.Request.Launch.AgentID, captured.Request.ExecutionID)
			requestOK(t, err)
			modelBytes, err := json.Marshal(r.Model)
			requestOK(t, err)
			capturedBytes, err := json.Marshal(captured.Model)
			requestOK(t, err)
			if !r.Actor.Equal(actor) || !bytes.Equal(modelBytes, capturedBytes) || !r.Model.LeaseOwner.Equal(captured.Model.LeaseOwner) || r.Model.CredentialLease.LeaseID != captured.Model.CredentialLease.LeaseID || !r.Model.CredentialLease.CredentialRef.Equal(captured.Model.CredentialLease.CredentialRef) {
				t.Fatal("captured Actor, Model, or lease changed")
			}
			raw := got.CanonicalBytes()
			decoded, err := ec.DecodeNextRoundModelRequest(context.Background(), raw)
			requestOK(t, err)
			if decoded.Fields() != got.Fields() || decoded.Digest() != got.Digest() || !bytes.Equal(decoded.CanonicalBytes(), raw) || !reflect.DeepEqual(decoded.Request().Messages, r.Messages) || !decoded.Request().Actor.Equal(r.Actor) {
				t.Fatal("complete request did not round trip with its original identities")
			}
			replayed, err := BuildNextRoundModelRequest(context.Background(), source, call)
			requestOK(t, err)
			if !bytes.Equal(replayed.CanonicalBytes(), raw) || replayed.Digest() != got.Digest() {
				t.Fatal("same source and call changed request bytes")
			}
			other, err := BuildNextRoundModelRequest(context.Background(), source, requestID[mc.Call](t, 2402))
			requestOK(t, err)
			if other.InputDigest() != got.InputDigest() || other.Digest() == got.Digest() {
				t.Fatal("semantic input and logical call binding were conflated")
			}
			if source.Predecessor.Fields().Round.Input.SchemaVersion != 2 || source.Predecessor.Fields().Round.Input.Digest == got.InputDigest() {
				t.Fatal("new request reused or changed the initial schema-2 digest")
			}
		})
	}
}

func TestNextRoundRequestCodecRejectsIncompleteBinding(t *testing.T) {
	source := roundProjectionFixture(t, true)
	good, err := BuildNextRoundModelRequest(context.Background(), source, requestID[mc.Call](t, 2410))
	requestOK(t, err)
	for _, name := range []string{"actor", "round", "input-digest", "input-schema", "body-schema", "tool-choice", "retry", "response-format", "model-parameters", "model-lease", "message", "tool-schema", "unknown-field", "duplicate-field", "noncanonical"} {
		t.Run(name, func(t *testing.T) {
			raw := good.CanonicalBytes()
			var value map[string]any
			requestOK(t, json.Unmarshal(raw, &value))
			semantic := value["semantic"].(map[string]any)
			switch name {
			case "actor":
				semantic["actor"].(map[string]any)["agent_id"] = requestID[mc.Call](t, 2411).String()
			case "round":
				value["round_input_id"] = requestID[ec.Round](t, 2412).String()
			case "input-digest":
				value["input"].(map[string]any)["digest"] = string(toolResultTestDigest([]byte("other")))
			case "input-schema":
				value["input"].(map[string]any)["schema_version"] = "2"
			case "body-schema":
				value["schema_version"] = "2"
			case "tool-choice":
				semantic["tool_choice"].(map[string]any)["kind"] = "required"
			case "retry":
				semantic["retry_class"] = "bounded_consumer"
			case "response-format":
				semantic["response_format"].(map[string]any)["kind"] = "json_schema"
			case "model-parameters":
				semantic["model"].(map[string]any)["snapshot"].(map[string]any)["parameters"] = map[string]any{"temperature": 1}
			case "model-lease":
				semantic["model"].(map[string]any)["credential_lease"].(map[string]any)["lease_id"] = requestID[mc.Call](t, 2413).String()
			case "message":
				semantic["messages"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(map[string]any)["text"] = "changed system"
			case "tool-schema":
				semantic["tools"].([]any)[0].(map[string]any)["input_schema"] = map[string]any{"type": "object"}
			case "unknown-field":
				value["committed"] = true
			}
			if name == "duplicate-field" {
				raw = append([]byte(`{"schema_version":"1",`), raw[1:]...)
			} else if name == "noncanonical" {
				raw = append([]byte(" "), raw...)
			} else {
				raw = roundProjectionJSON(t, value)
			}
			got, err := ec.DecodeNextRoundModelRequest(context.Background(), raw)
			if err == nil || got.Validate() == nil || got.CanonicalBytes() != nil {
				t.Fatal("damaged candidate became a valid complete request")
			}
		})
	}
}

func TestNextRoundRequestOwnsProtectedCopiesAndRejectsBadSources(t *testing.T) {
	source := roundProjectionFixture(t, true)
	got, err := BuildNextRoundModelRequest(context.Background(), source, requestID[mc.Call](t, 2420))
	requestOK(t, err)
	original := got.CanonicalBytes()
	r := got.Request()
	r.Messages[0].Parts[0].Text.Text = "overwritten"
	r.Tools[0].InputSchema[0] = '!'
	r.Model.Snapshot.Parameters[0] = '!'
	r.Input.ExecutionID = nil
	borrowed := got.CanonicalBytes()
	clear(borrowed)
	clear(source.Arguments)
	for n := range source.Transcript {
		clear(source.Transcript[n].CanonicalMessage)
	}
	if !bytes.Equal(got.CanonicalBytes(), original) || got.Request().Messages[0].Parts[0].Text.Text == "overwritten" || got.Request().Tools[0].InputSchema[0] == '!' || got.Request().Model.Snapshot.Parameters[0] == '!' {
		t.Fatal("returned copies or expired source loan changed owned request")
	}
	encoded, err := json.Marshal(got)
	requestOK(t, err)
	for _, text := range []string{fmt.Sprintf("%#v", got), string(encoded), got.LogValue().String()} {
		if strings.Contains(text, "private-content") || strings.Contains(text, got.Fields().CallID.String()) || strings.Contains(text, got.Request().Model.CredentialLease.LeaseID.String()) {
			t.Fatal("default request formatting exposed protected data")
		}
	}
	var logs bytes.Buffer
	slog.New(slog.NewJSONHandler(&logs, nil)).Info("candidate", "request", got)
	if strings.Contains(logs.String(), "private-content") {
		t.Fatal("structured log exposed request")
	}
	var fabricated ec.NextRoundModelRequest
	if json.Unmarshal(original, &fabricated) == nil || fabricated.Validate() == nil {
		t.Fatal("default JSON created a usable candidate")
	}
	for _, name := range []string{"reused-call", "missing-control", "changed-arguments", "changed-result", "cancelled", "nil-context"} {
		t.Run(name, func(t *testing.T) {
			s := roundProjectionFixture(t, true)
			ctx := context.Background()
			call := requestID[mc.Call](t, 2421)
			switch name {
			case "reused-call":
				call = s.Predecessor.Fields().Round.CallID
			case "missing-control":
				s.ControlCanonical = nil
			case "changed-arguments":
				s.Arguments = []byte(`{}`)
			case "changed-result":
				s.Result.ResultDigest = toolResultTestDigest([]byte("different"))
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil-context":
				ctx = nil
			}
			out, err := BuildNextRoundModelRequest(ctx, s, call)
			if err == nil || out.Validate() == nil || out.CanonicalBytes() != nil {
				t.Fatal("invalid source/cancel/reused logical call produced a request")
			}
			if name == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation was replaced")
			}
		})
	}
}

func TestNextRoundRequestCountsEscapedSizeBeforeEncoding(t *testing.T) {
	source := roundProjectionFixture(t, false)
	good, err := BuildNextRoundModelRequest(context.Background(), source, requestID[mc.Call](t, 2430))
	requestOK(t, err)
	parts := nextRoundRequestParts(good)
	// This string fits Model TextPart's existing raw 16 MiB limit, but its
	// actual JSON spelling alone exceeds this complete request's budget.
	parts.Messages[0].Parts[0].Text.Text = strings.Repeat("<", ec.MaxNextRoundModelRequestBytes/6+1)
	got, err := ec.NewNextRoundModelRequest(context.Background(), parts)
	var fault *f.Fault
	if !errors.As(err, &fault) || fault.Code != f.PayloadTooLarge || got.Validate() == nil {
		t.Fatal("escaping crossed the bounded request encoder")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = ec.DecodeNextRoundModelRequest(ctx, good.CanonicalBytes())
	if !errors.Is(err, context.Canceled) || got.Validate() == nil {
		t.Fatal("cancelled decode published a candidate")
	}
}
