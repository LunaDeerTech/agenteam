package work

import (
	"context"
	"encoding/json"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"slices"
	"strings"
)

type transitionCancellation struct {
	Scope     ec.CancellationScope `json:"scope"`
	BlockerID c.TaskBlockerID      `json:"blocker_id"`
	Reason    *string              `json:"reason"`
}

func (v transitionCancellation) validate(in transitionInput) error {
	meta := c.TaskBlockerExecutionCancellationMetadata{ExecutionID: v.Scope.ExecutionID, CancelledBy: in.User, Reason: v.Reason}
	if v.Scope.Validate() != nil || v.Scope.ProjectID != in.Project || v.Scope.TaskID != in.Task.String() || v.BlockerID.Validate() != nil || meta.Validate() != nil || in.Agent.Validate() == nil || in.Request.TargetState != c.TaskStateBlocked || in.Request.AssigneeAgentID != nil || len(in.Request.AddBlockers) != 0 || len(in.Request.ResolveBlockerIDs) != 0 {
		return internal(nil)
	}
	return nil
}
func (v *transitionCancellation) UnmarshalJSON(raw []byte) error {
	if _, err := taskPrivateObject(raw, 16<<10, []string{"scope", "blocker_id", "reason"}, []string{"reason"}); err != nil {
		return err
	}
	type wire transitionCancellation
	var n wire
	if err := json.Unmarshal(raw, &n); err != nil {
		return internal(err)
	}
	*v = transitionCancellation(n)
	return nil
}
func (in transitionInput) edge(before c.Task) error {
	if in.Cancellation == nil {
		return transitionEdge(before, in.Request)
	}
	if in.Cancellation.validate(in) != nil || (before.State != c.TaskStateTodo && before.State != c.TaskStateInProgress && before.State != c.TaskStateInReview) {
		return fault(f.InvalidState)
	}
	return nil
}
func (in transitionInput) transitionSemantic(actor i.Actor, key f.IdempotencyKey) (f.Digest, error) {
	rid, err := f.ParseID[f.Request](in.Task.String())
	if err != nil {
		return "", err
	}
	base, err := c.TaskTransferDigest(actor, f.CommandMeta{RequestID: rid, IdempotencyKey: key, ExpectedVersion: &in.Expected}, in.Project, in.Task, in.Request)
	if err != nil {
		return "", err
	}
	if in.Cancellation == nil {
		return base, nil
	}
	if err = in.Cancellation.validate(in); err != nil {
		return "", err
	}
	raw, err := canonical(struct {
		Base   f.Digest
		Scope  ec.CancellationScope
		Reason *string
	}{base, in.Cancellation.Scope, in.Cancellation.Reason})
	if err != nil {
		return "", err
	}
	return digest(raw), nil
}

type executionCancellationOwner interface {
	ec.CancellationInTx
	RequireTaskProtectionInTx(context.Context, f.Tx, i.Actor, ec.CancellationScope) error
}
type protectionContextKey struct{}
type protectionContext struct {
	owner *taskTransitionServiceState
	actor i.ActorDetails
	scope ec.CancellationScope
}

func (s *TaskTransitionService) protectionContext(ctx context.Context, actor i.Actor, scope ec.CancellationScope) context.Context {
	return context.WithValue(ctx, protectionContextKey{}, protectionContext{s.state(), actor.Details(), scope})
}
func (s *TaskTransitionService) requireTransitionLifetime(ctx context.Context, tx f.Tx, actor i.Actor, project c.ProjectID) error {
	if w, ok := ctx.Value(protectionContextKey{}).(protectionContext); ok {
		owner, ok := s.state().deps.Cancellation.(executionCancellationOwner)
		if !ok || nilPort(owner) || w.owner != s.state() || w.actor != actor.Details() || w.scope.ProjectID != project {
			return fault(f.Forbidden)
		}
		return portError(owner.RequireTaskProtectionInTx(ctx, tx, actor, w.scope))
	}
	if owned := s.state().ownership; owned != nil {
		return owned.requireRunning(ctx, tx, project, projectCallTransition)
	}
	return nil
}

