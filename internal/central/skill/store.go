package skill

import (
	"context"
	"errors"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/skill/contract"
)

// Store owns the callback-local transaction and the complete lock union. Skill
// code cannot use an opaque Tx without validating it with this same Store.
type Store interface {
	postgres.SQLExecutor
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

// ProjectPorts deliberately preserves the current lifecycle implementation's
// refusal of unbound phases. It is not an alternate Project authority.
type ProjectPorts interface {
	pc.ProjectAuthority
	pc.InitializationConvergenceAuthority
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func fault(code f.Code) *f.Fault  { return f.NewFault(code, f.NotStarted) }
func invalid() error              { return fault(f.InvalidArgument) }
func unavailable(err error) error { return fault(f.DependencyUnavailable).WithCause(err) }
func portError(err error) error {
	if err == nil {
		return nil
	}
	var known *f.Fault
	if errors.As(err, &known) {
		return err
	}
	return unavailable(err)
}

type commitFailure struct{ result f.CommitResult }

func (commitFailure) Error() string { return string(f.CommitUnknown) }

// UnknownAttempt retains the exact physical attempt/cause for original-key
// recovery. It is diagnostic provenance, never permission to replay a writer.
func UnknownAttempt(err error) (f.CommitResult, bool) {
	var failure commitFailure
	if errors.As(err, &failure) {
		return failure.result, true
	}
	return f.CommitResult{}, false
}
func commitError(r f.CommitResult) error {
	switch r.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		if e := r.Fault(); e != nil {
			return e
		}
		return unavailable(nil)
	default:
		e := f.NewFault(f.CommitUnknown, f.Unknown)
		e.RetryHint = "lookup"
		if r.AttemptID().Validate() == nil {
			e.CauseID = r.AttemptID().String()
		}
		return e.WithCause(commitFailure{r})
	}
}
func projectLock(project id.ProjectID, mode f.LockMode) f.LockRequest {
	k, _ := f.ProjectLock(project.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func skillLock(skill sc.SkillID, mode f.LockMode) f.LockRequest {
	k, _ := f.AggregateLock(f.SkillAggregate, skill.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func objectLock(object oc.ObjectID, mode f.LockMode) f.LockRequest {
	k, _ := f.AggregateLock(f.ObjectAggregate, object.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func userLock(user string, mode f.LockMode) f.LockRequest {
	k, _ := f.UserLock(user)
	return f.LockRequest{Key: k, Mode: mode}
}
func commandLock(command f.CommandIdentity) f.LockRequest {
	k, _ := f.CommandLock(command)
	return f.LockRequest{Key: k, Mode: f.Exclusive}
}
func initializationIdentity(r pc.InitializationRequest) (f.CommandIdentity, error) {
	if r.Validate() != nil {
		return f.CommandIdentity{}, invalid()
	}
	return f.NewCommandIdentity("skills", []string{r.ProjectID.String()}, "initialize", r.InitializationKey)
}
func initializationActor(actor id.Actor, r pc.InitializationRequest) error {
	if actor.Validate() != nil || r.Validate() != nil {
		return invalid()
	}
	d := actor.Details()
	if d.Kind != id.Service || d.ServiceName != id.ProjectInitialization || d.ProjectID != r.ProjectID.String() || d.CauseRef != r.CreationID.String() {
		return fault(f.Forbidden)
	}
	return nil
}
