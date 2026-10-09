package projectvariable

import (
	"context"
	"errors"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/projectvariable/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// SecretWriteAuthority is constructed after the real Project authority and
// before Secret. It owns D10 facts and has no Secret/Service reference or setter.
type SecretWriteAuthority struct {
	data func() *secretWriteAuthorityState
}
type secretWriteAuthorityState struct {
	store    Store
	projects pc.ProjectAuthority
	issuer   sc.PlanIssuer
}

func NewSecretWriteAuthority(store Store, projects pc.ProjectAuthority) (*SecretWriteAuthority, error) {
	if nilPort(store) || nilPort(projects) {
		return nil, fault(f.DependencyUnbound)
	}
	if !reflect.TypeOf(store).Comparable() {
		return nil, fault(f.InvalidArgument)
	}
	state := &secretWriteAuthorityState{store: store, projects: projects, issuer: sc.NewPlanIssuer()}
	return &SecretWriteAuthority{data: func() *secretWriteAuthorityState { return state }}, nil
}
func (a *SecretWriteAuthority) state() *secretWriteAuthorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}

// This private marker allows only the outer D10 writer to leave the original
// transaction and rediscover once. It never authorizes a retry after Unknown.
type secretPreparationChanged struct{}

func (secretPreparationChanged) Error() string { return "secret_variable_preparation_changed" }
func secretPrepareAgain() error                { return fault(f.ResourceBusy).WithCause(secretPreparationChanged{}) }

func secretRequestBaseLocks(request sc.ProjectVariableWriteRequest) []f.LockRequest {
	r := request.Fields()
	return []f.LockRequest{commandLock(r.Identity), userLock(r.Actor.Details().UserID, f.Exclusive), projectLock(r.ProjectID, f.Exclusive)}
}
func secretRequestCommand(request sc.ProjectVariableWriteRequest) c.SecretCommandName {
	return c.SecretCommandName(request.Fields().Identity.Command())
}

func (a *SecretWriteAuthority) current(ctx context.Context, tx f.Tx, request sc.ProjectVariableWriteRequest, intent i.AccessIntent) (postgres.SQLExecutor, error) {
	st := a.state()
	if st == nil {
		return nil, fault(f.DependencyUnbound)
	}
	r := request.Fields()
	x, err := st.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if nilPort(x) {
		return nil, internal(nil)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, secretRequestBaseLocks(request)); err != nil {
		return nil, portError(err)
	}
	access, err := st.projects.RequireOwnerInTx(ctx, tx, r.Actor, r.ProjectID, intent)
	if err != nil {
		return nil, portError(err)
	}
	if access.Project().ID != r.ProjectID {
		return nil, internal(nil)
	}
	return x, nil
}

func (a *SecretWriteAuthority) Discover(ctx context.Context, request sc.ProjectVariableWriteRequest) (sc.ProjectVariableWritePlan, error) {
	empty := sc.ProjectVariableWritePlan{}
	st := a.state()
	if st == nil {
		return empty, fault(f.DependencyUnbound)
	}
	if ctx == nil || request.Validate() != nil {
		return empty, fault(f.InvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return empty, canceled(err)
	}
	r := request.Fields()
	var basis sc.ProjectVariableWriteBasisFields
	result := st.store.WithinTx(ctx, commandCause(r.Identity), func(ctx context.Context, tx f.Tx) error {
		if err := st.store.AcquireAll(ctx, tx, secretRequestBaseLocks(request)); err != nil {
			return portError(err)
		}
		x, err := a.current(ctx, tx, request, i.Read)
		if err != nil {
			return err
		}
		saved, err := loadSecretCommand(ctx, x, r.ProjectID, secretRequestCommand(request), r.Identity.Key())
		if err != nil {
			return err
		}
		if saved != nil {
			if !secretRecordMatchesRequest(saved, request) {
				return fault(f.IdempotencyKeyReused)
			}
			observation, _ := saved.Observation.Result()
			basis = sc.ProjectVariableWriteBasisFields{Request: request, Ref: observation.Ref, CredentialVersion: observation.Version, Receipt: saved.Observation}
			return nil
		}
		if _, err = a.current(ctx, tx, request, i.Mutate); err != nil {
			return err
		}
		basis = sc.ProjectVariableWriteBasisFields{Request: request, Receipt: sc.ProjectVariableWriteNotObserved()}
		if r.Kind == sc.Create {
			if err = secretRequireUnusedID(ctx, x, r.ProjectID, r.VariableID); err != nil {
				return err
			}
			credential, err := f.NewID[sc.Credential]()
			if err != nil {
				return unavailable(err)
			}
			scope, err := i.InProject(r.ProjectID)
			if err != nil {
				return internal(err)
			}
			basis.Ref, err = sc.NewCredentialRef(credential, scope)
			return portError(err)
		}
		before, err := loadSecretVariable(ctx, x, r.ProjectID, r.VariableID, false)
		if err != nil {
			return err
		}
		if r.ExpectedVersion == nil || before.Variable.Fields().Version != *r.ExpectedVersion {
			return fault(f.VersionConflict)
		}
		basis.Ref, basis.VariableVersion, basis.CredentialVersion = before.Ref, before.Variable.Fields().Version, before.CredentialVersion
		return nil
	})
	if err := txError(result); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, canceled(err)
	}
	locks, err := sc.ProjectVariableWriteLocks(request, basis.Ref)
	if err != nil {
		return empty, portError(err)
	}
	plan, err := sc.NewProjectVariableWritePlan(st.issuer, basis, locks)
	return plan, portError(err)
}

