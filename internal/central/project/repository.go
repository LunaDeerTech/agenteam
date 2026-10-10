// Package project owns Project facts. It uses only formal ports for other
// domains and never queries Account, Skills, Audit or Outbox private tables.
package project

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"time"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Store interface {
	postgres.SQLExecutor
	InTx(foundation.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, foundation.TransactionCause, func(context.Context, foundation.Tx) error) foundation.CommitResult
	AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error
	RequireHeldLocks(context.Context, foundation.Tx, []foundation.LockRequest) error
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
func fault(code foundation.Code) *foundation.Fault {
	return foundation.NewFault(code, foundation.NotStarted)
}
func unavailable(err error) error { return fault(foundation.DependencyUnavailable).WithCause(err) }
func portError(err error) error {
	if err == nil {
		return nil
	}
	var f *foundation.Fault
	if errors.As(err, &f) {
		return f
	}
	return unavailable(err)
}
func invalid() error { return fault(foundation.InvalidArgument) }
func codedField(code foundation.Code, path, detail string) error {
	f := fault(code)
	f.FieldErrors = []foundation.FieldError{{Path: path, Code: detail}}
	return f
}
func nextVersion(v foundation.Version) (foundation.Version, error) {
	if v.Validate() != nil || int64(v) == math.MaxInt64 {
		return 0, fault(foundation.InvalidState)
	}
	return v + 1, nil
}
func digest(raw []byte) foundation.Digest {
	h := sha256.Sum256(raw)
	return foundation.Digest("sha256:" + hex.EncodeToString(h[:]))
}
func instant(t time.Time) foundation.Instant { v, _ := foundation.NewInstant(t); return v }
func parseID[K any](s string) (foundation.ID[K], error) {
	id, e := foundation.ParseID[K](s)
	if e != nil {
		return id, unavailable(e)
	}
	return id, nil
}
func userLock(id string, mode foundation.LockMode) foundation.LockRequest {
	k, _ := foundation.UserLock(id)
	return foundation.LockRequest{Key: k, Mode: mode}
}
func projectLock(id c.ProjectID, mode foundation.LockMode) foundation.LockRequest {
	k, _ := foundation.ProjectLock(id.String())
	return foundation.LockRequest{Key: k, Mode: mode}
}
func commandLock(id foundation.CommandIdentity) foundation.LockRequest {
	k, _ := foundation.CommandLock(id)
	return foundation.LockRequest{Key: k, Mode: foundation.Exclusive}
}
func commandCause(id foundation.CommandIdentity) foundation.TransactionCause {
	v, _ := foundation.NewCommandsCause(id)
	return v
}
func readCause(name string) (foundation.TransactionCause, error) {
	id, e := foundation.NewID[struct{}]()
	if e != nil {
		return foundation.TransactionCause{}, unavailable(e)
	}
	v, e := foundation.NewRecoveryCause("project."+name, id.String(), "")
	return v, portError(e)
}
func dbNow(ctx context.Context, x postgres.SQLExecutor) (foundation.Instant, error) {
	var t time.Time
	if e := x.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&t); e != nil {
		return foundation.Instant{}, unavailable(e)
	}
	v, e := foundation.NewInstant(t)
	return v, portError(e)
}
func uniqueName(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "projects_owner_name_key" {
		return codedField(foundation.ResourceBusy, "/name", "NAME_TAKEN")
	}
	return unavailable(err)
}

