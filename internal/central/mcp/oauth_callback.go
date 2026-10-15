package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

// OAuthCallback is deliberately opaque and redacted. The browser supplies no
// project, redirect, credential or connection authority with its callback.
type OAuthCallback struct{ state, code, issuer string }

func NewOAuthCallback(state, code, issuer string) (OAuthCallback, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != state || !oauthCallbackCode(code) || !text(issuer, 16384) {
		return OAuthCallback{}, fail(f.InvalidArgument)
	}
	return OAuthCallback{state, code, issuer}, nil
}
func oauthCallbackCode(code string) bool {
	if len(code) == 0 || len(code) > 8192 {
		return false
	}
	for _, c := range code {
		if c < 33 || c > 126 {
			return false
		}
	}
	return true
}
func (OAuthCallback) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "mcp_oauth_callback") }
func (OAuthCallback) MarshalJSON() ([]byte, error) { return []byte(`"mcp_oauth_callback"`), nil }
func (OAuthCallback) LogValue() slog.Value         { return slog.StringValue("mcp_oauth_callback") }

type oauthExchange struct {
	leaseAbsent, leaseAcquired, leaseReleased, flowRetired bool
	owner                                                  oauthControlOwner
	flowID, credential, issuer, semantic                   string
	version                                                f.Version
	lease                                                  sc.LeaseID
}

// discoverOAuthCallback locates only the original session's flow. Discovery is
// read-only; the following full plan rechecks all facts before claiming it.
func (s *Service) discoverOAuthCallback(ctx context.Context, actor id.Actor, callback OAuthCallback) (id.Scope, error) {
	if s == nil || ctx == nil || actor.Validate() != nil || actor.Details().Kind != id.Human {
		return id.Scope{}, fail(f.Forbidden)
	}
	if _, err := NewOAuthCallback(callback.state, callback.code, callback.issuer); err != nil {
		return id.Scope{}, err
	}
	user, _ := f.UserLock(actor.Details().UserID)
	cause, _ := f.NewRecoveryCause("mcp.oauth.callback", actor.Details().SessionID, "")
	var projectID string
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, []f.LockRequest{{Key: user, Mode: f.Shared}, catalogLock(f.Shared)}); err != nil {
			return err
		}
		if err := s.options.Sessions.RequireCurrentSession(ctx, tx, actor); err != nil {
			return safeError(err)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		err = x.QueryRow(ctx, `SELECT project_id FROM agenteam_mcp.oauth_flows WHERE state_digest=$1 AND user_id=$2 AND session_id=$3 AND phase<>'legacy'`, digest([]byte(callback.state)), actor.Details().UserID, actor.Details().SessionID).Scan(&projectID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fail(f.NotFound)
		}
		return err
	})
	if err := commit(result); err != nil {
		return id.Scope{}, err
	}
	projectIDValue, err := f.ParseID[id.Project](projectID)
	if err != nil {
		return id.Scope{}, fail(f.InvalidState)
	}
	return id.InProject(projectIDValue)
}

