package project

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

type registryTestParticipant struct {
	name         c.ParticipantName
	nameCalls    atomic.Int64
	requestCalls atomic.Int64
	inspectCalls atomic.Int64
	cleanupCalls atomic.Int64
}

func (p *registryTestParticipant) Name() c.ParticipantName {
	p.nameCalls.Add(1) // A typed nil panics here if construction calls Name.
	return p.name
}
func (p *registryTestParticipant) RequestStop(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error) {
	p.requestCalls.Add(1)
	return c.StopReport{}, errors.New("unexpected RequestStop")
}
func (p *registryTestParticipant) InspectStop(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef) (c.StopReport, error) {
	p.inspectCalls.Add(1)
	return c.StopReport{}, errors.New("unexpected InspectStop")
}
func (p *registryTestParticipant) Cleanup(context.Context, identity.Actor, c.LifecycleCause, c.ScopeRef, *c.CleanupCheckpoint) (c.CleanupReport, error) {
	p.cleanupCalls.Add(1)
	return c.CleanupReport{}, errors.New("unexpected Cleanup")
}

func registryTestEntries() []c.ParticipantRegistration {
	return []c.ParticipantRegistration{
		{Name: c.ArtifactObjectParticipant, ContractVersion: 1, OwnerModule: "artifact-object", ReferenceKinds: []c.ReferenceKind{"object", "artifact"}},
		{Name: c.SecretParticipant, ContractVersion: 1, OwnerModule: "secret", ReferenceKinds: []c.ReferenceKind{"secret", "lease"}},
		{Name: c.OutboxParticipant, ContractVersion: 1, OwnerModule: "outbox", ReferenceKinds: []c.ReferenceKind{"event", "delivery"}, CleanupAfter: []c.ParticipantName{c.SecretParticipant, c.ArtifactObjectParticipant}},
		{Name: c.AuditParticipant, ContractVersion: 1, OwnerModule: "audit", ReferenceKinds: []c.ReferenceKind{"record"}, CleanupAfter: []c.ParticipantName{c.OutboxParticipant}},
	}
}

func registryTestManifest(t *testing.T, entries []c.ParticipantRegistration) c.RequiredManifest {
	t.Helper()
	manifest, err := c.NewRequiredManifest(entries)
	if err != nil {
		t.Fatalf("invalid test manifest: %v", err)
	}
	return manifest
}

func registryTestBindings(t *testing.T, entries []c.ParticipantRegistration) ([]LifecycleParticipantBinding, map[c.ParticipantName]*registryTestParticipant) {
	t.Helper()
	bindings := make([]LifecycleParticipantBinding, 0, len(entries))
	adapters := make(map[c.ParticipantName]*registryTestParticipant, len(entries))
	for _, entry := range entries {
		adapter := &registryTestParticipant{name: entry.Name}
		bindings = append(bindings, LifecycleParticipantBinding{entry, adapter})
		adapters[entry.Name] = adapter
		t.Cleanup(func() {
			if adapter.requestCalls.Load() != 0 || adapter.inspectCalls.Load() != 0 || adapter.cleanupCalls.Load() != 0 {
				t.Errorf("registry called business methods for %s", adapter.name)
			}
		})
	}
	return bindings, adapters
}

func registryTestFault(t *testing.T, err error, code foundation.Code) {
	t.Helper()
	var f *foundation.Fault
	if !errors.As(err, &f) || f.Code != code || f.CommitState != foundation.NotStarted {
		t.Fatalf("fault = %v, want %s / not_started", err, code)
	}
}

func registryTestZeroPlan(t *testing.T, plan LifecyclePlan) {
	t.Helper()
	manifest, err := plan.Manifest()
	registryTestFault(t, err, foundation.DependencyUnbound)
	if manifest.Require() == nil {
		t.Fatal("failed plan exposed a manifest")
	}
	stop, err := plan.StopOrder()
	registryTestFault(t, err, foundation.DependencyUnbound)
	cleanup, err := plan.CleanupOrder()
	registryTestFault(t, err, foundation.DependencyUnbound)
	participant, err := plan.Participant(c.ArtifactObjectParticipant)
	registryTestFault(t, err, foundation.DependencyUnbound)
	if stop != nil || cleanup != nil || participant != nil {
		t.Fatal("failed plan exposed partial results")
	}
}

