package work

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
)

type preparedTransition struct {
	owner  *taskTransitionServiceState
	actor  i.ActorDetails
	record *transitionRecord
	event  event.Event
	append oc.AppendPlan
	locks  []f.LockRequest
}

func (p *preparedTransition) RequiredLocks() []f.LockRequest {
	if p == nil {
		return nil
	}
	return slices.Clone(p.locks)
}
func (*preparedTransition) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "prepared_task_transition")
}
func (*preparedTransition) LogValue() slog.Value { return slog.StringValue("prepared_task_transition") }
func (*preparedTransition) MarshalJSON() ([]byte, error) {
	return []byte(`"prepared_task_transition"`), nil
}

func transitionRequest(ctx context.Context, actor i.Actor, meta f.CommandMeta, project c.ProjectID, task c.TaskID, request c.TaskTransfer) (transitionInput, c.TaskTransitionLookupRequest, error) {
	var in transitionInput
	var q c.TaskTransitionLookupRequest
	if err := readInput(ctx, actor, project); err != nil {
		return in, q, err
	}
	if meta.Validate() != nil || meta.ExpectedVersion == nil || task.Validate() != nil {
		return in, q, fault(f.InvalidArgument)
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return in, q, err
	}
	request, err = c.DecodeTaskTransfer(raw)
	if err != nil {
		return in, q, err
	}
	user, err := f.ParseID[i.User](actor.Details().UserID)
	if err != nil {
		return in, q, fault(f.Unauthenticated)
	}
	in = transitionInput{Project: project, Task: task, User: user, Expected: *meta.ExpectedVersion, Request: request.Clone()}
	semantic, err := in.semantic(actor, meta.IdempotencyKey)
	if err != nil {
		return transitionInput{}, q, err
	}
	q = c.TaskTransitionLookupRequest{ProjectID: project, Command: c.TaskTransitionTransfer, IdempotencyKey: meta.IdempotencyKey, SemanticDigest: semantic}
	return in, q, nil
}
func (s *TaskTransitionService) transitionCurrent(ctx context.Context, tx f.Tx, actor i.Actor, q c.TaskTransitionLookupRequest) (postgres.SQLExecutor, *transitionRecord, *c.TaskTransitionMutation, error) {
	x, err := s.state().store.InTx(tx)
	if err != nil {
		return nil, nil, nil, portError(err)
	}
	grant, err := s.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, actor, q.ProjectID, i.Read)
	if err != nil {
		return nil, nil, nil, portError(err)
	}
	if !grant.Matches(actor, q.ProjectID) {
		return nil, nil, nil, fault(f.Forbidden)
	}
	id, err := c.TaskTransitionIdentity(q.ProjectID, q.IdempotencyKey)
	if err != nil {
		return nil, nil, nil, err
	}
	r, err := loadTransition(ctx, x, id)
	if err != nil {
		return nil, nil, nil, err
	}
	if r == nil {
		return x, nil, nil, nil
	}
	if r.Input.User.String() != actor.Details().UserID {
		return nil, nil, nil, fault(f.NotFound)
	}
	if r.Semantic != q.SemanticDigest {
		return nil, nil, nil, fault(f.IdempotencyKeyReused)
	}
	if err = validateTransitionRecord(r, actor); err != nil {
		return nil, nil, nil, err
	}
	if r.State == "completed" {
		v := r.Receipt.Clone()
		return x, r, &v, nil
	}
	return x, r, nil, nil
}
func (s *TaskTransitionService) transitionMutate(ctx context.Context, tx f.Tx, actor i.Actor, project c.ProjectID) error {
	grant, err := s.state().deps.Authority.state().projects.RequireOwnerInTx(ctx, tx, actor, project, i.Mutate)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return fault(f.Forbidden)
	}
	return nil
}

