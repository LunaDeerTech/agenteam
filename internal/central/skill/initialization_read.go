package skill

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// Discovery supplies only lock candidates. Its errors and values are not an
// authorization result: the current Project gate precedes the authoritative
// reread in the locked transaction. A changed parent requires rediscovery.
func (s *Service) withInitialization(ctx context.Context, actor id.Actor, request pc.InitializationRequest, converge bool, work func(context.Context, f.Tx, postgres.SQLExecutor, *initializationRow) error) error {
	state := s.state()
	if state == nil {
		return fault(f.DependencyUnbound)
	}
	if e := initializationActor(actor, request); e != nil {
		return e
	}
	command, e := initializationIdentity(request)
	if e != nil {
		return e
	}
	store := state.authority.state().store
	locks := []f.LockRequest{commandLock(command), projectLock(request.ProjectID, f.Exclusive)}
	discovered, _ := loadInitialization(ctx, store, request.ProjectID)
	if discovered != nil && discovered.request.ProjectID == request.ProjectID {
		locks, e = discovered.locks(f.Exclusive, discovered.object)
		if e != nil {
			return e
		}
		// Never allow a stored foreign key to replace the caller's command lock.
		locks[0] = commandLock(command)
	}
	cause, e := f.NewCommandsCause(command)
	if e != nil {
		return e
	}
	var callbackErr error
	result := store.WithinTx(ctx, cause, func(txctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if e := store.AcquireAll(txctx, tx, locks); e != nil {
			return portError(e)
		}
		x, row, e := state.authority.initializationInTx(txctx, tx, actor, request, converge)
		if e != nil {
			return e
		}
		if row != nil {
			required, e := row.locks(f.Exclusive, row.object)
			if e != nil {
				return e
			}
			if e = store.RequireHeldLocks(txctx, tx, required); e != nil {
				return portError(e)
			}
		}
		return work(txctx, tx, x, row)
	})
	// A known local rollback cannot rewrite a dependency's original Fault or
	// Unknown outcome. Conversely, an unknown outer commit is always retained.
	if result.State() == f.NotCommitted && callbackErr != nil {
		return callbackErr
	}
	return commitError(result)
}

func initializationResult(ctx context.Context, x postgres.SQLExecutor, request pc.InitializationRequest, row *initializationRow) (pc.InitializationResult, error) {
	result := pc.InitializationResult{State: pc.InitializationResultPending, ProjectID: request.ProjectID, CreationID: request.CreationID}
	if row == nil {
		return result, nil
	}
	if e := row.validate(); e != nil {
		return pc.InitializationResult{}, e
	}
	if row.request != request {
		return pc.InitializationResult{}, unavailable(nil)
	}
	switch row.phase {
	case initializationPublished:
		meta, revision, e := loadPublished(ctx, x, *row)
		if e != nil {
			return pc.InitializationResult{}, e
		}
		result.State = pc.InitializationCompleted
		result.AddSkillsID = &meta.ID
		result.Revision = &revision.Revision
	case initializationFailed:
		result.State = pc.InitializationFailed
		result.SafeReason = row.reason
	}
	if e := result.Validate(); e != nil {
		return pc.InitializationResult{}, unavailable(e)
	}
	return result, nil
}

// Inspect is observation of the original command. It cannot reserve an object,
// send bytes, retry a writer, or turn an absent receipt into a known rollback.
func (s *Service) InspectProjectSkills(ctx context.Context, actor id.Actor, request pc.InitializationRequest) (pc.InitializationResult, error) {
	call, e := s.begin(ctx, false)
	if e != nil {
		return pc.InitializationResult{}, e
	}
	defer s.end(call)
	var out pc.InitializationResult
	e = s.withInitialization(call.ctx, actor, request, true, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, row *initializationRow) error {
		var err error
		out, err = initializationResult(ctx, x, request, row)
		return err
	})
	if e != nil {
		return pc.InitializationResult{}, e
	}
	return out, nil
}

func (s *Service) DiscoverConfirmation(ctx context.Context, actor id.Actor, request pc.InitializationRequest) (pc.InitializationConfirmationPlan, error) {
	call, e := s.begin(ctx, false)
	if e != nil {
		return pc.InitializationConfirmationPlan{}, e
	}
	defer s.end(call)
	var receipt pc.InitializationReceipt
	var locks []f.LockRequest
	e = s.withInitialization(call.ctx, actor, request, false, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, row *initializationRow) error {
		result, e := initializationResult(ctx, x, request, row)
		if e != nil {
			return e
		}
		if result.State != pc.InitializationCompleted {
			return fault(f.InvalidState)
		}
		receipt = pc.InitializationReceipt{CreationID: request.CreationID, ProjectID: request.ProjectID, AddSkillsID: *result.AddSkillsID, Revision: *result.Revision}
		locks, e = row.locks(f.Exclusive, row.object)
		return e
	})
	if e != nil {
		return pc.InitializationConfirmationPlan{}, e
	}
	return s.state().confirmation.Plan(actor, request, receipt, locks)
}

// Confirm consumes only the caller's live transaction and complete lock union.
// The receipt is provisional until that outer transaction actually commits.
func (s *Service) ConfirmInitializedInTx(ctx context.Context, tx f.Tx, actor id.Actor, request pc.InitializationRequest, plan pc.InitializationConfirmationPlan) (pc.InitializationReceipt, error) {
	call, e := s.begin(ctx, false)
	if e != nil {
		return pc.InitializationReceipt{}, e
	}
	defer s.end(call)
	state := s.state()
	if !state.confirmation.Matches(plan, actor, request) || !tx.Valid() {
		return pc.InitializationReceipt{}, invalid()
	}
	store := state.authority.state().store
	if e = store.RequireHeldLocks(ctx, tx, plan.RequiredLocks()); e != nil {
		return pc.InitializationReceipt{}, portError(e)
	}
	x, row, e := state.authority.initializationInTx(ctx, tx, actor, request, false)
	if e != nil {
		return pc.InitializationReceipt{}, e
	}
	if row == nil {
		return pc.InitializationReceipt{}, fault(f.InvalidState)
	}
	required, e := row.locks(f.Exclusive, row.object)
	if e != nil {
		return pc.InitializationReceipt{}, e
	}
	if e = store.RequireHeldLocks(ctx, tx, required); e != nil {
		return pc.InitializationReceipt{}, portError(e)
	}
	result, e := initializationResult(ctx, x, request, row)
	if e != nil {
		return pc.InitializationReceipt{}, e
	}
	if result.State != pc.InitializationCompleted {
		return pc.InitializationReceipt{}, fault(f.InvalidState)
	}
	receipt := pc.InitializationReceipt{CreationID: request.CreationID, ProjectID: request.ProjectID, AddSkillsID: *result.AddSkillsID, Revision: *result.Revision}
	if receipt != plan.ProposedReceipt() {
		return pc.InitializationReceipt{}, unavailable(nil)
	}
	return receipt, nil
}
