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
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// Install publishes an ordinary revision-one Skill from an already validated
// canonical package. The original caller owns preparation, upload, publication
// and Discard before its admitted work can retire. Agent execution remains
// explicitly unbound until a real execution authority is composed.
func (s *Service) Install(ctx context.Context, actor id.Actor, meta f.CommandMeta, project id.ProjectID, request InstallRequest) (out InstallReceipt, err error) {
	if meta.Validate() != nil || meta.ExpectedVersion != nil || request.Validate() != nil {
		return out, invalid()
	}
	if _, err = installationActor(actor); err != nil {
		return out, err
	}
	call, err := s.beginProjectWork(ctx, project, installationWork)
	if err != nil {
		return out, err
	}
	defer s.end(call)
	ctx = call.ctx
	state := s.state()
	row, err := state.authority.planInstallation(ctx, actor, project, meta.IdempotencyKey, request)
	if err != nil {
		return out, err
	}
	if row.phase == installationPublished {
		return s.installedReceipt(ctx, actor, row)
	}
	if row.phase != installationPlanned {
		// A prior reserved attempt is never sent again just because its caller
		// disappeared or a commit response was lost. The exact key is retained.
		return out, installationOutcomePending(row)
	}
	work, err := s.newInstallationOwnedWork(row, installationWork, call)
	if err != nil {
		return out, err
	}
	err = s.installationTransaction(ctx, actor, row, nil, true, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, current installationRow, _ oc.LockedAccess) error {
		if !sameInstallation(current, row) || current.phase != installationPlanned {
			return fault(f.ResourceBusy)
		}
		return insertWork(ctx, x, work.fact)
	})
	if err != nil {
		s.registrationFailed(work, err)
		return out, err
	}
	defer func() {
		if tail := s.finishInstallationWork(ctx, work); err == nil && tail != nil {
			err = tail
			if out.Validate() == nil {
				committed := f.NewFault(f.DependencyUnavailable, f.Committed).WithCause(tail)
				committed.RetryHint = "lookup"
				err = committed
			}
			out = InstallReceipt{}
		}
	}()
	owner, err := row.owner()
	if err != nil {
		return out, err
	}
	body, err := request.data().pkg.Bytes()
	if err != nil {
		return out, err
	}
	if sum(body) != row.pkg.packageDigest || len(body) != int(row.pkg.size) {
		return out, unavailable(nil)
	}
	prepared, err := state.objects.PreparePayload(ctx, actor, owner, sc.PackageMediaType, int64(row.pkg.size), &row.pkg.packageDigest, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return out, portError(err)
	}
	state.mu.Lock()
	work.installationDiscard = func() error { return state.objects.DiscardPrepared(prepared) }
	state.mu.Unlock()
	material := prepared.Details()
	if prepared.Validate() != nil || material.MediaType != sc.PackageMediaType || material.Length != int64(row.pkg.size) || material.SHA256 != row.pkg.packageDigest {
		return out, unavailable(nil)
	}
	reserve, err := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.ReserveAccess, Actor: actor, Owner: owner, Intent: id.Mutate, Command: &meta, Prepared: prepared})
	if err != nil {
		return out, err
	}
	plan, err := state.objects.DiscoverAccess(ctx, reserve)
	if err != nil {
		return out, portError(err)
	}
	if plan.Validate() != nil || !plan.Details().Request.Equal(reserve) {
		return out, unavailable(nil)
	}
	var attempt oc.UploadAttempt
	err = s.installationTransaction(ctx, actor, row, []oc.AccessLockPlan{plan}, true, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, current installationRow, locked oc.LockedAccess) error {
		if !sameInstallation(current, row) || current.phase != installationPlanned {
			return fault(f.ResourceBusy)
		}
		var e error
		attempt, e = state.objects.ReserveUploadInTx(ctx, tx, actor, owner, meta, prepared, plan, locked)
		if e != nil {
			return portError(e)
		}
		if e = reserveInstallation(ctx, x, current, attempt, state.process); e != nil {
			return e
		}
		row = current
		row.object, row.upload, row.attempt = attempt.Details().ObjectID, attempt.Details().UploadID, attempt.Details().ID
		row.phase, row.version = installationReserved, row.version+1
		return nil
	})
	if err != nil {
		return InstallReceipt{}, err
	}
	verified, err := state.objects.UploadPrepared(ctx, actor, owner, prepared, attempt)
	if err != nil {
		return out, portError(err)
	}
	if verified.Validate() != nil || verified.Details() != attempt.Details() {
		return out, unavailable(nil)
	}
	publish, err := oc.NewOwnerAccess(oc.AccessRequestDetails{Operation: oc.PublishAccess, Actor: actor, Owner: owner, Intent: id.Mutate, Attempt: verified})
	if err != nil {
		return out, err
	}
	plan, err = state.objects.DiscoverAccess(ctx, publish)
	if err != nil {
		return out, portError(err)
	}
	if plan.Validate() != nil || !plan.Details().Request.Equal(publish) {
		return out, unavailable(nil)
	}
	err = s.installationTransaction(ctx, actor, row, []oc.AccessLockPlan{plan}, true, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, current installationRow, locked oc.LockedAccess) error {
		if !sameInstallation(current, row) || current.phase != installationReserved {
			return fault(f.ResourceBusy)
		}
		if e := insertInstallingSkill(ctx, x, current); e != nil {
			return e
		}
		stored, e := state.objects.PublishVerifiedInTx(ctx, tx, actor, owner, verified, plan, locked)
		if e != nil {
			return portError(e)
		}
		if e = publishInstallation(ctx, x, current, stored); e != nil {
			return e
		}
		published := current
		published.phase, published.version = installationPublished, current.version+1
		// The same-Tx canonical current/revision/object mapping, rather than
		// Object availability alone, is the source of the returned receipt.
		if _, _, e = loadInstalled(ctx, x, published); e != nil {
			return e
		}
		out, e = published.receipt()
		return e
	})
	if err != nil {
		return InstallReceipt{}, err
	}
	return out, nil
}

