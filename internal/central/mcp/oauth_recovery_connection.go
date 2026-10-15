package mcp

import (
	"context"
	"math"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

type oauthConnectionRecoveryFact struct {
	request, command, project, config, connection, user, session, credential, lease, phase string
	revision, version                                                                      f.Version
	retired, released                                                                      bool
}

func (v oauthConnectionRecoveryFact) matches(source oauthRecoveryFact) bool {
	if source.operation != "exchange" || source.phase != "complete" || source.resultCredential == "" ||
		source.resultVersion.Validate() != nil || source.version == f.Version(math.MaxInt64) ||
		v.command != source.command || v.project != source.project || v.config != source.config || v.revision != source.revision ||
		v.connection != source.connection || v.version != source.version+1 || v.user != source.user || v.session != source.session ||
		v.credential != source.resultCredential || v.released && !v.retired {
		return false
	}
	for _, raw := range []string{v.request, v.credential, v.lease} {
		if _, err := f.ParseID[f.Request](raw); err != nil {
			return false
		}
	}
	return v.phase == "pending" || v.phase == "complete" || v.phase == "abandoned"
}

func (v oauthConnectionRecoveryFact) sameOrigin(other oauthConnectionRecoveryFact) bool {
	return v.request == other.request && v.command == other.command && v.project == other.project && v.config == other.config &&
		v.connection == other.connection && v.user == other.user && v.session == other.session && v.credential == other.credential &&
		v.lease == other.lease && v.revision == other.revision && v.version == other.version
}

// publishOAuthToken creates this connection request and the complete exchange
// postimage in the same transaction, before its original process can open the
// SDK session. A matching command alone is insufficient: the unique complete
// original publication tuple and its +1 connection version are all required.
func loadOAuthRecoveryConnection(ctx context.Context, x postgres.SQLExecutor, source oauthRecoveryFact) (oauthConnectionRecoveryFact, error) {
	var found oauthConnectionRecoveryFact
	rows, err := x.Query(ctx, `SELECT r.id,r.command_digest,r.project_id,r.config_id,r.config_revision,r.connection_id,
 r.connection_version,r.user_id,r.session_id,COALESCE(r.credential_id,''),COALESCE(r.credential_lease_id,''),
 r.phase,r.io_retired,r.lease_released
 FROM agenteam_mcp.connection_requests r JOIN agenteam_mcp.commands c ON c.command_digest=r.command_digest
 WHERE r.command_digest=$1 AND c.command_name='oauth.complete' AND c.scope_kind='project'
 AND c.project_id=r.project_id AND c.actor_user_id=r.user_id ORDER BY r.id LIMIT 2`, source.command)
	if err != nil {
		return found, safeError(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if err = rows.Scan(&found.request, &found.command, &found.project, &found.config, &found.revision, &found.connection,
			&found.version, &found.user, &found.session, &found.credential, &found.lease, &found.phase, &found.retired, &found.released); err != nil {
			return oauthConnectionRecoveryFact{}, safeError(err)
		}
	}
	if err = rows.Err(); err != nil {
		return oauthConnectionRecoveryFact{}, safeError(err)
	}
	if count != 1 || !found.matches(source) {
		return oauthConnectionRecoveryFact{}, fail(f.InvalidState)
	}
	return found, nil
}

type oauthConnectionRecoveryKey struct{}
type oauthConnectionRecoveryUse struct {
	mu        sync.Mutex
	recovery  *oauthRecoveryUse
	original  oauthConnectionRecoveryFact
	request   sc.UsageRequest
	mapping   f.Digest
	active    bool
	validated f.Tx
}

func (w *oauthConnectionRecoveryUse) close() { w.mu.Lock(); w.active = false; w.mu.Unlock() }

func (s *Service) oauthConnectionRecoveryUse(ctx context.Context, r sc.UsageRequest) (*oauthConnectionRecoveryUse, error) {
	if s == nil || ctx == nil || r.Validate() != nil || r.Purpose != sc.MCP || r.Action != sc.ReleaseLeaseUsage {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(oauthConnectionRecoveryKey{}).(*oauthConnectionRecoveryUse)
	if !ok || w == nil || w.recovery == nil || w.recovery.service != s {
		return nil, fail(f.Forbidden)
	}
	original, err := s.oauthRecoveryUse(ctx)
	if err != nil || original != w.recovery {
		return nil, fail(f.Forbidden)
	}
	w.mu.Lock()
	active := w.active
	w.mu.Unlock()
	got, err := sc.UsageBinding(r)
	want, other := sc.UsageBinding(w.request)
	if !active || err != nil || other != nil || got != want {
		return nil, fail(f.Forbidden)
	}
	return w, nil
}

func (s *Service) discoverOAuthConnectionRecovery(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := s.oauthConnectionRecoveryUse(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, _ := sc.UsageBinding(r)
	return sc.NewUsageDependencies(s.secretIssuer, binding, w.mapping, w.recovery.locks)
}

func (s *Service) validateOAuthConnectionRecovery(ctx context.Context, tx f.Tx, r sc.UsageRequest, p sc.UsageDependencies) error {
	w, err := s.oauthConnectionRecoveryUse(ctx, r)
	if err != nil {
		return err
	}
	binding, _ := sc.UsageBinding(r)
	if !p.Matches(s.secretIssuer, binding, w.mapping) {
		return fail(f.Forbidden)
	}
	source, err := w.recovery.require(ctx, tx)
	if err != nil {
		return err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return safeError(err)
	}
	current, err := loadOAuthRecoveryConnection(ctx, x, source)
	if err != nil {
		return err
	}
	if !current.retired || !w.original.sameOrigin(current) {
		return fail(f.Forbidden)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.active {
		return fail(f.Forbidden)
	}
	w.validated = tx
	return ctx.Err()
}

func (s *Service) authorizeOAuthConnectionRecovery(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	w, ok := ctx.Value(oauthConnectionRecoveryKey{}).(*oauthConnectionRecoveryUse)
	if !ok || w == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	if _, err := s.oauthConnectionRecoveryUse(ctx, w.request); err != nil {
		return sc.UseGrant{}, err
	}
	w.mu.Lock()
	valid := tx.Valid() && w.active && w.validated == tx
	w.mu.Unlock()
	if !valid || action != sc.ReleaseLease || !actor.Equal(w.request.Actor) || !ref.Equal(w.request.Ref) || !owner.Equal(w.request.LeaseOwner) {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	return sc.UseGrant{Subject: actor, Consumer: sc.MCP}, nil
}

func (s *Service) releaseStoppedOAuthConnection(ctx context.Context, recovery *oauthRecoveryUse) error {
	var original oauthConnectionRecoveryFact
	needed := false
	cause, _ := f.NewRecoveryCause("mcp.oauth.connection_io", recovery.original.request, "")
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, recovery.locks); err != nil {
			return err
		}
		source, err := recovery.require(ctx, tx)
		if err != nil {
			return err
		}
		if source.operation != "exchange" || source.phase != "complete" {
			return nil
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		original, err = loadOAuthRecoveryConnection(ctx, x, source)
		if err != nil {
			return err
		}
		needed = !original.released
		if !needed {
			return clearStoppedOAuthConnection(ctx, x, original)
		}
		_, err = x.Exec(ctx, `UPDATE agenteam_mcp.connection_requests SET io_retired=true,retired_at=COALESCE(retired_at,clock_timestamp()),
 phase=CASE WHEN phase='pending' THEN 'abandoned' ELSE phase END WHERE id=$1 AND command_digest=$2`, original.request, original.command)
		return err
	})
	if err := commit(result); err != nil || !needed {
		return err
	}
	observer, ok := s.options.Secret.(sc.MCPRequestLeaseObservation)
	if !ok || nilPort(observer) {
		return fail(f.DependencyUnbound)
	}
	planner, ok := s.options.Secret.(sc.MCPRequestLeaseObservationPlanner)
	if !ok || nilPort(planner) {
		return fail(f.DependencyUnbound)
	}
	project, _ := f.ParseID[id.Project](original.project)
	scope, _ := id.InProject(project)
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(original.request, scope)
	if err != nil {
		return err
	}
	credential, _ := f.ParseID[sc.Credential](original.credential)
	ref, _ := sc.NewCredentialRef(credential, scope)
	lease, _ := f.ParseID[sc.Lease](original.lease)
	owner, _ := sc.NewCredentialLeaseOwner(sc.MCPConnectionRequestOwner, original.request)
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, Action: sc.ReleaseLeaseUsage, LeaseID: lease, LeaseOwner: owner}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return err
	}
	w := &oauthConnectionRecoveryUse{recovery: recovery, original: original, request: r, active: true,
		mapping: f.Digest(digest([]byte(string(binding) + ":" + recovery.claim + ":" + recovery.original.request)))}
	defer w.close()
	ctx = context.WithValue(ctx, oauthConnectionRecoveryKey{}, w)
	plan, err := planner.DiscoverMCPRequestLeaseObservation(ctx, r)
	if err != nil {
		return safeError(err)
	}
	cause, _ = f.NewRecoveryCause("mcp.oauth.connection_lease", original.request, "")
	return commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, plan.RequiredLocks()); err != nil {
			return err
		}
		state, err := observer.ObserveMCPRequestLeaseInTx(ctx, tx, r, plan)
		if err != nil {
			return safeError(err)
		}
		if state.Exists && !state.Released {
			if _, err = s.options.Secret.ApplyUsageInTx(ctx, tx, r, plan); err != nil {
				return safeError(err)
			}
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.connection_requests SET lease_released=true
 WHERE id=$1 AND command_digest=$2 AND credential_id=$3 AND credential_lease_id=$4 AND io_retired`,
			original.request, original.command, original.credential, original.lease)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(f.InvalidState)
		}
		closed := original
		closed.retired, closed.released = true, true
		if closed.phase == "pending" {
			closed.phase = "abandoned"
		}
		return clearStoppedOAuthConnection(ctx, x, closed)
	}))
}

// Called only under the original recovery claim/Guard and its complete lock
// set, after the actual Secret lease has retired. An abandoned SDK request
// cannot remain the current busy pointer. This is a local cancellation, not a
// claim about its remote result, and does not alter the original command or
// replace a newer Owner's connection publication.
func clearStoppedOAuthConnection(ctx context.Context, x postgres.SQLExecutor, original oauthConnectionRecoveryFact) error {
	if !original.retired || !original.released {
		return fail(f.Forbidden)
	}
	if original.phase == "abandoned" {
		_, err := x.Exec(ctx, `UPDATE agenteam_mcp.connections
 SET refresh_id=NULL,state=CASE WHEN state='connecting' THEN 'error' ELSE state END,
 last_error_code='mcp_cancelled',updated_at=clock_timestamp()
 WHERE id=$1 AND project_id=$2 AND config_id=$3 AND config_revision=$4 AND version=$5
 AND credential_id=$6 AND refresh_id=$7`, original.connection, original.project, original.config,
			int64(original.revision), int64(original.version), original.credential, original.request)
		if err != nil {
			return err
		}
	}
	return requireStoppedOAuthConnectionDetached(ctx, x, original)
}

func requireStoppedOAuthConnectionDetached(ctx context.Context, x postgres.SQLExecutor, original oauthConnectionRecoveryFact) error {
	var pending bool
	if err := x.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agenteam_mcp.connections
 WHERE id=$1 AND refresh_id=$2)`, original.connection, original.request).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return fail(f.ResourceBusy)
	}
	return nil
}