func registryTestPlan(t *testing.T, plan LifecyclePlan, manifest c.RequiredManifest, adapters map[c.ParticipantName]*registryTestParticipant) {
	t.Helper()
	actual, err := plan.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	wantDigest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	gotDigest, err := actual.Digest()
	if err != nil || gotDigest != wantDigest {
		t.Fatalf("manifest digest = %s / %v, want %s", gotDigest, err, wantDigest)
	}
	stop, err := plan.StopOrder()
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := plan.CleanupOrder()
	if err != nil {
		t.Fatal(err)
	}
	entries := manifest.Entries()
	if len(stop) != len(entries) || len(cleanup) != len(entries) || !slices.IsSorted(stop) {
		t.Fatalf("incorrect orders: stop=%v cleanup=%v", stop, cleanup)
	}
	positions := make(map[c.ParticipantName]int, len(cleanup))
	for i, name := range cleanup {
		if _, exists := positions[name]; exists {
			t.Fatalf("duplicate cleanup participant %s", name)
		}
		positions[name] = i
	}
	for i, entry := range entries {
		if stop[i] != entry.Name {
			t.Fatalf("stop participant %d = %s, want %s", i, stop[i], entry.Name)
		}
		position, exists := positions[entry.Name]
		if !exists {
			t.Fatalf("missing cleanup participant %s", entry.Name)
		}
		for _, dependency := range entry.CleanupAfter {
			before, exists := positions[dependency]
			if !exists || before >= position {
				t.Fatalf("cleanup %v violates %s after %s", cleanup, entry.Name, dependency)
			}
		}
		participant, err := plan.Participant(entry.Name)
		if err != nil || participant != adapters[entry.Name] {
			t.Fatalf("adapter %s = %v / %v, want original instance", entry.Name, participant, err)
		}
	}
	for _, name := range []c.ParticipantName{"", "unknown", "INVALID NAME"} {
		participant, err := plan.Participant(name)
		registryTestFault(t, err, foundation.DependencyUnbound)
		if participant != nil {
			t.Fatal("plan returned an undeclared adapter")
		}
	}
}

func TestLifecycleRegistryCurrentManifestAndSkills(t *testing.T) {
	for _, skills := range []bool{false, true} {
		t.Run(fmt.Sprintf("skills=%t", skills), func(t *testing.T) {
			entries := registryTestEntries()
			if skills {
				entries = append(entries, c.ParticipantRegistration{Name: c.SkillsParticipant, ContractVersion: 1, OwnerModule: "skills", ReferenceKinds: []c.ReferenceKind{"skill"}})
				entries[0].CleanupAfter = []c.ParticipantName{c.SkillsParticipant}
				entries[1].CleanupAfter = []c.ParticipantName{c.SkillsParticipant}
				entries[2].CleanupAfter = append(entries[2].CleanupAfter, c.SkillsParticipant)
			}
			manifest := registryTestManifest(t, entries)
			bindings, adapters := registryTestBindings(t, entries)
			registry, err := NewLifecycleRegistry(manifest, bindings)
			if err != nil {
				t.Fatal(err)
			}
			current, err := registry.Manifest()
			if err != nil {
				t.Fatal(err)
			}
			plan, err := registry.Resolve(current)
			if err != nil {
				t.Fatal(err)
			}
			registryTestPlan(t, plan, manifest, adapters)
			wantCleanup := []c.ParticipantName{c.ArtifactObjectParticipant, c.SecretParticipant, c.OutboxParticipant, c.AuditParticipant}
			if skills {
				wantCleanup = append([]c.ParticipantName{c.SkillsParticipant}, wantCleanup...)
			} else {
				_, err := plan.Participant(c.SkillsParticipant)
				registryTestFault(t, err, foundation.DependencyUnbound)
			}
			cleanup, _ := plan.CleanupOrder()
			if !slices.Equal(cleanup, wantCleanup) {
				t.Fatalf("cleanup = %v, want %v", cleanup, wantCleanup)
			}
			for _, adapter := range adapters {
				if adapter.nameCalls.Load() != 1 {
					t.Fatalf("Name called after registration for %s", adapter.name)
				}
			}
		})
	}
}

