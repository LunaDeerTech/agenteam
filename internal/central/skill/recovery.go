package skill

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

const MaxRecoveryWork = 100

type RecoveryReport struct {
	Examined f.Progress `json:"examined"`
	Joined   f.Progress `json:"joined"`
	Pending  f.Progress `json:"pending"`
}

// Recover examines one finite durable pass. Busy and currently denied entries
// move behind the tail, so a restart or an immortal first row cannot starve all
// later work. It never calls Reserve, Upload or Publish and cannot restart D08.
func (s *Service) Recover(ctx context.Context) (RecoveryReport, error) {
	var out RecoveryReport
	call, e := s.begin(ctx, false)
	if e != nil {
		return out, e
	}
	defer s.end(call)
	state := s.state()
	ids, e := recoveryCandidates(call.ctx, state.authority.state().store)
	if e != nil {
		return out, e
	}
	var firstErr error
	for _, workID := range ids {
		if e = call.ctx.Err(); e != nil {
			return out, portError(e)
		}
		out.Examined++
		joined, e := s.recoverWork(call.ctx, workID)
		if joined {
			out.Joined++
		} else {
			out.Pending++
		}
		if e != nil && firstErr == nil {
			firstErr = e
		}
	}
	return out, firstErr
}

func recoveryCandidates(ctx context.Context, x postgres.SQLExecutor) ([]skillWorkID, error) {
	var raw []string
	e := x.QueryRow(ctx, `SELECT COALESCE(array_agg(id::text ORDER BY recovery_pass,id),ARRAY[]::text[]) FROM (SELECT id,recovery_pass FROM agenteam_skill.work WHERE phase<>'joined' ORDER BY recovery_pass,id LIMIT 100) pending`).Scan(&raw)
	if e != nil {
		return nil, unavailable(e)
	}
	if len(raw) > MaxRecoveryWork {
		return nil, unavailable(nil)
	}
	out := make([]skillWorkID, 0, len(raw))
	seen := map[skillWorkID]bool{}
	for _, text := range raw {
		key, e := f.ParseID[skillWork](text)
		if e != nil || seen[key] {
			return nil, unavailable(e)
		}
		seen[key] = true
		out = append(out, key)
	}
	return out, nil
}

func sameWorkOwner(a, b workFact) bool {
	return a.id == b.id && a.project == b.project && a.skill == b.skill && a.process == b.process && a.kind == b.kind && a.fence == b.fence
}

func (s *Service) recoverWork(ctx context.Context, workID skillWorkID) (bool, error) {
	state := s.state()
	store := state.authority.state().store
	w, e := loadWork(ctx, store, workID)
	if e != nil {
		return false, e
	}
	if w == nil {
		return false, unavailable(nil)
	}
	if installedWork(w.kind) {
		return s.recoverInstallationWork(ctx, *w)
	}
	row, e := loadInitialization(ctx, store, w.project)
	if e != nil {
		return false, e
	}
	if row == nil || row.skill != w.skill {
		return false, unavailable(nil)
	}
	locks, e := row.locks(f.Exclusive, row.object)
	if e != nil {
		return false, e
	}
	var local *ownedWork
	var proofErr error
	if w.process == state.process {
		state.mu.Lock()
		local = state.work[w.id]
		ready := local != nil && local.returned && sameWorkOwner(local.fact, *w)
		state.mu.Unlock()
		if !ready {
			proofErr = fault(f.ResourceBusy)
		}
	} else {
		// Exact former-process death is necessary, and deliberately precedes
		// acquisition of the original command/Project lock. Acquiring that
		// complete union proves its registration/writer Tx has actually ended.
		proofErr = portError(state.processes.ConfirmStopped(ctx, w.process))
	}
	scope, e := id.InProject(row.request.ProjectID)
	if e != nil {
		return false, e
	}
	registration, e := id.RegisterService(id.ProjectInitialization)
	if e != nil {
		return false, e
	}
	actor, e := registration.Actor(row.request.CreationID.String(), scope)
	if e != nil {
		return false, e
	}
	cause, e := f.NewRecoveryCause("skill-work", w.id.String(), string(row.request.InitializationKey))
	if e != nil {
		return false, e
	}
	joined := false
	var callbackErr, gateErr error
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if e := store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		x, e := store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		currentRow, e := loadInitialization(ctx, x, w.project)
		if e != nil {
			return e
		}
		if currentRow == nil || currentRow.request != row.request || currentRow.skill != w.skill || currentRow.revision != row.revision || currentRow.semantic != row.semantic {
			return unavailable(nil)
		}
		current, e := loadWork(ctx, x, w.id)
		if e != nil {
			return e
		}
		if current == nil || !sameWorkOwner(*current, *w) {
			return fault(f.ResourceBusy)
		}
		if current.phase == workJoined {
			joined = true
			return nil
		}
		// Scheduling is technical metadata, even when the business gate is
		// closed. Preserve the gate/proof error after committing only this pass.
		if e = advanceRecoveryPass(ctx, x, *current); e != nil {
			return e
		}
		gateErr = portError(state.authority.state().projects.ValidateInitializationConvergenceInTx(ctx, tx, actor, currentRow.request))
		if gateErr != nil || proofErr != nil {
			return nil
		}
		if e = joinWork(ctx, x, *current); e != nil {
			return e
		}
		joined = true
		return nil
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return false, callbackErr
	}
	if e = commitError(result); e != nil {
		return false, e
	}
	if gateErr != nil {
		return false, gateErr
	}
	if proofErr != nil && !joined {
		return false, proofErr
	}
	if joined && local != nil {
		s.forgetOwnedWork(local)
	}
	return joined, nil
}

