package work

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"

	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

// TaskLaunchProvider validates a real Scheduler todo claim. Capture remains a
// separate provider: this reader neither freezes preparation input nor writes
// an Execution. Execution owns the enclosing Launch call and its actual tail.
type TaskLaunchProvider struct {
	store    Store
	projects schedulerClaimProject
	intents  c.TaskLaunchAuthority
}

func NewTaskLaunchProvider(store Store, authority *Authority, intents c.TaskLaunchAuthority) (*TaskLaunchProvider, error) {
	if nilPort(store) || authority.state() == nil || !sameStore(store, authority.state().store) || nilPort(intents) {
		return nil, fault(f.DependencyUnbound)
	}
	projects, ok := authority.state().projects.(schedulerClaimProject)
	if !ok || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	return &TaskLaunchProvider{store, projects, intents}, nil
}

type taskLaunchSource struct {
	Intent      c.TaskLaunchIntent
	ClaimDigest f.Digest
	Task        c.Task
	Sprint      c.Sprint
	Milestone   c.Milestone
}
type taskLaunchPlan struct {
	owner     *TaskLaunchProvider
	actor     i.Actor
	request   ec.LaunchRequest
	locks     []f.LockRequest
	source    taskLaunchSource
	reference f.Digest
}

func (p *taskLaunchPlan) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*taskLaunchPlan) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "work_task_launch_plan") }
func (*taskLaunchPlan) LogValue() slog.Value       { return slog.StringValue("work_task_launch_plan") }

func taskLaunchInput(ctx context.Context, actor i.Actor, r ec.LaunchRequest) error {
	if ctx == nil || r.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	d := actor.Details()
	if d.Kind != i.Service || d.ServiceName != i.Scheduler || d.ProjectID != r.ProjectID.String() || d.CauseRef != r.Lineage.DispatchID {
		return fault(f.Forbidden)
	}
	if r.Trigger.Kind != "task" || r.Purpose != "task/work" || r.Lineage.DispatchID == "" {
		return fault(f.CapabilityUnsupported)
	}
	if r.Meta.IdempotencyKey != f.IdempotencyKey("scheduler_dispatch:"+r.Lineage.DispatchID) {
		return fault(f.Forbidden)
	}
	// Denied tools are preserved verbatim in the request/digest. No resource
	// constraint or alternate lineage is silently interpreted as an empty one.
	if len(r.Policy.AllowedResourceConstraints) != 0 || r.Lineage.RetryOf != nil || r.Lineage.RegenerateOf != nil || r.Lineage.ContributionGeneration != nil || r.Lineage.ContributionAttempt != nil {
		return fault(f.DependencyUnbound)
	}
	return nil
}
func taskLaunchLocks(r ec.LaunchRequest) ([]f.LockRequest, error) {
	command, err := r.Command()
	if err != nil {
		return nil, err
	}
	agent, _ := f.AgentLock(r.AgentID.String())
	// Project SH protects Sprint/Milestone placement against structure writers;
	// Schedule EX and Task SH cover Task, blockers and immutable claim reads.
	// Every key is known before protected discovery; final validation adds none.
	return taskNormalize([]f.LockRequest{commandLock(command), projectLock(r.ProjectID, f.Shared), taskScheduleLock(r.ProjectID, f.Exclusive), {Key: agent, Mode: f.Shared}, taskLock(r.Trigger.TaskID, f.Shared)})
}
func sameTaskLaunchRequest(a, b ec.LaunchRequest) bool {
	return a.Meta.RequestID == b.Meta.RequestID && a.Meta.IdempotencyKey == b.Meta.IdempotencyKey && a.Meta.ExpectedVersion == nil && b.Meta.ExpectedVersion == nil && sameValue(a, b)
}

func (p *TaskLaunchProvider) DiscoverLaunch(ctx context.Context, actor i.Actor, request ec.LaunchRequest) (ec.LaunchPlan, error) {
	if err := taskLaunchInput(ctx, actor, request); err != nil {
		return nil, err
	}
	if p == nil || nilPort(p.store) || nilPort(p.intents) || nilPort(p.projects) {
		return nil, fault(f.DependencyUnbound)
	}
	r := request.Clone()
	locks, err := taskLaunchLocks(r)
	if err != nil {
		return nil, taskTriggerError(err)
	}
	cause, err := readCause("task-launch")
	if err != nil {
		return nil, taskTriggerError(err)
	}
	var source taskLaunchSource
	result := p.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := p.store.AcquireAll(ctx, tx, locks); err != nil {
			return taskTriggerError(err)
		}
		var err error
		source, err = p.currentSource(ctx, tx, actor, r, locks)
		return err
	})
	if err = taskTxError(ctx, result); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := canonical(source)
	if err != nil {
		return nil, err
	}
	return &taskLaunchPlan{p, actor, r, locks, source, digest(raw)}, nil
}