func TestLifecycleRegistryRejectsInvalidBindings(t *testing.T) {
	tests := []struct {
		name string
		edit func([]LifecycleParticipantBinding) []LifecycleParticipantBinding
		code foundation.Code
	}{
		{"nil", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding { v[0].Participant = nil; return v }, foundation.DependencyUnbound},
		{"typed-nil", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Participant = (*registryTestParticipant)(nil)
			return v
		}, foundation.DependencyUnbound},
		{"name-mismatch", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.Name = "another-name"
			return v
		}, foundation.InvalidArgument},
		{"empty-name", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.Name = ""
			return v
		}, foundation.InvalidArgument},
		{"invalid-name", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.Name = "INVALID NAME"
			return v
		}, foundation.InvalidArgument},
		{"zero-version", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.ContractVersion = 0
			return v
		}, foundation.InvalidArgument},
		{"negative-version", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.ContractVersion = -1
			return v
		}, foundation.InvalidArgument},
		{"empty-owner", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.OwnerModule = ""
			return v
		}, foundation.InvalidArgument},
		{"invalid-owner", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.OwnerModule = "INVALID OWNER"
			return v
		}, foundation.InvalidArgument},
		{"invalid-reference", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.ReferenceKinds = []c.ReferenceKind{"INVALID KIND"}
			return v
		}, foundation.InvalidArgument},
		{"empty-reference", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.ReferenceKinds = []c.ReferenceKind{""}
			return v
		}, foundation.InvalidArgument},
		{"duplicate-reference", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.ReferenceKinds = []c.ReferenceKind{"object", "artifact", "object"}
			return v
		}, foundation.InvalidArgument},
		{"invalid-dependency", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.CleanupAfter = []c.ParticipantName{"INVALID NAME"}
			return v
		}, foundation.InvalidArgument},
		{"empty-dependency", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[0].Registration.CleanupAfter = []c.ParticipantName{""}
			return v
		}, foundation.InvalidArgument},
		{"duplicate-dependency", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			v[2].Registration.CleanupAfter = append(v[2].Registration.CleanupAfter, c.SecretParticipant)
			return v
		}, foundation.InvalidArgument},
		{"duplicate-tuple", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding { return append(v, v[0]) }, foundation.InvalidArgument},
		{"conflicting-tuple", func(v []LifecycleParticipantBinding) []LifecycleParticipantBinding {
			duplicate := v[0]
			duplicate.Registration.OwnerModule = "another-owner"
			return append(v, duplicate)
		}, foundation.InvalidArgument},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			entries := registryTestEntries()
			manifest := registryTestManifest(t, entries)
			bindings, _ := registryTestBindings(t, entries)
			registry, err := NewLifecycleRegistry(manifest, test.edit(bindings))
			registryTestFault(t, err, test.code)
			if registry != nil {
				t.Fatal("invalid configuration returned a registry")
			}
		})
	}
}

func TestLifecycleRegistryRequiresEveryCurrentBinding(t *testing.T) {
	entries := registryTestEntries()
	entries = append(entries, c.ParticipantRegistration{Name: c.SkillsParticipant, ContractVersion: 1, OwnerModule: "skills"})
	entries[2].CleanupAfter = append(entries[2].CleanupAfter, c.SkillsParticipant)
	manifest := registryTestManifest(t, entries)
	for omitted, entry := range entries {
		t.Run(string(entry.Name), func(t *testing.T) {
			bindings, _ := registryTestBindings(t, entries)
			bindings = append(bindings[:omitted], bindings[omitted+1:]...)
			registry, err := NewLifecycleRegistry(manifest, bindings)
			registryTestFault(t, err, foundation.DependencyUnbound)
			if registry != nil {
				t.Fatal("incomplete current bindings returned a registry")
			}
		})
	}
	registry, err := NewLifecycleRegistry(manifest, nil)
	registryTestFault(t, err, foundation.DependencyUnbound)
	if registry != nil {
		t.Fatal("empty bindings returned a registry")
	}
}

