package project

import (
	"slices"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

// LifecycleParticipantBinding associates one exact contract version and its
// metadata with a trusted service instance whose Name must remain stable.
type LifecycleParticipantBinding struct {
	Registration c.ParticipantRegistration
	Participant  c.ProjectLifecycleParticipant
}

type lifecycleParticipantKey struct {
	name    c.ParticipantName
	version foundation.Version
}

// LifecycleRegistry is an immutable assembly of current and retained adapters.
// Registration and resolution establish neither authority nor completion.
type LifecycleRegistry struct {
	current  c.RequiredManifest
	bindings map[lifecycleParticipantKey]LifecycleParticipantBinding
}

// LifecyclePlan retains exactly the manifest and adapters for one operation.
// Its private maps and orders are never mutated after successful resolution.
type LifecyclePlan struct {
	manifest     c.RequiredManifest
	participants map[c.ParticipantName]c.ProjectLifecycleParticipant
	stopOrder    []c.ParticipantName
	cleanupOrder []c.ParticipantName
}

func NewLifecycleRegistry(current c.RequiredManifest, bindings []LifecycleParticipantBinding) (*LifecycleRegistry, error) {
	if err := current.Require(); err != nil {
		return nil, err
	}
	r := &LifecycleRegistry{
		current:  current,
		bindings: make(map[lifecycleParticipantKey]LifecycleParticipantBinding, len(bindings)),
	}
	for _, binding := range bindings {
		// In particular, a typed nil must never reach its Name method.
		if nilPort(binding.Participant) {
			return nil, fault(foundation.DependencyUnbound)
		}
		registration, err := normalizeLifecycleRegistration(binding.Registration)
		if err != nil {
			return nil, err
		}
		if binding.Participant.Name() != registration.Name {
			return nil, invalid()
		}
		key := lifecycleParticipantKey{registration.Name, registration.ContractVersion}
		if _, exists := r.bindings[key]; exists {
			return nil, invalid()
		}
		r.bindings[key] = LifecycleParticipantBinding{registration, binding.Participant}
	}
	if _, err := r.Resolve(current); err != nil {
		return nil, err
	}
	return r, nil
}

// normalizeLifecycleRegistration checks only an individual binding's fields
// and sets. RequiredManifest alone validates complete manifests and graphs.
func normalizeLifecycleRegistration(registration c.ParticipantRegistration) (c.ParticipantRegistration, error) {
	if registration.Name.Validate() != nil || registration.ContractVersion.Validate() != nil || registration.OwnerModule.Validate() != nil {
		return c.ParticipantRegistration{}, invalid()
	}
	registration.ReferenceKinds = slices.Clone(registration.ReferenceKinds)
	registration.CleanupAfter = slices.Clone(registration.CleanupAfter)
	slices.Sort(registration.ReferenceKinds)
	slices.Sort(registration.CleanupAfter)
	for i, kind := range registration.ReferenceKinds {
		if kind.Validate() != nil || i > 0 && kind == registration.ReferenceKinds[i-1] {
			return c.ParticipantRegistration{}, invalid()
		}
	}
	for i, name := range registration.CleanupAfter {
		if name.Validate() != nil || i > 0 && name == registration.CleanupAfter[i-1] {
			return c.ParticipantRegistration{}, invalid()
		}
	}
	return registration, nil
}

func (r *LifecycleRegistry) Manifest() (c.RequiredManifest, error) {
	if r == nil || r.bindings == nil {
		return c.RequiredManifest{}, fault(foundation.DependencyUnbound)
	}
	// RequiredManifest is already immutable; Entries returns deep copies.
	return r.current, nil
}

func (r *LifecycleRegistry) Resolve(frozen c.RequiredManifest) (LifecyclePlan, error) {
	if r == nil || r.bindings == nil {
		return LifecyclePlan{}, fault(foundation.DependencyUnbound)
	}
	// Only the contract can establish a valid manifest, including its digest.
	// Keep that original immutable value, without rebuilding or extending it.
	if _, err := frozen.Digest(); err != nil {
		return LifecyclePlan{}, err
	}
	entries := frozen.Entries()
	plan := LifecyclePlan{
		manifest:     frozen,
		participants: make(map[c.ParticipantName]c.ProjectLifecycleParticipant, len(entries)),
		stopOrder:    make([]c.ParticipantName, 0, len(entries)),
	}
	for _, entry := range entries {
		binding, ok := r.bindings[lifecycleParticipantKey{entry.Name, entry.ContractVersion}]
		if !ok {
			return LifecyclePlan{}, fault(foundation.DependencyUnbound)
		}
		registered := binding.Registration
		if registered.OwnerModule != entry.OwnerModule || !slices.Equal(registered.ReferenceKinds, entry.ReferenceKinds) || !slices.Equal(registered.CleanupAfter, entry.CleanupAfter) {
			return LifecyclePlan{}, fault(foundation.DependencyUnavailable)
		}
		plan.participants[entry.Name] = binding.Participant
		// Entries are already in canonical ParticipantName order.
		plan.stopOrder = append(plan.stopOrder, entry.Name)
	}
	plan.cleanupOrder = lifecycleCleanupOrder(entries)
	return plan, nil
}

// lifecycleCleanupOrder orders the graph already validated by RequiredManifest.
// A newly ready name competes with every other ready name at each step.
func lifecycleCleanupOrder(entries []c.ParticipantRegistration) []c.ParticipantName {
	remaining := make(map[c.ParticipantName]int, len(entries))
	dependents := make(map[c.ParticipantName][]c.ParticipantName, len(entries))
	ready := make([]c.ParticipantName, 0, len(entries))
	for _, entry := range entries {
		remaining[entry.Name] = len(entry.CleanupAfter)
		if len(entry.CleanupAfter) == 0 {
			ready = append(ready, entry.Name)
		}
		for _, dependency := range entry.CleanupAfter {
			dependents[dependency] = append(dependents[dependency], entry.Name)
		}
	}
	order := make([]c.ParticipantName, 0, len(entries))
	for len(ready) > 0 {
		name := ready[0]
		ready = ready[1:]
		order = append(order, name)
		for _, dependent := range dependents[name] {
			remaining[dependent]--
			if remaining[dependent] == 0 {
				index, _ := slices.BinarySearch(ready, dependent)
				ready = slices.Insert(ready, index, dependent)
			}
		}
	}
	return order
}

func (p LifecyclePlan) Manifest() (c.RequiredManifest, error) {
	if p.participants == nil {
		return c.RequiredManifest{}, fault(foundation.DependencyUnbound)
	}
	return p.manifest, nil
}

func (p LifecyclePlan) StopOrder() ([]c.ParticipantName, error) {
	if p.participants == nil {
		return nil, fault(foundation.DependencyUnbound)
	}
	return slices.Clone(p.stopOrder), nil
}

func (p LifecyclePlan) CleanupOrder() ([]c.ParticipantName, error) {
	if p.participants == nil {
		return nil, fault(foundation.DependencyUnbound)
	}
	return slices.Clone(p.cleanupOrder), nil
}

func (p LifecyclePlan) Participant(name c.ParticipantName) (c.ProjectLifecycleParticipant, error) {
	participant, ok := p.participants[name]
	if !ok {
		return nil, fault(foundation.DependencyUnbound)
	}
	return participant, nil
}
