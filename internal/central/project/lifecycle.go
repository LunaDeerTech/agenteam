package project

import (
	"context"
	"encoding/json"

	event "github.com/LunaDeerTech/agenteam/internal/central/event/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func (s *Service) BeginArchive(ctx context.Context, actor identity.Actor, meta foundation.CommandMeta, project c.ProjectID) (c.LifecycleOperation, error) {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return c.LifecycleOperation{}, err
	}
	defer done()
	if err = human(actor); err != nil {
		return c.LifecycleOperation{}, err
	}
	semantic, err := c.ArchiveDigest(actor, meta, project)
	if err != nil {
		return c.LifecycleOperation{}, err
	}
	result, err := s.beginLifecycle(ctx, actor, meta, project, c.ArchiveCommand, semantic, nil)
	if err != nil {
		return c.LifecycleOperation{}, err
	}
	if result.Operation == nil || result.Operation.Action != c.Archive {
		return c.LifecycleOperation{}, unavailable(nil)
	}
	return *result.Operation, nil
}

func (s *Service) BeginDeleteProject(ctx context.Context, actor identity.Actor, meta foundation.CommandMeta, project c.ProjectID, request c.DeleteProjectRequest) (c.LifecycleResult, error) {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return c.LifecycleResult{}, err
	}
	defer done()
	if err = human(actor); err != nil {
		return c.LifecycleResult{}, err
	}
	semantic, err := c.DeleteDigest(actor, meta, project, request)
	if err != nil {
		return c.LifecycleResult{}, err
	}
	request.NormalizedCurrentPath, _ = c.NormalizeConfirmationPath(request.NormalizedCurrentPath)
	return s.beginLifecycle(ctx, actor, meta, project, c.DeleteCommand, semantic, &request)
}

func (s *Service) checkNewLifecycle(ctx context.Context, tx foundation.Tx, actor identity.Actor, p *projectRecord, expected foundation.Version, name c.CommandName, confirmation *c.DeleteProjectRequest) error {
	if !p.initialized {
		return fault(foundation.ProjectNotActive)
	}
	if p.ref.Version != expected {
		return fault(foundation.VersionConflict)
	}
	if name == c.ArchiveCommand && p.ref.Lifecycle != c.Active || name == c.DeleteCommand && p.ref.Lifecycle != c.Active && p.ref.Lifecycle != c.Archived {
		return fault(foundation.ProjectNotActive)
	}
	if name == c.DeleteCommand {
		routes := s.state().deps.Authority.state().routes
		if nilPort(routes) {
			return fault(foundation.DependencyUnbound)
		}
		route, err := routes.CurrentUserRouteInTx(ctx, tx, actor)
		if err != nil {
			return portError(err)
		}
		path, err := c.NormalizeProjectPath(route.Username, p.ref.Name)
		if err != nil || route.UserID != p.ref.OwnerUserID || route.Version.Validate() != nil || confirmation == nil {
			return unavailable(err)
		}
		if path != confirmation.NormalizedCurrentPath {
			return fault(foundation.ConfirmationStale)
		}
	}
	return nil
}

