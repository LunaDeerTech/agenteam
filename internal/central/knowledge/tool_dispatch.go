package knowledge

import (
	"context"
	"encoding/json"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	kc "github.com/LunaDeerTech/agenteam/internal/central/knowledge/contract"
)

// ExecuteTool consumes arguments only after the original Tool authority loans
// and matches the same bytes inside each business authorization transaction.
func (s *IndexService) ExecuteTool(ctx context.Context, invocation kc.Invocation, name string, raw []byte) ([]byte, error) {
	if s == nil {
		return nil, fault(f.DependencyUnbound)
	}
	var result any
	switch name {
	case "query-doc":
		request, err := parseQueryArguments(raw)
		if err != nil {
			return nil, err
		}
		out, err := s.QueryDocuments(ctx, invocation, request)
		if err != nil {
			return nil, err
		}
		result = out
	case "read-doc":
		request, err := parseReadArguments(raw)
		if err != nil {
			return nil, err
		}
		out, err := s.ReadForInvocation(ctx, invocation, request.Document, request.Version, request.Request)
		if err != nil {
			return nil, err
		}
		result = out
	default:
		return nil, fault(f.CapabilityUnsupported)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, internal(err)
	}
	return raw, nil
}
