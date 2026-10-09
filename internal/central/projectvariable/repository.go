// Package projectvariable owns ordinary project configuration, not Secrets.
package projectvariable

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"log/slog"
	"math"
	"reflect"
	"time"
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

// SQL diagnostics may contain the bound ordinary value or a conflicting name.
// Retain them privately without publishing an unwrap/formatting path.
type privateFailure struct{ value func() error }

func (privateFailure) Error() string { return "project_variable_storage" }
func unavailable(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		switch pg.ConstraintName {
		case "variables_live_name":
			return field(f.ResourceBusy, "/request/name", "NAME_CONFLICT")
		case "variables_pkey":
			return field(f.ResourceBusy, "/variable_id", "ID_CONFLICT")
		}
	}
	if err == nil {
		return fault(f.DependencyUnavailable)
	}
	return fault(f.DependencyUnavailable).WithCause(privateFailure{func() error { return err }})
}

// Different Projects may concurrently reserve the same caller-generated ID.
// Same-Project writers are serialized and classified by checkPreimage above.
// Keep this fallback local to INSERT; other statements retain their own errors.
func createInsertFailure(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "variables_pkey" {
		return fault(f.NotFound)
	}
	return unavailable(err)
}
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
func internal(err error) error {
	if err == nil {
		return fault(f.InternalError)
	}
	return fault(f.InternalError).WithCause(privateFailure{func() error { return err }})
}
func nextVersion(v f.Version) (f.Version, error) {
	if v.Validate() != nil || int64(v) == math.MaxInt64 {
		return 0, field(f.ResourceBusy, "/expected_version", "VERSION_EXHAUSTED")
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

func commandCause(id f.CommandIdentity) f.TransactionCause { v, _ := f.NewCommandsCause(id); return v }
func readCause(name string) (f.TransactionCause, error) {
	id, err := f.NewID[struct{}]()
	if err != nil {
		return f.TransactionCause{}, unavailable(err)
	}
	v, err := f.NewRecoveryCause("projectvariable."+name, id.String(), "")
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

func (commitFailure) Error() string { return string(f.CommitUnknown) }
func (commitFailure) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_variable_commit_outcome")
}
func (commitFailure) LogValue() slog.Value {
	return slog.StringValue("project_variable_commit_outcome")
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

const variableColumns = `id::text,project_id::text,type,name,description,value,version,created_at,updated_at,deleted_at`

func scanVariable(row interface{ Scan(...any) error }) (c.Variable, *f.Instant, error) {
	var id, project, typ, name, description, value string
	var version int64
	var created, updated time.Time
	var deleted *time.Time
	if e := row.Scan(&id, &project, &typ, &name, &description, &value, &version, &created, &updated, &deleted); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return c.Variable{}, nil, fault(f.NotFound)
		}
		return c.Variable{}, nil, unavailable(e)
	}
	v := c.VariableFields{Type: typ, Name: name, Description: description, Value: value, Version: f.Version(version)}
	var e error
	if v.ID, e = f.ParseID[i.ProjectVariable](id); e != nil {
		return c.Variable{}, nil, internal(e)
	}
	if v.ProjectID, e = f.ParseID[i.Project](project); e != nil {
		return c.Variable{}, nil, internal(e)
	}
	if v.CreatedAt, e = f.NewInstant(created); e != nil {
		return c.Variable{}, nil, internal(e)
	}
	if v.UpdatedAt, e = f.NewInstant(updated); e != nil {
		return c.Variable{}, nil, internal(e)
	}
	result, e := c.NewVariable(v)
	if e != nil {
		return c.Variable{}, nil, internal(e)
	}
	if deleted == nil {
		return result, nil, nil
	}
	at, e := f.NewInstant(*deleted)
	if e != nil || version < 2 || !at.Time().Equal(updated) || value != "" || description != "" {
		return c.Variable{}, nil, internal(e)
	}
	return result, &at, nil
}
func loadVariable(ctx context.Context, x postgres.SQLExecutor, p c.ProjectID, id c.VariableID, deleted bool) (c.Variable, *f.Instant, error) {
	v, at, e := scanVariable(x.QueryRow(ctx, `SELECT `+variableColumns+` FROM agenteam_projectvariable.variables WHERE project_id=$1 AND id=$2`, p.String(), id.String()))
	if e == nil && at != nil && !deleted {
		return c.Variable{}, nil, fault(f.NotFound)
	}
	return v, at, e
}
func generation(ctx context.Context, x postgres.SQLExecutor, p c.ProjectID) (int64, error) {
	var n int64
	if e := x.QueryRow(ctx, `SELECT COALESCE((SELECT query_generation FROM agenteam_projectvariable.project_generations WHERE project_id=$1),1)`, p.String()).Scan(&n); e != nil {
		return 0, unavailable(e)
	}
	if n < 1 {
		return 0, internal(nil)
	}
	return n, nil
}

// These records are private persistence projections. Neither user text nor keys
// participate in diagnostic formatting; public DTOs additionally hide storage.
type commandInput struct {
	Project  c.ProjectID       `json:"project_id"`
	User     i.UserID          `json:"actor_user_id"`
	Command  c.CommandName     `json:"command"`
	Target   c.VariableID      `json:"target_id"`
	Expected *f.Version        `json:"expected_version,omitempty"`
	Create   *c.VariableCreate `json:"create,omitempty"`
	Update   *c.VariableUpdate `json:"update,omitempty"`
}

func (in commandInput) request() any {
	if in.Create != nil {
		return *in.Create
	}
	if in.Update != nil {
		return *in.Update
	}
	return nil
}
func (in commandInput) semantic(a i.Actor, key f.IdempotencyKey) (f.Digest, error) {
	requestID, _ := f.ParseID[f.Request](in.Target.String())
	return c.VariableCommandDigest(a, f.CommandMeta{RequestID: requestID, IdempotencyKey: key, ExpectedVersion: in.Expected}, in.Project, in.Target, in.Command, in.request())
}
func (in commandInput) locks(key f.IdempotencyKey) ([]f.LockRequest, error) {
	id, e := c.VariableCommandIdentity(in.Project, in.Command, key)
	if e != nil {
		return nil, e
	}
	return oc.NormalizeLocks([]f.LockRequest{commandLock(id), userLock(in.User.String(), f.Exclusive), projectLock(in.Project, f.Exclusive)})
}

type commandPlan struct {
	Before    *c.Variable   `json:"before"`
	After     c.Variable    `json:"after"`
	Deleted   bool          `json:"deleted"`
	Fields    []string      `json:"changed_fields"`
	HistoryID c.OperationID `json:"history_id"`
}
type commandRecord struct {
	ID        c.OperationID
	Project   c.ProjectID
	User      i.UserID
	Command   c.CommandName
	Key       f.IdempotencyKey
	Semantic  f.Digest
	Target    c.VariableID
	Input     commandInput
	State     string
	Revision  f.Version
	Plan      *commandPlan
	EventID   *event.EventID
	Receipt   *c.VariableMutation
	Created   f.Instant
	Committed *f.Instant
}

func (commandInput) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "project_variable_intent") }
func (commandPlan) Format(w fmt.State, _ rune)  { _, _ = io.WriteString(w, "project_variable_plan") }
func (commandRecord) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "project_variable_command")
}
func privateDecode(raw []byte, out any, limit int, required, optional []string) error {
	if len(raw) == 0 || len(raw) > limit {
		return internal(nil)
	}
	if _, e := cursor.CanonicalJSON(raw); e != nil {
		return internal(nil)
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return internal(nil)
	}
	allowed := map[string]bool{}
	for _, k := range required {
		allowed[k] = true
		if _, ok := m[k]; !ok {
			return internal(nil)
		}
	}
	for _, k := range optional {
		allowed[k] = true
	}
	for k := range m {
		if !allowed[k] {
			return internal(nil)
		}
	}
	if e := json.Unmarshal(raw, out); e != nil {
		return internal(nil)
	}
	return nil
}
func (in *commandInput) UnmarshalJSON(raw []byte) error {
	type wire commandInput
	var n wire
	if e := privateDecode(raw, &n, c.MaxRequestBytes, []string{"project_id", "actor_user_id", "command", "target_id"}, []string{"expected_version", "create", "update"}); e != nil {
		return e
	}
	if n.Project.Validate() != nil || n.User.Validate() != nil || n.Target.Validate() != nil || n.Command.Validate() != nil {
		return internal(nil)
	}
	if n.Command == c.CreateCommand {
		if n.Expected != nil || n.Create == nil || n.Create.Validate() != nil || n.Create.Fields().ID != n.Target || n.Update != nil {
			return internal(nil)
		}
	} else {
		if n.Expected == nil || n.Expected.Validate() != nil || n.Create != nil {
			return internal(nil)
		}
		if n.Command == c.UpdateCommand {
			if n.Update == nil || n.Update.Validate() != nil {
				return internal(nil)
			}
		} else if n.Update != nil {
			return internal(nil)
		}
	}
	*in = commandInput(n)
	return nil
}
func (p *commandPlan) UnmarshalJSON(raw []byte) error {
	type wire commandPlan
	var n wire
	if e := privateDecode(raw, &n, 1<<20, []string{"before", "after", "deleted", "changed_fields", "history_id"}, nil); e != nil {
		return e
	}
	if n.After.Validate() != nil || n.HistoryID.Validate() != nil || n.Before != nil && n.Before.Validate() != nil {
		return internal(nil)
	}
	*p = commandPlan(n)
	return nil
}

