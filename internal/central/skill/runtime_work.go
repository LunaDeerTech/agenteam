package skill

import (
	"context"
	"errors"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// A local work record is private ownership, not a transferable stop token.
// returned is protected by serviceState.mu and means the actual I/O owner has
// returned, including its Close/Discard tail. A cancelled context never sets it.
type ownedWork struct {
	fact         workFact
	row          initializationRow
	installation *installationRow
	call         *serviceCall
	returned     bool
}

func (s *Service) newInstallationOwnedWork(row installationRow, kind workKind, call *serviceCall) (*ownedWork, error) {
	state := s.state()
	if state == nil || row.validate() != nil || call == nil || !installedWork(kind) || call.project != row.project || call.kind != kind || call.workID.Validate() != nil {
		return nil, invalid()
	}
	now, err := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond))
	if err != nil {
		return nil, unavailable(err)
	}
	w := &ownedWork{fact: workFact{id: call.workID, project: row.project, skill: row.skill, process: state.process, kind: kind, phase: workRunning, fence: 1, created: now}, installation: &row, call: call}
	if err = w.fact.validate(); err != nil {
		return nil, err
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if _, admitted := state.calls[call]; !admitted || state.work[w.fact.id] != nil {
		return nil, fault(f.InvalidState)
	}
	state.work[w.fact.id] = w
	return w, nil
}

func (w *ownedWork) parentLocks() ([]f.LockRequest, error) {
	if installedWork(w.fact.kind) {
		if w.installation == nil || w.installation.project != w.fact.project || w.installation.skill != w.fact.skill {
			return nil, unavailable(nil)
		}
		return w.installation.locks(f.Exclusive)
	}
	if w.installation != nil {
		return nil, unavailable(nil)
	}
	return w.row.locks(f.Exclusive, w.row.object)
}

func (w *ownedWork) checkpoint() string {
	if w.installation != nil {
		return w.installation.key.String()
	}
	return string(w.row.request.InitializationKey)
}

func (w *ownedWork) checkParent(ctx context.Context, x postgres.SQLExecutor) error {
	if installedWork(w.fact.kind) {
		if w.installation == nil {
			return unavailable(nil)
		}
		row, err := loadInstallation(ctx, x, w.fact.project, w.installation.key)
		if err != nil {
			return err
		}
		if row == nil || row.id != w.installation.id || row.project != w.fact.project || row.skill != w.fact.skill || row.revision != w.installation.revision || row.semantic != w.installation.semantic {
			return unavailable(nil)
		}
		return nil
	}
	row, err := loadInitialization(ctx, x, w.fact.project)
	if err != nil {
		return err
	}
	if row == nil || row.request != w.row.request || row.skill != w.fact.skill || row.revision != w.row.revision || row.semantic != w.row.semantic {
		return unavailable(nil)
	}
	return nil
}

func (s *Service) newOwnedWork(row initializationRow, kind workKind, call *serviceCall) (*ownedWork, error) {
	state := s.state()
	if call == nil || call.project != row.request.ProjectID || call.kind != kind || call.workID.Validate() != nil {
		return nil, invalid()
	}
	now, e := f.NewInstant(time.Now().UTC().Truncate(time.Microsecond))
	if e != nil {
		return nil, unavailable(e)
	}
	w := &ownedWork{fact: workFact{id: call.workID, project: row.request.ProjectID, skill: row.skill, process: state.process, kind: kind, phase: workRunning, fence: 1, created: now}, row: row, call: call}
	if e = w.fact.validate(); e != nil {
		return nil, e
	}
	state.mu.Lock()
	if _, admitted := state.calls[call]; !admitted || state.work[w.fact.id] != nil {
		state.mu.Unlock()
		return nil, fault(f.InvalidState)
	}
	state.work[w.fact.id] = w
	state.mu.Unlock()
	return w, nil
}
func (s *Service) forgetOwnedWork(w *ownedWork) {
	state := s.state()
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.work[w.fact.id] == w {
		delete(state.work, w.fact.id)
		close(state.changed)
		state.changed = make(chan struct{})
	}
}
func (s *Service) registrationFailed(w *ownedWork, err error) {
	var known *f.Fault
	if errors.As(err, &known) && known.CommitState == f.Unknown {
		state := s.state()
		state.mu.Lock()
		w.returned = true
		state.mu.Unlock()
		return
	}
	s.forgetOwnedWork(w)
}
func (s *Service) registerInitializationWork(ctx context.Context, actor id.Actor, row initializationRow, call *serviceCall) (*ownedWork, error) {
	w, e := s.newOwnedWork(row, initializationWork, call)
	if e != nil {
		return nil, e
	}
	e = s.initializationWriteTx(ctx, actor, row, nil, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor, current *initializationRow, _ oc.LockedAccess) error {
		if current == nil || !sameInitialization(*current, row) {
			return fault(f.ResourceBusy)
		}
		return insertWork(ctx, x, w.fact)
	})
	if e != nil {
		s.registrationFailed(w, e)
		return nil, e
	}
	return w, nil
}
func (s *Service) finishOwnedWork(ctx context.Context, w *ownedWork) error {
	state := s.state()
	state.mu.Lock()
	w.returned = true
	state.mu.Unlock()
	return s.retireOwnedWork(ctx, w)
}

// Retirement updates only exact owned accounting. It does not require the old
// active business gate to remain open after cancellation/deletion. The original
// command/Project/parent lock proves the registration transaction is terminal;
// the private local record separately proves actual return in this process.
func (s *Service) retireOwnedWork(ctx context.Context, w *ownedWork) error {
	state := s.state()
	state.mu.Lock()
	if state.work[w.fact.id] != w {
		state.mu.Unlock()
		return nil
	}
	ready := w.returned
	state.mu.Unlock()
	if !ready {
		return fault(f.ResourceBusy)
	}
	if ctx == nil {
		return invalid()
	}
	if e := ctx.Err(); e != nil {
		return portError(e)
	}
	locks, e := w.parentLocks()
	if e != nil {
		return e
	}
	cause, e := f.NewRecoveryCause("skill-work", w.fact.id.String(), w.checkpoint())
	if e != nil {
		return e
	}
	store := state.authority.state().store
	var callbackErr error
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) (err error) {
		defer func() { callbackErr = err }()
		if e := store.AcquireAll(ctx, tx, locks); e != nil {
			return portError(e)
		}
		x, e := store.InTx(tx)
		if e != nil {
			return portError(e)
		}
		if e = w.checkParent(ctx, x); e != nil {
			return e
		}
		current, e := loadWork(ctx, x, w.fact.id)
		if e != nil {
			return e
		}
		if current == nil {
			return nil
		} // exact returned registration + held original locks
		return joinWork(ctx, x, w.fact)
	})
	if result.State() == f.NotCommitted && callbackErr != nil {
		return callbackErr
	}
	if e = commitError(result); e != nil {
		return e
	}
	s.forgetOwnedWork(w)
	return nil
}
