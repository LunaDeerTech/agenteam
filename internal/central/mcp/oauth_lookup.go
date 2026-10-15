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

// Called only inside the original Owner read transaction. A command key is
// user scoped; OAuth observations additionally require the original session.
// Neither a pending nor an absent receipt permits another network exchange.
func lookupOAuthCommand(ctx context.Context, x postgres.SQLExecutor, actor id.Actor, scope id.Scope, name string, identity f.CommandIdentity) (CommandObservation, error) {
	out := CommandObservation{State: "not_found"}
	operation := "begin"
	if name == "oauth.complete" {
		operation = "exchange"
	} else if name != "oauth.begin" {
		return out, fail(f.InvalidArgument)
	}
	if actor.Validate() != nil || actor.Details().Kind != id.Human || scope.Validate() != nil || scope.Details().Kind != id.ProjectScope || identity.Validate() != nil {
		return out, fail(f.InvalidArgument)
	}
	var phase string
	var body []byte
	err := x.QueryRow(ctx, `SELECT c.phase,c.response FROM agenteam_mcp.commands c
 JOIN agenteam_mcp.oauth_requests r ON r.command_digest=c.command_digest
 WHERE c.command_digest=$1 AND c.actor_user_id=$2 AND c.scope_kind='project'
 AND c.project_id=$3 AND c.command_name=$4 AND r.operation=$5
 AND r.user_id=c.actor_user_id AND r.project_id=c.project_id AND r.session_id=$6`,
		digest([]byte(identity.Canonical())), actor.Details().UserID, scope.Details().ProjectID, name, operation, actor.Details().SessionID).Scan(&phase, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, safeError(err)
	}
	if phase == "pending" && len(body) == 0 {
		stopped, err := observeStoppedOAuthCommand(ctx, x, actor, scope, name, operation, identity, phase)
		if err != nil {
			return out, err
		}
		if stopped {
			return CommandObservation{State: "stopped"}, nil
		}
		return CommandObservation{State: "pending"}, nil
	}
	var result CommandResult
	if phase != "complete" || len(body) == 0 || len(body) > 128<<10 || json.Unmarshal(body, &result) != nil {
		return out, fail(f.InvalidState)
	}
	if result.OAuth != nil {
		if name != "oauth.begin" {
			return out, fail(f.InvalidState)
		}
		var current bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_flows f
 JOIN agenteam_mcp.oauth_requests r ON r.id=f.begin_request_id
 WHERE f.id=$1 AND f.user_id=$2 AND f.session_id=$3 AND f.project_id=$4
 AND f.phase='ready' AND f.expires_at>clock_timestamp() AND r.command_digest=$5 AND r.operation='begin')`,
			result.OAuth.FlowID, actor.Details().UserID, actor.Details().SessionID, scope.Details().ProjectID, digest([]byte(identity.Canonical()))).Scan(&current)
		if err != nil {
			return out, safeError(err)
		}
		if !current {
			// The historical successful Begin receipt stays immutable. A stale
			// URL alone cannot retire its intent; require the complete original
			// cold cleanup proof before suppressing that unusable authorization.
			stopped, err := observeStoppedOAuthCommand(ctx, x, actor, scope, name, operation, identity, phase)
			if err != nil {
				return out, err
			}
			if stopped {
				return CommandObservation{State: "stopped"}, nil
			}
			return out, fail(f.ConfirmationStale)
		}
	}
	return CommandObservation{State: "complete", Result: &result}, nil
}

// A complete cold claim proves that the actual original process and every
// locally owned lease have retired. It establishes neither a remote outcome
// nor a new receipt. The original Session may end its retained intent and
// explicitly choose a new operation, without replaying this one.
func observeStoppedOAuthCommand(ctx context.Context, x postgres.SQLExecutor, actor id.Actor, scope id.Scope, name, operation string, identity f.CommandIdentity, phase string) (bool, error) {
	var stopped bool
	err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.commands c
 JOIN agenteam_mcp.oauth_requests r ON r.command_digest=c.command_digest
 JOIN agenteam_mcp.oauth_recovery_claims recovery ON recovery.request_id=r.id
 WHERE c.command_digest=$1 AND c.actor_user_id=$2 AND c.scope_kind='project'
 AND c.project_id=$3 AND c.command_name=$4 AND c.phase=$7
 AND r.operation=$5 AND r.user_id=c.actor_user_id AND r.project_id=c.project_id AND r.session_id=$6
 AND r.process_required AND r.phase IN ('unknown','failed','complete') AND r.io_retired AND r.lease_released
 AND recovery.phase='complete' AND recovery.original_process_id=r.process_id)`,
		digest([]byte(identity.Canonical())), actor.Details().UserID, scope.Details().ProjectID, name, operation, actor.Details().SessionID, phase).Scan(&stopped)
	return stopped, safeError(err)
}
