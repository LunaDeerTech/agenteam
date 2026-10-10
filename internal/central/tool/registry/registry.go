// Package registry owns persistent Tool metadata and configuration references.
// It does not execute tools, mint Agent authority or implement install-skill.
package registry

import (
	"context"
	"errors"
	"reflect"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type Store interface {
	postgres.SQLExecutor
	InTx(f.Tx) (postgres.SQLExecutor, error)
	WithinTx(context.Context, f.TransactionCause, func(context.Context, f.Tx) error) f.CommitResult
	AcquireAll(context.Context, f.Tx, []f.LockRequest) error
	RequireHeldLocks(context.Context, f.Tx, []f.LockRequest) error
}

// BuiltinSource is a trusted domain owner fixed at composition, not request
// input. DescribeInTx returns its current canonical definition and exact code
// bindings. CheckBindingInTx must prove the actual backend, scope resolver and
// risk classifier represented by that descriptor are bound. A nonnil interface,
// handler string, persisted row or test double does not establish this fact.
// Both methods use this caller's Store/Tx, add no locks and perform no I/O beyond
// authorized metadata. No production source for install-skill is supplied here.
type BuiltinSource interface {
	DescribeInTx(context.Context, f.Tx) (tc.BuiltinRegistration, error)
	CheckBindingInTx(context.Context, f.Tx, tc.BuiltinRegistration) error
}

type ProjectAuthority interface {
	AuthorizeProject(context.Context, f.Tx, id.Actor, id.ProjectID, id.AccessIntent) (id.AccessGrant, error)
}
type Authorizations struct {
	Sessions id.SessionAuthority
	Projects ProjectAuthority
}
type Options struct {
	Sources        map[string]BuiltinSource
	Authorizations Authorizations
}
type state struct {
	store   Store
	sources map[string]BuiltinSource
	auth    Authorizations
}

// A closure keeps default recursive formatting from walking registration data.
type Registry struct{ state func() *state }

func New(store Store, options Options) (*Registry, error) {
	if nilPort(store) {
		return nil, fail(f.DependencyUnbound)
	}
	sources := make(map[string]BuiltinSource, len(options.Sources))
	for key, source := range options.Sources {
		if !tc.ValidBuiltinKey(key) {
			return nil, fail(f.InvalidArgument)
		}
		if nilPort(source) {
			return nil, fail(f.DependencyUnbound)
		}
		sources[key] = source
	}
	s := &state{store: store, sources: sources, auth: options.Authorizations}
	return &Registry{state: func() *state { return s }}, nil
}
func (r *Registry) data() *state {
	if r == nil || r.state == nil {
		return nil
	}
	return r.state()
}
func nilPort(v any) bool {
	if v == nil {
		return true
	}
	switch reflect.ValueOf(v).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflect.ValueOf(v).IsNil()
	}
	return false
}
func fail(code f.Code) error { return f.NewFault(code, f.NotStarted) }
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

// RegistryLock is part of a caller's pre-collected complete lock set. Reads and
// reference writes take SH; reconciliation takes EX. Unregistration cannot race
// an Agent's current directory check and atomic reference write.
func RegistryLock(mode f.LockMode) f.LockRequest {
	k, _ := f.SystemConfigLock("tool-registry")
	return f.LockRequest{Key: k, Mode: mode}
}
func toolLock(tool id.ToolID, mode f.LockMode) f.LockRequest {
	k, _ := f.AggregateLock(f.ToolSpecAggregate, tool.String())
	return f.LockRequest{Key: k, Mode: mode}
}
func (r *Registry) executor(ctx context.Context, tx f.Tx, locks []f.LockRequest) (postgres.SQLExecutor, error) {
	if err := ctx.Err(); err != nil {
		return nil, portError(err)
	}
	s := r.data()
	if s == nil {
		return nil, fail(f.DependencyUnbound)
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, portError(err)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, locks); err != nil {
		return nil, portError(err)
	}
	return x, nil
}
func (r *Registry) source(ctx context.Context, tx f.Tx, key string) (tc.BuiltinRegistration, []byte, error) {
	s := r.data()
	if s == nil {
		return tc.BuiltinRegistration{}, nil, fail(f.DependencyUnbound)
	}
	provider, ok := s.sources[key]
	if !ok {
		return tc.BuiltinRegistration{}, nil, fail(f.DependencyUnbound)
	}
	registration, err := provider.DescribeInTx(ctx, tx)
	if err != nil {
		return tc.BuiltinRegistration{}, nil, portError(err)
	}
	if registration.Definition.StableKey != key {
		return tc.BuiltinRegistration{}, nil, fail(f.InvalidState)
	}
	if err = registration.Validate(ctx); err != nil {
		return tc.BuiltinRegistration{}, nil, portError(err)
	}
	definition, err := tc.CanonicalDefinition(ctx, registration.Definition)
	if err != nil {
		return tc.BuiltinRegistration{}, nil, portError(err)
	}
	// Inactive definitions are not callable and need not keep an obsolete backend
	// alive merely to remove current registration. They can never return valid.
	if registration.Active {
		if err = provider.CheckBindingInTx(ctx, tx, registration); err != nil {
			return tc.BuiltinRegistration{}, nil, portError(err)
		}
	}
	if err = ctx.Err(); err != nil {
		return tc.BuiltinRegistration{}, nil, portError(err)
	}
	return registration, definition, nil
}

func (r *Registry) currentProject(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID) error {
	if actor.Validate() != nil {
		return fail(f.Unauthenticated)
	}
	if actor.Details().Kind != id.Human {
		return fail(f.Forbidden)
	}
	if project.Validate() != nil {
		return fail(f.InvalidArgument)
	}
	s := r.data()
	if s == nil || nilPort(s.auth.Sessions) || nilPort(s.auth.Projects) {
		return fail(f.DependencyUnbound)
	}
	u, _ := f.UserLock(actor.Details().UserID)
	p, _ := f.ProjectLock(project.String())
	if _, err := r.executor(ctx, tx, []f.LockRequest{{Key: u, Mode: f.Shared}, {Key: p, Mode: f.Shared}, RegistryLock(f.Shared)}); err != nil {
		return err
	}
	if err := s.auth.Sessions.RequireCurrentSession(ctx, tx, actor); err != nil {
		return portError(err)
	}
	grant, err := s.auth.Projects.AuthorizeProject(ctx, tx, actor, project, id.Read)
	if err != nil {
		return portError(err)
	}
	scope, _ := id.InProject(project)
	if !grant.Matches(actor, scope, id.Read) {
		return fail(f.Forbidden)
	}
	return nil
}
