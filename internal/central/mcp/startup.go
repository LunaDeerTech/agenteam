package mcp

import (
	"context"
	"errors"
	"math"
	"time"

	ac "github.com/LunaDeerTech/agenteam/internal/central/audit/contract"
	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/mcp/protocol"
	"github.com/LunaDeerTech/agenteam/internal/central/outbound"
	pc "github.com/LunaDeerTech/agenteam/internal/central/project/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

// StartReconciliation admits the original worker before starting its goroutine.
// The existing Process owner joins it and every retained connection request.
// A failed connection is diagnosed independently and never blocks another one.
func (s *Service) StartReconciliation(ctx context.Context) error {
	if s == nil || s.owner == nil || nilPort(s.options.Registry) || s.options.Protocol == nil {
		return fail(f.DependencyUnbound)
	}
	if projects, ok := s.options.Projects.(pc.MCPRuntimeProjectAuthority); !ok || nilPort(projects) {
		return fail(f.DependencyUnbound)
	}
	ctx, call, err := s.owner.begin(ctx)
	if err != nil {
		return err
	}
	go func() {
		defer call.returnedWith(nil)
		_ = s.recoverOAuthRequests(ctx)
		_ = s.recoverOAuthRefreshes(ctx)
		_ = s.recoverStartupRequests(ctx)
		_ = s.reconcileEnabled(ctx)
	}()
	return nil
}

func (s *Service) reconcileEnabled(ctx context.Context) error {
	// Keyset batches bound memory and do not hold a transaction during network
	// work. Every candidate is loaded and authorized again under its own locks.
	after := ""
	for {
		rows, err := s.store.Query(ctx, `SELECT id,project_id FROM agenteam_mcp.connections WHERE enabled AND id>$1 ORDER BY id LIMIT 64`, after)
		if err != nil {
			return safeError(err)
		}
		type candidate struct {
			connection ConnectionID
			project    id.ProjectID
		}
		var batch []candidate
		for rows.Next() {
			var rawConnection, rawProject string
			if err = rows.Scan(&rawConnection, &rawProject); err != nil {
				break
			}
			connection, e := f.ParseID[ConnectionIdentity](rawConnection)
			project, p := f.ParseID[id.Project](rawProject)
			if e != nil || p != nil || len(batch) == 64 {
				err = fail(f.InvalidState)
				break
			}
			batch = append(batch, candidate{connection, project})
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return safeError(err)
		}
		if len(batch) == 0 {
			return ctx.Err()
		}
		for _, candidate := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			_ = s.reconcileConnection(ctx, candidate.project, candidate.connection)
			after = candidate.connection.String()
		}
	}
}