func sameInstallation(a, b installationRow) bool {
	return a.id == b.id && a.project == b.project && a.user == b.user && a.key == b.key && a.skill == b.skill && a.revision == b.revision && a.semantic == b.semantic && a.version == b.version && a.phase == b.phase && a.object == b.object && a.upload == b.upload && a.attempt == b.attempt
}

func installationOutcomePending(_ installationRow) error {
	// A retained reserved command is known to exist, but is not a receipt.
	// Only commitError may report an actual unknown transaction attempt.
	err := fault(f.ResourceBusy)
	err.RetryHint = "lookup"
	return err
}

// LookupInstall observes only the original semantic command under current
// Owner read permission. It never creates a plan or performs Object I/O.
// NotFound is not proof that an earlier unknown transaction rolled back;
// callers must keep the same key and input for any later Install invocation.
func (s *Service) LookupInstall(ctx context.Context, actor id.Actor, project id.ProjectID, key f.IdempotencyKey, request InstallRequest) (InstallReceipt, error) {
	if project.Validate() != nil || key.Validate() != nil || request.Validate() != nil {
		return InstallReceipt{}, invalid()
	}
	user, err := installationActor(actor)
	if err != nil {
		return InstallReceipt{}, err
	}
	call, err := s.begin(ctx, false)
	if err != nil {
		return InstallReceipt{}, err
	}
	defer s.end(call)
	input := request.data()
	semantic, err := installSemantic(project, user, input)
	if err != nil {
		return InstallReceipt{}, err
	}
	command, err := installIdentity(project, key)
	if err != nil {
		return InstallReceipt{}, err
	}
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		return InstallReceipt{}, err
	}
	locks, err := oc.NormalizeAccessLocks([]f.LockRequest{commandLock(command), userLock(user.String(), f.Exclusive), projectLock(project, f.Exclusive), skillLock(input.skill, f.Exclusive)})
	if err != nil {
		return InstallReceipt{}, err
	}
	state := s.state()
	store := state.authority.state().store
	var receipt InstallReceipt
	var callbackErr error
	result := store.WithinTx(call.ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = state.authority.installationOwnerInTx(ctx, tx, actor, project, id.Read); err != nil {
			return err
		}
		row, err := loadInstallation(ctx, x, project, key)
		if err != nil {
			return err
		}
		if row == nil {
			return fault(f.NotFound)
		}
		if row.user != user {
			return fault(f.Forbidden)
		}
		if row.skill != input.skill || row.semantic != semantic {
			return fault(f.IdempotencyKeyReused)
		}
		if row.phase == installationFailed {
			return fault(f.InvalidState)
		}
		if row.phase != installationPublished {
			return installationOutcomePending(*row)
		}
		if _, _, err = loadInstalled(ctx, x, *row); err != nil {
			return err
		}
		receipt, err = row.receipt()
		return err
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return InstallReceipt{}, callbackErr
	}
	if err = commitError(result); err != nil {
		return InstallReceipt{}, err
	}
	return receipt, nil
}