func (s *Service) prepareOAuthExchange(ctx context.Context, actor id.Actor, key f.IdempotencyKey, callback OAuthCallback) (*oauthExchange, *CommandResult, error) {
	scope, err := s.discoverOAuthCallback(ctx, actor, callback)
	if err != nil {
		return nil, nil, err
	}
	identity, err := CommandIdentity(actor, scope, "oauth.complete", key)
	if err != nil {
		return nil, nil, err
	}
	request, err := f.NewID[struct{}]()
	if err != nil {
		return nil, nil, fail(f.DependencyUnavailable)
	}
	lease, err := f.NewID[sc.Lease]()
	if err != nil {
		return nil, nil, fail(f.DependencyUnavailable)
	}
	p := &oauthExchange{owner: oauthControlOwner{service: s, command: Command{Actor: actor, Scope: scope, Identity: identity}, requestID: request.String()}, lease: lease, leaseAbsent: true}
	raw, err := encode(struct{ State, Code, Issuer string }{digest([]byte(callback.state)), digest([]byte(callback.code)), callback.issuer})
	if err != nil {
		return nil, nil, err
	}
	p.semantic = digest(raw)
	locks, err := s.locks(actor, scope, &identity, true)
	if err != nil {
		return nil, nil, err
	}
	cause, _ := f.NewCommandsCause(identity)
	var replay *CommandResult
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, actor, scope, id.Mutate); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		var semantic, phase, user string
		var body []byte
		err = x.QueryRow(ctx, `SELECT semantic_digest,phase,actor_user_id,response FROM agenteam_mcp.commands WHERE command_digest=$1`, digest([]byte(identity.Canonical()))).Scan(&semantic, &phase, &user, &body)
		if err == nil {
			if semantic != p.semantic || user != actor.Details().UserID {
				return fail(f.IdempotencyKeyReused)
			}
			if phase != "complete" {
				return fail(f.ResourceBusy)
			}
			var result CommandResult
			if json.Unmarshal(body, &result) != nil || result.Connection == nil {
				return fail(f.InvalidState)
			}
			replay = &result
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var config, connection string
		var configRevision, connectionVersion f.Version
		var expires time.Time
		err = x.QueryRow(ctx, `SELECT id,config_id,config_revision,connection_id,connection_version,credential_id,credential_version,issuer,expires_at FROM agenteam_mcp.oauth_flows WHERE state_digest=$1 AND user_id=$2 AND session_id=$3 AND project_id=$4 AND phase='ready' AND consumed_at IS NULL AND expires_at>clock_timestamp()`, digest([]byte(callback.state)), actor.Details().UserID, actor.Details().SessionID, scope.Details().ProjectID).Scan(&p.flowID, &config, &configRevision, &connection, &connectionVersion, &p.credential, &p.version, &p.issuer, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return fail(f.ConfirmationStale)
		}
		if err != nil {
			return err
		}
		if callback.issuer != "" && callback.issuer != p.issuer {
			return fail(f.Forbidden)
		}
		configID, err := f.ParseID[ConfigIdentity](config)
		if err != nil {
			return fail(f.InvalidState)
		}
		p.owner.config, err = loadConfig(ctx, x, configID)
		if err != nil {
			return err
		}
		if !configVisible(p.owner.config, scope) || p.owner.config.RuntimeRevision != configRevision || p.owner.config.Authentication.Scheme != "oauth" {
			return fail(f.ConfirmationStale)
		}
		p.owner.connection, err = scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1 AND project_id=$2`, connection, scope.Details().ProjectID))
		if err != nil {
			return err
		}
		if p.owner.connection.ConfigID != configID || p.owner.connection.ConfigRevision != configRevision || p.owner.connection.Version != connectionVersion || p.owner.connection.State != "auth_required" {
			return fail(f.ConfirmationStale)
		}
		p.owner.command.ExpectedVersion = connectionVersion
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.commands(command_digest,semantic_digest,actor_user_id,scope_kind,project_id,command_name,phase) VALUES($1,$2,$3,'project',$4,'oauth.complete','pending')`, digest([]byte(identity.Canonical())), p.semantic, actor.Details().UserID, scope.Details().ProjectID)
		if err != nil {
			return err
		}
		if err = s.oauthProcessInTx(ctx, tx, p.owner.requestID, digest([]byte(identity.Canonical()))); err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.oauth_requests(id,operation,phase,project_id,config_id,config_revision,connection_id,connection_version,flow_id,command_digest,user_id,session_id,credential_id,credential_version,credential_lease_id,process_id) VALUES($1,'exchange','claimed',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, p.owner.requestID, scope.Details().ProjectID, config, int64(configRevision), connection, int64(connectionVersion), p.flowID, digest([]byte(identity.Canonical())), actor.Details().UserID, actor.Details().SessionID, p.credential, int64(p.version), p.lease.String(), s.owner.process.String())
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.oauth_flows SET phase='exchanging',consumed_at=clock_timestamp(),exchange_request_id=$2 WHERE id=$1 AND phase='ready' AND consumed_at IS NULL`, p.flowID, p.owner.requestID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(f.ConfirmationStale)
		}
		return nil
	}))
	if err != nil {
		return nil, nil, err
	}
	return p, replay, nil
}