func (s *Service) prepareStartup(ctx context.Context, w *startupUse) error {
	cause, _ := f.NewRecoveryCause("mcp.startup", w.request, "")
	return commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		if err := s.owner.requireInTx(ctx, tx, w.call); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		var prior bool
		if err = x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.startup_requests WHERE process_id=$1 AND connection_id=$2)`, s.owner.process.String(), w.connection.ID.String()).Scan(&prior); err != nil {
			return err
		}
		if prior {
			return fail(f.ResourceBusy)
		}
		c, err := scanConnection(x.QueryRow(ctx, `SELECT `+connectionColumns+` FROM agenteam_mcp.connections WHERE id=$1 AND project_id=$2`, w.connection.ID.String(), w.connection.ProjectID.String()))
		if err != nil {
			return err
		}
		config, err := loadConfig(ctx, x, c.ConfigID)
		if err != nil {
			return err
		}
		if !c.Enabled || c.Version == math.MaxInt64 || c.ConfigRevision != config.RuntimeRevision {
			return fail(f.ConfirmationStale)
		}
		var pending bool
		if err = x.QueryRow(ctx, `SELECT refresh_id IS NOT NULL FROM agenteam_mcp.connections WHERE id=$1`, c.ID.String()).Scan(&pending); err != nil {
			return err
		}
		if pending {
			return fail(f.ResourceBusy)
		}
		if config.Authentication.Scheme == "none" && c.CredentialID != "" || config.Authentication.Scheme != "none" && c.CredentialID == "" {
			return fail(f.InvalidState)
		}
		if c.CredentialID != "" {
			w.lease, err = f.NewID[sc.Lease]()
			if err != nil {
				return fail(f.DependencyUnavailable)
			}
		}
		c.Version++
		w.config, w.connection = config, c
		lease, err := credentialLeaseValue(c.CredentialID, w.lease)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `INSERT INTO agenteam_mcp.startup_requests(id,process_id,project_id,connection_id,config_id,config_revision,connection_version,runtime_binding_id,credential_id,credential_lease_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, w.request, s.owner.process.String(), c.ProjectID.String(), c.ID.String(), config.ID.String(), int64(config.RuntimeRevision), int64(c.Version), null(c.RuntimeBindingID), null(c.CredentialID), null(lease))
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connections SET version=$2,refresh_id=$3,updated_at=clock_timestamp() WHERE id=$1`, c.ID.String(), int64(c.Version), w.request)
		if err != nil {
			return err
		}
		return s.requireStartupProject(ctx, tx, w)
	}))
}

func (s *Service) reconcileConnection(parent context.Context, project id.ProjectID, connection ConnectionID) (result error) {
	ctx, call, err := s.owner.begin(parent)
	if err != nil {
		return err
	}
	request, err := f.NewID[f.Request]()
	if err != nil {
		call.returnedWith(nil)
		return fail(f.DependencyUnavailable)
	}
	scope, _ := id.InProject(project)
	registration, _ := id.RegisterService(id.MCPRuntime)
	actor, err := registration.Actor(request.String(), scope)
	if err != nil {
		call.returnedWith(nil)
		return err
	}
	locks, err := startupLocks(project, connection)
	if err != nil {
		call.returnedWith(nil)
		return err
	}
	w := &startupUse{service: s, call: call, actor: actor, request: request.String(), process: s.owner.process, connection: Connection{ID: connection, ProjectID: project}, locks: locks}
	w.active.Store(true)
	ctx = context.WithValue(ctx, startupUseKey{}, w)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		confirmed, err := s.retireStartup(cleanup, w)
		if err == nil && confirmed {
			w.active.Store(false)
			call.returnedWith(nil)
			return
		}
		call.returnedWith(func(observe context.Context) (bool, error) {
			confirmed, err := s.retireStartup(context.WithValue(observe, startupUseKey{}, w), w)
			if err == nil && confirmed {
				w.active.Store(false)
			}
			return confirmed, err
		})
	}()
	if err = s.prepareStartup(ctx, w); err != nil {
		return err
	}
	var sync synchronization
	access, err := s.startupAccess(ctx, w)
	if err != nil {
		sync.connectError = safeProtocolCode(err)
	} else {
		session, err := s.options.Protocol.Open(ctx, access)
		if err != nil {
			sync.connectError = safeProtocolCode(err)
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
			if err = session.Close(); err != nil {
				if sync.toolsError == "" {
					sync.toolsError = safeProtocolCode(err)
				}
				if sync.resourcesError == "" {
					sync.resourcesError = safeProtocolCode(err)
				}
			}
		}
	}
	return s.finishStartup(ctx, w, sync)
}

func (s *Service) startupAccess(ctx context.Context, w *startupUse) (protocol.Access, error) {
	key, _ := ac.NewAppendKey(ac.AccessProducer, w.request, 0)
	scope, _ := id.InProject(w.connection.ProjectID)
	registration, _ := id.RegisterService(id.OutboundService)
	actor, err := registration.Actor(w.request, scope)
	if err != nil {
		return protocol.Access{}, err
	}
	call, err := outbound.NewCallContext(actor, scope, key, ac.Associations{RequestID: w.request})
	if err != nil {
		return protocol.Access{}, err
	}
	access := protocol.Access{Endpoint: w.config.Endpoint, Authentication: w.config.Authentication, Context: call}
	if w.config.Authentication.Scheme != "none" && w.config.Authentication.Scheme != "oauth" {
		access.Material, err = s.readStartupCredential(ctx, w)
		if err != nil {
			return protocol.Access{}, err
		}
	}
	cause, _ := f.NewRecoveryCause("mcp.startup.dispatch", w.request, "")
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		_, fact, err := s.requireStartupInTx(ctx, tx, true)
		if err != nil {
			return err
		}
		if fact.started || fact.retired {
			return fail(f.Forbidden)
		}
		if err = s.requireStartupProject(ctx, tx, w); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.startup_requests SET io_started=true WHERE id=$1`, w.request)
		return err
	}))
	if err != nil {
		access.Material.Destroy()
		return protocol.Access{}, err
	}
	if w.config.Authentication.Scheme == "oauth" {
		access.Material, err = s.startupOAuthAccess(ctx, w, call)
		if err != nil {
			return protocol.Access{}, err
		}
	}
	return access, nil
}

// After the actual protocol/session/body returned, only original retirement is
// repeated. Unknown dispatch or publish never causes another network request.
func (s *Service) retireStartup(ctx context.Context, w *startupUse) (bool, error) {
	if w.config.ID.Validate() != nil {
		return true, nil
	}
	cause, _ := f.NewRecoveryCause("mcp.startup.retire", w.request, "")
	var missing, released bool
	err := commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		if err := s.owner.requireInTx(ctx, tx, w.call); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		fact, err := loadStartupFact(ctx, x, w.request)
		var fault *f.Fault
		if errors.As(err, &fault) && fault.Code == f.NotFound {
			missing = true
			return nil
		}
		if err != nil {
			return err
		}
		if !w.matches(fact) {
			return fail(f.Forbidden)
		}
		if w.process != s.owner.process && (!w.stopped || fact.retirement != w.previousRetirement && fact.retirement != s.owner.process.String()) {
			return fail(f.Forbidden)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.startup_requests SET io_retired=true,retirement_process_id=$2,retired_at=COALESCE(retired_at,clock_timestamp()),phase=CASE WHEN phase='pending' THEN 'abandoned' ELSE phase END,completed_at=COALESCE(completed_at,clock_timestamp()),lease_released=lease_released OR credential_lease_id IS NULL WHERE id=$1`, w.request, s.owner.process.String())
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connections SET refresh_id=NULL,last_error_code='mcp_cancelled',updated_at=clock_timestamp() WHERE id=$1 AND refresh_id=$2`, w.connection.ID.String(), w.request)
		released = fact.released || fact.lease == ""
		return err
	}))
	if err != nil {
		return false, err
	}
	if missing || released {
		return true, nil
	}
	err = s.applyStartupSecret(ctx, w, sc.ReleaseLeaseUsage)
	var fault *f.Fault
	if err != nil && (!errors.As(err, &fault) || fault.Code != f.NotFound) {
		return false, err
	}
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, w.locks); err != nil {
			return err
		}
		_, fact, err := s.requireStartupInTx(ctx, tx, false)
		if err != nil {
			return err
		}
		if !fact.retired {
			return fail(f.Forbidden)
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.startup_requests SET lease_released=true WHERE id=$1`, w.request)
		return err
	}))
	return err == nil, err
}
