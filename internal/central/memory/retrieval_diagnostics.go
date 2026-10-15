package memory

import (
	"context"

	rc "github.com/LunaDeerTech/agenteam/internal/central/retrieval/contract"
)

// DiagnosticBackend reports only the configured implementation. Serving and
// namespace authorization remain in the ordinary retrieval provider methods.
func (p *RetrievalProvider) DiagnosticBackend(context.Context, rc.Request) string {
	if p == nil {
		return ""
	}
	return string(p.backend)
}
