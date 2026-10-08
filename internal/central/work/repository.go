// Package work owns only Work facts. Cross-domain authorization is exclusively
// through Project and Activity ports along the same caller-owned transaction.
package work

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"reflect"
	"slices"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
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

type Store interface {
	postgres.SQLExecutor
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	x := reflect.ValueOf(v)
	switch x.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return x.IsNil()
	}
	return false
}
func sameStore(a, b Store) bool {
	return !nilPort(a) && !nilPort(b) && reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.TypeOf(a).Comparable() && a == b
}
func fault(code f.Code) *f.Fault { return f.NewFault(code, f.NotStarted) }
func field(code f.Code, path, detail string) error {
	v := fault(code)
	v.FieldErrors = []f.FieldError{{Path: path, Code: detail}}
	return v
}
func unavailable(err error) error { return fault(f.DependencyUnavailable).WithCause(err) }
func portError(err error) error {
	if err == nil {
		return nil
	}
	var value *f.Fault
	if errors.As(err, &value) {
		return value
	}
	return unavailable(err)
}
func canceled(err error) error {
	return f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(err)
}
func internal(err error) error { return fault(f.InternalError).WithCause(err) }
func nextVersion(v f.Version) (f.Version, error) {
	if v.Validate() != nil || int64(v) == math.MaxInt64 {
		return 0, fault(f.InvalidState)
	}
	return v + 1, nil
}
func digest(raw []byte) f.Digest {
	h := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(h[:]))
}
func canonical(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, internal(err)
	}
	b, err := cursor.CanonicalJSON(raw)
	if err != nil {
		return nil, internal(err)
	}
	return b, nil
}
func sameValue(a, b any) bool {
	aa, err := canonical(a)
	if err != nil {
		return false
	}
	bb, err := canonical(b)
	return err == nil && bytes.Equal(aa, bb)
}
func userLock(id string, mode f.LockMode) f.LockRequest {
	k, _ := f.UserLock(id)
	return f.LockRequest{Key: k, Mode: mode}
}
func projectLock(id c.ProjectID, mode f.LockMode) f.LockRequest {
	k, _ := f.ProjectLock(id.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func commandLock(id f.CommandIdentity) f.LockRequest {
	k, _ := f.CommandLock(id)
	return f.LockRequest{Key: k, Mode: f.Exclusive}
}
func sprintLock(id string, mode f.LockMode) f.LockRequest {
	k, _ := f.AggregateLock(f.SprintAggregate, id)
	return f.LockRequest{Key: k, Mode: mode}
}
func rankLock(project c.ProjectID, parent string, mode f.LockMode) f.LockRequest {
	key := "work.milestone:" + project.String()
	if parent != "" {
		key = "work.sprint:" + project.String() + ":" + parent
	}
	k, _ := f.RankGroupLock(key)
	return f.LockRequest{Key: k, Mode: mode}
}
func commandCause(id f.CommandIdentity) f.TransactionCause { v, _ := f.NewCommandsCause(id); return v }
func readCause(name string) (f.TransactionCause, error) {
	id, err := f.NewID[struct{}]()
	if err != nil {
		return f.TransactionCause{}, unavailable(err)
	}
	v, err := f.NewRecoveryCause("work."+name, id.String(), "")
	return v, portError(err)
}
func dbNow(ctx context.Context, x postgres.SQLExecutor) (f.Instant, error) {
	var t time.Time
	if err := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t); err != nil {
		return f.Instant{}, unavailable(err)
	}
	v, err := f.NewInstant(t)
	return v, portError(err)
}
func txError(result f.CommitResult) error {
	if result.State() == f.Committed {
		return nil
	}
	if result.State() == f.Unknown {
		v := f.NewFault(f.CommitUnknown, f.Unknown)
		v.RetryHint = "lookup"
		if result.AttemptID().Validate() == nil {
			v.CauseID = result.AttemptID().String()
		}
		return v.WithCause(commitFailure{result})
	}
	if err := result.Fault(); err != nil {
		return err
	}
	return f.NewFault(f.InternalError, f.NotCommitted)
}

// Preserve the original physical attempt privately across all confirmation
// outcomes. Public errors expose only the safe attempt ID and retry semantics.
type commitFailure struct{ result f.CommitResult }

func (commitFailure) Error() string              { return string(f.CommitUnknown) }
func (commitFailure) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "work_commit_outcome") }
func (commitFailure) LogValue() slog.Value       { return slog.StringValue("work_commit_outcome") }

