package work

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

const blockerHistoryCap = 4096
const blockerTaskCap = 256
const blockerProjectCap = 262144

type blockerInput struct {
	Command  c.TaskBlockerCommandName `json:"command"`
	Project  c.ProjectID              `json:"project_id"`
	Target   c.TaskID                 `json:"task_id"`
	User     i.UserID                 `json:"actor_user_id"`
	Expected f.Version                `json:"expected_version"`
	Add      *c.TaskBlockerCreate     `json:"add"`
	Resolve  *c.TaskBlockerResolve    `json:"resolve"`
}

func (in blockerInput) semantic(a i.Actor, key f.IdempotencyKey) (f.Digest, error) {
	if in.User.String() != a.Details().UserID || in.Target.Validate() != nil || in.Expected.Validate() != nil {
		return "", fault(f.InvalidArgument)
	}
	rid, e := f.ParseID[f.Request](in.Target.String())
	if e != nil {
		return "", fault(f.InvalidArgument)
	}
	m := f.CommandMeta{RequestID: rid, IdempotencyKey: key, ExpectedVersion: &in.Expected}
	if in.Command == c.TaskBlockerCommandAdd && in.Add != nil && in.Resolve == nil {
		return c.TaskBlockerAddDigest(a, m, in.Project, in.Target, *in.Add)
	}
	if in.Command == c.TaskBlockerCommandResolve && in.Resolve != nil && in.Add == nil {
		return c.TaskBlockerResolveDigest(a, m, in.Project, in.Target, *in.Resolve)
	}
	return "", fault(f.InvalidArgument)
}
func (in blockerInput) locks(key f.IdempotencyKey, sprint c.SprintID) ([]f.LockRequest, error) {
	id, e := c.TaskBlockerCommandIdentity(in.Project, in.Command, key)
	if e != nil {
		return nil, e
	}
	return taskNormalize([]f.LockRequest{commandLock(id), userLock(in.User.String(), f.Exclusive), projectLock(in.Project, f.Shared), taskScheduleLock(in.Project, f.Exclusive), sprintLock(sprint.String(), f.Shared), taskLock(in.Target.String(), f.Exclusive)})
}
func (in blockerInput) blockerID() c.TaskBlockerID {
	if in.Add != nil {
		return in.Add.BlockerID
	}
	if in.Resolve != nil {
		return in.Resolve.BlockerID
	}
	return c.TaskBlockerID{}
}
func (v *blockerInput) UnmarshalJSON(raw []byte) error {
	fields, e := taskPrivateObject(raw, taskRequestCap, []string{"command", "project_id", "task_id", "actor_user_id", "expected_version", "add", "resolve"}, []string{"add", "resolve"})
	if e != nil {
		return e
	}
	var n blockerInput
	for _, p := range []struct {
		k string
		v any
	}{{"command", &n.Command}, {"project_id", &n.Project}, {"task_id", &n.Target}, {"actor_user_id", &n.User}, {"expected_version", &n.Expected}} {
		if e = json.Unmarshal(fields[p.k], p.v); e != nil {
			return internal(e)
		}
	}
	if !bytes.Equal(fields["add"], []byte("null")) {
		n.Add = new(c.TaskBlockerCreate)
		if e = n.Add.UnmarshalJSON(fields["add"]); e != nil {
			return internal(e)
		}
	}
	if !bytes.Equal(fields["resolve"], []byte("null")) {
		n.Resolve = new(c.TaskBlockerResolve)
		if e = n.Resolve.UnmarshalJSON(fields["resolve"]); e != nil {
			return internal(e)
		}
	}
	if n.Command.Validate() != nil || n.Project.Validate() != nil || n.Target.Validate() != nil || n.User.Validate() != nil || n.Expected.Validate() != nil || (n.Command == c.TaskBlockerCommandAdd) != (n.Add != nil) || (n.Command == c.TaskBlockerCommandResolve) != (n.Resolve != nil) {
		return internal(nil)
	}
	*v = n
	return nil
}

type blockerPlan struct {
	Before          c.Task                `json:"before"`
	BlockerBefore   *c.TaskBlocker        `json:"blocker_before"`
	After           c.TaskBlockerMutation `json:"after"`
	Placement       taskPlacement         `json:"placement"`
	QueryGeneration int64                 `json:"query_generation"`
	TaskEvent       c.TaskBlockerEvent    `json:"task_event"`
	Header          event.Header          `json:"header"`
	Payload         c.TaskBlockersChanged `json:"payload"`
}

