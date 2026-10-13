package app

import (
	"context"

	"github.com/LunaDeerTech/agenteam/internal/central/artifact"
	"github.com/LunaDeerTech/agenteam/internal/central/audit"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/project"
)

// The owner is fixed before the original Object service is constructed. The
// same provider is later given to the Artifact pair; there is no late setter.
func createArtifactOwners(db database, projects *projectUsageAssembly) (*artifact.OwnerProvider, error) {
	store, ok := db.(artifact.Store)
	if !ok || runtimeInformationNil(store) || projects == nil || projects.projects == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	authority, err := project.NewArtifactAuthority(projects.projects)
	if err != nil {
		return nil, err
	}
	return artifact.NewOwnerProvider(store, authority)
}

type artifactCallAssembly struct {
	pair *artifact.ProjectCallOwnership
}

// Account constructs this once after original Object.Initialize, before any
// service can be admitted. Sources and browser transfers remain unbound; the
// pair retains the original Object uploads/cleaner required by inline creation.
func createOwnedArtifacts(cfg config.Config, db database, owners *artifact.OwnerProvider, projects *projectUsageAssembly, objects *objectAssembly, auditor *audit.Service) (*artifactCallAssembly, error) {
	store, ok := db.(project.Store)
	if !ok || runtimeInformationNil(store) || owners == nil || projects == nil || projects.projects == nil || projects.lifecycle == nil || objects == nil || objects.service == nil || objects.guard == nil || objects.process.Validate() != nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	stop, err := project.NewArtifactStopAuthority(projects.lifecycle, store)
	if err != nil {
		return nil, err
	}
	pair, err := artifact.NewProjectCallOwnership(store, owners, objects.service, objects.service, objects.service, nil, nil, auditor, cfg.CursorKeyring(), objects.guard, objects.process, stop)
	if err != nil {
		return nil, err
	}
	return &artifactCallAssembly{pair: pair}, nil
}

func (a *artifactCallAssembly) StopAdmission() {
	if a != nil && a.pair != nil {
		a.pair.Stop()
	}
}
func (a *artifactCallAssembly) Drain(ctx context.Context) error {
	if a == nil || a.pair == nil {
		return nil
	}
	a.StopAdmission()
	if err := a.pair.Drain(ctx); err != nil {
		return err
	}
	if !a.pair.Joined() {
		return f.NewFault(f.DependencyUnavailable, f.NotStarted)
	}
	return nil
}
func (a *artifactCallAssembly) Force(ctx context.Context) error { return a.Drain(ctx) }
func (a *artifactCallAssembly) Joined() bool                    { return a == nil || a.pair == nil || a.pair.Joined() }

var _ accountWork = (*artifactCallAssembly)(nil)