func (s *Service) installedReceipt(ctx context.Context, actor id.Actor, row installationRow) (InstallReceipt, error) {
	var receipt InstallReceipt
	err := s.installationTransaction(ctx, actor, row, nil, false, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, current installationRow, _ oc.LockedAccess) error {
		if !sameInstallation(current, row) || current.phase != installationPublished {
			return fault(f.ResourceBusy)
		}
		if _, _, e := loadInstalled(ctx, x, current); e != nil {
			return e
		}
		var e error
		receipt, e = current.receipt()
		return e
	})
	if err != nil {
		return InstallReceipt{}, err
	}
	return receipt, nil
}

func (s *Service) installationTransaction(ctx context.Context, actor id.Actor, row installationRow, plans []oc.AccessLockPlan, mutate bool, work func(context.Context, f.Tx, postgres.SQLExecutor, installationRow, oc.LockedAccess) error) error {
	state := s.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	user, err := installationActor(actor)
	if err != nil {
		return err
	}
	if row.validate() != nil || user != row.user || work == nil {
		return fault(f.Forbidden)
	}
	locks, err := row.locks(f.Exclusive)
	if err != nil {
		return err
	}
	command, err := row.identity()
	if err != nil {
		return err
	}
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		return err
	}
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
		if err = store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = state.authority.installationOwnerInTx(ctx, tx, actor, row.project, id.Read); err != nil {
			return err
		}
		current, err := loadInstallation(ctx, x, row.project, row.key)
		if err != nil {
			return err
		}
		if current == nil || current.id != row.id || current.user != user || current.semantic != row.semantic || current.skill != row.skill || current.revision != row.revision {
			return fault(f.ResourceBusy)
		}
		if mutate {
			if err = state.authority.installationOwnerInTx(ctx, tx, actor, row.project, id.Mutate); err != nil {
				return err
			}
		}
		return work(ctx, tx, x, *current, locked)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return callbackErr
	}
	return commitError(result)
}

func installationActor(actor id.Actor) (id.UserID, error) {
	if actor.Validate() != nil {
		return id.UserID{}, invalid()
	}
	if actor.Details().Kind == id.AgentRun {
		// The production Tool/Agent execution witness is not implemented here.
		return id.UserID{}, fault(f.DependencyUnbound)
	}
	if actor.Details().Kind != id.Human {
		return id.UserID{}, fault(f.Forbidden)
	}
	user, err := f.ParseID[id.User](actor.Details().UserID)
	if err != nil {
		return id.UserID{}, invalid()
	}
	return user, nil
}

