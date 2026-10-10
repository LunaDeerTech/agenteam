package work

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Controlled SQL/Owner/Outbox ports exercise the actual public Task service,
// compact plan codecs, placement reader and historical replay. They do not
// claim a real Session, database commit, Scheduler claim or Outbox delivery.
type titleUpdateStore struct {
	denialStore
	tx         f.Tx
	locks      []f.LockRequest
	task       c.Task
	command    taskTriggerRow
	history    taskTriggerRow
	generation int64
	writes     int
	appends    int
	touches    int
	clock      time.Time
}

func (s *titleUpdateStore) InTx(tx f.Tx) (postgres.SQLExecutor, error) {
	if tx != s.tx || tx == (f.Tx{}) {
		return nil, fault(f.Forbidden)
	}
	return s, nil
}
func (s *titleUpdateStore) AcquireAll(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.locks = append([]f.LockRequest(nil), locks...)
	return nil
}
func (s *titleUpdateStore) RequireHeldLocks(_ context.Context, tx f.Tx, locks []f.LockRequest) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	for _, want := range locks {
		found := false
		for _, held := range s.locks {
			if f.CompareLockKeys(want.Key, held.Key) == 0 && (held.Mode == f.Exclusive || held.Mode == want.Mode) {
				found = true
			}
		}
		if !found {
			return fault(f.Forbidden)
		}
	}
	return nil
}
func (s *titleUpdateStore) WithinTx(ctx context.Context, _ f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	beforeTask, beforeCommand, beforeHistory := s.task.Clone(), append(taskTriggerRow(nil), s.command...), append(taskTriggerRow(nil), s.history...)
	beforeGeneration, beforeWrites, beforeAppends, beforeTouches := s.generation, s.writes, s.appends, s.touches
	s.tx, s.locks = f.NewTx(), nil
	defer func() { s.tx, s.locks = f.Tx{}, nil }()
	if err := fn(ctx, s.tx); err != nil {
		s.task, s.command, s.history = beforeTask, beforeCommand, beforeHistory
		s.generation, s.writes, s.appends, s.touches = beforeGeneration, beforeWrites, beforeAppends, beforeTouches
		var problem *f.Fault
		if errors.As(err, &problem) {
			return f.NotCommittedResult(problem)
		}
		return f.NotCommittedResult(f.NewFault(f.InternalError, f.NotCommitted).WithCause(err))
	}
	return f.CommittedResult()
}

type titleUpdateRowError struct{ err error }

