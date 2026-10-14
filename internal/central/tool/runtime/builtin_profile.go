package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// BuiltinInvocation is trusted composition metadata, not a bearer permission.
// Domain authorities require the original live invocation context as well.
type BuiltinInvocation struct {
	Call        tc.ToolCallBinding
	OperationID tc.OperationID
	AttemptID   tc.AttemptID
	RequestID   p.ID
}

func (v BuiltinInvocation) Validate() error {
	if v.Call.Validate() != nil || !tc.WorkArtifactBuiltinBinding(v.Call.Binding) || v.OperationID.Validate() != nil || v.AttemptID.Validate() != nil || !v.RequestID.Valid() {
		return fail(f.InvalidArgument)
	}
	return nil
}
func (BuiltinInvocation) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "builtin_invocation") }
func (BuiltinInvocation) LogValue() slog.Value         { return slog.StringValue("builtin_invocation") }
func (BuiltinInvocation) MarshalJSON() ([]byte, error) { return []byte(`"builtin_invocation"`), nil }

// BuiltinBackend is fixed by the App owner. RequireBindingInTx must verify the
// real domain Service, Store and ownership; a descriptor alone is not callable.
// Execute is synchronous. Lookup may inspect canonical idempotency receipts,
// but must never resend a domain mutation or repeat object/Runner I/O.
type BuiltinBackend interface {
	Registration() tc.BuiltinRegistration
	RequireBindingInTx(context.Context, f.Tx, any) error
	Execute(context.Context, BuiltinInvocation, []byte) ([]byte, error)
	Lookup(context.Context, BuiltinInvocation, []byte) ([]byte, bool, error)
}

type builtinRecord struct {
	ID          tc.OperationID
	Input       bindingData
	InputDigest f.Digest
	Fingerprint f.Digest
	Phase       string
	Version     f.Version
	Attempt     tc.AttemptID
	Request     p.ID
}

func builtinInput(ctx context.Context, call tc.ToolCallBinding, raw []byte) (bindingData, []byte, error) {
	if call.Validate() != nil || !tc.WorkArtifactBuiltinBinding(call.Binding) || int64(len(raw)) != int64(call.Input.Bytes) || digest(raw) != call.Input.SHA256 {
		return bindingData{}, nil, fail(f.Forbidden)
	}
	canonical, err := cursor.CanonicalJSON(raw)
	if err != nil || digest(canonical) != call.CanonicalArguments {
		return bindingData{}, nil, fail(f.InvalidArgument)
	}
	if err = ctx.Err(); err != nil {
		return bindingData{}, nil, err
	}
	in := bindingProjection(call)
	encoded, err := encode(in)
	return in, encoded, err
}

func (r builtinRecord) validate() error {
	call, err := r.Input.request()
	if err != nil || !tc.WorkArtifactBuiltinBinding(call.Binding) || r.ID.Validate() != nil {
		return fail(f.InvalidState)
	}
	raw, err := encode(r.Input)
	if err != nil || digest(raw) != r.InputDigest || r.Fingerprint != r.InputDigest {
		return fail(f.InvalidState)
	}
	switch r.Phase {
	case "created":
		if r.Version == 1 && r.Attempt == (tc.AttemptID{}) && r.Request == "" {
			return nil
		}
	case "running":
		if r.Version == 2 && r.Attempt.Validate() == nil && r.Request.Valid() {
			return nil
		}
	case "succeeded", "failed", "cancelled":
		if r.Version == 3 && r.Attempt.Validate() == nil && r.Request.Valid() {
			return nil
		}
	}
	return fail(f.InvalidState)
}

func decodeBuiltinInput(raw []byte) (bindingData, error) {
	var in bindingData
	if len(raw) < 2 || len(raw) > 32768 {
		return in, fail(f.InvalidState)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&in) != nil {
		return in, fail(f.InvalidState)
	}
	canonical, err := encode(in)
	if err != nil || !bytes.Equal(raw, canonical) {
		return in, fail(f.InvalidState)
	}
	if call, err := in.request(); err != nil || !tc.WorkArtifactBuiltinBinding(call.Binding) {
		return in, fail(f.InvalidState)
	}
	return in, nil
}

type builtinTerminal struct {
	Format       int
	Operation    tc.OperationID
	Attempt      tc.AttemptID
	Request      p.ID
	Outcome      string
	Code         string
	ResultDigest f.Digest
	ResultBytes  f.Progress
}

func (v builtinTerminal) validate(r builtinRecord, raw []byte) error {
	if v.Format != 1 || v.Operation != r.ID || v.Attempt != r.Attempt || v.Request != r.Request ||
		int64(v.ResultBytes) != int64(len(raw)) || v.ResultDigest != digest(raw) || len(raw) < 2 || len(raw) > 1<<20 || !json.Valid(raw) {
		return fail(f.InvalidState)
	}
	if v.Outcome == "success" && v.Code == "" || v.Outcome == "failure" && f.Code(v.Code).Known() || v.Outcome == "cancelled" && v.Code == "cancelled" {
		return nil
	}
	return fail(f.InvalidState)
}

func (v builtinTerminal) phases() (string, string) {
	switch v.Outcome {
	case "success":
		return "succeeded", "success"
	case "failure":
		return "failed", "error"
	case "cancelled":
		return "cancelled", "cancelled"
	}
	return "", ""
}

func (v builtinTerminal) binding(call tc.ToolCallBinding, r builtinRecord, terminal []byte) tc.RunnerResultBinding {
	return tc.RunnerResultBinding{Call: call, OperationID: r.ID, AttemptID: r.Attempt, RequestID: r.Request,
		InputDigest: r.InputDigest, Fingerprint: r.Fingerprint, TerminalDigest: digest(terminal), Outcome: v.Outcome,
		Code: v.Code, Attempted: true, ResultDigest: v.ResultDigest, ResultBytes: v.ResultBytes}
}
