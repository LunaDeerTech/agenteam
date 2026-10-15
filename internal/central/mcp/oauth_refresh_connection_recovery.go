package mcp

import (
	"context"
	"sync"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	"github.com/LunaDeerTech/agenteam/internal/central/postgres"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (v oauthConnectionRecoveryFact) matchesHumanRefresh(source oauthRefreshFact) bool {
	if source.validate() != nil || source.caller.command == "" ||
		v.command != source.caller.command || v.user != source.caller.user || v.session != source.caller.session ||
		v.project != source.project || v.config != source.config || v.revision != source.revision ||
		v.connection != source.connection || v.version != source.connectionVersion || v.credential != source.credential ||
		v.released && !v.retired {
		return false
	}
	for _, raw := range []string{v.request, v.credential, v.lease} {
		if _, err := f.ParseID[f.Request](raw); err != nil {
			return false
		}
	}
	return v.phase == "pending" || v.phase == "complete" || v.phase == "abandoned"
}

// prepareConnection creates its request with the first pending command in one
// transaction; replay of that pending command cannot create another request.
// refresh.prepare then checks the live private connection source in the same
// transaction that persists this process and full caller tuple. Still require
// exactly one matching request: neither a command digest alone nor absence can
// identify an original lease for recovery.
func loadHumanRefreshConnection(ctx context.Context, x postgres.SQLExecutor, source oauthRefreshFact) (oauthConnectionRecoveryFact, error) {
	var found oauthConnectionRecoveryFact
	rows, err := x.Query(ctx, `SELECT r.id,r.command_digest,r.project_id,r.config_id,r.config_revision,r.connection_id,
 r.connection_version,r.user_id,r.session_id,COALESCE(r.credential_id,''),COALESCE(r.credential_lease_id,''),
 r.phase,r.io_retired,r.lease_released
 FROM agenteam_mcp.connection_requests r JOIN agenteam_mcp.commands c ON c.command_digest=r.command_digest
 WHERE r.command_digest=$1 AND c.command_name='connection.refresh' AND c.scope_kind='project'
 AND c.project_id=r.project_id AND c.actor_user_id=r.user_id ORDER BY r.id LIMIT 2`, source.caller.command)
	if err != nil {
		return found, safeError(err)
	}
	defer rows.Close()
	return scanHumanRefreshConnection(rows, source)
}

type humanRefreshConnectionRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanHumanRefreshConnection(rows humanRefreshConnectionRows, source oauthRefreshFact) (oauthConnectionRecoveryFact, error) {
	var found oauthConnectionRecoveryFact
	count := 0
	for rows.Next() {
		count++
		if err := rows.Scan(&found.request, &found.command, &found.project, &found.config, &found.revision, &found.connection,
			&found.version, &found.user, &found.session, &found.credential, &found.lease, &found.phase, &found.retired, &found.released); err != nil {
			return oauthConnectionRecoveryFact{}, safeError(err)
		}
	}
	if err := rows.Err(); err != nil {
		return oauthConnectionRecoveryFact{}, safeError(err)
	}
	if count != 1 || !found.matchesHumanRefresh(source) {
		return oauthConnectionRecoveryFact{}, fail(f.InvalidState)
	}
	return found, nil
}

type humanRefreshRecoveryKey struct{}
type humanRefreshRecoveryUse struct {
	mu        sync.Mutex
	recovery  *oauthRefreshRecovery
	original  oauthConnectionRecoveryFact
	request   sc.UsageRequest
	mapping   f.Digest
	locks     []f.LockRequest
	active    bool
	validated f.Tx
}

func (w *humanRefreshRecoveryUse) close() { w.mu.Lock(); w.active = false; w.mu.Unlock() }

func (s *Service) humanRefreshRecoveryUse(ctx context.Context, r sc.UsageRequest) (*humanRefreshRecoveryUse, error) {
	if s == nil || ctx == nil || r.Validate() != nil || r.Purpose != sc.MCP || r.Action != sc.ReleaseLeaseUsage {
		return nil, fail(f.Forbidden)
	}
	w, ok := ctx.Value(humanRefreshRecoveryKey{}).(*humanRefreshRecoveryUse)
	if !ok || w == nil || w.recovery == nil || w.recovery.service != s || !w.recovery.stopped ||
		w.recovery.original.validate() != nil || s.owner == nil || w.recovery.call == nil ||
		w.recovery.call.owner != s.owner || w.recovery.original.process == s.owner.process.String() {
		return nil, fail(f.Forbidden)
	}
	s.owner.mu.Lock()
	_, registered := s.owner.calls[w.recovery.call]
	s.owner.mu.Unlock()
	if !registered {
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

func (s *Service) discoverHumanRefreshRecovery(ctx context.Context, r sc.UsageRequest) (sc.UsageDependencies, error) {
	w, err := s.humanRefreshRecoveryUse(ctx, r)
	if err != nil {
		return sc.UsageDependencies{}, err
	}
	binding, _ := sc.UsageBinding(r)
	return sc.NewUsageDependencies(s.secretIssuer, binding, w.mapping, w.locks)
}

func (s *Service) validateHumanRefreshRecovery(ctx context.Context, tx f.Tx, r sc.UsageRequest, plan sc.UsageDependencies) error {
	w, err := s.humanRefreshRecoveryUse(ctx, r)
	if err != nil {
		return err
	}
	binding, _ := sc.UsageBinding(r)
	if !plan.Matches(s.secretIssuer, binding, w.mapping) {
		return fail(f.Forbidden)
	}
	if err = s.store.RequireHeldLocks(ctx, tx, w.locks); err != nil {
		return safeError(err)
	}
	if err = w.recovery.require(ctx, tx); err != nil {
		return err
	}
	x, err := s.store.InTx(tx)
	if err != nil {
		return err
	}
	source, err := loadOAuthRefreshFact(ctx, x, w.recovery.original.request)
	if err != nil {
		return err
	}
	if !w.recovery.original.sameOrigin(source) || !source.retired || !source.released || source.phase == "claimed" {
		return fail(f.Forbidden)
	}
	current, err := loadHumanRefreshConnection(ctx, x, source)
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

func (s *Service) authorizeHumanRefreshRecovery(ctx context.Context, tx f.Tx, actor id.Actor, ref sc.CredentialRef, owner sc.CredentialLeaseOwner, action sc.LeaseAction) (sc.UseGrant, error) {
	if ctx == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	w, ok := ctx.Value(humanRefreshRecoveryKey{}).(*humanRefreshRecoveryUse)
	if !ok || w == nil {
		return sc.UseGrant{}, fail(f.Forbidden)
	}
	if _, err := s.humanRefreshRecoveryUse(ctx, w.request); err != nil {
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

func (w *oauthRefreshRecovery) releaseHumanConnection(ctx context.Context) (bool, error) {
	if w.original.caller.command == "" {
		return true, nil // Execution and startup retain their own retirement owners.
	}
	s := w.service
	var commandName string
	err := s.store.QueryRow(ctx, `SELECT command_name FROM agenteam_mcp.commands
 WHERE command_digest=$1 AND actor_user_id=$2 AND scope_kind='project' AND project_id=$3`,
		w.original.caller.command, w.original.caller.user, w.original.project).Scan(&commandName)
	if err != nil {
		return false, safeError(err)
	}
	if commandName != "connection.refresh" {
		return true, nil // This tail is scoped to the existing Owner Refresh command.
	}
	project, err := f.ParseID[id.Project](w.original.project)
	if err != nil {
		return false, fail(f.InvalidState)
	}
	connection, err := f.ParseID[ConnectionIdentity](w.original.connection)
	if err != nil {
		return false, fail(f.InvalidState)
	}
	locks, err := startupLocks(project, connection)
	if err != nil {
		return false, err
	}
	var original oauthConnectionRecoveryFact
	needed := false
	cause, _ := f.NewRecoveryCause("mcp.oauth.refresh_connection_io", w.original.request, "")
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
		if err := s.store.AcquireAll(ctx, tx, locks); err != nil {
			return err
		}
		if err := w.require(ctx, tx); err != nil {
			return err
		}
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		source, err := loadOAuthRefreshFact(ctx, x, w.original.request)
		if err != nil {
			return err
		}
		if !w.original.sameOrigin(source) || !source.retired || !source.released || source.phase == "claimed" {
			return fail(f.Forbidden)
		}
		original, err = loadHumanRefreshConnection(ctx, x, source)
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
	if err = commit(result); err != nil || !needed {
		return err == nil, err
	}
	observer, ok := s.options.Secret.(sc.MCPRequestLeaseObservation)
	if !ok || nilPort(observer) {
		return false, fail(f.DependencyUnbound)
	}
	scope, _ := id.InProject(project)
	registration, _ := id.RegisterService(id.SecretService)
	actor, err := registration.Actor(original.request, scope)
	if err != nil {
		return false, err
	}
	credential, _ := f.ParseID[sc.Credential](original.credential)
	ref, _ := sc.NewCredentialRef(credential, scope)
	lease, _ := f.ParseID[sc.Lease](original.lease)
	owner, _ := sc.NewCredentialLeaseOwner(sc.MCPConnectionRequestOwner, original.request)
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, Action: sc.ReleaseLeaseUsage, LeaseID: lease, LeaseOwner: owner}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return false, err
	}
	use := &humanRefreshRecoveryUse{recovery: w, original: original, request: r, locks: locks, active: true,
		mapping: f.Digest(digest([]byte(string(binding) + ":" + w.original.request + ":" + w.original.process)))}
	defer use.close()
	ctx = context.WithValue(ctx, humanRefreshRecoveryKey{}, use)
	plan, err := s.options.Secret.DiscoverUsage(ctx, r)
	if err != nil {
		return false, safeError(err)
	}
	cause, _ = f.NewRecoveryCause("mcp.oauth.refresh_connection_lease", original.request, "")
	err = commit(s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
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
	return err == nil, err
}

func (w *oauthRefreshRecovery) observeWithConnection(ctx context.Context) (bool, error) {
	confirmed, err := w.observe(ctx)
	if err != nil || !confirmed {
		return false, err
	}
	return w.releaseHumanConnection(ctx)
}
