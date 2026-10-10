//go:build integration

package projectvariable_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	objc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	"github.com/LunaDeerTech/agenteam/internal/central/work"
)

// These providers succeed inside the real preparation transaction. The
// unimplemented complete Snapshot prevents that transaction from committing;
// neither their tentative values nor an attempt's return is a sealed capture.
func TestExecutionCaptureProviders(t *testing.T) {
	t.Run("real-providers-roll-back-with-unbound-snapshot", func(t *testing.T) {
		x := newExecutionCaptureFixture(t)
		v := x.launch.task
		skills, err := skill.NewExecutionBindings(v.agent.providers.SkillAuthority, x.authority)
		if err != nil {
			t.Fatal("real Skill capture provider", err)
		}
		tools, err := registry.NewExecutionCapture(v.agent.providers.Registry, x.authority)
		if err != nil {
			t.Fatal("real Tool capture provider", err)
		}
		skillRequest := sc.SkillCaptureRequest{ProjectID: x.created.ProjectID, AgentID: x.created.AgentID, ExecutionID: x.created.ID}
		toolRequest := tc.ExecutionToolCaptureRequest{ProjectID: x.created.ProjectID, AgentID: x.created.AgentID, ExecutionID: x.created.ID}
		outside := func() {
			t.Helper()
			plan, err := skills.DiscoverInitialBindings(ctxFor(t), skillRequest)
			if err == nil || plan != nil {
				t.Fatal("Skill capture accepted identity without the original preparing call")
			}
			requireCode(t, err, f.Forbidden)
			toolPlan, err := tools.DiscoverExecutionTools(ctxFor(t), toolRequest)
			if err == nil || toolPlan != nil {
				t.Fatal("Tool capture accepted identity without the original preparing call")
			}
			requireCode(t, err, f.Forbidden)
			counts, err := captureReferenceCounts(ctxFor(t), v.base.raw, x.created.ID.String())
			if err != nil || counts != ([4]int64{}) {
				t.Fatal("provider references survived without a complete Snapshot transaction", err)
			}
		}
		outside()
		skillCalls := &observedSkillCapture{provider: skills, observe: x.observeSkill}
		toolCalls := &observedToolCapture{provider: tools, observe: x.observeTool}
		driver, err := execution.NewPreparationDriver(v.base.tracked, x.authority, execution.PreparationDependencies{
			Agents: x.configuration, Projects: v.base.projectAuthority, Processes: x.processes,
			Task: x.task, Skills: skillCalls, Tools: toolCalls,
		})
		if err != nil {
			t.Fatal("real preparation assembly", err)
		}
		t.Cleanup(func() {
			driver.Stop()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := driver.Drain(ctx); err != nil || !driver.Joined() {
				t.Error("original preparation did not join before Object guard cleanup", err)
			}
		})
		var physical f.CommitResult
		var returned bool
		var attempt string
		store := v.base.tracked
		store.mu.Lock()
		store.afterResult = func(cause f.TransactionCause, result f.CommitResult) {
			d := cause.Details()
			if !returned && skillCalls.resolve == 1 && toolCalls.resolve == 1 && d.Kind == f.JobCause && d.JobType == "execution-preparation" && d.JobID == x.created.ID.String() {
				physical, returned = result, true
				attempt = d.JobAttemptID
			}
		}
		store.mu.Unlock()
		clear := func() { store.mu.Lock(); store.afterResult = nil; store.mu.Unlock() }
		defer clear()
		before := v.databaseSnapshot(t)
		err = driver.Run(ctxFor(t), x.created.ID)
		clear()
		requireCode(t, err, f.DependencyUnbound)
		if !returned || physical.State() != f.NotCommitted {
			t.Fatal("missing complete Snapshot did not cause the original physical rollback")
		}
		requireCode(t, physical.Fault(), f.DependencyUnbound)
		if skillCalls.readErr != nil || toolCalls.readErr != nil {
			t.Fatal("same-transaction provider reference observation", errors.Join(skillCalls.readErr, toolCalls.readErr))
		}
		if skillCalls.discover != 1 || toolCalls.discover != 1 || skillCalls.resolve != 1 || toolCalls.resolve != 1 || skillCalls.tx != toolCalls.tx || skillCalls.result.Validate() != nil || len(skillCalls.result.Bindings) != 1 || len(toolCalls.result) != 1 || toolCalls.result[0].Validate() != nil {
			t.Fatal("the original two providers did not complete in one live preparation transaction")
		}
		if toolCalls.result[0].ToolID != v.agent.tool.ToolID || toolCalls.result[0].SpecRevision != v.agent.tool.SpecRevision || before != v.databaseSnapshot(t) {
			t.Fatal("captured Tool differs from the actual Agent selection or changed Work facts")
		}
		x.requireReturnedPreparation(t, attempt)
		outside()
	})
}

