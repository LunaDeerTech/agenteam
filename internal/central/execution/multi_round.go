package execution

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	gc "github.com/LunaDeerTech/agenteam/internal/central/governance/approval/contract"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	pvc "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

// BindTools resolves the construction cycle exactly once: the actual Tool
// runtime receives this driver's private same-Store authority. Its owners must
// be registered by the composition root before the callback returns.
type MultiRoundDependencies struct {
	Models             mc.JSONCaller
	Tools              ToolCallMetadata
	Guard              *object.ProcessGuard
	CurrentEnvironment pvc.CurrentProcessEnvironment
	BindTools          func(*ToolCallDriver) (tc.RunnerDispatcher, tc.RunnerResultAuthority, error)
	BindApproval       func(*ToolCallDriver) (gc.Gate, error)
}
type multiRoundState struct {
	authority *ToolCallDriver
	dispatch  tc.RunnerDispatcher
	results   tc.RunnerResultAuthority
	guard     *object.ProcessGuard
	approvals gc.Gate
	rejected  gc.RejectedResults
}
type MultiRoundDriver struct{ driver *DirectTextDriver }

func NewMultiRoundDriver(store Store, authority *Authority, base DirectTextDependencies, deps MultiRoundDependencies) (*MultiRoundDriver, error) {
	if nilPort(deps.Models) || nilPort(deps.Tools) || deps.Guard == nil || deps.BindTools == nil || base.Loop == nil {
		return nil, fault(f.DependencyUnbound)
	}
	d, err := newDirectTextDriver(store, authority, base, &toolCallDependencies{metadata: deps.Tools, currentEnvironment: deps.CurrentEnvironment})
	if err != nil {
		return nil, err
	}
	current, err := deps.Guard.CurrentProcess()
	if err != nil || current.String() != d.state.process.String() {
		return nil, fault(f.DependencyUnbound)
	}
	toolAuthority := &ToolCallDriver{driver: d}
	d.state.multi = &multiRoundState{authority: toolAuthority, guard: deps.Guard}
	dispatch, results, err := deps.BindTools(toolAuthority)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(dispatch) || nilPort(results) {
		return nil, fault(f.DependencyUnbound)
	}
	d.state.multi.dispatch, d.state.multi.results = dispatch, results
	if deps.BindApproval != nil {
		gate, err := deps.BindApproval(toolAuthority)
		if err != nil {
			return nil, portError(err)
		}
		rejected, ok := gate.(gc.RejectedResults)
		if nilPort(gate) || !ok || nilPort(rejected) {
			return nil, fault(f.DependencyUnbound)
		}
		d.state.multi.approvals, d.state.multi.rejected = gate, rejected
	}
	return &MultiRoundDriver{d}, nil
}
func (d *MultiRoundDriver) Start(ctx context.Context, e i.ExecutionID) (c.DirectTextReceipt, error) {
	if d == nil || d.driver == nil {
		return c.DirectTextReceipt{}, fault(f.DependencyUnbound)
	}
	return d.driver.Start(ctx, e)
}
func (d *MultiRoundDriver) ResolveUnknown(ctx context.Context, e i.ExecutionID) (c.DirectTextReceipt, error) {
	if d == nil || d.driver == nil {
		return c.DirectTextReceipt{}, fault(f.DependencyUnbound)
	}
	return d.driver.ResolveUnknown(ctx, e)
}
func (d *MultiRoundDriver) Stop() {
	if d != nil && d.driver != nil {
		d.driver.Stop()
	}
}
func (d *MultiRoundDriver) Drain(ctx context.Context) error {
	if d == nil || d.driver == nil {
		return fault(f.DependencyUnbound)
	}
	return d.driver.Drain(ctx)
}
func (d *MultiRoundDriver) Joined() bool { return d != nil && d.driver != nil && d.driver.Joined() }

func (s *directTextState) buildLoopRequest(ctx context.Context, run *directTextCall, built c.ExecutionContext, actor i.Actor, round c.RoundID, call mc.CallID) (agentloop.DirectTextRequest, error) {
	v := run.input.input.Fields()
	definitions := make([]tc.Definition, len(v.Tools))
	request := toolFactsRequest(run.input.input, v.Agent)
	cause, _ := f.NewRecoveryCause("execution.loop.definitions", run.request.ExecutionID.String(), round.String())
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, request.RequiredLocks()); err != nil {
			return portError(err)
		}
		if err := s.multi.guard.RequireCurrentProcessInTx(ctx, tx, s.multiProcess()); err != nil {
			return portError(err)
		}
		if err := s.tools.metadata.RequireExecutionToolFactsInTx(ctx, tx, request); err != nil {
			return portError(err)
		}
		for n, tool := range v.Tools {
			var err error
			definitions[n], err = s.tools.metadata.ReadDefinitionInTx(ctx, tx, tc.SpecRef{ToolID: tool.ToolID, SpecRevision: tool.SpecRevision})
			if err != nil {
				return portError(err)
			}
		}
		return ctx.Err()
	})
	if err := commitError(result); err != nil {
		return agentloop.DirectTextRequest{}, err
	}
	return agentloop.BuildLoopRequest(ctx, built, actor, round.String(), call, definitions)
}