// planInstallation persists an immutable command, not a successful Skill or
// permission to send an Object request. Its caller must own the complete
// physical-call lifetime and use the returned phase/current attempt unchanged.
// Every failure, including an unknown commit, returns no usable plan.
func (a *Authority) planInstallation(ctx context.Context, actor id.Actor, project id.ProjectID, key f.IdempotencyKey, request InstallRequest) (installationRow, error) {
	state := a.state()
	if state == nil {
		return installationRow{}, fault(f.DependencyUnbound)
	}
	if ctx == nil || project.Validate() != nil || key.Validate() != nil || request.Validate() != nil {
		return installationRow{}, invalid()
	}
	if err := ctx.Err(); err != nil {
		return installationRow{}, portError(err)
	}
	user, err := installationActor(actor)
	if err != nil {
		return installationRow{}, err
	}
	input := request.data()
	semantic, err := installSemantic(project, user, input)
	if err != nil {
		return installationRow{}, err
	}
	command, err := installIdentity(project, key)
	if err != nil {
		return installationRow{}, err
	}
	cause, err := f.NewCommandsCause(command)
	if err != nil {
		return installationRow{}, err
	}
	// Project EX freezes the same-domain name catalogue during creation. No
	// sequential lock extension is used after entering the transaction.
	locks, err := oc.NormalizeAccessLocks([]f.LockRequest{commandLock(command), userLock(user.String(), f.Exclusive), projectLock(project, f.Exclusive), skillLock(input.skill, f.Exclusive)})
	if err != nil {
		return installationRow{}, err
	}
	var row installationRow
	var callbackErr error
	result := state.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if err = state.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err = state.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		x, err := state.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = a.installationOwnerInTx(ctx, tx, actor, project, id.Read); err != nil {
			return err
		}
		current, err := loadInstallation(ctx, x, project, key)
		if err != nil {
			return err
		}
		if current != nil {
			if current.user != user {
				return fault(f.Forbidden)
			}
			if current.semantic != semantic || current.skill != input.skill {
				return fault(f.IdempotencyKeyReused)
			}
			if current.phase == installationFailed {
				return fault(f.InvalidState)
			}
			if current.phase != installationPublished {
				if err = a.installationOwnerInTx(ctx, tx, actor, project, id.Mutate); err != nil {
					return err
				}
			}
			row = *current
			return nil
		}
		if err = a.installationOwnerInTx(ctx, tx, actor, project, id.Mutate); err != nil {
			return err
		}
		if err = installationNameAvailable(ctx, x, project, input); err != nil {
			return err
		}
		installation, err := f.NewID[Installation]()
		if err != nil {
			return unavailable(err)
		}
		revision, err := f.NewID[sc.Revision]()
		if err != nil {
			return unavailable(err)
		}
		now, err := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond))
		if err != nil {
			return unavailable(err)
		}
		row = installationRow{id: installation, project: project, user: user, key: key, skill: input.skill, revision: revision,
			semantic: semantic, pkg: freezeInstallation(input), phase: installationPlanned, version: 1, created: now, updated: now}
		return insertInstallation(ctx, x, row)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return installationRow{}, callbackErr
	}
	if err = commitError(result); err != nil {
		return installationRow{}, err
	}
	return row, nil
}

func (a *Authority) installationOwnerInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID, intent id.AccessIntent) error {
	state := a.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	if _, err := installationActor(actor); err != nil {
		return err
	}
	if intent != id.Read && intent != id.Mutate {
		return fault(f.Forbidden)
	}
	grant, err := state.projects.RequireOwnerInTx(ctx, tx, actor, project, intent)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(actor, project) {
		return unavailable(nil)
	}
	return nil
}

func installationNameAvailable(ctx context.Context, x postgres.SQLExecutor, project id.ProjectID, input installInput) error {
	var conflict bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_skill.skills WHERE project_id=$1 AND (id=$2 OR normalized_name=$3)) OR EXISTS(SELECT 1 FROM agenteam_skill.installations WHERE skill_id=$2 OR (project_id=$1 AND normalized_name=$3 AND phase<>'failed'))`, project.String(), input.skill.String(), input.normalized).Scan(&conflict)
	if err != nil {
		return unavailable(err)
	}
	if conflict {
		return fault(f.VersionConflict)
	}
	return nil
}