func (s *Service) recoverInstallationWork(ctx context.Context, expected workFact) (bool, error) {
	state := s.state()
	store := state.authority.state().store
	row, err := loadInstallationSkill(ctx, store, expected.project, expected.skill)
	if err != nil {
		return false, err
	}
	if row == nil || row.project != expected.project || row.skill != expected.skill || !installedWork(expected.kind) {
		return false, unavailable(nil)
	}
	owner := &ownedWork{fact: expected, installation: row}
	locks, err := owner.parentLocks()
	if err != nil {
		return false, err
	}
	var local *ownedWork
	var proofErr error
	if expected.process == state.process {
		state.mu.Lock()
		local = state.work[expected.id]
		pendingDiscard := local != nil && local.fact.kind == installationWork && local.installationCallerReturned && !local.returned
		state.mu.Unlock()
		if pendingDiscard {
			proofErr = s.joinInstallationDiscard(local)
		}
		state.mu.Lock()
		ready := local != nil && local.returned && sameWorkOwner(local.fact, expected)
		state.mu.Unlock()
		if !ready && proofErr == nil {
			proofErr = fault(f.ResourceBusy)
		}
	} else {
		proofErr = portError(state.processes.ConfirmStopped(ctx, expected.process))
	}
	cause, err := f.NewRecoveryCause("skill-work", expected.id.String(), row.key.String())
	if err != nil {
		return false, err
	}
	joined := false
	var callbackErr error
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
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
		if err = owner.checkParent(ctx, x); err != nil {
			return err
		}
		current, err := loadWork(ctx, x, expected.id)
		if err != nil {
			return err
		}
		if current == nil || !sameWorkOwner(*current, expected) {
			return fault(f.ResourceBusy)
		}
		if current.phase == workJoined {
			joined = true
			return nil
		}
		if err = advanceRecoveryPass(ctx, x, *current); err != nil {
			return err
		}
		if proofErr != nil {
			return nil
		}
		// Only exact original accounting is retired. This is not a new install,
		// retry, publication, Object cleanup, or a grant of current User access.
		if err = joinWork(ctx, x, *current); err != nil {
			return err
		}
		joined = true
		return nil
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return false, callbackErr
	}
	if err = commitError(result); err != nil {
		return false, err
	}
	if !joined && proofErr != nil {
		return false, proofErr
	}
	if joined && local != nil {
		s.forgetOwnedWork(local)
	}
	return joined, nil
}

func advanceRecoveryPass(ctx context.Context, x postgres.SQLExecutor, w workFact) error {
	tag, e := x.Exec(ctx, `UPDATE agenteam_skill.work SET recovery_pass=(SELECT COALESCE(max(recovery_pass),0)+1 FROM agenteam_skill.work) WHERE id=$1 AND process_id=$2 AND fence=$3 AND phase<>'joined'`, w.id.String(), w.process.String(), int64(w.fence))
	if e != nil {
		return unavailable(e)
	}
	if tag.RowsAffected() != 1 {
		return fault(f.ResourceBusy)
	}
	return nil
}
