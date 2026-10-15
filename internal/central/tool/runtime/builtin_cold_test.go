package runtime

import (
	"bytes"
	"context"
	"errors"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"reflect"
	"testing"

	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// These controls test private-proof and original-tuple rejection. Only the App
// child integration scenario proves the real ProcessGuard stopped claim.
func coldProofFixture(t *testing.T) (*BuiltinExecutor, *builtinCall, *builtinColdProof) {
	t.Helper()
	h := readFileHandoffFixture(t)
	binding := h.binding
	binding.RunnerBinding = tc.RunnerBinding{}
	binding.Binding = tc.BuiltinBinding{HandlerID: "memory.retain", ContractRevision: 1}
	in := bindingProjection(binding)
	raw, err := encode(in)
	if err != nil {
		t.Fatal(err)
	}
	call := &builtinCall{binding: binding, lookup: true, record: builtinRecord{ID: h.record.ID, Input: in, InputDigest: digest(raw), Fingerprint: digest(raw), Phase: "running", Version: 2, Attempt: h.attempt, Request: h.request}}
	e := &BuiltinExecutor{authority: &BuiltinAuthority{process: newOperationID[oc.Process](t)}, lookups: map[*builtinCall]struct{}{call: {}}, calls: map[tc.ToolCallIdentity]*builtinCall{}}
	p := &builtinColdProof{owner: e, call: call, binding: binding, record: call.record, process: newOperationID[oc.Process](t)}
	p.active.Store(true)
	call.cold = p
	if !p.permits(e.authority.process, call.record) {
		t.Fatal("invalid closed cold-proof control")
	}
	return e, call, p
}

func TestBuiltinColdProofCannotTransferOrAuthorizeExecute(t *testing.T) {
	cases := map[string]func(*BuiltinExecutor, *builtinCall, *builtinColdProof){
		"retired-borrow": func(_ *BuiltinExecutor, _ *builtinCall, p *builtinColdProof) { p.active.Store(false) },
		"absent-lookup":  func(e *BuiltinExecutor, call *builtinCall, _ *builtinColdProof) { delete(e.lookups, call) },
		"other-call":     func(_ *BuiltinExecutor, call *builtinCall, p *builtinColdProof) { other := *call; p.call = &other },
		"execute":        func(_ *BuiltinExecutor, call *builtinCall, _ *builtinColdProof) { call.lookup = false },
		"same-process":   func(e *BuiltinExecutor, _ *builtinCall, p *builtinColdProof) { p.process = e.authority.process },
		"live-local-owner": func(e *BuiltinExecutor, call *builtinCall, _ *builtinColdProof) {
			e.calls[call.binding.Identity()] = &builtinCall{}
		},
		"stopped-executor": func(e *BuiltinExecutor, _ *builtinCall, _ *builtinColdProof) { e.stopped = true },
		"other-operation": func(_ *BuiltinExecutor, call *builtinCall, _ *builtinColdProof) {
			call.record.ID = newOperationID[tc.Operation](t)
		},
		"other-attempt": func(_ *BuiltinExecutor, call *builtinCall, _ *builtinColdProof) {
			call.record.Attempt = newOperationID[tc.Attempt](t)
		},
		"other-request": func(_ *BuiltinExecutor, call *builtinCall, _ *builtinColdProof) {
			call.record.Request = "other-request"
		},
		"other-input": func(_ *BuiltinExecutor, call *builtinCall, _ *builtinColdProof) {
			call.binding.RoundID = newOperationID[struct{}](t).String()
		},
		"other-backend": func(_ *BuiltinExecutor, call *builtinCall, _ *builtinColdProof) {
			call.binding.Binding.HandlerID = "memory.recall"
		},
		"install": func(_ *BuiltinExecutor, call *builtinCall, _ *builtinColdProof) {
			call.binding.Binding.HandlerID = "skill.install"
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e, call, p := coldProofFixture(t)
			mutate(e, call, p)
			if p.permits(e.authority.process, call.record) {
				t.Fatal("cold receipt proof crossed its original owner boundary")
			}
		})
	}
}

type coldAttemptSQL struct {
	postgres.SQLExecutor
	t      *testing.T
	record builtinRecord
	values []any
}

func (s *coldAttemptSQL) QueryRow(_ context.Context, _ string, args ...any) postgres.Row {
	s.t.Helper()
	want := []any{s.record.ID.String(), s.record.Attempt.String(), s.record.Input.Execution.String(), "memory.retain"}
	if !reflect.DeepEqual(args, want) {
		s.t.Fatal("attempt observer lost the original exact operation")
	}
	return controlRow{values: s.values}
}

