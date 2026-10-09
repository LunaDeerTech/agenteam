package skill

import (
	"bytes"
	"context"
	"io"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

var _ pc.ProjectSkillInitializer = (*Service)(nil)

// Each call follows one original command. No physical operation follows an
// unknown reservation commit; a later caller must rediscover the same facts.
func (s *Service) InitializeProjectSkills(ctx context.Context, actor id.Actor, request pc.InitializationRequest) (out pc.InitializationResult, err error) {
	call, err := s.begin(ctx, true)
	if err != nil {
		return out, err
	}
	defer s.end(call)
	ctx = call.ctx
	if err = initializationActor(actor, request); err != nil {
		return out, err
	}
	row, result, err := s.planInitialization(ctx, actor, request)
	if err != nil {
		return out, err
	}
	if result.State != pc.InitializationResultPending {
		return result, nil
	}
	work, err := s.registerInitializationWork(ctx, actor, row)
	if err != nil {
		return out, err
	}
	defer func() {
		if e := s.finishOwnedWork(ctx, work); err == nil && e != nil {
			out = pc.InitializationResult{}
			err = e
		}
	}()
	state := s.state()
	owner, err := row.owner()
	if err != nil {
		return out, err
	}
	pkg, err := state.bundle.Package()
	if err != nil {
		return out, err
	}
	body, err := pkg.Bytes()
	if err != nil {
		return out, err
	}
	// The accepted bundle, not a mutable latest version, supplies the payload.
	if sum(body) != row.bundle.packageDigest || int64(len(body)) != int64(row.bundle.size) {
		return out, unavailable(nil)
	}
	prepared, err := state.objects.PreparePayload(ctx, actor, owner, sc.PackageMediaType, int64(row.bundle.size), &row.bundle.packageDigest, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return out, portError(err)
	}
	defer func() {
		// Discard is an actual owned call. It completes before the admission is
		// released, and a failed discard cannot be presented as clean success.
		if e := state.objects.DiscardPrepared(prepared); err == nil && e != nil {
			out = pc.InitializationResult{}
			err = portError(e)
		}
	}()
	d := prepared.Details()
	if prepared.Validate() != nil || d.MediaType != sc.PackageMediaType || d.Length != int64(row.bundle.size) || d.SHA256 != row.bundle.packageDigest {
		return out, unavailable(nil)
	}
	requestID, err := f.NewID[f.Request]()
	if err != nil {
		return out, unavailable(err)
	}
	command := f.CommandMeta{RequestID: requestID, IdempotencyKey: request.InitializationKey}
	access, err := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.ReserveAccess, Actor: actor, Owner: owner, Intent: id.Mutate, Command: &command, Prepared: prepared})
	if err != nil {
		return out, err
	}
	plan, err := state.objects.DiscoverAccess(ctx, access)
	if err != nil {
		return out, portError(err)
	}
	if plan.Validate() != nil || !plan.Details().Request.Equal(access) {
		return out, unavailable(nil)
	}
	var attempt oc.UploadAttempt
	err = s.initializationWriteTx(ctx, actor, row, []oc.AccessLockPlan{plan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, current *initializationRow, locked oc.LockedAccess) error {
		if current == nil || !sameInitialization(*current, row) || current.phase != initializationPlanned && current.phase != initializationReserved {
			return fault(f.ResourceBusy)
		}
		var e error
		attempt, e = state.objects.ReserveUploadInTx(ctx, tx, actor, owner, command, prepared, plan, locked)
		if e != nil {
			return portError(e)
		}
		if e = recordInitializationAttempt(ctx, x, *current, attempt, state.process); e != nil {
			return e
		}
		row = *current
		row.object = attempt.Details().ObjectID
		row.upload = attempt.Details().UploadID
		row.attempt = attempt.Details().ID
		row.phase = initializationReserved
		row.version++
		return nil
	})
	if err != nil {
		return out, err
	}
	verified, err := state.objects.UploadPrepared(ctx, actor, owner, prepared, attempt)
	if err != nil {
		return out, portError(err)
	}
	if verified.Validate() != nil || verified.Details() != attempt.Details() {
		return out, unavailable(nil)
	}
	access, err = oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.PublishAccess, Actor: actor, Owner: owner, Intent: id.Mutate, Attempt: verified})
	if err != nil {
		return out, err
	}
	plan, err = state.objects.DiscoverAccess(ctx, access)
	if err != nil {
		return out, portError(err)
	}
	if plan.Validate() != nil || !plan.Details().Request.Equal(access) {
		return out, unavailable(nil)
	}
	err = s.initializationWriteTx(ctx, actor, row, []oc.AccessLockPlan{plan}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, current *initializationRow, locked oc.LockedAccess) error {
		if current == nil || !sameInitialization(*current, row) || current.phase != initializationReserved {
			return fault(f.ResourceBusy)
		}
		if e := insertPublishingSkill(ctx, x, *current); e != nil {
			return e
		}
		stored, e := state.objects.PublishVerifiedInTx(ctx, tx, actor, owner, verified, plan, locked)
		if e != nil {
			return portError(e)
		}
		if e = finishInitializationPublication(ctx, x, *current, stored); e != nil {
			return e
		}
		published := *current
		published.phase = initializationPublished
		published.version++
		out, e = initializationResult(ctx, x, request, &published)
		return e
	})
	if err != nil {
		return pc.InitializationResult{}, err
	}
	return out, nil
}

