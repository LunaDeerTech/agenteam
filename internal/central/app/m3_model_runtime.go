package app

import (
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	"github.com/LunaDeerTech/agenteam/internal/central/execution"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/model/adapter"
	mc "github.com/LunaDeerTech/agenteam/internal/central/model/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/secret"
)

// These owners are constructed before Registry captures domain tool sources.
// Initialize still belongs to the original startup after ProcessGuard binding.
func constructExecutionModels(b *executionRuntimeAssembly, cfg config.Config, store execution.Store, a *executionRuntimeAuthorities, projects *projectUsageAssembly, secrets *secret.Service, outbound *outboundRuntime, media ...mc.MediaSource) error {
	options, enabled := cfg.ExecutionRuntime()
	if !enabled || b == nil || b.modelRuntime != nil || a == nil || projects == nil || secrets == nil || outbound == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	if len(media) > 1 || len(media) == 1 && runtimeInformationNil(media[0]) {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	var source mc.MediaSource
	if len(media) == 1 {
		source = media[0]
	}
	budget := adapter.NewBudget()
	b.add(executionRuntimeWork{stop: budget.StopAdmission, drain: budget.Drain, joined: budget.Joined})
	wireTransport := adapter.Transport{Policy: outbound.policy, Trust: cfg.OutboundTrust(), Resolver: outbound.resolver}
	wire, err := adapter.NewOpenAIChat(wireTransport, budget)
	if err != nil {
		return err
	}
	embeddings, err := adapter.NewOpenAIEmbeddings(wireTransport, budget)
	if err != nil {
		return err
	}
	runtime, err := model.NewRuntimeWithAgentRetry(store, a.model, model.RuntimeDependencies{
		Media: source, Usage: projects.reader, SecretReader: secrets, SecretUsage: secrets, Adapter: wire, Embeddings: embeddings,
	}, options.AgentRetryTiming())
	if err != nil {
		return err
	}
	b.add(executionRuntimeWork{stop: runtime.StopAdmission, drain: runtime.Drain, joined: runtime.Joined})
	b.initialize = runtime.Initialize
	b.modelRuntime = runtime
	b.modelCalls, err = model.NewModelCallLeaseRetirement(a.model, secrets)
	if err != nil {
		return err
	}
	return nil
}
