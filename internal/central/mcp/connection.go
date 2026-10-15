package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync/atomic"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	tc "github.com/LunaDeerTech/agenteam/internal/central/tool/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/tool/schema"
	"github.com/jackc/pgx/v5"
)

const connectionColumns = `id,project_id,config_id,version,config_revision,state,enabled,COALESCE(credential_id,''),COALESCE(runtime_binding_id,''),COALESCE(protocol_revision,''),tools_state,resources_state,tools_revision,resources_revision,last_error_code`

func scanConnection(row interface{ Scan(...any) error }) (Connection, error) {
	var c Connection
	var connection, project, config string
	err := row.Scan(&connection, &project, &config, &c.Version, &c.ConfigRevision, &c.State, &c.Enabled, &c.CredentialID, &c.RuntimeBindingID, &c.ProtocolRevision, &c.ToolsState, &c.ResourcesState, &c.ToolsRevision, &c.ResourcesRevision, &c.LastErrorCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, fail(f.NotFound)
	}
	if err != nil {
		return c, safeError(err)
	}
	c.ID, err = f.ParseID[ConnectionIdentity](connection)
	if err != nil {
		return c, fail(f.InvalidState)
	}
	c.ProjectID, err = f.ParseID[id.Project](project)
	if err != nil {
		return c, fail(f.InvalidState)
	}
	c.ConfigID, err = f.ParseID[ConfigIdentity](config)
	if err != nil || c.Version.Validate() != nil || c.ConfigRevision.Validate() != nil {
		return c, fail(f.InvalidState)
	}
	return c, nil
}
func (s *Service) ListConnections(ctx context.Context, actor id.Actor, projectID id.ProjectID) ([]Connection, error) {
	scope, err := id.InProject(projectID)
	if err != nil {
		return nil, fail(f.InvalidArgument)
	}
	var out []Connection
	err = s.read(ctx, actor, scope, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		rows, err := x.Query(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE project_id=$1 ORDER BY id`, projectID.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanConnection(rows)
			if err != nil {
				return err
			}
			out = append(out, c)
			if len(out) > 512 {
				return fail(f.PayloadTooLarge)
			}
		}
		return rows.Err()
	})
	return out, err
}

type ConnectInput struct {
	ConfigID     ConfigID `json:"config_id"`
	CredentialID string   `json:"credential_id,omitempty"`
}
type pendingConnection struct {
	command                                  Command
	name                                     string
	config                                   Config
	connection                               Connection
	requestID, commandDigest, semanticDigest string
	credentialLease                          sc.LeaseID
}

func (s *Service) Connect(ctx context.Context, command Command, input ConnectInput) (CommandResult, error) {
	return s.synchronize(ctx, command, input, "connection.connect")
}
func (s *Service) Refresh(ctx context.Context, command Command, input ConnectInput) (CommandResult, error) {
	return s.synchronize(ctx, command, input, "connection.refresh")
}
func (s *Service) Enable(ctx context.Context, command Command, input ConnectInput) (CommandResult, error) {
	return s.synchronize(ctx, command, input, "connection.enable")
}

func connectionSemantic(input ConnectInput, version f.Version) ([]byte, error) {
	return encode(struct {
		Input   ConnectInput
		Version f.Progress
	}{input, f.Progress(version)})
}

func (s *Service) prepareConnection(ctx context.Context, command Command, input ConnectInput, name string) (*pendingConnection, *CommandResult, error) {
	if ctx == nil || validateCommand(command, name) != nil || command.Scope.Details().Kind != id.ProjectScope || input.ConfigID.Validate() != nil || command.ExpectedVersion < 0 {
		return nil, nil, fail(f.InvalidArgument)
	}
	if nilPort(s.options.Registry) {
		return nil, nil, fail(f.DependencyUnbound)
	}
	if input.CredentialID != "" {
		if _, err := f.ParseID[sc.Credential](input.CredentialID); err != nil {
			return nil, nil, fail(f.InvalidArgument)
		}
	}
	raw, err := connectionSemantic(input, command.ExpectedVersion)
	if err != nil {
		return nil, nil, err
	}
	request, err := f.NewID[struct{}]()
	if err != nil {
		return nil, nil, fail(f.DependencyUnavailable)
	}
	connection, err := f.NewID[ConnectionIdentity]()
	if err != nil {
		return nil, nil, fail(f.DependencyUnavailable)
	}
	p := &pendingConnection{command: command, name: name, requestID: request.String(), commandDigest: digest([]byte(command.Identity.Canonical())), semanticDigest: digest(raw)}
	if input.CredentialID != "" {
		p.credentialLease, err = f.NewID[sc.Lease]()
		if err != nil {
			return nil, nil, fail(f.DependencyUnavailable)
		}
	}
	locks, err := s.locks(command.Actor, command.Scope, &command.Identity, true)
	if err != nil {
		return nil, nil, err
	}
	cause, _ := f.NewCommandsCause(command.Identity)
	var replay *CommandResult
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, command.Actor, command.Scope, id.Mutate); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		var prior, phase, owner string
		var body []byte
		err = x.QueryRow(ctx, `SELECT semantic_digest,phase,actor_user_id,response FROM agenteam_mcp.commands WHERE command_digest=$1`, p.commandDigest).Scan(&prior, &phase, &owner, &body)
		if err == nil {
			if prior != p.semanticDigest || owner != command.Actor.Details().UserID {
				return fail(f.IdempotencyKeyReused)
			}
			if phase != "complete" {
				return fail(f.ResourceBusy)
			}
			var result CommandResult
			if json.Unmarshal(body, &result) != nil {
				return fail(f.InvalidState)
			}
			replay = &result
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		p.config, err = loadConfig(ctx, x, input.ConfigID)
		if err != nil {
			return err
		}
		if !configVisible(p.config, command.Scope) {
			return fail(f.NotFound)
		}
		if p.config.Authentication.Scheme == "none" && input.CredentialID != "" || p.config.Authentication.Scheme != "none" && input.CredentialID == "" {
			return fail(f.InvalidArgument)
		}
		p.connection, err = scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE project_id=$1 AND config_id=$2`, command.Scope.Details().ProjectID, input.ConfigID.String()))
		var fault *f.Fault
		if errors.As(err, &fault) && fault.Code == f.NotFound {
			if command.ExpectedVersion != 0 || name != "connection.connect" {
				return fail(f.VersionConflict)
			}
			_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.connections(id,project_id,config_id,config_revision,state,credential_id,refresh_id) VALUES($1,$2,$3,$4,'connecting',$5,$6)`, connection.String(), command.Scope.Details().ProjectID, input.ConfigID.String(), int64(p.config.RuntimeRevision), null(input.CredentialID), request.String())
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if p.connection.Version != command.ExpectedVersion {
				return fail(f.VersionConflict)
			}
			if p.connection.Version == math.MaxInt64 {
				return fail(f.InvalidState)
			}
			connection = p.connection.ID
			if name != "connection.connect" && (p.connection.State != "connected" || p.connection.ConfigRevision != p.config.RuntimeRevision || p.connection.CredentialID != input.CredentialID) {
				return fail(f.InvalidState)
			}
			if name == "connection.connect" {
				if err = s.removeTools(ctx, tx, p.connection.ProjectID, connection.String()); err != nil {
					return err
				}
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connections SET version=version+1,config_revision=$2,credential_id=$3,refresh_id=$4,state=CASE WHEN $5='connection.connect' THEN 'connecting' ELSE state END,updated_at=clock_timestamp() WHERE id=$1`, connection.String(), int64(p.config.RuntimeRevision), null(input.CredentialID), request.String(), name)
			if err != nil {
				return err
			}
		}
		p.connection, err = scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1`, connection.String()))
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.commands(command_digest,semantic_digest,actor_user_id,scope_kind,project_id,command_name,phase) VALUES($1,$2,$3,'project',$4,$5,'pending')`, p.commandDigest, p.semanticDigest, command.Actor.Details().UserID, command.Scope.Details().ProjectID, name)
		if err != nil {
			return err
		}
		leaseValue, err := credentialLeaseValue(p.connection.CredentialID, p.credentialLease)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.connection_requests(id,command_digest,connection_id,project_id,config_id,config_revision,connection_version,user_id,session_id,credential_id,credential_lease_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, p.requestID, p.commandDigest, p.connection.ID.String(), p.connection.ProjectID.String(), p.config.ID.String(), int64(p.config.RuntimeRevision), int64(p.connection.Version), command.Actor.Details().UserID, command.Actor.Details().SessionID, null(p.connection.CredentialID), null(leaseValue))
		return err
	}))
	if err != nil {
		return nil, nil, err
	}
	return p, replay, nil
}