func (v *blockerPlan) UnmarshalJSON(raw []byte) error {
	fields, e := taskPrivateObject(raw, taskPlanCap, []string{"before", "blocker_before", "after", "placement", "query_generation", "task_event", "header", "payload"}, []string{"blocker_before"})
	if e != nil {
		return e
	}
	var n blockerPlan
	if e = n.Before.UnmarshalJSON(fields["before"]); e != nil {
		return internal(e)
	}
	if !bytes.Equal(fields["blocker_before"], []byte("null")) {
		n.BlockerBefore = new(c.TaskBlocker)
		if e = n.BlockerBefore.UnmarshalJSON(fields["blocker_before"]); e != nil {
			return internal(e)
		}
	}
	if e = n.After.UnmarshalJSON(fields["after"]); e != nil {
		return internal(e)
	}
	if e = n.Placement.UnmarshalJSON(fields["placement"]); e != nil {
		return internal(e)
	}
	if e = json.Unmarshal(fields["query_generation"], &n.QueryGeneration); e != nil {
		return internal(e)
	}
	if e = n.TaskEvent.UnmarshalJSON(fields["task_event"]); e != nil {
		return internal(e)
	}
	if n.Header, e = decodeTaskPlanHeader(fields["header"]); e != nil {
		return e
	}
	if e = n.Payload.UnmarshalJSON(fields["payload"]); e != nil {
		return internal(e)
	}
	*v = n
	return nil
}

type blockerRecord struct {
	ID          c.TaskBlockerCommandID
	Project     c.ProjectID
	User        i.UserID
	Command     c.TaskBlockerCommandName
	Key         f.IdempotencyKey
	Semantic    f.Digest
	Input       blockerInput
	Revision    f.Version
	State       string
	Plan        *blockerPlan
	TaskEventID c.TaskEventID
	EventID     event.EventID
	Receipt     *c.TaskBlockerMutation
	Created     f.Instant
	Committed   *f.Instant
}

func (blockerInput) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "work_task_blocker") }
func (blockerInput) LogValue() slog.Value        { return slog.StringValue("work_task_blocker") }
func (blockerPlan) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "work_task_blocker") }
func (blockerPlan) LogValue() slog.Value         { return slog.StringValue("work_task_blocker") }
func (blockerRecord) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "work_task_blocker") }
func (blockerRecord) LogValue() slog.Value       { return slog.StringValue("work_task_blocker") }

const blockerCommandColumns = `id::text,project_id::text,actor_user_id::text,command_name,idempotency_key,semantic_digest,request,plan_revision,state,plan,task_event_id::text,event_id::text,receipt,created_at,committed_at`

func scanBlockerCommand(row interface{ Scan(...any) error }, writer string) (*blockerRecord, error) {
	var v blockerRecord
	var id, p, u, te, ev string
	var request, plan, receipt []byte
	var at time.Time
	var done *time.Time
	if e := row.Scan(&id, &p, &u, &v.Command, &v.Key, &v.Semantic, &request, &v.Revision, &v.State, &plan, &te, &ev, &receipt, &at, &done); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, taskSQL(e)
	}
	// The current Owner can encounter a previous Owner's key. Do not inspect
	// that writer's digest or private body before the indistinguishable denial.
	if u != writer {
		return nil, fault(f.NotFound)
	}
	var e error
	if v.ID, e = f.ParseID[c.TaskBlockerCommand](id); e != nil {
		return nil, internal(e)
	}
	if v.Project, e = f.ParseID[i.Project](p); e != nil {
		return nil, internal(e)
	}
	if v.User, e = f.ParseID[i.User](u); e != nil {
		return nil, internal(e)
	}
	if v.TaskEventID, e = f.ParseID[c.TaskEvent](te); e != nil {
		return nil, internal(e)
	}
	if v.EventID, e = f.ParseID[event.EventIdentity](ev); e != nil {
		return nil, internal(e)
	}
	if v.Created, e = f.NewInstant(at); e != nil {
		return nil, internal(e)
	}
	if done != nil {
		n, e := f.NewInstant(*done)
		if e != nil {
			return nil, internal(e)
		}
		v.Committed = &n
	}
	if e = v.Input.UnmarshalJSON(request); e != nil {
		return nil, e
	}
	v.Plan = new(blockerPlan)
	if e = v.Plan.UnmarshalJSON(plan); e != nil {
		return nil, e
	}
	if receipt != nil {
		v.Receipt = new(c.TaskBlockerMutation)
		if e = v.Receipt.UnmarshalJSON(receipt); e != nil {
			return nil, internal(e)
		}
	}
	if v.Command.Validate() != nil || v.Key.Validate() != nil || v.Semantic.Validate() != nil || v.Revision.Validate() != nil || v.Input.Command != v.Command || v.Input.Project != v.Project || v.Input.User != v.User {
		return nil, internal(nil)
	}
	if v.State == "planned" {
		if v.Receipt != nil || v.Committed != nil {
			return nil, internal(nil)
		}
	} else if v.State == "completed" {
		if v.Receipt == nil || v.Committed == nil || v.Committed.Time().Before(v.Created.Time()) {
			return nil, internal(nil)
		}
	} else {
		return nil, internal(nil)
	}
	return &v, nil
}
func loadBlockerCommand(ctx context.Context, x postgres.SQLExecutor, id f.CommandIdentity, writer string) (*blockerRecord, error) {
	return scanBlockerCommand(x.QueryRow(ctx, `SELECT `+blockerCommandColumns+` FROM agenteam_work.task_blocker_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, id.OwnerIDs()[0], id.Command(), string(id.Key())), writer)
}
func loadBlockerEventCommand(ctx context.Context, x postgres.SQLExecutor, id event.EventID, writer string) (*blockerRecord, error) {
	return scanBlockerCommand(x.QueryRow(ctx, `SELECT `+blockerCommandColumns+` FROM agenteam_work.task_blocker_commands WHERE event_id=$1`, id.String()), writer)
}
func storeBlockerPlan(ctx context.Context, x postgres.SQLExecutor, r *blockerRecord, insert bool) error {
	request, e := canonical(r.Input)
	if e != nil || len(request) > taskRequestCap {
		return internal(e)
	}
	plan, e := canonical(r.Plan)
	if e != nil || len(plan) > taskPlanCap {
		return internal(e)
	}
	if insert {
		return taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_blocker_commands(id,project_id,actor_user_id,command_name,idempotency_key,semantic_digest,request,plan_revision,state,plan,task_event_id,event_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'planned',$9,$10,$11,$12)`, r.ID.String(), r.Project.String(), r.User.String(), string(r.Command), string(r.Key), string(r.Semantic), request, int64(r.Revision), plan, r.TaskEventID.String(), r.EventID.String(), r.Created.Time()))
	}
	return taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_blocker_commands SET plan_revision=$2,plan=$3,task_event_id=$4,event_id=$5 WHERE id=$1 AND state='planned' AND plan_revision=$6`, r.ID.String(), int64(r.Revision), plan, r.TaskEventID.String(), r.EventID.String(), int64(r.Revision-1)))
}
func completeBlockerCommand(ctx context.Context, x postgres.SQLExecutor, r *blockerRecord, out c.TaskBlockerMutation) error {
	raw, e := canonical(out)
	if e != nil || len(raw) > taskRequestCap {
		return internal(e)
	}
	at, e := dbNow(ctx, x)
	if e != nil {
		return e
	}
	if at.Time().Before(r.Created.Time()) {
		at = r.Created
	}
	return taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_blocker_commands SET state='completed',receipt=$2,committed_at=$3 WHERE id=$1 AND state='planned' AND plan_revision=$4`, r.ID.String(), raw, at.Time(), int64(r.Revision)))
}

