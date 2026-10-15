package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	"github.com/jackc/pgx/v5"
)

const configColumns = `id,scope_kind,COALESCE(project_id,''),name,description,endpoint,auth_scheme,header_name,version,runtime_revision`

func scanConfig(row interface{ Scan(...any) error }) (Config, error) {
	var c Config
	var raw string
	err := row.Scan(&raw, &c.ScopeKind, &c.ProjectID, &c.Name, &c.Description, &c.Endpoint, &c.Authentication.Scheme, &c.Authentication.HeaderName, &c.Version, &c.RuntimeRevision)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, fail(f.NotFound)
	}
	if err != nil {
		return c, safeError(err)
	}
	c.ID, err = f.ParseID[ConfigIdentity](raw)
	if err != nil || c.Version.Validate() != nil || c.RuntimeRevision.Validate() != nil || (ConfigInput{c.Name, c.Description, c.Endpoint, c.Authentication}).validate() != nil {
		return Config{}, fail(f.InvalidState)
	}
	return c, nil
}
func loadConfig(ctx context.Context, x postgres.SQLExecutor, config ConfigID) (Config, error) {
	return scanConfig(x.QueryRow(ctx, `SELECT `+configColumns+` FROM agenteam_mcp.configs WHERE id=$1`, config.String()))
}
func configVisible(c Config, scope id.Scope) bool {
	return c.ScopeKind == "system" || scope.Details().Kind == id.ProjectScope && c.ProjectID == scope.Details().ProjectID
}
func configOwned(c Config, scope id.Scope) bool {
	return c.ScopeKind == string(scope.Details().Kind) && c.ProjectID == scope.Details().ProjectID
}

func (s *Service) ListConfigs(ctx context.Context, actor id.Actor, scope id.Scope) ([]Config, error) {
	var result []Config
	err := s.read(ctx, actor, scope, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		rows, err := x.Query(ctx, `SELECT `+configColumns+` FROM agenteam_mcp.configs WHERE scope_kind='system' OR project_id=$1 ORDER BY name COLLATE "C",id`, null(scope.Details().ProjectID))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			c, err := scanConfig(rows)
			if err != nil {
				return err
			}
			result = append(result, c)
			if len(result) > 512 {
				return fail(f.PayloadTooLarge)
			}
		}
		return rows.Err()
	})
	return result, err
}
func (s *Service) GetConfig(ctx context.Context, actor id.Actor, scope id.Scope, config ConfigID) (Config, error) {
	if config.Validate() != nil {
		return Config{}, fail(f.InvalidArgument)
	}
	var result Config
	err := s.read(ctx, actor, scope, func(ctx context.Context, _ f.Tx, x postgres.SQLExecutor) error {
		c, err := loadConfig(ctx, x, config)
		if err != nil {
			return err
		}
		if !configVisible(c, scope) {
			return fail(f.NotFound)
		}
		result = c
		return nil
	})
	return result, err
}