func secretRequireUnusedID(ctx context.Context, x postgres.SQLExecutor, project c.ProjectID, id c.VariableID) error {
	var owner string
	err := x.QueryRow(ctx, `SELECT project_id::text FROM agenteam_projectvariable.variables WHERE id=$1`, id.String()).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return unavailable(err)
	}
	if owner != project.String() {
		return fault(f.NotFound)
	}
	return field(f.ResourceBusy, "/variable_id", "ID_CONFLICT")
}

// CheckPlan is strictly private-issuer/original-binding validation. It performs
// no SQL, does not allocate a replacement create candidate, and grants no right.
func (a *SecretWriteAuthority) CheckPlan(request sc.ProjectVariableWriteRequest, plan sc.ProjectVariableWritePlan) error {
	st := a.state()
	if st == nil {
		return fault(f.DependencyUnbound)
	}
	if request.Validate() != nil || plan.Validate() != nil {
		return fault(f.InvalidArgument)
	}
	if !plan.Matches(st.issuer, request) {
		return fault(f.Forbidden)
	}
	return nil
}

func (a *SecretWriteAuthority) CheckInTx(ctx context.Context, tx f.Tx, request sc.ProjectVariableWriteRequest, plan sc.ProjectVariableWritePlan, stage sc.ProjectVariableWriteStage) error {
	if ctx == nil || !stage.Valid() {
		return fault(f.InvalidArgument)
	}
	if err := a.CheckPlan(request, plan); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return canceled(err)
	}
	st := a.state()
	if _, err := st.store.InTx(tx); err != nil {
		return portError(err)
	}
	locks, err := plan.RequiredLocks()
	if err != nil {
		return portError(err)
	}
	if err = st.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return portError(err)
	}
	x, err := a.current(ctx, tx, request, i.Read)
	if err != nil {
		return err
	}
	r := request.Fields()
	basis, err := plan.Basis()
	if err != nil {
		return portError(err)
	}
	saved, err := loadSecretCommand(ctx, x, r.ProjectID, secretRequestCommand(request), r.Identity.Key())
	if err != nil {
		return err
	}
	if saved != nil {
		if !secretRecordMatchesRequest(saved, request) {
			return fault(f.IdempotencyKeyReused)
		}
		observed, _ := saved.Observation.Result()
		if basis.Receipt.Observed() {
			if !sameSecretObservation(saved.Observation, basis.Receipt) {
				return internal(nil)
			}
		} else if !observed.Ref.Equal(basis.Ref) {
			return secretPrepareAgain()
		}
		if stage == sc.ProjectVariableNewWrite {
			return fault(f.Forbidden)
		}
		return nil
	}
	if basis.Receipt.Observed() {
		return internal(nil)
	}
	if stage == sc.ProjectVariableReceiptRead {
		return nil
	}
	if _, err = a.current(ctx, tx, request, i.Mutate); err != nil {
		return err
	}
	if r.Kind == sc.Create {
		return secretRequireUnusedID(ctx, x, r.ProjectID, r.VariableID)
	}
	before, err := loadSecretVariable(ctx, x, r.ProjectID, r.VariableID, false)
	if err != nil {
		return err
	}
	if r.ExpectedVersion == nil || before.Variable.Fields().Version != *r.ExpectedVersion ||
		before.Variable.Fields().Version != basis.VariableVersion || before.CredentialVersion != basis.CredentialVersion || !before.Ref.Equal(basis.Ref) {
		return fault(f.VersionConflict)
	}
	return nil
}

var _ sc.ProjectVariableWriteAuthority = (*SecretWriteAuthority)(nil)