func (s *TaskTransitionService) prepareTransition(ctx context.Context, actor i.Actor, in transitionInput, q c.TaskTransitionLookupRequest) (c.TaskTransitionPreparation, *f.CommitResult, error) {
	id, err := c.TaskTransitionIdentity(in.Project, q.IdempotencyKey)
	if err != nil {
		return c.TaskTransitionPreparation{}, nil, err
	}
	discovery, err := taskNormalize([]f.LockRequest{commandLock(id), userLock(in.User.String(), f.Shared), projectLock(in.Project, f.Shared), taskScheduleLock(in.Project, f.Exclusive), taskLock(in.Task.String(), f.Shared)})
	if err != nil {
		return c.TaskTransitionPreparation{}, nil, err
	}
	var before c.Task
	var out c.TaskTransitionPreparation
	result := s.state().store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, discovery); e != nil {
			return portError(e)
		}
		x, _, receipt, e := s.transitionCurrent(ctx, tx, actor, q)
		if e != nil {
			return e
		}
		if receipt != nil {
			out.CompletedReceipt = receipt
			return nil
		}
		if e = s.transitionMutate(ctx, tx, actor, in.Project); e != nil {
			return e
		}
		before, e = loadTask(ctx, x, in.Project, in.Task)
		if e != nil {
			return e
		}
		if before.Version != in.Expected {
			return field(f.TaskVersionConflict, "/expected_version", "STALE_VERSION")
		}
		return nil
	})
	if result.State() == f.Unknown {
		return c.TaskTransitionPreparation{}, &result, txError(result)
	}
	if err = taskTxError(ctx, result); err != nil {
		return c.TaskTransitionPreparation{}, nil, err
	}
	if out.CompletedReceipt != nil {
		return out, nil, nil
	}
	base, err := in.locks(q.IdempotencyKey, before)
	if err != nil {
		return c.TaskTransitionPreparation{}, nil, err
	}
	var prepared *transitionRecord
	result = s.state().store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, base); e != nil {
			return portError(e)
		}
		x, r, receipt, e := s.transitionCurrent(ctx, tx, actor, q)
		if e != nil {
			return e
		}
		if receipt != nil {
			out.CompletedReceipt = receipt
			return nil
		}
		if e = s.transitionMutate(ctx, tx, actor, in.Project); e != nil {
			return e
		}
		at, e := dbNow(ctx, x)
		if e != nil {
			return e
		}
		insert := r == nil
		if insert {
			rid, e := f.NewID[c.TaskTransitionCommand]()
			if e != nil {
				return unavailable(e)
			}
			r = &transitionRecord{ID: rid, Key: q.IdempotencyKey, Semantic: q.SemanticDigest, Input: in, Revision: 1, State: "planned", Created: at}
		} else {
			n, e := transitionNext(int64(r.Revision))
			if e != nil {
				return e
			}
			r.Revision = f.Version(n)
		}
		count := 1
		if before.AssigneeAgentID == nil || in.Request.AssigneeAgentID != nil && *in.Request.AssigneeAgentID != *before.AssigneeAgentID {
			count++
		}
		if in.Request.Comment != nil {
			count++
		}
		ids := make([]c.TaskEventID, count)
		for n := range ids {
			ids[n], e = f.NewID[c.TaskEvent]()
			if e != nil {
				return unavailable(e)
			}
		}
		slices.SortFunc(ids, func(a, b c.TaskEventID) int {
			if a.String() < b.String() {
				return -1
			}
			if a.String() > b.String() {
				return 1
			}
			return 0
		})
		ev, e := f.NewID[event.EventIdentity]()
		if e != nil {
			return unavailable(e)
		}
		r.Plan, e = s.evaluateTransition(ctx, tx, x, actor, r, before, at, ids, ev)
		if e != nil {
			return e
		}
		if e = storeTransitionPlan(ctx, x, r, insert); e != nil {
			return e
		}
		prepared = r
		return nil
	})
	if result.State() == f.Unknown {
		return c.TaskTransitionPreparation{}, &result, txError(result)
	}
	if err = taskTxError(ctx, result); err != nil {
		return c.TaskTransitionPreparation{}, nil, err
	}
	if out.CompletedReceipt != nil {
		return out, nil, nil
	}
	if prepared == nil {
		return c.TaskTransitionPreparation{}, nil, internal(nil)
	}
	ev, err := s.state().deps.TaskEvents.Restore(prepared.Plan.Header, prepared.Plan.Payload)
	if err != nil {
		return c.TaskTransitionPreparation{}, nil, internal(err)
	}
	appendPlan, err := s.state().deps.Events.PrepareAppend(ctx, actor, ev)
	if err != nil {
		return c.TaskTransitionPreparation{}, nil, portError(err)
	}
	locks, err := taskNormalize(append(slices.Clone(base), appendPlan.Locks()...))
	if err != nil {
		return c.TaskTransitionPreparation{}, nil, err
	}
	if err = ctx.Err(); err != nil {
		return c.TaskTransitionPreparation{}, nil, canceled(err)
	}
	out.Prepared = &preparedTransition{owner: s.state(), actor: actor.Details(), record: prepared, event: ev, append: appendPlan, locks: locks}
	return out, nil, nil
}

