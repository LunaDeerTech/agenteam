package mcp

import (
	"context"

	f "github.com/LunaDeerTech/agenteam/internal/central/foundation"
	id "github.com/LunaDeerTech/agenteam/internal/central/identity/contract"
	sc "github.com/LunaDeerTech/agenteam/internal/central/secret/contract"
)

func (s *Service) releaseStoppedOAuthLease(ctx context.Context, recovery *oauthRecoveryUse) error {
	original := recovery.original
	if original.operation != "exchange" || original.lease == "" || nilPort(s.options.Secret) {
		return fail(f.Forbidden)
	}
	observer, ok := s.options.Secret.(sc.MCPRequestLeaseObservation)
	if !ok || nilPort(observer) {
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
	r := sc.UsageRequest{Actor: actor, Ref: ref, Purpose: sc.MCP, LeaseOwner: owner, LeaseID: lease, Action: sc.ReleaseLeaseUsage}
	binding, err := sc.UsageBinding(r)
	if err != nil {
		return err
	}
	w := &oauthUsage{service: s, request: r, requestID: original.request, version: original.credentialVersion,
		locks: recovery.locks, mapping: f.Digest(digest([]byte(string(binding) + ":" + recovery.claim))), subject: actor, active: true,
		check: func(ctx context.Context, tx f.Tx) error {
			current, err := recovery.require(ctx, tx)
			if err != nil {
				return err
			}
			if current.operation != "exchange" || !current.retired || current.lease != original.lease || current.credential != original.credential || current.credentialVersion != original.credentialVersion {
				return fail(f.Forbidden)
			}
			return nil
		}}
	defer w.close()
	ctx = context.WithValue(ctx, oauthUsageKey{}, w)
	plan, err := s.options.Secret.DiscoverUsage(ctx, r)
	if err != nil {
		return safeError(err)
	}
	cause, _ := f.NewRecoveryCause("mcp.oauth.stopped_lease", original.request, "")
	result := s.store.WithinTx(ctx, cause, func(ctx context.Context, tx f.Tx) error {
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
		// A missing lease is usable here only after the actual stopped-owner
		// proof and the Secret provider's original full-lock observation.
		x, err := s.store.InTx(tx)
		if err != nil {
			return err
		}
		tag, err := x.Exec(ctx, `UPDATE agenteam_mcp.oauth_requests SET lease_released=true
 WHERE id=$1 AND process_id=$2 AND io_retired AND credential_lease_id=$3`, original.request, original.process, original.lease)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fail(f.InvalidState)
		}
		return nil
	})
	return commit(result)
}