// Every name-reserving Create and Update holds the same Owner User EX lock,
// including updates of different Project IDs. Check before a uniqueness error
// poisons the Store transaction. All retained rows reserve their names: an
// initializing, archived or deleting Project is not a free directory entry.
// The database unique constraint remains the final integrity backstop.
func (s *Service) requireAvailableName(ctx context.Context, tx foundation.Tx, x postgres.SQLExecutor, owner identity.UserID, project c.ProjectID, normalized string) error {
	if e := s.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{userLock(owner.String(), foundation.Exclusive), projectLock(project, foundation.Exclusive)}); e != nil {
		return unavailable(e)
	}
	var occupied bool
	if e := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_project.projects WHERE owner_user_id=$1 AND normalized_name=$2 AND id<>$3)`, owner.String(), normalized, project.String()).Scan(&occupied); e != nil {
		return unavailable(e)
	}
	if occupied {
		return codedField(foundation.ResourceBusy, "/name", "NAME_TAKEN")
	}
	return nil
}
func affected(tag pgconn.CommandTag, err error) error {
	if err != nil {
		return unavailable(err)
	}
	if tag.RowsAffected() != 1 {
		return fault(foundation.ResourceBusy)
	}
	return nil
}

type projectRecord struct {
	ref         c.ProjectRef
	creation    c.CreationID
	initialized bool
	operation   *c.OperationID
}

const projectColumns = `id::text,owner_user_id::text,name,normalized_name,description,lifecycle,version,current_sprint_id::text,created_at,updated_at,archived_at,creation_id::text,initialized_at IS NOT NULL,current_lifecycle_operation_id::text`

func scanProject(row interface{ Scan(...any) error }) (*projectRecord, error) {
	var r projectRecord
	var id, owner, creation string
	var sprint, operation *string
	var version int64
	var created, updated time.Time
	var archived *time.Time
	e := row.Scan(&id, &owner, &r.ref.Name, &r.ref.NormalizedName, &r.ref.Description, &r.ref.Lifecycle, &version, &sprint, &created, &updated, &archived, &creation, &r.initialized, &operation)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, unavailable(e)
	}
	r.ref.ID, e = parseID[identity.Project](id)
	if e != nil {
		return nil, e
	}
	r.ref.OwnerUserID, e = parseID[identity.User](owner)
	if e != nil {
		return nil, e
	}
	r.creation, e = parseID[c.Creation](creation)
	if e != nil {
		return nil, e
	}
	r.ref.Version = foundation.Version(version)
	r.ref.CreatedAt = instant(created)
	r.ref.UpdatedAt = instant(updated)
	if sprint != nil {
		v, e := parseID[c.Sprint](*sprint)
		if e != nil {
			return nil, e
		}
		r.ref.CurrentSprintID = &v
	}
	if operation != nil {
		v, e := parseID[c.Operation](*operation)
		if e != nil {
			return nil, e
		}
		r.operation = &v
	}
	if archived != nil {
		v := instant(*archived)
		r.ref.ArchivedAt = &v
	}
	if r.ref.Validate() != nil {
		return nil, unavailable(nil)
	}
	return &r, nil
}
func loadProject(ctx context.Context, x postgres.SQLExecutor, id c.ProjectID) (*projectRecord, error) {
	return scanProject(x.QueryRow(ctx, `SELECT `+projectColumns+` FROM agenteam_project.projects WHERE id=$1`, id.String()))
}
func deletedOwner(ctx context.Context, x postgres.SQLExecutor, id c.ProjectID) (string, error) {
	var owner string
	e := x.QueryRow(ctx, `SELECT original_owner_user_id::text FROM agenteam_project.deletion_receipts WHERE deleted_project_id=$1`, id.String()).Scan(&owner)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", nil
	}
	if e != nil {
		return "", unavailable(e)
	}
	if _, e = parseID[identity.User](owner); e != nil {
		return "", e
	}
	return owner, nil
}

// Caller checks current Session first. Ordinary Project body access exposes
// neither foreign occupancy nor a tombstone; only Create and lifecycle receipt
// paths have the explicit original-owner deleted-target projection.
func ownerProject(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, id c.ProjectID) (*projectRecord, error) {
	r, e := loadProject(ctx, x, id)
	if e != nil {
		return nil, e
	}
	if r == nil || r.ref.OwnerUserID.String() != actor.Details().UserID {
		return nil, fault(foundation.NotFound)
	}
	return r, nil
}

type creationRecord struct {
	operation                       c.CreationOperation
	owner                           identity.UserID
	key                             foundation.IdempotencyKey
	semantic                        foundation.Digest
	initializationKey               foundation.IdempotencyKey
	requestName, requestDescription *string
	protectedSkill                  *c.SkillID
	revision                        *foundation.Revision
	eventID                         string
	eventHeader, eventPayload       []byte
	result                          *c.ProjectRef
}

const creationColumns = `id::text,project_id::text,owner_user_id::text,command_key,semantic_digest,request_name,request_description,state,initialization_key,protected_skill_id::text,protected_revision,safe_reason,version,created_at,updated_at,event_id::text,event_header,event_payload,safe_result`

func scanCreation(row interface{ Scan(...any) error }) (*creationRecord, error) {
	var r creationRecord
	var id, project, owner string
	var skill, reason *string
	var revision *int64
	var version int64
	var result []byte
	var created, updated time.Time
	e := row.Scan(&id, &project, &owner, &r.key, &r.semantic, &r.requestName, &r.requestDescription, &r.operation.State, &r.initializationKey, &skill, &revision, &reason, &version, &created, &updated, &r.eventID, &r.eventHeader, &r.eventPayload, &result)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, unavailable(e)
	}
	r.operation.ID, e = parseID[c.Creation](id)
	if e != nil {
		return nil, e
	}
	r.operation.ProjectID, e = parseID[identity.Project](project)
	if e != nil {
		return nil, e
	}
	r.owner, e = parseID[identity.User](owner)
	if e != nil {
		return nil, e
	}
	r.operation.Version = foundation.Version(version)
	r.operation.CreatedAt = instant(created)
	r.operation.UpdatedAt = instant(updated)
	if reason != nil {
		r.operation.SafeReason = c.SafeReason(*reason)
	}
	if skill != nil {
		v, e := parseID[c.Skill](*skill)
		if e != nil {
			return nil, e
		}
		r.protectedSkill = &v
	}
	if revision != nil {
		v := foundation.Revision(*revision)
		if v.Validate() != nil {
			return nil, unavailable(nil)
		}
		r.revision = &v
	}
	if r.operation.Validate() != nil || r.key.Validate() != nil || r.semantic.Validate() != nil || r.initializationKey.Validate() != nil {
		return nil, unavailable(nil)
	}
	if _, e = parseID[struct{}](r.eventID); e != nil {
		return nil, e
	}
	if len(result) > 0 {
		var ref c.ProjectRef
		if json.Unmarshal(result, &ref) != nil {
			return nil, unavailable(nil)
		}
		r.result = &ref
	}
	complete := r.operation.State == c.CreationCompleted
	if complete != (r.result != nil) || complete != (r.protectedSkill != nil && r.revision != nil) || complete && (r.requestName != nil || r.requestDescription != nil) || !complete && (r.requestName == nil || r.requestDescription == nil) {
		return nil, unavailable(nil)
	}
	return &r, nil
}
func loadCreation(ctx context.Context, x postgres.SQLExecutor, id c.CreationID) (*creationRecord, error) {
	return scanCreation(x.QueryRow(ctx, `SELECT `+creationColumns+` FROM agenteam_project.creations WHERE id=$1`, id.String()))
}
func projectCreation(ctx context.Context, x postgres.SQLExecutor, id c.ProjectID) (*creationRecord, error) {
	return scanCreation(x.QueryRow(ctx, `SELECT `+creationColumns+` FROM agenteam_project.creations WHERE project_id=$1`, id.String()))
}
func (r *creationRecord) identity() foundation.CommandIdentity {
	id, _ := c.CommandIdentity(r.operation.ProjectID, c.CreateCommand, r.key)
	return id
}
func (r *creationRecord) request() c.InitializationRequest {
	return c.InitializationRequest{CreationID: r.operation.ID, ProjectID: r.operation.ProjectID, InitializationKey: r.initializationKey}
}
func creationResult(r *creationRecord, p *projectRecord) (c.CreationResult, error) {
	if r == nil || p == nil || r.operation.ProjectID != p.ref.ID || r.operation.ID != p.creation || r.owner != p.ref.OwnerUserID {
		return c.CreationResult{}, unavailable(nil)
	}
	if r.operation.State == c.CreationCompleted {
		if !p.initialized || p.ref.Lifecycle == c.Deleting {
			return c.CreationResult{}, fault(foundation.ResourceDeleted)
		}
		if r.result == nil || r.result.ID != p.ref.ID || r.result.OwnerUserID != r.owner {
			return c.CreationResult{}, unavailable(nil)
		}
		ref := *r.result
		return c.CreationResult{State: c.CreationReady, Project: &ref}, nil
	}
	if p.initialized {
		return c.CreationResult{}, unavailable(nil)
	}
	op := r.operation
	return c.CreationResult{State: c.CreationPending, Operation: &op}, nil
}

// Stored plans are private canonical facts, never a wire projection. safe_result
// remains NULL until the business change, Audit, Event and activity commit.
type updatePlan struct {
	ExpectedVersion foundation.Version        `json:"expected_version"`
	Project         c.ProjectRef              `json:"project"`
	Changed         []c.ChangedField          `json:"changed"`
	Header          json.RawMessage           `json:"event_header"`
	Payload         json.RawMessage           `json:"event_payload"`
	Scheduler       *c.ProjectSchedulerConfig `json:"scheduler,omitempty"`
}
type commandRecord struct {
	id       string
	project  c.ProjectID
	user     string
	name     c.CommandName
	key      foundation.IdempotencyKey
	semantic foundation.Digest
	state    string
	result   *c.CommandResult
	plan     *updatePlan
	eventID  *string
}

func loadCommand(ctx context.Context, x postgres.SQLExecutor, id c.ProjectID, name c.CommandName, key foundation.IdempotencyKey) (*commandRecord, error) {
	var r commandRecord
	var rawResult, rawPlan []byte
	var project string
	e := x.QueryRow(ctx, `SELECT id::text,project_id::text,actor_user_id::text,command_name,key,semantic_digest,state,safe_result,plan,event_id::text FROM agenteam_project.commands WHERE project_id=$1 AND command_name=$2 AND key=$3`, id.String(), string(name), string(key)).Scan(&r.id, &project, &r.user, &r.name, &r.key, &r.semantic, &r.state, &rawResult, &rawPlan, &r.eventID)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, unavailable(e)
	}
	r.project, e = parseID[identity.Project](project)
	if e != nil {
		return nil, e
	}
	if r.project != id || r.name != name || r.key != key || r.semantic.Validate() != nil || r.state != "planned" && r.state != "completed" {
		return nil, unavailable(nil)
	}
	if _, e = parseID[struct{}](r.id); e != nil {
		return nil, e
	}
	if _, e = parseID[identity.User](r.user); e != nil {
		return nil, e
	}
	if len(rawResult) > 0 {
		var v c.CommandResult
		if json.Unmarshal(rawResult, &v) != nil || v.Command != name {
			return nil, unavailable(nil)
		}
		r.result = &v
	}
	if len(rawPlan) > 0 {
		var v updatePlan
		if json.Unmarshal(rawPlan, &v) != nil || validateUpdatePlan(v) != nil || v.Project.ID != id || v.Project.OwnerUserID.String() != r.user {
			return nil, unavailable(nil)
		}
		r.plan = &v
	}
	if (r.state == "completed") != (r.result != nil) || r.state == "planned" && r.plan == nil {
		return nil, unavailable(nil)
	}
	return &r, nil
}
func (r *commandRecord) identity() foundation.CommandIdentity {
	id, _ := c.CommandIdentity(r.project, r.name, r.key)
	return id
}

func validateUpdatePlan(p updatePlan) error {
	next, e := nextVersion(p.ExpectedVersion)
	if e != nil || p.Project.Validate() != nil || p.Project.Version != next || p.Project.Lifecycle != c.Active || len(p.Header) == 0 || len(p.Payload) == 0 {
		return unavailable(nil)
	}
	fields := c.UpdatedPayload{ChangedFields: p.Changed}
	if fields.Validate() != nil {
		return unavailable(nil)
	}
	hasScheduler := false
	for _, field := range p.Changed {
		hasScheduler = hasScheduler || field == c.SchedulerEnabledChanged || field == c.SchedulerMaxConcurrencyChanged
	}
	if hasScheduler != (p.Scheduler != nil) || p.Scheduler != nil && p.Scheduler.Validate() != nil {
		return unavailable(nil)
	}
	var payload c.UpdatedPayload
	if json.Unmarshal(p.Payload, &payload) != nil || !reflect.DeepEqual(payload.ChangedFields, p.Changed) {
		return unavailable(nil)
	}
	header, e := event.DecodeHeader(p.Header)
	if e != nil || header.EventType != c.UpdatedEventName || header.Scope.Kind != event.ProjectScope || header.Scope.ProjectID.String() != p.Project.ID.String() || header.AggregateType != c.ProjectAggregate || header.AggregateID.String() != p.Project.ID.String() || header.AggregateVersion == nil || *header.AggregateVersion != next || header.AggregateSequence != nil || header.SchemaVersion != c.ProjectEventSchemaVersion {
		return unavailable(nil)
	}
	return nil
}
