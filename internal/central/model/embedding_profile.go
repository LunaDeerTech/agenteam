package model

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// EmbeddingProfileReader performs only authorized metadata reads. Its domain
// caller owns the synchronous lifetime; no Model call or Secret usage is made.
type EmbeddingProfileReader struct {
	authority *Authority
	consumers mc.EmbeddingProfileAuthority
}

func NewEmbeddingProfileReader(authority *Authority, consumers mc.EmbeddingProfileAuthority) (*EmbeddingProfileReader, error) {
	if authority.state() == nil || nilPort(consumers) {
		return nil, fault(f.DependencyUnbound)
	}
	return &EmbeddingProfileReader{authority, consumers}, nil
}
func (s *EmbeddingProfileReader) ReadEmbeddingProfile(ctx context.Context, request mc.EmbeddingProfileRequest) (mc.EmbeddingProfile, error) {
	var zero mc.EmbeddingProfile
	if err := resolutionContextError(ctx); err != nil {
		return zero, err
	}
	if s == nil || s.authority.state() == nil || nilPort(s.consumers) {
		return zero, fault(f.DependencyUnbound)
	}
	r := request.Clone()
	binding, err := mc.EmbeddingProfileBinding(r)
	if err != nil {
		return zero, fault(f.InvalidArgument)
	}
	plan, err := s.consumers.DiscoverEmbeddingProfile(ctx, r.Clone())
	if err != nil {
		return zero, portError(err)
	}
	if plan.Validate() != nil || plan.Details().Binding != binding || plan.Details().RetryPolicy != nil {
		return zero, fault(f.Forbidden)
	}
	store := s.authority.state().store
	// Candidate failures are withheld until the original domain authority has
	// been revalidated. No candidate data or error grants configuration access.
	candidate, candidateErr := readEmbeddingProfile(ctx, store)
	locks := []f.LockRequest{projectLock(r.Consumer.ProjectID.String()), systemLock("model-platform-selection", f.Shared)}
	if candidate.ModelID.Validate() == nil {
		locks = append(locks, aggregateLock(f.ModelConfigAggregate, candidate.ModelID.String(), f.Shared))
	}
	if candidate.ProviderID.Validate() == nil {
		locks = append(locks, aggregateLock(f.ProviderAggregate, candidate.ProviderID.String(), f.Shared))
	}
	locks, err = resolutionUnion(locks, plan.RequiredLocks())
	if err != nil {
		return zero, err
	}
	cause, err := readCause("embedding-profile")
	if err != nil {
		return zero, err
	}
	var out mc.EmbeddingProfile
	result := store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := store.AcquireAll(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := store.RequireHeldLocks(ctx, tx, locks); err != nil {
			return portError(err)
		}
		if err := s.consumers.RequireEmbeddingProfileInTx(ctx, tx, r.Clone(), plan); err != nil {
			return portError(err)
		}
		x, err := store.InTx(tx)
		if err != nil {
			return portError(err)
		}
		actual, actualErr := readEmbeddingProfile(ctx, x)
		if candidateErr != nil {
			if actualErr == nil {
				return fault(f.ResourceBusy)
			}
			return actualErr
		}
		if actualErr != nil {
			return actualErr
		}
		if actual != candidate {
			return fault(f.ResourceBusy)
		}
		out = actual
		return nil
	})
	if err := commitError(result); err != nil {
		return zero, err
	}
	if err := resolutionContextError(ctx); err != nil {
		return zero, err
	}
	return out, nil
}
func readEmbeddingProfile(ctx context.Context, x postgres.SQLExecutor) (mc.EmbeddingProfile, error) {
	var out mc.EmbeddingProfile
	selection, err := loadSelection(ctx, x)
	if err != nil {
		return out, err
	}
	if selection == nil || !selection.Configured || selection.Embedding == "" {
		return out, fault(f.InvalidState)
	}
	out.SelectionVersion = selection.Version
	m, err := loadModel(ctx, x, selection.Embedding)
	if err != nil {
		return out, err
	}
	if m == nil {
		return out, fault(f.NotFound)
	}
	out.ModelID, err = f.ParseID[mc.Model](m.ID)
	if err != nil {
		return out, unavailable(err)
	}
	out.ModelVersion = m.Version
	p, err := loadProviderScope(ctx, x, m.ProviderID, configurationScope(m.Project))
	if err != nil {
		return out, err
	}
	if p == nil {
		return out, unavailable(nil)
	}
	out.ProviderID, err = f.ParseID[mc.Provider](p.ID)
	if err != nil {
		return out, unavailable(err)
	}
	out.ProviderVersion = p.Version
	out.AdapterRevision, err = resolutionProfile(p, m, mc.SelectionRef{Kind: "platform", Selector: mc.EmbeddingSelector})
	if err != nil {
		return out, err
	}
	if m.Input.Capabilities.Embedding == nil {
		return out, fault(f.InvalidState)
	}
	out.Dimensions = int(m.Input.Capabilities.Embedding.Dimensions)
	out.DimensionsMode = m.Input.Capabilities.Embedding.DimensionsMode
	if err := out.Validate(); err != nil {
		return out, unavailable(err)
	}
	return out, nil
}

var _ mc.EmbeddingProfileReader = (*EmbeddingProfileReader)(nil)
