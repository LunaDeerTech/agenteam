package mcp

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type OAuthAuthorization struct {
	FlowID           string    `json:"flow_id"`
	AuthorizationURL string    `json:"authorization_url"`
	ExpiresAt        time.Time `json:"expires_at"`
}
type pendingOAuth struct {
	owner         oauthControlOwner
	flowID, state string
	expires       time.Time
	semantic      string
}

// BeginOAuth first persists the original owner, session and configuration
// revision, then performs bounded standard discovery/registration outside SQL.
// No HTTP handler enables this entry until callback and retirement are wired.
func (s *Service) BeginOAuth(ctx context.Context, command Command, config ConfigID) (CommandResult, error) {
	if s == nil || s.options.OAuth == nil || nilPort(s.options.OAuth.Writes) || s.options.OAuth.RedirectURI == "" {
		return CommandResult{}, fail(f.DependencyUnbound)
	}
	ctx, owned, err := s.beginOAuthProcess(ctx)
	if err != nil {
		return CommandResult{}, err
	}
	defer owned.returned(ctx)
	p, replay, err := s.prepareOAuthBegin(ctx, command, config)
	if err != nil {
		return CommandResult{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	key, err := ac.NewAppendKey(ac.AccessProducer, p.owner.requestID, 0)
	if err != nil {
		return CommandResult{}, err
	}
	call, err := outbound.NewCallContext(command.Actor, command.Scope, key, ac.Associations{RequestID: p.owner.requestID})
	if err != nil {
		return CommandResult{}, err
	}
	metadata, err := s.options.Protocol.DiscoverOAuth(ctx, p.owner.config.Endpoint, call)
	if err != nil {
		return s.failOAuthBegin(ctx, p, err)
	}
	prepared, err := s.options.Protocol.PrepareOAuth(ctx, metadata, s.options.OAuth.RedirectURI, p.state, call)
	if err != nil {
		return s.failOAuthBegin(ctx, p, err)
	}
	defer prepared.Material.Destroy()
	return s.finishOAuthBegin(ctx, p, prepared, metadata.Issuer)
}

func (s *Service) prepareOAuthBegin(ctx context.Context, c Command, config ConfigID) (*pendingOAuth, *CommandResult, error) {
	if ctx == nil || validateCommand(c, "oauth.begin") != nil || c.Scope.Details().Kind != id.ProjectScope || config.Validate() != nil || c.ExpectedVersion < 0 {
		return nil, nil, fail(f.InvalidArgument)
	}
	raw, err := encode(struct {
		Config  ConfigID
		Version f.Progress
	}{config, f.Progress(c.ExpectedVersion)})
	if err != nil {
		return nil, nil, err
	}
	request, err := f.NewID[struct{}]()
	if err != nil {
		return nil, nil, fail(f.DependencyUnavailable)
	}
	flow, err := f.NewID[struct{}]()
	if err != nil {
		return nil, nil, fail(f.DependencyUnavailable)
	}
	connection, err := f.NewID[ConnectionIdentity]()
	if err != nil {
		return nil, nil, fail(f.DependencyUnavailable)
	}
	var state [32]byte
	if _, err = rand.Read(state[:]); err != nil {
		return nil, nil, fail(f.DependencyUnavailable)
	}
	p := &pendingOAuth{owner: oauthControlOwner{service: s, command: c, requestID: request.String()}, flowID: flow.String(), state: base64.RawURLEncoding.EncodeToString(state[:]), expires: time.Now().UTC().Add(10 * time.Minute), semantic: digest(raw)}
	locks, err := s.locks(c.Actor, c.Scope, &c.Identity, true)
	if err != nil {
		return nil, nil, err
	}
	cause, _ := f.NewCommandsCause(c.Identity)
	var replay *CommandResult
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, c.Actor, c.Scope, id.Mutate); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		var semantic, phase, user string
		var body []byte
		err = x.QueryRow(ctx, `SELECT semantic_digest,phase,actor_user_id,response FROM agenteam_mcp.commands WHERE command_digest=$1`, digest([]byte(c.Identity.Canonical()))).Scan(&semantic, &phase, &user, &body)
		if err == nil {
			if semantic != p.semantic || user != c.Actor.Details().UserID {
				return fail(f.IdempotencyKeyReused)
			}
			if phase != "complete" {
				return fail(f.ResourceBusy)
			}
			var value CommandResult
			if json.Unmarshal(body, &value) != nil {
				return fail(f.InvalidState)
			}
			if value.OAuth != nil {
				var current bool
				err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.oauth_flows WHERE id=$1 AND user_id=$2 AND session_id=$3 AND phase='ready' AND expires_at>clock_timestamp())`, value.OAuth.FlowID, c.Actor.Details().UserID, c.Actor.Details().SessionID).Scan(&current)
				if err != nil {
					return err
				}
				if !current {
					return fail(f.ConfirmationStale)
				}
			}
			replay = &value
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		p.owner.config, err = loadConfig(ctx, x, config)
		if err != nil {
			return err
		}
		if !configVisible(p.owner.config, c.Scope) {
			return fail(f.NotFound)
		}
		if p.owner.config.Authentication.Scheme != "oauth" {
			return fail(f.InvalidArgument)
		}
		p.owner.connection, err = scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE project_id=$1 AND config_id=$2`, c.Scope.Details().ProjectID, config.String()))
		var fault *f.Fault
		if errors.As(err, &fault) && fault.Code == f.NotFound {
			if c.ExpectedVersion != 0 {
				return fail(f.VersionConflict)
			}
			_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.connections(id,project_id,config_id,config_revision,state) VALUES($1,$2,$3,$4,'auth_required')`, connection.String(), c.Scope.Details().ProjectID, config.String(), int64(p.owner.config.RuntimeRevision))
		} else if err == nil {
			if p.owner.connection.Version != c.ExpectedVersion {
				return fail(f.VersionConflict)
			}
			if c.ExpectedVersion == math.MaxInt64 {
				return fail(f.InvalidState)
			}
			connection = p.owner.connection.ID
			if err = s.removeTools(ctx, tx, p.owner.connection.ProjectID, connection.String()); err != nil {
				return err
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connections SET version=version+1,config_revision=$2,state='auth_required',enabled=false,refresh_id=NULL,credential_id=NULL,runtime_binding_id=NULL,protocol_revision=NULL,updated_at=clock_timestamp() WHERE id=$1`, connection.String(), int64(p.owner.config.RuntimeRevision))
		}
		if err != nil {
			return err
		}
		p.owner.connection, err = scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1`, connection.String()))
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.commands(command_digest,semantic_digest,actor_user_id,scope_kind,project_id,command_name,phase) VALUES($1,$2,$3,'project',$4,'oauth.begin','pending')`, digest([]byte(c.Identity.Canonical())), p.semantic, c.Actor.Details().UserID, c.Scope.Details().ProjectID)
		if err != nil {
			return err
		}
		if err = s.oauthProcessInTx(ctx, tx, p.owner.requestID, digest([]byte(c.Identity.Canonical()))); err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.oauth_requests(id,operation,project_id,config_id,config_revision,connection_id,connection_version,flow_id,command_digest,user_id,session_id,process_id) VALUES($1,'begin',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, p.owner.requestID, c.Scope.Details().ProjectID, config.String(), int64(p.owner.config.RuntimeRevision), connection.String(), int64(p.owner.connection.Version), p.flowID, digest([]byte(c.Identity.Canonical())), c.Actor.Details().UserID, c.Actor.Details().SessionID, s.owner.process.String())
		if err != nil {
			return err
		}
		return s.emit(ctx, tx, AuditEvent{Actor: c.Actor, Scope: c.Scope, Action: string(ac.MCPConnectionConnect), ConfigID: config.String(), ConnectionID: connection.String(), Version: p.owner.connection.Version, CauseID: p.owner.requestID})
	}))
	if err != nil {
		return nil, nil, err
	}
	return p, replay, nil
}

