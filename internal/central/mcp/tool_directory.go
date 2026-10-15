package mcp

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
)

func (s *Service) ListTools(ctx context.Context, actor id.Actor, project id.ProjectID, connection ConnectionID) ([]tc.MCPRegistrationView, error) {
	current, err := s.GetConnection(ctx, actor, project, connection)
	if err != nil {
		return nil, err
	}
	reader, ok := s.options.Registry.(tc.MCPToolDirectory)
	if !ok || nilPort(reader) {
		return nil, fail(f.DependencyUnbound)
	}
	values, err := reader.ListMCPTools(ctx, actor, project, connection.String())
	if err != nil {
		return nil, safeError(err)
	}
	// The Registry owns its own transaction. Recheck the current domain owner
	// after that read so deletion/reconnection cannot publish a mixed directory.
	after, err := s.GetConnection(ctx, actor, project, connection)
	if err != nil {
		return nil, err
	}
	if current.Version != after.Version || current.ConfigRevision != after.ConfigRevision {
		return nil, fail(f.ConfirmationStale)
	}
	for _, value := range values {
		if value.Binding.ProjectID != project || value.Binding.ConnectionID != connection.String() || value.Binding.ConfigID != current.ConfigID.String() {
			return nil, fail(f.InvalidState)
		}
	}
	return values, nil
}
