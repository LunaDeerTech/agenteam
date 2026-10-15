package mcp

import (
	"context"
	"errors"
	"sync/atomic"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	oc "github.com/LunaDeerTech/agenteam/internal/central/object/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
	"github.com/jackc/pgx/v5"
)

type startupUseKey struct{}

// A startup use belongs to one admitted call of the actual guarded process.
// Neither its public service Actor nor a row found by ID creates this witness.
type startupUse struct {
	service *Service
	call    *processCall
	actor   id.Actor
	request string
	process oc.ProcessID
	// stopped is set only after the actual Guard confirmed this exact former
	// process and, when present, its former retirement owner have stopped.
	stopped            bool
	previousRetirement string
	connection         Connection
	config             Config
	lease              sc.LeaseID
	locks              []f.LockRequest
	active             atomic.Bool
}

func startupLocks(project id.ProjectID, connection ConnectionID) ([]f.LockRequest, error) {
	if project.Validate() != nil || connection.Validate() != nil {
		return nil, fail(f.InvalidArgument)
	}
	p, err := f.ProjectLock(project.String())
	if err != nil {
		return nil, err
	}
	c, err := connectionLock(connection.String(), f.Exclusive)
	if err != nil {
		return nil, err
	}
	return mergeLocks(nil, []f.LockRequest{{Key: p, Mode: f.Shared}, catalogLock(f.Exclusive), registryLock(f.Exclusive), c})
}

func (s *Service) startupUse(ctx context.Context) (*startupUse, error) {
	if s == nil || s.owner == nil || ctx == nil {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(startupUseKey{}).(*startupUse)
	if !ok || w == nil || w.service != s || w.call == nil || w.call.owner != s.owner || !w.active.Load() || w.actor.Validate() != nil || w.actor.Details().ServiceName != id.MCPRuntime || w.actor.Details().CauseRef != w.request || w.actor.Details().ProjectID != w.connection.ProjectID.String() {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

type startupFact struct {
	process, project, connection, config, runtime, credential, lease, retirement string
	revision, version                                                            f.Version
	phase                                                                        string
	started, retired, released                                                   bool
}

func loadStartupFact(ctx context.Context, x postgres.SQLExecutor, request string) (startupFact, error) {
	var r startupFact
	err := x.QueryRow(ctx, `SELECT process_id::text,project_id,connection_id,config_id,config_revision,connection_version,COALESCE(runtime_binding_id,''),COALESCE(credential_id,''),COALESCE(credential_lease_id,''),phase,io_started,io_retired,lease_released,COALESCE(retirement_process_id::text,'') FROM agenteam_mcp.startup_requests WHERE id=$1`, request).Scan(&r.process, &r.project, &r.connection, &r.config, &r.revision, &r.version, &r.runtime, &r.credential, &r.lease, &r.phase, &r.started, &r.retired, &r.released, &r.retirement)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, fail(f.NotFound)
	}
	return r, safeError(err)
}

func (w *startupUse) matches(r startupFact) bool {
	lease, err := credentialLeaseValue(w.connection.CredentialID, w.lease)
	return err == nil && w.process.Validate() == nil && r.process == w.process.String() && r.project == w.connection.ProjectID.String() && r.connection == w.connection.ID.String() && r.config == w.config.ID.String() && r.revision == w.config.RuntimeRevision && r.version == w.connection.Version && r.runtime == w.connection.RuntimeBindingID && r.credential == w.connection.CredentialID && r.lease == lease
}

func (s *Service) requireStartupInTx(ctx context.Context, tx f.Tx, current bool) (*startupUse, startupFact, error) {
	w, err := s.startupUse(ctx)
	if err != nil {
		return nil, startupFact{}, err
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return nil, startupFact{}, safeError(err)
	}
	if err = s.owner.requireInTx(ctx, tx, w.call); err != nil {
		return nil, startupFact{}, err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return nil, startupFact{}, safeError(err)
	}
	r, err := loadStartupFact(ctx, x, w.request)
	if err != nil {
		return nil, r, err
	}
	if !w.matches(r) {
		return nil, r, fail(f.Forbidden)
	}
	if w.process != s.owner.process && (!w.stopped || current || r.retirement != s.owner.process.String()) {
		return nil, r, fail(f.Forbidden)
	}
	if current {
		if r.phase != "pending" {
			return nil, r, fail(f.Forbidden)
		}
		var present bool
		err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connections c JOIN agenteam_mcp.configs f ON f.id=c.config_id WHERE c.id=$1 AND c.project_id=$2 AND c.config_id=$3 AND c.version=$4 AND c.config_revision=$5 AND f.runtime_revision=$5 AND c.refresh_id=$6 AND c.enabled AND COALESCE(c.credential_id,'')=$7 AND COALESCE(c.runtime_binding_id,'')=$8)`, r.connection, r.project, r.config, int64(r.version), int64(r.revision), w.request, r.credential, r.runtime).Scan(&present)
		if err != nil {
			return nil, r, safeError(err)
		}
		if !present {
			return nil, r, fail(f.ConfirmationStale)
		}
	}
	return w, r, ctx.Err()
}

func (s *Service) RequireMCPRuntimeInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID) error {
	w, _, err := s.requireStartupInTx(ctx, tx, true)
	if err != nil {
		return err
	}
	if !actor.Equal(w.actor) || project != w.connection.ProjectID {
		return fail(f.Forbidden)
	}
	return ctx.Err()
}

func (a *Authority) RequireMCPRuntimeInTx(ctx context.Context, tx f.Tx, actor id.Actor, project id.ProjectID) error {
	s, err := a.service()
	if err != nil {
		return err
	}
	return s.RequireMCPRuntimeInTx(ctx, tx, actor, project)
}

func (s *Service) requireStartupProject(ctx context.Context, tx f.Tx, w *startupUse) error {
	projects, ok := s.options.Projects.(pc.MCPRuntimeProjectAuthority)
	if !ok || nilPort(projects) {
		return fail(f.DependencyUnbound)
	}
	return safeError(projects.RequireMCPRuntimeProjectInTx(ctx, tx, w.actor, w.connection.ProjectID))
}

var _ pc.MCPRuntimeProof = (*Authority)(nil)
var _ pc.MCPRuntimeProof = (*Service)(nil)