func (s *Service) finishOAuthBegin(ctx context.Context, p *pendingOAuth, prepared protocol.OAuthPrepared, issuer string) (CommandResult, error) {
	c := p.owner.command
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(p.owner.requestID, c.Scope)
	if err != nil {
		return CommandResult{}, err
	}
	key, err := f.ParseIdempotencyKey(p.owner.requestID)
	if err != nil {
		return CommandResult{}, err
	}
	identity, err := f.NewCommandIdentity("secret.mcp", []string{p.owner.connection.ProjectID.String(), p.owner.connection.ID.String(), p.flowID}, string(sc.Create), key)
	if err != nil {
		return CommandResult{}, err
	}
	request, err := sc.NewMCPOAuthWriteRequest(sc.MCPOAuthWriteFields{Actor: actor, Identity: identity, ProjectID: p.owner.connection.ProjectID, ConnectionID: p.owner.connection.ID.String(), ConfigRevision: p.owner.config.RuntimeRevision, Kind: sc.Create, OwnerKind: sc.MCPOAuthFlow, OwnerID: p.flowID, Value: prepared.Material})
	if err != nil {
		return CommandResult{}, err
	}
	binding, err := sc.MCPOAuthWriteBinding(request)
	if err != nil {
		return CommandResult{}, err
	}
	locks, err := s.locks(c.Actor, c.Scope, &c.Identity, true)
	if err != nil {
		return CommandResult{}, err
	}
	cause, _ := f.NewCommandsCause(c.Identity)
	// Persist the exact material-bound intent before Secret seals a write. A
	// repeated begin can only observe this request, not register another client.
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, c.Actor, c.Scope, id.Mutate); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET phase='claimed',secret_write_binding=$2 WHERE id=$1 AND phase='pending'`, p.owner.requestID, string(binding))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(f.ConfirmationStale)
		}
		return p.owner.checkOAuthWriteOwnerInTx(ctx, tx)
	}))
	if err != nil {
		return CommandResult{}, err
	}
	w := &oauthWriteUse{service: s, request: request.WithoutMaterial(), requestID: p.owner.requestID, mapping: binding, locks: locks, owner: &p.owner}
	w.active.Store(true)
	defer w.active.Store(false)
	writeCtx := context.WithValue(ctx, oauthWriteKey{}, w)
	plan, err := s.options.OAuth.Writes.DiscoverMCPOAuthWrite(writeCtx, request.WithoutMaterial())
	if err != nil {
		return CommandResult{}, safeError(err)
	}
	write, err := s.options.OAuth.Writes.PrepareMCPOAuthWrite(writeCtx, request, plan)
	if err != nil {
		return CommandResult{}, safeError(err)
	}
	prepared.Material.Destroy()
	var output CommandResult
	result := s.store.WithinTx(writeCtx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, write.RequiredLocks()); err != nil {
			return err
		}
		applied, err := s.options.OAuth.Writes.ApplyMCPOAuthWriteInTx(ctx, tx, write)
		if err != nil {
			return err
		}
		if !applied.Found {
			return fail(f.InvalidState)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		version := applied.Result.Metadata.Version
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.oauth_flows(id,connection_id,project_id,user_id,session_id,state_digest,credential_id,issuer,expires_at,config_id,config_revision,connection_version,begin_request_id,credential_version,phase) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'ready')`, p.flowID, p.owner.connection.ID.String(), p.owner.connection.ProjectID.String(), c.Actor.Details().UserID, c.Actor.Details().SessionID, digest([]byte(p.state)), write.Ref().Details().ID.String(), issuer, p.expires, p.owner.config.ID.String(), int64(p.owner.config.RuntimeRevision), int64(p.owner.connection.Version), p.owner.requestID, int64(version))
		if err != nil {
			return err
		}
		output = CommandResult{Connection: &p.owner.connection, OAuth: &OAuthAuthorization{FlowID: p.flowID, AuthorizationURL: prepared.AuthorizationURL, ExpiresAt: p.expires}}
		body, err := encode(output)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET phase='complete',result_credential_id=$2,result_credential_version=$3,io_retired=true,lease_released=true,retired_at=clock_timestamp() WHERE id=$1`, p.owner.requestID, write.Ref().Details().ID.String(), int64(version))
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.commands SET phase='complete',response=$2 WHERE command_digest=$1 AND phase='pending'`, digest([]byte(c.Identity.Canonical())), body)
		return err
	})
	if err = commit(result); err != nil {
		if result.State() == f.Unknown {
			// Keep the original sealed Secret command and private owner alive.
			// This is solely an observation of its atomic transaction; it never
			// prepares, applies, registers or exchanges again.
			observeCtx, cancel := context.WithTimeout(context.WithoutCancel(writeCtx), 5*time.Second)
			defer cancel()
			observeCause, _ := f.NewRecoveryCause("mcp.oauth.observe", p.owner.requestID, "")
			found := false
			observed := s.store.WithinTx(observeCtx, observeCause, func(ctx context.Context, tx f.Tx) error {
				if e := s.store.AcquireAll(ctx, tx, write.RequiredLocks()); e != nil {
					return e
				}
				receipt, e := s.options.OAuth.Writes.ObserveMCPOAuthWriteInTx(ctx, tx, write)
				if e != nil || !receipt.Found {
					return e
				}
				x, e := s.store.InTx(tx)
				if e != nil {
					return e
				}
				var body []byte
				e = x.QueryRow(ctx, `SELECT response FROM agenteam_mcp.commands WHERE command_digest=$1 AND semantic_digest=$2 AND phase='complete'`, digest([]byte(c.Identity.Canonical())), p.semantic).Scan(&body)
				if e != nil {
					return e
				}
				var value CommandResult
				if json.Unmarshal(body, &value) != nil || value.OAuth == nil || value.OAuth.FlowID != p.flowID || value.Connection == nil || value.Connection.ID != p.owner.connection.ID || value.Connection.Version != p.owner.connection.Version {
					return fail(f.InvalidState)
				}
				output, found = value, true
				return nil
			})
			if observed.State() == f.Committed && found {
				return output, nil
			}
		}
		return CommandResult{}, err
	}
	return output, nil
}

