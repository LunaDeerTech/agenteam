package mcp

import (
	"context"
	"encoding/json"
	"errors"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

type CommandObservation struct {
	State  string         `json:"state"`
	Result *CommandResult `json:"result,omitempty"`
}

// LookupCommand observes the original owner's command. Pending does not grant
// permission to repeat a connect, discovery or OAuth network exchange.
func (s *Service) LookupCommand(ctx context.Context, actor id.Actor, scope id.Scope, name string, key f.IdempotencyKey) (CommandObservation, error) {
	switch name {
	case "config.create", "config.update", "config.delete", "connection.connect", "connection.refresh", "connection.enable", "connection.disable", "connection.disconnect":
	case "oauth.begin", "oauth.complete":
		if scope.Details().Kind != id.ProjectScope {
			return CommandObservation{}, fail(f.InvalidArgument)
		}
	default:
		return CommandObservation{}, fail(f.InvalidArgument)
	}
	identity, err := CommandIdentity(actor, scope, name, key)
	if err != nil {
		return CommandObservation{}, err
	}
	out := CommandObservation{State: "not_found"}
	err = s.read(ctx, actor, scope, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		if name == "oauth.begin" || name == "oauth.complete" {
			var err error
			out, err = lookupOAuthCommand(ctx, x, actor, scope, name, identity)
			return err
		}
		var phase string
		var body []byte
		err := x.QueryRow(ctx, `SELECT phase,response FROM agenteam_mcp.commands WHERE command_digest=$1 AND actor_user_id=$2 AND scope_kind=$3 AND COALESCE(project_id,'')=$4 AND command_name=$5`, digest([]byte(identity.Canonical())), actor.Details().UserID, string(scope.Details().Kind), scope.Details().ProjectID, name).Scan(&phase, &body)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return safeError(err)
		}
		switch phase {
		case "pending":
			if len(body) != 0 {
				return fail(f.InvalidState)
			}
			out.State = phase
			if name == "connection.refresh" {
				stopped, err := humanRefreshCommandStopped(ctx, x, actor, scope, digest([]byte(identity.Canonical())))
				if err != nil {
					return err
				}
				if stopped {
					out.State = "stopped"
				}
			}
		case "complete":
			var result CommandResult
			if len(body) == 0 || len(body) > 128<<10 || json.Unmarshal(body, &result) != nil {
				return fail(f.InvalidState)
			}
			out.State, out.Result = phase, &result
		default:
			return fail(f.InvalidState)
		}
		return nil
	})
	return out, err
}

// This is an observation of the original request's retired resources, never
// an inference about the remote refresh result or permission to resend it.
func humanRefreshCommandStopped(ctx context.Context, x postgres.SQLExecutor, actor id.Actor, scope id.Scope, command string) (bool, error) {
	rows, err := x.Query(ctx, `SELECT id FROM agenteam_mcp.oauth_requests
 WHERE operation='refresh' AND command_digest=$1 AND project_id=$2 AND user_id=$3 AND session_id=$4
 AND process_required AND process_id IS NOT NULL ORDER BY id LIMIT 2`, command,
		scope.Details().ProjectID, actor.Details().UserID, actor.Details().SessionID)
	if err != nil {
		return false, safeError(err)
	}
	var ids []string
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			break
		}
		ids = append(ids, raw)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return false, safeError(err)
	}
	if len(ids) == 0 {
		return false, nil
	}
	if len(ids) != 1 {
		return false, fail(f.InvalidState)
	}
	source, err := loadOAuthRefreshFact(ctx, x, ids[0])
	if err != nil {
		return false, err
	}
	if !source.stoppedHumanRefreshCaller(actor, scope, command) {
		return false, nil
	}
	connection, err := loadHumanRefreshConnection(ctx, x, source)
	if err != nil {
		return false, err
	}
	if connection.phase != "abandoned" || !connection.retired || !connection.released {
		return false, nil
	}
	var pending bool
	err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connections WHERE id=$1 AND refresh_id=$2)`,
		connection.connection, connection.request).Scan(&pending)
	return err == nil && !pending, safeError(err)
}

func (source oauthRefreshFact) stoppedHumanRefreshCaller(actor id.Actor, scope id.Scope, command string) bool {
	return source.validate() == nil && actor.Validate() == nil && scope.Validate() == nil &&
		actor.Details().Kind == id.Human && scope.Details().Kind == id.ProjectScope &&
		source.caller.command == command && source.project == scope.Details().ProjectID &&
		source.caller.user == actor.Details().UserID && source.caller.session == actor.Details().SessionID &&
		source.retired && source.released && source.phase != "claimed"
}
