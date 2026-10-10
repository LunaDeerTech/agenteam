package project

import (
	"context"
	"encoding/json"

	"time"

	audit "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func updateValues(p c.ProjectRef, r c.UpdateProjectRequest) (c.ProjectRef, []c.ChangedField, error) {
	changed := []c.ChangedField{}
	if r.Description != nil && *r.Description != p.Description {
		p.Description = *r.Description
		changed = append(changed, c.DescriptionChanged)
	}
	if r.Name != nil && *r.Name != p.Name {
		p.Name = *r.Name
		p.NormalizedName, _ = c.NormalizeName(*r.Name)
		changed = append(changed, c.NameChanged)
	}
	if len(changed) > 0 {
		var e error
		p.Version, e = nextVersion(p.Version)
		if e != nil {
			return c.ProjectRef{}, nil, e
		}
	}
	return p, changed, nil
}
func visible(p *projectRecord) error {
	state := c.InitializationPending
	if p.initialized {
		state = c.Initialized
	}
	return c.CheckOwnerGate(p.ref.Lifecycle, state, identity.Read)
}
func semanticReplay(cmd *commandRecord, actor identity.Actor, d foundation.Digest) (*c.ProjectRef, error) {
	if cmd.user != actor.Details().UserID {
		return nil, fault(foundation.NotFound)
	}
	if cmd.semantic != d {
		return nil, fault(foundation.IdempotencyKeyReused)
	}
	if cmd.state == "completed" {
		if cmd.result == nil || cmd.result.Project == nil {
			return nil, unavailable(nil)
		}
		v := *cmd.result.Project
		return &v, nil
	}
	return nil, nil
}
func (s *Service) UpdateProject(ctx context.Context, actor identity.Actor, meta foundation.CommandMeta, id c.ProjectID, request c.UpdateProjectRequest) (c.ProjectRef, error) {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return c.ProjectRef{}, e
	}
	defer done()
	if e = human(actor); e != nil {
		return c.ProjectRef{}, e
	}
	if request.Scheduler != nil {
		config := request.Scheduler.Clone()
		request.Scheduler = &config
	}
	semantic, e := c.UpdateDigest(actor, meta, id, request)
	if e != nil {
		return c.ProjectRef{}, e
	}
	command, _ := c.CommandIdentity(id, c.UpdateCommand, meta.IdempotencyKey)
	locks := []foundation.LockRequest{commandLock(command), userLock(actor.Details().UserID, foundation.Exclusive), projectLock(id, foundation.Exclusive)}
	if request.Scheduler != nil {
		locks = append(locks, schedulerLock(id))
	}
	var planned *commandRecord
	var replay *c.ProjectRef
	result := s.state().store.WithinTx(ctx, commandCause(command), func(ctx context.Context, tx foundation.Tx) error {
		planned = nil
		replay = nil
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := s.state().deps.Authority.current(ctx, tx, actor, id, foundation.Exclusive)
		if e != nil {
			return e
		}
		p, e := ownerProject(ctx, x, actor, id)
		if e != nil {
			return e
		}
		if e = visible(p); e != nil {
			return e
		}
		cmd, e := loadCommand(ctx, x, id, c.UpdateCommand, meta.IdempotencyKey)
		if e != nil {
			return e
		}
		if cmd != nil {
			replay, e = semanticReplay(cmd, actor, semantic)
			if e != nil {
				return e
			}
			if replay != nil {
				return nil
			}
			planned = cmd
			return nil
		}
		if e = c.CheckOwnerGate(p.ref.Lifecycle, c.Initialized, identity.Mutate); e != nil {
			return e
		}
		if p.ref.Version != *meta.ExpectedVersion {
			return fault(foundation.VersionConflict)
		}
		ref, changed, e := updateValues(p.ref, request)
		if e != nil {
			return e
		}
		var scheduler *c.ProjectSchedulerConfig
		if request.Scheduler != nil {
			previous, err := loadSchedulerConfig(ctx, x, id)
			if err != nil {
				return err
			}
			fields := schedulerChangedFields(previous, *request.Scheduler)
			if len(fields) != 0 {
				if len(changed) == 0 {
					ref.Version, e = nextVersion(p.ref.Version)
					if e != nil {
						return e
					}
				}
				changed = append(changed, fields...)
				value := request.Scheduler.Clone()
				scheduler = &value
			}
		}
		if ref.NormalizedName != p.ref.NormalizedName {
			if e = s.requireAvailableName(ctx, tx, x, p.ref.OwnerUserID, id, ref.NormalizedName); e != nil {
				return e
			}
		}
		now, e := dbNow(ctx, x)
		if e != nil {
			return e
		}
		cmdID, e := foundation.NewID[struct{}]()
		if e != nil {
			return unavailable(e)
		}
		if len(changed) == 0 {
			saved := c.CommandResult{Command: c.UpdateCommand, Project: &ref}
			raw, e := json.Marshal(saved)
			if e != nil {
				return unavailable(e)
			}
			if _, e = x.Exec(ctx, `INSERT INTO agenteam_project.commands(id,project_id,actor_user_id,command_name,key,semantic_digest,state,safe_result,created_at,committed_at,audit_cause) VALUES($1,$2,$3,'update',$4,$5,'completed',$6::jsonb,$7,$7,$8)`, cmdID.String(), id.String(), actor.Details().UserID, string(meta.IdempotencyKey), semantic.String(), raw, now.Time(), commandAuditCause(command)); e != nil {
				return unavailable(e)
			}
			if e = s.state().deps.Activity.TouchActivityInTx(ctx, tx, actor); e != nil {
				return portError(e)
			}
			replay = &ref
			return nil
		}
		ref.UpdatedAt = now
		ev, e := s.updateEvent(ref, changed, now)
		if e != nil {
			return e
		}
		header, e := ev.HeaderJSON()
		if e != nil {
			return e
		}
		plan := updatePlan{ExpectedVersion: p.ref.Version, Project: ref, Changed: changed, Header: header, Payload: ev.PayloadBytes(), Scheduler: scheduler}
		raw, e := json.Marshal(plan)
		if e != nil {
			return unavailable(e)
		}
		eventID := ev.Header().EventID.String()
		eventIDs, _ := json.Marshal([]string{eventID})
		if _, e = x.Exec(ctx, `INSERT INTO agenteam_project.commands(id,project_id,actor_user_id,command_name,key,semantic_digest,state,plan,event_id,created_at,audit_cause,event_ids) VALUES($1,$2,$3,'update',$4,$5,'planned',$6::jsonb,$7,$8,$9,$10::jsonb)`, cmdID.String(), id.String(), actor.Details().UserID, string(meta.IdempotencyKey), semantic.String(), raw, eventID, now.Time(), commandAuditCause(command), eventIDs); e != nil {
			return unavailable(e)
		}
		planned = &commandRecord{id: cmdID.String(), project: id, user: actor.Details().UserID, name: c.UpdateCommand, key: meta.IdempotencyKey, semantic: semantic, state: "planned", plan: &plan, eventID: &eventID}
		return nil
	})
	if result.State() == foundation.Unknown {
		// Resolve the planning attempt under the same writer locks. Its absence
		// proves only that this planning attempt did not commit; no new plan is
		// generated inside the uncertain call.
		lookup, err := s.lookupAfterUnknown(ctx, actor, c.CommandLookupRequest{ProjectID: id, Command: c.UpdateCommand, Key: meta.IdempotencyKey}, result, semantic)
		if err != nil {
			return c.ProjectRef{}, err
		}
		if lookup.State == c.LookupCommitted && lookup.Result != nil && lookup.Result.Project != nil {
			return *lookup.Result.Project, nil
		}
		if lookup.State == c.LookupNotObserved {
			return c.ProjectRef{}, notCommittedAfterUnknown(result)
		}
		// A saved plan is not a completed business result, and its presence
		// cannot be reported as a rolled-back planning attempt.
		return c.ProjectRef{}, commitError(result)
	}
	if e = commitError(result); e != nil {
		return c.ProjectRef{}, e
	}
	if replay != nil {
		return *replay, nil
	}
	if planned == nil || planned.plan == nil {
		return c.ProjectRef{}, unavailable(nil)
	}
	header, e := event.DecodeHeader(planned.plan.Header)
	if e != nil {
		return c.ProjectRef{}, unavailable(e)
	}
	ev, e := s.state().deps.ProjectEvents.Restore(header, planned.plan.Payload)
	if e != nil {
		return c.ProjectRef{}, unavailable(e)
	}
	eventPlan, e := s.state().deps.Events.PrepareAppend(ctx, actor, ev)
	if e != nil {
		return c.ProjectRef{}, portError(e)
	}
	locks = append(locks, eventPlan.Locks()...)
	var output c.ProjectRef
	result = s.state().store.WithinTx(ctx, commandCause(command), func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, locks); e != nil {
			return unavailable(e)
		}
		x, e := s.state().deps.Authority.current(ctx, tx, actor, id, foundation.Exclusive)
		if e != nil {
			return e
		}
		p, e := ownerProject(ctx, x, actor, id)
		if e != nil {
			return e
		}
		if e = visible(p); e != nil {
			return e
		}
		current, e := loadCommand(ctx, x, id, c.UpdateCommand, meta.IdempotencyKey)
		if e != nil {
			return e
		}
		if current == nil {
			return fault(foundation.ResourceBusy)
		}
		replay, e := semanticReplay(current, actor, semantic)
		if e != nil {
			return e
		}
		if replay != nil {
			output = *replay
			return nil
		}
		if current.id != planned.id || !sameUpdatePlan(current.plan, planned.plan) {
			return fault(foundation.ResourceBusy)
		}
		if e = c.CheckOwnerGate(p.ref.Lifecycle, c.Initialized, identity.Mutate); e != nil {
			return e
		}
		if p.ref.Version != current.plan.ExpectedVersion {
			return fault(foundation.VersionConflict)
		}
		next := current.plan.Project
		if next.NormalizedName != p.ref.NormalizedName {
			// Another Project may reserve this name after planning commits.
			// Recheck under the final complete lock union before any mutation.
			if e = s.requireAvailableName(ctx, tx, x, p.ref.OwnerUserID, id, next.NormalizedName); e != nil {
				return e
			}
		}
		tag, e := x.Exec(ctx, `UPDATE agenteam_project.projects SET name=$2,normalized_name=$3,description=$4,version=$5,updated_at=$6 WHERE id=$1 AND version=$7 AND lifecycle='active' AND initialized_at IS NOT NULL`, id.String(), next.Name, next.NormalizedName, next.Description, int64(next.Version), next.UpdatedAt.Time(), int64(current.plan.ExpectedVersion))
		if e != nil {
			return uniqueName(e)
		}
		if tag.RowsAffected() != 1 {
			return fault(foundation.VersionConflict)
		}
		if current.plan.Scheduler != nil {
			if e = s.state().store.RequireHeldLocks(ctx, tx, []foundation.LockRequest{schedulerLock(id)}); e != nil {
				return preparationProjectError(e)
			}
			config := current.plan.Scheduler
			tag, e = x.Exec(ctx, `UPDATE agenteam_project.projects SET scheduler_enabled=$2,scheduler_max_concurrency=$3 WHERE id=$1 AND version=$4`, id.String(), config.Enabled, config.MaxConcurrency, int64(next.Version))
			if e = affected(tag, e); e != nil {
				return e
			}
		}
		if e = s.appendUpdateAudit(ctx, tx, actor, current); e != nil {
			return e
		}
		if _, e = s.state().deps.Events.AppendEventInTx(ctx, tx, actor, ev, eventPlan); e != nil {
			return portError(e)
		}
		saved := c.CommandResult{Command: c.UpdateCommand, Project: &next}
		raw, e := json.Marshal(saved)
		if e != nil {
			return unavailable(e)
		}
		tag, e = x.Exec(ctx, `UPDATE agenteam_project.commands SET state='completed',safe_result=$2::jsonb,committed_at=clock_timestamp() WHERE id=$1 AND state='planned'`, current.id, raw)
		if e = affected(tag, e); e != nil {
			return e
		}
		if e = s.state().deps.Activity.TouchActivityInTx(ctx, tx, actor); e != nil {
			return portError(e)
		}
		output = next
		return nil
	})
	if result.State() == foundation.Unknown {
		lookup, e := s.lookupAfterUnknown(ctx, actor, c.CommandLookupRequest{ProjectID: id, Command: c.UpdateCommand, Key: meta.IdempotencyKey}, result, semantic)
		if e != nil {
			return c.ProjectRef{}, e
		}
		if lookup.State == c.LookupCommitted && lookup.Result != nil && lookup.Result.Project != nil {
			return *lookup.Result.Project, nil
		}
		if lookup.State == c.LookupInProgress || lookup.State == c.LookupNotObserved {
			// This is the final mutation attempt: the same canonical plan still
			// being incomplete after writer serialization proves no final commit.
			// The durable plan itself remains available to the same-key retry.
			return c.ProjectRef{}, notCommittedAfterUnknown(result)
		}
		return c.ProjectRef{}, commitError(result)
	}
	if e = commitError(result); e != nil {
		return c.ProjectRef{}, e
	}
	return output, nil
}
func (s *Service) LookupCommand(ctx context.Context, actor identity.Actor, request c.CommandLookupRequest) (c.CommandLookupResult, error) {
	ctx, done, e := s.begin(ctx)
	if e != nil {
		return c.CommandLookupResult{}, e
	}
	defer done()
	return s.lookup(ctx, actor, request, nil)
}
func (s *Service) lookup(ctx context.Context, actor identity.Actor, request c.CommandLookupRequest, expected *foundation.Digest) (c.CommandLookupResult, error) {
	if e := human(actor); e != nil {
		return c.CommandLookupResult{}, e
	}
	if e := request.Validate(); e != nil {
		return c.CommandLookupResult{}, e
	}
	if request.Command == c.ArchiveCommand || request.Command == c.DeleteCommand {
		return s.lookupLifecycle(ctx, actor, request, expected)
	}
	if request.Command != c.CreateCommand && request.Command != c.UpdateCommand {
		return c.CommandLookupResult{}, fault(foundation.DependencyUnbound)
	}
	command, _ := c.CommandIdentity(request.ProjectID, request.Command, request.Key)
	var output c.CommandLookupResult
	result := s.state().store.WithinTx(ctx, commandCause(command), func(ctx context.Context, tx foundation.Tx) error {
		if e := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(command), userLock(actor.Details().UserID, foundation.Shared), projectLock(request.ProjectID, foundation.Exclusive)}); e != nil {
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
		if p == nil {
			owner, e := deletedOwner(ctx, x, request.ProjectID)
			if e != nil {
				return e
			}
			if owner != "" {
				if owner == actor.Details().UserID {
					return fault(foundation.ResourceDeleted)
				}
				return fault(foundation.NotFound)
			}
			// Original writer serialization is already held. No reservation and
			// no tombstone is a confirmed absence, not a pre-lock SELECT guess.
			output = c.CommandLookupResult{State: c.LookupNotObserved}
			return nil
		}
		if p.ref.OwnerUserID.String() != actor.Details().UserID {
			return fault(foundation.NotFound)
		}
		if request.Command == c.CreateCommand {
			r, e := projectCreation(ctx, x, request.ProjectID)
			if e != nil {
				return e
			}
			if r == nil {
				return unavailable(nil)
			}
			if r.key != request.Key {
				return codedField(foundation.ResourceBusy, "/project_id", "TARGET_OCCUPIED")
			}
			if expected != nil && r.semantic != *expected {
				return fault(foundation.IdempotencyKeyReused)
			}
			cr, e := creationResult(r, p)
			if e != nil {
				return e
			}
			cmd := c.CommandResult{Command: c.CreateCommand, Creation: &cr}
			output = c.CommandLookupResult{State: c.LookupCommitted, Result: &cmd}
			return nil
		}
		if e = visible(p); e != nil {
			return e
		}
		cmd, e := loadCommand(ctx, x, request.ProjectID, request.Command, request.Key)
		if e != nil {
			return e
		}
		if cmd == nil {
			output = c.CommandLookupResult{State: c.LookupNotObserved}
			return nil
		}
		if cmd.user != actor.Details().UserID {
			return fault(foundation.NotFound)
		}
		if expected != nil && cmd.semantic != *expected {
			return fault(foundation.IdempotencyKeyReused)
		}
		if cmd.state == "planned" {
			output = c.CommandLookupResult{State: c.LookupInProgress}
			return nil
		}
		output = c.CommandLookupResult{State: c.LookupCommitted, Result: cmd.result}
		return nil
	})
	if e := commitError(result); e != nil {
		return c.CommandLookupResult{}, e
	}
	return output, nil
}
func (s *Service) lookupAfterUnknown(ctx context.Context, actor identity.Actor, request c.CommandLookupRequest, original foundation.CommitResult, semantic foundation.Digest) (c.CommandLookupResult, error) {
	// Cancellation ends the HTTP wait; a short separate observation can still
	// establish the original outcome. Its timeout is never negative evidence.
	confirm, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	output, e := s.lookup(confirm, actor, request, &semantic)
	if e != nil {
		return c.CommandLookupResult{}, commitError(original)
	}
	return output, nil
}
func (s *Service) appendUpdateAudit(ctx context.Context, tx foundation.Tx, actor identity.Actor, r *commandRecord) error {
	changed := make([]audit.ProjectChangedField, len(r.plan.Changed))
	for i, v := range r.plan.Changed {
		changed[i] = audit.ProjectChangedField(v)
	}
	metadata, e := audit.ProjectMetadata(audit.ProjectUpdate, audit.ProjectMetadataFields{ProjectID: r.project.String(), InitiatorID: r.user, ProjectVersion: r.plan.Project.Version, ChangedFields: changed})
	if e != nil {
		return e
	}
	resource, e := audit.NewResource(audit.ProjectResource, r.project.String())
	if e != nil {
		return e
	}
	scope, _ := identity.InProject(r.project)
	entry, e := audit.NewEntry(audit.EntryFields{Scope: scope, Actor: actor, Action: audit.ProjectUpdate, Outcome: audit.Success, Resource: resource, Metadata: metadata})
	if e != nil {
		return e
	}
	key, e := auditCommandKey(r.identity(), 0)
	if e != nil {
		return e
	}
	_, e = s.state().deps.Audit.AppendInTx(ctx, tx, entry, key)
	return portError(e)
}
func auditCommandKey(id foundation.CommandIdentity, ordinal int64) (audit.AppendKey, error) {
	// Same canonical SHA-256 binding as audit.CommandAppendKey, without making
	// the business service depend on the Audit implementation package.
	return audit.NewAppendKey(audit.ProjectProducer, commandAuditCause(id), ordinal)
}
func commandAuditCause(id foundation.CommandIdentity) string {
	d, _ := cursor.Digest([]byte(id.Canonical()))
	return d.String()
}

func sameUpdatePlan(a, b *updatePlan) bool {
	if a == nil || b == nil {
		return a == b
	}
	x, e := json.Marshal(a)
	if e != nil {
		return false
	}
	y, e := json.Marshal(b)
	if e != nil {
		return false
	}
	xd, e := cursor.Digest(x)
	if e != nil {
		return false
	}
	yd, e := cursor.Digest(y)
	return e == nil && xd == yd
}
