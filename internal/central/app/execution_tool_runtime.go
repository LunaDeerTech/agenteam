package app

import (
	ac "github.com/LunaDeerTech/agenteam/internal/central/agent/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/mount"
	runnerhttp "github.com/LunaDeerTech/agenteam/internal/central/runner/http"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
	toolruntime "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
)

// The callback runs once inside the original Execution ownership constructor.
// It uses the same routed DeviceHandler, private Execution authority, Store,
// ProcessGuard and Agent configuration. Each acquired Tool owner is registered
// before another constructor can fail; the root retains the guard until all
// original Tool operations have actually joined.
func (b *executionRuntimeAssembly) runnerToolFactory(store execution.Store, objects *objectAssembly, agents ac.ExecutionConfiguration, registered *registry.Registry) func(*execution.ToolCallDriver) (tc.RunnerDispatcher, tc.RunnerResultAuthority, error) {
	return func(owner *execution.ToolCallDriver) (tc.RunnerDispatcher, tc.RunnerResultAuthority, error) {
		if b == nil || b.runnerDevice == nil || objects == nil || objects.guard == nil || owner == nil || registered == nil {
			return nil, nil, f.NewFault(f.DependencyUnbound, f.NotStarted)
		}
		schemas, err := toolruntime.NewRegistrySchemaValidator(store, registered)
		if err != nil {
			return nil, nil, err
		}
		readAuthority, err := toolruntime.NewReadFileAuthority(store, objects.guard, objects.process, owner)
		if err != nil {
			return nil, nil, err
		}
		b.add(runtimeWork(readAuthority))
		writeAuthority, err := toolruntime.NewWriteFileAuthority(store, objects.guard, objects.process, owner)
		if err != nil {
			return nil, nil, err
		}
		b.add(runtimeWork(writeAuthority))
		commandAuthority, err := toolruntime.NewRunCommandAuthority(store, objects.guard, objects.process, owner)
		if err != nil {
			return nil, nil, err
		}
		b.add(runtimeWork(commandAuthority))
		readMount, err := mount.NewReadFileResolver(store, agents, readAuthority)
		if err != nil {
			return nil, nil, err
		}
		writeMount, err := mount.NewWriteFileResolver(store, agents, writeAuthority)
		if err != nil {
			return nil, nil, err
		}
		commandMount, err := mount.NewCommandResolver(store, agents, commandAuthority)
		if err != nil {
			return nil, nil, err
		}
		readWire, err := runnerhttp.NewReadFileDispatcher(b.runnerDevice, readMount)
		if err != nil {
			return nil, nil, err
		}
		writeWire, err := runnerhttp.NewWriteFileDispatcher(b.runnerDevice, writeMount)
		if err != nil {
			return nil, nil, err
		}
		commandWire, err := runnerhttp.NewCommandDispatcher(b.runnerDevice, commandMount)
		if err != nil {
			return nil, nil, err
		}
		read, err := toolruntime.NewReadFileExecutor(readAuthority, readMount, readWire, schemas)
		if err != nil {
			return nil, nil, err
		}
		write, err := toolruntime.NewWriteFileExecutor(writeAuthority, writeMount, writeWire, schemas)
		if err != nil {
			return nil, nil, err
		}
		command, err := toolruntime.NewRunCommandExecutor(commandAuthority, commandMount, commandWire, schemas)
		if err != nil {
			return nil, nil, err
		}
		dispatch, err := toolruntime.NewRunnerExecutor(read, write, command)
		if err != nil {
			return nil, nil, err
		}
		readResult, err := toolruntime.NewReadFileResultReader(store, owner)
		if err != nil {
			return nil, nil, err
		}
		writeResult, err := toolruntime.NewWriteFileResultReader(store, owner)
		if err != nil {
			return nil, nil, err
		}
		commandResult, err := toolruntime.NewRunCommandResultReader(store, owner)
		if err != nil {
			return nil, nil, err
		}
		results, err := toolruntime.NewRunnerResultReader(readResult, writeResult, commandResult)
		if err != nil {
			return nil, nil, err
		}
		return dispatch, results, nil
	}
}
