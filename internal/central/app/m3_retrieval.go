package app

import (
	"context"
	"github.com/LunaDeerTech/agenteam/internal/central/config"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	"github.com/LunaDeerTech/agenteam/internal/central/knowledge"
	knowledgeTools "github.com/LunaDeerTech/agenteam/internal/central/knowledge/tools"
	"github.com/LunaDeerTech/agenteam/internal/central/model"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/chunker"
	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/retrieval/document"
	rr "github.com/LunaDeerTech/agenteam/internal/central/retrieval/runtime"
	tr "github.com/LunaDeerTech/agenteam/internal/central/tool/runtime"
	"time"
)

// These bounded initial values are deployment defaults, not a claim of tuned
// relevance. Backend and dimensions must always be explicitly configured.
func m3RetrievalLimits() rc.Limits {
	return rc.Limits{LexicalK: 64, DenseK: 64, FusedK: 64, ResultK: 8, RRFConstant: 60, MaxResultBytes: 64 << 10}
}
func m3RetrievalTimeouts() rr.Timeouts {
	return rr.Timeouts{Lexical: 5 * time.Second, Embedding: 90 * time.Second, Dense: 5 * time.Second}
}

type indexModelServices struct {
	*model.Service
	*model.ModelCallLeaseRetirement
}

func constructKnowledgeIndex(ctx context.Context, cfg config.Config, db database, documents *knowledgeCallAssembly, authority *m3Authorities, execution *executionRuntimeAssembly, models *model.Service, observer rc.Observer) error {
	options, enabled := cfg.RetrievalRuntime()
	if !enabled {
		return nil
	}
	store, ok := db.(knowledge.Store)
	if !ok || documents == nil || documents.pair == nil || authority == nil || execution == nil || execution.modelRuntime == nil || execution.modelCalls == nil {
		return f.NewFault(f.DependencyUnbound, f.NotStarted)
	}
	profile, parser := document.RegistryProfile, (*document.Registry)(nil)
	if configured, enabled := cfg.KnowledgeParser(); enabled {
		// Configuration checks shapes only. Startup owns both version probes,
		// including cancellation and their original process Wait before return.
		pdf, err := document.NewPoppler(ctx, configured.Poppler())
		if err != nil {
			return err
		}
		parser, err = document.NewRegistry(pdf)
		if err != nil {
			return err
		}
		profile = document.PopplerRegistryProfile
	}
	settings := knowledge.IndexSettings{ParserProfile: profile, Chunker: chunker.Config{SoftBytes: 2048, HardBytes: 4096, ForcedOverlapBytes: 256}, Backend: options.Backend(), Dimension: options.Dimensions(), Retrieval: m3RetrievalLimits(), Timeouts: m3RetrievalTimeouts(), ModelTimeout: 2 * time.Minute}
	index, err := knowledge.NewIndexService(store, documents.pair.Service(), authority.knowledge, knowledge.IndexDependencies{Models: indexModelServices{models, execution.modelCalls}, Calls: execution.modelRuntime, ServingRetention: execution.servingRetention, EmbeddingProfiles: execution.embeddingProfiles, Parser: parser, Settings: settings, Observer: observer})
	if err != nil {
		return err
	}
	documents.index = index
	execution.knowledgeIndex = index
	execution.add(executionRuntimeWork{stop: index.StopAdmission, drain: index.Drain, joined: index.Joined})
	return nil
}
func knowledgeToolBackends(index *knowledge.IndexService) ([]tr.BuiltinBackend, error) {
	return knowledgeTools.NewBackends(index)
}
