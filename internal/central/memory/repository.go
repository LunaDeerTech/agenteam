package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/memory/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

type Store interface {
	postgres.SQLExecutor
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	InTx(f.Tx) (postgres.SQLExecutor, error)
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return r.IsNil()
	}
	return false
}
func sameStore(a, b Store) bool {
	return !nilPort(a) && !nilPort(b) && reflect.TypeOf(a) == reflect.TypeOf(b) && reflect.TypeOf(a).Comparable() && a == b
}
func fault(code f.Code) *f.Fault { return f.NewFault(code, f.NotStarted) }
func portError(err error) error {
	if err == nil {
		return nil
	}
	var known *f.Fault
	if errors.As(err, &known) {
		return err
	}
	return fault(f.DependencyUnavailable).WithCause(err)
}
func checkContext(ctx context.Context) error {
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	return portError(ctx.Err())
}
func digest(raw []byte) f.Digest {
	sum := sha256.Sum256(raw)
	return f.Digest("sha256:" + hex.EncodeToString(sum[:]))
}
func encode(v any) ([]byte, error) { raw, err := json.Marshal(v); return raw, portError(err) }
func now() f.Instant               { v, _ := f.NewInstant(time.Now()); return v }
func namespaceLock(n c.Namespace, mode f.LockMode) f.LockRequest {
	key, _ := f.RecordLock(f.ProjectionRecordLock, "memory.namespace:"+n.ProjectID.String()+":"+n.AgentID.String())
	return f.LockRequest{Key: key, Mode: mode}
}
func normalizeLocks(locks []f.LockRequest) ([]f.LockRequest, error) {
	out := slices.Clone(locks)
	for _, l := range out {
		if l.Key.Validate() != nil || !l.Mode.Valid() {
			return nil, fault(f.InvalidArgument)
		}
	}
	slices.SortFunc(out, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	merged := out[:0]
	for _, l := range out {
		if len(merged) > 0 && f.CompareLockKeys(merged[len(merged)-1].Key, l.Key) == 0 {
			if l.Mode == f.Exclusive {
				merged[len(merged)-1].Mode = f.Exclusive
			}
		} else {
			merged = append(merged, l)
		}
	}
	return merged, nil
}
func (s *Service) transaction(ctx context.Context, entry *call, mutate bool, cause f.TransactionCause, fn func(context.Context, f.Tx, postgres.SQLExecutor) error) error {
	if entry == nil || entry.service != s || ctx.Value(callKey{}) != entry {
		return fault(f.Forbidden)
	}
	mode := f.Shared
	if mutate {
		mode = f.Exclusive
	}
	locks, err := normalizeLocks(append(slices.Clone(entry.locks), namespaceLock(entry.namespace, mode)))
	if err != nil {
		return err
	}
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.requireAccess(ctx, tx, entry, mutate); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		return fn(ctx, tx, x)
	})
	return transactionError(result)
}
func transactionError(result f.CommitResult) error {
	switch result.State() {
	case f.Committed:
		return nil
	case f.Unknown:
		err := f.NewFault(f.CommitUnknown, f.Unknown)
		err.CauseID = result.AttemptID().String()
		err.RetryHint = "lookup"
		return err.WithCause(commitOutcome{result})
	default:
		if result.Fault() != nil {
			return result.Fault()
		}
		return fault(f.InternalError)
	}
}

type commitOutcome struct{ result f.CommitResult }

func (commitOutcome) Error() string { return "memory_commit_unknown" }
func (s *Service) requireAccess(ctx context.Context, tx f.Tx, entry *call, mutate bool) error {
	s.mu.Lock()
	_, alive := s.calls[entry]
	stopped := s.stopped
	s.mu.Unlock()
	if !alive || stopped {
		return fault(f.Forbidden)
	}
	if entry.actor.Details().Kind == i.AgentRun {
		if mutate && entry.intent != "retain" {
			return fault(f.Forbidden)
		}
		return portError(s.deps.Tools.RequireMemoryToolInTx(ctx, tx, entry.actor, entry.namespace.ProjectID, entry.namespace.AgentID, entry.operation, entry.intent))
	}
	intent := i.Read
	if mutate {
		intent = i.Mutate
	}
	grant, err := s.deps.Projects.RequireOwnerInTx(ctx, tx, entry.actor, entry.namespace.ProjectID, intent)
	if err != nil {
		return portError(err)
	}
	if !grant.Matches(entry.actor, entry.namespace.ProjectID) {
		return fault(f.Forbidden)
	}
	agent, err := s.deps.Agents.RequireCurrentInTx(ctx, tx, entry.actor, entry.namespace.ProjectID, entry.namespace.AgentID)
	if err != nil {
		return portError(err)
	}
	if agent.Validate() != nil || agent.ProjectID != entry.namespace.ProjectID || agent.AgentID != entry.namespace.AgentID {
		return fault(f.Forbidden)
	}
	return checkContext(ctx)
}
func readCause(name string) (f.TransactionCause, error) {
	id, err := f.NewID[struct{}]()
	if err != nil {
		return f.TransactionCause{}, portError(err)
	}
	return f.NewRecoveryCause("memory", id.String(), name)
}
func commandCause(n c.Namespace, name string, key f.IdempotencyKey) (f.TransactionCause, error) {
	id, err := f.NewCommandIdentity("memory", []string{n.ProjectID.String(), n.AgentID.String()}, name, key)
	if err != nil {
		return f.TransactionCause{}, portError(err)
	}
	return f.NewCommandsCause(id)
}
