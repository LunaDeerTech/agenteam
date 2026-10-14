package app

import (
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/builtin"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

// All adapters point at the original, already owned domain Services. This
// fixed graph is constructed before Registry and never receives a late driver
// or Service setter. The actual driver only enters through its private call.
type executionBuiltinAssembly struct {
	authority *toolruntime.BuiltinAuthority
	backends  []toolruntime.BuiltinBackend
	sources   map[string]registry.BuiltinSource
}

func createExecutionBuiltins(authority *toolruntime.BuiltinAuthority, tasks *workPlanningAssembly, artifacts *artifactCallAssembly) (*executionBuiltinAssembly, error) {
	if authority == nil || tasks == nil || tasks.transitions == nil || artifacts == nil || artifacts.pair == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	taskAdapter, err := builtin.NewTaskAdapter(tasks.transitions)
	if err != nil {
		return nil, err
	}
	artifactAdapter, err := builtin.NewArtifactAdapter(artifacts.pair.Service())
	if err != nil {
		return nil, err
	}
	taskBackends, err := toolruntime.NewTaskBackends(taskAdapter)
	if err != nil {
		return nil, err
	}
	artifactBackends, err := toolruntime.NewArtifactBackends(artifactAdapter)
	if err != nil {
		return nil, err
	}
	out := &executionBuiltinAssembly{authority: authority, backends: append(taskBackends, artifactBackends...), sources: map[string]registry.BuiltinSource{}}
	for _, backend := range out.backends {
		source, err := toolruntime.NewBuiltinSource(authority, backend)
		if err != nil {
			return nil, err
		}
		out.sources[backend.Registration().Definition.StableKey] = source
	}
	return out, nil
}
