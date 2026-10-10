package work

import (
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
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/work/contract"
	"github.com/jackc/pgx/v5"
)

type sprintStartInput struct {
	Project  c.ProjectID `json:"project_id"`
	Sprint   c.SprintID  `json:"sprint_id"`
	User     i.UserID    `json:"actor_user_id"`
	Expected f.Version   `json:"expected_version"`
}

func (v sprintStartInput) semantic(actor i.Actor, key f.IdempotencyKey) (f.Digest, error) {
	if actor.Details().UserID != v.User.String() {
		return "", fault(f.Forbidden)
	}
	rid, err := f.ParseID[f.Request](v.Sprint.String())
	if err != nil {
		return "", fault(f.InvalidArgument)
	}
	return c.StartSprintDigest(actor, f.CommandMeta{RequestID: rid, IdempotencyKey: key, ExpectedVersion: &v.Expected}, v.Project, v.Sprint)
}
func (v sprintStartInput) locks(key f.IdempotencyKey) ([]f.LockRequest, error) {
	id, err := c.SprintStartIdentity(v.Project, key)
	if err != nil {
		return nil, err
	}
	return oc.NormalizeLocks([]f.LockRequest{commandLock(id), userLock(v.User.String(), f.Exclusive), projectLock(v.Project, f.Exclusive), taskScheduleLock(v.Project, f.Exclusive), sprintLock(v.Sprint.String(), f.Exclusive)})
}

type sprintStartPlan struct {
	Before  c.Sprint              `json:"before"`
	Project pc.ProjectRef         `json:"project"`
	After   c.SprintStartMutation `json:"after"`
	Header  event.Header          `json:"header"`
	Payload json.RawMessage       `json:"payload"`
}
type sprintStartRecord struct {
	ID        c.SprintStartCommandID
	Input     sprintStartInput
	Key       f.IdempotencyKey
	Semantic  f.Digest
	Revision  f.Version
	State     string
	Plan      sprintStartPlan
	Receipt   *c.SprintStartMutation
	Created   f.Instant
	Committed *f.Instant
}

func (sprintStartInput) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "sprint_start_input") }
func (sprintStartInput) LogValue() slog.Value        { return slog.StringValue("sprint_start_input") }
func (sprintStartPlan) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "sprint_start_plan") }
func (sprintStartPlan) LogValue() slog.Value         { return slog.StringValue("sprint_start_plan") }
func (sprintStartRecord) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "sprint_start_record") }
func (sprintStartRecord) LogValue() slog.Value       { return slog.StringValue("sprint_start_record") }

