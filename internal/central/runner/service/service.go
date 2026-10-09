// Package service owns Runner management and connection facts in one Store.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"sync"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	c "github.com/LunaDeerTech/agenteam/internal/central/runner/contract"
)

type Store interface {
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

// Authority is shared by the Runner service and its typed Audit binding. It
// carries no grant across requests: every Human operation rechecks the Account.
type Authority struct{ data func() *authorityState }
type authorityState struct {
	store  Store
	system id.SystemAuthority
}

func NewAuthority(store Store, system id.SystemAuthority) (*Authority, error) {
	if nilPort(store) || nilPort(system) {
		return nil, fault(f.DependencyUnbound)
	}
	state := &authorityState{store, system}
	return &Authority{data: func() *authorityState { return state }}, nil
}
func (a *Authority) state() *authorityState {
	if a == nil || a.data == nil {
		return nil
	}
	return a.data()
}

type Service struct{ data func() *serviceState }
type serviceState struct {
	authority *Authority
	audit     ac.Appender
	owner     f.ID[connectionOwner]
	mu        sync.Mutex
	stopped   bool
	calls     map[*call]struct{}
	changed   chan struct{}
}
type call struct{ cancel context.CancelFunc }

func New(authority *Authority, audit ac.Appender) (*Service, error) {
	if authority.state() == nil || nilPort(audit) {
		return nil, fault(f.DependencyUnbound)
	}
	owner, e := f.NewID[connectionOwner]()
	if e != nil {
		return nil, unavailable(e)
	}
	state := &serviceState{authority: authority, audit: audit, owner: owner, calls: map[*call]struct{}{}, changed: make(chan struct{})}
	return &Service{data: func() *serviceState { return state }}, nil
}
func (s *Service) state() *serviceState {
	if s == nil || s.data == nil {
		return nil
	}
	return s.data()
}
func (s *Service) begin(ctx context.Context) (context.Context, func(), error) {
	st := s.state()
	if st == nil {
		return nil, nil, fault(f.DependencyUnbound)
	}
	if ctx == nil {
		return nil, nil, fault(f.InvalidArgument)
	}
	if ctx.Err() != nil {
		return nil, nil, canceled(ctx.Err())
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.stopped {
		return nil, nil, fault(f.ShuttingDown)
	}
	owned, cancel := context.WithCancel(ctx)
	entry := &call{cancel}
	st.calls[entry] = struct{}{}
	var once sync.Once
	done := func() {
		once.Do(func() {
			cancel()
			st.mu.Lock()
			delete(st.calls, entry)
			close(st.changed)
			st.changed = make(chan struct{})
			st.mu.Unlock()
		})
	}
	return owned, done, nil
}
func (s *Service) Stop() {
	st := s.state()
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.stopped = true
	for call := range st.calls {
		call.cancel()
	}
}
func (s *Service) Drain(ctx context.Context) error {
	st := s.state()
	if st == nil {
		return nil
	}
	if ctx == nil {
		return fault(f.InvalidArgument)
	}
	for {
		st.mu.Lock()
		if len(st.calls) == 0 {
			st.mu.Unlock()
			return nil
		}
		changed := st.changed
		st.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
func (s *Service) Force(ctx context.Context) error { s.Stop(); return s.Drain(ctx) }
func (Service) Format(w fmt.State, _ rune)         { _, _ = io.WriteString(w, "runner_service") }
func (Service) MarshalJSON() ([]byte, error)       { return []byte(`"runner_service"`), nil }
func (Service) LogValue() slog.Value               { return slog.StringValue("runner_service") }
func (Authority) Format(w fmt.State, _ rune)       { _, _ = io.WriteString(w, "runner_authority") }
func (Authority) MarshalJSON() ([]byte, error)     { return []byte(`"runner_authority"`), nil }
func (Authority) LogValue() slog.Value             { return slog.StringValue("runner_authority") }
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	x := reflect.ValueOf(v)
	switch x.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Func, reflect.Slice:
		return x.IsNil()
	}
	return false
}
func fault(code f.Code) error { return f.NewFault(code, f.NotStarted) }
func unavailable(e error) error {
	return f.NewFault(f.DependencyUnavailable, f.NotStarted).WithCause(e)
}
func canceled(e error) error { return f.NewFault(f.DependencyUnavailable, f.NotCommitted).WithCause(e) }
func portError(e error) error {
	if e == nil {
		return nil
	}
	var ff *f.Fault
	if errors.As(e, &ff) {
		copy := *ff
		copy.FieldErrors = append([]f.FieldError(nil), ff.FieldErrors...)
		return &copy
	}
	return unavailable(e)
}

type commitFailure struct{ result f.CommitResult }

func (commitFailure) Error() string                  { return string(f.CommitUnknown) }
func (e commitFailure) CommitResult() f.CommitResult { return e.result }
func resultError(result f.CommitResult) error {
	switch result.State() {
	case f.Committed:
		return nil
	case f.NotCommitted:
		original := result.Fault()
		var database *postgres.Error
		if original.Code == f.InternalError && errors.As(original, &database) && database != nil && databaseAdmissionUnavailable(database.Code()) {
			copy := *original
			copy.Code = f.DependencyUnavailable
			return &copy
		}
		return original
	}
	v := f.NewFault(f.CommitUnknown, f.Unknown)
	v.RetryHint = "lookup"
	if result.AttemptID().Validate() == nil {
		v.CauseID = result.AttemptID().String()
	}
	return v.WithCause(commitFailure{result})
}

// Only borrow/admission failures are classified here. D03 also uses Error for
// poisoned SQL, invalid handles, lock errors and other internal defects; those
// retain their original domain code and must not become retryable outages.
func databaseAdmissionUnavailable(code postgres.Code) bool {
	return code == postgres.AdmissionStopped || code == postgres.ConnectionFailed
}

var _ c.Reader = (*Service)(nil)

func (commitFailure) Format(w fmt.State, _ rune) { _, _ = io.WriteString(w, "runner_commit_outcome") }
func (commitFailure) LogValue() slog.Value       { return slog.StringValue("runner_commit_outcome") }
