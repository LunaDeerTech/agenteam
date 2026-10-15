package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/LunaDeerTech/agenteam/internal/central/cursor"
	ec "github.com/LunaDeerTech/agenteam/internal/central/execution/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/meeting/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/outbox/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type Store interface {
	postgres.SQLExecutor
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

func sameStore(a, b any) bool {
	return a != nil && b != nil && reflect.TypeOf(a).Comparable() && reflect.TypeOf(b).Comparable() && a == b
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	x := reflect.ValueOf(v)
	switch x.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice:
		return x.IsNil()
	}
	return false
}
func fault(code f.Code) *f.Fault { return f.NewFault(code, f.NotStarted) }
func portError(err error) error {
	var known *f.Fault
	if err == nil || errors.As(err, &known) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fault(f.DependencyUnavailable).WithCause(err)
}

type commitFailure struct{ result f.CommitResult }

func (commitFailure) Error() string { return "meeting_commit_outcome" }
func UnknownAttempt(err error) (f.CommitResult, bool) {
	var v commitFailure
	if errors.As(err, &v) {
		return v.result, true
	}
	return f.CommitResult{}, false
}
func commitError(r f.CommitResult) error {
	switch r.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		if r.Fault() != nil {
			return r.Fault()
		}
		return f.NewFault(f.InternalError, f.NotCommitted)
	}
	e := f.NewFault(f.CommitUnknown, f.Unknown)
	e.RetryHint = "lookup"
	if r.AttemptID().Validate() == nil {
		e.CauseID = r.AttemptID().String()
	}
	return e.WithCause(commitFailure{r})
}
func canonical(v any) ([]byte, error) {
	raw, e := json.Marshal(v)
	if e != nil {
		return nil, fault(f.InvalidArgument)
	}
	return cursor.CanonicalJSON(raw)
}
func digest(v []byte) f.Digest { return ec.TriggerInputDigest(v) }
func now() time.Time           { return time.Now().UTC().Truncate(time.Microsecond) }
func projectLock(p i.ProjectID, mode f.LockMode) f.LockRequest {
	k, _ := f.ProjectLock(p.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func meetingLock(id c.MeetingID, mode f.LockMode) f.LockRequest {
	k, _ := f.AggregateLock(f.MeetingAggregate, id.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func userLock(a i.Actor) f.LockRequest {
	k, _ := f.UserLock(a.Details().UserID)
	return f.LockRequest{Key: k, Mode: f.Shared}
}
func agentLock(id i.AgentID) f.LockRequest {
	k, _ := f.AgentLock(id.String())
	return f.LockRequest{Key: k, Mode: f.Shared}
}
func commandLock(v f.CommandIdentity) f.LockRequest {
	k, _ := f.CommandLock(v)
	return f.LockRequest{Key: k, Mode: f.Exclusive}
}
func normalize(locks []f.LockRequest) ([]f.LockRequest, error) { return oc.NormalizeLocks(locks) }
func readCause(owner string) (f.TransactionCause, error) {
	id, e := f.NewID[f.Request]()
	if e != nil {
		return f.TransactionCause{}, portError(e)
	}
	return f.NewRecoveryCause(owner, id.String(), "")
}
func human(a i.Actor) error {
	if a.Validate() != nil {
		return fault(f.Unauthenticated)
	}
	if a.Details().Kind != i.Human {
		return fault(f.Forbidden)
	}
	return nil
}
