package execution

import (
	"context"
	"strings"
	"testing"

	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	p "github.com/LunaDeerTech/agenteam/internal/runnerprotocol"
)

// These controls exercise only Execution's metadata and result-provider gates.
// The cold App test proves the real retired attempt and opaque result reader.
func prepareUnknownCandidateControl(t *testing.T) (tc.ToolCallBinding, tc.RunnerOutcome, tc.RunnerResultBinding) {
	t.Helper()
	project, agent, execution := newTestID[i.Project](t), newTestID[i.Agent](t), newTestID[i.Execution](t)
	actor, err := i.NewAgentRun(project, agent, execution)
	if err != nil {
		t.Fatal(err)
	}
	tool := newTestID[i.Tool](t)
	digest := c.TriggerInputDigest([]byte("original-prepare-result"))
	binding := tc.ToolCallBinding{
		Actor: actor, ProjectID: project, AgentID: agent, ExecutionID: execution,
		RoundID: newTestID[c.Round](t).String(), SnapshotID: newTestID[struct{}](t).String(), InputBindingID: newTestID[struct{}](t).String(),
		LogicalCallID: newTestID[mc.Call](t), InvocationID: newTestID[mc.Invocation](t), CallID: "original-prepare-call",
		ModelVisibleName: "tool_" + strings.ReplaceAll(tool.String(), "-", ""), Spec: tc.SpecRef{ToolID: tool, SpecRevision: 1},
		Binding: tc.BuiltinBinding{HandlerID: "skill.prepare", ContractRevision: 1},
		Input:   tc.ToolInputReference{PayloadID: newTestID[struct{}](t).String(), SHA256: digest, Bytes: 2}, CanonicalArguments: digest,
	}
	out := tc.RunnerOutcome{OperationID: newTestID[tc.Operation](t), AttemptID: newTestID[tc.Attempt](t), Phase: "failed", Outcome: "unknown", Attempted: true, ResultAvailable: true}
	result := tc.RunnerResultBinding{Call: binding, OperationID: out.OperationID, AttemptID: out.AttemptID,
		RequestID: p.ID(newTestID[struct{}](t).String()), InputDigest: digest, Fingerprint: digest, TerminalDigest: digest,
		Outcome: "unknown", Code: string(f.DependencyUnavailable), Attempted: true, ResultDigest: digest, ResultBytes: 2}
	if binding.Validate() != nil || result.Validate() != nil {
		t.Fatal("invalid controlled prepare result")
	}
	return binding, out, result
}

func TestMultiRoundPrepareUnknownKeepsUncertainDispatchClosed(t *testing.T) {
	binding, out, _ := prepareUnknownCandidateControl(t)
	if loopDispatchUnknown(binding, out, nil) || loopPayloadGate(binding, out) != nil {
		t.Fatal("canonical prepare candidate could not reach its original result reader")
	}
	if !loopDispatchUnknown(binding, out, f.NewFault(f.CommitUnknown, f.Unknown)) {
		t.Fatal("terminal metadata masked an uncertain dispatch transaction")
	}
	for name, mutate := range map[string]func(*tc.ToolCallBinding, *tc.RunnerOutcome){
		"other builtin":  func(b *tc.ToolCallBinding, _ *tc.RunnerOutcome) { b.Binding.HandlerID = "skill.read" },
		"other revision": func(b *tc.ToolCallBinding, _ *tc.RunnerOutcome) { b.Binding.ContractRevision = 2 },
		"runner": func(b *tc.ToolCallBinding, _ *tc.RunnerOutcome) {
			b.Binding = tc.BuiltinBinding{}
			b.RunnerBinding = tc.ReadFileRegistration().Binding
		},
		"changed actor scope": func(b *tc.ToolCallBinding, _ *tc.RunnerOutcome) { b.ProjectID = newTestID[i.Project](t) },
		"running":             func(_ *tc.ToolCallBinding, o *tc.RunnerOutcome) { o.Phase = "running" },
		"not attempted":       func(_ *tc.ToolCallBinding, o *tc.RunnerOutcome) { o.Attempted = false },
		"no payload":          func(_ *tc.ToolCallBinding, o *tc.RunnerOutcome) { o.ResultAvailable = false },
		"no operation":        func(_ *tc.ToolCallBinding, o *tc.RunnerOutcome) { o.OperationID = tc.OperationID{} },
		"no attempt":          func(_ *tc.ToolCallBinding, o *tc.RunnerOutcome) { o.AttemptID = tc.AttemptID{} },
	} {
		t.Run(name, func(t *testing.T) {
			b, o := binding, out
			mutate(&b, &o)
			if !loopDispatchUnknown(b, o, nil) || !directTextUnknown(loopPayloadGate(b, o)) {
				t.Fatal("unresolved or unrelated Unknown became a result candidate")
			}
		})
	}
}