func (s *Service) failOAuthBegin(ctx context.Context, p *pendingOAuth, network error) (CommandResult, error) {
	// A cancelled caller still leaves durable original-request facts for recovery;
	// it never causes a second registration or token exchange.
	if ctx.Err() != nil {
		return CommandResult{}, ctx.Err()
	}
	c := p.owner.command
	var output CommandResult
	locks, err := s.locks(c.Actor, c.Scope, &c.Identity, true)
	if err != nil {
		return CommandResult{}, err
	}
	cause, _ := f.NewCommandsCause(c.Identity)
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, c.Actor, c.Scope, id.Mutate); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		code := safeProtocolCode(network)
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET phase='failed',error_code=$2,io_retired=true,lease_released=true,retired_at=clock_timestamp() WHERE id=$1 AND phase='pending'`, p.owner.requestID, code)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.connections SET last_error_code=$2 WHERE id=$1 AND version=$3 AND config_revision=$4`, p.owner.connection.ID.String(), code, int64(p.owner.connection.Version), int64(p.owner.config.RuntimeRevision))
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(f.ConfirmationStale)
		}
		connection := p.owner.connection
		connection.LastErrorCode = code
		output = CommandResult{Connection: &connection}
		body, err := encode(output)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.commands SET phase='complete',response=$2 WHERE command_digest=$1 AND phase='pending'`, digest([]byte(c.Identity.Canonical())), body)
		return err
	}))
	return output, err
}