func lifecycleSemanticReplay(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, command *lifecycleCommandRecord, semantic foundation.Digest) (*c.LifecycleResult, error) {
	if command.user != actor.Details().UserID {
		return nil, fault(foundation.NotFound)
	}
	if command.semantic != semantic {
		return nil, fault(foundation.IdempotencyKeyReused)
	}
	if command.state != "completed" {
		return nil, nil
	}
	result, err := lifecycleCommandResult(ctx, x, command)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (s *Service) beginLifecycle(ctx context.Context, actor identity.Actor, meta foundation.CommandMeta, project c.ProjectID, name c.CommandName, semantic foundation.Digest, confirmation *c.DeleteProjectRequest) (c.LifecycleResult, error) {
	command, _ := c.CommandIdentity(project, name, meta.IdempotencyKey)
	locks := []foundation.LockRequest{commandLock(command), userLock(actor.Details().UserID, foundation.Exclusive), projectLock(project, foundation.Exclusive)}
	var planned *lifecycleCommandRecord
	var replay *c.LifecycleResult
	result := s.state().store.WithinTx(ctx, commandCause(command), func(ctx context.Context, tx foundation.Tx) error {
		planned, replay = nil, nil
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return unavailable(err)
		}
		x, err := s.state().deps.Authority.current(ctx, tx, actor, project, foundation.Exclusive)
		if err != nil {
			return err
		}
		p, err := loadProject(ctx, x, project)
		if err != nil {
			return err
		}
		if p == nil {
			replay, err = deletedLifecycleCommand(ctx, x, actor, project, name, meta.IdempotencyKey, &semantic)
			if err == nil && replay == nil {
				return fault(foundation.NotFound)
			}
			return err
		}
		if p.ref.OwnerUserID.String() != actor.Details().UserID {
			return fault(foundation.NotFound)
		}
		current, err := loadLifecycleCommand(ctx, x, project, name, meta.IdempotencyKey)
		if err != nil {
			return err
		}
		if current != nil {
			replay, err = lifecycleSemanticReplay(ctx, x, actor, current, semantic)
			planned = current
			return err
		}
		if err = s.checkNewLifecycle(ctx, tx, actor, p, *meta.ExpectedVersion, name, confirmation); err != nil {
			return err
		}
		manifest, err := s.state().deps.LifecycleRegistry.Manifest()
		if err != nil {
			return err
		}
		if _, err = s.state().deps.LifecycleRegistry.Resolve(manifest); err != nil {
			return err
		}
		manifestDigest, err := manifest.Digest()
		if err != nil {
			return err
		}
		next, err := nextVersion(p.ref.Version)
		if err != nil {
			return err
		}
		operation, err := foundation.NewID[c.Operation]()
		if err != nil {
			return unavailable(err)
		}
		eventID, err := foundation.NewID[event.EventIdentity]()
		if err != nil {
			return unavailable(err)
		}
		commandID, err := foundation.NewID[struct{}]()
		if err != nil {
			return unavailable(err)
		}
		now, err := dbNow(ctx, x)
		if err != nil {
			return err
		}
		action, to := c.Archive, c.Archiving
		if name == c.DeleteCommand {
			action, to = c.Delete, c.Deleting
		}
		header, err := eventHeader(eventID, project, c.LifecycleChangedEventName, next, now)
		if err != nil {
			return err
		}
		ev, err := s.state().deps.ProjectEvents.NewLifecycleChanged(header, c.LifecycleChangedPayload{OperationID: &operation, From: p.ref.Lifecycle, To: to, Action: action})
		if err != nil {
			return err
		}
		rawHeader, err := ev.HeaderJSON()
		if err != nil {
			return err
		}
		plan := lifecycleCommandPlan{ExpectedVersion: p.ref.Version, OperationID: operation, From: p.ref.Lifecycle, To: to, Action: action, Manifest: manifest.Entries(), ManifestDigest: manifestDigest, Confirmation: confirmation, Header: rawHeader, Payload: ev.PayloadBytes()}
		rawPlan, err := json.Marshal(plan)
		if err != nil {
			return unavailable(err)
		}
		eventIDs, _ := json.Marshal([]string{eventID.String()})
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.commands(id,project_id,actor_user_id,command_name,key,semantic_digest,state,plan,event_id,event_ids,audit_cause,created_at) VALUES($1,$2,$3,$4,$5,$6,'planned',$7::jsonb,$8,$9::jsonb,$10,$11)`, commandID.String(), project.String(), actor.Details().UserID, string(name), string(meta.IdempotencyKey), semantic.String(), rawPlan, eventID.String(), eventIDs, commandAuditCause(command), now.Time()); err != nil {
			return unavailable(err)
		}
		planned = &lifecycleCommandRecord{id: commandID.String(), user: actor.Details().UserID, project: project, name: name, key: meta.IdempotencyKey, semantic: semantic, state: "planned", plan: plan, manifest: manifest, header: header}
		return nil
	})
	if result.State() == foundation.Unknown {
		return s.lifecycleAfterUnknown(ctx, actor, project, name, meta.IdempotencyKey, semantic, result, false)
	}
	if err := commitError(result); err != nil {
		return c.LifecycleResult{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	if planned == nil {
		return c.LifecycleResult{}, unavailable(nil)
	}
	// Even an unaccepted retained plan requires its original compatible set.
	if _, err := s.state().deps.LifecycleRegistry.Resolve(planned.manifest); err != nil {
		return c.LifecycleResult{}, err
	}
	ev, err := s.state().deps.ProjectEvents.Restore(planned.header, planned.plan.Payload)
	if err != nil {
		return c.LifecycleResult{}, unavailable(err)
	}
	eventPlan, err := s.state().deps.Events.PrepareAppend(ctx, actor, ev)
	if err != nil {
		return c.LifecycleResult{}, portError(err)
	}
	locks = append(locks, eventPlan.Locks()...)
	var output c.LifecycleResult
	result = s.state().store.WithinTx(ctx, commandCause(command), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, locks); err != nil {
			return unavailable(err)
		}
		x, err := s.state().deps.Authority.current(ctx, tx, actor, project, foundation.Exclusive)
		if err != nil {
			return err
		}
		p, err := ownerProject(ctx, x, actor, project)
		if err != nil {
			return err
		}
		current, err := loadLifecycleCommand(ctx, x, project, name, meta.IdempotencyKey)
		if err != nil {
			return err
		}
		if current == nil {
			return fault(foundation.ResourceBusy)
		}
		replayed, err := lifecycleSemanticReplay(ctx, x, actor, current, semantic)
		if err != nil {
			return err
		}
		if replayed != nil {
			output = *replayed
			return nil
		}
		if !sameLifecyclePlan(current, planned) {
			return fault(foundation.ResourceBusy)
		}
		if err = s.checkNewLifecycle(ctx, tx, actor, p, current.plan.ExpectedVersion, name, current.plan.Confirmation); err != nil {
			return err
		}
		if p.ref.Lifecycle != current.plan.From {
			return fault(foundation.ProjectNotActive)
		}
		if _, err = s.state().deps.LifecycleRegistry.Resolve(current.manifest); err != nil {
			return err
		}
		if err = insertLifecycleAcceptance(ctx, x, current); err != nil {
			return err
		}
		if err = s.appendLifecycleAcceptedAudit(ctx, tx, actor, current); err != nil {
			return err
		}
		if _, err = s.state().deps.Events.AppendEventInTx(ctx, tx, actor, ev, eventPlan); err != nil {
			return portError(err)
		}
		output, err = lifecycleCommandResult(ctx, x, current)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(c.CommandResult{Command: name, Lifecycle: &output})
		if err != nil {
			return unavailable(err)
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_project.commands SET state='completed',safe_result=$2::jsonb,committed_at=clock_timestamp() WHERE id=$1 AND state='planned'`, current.id, raw)
		if err = affected(tag, err); err != nil {
			return err
		}
		return portError(s.state().deps.Activity.TouchActivityInTx(ctx, tx, actor))
	})
	if result.State() == foundation.Unknown {
		return s.lifecycleAfterUnknown(ctx, actor, project, name, meta.IdempotencyKey, semantic, result, true)
	}
	if err = commitError(result); err != nil {
		return c.LifecycleResult{}, err
	}
	return output, nil
}