type synchronization struct {
	revision                                 string
	tools                                    []protocol.Tool
	resources                                []protocol.Resource
	templates                                []protocol.Template
	connectError, toolsError, resourcesError string
}

func safeProtocolCode(err error) string {
	if err == nil {
		return ""
	}
	var p *protocol.Error
	if errors.As(err, &p) {
		return string(p.Code)
	}
	if errors.Is(err, context.Canceled) {
		return string(protocol.Cancelled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return string(protocol.Timeout)
	}
	return "mcp_dependency_unavailable"
}
func (s *Service) synchronize(ctx context.Context, command Command, input ConnectInput, name string) (CommandResult, error) {
	p, replay, err := s.prepareConnection(ctx, command, input, name)
	if err != nil {
		return CommandResult{}, err
	}
	if replay != nil {
		return *replay, nil
	}
	return s.synchronizePrepared(ctx, p)
}

func (s *Service) synchronizePrepared(ctx context.Context, p *pendingConnection) (CommandResult, error) {
	defer s.retireConnectionRequest(p)
	var sync synchronization
	access, err := s.connectionAccess(ctx, p)
	if err != nil {
		sync.connectError = safeProtocolCode(err)
	} else {
		session, openErr := s.options.Protocol.Open(ctx, access)
		if openErr != nil {
			sync.connectError = safeProtocolCode(openErr)
		} else {
			sync.revision = session.Revision()
			sync.tools, err = session.ListTools(ctx)
			sync.toolsError = safeProtocolCode(err)
			sync.resources, err = session.ListResources(ctx)
			sync.resourcesError = safeProtocolCode(err)
			if err == nil {
				sync.templates, err = session.ListResourceTemplates(ctx)
				sync.resourcesError = safeProtocolCode(err)
			}
			if err = session.Close(); err != nil && sync.toolsError == "" && sync.resourcesError == "" {
				sync.toolsError = safeProtocolCode(err)
				sync.resourcesError = safeProtocolCode(err)
			}
		}
	}
	return s.finishConnection(ctx, p, sync)
}

type credentialWitnessKey struct{}
type credentialWitness struct {
	owner   *Service
	request CredentialRequest
	active  atomic.Bool
}

func (s *Service) connectionAccess(ctx context.Context, p *pendingConnection) (protocol.Access, error) {
	key, err := ac.NewAppendKey(ac.AccessProducer, p.requestID, 0)
	if err != nil {
		return protocol.Access{}, err
	}
	call, err := outbound.NewCallContext(p.command.Actor, p.command.Scope, key, ac.Associations{RequestID: p.requestID})
	if err != nil {
		return protocol.Access{}, err
	}
	a := protocol.Access{Endpoint: p.config.Endpoint, Authentication: p.config.Authentication, Context: call}
	if p.config.Authentication.Scheme == "none" {
		return a, nil
	}
	if p.config.Authentication.Scheme == "oauth" {
		a.Material, err = s.connectionOAuthAccess(ctx, p, call)
	} else {
		a.Material, err = s.readConnectionCredential(ctx, p)
	}
	return a, err
}

func (s *Service) finishConnection(ctx context.Context, p *pendingConnection, sync synchronization) (CommandResult, error) {
	if ctx.Err() != nil {
		return CommandResult{}, ctx.Err()
	}
	locks, err := s.locks(p.command.Actor, p.command.Scope, &p.command.Identity, true)
	if err != nil {
		return CommandResult{}, err
	}
	cause, _ := f.NewCommandsCause(p.command.Identity)
	var output CommandResult
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := s.authorize(ctx, tx, p.command.Actor, p.command.Scope, id.Mutate); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		current, err := loadConfig(ctx, x, p.config.ID)
		if err != nil {
			return err
		}
		if current.RuntimeRevision != p.config.RuntimeRevision {
			return fail(f.ConfirmationStale)
		}
		var request string
		err = x.QueryRow(ctx, `SELECT COALESCE(refresh_id,'') FROM agenteam_mcp.connections WHERE id=$1 AND project_id=$2 AND version=$3`, p.connection.ID.String(), p.connection.ProjectID.String(), int64(p.connection.Version)).Scan(&request)
		if err != nil || request != p.requestID {
			return fail(f.ConfirmationStale)
		}
		c := p.connection
		validTools, invalidTools := 0, 0
		if c.ToolsRevision == math.MaxInt64 || c.ResourcesRevision == math.MaxInt64 {
			return fail(f.InvalidState)
		}
		if sync.connectError != "" {
			state := "error"
			if sync.connectError == string(protocol.AuthenticationRequired) {
				state = "auth_required"
			}
			if p.name != "connection.connect" {
				state = c.State
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connections SET state=$2,last_error_code=$3,refresh_id=NULL,updated_at=clock_timestamp() WHERE id=$1`, c.ID.String(), state, sync.connectError)
		} else {
			binding := c.RuntimeBindingID
			if p.name == "connection.connect" || binding == "" {
				fresh, e := f.NewID[struct{}]()
				if e != nil {
					return fail(f.DependencyUnavailable)
				}
				binding = fresh.String()
				_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.runtime_bindings(id,project_id,config_id,connection_id,config_revision,endpoint,auth_scheme,header_name,credential_id,protocol_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, binding, c.ProjectID.String(), c.ConfigID.String(), c.ID.String(), int64(p.config.RuntimeRevision), p.config.Endpoint, p.config.Authentication.Scheme, p.config.Authentication.HeaderName, null(c.CredentialID), sync.revision)
				if err != nil {
					return err
				}
			}
			enabled := c.Enabled
			if p.name == "connection.connect" {
				enabled = true
			}
			if p.name == "connection.enable" {
				// A Resources-only server is independently usable. A failed Tool
				// refresh still cannot reactivate its previous registrations.
				enabled = sync.toolsError == "" || sync.resourcesError == ""
			}
			if sync.toolsError == "" {
				discovery, invalid, err := normalizeTools(ctx, tc.MCPBinding{ProjectID: c.ProjectID, ConfigID: c.ConfigID.String(), ConnectionID: c.ID.String(), RuntimeBindingID: binding, ConfigRevision: p.config.RuntimeRevision}, sync.tools, enabled)
				if err != nil {
					return err
				}
				validTools, invalidTools = len(discovery.Tools), invalid
				if _, err = s.publishTools(ctx, tx, discovery); err != nil {
					return err
				}
				raw, err := json.Marshal(sync.tools)
				if err != nil {
					return fail(f.InvalidState)
				}
				_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.tool_snapshots(connection_id,revision,protocol_revision,tool_count,invalid_count,content_sha256) VALUES($1,$2,$3,$4,$5,$6)`, c.ID.String(), int64(c.ToolsRevision+1), sync.revision, len(discovery.Tools), invalid, digest(raw))
				if err != nil {
					return err
				}
			}
			if sync.resourcesError == "" {
				if err = s.publishResources(ctx, x, c, sync.resources, sync.templates); err != nil {
					return err
				}
			}
			lastError := sync.toolsError
			if lastError == "" {
				lastError = sync.resourcesError
			}
			_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connections SET state='connected',enabled=$2,runtime_binding_id=$3,protocol_revision=$4,refresh_id=NULL,last_error_code=$5,last_connected_at=clock_timestamp(),updated_at=clock_timestamp(),tools_state=CASE WHEN $6='' THEN 'ready' ELSE 'error' END,resources_state=CASE WHEN $7='' THEN 'ready' ELSE 'error' END,tools_revision=tools_revision+CASE WHEN $6='' THEN 1 ELSE 0 END,resources_revision=resources_revision+CASE WHEN $7='' THEN 1 ELSE 0 END WHERE id=$1`, c.ID.String(), enabled, binding, sync.revision, lastError, sync.toolsError, sync.resourcesError)
		}
		if err != nil {
			return err
		}
		c, err = scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1`, c.ID.String()))
		if err != nil {
			return err
		}
		output = CommandResult{Connection: &c}
		body, err := encode(output)
		if err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_mcp.commands SET phase='complete',response=$2 WHERE command_digest=$1 AND semantic_digest=$3 AND phase='pending'`, p.commandDigest, body, p.semanticDigest); err != nil {
			return err
		}
		if _, err = x.Exec(ctx, `UPDATE agenteam_mcp.connection_requests SET phase='complete' WHERE id=$1 AND command_digest=$2`, p.requestID, p.commandDigest); err != nil {
			return err
		}
		if p.name != "connection.refresh" {
			if err = s.emit(ctx, tx, AuditEvent{Actor: p.command.Actor, Scope: p.command.Scope, Action: "mcp." + p.name, ConfigID: c.ConfigID.String(), ConnectionID: c.ID.String(), Version: c.Version, CauseID: p.commandDigest}); err != nil {
				return err
			}
		}
		code := ""
		if c.LastErrorCode != "" {
			code = auditFailure(c.LastErrorCode)
		}
		return s.emit(ctx, tx, AuditEvent{Actor: p.command.Actor, Scope: p.command.Scope, Action: string(ac.MCPDiscoveryRefresh), ConfigID: c.ConfigID.String(), ConnectionID: c.ID.String(), Version: c.Version, ToolCount: validTools, ResourceCount: len(sync.resources) + len(sync.templates), InvalidCount: invalidTools, ErrorCode: code, CauseID: p.requestID})
	}))
	if err != nil {
		return CommandResult{}, err
	}
	return output, nil
}

func normalizeTools(ctx context.Context, b tc.MCPBinding, tools []protocol.Tool, active bool) (tc.MCPDiscovery, int, error) {
	d := tc.MCPDiscovery{ProjectID: b.ProjectID, ConfigID: b.ConfigID, ConnectionID: b.ConnectionID, RuntimeBindingID: b.RuntimeBindingID, ConfigRevision: b.ConfigRevision}
	invalid := 0
	for _, remote := range tools {
		d.PresentNames = append(d.PresentNames, remote.Name)
		binding := b
		binding.RemoteToolName = remote.Name
		idem := tc.Unknown
		if remote.ReadOnly || remote.Idempotent {
			idem = tc.Inherent
		}
		destructive := true
		if remote.Destructive != nil {
			destructive = *remote.Destructive
		}
		definition := tc.Definition{StableKey: "mcp:" + b.ConfigID + ":" + remote.Name, Name: remote.Name, Description: remote.Description, InputSchema: remote.InputSchema, OutputSchema: remote.OutputSchema, Annotations: &tc.Annotations{ReadOnly: remote.ReadOnly, Destructive: destructive, Idempotency: idem}}
		if _, err := tc.CanonicalDefinition(ctx, definition); err != nil {
			invalid++
			continue
		}
		if _, err := schema.Compile(ctx, definition.InputSchema); err != nil {
			invalid++
			continue
		}
		if len(definition.OutputSchema) > 0 {
			if _, err := schema.Compile(ctx, definition.OutputSchema); err != nil {
				invalid++
				continue
			}
		}
		d.Tools = append(d.Tools, tc.MCPRegistration{Definition: definition, Binding: binding, DefaultEnabled: remote.ReadOnly, Active: active})
	}
	return d, invalid, ctx.Err()
}

func (s *Service) publishResources(ctx context.Context, x postgres.SQLExecutor, c Connection, resources []protocol.Resource, templates []protocol.Template) error {
	if _, err := x.Exec(ctx, `UPDATE agenteam_mcp.resource_catalog SET active=false WHERE connection_id=$1`, c.ID.String()); err != nil {
		return err
	}
	write := func(kind, uri, name, title, description, mime string, size *int64) error {
		resource, err := f.NewID[struct{}]()
		if err != nil {
			return fail(f.DependencyUnavailable)
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.resource_catalog(id,connection_id,project_id,kind,uri,name,title,description,mime_type,byte_size,catalog_revision,active) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,true) ON CONFLICT(connection_id,kind,uri) DO UPDATE SET name=EXCLUDED.name,title=EXCLUDED.title,description=EXCLUDED.description,mime_type=EXCLUDED.mime_type,byte_size=EXCLUDED.byte_size,catalog_revision=EXCLUDED.catalog_revision,active=true`, resource.String(), c.ID.String(), c.ProjectID.String(), kind, uri, name, title, description, mime, size, int64(c.ResourcesRevision+1))
		return err
	}
	for _, r := range resources {
		if err := write("resource", r.URI, r.Name, r.Title, r.Description, r.MIMEType, r.Size); err != nil {
			return err
		}
	}
	for _, r := range templates {
		if err := write("template", r.URITemplate, r.Name, r.Title, r.Description, r.MIMEType, nil); err != nil {
			return err
		}
	}
	return nil
}
