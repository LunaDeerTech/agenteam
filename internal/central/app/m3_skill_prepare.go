package app

import (
	"github.com/LunaDeerTech/agenteam/internal/central/agent"
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	"github.com/LunaDeerTech/agenteam/internal/central/object"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	"github.com/LunaDeerTech/agenteam/internal/central/skill"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

// The preparation authority is created before Object's transfer provider and
// reused here after the Skill service is bound. All physical calls remain
// owned by those existing services and the original authenticated DeviceHandler.
func createSkillPreparationBackend(db database, core *skill.Service, authority *skill.PreparationAuthority, transfers *object.TransferService, agents *agent.Authority, executions ac.ExecutionIdentityAuthority, device *runnerhttp.DeviceHandler) (toolruntime.BuiltinBackend, error) {
	store, ok := db.(mount.Store)
	if !ok || runtimeInformationNil(store) || core == nil || authority == nil || transfers == nil || agents == nil || runtimeInformationNil(executions) || device == nil {
		return nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	configuration, err := agent.NewExecutionConfiguration(agents, executions)
	if err != nil {
		return nil, err
	}
	resolver, err := mount.NewSkillPrepareResolver(store, configuration, authority)
	if err != nil {
		return nil, err
	}
	dispatcher, err := runnerhttp.NewSkillPrepareDispatcher(device, resolver)
	if err != nil {
		return nil, err
	}
	preparer, err := skill.NewPreparer(core, authority, transfers, resolver, dispatcher)
	if err != nil {
		return nil, err
	}
	return toolruntime.NewSkillPrepareBackend(preparer)
}