// Binding happens once during composition because the two real owners refer
// to each other's same-transaction proof. It never substitutes a fake owner.
func (s *TaskTransitionService) BindCancellation(owner ec.CancellationInTx) error {
	st := s.state()
	if st == nil || nilPort(owner) {
		return fault(f.DependencyUnbound)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if !nilPort(st.deps.Cancellation) || len(st.calls) != 0 || st.stopped {
		return fault(f.InvalidState)
	}
	if _, ok := owner.(executionCancellationOwner); !ok {
		return fault(f.DependencyUnbound)
	}
	st.deps.Cancellation = owner
	return nil
}
func (s *TaskTransitionService) RequireStore(value any) error {
	st := s.state()
	store, ok := value.(Store)
	if st == nil || !ok || !sameStore(st.store, store) {
		return fault(f.DependencyUnbound)
	}
	return nil
}

type preparedExecutionCancellation struct {
	owner *taskTransitionServiceState
	actor i.ActorDetails
	scope ec.CancellationScope
	plan  *preparedTransition
}

func (p *preparedExecutionCancellation) RequiredLocks() []f.LockRequest {
	if p == nil || p.plan == nil {
		return nil
	}
	return p.plan.RequiredLocks()
}

func (s *TaskTransitionService) PrepareExecutionCancellation(ctx context.Context, actor i.Actor, meta f.CommandMeta, scope ec.CancellationScope, expected f.Version, reason *string) (ec.TaskCancellationPlan, error) {
	ctx, _, done, err := s.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if err = readInput(ctx, actor, scope.ProjectID); err != nil {
		return nil, err
	}
	if scope.Validate() != nil || expected.Validate() != nil || meta.Validate() != nil || meta.ExpectedVersion != nil {
		return nil, fault(f.InvalidArgument)
	}
	if _, ok := s.state().deps.Cancellation.(executionCancellationOwner); !ok {
		return nil, fault(f.DependencyUnbound)
	}
	task, _ := f.ParseID[c.Task](scope.TaskID)
	user, _ := f.ParseID[i.User](actor.Details().UserID)
	blocker, err := f.NewID[c.TaskBlockerIdentity]()
	if err != nil {
		return nil, unavailable(err)
	}
	var comment *string
	if reason != nil {
		text := *reason
		reason = &text
		body := text
		comment = &body
	}
	// Review blocking requires a comment; the cause remains explicit even when
	// the optional Human reason is absent.
	if comment == nil {
		text := "Execution cancelled by user"
		comment = &text
	}
	in := transitionInput{Project: scope.ProjectID, Task: task, User: user, Expected: expected, Request: c.TaskTransfer{TargetState: c.TaskStateBlocked, Comment: comment}, Cancellation: &transitionCancellation{scope, blocker, reason}}
	keyRaw, _ := canonical(struct {
		Scope ec.CancellationScope
		Key   f.IdempotencyKey
	}{scope, meta.IdempotencyKey})
	key := f.IdempotencyKey("execution-cancel/" + strings.TrimPrefix(digest(keyRaw).String(), "sha256:"))
	semantic, err := in.semantic(actor, key)
	if err != nil {
		return nil, err
	}
	q := c.TaskTransitionLookupRequest{ProjectID: scope.ProjectID, Command: c.TaskTransitionTransfer, IdempotencyKey: key, SemanticDigest: semantic}
	ctx = s.protectionContext(ctx, actor, scope)
	out, unknown, err := s.prepareTransition(ctx, actor, in, q)
	if err != nil {
		return nil, err
	}
	if unknown != nil {
		return nil, txError(*unknown)
	}
	if out.CompletedReceipt != nil {
		return nil, fault(f.InvalidState)
	}
	p, ok := out.Prepared.(*preparedTransition)
	if !ok {
		return nil, internal(nil)
	}
	return &preparedExecutionCancellation{s.state(), actor.Details(), scope, p}, nil
}
func (s *TaskTransitionService) ApplyExecutionCancellationInTx(ctx context.Context, tx f.Tx, actor i.Actor, scope ec.CancellationScope, plan ec.TaskCancellationPlan) (ec.TaskCancellationProtection, error) {
	ctx, _, done, err := s.begin(ctx)
	if err != nil {
		return ec.TaskCancellationProtection{}, err
	}
	defer done()
	p, ok := plan.(*preparedExecutionCancellation)
	if !ok || p == nil || p.owner != s.state() || p.actor != actor.Details() || p.scope != scope || p.plan == nil {
		return ec.TaskCancellationProtection{}, fault(f.Forbidden)
	}
	ctx = s.protectionContext(ctx, actor, scope)
	if err = s.requireTransitionLifetime(ctx, tx, actor, scope.ProjectID); err != nil {
		return ec.TaskCancellationProtection{}, err
	}
	out, err := s.transferTransitionInTx(ctx, tx, actor, p.plan)
	if err != nil {
		return ec.TaskCancellationProtection{}, err
	}
	b := p.plan.record.Plan.CancellationBlocker
	if b == nil {
		return ec.TaskCancellationProtection{}, internal(nil)
	}
	return ec.TaskCancellationProtection{ID: out.Task.ID.String(), Version: out.Task.Version, State: string(out.Task.State), BlockerID: b.ID.String()}, nil
}

type taskCancelContextKey struct{}
type taskCancelWitness struct {
	owner  *taskTransitionServiceState
	tx     f.Tx
	actor  i.ActorDetails
	scope  ec.CancellationScope
	record *transitionRecord
}

func (s *TaskTransitionService) RequireTaskCancellationInTx(ctx context.Context, tx f.Tx, actor i.Actor, scope ec.CancellationScope) error {
	w, _ := ctx.Value(taskCancelContextKey{}).(*taskCancelWitness)
	if w == nil || w.owner != s.state() || w.tx != tx || w.actor != actor.Details() || w.scope != scope || w.record == nil || w.record.Plan.After.Task.State != c.TaskStateCancelled {
		return fault(f.Forbidden)
	}
	if err := s.requireTransitionLifetime(ctx, tx, actor, scope.ProjectID); err != nil {
		return err
	}
	if _, err := s.state().deps.Authority.requireTransitionAccess(ctx, tx, actor, scope.ProjectID, i.Mutate); err != nil {
		return err
	}
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return portError(err)
	}
	return verifyTransitionPostimage(ctx, x, w.record)
}
func (s *TaskTransitionService) cancelExecutionsInTx(ctx context.Context, tx f.Tx, actor i.Actor, r *transitionRecord) error {
	for _, scope := range r.Plan.CancelExecutions {
		if nilPort(s.state().deps.Cancellation) {
			return fault(f.DependencyUnbound)
		}
		w := &taskCancelWitness{s.state(), tx, actor.Details(), scope, r}
		inner := context.WithValue(ctx, taskCancelContextKey{}, w)
		if _, err := s.state().deps.Cancellation.RequestCancelInTx(inner, tx, actor, scope, nil); err != nil {
			return portError(err)
		}
	}
	return nil
}

