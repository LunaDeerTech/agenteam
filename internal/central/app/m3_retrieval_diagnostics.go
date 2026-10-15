package app

import (
	"context"

	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
	"github.com/LunaDeerTech/agenteam/internal/platform/logging"
)

// Both domains observe the original process logger through one text-free
// projection. The runtime supplies actual branch facts; the logger validates
// the closed fields independently. This observer grants no retrieval access.
type m3RetrievalObserver struct {
	logger interface {
		Retrieval(logging.RetrievalDiagnostic)
	}
}

func (o m3RetrievalObserver) ObserveRetrieval(_ context.Context, d rc.Diagnostics) {
	if o.logger == nil {
		return
	}
	o.logger.Retrieval(logging.RetrievalDiagnostic{
		RequestID: d.RequestID, ProjectID: d.ProjectID, AgentID: d.AgentID,
		Domain: string(d.Domain), QuerySHA256: d.QuerySHA256, QueryBytes: d.QueryBytes,
		Backend: d.Backend, Mode: string(d.Mode),
		LexicalCandidates: d.LexicalCandidates, DenseCandidates: d.DenseCandidates,
		FusionCandidates: d.FusionCandidates, ResultCount: d.ResultCount,
		RerankUsed: d.RerankUsed, DegradationReason: d.DegradationReason, ErrorCode: d.ErrorCode,
		Lexical: d.Lexical, Embedding: d.Embedding, Dense: d.Dense, Fusion: d.Fusion, Total: d.Total,
	})
}

var _ rc.Observer = m3RetrievalObserver{}