// Only a live, authorized caller transaction can mint this private scope. It
// holds the complete domain locks before any canonical Blocker/graph SQL.

func applyBlockerPlan(ctx context.Context, s *blockerScope, r *blockerRecord) error {
	p := r.Plan
	t := p.After.Task
	b := p.After.Blocker
	x := s.x
	if e := taskAffected(x.Exec(ctx, `UPDATE agenteam_work.tasks SET version=$4,updated_at=$5 WHERE project_id=$1 AND id=$2 AND version=$3`, r.Project.String(), t.ID.String(), int64(r.Input.Expected), int64(t.Version), t.UpdatedAt.Time())); e != nil {
		return e
	}
	if r.Input.Add != nil {
		var metadata any = b.Metadata.WaitingForHuman
		if b.Type == c.TaskBlockerRelyOn {
			metadata = b.Metadata.RelyOn
		}
		raw, e := canonical(metadata)
		if e != nil {
			return e
		}
		actor, e := canonical(b.CreatedBy)
		if e != nil {
			return e
		}
		if e = taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_blockers(id,project_id,task_id,type,description,metadata,created_at,created_by,created_operation_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, b.ID.String(), b.ProjectID.String(), b.TaskID.String(), string(b.Type), b.Description, raw, b.CreatedAt.Time(), actor, r.ID.String())); e != nil {
			return e
		}
	} else {
		actor, e := canonical(b.ResolvedBy)
		if e != nil {
			return e
		}
		if e = taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_blockers SET resolved_at=$4,resolved_by=$5,resolution_comment=$6,resolved_operation_id=$7 WHERE project_id=$1 AND task_id=$2 AND id=$3 AND resolved_at IS NULL`, b.ProjectID.String(), b.TaskID.String(), b.ID.String(), b.ResolvedAt.Time(), actor, b.ResolutionComment, r.ID.String())); e != nil {
			return e
		}
	}
	next, e := taskCounter(p.QueryGeneration)
	if e != nil {
		return e
	}
	if e = taskAffected(x.Exec(ctx, `UPDATE agenteam_work.task_query_generations SET query_generation=$2 WHERE project_id=$1 AND query_generation=$3`, r.Project.String(), next, p.QueryGeneration)); e != nil {
		return e
	}
	h := p.TaskEvent
	actor, e := canonical(h.Actor)
	if e != nil {
		return e
	}
	var payload any = h.Payload.Added
	if h.Type == c.TaskBlockerEventResolved {
		payload = h.Payload.Resolved
	}
	raw, e := canonical(payload)
	if e != nil {
		return e
	}
	return taskAffected(x.Exec(ctx, `INSERT INTO agenteam_work.task_events(id,project_id,task_id,task_version,type,actor,operation_id,blocker_operation_id,correlation_id,payload,created_at) VALUES($1,$2,$3,$4,$5,$6,NULL,$7,$7,$8,$9)`, h.ID.String(), h.ProjectID.String(), h.TaskID.String(), int64(h.TaskVersion), string(h.Type), actor, h.OperationID.String(), raw, h.CreatedAt.Time()))
}