func notCommittedAfterUnknown(original f.CommitResult) error {
	v := f.NewFault(f.DependencyUnavailable, f.NotCommitted)
	v.RetryHint = "retry_same_key"
	if original.AttemptID().Validate() == nil {
		v.CauseID = original.AttemptID().String()
	}
	return v.WithCause(commitFailure{original})
}
func affected(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return internal(nil)
	}
	return nil
}
func isNotFound(err error) bool { var v *f.Fault; return errors.As(err, &v) && v.Code == f.NotFound }
func typedID[K any](s string) (f.ID[K], error) {
	v, err := f.ParseID[K](s)
	if err != nil {
		return v, internal(err)
	}
	return v, nil
}

const milestoneColumns = `id::text,project_id::text,title,description,manual_rank,version,created_at,updated_at`
const sprintColumns = `id::text,project_id::text,milestone_id::text,title,description,manual_rank,version,created_at,updated_at,started_at,started_by,completed_at,completed_by`

func scanMilestone(row interface{ Scan(...any) error }) (c.Milestone, error) {
	var v c.Milestone
	var id, project string
	var created, updated time.Time
	if err := row.Scan(&id, &project, &v.Title, &v.Description, &v.ManualRank, &v.Version, &created, &updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return v, fault(f.NotFound)
		}
		return v, unavailable(err)
	}
	var err error
	v.ID, err = typedID[c.Milestone](id)
	if err != nil {
		return c.Milestone{}, err
	}
	v.ProjectID, err = typedID[i.Project](project)
	if err != nil {
		return c.Milestone{}, err
	}
	v.CreatedAt, err = f.NewInstant(created)
	if err != nil {
		return c.Milestone{}, internal(err)
	}
	v.UpdatedAt, err = f.NewInstant(updated)
	if err != nil || v.Validate() != nil {
		return c.Milestone{}, internal(err)
	}
	return v, nil
}
func scanSprint(row interface{ Scan(...any) error }, pointer *pc.SprintID) (c.Sprint, error) {
	var v c.Sprint
	var id, project, parent string
	var created, updated time.Time
	var started, completed *time.Time
	var by, doneBy []byte
	if err := row.Scan(&id, &project, &parent, &v.Title, &v.Description, &v.ManualRank, &v.Version, &created, &updated, &started, &by, &completed, &doneBy); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return v, fault(f.NotFound)
		}
		return v, unavailable(err)
	}
	var err error
	v.ID, err = typedID[pc.Sprint](id)
	if err != nil {
		return c.Sprint{}, err
	}
	v.ProjectID, err = typedID[i.Project](project)
	if err != nil {
		return c.Sprint{}, err
	}
	v.MilestoneID, err = typedID[c.Milestone](parent)
	if err != nil {
		return c.Sprint{}, err
	}
	v.CreatedAt, err = f.NewInstant(created)
	if err != nil {
		return c.Sprint{}, internal(err)
	}
	v.UpdatedAt, err = f.NewInstant(updated)
	if err != nil {
		return c.Sprint{}, internal(err)
	}
	if started != nil {
		at, e := f.NewInstant(*started)
		if e != nil {
			return c.Sprint{}, internal(e)
		}
		v.StartedAt = &at
	}
	if completed != nil {
		at, e := f.NewInstant(*completed)
		if e != nil {
			return c.Sprint{}, internal(e)
		}
		v.CompletedAt = &at
	}
	if by != nil {
		var a c.ActorHistory
		if json.Unmarshal(by, &a) != nil {
			return c.Sprint{}, internal(nil)
		}
		v.StartedBy = &a
	}
	if doneBy != nil {
		var a c.ActorHistory
		if json.Unmarshal(doneBy, &a) != nil {
			return c.Sprint{}, internal(nil)
		}
		v.CompletedBy = &a
	}
	isPointer := pointer != nil && *pointer == v.ID
	switch {
	case v.StartedAt == nil && v.CompletedAt == nil && !isPointer:
		v.State = c.Planned
	case v.StartedAt != nil && v.CompletedAt == nil && isPointer:
		v.State = c.Current
	case v.StartedAt != nil && v.CompletedAt != nil && !isPointer:
		v.State = c.Completed
	default:
		return c.Sprint{}, internal(nil)
	}
	if v.Validate() != nil {
		return c.Sprint{}, internal(nil)
	}
	return v, nil
}
func loadMilestone(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, id c.MilestoneID) (c.Milestone, error) {
	return scanMilestone(x.QueryRow(ctx, `SELECT `+milestoneColumns+` FROM agenteam_work.milestones WHERE project_id=$1 AND id=$2`, project.String(), id.String()))
}
func loadSprint(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, id c.SprintID, pointer *pc.SprintID) (c.Sprint, error) {
	return scanSprint(x.QueryRow(ctx, `SELECT `+sprintColumns+` FROM agenteam_work.sprints WHERE project_id=$1 AND id=$2`, project.String(), id.String()), pointer)
}
func checkPointer(ctx context.Context, x postgres.SQLExecutor, ref pc.ProjectRef) error {
	if ref.CurrentSprintID == nil {
		return nil
	}
	v, err := loadSprint(ctx, x, ref.ID, *ref.CurrentSprintID, ref.CurrentSprintID)
	if err != nil {
		if isNotFound(err) {
			return internal(err)
		}
		return err
	}
	if v.State != c.Current {
		return internal(nil)
	}
	_, err = loadMilestone(ctx, x, ref.ID, v.MilestoneID)
	if isNotFound(err) {
		return internal(err)
	}
	return err
}

