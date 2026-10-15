package runtime

import (
	"bytes"
	"context"
	"strings"
	"testing"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func skillPrepareTerminalBody(t *testing.T, outcome string) []byte {
	t.Helper()
	r := sc.PrepareUnknownResult{SkillID: newOperationID[pc.Skill](t), RevisionID: newOperationID[sc.Revision](t), Revision: 1, MountID: newOperationID[id.Mount](t), Outcome: "unknown", MayHavePrepared: true}
	r.Error.Code = f.DependencyUnavailable
	if outcome == "unknown" {
		body, err := sc.EncodePrepareUnknownResult(r)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	u := sc.PrepareUnissuedResult{SkillID: r.SkillID, RevisionID: r.RevisionID, Revision: r.Revision, MountID: r.MountID, Outcome: "failure", MayHavePrepared: false, Error: r.Error}
	body, err := sc.EncodePrepareUnissuedResult(u)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestSkillPrepareTerminalPreservesReturnedEffectAndExactBinding(t *testing.T) {
	h := readFileHandoffFixture(t)
	call := h.binding
	call.RunnerBinding = tc.RunnerBinding{}
	call.Binding = tc.BuiltinBinding{HandlerID: "skill.prepare", ContractRevision: 1}
	r := builtinRecord{ID: h.record.ID, Attempt: h.attempt, Request: h.request, Input: bindingProjection(call), InputDigest: h.record.InputDigest, Fingerprint: h.record.Fingerprint}
	for _, want := range []string{"unknown", "failure"} {
		t.Run(want, func(t *testing.T) {
			body := skillPrepareTerminalBody(t, want)
			e := &BuiltinExecutor{}
			outcome, code, err := e.backendTerminal(&builtinCall{binding: call}, body)
			if err != nil || outcome != want || code != string(f.DependencyUnavailable) {
				t.Fatal("returned effect was promoted or lost", err)
			}
			term := builtinTerminal{Format: 1, Operation: r.ID, Attempt: r.Attempt, Request: r.Request, Outcome: outcome, Code: code, ResultDigest: digest(body), ResultBytes: f.Progress(len(body))}
			if err := term.validate(r, body); err != nil {
				t.Fatal(err)
			}
			encoded, _ := encode(term)
			result := term.binding(call, r, encoded)
			if err := result.Validate(); err != nil {
				t.Fatal("exact original result contract refused returned effect", err)
			}
			promoted := term
			promoted.Outcome, promoted.Code = "success", ""
			if promoted.validate(r, body) == nil {
				t.Fatal("unknown/unissued body was accepted as a successful preparation")
			}
			if want == "unknown" {
				for _, binding := range []tc.BuiltinBinding{{HandlerID: "skill.read", ContractRevision: 1}, {HandlerID: "skill.prepare", ContractRevision: 2}} {
					foreign := result
					foreign.Call.Binding = binding
					if foreign.Validate() == nil {
						t.Fatal("unrelated handler gained terminal unknown")
					}
				}
				if phase, visible := term.phases(); phase != "failed" || visible != "unknown" || term.attemptPhase() != "unknown" {
					t.Fatal("returned unknown changed the original operation ledger")
				}
			}
			bad := []byte(strings.TrimSuffix(string(body), "}") + `,"path":"unproven"}`)
			if _, _, err := e.backendTerminal(&builtinCall{binding: call}, bad); err == nil {
				t.Fatal("a returned failure lent an unproven package path")
			}
		})
	}
}

type prepareReturnedBackend struct {
	BuiltinBackend
	body    []byte
	lookups int
}

func (b *prepareReturnedBackend) Lookup(context.Context, BuiltinInvocation, []byte) ([]byte, bool, error) {
	b.lookups++
	return bytes.Clone(b.body), true, nil
}

// This control deliberately refuses publication. It observes the real
// resolveReturned classification before any SQL, without faking a commit or
// original Runner/Transfer retirement proof.
type preparePublicationBarrier struct {
	Store
	calls int
}

func (s *preparePublicationBarrier) WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult {
	s.calls++
	return f.NotCommittedResult(f.NewFault(f.DependencyUnavailable, f.NotCommitted))
}

func TestSkillPrepareReturnedLookupKeepsEffectAndRequiresOriginalReturn(t *testing.T) {
	for _, outcome := range []string{"unknown", "failure"} {
		t.Run(outcome, func(t *testing.T) {
			h := readFileHandoffFixture(t)
			binding := h.binding
			binding.RunnerBinding = tc.RunnerBinding{}
			binding.Binding = tc.BuiltinBinding{HandlerID: "skill.prepare", ContractRevision: 1}
			body := skillPrepareTerminalBody(t, outcome)
			backend := &prepareReturnedBackend{body: body}
			store := &preparePublicationBarrier{}
			record := builtinRecord{ID: h.record.ID, Attempt: h.attempt, Request: h.request, Input: bindingProjection(binding), InputDigest: h.record.InputDigest}
			old := &builtinCall{binding: binding, record: record, done: make(chan struct{}), arguments: []byte(`{}`)}
			e := &BuiltinExecutor{authority: &BuiltinAuthority{store: store}, calls: map[tc.ToolCallIdentity]*builtinCall{binding.Identity(): old}, backends: map[tc.BuiltinBinding]BuiltinBackend{binding.Binding: backend}}
			observed := &builtinCall{binding: binding, record: record}
			if e.resolveReturned(context.Background(), observed) == nil || backend.lookups != 0 || store.calls != 0 {
				t.Fatal("an unreturned call became a result lookup")
			}
			old.domainReturned = true
			close(old.done)
			if e.resolveReturned(context.Background(), observed) == nil || backend.lookups != 1 || store.calls != 1 {
				t.Fatal("lookup repeated execution or bypassed the publication barrier")
			}
			if observed.terminal == nil || observed.terminal.Outcome != outcome || observed.terminal.Code != string(f.DependencyUnavailable) || !bytes.Equal(observed.body, body) || old.terminal == nil || *old.terminal != *observed.terminal {
				t.Fatal("returned lookup promoted or replaced the exact retained result")
			}
		})
	}
}