// deriveSprintStart is also used when verifying a persisted plan. No task,
// scheduler-enabled, ordering or rank condition participates in Start.
func deriveSprintStart(in sprintStartInput, b c.Sprint, p pc.ProjectRef, at f.Instant, ev event.EventID) (c.SprintStartMutation, error) {
	if b.Validate() != nil || p.Validate() != nil || b.ProjectID != in.Project || b.ID != in.Sprint || p.ID != in.Project || in.User.Validate() != nil || p.OwnerUserID != in.User {
		return c.SprintStartMutation{}, internal(nil)
	}
	if b.Version != in.Expected {
		return c.SprintStartMutation{}, field(f.VersionConflict, "/expected_version", "STALE_VERSION")
	}
	if b.State != c.Planned || p.CurrentSprintID != nil || p.Lifecycle != pc.Active {
		return c.SprintStartMutation{}, fault(f.InvalidState)
	}
	if at.Validate() != nil || at.Time().Before(b.UpdatedAt.Time()) || at.Time().Before(p.UpdatedAt.Time()) {
		return c.SprintStartMutation{}, internal(nil)
	}
	var err error
	b = b.Clone()
	p = p.Clone()
	b.Version, err = nextVersion(b.Version)
	if err != nil {
		return c.SprintStartMutation{}, err
	}
	p.Version, err = nextVersion(p.Version)
	if err != nil {
		return c.SprintStartMutation{}, err
	}
	history := c.ActorHistory{Kind: i.Human, UserID: in.User.String()}
	b.State = c.Current
	b.StartedAt = &at
	b.StartedBy = &history
	b.UpdatedAt = at
	target := in.Sprint
	p.CurrentSprintID = &target
	p.UpdatedAt = at
	out := c.SprintStartMutation{Sprint: b, Project: p, EventID: ev}
	if err = out.Validate(); err != nil {
		return c.SprintStartMutation{}, internal(err)
	}
	return out, nil
}
func sprintStartHeader(result c.SprintStartMutation) (event.Header, error) {
	p, err := f.ParseID[event.Project](result.Project.ID.String())
	if err != nil {
		return event.Header{}, err
	}
	target, err := f.ParseID[event.Aggregate](result.Sprint.ID.String())
	if err != nil {
		return event.Header{}, err
	}
	version := result.Sprint.Version
	return event.Header{EventID: result.EventID, EventType: c.SprintStartedName, SchemaVersion: c.SprintLifecycleSchemaVersion, OccurredAt: result.Sprint.UpdatedAt, Scope: event.Scope{Kind: event.ProjectScope, ProjectID: p}, AggregateType: c.SprintAggregate, AggregateID: target, AggregateVersion: &version}, nil
}
func validateSprintStartRecord(r *sprintStartRecord, actor i.Actor) error {
	if r == nil || r.ID.Validate() != nil || r.Revision.Validate() != nil || r.Created.Validate() != nil {
		return internal(nil)
	}
	semantic, err := r.Input.semantic(actor, r.Key)
	if err != nil || semantic != r.Semantic {
		return internal(err)
	}
	p := r.Plan
	expected, err := deriveSprintStart(r.Input, p.Before, p.Project, p.Header.OccurredAt, p.After.EventID)
	if err != nil || !sameValue(expected, p.After) || p.Header.OccurredAt.Time().Before(r.Created.Time()) {
		return internal(err)
	}
	header, err := sprintStartHeader(expected)
	if err != nil || !sameValue(header, p.Header) {
		return internal(err)
	}
	var payload c.SprintStarted
	if json.Unmarshal(p.Payload, &payload) != nil || payload.CommandID != r.ID || payload.ActorUserID != r.Input.User || payload.MilestoneID != p.Before.MilestoneID || payload.ProjectVersion != p.After.Project.Version {
		return internal(nil)
	}
	switch r.State {
	case "planned":
		if r.Receipt != nil || r.Committed != nil {
			return internal(nil)
		}
	case "completed":
		if r.Receipt == nil || r.Committed == nil || r.Committed.Time().Before(r.Created.Time()) || !sameValue(*r.Receipt, p.After) {
			return internal(nil)
		}
	default:
		return internal(nil)
	}
	return nil
}

const sprintStartColumns = `id::text,project_id::text,sprint_id::text,actor_user_id::text,idempotency_key,semantic_digest,expected_version,plan_revision,state,plan,event_id::text,receipt,created_at,committed_at`