func (s *Service) write(ctx context.Context, c Command, name string, semantic any, apply func(context.Context, f.Tx, postgres.SQLExecutor) (CommandResult, error)) (CommandResult, error) {
	if ctx == nil || validateCommand(c, name) != nil {
		return CommandResult{}, fail(f.InvalidArgument)
	}
	raw, err := encode(semantic)
	if err != nil {
		return CommandResult{}, err
	}
	semanticDigest := digest(raw)
	commandDigest := digest([]byte(c.Identity.Canonical()))
	locks, err := s.locks(c.Actor, c.Scope, &c.Identity, true)
	if err != nil {
		return CommandResult{}, err
	}
	cause, _ := f.NewCommandsCause(c.Identity)
	var output CommandResult
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
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
		var prior, owner, scopeKind, projectID, phase string
		var body []byte
		err = x.QueryRow(ctx, `SELECT semantic_digest,actor_user_id,scope_kind,COALESCE(project_id,''),phase,response FROM agenteam_mcp.commands WHERE command_digest=$1`, commandDigest).Scan(&prior, &owner, &scopeKind, &projectID, &phase, &body)
		if err == nil {
			if prior != semanticDigest || owner != c.Actor.Details().UserID || scopeKind != string(c.Scope.Details().Kind) || projectID != c.Scope.Details().ProjectID {
				return fail(f.IdempotencyKeyReused)
			}
			if phase != "complete" {
				return fail(f.ResourceBusy)
			}
			if len(body) > 128<<10 || json.Unmarshal(body, &output) != nil {
				return fail(f.InvalidState)
			}
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		output, err = apply(ctx, tx, x)
		if err != nil {
			return err
		}
		body, err = encode(output)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.commands(command_digest,semantic_digest,actor_user_id,scope_kind,project_id,command_name,phase,response) VALUES($1,$2,$3,$4,$5,$6,'complete',$7)`, commandDigest, semanticDigest, c.Actor.Details().UserID, string(c.Scope.Details().Kind), null(c.Scope.Details().ProjectID), name, body)
		return err
	})
	if err = commit(result); err != nil {
		return CommandResult{}, err
	}
	return output, nil
}

func (s *Service) CreateConfig(ctx context.Context, command Command, input ConfigInput) (CommandResult, error) {
	if input.validate() != nil || command.ExpectedVersion != 0 {
		return CommandResult{}, fail(f.InvalidArgument)
	}
	config, err := f.NewID[ConfigIdentity]()
	if err != nil {
		return CommandResult{}, fail(f.DependencyUnavailable)
	}
	return s.write(ctx, command, "config.create", input, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) (CommandResult, error) {
		_, err := x.Exec(ctx, `INSERT INTO agenteam_mcp.configs(id,scope_kind,project_id,name,description,endpoint,auth_scheme,header_name) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, config.String(), string(command.Scope.Details().Kind), null(command.Scope.Details().ProjectID), input.Name, input.Description, input.Endpoint, input.Authentication.Scheme, input.Authentication.HeaderName)
		if err != nil {
			return CommandResult{}, err
		}
		c, err := loadConfig(ctx, x, config)
		if err != nil {
			return CommandResult{}, err
		}
		err = s.emit(ctx, tx, AuditEvent{Actor: command.Actor, Scope: command.Scope, Action: "mcp.config.create", ConfigID: config.String(), Version: c.Version, CauseID: digest([]byte(command.Identity.Canonical()))})
		return CommandResult{Config: &c}, err
	})
}
func (s *Service) UpdateConfig(ctx context.Context, command Command, config ConfigID, input ConfigInput) (CommandResult, error) {
	if config.Validate() != nil || input.validate() != nil || command.ExpectedVersion.Validate() != nil {
		return CommandResult{}, fail(f.InvalidArgument)
	}
	semantic := struct {
		Config  ConfigID
		Version f.Version
		Input   ConfigInput
	}{config, command.ExpectedVersion, input}
	return s.write(ctx, command, "config.update", semantic, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) (CommandResult, error) {
		c, err := loadConfig(ctx, x, config)
		if err != nil {
			return CommandResult{}, err
		}
		if !configOwned(c, command.Scope) {
			return CommandResult{}, fail(f.NotFound)
		}
		if c.Version != command.ExpectedVersion {
			return CommandResult{}, fail(f.VersionConflict)
		}
		if c.Version == math.MaxInt64 || c.RuntimeRevision == math.MaxInt64 {
			return CommandResult{}, fail(f.InvalidState)
		}
		runtimeRevision := c.RuntimeRevision
		if c.Endpoint != input.Endpoint || c.Authentication != input.Authentication {
			runtimeRevision++
			if err := s.removeRegistrations(ctx, tx, x, config); err != nil {
				return CommandResult{}, err
			}
			if _, err = x.Exec(ctx, `UPDATE agenteam_mcp.connections SET state='needs_reconnect',version=version+1,refresh_id=NULL,updated_at=clock_timestamp() WHERE config_id=$1`, config.String()); err != nil {
				return CommandResult{}, err
			}
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.configs SET name=$2,description=$3,endpoint=$4,auth_scheme=$5,header_name=$6,version=version+1,runtime_revision=$7,updated_at=clock_timestamp() WHERE id=$1`, config.String(), input.Name, input.Description, input.Endpoint, input.Authentication.Scheme, input.Authentication.HeaderName, int64(runtimeRevision))
		if err != nil {
			return CommandResult{}, err
		}
		c, err = loadConfig(ctx, x, config)
		if err != nil {
			return CommandResult{}, err
		}
		err = s.emit(ctx, tx, AuditEvent{Actor: command.Actor, Scope: command.Scope, Action: "mcp.config.update", ConfigID: config.String(), Version: c.Version, CauseID: digest([]byte(command.Identity.Canonical()))})
		return CommandResult{Config: &c}, err
	})
}
func (s *Service) DeleteConfig(ctx context.Context, command Command, config ConfigID) (CommandResult, error) {
	if config.Validate() != nil || command.ExpectedVersion.Validate() != nil {
		return CommandResult{}, fail(f.InvalidArgument)
	}
	return s.write(ctx, command, "config.delete", struct {
		Config  ConfigID
		Version f.Version
	}{config, command.ExpectedVersion}, func(ctx context.Context, tx f.Tx, x postgres.SQLExecutor) (CommandResult, error) {
		c, err := loadConfig(ctx, x, config)
		if err != nil {
			return CommandResult{}, err
		}
		if !configOwned(c, command.Scope) {
			return CommandResult{}, fail(f.NotFound)
		}
		if c.Version != command.ExpectedVersion {
			return CommandResult{}, fail(f.VersionConflict)
		}
		if err = s.removeRegistrations(ctx, tx, x, config); err != nil {
			return CommandResult{}, err
		}
		if _, err = x.Exec(ctx, `DELETE FROM agenteam_mcp.configs WHERE id=$1`, config.String()); err != nil {
			return CommandResult{}, err
		}
		err = s.emit(ctx, tx, AuditEvent{Actor: command.Actor, Scope: command.Scope, Action: "mcp.config.delete", ConfigID: config.String(), Version: c.Version, CauseID: digest([]byte(command.Identity.Canonical()))})
		return CommandResult{Deleted: true}, err
	})
}
func (s *Service) removeRegistrations(ctx context.Context, tx f.Tx, x postgres.SQLExecutor, config ConfigID) error {
	rows, err := x.Query(ctx, `SELECT id,project_id FROM agenteam_mcp.connections WHERE config_id=$1 ORDER BY id`, config.String())
	if err != nil {
		return err
	}
	type pair struct{ connection, project string }
	var all []pair
	for rows.Next() {
		var p pair
		if err = rows.Scan(&p.connection, &p.project); err != nil {
			rows.Close()
			return err
		}
		all = append(all, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(all) > 0 && nilPort(s.options.Registry) {
		return fail(f.DependencyUnbound)
	}
	for _, p := range all {
		project, err := f.ParseID[id.Project](p.project)
		if err != nil {
			return fail(f.InvalidState)
		}
		if err = s.removeTools(ctx, tx, project, p.connection); err != nil {
			return err
		}
	}
	return nil
}

type auditWitnessKey struct{}
type auditWitness struct {
	owner  *Service
	tx     f.Tx
	event  AuditEvent
	active atomic.Bool
}

func (s *Service) emit(ctx context.Context, tx f.Tx, event AuditEvent) error {
	w := &auditWitness{owner: s, tx: tx, event: event}
	w.active.Store(true)
	defer w.active.Store(false)
	return s.options.Audit.AppendMCPInTx(context.WithValue(ctx, auditWitnessKey{}, w), tx, event)
}

// RequireMCPAuditInTx is consumed by the typed Audit adapter. It cannot be
// satisfied by constructing a matching DTO outside the original mutation.
func (s *Service) requireAuditWitness(ctx context.Context, tx f.Tx, event AuditEvent) error {
	if ctx == nil || s == nil {
		return fail(f.Forbidden)
	}
	w, ok := ctx.Value(auditWitnessKey{}).(*auditWitness)
	if !ok || w == nil || !w.active.Load() || w.owner != s || w.tx != tx || !w.event.Actor.Equal(event.Actor) || !w.event.Scope.Equal(event.Scope) {
		return fail(f.Forbidden)
	}
	a, b := w.event, event
	a.Actor = id.Actor{}
	b.Actor = id.Actor{}
	a.Scope = id.Scope{}
	b.Scope = id.Scope{}
	if a.Action != b.Action || a.ConfigID != b.ConfigID || a.ConnectionID != b.ConnectionID || a.Version != b.Version || a.ToolCount != b.ToolCount || a.ResourceCount != b.ResourceCount || a.InvalidCount != b.InvalidCount || a.ErrorCode != b.ErrorCode || a.CauseID != b.CauseID {
		return fail(f.Forbidden)
	}
	if _, err := s.store.InTx(tx); err != nil {
		return safeError(err)
	}
	return safeError(s.store.RequireHeldLocks(ctx, tx, []f.LockRequest{catalogLock(f.Exclusive)}))
}