func loadGeneration(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, parent string) (f.Version, error) {
	var gen f.Version
	var err error
	if parent == "" {
		err = x.QueryRow(ctx, `SELECT order_generation FROM agenteam_work.milestone_order_groups WHERE project_id=$1`, project.String()).Scan(&gen)
	} else {
		err = x.QueryRow(ctx, `SELECT order_generation FROM agenteam_work.sprint_order_groups WHERE project_id=$1 AND milestone_id=$2`, project.String(), parent).Scan(&gen)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if parent != "" {
			return 0, internal(err)
		}
		var exists bool
		if e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_work.milestones WHERE project_id=$1)`, project.String()).Scan(&exists); e != nil {
			return 0, unavailable(e)
		}
		if exists {
			return 0, internal(nil)
		}
		return 1, nil
	}
	if err != nil {
		return 0, unavailable(err)
	}
	if gen.Validate() != nil {
		return 0, internal(nil)
	}
	return gen, nil
}
func loadRanks(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, parent string) ([]rankItem, error) {
	query := `SELECT id::text,manual_rank FROM agenteam_work.milestones WHERE project_id=$1 ORDER BY manual_rank,id LIMIT 4097`
	args := []any{project.String()}
	if parent != "" {
		query = `SELECT id::text,manual_rank FROM agenteam_work.sprints WHERE project_id=$1 AND milestone_id=$2 ORDER BY manual_rank,id LIMIT 4097`
		args = append(args, parent)
	}
	rows, err := x.Query(ctx, query, args...)
	if err != nil {
		return nil, unavailable(err)
	}
	defer rows.Close()
	out := []rankItem{}
	for rows.Next() {
		var row rankItem
		if rows.Scan(&row.ID, &row.Rank) != nil {
			return nil, internal(nil)
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, unavailable(err)
	}
	if len(out) > c.MaxGroupSize {
		return nil, internal(nil)
	}
	return out, nil
}

// No RequestID, Session, CSRF or diagnostics are stored in the business input.
type commandInput struct {
	Command          c.CommandName              `json:"command"`
	Project          c.ProjectID                `json:"project_id"`
	Target           string                     `json:"target_id"`
	User             i.UserID                   `json:"actor_user_id"`
	Expected         *f.Version                 `json:"expected_version"`
	CreateMilestone  *c.CreateMilestoneRequest  `json:"create_milestone"`
	CreateSprint     *c.CreateSprintRequest     `json:"create_sprint"`
	Update           *c.UpdateFields            `json:"update"`
	ReorderMilestone *c.ReorderMilestoneRequest `json:"reorder_milestone"`
	ReorderSprint    *c.ReorderSprintRequest    `json:"reorder_sprint"`
}

func (in commandInput) semantic(actor i.Actor, key f.IdempotencyKey) (f.Digest, error) {
	if in.Project.Validate() != nil || in.User.Validate() != nil || in.User.String() != actor.Details().UserID || in.Command.Validate() != nil {
		return "", fault(f.InvalidArgument)
	}
	count := 0
	for _, present := range []bool{in.CreateMilestone != nil, in.CreateSprint != nil, in.Update != nil, in.ReorderMilestone != nil, in.ReorderSprint != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return "", fault(f.InvalidArgument)
	}
	id, err := f.ParseID[f.Request](in.Target)
	if err != nil {
		return "", fault(f.InvalidArgument)
	}
	meta := f.CommandMeta{RequestID: id, IdempotencyKey: key, ExpectedVersion: in.Expected}
	switch in.Command {
	case c.MilestoneCreate:
		if in.CreateMilestone != nil && in.CreateMilestone.MilestoneID.String() == in.Target {
			return c.CreateMilestoneDigest(actor, meta, in.Project, *in.CreateMilestone)
		}
	case c.SprintCreate:
		if in.CreateSprint != nil && in.CreateSprint.SprintID.String() == in.Target {
			return c.CreateSprintDigest(actor, meta, in.Project, *in.CreateSprint)
		}
	case c.MilestoneUpdate:
		if in.Update != nil {
			v, e := f.ParseID[c.Milestone](in.Target)
			if e == nil {
				return c.UpdateMilestoneDigest(actor, meta, in.Project, v, *in.Update)
			}
		}
	case c.SprintUpdate:
		if in.Update != nil {
			v, e := f.ParseID[pc.Sprint](in.Target)
			if e == nil {
				return c.UpdateSprintDigest(actor, meta, in.Project, v, *in.Update)
			}
		}
	case c.MilestoneReorder:
		if in.ReorderMilestone != nil {
			v, e := f.ParseID[c.Milestone](in.Target)
			if e == nil {
				return c.ReorderMilestoneDigest(actor, meta, in.Project, v, *in.ReorderMilestone)
			}
		}
	case c.SprintReorder:
		if in.ReorderSprint != nil {
			v, e := f.ParseID[pc.Sprint](in.Target)
			if e == nil {
				return c.ReorderSprintDigest(actor, meta, in.Project, v, *in.ReorderSprint)
			}
		}
	}
	return "", fault(f.InvalidArgument)
}
func (in commandInput) parent() string {
	if in.CreateSprint != nil {
		return in.CreateSprint.MilestoneID.String()
	}
	if in.ReorderSprint != nil {
		return in.ReorderSprint.MilestoneID.String()
	}
	return ""
}
func (in commandInput) before() string {
	if in.ReorderMilestone != nil && in.ReorderMilestone.BeforeID != nil {
		return in.ReorderMilestone.BeforeID.String()
	}
	if in.ReorderSprint != nil && in.ReorderSprint.BeforeID != nil {
		return in.ReorderSprint.BeforeID.String()
	}
	return ""
}
func (in commandInput) ordered() bool {
	return in.Command.IsCreate() || in.Command == c.MilestoneReorder || in.Command == c.SprintReorder
}
func (in commandInput) identity(key f.IdempotencyKey) (f.CommandIdentity, error) {
	return c.Identity(in.Project, in.Command, key)
}
func (in commandInput) locks(key f.IdempotencyKey) ([]f.LockRequest, error) {
	id, err := in.identity(key)
	if err != nil {
		return nil, err
	}
	locks := []f.LockRequest{commandLock(id), userLock(in.User.String(), f.Exclusive), projectLock(in.Project, f.Exclusive)}
	if in.ordered() {
		locks = append(locks, rankLock(in.Project, in.parent(), f.Exclusive))
	}
	if in.Command.IsSprint() {
		locks = append(locks, sprintLock(in.Target, f.Exclusive))
	}
	return oc.NormalizeLocks(locks)
}

type structurePlan struct {
	BeforeMilestone *c.Milestone        `json:"before_milestone"`
	BeforeSprint    *c.Sprint           `json:"before_sprint"`
	After           c.StructureMutation `json:"after"`
	GroupBefore     *f.Version          `json:"group_before"`
	Header          event.Header        `json:"header"`
	Payload         json.RawMessage     `json:"payload"`
}
type commandRecord struct {
	ID        c.CommandID
	Project   c.ProjectID
	User      i.UserID
	Command   c.CommandName
	Key       f.IdempotencyKey
	Semantic  f.Digest
	Input     commandInput
	Revision  f.Version
	State     string
	Plan      *structurePlan
	EventID   *event.EventID
	Receipt   *c.StructureMutation
	Created   f.Instant
	Committed *f.Instant
}

func (v commandInput) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "work_command_input") }
func (v commandInput) LogValue() slog.Value        { return slog.StringValue("work_command_input") }
func (v structurePlan) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "work_structure_plan") }
func (v structurePlan) LogValue() slog.Value       { return slog.StringValue("work_structure_plan") }
func (v commandRecord) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "work_command_record") }
func (v commandRecord) LogValue() slog.Value       { return slog.StringValue("work_command_record") }

func decodePrivate[T any](raw []byte, limit int, fields []string) (T, error) {
	var value T
	if len(raw) == 0 || len(raw) > limit {
		return value, internal(nil)
	}
	if _, err := cursor.CanonicalJSON(raw); err != nil {
		return value, internal(err)
	}
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil || len(shape) != len(fields) {
		return value, internal(nil)
	}
	for key := range shape {
		if !slices.Contains(fields, key) {
			return value, internal(nil)
		}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(&value); err != nil {
		return value, internal(err)
	}
	return value, nil
}

const commandColumns = `id::text,project_id::text,actor_user_id::text,command_name,idempotency_key,semantic_digest,request,plan_revision,state,plan,event_id::text,receipt,created_at,committed_at`

func scanCommand(row interface{ Scan(...any) error }) (*commandRecord, error) {
	var v commandRecord
	var id, project, user string
	var request, plan, receipt []byte
	var eventID *string
	var created time.Time
	var committed *time.Time
	if err := row.Scan(&id, &project, &user, &v.Command, &v.Key, &v.Semantic, &request, &v.Revision, &v.State, &plan, &eventID, &receipt, &created, &committed); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, unavailable(err)
	}
	var err error
	v.ID, err = typedID[c.StructureCommand](id)
	if err != nil {
		return nil, err
	}
	v.Project, err = typedID[i.Project](project)
	if err != nil {
		return nil, err
	}
	v.User, err = typedID[i.User](user)
	if err != nil {
		return nil, err
	}
	v.Created, err = f.NewInstant(created)
	if err != nil {
		return nil, internal(err)
	}
	if committed != nil {
		t, e := f.NewInstant(*committed)
		if e != nil {
			return nil, internal(e)
		}
		v.Committed = &t
	}
	v.Input, err = decodePrivate[commandInput](request, c.MaxRequestBytes, []string{"command", "project_id", "target_id", "actor_user_id", "expected_version", "create_milestone", "create_sprint", "update", "reorder_milestone", "reorder_sprint"})
	if err != nil {
		return nil, err
	}
	if plan != nil {
		p, e := decodePrivate[structurePlan](plan, 1<<20, []string{"before_milestone", "before_sprint", "after", "group_before", "header", "payload"})
		if e != nil {
			return nil, e
		}
		var shape map[string]json.RawMessage
		if json.Unmarshal(plan, &shape) != nil {
			return nil, internal(nil)
		}
		p.Header, e = event.DecodeHeader(shape["header"])
		if e != nil {
			return nil, internal(e)
		}
		v.Plan = &p
	}
	if eventID != nil {
		id, e := typedID[event.EventIdentity](*eventID)
		if e != nil {
			return nil, e
		}
		v.EventID = &id
	}
	if receipt != nil {
		var r c.StructureMutation
		if json.Unmarshal(receipt, &r) != nil {
			return nil, internal(nil)
		}
		v.Receipt = &r
	}
	if v.Command.Validate() != nil || v.Key.Validate() != nil || v.Semantic.Validate() != nil || v.Revision.Validate() != nil || v.Input.Command != v.Command || v.Input.Project != v.Project || v.Input.User != v.User || (v.Plan == nil) != (v.EventID == nil) {
		return nil, internal(nil)
	}
	if v.State == "planned" {
		if v.Plan == nil || v.Receipt != nil || v.Committed != nil {
			return nil, internal(nil)
		}
	} else if v.State == "completed" {
		if v.Receipt == nil || v.Committed == nil || v.Committed.Time().Before(v.Created.Time()) || v.Receipt.Changed != (v.Plan != nil) {
			return nil, internal(nil)
		}
	} else {
		return nil, internal(nil)
	}
	return &v, nil
}
func loadCommand(ctx context.Context, x postgres.SQLExecutor, id f.CommandIdentity) (*commandRecord, error) {
	return scanCommand(x.QueryRow(ctx, `SELECT `+commandColumns+` FROM agenteam_work.structure_commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, id.OwnerIDs()[0], id.Command(), string(id.Key())))
}
func loadEventCommand(ctx context.Context, x postgres.SQLExecutor, id event.EventID) (*commandRecord, error) {
	return scanCommand(x.QueryRow(ctx, `SELECT `+commandColumns+` FROM agenteam_work.structure_commands WHERE event_id=$1`, id.String()))
}
func storePlan(ctx context.Context, x postgres.SQLExecutor, record *commandRecord, insert bool) error {
	input, err := canonical(record.Input)
	if err != nil || len(input) > c.MaxRequestBytes {
		return internal(err)
	}
	plan, err := canonical(record.Plan)
	if err != nil || len(plan) > 1<<20 {
		return internal(err)
	}
	if insert {
		return affected(x.Exec(ctx, `INSERT INTO agenteam_work.structure_commands(id,project_id,actor_user_id,command_name,idempotency_key,semantic_digest,request,plan_revision,state,plan,event_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'planned',$9,$10,$11)`, record.ID.String(), record.Project.String(), record.User.String(), string(record.Command), string(record.Key), string(record.Semantic), input, int64(record.Revision), plan, record.EventID.String(), record.Created.Time()))
	}
	return affected(x.Exec(ctx, `UPDATE agenteam_work.structure_commands SET plan_revision=$2,plan=$3,event_id=$4 WHERE id=$1 AND state='planned' AND plan_revision=$5`, record.ID.String(), int64(record.Revision), plan, record.EventID.String(), int64(record.Revision-1)))
}
func completeCommand(ctx context.Context, x postgres.SQLExecutor, record *commandRecord, result c.StructureMutation, insert bool) error {
	receipt, err := canonical(result)
	if err != nil || len(receipt) > c.MaxRequestBytes {
		return internal(err)
	}
	now, err := dbNow(ctx, x)
	if err != nil {
		return err
	}
	if insert {
		input, e := canonical(record.Input)
		if e != nil || len(input) > c.MaxRequestBytes {
			return internal(e)
		}
		return affected(x.Exec(ctx, `INSERT INTO agenteam_work.structure_commands(id,project_id,actor_user_id,command_name,idempotency_key,semantic_digest,request,plan_revision,state,receipt,created_at,committed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'completed',$9,$10,$11)`, record.ID.String(), record.Project.String(), record.User.String(), string(record.Command), string(record.Key), string(record.Semantic), input, int64(record.Revision), receipt, record.Created.Time(), now.Time()))
	}
	if result.Changed {
		return affected(x.Exec(ctx, `UPDATE agenteam_work.structure_commands SET state='completed',receipt=$2,committed_at=$3 WHERE id=$1 AND state='planned' AND plan_revision=$4`, record.ID.String(), receipt, now.Time(), int64(record.Revision)))
	}
	return affected(x.Exec(ctx, `UPDATE agenteam_work.structure_commands SET state='completed',receipt=$2,committed_at=$3,plan=NULL,event_id=NULL WHERE id=$1 AND state='planned' AND plan_revision=$4`, record.ID.String(), receipt, now.Time(), int64(record.Revision)))
}
