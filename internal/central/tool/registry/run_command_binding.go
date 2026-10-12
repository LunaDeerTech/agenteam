package registry

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type runCommandMetadataSource struct{ readFileMetadataSource }

// NewRunCommandMetadataSource only binds fixed metadata to this Store. It does
// not enable dispatch, an online Runner, any approval or an authorization grant.
func NewRunCommandMetadataSource(store Store) (RunnerSource, error) {
	if nilPort(store) {
		return nil, fail(f.DependencyUnbound)
	}
	return &runCommandMetadataSource{readFileMetadataSource{store: store}}, nil
}
func (s *runCommandMetadataSource) DescribeInTx(ctx context.Context, tx f.Tx) (tc.RunnerRegistration, error) {
	if err := s.current(ctx, tx); err != nil {
		return tc.RunnerRegistration{}, err
	}
	return tc.RunCommandRegistration(), nil
}
func (s *runCommandMetadataSource) CheckBindingInTx(ctx context.Context, tx f.Tx, v tc.RunnerRegistration) error {
	if err := s.current(ctx, tx); err != nil {
		return err
	}
	if !v.Active || v.Binding != tc.RunCommandRegistration().Binding {
		return fail(f.InvalidArgument)
	}
	return v.Validate(ctx)
}
