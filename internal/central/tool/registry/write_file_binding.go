package registry

import (
	"context"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

type writeFileMetadataSource struct{ readFileMetadataSource }

// NewWriteFileMetadataSource only binds fixed metadata to this Store. It does
// not enable dispatch, an online Runner, any approval or an authorization grant.
func NewWriteFileMetadataSource(store Store) (RunnerSource, error) {
	if nilPort(store) {
		return nil, fail(f.DependencyUnbound)
	}
	return &writeFileMetadataSource{readFileMetadataSource{store: store}}, nil
}
func (s *writeFileMetadataSource) DescribeInTx(ctx context.Context, tx f.Tx) (tc.RunnerRegistration, error) {
	if err := s.current(ctx, tx); err != nil {
		return tc.RunnerRegistration{}, err
	}
	return tc.WriteFileRegistration(), nil
}
func (s *writeFileMetadataSource) CheckBindingInTx(ctx context.Context, tx f.Tx, v tc.RunnerRegistration) error {
	if err := s.current(ctx, tx); err != nil {
		return err
	}
	if !v.Active || v.Binding != tc.WriteFileRegistration().Binding {
		return fail(f.InvalidArgument)
	}
	return v.Validate(ctx)
}