func TestBuiltinColdAttemptKeepsOriginalProcessAndRejectsChangedTuple(t *testing.T) {
	e, call, proof := coldProofFixture(t)
	values := []any{proof.process.String(), string(call.record.Request), int64(1), int64(1), string(call.record.InputDigest), string(call.record.Fingerprint)}
	s := &coldAttemptSQL{t: t, record: call.record, values: values}
	if err := requireBuiltinAttempt(context.Background(), s, e.authority.process, call.record); err == nil {
		t.Fatal("foreign process accepted without stopped-process proof")
	}
	if err := requireBuiltinAttempt(context.Background(), s, e.authority.process, call.record, proof); err != nil {
		t.Fatal(err)
	}
	for n, changed := range []any{e.authority.process.String(), "other-request", int64(2), int64(2), string(digest([]byte("other-input"))), string(digest([]byte("other-fingerprint")))} {
		s.values = append([]any(nil), values...)
		s.values[n] = changed
		if err := requireBuiltinAttempt(context.Background(), s, e.authority.process, call.record, proof); err == nil {
			t.Fatalf("cold receipt accepted changed tuple column=%d", n)
		}
	}
	if call.record != proof.record {
		t.Fatal("cold observation rewrote original record")
	}
}

func TestBuiltinColdResolverLeavesUnsupportedBackendUnchanged(t *testing.T) {
	for _, handler := range []string{"task.transfer", "skill.install", "mcp.read-resource", "memory.recall"} {
		e, call, _ := coldProofFixture(t)
		call.binding.Binding.HandlerID = handler
		// No Store or ProcessGuard: an unrelated backend must never reach them.
		if err := e.resolveCold(context.Background(), call); err != nil || len(e.calls) != 0 {
			t.Fatal("cold resolver widened backend support", err)
		}
	}
}

type coldCanonicalControl struct {
	BuiltinBackend
	result   BuiltinCanonicalOutcome
	err      error
	panics   bool
	witness  *builtinWitness
	borrowed []byte
}

func (b *coldCanonicalControl) LookupCanonical(ctx context.Context, _ BuiltinInvocation, raw []byte) (BuiltinCanonicalOutcome, error) {
	b.witness, _ = ctx.Value(builtinWitnessKey{}).(*builtinWitness)
	b.borrowed = raw
	if b.panics {
		panic("controlled")
	}
	return b.result, b.err
}

func TestBuiltinColdCanonicalNegativeNeedsExplicitFactAndOriginalBorrow(t *testing.T) {
	for name, result := range map[string]BuiltinCanonicalOutcome{
		"absent": {}, "success": {Found: true, Body: []byte(`{"retained":true}`)}, "negative": {Found: true, Failure: f.DependencyUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			e, call, _ := coldProofFixture(t)
			call.arguments = []byte(`{"original":true}`)
			backend := &coldCanonicalControl{result: result}
			got, err := e.invokeCanonicalLookup(context.Background(), call, backend)
			if err != nil || got.Found != result.Found || got.Failure != result.Failure || !bytes.Equal(got.Body, result.Body) {
				t.Fatal("canonical fact changed", err)
			}
			if backend.witness == nil || backend.witness.active.Load() || !bytes.Equal(backend.borrowed, make([]byte, len(call.arguments))) || string(call.arguments) != `{"original":true}` {
				t.Fatal("original argument/witness loan escaped")
			}
			if call.terminal != nil {
				t.Fatal("lookup bypassed original publication")
			}
		})
	}
	for name, result := range map[string]BuiltinCanonicalOutcome{
		"missing-with-code": {Failure: f.DependencyUnavailable}, "unknown-code": {Found: true, Failure: f.CommitUnknown}, "arbitrary-code": {Found: true, Failure: f.Forbidden}, "failure-with-body": {Found: true, Failure: f.DependencyUnavailable, Body: []byte(`{}`)}, "empty-success": {Found: true},
	} {
		t.Run(name, func(t *testing.T) {
			e, call, _ := coldProofFixture(t)
			if _, err := e.invokeCanonicalLookup(context.Background(), call, &coldCanonicalControl{result: result}); err == nil {
				t.Fatal("ambiguous outcome terminalized")
			}
		})
	}
	for _, panicControl := range []bool{false, true} {
		e, call, _ := coldProofFixture(t)
		original := f.NewFault(f.CommitUnknown, f.Unknown)
		b := &coldCanonicalControl{result: BuiltinCanonicalOutcome{Found: true, Failure: f.DependencyUnavailable}, err: original, panics: panicControl}
		got, err := e.invokeCanonicalLookup(context.Background(), call, b)
		var fault *f.Fault
		if !errors.As(err, &fault) || fault.Code != f.CommitUnknown || fault.CommitState != f.Unknown || got.Found || call.terminal != nil || b.witness.active.Load() {
			t.Fatal("error/panic became canonical failure", err)
		}
	}
	e, call, proof := coldProofFixture(t)
	proof.active.Store(false)
	b := &coldCanonicalControl{}
	if _, err := e.invokeCanonicalLookup(context.Background(), call, b); err == nil || b.witness != nil {
		t.Fatal("expired cold proof invoked domain")
	}
}