func (s *TaskTransitionService) PrepareTaskTransition(ctx context.Context, actor i.Actor, meta f.CommandMeta, p c.ProjectID, task c.TaskID, request c.TaskTransfer) (c.TaskTransitionPreparation, error) {
	ctx, entry, done, err := s.begin(ctx)
	if err != nil {
		return c.TaskTransitionPreparation{}, err
	}
	defer done()
	in, q, err := transitionRequest(ctx, actor, meta, p, task, request)
	if err != nil {
		return c.TaskTransitionPreparation{}, err
	}
	for range 3 {
		out, unknown, e := s.prepareTransition(ctx, actor, in, q)
		if unknown != nil {
			receipt, e := s.confirmTransitionUnknown(ctx, entry, actor, q, *unknown)
			if e != nil {
				return c.TaskTransitionPreparation{}, e
			}
			return c.TaskTransitionPreparation{CompletedReceipt: &receipt}, nil
		}
		if errors.Is(e, errReplan) {
			continue
		}
		return out, e
	}
	return c.TaskTransitionPreparation{}, fault(f.ResourceBusy)
}
func (s *TaskTransitionService) transferTransitionInTx(ctx context.Context, tx f.Tx, actor i.Actor, plan c.PreparedTaskTransition) (c.TaskTransitionMutation, error) {
	p, ok := plan.(*preparedTransition)
	if !ok || p == nil || p.owner != s.state() || !sameValue(p.actor, actor.Details()) {
		return c.TaskTransitionMutation{}, fault(f.Forbidden)
	}
	if err := readInput(ctx, actor, p.record.Input.Project); err != nil {
		return c.TaskTransitionMutation{}, err
	}
	if _, err := s.state().store.InTx(tx); err != nil {
		return c.TaskTransitionMutation{}, portError(err)
	}
	if err := s.state().store.RequireHeldLocks(ctx, tx, p.locks); err != nil {
		return c.TaskTransitionMutation{}, portError(err)
	}
	q := c.TaskTransitionLookupRequest{ProjectID: p.record.Input.Project, Command: c.TaskTransitionTransfer, IdempotencyKey: p.record.Key, SemanticDigest: p.record.Semantic}
	x, r, replay, err := s.transitionCurrent(ctx, tx, actor, q)
	if err != nil {
		return c.TaskTransitionMutation{}, err
	}
	if replay != nil {
		return replay.Clone(), nil
	}
	if err = s.transitionMutate(ctx, tx, actor, q.ProjectID); err != nil {
		return c.TaskTransitionMutation{}, err
	}
	if r == nil || r.ID != p.record.ID || r.Revision != p.record.Revision || !sameValue(r.Plan, p.record.Plan) {
		return c.TaskTransitionMutation{}, errReplan
	}
	current, err := s.evaluateTransition(ctx, tx, x, actor, r, r.Plan.Before, r.Plan.Header.OccurredAt, r.Plan.After.TaskEventIDs, r.Plan.Header.EventID)
	if err != nil {
		return c.TaskTransitionMutation{}, err
	}
	if !sameValue(current, r.Plan) {
		return c.TaskTransitionMutation{}, errReplan
	}
	if err = applyTransition(ctx, x, r); err != nil {
		return c.TaskTransitionMutation{}, err
	}
	receipt, err := s.state().deps.Events.AppendEventInTx(ctx, tx, actor, p.event, p.append)
	if err != nil {
		return c.TaskTransitionMutation{}, portError(err)
	}
	if receipt.EventID != r.Plan.Header.EventID || receipt.Sequence.Validate() != nil {
		return c.TaskTransitionMutation{}, internal(nil)
	}
	if err = completeTransition(ctx, x, r); err != nil {
		return c.TaskTransitionMutation{}, err
	}
	if err = s.state().deps.Activity.TouchActivityInTx(ctx, tx, actor); err != nil {
		return c.TaskTransitionMutation{}, portError(err)
	}
	if err = ctx.Err(); err != nil {
		return c.TaskTransitionMutation{}, canceled(err)
	}
	return current.After.Clone(), nil
}
func (s *TaskTransitionService) TransferTaskInTx(ctx context.Context, tx f.Tx, actor i.Actor, p c.PreparedTaskTransition) (c.TaskTransitionMutation, error) {
	ctx, _, done, err := s.begin(ctx)
	if err != nil {
		return c.TaskTransitionMutation{}, err
	}
	defer done()
	out, err := s.transferTransitionInTx(ctx, tx, actor, p)
	if errors.Is(err, errReplan) {
		return c.TaskTransitionMutation{}, fault(f.ResourceBusy)
	}
	return out, err
}
func (s *TaskTransitionService) TransferTask(ctx context.Context, actor i.Actor, meta f.CommandMeta, p c.ProjectID, task c.TaskID, request c.TaskTransfer) (c.TaskTransitionMutation, error) {
	ctx, entry, done, err := s.begin(ctx)
	if err != nil {
		return c.TaskTransitionMutation{}, err
	}
	defer done()
	in, q, err := transitionRequest(ctx, actor, meta, p, task, request)
	if err != nil {
		return c.TaskTransitionMutation{}, err
	}
	id, _ := c.TaskTransitionIdentity(p, meta.IdempotencyKey)
	for range 3 {
		prepared, unknown, e := s.prepareTransition(ctx, actor, in, q)
		if unknown != nil {
			return s.confirmTransitionUnknown(ctx, entry, actor, q, *unknown)
		}
		if errors.Is(e, errReplan) {
			continue
		}
		if e != nil {
			return c.TaskTransitionMutation{}, e
		}
		if prepared.CompletedReceipt != nil {
			return prepared.CompletedReceipt.Clone(), nil
		}
		var out c.TaskTransitionMutation
		replan := false
		result := s.state().store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
			if e := s.state().store.AcquireAll(ctx, tx, prepared.Prepared.RequiredLocks()); e != nil {
				return portError(e)
			}
			var e error
			out, e = s.transferTransitionInTx(ctx, tx, actor, prepared.Prepared)
			replan = errors.Is(e, errReplan)
			return e
		})
		if result.State() == f.Unknown {
			return s.confirmTransitionUnknown(ctx, entry, actor, q, result)
		}
		if result.State() == f.NotCommitted && replan && ctx.Err() == nil {
			continue
		}
		if e = taskTxError(ctx, result); e != nil {
			return c.TaskTransitionMutation{}, e
		}
		if out.Validate() != nil {
			return c.TaskTransitionMutation{}, internal(nil)
		}
		return out.Clone(), nil
	}
	return c.TaskTransitionMutation{}, fault(f.ResourceBusy)
}
func (s *TaskTransitionService) LookupTaskTransition(ctx context.Context, actor i.Actor, q c.TaskTransitionLookupRequest) (c.TaskTransitionLookup, error) {
	ctx, _, done, err := s.begin(ctx)
	if err != nil {
		return c.TaskTransitionLookup{}, err
	}
	defer done()
	if err = readInput(ctx, actor, q.ProjectID); err != nil {
		return c.TaskTransitionLookup{}, err
	}
	if err = q.Validate(); err != nil {
		return c.TaskTransitionLookup{}, err
	}
	return s.lookupTransition(ctx, actor, q)
}
func (s *TaskTransitionService) lookupTransition(ctx context.Context, actor i.Actor, q c.TaskTransitionLookupRequest) (c.TaskTransitionLookup, error) {
	id, err := c.TaskTransitionIdentity(q.ProjectID, q.IdempotencyKey)
	if err != nil {
		return c.TaskTransitionLookup{}, err
	}
	locks, err := taskNormalize([]f.LockRequest{commandLock(id), userLock(actor.Details().UserID, f.Shared), projectLock(q.ProjectID, f.Shared)})
	if err != nil {
		return c.TaskTransitionLookup{}, err
	}
	var out c.TaskTransitionLookup
	result := s.state().store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		_, r, receipt, e := s.transitionCurrent(ctx, tx, actor, q)
		if e != nil {
			return e
		}
		switch {
		case receipt != nil:
			out = c.TaskTransitionLookup{Status: c.LookupCommitted, Receipt: receipt}
		case r != nil:
			out = c.TaskTransitionLookup{Status: c.LookupInProgress}
		default:
			out = c.TaskTransitionLookup{Status: c.LookupNotObserved}
		}
		return nil
	})
	if err = taskTxError(ctx, result); err != nil {
		return c.TaskTransitionLookup{}, err
	}
	return out.Clone(), nil
}
func (s *TaskTransitionService) confirmTransitionUnknown(ctx context.Context, entry *call, actor i.Actor, q c.TaskTransitionLookupRequest, original f.CommitResult) (c.TaskTransitionMutation, error) {
	confirm, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	token := &confirmation{cancel: cancel}
	st := s.state()
	st.mu.Lock()
	entry.confirmations[token] = struct{}{}
	if st.stopped {
		cancel()
	}
	st.mu.Unlock()
	defer func() { cancel(); st.mu.Lock(); delete(entry.confirmations, token); st.mu.Unlock() }()
	out, err := s.lookupTransition(confirm, actor, q)
	if err != nil {
		return c.TaskTransitionMutation{}, txError(original)
	}
	if out.Status == c.LookupCommitted && out.Receipt != nil {
		return out.Receipt.Clone(), nil
	}
	if out.Status == c.LookupNotObserved {
		return c.TaskTransitionMutation{}, notCommittedAfterUnknown(original)
	}
	return c.TaskTransitionMutation{}, txError(original)
}

var _ c.TaskTransitionComposer = (*TaskTransitionService)(nil)