type prepareUnknownResultPlan struct{ result tc.RunnerResultBinding }

func (*prepareUnknownResultPlan) RequiredLocks() []f.LockRequest    { return nil }
func (p *prepareUnknownResultPlan) Binding() tc.RunnerResultBinding { return p.result }

type prepareUnknownResultControl struct {
	plan        tc.RunnerResultPlan
	err         error
	discoveries int
}

func (p *prepareUnknownResultControl) DiscoverRunnerResult(context.Context, tc.ToolCallBinding) (tc.RunnerResultPlan, error) {
	p.discoveries++
	return p.plan, p.err
}
func (*prepareUnknownResultControl) WithRunnerResultInTx(context.Context, f.Tx, tc.RunnerResultPlan, func(context.Context, tc.RunnerResultBinding, mc.ToolResultPart) error) error {
	panic("rejected metadata must not enter result consumption")
}

func TestMultiRoundPrepareUnknownStillRequiresJoinAndResult(t *testing.T) {
	binding, out, result := prepareUnknownCandidateControl(t)
	for _, test := range []struct {
		name                 string
		joinErr, discoverErr error
		plan                 tc.RunnerResultPlan
		want                 f.Code
		discoveries          int
	}{
		{name: "unjoined owner", joinErr: f.NewFault(f.CommitUnknown, f.Unknown), want: f.CommitUnknown},
		{name: "uncertain discovery", discoverErr: f.NewFault(f.CommitUnknown, f.Unknown), want: f.CommitUnknown, discoveries: 1},
		{name: "missing plan", want: f.Forbidden, discoveries: 1},
		{name: "invalid terminal", plan: &prepareUnknownResultPlan{}, want: f.Forbidden, discoveries: 1},
		{name: "different operation", plan: func() tc.RunnerResultPlan {
			changed := result
			changed.OperationID = newTestID[tc.Operation](t)
			return &prepareUnknownResultPlan{changed}
		}(), want: f.Forbidden, discoveries: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := runtimeCandidate(t, "unused")
			run.terminal, run.ctx = nil, context.Background()
			owner := &loopJoinControl{joinErr: test.joinErr}
			reader := &prepareUnknownResultControl{plan: test.plan, err: test.discoverErr}
			state := &directTextState{multi: &multiRoundState{dispatch: owner, results: reader}, calls: map[i.ExecutionID]*directTextCall{run.request.ExecutionID: run}}
			run.owner = state
			// Store/Model are deliberately unbound: no rejection may reach them.
			_, err := state.continueLoopTool(context.Background(), run, binding, out)
			requireCode(t, err, test.want)
			if owner.joined != 1 || owner.cancelled != 0 || reader.discoveries != test.discoveries || run.unresolved == nil || run.next != nil || run.terminal != nil {
				t.Fatal("Unknown receipt bypassed original owner/result authority")
			}
		})
	}
}

func TestMultiRoundPrepareUnknownResultBindsOriginalObservation(t *testing.T) {
	binding, out, original := prepareUnknownCandidateControl(t)
	if !loopResultMatchesObservation(binding, out, original) {
		t.Fatal("exact canonical prepare result rejected")
	}
	for name, mutate := range map[string]func(*tc.RunnerResultBinding){
		"call":             func(r *tc.RunnerResultBinding) { r.Call.InvocationID = newTestID[mc.Invocation](t) },
		"operation":        func(r *tc.RunnerResultBinding) { r.OperationID = newTestID[tc.Operation](t) },
		"attempt":          func(r *tc.RunnerResultBinding) { r.AttemptID = newTestID[tc.Attempt](t) },
		"outcome":          func(r *tc.RunnerResultBinding) { r.Outcome = "failure" },
		"code":             func(r *tc.RunnerResultBinding) { r.Code = string(f.InternalError) },
		"terminal digest":  func(r *tc.RunnerResultBinding) { r.TerminalDigest = "" },
		"result reference": func(r *tc.RunnerResultBinding) { r.ResultBytes = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			changed := original
			mutate(&changed)
			if loopResultMatchesObservation(binding, out, changed) {
				t.Fatal("unvalidated or different canonical result matched observation")
			}
		})
	}
}
