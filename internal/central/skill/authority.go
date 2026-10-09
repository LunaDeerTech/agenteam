package skill

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type authorityState struct {
	store    Store
	projects ProjectPorts
}

// Authority is constructed before Object/Audit services and owns no physical
// I/O, callback goroutine or mutable provider registry.
type Authority struct{ data func() *authorityState }

func NewAuthority(store Store, projects ProjectPorts) (*Authority, error) {
	if nilPort(store) || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	state := &authorityState{store, projects}
	return &Authority{func() *authorityState { return state }}, nil
}
func (a *Authority) state() *authorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}
func (Authority) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "skill_authority") }
func (Authority) MarshalJSON() ([]byte, error) { return []byte(`"skill_authority"`), nil }
func (*Authority) UnmarshalJSON([]byte) error  { return invalid() }
func (Authority) LogValue() slog.Value         { return slog.StringValue("skill_authority") }

// Initialization facts are read only after the actual current Project gate.
// Convergence cannot be selected by an actor flag or used to authorize writes.
func (a *Authority) initializationInTx(ctx context.Context, tx f.Tx, actor id.Actor, request pc.InitializationRequest, converge bool) (postgres.SQLExecutor, *initializationRow, error) {
	state := a.state()
	if state == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	if e := initializationActor(actor, request); e != nil {
		return nil, nil, e
	}
	if !tx.Valid() {
		return nil, nil, invalid()
	}
	command, e := initializationIdentity(request)
	if e != nil {
		return nil, nil, e
	}
	if e = state.store.RequireHeldLocks(ctx, tx, []f.LockRequest{commandLock(command), projectLock(request.ProjectID, f.Exclusive)}); e != nil {
		return nil, nil, portError(e)
	}
	x, e := state.store.InTx(tx)
	if e != nil {
		return nil, nil, portError(e)
	}
	if converge {
		e = state.projects.ValidateInitializationConvergenceInTx(ctx, tx, actor, request)
	} else {
		e = state.projects.ValidateInitializationInTx(ctx, tx, actor, request.CreationID, request.ProjectID, request.InitializationKey)
	}
	if e != nil {
		return nil, nil, portError(e)
	}
	row, e := loadInitialization(ctx, x, request.ProjectID)
	if e != nil {
		return nil, nil, e
	}
	if row != nil {
		if row.request.ProjectID != request.ProjectID || row.request.CreationID != request.CreationID {
			return nil, nil, unavailable(nil)
		}
		if row.request.InitializationKey != request.InitializationKey {
			return nil, nil, unavailable(nil)
		}
	}
	return x, row, nil
}