func (p *TaskLaunchProvider) ValidateLaunchInTx(ctx context.Context, tx f.Tx, actor i.Actor, request ec.LaunchRequest, plan ec.LaunchPlan) (ec.LaunchPermit, error) {
	if err := taskLaunchInput(ctx, actor, request); err != nil {
		return ec.LaunchPermit{}, err
	}
	selected, ok := plan.(*taskLaunchPlan)
	if p == nil || !ok || selected == nil || selected.owner != p || !selected.actor.Equal(actor) || !sameTaskLaunchRequest(selected.request, request) {
		return ec.LaunchPermit{}, fault(f.Forbidden)
	}
	source, err := p.currentSource(ctx, tx, actor, selected.request.Clone(), selected.locks)
	if err != nil {
		return ec.LaunchPermit{}, err
	}
	raw, err := canonical(source)
	if err != nil {
		return ec.LaunchPermit{}, err
	}
	if digest(raw) != selected.reference || !sameValue(source, selected.source) {
		return ec.LaunchPermit{}, fault(f.ConfirmationStale)
	}
	if err = ctx.Err(); err != nil {
		return ec.LaunchPermit{}, err
	}
	requestDigest, _ := request.Digest()
	return ec.LaunchPermit{ProviderType: "task", ProjectID: request.ProjectID, AgentID: request.AgentID, RequestDigest: requestDigest, ReferenceDigest: selected.reference}, nil
}

func (p *TaskLaunchProvider) currentSource(ctx context.Context, tx f.Tx, actor i.Actor, r ec.LaunchRequest, locks []f.LockRequest) (taskLaunchSource, error) {
	var zero taskLaunchSource
	x, err := p.store.InTx(tx)
	if err != nil {
		return zero, taskTriggerError(err)
	}
	if err = p.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return zero, taskTriggerError(err)
	}
	intent, err := p.intents.RequireTaskLaunchInTx(ctx, tx, actor, r.Clone())
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if err != nil {
		return zero, taskTriggerError(err)
	}
	if intent.Validate() != nil || intent.ProjectID != r.ProjectID || intent.TaskID.String() != r.Trigger.TaskID || intent.AgentID != r.AgentID || intent.DispatchID != r.Lineage.DispatchID {
		return zero, fault(f.Forbidden)
	}
	project, err := p.projects.RequireSchedulerProjectInTx(ctx, tx, r.ProjectID)
	if ctx.Err() != nil {
		return zero, ctx.Err()
	}
	if err != nil {
		return zero, taskTriggerError(err)
	}
	if project.Project.Validate() != nil || project.Project.ID != r.ProjectID || project.Config.Validate() != nil {
		return zero, internal(nil)
	}
	if project.Project.Lifecycle != pc.Active || !project.Config.Enabled || project.Project.CurrentSprintID == nil || *project.Project.CurrentSprintID != intent.SprintID {
		return zero, fault(f.InvalidState)
	}
	claim, err := loadSchedulerClaim(ctx, x, r.ProjectID, intent.DispatchID)
	if err != nil {
		return zero, err
	}
	if claim == nil || claim.Request.ProjectID != r.ProjectID || claim.Request.TaskID != intent.TaskID || claim.Request.AgentID != r.AgentID || claim.Request.CurrentSprintID != intent.SprintID || claim.Request.Purpose != r.Purpose || claim.Request.RequestID != r.Meta.RequestID || claim.After.Version != intent.ClaimedVersion {
		return zero, fault(f.Forbidden)
	}
	task, err := loadTask(ctx, x, r.ProjectID, intent.TaskID)
	if err != nil {
		return zero, err
	}
	if task.ProjectID != r.ProjectID || task.ID != intent.TaskID {
		return zero, internal(nil)
	}
	// An eligible concurrent title/description change before discovery is legal.
	// It is the newly observed version, not the historical claim postimage, that
	// must remain unchanged through this plan's final validation.
	if task.Version < intent.ClaimedVersion || task.State != c.TaskStateInProgress || task.SprintID != intent.SprintID || task.AssigneeAgentID == nil || *task.AssigneeAgentID != r.AgentID {
		return zero, fault(f.InvalidState)
	}
	sprint, err := loadSprint(ctx, x, r.ProjectID, task.SprintID, project.Project.CurrentSprintID)
	if err != nil {
		return zero, err
	}
	if sprint.ProjectID != r.ProjectID || sprint.ID != intent.SprintID || sprint.State != c.Current || sprint.MilestoneID != task.MilestoneID {
		return zero, fault(f.TaskSprintInvalid)
	}
	milestone, err := loadMilestone(ctx, x, r.ProjectID, task.MilestoneID)
	if err != nil {
		return zero, err
	}
	if milestone.ProjectID != r.ProjectID || milestone.ID != task.MilestoneID {
		return zero, internal(nil)
	}
	var unresolved int64
	if err = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_work.task_blockers WHERE project_id=$1 AND task_id=$2 AND resolved_at IS NULL`, r.ProjectID.String(), task.ID.String()).Scan(&unresolved); err != nil {
		return zero, taskSQL(err)
	}
	if unresolved < 0 {
		return zero, internal(nil)
	}
	if unresolved != 0 {
		return zero, field(f.InvalidState, "/task_id", "UNRESOLVED_BLOCKERS")
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	claimBytes, err := canonical(claim)
	if err != nil {
		return zero, err
	}
	return taskLaunchSource{intent, digest(claimBytes), task.Clone(), sprint.Clone(), milestone}, nil
}

var _ ec.TriggerProvider = (*TaskLaunchProvider)(nil)
