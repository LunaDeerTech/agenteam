package mount

import (
	"context"
	"slices"

	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	i "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

// readCurrentMount is only the shared current-fact read. Callers still own their
// exact operation authority, same-Store transaction, full held locks and live
// Agent Current witness before obtaining config. It creates no dispatch grant.
func readCurrentMount(ctx context.Context, x postgres.SQLExecutor, config ac.AgentConfig, project i.ProjectID, agent i.AgentID, mount i.MountID) (readFileCurrent, error) {
	if err := config.Validate(); err != nil {
		return readFileCurrent{}, fault(f.InvalidState)
	}
	fields := config.Fields()
	if fields.Core.ID != agent || fields.Core.ProjectID != project {
		return readFileCurrent{}, fault(f.InvalidState)
	}
	if fields.Core.Lifecycle != ac.AgentActive {
		return readFileCurrent{}, fault(f.Forbidden)
	}
	refs, err := loadReferences(ctx, x, project, agent)
	if err != nil {
		return readFileCurrent{}, err
	}
	ids := slices.Clone(fields.AllowedMountIDs)
	slices.SortFunc(ids, func(a, b i.MountID) int {
		if a.String() < b.String() {
			return -1
		}
		if a == b {
			return 0
		}
		return 1
	})
	if !refs.exists || refs.version != fields.Core.Version || !slices.Equal(refs.ids, ids) {
		return readFileCurrent{}, fault(f.InvalidState)
	}
	if !slices.Contains(ids, mount) {
		return readFileCurrent{}, fault(f.Forbidden)
	}
	d, err := loadDefinition(ctx, x, project, agent, mount)
	if err != nil {
		return readFileCurrent{}, err
	}
	unique, err := definitionForTuple(ctx, x, project, agent, d.Runner, d.Workspace)
	if err != nil {
		return readFileCurrent{}, err
	}
	if unique == nil || !sameDefinition(d, *unique) {
		return readFileCurrent{}, fault(f.InvalidState)
	}
	mapping, err := loadWorkspaceMapping(ctx, x, mount)
	if err != nil {
		return readFileCurrent{}, err
	}
	if !mappingMatches(mapping, d) {
		return readFileCurrent{}, fault(f.InvalidState)
	}
	return readFileCurrent{agentVersion: fields.Core.Version, ids: ids, definition: d, mapping: *mapping}, ctx.Err()
}
