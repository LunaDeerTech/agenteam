package project

import (
	"context"
	"testing"

	"github.com/LunaDeerTech/agenteam/internal/central/foundation"
	identity "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	c "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
)

func TestLifecycleUnboundServiceAndNewIntentGates(t *testing.T) {
	var service *Service
	_, err := service.BeginArchive(context.Background(), identity.Actor{}, foundation.CommandMeta{}, c.ProjectID{})
	hasCode(t, err, foundation.DependencyUnbound)
	_, err = service.BeginDeleteProject(context.Background(), identity.Actor{}, foundation.CommandMeta{}, c.ProjectID{}, c.DeleteProjectRequest{})
	hasCode(t, err, foundation.DependencyUnbound)
	_, err = service.GetLifecycle(context.Background(), identity.Actor{}, c.ProjectID{}, c.OperationID{})
	hasCode(t, err, foundation.DependencyUnbound)
	p := &projectRecord{ref: testProject(t)}
	// These rejections precede any dependency or SQL use.
	hasCode(t, service.checkNewLifecycle(context.Background(), foundation.Tx{}, identity.Actor{}, p, 1, c.ArchiveCommand, nil), foundation.ProjectNotActive)
	p.initialized = true
	hasCode(t, service.checkNewLifecycle(context.Background(), foundation.Tx{}, identity.Actor{}, p, 2, c.ArchiveCommand, nil), foundation.VersionConflict)
	for _, state := range []c.Lifecycle{c.Archiving, c.Deleting, c.Archived} {
		p.ref.Lifecycle = state
		hasCode(t, service.checkNewLifecycle(context.Background(), foundation.Tx{}, identity.Actor{}, p, 1, c.ArchiveCommand, nil), foundation.ProjectNotActive)
	}
}