func (s *TaskTransitionService) cancellationBlocker(ctx context.Context, x postgres.SQLExecutor, in transitionInput, at f.Instant) (*c.TaskBlocker, error) {
	if in.Cancellation == nil {
		return nil, nil
	}
	if err := in.Cancellation.validate(in); err != nil {
		return nil, err
	}
	var taskCount, projectCount, historyCount int64
	if err := x.QueryRow(ctx, `SELECT count(*) FILTER(WHERE task_id=$2 AND resolved_at IS NULL),count(*) FILTER(WHERE resolved_at IS NULL),count(*) FILTER(WHERE task_id=$2) FROM agenteam_work.task_blockers WHERE project_id=$1`, in.Project.String(), in.Task.String()).Scan(&taskCount, &projectCount, &historyCount); err != nil {
		return nil, taskSQL(err)
	}
	if historyCount >= blockerHistoryCap {
		return nil, field(f.ResourceBusy, "/blocker_id", "BLOCKER_HISTORY_LIMIT")
	}
	if taskCount >= blockerTaskCap || projectCount >= blockerProjectCap {
		return nil, fault(f.ResourceBusy)
	}
	meta := c.TaskBlockerExecutionCancellationMetadata{ExecutionID: in.Cancellation.Scope.ExecutionID, CancelledBy: in.User, Reason: in.Cancellation.Reason}
	b := c.TaskBlocker{ID: in.Cancellation.BlockerID, ProjectID: in.Project, TaskID: in.Task, Type: c.TaskBlockerUserCancelledExecution, Description: "Execution cancelled by user", CreatedAt: at, CreatedBy: c.TaskEventActor{Type: i.Human, UserID: in.User, Source: "task_domain"}, ExecutionCancellation: &meta}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return &b, nil
}
func transitionMutation(events c.TaskTransitionEvents, header event.Header, before, after c.Task, in transitionInput, history []c.TaskTransitionEvent, source, target c.TaskTransitionPosition) (c.TaskTransitionMutation, event.Event, error) {
	if in.Cancellation == nil {
		return events.NewTaskTransitionData(header, before, after, in.Request, history, source, target)
	}
	ids := make([]c.TaskEventID, len(history))
	for n, h := range history {
		if h.Validate() != nil {
			return c.TaskTransitionMutation{}, event.Event{}, internal(nil)
		}
		ids[n] = h.ID
	}
	payload := c.TaskTransitioned{CommandID: history[0].OperationID, Actor: in.historyActor(), TaskEventIDs: ids, MilestoneID: before.MilestoneID, SprintID: before.SprintID, FromState: before.State, ToState: after.State, SourcePosition: source, TargetPosition: target}
	ev, err := events.NewTaskTransitioned(header, payload)
	if err != nil {
		return c.TaskTransitionMutation{}, event.Event{}, err
	}
	out := c.TaskTransitionMutation{Task: after.Clone(), TaskEventIDs: slices.Clone(ids), EventIDs: []event.EventID{header.EventID}}
	return out, ev, out.Validate()
}
func applyCancellationBlocker(ctx context.Context, x postgres.SQLExecutor, r *transitionRecord) error {
	b := r.Plan.CancellationBlocker
	if b == nil {
		return nil
	}
	metadata, err := json.Marshal(b.ExecutionCancellation)
	if err != nil {
		return internal(err)
	}
	actor, err := json.Marshal(b.CreatedBy)
	if err != nil {
		return internal(err)
	}
	return taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_blockers(id,project_id,task_id,type,description,metadata,created_at,created_by,created_transition_operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, b.ID.String(), b.ProjectID.String(), b.TaskID.String(), string(b.Type), b.Description, metadata, b.CreatedAt.Time(), actor, r.ID.String()))
}

