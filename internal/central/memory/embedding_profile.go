package memory

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
)

func (a *ModelAuthority) profileRequest(entry *call) (mc.EmbeddingProfileRequest, error) {
	if a == nil || entry == nil || entry.service != a.domain || entry.operation.Validate() != nil || entry.namespace.Validate() != nil {
		return mc.EmbeddingProfileRequest{}, fault(f.Forbidden)
	}
	scope, _ := i.InProject(entry.namespace.ProjectID)
	actor, err := a.registration.Actor(entry.operation.String(), scope)
	if err != nil {
		return mc.EmbeddingProfileRequest{}, err
	}
	agent := entry.namespace.AgentID
	return mc.EmbeddingProfileRequest{Actor: actor, Consumer: mc.Consumer{Kind: mc.MemoryConsumer, ProjectID: entry.namespace.ProjectID, AgentID: &agent, OperationID: entry.operation.String(), Purpose: mc.MemoryEmbedding}}, nil
}

func (a *ModelAuthority) profileWitness(ctx context.Context, request mc.EmbeddingProfileRequest) (*call, error) {
	if checkContext(ctx) != nil || a == nil || request.Validate() != nil {
		return nil, fault(f.Forbidden)
	}
	entry, _ := ctx.Value(callKey{}).(*call)
	expected, err := a.profileRequest(entry)
	if err != nil || !request.Actor.Equal(expected.Actor) || !request.Consumer.Equal(expected.Consumer) {
		return nil, fault(f.Forbidden)
	}
	if entry.intent != "retain" && entry.intent != "recall" && entry.intent != "reflect" || entry.actor.Details().Kind != i.AgentRun {
		return nil, fault(f.Forbidden)
	}
	a.domain.mu.Lock()
	_, active := a.domain.calls[entry]
	stopped := a.domain.stopped
	a.domain.mu.Unlock()
	if !active || stopped {
		return nil, fault(f.Forbidden)
	}
	return entry, nil
}

func (a *ModelAuthority) DiscoverEmbeddingProfile(ctx context.Context, request mc.EmbeddingProfileRequest) (mc.ConsumerDependencies, error) {
	entry, err := a.profileWitness(ctx, request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	binding, err := mc.EmbeddingProfileBinding(request)
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	raw, err := encode(struct {
		Actor           i.ActorDetails
		Intent, Process string
	}{entry.actor.Details(), entry.intent, a.process.String()})
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	locks, err := normalizeLocks(append(append([]f.LockRequest{}, entry.locks...), namespaceLock(entry.namespace, f.Shared)))
	if err != nil {
		return mc.ConsumerDependencies{}, err
	}
	return mc.NewConsumerDependencies(a.issuer, mc.ConsumerDependencyDetails{Binding: binding, Mapping: digest(raw), Locks: locks})
}

func (a *ModelAuthority) RequireEmbeddingProfileInTx(ctx context.Context, tx f.Tx, request mc.EmbeddingProfileRequest, plan mc.ConsumerDependencies) error {
	entry, err := a.profileWitness(ctx, request)
	if err != nil {
		return err
	}
	current, err := a.DiscoverEmbeddingProfile(ctx, request)
	if err != nil {
		return err
	}
	if plan.Validate() != nil || plan.Details().RetryPolicy != nil || !plan.Matches(a.issuer, current.Details().Binding, current.Details().Mapping) {
		return fault(f.Forbidden)
	}
	if _, err := a.store.InTx(tx); err != nil {
		return portError(err)
	}
	if err := a.store.RequireHeldLocks(ctx, tx, current.RequiredLocks()); err != nil {
		return portError(err)
	}
	if a.guard == nil || a.process.Validate() != nil {
		return fault(f.DependencyUnbound)
	}
	if err := a.guard.RequireCurrentProcessInTx(ctx, tx, a.process); err != nil {
		return portError(err)
	}
	return a.domain.requireAccess(ctx, tx, entry, false)
}

func (m *Models) currentProfile(ctx context.Context, entry *call) (mc.EmbeddingProfile, error) {
	request, err := m.authority.profileRequest(entry)
	if err != nil {
		return mc.EmbeddingProfile{}, err
	}
	profile, err := m.deps.Profiles.ReadEmbeddingProfile(ctx, request)
	if err != nil {
		return mc.EmbeddingProfile{}, portError(err)
	}
	if profile.Validate() != nil {
		return mc.EmbeddingProfile{}, fault(f.DependencyUnavailable)
	}
	return profile, nil
}

func (s *Service) servingProfileMatches(ctx context.Context, entry *call, serving rc.ServingIdentity, current mc.EmbeddingProfile) (bool, error) {
	cause, err := readCause("generation-profile")
	if err != nil {
		return false, err
	}
	matched := false
	err = s.transaction(ctx, entry, false, cause, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) error {
		if err := requireServing(ctx, x, entry.namespace, serving); err != nil {
			return err
		}
		var raw []byte
		var stored *string
		if err := x.QueryRow(ctx, `SELECT profile_data,profile_digest FROM agenteam_memory.generations WHERE generation_id=$1`, serving.GenerationID).Scan(&raw, &stored); err != nil {
			return portError(err)
		}
		if raw == nil && stored == nil {
			return nil // Legacy profile remains pinned until a complete rebuild.
		}
		var profile mc.EmbeddingProfile
		if stored == nil || json.Unmarshal(raw, &profile) != nil || profile.Validate() != nil {
			return fault(f.DependencyUnavailable)
		}
		digest, err := profile.Digest()
		if err != nil || string(digest) != *stored {
			return fault(f.DependencyUnavailable)
		}
		matched = profile == current && serving.Dimension == current.Dimensions
		return nil
	})
	return matched, err
}

func profileMatchesSnapshot(profile mc.EmbeddingProfile, snapshot mc.ConfigSnapshot) bool {
	return profile.Validate() == nil && snapshot.SelectionVersion != nil && *snapshot.SelectionVersion == profile.SelectionVersion &&
		snapshot.Capabilities.Embedding != nil && int(snapshot.Capabilities.Embedding.Dimensions) == profile.Dimensions &&
		snapshot.Capabilities.Embedding.DimensionsMode == profile.DimensionsMode &&
		snapshot.Identity.ModelID == profile.ModelID && snapshot.Identity.ProviderID == profile.ProviderID && snapshot.Identity.AdapterRevision == profile.AdapterRevision
}

var _ mc.EmbeddingProfileAuthority = (*ModelAuthority)(nil)
