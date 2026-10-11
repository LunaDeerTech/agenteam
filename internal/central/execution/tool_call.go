package execution

import (
	"context"
	"reflect"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/agentloop"
	c "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type ToolCallMetadata interface {
	tc.ExecutionToolFacts
	ReadDefinitionInTx(context.Context, f.Tx, tc.SpecRef) (tc.Definition, error)
}
type ToolCallDependencies struct {
	Base  DirectTextDependencies
	Loop  *agentloop.ToolCallController
	Tools ToolCallMetadata
}
type toolCallDependencies struct {
	loop     *agentloop.ToolCallController
	metadata ToolCallMetadata
}

// This explicitly constructed first-round driver persists a nonterminal
// tool-pending checkpoint. It never creates an Operation, invokes a Tool,
// sends Runner traffic or replaces the default no-tools driver.
type ToolCallDriver struct{ driver *DirectTextDriver }

func NewToolCallDriver(store Store, a *Authority, deps ToolCallDependencies) (*ToolCallDriver, error) {
	if deps.Loop == nil || nilPort(deps.Tools) || deps.Base.Loop != nil {
		return nil, fault(f.DependencyUnbound)
	}
	d, err := newDirectTextDriver(store, a, deps.Base, &toolCallDependencies{deps.Loop, deps.Tools})
	if err != nil {
		return nil, err
	}
	return &ToolCallDriver{d}, nil
}
func (d *ToolCallDriver) Start(ctx context.Context, e i.ExecutionID) (c.DirectTextReceipt, error) {
	if d == nil || d.driver == nil {
		return c.DirectTextReceipt{}, fault(f.DependencyUnbound)
	}
	return d.driver.Start(ctx, e)
}
func (d *ToolCallDriver) ResolveUnknown(ctx context.Context, e i.ExecutionID) (c.DirectTextReceipt, error) {
	if d == nil || d.driver == nil {
		return c.DirectTextReceipt{}, fault(f.DependencyUnbound)
	}
	return d.driver.ResolveUnknown(ctx, e)
}
func (d *ToolCallDriver) Stop() {
	if d != nil && d.driver != nil {
		d.driver.Stop()
	}
}
func (d *ToolCallDriver) Drain(ctx context.Context) error {
	if d == nil || d.driver == nil {
		return fault(f.DependencyUnbound)
	}
	return d.driver.Drain(ctx)
}
func (d *ToolCallDriver) Joined() bool { return d != nil && d.driver != nil && d.driver.Joined() }

func toolFactsRequest(input c.PreparationInput, agent ac.AgentConfig) tc.ExecutionToolFactsRequest {
	v := input.Fields()
	q := v.Request
	return tc.ExecutionToolFactsRequest{Capture: tc.ExecutionToolCaptureRequest{ProjectID: q.Launch.ProjectID, AgentID: q.Launch.AgentID, ExecutionID: q.ExecutionID}, AgentVersion: agent.Fields().Core.Version, CaptureAgentVersion: v.Agent.Fields().Core.Version, AttemptBinding: v.AttemptBinding, Selection: tc.ToolSnapshotSelection{AllowedToolIDs: v.Agent.Fields().AllowedToolIDs, DeniedToolIDs: q.Launch.Policy.DeniedToolIDs, ScopeConstraints: q.Launch.Policy.AllowedResourceConstraints}, AllowedToolIDs: agent.Fields().AllowedToolIDs, Tools: v.Tools}
}
func (s *directTextState) buildToolCallRequest(ctx context.Context, run *directTextCall, built c.ExecutionContext, actor i.Actor, round c.RoundID, call mc.CallID) (agentloop.DirectTextRequest, error) {
	var definition tc.Definition
	v := run.input.input.Fields()
	if len(v.Tools) != 1 || v.Tools[0].BindingSnapshot.RunnerBinding.Validate() != nil {
		return agentloop.DirectTextRequest{}, fault(f.CapabilityUnsupported)
	}
	request := toolFactsRequest(run.input.input, v.Agent)
	cause, _ := f.NewRecoveryCause("execution.tool-call.definition", run.request.ExecutionID.String(), round.String())
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, request.RequiredLocks()); err != nil {
			return portError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		row, err := loadExecution(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		input, err := loadPreparationInput(ctx, x, run.request.ExecutionID)
		if err != nil {
			return err
		}
		if row == nil || row.summary.Status != c.Preparing || row.summary.CancelRequestedAt != nil || !samePreparationInput(input, run.input) {
			return fault(f.ConfirmationStale)
		}
		if _, err = s.deps.Projects.RequirePreparingProjectInTx(ctx, tx, run.request.Launch.ProjectID); err != nil {
			return portError(err)
		}
		if err = s.tools.metadata.RequireExecutionToolFactsInTx(ctx, tx, request); err != nil {
			return portError(err)
		}
		definition, err = s.tools.metadata.ReadDefinitionInTx(ctx, tx, tc.SpecRef{ToolID: v.Tools[0].ToolID, SpecRevision: v.Tools[0].SpecRevision})
		return portError(err)
	})
	if err := commitError(result); err != nil {
		return agentloop.DirectTextRequest{}, err
	}
	return agentloop.BuildToolCallRequest(ctx, built, actor, round.String(), call, definition)
}
func (s *directTextState) requireCurrentToolFacts(ctx context.Context, tx f.Tx, run *directTextCall) error {
	actor, _ := i.NewAgentRun(run.request.Launch.ProjectID, run.request.Launch.AgentID, run.request.ExecutionID)
	agent, err := s.deps.Agents.ReadExecutionConfigurationInTx(ctx, tx, ac.ExecutionConfigurationRequest{Actor: actor, ProjectID: run.request.Launch.ProjectID, AgentID: run.request.Launch.AgentID, ExecutionID: run.request.ExecutionID, Stage: ac.ExecutionConfigurationCurrent})
	if err != nil {
		return portError(err)
	}
	if _, err = s.deps.Projects.RequirePreparingProjectInTx(ctx, tx, run.request.Launch.ProjectID); err != nil {
		return portError(err)
	}
	if run.toolFacts != nil {
		args, err := tc.DecodeReadFileToolArguments(ctx, run.toolFacts.original)
		if err != nil {
			return err
		}
		if !slices.Contains(agent.Fields().AllowedMountIDs, args.MountID) {
			return fault(f.Forbidden)
		}
	}
	fields := run.input.input.Fields()
	if len(fields.Tools) != 1 || !slices.Contains(agent.Fields().AllowedToolIDs, fields.Tools[0].ToolID) || slices.Contains(fields.Request.Launch.Policy.DeniedToolIDs, fields.Tools[0].ToolID) {
		return fault(f.Forbidden)
	}
	request := toolFactsRequest(run.input.input, agent)
	if err = s.tools.metadata.RequireExecutionToolFactsInTx(ctx, tx, request); err != nil {
		return portError(err)
	}
	v := run.input.input.Fields()
	if len(v.Tools) != 1 {
		return fault(f.InvalidState)
	}
	definition, err := s.tools.metadata.ReadDefinitionInTx(ctx, tx, tc.SpecRef{ToolID: v.Tools[0].ToolID, SpecRevision: v.Tools[0].SpecRevision})
	if err != nil {
		return portError(err)
	}
	tool, ok := run.round.(toolExecutionRound)
	expected := mc.Tool{Name: v.Tools[0].ModelVisibleName, Description: definition.Description, InputSchema: definition.InputSchema, OutputSchema: definition.OutputSchema}
	if !ok || !reflect.DeepEqual(expected, tool.tool()) {
		return fault(f.ConfirmationStale)
	}
	return ctx.Err()
}
