package runtime

import (
	"bytes"
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

type BuiltinSource struct {
	authority *BuiltinAuthority
	backend   BuiltinBackend
}

func NewBuiltinSource(authority *BuiltinAuthority, backend BuiltinBackend) (*BuiltinSource, error) {
	if authority == nil || nilPort(backend) {
		return nil, fail(f.DependencyUnbound)
	}
	reg := backend.Registration()
	if !tc.WorkArtifactBuiltinBinding(reg.Binding) || reg.Validate(context.Background()) != nil || !reg.Active {
		return nil, fail(f.InvalidArgument)
	}
	return &BuiltinSource{authority, backend}, nil
}

func (s *BuiltinSource) require(ctx context.Context, tx f.Tx) error {
	if err := contextError(ctx); err != nil {
		return err
	}
	if s == nil || s.authority == nil || nilPort(s.backend) {
		return fail(f.DependencyUnbound)
	}
	if _, err := s.authority.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := s.authority.store.RequireHeldLocks(ctx, tx, []f.LockRequest{registry.RegistryLock(f.Shared)}); err != nil {
		return portError(err)
	}
	if process, err := s.authority.guard.CurrentProcess(); err != nil || process != s.authority.process {
		return fail(f.DependencyUnavailable)
	}
	return s.backend.RequireBindingInTx(ctx, tx, s.authority.store)
}
func (s *BuiltinSource) DescribeInTx(ctx context.Context, tx f.Tx) (tc.BuiltinRegistration, error) {
	if err := s.require(ctx, tx); err != nil {
		return tc.BuiltinRegistration{}, err
	}
	return s.backend.Registration(), nil
}
func (s *BuiltinSource) CheckBindingInTx(ctx context.Context, tx f.Tx, got tc.BuiltinRegistration) error {
	if err := s.require(ctx, tx); err != nil {
		return err
	}
	want := s.backend.Registration()
	if got.Active != want.Active || got.Binding != want.Binding || got.ScopeResolverID != want.ScopeResolverID || got.RiskClassifierID != want.RiskClassifierID || got.Class != want.Class {
		return fail(f.InvalidState)
	}
	a, err := tc.CanonicalDefinition(ctx, got.Definition)
	if err != nil {
		return err
	}
	b, err := tc.CanonicalDefinition(ctx, want.Definition)
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return fail(f.InvalidState)
	}
	return nil
}

var _ registry.BuiltinSource = (*BuiltinSource)(nil)