func TestLifecycleRegistryZeroValuesFailClosed(t *testing.T) {
	manifest := registryTestManifest(t, registryTestEntries())
	for _, registry := range []*LifecycleRegistry{nil, {}} {
		current, err := registry.Manifest()
		registryTestFault(t, err, foundation.DependencyUnbound)
		if current.Require() == nil {
			t.Fatal("zero registry exposed a manifest")
		}
		plan, err := registry.Resolve(manifest)
		registryTestFault(t, err, foundation.DependencyUnbound)
		registryTestZeroPlan(t, plan)
	}
	registryTestZeroPlan(t, LifecyclePlan{})
	bindings, _ := registryTestBindings(t, manifest.Entries())
	registry, err := NewLifecycleRegistry(c.RequiredManifest{}, bindings)
	registryTestFault(t, err, foundation.DependencyUnbound)
	if registry != nil {
		t.Fatal("zero manifest returned a registry")
	}
	registry, err = NewLifecycleRegistry(manifest, bindings)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := registry.Resolve(c.RequiredManifest{})
	registryTestFault(t, err, foundation.DependencyUnbound)
	registryTestZeroPlan(t, plan)
}

func registryTestVersions(t *testing.T) (c.RequiredManifest, c.RequiredManifest, []LifecycleParticipantBinding, []LifecycleParticipantBinding, map[c.ParticipantName]*registryTestParticipant, map[c.ParticipantName]*registryTestParticipant) {
	t.Helper()
	oldEntries := registryTestEntries()
	currentEntries := registryTestEntries()
	currentEntries = append(currentEntries, c.ParticipantRegistration{Name: c.SkillsParticipant, OwnerModule: "skills", ReferenceKinds: []c.ReferenceKind{"skill"}})
	currentEntries[0].CleanupAfter = []c.ParticipantName{c.SkillsParticipant}
	currentEntries[2].CleanupAfter = append(currentEntries[2].CleanupAfter, c.SkillsParticipant)
	for i := range currentEntries {
		currentEntries[i].ContractVersion = 2
	}
	old := registryTestManifest(t, oldEntries)
	current := registryTestManifest(t, currentEntries)
	oldBindings, oldAdapters := registryTestBindings(t, oldEntries)
	currentBindings, currentAdapters := registryTestBindings(t, currentEntries)
	return old, current, oldBindings, currentBindings, oldAdapters, currentAdapters
}

func TestLifecycleRegistryRetainsExactFrozenVersions(t *testing.T) {
	old, current, oldBindings, currentBindings, oldAdapters, currentAdapters := registryTestVersions(t)
	registry, err := NewLifecycleRegistry(current, append(currentBindings, oldBindings...))
	if err != nil {
		t.Fatal(err)
	}
	oldPlan, err := registry.Resolve(old)
	if err != nil {
		t.Fatal(err)
	}
	registryTestPlan(t, oldPlan, old, oldAdapters)
	_, err = oldPlan.Participant(c.SkillsParticipant)
	registryTestFault(t, err, foundation.DependencyUnbound)
	oldCleanup, _ := oldPlan.CleanupOrder()
	if !slices.Equal(oldCleanup, []c.ParticipantName{c.ArtifactObjectParticipant, c.SecretParticipant, c.OutboxParticipant, c.AuditParticipant}) {
		t.Fatalf("old cleanup graph was replaced: %v", oldCleanup)
	}
	actualCurrent, err := registry.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	currentPlan, err := registry.Resolve(actualCurrent)
	if err != nil {
		t.Fatal(err)
	}
	registryTestPlan(t, currentPlan, current, currentAdapters)
}

