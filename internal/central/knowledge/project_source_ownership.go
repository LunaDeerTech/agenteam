package knowledge

import (
	"context"
	"errors"
	"sort"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// The factory creates this resolver before publishing the pair. No caller can
// retrofit a legacy resolver with a different call owner or Project authority.
func newOwnedSourceResolver(store Store, authority *Authority, objects *object.Service, ownership *projectCallState) (*SourceResolver, error) {
	if ownership == nil || objects == nil || objects.RequireProjectCleanupStore(store) != nil {
		return nil, fault(f.DependencyUnbound)
	}
	r, err := NewSourceResolver(store, authority, objects)
	if err != nil {
		return nil, err
	}
	r.state().ownership = ownership
	return r, nil
}

type knowledgeProjectSource struct {
	originalCall                                   context.Context
	origin                                         f.Tx
	command                                        f.CommandIdentity
	record                                         commandRecord
	actor                                          id.Actor
	work                                           publicationWork
	source                                         businessPublicationSource
	lease                                          oc.SourceLease
	acquisitionReturned, active, retired, retained bool
	original                                       f.CommitResult
}

func (s *Service) knowledgeSourceState() *sourceState {
	if s.state() == nil || s.state().ownership == nil {
		return nil
	}
	return s.state().ownership.resolver.state()
}

func (s *Service) withinKnowledgeSourceAcquisition(ctx context.Context, cause f.TransactionCause, fn func(context.Context, f.Tx) error) f.CommitResult {
	st := s.knowledgeSourceState()
	if st == nil {
		return s.state().store.WithinTx(ctx, cause, fn)
	}
	var origin f.Tx
	result := st.ownership.reads.WithinKnowledgeProjectSourceAcquisition(ctx, cause, func(ctx context.Context, tx f.Tx) error { origin = tx; return fn(ctx, tx) })
	st.mu.Lock()
	var rollback []*knowledgeProjectSource
	for _, item := range st.sources {
		if origin.Valid() && item.origin == origin {
			item.acquisitionReturned = true
			if result.State() == f.NotCommitted {
				rollback = append(rollback, item)
			}
		}
	}
	st.mu.Unlock()
	for _, item := range rollback {
		if st.ownership.reads.ForgetProjectSource(item.lease) == nil {
			st.mu.Lock()
			item.retired = true
			st.mu.Unlock()
		}
	}
	return result
}

func (s *Service) bindKnowledgeSource(ctx context.Context, tx f.Tx, input contentInput, intent contentIntent, work publicationWork, source businessPublicationSource, lease oc.SourceLease) error {
	st := s.knowledgeSourceState()
	if st == nil {
		return nil
	}
	if err := s.requireProjectCall(ctx, input.project, work.source, projectCallCommand); err != nil {
		return err
	}
	command, err := kc.CommandIdentity(input.project, input.name, input.meta.IdempotencyKey)
	if err != nil {
		return portError(err)
	}
	if err = st.ownership.reads.BindKnowledgeProjectSourceInTx(ctx, tx, lease, input.project, command); err != nil {
		return portError(err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.sources == nil {
		st.sources = map[oc.LeaseID]*knowledgeProjectSource{}
	}
	if prior := st.sources[lease.ID()]; prior != nil {
		if prior.origin != tx || prior.command.Canonical() != command.Canonical() || !prior.work.equal(work) {
			return fault(f.ResourceBusy)
		}
		return nil
	}
	st.sources[lease.ID()] = &knowledgeProjectSource{originalCall: ctx, origin: tx, command: command, record: *intent.record, actor: input.actor, work: work.clone(), source: source, lease: lease}
	return nil
}

func (s *Service) cancelKnowledgeSource(ctx context.Context, lease oc.SourceLease) error {
	st := s.knowledgeSourceState()
	if st != nil {
		st.mu.Lock()
		item := st.sources[lease.ID()]
		retired := item != nil && item.retired
		var retained bool
		var original f.CommitResult
		if item != nil {
			retained, original = item.retained, item.original
		}
		st.mu.Unlock()
		if retained {
			return txError(original)
		}
		if retired {
			return nil
		}
	}
	return portError(s.state().deps.SourceReads.CancelSourceLease(ctx, lease))
}

// A reader caches its original Close error. Only the exact later typed stop
// proof may retire that already-returned close; it never discards a live Close.
func (s *Service) closeKnowledgeSourceReader(lease oc.SourceLease, close func() error) error {
	err := close()
	st := s.knowledgeSourceState()
	if st == nil || err == nil {
		return err
	}
	st.mu.Lock()
	item := st.sources[lease.ID()]
	retired := item != nil && item.retired
	st.mu.Unlock()
	if retired {
		return nil
	}
	return err
}

func (s *Service) returnedKnowledgeSource(lease oc.SourceLease, result f.CommitResult) error {
	st := s.knowledgeSourceState()
	if st == nil {
		return txError(result)
	}
	st.mu.Lock()
	item := st.sources[lease.ID()]
	if item == nil {
		st.mu.Unlock()
		return fault(f.InvalidState)
	}
	if !item.retained && result.State() == f.Unknown {
		item.retained = true
		item.original = result
	}
	retained, original := item.retained, item.original
	st.mu.Unlock()
	if retained {
		return txError(original)
	}
	if err := txError(result); err != nil {
		return err
	}
	if err := st.ownership.reads.ForgetProjectSource(lease); err != nil {
		return portError(err)
	}
	st.mu.Lock()
	item.retired = true
	st.mu.Unlock()
	return nil
}

func (s *Service) forgetRetiredKnowledgeSources(work publicationWork) {
	st := s.knowledgeSourceState()
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for key, item := range st.sources {
		if item.work.equal(work) && item.retired && !item.active && !item.retained {
			delete(st.sources, key)
		}
	}
}

// The parent has already checked the exact Knowledge Stop claim. Each child
// still consumes its own independent Project source loan and original handle.
func (s *Service) stopKnowledgeProjectSources(ctx context.Context, actor id.Actor, cause pc.LifecycleCause, scope pc.ScopeRef, observe bool) (bool, error) {
	st := s.knowledgeSourceState()
	if st == nil {
		return false, fault(f.DependencyUnbound)
	}
	if cause.Validate() != nil || cause.Action != pc.Delete || pc.RequireProjectScope(scope) != nil {
		return false, fault(f.InvalidArgument)
	}
	op, err := f.ParseID[oc.ProjectStopOperation](cause.OperationID.String())
	if err != nil {
		return false, internal(err)
	}
	objectCause, err := oc.NewProjectStopCause(oc.ProjectStopCauseDetails{ProjectID: scope.ProjectID, OperationID: op, Action: oc.ProjectStopDelete, ProjectVersion: cause.ProjectVersion})
	if err != nil {
		return false, portError(err)
	}
	st.mu.Lock()
	var items []*knowledgeProjectSource
	for _, item := range st.sources {
		if item.work.project == scope.ProjectID || item.work.source != nil && *item.work.source == scope.ProjectID {
			items = append(items, item)
		}
	}
	st.mu.Unlock()
	sort.Slice(items, func(a, b int) bool { return items[a].lease.ID().String() < items[b].lease.ID().String() })
	ready := true
	for _, item := range items {
		st.mu.Lock()
		if item.retired {
			st.mu.Unlock()
			continue
		}
		if item.active || !item.acquisitionReturned {
			st.mu.Unlock()
			ready = false
			continue
		}
		item.active = true
		st.mu.Unlock()
		sourceP := item.source.resolved.Details().Reference.Details().ProjectID
		request, err := oc.NewKnowledgeProjectSourceStopRequest(oc.KnowledgeProjectSourceStopDetails{Actor: actor, Cause: objectCause, Command: item.command, Lease: item.lease, SourceProjectID: sourceP, TargetProjectID: item.work.project, ObjectID: item.source.resolved.Details().Meta.ID})
		done := false
		if err == nil {
			err = st.ownership.reads.WithKnowledgeProjectSourceStop(ctx, request, func(run context.Context) error {
				run = s.originalProjectContext(run, item.originalCall)
				checked, err := s.checkKnowledgeProjectSource(run, request, item, false)
				if err != nil || !checked {
					return err
				}
				var closed bool
				if observe {
					closed, err = st.ownership.reads.InspectKnowledgeProjectSource(run, request)
				} else {
					closed, err = st.ownership.reads.StopKnowledgeProjectSource(run, request)
				}
				if err != nil || !closed {
					return err
				}
				if observe {
					return nil
				}
				done, err = s.checkKnowledgeProjectSource(run, request, item, true)
				return err
			})
		}
		if err == nil && done {
			err = st.ownership.reads.ForgetProjectSource(item.lease)
			if err == nil {
				st.mu.Lock()
				item.retired = true
				st.mu.Unlock()
			}
		}
		st.mu.Lock()
		item.active = false
		st.mu.Unlock()
		if err != nil {
			return false, err
		}
		if !done {
			ready = false
		}
	}
	return ready, nil
}

func (s *Service) checkKnowledgeProjectSource(ctx context.Context, request oc.KnowledgeProjectSourceStopRequest, item *knowledgeProjectSource, mark bool) (bool, error) {
	st := s.knowledgeSourceState()
	st.mu.Lock()
	retained, original := item.retained, item.original
	st.mu.Unlock()
	locks, err := publicationLocks(item.actor, &item.record, item.work.source)
	if err != nil {
		return false, err
	}
	extra, err := request.RequiredLocks()
	if err != nil {
		return false, portError(err)
	}
	locks = append(locks, extra...)
	cause, _ := f.NewRecoveryCause("knowledge-source-owner", item.lease.ID().String(), request.Details().Cause.Details().OperationID.String())
	if retained {
		if original.AttemptID().Validate() == nil && original.Cause().Validate() == nil {
			cause = original.Cause()
		}
	}
	valid := false
	result := st.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := st.ownership.proof.RequireCurrentProcessInTx(ctx, tx, st.ownership.process); err != nil {
			return portError(err)
		}
		if err := st.ownership.reads.RequireKnowledgeProjectSourceStopInTx(ctx, tx, request); err != nil {
			return portError(err)
		}
		x, err := st.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		current, err := loadCommand(ctx, x, item.record.project, item.record.name, item.record.key)
		if err != nil {
			return err
		}
		if current == nil || current.id != item.record.id || current.document != item.record.document || current.digest != item.record.digest || current.user != item.record.user {
			return fault(f.InvalidState)
		}
		work, err := loadPublicationWork(ctx, x, item.work.command)
		if err != nil {
			return err
		}
		if work == nil {
			return fault(f.InvalidState)
		}
		copy := *work
		copy.phase = item.work.phase
		if !copy.equal(item.work) {
			return fault(f.InvalidState)
		}
		var raw []byte
		var lease *string
		if err = x.QueryRow(ctx, `SELECT source,source_lease_id::text FROM agenteam_knowledge.publications WHERE command_id=$1 AND project_id=$2 AND document_id=$3`, item.record.id.String(), item.record.project.String(), item.record.document.String()).Scan(&raw, &lease); err != nil {
			return unavailable(err)
		}
		description, err := decodePublicationSource(raw)
		if err != nil {
			return err
		}
		if !publicationSourceEqual(description, item.source.description) || lease != nil && *lease != item.lease.ID().String() {
			return fault(f.InvalidState)
		}
		if mark && !retained && lease != nil {
			tag, err := x.Exec(ctx, `UPDATE agenteam_knowledge.publications SET source_lease_id=NULL WHERE command_id=$1 AND source_lease_id=$2`, item.record.id.String(), item.lease.ID().String())
			if err != nil {
				return unavailable(err)
			}
			if tag.RowsAffected() != 1 {
				return fault(f.ResourceBusy)
			}
		}
		valid = ctx.Err() == nil
		return ctx.Err()
	})
	if retained {
		if result.State() != f.Committed || !valid || ctx.Err() != nil {
			return false, errors.Join(txError(original), txError(result), ctx.Err())
		}
		st.mu.Lock()
		item.retained = false
		st.mu.Unlock()
		return false, nil
	}
	if result.State() == f.Unknown {
		st.mu.Lock()
		item.retained = true
		item.original = result
		st.mu.Unlock()
	}
	if err = txError(result); err != nil {
		return false, err
	}
	if !valid || ctx.Err() != nil {
		return false, unavailable(ctx.Err())
	}
	return true, nil
}
