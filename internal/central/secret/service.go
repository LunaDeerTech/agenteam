package secret

import (
	"context"
	"reflect"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type Store interface {
	postgres.SQLExecutor
	InTx(foundation.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, foundation.TransactionCause, func(context.Context, foundation.Tx) error) foundation.CommitResult
	Acquire(context.Context, foundation.Tx, foundation.LockKey, foundation.LockMode) error
	AcquireAll(context.Context, foundation.Tx, []foundation.LockRequest) error
	RequireHeldLocks(context.Context, foundation.Tx, []foundation.LockRequest) error
}
type Authorizations struct {
	AccountWrites    sc.AccountWriteAuthority
	ProjectVariables sc.ProjectVariableWriteAuthority
	Sessions         identity.SessionAuthority
	System           identity.SystemAuthority
	Projects         sc.ProjectAuthority
	Usage            sc.UsageAuthority
}
type serviceState struct {
	usageIssuer   sc.PlanIssuer
	store         Store
	keys          Keyring
	audit         ac.Appender
	auth          Authorizations
	nonceMu       sync.Mutex
	nonces        map[foundation.Version]*nonceRange
	mu            sync.Mutex
	initialized   bool
	unavailable   bool
	writeVersion  foundation.Version
	epoch         foundation.Version
	rotationState string
	remaining     foundation.Progress
	stop          chan struct{}
	stopOnce      sync.Once
}
type Service struct{ data func() *serviceState }

func New(store Store, keys Keyring, audit ac.Appender, auth Authorizations) (*Service, error) {
	if nilPort(store) || nilPort(audit) || keys.Validate() != nil {
		return nil, invalid()
	}
	state := &serviceState{usageIssuer: sc.NewPlanIssuer(), store: store, keys: keys, audit: audit, auth: auth, nonces: map[foundation.Version]*nonceRange{}, stop: make(chan struct{})}
	return &Service{data: func() *serviceState { return state }}, nil
}
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func (s *Service) state() *serviceState { return s.data() }

type Status struct {
	Available    bool                `json:"available"`
	WriteVersion *foundation.Version `json:"write_version,omitempty"`
	Rotation     string              `json:"rotation"`
	Remaining    foundation.Progress `json:"remaining"`
}

func (s *Service) Status() Status {
	state := s.state()
	state.mu.Lock()
	defer state.mu.Unlock()
	out := Status{Available: state.initialized && !state.unavailable, Rotation: state.rotationState, Remaining: state.remaining}
	if state.writeVersion.Validate() == nil {
		v := state.writeVersion
		out.WriteVersion = &v
	}
	return out
}
func (s *Service) writable() error {
	state := s.state()
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.initialized || state.unavailable {
		return unavailable(nil)
	}
	return nil
}
func null(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func scopeKey(scope identity.Scope) string {
	if scope.Details().Kind == identity.System {
		return "system"
	}
	return scope.Details().ProjectID
}
func validScope(scope identity.Scope) bool {
	return scope.Validate() == nil && (scope.Details().Kind == identity.System || scope.Details().Kind == identity.ProjectScope)
}
func projectID(scope identity.Scope) identity.ProjectID {
	id, _ := foundation.ParseID[identity.Project](scope.Details().ProjectID)
	return id
}
func writeLock() foundation.LockKey {
	key, _ := foundation.SystemConfigLock("secret-write-key")
	return key
}
func recoveryCause(owner string) (foundation.TransactionCause, error) {
	id, err := foundation.NewID[struct{}]()
	if err != nil {
		return foundation.TransactionCause{}, unavailable(err)
	}
	cause, err := foundation.NewRecoveryCause(owner, id.String(), "")
	if err != nil {
		return foundation.TransactionCause{}, unavailable(err)
	}
	return cause, nil
}
func commitError(result foundation.CommitResult) error {
	if result.State() == foundation.Committed {
		return nil
	}
	if result.State() == foundation.Unknown {
		return failure(CommitUnknown, foundation.CommitUnknown, nil)
	}
	if fault := result.Fault(); fault != nil {
		return fault
	}
	return unavailable(nil)
}
func (s *Service) authorize(ctx context.Context, tx foundation.Tx, actor identity.Actor, scope identity.Scope, intent identity.AccessIntent) error {
	state := s.state()
	if actor.Validate() != nil || actor.Details().Kind != identity.Human || !validScope(scope) {
		return failure(AuthorizationRejected, foundation.Forbidden, nil)
	}
	if nilPort(state.auth.Sessions) {
		return failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	if err := state.auth.Sessions.RequireCurrentSession(ctx, tx, actor); err != nil {
		return authorization(err)
	}
	var grant identity.AccessGrant
	var err error
	if scope.Details().Kind == identity.System {
		if nilPort(state.auth.System) {
			return failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
		}
		grant, err = state.auth.System.AuthorizeSystem(ctx, tx, actor, intent)
	} else {
		if nilPort(state.auth.Projects) {
			return failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
		}
		grant, err = state.auth.Projects.AuthorizeProject(ctx, tx, actor, projectID(scope), intent)
	}
	if err != nil {
		return authorization(err)
	}
	if !grant.Matches(actor, scope, intent) {
		return unavailable(nil)
	}
	if err = ctx.Err(); err != nil {
		return unavailable(err)
	}
	return nil
}
func (s *Service) mutationGate(ctx context.Context, tx foundation.Tx, actor identity.Actor, ref sc.CredentialRef) error {
	if ref.Details().Scope.Details().Kind == identity.System {
		return nil
	}
	port := s.state().auth.Projects
	if nilPort(port) {
		return failure(AuthorizationUnbound, foundation.DependencyUnbound, nil)
	}
	if err := port.CheckMutationInTx(ctx, tx, actor, ref); err != nil {
		return authorization(err)
	}
	return nil
}
func mutationLocks(command foundation.CommandIdentity, ref sc.CredentialRef, actor identity.Actor) ([]foundation.LockRequest, error) {
	commandKey, err := foundation.CommandLock(command)
	if err != nil || ref.Validate() != nil {
		return nil, invalid()
	}
	aggregate, _ := foundation.AggregateLock(foundation.CredentialRefAggregate, ref.Details().ID.String())
	locks := []foundation.LockRequest{{Key: commandKey, Mode: foundation.Exclusive}, {Key: writeLock(), Mode: foundation.Shared}}
	if actor.Details().Kind == identity.Human {
		key, err := foundation.UserLock(actor.Details().UserID)
		if err != nil {
			return nil, invalid()
		}
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	if ref.Details().Scope.Details().Kind == identity.ProjectScope {
		key, _ := foundation.ProjectLock(ref.Details().Scope.Details().ProjectID)
		locks = append(locks, foundation.LockRequest{Key: key, Mode: foundation.Shared})
	}
	locks = append(locks, foundation.LockRequest{Key: aggregate, Mode: foundation.Exclusive})
	return locks, nil
}
func (s *Service) acquireMutationLocks(ctx context.Context, tx foundation.Tx, command foundation.CommandIdentity, ref sc.CredentialRef, actor identity.Actor) error {
	locks, e := mutationLocks(command, ref, actor)
	if e != nil {
		return e
	}
	return s.state().store.AcquireAll(ctx, tx, locks)
}
