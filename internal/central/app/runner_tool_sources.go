package app

import (
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/registry"
)

// These are the original trusted metadata sources. Runtime authorization,
// current Mount facts and an active Runner are still required for each call.
func createRunnerToolSources(store registry.Store) (map[string]registry.RunnerSource, error) {
	read, err := registry.NewReadFileMetadataSource(store)
	if err != nil {
		return nil, err
	}
	write, err := registry.NewWriteFileMetadataSource(store)
	if err != nil {
		return nil, err
	}
	command, err := registry.NewRunCommandMetadataSource(store)
	if err != nil {
		return nil, err
	}
	return map[string]registry.RunnerSource{
		tc.RunnerReadFileKey: read, tc.RunnerWriteFileKey: write, tc.RunnerRunCommandKey: command,
	}, nil
}