func sameInitialization(a, b initializationRow) bool {
	return a.request == b.request && a.skill == b.skill && a.revision == b.revision && a.semantic == b.semantic && a.version == b.version && a.phase == b.phase && a.object == b.object && a.upload == b.upload && a.attempt == b.attempt
}
func (s *Service) planInitialization(ctx context.Context, actor id.Actor, request pc.InitializationRequest) (initializationRow, pc.InitializationResult, error) {
	state := s.state()
	discovered, _ := loadInitialization(ctx, state.authority.state().store, request.ProjectID)
	var row initializationRow
	if discovered != nil && discovered.request.ProjectID == request.ProjectID {
		row = *discovered
	} else {
		skill, e := f.NewID[pc.Skill]()
		if e != nil {
			return row, pc.InitializationResult{}, unavailable(e)
		}
		revision, e := f.NewID[sc.Revision]()
		if e != nil {
			return row, pc.InitializationResult{}, unavailable(e)
		}
		now, e := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond))
		if e != nil {
			return row, pc.InitializationResult{}, unavailable(e)
		}
		semantic, e := initializationSemantic(request, state.frozen)
		if e != nil {
			return row, pc.InitializationResult{}, e
		}
		row = initializationRow{request: request, skill: skill, revision: revision, bundle: state.frozen, semantic: semantic, phase: initializationPlanned, version: 1, created: now, updated: now}
	}
	var out pc.InitializationResult
	e := s.initializationWriteTx(ctx, actor, row, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, current *initializationRow, _ oc.LockedAccess) error {
		if current != nil {
			if current.skill != row.skill || current.revision != row.revision {
				return fault(f.ResourceBusy)
			}
			if e := current.matches(request, state.frozen); e != nil {
				return e
			}
			row = *current
		} else {
			if discovered != nil {
				return fault(f.ResourceBusy)
			}
			if e := insertInitialization(ctx, x, row); e != nil {
				return e
			}
		}
		var e error
		out, e = initializationResult(ctx, x, request, &row)
		return e
	})
	if e != nil {
		return initializationRow{}, pc.InitializationResult{}, e
	}
	return row, out, nil
}