func insertLifecycleAcceptance(ctx context.Context, x postgres.SQLExecutor, command *lifecycleCommandRecord) error {
	p, h := command.plan, command.header
	raw, err := json.Marshal(command.manifest)
	if err != nil {
		return unavailable(err)
	}
	if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_operations(id,project_id,owner_user_id,action,project_version,state,version,required_manifest,manifest_digest,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'accepted',1,$6::jsonb,$7,$8,$8)`, p.OperationID.String(), command.project.String(), command.user, string(p.Action), int64(*h.AggregateVersion), raw, p.ManifestDigest.String(), h.OccurredAt.Time()); err != nil {
		return unavailable(err)
	}
	cleanup := "not_applicable"
	if p.Action == c.Delete {
		cleanup = "required"
	}
	for _, entry := range command.manifest.Entries() {
		if _, err = x.Exec(ctx, `INSERT INTO agenteam_project.lifecycle_participants(operation_id,participant_name,contract_version,stop_state,cleanup_state,version) VALUES($1,$2,$3,'required',$4,1)`, p.OperationID.String(), string(entry.Name), int64(entry.ContractVersion), cleanup); err != nil {
			return unavailable(err)
		}
	}
	tag, err := x.Exec(ctx, `UPDATE agenteam_project.projects SET lifecycle=$2,version=$3,current_lifecycle_operation_id=$4,updated_at=$5 WHERE id=$1 AND version=$6 AND lifecycle=$7 AND initialized_at IS NOT NULL`, command.project.String(), string(p.To), int64(*h.AggregateVersion), p.OperationID.String(), h.OccurredAt.Time(), int64(p.ExpectedVersion), string(p.From))
	return affected(tag, err)
}

func (s *Service) lifecycleAfterUnknown(ctx context.Context, actor identity.Actor, project c.ProjectID, name c.CommandName, key foundation.IdempotencyKey, semantic foundation.Digest, original foundation.CommitResult, final bool) (c.LifecycleResult, error) {
	lookup, err := s.lookupAfterUnknown(ctx, actor, c.CommandLookupRequest{ProjectID: project, Command: name, Key: key}, original, semantic)
	if err != nil {
		return c.LifecycleResult{}, err
	}
	if lookup.State == c.LookupCommitted && lookup.Result != nil && lookup.Result.Lifecycle != nil {
		return *lookup.Result.Lifecycle, nil
	}
	if lookup.State == c.LookupNotObserved || final && lookup.State == c.LookupInProgress {
		return c.LifecycleResult{}, notCommittedAfterUnknown(original)
	}
	return c.LifecycleResult{}, commitError(original)
}

func deletedLifecycleCommand(ctx context.Context, x postgres.SQLExecutor, actor identity.Actor, project c.ProjectID, name c.CommandName, key foundation.IdempotencyKey, semantic *foundation.Digest) (*c.LifecycleResult, error) {
	receipt, err := loadDeletionReceipt(ctx, x, project)
	if err != nil || receipt == nil {
		return nil, err
	}
	data := receipt.Details()
	if data.OriginalOwnerUserID.String() != actor.Details().UserID {
		return nil, fault(foundation.NotFound)
	}
	hash, err := c.DeletionCommandKeyHash(project, data.OriginalOwnerUserID, key)
	if err != nil {
		return nil, err
	}
	if name != c.DeleteCommand || hash != data.CommandKeyHash {
		return nil, fault(foundation.ResourceDeleted)
	}
	if semantic != nil && *semantic != data.RequestDigest {
		return nil, fault(foundation.IdempotencyKeyReused)
	}
	return &c.LifecycleResult{Receipt: receipt}, nil
}

func (s *Service) lookupLifecycle(ctx context.Context, actor identity.Actor, request c.CommandLookupRequest, expected *foundation.Digest) (c.CommandLookupResult, error) {
	command, _ := c.CommandIdentity(request.ProjectID, request.Command, request.Key)
	var output c.CommandLookupResult
	result := s.state().store.WithinTx(ctx, commandCause(command), func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{commandLock(command), userLock(actor.Details().UserID, foundation.Shared), projectLock(request.ProjectID, foundation.Exclusive)}); err != nil {
			return unavailable(err)
		}
		x, err := s.state().deps.Authority.current(ctx, tx, actor, request.ProjectID, foundation.Exclusive)
		if err != nil {
			return err
		}
		p, err := loadProject(ctx, x, request.ProjectID)
		if err != nil {
			return err
		}
		if p == nil {
			deleted, err := deletedLifecycleCommand(ctx, x, actor, request.ProjectID, request.Command, request.Key, expected)
			if err != nil {
				return err
			}
			output.State = c.LookupNotObserved
			if deleted != nil {
				output = c.CommandLookupResult{State: c.LookupCommitted, Result: &c.CommandResult{Command: request.Command, Lifecycle: deleted}}
			}
			return nil
		}
		if p.ref.OwnerUserID.String() != actor.Details().UserID {
			return fault(foundation.NotFound)
		}
		r, err := loadLifecycleCommand(ctx, x, request.ProjectID, request.Command, request.Key)
		if err != nil {
			return err
		}
		if r == nil {
			output.State = c.LookupNotObserved
			return nil
		}
		if r.user != actor.Details().UserID {
			return fault(foundation.NotFound)
		}
		if expected != nil && r.semantic != *expected {
			return fault(foundation.IdempotencyKeyReused)
		}
		if r.state == "planned" {
			output.State = c.LookupInProgress
			return nil
		}
		current, err := lifecycleCommandResult(ctx, x, r)
		if err != nil {
			return err
		}
		output = c.CommandLookupResult{State: c.LookupCommitted, Result: &c.CommandResult{Command: request.Command, Lifecycle: &current}}
		return nil
	})
	if err := commitError(result); err != nil {
		return c.CommandLookupResult{}, err
	}
	return output, nil
}

func (s *Service) GetLifecycle(ctx context.Context, actor identity.Actor, project c.ProjectID, operation c.OperationID) (c.LifecycleResult, error) {
	ctx, done, err := s.begin(ctx)
	if err != nil {
		return c.LifecycleResult{}, err
	}
	defer done()
	if err = human(actor); err != nil {
		return c.LifecycleResult{}, err
	}
	if project.Validate() != nil || operation.Validate() != nil {
		return c.LifecycleResult{}, invalid()
	}
	cause, err := readCause("lifecycle")
	if err != nil {
		return c.LifecycleResult{}, err
	}
	var output c.LifecycleResult
	result := s.state().store.WithinTx(ctx, cause, func(ctx context.Context, tx foundation.Tx) error {
		if err := s.state().store.AcquireAll(ctx, tx, []foundation.LockRequest{userLock(actor.Details().UserID, foundation.Shared), projectLock(project, foundation.Shared)}); err != nil {
			return unavailable(err)
		}
		x, err := s.state().deps.Authority.current(ctx, tx, actor, project, foundation.Shared)
		if err != nil {
			return err
		}
		p, err := loadProject(ctx, x, project)
		if err != nil {
			return err
		}
		if p == nil {
			receipt, err := loadDeletionReceipt(ctx, x, project)
			if err != nil {
				return err
			}
			if receipt == nil || receipt.Details().OriginalOwnerUserID.String() != actor.Details().UserID || receipt.Details().OperationID != operation {
				return fault(foundation.NotFound)
			}
			output.Receipt = receipt
			return nil
		}
		if p.ref.OwnerUserID.String() != actor.Details().UserID {
			return fault(foundation.NotFound)
		}
		r, err := loadLifecycleOperation(ctx, x, project, operation)
		if err != nil {
			return err
		}
		if r == nil || r.owner != actor.Details().UserID {
			return fault(foundation.NotFound)
		}
		output.Operation = &r.operation
		return nil
	})
	if err = commitError(result); err != nil {
		return c.LifecycleResult{}, err
	}
	return output, nil
}
