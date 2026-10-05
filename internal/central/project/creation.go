package project

import (
	"context"
	"encoding/json"
	"errors"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/jackc/pgx/v5"
)

func (s *Service) CreateProject(ctx context.Context, actor identity.Actor, meta foundation.CommandMeta, request c.CreateProjectRequest) (c.CreationResult, error) {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return c.CreationResult{}, e
	}
	defer done()
	if e = human(actor); e != nil {
		return c.CreationResult{}, e
	}
	semantic, e := c.CreateDigest(actor, meta, request)
	if e != nil {
		return c.CreationResult{}, e
	}
	command, _ := c.CommandIdentity(request.ProjectID, c.CreateCommand, meta.IdempotencyKey)
	var saved *creationRecord
	var output c.CreationResult
	reserved := false
	defer func() {
		if reserved {
			s.releaseSlot()
		}
	}()
	result := s.state().store.WithinTx(ctx, commandCause(command), func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(command), userLock(actor.Details().UserID, foundation.Exclusive), projectLock(request.ProjectID, foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().deps.Authority.current(ctx, tx, actor, request.ProjectID, foundation.Exclusive)
		if e != nil {
			return e
		}
		p, e := loadProject(ctx, x, request.ProjectID)
		if e != nil {
			return e
		}
		owner, e := deletedOwner(ctx, x, request.ProjectID)
		if e != nil {
			return e
		}
		if p != nil && p.ref.OwnerUserID.String() != actor.Details().UserID {
			return fault(foundation.NotFound)
		}
		if owner != "" {
			if owner == actor.Details().UserID {
				return fault(foundation.ResourceDeleted)
			}
			return fault(foundation.NotFound)
		}
		if p != nil {
			if p.ref.OwnerUserID.String() != actor.Details().UserID {
				return fault(foundation.NotFound)
			}
			r, e := projectCreation(ctx, x, request.ProjectID)
			if e != nil {
				return e
			}
			if r == nil {
				return unavailable(nil)
			}
			if r.key != meta.IdempotencyKey {
				return codedField(foundation.ResourceBusy, "/project_id", "TARGET_OCCUPIED")
			}
			if p.ref.Lifecycle == c.Deleting {
				return fault(foundation.ResourceDeleted)
			}
			if r.semantic != semantic {
				return fault(foundation.IdempotencyKeyReused)
			}
			output, e = creationResult(r, p)
			if e != nil {
				return e
			}
			saved = r
			// Authorized completed receipts precede changed binding/capacity.
			if output.State == c.CreationReady {
				return nil
			}
			if nilPort(s.state().deps.Initializer) {
				return fault(foundation.DependencyUnbound)
			}
			if !s.slot() {
				return fault(foundation.ResourceBusy)
			}
			reserved = true
			return nil
		}
		if nilPort(s.state().deps.Initializer) {
			return fault(foundation.DependencyUnbound)
		}
		if !s.slot() {
			return fault(foundation.ResourceBusy)
		}
		reserved = true
		now, e := dbNow(ctx, x)
		if e != nil {
			return e
		}
		creation, e := foundation.NewID[c.Creation]()
		if e != nil {
			return unavailable(e)
		}
		eventID, e := foundation.NewID[event.EventIdentity]()
		if e != nil {
			return unavailable(e)
		}
		ownerID, e := parseID[identity.User](actor.Details().UserID)
		if e != nil {
			return e
		}
		normalized, _ := c.NormalizeName(request.Name)
		if e = s.requireAvailableName(ctx, tx, x, ownerID, request.ProjectID, normalized); e != nil {
			return e
		}
		initializationKey := foundation.IdempotencyKey("project-init-" + creation.String())
		p = &projectRecord{ref: c.ProjectRef{ID: request.ProjectID, OwnerUserID: ownerID, Name: request.Name, NormalizedName: normalized, Description: request.Description, Lifecycle: c.Active, Version: 1, CreatedAt: now, UpdatedAt: now}, creation: creation}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_project.projects(id,owner_user_id,name,normalized_name,description,lifecycle,version,created_at,updated_at,creation_id) VALUES($1,$2,$3,$4,$5,'active',1,$6,$6,$7)`, request.ProjectID.String(), ownerID.String(), request.Name, normalized, request.Description, now.Time(), creation.String()); e != nil {
			return uniqueName(e)
		}
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_project.creations(id,project_id,owner_user_id,command_key,semantic_digest,request_name,request_description,state,initialization_key,version,created_at,updated_at,event_id) VALUES($1,$2,$3,$4,$5,$6,$7,'accepted',$8,1,$9,$9,$10)`, creation.String(), request.ProjectID.String(), ownerID.String(), string(meta.IdempotencyKey), semantic.String(), request.Name, request.Description, string(initializationKey), now.Time(), eventID.String()); e != nil {
			return unavailable(e)
		}
		saved = &creationRecord{operation: c.CreationOperation{ID: creation, ProjectID: request.ProjectID, State: c.CreationAccepted, Version: 1, CreatedAt: now, UpdatedAt: now}, owner: ownerID, key: meta.IdempotencyKey, semantic: semantic, initializationKey: initializationKey, requestName: &request.Name, requestDescription: &request.Description, eventID: eventID.String()}
		if e = s.appendCreationAudit(ctx, tx, actor, saved, audit.ProjectCreateAccepted); e != nil {
			return e
		}
		if e = s.state().deps.Activity.TouchActivityInTx(ctx, tx, actor); e != nil {
			return portError(e)
		}
		output, e = creationResult(saved, p)
		return e
	})
	if result.State() == foundation.Unknown {
		lookup, e := s.lookupAfterUnknown(ctx, actor, c.CommandLookupRequest{ProjectID: request.ProjectID, Command: c.CreateCommand, Key: meta.IdempotencyKey}, result, semantic)
		if e != nil {
			return c.CreationResult{}, e
		}
		if lookup.State == c.LookupCommitted && lookup.Result != nil && lookup.Result.Creation != nil {
			return creationRoundResult(*lookup.Result.Creation, commitFailure{result})
		}
		if lookup.State == c.LookupNotObserved {
			return c.CreationResult{}, notCommittedAfterUnknown(result)
		}
		return c.CreationResult{}, commitError(result)
	}
	if e = commitError(result); e != nil {
		return c.CreationResult{}, e
	}
	if output.State == c.CreationReady {
		return output, nil
	}
	// The durable acceptance survives a disconnected request. This round may
	// stop waiting; later retries/recovery retain the exact IDs and init key.
	if e = ctx.Err(); e != nil {
		return output, nil
	}
	advanced, e := s.advanceCreation(ctx, saved.operation.ID)
	if e != nil {
		var f *foundation.Fault
		if errors.As(e, &f) && (f.CommitState == foundation.Unknown || f.CommitState == foundation.Committed) {
			// A confirmed failed checkpoint is a safe error with the durable
			// acceptance preserved; it is not another successful pending reply.
			return c.CreationResult{}, e
		}
		// Acceptance already committed. A later failed attempt cannot relabel
		// the whole Create as not committed; its original operation remains.
		return output, nil
	}
	return advanced, nil
}
func (s *Service) GetCreation(ctx context.Context, actor identity.Actor, id c.CreationID) (c.CreationResult, error) {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return c.CreationResult{}, e
	}
	defer done()
	if e = human(actor); e != nil {
		return c.CreationResult{}, e
	}
	if id.Validate() != nil {
		return c.CreationResult{}, invalid()
	}
	// Prescan only discovers the lock target; not a current authorization.
	var candidateID string
	e = s.state().store.QueryRow(ctx, `SELECT project_id::text FROM agenteam_project.creations WHERE id=$1`, id.String()).Scan(&candidateID)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return c.CreationResult{}, unavailable(e)
	}
	if candidateID == "" {
		cause, e := readCause("creation-missing")
		if e != nil {
			return c.CreationResult{}, e
		}
		result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
			if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared)}); e != nil {
				return unavailable(e)
			}
			if e := s.state().deps.Authority.state().sessions.RequireCurrentSession(ctx, tx, actor); e != nil {
				return portError(e)
			}
			return fault(foundation.NotFound)
		})
		return c.CreationResult{}, commitError(result)
	}
	candidateProject, e := parseID[identity.Project](candidateID)
	if e != nil {
		return c.CreationResult{}, e
	}
	cause, e := readCause("creation-get")
	if e != nil {
		return c.CreationResult{}, e
	}
	var output c.CreationResult
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared), projectLock(candidateProject, foundation.Shared)}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().deps.Authority.current(ctx, tx, actor, candidateProject, foundation.Shared)
		if e != nil {
			return e
		}
		r, e := loadCreation(ctx, x, id)
		if e != nil {
			return e
		}
		if r == nil || r.owner.String() != actor.Details().UserID || r.operation.ProjectID != candidateProject {
			return fault(foundation.NotFound)
		}
		p, e := ownerProject(ctx, x, actor, r.operation.ProjectID)
		if e != nil {
			return e
		}
		output, e = creationResult(r, p)
		return e
	})
	if e = commitError(result); e != nil {
		return c.CreationResult{}, e
	}
	return output, nil
}
func (s *Service) initializationActor(r *creationRecord) (identity.Actor, error) {
	scope, e := identity.InProject(r.operation.ProjectID)
	if e != nil {
		return identity.Actor{}, e
	}
	return s.state().initRegistration.Actor(r.operation.ID.String(), scope)
}
func (s *Service) advanceCreation(ctx context.Context, id c.CreationID) (c.CreationResult, error) {
	if nilPort(s.state().deps.Initializer) {
		return c.CreationResult{}, fault(foundation.DependencyUnbound)
	}
	r, claim, e := s.claimCreation(ctx, id)
	if e != nil {
		return c.CreationResult{}, e
	}
	if claim == nil {
		p, e := loadProject(ctx, s.state().store, r.operation.ProjectID)
		if e != nil {
			return c.CreationResult{}, e
		}
		return creationResult(r, p)
	}
	defer func() {
		if !claim.terminalKnown {
			s.joined(claim.attempt)
		}
	}()
	actor, e := s.initializationActor(r)
	if e != nil {
		return c.CreationResult{}, e
	}
	request := r.request()
	// Inspect the original provider checkpoint on every round, including after
	// ambiguous results. Initialize must itself be idempotent for this key.
	inspected, e := s.state().deps.Initializer.InspectProjectSkills(ctx, actor, request)
	if e != nil {
		return s.finishCreationRound(ctx, r, claim, reasonFor(e), e)
	}
	if !inspected.Matches(request) {
		return s.finishCreationRound(ctx, r, claim, c.ReasonOperationFailed, invalid())
	}
	if inspected.State != c.InitializationCompleted {
		inspected, e = s.state().deps.Initializer.InitializeProjectSkills(ctx, actor, request)
		if e != nil {
			return s.finishCreationRound(ctx, r, claim, reasonFor(e), e)
		}
		if !inspected.Matches(request) {
			return s.finishCreationRound(ctx, r, claim, c.ReasonOperationFailed, invalid())
		}
	}
	if inspected.State != c.InitializationCompleted {
		if inspected.State == c.InitializationResultPending {
			return s.checkpointCreationRound(ctx, r, claim, c.CreationInitializing, inspected.SafeReason, nil)
		}
		return s.finishCreationRound(ctx, r, claim, inspected.SafeReason, nil)
	}
	confirmation, e := s.state().deps.Initializer.DiscoverConfirmation(ctx, actor, request)
	if e != nil {
		return s.finishCreationRound(ctx, r, claim, reasonFor(e), e)
	}
	proposed := confirmation.ProposedReceipt()
	if !proposed.Matches(request) || inspected.AddSkillsID == nil || inspected.Revision == nil || proposed.AddSkillsID != *inspected.AddSkillsID || proposed.Revision != *inspected.Revision {
		return s.finishCreationRound(ctx, r, claim, c.ReasonOperationFailed, invalid())
	}
	// Persist the completion event plan before asking Outbox to discover it.
	r, e = s.planCreationEvent(ctx, r, claim)
	if e != nil {
		return c.CreationResult{}, e
	}
	header, e := event.DecodeHeader(r.eventHeader)
	if e != nil {
		return c.CreationResult{}, unavailable(e)
	}
	ev, e := s.state().deps.ProjectEvents.Restore(header, r.eventPayload)
	if e != nil {
		return c.CreationResult{}, e
	}
	eventPlan, e := s.state().deps.Events.PrepareAppend(ctx, actor, ev)
	if e != nil {
		return s.finishCreationRound(ctx, r, claim, reasonFor(e), e)
	}
	locks := append([]foundation.LockRequest{commandLock(r.identity()), projectLock(r.operation.ProjectID, foundation.Exclusive)}, confirmation.RequiredLocks()...)
	locks = append(locks, eventPlan.Locks()...)
	var output c.CreationResult
	result := s.state().store.WithinTx(ctx, claim.cause(), func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if e = checkClaim(ctx, x, *claim); e != nil {
			return e
		}
		current, e := loadCreation(ctx, x, id)
		if e != nil {
			return e
		}
		if current == nil || current.operation.ProjectID != r.operation.ProjectID || current.initializationKey != r.initializationKey {
			return fault(foundation.NotFound)
		}
		if e = s.state().deps.Authority.ValidateInitializationInTx(ctx, tx, actor, id, r.operation.ProjectID, r.initializationKey); e != nil {
			return e
		}
		p, e := loadProject(ctx, x, r.operation.ProjectID)
		if e != nil {
			return e
		}
		if p == nil {
			return fault(foundation.NotFound)
		}
		if current.operation.State == c.CreationCompleted {
			output, e = creationResult(current, p)
			return e
		}
		receipt, e := s.state().deps.Initializer.ConfirmInitializedInTx(ctx, tx, actor, request, confirmation)
		if e != nil {
			return portError(e)
		}
		if !receipt.Matches(request) || receipt != proposed {
			return fault(foundation.Forbidden)
		}
		now, e := dbNow(ctx, x)
		if e != nil {
			return e
		}
		version, e := nextVersion(current.operation.Version)
		if e != nil {
			return e
		}
		// Ready Project stays version one. No available empty shell is exposed.
		if p.ref.Version != 1 || p.initialized || p.ref.Lifecycle != c.Active {
			return fault(foundation.InvalidState)
		}
		p.ref.UpdatedAt = now
		p.initialized = true
		raw, e := json.Marshal(p.ref)
		if e != nil {
			return e
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_project.projects SET initialized_at=$2,updated_at=$2 WHERE id=$1 AND initialized_at IS NULL AND lifecycle='active' AND version=1`, p.ref.ID.String(), now.Time())
		if e = affected(tag, e); e != nil {
			return e
		}
		tag, e = x.Exec(ctx, `UPDATE agenteam_project.creations SET state='completed',version=$2,updated_at=$3,safe_reason=NULL,request_name=NULL,request_description=NULL,protected_skill_id=$4,protected_revision=$5,safe_result=$6::jsonb WHERE id=$1 AND state='initializing'`, id.String(), int64(version), now.Time(), receipt.AddSkillsID.String(), int64(receipt.Revision), raw)
		if e = affected(tag, e); e != nil {
			return e
		}
		current.operation.State = c.CreationCompleted
		current.operation.Version = version
		current.operation.UpdatedAt = now
		current.operation.SafeReason = ""
		current.protectedSkill = &receipt.AddSkillsID
		current.revision = &receipt.Revision
		current.result = &p.ref
		current.requestName = nil
		current.requestDescription = nil
		if e = s.appendCreationAudit(ctx, tx, actor, current, audit.ProjectCreateCompleted); e != nil {
			return e
		}
		if _, e = s.state().deps.Events.AppendEventInTx(ctx, tx, actor, ev, eventPlan); e != nil {
			return portError(e)
		}
		if e = terminalClaim(ctx, x, *claim); e != nil {
			return e
		}
		output, e = creationResult(current, p)
		return e
	})
	if result.State() == foundation.Unknown {
		return s.confirmCreationUnknown(ctx, r, claim, result)
	}
	if e = commitError(result); e != nil {
		return s.finishCreationRound(ctx, r, claim, reasonFor(e), e)
	}
	claim.terminalKnown = true
	return output, nil
}
func (s *Service) planCreationEvent(ctx context.Context, r *creationRecord, claim *workClaim) (*creationRecord, error) {
	var output *creationRecord
	result := s.state().store.WithinTx(ctx, claim.cause(), func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(r.identity()), projectLock(r.operation.ProjectID, foundation.Exclusive)}); e != nil {
			return unavailable(e)
		}
		x, e := s.state().store.InTx(tx)
		if e != nil {
			return unavailable(e)
		}
		if e = checkClaim(ctx, x, *claim); e != nil {
			return e
		}
		current, e := loadCreation(ctx, x, r.operation.ID)
		if e != nil {
			return e
		}
		if current == nil || current.operation.State != c.CreationInitializing {
			return fault(foundation.InvalidState)
		}
		if len(current.eventHeader) == 0 {
			now, e := dbNow(ctx, x)
			if e != nil {
				return e
			}
			ev, e := s.createdEvent(current, now)
			if e != nil {
				return e
			}
			header, e := ev.HeaderJSON()
			if e != nil {
				return e
			}
			tag, e := x.Exec(ctx, `UPDATE agenteam_project.creations SET event_header=$2::jsonb,event_payload=$3::jsonb WHERE id=$1 AND event_header IS NULL`, current.operation.ID.String(), header, ev.PayloadBytes())
			if e = affected(tag, e); e != nil {
				return e
			}
			current.eventHeader = header
			current.eventPayload = ev.PayloadBytes()
		}
		output = current
		return nil
	})
	if e := commitError(result); e != nil {
		return nil, e
	}
	return output, nil
}
func reasonFor(err error) c.SafeReason {
	var f *foundation.Fault
	if errors.As(err, &f) {
		if f.CommitState == foundation.Unknown {
			return c.ReasonOutcomeUnknown
		}
		switch f.Code {
		case foundation.DependencyUnbound:
			return c.ReasonDependencyUnbound
		case foundation.CommitUnknown:
			return c.ReasonOutcomeUnknown
		}
	}
	return c.ReasonDependencyUnavailable
}
func (s *Service) appendCreationAudit(ctx context.Context, tx foundation.Tx, actor identity.Actor, r *creationRecord, action audit.Action) error {
	version := r.operation.Version
	metadata, e := audit.ProjectMetadata(action, audit.ProjectMetadataFields{ProjectID: r.operation.ProjectID.String(), InitiatorID: r.owner.String(), ProjectVersion: 1, CreationID: r.operation.ID.String(), CreationVersion: &version})
	if e != nil {
		return e
	}
	resource, e := audit.NewResource(audit.ProjectCreationResource, r.operation.ID.String())
	if e != nil {
		return e
	}
	scope, _ := identity.InProject(r.operation.ProjectID)
	entry, e := audit.NewEntry(audit.EntryFields{Scope: scope, Actor: actor, Action: action, Outcome: audit.Success, Resource: resource, Metadata: metadata})
	if e != nil {
		return e
	}
	var key audit.AppendKey
	if action == audit.ProjectCreateAccepted {
		key, e = auditCommandKey(r.identity(), 0)
	} else {
		key, e = audit.NewAppendKey(audit.ProjectProducer, r.operation.ID.String(), 0)
	}
	if e != nil {
		return e
	}
	_, e = s.state().deps.Audit.AppendInTx(ctx, tx, entry, key)
	return portError(e)
}