func validateCancellationRecord(r *transitionRecord) error {
	in, p := r.Input, r.Plan
	if len(p.CancelDispatches) > 1 || len(p.CancelDispatches) > 0 && p.After.Task.State != c.TaskStateCancelled {
		return internal(nil)
	}
	for _, id := range p.CancelDispatches {
		if _, err := f.ParseID[struct{}](id); err != nil {
			return internal(err)
		}
	}
	if in.Cancellation == nil {
		if p.CancellationBlocker != nil {
			return internal(nil)
		}
	} else {
		b := p.CancellationBlocker
		if in.Cancellation.validate(in) != nil || b == nil || b.Validate() != nil || b.ID != in.Cancellation.BlockerID || b.ProjectID != in.Project || b.TaskID != in.Task || b.Type != c.TaskBlockerUserCancelledExecution || b.ResolvedAt != nil || b.ExecutionCancellation.ExecutionID != in.Cancellation.Scope.ExecutionID || b.ExecutionCancellation.CancelledBy != in.User || !sameValue(b.ExecutionCancellation.Reason, in.Cancellation.Reason) || !b.CreatedAt.Time().Equal(p.After.Task.UpdatedAt.Time()) {
			return internal(nil)
		}
	}
	if len(p.CancelExecutions) > 1 || len(p.CancelExecutions) > 0 && p.After.Task.State != c.TaskStateCancelled {
		return internal(nil)
	}
	for _, scope := range p.CancelExecutions {
		if scope.Validate() != nil || scope.ProjectID != in.Project || scope.TaskID != in.Task.String() {
			return internal(nil)
		}
	}
	return nil
}
func verifyCancellationBlocker(ctx context.Context, x postgres.SQLExecutor, r *transitionRecord) error {
	b := r.Plan.CancellationBlocker
	if b == nil {
		return nil
	}
	row, err := loadBlocker(ctx, &blockerScope{x, r.Input.Project, r.Input.Task}, b.ID)
	if err != nil {
		return err
	}
	if row == nil || !sameValue(row.Value, *b) || row.CreatedTransition == nil || *row.CreatedTransition != r.ID || row.ResolvedOperation != nil || row.ResolvedTransition != nil {
		return fault(f.Forbidden)
	}
	return nil
}
func (s *TaskTransitionService) completeTransitionLocks(actor i.Actor, r *transitionRecord, base []f.LockRequest) ([]f.LockRequest, error) {
	if len(r.Plan.CancelDispatches) > 0 {
		if nilPort(s.state().deps.PendingCancellation) {
			return nil, fault(f.DependencyUnbound)
		}
		locks, err := s.state().deps.PendingCancellation.CancellationLocks(r.Input.Project, r.Plan.CancelDispatches)
		if err != nil {
			return nil, portError(err)
		}
		base = append(base, locks...)
	}
	for _, scope := range r.Plan.CancelExecutions {
		if nilPort(s.state().deps.Cancellation) {
			return nil, fault(f.DependencyUnbound)
		}
		locks, err := s.state().deps.Cancellation.CancelLocks(actor, scope)
		if err != nil {
			return nil, portError(err)
		}
		base = append(base, locks...)
	}
	return taskNormalize(base)
}
func (s *TaskTransitionService) notifyTransitionCancellation(ctx context.Context, plan c.PreparedTaskTransition) {
	p, ok := plan.(*preparedTransition)
	if !ok || p == nil || p.owner != s.state() || nilPort(s.state().deps.Cancellation) {
		return
	}
	for _, scope := range p.record.Plan.CancelExecutions {
		_ = s.state().deps.Cancellation.Notify(ctx, scope)
	}
}

var _ ec.TaskCancellationProvider = (*TaskTransitionService)(nil)