func scanSprintStart(row interface{ Scan(...any) error }) (*sprintStartRecord, error) {
	var r sprintStartRecord
	var id, p, s, u, ev string
	var plan, receipt []byte
	var at time.Time
	var done *time.Time
	if err := row.Scan(&id, &p, &s, &u, &r.Key, &r.Semantic, &r.Input.Expected, &r.Revision, &r.State, &plan, &ev, &receipt, &at, &done); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, unavailable(err)
	}
	var err error
	if r.ID, err = f.ParseID[c.SprintStartCommand](id); err != nil {
		return nil, internal(err)
	}
	if r.Input.Project, err = f.ParseID[i.Project](p); err != nil {
		return nil, internal(err)
	}
	if r.Input.Sprint, err = f.ParseID[pc.Sprint](s); err != nil {
		return nil, internal(err)
	}
	if r.Input.User, err = f.ParseID[i.User](u); err != nil {
		return nil, internal(err)
	}
	if r.Created, err = f.NewInstant(at); err != nil {
		return nil, internal(err)
	}
	if done != nil {
		v, e := f.NewInstant(*done)
		if e != nil {
			return nil, internal(e)
		}
		r.Committed = &v
	}
	r.Plan, err = decodePrivate[sprintStartPlan](plan, 1<<20, []string{"before", "project", "after", "header", "payload"})
	if err != nil {
		return nil, err
	}
	if r.Key.Validate() != nil || r.Semantic.Validate() != nil || r.Input.Expected.Validate() != nil || r.Revision.Validate() != nil || r.Plan.After.EventID.String() != ev {
		return nil, internal(nil)
	}
	if receipt != nil {
		var v c.SprintStartMutation
		if len(receipt) > c.MaxRequestBytes || json.Unmarshal(receipt, &v) != nil {
			return nil, internal(nil)
		}
		r.Receipt = &v
	}
	return &r, nil
}
func loadSprintStart(ctx context.Context, x postgres.SQLExecutor, p c.ProjectID, key f.IdempotencyKey) (*sprintStartRecord, error) {
	return scanSprintStart(x.QueryRow(ctx, `SELECT `+sprintStartColumns+` FROM agenteam_work.sprint_start_commands WHERE project_id=$1 AND idempotency_key=$2`, p.String(), string(key)))
}
func loadSprintStartEvent(ctx context.Context, x postgres.SQLExecutor, id event.EventID) (*sprintStartRecord, error) {
	return scanSprintStart(x.QueryRow(ctx, `SELECT `+sprintStartColumns+` FROM agenteam_work.sprint_start_commands WHERE event_id=$1`, id.String()))
}
func saveSprintStartPlan(ctx context.Context, x postgres.SQLExecutor, r *sprintStartRecord, insert bool) error {
	raw, err := canonical(r.Plan)
	if err != nil {
		return err
	}
	if insert {
		return affected(x.Exec(ctx, `INSERT INTO agenteam_work.sprint_start_commands(id,project_id,sprint_id,actor_user_id,idempotency_key,semantic_digest,expected_version,plan_revision,state,plan,event_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'planned',$9,$10,$11)`, r.ID.String(), r.Input.Project.String(), r.Input.Sprint.String(), r.Input.User.String(), string(r.Key), string(r.Semantic), int64(r.Input.Expected), int64(r.Revision), raw, r.Plan.After.EventID.String(), r.Created.Time()))
	}
	return affected(x.Exec(ctx, `UPDATE agenteam_work.sprint_start_commands SET plan_revision=$2,plan=$3,event_id=$4 WHERE id=$1 AND state='planned' AND plan_revision=$5`, r.ID.String(), int64(r.Revision), raw, r.Plan.After.EventID.String(), int64(r.Revision-1)))
}
func completeSprintStart(ctx context.Context, x postgres.SQLExecutor, r *sprintStartRecord) error {
	raw, err := canonical(r.Plan.After)
	if err != nil {
		return err
	}
	at, err := dbNow(ctx, x)
	if err != nil {
		return err
	}
	return affected(x.Exec(ctx, `UPDATE agenteam_work.sprint_start_commands SET state='completed',receipt=$2,committed_at=$3 WHERE id=$1 AND state='planned' AND plan_revision=$4`, r.ID.String(), raw, at.Time(), int64(r.Revision)))
}
func sprintStartChange(r *sprintStartRecord) (pc.SprintStartChange, error) {
	id, err := c.SprintStartIdentity(r.Input.Project, r.Key)
	if err != nil {
		return pc.SprintStartChange{}, err
	}
	v := pc.SprintStartChange{Command: id, CommandID: r.ID.String(), PlanRevision: r.Revision, SprintID: r.Input.Sprint, SprintVersion: r.Plan.After.Sprint.Version, StartedAt: r.Plan.Header.OccurredAt, Before: r.Plan.Project.Clone(), After: r.Plan.After.Project.Clone()}
	return v, v.Validate()
}