// One complete union is acquired before any domain or Object mutation. The
// Object service validates its own private issuer and current dependency plan.
func (s *Service) initializationWriteTx(ctx context.Context, actor id.Actor, row initializationRow, plans []oc.AccessLockPlan, work func(context.Context, f.Tx, postgres.SQLExecutor, *initializationRow, oc.LockedAccess) error) error {
	if e := row.validate(); e != nil {
		return e
	}
	locks, e := row.locks(f.Exclusive, row.object)
	if e != nil {
		return e
	}
	command, e := initializationIdentity(row.request)
	if e != nil {
		return e
	}
	cause, e := f.NewCommandsCause(command)
	if e != nil {
		return e
	}
	state := s.state()
	store := state.authority.state().store
	var callbackErr error
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		var locked oc.LockedAccess
		if len(plans) == 0 {
			err = store.AcquireAll(ctx, tx, locks)
		} else {
			locked, err = state.objects.AcquireAccessPlansInTx(ctx, tx, plans, locks)
		}
		if err != nil {
			return portError(err)
		}
		x, current, err := state.authority.initializationInTx(ctx, tx, actor, row.request, false)
		if err != nil {
			return err
		}
		if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		return work(ctx, tx, x, current, locked)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return callbackErr
	}
	return commitError(result)
}

func recordInitializationAttempt(ctx context.Context, x postgres.SQLExecutor, row initializationRow, attempt oc.UploadAttempt, process oc.ProcessID) error {
	if attempt.Validate() != nil || process.Validate() != nil {
		return unavailable(nil)
	}
	d := attempt.Details()
	if row.object != (oc.ObjectID{}) && (row.object != d.ObjectID || row.upload != d.UploadID) {
		return unavailable(nil)
	}
	if row.attempt != d.ID {
		_, e := x.Exec(ctx, `INSERT INTO agenteam_skill.object_attempts(attempt_id,project_id,creation_id,skill_id,revision_id,object_id,upload_id,process_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,clock_timestamp())`, d.ID.String(), row.request.ProjectID.String(), row.request.CreationID.String(), row.skill.String(), row.revision.String(), d.ObjectID.String(), d.UploadID.String(), process.String())
		if e != nil {
			return unavailable(e)
		}
	}
	tag, e := x.Exec(ctx, `UPDATE agenteam_skill.initializations SET object_id=$2,upload_id=$3,current_attempt_id=$4,phase='reserved',version=version+1,updated_at=clock_timestamp() WHERE project_id=$1 AND version=$5 AND phase IN ('planned','reserved')`, row.request.ProjectID.String(), d.ObjectID.String(), d.UploadID.String(), d.ID.String(), int64(row.version))
	if e != nil {
		return unavailable(e)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}
func insertPublishingSkill(ctx context.Context, x postgres.SQLExecutor, row initializationRow) error {
	_, e := x.Exec(ctx, `INSERT INTO agenteam_skill.skills(id,project_id,creation_id,revision_id,name,normalized_name,description,protected,current_revision,version,serving) VALUES($1,$2,$3,$4,$5,$6,$7,true,1,1,true)`, row.skill.String(), row.request.ProjectID.String(), row.request.CreationID.String(), row.revision.String(), row.bundle.name, AddSkillsNormalizedName, row.bundle.description)
	if e != nil {
		return unavailable(e)
	}
	return nil
}
func finishInitializationPublication(ctx context.Context, x postgres.SQLExecutor, row initializationRow, stored oc.PutResult) error {
	m := stored.Meta
	scope, e := id.InProject(row.request.ProjectID)
	if e != nil || stored.Receipt.Validate() == nil || m.Validate() != nil || m.ID != row.object || !m.Scope.Equal(scope) || m.State != oc.Available || m.MediaType != sc.PackageMediaType || m.ByteSize != row.bundle.size || m.SHA256 != row.bundle.packageDigest {
		return unavailable(nil)
	}
	_, e = x.Exec(ctx, `INSERT INTO agenteam_skill.revisions(id,project_id,skill_id,revision,object_id,object_version,object_created_at,published_at) VALUES($1,$2,$3,1,$4,$5,$6,clock_timestamp())`, row.revision.String(), row.request.ProjectID.String(), row.skill.String(), row.object.String(), int64(m.Version), m.CreatedAt.Time())
	if e != nil {
		return unavailable(e)
	}
	tag, e := x.Exec(ctx, `UPDATE agenteam_skill.initializations SET phase='published',version=version+1,updated_at=clock_timestamp() WHERE project_id=$1 AND phase='reserved' AND version=$2 AND current_attempt_id=$3`, row.request.ProjectID.String(), int64(row.version), row.attempt.String())
	if e != nil {
		return unavailable(e)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}
