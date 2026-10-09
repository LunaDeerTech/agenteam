package projectvariable

import (
	"context"
	"errors"
	"math"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	"github.com/jackc/pgx/v5"
)

func (s *Service) CreateVariable(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, in c.VariableCreate) (c.VariableMutation, error) {
	return s.execute(ctx, a, m, commandInput{Project: p, Target: in.Fields().ID, Command: c.CreateCommand, Create: &in})
}
func (s *Service) UpdateVariable(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, id c.VariableID, in c.VariableUpdate) (c.VariableMutation, error) {
	return s.execute(ctx, a, m, commandInput{Project: p, Target: id, Command: c.UpdateCommand, Update: &in})
}
func (s *Service) DeleteVariable(ctx context.Context, a i.Actor, m f.CommandMeta, p c.ProjectID, id c.VariableID) (c.VariableMutation, error) {
	return s.execute(ctx, a, m, commandInput{Project: p, Target: id, Command: c.DeleteCommand})
}

func (s *Service) current(ctx context.Context, tx f.Tx, a i.Actor, q c.VariableCommandLookupRequest) (postgres.SQLExecutor, *commandRecord, *c.VariableMutation, error) {
	st := s.state()
	query := q.Fields()
	access, e := st.deps.Projects.RequireOwnerInTx(ctx, tx, a, query.ProjectID, i.Read)
	if e != nil {
		return nil, nil, nil, portError(e)
	}
	if access.Project().ID != query.ProjectID {
		return nil, nil, nil, internal(nil)
	}
	x, e := st.store.InTx(tx)
	if e != nil {
		return nil, nil, nil, portError(e)
	}
	r, e := loadCommand(ctx, x, query.ProjectID, query.Command, query.IdempotencyKey)
	if e != nil {
		return nil, nil, nil, e
	}
	if r == nil {
		return x, nil, nil, nil
	}
	if r.User.String() != a.Details().UserID || r.Semantic != query.SemanticDigest {
		return nil, nil, nil, fault(f.IdempotencyKeyReused)
	}
	if e = validateRecord(r, a); e != nil {
		return nil, nil, nil, e
	}
	if r.State == "completed" {
		out := r.Receipt.Clone()
		return x, r, &out, nil
	}
	return x, r, nil, nil
}
func (s *Service) execute(ctx context.Context, a i.Actor, m f.CommandMeta, in commandInput) (c.VariableMutation, error) {
	empty := c.VariableMutation{}
	ctx, call, done, e := s.begin(ctx)
	if e != nil {
		return empty, e
	}
	defer done()
	if e = readInput(a, in.Project); e != nil {
		return empty, e
	}
	if e = c.ValidateCommandMeta(in.Command, m); e != nil {
		return empty, e
	}
	if m.ExpectedVersion != nil {
		version := *m.ExpectedVersion
		in.Expected = &version
	}
	in.User, e = f.ParseID[i.User](a.Details().UserID)
	if e != nil {
		return empty, fault(f.Unauthenticated)
	}
	semantic, e := in.semantic(a, m.IdempotencyKey)
	if e != nil {
		return empty, e
	}
	raw, e := canonical(in)
	if e != nil {
		return empty, e
	}
	if e = in.UnmarshalJSON(raw); e != nil {
		return empty, e
	}
	identity, e := c.VariableCommandIdentity(in.Project, in.Command, m.IdempotencyKey)
	if e != nil {
		return empty, e
	}
	q, e := c.NewVariableCommandLookupRequest(c.VariableCommandLookupFields{ProjectID: in.Project, Command: in.Command, IdempotencyKey: m.IdempotencyKey, SemanticDigest: semantic})
	if e != nil {
		return empty, e
	}
	base, e := in.locks(m.IdempotencyKey)
	if e != nil {
		return empty, e
	}
	st := s.state()
	var prepared *commandRecord
	var out *c.VariableMutation
	result := st.store.WithinTx(ctx, commandCause(identity), func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, base); e != nil {
			return portError(e)
		}
		x, r, replay, e := s.current(ctx, tx, a, q)
		if e != nil {
			return e
		}
		if replay != nil {
			out = replay
			return nil
		}
		if _, e = st.deps.Projects.RequireOwnerInTx(ctx, tx, a, in.Project, i.Mutate); e != nil {
			return portError(e)
		}
		if r != nil {
			if e = checkPreimage(ctx, x, r); e != nil {
				return e
			}
			prepared = r
			return nil
		}
		at, e := dbNow(ctx, x)
		if e != nil {
			return e
		}
		op, e := f.NewID[c.Operation]()
		if e != nil {
			return unavailable(e)
		}
		r = &commandRecord{ID: op, Project: in.Project, User: in.User, Command: in.Command, Key: m.IdempotencyKey, Semantic: semantic, Target: in.Target, Input: in, Revision: 1, State: "planned", Created: at}
		var before *c.Variable
		if in.Command != c.CreateCommand {
			v, _, e := loadVariable(ctx, x, in.Project, in.Target, false)
			if e != nil {
				return e
			}
			before = &v
		}
		plan, changed, e := planVariable(in, before, at, op)
		if e != nil {
			return e
		}
		if !changed {
			receipt, e := c.NewVariableMutation(c.VariableMutationFields{Command: in.Command, Changed: false, Variable: plan.After})
			if e != nil {
				return internal(e)
			}
			r.State = "completed"
			r.Receipt = &receipt
			r.Committed = &at
			if e = validateRecord(r, a); e != nil {
				return e
			}
			if e = insertCommand(ctx, x, r); e != nil {
				return e
			}
			out = &receipt
			return nil
		}
		eventID, e := f.NewID[event.EventIdentity]()
		if e != nil {
			return unavailable(e)
		}
		r.Plan = plan
		r.EventID = &eventID
		if e = validateRecord(r, a); e != nil {
			return e
		}
		if e = checkPreimage(ctx, x, r); e != nil {
			return e
		}
		if e = insertCommand(ctx, x, r); e != nil {
			return e
		}
		prepared = r
		return nil
	})
	if result.State() == f.Unknown {
		return s.confirmUnknown(ctx, call, a, q, result)
	}
	if e = txError(result); e != nil {
		return empty, e
	}
	if out != nil {
		return out.Clone(), nil
	}
	if prepared == nil || prepared.Plan == nil {
		return empty, internal(nil)
	}
	ev, e := st.deps.VariableEvents.NewVariableChanged(recordHeader(prepared), recordPayload(prepared))
	if e != nil {
		return empty, internal(e)
	}
	appendPlan, e := st.deps.Events.PrepareAppend(ctx, a, ev)
	if e != nil {
		return empty, portError(e)
	}
	locks, e := oc.NormalizeLocks(append(append([]f.LockRequest{}, base...), appendPlan.Locks()...))
	if e != nil {
		return empty, portError(e)
	}
	result = st.store.WithinTx(ctx, commandCause(identity), func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		x, r, replay, e := s.current(ctx, tx, a, q)
		if e != nil {
			return e
		}
		if replay != nil {
			out = replay
			return nil
		}
		if _, e = st.deps.Projects.RequireOwnerInTx(ctx, tx, a, in.Project, i.Mutate); e != nil {
			return portError(e)
		}
		if r == nil || r.ID != prepared.ID || r.Revision != prepared.Revision || !sameValue(r.Plan, prepared.Plan) || r.EventID == nil || *r.EventID != *prepared.EventID {
			return fault(f.VersionConflict)
		}
		if e = checkPreimage(ctx, x, r); e != nil {
			return e
		}
		if e = applyPlan(ctx, x, r); e != nil {
			return e
		}
		entry, key, e := recordAudit(r, a)
		if e != nil {
			return e
		}
		auditReceipt, e := st.deps.Audit.AppendInTx(ctx, tx, entry, key)
		if e != nil {
			return portError(e)
		}
		if auditReceipt.AuditID.Validate() != nil || auditReceipt.CreatedAt.Validate() != nil {
			return internal(nil)
		}
		eventReceipt, e := st.deps.Events.AppendEventInTx(ctx, tx, a, ev, appendPlan)
		if e != nil {
			return portError(e)
		}
		if eventReceipt.EventID != *r.EventID || eventReceipt.Sequence.Validate() != nil {
			return internal(nil)
		}
		receipt, e := mutationReceipt(r, auditReceipt.AuditID)
		if e != nil {
			return e
		}
		if e = completeCommand(ctx, x, r, receipt); e != nil {
			return e
		}
		if e = st.deps.Activity.TouchActivityInTx(ctx, tx, a); e != nil {
			return portError(e)
		}
		out = &receipt
		return nil
	})
	if result.State() == f.Unknown {
		return s.confirmUnknown(ctx, call, a, q, result)
	}
	if e = txError(result); e != nil {
		return empty, e
	}
	if out == nil || out.Validate() != nil {
		return empty, internal(nil)
	}
	return out.Clone(), nil
}