func TestLifecycleRegistryNeverSubstitutesMissingVersions(t *testing.T) {
	old, current, oldBindings, currentBindings, _, _ := registryTestVersions(t)
	for _, test := range []struct {
		name     string
		bindings []LifecycleParticipantBinding
	}{
		{"only-current", currentBindings},
		{"partial-old", append(slices.Clone(currentBindings), oldBindings[0], oldBindings[2], oldBindings[3])},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := NewLifecycleRegistry(current, test.bindings)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := registry.Resolve(old)
			registryTestFault(t, err, foundation.DependencyUnbound)
			registryTestZeroPlan(t, plan)
		})
	}
	for _, test := range []struct {
		name     string
		manifest c.RequiredManifest
		bindings []LifecycleParticipantBinding
	}{
		{"cannot-upgrade-current", old, currentBindings},
		{"cannot-downgrade-current", current, oldBindings},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry, err := NewLifecycleRegistry(test.manifest, test.bindings)
			registryTestFault(t, err, foundation.DependencyUnbound)
			if registry != nil {
				t.Fatal("constructor substituted another version")
			}
		})
	}
}

func TestLifecycleRegistryRejectsValidFrozenMetadataDrift(t *testing.T) {
	entries := registryTestEntries()
	entries[3].CleanupAfter = append(entries[3].CleanupAfter, c.SecretParticipant)
	current := registryTestManifest(t, entries)
	bindings, _ := registryTestBindings(t, entries)
	registry, err := NewLifecycleRegistry(current, bindings)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		edit func([]c.ParticipantRegistration)
	}{
		{"owner", func(v []c.ParticipantRegistration) { v[0].OwnerModule = "legacy-object" }},
		{"reference-shrink", func(v []c.ParticipantRegistration) { v[0].ReferenceKinds = []c.ReferenceKind{"object"} }},
		{"reference-expand", func(v []c.ParticipantRegistration) { v[0].ReferenceKinds = append(v[0].ReferenceKinds, "blob") }},
		{"reference-replace", func(v []c.ParticipantRegistration) { v[0].ReferenceKinds = []c.ReferenceKind{"blob", "object"} }},
		{"cleanup-shrink", func(v []c.ParticipantRegistration) { v[1].CleanupAfter = []c.ParticipantName{c.OutboxParticipant} }},
		{"cleanup-expand", func(v []c.ParticipantRegistration) {
			v[1].CleanupAfter = append(v[1].CleanupAfter, c.ArtifactObjectParticipant)
		}},
		{"cleanup-replace", func(v []c.ParticipantRegistration) {
			v[1].CleanupAfter = []c.ParticipantName{c.OutboxParticipant, c.ArtifactObjectParticipant}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := current.Entries() // Canonical order: artifact, audit, outbox, secret.
			test.edit(changed)
			frozen := registryTestManifest(t, changed)
			plan, err := registry.Resolve(frozen)
			registryTestFault(t, err, foundation.DependencyUnavailable)
			registryTestZeroPlan(t, plan)
			candidate, err := NewLifecycleRegistry(frozen, bindings)
			registryTestFault(t, err, foundation.DependencyUnavailable)
			if candidate != nil {
				t.Fatal("constructor accepted mismatched current metadata")
			}
		})
	}
}

func TestLifecycleRegistryOrderIgnoresInputAndSetPermutation(t *testing.T) {
	entries := registryTestEntries()
	entries[0].CleanupAfter = []c.ParticipantName{"a-child"}
	entries = append(entries,
		c.ParticipantRegistration{Name: "a-child", ContractVersion: 1, OwnerModule: "children", CleanupAfter: []c.ParticipantName{"b-root"}},
		c.ParticipantRegistration{Name: "b-root", ContractVersion: 1, OwnerModule: "roots"},
		c.ParticipantRegistration{Name: "z-idle", ContractVersion: 1, OwnerModule: "idle"},
	)
	entries[2].CleanupAfter = append(entries[2].CleanupAfter, "a-child", "b-root", "z-idle")
	manifest := registryTestManifest(t, entries)
	wantCleanup := []c.ParticipantName{"b-root", "a-child", c.ArtifactObjectParticipant, c.SecretParticipant, "z-idle", c.OutboxParticipant, c.AuditParticipant}
	for rotation := range entries {
		permuted := manifest.Entries()
		permuted = append(permuted[rotation:], permuted[:rotation]...)
		for i := range permuted {
			slices.Reverse(permuted[i].ReferenceKinds)
			slices.Reverse(permuted[i].CleanupAfter)
		}
		bindings, adapters := registryTestBindings(t, permuted)
		registry, err := NewLifecycleRegistry(manifest, bindings)
		if err != nil {
			t.Fatal(err)
		}
		frozen := registryTestManifest(t, permuted)
		plan, err := registry.Resolve(frozen)
		if err != nil {
			t.Fatal(err)
		}
		registryTestPlan(t, plan, manifest, adapters)
		cleanup, _ := plan.CleanupOrder()
		if !slices.Equal(cleanup, wantCleanup) {
			t.Fatalf("rotation %d cleanup = %v, want %v", rotation, cleanup, wantCleanup)
		}
	}
}