const commandColumns = `id::text,project_id::text,actor_user_id::text,command_name,idempotency_key,semantic_digest,target_id::text,request,state,plan_revision,plan,event_id::text,receipt,created_at,committed_at`

func scanCommand(row interface{ Scan(...any) error }) (*commandRecord, error) {
	var id, p, u, target, name, key, semantic, state string
	var input, plan, receipt []byte
	var eventID *string
	var revision int64
	var created time.Time
	var committed *time.Time
	if e := row.Scan(&id, &p, &u, &name, &key, &semantic, &target, &input, &state, &revision, &plan, &eventID, &receipt, &created, &committed); e != nil {
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, unavailable(e)
	}
	r := &commandRecord{Command: c.CommandName(name), Key: f.IdempotencyKey(key), Semantic: f.Digest(semantic), State: state, Revision: f.Version(revision)}
	var e error
	if r.ID, e = f.ParseID[c.Operation](id); e != nil {
		return nil, internal(e)
	}
	if r.Project, e = f.ParseID[i.Project](p); e != nil {
		return nil, internal(e)
	}
	if r.User, e = f.ParseID[i.User](u); e != nil {
		return nil, internal(e)
	}
	if r.Target, e = f.ParseID[i.ProjectVariable](target); e != nil {
		return nil, internal(e)
	}
	if r.Created, e = f.NewInstant(created); e != nil {
		return nil, internal(e)
	}
	if committed != nil {
		at, e := f.NewInstant(*committed)
		if e != nil {
			return nil, internal(e)
		}
		r.Committed = &at
	}
	if e = r.Input.UnmarshalJSON(input); e != nil {
		return nil, e
	}
	if plan != nil {
		r.Plan = &commandPlan{}
		if e = r.Plan.UnmarshalJSON(plan); e != nil {
			return nil, e
		}
	}
	if eventID != nil {
		id, e := f.ParseID[event.EventIdentity](*eventID)
		if e != nil {
			return nil, internal(e)
		}
		r.EventID = &id
	}
	if receipt != nil {
		r.Receipt = &c.VariableMutation{}
		if e = r.Receipt.UnmarshalJSON(receipt); e != nil {
			return nil, internal(e)
		}
	}
	if r.Command.Validate() != nil || r.Key.Validate() != nil || r.Semantic.Validate() != nil || r.Revision.Validate() != nil || r.Input.Project != r.Project || r.Input.User != r.User || r.Input.Command != r.Command || r.Input.Target != r.Target {
		return nil, internal(nil)
	}
	if state == "planned" {
		if r.Plan == nil || r.EventID == nil || r.Receipt != nil || r.Committed != nil {
			return nil, internal(nil)
		}
	} else if state == "completed" {
		if r.Receipt == nil || r.Committed == nil || r.Committed.Time().Before(r.Created.Time()) {
			return nil, internal(nil)
		}
		fields := r.Receipt.Fields()
		if fields.Command != r.Command || fields.Changed != (r.Plan != nil) || fields.Changed != (r.EventID != nil) {
			return nil, internal(nil)
		}
		if fields.Changed && *fields.EventID != *r.EventID {
			return nil, internal(nil)
		}
	} else {
		return nil, internal(nil)
	}
	return r, nil
}
func loadCommand(ctx context.Context, x postgres.SQLExecutor, p c.ProjectID, name c.CommandName, key f.IdempotencyKey) (*commandRecord, error) {
	return scanCommand(x.QueryRow(ctx, `SELECT `+commandColumns+` FROM agenteam_projectvariable.commands WHERE project_id=$1 AND command_name=$2 AND idempotency_key=$3`, p.String(), string(name), string(key)))
}
func loadEventCommand(ctx context.Context, x postgres.SQLExecutor, id event.EventID) (*commandRecord, error) {
	return scanCommand(x.QueryRow(ctx, `SELECT `+commandColumns+` FROM agenteam_projectvariable.commands WHERE event_id=$1`, id.String()))
}
func insertCommand(ctx context.Context, x postgres.SQLExecutor, r *commandRecord) error {
	raw, e := canonical(r.Input)
	if e != nil {
		return e
	}
	var plan, receipt any
	var eventID, committed any
	if r.Plan != nil {
		b, e := canonical(r.Plan)
		if e != nil {
			return e
		}
		plan = b
	}
	if r.Receipt != nil {
		b, e := canonical(*r.Receipt)
		if e != nil {
			return e
		}
		receipt = b
	}
	if r.EventID != nil {
		eventID = r.EventID.String()
	}
	if r.Committed != nil {
		committed = r.Committed.Time()
	}
	return affected(x.Exec(ctx, `INSERT INTO agenteam_projectvariable.commands(id,project_id,actor_user_id,command_name,idempotency_key,semantic_digest,target_id,request,state,plan_revision,plan,event_id,receipt,created_at,committed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, r.ID.String(), r.Project.String(), r.User.String(), string(r.Command), string(r.Key), string(r.Semantic), r.Target.String(), raw, r.State, int64(r.Revision), plan, eventID, receipt, r.Created.Time(), committed))
}
func completeCommand(ctx context.Context, x postgres.SQLExecutor, r *commandRecord, out c.VariableMutation) error {
	raw, e := canonical(out)
	if e != nil {
		return e
	}
	return affected(x.Exec(ctx, `UPDATE agenteam_projectvariable.commands SET state='completed',receipt=$1,committed_at=clock_timestamp() WHERE id=$2 AND state='planned' AND plan_revision=$3`, raw, r.ID.String(), int64(r.Revision)))
}