func planVariable(in commandInput, before *c.Variable, at f.Instant, history c.OperationID) (*commandPlan, bool, error) {
	var next c.VariableFields
	if in.Command == c.CreateCommand {
		if before != nil || in.Create == nil {
			return nil, false, internal(nil)
		}
		fields := in.Create.Fields()
		next = c.VariableFields{ID: in.Target, ProjectID: in.Project, Type: c.VariableType, Name: fields.Name, Description: fields.Description, Value: fields.Value, Version: 1, CreatedAt: at, UpdatedAt: at}
		v, e := c.NewVariable(next)
		if e != nil {
			return nil, false, internal(e)
		}
		return &commandPlan{After: v, Fields: []string{"created"}, HistoryID: history}, true, nil
	}
	if before == nil || before.Validate() != nil || in.Expected == nil {
		return nil, false, internal(nil)
	}
	next = before.Fields()
	if next.ProjectID != in.Project || next.ID != in.Target {
		return nil, false, internal(nil)
	}
	if next.Version != *in.Expected {
		return nil, false, fault(f.VersionConflict)
	}
	fields := []string{}
	if in.Command == c.DeleteCommand {
		fields = []string{"deleted"}
		next.Value = ""
		next.Description = ""
	} else {
		update := in.Update.Fields()
		if update.Description != nil && *update.Description != next.Description {
			next.Description = *update.Description
			fields = append(fields, "description")
		}
		if update.Name != nil && *update.Name != next.Name {
			next.Name = *update.Name
			fields = append(fields, "name")
		}
		if update.Value != nil && *update.Value != next.Value {
			next.Value = *update.Value
			fields = append(fields, "value")
		}
	}
	if len(fields) == 0 {
		return &commandPlan{Before: before, After: *before, Fields: fields, HistoryID: history}, false, nil
	}
	version, e := nextVersion(next.Version)
	if e != nil {
		return nil, false, e
	}
	next.Version = version
	if at.Time().Before(next.UpdatedAt.Time()) {
		return nil, false, internal(nil)
	}
	next.UpdatedAt = at
	v, e := c.NewVariable(next)
	if e != nil {
		return nil, false, internal(e)
	}
	return &commandPlan{Before: before, After: v, Deleted: in.Command == c.DeleteCommand, Fields: fields, HistoryID: history}, true, nil
}
func checkPreimage(ctx context.Context, x postgres.SQLExecutor, r *commandRecord) error {
	if r.Plan == nil {
		return internal(nil)
	}
	p := r.Plan
	if r.Command == c.CreateCommand {
		// IDs are globally non-reusable. Only read the owning Project marker;
		// a caller must never receive another Project's object or ID conflict.
		var project string
		e := x.QueryRow(ctx, `SELECT project_id FROM agenteam_projectvariable.variables WHERE id=$1`, r.Target.String()).Scan(&project)
		if e == nil {
			if project != r.Project.String() {
				return fault(f.NotFound)
			}
			return field(f.ResourceBusy, "/variable_id", "ID_CONFLICT")
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return unavailable(e)
		}
		var count int64
		if e = x.QueryRow(ctx, `SELECT count(*) FROM agenteam_projectvariable.variables WHERE project_id=$1 AND deleted_at IS NULL`, r.Project.String()).Scan(&count); e != nil {
			return unavailable(e)
		}
		if count >= c.MaxVariables {
			return field(f.ResourceBusy, "/variable_id", "PROJECT_VARIABLE_LIMIT")
		}
	} else {
		current, deleted, e := loadVariable(ctx, x, r.Project, r.Target, true)
		if e != nil {
			return e
		}
		if deleted != nil {
			return fault(f.NotFound)
		}
		if p.Before == nil || !sameValue(current, *p.Before) {
			return fault(f.VersionConflict)
		}
	}
	if !p.Deleted {
		var exists bool
		if e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_projectvariable.variables WHERE project_id=$1 AND name=$2 AND id<>$3 AND deleted_at IS NULL)`, r.Project.String(), p.After.Fields().Name, r.Target.String()).Scan(&exists); e != nil {
			return unavailable(e)
		}
		if exists {
			return field(f.ResourceBusy, "/request/name", "NAME_CONFLICT")
		}
	}
	gen, e := generation(ctx, x, r.Project)
	if e != nil {
		return e
	}
	if gen == math.MaxInt64 {
		return field(f.ResourceBusy, "/project_id", "VERSION_EXHAUSTED")
	}
	return nil
}
func applyPlan(ctx context.Context, x postgres.SQLExecutor, r *commandRecord) error {
	p := r.Plan
	v := p.After.Fields()
	if r.Command == c.CreateCommand {
		// A different Project may win this globally unique ID after our precheck.
		// Do not raise SQL 23505: Store correctly preserves a poisoned transaction
		// ahead of a later domain classification. Other constraints still fail.
		tag, e := x.Exec(ctx, `INSERT INTO agenteam_projectvariable.variables(id,project_id,type,name,description,value,version,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT ON CONSTRAINT variables_pkey DO NOTHING`, v.ID.String(), v.ProjectID.String(), v.Type, v.Name, v.Description, v.Value, int64(v.Version), v.CreatedAt.Time(), v.UpdatedAt.Time())
		if e != nil {
			return unavailable(e)
		}
		if tag.RowsAffected() == 0 {
			return fault(f.NotFound)
		}
		if e = affected(tag, nil); e != nil {
			return e
		}
	} else {
		var deleted any
		if p.Deleted {
			deleted = v.UpdatedAt.Time()
		}
		if e := affected(x.Exec(ctx, `UPDATE agenteam_projectvariable.variables SET name=$3,description=$4,value=$5,version=$6,updated_at=$7,deleted_at=$8 WHERE project_id=$1 AND id=$2 AND version=$9 AND deleted_at IS NULL`, v.ProjectID.String(), v.ID.String(), v.Name, v.Description, v.Value, int64(v.Version), v.UpdatedAt.Time(), deleted, int64(p.Before.Fields().Version))); e != nil {
			return e
		}
	}
	if e := affected(x.Exec(ctx, `INSERT INTO agenteam_projectvariable.project_generations(project_id,query_generation) VALUES($1,2) ON CONFLICT(project_id) DO UPDATE SET query_generation=agenteam_projectvariable.project_generations.query_generation+1`, r.Project.String())); e != nil {
		return e
	}
	fields, e := canonical(p.Fields)
	if e != nil {
		return e
	}
	return affected(x.Exec(ctx, `INSERT INTO agenteam_projectvariable.history(id,project_id,variable_id,operation_id,version,kind,changed_fields,actor_user_id,occurred_at,event_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, p.HistoryID.String(), r.Project.String(), r.Target.String(), r.ID.String(), int64(v.Version), string(recordPayload(r).Change), fields, r.User.String(), v.UpdatedAt.Time(), r.EventID.String()))
}
func mutationReceipt(r *commandRecord, auditID ac.ID) (c.VariableMutation, error) {
	fields := c.VariableMutationFields{Command: r.Command, Changed: true, EventID: r.EventID, AuditID: &auditID}
	if r.Plan.Deleted {
		v := r.Plan.After.Fields()
		fields.Deleted = &c.VariableDeleted{ID: v.ID, ProjectID: v.ProjectID, Type: v.Type, Version: v.Version, DeletedAt: v.UpdatedAt}
	} else {
		fields.Variable = r.Plan.After
	}
	v, e := c.NewVariableMutation(fields)
	if e != nil {
		return c.VariableMutation{}, internal(e)
	}
	return v, nil
}
func validateRecord(r *commandRecord, a i.Actor) error {
	if r == nil || r.ID.Validate() != nil || r.User.String() != a.Details().UserID || r.Created.Validate() != nil || r.Created.Time().IsZero() || r.Revision.Validate() != nil || r.Project != r.Input.Project || r.User != r.Input.User || r.Target != r.Input.Target || r.Command != r.Input.Command {
		return internal(nil)
	}
	if r.State == "planned" {
		if r.Plan == nil || r.EventID == nil || r.Receipt != nil || r.Committed != nil {
			return internal(nil)
		}
	} else if r.State == "completed" {
		if r.Receipt == nil || r.Receipt.Validate() != nil || r.Committed == nil {
			return internal(nil)
		}
		out := r.Receipt.Fields()
		if out.Command != r.Command || out.Changed != (r.Plan != nil) || out.Changed != (r.EventID != nil) {
			return internal(nil)
		}
	} else {
		return internal(nil)
	}
	d, e := r.Input.semantic(a, r.Key)
	if e != nil || d != r.Semantic {
		return internal(nil)
	}
	if r.Plan != nil {
		expected, changed, e := planVariable(r.Input, r.Plan.Before, r.Plan.After.Fields().UpdatedAt, r.Plan.HistoryID)
		if e != nil || !changed || !sameValue(expected, r.Plan) || r.EventID == nil || r.EventID.Validate() != nil || r.Plan.After.Fields().UpdatedAt.Time().Before(r.Created.Time()) {
			return internal(nil)
		}
		if r.Receipt != nil {
			fields := r.Receipt.Fields()
			if fields.AuditID == nil {
				return internal(nil)
			}
			expected, e := mutationReceipt(r, *fields.AuditID)
			if e != nil || !sameValue(expected, *r.Receipt) {
				return internal(nil)
			}
		}
	} else {
		if r.State != "completed" || r.Receipt == nil || r.Command != c.UpdateCommand || r.EventID != nil {
			return internal(nil)
		}
		v := r.Receipt.Fields()
		before := v.Variable
		plan, changed, e := planVariable(r.Input, &before, before.Fields().UpdatedAt, r.ID)
		if e != nil || changed || v.Changed || v.Command != r.Command || !sameValue(plan.After, v.Variable) {
			return internal(nil)
		}
	}
	if r.State == "completed" && (r.Committed == nil || r.Committed.Time().Before(r.Created.Time()) || r.Plan != nil && r.Committed.Time().Before(r.Plan.After.Fields().UpdatedAt.Time())) {
		return internal(nil)
	}
	return nil
}
func (s *Service) LookupVariableCommand(ctx context.Context, a i.Actor, q c.VariableCommandLookupRequest) (c.VariableCommandLookup, error) {
	ctx, _, done, e := s.begin(ctx)
	if e != nil {
		return c.VariableCommandLookup{}, e
	}
	defer done()
	return s.lookup(ctx, a, q)
}
func (s *Service) lookup(ctx context.Context, a i.Actor, q c.VariableCommandLookupRequest) (c.VariableCommandLookup, error) {
	empty := c.VariableCommandLookup{}
	if q.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	v := q.Fields()
	if e := readInput(a, v.ProjectID); e != nil {
		return empty, e
	}
	id, e := c.VariableCommandIdentity(v.ProjectID, v.Command, v.IdempotencyKey)
	if e != nil {
		return empty, e
	}
	locks, e := oc.NormalizeLocks(append(readLocks(a, v.ProjectID), commandLock(id)))
	if e != nil {
		return empty, e
	}
	var out c.VariableCommandLookup
	st := s.state()
	result := st.store.WithinTx(ctx, commandCause(id), func(ctx context.Context, tx f.Tx) error {
		if e := st.store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		_, r, receipt, e := s.current(ctx, tx, a, q)
		if e != nil {
			return e
		}
		status := c.LookupNotObserved
		if r != nil {
			status = c.LookupInProgress
		}
		if receipt != nil {
			status = c.LookupCommitted
		}
		out, e = c.NewVariableCommandLookup(status, receipt)
		return e
	})
	if e = txError(result); e != nil {
		return empty, e
	}
	if e = ctx.Err(); e != nil {
		return empty, canceled(e)
	}
	return out, nil
}
func (s *Service) confirmUnknown(ctx context.Context, entry *call, a i.Actor, q c.VariableCommandLookupRequest, original f.CommitResult) (c.VariableMutation, error) {
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
	out, e := s.lookup(confirm, a, q)
	if e == nil && out.Status() == c.LookupCommitted {
		receipt := out.Receipt()
		if receipt != nil {
			return receipt.Clone(), nil
		}
	}
	return c.VariableMutation{}, txError(original)
}

var _ c.Commands = (*Service)(nil)
