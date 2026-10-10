package runtime

import (
	"context"
	"errors"
	"reflect"
	"slices"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

type Store interface {
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

type Service struct{ data func() serviceState }
type serviceState struct {
	store      Store
	executions tc.OperationExecutionAuthority
}

func New(store Store, executions tc.OperationExecutionAuthority) (*Service, error) {
	if nilPort(store) || nilPort(executions) {
		return nil, fail(f.DependencyUnbound)
	}
	s := serviceState{store, executions}
	return &Service{func() serviceState { return s }}, nil
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
func fail(code f.Code) error { return f.NewFault(code, f.NotStarted) }
func contextError(ctx context.Context) error {
	if ctx == nil {
		return fail(f.InvalidArgument)
	}
	return ctx.Err()
}
func portError(err error) error {
	if err == nil {
		return nil
	}
	var fault *f.Fault
	if errors.As(err, &fault) {
		return fault
	}
	return f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(err)
}

type CommitFailure struct{ result f.CommitResult }

func (e CommitFailure) Error() string          { return "tool_operation_commit_unknown" }
func (e CommitFailure) Result() f.CommitResult { return e.result }
func commitError(result f.CommitResult) error {
	switch result.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		if err := result.Fault(); err != nil {
			return err
		}
		return fail(f.DependencyUnavailable)
	}
	err := f.NewFault(f.CommitUnknown, f.Unknown)
	err.RetryHint = "lookup"
	if result.AttemptID().Validate() == nil {
		err.CauseID = result.AttemptID().String()
	}
	return err.WithCause(CommitFailure{result})
}

func inputCause(b tc.ToolCallBinding) (f.TransactionCause, error) {
	raw, err := encode(struct{ Logical, Invocation, Call string }{b.LogicalCallID.String(), b.InvocationID.String(), b.CallID})
	if err != nil {
		return f.TransactionCause{}, err
	}
	command, err := f.NewCommandIdentity("tool.operation", []string{b.ExecutionID.String()}, "prepare", f.IdempotencyKey(digest(raw)))
	if err != nil {
		return f.TransactionCause{}, portError(err)
	}
	return f.NewCommandsCause(command)
}

func unionLocks(requests []f.LockRequest) ([]f.LockRequest, error) {
	requests = slices.Clone(requests)
	for _, r := range requests {
		if r.Key.Validate() != nil || !r.Mode.Valid() {
			return nil, fail(f.InvalidArgument)
		}
	}
	slices.SortFunc(requests, func(a, b f.LockRequest) int { return f.CompareLockKeys(a.Key, b.Key) })
	n := 0
	for _, r := range requests {
		if n > 0 && f.CompareLockKeys(requests[n-1].Key, r.Key) == 0 {
			if r.Mode == f.Exclusive {
				requests[n-1].Mode = f.Exclusive
			}
			continue
		}
		requests[n] = r
		n++
	}
	return requests[:n], nil
}

func baseLocks(b tc.ToolCallBinding, cause f.TransactionCause, extra []f.LockRequest) ([]f.LockRequest, error) {
	command, err := f.CommandLock(cause.Details().Primary)
	if err != nil {
		return nil, portError(err)
	}
	project, _ := f.ProjectLock(b.ProjectID.String())
	agent, _ := f.AgentLock(b.AgentID.String())
	execution, _ := f.AggregateLock(f.ExecutionAggregate, b.ExecutionID.String())
	locks := []f.LockRequest{{Key: command, Mode: f.Exclusive}, registry.RegistryLock(f.Shared), {Key: project, Mode: f.Shared}, {Key: agent, Mode: f.Shared}, {Key: execution, Mode: f.Exclusive}}
	return unionLocks(append(locks, extra...))
}

func (s *Service) current(ctx context.Context, b tc.ToolCallBinding, fn func(context.Context, postgres.SQLExecutor) error) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if err := b.Validate(); err != nil {
		return err
	}
	if s == nil || s.data == nil {
		return fail(f.DependencyUnbound)
	}
	d := s.data()
	plan, err := d.executions.DiscoverToolCall(ctx, b)
	if err != nil {
		return portError(err)
	}
	if nilPort(plan) {
		return fail(f.DependencyUnbound)
	}
	cause, err := inputCause(b)
	if err != nil {
		return err
	}
	locks, err := baseLocks(b, cause, plan.RequiredLocks())
	if err != nil {
		return err
	}
	result := d.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		x, err := d.store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		if err = d.store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err = d.store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err = d.executions.RequireToolCallInTx(ctx, tx, b, plan); err != nil {
			return portError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = fn(ctx, x); err != nil {
			return err
		}
		return ctx.Err()
	})
	return commitError(result)
}

// PrepareInstall persists only the fixed typed profile's created metadata. It
// neither starts an Attempt nor certifies schema validation or authorization.
// A repeated exact original input returns the same IDs without a second insert.
func (s *Service) PrepareInstall(ctx context.Context, b tc.ToolCallBinding, rawArguments []byte) (PreparedOperation, error) {
	input, err := prepareInput(ctx, b, rawArguments)
	if err != nil {
		return PreparedOperation{}, err
	}
	raw, err := encode(input)
	if err != nil {
		return PreparedOperation{}, err
	}
	want := digest(raw)
	var record operationRecord
	err = s.current(ctx, b, func(ctx context.Context, x postgres.SQLExecutor) error {
		old, err := loadOperation(ctx, x, b)
		if err != nil {
			return err
		}
		if old != nil {
			if old.InputDigest != want {
				return fail(f.IdempotencyKeyReused)
			}
			record = *old
			return nil
		}
		operation, err := f.NewID[tc.Operation]()
		if err != nil {
			return portError(err)
		}
		skillID, err := f.NewID[pc.Skill]()
		if err != nil {
			return portError(err)
		}
		key, err := builtin.SkillInstallKey(operation.String())
		if err != nil {
			return err
		}
		fp, err := fingerprint(input, skillID)
		if err != nil {
			return err
		}
		record = operationRecord{operation, input, want, skillID, key, fp, "created", 1}
		return insertOperation(ctx, x, record)
	})
	if err != nil {
		return PreparedOperation{}, err
	}
	return PreparedOperation{func() operationRecord { return record }}, nil
}

// LookupInstall does no insertion or attempt work. false only means no observed
// committed metadata; it cannot certify that an older Unknown cannot commit.
func (s *Service) LookupInstall(ctx context.Context, b tc.ToolCallBinding) (PreparedOperation, bool, error) {
	var record *operationRecord
	err := s.current(ctx, b, func(ctx context.Context, x postgres.SQLExecutor) error {
		var err error
		record, err = loadOperation(ctx, x, b)
		if err != nil || record == nil {
			return err
		}
		want, _ := encode(bindingProjection(b))
		got, _ := encode(record.Input.Binding)
		if digest(want) != digest(got) {
			return fail(f.IdempotencyKeyReused)
		}
		return nil
	})
	if err != nil {
		return PreparedOperation{}, false, err
	}
	if record == nil {
		return PreparedOperation{}, false, nil
	}
	copy := *record
	return PreparedOperation{func() operationRecord { return copy }}, true, nil
}