func TestLifecycleRegistryDoesNotExposeMutableMetadata(t *testing.T) {
	entries := registryTestEntries()
	manifest := registryTestManifest(t, entries)
	bindings, adapters := registryTestBindings(t, entries)
	registry, err := NewLifecycleRegistry(manifest, bindings)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := registry.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	for i := range entries {
		entries[i].Name = "changed-input"
		for j := range entries[i].ReferenceKinds {
			entries[i].ReferenceKinds[j] = "changed-kind"
		}
		for j := range entries[i].CleanupAfter {
			entries[i].CleanupAfter[j] = "changed-dependency"
		}
		bindings[i].Registration.OwnerModule = "changed-owner"
		bindings[i].Registration.ContractVersion = 99
		bindings[i].Participant = nil
	}
	bindings[0] = LifecycleParticipantBinding{}
	registryTestPlan(t, plan, manifest, adapters)
	stop, _ := plan.StopOrder()
	cleanup, _ := plan.CleanupOrder()
	stop[0], cleanup[0] = "changed-stop", "changed-cleanup"
	for _, getter := range []func() (c.RequiredManifest, error){registry.Manifest, plan.Manifest} {
		returned, err := getter()
		if err != nil {
			t.Fatal(err)
		}
		copies := returned.Entries()
		for i := range copies {
			copies[i].Name = "changed-return"
			copies[i].ContractVersion = 99
			copies[i].OwnerModule = "changed-owner"
			for j := range copies[i].ReferenceKinds {
				copies[i].ReferenceKinds[j] = "changed-kind"
			}
			for j := range copies[i].CleanupAfter {
				copies[i].CleanupAfter[j] = "changed-dependency"
			}
		}
	}
	registryTestPlan(t, plan, manifest, adapters)
	current, err := registry.Manifest()
	if err != nil {
		t.Fatal(err)
	}
	again, err := registry.Resolve(current)
	if err != nil {
		t.Fatal(err)
	}
	registryTestPlan(t, again, manifest, adapters)
}

func TestLifecycleRegistryConcurrentResolutionAndReads(t *testing.T) {
	old, current, oldBindings, currentBindings, oldAdapters, currentAdapters := registryTestVersions(t)
	registry, err := NewLifecycleRegistry(current, append(currentBindings, oldBindings...))
	if err != nil {
		t.Fatal(err)
	}
	sharedPlan, err := registry.Resolve(old)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for worker := range 24 {
		workers.Go(func() {
			manifest, adapters := old, oldAdapters
			if worker%2 == 0 {
				manifest, adapters = current, currentAdapters
			}
			for range 40 {
				plan, err := registry.Resolve(manifest)
				if err != nil {
					t.Error(err)
					return
				}
				registryTestPlan(t, plan, manifest, adapters)
				registryTestPlan(t, sharedPlan, old, oldAdapters)
				stop, _ := sharedPlan.StopOrder()
				cleanup, _ := sharedPlan.CleanupOrder()
				stop[0], cleanup[0] = "private-stop", "private-cleanup"
				returned, err := registry.Manifest()
				if err != nil {
					t.Error(err)
					return
				}
				copies := returned.Entries()
				copies[0].Name = "private-name"
				copies[0].ReferenceKinds[0] = "private-kind"
			}
		})
	}
	workers.Wait()
	registryTestPlan(t, sharedPlan, old, oldAdapters)
	for _, adapters := range []map[c.ParticipantName]*registryTestParticipant{oldAdapters, currentAdapters} {
		for _, adapter := range adapters {
			if adapter.nameCalls.Load() != 1 {
				t.Fatalf("resolution called adapter Name for %s", adapter.name)
			}
		}
	}
}
