package mcp

import (
	"context"
	"math"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
)

func (s *Service) GetConnection(ctx context.Context, actor id.Actor, projectID id.ProjectID, connection ConnectionID) (Connection, error) {
	scope, err := id.InProject(projectID)
	if err != nil || connection.Validate() != nil {
		return Connection{}, fail(f.InvalidArgument)
	}
	var out Connection
	err = s.read(ctx, actor, scope, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		var e error
		out, e = scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1 AND project_id=$2`, connection.String(), projectID.String()))
		return e
	})
	return out, err
}
func (s *Service) Disable(ctx context.Context, command Command, connection ConnectionID) (CommandResult, error) {
	return s.stopConnection(ctx, command, connection, false)
}
func (s *Service) Disconnect(ctx context.Context, command Command, connection ConnectionID) (CommandResult, error) {
	return s.stopConnection(ctx, command, connection, true)
}
func (s *Service) stopConnection(ctx context.Context, command Command, connection ConnectionID, disconnect bool) (CommandResult, error) {
	if connection.Validate() != nil || command.ExpectedVersion.Validate() != nil || command.Scope.Details().Kind != id.ProjectScope {
		return CommandResult{}, fail(f.InvalidArgument)
	}
	name := "connection.disable"
	if disconnect {
		name = "connection.disconnect"
	}
	return s.write(ctx, command, name, struct {
		Connection ConnectionID
		Version    f.Version
	}{connection, command.ExpectedVersion}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) (CommandResult, error) {
		c, err := scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1 AND project_id=$2`, connection.String(), command.Scope.Details().ProjectID))
		if err != nil {
			return CommandResult{}, err
		}
		if c.Version != command.ExpectedVersion {
			return CommandResult{}, fail(f.VersionConflict)
		}
		if c.Version == math.MaxInt64 {
			return CommandResult{}, fail(f.InvalidState)
		}
		if err = s.removeTools(ctx, tx, c.ProjectID, c.ID.String()); err != nil {
			return CommandResult{}, err
		}
		// Runtime shadows and active Execution leases intentionally survive both
		// transitions; no new capture can pass the current binding authority.
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connections SET version=version+1,enabled=false,refresh_id=NULL,updated_at=clock_timestamp(),state=CASE WHEN $2 THEN 'unconnected' ELSE state END,credential_id=CASE WHEN $2 THEN NULL ELSE credential_id END,runtime_binding_id=CASE WHEN $2 THEN NULL ELSE runtime_binding_id END,protocol_revision=CASE WHEN $2 THEN NULL ELSE protocol_revision END WHERE id=$1`, connection.String(), disconnect)
		if err != nil {
			return CommandResult{}, err
		}
		if disconnect {
			if _, err = x.Exec(ctx, `UPDATE agenteam_mcp.resource_catalog SET active=false WHERE connection_id=$1`, connection.String()); err != nil {
				return CommandResult{}, err
			}
		}
		c, err = scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1`, connection.String()))
		if err != nil {
			return CommandResult{}, err
		}
		err = s.emit(ctx, tx, AuditEvent{Actor: command.Actor, Scope: command.Scope, Action: "mcp." + name, ConfigID: c.ConfigID.String(), ConnectionID: c.ID.String(), Version: c.Version, CauseID: digest([]byte(command.Identity.Canonical()))})
		return CommandResult{Connection: &c}, err
	})
}