func (r titleUpdateRowError) Scan(...any) error { return r.err }
func (s *titleUpdateStore) QueryRow(_ context.Context, query string, args ...any) postgres.Row {
	t := s.task
	switch {
	case query == "SELECT clock_timestamp()":
		return taskTriggerRow{s.clock}
	case strings.Contains(query, "FROM agenteam_work.task_commands"):
		if s.command == nil || args[0] != t.ProjectID.String() || args[1] != string(s.command[3].(c.TaskCommandName)) || args[2] != string(s.command[4].(f.IdempotencyKey)) {
			return titleUpdateRowError{pgx.ErrNoRows}
		}
		return s.command
	case strings.Contains(query, "FROM agenteam_work.tasks WHERE"):
		var agent *string
		if t.AssigneeAgentID != nil {
			v := t.AssigneeAgentID.String()
			agent = &v
		}
		return taskTriggerRow{t.ID.String(), t.ProjectID.String(), t.MilestoneID.String(), t.SprintID.String(), t.Title, t.Description, t.Type, t.Priority, t.State, agent, t.Plan, t.ManualRank, t.Version, t.CreatedAt.Time(), t.UpdatedAt.Time()}
	case strings.Contains(query, "FROM agenteam_work.sprints"):
		return taskTriggerRow{t.SprintID.String(), t.ProjectID.String(), t.MilestoneID.String(), "Planning sprint", "", t.ManualRank, f.Version(1), t.CreatedAt.Time(), t.CreatedAt.Time(), (*time.Time)(nil), []byte(nil), (*time.Time)(nil), []byte(nil)}
	case strings.Contains(query, "FROM agenteam_work.milestones"):
		return taskTriggerRow{t.MilestoneID.String(), t.ProjectID.String(), "Planning milestone", "", t.ManualRank, f.Version(1), t.CreatedAt.Time(), t.CreatedAt.Time()}
	case strings.Contains(query, "FROM agenteam_work.task_query_generations"):
		return taskTriggerRow{s.generation}
	}
	return titleUpdateRowError{errors.New("unexpected title control query")}
}
func (s *titleUpdateStore) Exec(_ context.Context, query string, a ...any) (pgconn.CommandTag, error) {
	switch {
	case strings.HasPrefix(query, "INSERT INTO agenteam_work.task_commands"):
		if s.command != nil {
			return pgconn.CommandTag{}, errors.New("unexpected second command insertion")
		}
		r := taskTriggerRow{a[0], a[1], a[2], c.TaskCommandName(a[3].(string)), f.IdempotencyKey(a[4].(string)), f.Digest(a[5].(string)), a[6], f.Version(a[7].(int64)), "planned", a[8], (*string)(nil), (*string)(nil), []byte(nil), time.Time{}, (*time.Time)(nil)}
		if strings.Contains(query, "'planned'") {
			history, outbox := a[9].(string), a[10].(string)
			r[10], r[11], r[13] = &history, &outbox, a[11]
		} else {
			done := a[10].(time.Time)
			r[8], r[9], r[12], r[13], r[14] = "completed", []byte(nil), a[8], a[9], &done
		}
		s.command = r
	case query == "UPDATE agenteam_work.tasks SET version=$4,updated_at=$5,title=$6 WHERE project_id=$1 AND id=$2 AND version=$3":
		if a[0] != s.task.ProjectID.String() || a[1] != s.task.ID.String() || a[2] != int64(s.task.Version) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		s.task.Title, s.task.Version = a[5].(string), f.Version(a[3].(int64))
		at, err := f.NewInstant(a[4].(time.Time))
		if err != nil {
			return pgconn.CommandTag{}, err
		}
		s.task.UpdatedAt = at
	case strings.HasPrefix(query, "INSERT INTO agenteam_work.task_query_generations"):
		if a[2] != s.generation {
			return pgconn.NewCommandTag("INSERT 0 0"), nil
		}
		s.generation = a[1].(int64)
	case strings.HasPrefix(query, "INSERT INTO agenteam_work.task_events"):
		s.history = append(taskTriggerRow(nil), a...)
	case strings.HasPrefix(query, "UPDATE agenteam_work.task_commands SET state='completed'"):
		if s.command == nil || a[0] != s.command[0] || a[3] != int64(s.command[7].(f.Version)) {
			return pgconn.NewCommandTag("UPDATE 0"), nil
		}
		done := a[2].(time.Time)
		s.command[8], s.command[12], s.command[14] = "completed", a[1], &done
	default:
		return pgconn.CommandTag{}, errors.New("unexpected title control write")
	}
	s.writes++
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

type titleUpdateOwner struct {
	pc.ProjectAuthority
	store   *titleUpdateStore
	actor   i.Actor
	ref     pc.ProjectRef
	revoked bool
}

func (p *titleUpdateOwner) RequireOwnerInTx(ctx context.Context, tx f.Tx, actor i.Actor, project i.ProjectID, intent i.AccessIntent) (pc.ProjectAccess, error) {
	if err := p.store.RequireHeldLocks(ctx, tx, []f.LockRequest{userLock(actor.Details().UserID, f.Shared), projectLock(project, f.Shared)}); err != nil {
		return pc.ProjectAccess{}, err
	}
	if p.revoked {
		return pc.ProjectAccess{}, fault(f.SessionRevoked)
	}
	if actor.Details() != p.actor.Details() || project != p.ref.ID {
		return pc.ProjectAccess{}, fault(f.NotFound)
	}
	if intent == i.Mutate && p.ref.Lifecycle != pc.Active {
		return pc.ProjectAccess{}, fault(f.ProjectNotActive)
	}
	return pc.NewProjectAccess(actor, p.ref, p.ref.UpdatedAt)
}

type titleUpdateEvents struct {
	store   *titleUpdateStore
	prepare func()
}

func (p *titleUpdateEvents) PrepareAppend(_ context.Context, actor i.Actor, e event.Event) (oc.AppendPlan, error) {
	r, err := scanTaskCommand(p.store.command)
	if err != nil {
		return oc.AppendPlan{}, err
	}
	if err = validateTaskRecord(r, actor); err != nil {
		return oc.AppendPlan{}, err
	}
	if !sameValue(r.Plan.Header, e.Header()) || string(r.Plan.Payload) != string(e.PayloadBytes()) {
		return oc.AppendPlan{}, errors.New("different title event")
	}
	if p.prepare != nil {
		p.prepare()
	}
	// The controlled appender adds no locks; actual Outbox authority is covered
	// by the real integration fixture, not by this zero plan.
	return oc.AppendPlan{}, nil
}
func (p *titleUpdateEvents) AppendEventInTx(_ context.Context, tx f.Tx, _ i.Actor, e event.Event, _ oc.AppendPlan) (oc.AppendReceipt, error) {
	if _, err := p.store.InTx(tx); err != nil || p.store.history == nil {
		return oc.AppendReceipt{}, errors.New("title event preceded original history/Tx")
	}
	p.store.appends++
	return oc.AppendReceipt{EventID: e.Header().EventID, Sequence: 1}, nil
}
func (s *titleUpdateStore) TouchActivityInTx(_ context.Context, tx f.Tx, _ i.Actor) error {
	if _, err := s.InTx(tx); err != nil {
		return err
	}
	s.touches++
	return nil
}

func titleUpdateFixture(t *testing.T) (*TaskService, *titleUpdateStore, *titleUpdateOwner, *titleUpdateEvents, i.Actor, f.CommandMeta) {
	t.Helper()
	r, actor, _ := pureTaskRecord(t)
	agent := pureID[i.Agent](t, 22)
	task := r.Plan.After.Task.Clone()
	task.State, task.AssigneeAgentID, task.Version = c.TaskStateInProgress, &agent, 3
	s := &titleUpdateStore{task: task, generation: 7, clock: task.UpdatedAt.Time().Add(time.Second)}
	p := &titleUpdateOwner{store: s, actor: actor, ref: pc.ProjectRef{ID: task.ProjectID, OwnerUserID: r.User, Name: "title-update", NormalizedName: "title-update", Lifecycle: pc.Active, Version: 1, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}}
	authority, err := NewAuthority(s, p)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := NewReader(s, authority, pureKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	events, err := c.RegisterTaskEvents(event.NewCatalog())
	if err != nil {
		t.Fatal(err)
	}
	box := &titleUpdateEvents{store: s}
	service, err := NewTask(s, TaskDependencies{Authority: authority, Structure: reader, Events: box, TaskEvents: events, Activity: s, Pending: &denialTaskPending{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		service.Stop()
		if err := service.Drain(context.Background()); err != nil {
			t.Error(err)
		}
	})
	version := task.Version
	return service, s, p, box, actor, f.CommandMeta{RequestID: pureID[f.Request](t, 23), IdempotencyKey: "title-update", ExpectedVersion: &version}
}

func TestTaskInProgressTitleUpdateAndReplay(t *testing.T) {
	for _, name := range []string{"changed", "no-op", "existing-backlog"} {
		t.Run(name, func(t *testing.T) {
			service, store, _, _, actor, meta := titleUpdateFixture(t)
			if name == "existing-backlog" {
				store.task.State, store.task.AssigneeAgentID = c.TaskStateBacklog, nil
			}
			noOp := name == "no-op"
			before := store.task.Clone()
			title := "Owner changed title"
			if noOp {
				title = before.Title
			}
			request := c.TaskFieldsUpdate{Title: &title}
			got, err := service.UpdateTask(context.Background(), actor, meta, before.ProjectID, before.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			want := before.Clone()
			want.Title = title
			if !noOp {
				want.Version++
				want.UpdatedAt, _ = f.NewInstant(store.clock)
			}
			if got.Changed == noOp || !sameValue(got.Task, want) || !sameValue(store.task, want) || store.touches != 1 {
				t.Fatal("title mutation changed other facts or lost activity")
			}
			if noOp {
				if store.history != nil || store.appends != 0 || store.generation != 7 {
					t.Fatal("no-op produced business facts")
				}
			} else {
				var payload c.TaskFieldsUpdatedPayload
				if store.history == nil || json.Unmarshal(store.history[8].([]byte), &payload) != nil || !sameValue(payload.ChangedFields, []c.TaskChangedField{c.TaskTitleChanged}) || payload.Position != nil || payload.TypeChange != nil || payload.PriorityChange != nil || store.appends != 1 || store.generation != 8 {
					t.Fatal("title-only history/event/query generation mismatch")
				}
			}
			writes, appends := store.writes, store.appends
			// Later Task facts cannot replace the immutable completed receipt.
			store.task.Title, store.task.Version = "A later title", 8
			replay, err := service.UpdateTask(context.Background(), actor, meta, before.ProjectID, before.ID, request)
			if err != nil || !sameValue(replay, got) {
				t.Fatal("same-key replay changed", err)
			}
			digest, err := c.TaskUpdateDigest(actor, meta, before.ProjectID, before.ID, request)
			if err != nil {
				t.Fatal(err)
			}
			lookup, err := service.LookupTaskCommand(context.Background(), actor, c.TaskCommandLookupRequest{ProjectID: before.ProjectID, Command: c.TaskCommandUpdate, IdempotencyKey: meta.IdempotencyKey, SemanticDigest: digest})
			if err != nil || lookup.Status != c.LookupCommitted || lookup.Receipt == nil || !sameValue(*lookup.Receipt, got) || store.writes != writes || store.appends != appends || store.touches != 1 {
				t.Fatal("Lookup/replay mutated or lost original receipt", err)
			}
			otherTitle := "Different original command intent"
			_, err = service.UpdateTask(context.Background(), actor, meta, before.ProjectID, before.ID, c.TaskFieldsUpdate{Title: &otherTitle})
			pureCode(t, err, f.IdempotencyKeyReused)
			if store.writes != writes || store.appends != appends || store.touches != 1 {
				t.Fatal("same key with a different title produced facts")
			}
		})
	}
}

func TestTaskInProgressTitlePreservesWriteBoundaries(t *testing.T) {
	for _, mode := range []string{"stale", "priority", "description", "plan", "type", "reorder", "todo", "terminal", "revoked-before-final"} {
		t.Run(mode, func(t *testing.T) {
			service, store, owner, box, actor, meta := titleUpdateFixture(t)
			title, other, kind, priority := "Owner changed title", "unchanged", c.TaskTypeTask, c.TaskPriorityCritical
			request := c.TaskFieldsUpdate{Title: &title}
			want := f.DependencyUnbound
			switch mode {
			case "stale":
				*meta.ExpectedVersion = 2
				want = f.TaskVersionConflict
			case "priority":
				request.Priority = &priority
			case "description":
				request.Description = &other
			case "plan":
				request.Plan = &other
			case "type":
				request.Type = &kind
			case "todo":
				store.task.State = c.TaskStateTodo
			case "terminal":
				store.task.State = c.TaskStateDone
				want = f.TaskTerminalImmutable
			case "revoked-before-final":
				box.prepare = func() { owner.revoked = true }
				want = f.SessionRevoked
			}
			before := store.task.Clone()
			var err error
			if mode == "reorder" {
				_, err = service.ReorderTask(context.Background(), actor, meta, before.ProjectID, before.ID, c.TaskReorder{})
			} else {
				_, err = service.UpdateTask(context.Background(), actor, meta, before.ProjectID, before.ID, request)
			}
			pureCode(t, err, want)
			if !sameValue(store.task, before) || store.history != nil || store.appends != 0 || store.touches != 0 || store.generation != 7 {
				t.Fatal("rejected title request changed business facts")
			}
		})
	}
}