func captureReferenceCounts(ctx context.Context, sql postgres.SQLExecutor, execution string) ([4]int64, error) {
	var out [4]int64
	err := sql.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agenteam_skill.execution_binding_heads WHERE execution_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_skill.execution_bindings WHERE execution_id=$1::text::uuid),
 (SELECT count(*) FROM agenteam_tool.execution_configurations WHERE execution_id=$1::text),
 (SELECT count(*) FROM agenteam_tool.execution_references WHERE execution_id=$1::text)`, execution).Scan(&out[0], &out[1], &out[2], &out[3])
	return out, err
}

func (x *executionCaptureFixture) observeSkill(ctx context.Context, tx f.Tx, result sc.InitialSkillBindings) error {
	if result.Validate() != nil || len(result.Bindings) != 1 || result.Request.ProjectID != x.created.ProjectID || result.Request.AgentID != x.created.AgentID || result.Request.ExecutionID != x.created.ID {
		return errors.New("Skill result is not the original nonempty capture")
	}
	sql, err := x.launch.task.base.tracked.InTx(tx)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var matching int64
	err = sql.QueryRow(ctx, `SELECT count(*)
 FROM agenteam_skill.execution_binding_heads h
 JOIN agenteam_skill.execution_bindings b ON (b.execution_id,b.project_id,b.agent_id)=(h.execution_id,h.project_id,h.agent_id)
 JOIN agenteam_skill.agent_assignment_heads ah ON (ah.project_id,ah.agent_id)=(h.project_id,h.agent_id)
 JOIN agenteam_skill.agent_assignments a ON (a.project_id,a.agent_id,a.id,a.skill_id,a.assignment_sequence)=(b.project_id,b.agent_id,b.assignment_id,b.skill_id,b.assignment_sequence)
 JOIN agenteam_skill.revisions r ON (r.project_id,r.skill_id,r.id,r.revision,r.object_id)=(b.project_id,b.skill_id,b.revision_id,b.revision,b.object_id)
 JOIN agenteam_skill.skills s ON (s.project_id,s.id,s.revision_id,s.current_revision)=(r.project_id,r.skill_id,r.id,r.revision)
 JOIN agenteam_skill.initializations i ON (i.project_id,i.skill_id,i.revision_id,i.object_id)=(r.project_id,r.skill_id,r.id,r.object_id)
 WHERE h.execution_id=$1::text::uuid AND h.project_id=$2::text::uuid AND h.agent_id=$3::text::uuid
 AND h.binding_count=1 AND h.assignment_sequence=ah.assignment_sequence AND ah.add_skills_enabled
 AND ah.initial_assignment_id=b.assignment_id AND a.enabled AND a.removed_at IS NULL AND s.protected AND s.serving AND i.phase='published'
 AND h.record#>'{source,result}'=$4::jsonb
 AND h.record->>'attempt_binding'=h.attempt_binding AND h.record->>'source_digest'=h.source_digest
 AND h.record#>>'{source,object_id}'=r.object_id::text
 AND (h.record#>>'{source,object_version}')::bigint=r.object_version
 AND (h.record#>>'{source,object_created_at}')::timestamptz=r.object_created_at
 AND h.record#>>'{source,package_digest}'=i.package_sha256 AND h.record#>>'{source,manifest_digest}'=i.manifest_sha256
 AND h.record#>>'{source,result,bindings,0,skill_id}'=b.skill_id::text
 AND h.record#>>'{source,result,bindings,0,revision_id}'=b.revision_id::text
 AND (h.record#>>'{source,result,bindings,0,revision}')::bigint=b.revision
 AND h.record#>>'{source,result,bindings,0,assignment_id}'=b.assignment_id::text
 AND (h.record#>>'{source,result,bindings,0,assignment_sequence}')::bigint=b.assignment_sequence
 AND h.record#>>'{source,result,bindings,0,name}'=i.name
 AND h.record#>>'{source,result,bindings,0,description}'=i.description
 AND h.record#>>'{source,result,bindings,0,package_sha256}'=i.package_sha256`, x.created.ID.String(), x.created.ProjectID.String(), x.created.AgentID.String(), raw).Scan(&matching)
	if err != nil {
		return err
	}
	counts, err := captureReferenceCounts(ctx, sql, x.created.ID.String())
	if err != nil {
		return err
	}
	if matching != 1 || counts != ([4]int64{1, 1, 0, 0}) {
		return errors.New("Skill fixed revision or original assignment references were not written in the caller transaction")
	}
	return nil
}

func (x *executionCaptureFixture) observeTool(ctx context.Context, tx f.Tx, result []tc.ExecutionTool) error {
	if len(result) != 1 || result[0].Validate() != nil {
		return errors.New("Tool result is not the original nonempty capture")
	}
	tool := result[0]
	binding := tool.BindingSnapshot
	if tool.ToolID != x.launch.task.agent.tool.ToolID || tool.SpecRevision != x.launch.task.agent.tool.SpecRevision || binding.Binding.HandlerID != builtin.SkillInstallHandlerID || binding.Binding.ContractRevision != 1 || binding.ScopeResolverID != builtin.SkillInstallScopeID || binding.RiskClassifierID != builtin.SkillInstallRiskID || binding.Class != tc.OrdinaryTool {
		return errors.New("Tool capture did not freeze the actual install builtin")
	}
	sql, err := x.launch.task.base.tracked.InTx(tx)
	if err != nil {
		return err
	}
	var matching int64
	err = sql.QueryRow(ctx, `SELECT count(*)
 FROM agenteam_tool.execution_configurations h
 JOIN agenteam_tool.execution_references r ON r.execution_id=h.execution_id
 JOIN agenteam_tool.agent_configurations a ON (a.project_id,a.agent_id,a.config_version)=(h.project_id,h.agent_id,h.agent_version)
 JOIN agenteam_tool.agent_references ar ON (ar.agent_id,ar.tool_id)=(a.agent_id,r.tool_id)
 JOIN agenteam_tool.registrations g ON (g.tool_id,g.spec_revision,g.handler_id,g.contract_revision,g.scope_resolver_id,g.risk_classifier_id,g.class)=(r.tool_id,r.spec_revision,r.handler_id,r.contract_revision,r.scope_resolver_id,r.risk_classifier_id,r.class)
 JOIN agenteam_tool.spec_revisions s ON (s.tool_id,s.spec_revision)=(r.tool_id,r.spec_revision)
 JOIN agenteam_tool.identities i ON i.tool_id=r.tool_id
 JOIN agenteam_skill.execution_binding_heads sh ON sh.execution_id::text=h.execution_id AND sh.attempt_binding=h.attempt_binding
 WHERE h.execution_id=$1 AND h.project_id=$2 AND h.agent_id=$3 AND h.tool_count=1 AND h.agent_version=1
 AND r.tool_id=$4 AND r.spec_revision=$5 AND r.model_visible_name=$6
 AND r.handler_id=$7 AND r.contract_revision=$8 AND r.scope_resolver_id=$9 AND r.risk_classifier_id=$10 AND r.class=$11
 AND i.stable_key=$12`, x.created.ID.String(), x.created.ProjectID.String(), x.created.AgentID.String(), tool.ToolID.String(), int64(tool.SpecRevision), tool.ModelVisibleName,
		binding.Binding.HandlerID, int64(binding.Binding.ContractRevision), string(binding.ScopeResolverID), string(binding.RiskClassifierID), string(binding.Class), builtin.SkillInstallStableKey).Scan(&matching)
	if err != nil {
		return err
	}
	counts, err := captureReferenceCounts(ctx, sql, x.created.ID.String())
	if err != nil {
		return err
	}
	if matching != 1 || counts != ([4]int64{1, 1, 1, 1}) {
		return errors.New("Tool metadata and both providers' references were not present in the original caller transaction")
	}
	return nil
}

// Each process query delegates to the already bound, still-held Object guard.
// Typed-ID conversion does not infer that an old process has stopped. The
// enclosing P2 fixture retains its original guard until all borrowers drain.
type captureProviderProcesses struct{ guard *object.ProcessGuard }

func (p captureProviderProcesses) CurrentProcess() oc.ProcessID {
	id, err := p.guard.CurrentProcess()
	if err != nil {
		return oc.ProcessID{}
	}
	value, _ := f.ParseID[oc.Process](id.String())
	return value
}

func (p captureProviderProcesses) ConfirmStopped(ctx context.Context, id oc.ProcessID) error {
	value, err := f.ParseID[objc.Process](id.String())
	if err != nil {
		return err
	}
	return p.guard.ConfirmStopped(ctx, value)
}

type executionCaptureFixture struct {
	launch        *schedulerLaunchFixture
	created       ec.Summary
	authority     *execution.Authority
	configuration *agent.ExecutionConfigurationAuthority
	task          *work.TaskTrigger
	processes     captureProviderProcesses
}

// All persisted source facts come from the original P2/Agent/Work/Scheduler
// services. This setup creates an Execution; it does not manufacture Snapshot,
// preparing ownership, capture permissions, provider references or leases.
func newExecutionCaptureFixture(t *testing.T) *executionCaptureFixture {
	t.Helper()
	x := newSchedulerLaunchFixture(t)
	dispatch, err := x.handoff.LaunchOnce(ctxFor(t), x.request.ProjectID, x.dispatch)
	if err != nil {
		t.Fatal("real source Launch before capture", err)
	}
	x.requireAssociated(t, dispatch)
	launches, _, created := x.launcher.observed()
	if launches != 1 || created.Status != ec.Created || created.SnapshotID != nil || created.StartedAt != nil {
		t.Fatal("capture source is not the original created Execution")
	}
	v := x.task
	access, err := project.NewSchedulerExecutionAccess(v.base.projectAuthority, v.pending)
	if err != nil {
		t.Fatal("capture same-Store Project access", err)
	}
	authority, err := execution.NewAuthority(v.base.tracked, v.base.projectAuthority, access)
	if err != nil {
		t.Fatal("capture actual Execution authority", err)
	}
	configuration, err := agent.NewExecutionConfiguration(v.agent.providers.Agents, authority)
	if err != nil {
		t.Fatal("capture initialized Agent configuration", err)
	}
	task, err := work.NewTaskTrigger(v.base.tracked, v.authority, authority)
	if err != nil {
		t.Fatal("capture real Task source", err)
	}
	processes := captureProviderProcesses{v.agent.guard}
	if processes.CurrentProcess().Validate() != nil {
		t.Fatal("capture requires the original live Object process guard")
	}
	return &executionCaptureFixture{x, created, authority, configuration, task, processes}
}

func (x *executionCaptureFixture) requireReturnedPreparation(t *testing.T, attempt string) {
	t.Helper()
	var status, digest, phase, actualAttempt, process string
	var version, fence, attempts, active int64
	var snapshot *string
	var started, completed, cancelled *time.Time
	err := x.launch.task.base.raw.QueryRow(ctxFor(t), `SELECT e.status,e.version,e.snapshot_id::text,e.started_at,e.completed_at,e.cancel_requested_at,e.request_digest,
 a.phase,a.attempt_id::text,a.process_id::text,a.fence,
 (SELECT count(*) FROM agenteam_execution.preparation_attempts n WHERE n.execution_id=e.id),
 (SELECT count(*) FROM agenteam_execution.executions n WHERE n.agent_id=e.agent_id AND n.status IN('created','preparing','running','waiting'))
 FROM agenteam_execution.executions e
 JOIN agenteam_execution.preparation_claims c ON c.execution_id=e.id
 JOIN agenteam_execution.preparation_attempts a ON (a.execution_id,a.attempt_id,a.process_id,a.fence)=(c.execution_id,c.attempt_id,c.process_id,c.fence)
 WHERE e.id=$1`, x.created.ID.String()).Scan(&status, &version, &snapshot, &started, &completed, &cancelled, &digest, &phase, &actualAttempt, &process, &fence, &attempts, &active)
	if err != nil {
		t.Fatal("original returned preparation facts", err)
	}
	want, err := x.launch.request.Digest()
	if err != nil || digest != string(want) || status != string(ec.Preparing) || version != int64(x.created.Version)+1 || snapshot != nil || started != nil || completed != nil || cancelled != nil || phase != "terminal" || actualAttempt != attempt || process != x.processes.CurrentProcess().String() || fence != 1 || attempts != 1 || active != 1 {
		t.Fatal("attempt return was confused with sealed preparation or Execution termination")
	}
}

// Observation never supplies authority, replaces a plan or converts a failed
// provider result into success. SQL observations run only after the real
// provider returned successfully and still use that original live caller Tx.
type observedSkillCapture struct {
	provider sc.InitialBindingsProvider
	observe  func(context.Context, f.Tx, sc.InitialSkillBindings) error
	result   sc.InitialSkillBindings
	tx       f.Tx
	discover int
	resolve  int
	readErr  error
}

func (o *observedSkillCapture) DiscoverInitialBindings(ctx context.Context, request sc.SkillCaptureRequest) (sc.InitialBindingsPlan, error) {
	plan, err := o.provider.DiscoverInitialBindings(ctx, request)
	if err == nil {
		o.discover++
	}
	return plan, err
}

func (o *observedSkillCapture) ResolveInitialBindingsInTx(ctx context.Context, tx f.Tx, request sc.SkillCaptureRequest, plan sc.InitialBindingsPlan) (sc.InitialSkillBindings, error) {
	result, err := o.provider.ResolveInitialBindingsInTx(ctx, tx, request, plan)
	if err == nil {
		o.resolve++
		o.tx, o.result = tx, result.Clone()
		if o.observe != nil {
			o.readErr = o.observe(ctx, tx, result.Clone())
		}
	}
	return result, err
}

type observedToolCapture struct {
	provider tc.ExecutionTools
	observe  func(context.Context, f.Tx, []tc.ExecutionTool) error
	result   []tc.ExecutionTool
	tx       f.Tx
	discover int
	resolve  int
	readErr  error
}

func (o *observedToolCapture) DiscoverExecutionTools(ctx context.Context, request tc.ExecutionToolCaptureRequest) (tc.ExecutionToolPlan, error) {
	plan, err := o.provider.DiscoverExecutionTools(ctx, request)
	if err == nil {
		o.discover++
	}
	return plan, err
}

func (o *observedToolCapture) ResolveExecutionToolsInTx(ctx context.Context, tx f.Tx, request tc.ExecutionToolCaptureRequest, plan tc.ExecutionToolPlan) ([]tc.ExecutionTool, error) {
	result, err := o.provider.ResolveExecutionToolsInTx(ctx, tx, request, plan)
	if err == nil {
		o.resolve++
		o.tx, o.result = tx, slices.Clone(result)
		if o.observe != nil {
			o.readErr = o.observe(ctx, tx, slices.Clone(result))
		}
	}
	return result, err
}
